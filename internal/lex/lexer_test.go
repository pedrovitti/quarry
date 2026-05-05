package lex

import (
	"strings"
	"testing"
)

// kinds tokenizes src and returns just the kinds, failing the test on error.
func kinds(t *testing.T, src string) []Kind {
	t.Helper()
	toks, err := ScanAll(src)
	if err != nil {
		t.Fatalf("ScanAll(%q) returned error: %v", src, err)
	}
	got := make([]Kind, len(toks))
	for i, tok := range toks {
		got[i] = tok.Kind
	}
	return got
}

func equalKinds(a, b []Kind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestScanStatements(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Kind
	}{
		{
			name: "select star",
			src:  "SELECT * FROM customers",
			want: []Kind{Select, Star, From, Ident},
		},
		{
			name: "lower case keywords",
			src:  "select id from t",
			want: []Kind{Select, Ident, From, Ident},
		},
		{
			name: "qualified column",
			src:  "SELECT c.id FROM customers c",
			want: []Kind{Select, Ident, Dot, Ident, From, Ident, Ident},
		},
		{
			name: "where clause",
			src:  "SELECT * FROM t WHERE a >= 1 AND b <> 'x'",
			want: []Kind{Select, Star, From, Ident, Where, Ident, GreaterEq, Int, And, Ident, NotEq, String},
		},
		{
			name: "order by",
			src:  "SELECT a FROM t ORDER BY a DESC LIMIT 10 OFFSET 5",
			want: []Kind{Select, Ident, From, Ident, Order, By, Ident, Desc, Limit, Int, Offset, Int},
		},
		{
			name: "insert values",
			src:  "INSERT INTO t VALUES (1, 'a', NULL)",
			want: []Kind{Insert, Into, Ident, Values, LParen, Int, Comma, String, Comma, Null, RParen},
		},
		{
			name: "trailing semicolon",
			src:  "SELECT 1;",
			want: []Kind{Select, Int, Semicolon},
		},
		{
			name: "empty input",
			src:  "",
			want: nil,
		},
		{
			name: "whitespace only",
			src:  "   \n\t  ",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kinds(t, tt.src); !equalKinds(got, tt.want) {
				t.Errorf("ScanAll(%q) kinds = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestScanOperators(t *testing.T) {
	tests := []struct {
		src  string
		want Kind
	}{
		{"+", Plus},
		{"-", Minus},
		{"*", Star},
		{"/", Slash},
		{"%", Percent},
		{"=", Eq},
		{"<", Less},
		{"<=", LessEq},
		{">", Greater},
		{">=", GreaterEq},
		{"<>", NotEq},
		{"!=", NotEq},
		{"||", Concat},
		{"(", LParen},
		{")", RParen},
		{",", Comma},
		{";", Semicolon},
		{".", Dot},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got := kinds(t, tt.src)
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("ScanAll(%q) = %v, want [%v]", tt.src, got, tt.want)
			}
		})
	}
}

func TestScanOperatorsAreNotGreedyAcrossTokens(t *testing.T) {
	// "a<-1" is a less-than followed by a negative literal, not "<-".
	got := kinds(t, "a<-1")
	want := []Kind{Ident, Less, Minus, Int}
	if !equalKinds(got, want) {
		t.Errorf("ScanAll(\"a<-1\") = %v, want %v", got, want)
	}
}

func TestScanNumbers(t *testing.T) {
	tests := []struct {
		src  string
		kind Kind
		text string
	}{
		{"0", Int, "0"},
		{"42", Int, "42"},
		{"4.2", Float, "4.2"},
		{"4.", Float, "4."},
		{".5", Float, ".5"},
		{"1e9", Float, "1e9"},
		{"1E9", Float, "1E9"},
		{"1e+9", Float, "1e+9"},
		{"1e-9", Float, "1e-9"},
		{"1.5e-9", Float, "1.5e-9"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			toks, err := ScanAll(tt.src)
			if err != nil {
				t.Fatalf("ScanAll(%q) returned error: %v", tt.src, err)
			}
			if len(toks) != 1 {
				t.Fatalf("ScanAll(%q) returned %d tokens, want 1", tt.src, len(toks))
			}
			if toks[0].Kind != tt.kind {
				t.Errorf("kind = %v, want %v", toks[0].Kind, tt.kind)
			}
			if toks[0].Text != tt.text {
				t.Errorf("text = %q, want %q", toks[0].Text, tt.text)
			}
		})
	}
}

func TestScanStrings(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"plain", `'hello'`, "hello"},
		{"empty", `''`, ""},
		{"escaped quote", `'it''s'`, "it's"},
		{"only an escaped quote", `''''`, "'"},
		{"keywords inside", `'SELECT FROM'`, "SELECT FROM"},
		{"multibyte", `'héllo'`, "héllo"},
		{"newline inside", "'a\nb'", "a\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, err := ScanAll(tt.src)
			if err != nil {
				t.Fatalf("ScanAll(%q) returned error: %v", tt.src, err)
			}
			if len(toks) != 1 || toks[0].Kind != String {
				t.Fatalf("ScanAll(%q) = %v, want one String token", tt.src, toks)
			}
			if toks[0].Text != tt.want {
				t.Errorf("text = %q, want %q", toks[0].Text, tt.want)
			}
		})
	}
}

func TestScanQuotedIdent(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"preserves case", `"Orders"`, "Orders"},
		{"reserved word as name", `"select"`, "select"},
		{"embedded space", `"order total"`, "order total"},
		{"escaped quote", `"a""b"`, `a"b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, err := ScanAll(tt.src)
			if err != nil {
				t.Fatalf("ScanAll(%q) returned error: %v", tt.src, err)
			}
			if len(toks) != 1 || toks[0].Kind != Ident {
				t.Fatalf("ScanAll(%q) = %v, want one Ident token", tt.src, toks)
			}
			if toks[0].Text != tt.want {
				t.Errorf("text = %q, want %q", toks[0].Text, tt.want)
			}
		})
	}
}

func TestQuotedIdentIsNeverAKeyword(t *testing.T) {
	toks, err := ScanAll(`"from"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if toks[0].Kind != Ident {
		t.Errorf(`ScanAll("\"from\"") kind = %v, want Ident`, toks[0].Kind)
	}
}

func TestScanParam(t *testing.T) {
	toks, err := ScanAll("$1, $27")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []struct{ text string }{{"1"}, {}, {"27"}}
	if len(toks) != 3 {
		t.Fatalf("got %d tokens, want 3", len(toks))
	}
	if toks[0].Kind != Param || toks[0].Text != want[0].text {
		t.Errorf("first token = %v/%q, want Param/1", toks[0].Kind, toks[0].Text)
	}
	if toks[2].Kind != Param || toks[2].Text != want[2].text {
		t.Errorf("third token = %v/%q, want Param/27", toks[2].Kind, toks[2].Text)
	}
}

func TestSkipComments(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Kind
	}{
		{"line comment", "SELECT -- everything\n1", []Kind{Select, Int}},
		{"line comment at eof", "SELECT 1 -- done", []Kind{Select, Int}},
		{"block comment", "SELECT /* all of it */ 1", []Kind{Select, Int}},
		{"block comment with newline", "SELECT /* a\nb */ 1", []Kind{Select, Int}},
		{"block comment does not nest", "SELECT /* /* */ 1", []Kind{Select, Int}},
		{"comment only", "-- nothing here", nil},
		{"minus is not a comment", "1 - 2", []Kind{Int, Minus, Int}},
		{"slash is not a comment", "1 / 2", []Kind{Int, Slash, Int}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kinds(t, tt.src); !equalKinds(got, tt.want) {
				t.Errorf("ScanAll(%q) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestTokenPositions(t *testing.T) {
	toks, err := ScanAll("SELECT a\nFROM t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Position{
		{Offset: 0, Line: 1, Column: 1},  // SELECT
		{Offset: 7, Line: 1, Column: 8},  // a
		{Offset: 9, Line: 2, Column: 1},  // FROM
		{Offset: 14, Line: 2, Column: 6}, // t
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d", len(toks), len(want))
	}
	for i, tok := range toks {
		if tok.Pos != want[i] {
			t.Errorf("token %d (%s) position = %+v, want %+v", i, tok, tok.Pos, want[i])
		}
	}
}

func TestColumnsCountRunesNotBytes(t *testing.T) {
	// The identifier "é" is two bytes; the token after it is at column 3.
	toks, err := ScanAll("é x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toks) != 2 {
		t.Fatalf("got %d tokens, want 2", len(toks))
	}
	if toks[1].Pos.Column != 3 {
		t.Errorf("second token column = %d, want 3", toks[1].Pos.Column)
	}
}

func TestScanErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"unterminated string", "'abc", "unterminated string literal"},
		{"unterminated quoted ident", `"abc`, "unterminated quoted identifier"},
		{"quoted ident across lines", "\"abc\ndef\"", "unterminated quoted identifier"},
		{"empty quoted ident", `""`, "empty quoted identifier"},
		{"unterminated block comment", "SELECT /* a", "unterminated block comment"},
		{"number with two points", "1.2.3", "second decimal point"},
		{"exponent without digits", "1e", "exponent has no digits"},
		{"digits then letters", "123abc", "unexpected"},
		{"bare dollar", "$", "must be followed by a number"},
		{"bare pipe", "|", "unexpected character"},
		{"bare bang", "!", "unexpected character"},
		{"unknown character", "SELECT #", "unexpected character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ScanAll(tt.src)
			if err == nil {
				t.Fatalf("ScanAll(%q) succeeded, want error", tt.src)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantMsg)
			}
			var lexErr *Error
			if !asLexError(err, &lexErr) {
				t.Fatalf("error is %T, want *lex.Error", err)
			}
			if !lexErr.Pos.IsValid() {
				t.Errorf("error position %+v is not valid", lexErr.Pos)
			}
		})
	}
}

func asLexError(err error, target **Error) bool {
	le, ok := err.(*Error)
	if ok {
		*target = le
	}
	return ok
}

func TestErrorAnnotate(t *testing.T) {
	src := "SELECT *\nFROM #"
	_, err := ScanAll(src)
	if err == nil {
		t.Fatal("expected an error")
	}
	lexErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *lex.Error", err)
	}
	out := lexErr.Annotate(src)
	if !strings.Contains(out, "FROM #") {
		t.Errorf("Annotate output missing the offending line:\n%s", out)
	}
	if !strings.Contains(out, "^") {
		t.Errorf("Annotate output missing the caret:\n%s", out)
	}
}

func TestNextReturnsEOFRepeatedly(t *testing.T) {
	l := New("1")
	for i := 0; i < 3; i++ {
		if _, err := l.Next(); err != nil {
			t.Fatalf("Next %d: %v", i, err)
		}
	}
	tok, err := l.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.Kind != EOF {
		t.Errorf("kind = %v, want EOF", tok.Kind)
	}
}

func TestTokenString(t *testing.T) {
	tests := []struct {
		tok  Token
		want string
	}{
		{Token{Kind: EOF}, "end of input"},
		{Token{Kind: String, Text: "x"}, "'x'"},
		{Token{Kind: Select, Text: "select"}, "SELECT"},
		{Token{Kind: Ident, Text: "customers"}, "customers"},
		{Token{Kind: Comma}, ","},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.tok.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
