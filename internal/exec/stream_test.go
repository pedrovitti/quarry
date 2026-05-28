package exec

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

// drain pulls a stream to the end and renders it the way query does, so the
// two can be compared against each other.
func drain(t *testing.T, rows Rows) string {
	t.Helper()
	lines := []string{strings.Join(rows.Binding().Labels(), "|")}
	for {
		row, ok, err := rows.Next()
		if err != nil {
			t.Fatalf("Next returned error: %v", err)
		}
		if !ok {
			return strings.Join(lines, "\n")
		}
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = v.String()
		}
		lines = append(lines, strings.Join(cells, "|"))
	}
}

// TestQueryStreamsTheSameRowsAsExec is the contract between the two entry
// points: Query hands back the stream Exec would have collected, so a caller
// that does not want every row in memory gets the same answer.
func TestQueryStreamsTheSameRowsAsExec(t *testing.T) {
	sqls := []string{
		"SELECT * FROM customers",
		"SELECT name FROM customers WHERE city = 'london'",
		"SELECT DISTINCT city FROM customers",
		"SELECT name FROM customers ORDER BY name DESC",
		"SELECT id FROM customers LIMIT 2 OFFSET 1",
		"SELECT c.name, o.total FROM customers AS c JOIN orders AS o ON c.id = o.customer_id",
		"SELECT 1",
	}
	for _, sql := range sqls {
		t.Run(sql, func(t *testing.T) {
			e := fixture(t)
			rows, err := e.Query(sql)
			if err != nil {
				t.Fatalf("Query returned error: %v", err)
			}
			if got, want := drain(t, rows), query(t, fixture(t), sql); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestQueryTakesParameters(t *testing.T) {
	e := fixture(t)
	rows, err := e.Query("SELECT name FROM customers WHERE city = $1", types.Text("paris"))
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if got, want := drain(t, rows), "name\nbob"; got != want {
		t.Errorf("= %q, want %q", got, want)
	}
}

func TestQueryRejectsStatementsThatAreNotSelects(t *testing.T) {
	// Anything else has no rows to stream, and running it as a side effect of
	// asking for rows would be a surprise.
	e := fixture(t)
	for _, sql := range []string{
		"INSERT INTO customers VALUES (9, 'zoe', 'rome')",
		"UPDATE customers SET name = 'x'",
		"DELETE FROM customers",
		"CREATE TABLE t (a INT)",
		"DROP TABLE customers",
	} {
		t.Run(sql, func(t *testing.T) {
			if _, err := e.Query(sql); err == nil {
				t.Fatal("Query succeeded, want an error")
			}
		})
	}

	// And the statement must not have run.
	if got, err := e.RowCount("customers"); err != nil || got != 4 {
		t.Errorf("RowCount = %d (err %v), want 4", got, err)
	}
}

func TestQueryReportsParseAndPlanErrors(t *testing.T) {
	e := fixture(t)
	for _, sql := range []string{
		"SELECT (",
		"SELECT * FROM nosuch",
		"SELECT nosuch FROM customers",
	} {
		t.Run(sql, func(t *testing.T) {
			if _, err := e.Query(sql); err == nil {
				t.Fatal("Query succeeded, want an error")
			}
		})
	}
}

func TestAStreamStaysDoneOnceItIsDone(t *testing.T) {
	// Calling Next past the end returns false rather than wrapping around or
	// failing, which is what makes Collect's loop safe.
	e := fixture(t)
	rows, err := e.Query("SELECT id FROM customers LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := rows.Next(); err != nil || !ok {
		t.Fatalf("the first Next gave ok=%v, err=%v", ok, err)
	}
	for i := 0; i < 3; i++ {
		if _, ok, err := rows.Next(); err != nil || ok {
			t.Fatalf("Next after the end gave ok=%v, err=%v", ok, err)
		}
	}
}

func TestBindingDescribesTheStream(t *testing.T) {
	e := fixture(t)
	rows, err := e.Query("SELECT c.name, o.total FROM customers AS c JOIN orders AS o ON c.id = o.customer_id")
	if err != nil {
		t.Fatal(err)
	}

	binding := rows.Binding()
	if got, want := binding.Len(), 2; got != want {
		t.Fatalf("Len = %d, want %d", got, want)
	}
	if got, want := strings.Join(binding.Labels(), ","), "name,total"; got != want {
		t.Errorf("Labels = %q, want %q", got, want)
	}

	columns := binding.Columns()
	if len(columns) != 2 {
		t.Fatalf("Columns returned %d entries", len(columns))
	}
	// A joined column keeps the name it is reached by, which is the alias.
	if got, want := columns[0].Table, "c"; got != want {
		t.Errorf("the first column belongs to %q, want %q", got, want)
	}
	if got, want := columns[1].Table, "o"; got != want {
		t.Errorf("the second column belongs to %q, want %q", got, want)
	}
}

func TestBindingColumnsIsACopy(t *testing.T) {
	// Columns hands out a slice; writing to it must not reach the binding, or
	// one caller's scribble becomes every later caller's column list.
	e := fixture(t)
	rows, err := e.Query("SELECT * FROM customers")
	if err != nil {
		t.Fatal(err)
	}

	binding := rows.Binding()
	before := binding.Column(0)
	columns := binding.Columns()
	columns[0].Name = "scribbled"
	if got := binding.Column(0); got != before {
		t.Errorf("the binding changed to %+v, want %+v", got, before)
	}
}

func TestRowCount(t *testing.T) {
	e := fixture(t)
	if got, err := e.RowCount("customers"); err != nil || got != 4 {
		t.Errorf("RowCount = %d (err %v), want 4", got, err)
	}
	if got, err := e.RowCount("orders"); err != nil || got != 3 {
		t.Errorf("RowCount = %d (err %v), want 3", got, err)
	}
}

func TestRowCountFollowsTheRows(t *testing.T) {
	e := fixture(t)
	steps := []struct {
		sql  string
		want int
	}{
		{"INSERT INTO customers VALUES (5, 'eve', 'rome')", 5},
		{"DELETE FROM customers WHERE id = 1", 4},
		{"UPDATE customers SET city = 'x'", 4}, // an update moves nothing
		{"DELETE FROM customers", 0},
	}
	for _, step := range steps {
		t.Run(step.sql, func(t *testing.T) {
			if _, err := e.Exec(step.sql); err != nil {
				t.Fatal(err)
			}
			if got, err := e.RowCount("customers"); err != nil || got != step.want {
				t.Errorf("RowCount = %d (err %v), want %d", got, err, step.want)
			}
		})
	}
}

func TestRowCountOfATableThatIsNotThere(t *testing.T) {
	e := fixture(t)
	if _, err := e.RowCount("nosuch"); err == nil {
		t.Fatal("RowCount succeeded, want an error")
	}

	if _, err := e.Exec("DROP TABLE customers"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RowCount("customers"); err == nil {
		t.Error("RowCount succeeded on a dropped table")
	}
}

func TestRunRejectsAStatementItCannotExecute(t *testing.T) {
	// Run switches on the node type; a nil statement is the one input that
	// reaches the default arm.
	if _, err := New().Run(nil); err == nil {
		t.Fatal("Run(nil) succeeded, want an error")
	}
}

func TestRowsReturnedTellsAQueryFromACommand(t *testing.T) {
	e := fixture(t)
	tests := []struct {
		sql  string
		want bool
	}{
		{"SELECT * FROM customers", true},
		{"SELECT 1", true},
		{"INSERT INTO customers VALUES (9, 'zoe', 'rome')", false},
		{"UPDATE customers SET city = 'x' WHERE id = 9", false},
		{"DELETE FROM customers WHERE id = 9", false},
		{"CREATE TABLE t (a INT)", false},
		{"DROP TABLE t", false},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			result, err := e.Exec(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			if got := result.RowsReturned(); got != tt.want {
				t.Errorf("RowsReturned = %v, want %v (tag %q)", got, tt.want, result.Tag)
			}
			if !tt.want && len(result.Rows) != 0 {
				t.Errorf("a command returned %d rows", len(result.Rows))
			}
		})
	}
}
