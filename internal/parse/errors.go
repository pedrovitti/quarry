package parse

import (
	"fmt"

	"github.com/pedrovitti/quarry/internal/lex"
)

// Error is a parse error at a position in the source.
type Error struct {
	Pos lex.Position
	Msg string
}

func (e *Error) Error() string {
	return fmt.Sprintf("syntax error at %s: %s", e.Pos, e.Msg)
}

// Annotate renders the offending line with a caret under the error column.
func (e *Error) Annotate(src string) string {
	return lex.Annotate(src, e.Pos, e.Error(), e.Msg)
}

func errorf(pos lex.Position, format string, args ...any) *Error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// unexpected builds the error for a token that cannot appear where it does.
// want describes what would have been valid, in the same voice as the rest of
// the message: "expected a column name".
func unexpected(tok lex.Token, want string) *Error {
	if want == "" {
		return errorf(tok.Pos, "unexpected %s", describe(tok))
	}
	return errorf(tok.Pos, "unexpected %s, expected %s", describe(tok), want)
}

// describe names a token in prose, so that error messages read as English
// rather than as a dump of the token kind.
func describe(tok lex.Token) string {
	switch {
	case tok.Kind == lex.EOF:
		return "end of input"
	case tok.Kind == lex.Ident:
		return fmt.Sprintf("identifier %q", tok.Text)
	case tok.Kind == lex.String:
		return fmt.Sprintf("string %s", tok)
	case tok.Kind == lex.Int || tok.Kind == lex.Float:
		return fmt.Sprintf("number %s", tok.Text)
	case tok.Kind.IsKeyword():
		return fmt.Sprintf("keyword %s", tok)
	}
	return fmt.Sprintf("%q", tok.String())
}
