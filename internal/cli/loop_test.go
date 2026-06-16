package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingReader stands in for a stream that breaks part way through, which is
// the one thing a scanner can report that an ordinary end of input cannot.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestRunPrintsPromptsWhenInteractive(t *testing.T) {
	s, drain := shell(t)
	input := strings.NewReader("SELECT 1;\n")

	if err := s.Run(input, true); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	stdout, _ := drain()

	if !strings.Contains(stdout, "quarry> ") {
		t.Errorf("no prompt was printed:\n%s", stdout)
	}
	// One prompt before the line and one before the read that ends the
	// session, since the shell cannot know the input is over until it tries.
	if got, want := strings.Count(stdout, "quarry> "), 2; got != want {
		t.Errorf("printed %d prompts, want %d:\n%s", got, want, stdout)
	}
	// And a closing newline, so the shell prompt of the terminal underneath
	// does not land at the end of the last line.
	if !strings.HasSuffix(stdout, "\n") {
		t.Errorf("output does not end in a newline:\n%q", stdout)
	}
}

func TestRunPrintsAContinuationPromptForAPartialStatement(t *testing.T) {
	s, drain := shell(t)
	input := strings.NewReader("SELECT\n1;\n")

	if err := s.Run(input, true); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	stdout, _ := drain()

	if !strings.Contains(stdout, "  ..> ") {
		t.Errorf("no continuation prompt was printed:\n%s", stdout)
	}
}

func TestRunStopsAtQuit(t *testing.T) {
	s, drain := shell(t)
	input := strings.NewReader(".quit\nSELECT 1;\n")

	if err := s.Run(input, false); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	stdout, _ := drain()

	// The statement after the quit must not have run.
	if strings.Contains(stdout, "(1 row)") {
		t.Errorf("input after .quit was executed:\n%s", stdout)
	}
}

func TestRunReportsAReadFailure(t *testing.T) {
	s, _ := shell(t)
	want := errors.New("the pipe broke")

	err := s.Run(failingReader{err: want}, false)
	if err == nil {
		t.Fatal("Run succeeded on a broken stream")
	}
	if !errors.Is(err, want) {
		t.Errorf("error = %v, want %v", err, want)
	}
}

func TestRunKeepsGoingAfterAnError(t *testing.T) {
	// A failed statement ends that statement, not the session: a script with
	// a mistake half way down still runs the rest.
	s, drain := shell(t)
	input := strings.NewReader("SELECT * FROM nosuch;\nSELECT 1;\n")

	if err := s.Run(input, false); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	stdout, stderr := drain()

	if !strings.Contains(stderr, "does not exist") {
		t.Errorf("the failure was not reported: %s", stderr)
	}
	if !strings.Contains(stdout, "(1 row)") {
		t.Errorf("the statement after the failure did not run:\n%s", stdout)
	}
}

func TestRunFlushesInputWithNoFinalSemicolon(t *testing.T) {
	s, drain := shell(t)
	input := strings.NewReader("SELECT 1")

	if err := s.Run(input, false); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	stdout, _ := drain()

	if !strings.Contains(stdout, "(1 row)") {
		t.Errorf("the unterminated statement did not run:\n%s", stdout)
	}
}

func TestFeedTreatsMetaCommandsOnlyAtTheStartOfAStatement(t *testing.T) {
	// A line beginning with a dot is a meta command when nothing is pending.
	// Part way through a statement it is just more SQL, or the shell could
	// not hold a statement containing one.
	s, drain := shell(t)

	if err := s.Feed(".mode csv"); err != nil {
		t.Fatalf("Feed returned error: %v", err)
	}
	if s.Mode() != CSVMode {
		t.Fatalf("mode = %v, want csv", s.Mode())
	}
	drain()

	// Now the same text inside an unfinished statement.
	if err := s.Feed("SELECT"); err != nil {
		t.Fatalf("Feed returned error: %v", err)
	}
	if !s.Pending() {
		t.Fatal("the shell is not pending after a partial statement")
	}
	if err := s.Feed(".mode table"); err != nil {
		t.Fatalf("Feed returned error: %v", err)
	}
	if s.Mode() != CSVMode {
		t.Error("a meta command ran while a statement was pending")
	}
	s.Feed(";")
	drain()
}

func TestFeedIgnoresBlankLinesBetweenStatements(t *testing.T) {
	s, drain := shell(t)
	for _, line := range []string{"", "   ", "\t"} {
		if err := s.Feed(line); err != nil {
			t.Fatalf("Feed(%q) returned error: %v", line, err)
		}
		if s.Pending() {
			t.Errorf("Feed(%q) started a statement", line)
		}
	}
	if stdout, stderr := drain(); stdout != "" || stderr != "" {
		t.Errorf("blank lines produced output: %q %q", stdout, stderr)
	}
}

func TestFeedKeepsBlankLinesInsideAStatement(t *testing.T) {
	// Once a statement is under way a blank line is part of it, so a pasted
	// statement with a gap in it still runs as one.
	s, drain := shell(t)
	for _, line := range []string{"SELECT", "", "1;"} {
		if err := s.Feed(line); err != nil {
			t.Fatalf("Feed(%q) returned error: %v", line, err)
		}
	}
	stdout, stderr := drain()
	if stderr != "" {
		t.Fatalf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "(1 row)") {
		t.Errorf("the statement did not run:\n%s", stdout)
	}
}

func TestFeedWaitsForAnUnterminatedString(t *testing.T) {
	// A half-typed string is an unfinished statement rather than a broken one,
	// so the shell asks for more input instead of reporting a lexical error.
	s, drain := shell(t)

	if err := s.Feed("SELECT 'a"); err != nil {
		t.Fatalf("Feed returned error: %v", err)
	}
	if !s.Pending() {
		t.Fatal("the shell did not wait for the rest of the string")
	}
	if _, stderr := drain(); stderr != "" {
		t.Errorf("an unterminated string was reported as an error: %s", stderr)
	}

	if err := s.Feed("b';"); err != nil {
		t.Fatalf("Feed returned error: %v", err)
	}
	stdout, stderr := drain()
	if stderr != "" {
		t.Fatalf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "a\nb") {
		t.Errorf("the string did not survive the line break:\n%s", stdout)
	}
}

func TestFeedDoesNotEndAStatementOnASemicolonInsideAString(t *testing.T) {
	s, drain := shell(t)
	if err := s.Feed("SELECT 'a;b'"); err != nil {
		t.Fatalf("Feed returned error: %v", err)
	}
	if !s.Pending() {
		t.Error("a semicolon inside a string ended the statement")
	}
	s.Feed(";")
	stdout, _ := drain()
	if !strings.Contains(stdout, "a;b") {
		t.Errorf("= %s", stdout)
	}
}

func TestFeedRunsSeveralStatementsOnOneLine(t *testing.T) {
	s, drain := shell(t)
	if err := s.Feed("CREATE TABLE t (a INT); INSERT INTO t VALUES (1); SELECT * FROM t;"); err != nil {
		t.Fatalf("Feed returned error: %v", err)
	}
	stdout, stderr := drain()
	if stderr != "" {
		t.Fatalf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "(1 row)") {
		t.Errorf("the last statement did not run:\n%s", stdout)
	}
}

func TestFlushOnAnEmptyBufferDoesNothing(t *testing.T) {
	s, drain := shell(t)
	s.Flush()
	if stdout, stderr := drain(); stdout != "" || stderr != "" {
		t.Errorf("Flush wrote %q %q", stdout, stderr)
	}
}

func TestSaveReportsAPathItCannotWrite(t *testing.T) {
	s, _ := shell(t)
	// A directory that does not exist, so the write fails rather than the
	// open succeeding and leaving a file behind.
	path := filepath.Join(t.TempDir(), "nosuchdir", "db.sql")

	if _, err := s.save(path); err == nil {
		t.Fatal("save succeeded on an unwritable path")
	}
}

func TestOpenReportsAMissingFile(t *testing.T) {
	s, _ := shell(t)
	path := filepath.Join(t.TempDir(), "nosuch.sql")

	_, err := s.open(path)
	if err == nil {
		t.Fatal("open succeeded on a missing file")
	}
	if !os.IsNotExist(errors.Unwrap(err)) && !strings.Contains(err.Error(), "no such file") {
		t.Errorf("error = %v, want it to say the file is missing", err)
	}
}
