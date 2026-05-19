package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shell returns a shell writing into two buffers, plus a function that drains
// them.
func shell(t *testing.T) (*Shell, func() (string, string)) {
	t.Helper()
	var out, errOut strings.Builder
	s := New(&out, &errOut)
	return s, func() (string, string) {
		stdout, stderr := out.String(), errOut.String()
		out.Reset()
		errOut.Reset()
		return stdout, stderr
	}
}

// run feeds SQL to a shell and returns its stdout.
func run(t *testing.T, s *Shell, drain func() (string, string), sql string) string {
	t.Helper()
	s.RunStatements(sql)
	stdout, stderr := drain()
	if stderr != "" {
		t.Fatalf("running %q wrote to stderr: %s", sql, stderr)
	}
	return stdout
}

func TestFormatTable(t *testing.T) {
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (id INT, name TEXT)")
	run(t, s, drain, "INSERT INTO t VALUES (1, 'ada'), (22, 'bob')")

	got := run(t, s, drain, "SELECT * FROM t")
	want := strings.Join([]string{
		" id | name",
		"----+------",
		"  1 | ada",
		" 22 | bob",
		"(2 rows)",
		"",
	}, "\n")
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestFormatRightAlignsNumbers(t *testing.T) {
	// A column of figures should read down its digits, so numbers are right
	// aligned and text is not.
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (n INT, s TEXT)")
	run(t, s, drain, "INSERT INTO t VALUES (1, 'a'), (1000, 'bbbb')")

	got := run(t, s, drain, "SELECT * FROM t")
	if !strings.Contains(got, "    1 | a") {
		t.Errorf("the short number was not right aligned:\n%s", got)
	}
	if !strings.Contains(got, " 1000 | bbbb") {
		t.Errorf("unexpected layout:\n%s", got)
	}
}

func TestFormatNullIsSpelledOut(t *testing.T) {
	// An empty string and a NULL must not look the same.
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (a TEXT, b TEXT)")
	run(t, s, drain, "INSERT INTO t VALUES (NULL, '')")

	got := run(t, s, drain, "SELECT * FROM t")
	if !strings.Contains(got, "NULL") {
		t.Errorf("NULL was not printed:\n%s", got)
	}
}

func TestFormatMultibyteColumnsLineUp(t *testing.T) {
	// Widths are counted in runes, so a multibyte value does not skew the
	// layout by its byte length.
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (a TEXT)")
	run(t, s, drain, "INSERT INTO t VALUES ('héllo'), ('abcde')")

	got := run(t, s, drain, "SELECT * FROM t")
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("unexpected output:\n%s", got)
	}
	if len([]rune(lines[2])) != len([]rune(lines[3])) {
		t.Errorf("rows of equal character width rendered differently:\n%q\n%q", lines[2], lines[3])
	}
}

func TestEmptyResultStillShowsItsColumns(t *testing.T) {
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (id INT)")

	got := run(t, s, drain, "SELECT * FROM t")
	if !strings.Contains(got, "id") {
		t.Errorf("the heading is missing:\n%s", got)
	}
	if !strings.Contains(got, "(0 rows)") {
		t.Errorf("the count is missing:\n%s", got)
	}
}

func TestSummaryLines(t *testing.T) {
	s, drain := shell(t)
	tests := []struct {
		sql  string
		want string
	}{
		{"CREATE TABLE t (a INT)", "(CREATE TABLE)"},
		{"INSERT INTO t VALUES (1)", "(INSERT 1 row)"},
		{"INSERT INTO t VALUES (2), (3)", "(INSERT 2 rows)"},
		{"UPDATE t SET a = 9 WHERE a = 1", "(UPDATE 1 row)"},
		{"DELETE FROM t WHERE a = 9", "(DELETE 1 row)"},
		{"DELETE FROM t WHERE a = 99", "(DELETE 0 rows)"},
		{"SELECT * FROM t", "(2 rows)"},
		{"DROP TABLE t", "(DROP TABLE)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := run(t, s, drain, tt.sql)
			if !strings.Contains(got, tt.want) {
				t.Errorf("got %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

func TestErrorsGoToStderrWithACaret(t *testing.T) {
	s, drain := shell(t)
	s.RunStatements("SELECT * FRM t")
	stdout, stderr := drain()

	if stdout != "" {
		t.Errorf("a failed statement wrote to stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "^") {
		t.Errorf("no caret in the error:\n%s", stderr)
	}
	if !strings.Contains(stderr, "SELECT * FRM t") {
		t.Errorf("the offending line is missing:\n%s", stderr)
	}
}

func TestRuntimeErrorsGoToStderr(t *testing.T) {
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (a INT)")

	s.RunStatements("SELECT * FROM nope")
	_, stderr := drain()
	if !strings.Contains(stderr, "does not exist") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestStatementCompletion(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"finished", "SELECT 1;", true},
		{"unfinished", "SELECT 1", false},
		{"blank", "", false},
		{"semicolon in a string does not finish it", "SELECT 'a;b'", false},
		{"semicolon in a string, then finished", "SELECT 'a;b';", true},
		{"unterminated string", "SELECT 'abc", false},
		{"unterminated quoted identifier", `SELECT "abc`, false},
		{"semicolon in a comment", "SELECT 1 -- ;", false},
		{"trailing comment after a semicolon", "SELECT 1; -- done", true},
		{"across lines", "SELECT 1,\n2;", true},
		{"open block comment", "SELECT 1 /* a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := complete(tt.src); got != tt.want {
				t.Errorf("complete(%q) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestFeedBuffersUntilASemicolon(t *testing.T) {
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (a INT, b INT)")

	for _, line := range []string{"INSERT INTO t", "VALUES", "(1, 2)"} {
		if err := s.Feed(line); err != nil {
			t.Fatal(err)
		}
		if stdout, _ := drain(); stdout != "" {
			t.Errorf("an unfinished statement produced output: %q", stdout)
		}
		if !s.Pending() {
			t.Error("the shell does not consider the statement pending")
		}
	}

	if err := s.Feed(";"); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := drain()
	if stderr != "" {
		t.Fatalf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "INSERT 1 row") {
		t.Errorf("the statement did not run: %q", stdout)
	}
	if s.Pending() {
		t.Error("the shell is still pending after a complete statement")
	}
}

func TestPromptChangesWhilePending(t *testing.T) {
	s, _ := shell(t)
	if got := s.Prompt(); got != "quarry> " {
		t.Errorf("Prompt() = %q", got)
	}
	if err := s.Feed("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if got := s.Prompt(); got != "  ..> " {
		t.Errorf("continuation Prompt() = %q", got)
	}
}

func TestFlushRunsUnterminatedInput(t *testing.T) {
	// A file that ends without a final semicolon still runs.
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (a INT)")

	if err := s.Feed("INSERT INTO t VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	s.Flush()

	stdout, _ := drain()
	if !strings.Contains(stdout, "INSERT 1 row") {
		t.Errorf("Flush did not run the buffered statement: %q", stdout)
	}
}

func TestRunReadsAStream(t *testing.T) {
	s, drain := shell(t)
	input := strings.NewReader("CREATE TABLE t (a INT);\nINSERT INTO t VALUES (1);\nSELECT * FROM t;\n")

	if err := s.Run(input, false); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	stdout, stderr := drain()
	if stderr != "" {
		t.Fatalf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "(1 row)") {
		t.Errorf("the select did not run:\n%s", stdout)
	}
	// Not interactive, so no prompts.
	if strings.Contains(stdout, "quarry>") {
		t.Errorf("prompts were printed for a non-interactive stream:\n%s", stdout)
	}
}

func TestMetaCommands(t *testing.T) {
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE zebra (a INT)")
	run(t, s, drain, "CREATE TABLE apple (b TEXT NOT NULL)")

	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"tables are listed in order", ".tables", "apple\nzebra"},
		{"schema of one table", ".schema apple", `CREATE TABLE apple (b TEXT NOT NULL);`},
		{"schema of every table", ".schema", "CREATE TABLE apple"},
		{"help", ".help", ".tables"},
		{"functions", ".functions", "coalesce"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Meta(tt.command)
			if err != nil {
				t.Fatalf("%s returned error: %v", tt.command, err)
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("%s = %q, want it to contain %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestMetaQuit(t *testing.T) {
	s, _ := shell(t)
	for _, command := range []string{".quit", ".exit"} {
		if _, err := s.Meta(command); err != ErrQuit {
			t.Errorf("%s returned %v, want ErrQuit", command, err)
		}
	}
}

func TestMetaErrors(t *testing.T) {
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (a INT)")

	tests := []struct {
		name    string
		command string
		wantMsg string
	}{
		{"unknown command", ".nope", `unknown command ".nope"`},
		{"schema of a missing table", ".schema nope", "does not exist"},
		{"schema with too many names", ".schema a b", "at most one"},
		{"read without a file", ".read", "takes a file name"},
		{"save without a file", ".save", "takes a file name"},
		{"read a missing file", ".read /nonexistent/nope.sql", "no such file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Meta(tt.command)
			if err == nil {
				t.Fatalf("%s succeeded, want an error", tt.command)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestIsMeta(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{".tables", true},
		{"  .tables", true},
		{"SELECT 1", false},
		{"", false},
		{"SELECT '.tables'", false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			if got := IsMeta(tt.line); got != tt.want {
				t.Errorf("IsMeta(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestSaveAndOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.sql")

	first, drain := shell(t)
	run(t, first, drain, "CREATE TABLE t (id INT NOT NULL, name TEXT, score FLOAT)")
	run(t, first, drain, "INSERT INTO t VALUES (1, 'ada', 1.5), (2, NULL, 2.5)")
	before := run(t, first, drain, "SELECT * FROM t")

	if _, err := first.Meta(".save " + path); err != nil {
		t.Fatalf(".save returned error: %v", err)
	}

	second, drain2 := shell(t)
	if _, err := second.Meta(".open " + path); err != nil {
		t.Fatalf(".open returned error: %v", err)
	}
	after := run(t, second, drain2, "SELECT * FROM t")

	if before != after {
		t.Errorf("the database changed across a save and open:\n%s\n%s", before, after)
	}
}

func TestDumpIsStable(t *testing.T) {
	// Dumping the same database twice must give the same bytes, or a saved
	// file would churn for no reason.
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE zebra (a INT)")
	run(t, s, drain, "CREATE TABLE apple (b TEXT)")
	run(t, s, drain, "INSERT INTO zebra VALUES (1), (2), (3)")

	first := s.Engine().Dump()
	for i := 0; i < 20; i++ {
		if got := s.Engine().Dump(); got != first {
			t.Fatalf("dump %d differed:\n%s\n%s", i, first, got)
		}
	}
	// Tables come out in name order.
	if strings.Index(first, "apple") > strings.Index(first, "zebra") {
		t.Errorf("tables are not dumped in name order:\n%s", first)
	}
}

func TestOpenLeavesTheEngineAloneOnABadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.sql")
	if err := os.WriteFile(path, []byte("CREATE TABLE ok (a INT); this is not sql"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE keep (a INT)")
	run(t, s, drain, "INSERT INTO keep VALUES (1)")
	before := run(t, s, drain, "SELECT * FROM keep")

	if _, err := s.Meta(".open " + path); err == nil {
		t.Fatal(".open on a broken file succeeded")
	}
	if after := run(t, s, drain, "SELECT * FROM keep"); after != before {
		t.Errorf("a failed .open changed the database:\n%s\n%s", before, after)
	}
}

func TestReadRunsAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "setup.sql")
	script := "CREATE TABLE t (a INT);\nINSERT INTO t VALUES (1), (2);\n"
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	s, drain := shell(t)
	got, err := s.Meta(".read " + path)
	if err != nil {
		t.Fatalf(".read returned error: %v", err)
	}
	if !strings.Contains(got, "2 statements") {
		t.Errorf(".read reported %q", got)
	}
	if out := run(t, s, drain, "SELECT * FROM t"); !strings.Contains(out, "(2 rows)") {
		t.Errorf("the file did not take effect:\n%s", out)
	}
}

func TestEcho(t *testing.T) {
	s, drain := shell(t)
	s.SetEcho(true)
	got := run(t, s, drain, "CREATE TABLE t (a INT)")
	if !strings.Contains(got, "CREATE TABLE t (a INT);") {
		t.Errorf("the statement was not echoed:\n%s", got)
	}
}

func TestMetaDump(t *testing.T) {
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (a INT, b TEXT)")
	run(t, s, drain, "INSERT INTO t VALUES (1, 'x')")

	got, err := s.Meta(".dump")
	if err != nil {
		t.Fatalf(".dump returned error: %v", err)
	}
	if !strings.Contains(got, "CREATE TABLE t") {
		t.Errorf(".dump omitted the schema:\n%s", got)
	}
	if !strings.Contains(got, "INSERT INTO t") {
		t.Errorf(".dump omitted the rows:\n%s", got)
	}
	if _, err := s.Meta(".dump extra"); err == nil {
		t.Error(".dump accepted an argument")
	}
}

func TestMetaStats(t *testing.T) {
	s, drain := shell(t)

	got, err := s.Meta(".stats")
	if err != nil {
		t.Fatal(err)
	}
	if got != "no tables" {
		t.Errorf("empty database reported %q", got)
	}

	run(t, s, drain, "CREATE TABLE zebra (a INT)")
	run(t, s, drain, "CREATE TABLE apple (a INT, b TEXT)")
	run(t, s, drain, "INSERT INTO zebra VALUES (1), (2), (3)")

	got, err = s.Meta(".stats")
	if err != nil {
		t.Fatal(err)
	}
	// Tables come out in name order, so the output is stable.
	if !strings.HasPrefix(got, "apple: 2 columns, 0 rows") {
		t.Errorf(".stats = %q", got)
	}
	if !strings.Contains(got, "zebra: 1 column, 3 rows") {
		t.Errorf(".stats = %q", got)
	}
	if !strings.Contains(got, "3 rows across 2 tables") {
		t.Errorf(".stats total = %q", got)
	}
}

func TestMetaTimer(t *testing.T) {
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (a INT)")

	if got, err := s.Meta(".timer"); err != nil || got != "timer is off" {
		t.Fatalf(".timer reported %q, %v", got, err)
	}
	if _, err := s.Meta(".timer on"); err != nil {
		t.Fatal(err)
	}

	got := run(t, s, drain, "SELECT * FROM t")
	if !strings.Contains(got, "took ") {
		t.Errorf("no timing line:\n%s", got)
	}

	if _, err := s.Meta(".timer off"); err != nil {
		t.Fatal(err)
	}
	got = run(t, s, drain, "SELECT * FROM t")
	if strings.Contains(got, "took ") {
		t.Errorf("timing printed after .timer off:\n%s", got)
	}

	if _, err := s.Meta(".timer maybe"); err == nil {
		t.Error(".timer accepted a value that is not on or off")
	}
}

func TestRowCount(t *testing.T) {
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (a INT)")
	run(t, s, drain, "INSERT INTO t VALUES (1), (2), (3)")
	run(t, s, drain, "DELETE FROM t WHERE a = 2")

	count, err := s.Engine().RowCount("t")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("RowCount = %d, want 2 -- deleted rows must not count", count)
	}
	if _, err := s.Engine().RowCount("nope"); err == nil {
		t.Error("RowCount on a missing table succeeded")
	}
}
