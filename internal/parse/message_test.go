package parse

import (
	"strings"
	"testing"
)

// A parse error is the only output of the parser a user is guaranteed to see,
// so what it says is behaviour. These pin the two halves of a message: the
// prose name for the token that was found, and the caret that points at it.

func TestErrorNamesTheTokenItFound(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{"an identifier", "SELECT a FROM t t2 t3", `identifier "t3"`},
		{"a keyword", "SELECT FROM t", "keyword FROM"},
		{"a number", "SELECT 1 FROM 2", "number 2"},
		{"a float", "SELECT 1 FROM 1.5", "number 1.5"},
		{"a string", "SELECT 1 FROM 'x'", "string 'x'"},
		{"an operator", "SELECT 1 +", "end of input"},
		{"the end of the input", "SELECT", "end of input"},
		{"a punctuation token", "SELECT ,", `","`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.sql)
			if err == nil {
				t.Fatalf("%q parsed", tt.sql)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestErrorSaysWhatWouldHaveBeenValid(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{"the start of a statement", "nonsense", "expected the start of a statement"},
		{"a table name", "SELECT a FROM 1", "expected a table name"},
		{"a column name after a dot", "SELECT a.'x' FROM t", "expected a column name after the dot"},
		{"a closing parenthesis", "SELECT (1", "expected a closing parenthesis"},
		{"a table name", "DROP TABLE 1", "expected a table name"},
		{"a keyword", "INSERT t VALUES (1)", "expected INTO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.sql)
			if err == nil {
				t.Fatalf("%q parsed", tt.sql)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestErrorPositionIsTheOffendingToken(t *testing.T) {
	tests := []struct {
		sql        string
		wantLine   int
		wantColumn int
	}{
		{"SELECT 1 FROM 2", 1, 15},       // the 2
		{"SELECT (1", 1, 9},              // end of input, at the character after the 1
		{"SELECT a FROM t t2 t3", 1, 20}, // the second alias
		{"SELECT\n1 FROM 2", 2, 8},       // a position on the second line
		{"SELECT\n\n1 FROM 2", 3, 8},     // and the third
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			_, err := Parse(tt.sql)
			if err == nil {
				t.Fatal("parsed")
			}
			parseErr, ok := err.(*Error)
			if !ok {
				t.Fatalf("error is %T, want *parse.Error", err)
			}
			if parseErr.Pos.Line != tt.wantLine || parseErr.Pos.Column != tt.wantColumn {
				t.Errorf("position = %d:%d, want %d:%d",
					parseErr.Pos.Line, parseErr.Pos.Column, tt.wantLine, tt.wantColumn)
			}
		})
	}
}

func TestAnnotatePutsTheCaretUnderTheColumn(t *testing.T) {
	src := "SELECT 1 FROM 2"
	err := parseErrorFor(t, src)

	annotated := err.Annotate(src)
	lines := strings.Split(annotated, "\n")
	if len(lines) != 3 {
		t.Fatalf("Annotate produced %d lines, want 3:\n%s", len(lines), annotated)
	}
	if lines[1] != src {
		t.Errorf("the second line is %q, want the source", lines[1])
	}

	caret := strings.Index(lines[2], "^")
	if caret < 0 {
		t.Fatalf("no caret:\n%s", annotated)
	}
	// The caret sits under the offending column, counting from zero.
	if got, want := caret, err.Pos.Column-1; got != want {
		t.Errorf("caret at %d, want %d:\n%s", got, want, annotated)
	}
	// And the message is repeated after it, so the line reads on its own.
	if !strings.Contains(lines[2], err.Msg) {
		t.Errorf("the caret line does not carry the message:\n%s", annotated)
	}
}

func TestAnnotatePicksTheOffendingLine(t *testing.T) {
	src := "SELECT 1\nFROM 2\nWHERE 1 = 1"
	err := parseErrorFor(t, src)

	annotated := err.Annotate(src)
	if !strings.Contains(annotated, "FROM 2") {
		t.Errorf("the offending line is not shown:\n%s", annotated)
	}
	if strings.Contains(annotated, "WHERE") {
		t.Errorf("a later line was included:\n%s", annotated)
	}
}

func TestAnnotateFallsBackWhenThePositionIsNotInTheSource(t *testing.T) {
	// The caller may pass a different string than the one that was parsed.
	// Rather than point at the wrong thing, the plain message comes back.
	err := parseErrorFor(t, "SELECT\n\n1 FROM 2")
	if err.Pos.Line != 3 {
		t.Fatalf("the error is on line %d, want 3", err.Pos.Line)
	}

	got := err.Annotate("one line only")
	if got != err.Error() {
		t.Errorf("= %q, want the plain message %q", got, err.Error())
	}
	if strings.Contains(got, "^") {
		t.Errorf("a caret was drawn against unrelated source:\n%s", got)
	}
}

func TestAnnotateAlignsPastATab(t *testing.T) {
	// Columns are counted in runes, so a tab in the source has to be echoed
	// as a tab in the padding or the caret lands in the wrong place in any
	// terminal that does not use eight-space tabs.
	src := "SELECT\t1 FROM 2"
	err := parseErrorFor(t, src)

	lines := strings.Split(err.Annotate(src), "\n")
	pad := lines[2][:strings.Index(lines[2], "^")]
	if !strings.Contains(pad, "\t") {
		t.Errorf("the source's tab was not echoed in the padding: %q", pad)
	}
	// Everything before the caret is padding, and nothing else.
	if strings.Trim(pad, " \t") != "" {
		t.Errorf("the padding holds more than spaces and tabs: %q", pad)
	}
}

func TestLexicalErrorsSurfaceThroughTheParser(t *testing.T) {
	// The lexer's errors carry a position too, and the parser passes them on
	// rather than reporting its own confusion about the tokens that followed.
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{"an unterminated string", "SELECT 'a", "unterminated"},
		{"an unterminated identifier", `SELECT "a`, "unterminated"},
		{"a character that is not SQL", "SELECT 1 # 2", "unexpected character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.sql)
			if err == nil {
				t.Fatalf("%q parsed", tt.sql)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

// parseErrorFor parses src expecting a *parse.Error and returns it.
func parseErrorFor(t *testing.T, src string) *Error {
	t.Helper()
	_, err := Parse(src)
	if err == nil {
		t.Fatalf("%q parsed", src)
	}
	parseErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *parse.Error", err)
	}
	return parseErr
}
