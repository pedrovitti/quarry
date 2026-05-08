package exec

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

// fixture returns an engine holding two small related tables.
func fixture(t *testing.T) *Engine {
	t.Helper()
	e := New()
	setup := []string{
		"CREATE TABLE customers (id INT NOT NULL, name TEXT, city TEXT)",
		"INSERT INTO customers VALUES (1, 'ada', 'london'), (2, 'bob', 'paris'), (3, 'cy', 'london'), (4, 'dee', NULL)",
		"CREATE TABLE orders (id INT, customer_id INT, total FLOAT)",
		"INSERT INTO orders VALUES (10, 1, 99.5), (11, 1, 5.0), (12, 2, 20.0)",
	}
	for _, sql := range setup {
		if _, err := e.Exec(sql); err != nil {
			t.Fatalf("setup %q: %v", sql, err)
		}
	}
	return e
}

// query runs a SELECT and renders the result as "col|col" then one line per
// row, so a whole result set is one string to compare against.
func query(t *testing.T, e *Engine, sql string, params ...types.Value) string {
	t.Helper()
	result, err := e.Exec(sql, params...)
	if err != nil {
		t.Fatalf("Exec(%q) returned error: %v", sql, err)
	}

	lines := []string{strings.Join(result.Columns, "|")}
	for _, row := range result.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = v.String()
		}
		lines = append(lines, strings.Join(cells, "|"))
	}
	return strings.Join(lines, "\n")
}

// execError runs a statement expecting it to fail.
func execError(t *testing.T, e *Engine, sql string) error {
	t.Helper()
	if _, err := e.Exec(sql); err != nil {
		return err
	}
	t.Fatalf("Exec(%q) succeeded, want an error", sql)
	return nil
}

func TestSelectAll(t *testing.T) {
	e := fixture(t)
	want := strings.Join([]string{
		"id|name|city",
		"1|ada|london",
		"2|bob|paris",
		"3|cy|london",
		"4|dee|NULL",
	}, "\n")
	if got := query(t, e, "SELECT * FROM customers"); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSelectRowsComeBackInInsertionOrder(t *testing.T) {
	// Without ORDER BY the order is unspecified in SQL, but quarry pins it to
	// insertion order so results are reproducible.
	e := fixture(t)
	first := query(t, e, "SELECT id FROM customers")
	for i := 0; i < 20; i++ {
		if got := query(t, e, "SELECT id FROM customers"); got != first {
			t.Fatalf("run %d differed:\n%s\n%s", i, first, got)
		}
	}
}

func TestSelectColumns(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"one column", "SELECT name FROM customers", "name\nada\nbob\ncy\ndee"},
		{"reordered", "SELECT city, id FROM customers WHERE id = 1", "city|id\nlondon|1"},
		{"repeated", "SELECT id, id FROM customers WHERE id = 1", "id|id\n1|1"},
		{"aliased", "SELECT name AS who FROM customers WHERE id = 1", "who\nada"},
		{"qualified", "SELECT customers.name FROM customers WHERE id = 1", "name\nada"},
		{"expression", "SELECT id * 10 FROM customers WHERE id = 2", "id * 10\n20"},
		{"aliased expression", "SELECT id * 10 AS x FROM customers WHERE id = 2", "x\n20"},
		{"constant", "SELECT 1", "1\n1"},
		{"arithmetic", "SELECT 1 + 2 * 3", "1 + 2 * 3\n7"},
		{"function", "SELECT upper(name) FROM customers WHERE id = 1", "upper(name)\nADA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := fixture(t)
			if got := query(t, e, tt.sql); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestStarExpansion(t *testing.T) {
	e := fixture(t)
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			"qualified star",
			"SELECT c.* FROM customers c WHERE id = 1",
			"id|name|city\n1|ada|london",
		},
		{
			"star with a column",
			"SELECT id, c.* FROM customers c WHERE id = 1",
			"id|id|name|city\n1|1|ada|london",
		},
		{
			"star over a join covers both sides",
			"SELECT * FROM customers c JOIN orders o ON c.id = o.customer_id WHERE o.id = 12",
			"id|name|city|id|customer_id|total\n2|bob|paris|12|2|20.0",
		},
		{
			"qualified star over a join covers one side",
			"SELECT o.* FROM customers c JOIN orders o ON c.id = o.customer_id WHERE o.id = 12",
			"id|customer_id|total\n12|2|20.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := query(t, e, tt.sql); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestWhere(t *testing.T) {
	e := fixture(t)
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"equality", "SELECT id FROM customers WHERE city = 'london'", "id\n1\n3"},
		{"inequality", "SELECT id FROM customers WHERE id > 2", "id\n3\n4"},
		{"and", "SELECT id FROM customers WHERE id > 1 AND city = 'london'", "id\n3"},
		{"or", "SELECT id FROM customers WHERE id = 1 OR id = 4", "id\n1\n4"},
		{"not", "SELECT id FROM customers WHERE NOT id = 1", "id\n2\n3\n4"},
		{"is null", "SELECT id FROM customers WHERE city IS NULL", "id\n4"},
		{"is not null", "SELECT id FROM customers WHERE city IS NOT NULL", "id\n1\n2\n3"},
		{"no matches", "SELECT id FROM customers WHERE id = 99", "id"},
		{"always true", "SELECT id FROM customers WHERE TRUE", "id\n1\n2\n3\n4"},
		{"always false", "SELECT id FROM customers WHERE FALSE", "id"},
		{"function in predicate", "SELECT id FROM customers WHERE upper(name) = 'ADA'", "id\n1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := query(t, e, tt.sql); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestWhereWithNullIsNotTrue(t *testing.T) {
	// The row with a NULL city matches neither the predicate nor its negation,
	// because unknown does not pass a filter.
	e := fixture(t)

	matching := query(t, e, "SELECT id FROM customers WHERE city = 'nowhere'")
	if matching != "id" {
		t.Errorf("equality against a NULL row matched: %s", matching)
	}
	negated := query(t, e, "SELECT id FROM customers WHERE city <> 'london'")
	if strings.Contains(negated, "4") {
		t.Errorf("the NULL row passed a <> filter:\n%s", negated)
	}
}

func TestOrderBy(t *testing.T) {
	e := fixture(t)
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"ascending", "SELECT name FROM customers ORDER BY name", "name\nada\nbob\ncy\ndee"},
		{"descending", "SELECT name FROM customers ORDER BY name DESC", "name\ndee\ncy\nbob\nada"},
		{"numeric", "SELECT id FROM customers ORDER BY id DESC", "id\n4\n3\n2\n1"},
		{"by an alias", "SELECT name AS who FROM customers ORDER BY who DESC", "who\ndee\ncy\nbob\nada"},
		{"by an expression", "SELECT id FROM customers ORDER BY id * -1", "id\n4\n3\n2\n1"},
		{"nulls first ascending", "SELECT city FROM customers ORDER BY city", "city\nNULL\nlondon\nlondon\nparis"},
		{"nulls last descending", "SELECT city FROM customers ORDER BY city DESC", "city\nparis\nlondon\nlondon\nNULL"},
		{
			"two keys",
			"SELECT city, name FROM customers ORDER BY city DESC, name DESC",
			"city|name\nparis|bob\nlondon|cy\nlondon|ada\nNULL|dee",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := query(t, e, tt.sql); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestOrderByIsStableOnTies(t *testing.T) {
	// Rows tied on the sort key keep their input order, so the result of an
	// ambiguous ORDER BY is still the same on every run.
	e := fixture(t)
	first := query(t, e, "SELECT name FROM customers ORDER BY city")
	for i := 0; i < 20; i++ {
		if got := query(t, e, "SELECT name FROM customers ORDER BY city"); got != first {
			t.Fatalf("run %d differed:\n%s\n%s", i, first, got)
		}
	}
}

func TestDistinct(t *testing.T) {
	e := fixture(t)
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"dedupes", "SELECT DISTINCT city FROM customers ORDER BY city", "city|NULL|london|paris"},
		{"keeps distinct rows", "SELECT DISTINCT id FROM customers", "id|1|2|3|4"},
		{
			// No ORDER BY: with two output columns both called "city" the
			// name is ambiguous, so the rows come back in input order.
			"over several columns",
			"SELECT DISTINCT city, city FROM customers",
			"city|city|london|london|paris|paris|NULL|NULL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.ReplaceAll(query(t, e, tt.sql), "\n", "|")
			if got != tt.want {
				t.Errorf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestDistinctTreatsNullsAsEqual(t *testing.T) {
	// DISTINCT groups NULLs together even though NULL = NULL is unknown.
	e := New()
	if _, err := e.Exec("CREATE TABLE t (a INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Exec("INSERT INTO t VALUES (NULL), (NULL), (1)"); err != nil {
		t.Fatal(err)
	}
	if got, want := query(t, e, "SELECT DISTINCT a FROM t"), "a\nNULL\n1"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestLimitAndOffset(t *testing.T) {
	e := fixture(t)
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"limit", "SELECT id FROM customers ORDER BY id LIMIT 2", "id\n1\n2"},
		{"limit past the end", "SELECT id FROM customers ORDER BY id LIMIT 99", "id\n1\n2\n3\n4"},
		{"limit zero", "SELECT id FROM customers LIMIT 0", "id"},
		{"offset", "SELECT id FROM customers ORDER BY id OFFSET 2", "id\n3\n4"},
		{"offset past the end", "SELECT id FROM customers OFFSET 99", "id"},
		{"both", "SELECT id FROM customers ORDER BY id LIMIT 2 OFFSET 1", "id\n2\n3"},
		{"offset zero", "SELECT id FROM customers ORDER BY id LIMIT 1 OFFSET 0", "id\n1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := query(t, e, tt.sql); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestJoin(t *testing.T) {
	e := fixture(t)
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			"matching rows only",
			"SELECT c.name, o.total FROM customers c JOIN orders o ON c.id = o.customer_id ORDER BY o.id",
			"name|total\nada|99.5\nada|5.0\nbob|20.0",
		},
		{
			"unmatched rows are dropped",
			"SELECT c.name FROM customers c JOIN orders o ON c.id = o.customer_id WHERE c.name = 'cy'",
			"name",
		},
		{
			"join with a filter",
			"SELECT o.id FROM customers c JOIN orders o ON c.id = o.customer_id WHERE o.total > 10 ORDER BY o.id",
			"id\n10\n12",
		},
		{
			"compound condition",
			"SELECT o.id FROM customers c JOIN orders o ON c.id = o.customer_id AND o.total > 50",
			"id\n10",
		},
		{
			"join condition that matches nothing",
			"SELECT o.id FROM customers c JOIN orders o ON c.id = o.id",
			"id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := query(t, e, tt.sql); got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestAmbiguousColumnIsAnError(t *testing.T) {
	// Both tables have an "id", so a bare one has no defensible answer.
	e := fixture(t)
	err := execError(t, e, "SELECT id FROM customers c JOIN orders o ON c.id = o.customer_id")
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error = %q, want it to say the column is ambiguous", err)
	}
}

func TestParameters(t *testing.T) {
	e := fixture(t)
	got := query(t, e, "SELECT name FROM customers WHERE id = $1", types.Int(2))
	if want := "name\nbob"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	got = query(t, e, "SELECT id FROM customers ORDER BY id LIMIT $1 OFFSET $2", types.Int(2), types.Int(1))
	if want := "id\n2\n3"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMissingParameterIsAnError(t *testing.T) {
	e := fixture(t)
	_, err := e.Exec("SELECT * FROM customers WHERE id = $2", types.Int(1))
	if err == nil {
		t.Fatal("a query with an unbound parameter succeeded")
	}
	if !strings.Contains(err.Error(), "$2") {
		t.Errorf("error = %q, want it to name the parameter", err)
	}
}

func TestQueryErrors(t *testing.T) {
	e := fixture(t)
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{"unknown table", "SELECT * FROM nope", `table "nope" does not exist`},
		{"unknown column", "SELECT nope FROM customers", `no column "nope"`},
		{"unknown column lists the real ones", "SELECT nope FROM customers", "available columns are"},
		{"unknown qualified column", "SELECT c.nope FROM customers c", `no column "c.nope"`},
		{"wrong qualifier", "SELECT x.id FROM customers c", `no column "x.id"`},
		{"unknown function", "SELECT nosuch(name) FROM customers", `no function "nosuch"`},
		{"wrong arity", "SELECT upper(name, 2) FROM customers", "upper takes 1 argument"},
		{"comparing unlike types", "SELECT * FROM customers WHERE name > 1", "cannot compare TEXT with INT"},
		{"adding text to a number", "SELECT name + 1 FROM customers", "cannot apply + to TEXT and INT"},
		{"concat on a number", "SELECT id || 'x' FROM customers", "|| needs text"},
		{"non-boolean predicate", "SELECT * FROM customers WHERE id", "not a condition"},
		{"and on non-booleans", "SELECT * FROM customers WHERE id AND id", "needs booleans"},
		{"negative limit", "SELECT * FROM customers LIMIT -1", "must not be negative"},
		{"non-integer limit", "SELECT * FROM customers LIMIT 'x'", "LIMIT needs an integer"},
		{"star over nothing", "SELECT *", "no columns to expand"},
		{"unknown star qualifier", "SELECT x.* FROM customers c", `no table "x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := execError(t, e, tt.sql)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestDivisionByZeroIsARuntimeError(t *testing.T) {
	e := fixture(t)
	err := execError(t, e, "SELECT id / 0 FROM customers")
	if !strings.Contains(err.Error(), "division by zero") {
		t.Errorf("error = %q", err)
	}
}
