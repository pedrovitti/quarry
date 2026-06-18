package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// invoke runs the command with the given arguments and returns its two output
// streams alongside the error main would have exited on.
func invoke(t *testing.T, command, load string, echo bool, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut strings.Builder
	err := run(&out, &errOut, command, load, echo, args)
	return out.String(), errOut.String(), err
}

// write puts a file in a temporary directory and returns its path.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunOneStatement(t *testing.T) {
	stdout, stderr, err := invoke(t, "SELECT 1", "", false)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "(1 row)") {
		t.Errorf("the statement did not run:\n%s", stdout)
	}
}

func TestRunOneStatementThatFails(t *testing.T) {
	// The message has already gone to stderr, so the error exists only to set
	// the exit status.
	stdout, stderr, err := invoke(t, "SELECT * FROM nosuch", "", false)
	if err == nil {
		t.Fatal("a failing statement returned no error")
	}
	if !strings.Contains(stderr, "does not exist") {
		t.Errorf("the failure was not reported: %s", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout: %s", stdout)
	}
}

func TestRunOneStatementThatDoesNotParse(t *testing.T) {
	_, stderr, err := invoke(t, "SELECT (", "", false)
	if err == nil {
		t.Fatal("unparseable input returned no error")
	}
	if !strings.Contains(stderr, "syntax error") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRunAScript(t *testing.T) {
	path := write(t, "script.sql", "CREATE TABLE t (a INT);\nINSERT INTO t VALUES (1);\nSELECT * FROM t;\n")

	stdout, stderr, err := invoke(t, "", "", false, path)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "(1 row)") {
		t.Errorf("the script did not run:\n%s", stdout)
	}
}

func TestRunSeveralScriptsShareOneDatabase(t *testing.T) {
	// Each file is not its own session: a table created by the first is there
	// for the second.
	dir := t.TempDir()
	first := filepath.Join(dir, "a.sql")
	second := filepath.Join(dir, "b.sql")
	if err := os.WriteFile(first, []byte("CREATE TABLE t (a INT);\nINSERT INTO t VALUES (1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("SELECT * FROM t;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := invoke(t, "", "", false, first, second)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "(1 row)") {
		t.Errorf("the second script did not see the first:\n%s", stdout)
	}
}

func TestRunStopsAtTheFirstFailingScript(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.sql")
	good := filepath.Join(dir, "good.sql")
	if err := os.WriteFile(bad, []byte("SELECT * FROM nosuch;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, []byte("SELECT 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := invoke(t, "", "", false, bad, good)
	if err == nil {
		t.Fatal("a failing script returned no error")
	}
	// The error names the file, since a run may have been given several.
	if !strings.Contains(err.Error(), "bad.sql") {
		t.Errorf("error = %v, want it to name the file", err)
	}
	if strings.Contains(stdout, "(1 row)") {
		t.Errorf("the script after the failure ran:\n%s", stdout)
	}
}

func TestRunReportsAMissingScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nosuch.sql")
	if _, _, err := invoke(t, "", "", false, path); err == nil {
		t.Fatal("a missing script returned no error")
	}
}

func TestLoadADumpBeforeRunning(t *testing.T) {
	dump := write(t, "db.sql", "CREATE TABLE t (a INT);\nINSERT INTO t VALUES (7);\n")

	stdout, stderr, err := invoke(t, "SELECT a FROM t", dump, false)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr: %s", stderr)
	}
	if !strings.Contains(stdout, "7") {
		t.Errorf("the dump was not loaded:\n%s", stdout)
	}
}

func TestLoadReportsAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nosuch.sql")
	if _, _, err := invoke(t, "SELECT 1", path, false); err == nil {
		t.Fatal("a missing dump returned no error")
	}
}

func TestLoadReportsABadDump(t *testing.T) {
	dump := write(t, "bad.sql", "CREATE TABLE (\n")

	_, _, err := invoke(t, "SELECT 1", dump, false)
	if err == nil {
		t.Fatal("a bad dump returned no error")
	}
	// The message names the file, since the error itself is about SQL and
	// would otherwise not say where the SQL came from.
	if !strings.Contains(err.Error(), "bad.sql") {
		t.Errorf("error = %v, want it to name the file", err)
	}
}

func TestEchoPrintsStatementsBeforeRunningThem(t *testing.T) {
	stdout, _, err := invoke(t, "SELECT 1", "", true)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !strings.Contains(stdout, "SELECT 1;") {
		t.Errorf("the statement was not echoed:\n%s", stdout)
	}

	stdout, _, err = invoke(t, "SELECT 1", "", false)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if strings.Contains(stdout, "SELECT 1;") {
		t.Errorf("the statement was echoed with -echo off:\n%s", stdout)
	}
}

func TestACommandBeatsAScript(t *testing.T) {
	// -c and a file are both ways of saying what to run; -c wins rather than
	// both happening in some order.
	path := write(t, "script.sql", "SELECT 'from the file';\n")

	stdout, _, err := invoke(t, "SELECT 'from the flag'", "", false, path)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !strings.Contains(stdout, "from the flag") {
		t.Errorf("the -c statement did not run:\n%s", stdout)
	}
	if strings.Contains(stdout, "from the file") {
		t.Errorf("the script ran as well as -c:\n%s", stdout)
	}
}

func TestIsTerminalIsFalseForAFile(t *testing.T) {
	// A pipe or a file gets no prompts, which is what keeps piped output to
	// just the results.
	f, err := os.Open(write(t, "input.sql", "SELECT 1;\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if isTerminal(f) {
		t.Error("a regular file was taken for a terminal")
	}
}

func TestIsTerminalIsFalseForAClosedFile(t *testing.T) {
	// Stat fails on a closed descriptor, and the answer then has to be no
	// rather than a panic.
	f, err := os.Open(write(t, "input.sql", ""))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if isTerminal(f) {
		t.Error("a closed file was taken for a terminal")
	}
}

func TestSilentErrorStillHasAMessage(t *testing.T) {
	// It is never printed by main - the statement's own error already was -
	// but an error with an empty message is a trap for any other caller.
	if got := errFailed.Error(); got == "" {
		t.Error("errFailed has no message")
	}
}
