package types

import (
	"math"

	"github.com/pedrovitti/quarry/internal/ast"
)

// Apply evaluates a binary operator over two values.
//
// NULL propagates through every operator here: if either operand is NULL the
// result is NULL, without the operator being consulted. Comparisons go through
// the three-valued path and come back as a boolean value, so "1 = NULL" is
// NULL rather than false.
func Apply(op ast.BinaryOp, a, b Value) (Value, error) {
	switch {
	case op.IsComparison():
		return compareOp(op, a, b), nil
	case op.IsLogical():
		return logicalOp(op, a, b)
	}

	if a.IsNull() || b.IsNull() {
		return Null(), nil
	}
	if op == ast.Concat {
		return concat(a, b)
	}
	return arithmetic(op, a, b)
}

// ApplyUnary evaluates a prefix operator.
func ApplyUnary(op ast.UnaryOp, v Value) (Value, error) {
	switch op {
	case ast.LogicalNot:
		truth, ok := Truth(v)
		if !ok {
			return Null(), errorf("NOT needs a boolean, got %s", v.kind)
		}
		return truth.Not().Value(), nil

	case ast.Negate:
		if v.IsNull() {
			return Null(), nil
		}
		switch v.kind {
		case KindInt:
			return Int(-v.i), nil
		case KindFloat:
			return Float(-v.f), nil
		}
		return Null(), errorf("cannot negate a %s value", v.kind)

	case ast.Plus:
		if v.IsNull() || v.kind.IsNumeric() {
			return v, nil
		}
		return Null(), errorf("unary + needs a number, got %s", v.kind)
	}
	return Null(), errorf("unknown unary operator")
}

// compareOp turns a comparison into a boolean value, or NULL for unknown.
func compareOp(op ast.BinaryOp, a, b Value) Value {
	cmp, ok := Compare(a, b)
	if !ok {
		return Null()
	}
	switch op {
	case ast.Equal:
		return Bool(cmp == 0)
	case ast.NotEqual:
		return Bool(cmp != 0)
	case ast.LessThan:
		return Bool(cmp < 0)
	case ast.LessOrEqual:
		return Bool(cmp <= 0)
	case ast.GreaterThan:
		return Bool(cmp > 0)
	case ast.GreaterOrEqual:
		return Bool(cmp >= 0)
	}
	return Null()
}

// logicalOp evaluates AND and OR. Unlike arithmetic, these do not simply
// propagate NULL: "false AND NULL" is false, because no value of the unknown
// side could make it true.
func logicalOp(op ast.BinaryOp, a, b Value) (Value, error) {
	left, ok := Truth(a)
	if !ok {
		return Null(), errorf("%s needs a boolean, got %s", op, a.kind)
	}
	right, ok := Truth(b)
	if !ok {
		return Null(), errorf("%s needs a boolean, got %s", op, b.kind)
	}

	if op == ast.LogicalAnd {
		return left.And(right).Value(), nil
	}
	return left.Or(right).Value(), nil
}

func concat(a, b Value) (Value, error) {
	left, ok := a.Text()
	if !ok {
		return Null(), errorf("|| needs text, got %s", a.kind)
	}
	right, ok := b.Text()
	if !ok {
		return Null(), errorf("|| needs text, got %s", b.kind)
	}
	return Text(left + right), nil
}

func arithmetic(op ast.BinaryOp, a, b Value) (Value, error) {
	if !a.kind.IsNumeric() || !b.kind.IsNumeric() {
		return Null(), errorf("cannot apply %s to %s and %s", op, a.kind, b.kind)
	}

	// Integer arithmetic stays integral; anything touching a float widens.
	if a.kind == KindInt && b.kind == KindInt {
		return intArithmetic(op, a.i, b.i)
	}
	x, _ := a.AsFloat()
	y, _ := b.AsFloat()
	return floatArithmetic(op, x, y)
}

func intArithmetic(op ast.BinaryOp, a, b int64) (Value, error) {
	switch op {
	case ast.Add:
		// Checked before the fact rather than after: negating b to reuse the
		// addition check would itself overflow when b is MinInt64.
		if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
			return Null(), errorf("integer overflow in %d + %d", a, b)
		}
		return Int(a + b), nil

	case ast.Sub:
		if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
			return Null(), errorf("integer overflow in %d - %d", a, b)
		}
		return Int(a - b), nil

	case ast.Mul:
		product := a * b
		if a != 0 && (product/a != b || (a == -1 && b == math.MinInt64)) {
			return Null(), errorf("integer overflow in %d * %d", a, b)
		}
		return Int(product), nil

	case ast.Div:
		if b == 0 {
			return Null(), errorf("division by zero")
		}
		if a == math.MinInt64 && b == -1 {
			return Null(), errorf("integer overflow in %d / %d", a, b)
		}
		return Int(a / b), nil

	case ast.Mod:
		if b == 0 {
			return Null(), errorf("division by zero")
		}
		if b == -1 {
			// MinInt64 % -1 traps on some architectures even though the
			// answer is 0.
			return Int(0), nil
		}
		return Int(a % b), nil
	}
	return Null(), errorf("unknown operator %s", op)
}

func floatArithmetic(op ast.BinaryOp, a, b float64) (Value, error) {
	switch op {
	case ast.Add:
		return Float(a + b), nil
	case ast.Sub:
		return Float(a - b), nil
	case ast.Mul:
		return Float(a * b), nil
	case ast.Div:
		if b == 0 {
			return Null(), errorf("division by zero")
		}
		return Float(a / b), nil
	case ast.Mod:
		if b == 0 {
			return Null(), errorf("division by zero")
		}
		return Float(math.Mod(a, b)), nil
	}
	return Null(), errorf("unknown operator %s", op)
}
