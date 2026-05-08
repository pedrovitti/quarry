package exec

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/fn"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// Expr is a compiled expression: resolved against a binding once, then
// evaluated per row.
//
// Compiling separates errors that belong to the query -- an unknown column, an
// unknown function -- from errors that belong to a row, such as dividing by a
// zero that happened to be in it. The first kind surfaces before any row is
// read.
type Expr interface {
	Eval(row storage.Row) (types.Value, error)
	// Kind is the type the expression produces where that is known before
	// running, and KindNull where it is not.
	Kind() types.Kind
}

// Compile resolves an expression against a binding.
func Compile(expr ast.Expr, binding *Binding, params []types.Value) (Expr, error) {
	switch node := expr.(type) {
	case *ast.Literal:
		v, err := types.FromLiteral(node)
		if err != nil {
			return nil, errorf("%s", err)
		}
		return constExpr{v: v}, nil

	case *ast.ParenExpr:
		return Compile(node.Inner, binding, params)

	case *ast.ColumnRef:
		index, err := binding.Resolve(node.Table, node.Name)
		if err != nil {
			return nil, err
		}
		return columnExpr{index: index, kind: binding.Column(index).Kind}, nil

	case *ast.Param:
		if node.Ordinal > len(params) {
			return nil, errorf("$%d has no value; %d parameters were given", node.Ordinal, len(params))
		}
		v := params[node.Ordinal-1]
		return constExpr{v: v}, nil

	case *ast.Star:
		return nil, errorf("* cannot be evaluated as a value")

	case *ast.UnaryExpr:
		operand, err := Compile(node.Operand, binding, params)
		if err != nil {
			return nil, err
		}
		return unaryExpr{op: node.Op, operand: operand}, nil

	case *ast.BinaryExpr:
		left, err := Compile(node.Left, binding, params)
		if err != nil {
			return nil, err
		}
		right, err := Compile(node.Right, binding, params)
		if err != nil {
			return nil, err
		}
		if err := checkOperandKinds(node.Op, left.Kind(), right.Kind()); err != nil {
			return nil, err
		}
		return binaryExpr{op: node.Op, left: left, right: right}, nil

	case *ast.IsNullExpr:
		operand, err := Compile(node.Operand, binding, params)
		if err != nil {
			return nil, err
		}
		return isNullExpr{operand: operand, negated: node.Negated}, nil

	case *ast.FuncCall:
		f, ok := fn.Lookup(node.Name)
		if !ok {
			return nil, errorf("no function %q", node.Name)
		}
		args := make([]Expr, len(node.Args))
		for i, arg := range node.Args {
			compiled, err := Compile(arg, binding, params)
			if err != nil {
				return nil, err
			}
			args[i] = compiled
		}
		// Arity is a property of the query, so it is checked once here rather
		// than on every row.
		if err := checkArity(f, len(args)); err != nil {
			return nil, err
		}
		return callExpr{name: node.Name, args: args}, nil
	}
	return nil, errorf("cannot evaluate this expression")
}

// checkOperandKinds rejects an operator applied to kinds it can never accept,
// before any row is read. Comparing a TEXT column with a number is a mistake
// in the query, not a property of the data, and reporting it as "no rows
// matched" would hide it.
//
// KindNull stands for "not known until it runs" as well as for NULL, so an
// operand whose kind is unknown -- a function result, a NULL literal -- is
// left alone and checked per row instead.
func checkOperandKinds(op ast.BinaryOp, left, right types.Kind) error {
	if left == types.KindNull || right == types.KindNull {
		return nil
	}

	switch {
	case op.IsComparison():
		if !types.Comparable(left, right) {
			return errorf("cannot compare %s with %s", left, right)
		}

	case op.IsLogical():
		if left != types.KindBool || right != types.KindBool {
			return errorf("%s needs booleans, got %s and %s", op, left, right)
		}

	case op == ast.Concat:
		if left != types.KindText || right != types.KindText {
			return errorf("|| needs text, got %s and %s", left, right)
		}

	default:
		if !left.IsNumeric() || !right.IsNumeric() {
			return errorf("cannot apply %s to %s and %s", op, left, right)
		}
	}
	return nil
}

func checkArity(f *fn.Func, n int) error {
	if n < f.MinArgs || (f.MaxArgs >= 0 && n > f.MaxArgs) {
		if f.MaxArgs < 0 {
			return errorf("%s takes at least %d arguments, got %d", f.Name, f.MinArgs, n)
		}
		if f.MinArgs == f.MaxArgs {
			return errorf("%s takes %d arguments, got %d", f.Name, f.MinArgs, n)
		}
		return errorf("%s takes %d to %d arguments, got %d", f.Name, f.MinArgs, f.MaxArgs, n)
	}
	return nil
}

// constExpr is a literal or a bound parameter.
type constExpr struct{ v types.Value }

func (e constExpr) Eval(storage.Row) (types.Value, error) { return e.v, nil }
func (e constExpr) Kind() types.Kind                      { return e.v.Kind() }

// columnExpr reads a position out of the row.
type columnExpr struct {
	index int
	kind  types.Kind
}

func (e columnExpr) Eval(row storage.Row) (types.Value, error) {
	if e.index >= len(row) {
		return types.Null(), errorf("row has %d columns, wanted column %d", len(row), e.index+1)
	}
	return row[e.index], nil
}

func (e columnExpr) Kind() types.Kind { return e.kind }

type unaryExpr struct {
	op      ast.UnaryOp
	operand Expr
}

func (e unaryExpr) Eval(row storage.Row) (types.Value, error) {
	v, err := e.operand.Eval(row)
	if err != nil {
		return types.Null(), err
	}
	out, err := types.ApplyUnary(e.op, v)
	if err != nil {
		return types.Null(), errorf("%s", err)
	}
	return out, nil
}

func (e unaryExpr) Kind() types.Kind {
	if e.op == ast.LogicalNot {
		return types.KindBool
	}
	return e.operand.Kind()
}

type binaryExpr struct {
	op          ast.BinaryOp
	left, right Expr
}

func (e binaryExpr) Eval(row storage.Row) (types.Value, error) {
	left, err := e.left.Eval(row)
	if err != nil {
		return types.Null(), err
	}
	right, err := e.right.Eval(row)
	if err != nil {
		return types.Null(), err
	}
	out, err := types.Apply(e.op, left, right)
	if err != nil {
		return types.Null(), errorf("%s", err)
	}
	return out, nil
}

func (e binaryExpr) Kind() types.Kind {
	if e.op.IsComparison() || e.op.IsLogical() {
		return types.KindBool
	}
	if e.op == ast.Concat {
		return types.KindText
	}
	kind, ok := types.Unify(e.left.Kind(), e.right.Kind())
	if !ok {
		return types.KindNull
	}
	return kind
}

type isNullExpr struct {
	operand Expr
	negated bool
}

// Eval never returns NULL: "NULL IS NULL" is true, which is the point.
func (e isNullExpr) Eval(row storage.Row) (types.Value, error) {
	v, err := e.operand.Eval(row)
	if err != nil {
		return types.Null(), err
	}
	return types.Bool(v.IsNull() != e.negated), nil
}

func (e isNullExpr) Kind() types.Kind { return types.KindBool }

type callExpr struct {
	name string
	args []Expr
}

func (e callExpr) Eval(row storage.Row) (types.Value, error) {
	args := make([]types.Value, len(e.args))
	for i, arg := range e.args {
		v, err := arg.Eval(row)
		if err != nil {
			return types.Null(), err
		}
		args[i] = v
	}
	out, err := fn.Call(e.name, args)
	if err != nil {
		return types.Null(), errorf("%s", err)
	}
	return out, nil
}

func (e callExpr) Kind() types.Kind { return types.KindNull }

// Predicate compiles an expression used as a filter and wraps it so callers
// read the result through SQL's three-valued rules, where unknown does not
// pass.
type Predicate struct {
	expr Expr
}

// CompilePredicate compiles a WHERE or ON expression.
func CompilePredicate(expr ast.Expr, binding *Binding, params []types.Value) (*Predicate, error) {
	compiled, err := Compile(expr, binding, params)
	if err != nil {
		return nil, err
	}
	return &Predicate{expr: compiled}, nil
}

// Keep reports whether a row passes. A predicate that evaluates to NULL is
// unknown, and unknown does not pass.
func (p *Predicate) Keep(row storage.Row) (bool, error) {
	v, err := p.expr.Eval(row)
	if err != nil {
		return false, err
	}
	truth, ok := types.Truth(v)
	if !ok {
		return false, errorf("a %s is not a condition; expected true or false", v.Kind())
	}
	return truth.IsTrue(), nil
}
