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
	return Annotate(src, e.Pos, e.Error(), e.Msg)
}

// Annotate renders line pos.Line of src with a caret under pos.Column. header
// is the first line of the output and msg is written after the caret. Callers
// outside this package use it to format their own positioned errors the same
// way; if pos does not fall inside src, header is returned unchanged.
func Annotate(src string, pos Position, header, msg string) string {
	lines := strings.Split(src, "\n")
	if pos.Line < 1 || pos.Line > len(lines) {
		return header
	}
	line := lines[pos.Line-1]

	// Column is counted in runes; pad with the source's own tabs so the caret
	// stays aligned in terminals with any tab width.
	var pad strings.Builder
	for i, r := range []rune(line) {
		if i >= pos.Column-1 {
			break
		}
		if r == '\t' {
			pad.WriteRune('\t')
		} else {
			pad.WriteByte(' ')
		}
	}
	return fmt.Sprintf("%s\n%s\n%s^ %s", header, line, pad.String(), msg)
}
