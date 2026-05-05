package lex

import "strings"

// keywords maps the upper-cased spelling of every reserved word to its kind.
// Built from kindNames so the two can never drift apart: adding a keyword kind
// with a name is enough to make it reserved.
var keywords = func() map[string]Kind {
	m := make(map[string]Kind)
	for kind, name := range kindNames {
		if kind.IsKeyword() {
			m[name] = kind
		}
	}
	return m
}()

// LookupIdent classifies a bare word. SQL keywords are case-insensitive, so
// "select", "Select" and "SELECT" all yield the Select kind. Anything that is
// not reserved comes back as Ident.
//
// Quoted identifiers must not be passed here: "select" in double quotes is a
// column named select, not the keyword.
func LookupIdent(word string) Kind {
	if kind, ok := keywords[strings.ToUpper(word)]; ok {
		return kind
	}
	return Ident
}

// IsReserved reports whether word would be treated as a keyword if written
// unquoted. Callers building error messages use it to suggest quoting.
func IsReserved(word string) bool {
	_, ok := keywords[strings.ToUpper(word)]
	return ok
}

// Keywords returns every reserved word in upper case. The result is freshly
// allocated and unsorted; callers that display it should sort first.
func Keywords() []string {
	words := make([]string, 0, len(keywords))
	for word := range keywords {
		words = append(words, word)
	}
	return words
}
