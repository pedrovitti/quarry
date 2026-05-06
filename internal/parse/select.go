package parse

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/lex"
)

// parseSelect parses a SELECT statement. The cursor is on SELECT.
func (p *Parser) parseSelect() (ast.Stmt, error) {
	stmt := &ast.Select{Start: p.tok.Pos}
	p.next()

	stmt.Distinct = p.accept(lex.Distinct)

	columns, err := p.parseSelectList()
	if err != nil {
		return nil, err
	}
	stmt.Columns = columns

	if p.accept(lex.From) {
		from, err := p.parseTableRef()
		if err != nil {
			return nil, err
		}
		stmt.From = from
	}

	if p.accept(lex.Where) {
		where, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}

	if p.at(lex.Order) {
		if err := p.expectKeywords(lex.Order, lex.By); err != nil {
			return nil, err
		}
		order, err := p.parseOrderBy()
		if err != nil {
			return nil, err
		}
		stmt.OrderBy = order
	}

	if p.accept(lex.Limit) {
		limit, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt.Limit = limit
	}

	if p.accept(lex.Offset) {
		offset, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt.Offset = offset
	}

	// OFFSET before LIMIT is a common slip and the message is worth getting
	// right, since "unexpected keyword LIMIT" would not explain it.
	if p.at(lex.Limit) {
		return nil, errorf(p.tok.Pos, "LIMIT must come before OFFSET")
	}
	return stmt, nil
}

// parseSelectList parses the comma-separated projection.
func (p *Parser) parseSelectList() ([]ast.SelectItem, error) {
	var items []ast.SelectItem
	for {
		p.starOK = true
		expr, err := p.parseExpr()
		p.starOK = false
		if err != nil {
			return nil, err
		}
		if err := checkStarIsWholeItem(expr); err != nil {
			return nil, err
		}
		alias, err := p.parseAlias()
		if err != nil {
			return nil, err
		}
		if alias != "" {
			if _, isStar := expr.(*ast.Star); isStar {
				return nil, errorf(expr.Pos(), "* cannot be given an alias")
			}
		}
		items = append(items, ast.SelectItem{Expr: expr, Alias: alias})

		if !p.accept(lex.Comma) {
			return items, nil
		}
	}
}

// checkStarIsWholeItem rejects a star buried inside a larger expression. The
// select list is the only place * is allowed at all, and even there it has to
// be the entire item: "SELECT count(*) + 1" is fine to write but "SELECT * + 1"
// is not, because a projection is not a value.
func checkStarIsWholeItem(item ast.Expr) error {
	if _, ok := item.(*ast.Star); ok {
		return nil
	}
	for _, node := range ast.Inspect(item, func(n ast.Node) bool {
		_, ok := n.(*ast.Star)
		return ok
	}) {
		return errorf(node.Pos(), "* cannot appear inside an expression")
	}
	return nil
}

// parseAlias parses an optional alias, with or without the AS keyword. It
// returns "" when there is none.
//
// Without AS the alias is just an identifier, so it has to be distinguished
// from the keyword that starts the next clause: FROM, WHERE and the rest are
// reserved, which is what makes "SELECT a FROM t" unambiguous.
func (p *Parser) parseAlias() (string, error) {
	if p.accept(lex.As) {
		return p.expectIdent("an alias after AS")
	}
	if p.at(lex.Ident) {
		name := p.tok.Text
		p.next()
		return name, nil
	}
	return "", nil
}

// parseTableRef parses a FROM clause: a table, optionally joined to further
// tables. Joins associate to the left, so "a JOIN b JOIN c" nests as
// "(a JOIN b) JOIN c".
func (p *Parser) parseTableRef() (ast.TableRef, error) {
	left, err := p.parseTableName()
	if err != nil {
		return nil, err
	}

	for p.accept(lex.Join) {
		right, err := p.parseTableName()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lex.On); err != nil {
			return nil, unexpected(p.tok, "ON followed by a join condition")
		}
		on, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		left = &ast.Join{Left: left, Right: right, On: on}
	}
	return left, nil
}

// parseTableName parses a table name and its optional alias.
func (p *Parser) parseTableName() (ast.TableRef, error) {
	pos := p.tok.Pos
	name, err := p.expectIdent("a table name")
	if err != nil {
		return nil, err
	}
	alias, err := p.parseAlias()
	if err != nil {
		return nil, err
	}
	return &ast.TableName{Name: name, Alias: alias, Start: pos}, nil
}

// parseOrderBy parses the sort list. The cursor is past ORDER BY.
func (p *Parser) parseOrderBy() ([]ast.OrderItem, error) {
	var items []ast.OrderItem
	for {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		item := ast.OrderItem{Expr: expr}
		switch {
		case p.accept(lex.Desc):
			item.Desc = true
		case p.accept(lex.Asc):
			item.Desc = false
		}
		items = append(items, item)

		if !p.accept(lex.Comma) {
			return items, nil
		}
	}
}
