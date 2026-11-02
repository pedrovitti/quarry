package catalog

import (
	"strings"

	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/types"
)

// Schema is an ordered list of columns. Order is part of the schema: it is
// what "INSERT INTO t VALUES (...)" without a column list means, and what
// SELECT * expands to.
type Schema struct {
	columns     []Column
	constraints []Constraint
	byName      map[string]int
}

// NewSchema builds a schema whose only constraints are the ones its columns
// carry. It reports an error if two columns share a name, since every later
// lookup would then be ambiguous.
func NewSchema(columns []Column) (*Schema, error) {
	return NewSchemaWith(columns, nil)
}

// NewSchemaWith builds a schema with constraints over several columns beside
// the ones the columns carry themselves.
func NewSchemaWith(columns []Column, constraints []Constraint) (*Schema, error) {
	s := &Schema{
		columns:     make([]Column, len(columns)),
		constraints: make([]Constraint, len(constraints)),
		byName:      make(map[string]int, len(columns)),
	}
	copy(s.columns, columns)
	copy(s.constraints, constraints)

	for i, column := range columns {
		if _, taken := s.byName[column.Name]; taken {
			return nil, errorf("column %q is defined twice", column.Name)
		}
		s.byName[column.Name] = i
	}
	return s, nil
}

// Len is the number of columns.
func (s *Schema) Len() int { return len(s.columns) }

// Columns returns the columns in declaration order. The result shares no
// storage with the schema, so callers may keep or modify it.
func (s *Schema) Columns() []Column {
	out := make([]Column, len(s.columns))
	copy(out, s.columns)
	return out
}

// Column returns the column at an index, which must be in range.
func (s *Schema) Column(i int) Column { return s.columns[i] }

// Index returns the position of a named column, and false if there is none.
// Names are case sensitive: quarry folds nothing, so "ID" and "id" are
// different columns if both were declared.
func (s *Schema) Index(name string) (int, bool) {
	i, ok := s.byName[name]
	return i, ok
}

// Find returns a named column.
func (s *Schema) Find(name string) (Column, bool) {
	i, ok := s.byName[name]
	if !ok {
		return Column{}, false
	}
	return s.columns[i], true
}

// Names returns the column names in declaration order.
func (s *Schema) Names() []string {
	out := make([]string, len(s.columns))
	for i, column := range s.columns {
		out[i] = column.Name
	}
	return out
}

// Kinds returns the runtime kind of each column, in declaration order.
func (s *Schema) Kinds() []types.Kind {
	out := make([]types.Kind, len(s.columns))
	for i, column := range s.columns {
		out[i] = column.Kind()
	}
	return out
}

// Constraints returns every constraint the schema states, the ones written
// beside a column first, in column order, then the ones written over several
// columns, in declaration order.
//
// Flattening them into one list is what lets the executor enforce a key
// without caring which way it was written.
func (s *Schema) Constraints() []Constraint {
	out := make([]Constraint, 0, len(s.constraints)+len(s.columns))
	for _, column := range s.columns {
		if column.PrimaryKey {
			out = append(out, Constraint{Kind: ast.PrimaryKeyConstraint, Columns: []string{column.Name}})
		}
		if column.Unique {
			out = append(out, Constraint{Kind: ast.UniqueConstraint, Columns: []string{column.Name}})
		}
		if column.Check != nil {
			out = append(out, Constraint{Kind: ast.CheckConstraint, Expr: column.Check})
		}
	}
	return append(out, s.constraints...)
}

// String renders the schema as a column list followed by the constraints that
// were written over several columns.
func (s *Schema) String() string {
	parts := make([]string, 0, len(s.columns)+len(s.constraints))
	for _, column := range s.columns {
		parts = append(parts, column.String())
	}
	for _, constraint := range s.constraints {
		parts = append(parts, constraint.String())
	}
	return strings.Join(parts, ", ")
}
