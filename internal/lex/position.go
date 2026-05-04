package lex

import "fmt"

// Position is a location in a SQL statement. Offset is the byte index into the
// source; Line and Column are 1-based and counted in runes, so that error
// carets line up under multi-byte identifiers.
type Position struct {
	Offset int
	Line   int
	Column int
}

// StartPosition is the position of the first byte of a statement.
var StartPosition = Position{Offset: 0, Line: 1, Column: 1}

func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// IsValid reports whether p refers to a real location. The zero Position does
// not: lines and columns start at 1.
func (p Position) IsValid() bool {
	return p.Line > 0 && p.Column > 0
}

// Before reports whether p occurs earlier in the source than q.
func (p Position) Before(q Position) bool {
	return p.Offset < q.Offset
}
