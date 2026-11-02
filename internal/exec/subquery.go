package exec

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// listSource is where a list predicate reads its values. A written list holds
// expressions and evaluates them one at a time; a subquery holds a plan and
// runs it. Both stop as soon as the predicate says it has an answer.
type listSource interface {
	each(row storage.Row, visit func(types.Value) (bool, error)) error
}

// exprList is the values written out in the query text.
type exprList struct {
	items []Expr
}

func (l exprList) each(row storage.Row, visit func(types.Value) (bool, error)) error {
	for _, item := range l.items {
		value, err := item.Eval(row)
		if err != nil {
			return err
		}
		done, err := visit(value)
		if err != nil || done {
			return err
		}
	}
	return nil
}

// subquery is a compiled inner SELECT together with the scope it was compiled
// in. The plan is rebuilt for each evaluation because a correlated query
// answers differently for each row of the query around it, and because a
// stream is read only once.
type subquery struct {
	engine *Engine
	stmt   *ast.Select
	params []types.Value
	outer  *Binding
	kind   types.Kind
}

// values runs the inner query for one row of the enclosing query.
func (q *subquery) values(row storage.Row) ([]types.Value, error) {
	if q.outer != nil && q.outer.cell != nil {
		// A scope can be re-entered while it is already running, so the row
		// it held is put back rather than dropped.
		saved := q.outer.cell.row
		q.outer.cell.row = row
		defer func() { q.outer.cell.row = saved }()
	}

	rows, err := q.engine.buildSelectIn(q.stmt, q.params, q.outer)
	if err != nil {
		return nil, err
	}
	collected, err := Collect(rows)
	if err != nil {
		return nil, err
	}

	out := make([]types.Value, len(collected))
	for i, r := range collected {
		out[i] = r[0]
	}
	return out, nil
}

// subqueryList is a subquery standing where a list of values stands.
type subqueryList struct {
	q *subquery
}

func (l subqueryList) each(row storage.Row, visit func(types.Value) (bool, error)) error {
	values, err := l.q.values(row)
	if err != nil {
		return err
	}
	for _, value := range values {
		done, err := visit(value)
		if err != nil || done {
			return err
		}
	}
	return nil
}

// subqueryExpr is a subquery standing where a single value stands.
type subqueryExpr struct {
	q *subquery
}

func (e subqueryExpr) Eval(row storage.Row) (types.Value, error) {
	values, err := e.q.values(row)
	if err != nil {
		return types.Null(), err
	}
	switch len(values) {
	case 0:
		return types.Null(), nil
	case 1:
		return values[0], nil
	}
	return types.Null(), errorf("a subquery used as a value returned more than one row")
}

func (e subqueryExpr) Kind() types.Kind { return e.q.kind }

// outerColumnExpr is a column of a query that encloses the one being
// evaluated. The row it belongs to is not the row passed to Eval, so it comes
// from the cell the enclosing scope fills in before running this query.
type outerColumnExpr struct {
	cell  *rowCell
	index int
	kind  types.Kind
}

func (e outerColumnExpr) Eval(storage.Row) (types.Value, error) {
	if e.index >= len(e.cell.row) {
		return types.Null(), errorf("the enclosing row is not available here")
	}
	return e.cell.row[e.index], nil
}

func (e outerColumnExpr) Kind() types.Kind { return e.kind }

// compileSubquery plans the inner SELECT once, which settles everything that
// can be known before any row is read: the columns it names, how many it
// returns, and the kind of the one it returns.
func compileSubquery(node *ast.SubqueryExpr, binding *Binding, params []types.Value) (*subquery, error) {
	if binding == nil || binding.engine == nil {
		return nil, errorf("a subquery cannot be used here")
	}
	rows, err := binding.engine.buildSelectIn(node.Select, params, binding)
	if err != nil {
		return nil, err
	}
	inner := rows.Binding()
	if inner.Len() != 1 {
		return nil, errorf("a subquery must return one column")
	}
	return &subquery{
		engine: binding.engine,
		stmt:   node.Select,
		params: params,
		outer:  binding,
		kind:   inner.Column(0).Kind,
	}, nil
}

// asSubquery reports the subquery a list is, where the list is one.
func asSubquery(list []ast.Expr) (*ast.SubqueryExpr, bool) {
	if len(list) != 1 {
		return nil, false
	}
	node, ok := list[0].(*ast.SubqueryExpr)
	return node, ok
}

// compileList compiles the right of IN or of a quantifier, which is either a
// subquery or the values written out.
func compileList(list []ast.Expr, operand Expr, binding *Binding, params []types.Value) (listSource, error) {
	if node, ok := asSubquery(list); ok {
		q, err := compileSubquery(node, binding, params)
		if err != nil {
			return nil, err
		}
		if err := checkComparableKinds(operand.Kind(), q.kind); err != nil {
			return nil, err
		}
		return subqueryList{q: q}, nil
	}

	items := make([]Expr, len(list))
	for i, item := range list {
		compiled, err := Compile(item, binding, params)
		if err != nil {
			return nil, err
		}
		if err := checkComparableKinds(operand.Kind(), compiled.Kind()); err != nil {
			return nil, err
		}
		items[i] = compiled
	}
	return exprList{items: items}, nil
}
