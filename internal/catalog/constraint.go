package catalog

import (
	"strings"

	"github.com/pedrovitti/quarry/internal/ast"
)

// Constraint is one rule a row has to satisfy, in the form the executor reads
// it: a key names the columns it covers, a check carries its condition.
//
// A constraint written beside a column and one written as an item of its own
// are the same thing here. The difference survives only in the schema text,
// where a column-level constraint is printed back beside its column.
type Constraint struct {
	Kind    ast.ConstraintKind
	Columns []string
	Expr    ast.Expr
}

// IsKey reports whether the constraint is one rows have to differ on.
func (c Constraint) IsKey() bool {
	return c.Kind == ast.PrimaryKeyConstraint || c.Kind == ast.UniqueConstraint
}

// String renders the constraint the way it would be written as an item of a
// CREATE TABLE, which is also how an error message names it.
func (c Constraint) String() string {
	if c.Kind == ast.CheckConstraint {
		return "CHECK (" + c.Expr.String() + ")"
	}
	quoted := make([]string, len(c.Columns))
	for i, name := range c.Columns {
		quoted[i] = ast.QuoteIdent(name)
	}
	return c.Kind.String() + " (" + strings.Join(quoted, ", ") + ")"
}

// ConstraintFromClause converts a constraint written as its own item.
func ConstraintFromClause(clause ast.TableConstraint) Constraint {
	columns := make([]string, len(clause.Columns))
	copy(columns, clause.Columns)
	return Constraint{Kind: clause.Kind, Columns: columns, Expr: clause.Expr}
}
