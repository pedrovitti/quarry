// Package catalog holds the schemas of the tables a database knows about.
//
// The catalog is pure metadata: it says what a table looks like, never what is
// in it. Rows live in the storage package, keyed by the same table names.
package catalog

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/types"
)

// Column is one column of a table.
type Column struct {
	Name string
	// Type is the type as declared in CREATE TABLE.
	Type ast.TypeName
	// NotNull rejects NULL on insert and update.
	NotNull bool
	// PrimaryKey and Unique are the keys the column carries on its own. A key
	// over several columns is a constraint on the schema instead.
	PrimaryKey bool
	Unique     bool
	// Default is what the column holds in a row that does not name it, and is
	// used only when HasDefault is set: NULL is a default a column can be
	// given, and it is not the same as having none.
	Default    types.Value
	HasDefault bool
	// Check is a condition over the row, written beside this column.
	Check ast.Expr
}

// Kind is the runtime kind of the column's values.
func (c Column) Kind() types.Kind { return types.KindOf(c.Type) }

// String renders the column as it would appear in a CREATE TABLE, with its
// constraints in one fixed order however they were declared.
func (c Column) String() string {
	out := ast.QuoteIdent(c.Name) + " " + c.Type.String()
	if c.NotNull {
		out += " NOT NULL"
	}
	if c.PrimaryKey {
		out += " PRIMARY KEY"
	}
	if c.Unique {
		out += " UNIQUE"
	}
	if c.HasDefault {
		out += " DEFAULT " + c.Default.SQL()
	}
	if c.Check != nil {
		out += " CHECK (" + c.Check.String() + ")"
	}
	return out
}

// ColumnFromDef converts a parsed column definition. A DEFAULT is left out:
// it is an expression, and reducing it to a value is the engine's job.
func ColumnFromDef(def ast.ColumnDef) Column {
	return Column{
		Name:       def.Name,
		Type:       def.Type,
		NotNull:    def.NotNull,
		PrimaryKey: def.PrimaryKey,
		Unique:     def.Unique,
		Check:      def.Check,
	}
}
