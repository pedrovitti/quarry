package exec

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

func TestPrepareCountsParameters(t *testing.T) {
	e := fixture(t)
	tests := []struct {
		sql  string
		want int
	}{
		{"SELECT * FROM customers", 0},
		{"SELECT * FROM customers WHERE id = $1", 1},
		{"SELECT * FROM customers WHERE id = $1 AND city = $2", 2},
		{"SELECT * FROM customers WHERE id = $1 OR id = $1", 1},
		{"SELECT * FROM customers LIMIT $1 OFFSET $2", 2},
		{"INSERT INTO customers VALUES ($1, $2, $3)", 3},
		{"UPDATE customers SET name = $1 WHERE id = $2", 2},
		{"DELETE FROM customers WHERE id = $1", 1},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			prepared, err := e.Prepare(tt.sql)
			if err != nil {
				t.Fatalf("Prepare returned error: %v", err)
			}
			if got := prepared.NumParams(); got != tt.want {
				t.Errorf("NumParams() = %d, want %d", got, tt.want)
			}
			if prepared.SQL() != tt.sql {
				t.Errorf("SQL() = %q", prepared.SQL())
			}
		})
	}
}

func TestPrepareRejectsGapsInNumbering(t *testing.T) {
	// There is nothing sensible for a caller to pass in the gap.
	e := fixture(t)
	_, err := e.Prepare("SELECT * FROM customers WHERE id = $1 AND city = $3")
	if err == nil {
		t.Fatal("a gap in the numbering was accepted")
	}
	if !strings.Contains(err.Error(), "never $2") {
		t.Errorf("error = %q", err)
	}
}

func TestPrepareRejectsBadSQL(t *testing.T) {
	e := fixture(t)
	if _, err := e.Prepare("SELECT FROM"); err == nil {
		t.Error("Prepare accepted a syntax error")
	}
}

func TestPreparedExec(t *testing.T) {
	e := fixture(t)
	prepared, err := e.Prepare("SELECT name FROM customers WHERE id = $1")
	if err != nil {
		t.Fatal(err)
	}

	// The same statement, run repeatedly with different values.
	tests := []struct {
		id   int64
		want string
	}{
		{1, "ada"},
		{2, "bob"},
		{3, "cy"},
		{99, ""},
	}
	for _, tt := range tests {
		result, err := prepared.Exec(types.Int(tt.id))
		if err != nil {
			t.Fatalf("Exec(%d) returned error: %v", tt.id, err)
		}
		if tt.want == "" {
			if len(result.Rows) != 0 {
				t.Errorf("id %d matched %d rows, want none", tt.id, len(result.Rows))
			}
			continue
		}
		if len(result.Rows) != 1 {
			t.Fatalf("id %d matched %d rows, want 1", tt.id, len(result.Rows))
		}
		if got, _ := result.Rows[0][0].Text(); got != tt.want {
			t.Errorf("id %d gave %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestPreparedChecksArgumentCount(t *testing.T) {
	e := fixture(t)
	prepared, err := e.Prepare("SELECT * FROM customers WHERE id = $1 AND city = $2")
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]types.Value{
		nil,
		{types.Int(1)},
		{types.Int(1), types.Text("london"), types.Int(9)},
	} {
		_, err := prepared.Exec(args...)
		if err == nil {
			t.Errorf("Exec with %d arguments succeeded, want an error", len(args))
			continue
		}
		if !strings.Contains(err.Error(), "takes 2 parameters") {
			t.Errorf("error = %q", err)
		}
	}
}

func TestPreparedArityMessageIsSingularForOne(t *testing.T) {
	e := fixture(t)
	prepared, err := e.Prepare("SELECT * FROM customers WHERE id = $1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = prepared.Exec()
	if err == nil {
		t.Fatal("Exec with no arguments succeeded")
	}
	if !strings.Contains(err.Error(), "takes 1 parameter,") {
		t.Errorf("error = %q, want the singular form", err)
	}
}

func TestPreparedInsertRunsRepeatedly(t *testing.T) {
	e := New()
	if _, err := e.Exec("CREATE TABLE t (id INT NOT NULL, name TEXT)"); err != nil {
		t.Fatal(err)
	}

	prepared, err := e.Prepare("INSERT INTO t VALUES ($1, $2)")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		result, err := prepared.Exec(types.Int(int64(i)), types.Text(string(rune('a'+i-1))))
		if err != nil {
			t.Fatalf("Exec %d returned error: %v", i, err)
		}
		if result.Affected != 1 {
			t.Errorf("Affected = %d, want 1", result.Affected)
		}
	}

	if got, want := query(t, e, "SELECT * FROM t"), "id|name\n1|a\n2|b\n3|c"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestPreparedQueryReturnsAStream(t *testing.T) {
	e := fixture(t)
	prepared, err := e.Prepare("SELECT id FROM customers WHERE id > $1")
	if err != nil {
		t.Fatal(err)
	}

	rows, err := prepared.Query(types.Int(2))
	if err != nil {
		t.Fatal(err)
	}
	collected, err := Collect(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(collected) != 2 {
		t.Errorf("got %d rows, want 2", len(collected))
	}
}

func TestPreparedQueryRejectsNonSelect(t *testing.T) {
	e := fixture(t)
	prepared, err := e.Prepare("DELETE FROM customers WHERE id = $1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Query(types.Int(1)); err == nil {
		t.Error("Query accepted a DELETE")
	}
}

func TestPreparedSeesLaterSchemaChanges(t *testing.T) {
	// Expressions are compiled per execution, so a statement prepared before a
	// table was recreated resolves against the new one.
	e := New()
	if _, err := e.Exec("CREATE TABLE t (a INT)"); err != nil {
		t.Fatal(err)
	}
	prepared, err := e.Prepare("SELECT a FROM t WHERE a = $1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Exec(types.Int(1)); err != nil {
		t.Fatal(err)
	}

	if _, err := e.Exec("DROP TABLE t"); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Exec(types.Int(1)); err == nil {
		t.Error("a statement over a dropped table still ran")
	}

	if _, err := e.Exec("CREATE TABLE t (a INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Exec("INSERT INTO t VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	result, err := prepared.Exec(types.Int(1))
	if err != nil {
		t.Fatalf("after recreating the table: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("got %d rows, want 1", len(result.Rows))
	}
}

func TestPreparedParameterKindsCanVary(t *testing.T) {
	// Nothing binds a parameter to a type at prepare time, so the same
	// statement can take an int one run and a string the next.
	e := New()
	if _, err := e.Exec("CREATE TABLE t (a INT, s TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'x')"); err != nil {
		t.Fatal(err)
	}

	prepared, err := e.Prepare("SELECT $1")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []types.Value{types.Int(1), types.Text("a"), types.Null(), types.Bool(true)} {
		result, err := prepared.Exec(v)
		if err != nil {
			t.Fatalf("Exec(%v) returned error: %v", v, err)
		}
		if result.Rows[0][0] != v {
			t.Errorf("got %v, want %v", result.Rows[0][0], v)
		}
	}
}
