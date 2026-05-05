package lex

import (
	"fmt"
	"strings"
)

// Error is a lexical error tied to a position in the source. The parser wraps
// these rather than reformatting them, so the position survives all the way to
// the user.
type Error struct {
	Pos Position
	Msg string
}

func (e *Error) Error() string {
	return fmt.Sprintf("syntax error at %s: %s", e.Pos, e.Msg)
}

func errorf(pos Position, format string, args ...any) *Error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// Annotate renders the offending line of src with a caret under the error:
//
//	SELECT * FRM t
//	         ^ unexpected identifier "FRM"
//
// If the position does not fall inside src the plain message is returned.
func (e *Error) Annotate(src string) string {
	lines := strings.Split(src, "\n")
	if e.Pos.Line < 1 || e.Pos.Line > len(lines) {
		return e.Error()
	}
	line := lines[e.Pos.Line-1]

	// Column is counted in runes; pad with the source's own tabs so the caret
	// stays aligned in terminals with any tab width.
	var pad strings.Builder
	for i, r := range []rune(line) {
		if i >= e.Pos.Column-1 {
			break
		}
		if r == '\t' {
			pad.WriteRune('\t')
		} else {
			pad.WriteByte(' ')
		}
	}
	return fmt.Sprintf("%s\n%s\n%s^ %s", e.Error(), line, pad.String(), e.Msg)
}
