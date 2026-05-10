package exec

import (
	"strings"
	"testing"
)

// blank returns an engine with one empty three-column table.
func blank(t *testing.T) *Engine {
	t.Helper()
	e := New()
	if _, err := e.Exec("CREATE TABLE t (id INT NOT NULL, name TEXT, score FLOAT)"); err != nil {
		t.Fatal(err)
	}
	return e
}

func affected(t *testing.T, e *Engine, sql string) int {
	t.Helper()
	result, err := e.Exec(sql)
	if err != nil {
		t.Fatalf("Exec(%q) returned error: %v", sql, err)
	}
	return result.Affected
}

func TestCreateTable(t *testing.T) {
	e := New()
	result, err := e.Exec("CREATE TABLE t (a INT)")
	if err != nil {
		t.Fatalf("CREATE returned error: %v", err)
	}
	if result.Tag != "CREATE TABLE" {
		t.Errorf("Tag = %q", result.Tag)
	}
	if !e.Catalog().Has("t") {
		t.Error("the table is not in the catalog")
	}
	if result.RowsReturned() {
		t.Error("CREATE TABLE reported a result set")
	}
}

func TestCreateTableTwiceIsAnError(t *testing.T) {
	e := blank(t)
	err := execError(t, e, "CREATE TABLE t (a INT)")
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q", err)
	}
}

func TestDropTable(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("DROP TABLE t"); err != nil {
		t.Fatalf("DROP returned error: %v", err)
	}
	if e.Catalog().Has("t") {
		t.Error("the table is still in the catalog")
	}
	// Its rows are gone too, so recreating it gives an empty table.
	if _, err := e.Exec("CREATE TABLE t (a INT)"); err != nil {
		t.Fatal(err)
	}
	if got := query(t, e, "SELECT * FROM t"); got != "a" {
		t.Errorf("the recreated table is not empty:\n%s", got)
	}
}

func TestDropMissingTableIsAnError(t *testing.T) {
	e := New()
	err := execError(t, e, "DROP TABLE nope")
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q", err)
	}
}

func TestInsert(t *testing.T) {
	e := blank(t)
	if got := affected(t, e, "INSERT INTO t VALUES (1, 'a', 1.5)"); got != 1 {
		t.Errorf("Affected = %d, want 1", got)
	}
	if got := affected(t, e, "INSERT INTO t VALUES (2, 'b', 2.5), (3, 'c', 3.5)"); got != 2 {
		t.Errorf("Affected = %d, want 2", got)
	}

	want := "id|name|score\n1|a|1.5\n2|b|2.5\n3|c|3.5"
	if got := query(t, e, "SELECT * FROM t"); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestInsertWithNamedColumns(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t (name, id) VALUES ('a', 1)"); err != nil {
		t.Fatal(err)
	}
	// Unmentioned columns are NULL rather than a zero of their type.
	want := "id|name|score\n1|a|NULL"
	if got := query(t, e, "SELECT * FROM t"); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestInsertWidensIntegersIntoFloatColumns(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 2)"); err != nil {
		t.Fatal(err)
	}
	if got := query(t, e, "SELECT score FROM t"); got != "score\n2.0" {
		t.Errorf("got %q, want the value stored as a float", got)
	}
}

func TestInsertEvaluatesExpressions(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1 + 1, upper('a'), 1.0 / 2)"); err != nil {
		t.Fatal(err)
	}
	if got, want := query(t, e, "SELECT * FROM t"), "id|name|score\n2|A|0.5"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestInsertErrors(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{"unknown table", "INSERT INTO nope VALUES (1)", "does not exist"},
		{"unknown column", "INSERT INTO t (nope) VALUES (1)", `no column "nope"`},
		{"column named twice", "INSERT INTO t (id, id) VALUES (1, 2)", "named twice"},
		{"too few values", "INSERT INTO t VALUES (1, 'a')", "3 columns but 2 values"},
		{"too many values", "INSERT INTO t VALUES (1, 'a', 1.0, 9)", "3 columns but 4 values"},
		{"null into a not null column", "INSERT INTO t (name) VALUES ('a')", `column "id" must not be NULL`},
		{"explicit null into not null", "INSERT INTO t VALUES (NULL, 'a', 1.0)", `column "id" must not be NULL`},
		{"text into an int column", "INSERT INTO t VALUES ('x', 'a', 1.0)", "cannot store TEXT value in INT"},
		{"float into an int column", "INSERT INTO t VALUES (1.5, 'a', 1.0)", "cannot store FLOAT value in INT"},
		{"column reference as a value", "INSERT INTO t VALUES (id, 'a', 1.0)", `no column "id"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := blank(t)
			err := execError(t, e, tt.sql)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestInsertIsAllOrNothing(t *testing.T) {
	// A bad value in the last row must not leave the earlier ones written.
	e := blank(t)
	execError(t, e, "INSERT INTO t VALUES (1, 'a', 1.0), (2, 'b', 2.0), (NULL, 'c', 3.0)")

	if got := query(t, e, "SELECT * FROM t"); got != "id|name|score" {
		t.Errorf("rows were written despite the failure:\n%s", got)
	}
}

func TestUpdate(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 1.0), (2, 'b', 2.0), (3, 'c', 3.0)"); err != nil {
		t.Fatal(err)
	}

	if got := affected(t, e, "UPDATE t SET name = 'x' WHERE id = 2"); got != 1 {
		t.Errorf("Affected = %d, want 1", got)
	}
	want := "id|name|score\n1|a|1.0\n2|x|2.0\n3|c|3.0"
	if got := query(t, e, "SELECT * FROM t"); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUpdateWithoutWhereTouchesEveryRow(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 1.0), (2, 'b', 2.0)"); err != nil {
		t.Fatal(err)
	}
	if got := affected(t, e, "UPDATE t SET name = 'x'"); got != 2 {
		t.Errorf("Affected = %d, want 2", got)
	}
	if got := query(t, e, "SELECT name FROM t"); got != "name\nx\nx" {
		t.Errorf("got\n%s", got)
	}
}

func TestUpdateReadsTheRowAsItWas(t *testing.T) {
	// Assignments see the row before any of them ran, so this swaps rather
	// than copying one value over both.
	e := New()
	if _, err := e.Exec("CREATE TABLE p (a INT, b INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Exec("INSERT INTO p VALUES (1, 2)"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Exec("UPDATE p SET a = b, b = a"); err != nil {
		t.Fatal(err)
	}
	if got, want := query(t, e, "SELECT * FROM p"), "a|b\n2|1"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUpdateComputesFromTheCurrentRow(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 1.0), (2, 'b', 2.0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Exec("UPDATE t SET score = score * 10"); err != nil {
		t.Fatal(err)
	}
	if got, want := query(t, e, "SELECT score FROM t"), "score\n10.0\n20.0"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUpdateErrors(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{"unknown table", "UPDATE nope SET a = 1", "does not exist"},
		{"unknown column", "UPDATE t SET nope = 1", `no column "nope"`},
		{"assigned twice", "UPDATE t SET id = 1, id = 2", "assigned twice"},
		{"null into a not null column", "UPDATE t SET id = NULL", "must not be NULL"},
		{"wrong type", "UPDATE t SET id = 'x'", "cannot store TEXT value in INT"},
		{"unknown column in where", "UPDATE t SET name = 'x' WHERE nope = 1", `no column "nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := blank(t)
			if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 1.0)"); err != nil {
				t.Fatal(err)
			}
			err := execError(t, e, tt.sql)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestUpdateIsAllOrNothing(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 1.0), (2, 'b', 2.0)"); err != nil {
		t.Fatal(err)
	}
	// The update matches both rows and violates NOT NULL on the way, so
	// neither row may be left changed.
	before := query(t, e, "SELECT * FROM t")
	execError(t, e, "UPDATE t SET id = NULL")
	if got := query(t, e, "SELECT * FROM t"); got != before {
		t.Errorf("a failed update changed rows:\n%s\n%s", before, got)
	}
}

func TestDelete(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 1.0), (2, 'b', 2.0), (3, 'c', 3.0)"); err != nil {
		t.Fatal(err)
	}

	if got := affected(t, e, "DELETE FROM t WHERE id = 2"); got != 1 {
		t.Errorf("Affected = %d, want 1", got)
	}
	if got, want := query(t, e, "SELECT id FROM t"), "id\n1\n3"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestDeleteWithoutWhereEmptiesTheTable(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 1.0), (2, 'b', 2.0)"); err != nil {
		t.Fatal(err)
	}
	if got := affected(t, e, "DELETE FROM t"); got != 2 {
		t.Errorf("Affected = %d, want 2", got)
	}
	if got := query(t, e, "SELECT * FROM t"); got != "id|name|score" {
		t.Errorf("rows remain:\n%s", got)
	}
}

func TestDeleteMatchingNothing(t *testing.T) {
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 1.0)"); err != nil {
		t.Fatal(err)
	}
	if got := affected(t, e, "DELETE FROM t WHERE id = 99"); got != 0 {
		t.Errorf("Affected = %d, want 0", got)
	}
}

func TestInsertAfterDeleteKeepsScanOrder(t *testing.T) {
	// Deleting leaves a tombstone and new rows land after it, so the scan
	// order stays insertion order rather than reusing the hole.
	e := blank(t)
	if _, err := e.Exec("INSERT INTO t VALUES (1, 'a', 1.0), (2, 'b', 2.0), (3, 'c', 3.0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Exec("DELETE FROM t WHERE id = 2"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Exec("INSERT INTO t VALUES (4, 'd', 4.0)"); err != nil {
		t.Fatal(err)
	}
	if got, want := query(t, e, "SELECT id FROM t"), "id\n1\n3\n4"; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestExecAll(t *testing.T) {
	e := New()
	results, err := e.ExecAll(`
		CREATE TABLE t (a INT);
		INSERT INTO t VALUES (1), (2);
		SELECT * FROM t;
	`)
	if err != nil {
		t.Fatalf("ExecAll returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[1].Affected != 2 {
		t.Errorf("insert affected %d, want 2", results[1].Affected)
	}
	if len(results[2].Rows) != 2 {
		t.Errorf("select returned %d rows, want 2", len(results[2].Rows))
	}
}

func TestExecAllStopsAtTheFirstFailure(t *testing.T) {
	e := New()
	results, err := e.ExecAll("CREATE TABLE t (a INT); INSERT INTO t VALUES (1); INSERT INTO nope VALUES (1)")
	if err == nil {
		t.Fatal("ExecAll succeeded, want an error")
	}
	// The statements before the failure stay applied.
	if len(results) != 2 {
		t.Errorf("got %d results before the failure, want 2", len(results))
	}
	if got := query(t, e, "SELECT * FROM t"); got != "a\n1" {
		t.Errorf("the successful insert was lost:\n%s", got)
	}
}
