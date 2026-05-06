package parse

import (
	"strconv"

	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/lex"
)

// parseExpr parses a full expression.
func (p *Parser) parseExpr() (ast.Expr, error) {
	return p.parseBinary(lowest)
}

// parseBinary is the precedence-climbing loop. It parses operands at binding
// power strictly greater than min, so that a left-associative operator stops
// at the next operator of equal precedence and the loop reassociates.
func (p *Parser) parseBinary(min precedence) (ast.Expr, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}

	for {
		prec := precedenceOf(p.tok.Kind)
		if prec <= min {
			return left, nil
		}

		// IS is postfix, not infix: it takes NULL rather than an expression.
		if p.at(lex.Is) {
			left, err = p.parseIsNull(left)
			if err != nil {
				return nil, err
			}
			continue
		}

		op, ok := ast.BinaryOpFor(p.tok.Kind)
		if !ok {
			return nil, unexpected(p.tok, "an operator")
		}
		p.next()

		right, err := p.parseBinary(prec)
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{Op: op, Left: left, Right: right}
	}
}

// parsePrefix parses NOT and the unary sign operators, then a primary.
func (p *Parser) parsePrefix() (ast.Expr, error) {
	switch p.tok.Kind {
	case lex.Not:
		pos := p.tok.Pos
		p.next()
		operand, err := p.parseBinary(notPrec)
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Op: ast.LogicalNot, Operand: operand, Start: pos}, nil

	case lex.Minus, lex.Plus:
		pos := p.tok.Pos
		op := ast.Negate
		if p.at(lex.Plus) {
			op = ast.Plus
		}
		p.next()
		operand, err := p.parseBinary(unaryPrec)
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Op: op, Operand: operand, Start: pos}, nil
	}
	return p.parsePrimary()
}

// parseIsNull parses the postfix "IS NULL" and "IS NOT NULL" on an operand
// that has already been read. The cursor is on IS.
func (p *Parser) parseIsNull(operand ast.Expr) (ast.Expr, error) {
	p.next() // IS
	negated := p.accept(lex.Not)
	if _, err := p.expect(lex.Null); err != nil {
		if negated {
			return nil, unexpected(p.tok, "NULL after IS NOT")
		}
		return nil, unexpected(p.tok, "NULL or NOT NULL after IS")
	}
	return &ast.IsNullExpr{Operand: operand, Negated: negated}, nil
}

// parsePrimary parses a literal, a parenthesized expression, a column
// reference, a star or a function call.
func (p *Parser) parsePrimary() (ast.Expr, error) {
	tok := p.tok
	switch tok.Kind {
	case lex.Int:
		p.next()
		return &ast.Literal{Kind: ast.IntLit, Text: tok.Text, Start: tok.Pos}, nil

	case lex.Float:
		p.next()
		return &ast.Literal{Kind: ast.FloatLit, Text: tok.Text, Start: tok.Pos}, nil

	case lex.String:
		p.next()
		return &ast.Literal{Kind: ast.StringLit, Text: tok.Text, Start: tok.Pos}, nil

	case lex.True, lex.False:
		p.next()
		return &ast.Literal{Kind: ast.BoolLit, Text: tok.String(), Start: tok.Pos}, nil

	case lex.Null:
		p.next()
		return &ast.Literal{Kind: ast.NullLit, Start: tok.Pos}, nil

	case lex.Param:
		p.next()
		ordinal, err := strconv.Atoi(tok.Text)
		if err != nil || ordinal < 1 {
			return nil, errorf(tok.Pos, "parameter number must be 1 or greater")
		}
		return &ast.Param{Ordinal: ordinal, Start: tok.Pos}, nil

	case lex.Star:
		if !p.starOK {
			return nil, unexpected(p.tok, "an expression")
		}
		p.next()
		return &ast.Star{Start: tok.Pos}, nil

	case lex.LParen:
		p.next()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lex.RParen); err != nil {
			return nil, unexpected(p.tok, "a closing parenthesis")
		}
		return &ast.ParenExpr{Inner: inner, Start: tok.Pos}, nil

	case lex.Ident:
		return p.parseIdentExpr()
	}
	return nil, unexpected(p.tok, "an expression")
}

// parseIdentExpr resolves the one place the grammar needs lookahead: a bare
// identifier is a column, "a.b" is a qualified column, "a.*" is a qualified
// star, and "a(" is a function call.
func (p *Parser) parseIdentExpr() (ast.Expr, error) {
	tok := p.tok
	p.next()

	switch p.tok.Kind {
	case lex.Dot:
		p.next()
		if p.at(lex.Star) {
			if !p.starOK {
				return nil, errorf(p.tok.Pos, "%s.* is only valid in a select list", tok.Text)
			}
			p.next()
			return &ast.Star{Table: tok.Text, Start: tok.Pos}, nil
		}
		name, err := p.expectIdent("a column name after the dot")
		if err != nil {
			return nil, err
		}
		return &ast.ColumnRef{Table: tok.Text, Name: name, Start: tok.Pos}, nil

	case lex.LParen:
		return p.parseCallArgs(tok)
	}
	return &ast.ColumnRef{Name: tok.Text, Start: tok.Pos}, nil
}

// parseCallArgs parses the argument list of a function call. The cursor is on
// the opening parenthesis and name has already been consumed.
func (p *Parser) parseCallArgs(name lex.Token) (ast.Expr, error) {
	p.next() // (
	call := &ast.FuncCall{Name: name.Text, Start: name.Pos}

	if p.accept(lex.RParen) {
		return call, nil
	}
	for {
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)
		if !p.accept(lex.Comma) {
			break
		}
	}
	if _, err := p.expect(lex.RParen); err != nil {
		return nil, unexpected(p.tok, "a comma or a closing parenthesis")
	}
	return call, nil
}
