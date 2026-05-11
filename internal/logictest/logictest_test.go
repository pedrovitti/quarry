package logictest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/exec"
)

// TestFiles runs every .test file under testdata. Each file gets a fresh
// database and each record becomes a subtest, so a failure names the file, the
// line and the statement.
func TestFiles(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.test"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no test files found under testdata")
	}

	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".test"), func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			records, err := Parse(string(source))
			if err != nil {
				t.Fatalf("%s: %s", path, err)
			}
			if len(records) == 0 {
				t.Fatalf("%s has no records", path)
			}

			// One engine per file: records build on each other, so a schema
			// created near the top is there for the queries below it.
			engine := exec.New()
			for _, record := range records {
				name := fmt.Sprintf("%d_%s", record.Line, record.Label)
				passed := t.Run(name, func(t *testing.T) {
					if err := Run(engine, record); err != nil {
						t.Errorf("%s:%d\n%s", path, record.Line, err)
					}
				})
				// Later records assume the earlier ones applied, so stop
				// rather than report a cascade of failures.
				if !passed {
					t.FailNow()
				}
			}
		})
	}
}

// TestFilesAreDeterministic runs every file several times and checks the
// results do not change. It is the guard against map iteration order or any
// other source of run-to-run variation leaking into output.
func TestFilesAreDeterministic(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.test"))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".test"), func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			records, err := Parse(string(source))
			if err != nil {
				t.Fatal(err)
			}

			for i := 0; i < 10; i++ {
				engine := exec.New()
				if err := RunAll(engine, records); err != nil {
					t.Fatalf("run %d: %s", i, err)
				}
			}
		})
	}
}

func TestParseRecords(t *testing.T) {
	source := strings.Join([]string{
		"# a comment",
		"",
		"statement ok",
		"CREATE TABLE t (a INT)",
		"",
		"statement error must not be NULL",
		"INSERT INTO t VALUES (NULL)",
		"",
		"query",
		"SELECT a FROM t",
		"----",
		"a",
		"1",
		"",
	}, "\n")

	records, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}

	if records[0].Kind != StatementOK {
		t.Errorf("first record kind = %v", records[0].Kind)
	}
	if records[0].SQL != "CREATE TABLE t (a INT)" {
		t.Errorf("first record SQL = %q", records[0].SQL)
	}
	if records[0].Line != 3 {
		t.Errorf("first record line = %d, want 3", records[0].Line)
	}

	if records[1].Kind != StatementError {
		t.Errorf("second record kind = %v", records[1].Kind)
	}
	if records[1].Expect != "must not be NULL" {
		t.Errorf("second record Expect = %q", records[1].Expect)
	}

	if records[2].Kind != Query {
		t.Errorf("third record kind = %v", records[2].Kind)
	}
	if records[2].Expect != "a\n1" {
		t.Errorf("third record Expect = %q", records[2].Expect)
	}
}

func TestParseMultiLineSQL(t *testing.T) {
	records, err := Parse("query\nSELECT a,\n       b\nFROM t\n----\na|b\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].SQL != "SELECT a,\n       b\nFROM t" {
		t.Errorf("SQL = %q", records[0].SQL)
	}
}

func TestParseEmptyQueryResult(t *testing.T) {
	// A query that returns no rows still has its heading line.
	records, err := Parse("query\nSELECT a FROM t\n----\na\n")
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Expect != "a" {
		t.Errorf("Expect = %q, want just the heading", records[0].Expect)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantMsg string
	}{
		{"unknown directive", "nonsense\nSELECT 1", "unknown directive"},
		{"no sql", "statement ok\n\nstatement ok\nSELECT 1", "no SQL"},
		{"error without text", "statement error\nSELECT 1", "must contain"},
		{"query without a separator", "query\nSELECT 1", "---- separator"},
		{"statement with a separator", "statement ok\nSELECT 1\n----\nx", "only a query record"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.source)
			if err == nil {
				t.Fatal("Parse succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestParseIgnoresCommentsAndBlankLines(t *testing.T) {
	records, err := Parse("\n\n# one\n# two\n\nstatement ok\nSELECT 1\n\n\n# three\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
}

func TestRunReportsAMissingError(t *testing.T) {
	engine := exec.New()
	err := Run(engine, Record{Kind: StatementError, SQL: "CREATE TABLE t (a INT)", Expect: "boom"})
	if err == nil {
		t.Fatal("Run accepted a statement that should have failed")
	}
	if !strings.Contains(err.Error(), "want an error") {
		t.Errorf("error = %q", err)
	}
}

func TestRunReportsTheWrongError(t *testing.T) {
	engine := exec.New()
	err := Run(engine, Record{Kind: StatementError, SQL: "SELECT * FROM nope", Expect: "something else"})
	if err == nil {
		t.Fatal("Run accepted the wrong error")
	}
	if !strings.Contains(err.Error(), "want it to contain") {
		t.Errorf("error = %q", err)
	}
}

func TestRunReportsAnOutputDifference(t *testing.T) {
	engine := exec.New()
	if _, err := engine.Exec("CREATE TABLE t (a INT)"); err != nil {
		t.Fatal(err)
	}
	err := Run(engine, Record{Kind: Query, SQL: "SELECT a FROM t", Expect: "a\n99"})
	if err == nil {
		t.Fatal("Run accepted the wrong output")
	}
	if !strings.Contains(err.Error(), "output differs") {
		t.Errorf("error = %q", err)
	}
}
