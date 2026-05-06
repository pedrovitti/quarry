// Package parse turns SQL text into an ast tree.
//
// The parser is recursive descent with precedence climbing for expressions. It
// carries one token of lookahead, which is enough for every construct quarry
// accepts: the only genuinely ambiguous point is an identifier in a select
// list, where the token after it decides between a qualified name, a function
// call and an implicit alias.
package parse

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/lex"
)

// Parser reads statements from a SQL string.
type Parser struct {
	lexer *lex.Lexer

	tok    lex.Token  // token under the cursor
	ahead  *lex.Token // lookahead, filled on demand by peek
	failed error      // first error seen; the parser stops advancing after it

	// starOK is set while parsing a select item, the only place * and t.* are
	// expressions. Everywhere else * is multiplication and a bare one is an
	// error rather than a projection.
	starOK bool
}

// New returns a parser over src, positioned at its first token.
func New(src string) *Parser {
	p := &Parser{lexer: lex.New(src)}
	p.next()
	return p
}

// Parse parses exactly one statement, which must be the whole of src apart
// from an optional trailing semicolon.
func Parse(src string) (ast.Stmt, error) {
	p := New(src)
	stmt, err := p.Statement()
	if err != nil {
		return nil, err
	}
	p.accept(lex.Semicolon)
	if !p.at(lex.EOF) {
		return nil, unexpected(p.tok, "end of statement")
	}
	return stmt, p.failed
}

// ParseAll parses every statement in src. Statements are separated by
// semicolons; a trailing semicolon is allowed but not required.
func ParseAll(src string) ([]ast.Stmt, error) {
	p := New(src)
	var stmts []ast.Stmt
	for !p.at(lex.EOF) {
		stmt, err := p.Statement()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)

		if !p.accept(lex.Semicolon) && !p.at(lex.EOF) {
			return nil, unexpected(p.tok, "a semicolon between statements")
		}
	}
	return stmts, p.failed
}

// ParseExpr parses a single expression, which must be the whole of src. It
// exists for callers that need an expression outside a statement, such as
// tests and the REPL's scratch evaluator.
func ParseExpr(src string) (ast.Expr, error) {
	p := New(src)
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if !p.at(lex.EOF) {
		return nil, unexpected(p.tok, "end of expression")
	}
	return expr, p.failed
}

// Statement parses the next statement, dispatching on its leading keyword.
func (p *Parser) Statement() (ast.Stmt, error) {
	switch p.tok.Kind {
	case lex.Select:
		return p.parseSelect()
	case lex.Insert:
		return p.parseInsert()
	case lex.Update:
		return p.parseUpdate()
	case lex.Delete:
		return p.parseDelete()
	case lex.Create:
		return p.parseCreateTable()
	case lex.Drop:
		return p.parseDropTable()
	}
	return nil, unexpected(p.tok, "the start of a statement")
}

// next advances the cursor by one token. A lexical error is recorded and the
// cursor is parked on EOF, so callers can keep using p.tok without checking.
func (p *Parser) next() {
	if p.ahead != nil {
		p.tok = *p.ahead
		p.ahead = nil
		return
	}
	tok, err := p.lexer.Next()
	if err != nil {
		if p.failed == nil {
			p.failed = err
		}
		p.tok = lex.Token{Kind: lex.EOF, Pos: p.tok.Pos}
		return
	}
	p.tok = tok
}

// peek returns the token after the cursor without consuming it.
func (p *Parser) peek() lex.Token {
	if p.ahead == nil {
		tok, err := p.lexer.Next()
		if err != nil {
			if p.failed == nil {
				p.failed = err
			}
			tok = lex.Token{Kind: lex.EOF, Pos: p.tok.Pos}
		}
		p.ahead = &tok
	}
	return *p.ahead
}

// at reports whether the cursor is on a token of the given kind.
func (p *Parser) at(kind lex.Kind) bool { return p.tok.Kind == kind }

// accept consumes the current token if it is of the given kind.
func (p *Parser) accept(kind lex.Kind) bool {
	if p.at(kind) {
		p.next()
		return true
	}
	return false
}

// expect consumes the current token, which must be of the given kind.
func (p *Parser) expect(kind lex.Kind) (lex.Token, error) {
	if !p.at(kind) {
		return lex.Token{}, unexpected(p.tok, quoted(kind))
	}
	tok := p.tok
	p.next()
	return tok, nil
}

// expectKeywords consumes a run of keywords that must appear together, such as
// ORDER BY or IS NOT NULL.
func (p *Parser) expectKeywords(kinds ...lex.Kind) error {
	for _, kind := range kinds {
		if _, err := p.expect(kind); err != nil {
			return err
		}
	}
	return nil
}

// expectIdent consumes an identifier and returns its name. Reserved words are
// rejected with a message pointing at the fix, since using one as a name is a
// common mistake and quoting is not obvious.
func (p *Parser) expectIdent(what string) (string, error) {
	if p.tok.Kind.IsKeyword() {
		return "", errorf(p.tok.Pos,
			"%s is a reserved word and cannot be used as %s; write it as %q to use it anyway",
			p.tok, what, p.tok.Text)
	}
	tok, err := p.expect(lex.Ident)
	if err != nil {
		return "", unexpected(p.tok, what)
	}
	return tok.Text, nil
}

// quoted renders a token kind for an "expected ..." message.
func quoted(kind lex.Kind) string {
	if kind.IsKeyword() {
		return kind.String()
	}
	switch kind {
	case lex.Ident:
		return "an identifier"
	case lex.EOF:
		return "end of input"
	}
	return "\"" + kind.String() + "\""
}
