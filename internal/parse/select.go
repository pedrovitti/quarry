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

	if p.at(lex.Group) {
		if err := p.expectKeywords(lex.Group, lex.By); err != nil {
			return nil, err
		}
		group, err := p.parseGroupBy()
		if err != nil {
			return nil, err
		}
		stmt.GroupBy = group
	}

	if p.accept(lex.Having) {
		having, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt.Having = having
	}

	// The clauses are written in the order they are evaluated, and a HAVING
	// ahead of its GROUP BY is common enough to name rather than report as an
	// unexpected keyword.
	if p.at(lex.Group) {
		return nil, errorf(p.tok.Pos, "GROUP BY must come before HAVING")
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
// be the entire item or the argument of count: "SELECT count(*) + 1" is fine
// to write but "SELECT * + 1" is not, because a projection is not a value.
func checkStarIsWholeItem(item ast.Expr) error {
	if _, ok := item.(*ast.Star); ok {
		return nil
	}

	// A star inside a call was already held to the count(*) rule as the call
	// was parsed, so what is left to reject is a star loose in an expression.
	counted := map[ast.Node]bool{}
	ast.Walk(item, func(n ast.Node) bool {
		call, ok := n.(*ast.FuncCall)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			if star, ok := arg.(*ast.Star); ok {
				counted[star] = true
			}
		}
		return true
	})

	for _, node := range ast.Inspect(item, func(n ast.Node) bool {
		return isStar(n) && !counted[n]
	}) {
		return errorf(node.Pos(), "* cannot appear inside an expression")
	}
	return nil
}

// parseGroupBy parses the grouping list. The cursor is past GROUP BY.
func (p *Parser) parseGroupBy() ([]ast.Expr, error) {
	var exprs []ast.Expr
	for {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, expr)

		if !p.accept(lex.Comma) {
			return exprs, nil
		}
	}
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
	left, err := p.parseTableSource()
	if err != nil {
		return nil, err
	}

	for p.accept(lex.Join) {
		right, err := p.parseTableSource()
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

// parseTableSource parses one source in a FROM clause: a table name, or a
// parenthesized SELECT, optionally marked LATERAL, that must then be given a
// name.
func (p *Parser) parseTableSource() (ast.TableRef, error) {
	lateral := p.at(lex.Lateral)
	if lateral {
		p.next()
		if !p.at(lex.LParen) {
			return nil, unexpected(p.tok, "a subquery after LATERAL")
		}
	}
	if !p.at(lex.LParen) {
		return p.parseTableName()
	}
	pos := p.tok.Pos
	p.next()
	if !p.at(lex.Select) {
		return nil, unexpected(p.tok, "SELECT after ( in FROM")
	}
	stmt, err := p.parseSelect()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lex.RParen); err != nil {
		return nil, unexpected(p.tok, "a closing parenthesis after the subquery")
	}
	alias, err := p.parseAlias()
	if err != nil {
		return nil, err
	}
	if alias == "" {
		return nil, errorf(pos, "a subquery in FROM needs a name")
	}
	return &ast.SubqueryTable{Select: stmt.(*ast.Select), Alias: alias, Lateral: lateral, Start: pos}, nil
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
