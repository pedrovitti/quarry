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
		if p.at(lex.Primary) || p.at(lex.Unique) || p.at(lex.Check) {
			constraint, err := p.parseTableConstraint()
			if err != nil {
				return nil, err
			}
			stmt.Constraints = append(stmt.Constraints, constraint)
		} else {
			column, err := p.parseColumnDef()
			if err != nil {
				return nil, err
			}
			if seen[column.Name] {
				return nil, errorf(column.Start, "column %q is defined twice", column.Name)
			}
			seen[column.Name] = true
			stmt.Columns = append(stmt.Columns, column)
		}

		if !p.accept(lex.Comma) {
			break
		}
	}

	if _, err := p.expect(lex.RParen); err != nil {
		return nil, unexpected(p.tok, "a comma or a closing parenthesis")
	}
	return stmt, nil
}

// parseColumnDef parses one column definition: a name, a type, and the
// constraints written beside it, in whatever order they were written.
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
	for {
		switch {
		case p.at(lex.Not):
			if err := p.expectKeywords(lex.Not, lex.Null); err != nil {
				return ast.ColumnDef{}, unexpected(p.tok, "NULL after NOT")
			}
			def.NotNull = true

		case p.at(lex.Primary):
			if err := p.expectPrimaryKey(); err != nil {
				return ast.ColumnDef{}, err
			}
			def.PrimaryKey = true

		case p.accept(lex.Unique):
			def.Unique = true

		case p.accept(lex.Default):
			value, err := p.parseExpr()
			if err != nil {
				return ast.ColumnDef{}, err
			}
			def.Default = value

		case p.at(lex.Check):
			condition, err := p.parseCheck()
			if err != nil {
				return ast.ColumnDef{}, err
			}
			def.Check = condition

		default:
			return def, nil
		}
	}
}

// parseTableConstraint parses a constraint written as an item of its own. The
// cursor is on PRIMARY, UNIQUE or CHECK.
func (p *Parser) parseTableConstraint() (ast.TableConstraint, error) {
	constraint := ast.TableConstraint{Start: p.tok.Pos}

	switch {
	case p.at(lex.Check):
		condition, err := p.parseCheck()
		if err != nil {
			return ast.TableConstraint{}, err
		}
		constraint.Kind = ast.CheckConstraint
		constraint.Expr = condition
		return constraint, nil

	case p.at(lex.Primary):
		if err := p.expectPrimaryKey(); err != nil {
			return ast.TableConstraint{}, err
		}
		constraint.Kind = ast.PrimaryKeyConstraint

	default:
		p.next()
		constraint.Kind = ast.UniqueConstraint
	}

	columns, err := p.parseConstrainedColumns()
	if err != nil {
		return ast.TableConstraint{}, err
	}
	constraint.Columns = columns
	return constraint, nil
}

// expectPrimaryKey consumes the two words PRIMARY KEY.
func (p *Parser) expectPrimaryKey() error {
	p.next()
	if _, err := p.expect(lex.Key); err != nil {
		return unexpected(p.tok, "KEY after PRIMARY")
	}
	return nil
}

// parseCheck parses "CHECK (condition)". The cursor is on CHECK, and the
// parentheses belong to the clause rather than to the condition, so the
// condition comes back as it was written inside them.
func (p *Parser) parseCheck() (ast.Expr, error) {
	p.next()
	if _, err := p.expect(lex.LParen); err != nil {
		return nil, unexpected(p.tok, "a parenthesized condition after CHECK")
	}
	condition, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lex.RParen); err != nil {
		return nil, unexpected(p.tok, "a closing parenthesis after the condition")
	}
	return condition, nil
}

// parseConstrainedColumns parses the parenthesized column list a key covers.
func (p *Parser) parseConstrainedColumns() ([]string, error) {
	if _, err := p.expect(lex.LParen); err != nil {
		return nil, unexpected(p.tok, "a parenthesized list of columns")
	}
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

// listTypes renders the accepted type names for an error message. The names
// are sorted so the message is stable across runs: ranging over a map would
// order them differently every time.
func listTypes() string {
	names := ast.TypeNames()
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// parseAlterTable parses an ALTER TABLE statement: a table name and a
// comma-separated list of actions. The cursor is on ALTER.
func (p *Parser) parseAlterTable() (ast.Stmt, error) {
	stmt := &ast.AlterTable{Start: p.tok.Pos}
	p.next()

	if _, err := p.expect(lex.Table); err != nil {
		return nil, unexpected(p.tok, "TABLE after ALTER")
	}

	name, err := p.expectIdent("a table name")
	if err != nil {
		return nil, err
	}
	stmt.Table = name

	for {
		action, err := p.parseAlterAction()
		if err != nil {
			return nil, err
		}
		stmt.Actions = append(stmt.Actions, action)

		if !p.accept(lex.Comma) {
			break
		}
	}
	return stmt, nil
}

// parseAlterAction parses one action of an ALTER TABLE list, dispatching on
// its leading keyword.
func (p *Parser) parseAlterAction() (ast.AlterAction, error) {
	switch p.tok.Kind {
	case lex.Rename:
		p.next()
		if p.accept(lex.To) {
			to, err := p.expectIdent("a table name")
			if err != nil {
				return nil, err
			}
			return &ast.RenameTable{To: to}, nil
		}
		if _, err := p.expect(lex.Column); err != nil {
			return nil, unexpected(p.tok, "TO or COLUMN after RENAME")
		}
		from, err := p.expectIdent("a column name")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lex.To); err != nil {
			return nil, unexpected(p.tok, "TO after the column name")
		}
		to, err := p.expectIdent("a column name")
		if err != nil {
			return nil, err
		}
		return &ast.RenameColumn{From: from, To: to}, nil

	case lex.Add:
		p.next()
		if _, err := p.expect(lex.Column); err != nil {
			return nil, unexpected(p.tok, "COLUMN after ADD")
		}
		def, err := p.parseColumnDef()
		if err != nil {
			return nil, err
		}
		action := &ast.AddColumn{Def: def, Default: def.Default}
		action.Def.Default = nil
		return action, nil

	case lex.Drop:
		p.next()
		if _, err := p.expect(lex.Column); err != nil {
			return nil, unexpected(p.tok, "COLUMN after DROP")
		}
		name, err := p.expectIdent("a column name")
		if err != nil {
			return nil, err
		}
		return &ast.DropColumn{Name: name}, nil

	case lex.Alter:
		p.next()
		if _, err := p.expect(lex.Column); err != nil {
			return nil, unexpected(p.tok, "COLUMN after ALTER")
		}
		name, err := p.expectIdent("a column name")
		if err != nil {
			return nil, err
		}
		switch p.tok.Kind {
		case lex.Set:
			p.next()
			if err := p.expectKeywords(lex.Not, lex.Null); err != nil {
				return nil, unexpected(p.tok, "NOT NULL after SET")
			}
			return &ast.SetNotNull{Column: name, NotNull: true}, nil
		case lex.Drop:
			p.next()
			if err := p.expectKeywords(lex.Not, lex.Null); err != nil {
				return nil, unexpected(p.tok, "NOT NULL after DROP")
			}
			return &ast.SetNotNull{Column: name, NotNull: false}, nil
		case lex.Type:
			p.next()
			typeTok := p.tok
			typeName, err := p.expectIdent("a column type")
			if err != nil {
				return nil, err
			}
			columnType, ok := ast.LookupType(typeName)
			if !ok {
				return nil, errorf(typeTok.Pos,
					"unknown column type %q; quarry has %s", typeName, listTypes())
			}
			action := &ast.SetColumnType{Column: name, Type: columnType}
			if p.accept(lex.Using) {
				value, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				action.Using = value
			}
			return action, nil
		}
		return nil, unexpected(p.tok, "SET NOT NULL, DROP NOT NULL or TYPE")
	}
	return nil, unexpected(p.tok, "RENAME, ADD, DROP or ALTER COLUMN")
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
