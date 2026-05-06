package parse

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/ast"
)

// dumpExpr parses src and returns the S-expression form of the result.
func dumpExpr(t *testing.T, src string) string {
	t.Helper()
	expr, err := ParseExpr(src)
	if err != nil {
		t.Fatalf("ParseExpr(%q) returned error: %v", src, err)
	}
	return ast.Dump(expr)
}

func TestParsePrimaries(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"1", "1"},
		{"4.2", "4.2"},
		{"1e9", "1e9"},
		{"'hello'", "'hello'"},
		{"'it''s'", "'it''s'"},
		{"NULL", "NULL"},
		{"TRUE", "TRUE"},
		{"false", "FALSE"},
		{"$1", "$1"},
		{"$12", "$12"},
		{"a", "a"},
		{"t.a", "t.a"},
		{`"order"`, `"order"`},
		{`"Mixed Case".x`, `"Mixed Case".x`},
		{"now()", "(call now)"},
		{"upper(name)", "(call upper name)"},
		{"substr(s, 1, 3)", "(call substr s 1 3)"},
		{"upper(lower(x))", "(call upper (call lower x))"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			if got := dumpExpr(t, tt.src); got != tt.want {
				t.Errorf("ParseExpr(%q) = %s, want %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestParsePrecedence(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"product before sum", "1 + 2 * 3", "(+ 1 (* 2 3))"},
		{"product before sum, reversed", "1 * 2 + 3", "(+ (* 1 2) 3)"},
		{"parens override", "(1 + 2) * 3", "(* (+ 1 2) 3)"},
		{"sum is left associative", "1 - 2 - 3", "(- (- 1 2) 3)"},
		{"product is left associative", "8 / 4 / 2", "(/ (/ 8 4) 2)"},
		{"modulo binds like product", "1 + 2 % 3", "(+ 1 (% 2 3))"},
		{"concat binds looser than sum", "a || b + c", "(|| a (+ b c))"},
		{"comparison binds looser than concat", "a || b = c", "(= (|| a b) c)"},
		{"comparison binds looser than arithmetic", "a + 1 > b * 2", "(> (+ a 1) (* b 2))"},
		{"and binds tighter than or", "a OR b AND c", "(OR a (AND b c))"},
		{"and binds tighter than or, reversed", "a AND b OR c", "(OR (AND a b) c)"},
		{"and is left associative", "a AND b AND c", "(AND (AND a b) c)"},
		{"or is left associative", "a OR b OR c", "(OR (OR a b) c)"},
		{"comparison binds tighter than and", "a = 1 AND b = 2", "(AND (= a 1) (= b 2))"},
		{"not binds looser than comparison", "NOT a = 1", "(NOT (= a 1))"},
		{"not binds tighter than and", "NOT a AND b", "(AND (NOT a) b)"},
		{"not binds tighter than or", "NOT a OR b", "(OR (NOT a) b)"},
		{"double not", "NOT NOT a", "(NOT (NOT a))"},
		{"negate binds tighter than product", "-a * b", "(* (neg a) b)"},
		{"negate a literal", "-1", "(neg 1)"},
		{"unary plus", "+1", "(pos 1)"},
		{"negate after operator", "1 - -2", "(- 1 (neg 2))"},
		{"nested parens collapse", "((a))", "a"},
		{"mixed", "a = 1 OR b = 2 AND c = 3", "(OR (= a 1) (AND (= b 2) (= c 3)))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dumpExpr(t, tt.src); got != tt.want {
				t.Errorf("ParseExpr(%q) =\n  %s\nwant\n  %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseIsNull(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"a IS NULL", "(is-null a)"},
		{"a IS NOT NULL", "(is-not-null a)"},
		{"a + 1 IS NULL", "(is-null (+ a 1))"},
		{"a IS NULL AND b IS NOT NULL", "(AND (is-null a) (is-not-null b))"},
		{"NOT a IS NULL", "(NOT (is-null a))"},
		{"t.a IS NULL", "(is-null t.a)"},
		{"upper(a) IS NULL", "(is-null (call upper a))"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			if got := dumpExpr(t, tt.src); got != tt.want {
				t.Errorf("ParseExpr(%q) = %s, want %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseExprErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"empty", "", "unexpected end of input"},
		{"trailing operator", "1 +", "unexpected end of input"},
		{"leading operator", "* 1", "unexpected"},
		{"unclosed paren", "(1 + 2", "closing parenthesis"},
		{"empty parens", "()", "expected an expression"},
		{"unclosed call", "upper(a", "closing parenthesis"},
		{"call with trailing comma", "upper(a,)", "expected an expression"},
		{"is without null", "a IS 1", "NULL or NOT NULL after IS"},
		{"is not without null", "a IS NOT 1", "NULL after IS NOT"},
		{"dot without name", "t.", "a column name after the dot"},
		{"dot onto keyword", "t.select", "reserved word"},
		{"two expressions", "1 2", "end of expression"},
		{"keyword as expression", "SELECT", "unexpected keyword SELECT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseExpr(tt.src)
			if err == nil {
				t.Fatalf("ParseExpr(%q) succeeded, want error", tt.src)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestStarIsNotAnExpression(t *testing.T) {
	// * is a projection, not a value, so it is only meaningful in a select
	// list. Anywhere else it is multiplication and a bare one is an error.
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"bare star as expression", "*", `unexpected "*"`},
		{"qualified star as expression", "t.*", "only valid in a select list"},
		{"star inside arithmetic", "1 + * 2", `unexpected "*"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseExpr(tt.src)
			if err == nil {
				t.Fatalf("ParseExpr(%q) succeeded, want error", tt.src)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}

	// In a select list both spellings are fine, but only as the whole item.
	for _, src := range []string{"SELECT * FROM t", "SELECT t.* FROM t"} {
		if _, err := Parse(src); err != nil {
			t.Errorf("Parse(%q) returned error: %v", src, err)
		}
	}
	for _, src := range []string{"SELECT * + 1 FROM t", "SELECT 1 + t.* FROM t"} {
		err := parseError(t, src)
		if !strings.Contains(err.Error(), "cannot appear inside an expression") &&
			!strings.Contains(err.Error(), `unexpected "*"`) {
			t.Errorf("Parse(%q) error = %q, want a complaint about *", src, err)
		}
	}
}

// parseError parses src and fails the test unless it produces an error.
func parseError(t *testing.T, src string) error {
	t.Helper()
	if _, err := Parse(src); err != nil {
		return err
	}
	t.Fatalf("Parse(%q) succeeded, want error", src)
	return nil
}

func TestParseErrorsCarryPositions(t *testing.T) {
	_, err := ParseExpr("1 + * 2")
	if err == nil {
		t.Fatal("expected an error")
	}
	parseErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *parse.Error", err)
	}
	if parseErr.Pos.Column != 5 {
		t.Errorf("error column = %d, want 5", parseErr.Pos.Column)
	}
	annotated := parseErr.Annotate("1 + * 2")
	if !strings.Contains(annotated, "^") {
		t.Errorf("Annotate produced no caret:\n%s", annotated)
	}
}

func TestReservedWordSuggestsQuoting(t *testing.T) {
	_, err := Parse("SELECT a FROM order")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "reserved word") {
		t.Errorf("error = %q, want it to mention a reserved word", msg)
	}
	if !strings.Contains(msg, `"order"`) {
		t.Errorf("error = %q, want it to suggest the quoted spelling", msg)
	}
}
