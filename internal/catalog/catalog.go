package catalog

import (
	"fmt"
	"sort"

	"github.com/pedrovitti/quarry/internal/ast"
)

// Error is a schema error: a missing table, a duplicate name, a column that
// does not exist.
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func errorf(format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// Table is a named schema.
type Table struct {
	Name   string
	Schema *Schema
}

// Catalog is the set of tables a database knows about.
//
// It is not safe for concurrent use; the session owning it serializes access.
type Catalog struct {
	tables map[string]*Table
}

// New returns an empty catalog.
func New() *Catalog {
	return &Catalog{tables: make(map[string]*Table)}
}

// Create adds a table whose only constraints are the ones its columns carry.
// It reports an error if the name is taken.
func (c *Catalog) Create(name string, columns []Column) (*Table, error) {
	return c.CreateWith(name, columns, nil)
}

// CreateWith adds a table with constraints over several columns beside the
// ones the columns carry themselves.
//
// The constraints are checked against the columns here, so a table that
// reaches the catalog is one whose rules can be enforced: every column a key
// names exists, and there is at most one primary key.
func (c *Catalog) CreateWith(name string, columns []Column, constraints []Constraint) (*Table, error) {
	if _, exists := c.tables[name]; exists {
		return nil, errorf("table %q already exists", name)
	}
	if len(columns) == 0 {
		return nil, errorf("table %q has no columns", name)
	}

	schema, err := NewSchemaWith(columns, constraints)
	if err != nil {
		return nil, err
	}
	if err := validateConstraints(name, schema); err != nil {
		return nil, err
	}
	markKeyColumnsNotNull(schema)

	table := &Table{Name: name, Schema: schema}
	c.tables[name] = table
	return table, nil
}

// validateConstraints rejects a schema whose constraints do not fit its
// columns. Conditions are not checked here: a condition is an expression, and
// only the executor can compile one.
func validateConstraints(name string, schema *Schema) error {
	keys := 0
	for _, constraint := range schema.Constraints() {
		if constraint.Kind == ast.PrimaryKeyConstraint {
			keys++
		}
		for _, column := range constraint.Columns {
			if _, ok := schema.Index(column); !ok {
				return errorf("table %q has no column %q", name, column)
			}
		}
	}
	if keys > 1 {
		return errorf("table %q has more than one primary key", name)
	}
	return nil
}

// markKeyColumnsNotNull applies what a primary key means to the columns under
// it, so that the rest of the system, and the schema text, need know only
// about NOT NULL.
func markKeyColumnsNotNull(schema *Schema) {
	for _, constraint := range schema.Constraints() {
		if constraint.Kind != ast.PrimaryKeyConstraint {
			continue
		}
		for _, name := range constraint.Columns {
			if i, ok := schema.Index(name); ok {
				schema.columns[i].NotNull = true
			}
		}
	}
}

// CreateFromStmt adds the table described by a parsed CREATE TABLE, with the
// constraints it declares. A DEFAULT is not among them: it is an expression,
// and a caller that has an evaluator reduces it to a value first.
func (c *Catalog) CreateFromStmt(stmt *ast.CreateTable) (*Table, error) {
	columns := make([]Column, len(stmt.Columns))
	for i, def := range stmt.Columns {
		if def.Default != nil {
			return nil, errorf("column %q has a DEFAULT that has not been reduced to a value", def.Name)
		}
		columns[i] = ColumnFromDef(def)
	}
	return c.CreateWith(stmt.Name, columns, ConstraintsFromStmt(stmt))
}

// ConstraintsFromStmt converts the constraints a parsed CREATE TABLE writes
// as items of their own.
func ConstraintsFromStmt(stmt *ast.CreateTable) []Constraint {
	out := make([]Constraint, len(stmt.Constraints))
	for i, clause := range stmt.Constraints {
		out[i] = ConstraintFromClause(clause)
	}
	return out
}

// Drop removes a table, reporting an error if it does not exist.
func (c *Catalog) Drop(name string) error {
	if _, exists := c.tables[name]; !exists {
		return errorf("table %q does not exist", name)
	}
	delete(c.tables, name)
	return nil
}

// Get returns a table by name.
func (c *Catalog) Get(name string) (*Table, error) {
	table, exists := c.tables[name]
	if !exists {
		return nil, errorf("table %q does not exist", name)
	}
	return table, nil
}

// Has reports whether a table exists.
func (c *Catalog) Has(name string) bool {
	_, exists := c.tables[name]
	return exists
}

// Len is the number of tables.
func (c *Catalog) Len() int { return len(c.tables) }

// Names returns every table name, sorted.
//
// Sorted rather than in creation order because the only alternative is map
// iteration, which Go randomizes: anything that printed this list would differ
// between runs.
func (c *Catalog) Names() []string {
	names := make([]string, 0, len(c.tables))
	for name := range c.tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Tables returns every table, ordered by name.
func (c *Catalog) Tables() []*Table {
	out := make([]*Table, 0, len(c.tables))
	for _, name := range c.Names() {
		out = append(out, c.tables[name])
	}
	return out
}
