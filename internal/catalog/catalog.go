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

// Create adds a table. It reports an error if the name is taken.
func (c *Catalog) Create(name string, columns []Column) (*Table, error) {
	if _, exists := c.tables[name]; exists {
		return nil, errorf("table %q already exists", name)
	}
	if len(columns) == 0 {
		return nil, errorf("table %q has no columns", name)
	}

	schema, err := NewSchema(columns)
	if err != nil {
		return nil, err
	}
	table := &Table{Name: name, Schema: schema}
	c.tables[name] = table
	return table, nil
}

// CreateFromStmt adds the table described by a parsed CREATE TABLE.
func (c *Catalog) CreateFromStmt(stmt *ast.CreateTable) (*Table, error) {
	columns := make([]Column, len(stmt.Columns))
	for i, def := range stmt.Columns {
		columns[i] = ColumnFromDef(def)
	}
	return c.Create(stmt.Name, columns)
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
