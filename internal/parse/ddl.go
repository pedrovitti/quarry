package parse

import (
	"sort"
	"strings"

	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/lex"
)

// parseCreateTable parses a CREATE TABLE statement. The cursor is on CREATE.
func (p *Parser) parseCreateTable() (ast.Stmt, error) {
	stmt := &ast.CreateTable{Start: p.tok.Pos}
	p.next()

	if _, err := p.expect(lex.Table); err != nil {
		return nil, unexpected(p.tok, "TABLE after CREATE")
	}

	name, err := p.expectIdent("a table name")
	if err != nil {
		return nil, err
	}
	stmt.Name = name

	if _, err := p.expect(lex.LParen); err != nil {
		return nil, unexpected(p.tok, "a parenthesized list of columns")
	}

	seen := make(map[string]bool)
	for {
		column, err := p.parseColumnDef()
		if err != nil {
			return nil, err
		}
		if seen[column.Name] {
			return nil, errorf(column.Start, "column %q is defined twice", column.Name)
		}
		seen[column.Name] = true
		stmt.Columns = append(stmt.Columns, column)

		if !p.accept(lex.Comma) {
			break
		}
	}

	if _, err := p.expect(lex.RParen); err != nil {
		return nil, unexpected(p.tok, "a comma or a closing parenthesis")
	}
	return stmt, nil
}

// parseColumnDef parses one column definition: a name, a type, and an
// optional NOT NULL.
func (p *Parser) parseColumnDef() (ast.ColumnDef, error) {
	pos := p.tok.Pos
	name, err := p.expectIdent("a column name")
	if err != nil {
		return ast.ColumnDef{}, err
	}

	typeTok := p.tok
	typeName, err := p.expectIdent("a column type")
	if err != nil {
		return ast.ColumnDef{}, err
	}
	columnType, ok := ast.LookupType(typeName)
	if !ok {
		return ast.ColumnDef{}, errorf(typeTok.Pos,
			"unknown column type %q; quarry has %s", typeName, listTypes())
	}

	def := ast.ColumnDef{Name: name, Type: columnType, Start: pos}
	if p.at(lex.Not) {
		if err := p.expectKeywords(lex.Not, lex.Null); err != nil {
			return ast.ColumnDef{}, unexpected(p.tok, "NULL after NOT")
		}
		def.NotNull = true
	}
	return def, nil
}

// listTypes renders the accepted type names for an error message. The names
// are sorted so the message is stable across runs: ranging over a map would
// order them differently every time.
func listTypes() string {
	names := ast.TypeNames()
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// parseDropTable parses a DROP TABLE statement. The cursor is on DROP.
func (p *Parser) parseDropTable() (ast.Stmt, error) {
	stmt := &ast.DropTable{Start: p.tok.Pos}
	p.next()

	if _, err := p.expect(lex.Table); err != nil {
		return nil, unexpected(p.tok, "TABLE after DROP")
	}

	name, err := p.expectIdent("a table name")
	if err != nil {
		return nil, err
	}
	stmt.Name = name
	return stmt, nil
}
