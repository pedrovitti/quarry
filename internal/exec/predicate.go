package exec

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

func checkComparableKinds(left, right types.Kind) error {
	if left == types.KindNull || right == types.KindNull {
		return nil
	}
	if !types.Comparable(left, right) {
		return errorf("cannot compare %s with %s", left, right)
	}
	return nil
}

func negate(answer types.Bool3, negated bool) types.Value {
	if negated {
		return answer.Not().Value()
	}
	return answer.Value()
}

type inExpr struct {
	operand Expr
	list    listSource
	negated bool
}

func (e inExpr) Eval(row storage.Row) (types.Value, error) {
	left, err := e.operand.Eval(row)
	if err != nil {
		return types.Null(), err
	}

	answer := types.False
	matched := false
	err = e.list.each(row, func(right types.Value) (bool, error) {
		switch types.Equal(left, right) {
		case types.True:
			matched = true
			return true, nil
		case types.Unknown:
			answer = types.Unknown
		}
		return false, nil
	})
	if err != nil {
		return types.Null(), err
	}
	if matched {
		return negate(types.True, e.negated), nil
	}
	return negate(answer, e.negated), nil
}

func (e inExpr) Kind() types.Kind { return types.KindBool }

func truthOf(op ast.BinaryOp, left, right types.Value) (types.Bool3, error) {
	out, err := types.Apply(op, left, right)
	if err != nil {
		return types.Unknown, errorf("%s", err)
	}
	truth, ok := types.Truth(out)
	if !ok {
		return types.Unknown, errorf("a %s is not a condition; expected true or false", out.Kind())
	}
	return truth, nil
}

type quantifiedExpr struct {
	operand    Expr
	op         ast.BinaryOp
	quantifier ast.Quantifier
	list       listSource
}

func (e quantifiedExpr) Eval(row storage.Row) (types.Value, error) {
	left, err := e.operand.Eval(row)
	if err != nil {
		return types.Null(), err
	}

	answer := undecided(e.quantifier)
	settled := types.Unknown
	err = e.list.each(row, func(right types.Value) (bool, error) {
		truth, err := truthOf(e.op, left, right)
		if err != nil {
			return false, err
		}
		if settles(e.quantifier, truth) {
			settled = truth
			return true, nil
		}
		if truth == types.Unknown {
			answer = types.Unknown
		}
		return false, nil
	})
	if err != nil {
		return types.Null(), err
	}
	if settled != types.Unknown {
		return settled.Value(), nil
	}
	return answer.Value(), nil
}

func (e quantifiedExpr) Kind() types.Kind { return types.KindBool }

func undecided(q ast.Quantifier) types.Bool3 {
	if q == ast.AllOf {
		return types.True
	}
	return types.False
}

func settles(q ast.Quantifier, truth types.Bool3) bool {
	if q == ast.AllOf {
		return truth == types.False
	}
	return truth == types.True
}
