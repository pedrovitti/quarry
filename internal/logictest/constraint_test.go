package logictest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/exec"
)

// The data files cover what a constraint does to a row. These cover what a
// constraint does to the database around it: the schema text every table is
// printed through, the dump that has to carry the constraints to the next
// session, and the promise that a refused statement wrote nothing.

func mustExec(t *testing.T, engine *exec.Engine, statements ...string) {
	t.Helper()
	for _, sql := range statements {
		if _, err := engine.Exec(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
}

func mustFail(t *testing.T, engine *exec.Engine, sql string) error {
	t.Helper()
	if _, err := engine.Exec(sql); err != nil {
		return err
	}
	t.Fatalf("%s: succeeded, want an error", sql)
	return nil
}

func schemaLine(t *testing.T, engine *exec.Engine, table string) string {
	t.Helper()
	prefix := "CREATE TABLE " + table + " ("
	for _, line := range strings.Split(engine.Dump(), "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("no CREATE TABLE for %s in:\n%s", table, engine.Dump())
	return ""
}

func rowCount(t *testing.T, engine *exec.Engine, table string) int {
	t.Helper()
	count, err := engine.RowCount(table)
	if err != nil {
		t.Fatalf("RowCount(%s): %v", table, err)
	}
	return count
}

func TestSchemaTextCarriesEveryConstraint(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE orders (id INT PRIMARY KEY, sku TEXT UNIQUE, qty INT DEFAULT 1 CHECK (qty > 0))")

	want := "CREATE TABLE orders (id INT NOT NULL PRIMARY KEY, sku TEXT UNIQUE, " +
		"qty INT DEFAULT 1 CHECK (qty > 0));"
	if got := schemaLine(t, engine, "orders"); got != want {
		t.Errorf("schema text is\n%s\nwant\n%s", got, want)
	}
}

func TestSchemaTextPutsTheClausesInOneOrder(t *testing.T) {
	// However they were written, so two tables that mean the same thing print
	// the same way.
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE a (x INT CHECK (x > 0) DEFAULT 5 UNIQUE NOT NULL)",
		"CREATE TABLE b (x INT NOT NULL UNIQUE DEFAULT 5 CHECK (x > 0))")

	first := strings.TrimPrefix(schemaLine(t, engine, "a"), "CREATE TABLE a")
	second := strings.TrimPrefix(schemaLine(t, engine, "b"), "CREATE TABLE b")
	if first != second {
		t.Errorf("the same column printed two ways:\n%s\n%s", first, second)
	}
	if want := " (x INT NOT NULL UNIQUE DEFAULT 5 CHECK (x > 0));"; first != want {
		t.Errorf("= %q, want %q", first, want)
	}
}

func TestSchemaTextPutsTableConstraintsAfterTheColumns(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE seats (aisle INT, letter TEXT, holder TEXT, PRIMARY KEY (aisle, letter), CHECK (aisle > 0))")

	want := "CREATE TABLE seats (aisle INT NOT NULL, letter TEXT NOT NULL, holder TEXT, " +
		"PRIMARY KEY (aisle, letter), CHECK (aisle > 0));"
	if got := schemaLine(t, engine, "seats"); got != want {
		t.Errorf("schema text is\n%s\nwant\n%s", got, want)
	}
}

func TestSchemaTextReducesADefaultToItsValue(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine, "CREATE TABLE reduced (a INT DEFAULT 2 * 3, b FLOAT DEFAULT 1, c TEXT DEFAULT 'x' || 'y')")

	want := "CREATE TABLE reduced (a INT DEFAULT 6, b FLOAT DEFAULT 1.0, c TEXT DEFAULT 'xy');"
	if got := schemaLine(t, engine, "reduced"); got != want {
		t.Errorf("schema text is\n%s\nwant\n%s", got, want)
	}
}

func TestSchemaTextMentionsOnlyWhatWasDeclared(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE plain (a INT, b TEXT NOT NULL, c FLOAT)",
		"CREATE TABLE spare (a INT, b TEXT UNIQUE, c FLOAT)")

	tests := []struct {
		table string
		want  string
	}{
		{"plain", "CREATE TABLE plain (a INT, b TEXT NOT NULL, c FLOAT);"},
		{"spare", "CREATE TABLE spare (a INT, b TEXT UNIQUE, c FLOAT);"},
	}
	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			if got := schemaLine(t, engine, tt.table); got != tt.want {
				t.Errorf("schema text is\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestADumpReloadsIntoTheSameSchemaText(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE people (id INT PRIMARY KEY, email TEXT UNIQUE, age INT DEFAULT 0 CHECK (age >= 0))",
		"CREATE TABLE seats (aisle INT, letter TEXT, UNIQUE (aisle, letter), CHECK (aisle > 0))",
		"INSERT INTO people VALUES (1, 'ann@example.com', 31)",
		"INSERT INTO seats VALUES (1, 'a')")

	dump := engine.Dump()
	reloaded := exec.New()
	if err := reloaded.Load(dump); err != nil {
		t.Fatalf("Load returned error: %v\n%s", err, dump)
	}
	if got := reloaded.Dump(); got != dump {
		t.Errorf("the dump changed across a reload\n got:\n%s\nwant:\n%s", got, dump)
	}
}

func TestConstraintsStillHoldAfterAReload(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE people (id INT PRIMARY KEY, email TEXT UNIQUE, age INT DEFAULT 7 CHECK (age >= 0))",
		"INSERT INTO people VALUES (1, 'ann@example.com', 31)")

	reloaded := exec.New()
	if err := reloaded.Load(engine.Dump()); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	for _, sql := range []string{
		"INSERT INTO people VALUES (1, 'bo@example.com', 4)",
		"INSERT INTO people VALUES (2, 'ann@example.com', 4)",
		"INSERT INTO people VALUES (3, 'cyd@example.com', -4)",
		"INSERT INTO people VALUES (NULL, 'dee@example.com', 4)",
	} {
		if _, err := reloaded.Exec(sql); err == nil {
			t.Errorf("%s: succeeded after a reload", sql)
		}
	}

	mustExec(t, reloaded, "INSERT INTO people (id, email) VALUES (2, 'bo@example.com')")
	result, err := reloaded.Exec("SELECT age FROM people WHERE id = 2")
	if err != nil {
		t.Fatal(err)
	}
	if got := Render(result); got != "age\n7" {
		t.Errorf("the default did not survive the reload: %q", got)
	}
}

func TestARefusedInsertWritesNothing(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE t (id INT PRIMARY KEY, n INT CHECK (n < 10))",
		"INSERT INTO t VALUES (1, 1)")

	refused := []string{
		"INSERT INTO t VALUES (2, 2), (3, 3), (2, 4)",
		"INSERT INTO t VALUES (4, 4), (5, 50)",
		"INSERT INTO t VALUES (6, 6), (1, 6)",
		"INSERT INTO t VALUES (7, 7), (NULL, 7)",
	}
	for _, sql := range refused {
		mustFail(t, engine, sql)
		if got := rowCount(t, engine, "t"); got != 1 {
			t.Fatalf("%s left %d rows, want 1", sql, got)
		}
	}
}

func TestARefusedUpdateLeavesEveryRowAsItWas(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE t (id INT PRIMARY KEY, n INT CHECK (n < 10))",
		"INSERT INTO t VALUES (1, 1), (2, 2), (3, 3)")

	before, err := engine.Exec("SELECT id, n FROM t")
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"UPDATE t SET id = 1 WHERE id = 3",
		"UPDATE t SET id = 4",
		"UPDATE t SET n = 99 WHERE id = 2",
	} {
		mustFail(t, engine, sql)
	}

	after, err := engine.Exec("SELECT id, n FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if Render(before) != Render(after) {
		t.Errorf("the table changed\n got:\n%s\nwant:\n%s", Render(after), Render(before))
	}
}

func TestARowStillFitsWhereItAlreadyIs(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE t (id INT PRIMARY KEY, tag TEXT UNIQUE, n INT)",
		"INSERT INTO t VALUES (1, 'a', 1), (2, 'b', 2)",
		"UPDATE t SET n = n + 1",
		"UPDATE t SET id = id, tag = tag",
		"UPDATE t SET tag = 'c' WHERE id = 1",
		"UPDATE t SET tag = 'a' WHERE id = 1")

	result, err := engine.Exec("SELECT id, tag, n FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Render(result), "id|tag|n\n1|a|2\n2|b|3"; got != want {
		t.Errorf("= %q, want %q", got, want)
	}
}

func TestUnknownPassesACheckAndFailsAWhere(t *testing.T) {
	// The two read alike and mean opposite things: WHERE keeps what is true,
	// a check rejects only what is false.
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE t (a INT, b INT, CHECK (a < b))",
		"INSERT INTO t VALUES (1, 2), (NULL, 2), (1, NULL), (NULL, NULL)")

	if got := rowCount(t, engine, "t"); got != 4 {
		t.Fatalf("the table holds %d rows, want 4", got)
	}
	result, err := engine.Exec("SELECT a, b FROM t WHERE a < b")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Render(result), "a|b\n1|2"; got != want {
		t.Errorf("WHERE returned %q, want %q", got, want)
	}
}

func TestAUniqueColumnHoldsAnyNumberOfNulls(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine, "CREATE TABLE t (id INT PRIMARY KEY, tag TEXT UNIQUE)")
	for i := 0; i < 40; i++ {
		mustExec(t, engine, fmt.Sprintf("INSERT INTO t VALUES (%d, NULL)", i))
	}
	if got := rowCount(t, engine, "t"); got != 40 {
		t.Errorf("the table holds %d rows, want 40", got)
	}

	mustExec(t, engine, "UPDATE t SET tag = 'only' WHERE id = 1")
	err := mustFail(t, engine, "UPDATE t SET tag = 'only' WHERE id = 2")
	if !strings.Contains(err.Error(), "duplicate value") {
		t.Errorf("error = %q", err)
	}
	mustExec(t, engine, "UPDATE t SET tag = NULL WHERE id = 1")
	mustExec(t, engine, "UPDATE t SET tag = 'only' WHERE id = 2")
}

func TestTheMessageNamesTheValueAndTheConstraint(t *testing.T) {
	engine := exec.New()
	mustExec(t, engine,
		"CREATE TABLE t (a INT, b TEXT, c FLOAT, PRIMARY KEY (a, b), UNIQUE (c))",
		"INSERT INTO t VALUES (1, 'x', 2.5)")

	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"a key of two columns", "INSERT INTO t VALUES (1, 'x', 3.5)",
			`duplicate value (1, 'x') for PRIMARY KEY (a, b)`},
		{"a unique column", "INSERT INTO t VALUES (2, 'y', 2.5)",
			`duplicate value (2.5) for UNIQUE (c)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mustFail(t, engine, tt.sql)
			if err.Error() != tt.want {
				t.Errorf("error = %q, want %q", err, tt.want)
			}
		})
	}
}

func TestACheckIsSettledWhenTheTableIsMade(t *testing.T) {
	engine := exec.New()
	for _, sql := range []string{
		"CREATE TABLE t (a INT CHECK (a + 1))",
		"CREATE TABLE t (a INT CHECK (b > 0))",
		"CREATE TABLE t (a INT, PRIMARY KEY (b))",
		"CREATE TABLE t (a INT PRIMARY KEY, b INT PRIMARY KEY)",
		"CREATE TABLE t (a INT DEFAULT 'x')",
		"CREATE TABLE t (a INT DEFAULT a)",
	} {
		if _, err := engine.Exec(sql); err == nil {
			t.Errorf("%s: succeeded, want an error", sql)
		}
		if engine.Catalog().Has("t") {
			t.Fatalf("%s: left a table behind", sql)
		}
	}

	// The same shapes, said correctly, are accepted.
	mustExec(t, engine,
		"CREATE TABLE t (a INT CHECK (a > 0), b INT DEFAULT 1, PRIMARY KEY (a))",
		"INSERT INTO t (a) VALUES (1)")
	if !engine.Catalog().Has("t") {
		t.Fatal("a table with constraints was not created")
	}
}
