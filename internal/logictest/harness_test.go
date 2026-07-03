package logictest

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/exec"
)

// The harness decides whether a test file passes, so a bug in it is a bug in
// every file at once - one that reads as everything passing. These check the
// parts that could fail silently: that a query with the wrong output is
// reported rather than accepted, and that RunAll stops where it says it does.

func TestRunAllAppliesRecordsInOrder(t *testing.T) {
	records, err := Parse(`statement ok
CREATE TABLE t (a INT)

statement ok
INSERT INTO t VALUES (1)

query
SELECT a FROM t
----
a
1
`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("parsed %d records, want 3", len(records))
	}
	if err := RunAll(exec.New(), records); err != nil {
		t.Errorf("RunAll returned error: %v", err)
	}
}

func TestRunAllStopsAtTheFirstFailure(t *testing.T) {
	// Later records assume the earlier ones applied, so carrying on after a
	// failure would report a cascade rather than the cause.
	records, err := Parse(`statement ok
CREATE TABLE t (a INT)

statement ok
INSERT INTO nosuch VALUES (1)

statement ok
INSERT INTO t VALUES (2)
`)
	if err != nil {
		t.Fatal(err)
	}

	engine := exec.New()
	err = RunAll(engine, records)
	if err == nil {
		t.Fatal("RunAll succeeded")
	}
	// The message names the line, which is the only way to find the record in
	// a file of a hundred.
	if !strings.Contains(err.Error(), "line 4") {
		t.Errorf("error = %q, want it to name the line", err)
	}
	// And the record after the failure did not run.
	if count, err := engine.RowCount("t"); err != nil || count != 0 {
		t.Errorf("RowCount = %d (err %v), want 0", count, err)
	}
}

func TestRunAllOnNoRecordsSucceeds(t *testing.T) {
	if err := RunAll(exec.New(), nil); err != nil {
		t.Errorf("RunAll returned error: %v", err)
	}
}

func TestRunRejectsAnUnknownRecordKind(t *testing.T) {
	// Not reachable through Parse, which is the point: if a kind is ever
	// added without a case here, this is what says so.
	err := Run(exec.New(), Record{Kind: Kind(99), SQL: "SELECT 1"})
	if err == nil {
		t.Fatal("an unknown record kind was accepted")
	}
	if !strings.Contains(err.Error(), "unknown record kind") {
		t.Errorf("error = %q", err)
	}
}

func TestRunReportsAStatementThatShouldHaveWorked(t *testing.T) {
	err := Run(exec.New(), Record{Kind: StatementOK, SQL: "SELECT * FROM nosuch"})
	if err == nil {
		t.Fatal("a failing statement passed a statement ok record")
	}
	if !strings.Contains(err.Error(), "statement failed") {
		t.Errorf("error = %q", err)
	}
}

func TestRunReportsAQueryThatFailed(t *testing.T) {
	// A query record whose SQL does not even run is a failure, not an empty
	// result that happens not to match.
	err := Run(exec.New(), Record{Kind: Query, SQL: "SELECT * FROM nosuch", Expect: ""})
	if err == nil {
		t.Fatal("a failing query passed")
	}
	if !strings.Contains(err.Error(), "query failed") {
		t.Errorf("error = %q", err)
	}
}

func TestKindNames(t *testing.T) {
	// These name the record in a failure message, and the directive they
	// describe is what the file says, so they have to match it.
	tests := []struct {
		kind Kind
		want string
	}{
		{StatementOK, "statement ok"},
		{StatementError, "statement error"},
		{Query, "query"},
		{Kind(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("= %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderMatchesWhatTheFilesHold(t *testing.T) {
	// Render is the other half of every query record: if it changed, every
	// expected block in every file would have to change with it.
	engine := exec.New()
	for _, sql := range []string{
		"CREATE TABLE t (a INT, b TEXT, c FLOAT)",
		"INSERT INTO t VALUES (1, 'x', 1.0), (2, NULL, 2.5)",
	} {
		if _, err := engine.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}

	result, err := engine.Exec("SELECT * FROM t ORDER BY a")
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{
		"a|b|c",
		"1|x|1.0",
		"2|NULL|2.5",
	}, "\n")
	if got := Render(result); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderOfAnEmptyResultIsJustTheHeading(t *testing.T) {
	// Which is why a query record expecting no rows still has one line under
	// the separator.
	engine := exec.New()
	if _, err := engine.Exec("CREATE TABLE t (a INT)"); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Exec("SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if got := Render(result); got != "a" {
		t.Errorf("= %q, want %q", got, "a")
	}
}

func TestRenderDistinguishesAnIntegerFromAFloat(t *testing.T) {
	// A float always shows a decimal point, so a file states which kind it
	// expects without needing a type annotation.
	engine := exec.New()
	result, err := engine.Exec("SELECT 2, 2.0")
	if err != nil {
		t.Fatal(err)
	}
	rendered := Render(result)
	if !strings.HasSuffix(rendered, "2|2.0") {
		t.Errorf("= %q, want the second column to keep its decimal point", rendered)
	}
}
