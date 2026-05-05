package lex

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// eof is the rune reported once the source is exhausted. It is outside the
// range of valid code points so it can never collide with real input.
const eof rune = -1

// Lexer turns SQL text into tokens. Create one with New and call Next until it
// yields an EOF token or an error.
//
// A Lexer is single-pass and not safe for concurrent use.
type Lexer struct {
	src string

	ch      rune     // rune under the cursor, or eof
	pos     Position // position of ch
	next    int      // byte offset just past ch
	invalid bool     // ch came from an invalid UTF-8 byte
}

// New returns a Lexer over src.
func New(src string) *Lexer {
	l := &Lexer{src: src, pos: Position{Line: 1, Column: 0}}
	l.read()
	return l
}

// ScanAll tokenizes src completely, returning tokens up to but not including
// EOF. It stops at the first error.
func ScanAll(src string) ([]Token, error) {
	l := New(src)
	var toks []Token
	for {
		tok, err := l.Next()
		if err != nil {
			return nil, err
		}
		if tok.Kind == EOF {
			return toks, nil
		}
		toks = append(toks, tok)
	}
}

// read advances the cursor one rune.
func (l *Lexer) read() {
	if l.ch == '\n' {
		l.pos.Line++
		l.pos.Column = 0
	}
	if l.next >= len(l.src) {
		l.ch = eof
		l.pos.Offset = len(l.src)
		l.invalid = false
		return
	}
	r, w := utf8.DecodeRuneInString(l.src[l.next:])
	l.pos.Offset = l.next
	l.pos.Column++
	l.ch = r
	l.invalid = r == utf8.RuneError && w == 1
	l.next += w
}

// peek returns the rune after the cursor without consuming it.
func (l *Lexer) peek() rune {
	if l.next >= len(l.src) {
		return eof
	}
	r, _ := utf8.DecodeRuneInString(l.src[l.next:])
	return r
}

// Next returns the next token. At the end of the input it returns an EOF token
// indefinitely.
func (l *Lexer) Next() (Token, error) {
	if err := l.skipSpace(); err != nil {
		return Token{}, err
	}

	pos := l.pos
	switch {
	case l.ch == eof:
		return Token{Kind: EOF, Pos: pos}, nil
	case l.invalid:
		return Token{}, errorf(pos, "invalid UTF-8 encoding")
	case isIdentStart(l.ch):
		return l.scanWord(pos), nil
	case isDigit(l.ch):
		return l.scanNumber(pos)
	case l.ch == '.' && isDigit(l.peek()):
		return l.scanNumber(pos)
	case l.ch == '\'':
		return l.scanString(pos)
	case l.ch == '"':
		return l.scanQuotedIdent(pos)
	case l.ch == '$':
		return l.scanParam(pos)
	}
	return l.scanOperator(pos)
}

// skipSpace consumes whitespace, -- line comments and /* */ block comments.
func (l *Lexer) skipSpace() error {
	for {
		switch {
		case l.ch == eof:
			return nil
		case unicode.IsSpace(l.ch):
			l.read()
		case l.ch == '-' && l.peek() == '-':
			for l.ch != '\n' && l.ch != eof {
				l.read()
			}
		case l.ch == '/' && l.peek() == '*':
			if err := l.skipBlockComment(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

// skipBlockComment consumes a /* */ comment. Block comments do not nest, which
// matches the SQL standard: the first */ ends the comment.
func (l *Lexer) skipBlockComment() error {
	start := l.pos
	l.read() // '/'
	l.read() // '*'
	for {
		switch {
		case l.ch == eof:
			return errorf(start, "unterminated block comment")
		case l.ch == '*' && l.peek() == '/':
			l.read()
			l.read()
			return nil
		default:
			l.read()
		}
	}
}

// scanWord reads a bare identifier or keyword.
func (l *Lexer) scanWord(pos Position) Token {
	for isIdentPart(l.ch) {
		l.read()
	}
	text := l.src[pos.Offset:l.pos.Offset]
	return Token{Kind: LookupIdent(text), Text: text, Pos: pos}
}

// scanNumber reads an integer or float, including exponent notation.
func (l *Lexer) scanNumber(pos Position) (Token, error) {
	kind := Int
	for isDigit(l.ch) {
		l.read()
	}
	if l.ch == '.' {
		kind = Float
		l.read()
		for isDigit(l.ch) {
			l.read()
		}
		if l.ch == '.' {
			return Token{}, errorf(l.pos, "malformed number: second decimal point")
		}
	}
	if l.ch == 'e' || l.ch == 'E' {
		kind = Float
		l.read()
		if l.ch == '+' || l.ch == '-' {
			l.read()
		}
		if !isDigit(l.ch) {
			return Token{}, errorf(pos, "malformed number: exponent has no digits")
		}
		for isDigit(l.ch) {
			l.read()
		}
	}
	// 123abc is a typo, not a number followed by an identifier.
	if isIdentStart(l.ch) {
		return Token{}, errorf(l.pos, "malformed number: unexpected %q after digits", l.ch)
	}
	return Token{Kind: kind, Text: l.src[pos.Offset:l.pos.Offset], Pos: pos}, nil
}

// scanString reads a 'single quoted' literal. Two adjacent quotes inside the
// literal stand for one quote character.
func (l *Lexer) scanString(pos Position) (Token, error) {
	l.read() // opening quote
	var sb strings.Builder
	for {
		switch {
		case l.ch == eof:
			return Token{}, errorf(pos, "unterminated string literal")
		case l.invalid:
			return Token{}, errorf(l.pos, "invalid UTF-8 encoding in string literal")
		case l.ch == '\'':
			if l.peek() != '\'' {
				l.read()
				return Token{Kind: String, Text: sb.String(), Pos: pos}, nil
			}
			l.read()
			l.read()
			sb.WriteByte('\'')
		default:
			sb.WriteRune(l.ch)
			l.read()
		}
	}
}

// scanQuotedIdent reads a "double quoted" identifier. Quoting preserves case
// and lets reserved words be used as names. Two adjacent quotes stand for one.
func (l *Lexer) scanQuotedIdent(pos Position) (Token, error) {
	l.read() // opening quote
	var sb strings.Builder
	for {
		switch {
		case l.ch == eof || l.ch == '\n':
			return Token{}, errorf(pos, "unterminated quoted identifier")
		case l.invalid:
			return Token{}, errorf(l.pos, "invalid UTF-8 encoding in quoted identifier")
		case l.ch == '"':
			if l.peek() != '"' {
				l.read()
				if sb.Len() == 0 {
					return Token{}, errorf(pos, "empty quoted identifier")
				}
				return Token{Kind: Ident, Text: sb.String(), Pos: pos}, nil
			}
			l.read()
			l.read()
			sb.WriteByte('"')
		default:
			sb.WriteRune(l.ch)
			l.read()
		}
	}
}

// scanParam reads a $1 style placeholder. Text excludes the dollar sign.
func (l *Lexer) scanParam(pos Position) (Token, error) {
	l.read() // '$'
	start := l.pos.Offset
	for isDigit(l.ch) {
		l.read()
	}
	if start == l.pos.Offset {
		return Token{}, errorf(pos, "parameter marker $ must be followed by a number")
	}
	return Token{Kind: Param, Text: l.src[start:l.pos.Offset], Pos: pos}, nil
}

// twoRuneOps are operators whose first rune is meaningless on its own, keyed
// by that first rune.
var twoRuneOps = map[rune]struct {
	second rune
	kind   Kind
}{
	'|': {'|', Concat},
	'!': {'=', NotEq},
}

var singleRuneOps = map[rune]Kind{
	'+': Plus,
	'-': Minus,
	'*': Star,
	'/': Slash,
	'%': Percent,
	'=': Eq,
	'(': LParen,
	')': RParen,
	',': Comma,
	';': Semicolon,
	'.': Dot,
}

// scanOperator reads punctuation and operator tokens.
func (l *Lexer) scanOperator(pos Position) (Token, error) {
	if p, ok := twoRuneOps[l.ch]; ok {
		if l.peek() != p.second {
			return Token{}, errorf(pos, "unexpected character %q", l.ch)
		}
		l.read()
		l.read()
		return Token{Kind: p.kind, Pos: pos}, nil
	}

	first := l.ch
	l.read()

	// Operators whose meaning depends on an optional second rune.
	switch first {
	case '<':
		switch l.ch {
		case '=':
			l.read()
			return Token{Kind: LessEq, Pos: pos}, nil
		case '>':
			l.read()
			return Token{Kind: NotEq, Pos: pos}, nil
		}
		return Token{Kind: Less, Pos: pos}, nil
	case '>':
		if l.ch == '=' {
			l.read()
			return Token{Kind: GreaterEq, Pos: pos}, nil
		}
		return Token{Kind: Greater, Pos: pos}, nil
	}

	if kind, ok := singleRuneOps[first]; ok {
		return Token{Kind: kind, Pos: pos}, nil
	}
	return Token{}, errorf(pos, "unexpected character %q", first)
}

// IsBareIdentifier reports whether s can be written in SQL without double
// quotes: it must be a non-empty run of identifier characters that does not
// start with a digit and is not a reserved word.
func IsBareIdentifier(s string) bool {
	if s == "" || IsReserved(s) {
		return false
	}
	for i, r := range s {
		if i == 0 && !isIdentStart(r) {
			return false
		}
		if i > 0 && !isIdentPart(r) {
			return false
		}
	}
	return true
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || isDigit(r)
}
