package exec

import (
	"strings"
	"testing"
)

// Planning happens before any row is read, so the errors here are the ones a
// user sees immediately and the column names are what the output is labelled
// with. Both are decided once per statement rather than per row.

func TestPlanningRejectsWhatItCannotResolve(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{"a missing table", "SELECT * FROM nosuch", `table "nosuch" does not exist`},
		{"a missing table on the right of a join",
			"SELECT * FROM customers JOIN nosuch ON TRUE", `table "nosuch" does not exist`},
		{"a missing table in the middle of a chain",
			"SELECT * FROM customers JOIN nosuch ON TRUE JOIN orders ON TRUE",
			`table "nosuch" does not exist`},
		{"a missing column", "SELECT nosuch FROM customers", `no column "nosuch"`},
		{"a missing column in a WHERE", "SELECT id FROM customers WHERE nosuch = 1", `no column "nosuch"`},
		{"a missing column in an ON", "SELECT customers.id FROM customers JOIN orders ON nosuch = 1",
			`no column "nosuch"`},
		{"a qualifier naming no table", "SELECT x.id FROM customers", `no column "x.id"`},
		{"a star qualified by no table", "SELECT x.* FROM customers", "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := fixture(t)
			_, err := e.Exec(tt.sql)
			if err == nil {
				t.Fatal("the statement ran, want a planning error")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestAmbiguousColumnsAreRefusedRatherThanGuessed(t *testing.T) {
	// Both tables have an id, so a bare one has no defensible answer. The
	// message says how to fix it, since the fix is not obvious.
	e := fixture(t)
	_, err := e.Exec("SELECT id FROM customers JOIN orders ON customers.id = orders.customer_id")
	if err == nil {
		t.Fatal("an ambiguous column was resolved")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error = %q", err)
	}
	if !strings.Contains(err.Error(), "qualify it") {
		t.Errorf("error = %q, want it to say what to do", err)
	}
}

func TestQualifyingResolvesAnAmbiguousColumn(t *testing.T) {
	e := fixture(t)
	got := query(t, e, "SELECT customers.id FROM customers JOIN orders ON customers.id = orders.customer_id ORDER BY orders.id")
	if want := "id\n1\n1\n2"; got != want {
		t.Errorf("= %q, want %q", got, want)
	}
}

func TestOutputColumnNames(t *testing.T) {
	// What a column is called in the output, which is also the name ORDER BY
	// can reach it by.
	tests := []struct {
		sql  string
		want string
	}{
		{"SELECT id FROM customers", "id"},
		{"SELECT customers.id FROM customers", "id"},
		{"SELECT id AS n FROM customers", "n"},
		{"SELECT customers.id AS n FROM customers", "n"},

		// A computed column has no table to belong to, so it is labelled with
		// the expression that produced it.
		{"SELECT id + 1 FROM customers", "id + 1"},
		{"SELECT id + 1 AS n FROM customers", "n"},
		{"SELECT upper(name) FROM customers", "upper(name)"},
		{"SELECT 1 FROM customers", "1"},
		{"SELECT 'x' FROM customers", "'x'"},

		// Parentheses are part of the expression as written, so they are part
		// of the label.
		{"SELECT (id) FROM customers", "id"},
		{"SELECT (id + 1) FROM customers", "(id + 1)"},

		// A star expands to the columns it stands for, under their own names.
		{"SELECT * FROM customers", "id,name,city"},
		{"SELECT customers.* FROM customers", "id,name,city"},
		{"SELECT id, * FROM customers", "id,id,name,city"},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			e := fixture(t)
			result, err := e.Exec(tt.sql)
			if err != nil {
				t.Fatalf("Exec returned error: %v", err)
			}
			if got := strings.Join(result.Columns, ","); got != tt.want {
				t.Errorf("columns = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStarOverAJoinCoversBothSidesInOrder(t *testing.T) {
	e := fixture(t)
	result, err := e.Exec("SELECT * FROM customers AS c JOIN orders AS o ON c.id = o.customer_id")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(result.Columns, ","), "id,name,city,id,customer_id,total"; got != want {
		t.Errorf("columns = %q, want %q", got, want)
	}

	// And a qualified star covers only its side.
	result, err = e.Exec("SELECT o.* FROM customers AS c JOIN orders AS o ON c.id = o.customer_id")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(result.Columns, ","), "id,customer_id,total"; got != want {
		t.Errorf("columns = %q, want %q", got, want)
	}
}

func TestAnAliasReplacesTheTableName(t *testing.T) {
	// Once a table is aliased, the alias is the only name that reaches it.
	e := fixture(t)

	if got := query(t, e, "SELECT c.id FROM customers AS c WHERE c.id = 1"); got != "id\n1" {
		t.Errorf("= %q", got)
	}
	if _, err := e.Exec("SELECT customers.id FROM customers AS c"); err == nil {
		t.Error("the original table name still resolved")
	}
}

func TestPlanningErrorsHappenBeforeAnyRowIsRead(t *testing.T) {
	// A statement that cannot be planned must not have run: this one would
	// otherwise have deleted rows on its way to failing.
	e := fixture(t)
	if _, err := e.Exec("DELETE FROM customers WHERE nosuch = 1"); err == nil {
		t.Fatal("the delete ran")
	}
	if got, err := e.RowCount("customers"); err != nil || got != 4 {
		t.Errorf("RowCount = %d (err %v), want 4", got, err)
	}

	if _, err := e.Exec("UPDATE customers SET name = nosuch"); err == nil {
		t.Fatal("the update ran")
	}
	if got := query(t, e, "SELECT name FROM customers WHERE id = 1"); got != "name\nada" {
		t.Errorf("the update changed a row: %q", got)
	}
}

func TestSelectWithNoFromRunsOverOneEmptyRow(t *testing.T) {
	e := fixture(t)
	if got := query(t, e, "SELECT 1"); got != "1\n1" {
		t.Errorf("= %q", got)
	}
	// One row, not none and not one per row of some table.
	result, err := e.Exec("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("returned %d rows, want 1", len(result.Rows))
	}
	// And no column can be named, since there is no table under it.
	if _, err := e.Exec("SELECT id"); err == nil {
		t.Error("a column resolved with no FROM")
	}
}
