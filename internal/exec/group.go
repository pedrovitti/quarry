package exec

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// grouping is the plan of a summarized query: the keys rows are gathered by,
// the aggregates folded over each gathering, and the shape of the rows that
// come out.
//
// Everything above a grouping is written against its output, so the select
// list, HAVING and ORDER BY are rewritten to name the columns below before
// they are compiled.
type grouping struct {
	source  *Binding
	keys    []Expr
	labels  []string
	aggs    []aggregate
	binding *Binding
	params  []types.Value
}

// buildGrouping plans the grouping of a select. It collects the aggregates
// used anywhere the groups are visible, since ORDER BY may name one the select
// list never mentions.
func buildGrouping(stmt *ast.Select, source *Binding, params []types.Value) (*grouping, error) {
	g := &grouping{source: source, params: params}

	for _, expr := range stmt.GroupBy {
		if err := rejectAggregates(expr, "GROUP BY"); err != nil {
			return nil, err
		}
		label := ast.Unparen(expr).String()
		if g.labelled(label) {
			continue
		}
		compiled, err := Compile(expr, source, params)
		if err != nil {
			return nil, err
		}
		g.keys = append(g.keys, compiled)
		g.labels = append(g.labels, label)
	}

	for _, item := range stmt.Columns {
		if err := g.collect(item.Expr); err != nil {
			return nil, err
		}
	}
	if err := g.collect(stmt.Having); err != nil {
		return nil, err
	}
	for _, item := range stmt.OrderBy {
		if err := g.collect(item.Expr); err != nil {
			return nil, err
		}
	}

	columns := make([]ColumnInfo, 0, len(g.keys)+len(g.aggs))
	for i, key := range g.keys {
		columns = append(columns, ColumnInfo{Name: g.labels[i], Kind: key.Kind()})
	}
	for _, agg := range g.aggs {
		columns = append(columns, ColumnInfo{Name: agg.label, Kind: agg.kind})
	}
	g.binding = NewBinding(columns)
	return g, nil
}

// labelled reports whether a key with this printed form is already held, so
// that "GROUP BY a, a" gathers by one key rather than two.
func (g *grouping) labelled(label string) bool {
	for _, held := range g.labels {
		if held == label {
			return true
		}
	}
	return false
}

// collect plans every aggregate call in a tree, skipping the ones already
// planned so that a call written twice is folded once.
func (g *grouping) collect(n ast.Node) error {
	if n == nil {
		return nil
	}

	var err error
	ast.Walk(n, func(node ast.Node) bool {
		if err != nil {
			return false
		}
		call, ok := node.(*ast.FuncCall)
		if !ok || !IsAggregate(call) {
			return true
		}
		if !g.planned(call.String()) {
			var planned aggregate
			planned, err = planAggregate(call, g.source, g.params)
			if err != nil {
				return false
			}
			g.aggs = append(g.aggs, planned)
		}
		return false
	})
	return err
}

func (g *grouping) planned(label string) bool {
	for _, agg := range g.aggs {
		if agg.label == label {
			return true
		}
	}
	return false
}

// rewrite replaces every aggregate call and every grouped expression with a
// reference to the column the grouping produces for it, leaving the rest of
// the tree alone.
func (g *grouping) rewrite(expr ast.Expr) ast.Expr {
	if expr == nil {
		return nil
	}

	if call, ok := ast.Unparen(expr).(*ast.FuncCall); ok && IsAggregate(call) {
		return &ast.ColumnRef{Name: call.String(), Start: expr.Pos()}
	}
	if g.labelled(ast.Unparen(expr).String()) {
		return &ast.ColumnRef{Name: ast.Unparen(expr).String(), Start: expr.Pos()}
	}

	switch node := expr.(type) {
	case *ast.ParenExpr:
		return &ast.ParenExpr{Inner: g.rewrite(node.Inner), Start: node.Start}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Op: node.Op, Operand: g.rewrite(node.Operand), Start: node.Start}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Op:    node.Op,
			Left:  g.rewrite(node.Left),
			Right: g.rewrite(node.Right),
		}
	case *ast.IsNullExpr:
		return &ast.IsNullExpr{Operand: g.rewrite(node.Operand), Negated: node.Negated}
	case *ast.FuncCall:
		args := make([]ast.Expr, len(node.Args))
		for i, arg := range node.Args {
			args[i] = g.rewrite(arg)
		}
		return &ast.FuncCall{Name: node.Name, Args: args, Start: node.Start}
	}
	return expr
}

// compile rewrites an expression and compiles it against the grouped rows. A
// column that survived the rewrite belongs to neither a group nor an
// aggregate, and so has no single value to show.
func (g *grouping) compile(expr ast.Expr) (ast.Expr, Expr, error) {
	rewritten := g.rewrite(expr)
	if err := g.checkGrouped(rewritten); err != nil {
		return nil, nil, err
	}
	compiled, err := Compile(rewritten, g.binding, g.params)
	if err != nil {
		return nil, nil, err
	}
	return rewritten, compiled, nil
}

// checkGrouped reports the first column left ungathered. A name the query
// never had is reported as the unknown column it is, rather than as one that
// should have been grouped.
func (g *grouping) checkGrouped(rewritten ast.Expr) error {
	for _, ref := range ast.ColumnRefs(rewritten) {
		if _, err := g.binding.Resolve(ref.Table, ref.Name); err == nil {
			continue
		}
		if _, err := g.source.Resolve(ref.Table, ref.Name); err != nil {
			return err
		}
		return errorf("column %q must appear in GROUP BY or be used in an aggregate", ref.Name)
	}
	return nil
}

// groupOp gathers its input into groups and emits one row for each: the key
// values it was gathered by, followed by the aggregate results.
//
// It has to read everything before it can emit anything, which is the cost of
// summarizing. Groups leave in the order they were first seen, so the output
// of a query does not depend on how a map happened to store them.
type groupOp struct {
	input   Rows
	keys    []Expr
	aggs    []aggregate
	binding *Binding
	whole   bool

	rows []storage.Row
	next int
	done bool
}

func (o *groupOp) Binding() *Binding { return o.binding }

func (o *groupOp) Next() (storage.Row, bool, error) {
	if !o.done {
		if err := o.materialize(); err != nil {
			return nil, false, err
		}
		o.done = true
	}
	if o.next >= len(o.rows) {
		return nil, false, nil
	}
	row := o.rows[o.next]
	o.next++
	return row, true, nil
}

func (o *groupOp) materialize() error {
	seen := make(map[string]int)
	var order [][]types.Value
	var held [][]accumulator

	for {
		row, ok, err := o.input.Next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}

		key := make([]types.Value, len(o.keys))
		for i, expr := range o.keys {
			v, err := expr.Eval(row)
			if err != nil {
				return err
			}
			key[i] = v
		}

		// Two NULL keys are one group, the way DISTINCT counts two NULL rows
		// as one, and the same encoding is what says so.
		encoded := string(storage.EncodeRow(nil, key))
		index, ok := seen[encoded]
		if !ok {
			index = len(order)
			seen[encoded] = index
			order = append(order, key)
			held = append(held, o.fresh())
		}

		for i, agg := range o.aggs {
			v, err := agg.arg.Eval(row)
			if err != nil {
				return err
			}
			if err := held[index][i].add(v); err != nil {
				return err
			}
		}
	}

	// With no GROUP BY the whole input is one group, and it answers even when
	// there was no input at all: counting nothing is 0, not no rows.
	if len(order) == 0 && o.whole {
		order = append(order, nil)
		held = append(held, o.fresh())
	}

	o.rows = make([]storage.Row, len(order))
	for i, key := range order {
		row := make(storage.Row, 0, len(key)+len(o.aggs))
		row = append(row, key...)
		for _, acc := range held[i] {
			row = append(row, acc.result())
		}
		o.rows[i] = row
	}
	return nil
}

func (o *groupOp) fresh() []accumulator {
	accs := make([]accumulator, len(o.aggs))
	for i, agg := range o.aggs {
		accs[i] = agg.start()
	}
	return accs
}
