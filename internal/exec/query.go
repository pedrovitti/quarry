package exec

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/catalog"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// noLimit is the limit of a query that has none.
const noLimit int64 = -1

// buildSelect assembles the operator tree for a SELECT.
//
// The operators are stacked in the order SQL evaluates them, which is not the
// order the clauses are written: FROM, then WHERE, then the select list, then
// DISTINCT, then ORDER BY, then LIMIT. Sorting after projection is what lets
// "ORDER BY" name an output alias.
func (e *Engine) buildSelect(stmt *ast.Select, params []types.Value) (Rows, error) {
	input, err := e.buildFrom(stmt.From, params)
	if err != nil {
		return nil, err
	}

	if stmt.Where != nil {
		predicate, err := CompilePredicate(stmt.Where, input.Binding(), params)
		if err != nil {
			return nil, err
		}
		input = &filterOp{input: input, predicate: predicate}
	}

	source := input
	exprs, columns, err := e.buildProjection(stmt.Columns, source.Binding(), params)
	if err != nil {
		return nil, err
	}
	visible := len(columns)

	// ORDER BY may name a column that is not selected: "SELECT name FROM t
	// ORDER BY city" is legal. Such a term is projected as an extra hidden
	// column, sorted on, and trimmed off at the end.
	keys, exprs, columns, err := resolveOrderBy(stmt, exprs, columns, source.Binding(), params)
	if err != nil {
		return nil, err
	}

	input = &projectOp{input: source, exprs: exprs, binding: NewBinding(columns)}

	if stmt.Distinct {
		input = &distinctOp{input: input}
	}
	if len(keys) > 0 {
		input = &sortOp{input: input, keys: keys}
	}

	input, err = buildLimit(stmt, input, params)
	if err != nil {
		return nil, err
	}

	if len(columns) > visible {
		input = &trimOp{input: input, keep: visible, binding: NewBinding(columns[:visible])}
	}
	return input, nil
}

// resolveOrderBy compiles the sort terms, appending a hidden projected column
// for any term that names something outside the select list.
func resolveOrderBy(
	stmt *ast.Select,
	exprs []Expr,
	columns []ColumnInfo,
	source *Binding,
	params []types.Value,
) ([]sortKey, []Expr, []ColumnInfo, error) {
	if len(stmt.OrderBy) == 0 {
		return nil, exprs, columns, nil
	}

	keys := make([]sortKey, 0, len(stmt.OrderBy))
	for _, item := range stmt.OrderBy {
		// Output names win, so ORDER BY can use an alias the select list
		// introduced.
		projected := NewBinding(columns)
		if compiled, err := Compile(item.Expr, projected, params); err == nil {
			keys = append(keys, sortKey{expr: compiled, desc: item.Desc})
			continue
		}

		compiled, err := Compile(item.Expr, source, params)
		if err != nil {
			return nil, nil, nil, err
		}
		if stmt.Distinct {
			return nil, nil, nil, errorf(
				"ORDER BY %s must appear in the select list when DISTINCT is used", item.Expr)
		}

		exprs = append(exprs, compiled)
		columns = append(columns, ColumnInfo{Name: item.Expr.String(), Kind: compiled.Kind()})
		keys = append(keys, sortKey{
			expr: columnExpr{index: len(columns) - 1, kind: compiled.Kind()},
			desc: item.Desc,
		})
	}
	return keys, exprs, columns, nil
}

// buildFrom turns a FROM clause into a stream. A missing FROM yields one
// empty row, so "SELECT 1" projects over it and returns a single value.
func (e *Engine) buildFrom(from ast.TableRef, params []types.Value) (Rows, error) {
	if from == nil {
		return newSingleRow(), nil
	}

	switch node := from.(type) {
	case *ast.TableName:
		table, err := e.catalog.Get(node.Name)
		if err != nil {
			return nil, errorf("%s", err)
		}
		heap, err := e.store.Get(node.Name)
		if err != nil {
			return nil, errorf("%s", err)
		}
		return newScan(heap, BindingFor(node.Ref(), table.Schema)), nil

	case *ast.Join:
		left, err := e.buildFrom(node.Left, params)
		if err != nil {
			return nil, err
		}
		right, err := e.buildFrom(node.Right, params)
		if err != nil {
			return nil, err
		}

		// The inner side is read once and replayed for each outer row, so a
		// join costs one scan of the right table rather than one per row.
		buffered, err := Collect(right)
		if err != nil {
			return nil, err
		}

		binding := left.Binding().Concat(right.Binding())
		predicate, err := CompilePredicate(node.On, binding, params)
		if err != nil {
			return nil, err
		}
		return &nestedLoopOp{left: left, right: buffered, predicate: predicate, binding: binding}, nil
	}
	return nil, errorf("cannot read from this kind of source")
}

// buildProjection compiles the select list, expanding stars against the input
// binding. It returns one expression and one column description per output
// column, which the caller stacks a projectOp on.
func (e *Engine) buildProjection(
	items []ast.SelectItem,
	binding *Binding,
	params []types.Value,
) ([]Expr, []ColumnInfo, error) {
	var exprs []Expr
	var columns []ColumnInfo

	for _, item := range items {
		if star, ok := item.Expr.(*ast.Star); ok {
			indexes, err := binding.ExpandStar(star.Table)
			if err != nil {
				return nil, nil, err
			}
			for _, index := range indexes {
				info := binding.Column(index)
				exprs = append(exprs, columnExpr{index: index, kind: info.Kind})
				columns = append(columns, info)
			}
			continue
		}

		compiled, err := Compile(item.Expr, binding, params)
		if err != nil {
			return nil, nil, err
		}
		exprs = append(exprs, compiled)
		columns = append(columns, projectedColumn(item, compiled, binding))
	}
	return exprs, columns, nil
}

// projectedColumn decides what an output column is called and whether it can
// still be qualified. A bare column reference keeps its table, so ORDER BY may
// still say "t.a"; anything computed loses it, because there is no table it
// belongs to.
func projectedColumn(item ast.SelectItem, compiled Expr, binding *Binding) ColumnInfo {
	info := ColumnInfo{Name: item.Alias, Kind: compiled.Kind()}

	if ref, ok := ast.Unparen(item.Expr).(*ast.ColumnRef); ok {
		if item.Alias == "" {
			info.Name = ref.Name
			info.Table = ref.Table
		}
		// An aliased column keeps its qualifier so both names reach it.
		if item.Alias != "" {
			info.Table = ref.Table
		}
		return info
	}

	if info.Name == "" {
		// Unaliased computed columns are labelled with the expression, which
		// is what a user reading the output would call them anyway.
		info.Name = item.Expr.String()
	}
	return info
}

// buildLimit applies LIMIT and OFFSET, which must be constant non-negative
// integers -- they are evaluated once, against no row.
func buildLimit(stmt *ast.Select, input Rows, params []types.Value) (Rows, error) {
	if stmt.Limit == nil && stmt.Offset == nil {
		return input, nil
	}

	limit, err := constantCount(stmt.Limit, "LIMIT", params)
	if err != nil {
		return nil, err
	}
	offset, err := constantCount(stmt.Offset, "OFFSET", params)
	if err != nil {
		return nil, err
	}
	if stmt.Limit == nil {
		limit = noLimit
	}
	if stmt.Offset == nil {
		offset = 0
	}
	return &limitOp{input: input, limit: limit, offset: offset}, nil
}

// constantCount evaluates a LIMIT or OFFSET expression.
func constantCount(expr ast.Expr, clause string, params []types.Value) (int64, error) {
	if expr == nil {
		return 0, nil
	}
	compiled, err := Compile(expr, NewBinding(nil), params)
	if err != nil {
		return 0, err
	}
	v, err := compiled.Eval(storage.Row{})
	if err != nil {
		return 0, err
	}
	if v.IsNull() {
		return 0, errorf("%s must not be NULL", clause)
	}
	n, ok := v.Int()
	if !ok {
		return 0, errorf("%s needs an integer, got %s", clause, v.Kind())
	}
	if n < 0 {
		return 0, errorf("%s must not be negative", clause)
	}
	return n, nil
}

// bindingForTable is the binding of a table addressed by its own name, which
// is what INSERT, UPDATE and DELETE resolve against.
func bindingForTable(table *catalog.Table) *Binding {
	return BindingFor(table.Name, table.Schema)
}
