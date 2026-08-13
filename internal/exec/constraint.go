package exec

import (
	"strings"

	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/catalog"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// constraints is a table's rules compiled against its own columns: the
// conditions as expressions that can be evaluated per row, and the keys as
// column positions rather than names.
//
// Compiling once per statement rather than once per row is the same split the
// rest of the executor makes: an unknown column in a condition belongs to the
// table, and a duplicate belongs to the row.
type constraints struct {
	table  string
	checks []check
	keys   []key
}

// check is a condition a row is allowed to satisfy or leave unknown.
type check struct {
	expr Expr
}

// key is a constraint rows have to differ on, resolved to positions.
type key struct {
	constraint catalog.Constraint
	positions  []int
}

// compileConstraints resolves everything a schema says about its rows. It is
// what refuses a condition that cannot hold for any row -- one naming a column
// the table has not got, or one that is not a condition at all -- and it runs
// when the table is created as well as when a statement writes to it.
func compileConstraints(table *catalog.Table) (*constraints, error) {
	binding := bindingForTable(table)
	compiled := &constraints{table: table.Name}

	for _, constraint := range table.Schema.Constraints() {
		if constraint.Kind == ast.CheckConstraint {
			expr, err := compileCheck(constraint.Expr, binding)
			if err != nil {
				return nil, err
			}
			compiled.checks = append(compiled.checks, check{expr: expr})
			continue
		}

		positions := make([]int, len(constraint.Columns))
		for i, name := range constraint.Columns {
			index, ok := table.Schema.Index(name)
			if !ok {
				return nil, errorf("table %s has no column %q", table.Name, name)
			}
			positions[i] = index
		}
		compiled.keys = append(compiled.keys, key{constraint: constraint, positions: positions})
	}
	return compiled, nil
}

// compileCheck compiles a condition and refuses one whose kind is known and is
// not a truth value, the way an operator applied to the wrong kinds is refused
// before any row is read.
func compileCheck(condition ast.Expr, binding *Binding) (Expr, error) {
	expr, err := Compile(condition, binding, nil)
	if err != nil {
		return nil, err
	}
	if kind := expr.Kind(); kind != types.KindNull && kind != types.KindBool {
		return nil, errorf("a CHECK must be a condition, got %s", kind)
	}
	return expr, nil
}

// enforce reports what stops rows from being written. The rows are the ones a
// statement wants the table to hold; replaced names the rows they are taking
// the place of, which an UPDATE fills in and an INSERT leaves empty.
//
// Nothing here writes anything: a statement checks every row it built before
// storing any of them, so a violation leaves the table as it was.
func (c *constraints) enforce(heap *storage.Heap, rows []storage.Row, replaced map[storage.RowID]bool) error {
	for _, row := range rows {
		for _, condition := range c.checks {
			held, err := condition.holds(row)
			if err != nil {
				return err
			}
			if !held {
				return errorf("CHECK constraint on %q failed", c.table)
			}
		}
	}
	return c.enforceKeys(heap, rows, replaced)
}

// holds reports whether a row is allowed by the condition. Only a condition
// that is definitely false rejects one: a constraint is a proof of guilt,
// where WHERE is a proof of innocence, so unknown falls the other way here.
func (c check) holds(row storage.Row) (bool, error) {
	v, err := c.expr.Eval(row)
	if err != nil {
		return false, err
	}
	truth, ok := types.Truth(v)
	if !ok {
		return false, errorf("a %s is not a condition; expected true or false", v.Kind())
	}
	return truth != types.False, nil
}

func (c *constraints) enforceKeys(heap *storage.Heap, rows []storage.Row, replaced map[storage.RowID]bool) error {
	for _, k := range c.keys {
		taken := make(map[string]bool)

		cursor := heap.Scan()
		for cursor.Next() {
			if replaced[cursor.RowID()] {
				continue
			}
			if encoded, ok := k.encode(cursor.Row()); ok {
				taken[encoded] = true
			}
		}

		// The rows of one statement are keyed against each other as well as
		// against the table, so two new rows sharing a key collide even when
		// the table is empty.
		for _, row := range rows {
			encoded, ok := k.encode(row)
			if !ok {
				continue
			}
			if taken[encoded] {
				return errorf("duplicate value %s for %s", k.render(row), k.constraint)
			}
			taken[encoded] = true
		}
	}
	return nil
}

// encode builds the comparable form of a row's key, reporting false for a row
// the constraint does not reach. A NULL is not equal to anything, itself
// included, so a key holding one collides with nothing -- which is why a
// unique column takes as many NULLs as it is given. A primary key has no such
// rows, because its columns are NOT NULL.
func (k key) encode(row storage.Row) (string, bool) {
	values := make(storage.Row, len(k.positions))
	for i, position := range k.positions {
		if row[position].IsNull() {
			return "", false
		}
		values[i] = row[position]
	}
	return string(storage.EncodeRow(nil, values)), true
}

// render writes a row's key the way a literal would be written, for the error
// that reports it as already taken.
func (k key) render(row storage.Row) string {
	parts := make([]string, len(k.positions))
	for i, position := range k.positions {
		parts[i] = row[position].SQL()
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// createTable settles everything about a table that does not depend on a row:
// a default is reduced to the value it produced, and every condition is
// compiled against the columns it will read. A table that reaches the catalog
// is one whose rules are known to be enforceable.
func (e *Engine) createTable(stmt *ast.CreateTable) (*catalog.Table, error) {
	columns := make([]catalog.Column, len(stmt.Columns))
	for i, def := range stmt.Columns {
		columns[i] = catalog.ColumnFromDef(def)
		if def.Default == nil {
			continue
		}
		value, err := reduceDefault(def.Default, columns[i])
		if err != nil {
			return nil, err
		}
		columns[i].Default, columns[i].HasDefault = value, true
	}

	table, err := e.catalog.CreateWith(stmt.Name, columns, catalog.ConstraintsFromStmt(stmt))
	if err != nil {
		return nil, errorf("%s", err)
	}
	if _, err := compileConstraints(table); err != nil {
		// A table whose conditions do not compile is worse than no table: it
		// would accept rows nobody could ever describe the rules for.
		_ = e.catalog.Drop(stmt.Name)
		return nil, err
	}
	return table, nil
}

// reduceDefault works out what a column with a default holds in a row that
// does not name it. The expression is compiled against an empty binding, so a
// default that reads a column is refused here: there is no row for it to read
// from, and there never will be at the moment it is needed.
func reduceDefault(expr ast.Expr, column catalog.Column) (types.Value, error) {
	compiled, err := Compile(expr, NewBinding(nil), nil)
	if err != nil {
		return types.Null(), err
	}
	value, err := compiled.Eval(storage.Row{})
	if err != nil {
		return types.Null(), err
	}
	converted, err := types.Convert(value, column.Kind())
	if err != nil {
		return types.Null(), errorf("column %q: %s", column.Name, err)
	}
	return converted, nil
}

// defaultRow is the row a statement starts from before its own values are
// written into it: the defaults of every column it does not name, and NULL
// everywhere else.
func defaultRow(schema *catalog.Schema, targets []int) storage.Row {
	named := make([]bool, schema.Len())
	for _, target := range targets {
		named[target] = true
	}

	row := make(storage.Row, schema.Len())
	for i := range row {
		column := schema.Column(i)
		if !named[i] && column.HasDefault {
			row[i] = column.Default
		}
	}
	return row
}
