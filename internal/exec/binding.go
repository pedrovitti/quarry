// Package exec evaluates statements against a catalog and a store.
//
// Query operators are iterators in the volcano style: each pulls rows from the
// one below it, so a SELECT with a LIMIT stops the scan early instead of
// materializing the table and throwing most of it away. The two operators that
// must materialize -- sort and distinct -- say so.
package exec

import (
	"fmt"
	"strings"

	"github.com/pedrovitti/quarry/internal/catalog"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// Error is a runtime error from planning or executing a statement.
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func errorf(format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// ColumnInfo describes one column of an operator's output. Table is the name a
// qualifier must use to reach it -- an alias where one was given -- and is
// empty for computed columns, which cannot be qualified.
type ColumnInfo struct {
	Table string
	Name  string
	Kind  types.Kind
}

// Label is the heading this column gets in output.
func (c ColumnInfo) Label() string { return c.Name }

// Binding is the shape of an operator's rows: what each position holds and
// what names reach it.
type Binding struct {
	columns []ColumnInfo
	// outer is the scope a subquery was compiled inside, and cell holds that
	// scope's current row while the subquery runs. Both are nil for a query
	// that stands on its own.
	outer  *Binding
	cell   *rowCell
	engine *Engine
}

// rowCell carries one query's current row to the subqueries compiled inside
// it. A subquery is evaluated with the row of the query around it, so it
// stores that row here before running, and a column resolved to the outer
// scope reads it back.
type rowCell struct {
	row storage.Row
}

// NewBinding returns a binding over the given columns.
func NewBinding(columns []ColumnInfo) *Binding {
	out := make([]ColumnInfo, len(columns))
	copy(out, columns)
	return &Binding{columns: out}
}

// BindingFor returns the binding of a base table scanned under a given name.
func BindingFor(as string, schema *catalog.Schema) *Binding {
	columns := make([]ColumnInfo, schema.Len())
	for i, column := range schema.Columns() {
		columns[i] = ColumnInfo{Table: as, Name: column.Name, Kind: column.Kind()}
	}
	return &Binding{columns: columns}
}

// Len is the number of columns.
func (b *Binding) Len() int { return len(b.columns) }

// Columns returns the column descriptions in order.
func (b *Binding) Columns() []ColumnInfo {
	out := make([]ColumnInfo, len(b.columns))
	copy(out, b.columns)
	return out
}

// Column returns the description at an index.
func (b *Binding) Column(i int) ColumnInfo { return b.columns[i] }

// Labels returns the output headings in order.
func (b *Binding) Labels() []string {
	out := make([]string, len(b.columns))
	for i, column := range b.columns {
		out[i] = column.Label()
	}
	return out
}

// Concat returns the binding of a row that is one of these followed by one of
// those, which is what a join produces.
func (b *Binding) Concat(other *Binding) *Binding {
	columns := make([]ColumnInfo, 0, len(b.columns)+len(other.columns))
	columns = append(columns, b.columns...)
	columns = append(columns, other.columns...)
	return &Binding{columns: columns}
}

// inside returns the binding a subquery of this one compiles against: its own
// columns first, then this scope, then whatever this scope could already see.
func (b *Binding) inside(inner *Binding) *Binding {
	if b.cell == nil {
		b.cell = &rowCell{}
	}
	out := &Binding{columns: inner.columns, outer: b, engine: b.engine}
	return out
}

// resolveScope finds a column reference in this binding or, failing that, in
// the scopes around it. The binding it was found in comes back with it,
// because a column of an enclosing query is read from that query's cell
// rather than from the row being evaluated.
func (b *Binding) resolveScope(table, name string) (int, *Binding, error) {
	index, err := b.Resolve(table, name)
	if err == nil {
		return index, b, nil
	}
	if b.outer == nil {
		return 0, nil, err
	}
	index, owner, outerErr := b.outer.resolveScope(table, name)
	if outerErr != nil {
		// The innermost scope names the columns the reader was closest to.
		return 0, nil, err
	}
	return index, owner, nil
}

// Resolve finds the position of a column reference. An unqualified name that
// matches columns of two different tables is an error rather than a guess: in
// "a JOIN b ON a.id = b.id", a bare "id" has no defensible answer.
func (b *Binding) Resolve(table, name string) (int, error) {
	found := -1
	for i, column := range b.columns {
		if column.Name != name {
			continue
		}
		if table != "" && column.Table != table {
			continue
		}
		if found >= 0 {
			if table == "" {
				return 0, errorf("column %q is ambiguous; qualify it with a table name", name)
			}
			return 0, errorf("column %q is ambiguous", table+"."+name)
		}
		found = i
	}

	if found < 0 {
		if table != "" {
			return 0, errorf("no column %q; %s", table+"."+name, b.describe())
		}
		return 0, errorf("no column %q; %s", name, b.describe())
	}
	return found, nil
}

// ExpandStar returns the positions a star covers, in order. An unqualified
// star covers everything; a qualified one covers the columns of that table.
func (b *Binding) ExpandStar(table string) ([]int, error) {
	var out []int
	for i, column := range b.columns {
		if table == "" || column.Table == table {
			out = append(out, i)
		}
	}
	if len(out) == 0 {
		if table != "" {
			return nil, errorf("no table %q in this query", table)
		}
		return nil, errorf("this query has no columns to expand * over")
	}
	return out, nil
}

// describe lists the available columns, for the tail of a "no such column"
// message.
func (b *Binding) describe() string {
	if len(b.columns) == 0 {
		return "this query has no columns"
	}
	names := make([]string, len(b.columns))
	for i, column := range b.columns {
		if column.Table == "" {
			names[i] = column.Name
			continue
		}
		names[i] = column.Table + "." + column.Name
	}
	return "available columns are " + strings.Join(names, ", ")
}
