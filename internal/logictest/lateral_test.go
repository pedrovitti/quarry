package logictest

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/exec"
	"github.com/pedrovitti/quarry/internal/types"
)

func newDepot(t *testing.T) *exec.Engine {
	t.Helper()
	engine := exec.New()
	for _, sql := range []string{
		"CREATE TABLE bins (id INT, tag TEXT)",
		"INSERT INTO bins VALUES (10, 'a'), (20, 'b'), (30, 'c')",
		"CREATE TABLE parts (sku TEXT, bin INT, qty INT)",
		"INSERT INTO parts VALUES ('bolt', 10, 7), ('nut', 10, 3), ('cap', 20, 9), ('washer', 20, 5), ('pin', NULL, 1)",
	} {
		if _, err := engine.Exec(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	return engine
}

func TestALateralRunsForEachRowBeforeIt(t *testing.T) {
	engine := newDepot(t)
	result := queryOne(t, engine,
		"SELECT b.tag, top.sku FROM bins b JOIN LATERAL "+
			"(SELECT sku FROM parts WHERE parts.bin = b.id ORDER BY qty DESC LIMIT 1) top ON TRUE "+
			"ORDER BY b.tag")
	if got := strings.Join(rowsOf(t, result), ";"); got != "a|bolt;b|cap" {
		t.Errorf("= %q, want a|bolt;b|cap", got)
	}
}

func TestOnlyTheTablesBeforeALateralAreVisible(t *testing.T) {
	engine := newDepot(t)

	t.Run("unmarked stays blind", func(t *testing.T) {
		sql := "SELECT b.tag, p.sku FROM bins b JOIN " +
			"(SELECT sku FROM parts WHERE parts.bin = b.id) p ON TRUE"
		if got := errorFrom(t, engine, sql); !strings.Contains(got, `no column "b.id"`) {
			t.Errorf("error = %q, want the left table to be unknown", got)
		}
	})

	t.Run("marked sees the left side", func(t *testing.T) {
		sql := "SELECT b.tag, p.sku FROM bins b JOIN LATERAL " +
			"(SELECT sku FROM parts WHERE parts.bin = b.id) p ON TRUE ORDER BY b.tag, p.sku"
		result := queryOne(t, engine, sql)
		if got := strings.Join(rowsOf(t, result), ";"); got != "a|bolt;a|nut;b|cap;b|washer" {
			t.Errorf("= %q, want a|bolt;a|nut;b|cap;b|washer", got)
		}
	})

	t.Run("the enclosing statement stays out of reach", func(t *testing.T) {
		sql := "SELECT id FROM bins WHERE id IN (SELECT v FROM LATERAL (SELECT bins.tag AS v) d)"
		if got := errorFrom(t, engine, sql); !strings.Contains(got, `no column "bins.tag"`) {
			t.Errorf("error = %q, want the outer table to be unknown inside FROM", got)
		}
	})

	t.Run("out of reach through a join too", func(t *testing.T) {
		sql := "SELECT id FROM bins WHERE id IN " +
			"(SELECT d.v FROM parts p JOIN LATERAL (SELECT bins.tag AS v) d ON TRUE)"
		if got := errorFrom(t, engine, sql); !strings.Contains(got, `no column "bins.tag"`) {
			t.Errorf("error = %q, want the outer table to be unknown inside FROM", got)
		}
	})
}

func TestParametersInsideALateralAreCounted(t *testing.T) {
	tests := []struct {
		sql  string
		args []types.Value
	}{
		{"SELECT b.tag FROM bins b JOIN LATERAL (SELECT sku FROM parts WHERE parts.bin = b.id AND qty > $1) p ON TRUE",
			[]types.Value{types.Int(1)}},
		{"SELECT b.tag, p.sku FROM bins b JOIN LATERAL (SELECT sku FROM parts WHERE parts.bin = b.id AND qty > $1) p ON p.sku <> $2",
			[]types.Value{types.Int(1), types.Text("nut")}},
		{"SELECT d.v FROM LATERAL (SELECT $1 + 1 AS v) d",
			[]types.Value{types.Int(1)}},
	}
	engine := newDepot(t)
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			prepared, err := engine.Prepare(tt.sql)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if got := prepared.NumParams(); got != len(tt.args) {
				t.Errorf("NumParams = %d, want %d", got, len(tt.args))
			}
			if _, err := prepared.Exec(tt.args...); err != nil {
				t.Errorf("Exec: %v", err)
			}
		})
	}
}

func TestAPreparedLateralRunsAgainWithOtherValues(t *testing.T) {
	engine := newDepot(t)
	prepared, err := engine.Prepare(
		"SELECT b.tag, p.sku FROM bins b JOIN LATERAL " +
			"(SELECT sku FROM parts WHERE parts.bin = b.id AND qty > $1) p ON TRUE ORDER BY b.tag, p.sku")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	first, err := prepared.Exec(types.Int(4))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.Join(rowsOf(t, first), ";"); got != "a|bolt;b|cap;b|washer" {
		t.Errorf("first run = %q, want a|bolt;b|cap;b|washer", got)
	}

	second, err := prepared.Exec(types.Int(8))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.Join(rowsOf(t, second), ";"); got != "b|cap" {
		t.Errorf("second run = %q, want b|cap", got)
	}
}

func TestALateralNeedsANameToo(t *testing.T) {
	engine := newDepot(t)
	tests := []struct{ name, sql string }{
		{"alone", "SELECT one FROM LATERAL (SELECT 1 AS one)"},
		{"joined", "SELECT sku FROM bins b JOIN LATERAL (SELECT sku FROM parts WHERE parts.bin = b.id) ON TRUE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := "a subquery in FROM needs a name"
			if got := errorFrom(t, engine, tt.sql); !strings.Contains(got, want) {
				t.Errorf("error = %q, want it to contain %q", got, want)
			}
		})
	}
}

func TestTheKindsThroughALateralAreNamedBeforeRunning(t *testing.T) {
	engine := newDepot(t)
	sql := "SELECT b.id FROM bins b JOIN LATERAL " +
		"(SELECT sku FROM parts WHERE parts.bin = b.id) p ON TRUE WHERE p.sku IN (1, 2)"
	if got := errorFrom(t, engine, sql); got != "cannot compare TEXT with INT" {
		t.Errorf("error = %q, want %q", got, "cannot compare TEXT with INT")
	}
}

func TestAStarExpandsTheLateralColumnsInPlace(t *testing.T) {
	engine := newDepot(t)
	result := queryOne(t, engine,
		"SELECT * FROM bins b JOIN LATERAL "+
			"(SELECT sku FROM parts WHERE parts.bin = b.id AND qty > 6) p ON TRUE ORDER BY b.id")
	if got := strings.Join(result.Columns, "|"); got != "id|tag|sku" {
		t.Errorf("columns = %q, want id|tag|sku", got)
	}
	if got := strings.Join(rowsOf(t, result), ";"); got != "10|a|bolt;20|b|cap" {
		t.Errorf("= %q, want 10|a|bolt;20|b|cap", got)
	}
}

func TestTheSameLateralQueryAnswersTheSameWayEveryTime(t *testing.T) {
	engine := newDepot(t)
	sql := "SELECT b.tag, d.v FROM bins b JOIN LATERAL " +
		"(SELECT (SELECT sku FROM parts WHERE parts.bin = b.id ORDER BY qty DESC LIMIT 1) AS v) d ON TRUE " +
		"ORDER BY b.tag"

	first := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";")
	for i := 0; i < 10; i++ {
		if got := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";"); got != first {
			t.Fatalf("run %d gave %q, want %q", i, got, first)
		}
	}
	if first != "a|bolt;b|cap;c|NULL" {
		t.Errorf("= %q", first)
	}
}
