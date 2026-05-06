package parse

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/lex"
)

// parseInsert parses an INSERT statement. The cursor is on INSERT.
func (p *Parser) parseInsert() (ast.Stmt, error) {
	stmt := &ast.Insert{Start: p.tok.Pos}
	p.next()

	if _, err := p.expect(lex.Into); err != nil {
		return nil, unexpected(p.tok, "INTO after INSERT")
	}

	table, err := p.expectIdent("a table name")
	if err != nil {
		return nil, err
	}
	stmt.Table = table

	if p.accept(lex.LParen) {
		columns, err := p.parseColumnList()
		if err != nil {
			return nil, err
		}
		stmt.Columns = columns
	}

	if _, err := p.expect(lex.Values); err != nil {
		return nil, unexpected(p.tok, "VALUES")
	}

	for {
		row, err := p.parseValueRow(len(stmt.Columns))
		if err != nil {
			return nil, err
		}
		stmt.Rows = append(stmt.Rows, row)

		if !p.accept(lex.Comma) {
			break
		}
	}

	// Rows must agree with each other even when there is no column list to
	// check against, or the executor would have to reject them one at a time.
	for i, row := range stmt.Rows {
		if len(row) != len(stmt.Rows[0]) {
			return nil, errorf(row[0].Pos(),
				"row %d has %d values but row 1 has %d", i+1, len(row), len(stmt.Rows[0]))
		}
	}
	return stmt, nil
}

// parseColumnList parses a parenthesized list of column names. The cursor is
// past the opening parenthesis.
func (p *Parser) parseColumnList() ([]string, error) {
	var columns []string
	for {
		name, err := p.expectIdent("a column name")
		if err != nil {
			return nil, err
		}
		columns = append(columns, name)

		if !p.accept(lex.Comma) {
			break
		}
	}
	if _, err := p.expect(lex.RParen); err != nil {
		return nil, unexpected(p.tok, "a comma or a closing parenthesis")
	}
	return columns, nil
}

// parseValueRow parses one parenthesized tuple of values. want is the number
// of columns named in the statement, or 0 when none were.
func (p *Parser) parseValueRow(want int) ([]ast.Expr, error) {
	open := p.tok.Pos
	if _, err := p.expect(lex.LParen); err != nil {
		return nil, unexpected(p.tok, "a parenthesized list of values")
	}

	var row []ast.Expr
	for {
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		row = append(row, value)

		if !p.accept(lex.Comma) {
			break
		}
	}
	if _, err := p.expect(lex.RParen); err != nil {
		return nil, unexpected(p.tok, "a comma or a closing parenthesis")
	}

	if want > 0 && len(row) != want {
		return nil, errorf(open, "%d values listed but %d columns named", len(row), want)
	}
	return row, nil
}

// parseUpdate parses an UPDATE statement. The cursor is on UPDATE.
func (p *Parser) parseUpdate() (ast.Stmt, error) {
	stmt := &ast.Update{Start: p.tok.Pos}
	p.next()

	table, err := p.expectIdent("a table name")
	if err != nil {
		return nil, err
	}
	stmt.Table = table

	if _, err := p.expect(lex.Set); err != nil {
		return nil, unexpected(p.tok, "SET")
	}

	for {
		assign, err := p.parseAssignment()
		if err != nil {
			return nil, err
		}
		stmt.Set = append(stmt.Set, assign)

		if !p.accept(lex.Comma) {
			break
		}
	}

	if p.accept(lex.Where) {
		where, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}
	return stmt, nil
}

// parseAssignment parses one "column = expression" pair.
func (p *Parser) parseAssignment() (ast.Assignment, error) {
	column, err := p.expectIdent("a column name")
	if err != nil {
		return ast.Assignment{}, err
	}
	if _, err := p.expect(lex.Eq); err != nil {
		return ast.Assignment{}, unexpected(p.tok, "= after the column name")
	}
	value, err := p.parseExpr()
	if err != nil {
		return ast.Assignment{}, err
	}
	return ast.Assignment{Column: column, Value: value}, nil
}

// parseDelete parses a DELETE statement. The cursor is on DELETE.
func (p *Parser) parseDelete() (ast.Stmt, error) {
	stmt := &ast.Delete{Start: p.tok.Pos}
	p.next()

	if _, err := p.expect(lex.From); err != nil {
		return nil, unexpected(p.tok, "FROM after DELETE")
	}

	table, err := p.expectIdent("a table name")
	if err != nil {
		return nil, err
	}
	stmt.Table = table

	if p.accept(lex.Where) {
		where, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}
	return stmt, nil
}
