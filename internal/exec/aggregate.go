package exec

import (
	"strings"

	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/types"
)

// IsAggregate reports whether a call names an aggregate rather than a scalar.
//
// Arity is what separates the two for min and max: one argument takes the
// smallest value of a column, while two or more is the scalar that picks the
// smallest of its arguments within a single row.
func IsAggregate(call *ast.FuncCall) bool {
	switch strings.ToLower(call.Name) {
	case "count", "sum", "avg":
		return true
	case "min", "max":
		return len(call.Args) == 1
	}
	return false
}

// hasAggregate reports whether an aggregate appears anywhere in a tree.
func hasAggregate(n ast.Node) bool {
	found := false
	ast.Walk(n, func(node ast.Node) bool {
		if call, ok := node.(*ast.FuncCall); ok && IsAggregate(call) {
			found = true
		}
		return !found
	})
	return found
}

// rejectAggregates refuses an aggregate in a clause that is decided before the
// groups exist. WHERE is the one users reach for, and "no column" would not
// explain why it cannot work.
func rejectAggregates(n ast.Node, clause string) error {
	if n == nil {
		return nil
	}
	if hasAggregate(n) {
		return errorf("an aggregate is not allowed in %s", clause)
	}
	return nil
}

// accumulator folds the values of one aggregate over one group.
type accumulator interface {
	add(v types.Value) error
	result() types.Value
}

// aggregate is one aggregate call in a query: the argument to feed it, the
// kind it produces, and how to start a fresh accumulator per group.
type aggregate struct {
	label string
	arg   Expr
	kind  types.Kind
	start func() accumulator
}

// planAggregate compiles one aggregate call against the pre-grouping binding.
func planAggregate(call *ast.FuncCall, binding *Binding, params []types.Value) (aggregate, error) {
	name := strings.ToLower(call.Name)

	for _, arg := range call.Args {
		if hasAggregate(arg) {
			return aggregate{}, errorf("an aggregate must not contain another aggregate")
		}
	}
	if len(call.Args) != 1 {
		return aggregate{}, errorf("%s takes 1 argument, got %d", name, len(call.Args))
	}

	planned := aggregate{label: call.String()}

	// count(*) counts rows rather than values, which is the same as counting
	// something that is never NULL.
	if _, isStar := call.Args[0].(*ast.Star); isStar {
		planned.arg = constExpr{v: types.Int(1)}
	} else {
		compiled, err := Compile(call.Args[0], binding, params)
		if err != nil {
			return aggregate{}, err
		}
		planned.arg = compiled
	}
	argKind := planned.arg.Kind()

	switch name {
	case "count":
		planned.kind = types.KindInt
		planned.start = func() accumulator { return &countAccumulator{} }

	case "sum":
		if err := needsNumber(name, argKind); err != nil {
			return aggregate{}, err
		}
		planned.kind = argKind
		planned.start = func() accumulator { return &sumAccumulator{} }

	case "avg":
		if err := needsNumber(name, argKind); err != nil {
			return aggregate{}, err
		}
		planned.kind = types.KindFloat
		planned.start = func() accumulator { return &avgAccumulator{} }

	case "min", "max":
		planned.kind = argKind
		want := 1
		if name == "min" {
			want = -1
		}
		planned.start = func() accumulator { return &extremeAccumulator{want: want} }

	default:
		return aggregate{}, errorf("no function %q", call.Name)
	}
	return planned, nil
}

// needsNumber rejects an argument no arithmetic could fold. A NULL-kinded
// argument is left alone: it produces NULL, which every aggregate may.
func needsNumber(name string, kind types.Kind) error {
	if kind == types.KindNull || kind.IsNumeric() {
		return nil
	}
	return errorf("%s needs a number, got %s", name, kind)
}

// countAccumulator counts the values it is given that are not NULL.
type countAccumulator struct {
	n int64
}

func (a *countAccumulator) add(v types.Value) error {
	if !v.IsNull() {
		a.n++
	}
	return nil
}

func (a *countAccumulator) result() types.Value { return types.Int(a.n) }

// sumAccumulator adds through the ordinary arithmetic path, so an integer sum
// stays integral, a float widens it, and an overflow is reported rather than
// wrapped.
type sumAccumulator struct {
	total types.Value
	any   bool
}

func (a *sumAccumulator) add(v types.Value) error {
	if v.IsNull() {
		return nil
	}
	if !a.any {
		a.total, a.any = v, true
		return nil
	}
	sum, err := types.Apply(ast.Add, a.total, v)
	if err != nil {
		return errorf("%s", err)
	}
	a.total = sum
	return nil
}

func (a *sumAccumulator) result() types.Value {
	if !a.any {
		return types.Null()
	}
	return a.total
}

// avgAccumulator divides in floating point whatever it was given, so the mean
// of two integers can land between them.
type avgAccumulator struct {
	total float64
	n     int64
}

func (a *avgAccumulator) add(v types.Value) error {
	if v.IsNull() {
		return nil
	}
	f, ok := v.AsFloat()
	if !ok {
		return errorf("avg needs a number, got %s", v.Kind())
	}
	a.total += f
	a.n++
	return nil
}

func (a *avgAccumulator) result() types.Value {
	if a.n == 0 {
		return types.Null()
	}
	return types.Float(a.total / float64(a.n))
}

// extremeAccumulator keeps the smallest or largest value it has seen, ordered
// by comparison rather than by sort order: NULL is skipped, not ranked.
type extremeAccumulator struct {
	best types.Value
	want int
	any  bool
}

func (a *extremeAccumulator) add(v types.Value) error {
	if v.IsNull() {
		return nil
	}
	if !a.any {
		a.best, a.any = v, true
		return nil
	}
	cmp, ok := types.Compare(v, a.best)
	if !ok {
		return errorf("cannot compare %s with %s", v.Kind(), a.best.Kind())
	}
	if cmp == a.want {
		a.best = v
	}
	return nil
}

func (a *extremeAccumulator) result() types.Value {
	if !a.any {
		return types.Null()
	}
	return a.best
}
