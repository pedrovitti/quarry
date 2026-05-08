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
}

// Kind is the runtime kind of the column's values.
func (c Column) Kind() types.Kind { return types.KindOf(c.Type) }

// String renders the column as it would appear in a CREATE TABLE.
func (c Column) String() string {
	out := ast.QuoteIdent(c.Name) + " " + c.Type.String()
	if c.NotNull {
		out += " NOT NULL"
	}
	return out
}

// ColumnFromDef converts a parsed column definition.
func ColumnFromDef(def ast.ColumnDef) Column {
	return Column{Name: def.Name, Type: def.Type, NotNull: def.NotNull}
}
