package lex

import "testing"

// TestIsBareIdentifier is what decides whether a name needs quoting on the way
// back out, so it has to agree with what the lexer will accept on the way back
// in. TestBareIdentifiersReparse below checks that agreement directly.
func TestIsBareIdentifier(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"a", true},
		{"abc", true},
		{"_", true},
		{"_abc", true},
		{"a1", true},
		{"a_1", true},
		{"café", true}, // letters are letters, not just ASCII ones
		{"日本語", true},

		{"", false},
		{"1", false},   // may not start with a digit
		{"1a", false},  // nor may an identifier that continues with letters
		{"a b", false}, // no spaces
		{"a-b", false}, // nor punctuation
		{"a.b", false}, // a qualified name is two identifiers, not one
		{`a"b`, false},
		{"a\tb", false},
		{"a\nb", false},

		// Reserved words are not bare identifiers whatever their case, since
		// writing one unquoted would lex as the keyword.
		{"select", false},
		{"SELECT", false},
		{"SeLeCt", false},
		{"from", false},
		{"null", false},
		{"true", false},

		// Type names are not reserved, so they need no quoting.
		{"int", true},
		{"text", true},
		{"integer", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBareIdentifier(tt.name); got != tt.want {
				t.Errorf("IsBareIdentifier(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// TestBareIdentifiersReparse is the property behind the previous test: a name
// IsBareIdentifier accepts must lex back as exactly that identifier.
func TestBareIdentifiersReparse(t *testing.T) {
	names := []string{"a", "abc", "_", "_a1", "café", "int", "x9"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if !IsBareIdentifier(name) {
				t.Fatalf("%q is not a bare identifier", name)
			}
			tokens, err := ScanAll(name)
			if err != nil {
				t.Fatalf("ScanAll(%q) returned error: %v", name, err)
			}
			if len(tokens) != 1 {
				t.Fatalf("%q lexed as %d tokens, want 1", name, len(tokens))
			}
			if tokens[0].Kind != Ident {
				t.Errorf("%q lexed as %v, want an identifier", name, tokens[0].Kind)
			}
			if tokens[0].Text != name {
				t.Errorf("%q lexed with text %q", name, tokens[0].Text)
			}
		})
	}
}

func TestIsLiteral(t *testing.T) {
	// A literal is a token that is a value on its own. Identifiers are not,
	// since they have to be looked up, and NULL is, since it is a constant.
	literals := []Kind{Int, Float, String, True, False, Null}
	for _, kind := range literals {
		t.Run(kind.String(), func(t *testing.T) {
			if !kind.IsLiteral() {
				t.Errorf("%v.IsLiteral() = false", kind)
			}
		})
	}

	notLiterals := []Kind{Ident, Select, From, Where, Plus, Star, LParen, EOF, Comma}
	for _, kind := range notLiterals {
		t.Run(kind.String(), func(t *testing.T) {
			if kind.IsLiteral() {
				t.Errorf("%v.IsLiteral() = true", kind)
			}
		})
	}
}

func TestKeywordAndLiteralOverlap(t *testing.T) {
	// TRUE, FALSE and NULL are both: reserved words that are also constants.
	// Anything reading tokens has to expect the two sets to intersect.
	for _, kind := range []Kind{True, False, Null} {
		t.Run(kind.String(), func(t *testing.T) {
			if !kind.IsKeyword() {
				t.Errorf("%v is not a keyword", kind)
			}
			if !kind.IsLiteral() {
				t.Errorf("%v is not a literal", kind)
			}
		})
	}
	// While SELECT is only the one.
	if !Select.IsKeyword() || Select.IsLiteral() {
		t.Error("SELECT should be a keyword and not a literal")
	}
}

func TestPositionOrdering(t *testing.T) {
	first := Position{Offset: 0, Line: 1, Column: 1}
	second := Position{Offset: 7, Line: 1, Column: 8}
	// A later line with a smaller column is still later: the ordering is by
	// offset, not by the line and column pair.
	third := Position{Offset: 20, Line: 3, Column: 1}

	if !first.Before(second) {
		t.Error("the start of the input is not before a position in it")
	}
	if !second.Before(third) {
		t.Error("a position is not before one on a later line")
	}
	if third.Before(second) {
		t.Error("a later position claims to come first")
	}
	if first.Before(first) {
		t.Error("a position is before itself")
	}
}

func TestStartPositionIsTheFirstByte(t *testing.T) {
	if !StartPosition.IsValid() {
		t.Error("StartPosition is not a valid position")
	}
	if got := StartPosition.String(); got != "1:1" {
		t.Errorf("StartPosition = %q, want %q", got, "1:1")
	}
	// The zero Position is not a location: lines and columns start at 1, so
	// an unset field is distinguishable from the start of the input.
	var zero Position
	if zero.IsValid() {
		t.Error("the zero Position claims to be valid")
	}
	if !StartPosition.Before(Position{Offset: 1}) {
		t.Error("StartPosition is not before the second byte")
	}
}
