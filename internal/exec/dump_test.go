package exec

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

// TestDumpRoundTrips is the property the whole file exists for: loading a dump
// into a fresh engine has to give a database that dumps to the same bytes.
func TestDumpRoundTrips(t *testing.T) {
	original := fixture(t)
	dump := original.Dump()

	reloaded := New()
	if err := reloaded.Load(dump); err != nil {
		t.Fatalf("Load returned error: %v\ndump:\n%s", err, dump)
	}

	if got := reloaded.Dump(); got != dump {
		t.Errorf("the reloaded database dumped differently\n got:\n%s\nwant:\n%s", got, dump)
	}
}

func TestDumpRoundTripsTheRowsThemselves(t *testing.T) {
	original := fixture(t)

	reloaded := New()
	if err := reloaded.Load(original.Dump()); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	for _, sql := range []string{
		"SELECT * FROM customers ORDER BY id",
		"SELECT * FROM orders ORDER BY id",
		"SELECT c.name, o.total FROM customers AS c JOIN orders AS o ON c.id = o.customer_id ORDER BY o.id",
	} {
		t.Run(sql, func(t *testing.T) {
			if got, want := query(t, reloaded, sql), query(t, original, sql); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestDumpIsStable(t *testing.T) {
	// Tables come out in name order and rows in insertion order, so two dumps
	// of the same database are byte for byte the same. Without that, a dump
	// is not something you can diff.
	e := fixture(t)
	first := e.Dump()
	for i := 0; i < 5; i++ {
		if got := e.Dump(); got != first {
			t.Fatalf("dump %d differed:\n%s", i, got)
		}
	}
}

func TestDumpOrdersTablesByName(t *testing.T) {
	e := New()
	for _, name := range []string{"zebra", "apple", "mango"} {
		if _, err := e.Exec("CREATE TABLE " + name + " (a INT)"); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	dump := e.Dump()
	apple := strings.Index(dump, "apple")
	mango := strings.Index(dump, "mango")
	zebra := strings.Index(dump, "zebra")
	if !(apple < mango && mango < zebra) {
		t.Errorf("tables are not in name order:\n%s", dump)
	}
}

func TestDumpOfAnEmptyDatabase(t *testing.T) {
	if got := New().Dump(); got != "" {
		t.Errorf("= %q, want an empty string", got)
	}
}

func TestDumpOfAnEmptyTableIsJustItsSchema(t *testing.T) {
	// No rows means no INSERT at all, rather than an INSERT with nothing in it,
	// which would not parse.
	e := New()
	if _, err := e.Exec("CREATE TABLE t (a INT NOT NULL, b TEXT)"); err != nil {
		t.Fatal(err)
	}

	dump := e.Dump()
	if strings.Contains(dump, "INSERT") {
		t.Errorf("an empty table produced an INSERT:\n%s", dump)
	}
	if !strings.Contains(dump, "CREATE TABLE t") {
		t.Errorf("the schema is missing:\n%s", dump)
	}
	if !strings.Contains(dump, "NOT NULL") {
		t.Errorf("NOT NULL did not survive the dump:\n%s", dump)
	}

	reloaded := New()
	if err := reloaded.Load(dump); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if _, err := reloaded.Exec("INSERT INTO t VALUES (NULL, 'x')"); err == nil {
		t.Error("the reloaded table accepted a NULL in a NOT NULL column")
	}
}

func TestDumpQuotesValuesSoTheyReparse(t *testing.T) {
	e := New()
	setup := []string{
		"CREATE TABLE t (s TEXT, n INT, f FLOAT, b BOOL)",
		`INSERT INTO t VALUES ('it''s', 1, 1.5, TRUE)`,
		`INSERT INTO t VALUES ('a
b', -2, -0.5, FALSE)`,
		"INSERT INTO t VALUES (NULL, NULL, NULL, NULL)",
		`INSERT INTO t VALUES ('', 0, 0.0, TRUE)`,
	}
	for _, sql := range setup {
		if _, err := e.Exec(sql); err != nil {
			t.Fatalf("setup %q: %v", sql, err)
		}
	}

	reloaded := New()
	if err := reloaded.Load(e.Dump()); err != nil {
		t.Fatalf("Load returned error: %v\ndump:\n%s", err, e.Dump())
	}
	if got, want := query(t, reloaded, "SELECT * FROM t"), query(t, e, "SELECT * FROM t"); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDumpQuotesIdentifiersSoTheyReparse(t *testing.T) {
	e := New()
	if _, err := e.Exec(`CREATE TABLE "select" ("order by" TEXT, "a""b" INT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Exec(`INSERT INTO "select" VALUES ('x', 1)`); err != nil {
		t.Fatal(err)
	}

	reloaded := New()
	if err := reloaded.Load(e.Dump()); err != nil {
		t.Fatalf("Load returned error: %v\ndump:\n%s", err, e.Dump())
	}
	if got := query(t, reloaded, `SELECT * FROM "select"`); got != `order by|a"b`+"\nx|1" {
		t.Errorf("= %q", got)
	}
}

func TestDumpBatchesLargeTables(t *testing.T) {
	// Rows are batched into several INSERTs rather than one enormous statement.
	// The count has to survive the batching, including the partial last batch.
	const rows = insertBatch*2 + 3

	e := New()
	if _, err := e.Exec("CREATE TABLE t (a INT)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		if _, err := e.Exec("INSERT INTO t VALUES ($1)", types.Int(int64(i))); err != nil {
			t.Fatalf("inserting row %d: %v", i, err)
		}
	}

	dump := e.Dump()
	if got, want := strings.Count(dump, "INSERT INTO"), 3; got != want {
		t.Errorf("the dump has %d INSERTs, want %d:\n%s", got, want, dump)
	}

	reloaded := New()
	if err := reloaded.Load(dump); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got, err := reloaded.RowCount("t"); err != nil || got != rows {
		t.Errorf("RowCount = %d (err %v), want %d", got, err, rows)
	}
}

func TestDumpSkipsDeletedRows(t *testing.T) {
	e := fixture(t)
	if _, err := e.Exec("DELETE FROM customers WHERE id = 2"); err != nil {
		t.Fatal(err)
	}

	dump := e.Dump()
	if strings.Contains(dump, "'bob'") {
		t.Errorf("a deleted row is still in the dump:\n%s", dump)
	}

	reloaded := New()
	if err := reloaded.Load(dump); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got, err := reloaded.RowCount("customers"); err != nil || got != 3 {
		t.Errorf("RowCount = %d (err %v), want 3", got, err)
	}
}

func TestLoadReplacesWhatWasThere(t *testing.T) {
	e := fixture(t)
	if err := e.Load("CREATE TABLE only (a INT);\nINSERT INTO only VALUES (1);\n"); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if _, err := e.Exec("SELECT * FROM customers"); err == nil {
		t.Error("a table from before the load survived it")
	}
	if got := query(t, e, "SELECT * FROM only"); got != "a\n1" {
		t.Errorf("= %q", got)
	}
}

func TestLoadLeavesTheEngineAloneWhenTheDumpIsBad(t *testing.T) {
	// The load runs into a fresh database and is swapped in only once it has
	// all applied, so a file that fails half way through does not leave a
	// half-loaded engine.
	tests := []struct {
		name string
		dump string
	}{
		{"unparseable", "CREATE TABLE ("},
		{"fails part way", "CREATE TABLE ok (a INT);\nINSERT INTO nosuch VALUES (1);\n"},
		{"a duplicate table", "CREATE TABLE t (a INT);\nCREATE TABLE t (a INT);\n"},
		{"a constraint violation", "CREATE TABLE t (a INT NOT NULL);\nINSERT INTO t VALUES (NULL);\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := fixture(t)
			before := e.Dump()

			if err := e.Load(tt.dump); err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			if got := e.Dump(); got != before {
				t.Errorf("the engine changed\n got:\n%s\nwant:\n%s", got, before)
			}
			// Still usable, not just unchanged.
			if _, err := e.Exec("SELECT * FROM customers"); err != nil {
				t.Errorf("the engine is broken after a failed load: %v", err)
			}
		})
	}
}

func TestLoadOfAnEmptyDumpGivesAnEmptyDatabase(t *testing.T) {
	e := fixture(t)
	if err := e.Load(""); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := e.Dump(); got != "" {
		t.Errorf("= %q, want an empty string", got)
	}
}
