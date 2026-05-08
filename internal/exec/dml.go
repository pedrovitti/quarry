package exec

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/catalog"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

func (e *Engine) runInsert(stmt *ast.Insert, params []types.Value) (*Result, error) {
	table, err := e.catalog.Get(stmt.Table)
	if err != nil {
		return nil, errorf("%s", err)
	}
	heap, err := e.store.Get(stmt.Table)
	if err != nil {
		return nil, errorf("%s", err)
	}

	targets, err := insertTargets(stmt, table.Schema)
	if err != nil {
		return nil, err
	}

	// Values are compiled against an empty binding: an INSERT has no row to
	// read columns from, so "INSERT INTO t VALUES (a)" is a missing-column
	// error rather than a reference to the row being written.
	empty := NewBinding(nil)

	// Every row is built before any is stored, so a bad value in the last row
	// does not leave the earlier ones written.
	built := make([]storage.Row, 0, len(stmt.Rows))
	for _, values := range stmt.Rows {
		row, err := e.buildInsertRow(values, targets, table.Schema, empty, params)
		if err != nil {
			return nil, err
		}
		built = append(built, row)
	}

	for _, row := range built {
		heap.Insert(row)
	}
	return &Result{Tag: "INSERT", Affected: len(built)}, nil
}

// insertTargets returns the column position each value in a row maps to. With
// no column list, values map to the schema in declaration order.
func insertTargets(stmt *ast.Insert, schema *catalog.Schema) ([]int, error) {
	if len(stmt.Columns) == 0 {
		targets := make([]int, schema.Len())
		for i := range targets {
			targets[i] = i
		}
		for _, row := range stmt.Rows {
			if len(row) != schema.Len() {
				return nil, errorf("table %s has %d columns but %d values were given",
					stmt.Table, schema.Len(), len(row))
			}
		}
		return targets, nil
	}

	targets := make([]int, len(stmt.Columns))
	seen := make(map[int]bool, len(stmt.Columns))
	for i, name := range stmt.Columns {
		index, ok := schema.Index(name)
		if !ok {
			return nil, errorf("table %s has no column %q", stmt.Table, name)
		}
		if seen[index] {
			return nil, errorf("column %q is named twice", name)
		}
		seen[index] = true
		targets[i] = index
	}
	return targets, nil
}

// buildInsertRow evaluates one VALUES tuple into a full row, filling
// unmentioned columns with NULL and checking types and NOT NULL.
func (e *Engine) buildInsertRow(
	values []ast.Expr,
	targets []int,
	schema *catalog.Schema,
	binding *Binding,
	params []types.Value,
) (storage.Row, error) {
	if len(values) != len(targets) {
		return nil, errorf("%d values given for %d columns", len(values), len(targets))
	}

	// The zero Value is NULL, so a column nobody names is NULL rather than a
	// zero of its declared type.
	row := make(storage.Row, schema.Len())

	for i, expr := range values {
		compiled, err := Compile(expr, binding, params)
		if err != nil {
			return nil, err
		}
		v, err := compiled.Eval(storage.Row{})
		if err != nil {
			return nil, err
		}
		row[targets[i]] = v
	}

	if err := checkRow(row, schema); err != nil {
		return nil, err
	}
	return row, nil
}

// checkRow enforces the column types and NOT NULL, converting where SQL allows
// it -- an integer written into a FLOAT column widens.
func checkRow(row storage.Row, schema *catalog.Schema) error {
	for i := range row {
		column := schema.Column(i)

		if row[i].IsNull() {
			if column.NotNull {
				return errorf("column %q must not be NULL", column.Name)
			}
			continue
		}

		converted, err := types.Convert(row[i], column.Kind())
		if err != nil {
			return errorf("column %q: %s", column.Name, err)
		}
		row[i] = converted
	}
	return nil
}

func (e *Engine) runUpdate(stmt *ast.Update, params []types.Value) (*Result, error) {
	table, err := e.catalog.Get(stmt.Table)
	if err != nil {
		return nil, errorf("%s", err)
	}
	heap, err := e.store.Get(stmt.Table)
	if err != nil {
		return nil, errorf("%s", err)
	}
	binding := bindingForTable(table)

	assignments := make([]int, len(stmt.Set))
	exprs := make([]Expr, len(stmt.Set))
	seen := make(map[int]bool, len(stmt.Set))
	for i, set := range stmt.Set {
		index, ok := table.Schema.Index(set.Column)
		if !ok {
			return nil, errorf("table %s has no column %q", stmt.Table, set.Column)
		}
		if seen[index] {
			return nil, errorf("column %q is assigned twice", set.Column)
		}
		seen[index] = true
		assignments[i] = index

		compiled, err := Compile(set.Value, binding, params)
		if err != nil {
			return nil, err
		}
		exprs[i] = compiled
	}

	var predicate *Predicate
	if stmt.Where != nil {
		predicate, err = CompilePredicate(stmt.Where, binding, params)
		if err != nil {
			return nil, err
		}
	}

	// Matching rows are collected before any is written. Updating during the
	// scan would let a row that a previous assignment moved be visited twice.
	type pending struct {
		id  storage.RowID
		row storage.Row
	}
	var updates []pending

	cursor := heap.Scan()
	for cursor.Next() {
		row := cursor.Row()
		if predicate != nil {
			keep, err := predicate.Keep(row)
			if err != nil {
				return nil, err
			}
			if !keep {
				continue
			}
		}

		// Assignments read the row as it was before any of them ran, so
		// "SET a = b, b = a" swaps rather than copies.
		next := row.Clone()
		for i, expr := range exprs {
			v, err := expr.Eval(row)
			if err != nil {
				return nil, err
			}
			next[assignments[i]] = v
		}
		if err := checkRow(next, table.Schema); err != nil {
			return nil, err
		}
		updates = append(updates, pending{id: cursor.RowID(), row: next})
	}

	for _, update := range updates {
		if err := heap.Update(update.id, update.row); err != nil {
			return nil, errorf("%s", err)
		}
	}
	return &Result{Tag: "UPDATE", Affected: len(updates)}, nil
}

func (e *Engine) runDelete(stmt *ast.Delete, params []types.Value) (*Result, error) {
	table, err := e.catalog.Get(stmt.Table)
	if err != nil {
		return nil, errorf("%s", err)
	}
	heap, err := e.store.Get(stmt.Table)
	if err != nil {
		return nil, errorf("%s", err)
	}

	var predicate *Predicate
	if stmt.Where != nil {
		predicate, err = CompilePredicate(stmt.Where, bindingForTable(table), params)
		if err != nil {
			return nil, err
		}
	}

	// Collected first for the same reason as UPDATE: deleting during a scan
	// changes what the cursor walks.
	var doomed []storage.RowID
	cursor := heap.Scan()
	for cursor.Next() {
		if predicate != nil {
			keep, err := predicate.Keep(cursor.Row())
			if err != nil {
				return nil, err
			}
			if !keep {
				continue
			}
		}
		doomed = append(doomed, cursor.RowID())
	}

	for _, id := range doomed {
		if err := heap.Delete(id); err != nil {
			return nil, errorf("%s", err)
		}
	}
	return &Result{Tag: "DELETE", Affected: len(doomed)}, nil
}
