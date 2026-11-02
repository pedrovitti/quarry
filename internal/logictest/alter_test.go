package logictest

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/cli"
	"github.com/pedrovitti/quarry/internal/exec"
	"github.com/pedrovitti/quarry/internal/types"
)

func alterEngine(t *testing.T) *exec.Engine {
	t.Helper()
	engine := exec.New()
	for _, sql := range []string{
		"CREATE TABLE inventory (sku INT NOT NULL, name TEXT, qty INT)",
		"INSERT INTO inventory VALUES (1, 'bolt', 40), (2, 'nut', 55), (3, 'washer', NULL)",
	} {
		if _, err := engine.Exec(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	return engine
}

func execResult(t *testing.T, engine *exec.Engine, sql string) *exec.Result {
	t.Helper()
	result, err := engine.Exec(sql)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return result
}

func queryRows(t *testing.T, engine *exec.Engine, sql string) []string {
	t.Helper()
	result := execResult(t, engine, sql)
	out := make([]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = v.String()
		}
		out = append(out, strings.Join(cells, "|"))
	}
	return out
}

func TestAlterResultShape(t *testing.T) {
	engine := alterEngine(t)
	result := execResult(t, engine, "ALTER TABLE inventory ADD COLUMN bin TEXT")
	if result.Tag != "ALTER TABLE" {
		t.Fatalf("Tag = %q, want %q", result.Tag, "ALTER TABLE")
	}
	if result.RowsReturned() {
		t.Fatal("an ALTER TABLE must not report a result set")
	}
	if result.Affected != 0 {
		t.Fatalf("Affected = %d, want 0", result.Affected)
	}
}

func TestEvolvedSchemaSurvivesDumpAndLoad(t *testing.T) {
	engine := alterEngine(t)
	execResult(t, engine, "ALTER TABLE inventory RENAME COLUMN name TO label")
	execResult(t, engine, "ALTER TABLE inventory ADD COLUMN price FLOAT DEFAULT 1")
	execResult(t, engine, "ALTER TABLE inventory DROP COLUMN qty")
	execResult(t, engine, "ALTER TABLE inventory ALTER COLUMN sku TYPE BIGINT")
	execResult(t, engine, "ALTER TABLE inventory RENAME TO stock")

	dump := engine.Dump()
	restored := exec.New()
	if err := restored.Load(dump); err != nil {
		t.Fatalf("load: %v\n%s", err, dump)
	}

	const q = "SELECT sku, label, price FROM stock ORDER BY sku"
	got := queryRows(t, restored, q)
	want := queryRows(t, engine, q)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("restored rows differ\n--- got\n%v\n--- want\n%v", got, want)
	}
	if restored.Dump() != dump {
		t.Fatalf("dump is not a fixed point\n--- first\n%s\n--- second\n%s", dump, restored.Dump())
	}
}

func TestDumpDeclaresTheCurrentSchema(t *testing.T) {
	engine := alterEngine(t)
	execResult(t, engine, "ALTER TABLE inventory ALTER COLUMN sku TYPE BIGINT")
	execResult(t, engine, "ALTER TABLE inventory DROP COLUMN qty")
	execResult(t, engine, "ALTER TABLE inventory ADD COLUMN note TEXT")

	dump := engine.Dump()
	if !strings.Contains(dump, "BIGINT") {
		t.Fatalf("dump does not declare the changed type:\n%s", dump)
	}
	if strings.Contains(dump, "qty") {
		t.Fatalf("dump still mentions the dropped column:\n%s", dump)
	}
	if !strings.Contains(dump, "note") {
		t.Fatalf("dump misses the added column:\n%s", dump)
	}
	if !strings.Contains(dump, "NOT NULL") {
		t.Fatalf("dump lost the NOT NULL on sku:\n%s", dump)
	}
}

func TestPreparedStatementsResolveTheRename(t *testing.T) {
	engine := alterEngine(t)
	bySku, err := engine.Prepare("SELECT name FROM inventory WHERE sku = $1")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := bySku.Exec(types.Int(1)); err != nil {
		t.Fatalf("first execution: %v", err)
	}

	execResult(t, engine, "ALTER TABLE inventory RENAME TO stock")
	if _, err := bySku.Exec(types.Int(1)); err == nil ||
		!strings.Contains(err.Error(), `table "inventory" does not exist`) {
		t.Fatalf("after the rename, err = %v, want the unknown-table error", err)
	}

	byName, err := engine.Prepare("SELECT name FROM stock WHERE sku = $1")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := byName.Exec(types.Int(1)); err != nil {
		t.Fatalf("before the column rename: %v", err)
	}

	execResult(t, engine, "ALTER TABLE stock RENAME COLUMN name TO label")
	if _, err := byName.Exec(types.Int(1)); err == nil ||
		!strings.Contains(err.Error(), `no column "name"`) {
		t.Fatalf("after the column rename, err = %v, want the unknown-column error", err)
	}
}

func TestFailedStatementLeavesNoTrace(t *testing.T) {
	engine := alterEngine(t)
	before := engine.Dump()

	for _, sql := range []string{
		"ALTER TABLE inventory ADD COLUMN a INT, ADD COLUMN a INT",
		"ALTER TABLE inventory RENAME TO archive, ADD COLUMN qty INT",
		"ALTER TABLE inventory DROP COLUMN name, DROP COLUMN name",
		"ALTER TABLE inventory ALTER COLUMN name TYPE INT",
		"ALTER TABLE inventory ADD COLUMN pad INT DEFAULT 0, ALTER COLUMN qty SET NOT NULL",
	} {
		if _, err := engine.Exec(sql); err == nil {
			t.Fatalf("%s: expected an error", sql)
		}
		if got := engine.Dump(); got != before {
			t.Fatalf("%s: the failed statement changed the database\n--- before\n%s\n--- after\n%s", sql, before, got)
		}
	}

	execResult(t, engine, "ALTER TABLE inventory ADD COLUMN bin TEXT DEFAULT 'a1'")
	if engine.Dump() == before {
		t.Fatal("a successful statement must change the database")
	}
}

func TestRenameKeepsTheRowsAndTheCount(t *testing.T) {
	engine := alterEngine(t)
	execResult(t, engine, "ALTER TABLE inventory RENAME TO stock")
	n, err := engine.RowCount("stock")
	if err != nil {
		t.Fatalf("RowCount: %v", err)
	}
	if n != 3 {
		t.Fatalf("RowCount = %d, want 3", n)
	}
	if _, err := engine.RowCount("inventory"); err == nil {
		t.Fatal("the old name still has storage")
	}
}

func TestBackfillIsNotARememberedDefault(t *testing.T) {
	engine := alterEngine(t)
	execResult(t, engine, "ALTER TABLE inventory ADD COLUMN grade INT DEFAULT 9")

	insert, err := engine.Prepare("INSERT INTO inventory (sku, name) VALUES ($1, $2)")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := insert.Exec(types.Int(4), types.Text("late")); err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows := queryRows(t, engine, "SELECT sku, grade FROM inventory ORDER BY sku")
	want := []string{"1|9", "2|9", "3|9", "4|NULL"}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Fatalf("grades = %v, want %v", rows, want)
	}
}

func TestShellSchemaShowsTheEvolvedTable(t *testing.T) {
	var out, errOut bytes.Buffer
	shell := cli.New(&out, &errOut)
	engine := shell.Engine()
	execResult(t, engine, "CREATE TABLE notes (id INT, body TEXT)")
	execResult(t, engine, "ALTER TABLE notes RENAME COLUMN body TO content, ADD COLUMN pinned BOOL")

	schema, err := shell.Meta(".schema notes")
	if err != nil {
		t.Fatalf(".schema: %v", err)
	}
	for _, part := range []string{"content", "pinned", "BOOL"} {
		if !strings.Contains(schema, part) {
			t.Fatalf(".schema misses %q:\n%s", part, schema)
		}
	}
	if strings.Contains(schema, "body") {
		t.Fatalf(".schema still shows the old column name:\n%s", schema)
	}
}

func TestAlterKeywordsAreReserved(t *testing.T) {
	engine := exec.New()
	if _, err := engine.Exec("CREATE TABLE alter (a INT)"); err == nil {
		t.Fatal("ALTER must be reserved")
	}
	for _, word := range []string{"add", "column", "default", "rename", "to", "type", "using"} {
		if _, err := engine.Exec("CREATE TABLE t (" + word + " INT)"); err == nil {
			t.Fatalf("%s must be reserved", word)
		}
		if _, err := engine.Exec("CREATE TABLE " + word + " (a INT)"); err == nil {
			t.Fatalf("%s must be reserved as a table name too", word)
		}
	}
	if _, err := engine.Exec(`CREATE TABLE "alter" ("column" INT, "to" TEXT)`); err != nil {
		t.Fatalf("quoting must still make the words usable: %v", err)
	}
	if _, err := engine.Exec(`ALTER TABLE "alter" RENAME COLUMN "to" TO "using"`); err != nil {
		t.Fatalf("quoted identifiers inside ALTER TABLE: %v", err)
	}
}
