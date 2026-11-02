package logictest

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/exec"
	"github.com/pedrovitti/quarry/internal/types"
)

// The data files cover what a subquery answers. These cover what it is: an
// expression, so it carries a heading, holds parameters, survives preparing,
// and reaches the same errors from every statement that has a WHERE.

func newItems(t *testing.T) *exec.Engine {
	t.Helper()
	engine := exec.New()
	for _, sql := range []string{
		"CREATE TABLE items (id INT, name TEXT, bin INT)",
		"INSERT INTO items VALUES (1, 'bolt', 10), (2, 'nut', 20), (3, 'washer', 10), (4, 'cap', NULL)",
		"CREATE TABLE bins (id INT, tag TEXT, cap INT)",
		"INSERT INTO bins VALUES (10, 'a', 2), (20, 'b', 1), (30, 'c', 9)",
	} {
		if _, err := engine.Exec(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	return engine
}

func TestASubqueryIsHeadedByTheSqlThatWroteIt(t *testing.T) {
	tests := []string{
		"bin IN (SELECT id FROM bins)",
		"bin NOT IN (SELECT id FROM bins)",
		"bin = ANY (SELECT id FROM bins)",
		"bin < ALL (SELECT id FROM bins)",
		"(SELECT tag FROM bins WHERE id = 10)",
		"(SELECT cap FROM bins WHERE id = 10) + 1",
	}
	engine := newItems(t)
	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			result := queryOne(t, engine, "SELECT "+expr+" FROM items WHERE id = 1")
			if len(result.Columns) != 1 {
				t.Fatalf("got %d columns, want 1", len(result.Columns))
			}
			if result.Columns[0] != expr {
				t.Fatalf("heading = %q, want %q", result.Columns[0], expr)
			}

			again := queryOne(t, engine, "SELECT "+result.Columns[0]+" FROM items WHERE id = 1")
			if again.Columns[0] != expr {
				t.Errorf("the heading did not parse back to itself: %q", again.Columns[0])
			}
		})
	}
}

func TestParametersInsideASubqueryAreCounted(t *testing.T) {
	tests := []struct {
		sql  string
		want int
	}{
		{"SELECT id FROM items WHERE bin IN (SELECT id FROM bins WHERE cap = $1)", 1},
		{"SELECT id FROM items WHERE bin = ANY (SELECT id FROM bins WHERE cap > $1 AND tag <> $2)", 2},
		{"SELECT (SELECT tag FROM bins WHERE id = $1) FROM items WHERE id = $2", 2},
		{"SELECT id FROM items WHERE bin IN (SELECT id FROM bins WHERE cap = $1) AND id = $2", 2},
	}
	engine := newItems(t)
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			prepared, err := engine.Prepare(tt.sql)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if got := prepared.NumParams(); got != tt.want {
				t.Errorf("NumParams = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAPreparedSubqueryRunsAgainWithOtherValues(t *testing.T) {
	engine := newItems(t)
	prepared, err := engine.Prepare(
		"SELECT id FROM items WHERE bin IN (SELECT id FROM bins WHERE tag = $1) ORDER BY id")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	first, err := prepared.Exec(types.Text("a"))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.Join(rowsOf(t, first), ","); got != "1,3" {
		t.Errorf("first run = %q, want 1,3", got)
	}

	second, err := prepared.Exec(types.Text("b"))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.Join(rowsOf(t, second), ","); got != "2" {
		t.Errorf("second run = %q, want 2", got)
	}
}

func TestASubqueryMustReturnOneColumn(t *testing.T) {
	engine := newItems(t)
	want := "a subquery must return one column"
	tests := []struct{ name, sql string }{
		{"a list", "SELECT id FROM items WHERE bin IN (SELECT id, tag FROM bins)"},
		{"a quantified list", "SELECT id FROM items WHERE bin = ANY (SELECT id, tag FROM bins)"},
		{"a value", "SELECT (SELECT id, tag FROM bins) FROM items"},
		{"a star", "SELECT id FROM items WHERE bin IN (SELECT * FROM bins)"},
		{"an update", "UPDATE items SET bin = (SELECT id, tag FROM bins)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorFrom(t, engine, tt.sql); got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
		})
	}
}

func TestAScalarSubqueryAnswersByItsRowCount(t *testing.T) {
	engine := newItems(t)

	one := queryOne(t, engine, "SELECT (SELECT tag FROM bins WHERE id = 10) AS a")
	if got := strings.Join(rowsOf(t, one), ","); got != "a" {
		t.Errorf("one row = %q, want a", got)
	}

	none := queryOne(t, engine, "SELECT (SELECT tag FROM bins WHERE id = 99) AS a")
	if got := strings.Join(rowsOf(t, none), ","); got != "NULL" {
		t.Errorf("no rows = %q, want NULL", got)
	}

	want := "a subquery used as a value returned more than one row"
	if got := errorFrom(t, engine, "SELECT (SELECT tag FROM bins) AS a"); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestTheKindsAcrossTheQueryBoundaryAreNamedBeforeRunning(t *testing.T) {
	engine := newItems(t)
	tests := []struct{ name, sql, want string }{
		{"a list", "SELECT id FROM items WHERE bin IN (SELECT tag FROM bins)",
			"cannot compare INT with TEXT"},
		{"a quantified list", "SELECT id FROM items WHERE bin < ALL (SELECT tag FROM bins)",
			"cannot compare INT with TEXT"},
		{"the other way round", "SELECT id FROM items WHERE name IN (SELECT cap FROM bins)",
			"cannot compare TEXT with INT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorFrom(t, engine, tt.sql); got != tt.want {
				t.Errorf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAParameterInsideASubquerySettlesTheKinds(t *testing.T) {
	engine := newItems(t)
	want := "cannot compare INT with TEXT"
	sql := "SELECT id FROM items WHERE bin IN (SELECT id FROM bins WHERE cap = $1)"
	if got := errorFrom(t, engine, sql, types.Text("x")); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestACorrelatedSubqueryReadsTheOuterRow(t *testing.T) {
	engine := newItems(t)
	result := queryOne(t, engine,
		"SELECT id, (SELECT tag FROM bins WHERE bins.id = items.bin) AS tag FROM items ORDER BY id")
	if got := strings.Join(rowsOf(t, result), ";"); got != "1|a;2|b;3|a;4|NULL" {
		t.Errorf("= %q, want 1|a;2|b;3|a;4|NULL", got)
	}
}

func TestTheInnerFromWinsAName(t *testing.T) {
	engine := newItems(t)

	inner := queryOne(t, engine, "SELECT (SELECT id FROM bins WHERE tag = 'a') AS a FROM items WHERE items.id = 2")
	if got := strings.Join(rowsOf(t, inner), ","); got != "10" {
		t.Errorf("= %q, want 10: id belongs to the inner FROM", got)
	}

	outer := queryOne(t, engine, "SELECT (SELECT tag FROM bins WHERE bins.id = bin) AS a FROM items WHERE id = 1")
	if got := strings.Join(rowsOf(t, outer), ","); got != "a" {
		t.Errorf("= %q, want a: bin is only outside", got)
	}

	if got := errorFrom(t, engine, "SELECT (SELECT tag FROM bins WHERE bins.id = nope) FROM items"); !strings.Contains(got, `no column "nope"`) {
		t.Errorf("error = %q, want an unknown column", got)
	}
}

func TestASubqueryJoinsTwoTables(t *testing.T) {
	engine := newItems(t)
	result := queryOne(t, engine,
		"SELECT items.id FROM items JOIN bins ON items.bin = bins.id "+
			"WHERE bins.cap IN (SELECT cap FROM bins WHERE tag = 'a') ORDER BY items.id")
	if got := strings.Join(rowsOf(t, result), ","); got != "1,3" {
		t.Errorf("rows = %q, want 1,3", got)
	}
}

func TestUpdateAndDeleteReachSubqueriesToo(t *testing.T) {
	engine := newItems(t)

	if _, err := engine.Exec(
		"UPDATE items SET name = 'seen' WHERE bin IN (SELECT id FROM bins WHERE tag = 'a')"); err != nil {
		t.Fatalf("UPDATE: %v", err)
	}
	result := queryOne(t, engine, "SELECT name FROM items ORDER BY id")
	if got := strings.Join(rowsOf(t, result), ","); got != "seen,nut,seen,cap" {
		t.Errorf("after the update: %q", got)
	}

	deleted, err := engine.Exec("DELETE FROM items WHERE bin = (SELECT id FROM bins WHERE tag = 'b')")
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	if deleted.Affected != 1 {
		t.Errorf("deleted %d rows, want 1", deleted.Affected)
	}
}

func TestTheSameSubqueryQueryAnswersTheSameWayEveryTime(t *testing.T) {
	engine := newItems(t)
	sql := "SELECT id, bin IN (SELECT id FROM bins WHERE tag = 'a') AS a, " +
		"(SELECT tag FROM bins WHERE bins.id = items.bin) AS b FROM items ORDER BY id"

	first := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";")
	for i := 0; i < 10; i++ {
		if got := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";"); got != first {
			t.Fatalf("run %d gave %q, want %q", i, got, first)
		}
	}
	if first != "1|true|a;2|false|b;3|true|a;4|NULL|NULL" {
		t.Errorf("= %q", first)
	}
}

func TestParametersInsideADerivedTableAreCounted(t *testing.T) {
	tests := []struct {
		sql  string
		want int
	}{
		{"SELECT id FROM (SELECT id FROM bins WHERE cap > $1) AS b", 1},
		{"SELECT id FROM (SELECT id, cap FROM bins WHERE cap > $1) AS b WHERE id < $2", 2},
		{"SELECT id FROM items WHERE bin IN (SELECT id FROM (SELECT id FROM bins WHERE cap <> $1) AS b)", 1},
	}
	engine := newItems(t)
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			prepared, err := engine.Prepare(tt.sql)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if got := prepared.NumParams(); got != tt.want {
				t.Errorf("NumParams = %d, want %d", got, tt.want)
			}

			args := make([]types.Value, tt.want)
			for i := range args {
				args[i] = types.Int(1)
			}
			if _, err := prepared.Exec(args...); err != nil {
				t.Errorf("Exec: %v", err)
			}
		})
	}
}

func TestAPreparedDerivedTableRunsAgainWithOtherValues(t *testing.T) {
	engine := newItems(t)
	prepared, err := engine.Prepare(
		"SELECT b.id FROM (SELECT id FROM bins WHERE tag = $1) AS b ORDER BY b.id")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	first, err := prepared.Exec(types.Text("a"))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.Join(rowsOf(t, first), ","); got != "10" {
		t.Errorf("first run = %q, want 10", got)
	}

	second, err := prepared.Exec(types.Text("b"))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.Join(rowsOf(t, second), ","); got != "20" {
		t.Errorf("second run = %q, want 20", got)
	}
}

func TestADerivedTableNeverReadsTheStatementAroundIt(t *testing.T) {
	engine := newItems(t)
	sql := "SELECT id FROM items WHERE bin IN " +
		"(SELECT id FROM (SELECT id FROM bins WHERE bins.id = items.bin) AS d)"
	if got := errorFrom(t, engine, sql); !strings.Contains(got, `no column "items.bin"`) {
		t.Errorf("error = %q, want the outer name to be unknown inside FROM", got)
	}
}

func TestASubqueryInFromNeedsAName(t *testing.T) {
	engine := newItems(t)
	tests := []struct{ name, sql string }{
		{"alone", "SELECT id FROM (SELECT id FROM bins)"},
		{"joined", "SELECT items.id FROM items JOIN (SELECT id FROM bins) ON items.bin = id"},
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

func TestTheSameDerivedTableQueryAnswersTheSameWayEveryTime(t *testing.T) {
	engine := newItems(t)
	sql := "SELECT items.id, big.tag FROM items " +
		"JOIN (SELECT id, tag FROM bins WHERE cap >= 2) AS big ON items.bin = big.id " +
		"ORDER BY items.id"

	first := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";")
	for i := 0; i < 10; i++ {
		if got := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";"); got != first {
			t.Fatalf("run %d gave %q, want %q", i, got, first)
		}
	}
	if first != "1|a;3|a" {
		t.Errorf("= %q", first)
	}
}
