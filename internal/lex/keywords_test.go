package lex

import (
	"sort"
	"testing"
)

func TestLookupIdentIsCaseInsensitive(t *testing.T) {
	for _, spelling := range []string{"select", "SELECT", "Select", "SeLeCt"} {
		if got := LookupIdent(spelling); got != Select {
			t.Errorf("LookupIdent(%q) = %v, want Select", spelling, got)
		}
	}
}

func TestLookupIdentNonKeyword(t *testing.T) {
	for _, word := range []string{"customers", "id", "selected", "wheres", "x"} {
		if got := LookupIdent(word); got != Ident {
			t.Errorf("LookupIdent(%q) = %v, want Ident", word, got)
		}
	}
}

func TestEveryKeywordKindIsReachable(t *testing.T) {
	// Guards the keywords map against a keyword kind that was declared in the
	// Kind block but never given a name in kindNames.
	for kind := keywordBegin + 1; kind < keywordEnd; kind++ {
		name, ok := kindNames[kind]
		if !ok {
			t.Errorf("keyword kind %d has no entry in kindNames", int(kind))
			continue
		}
		if got := LookupIdent(name); got != kind {
			t.Errorf("LookupIdent(%q) = %v, want %v", name, got, kind)
		}
	}
}

func TestKeywordsAreUpperCase(t *testing.T) {
	for _, word := range Keywords() {
		if !IsReserved(word) {
			t.Errorf("Keywords() returned %q, which IsReserved rejects", word)
		}
	}
}

func TestKeywordsReturnsFreshSlice(t *testing.T) {
	first := Keywords()
	sort.Strings(first)
	second := Keywords()
	sort.Strings(second)

	if len(first) != len(second) {
		t.Fatalf("Keywords() length changed between calls: %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("Keywords()[%d] = %q, want %q", i, second[i], first[i])
		}
	}
	if len(first) == 0 {
		t.Fatal("Keywords() returned nothing")
	}
}

func TestIsReserved(t *testing.T) {
	tests := []struct {
		word string
		want bool
	}{
		{"from", true},
		{"FROM", true},
		{"order", true},
		{"customers", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsReserved(tt.word); got != tt.want {
			t.Errorf("IsReserved(%q) = %v, want %v", tt.word, got, tt.want)
		}
	}
}
