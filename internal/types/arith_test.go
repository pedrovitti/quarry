package types

import (
	"math"
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/ast"
)

func apply(t *testing.T, op ast.BinaryOp, a, b Value) Value {
	t.Helper()
	got, err := Apply(op, a, b)
	if err != nil {
		t.Fatalf("%v %v %v returned error: %v", a, op, b, err)
	}
	return got
}

func TestIntegerArithmetic(t *testing.T) {
	tests := []struct {
		op   ast.BinaryOp
		a, b int64
		want int64
	}{
		{ast.Add, 1, 2, 3},
		{ast.Add, -1, 1, 0},
		{ast.Sub, 5, 3, 2},
		{ast.Sub, 3, 5, -2},
		{ast.Mul, 3, 4, 12},
		{ast.Mul, -3, 4, -12},
		{ast.Mul, 0, 5, 0},
		{ast.Div, 7, 2, 3}, // integer division truncates
		{ast.Div, -7, 2, -3},
		{ast.Mod, 7, 3, 1},
		{ast.Mod, -7, 3, -1},
	}
	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			got := apply(t, tt.op, Int(tt.a), Int(tt.b))
			n, ok := got.Int()
			if !ok {
				t.Fatalf("result kind = %v, want KindInt", got.Kind())
			}
			if n != tt.want {
				t.Errorf("%d %v %d = %d, want %d", tt.a, tt.op, tt.b, n, tt.want)
			}
		})
	}
}

func TestArithmeticWidensToFloat(t *testing.T) {
	tests := []struct {
		name string
		a, b Value
		want float64
	}{
		{"int and float", Int(1), Float(0.5), 1.5},
		{"float and int", Float(0.5), Int(1), 1.5},
		{"float and float", Float(0.25), Float(0.25), 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := apply(t, ast.Add, tt.a, tt.b)
			f, ok := got.Float()
			if !ok {
				t.Fatalf("result kind = %v, want KindFloat", got.Kind())
			}
			if f != tt.want {
				t.Errorf("= %v, want %v", f, tt.want)
			}
		})
	}
}

func TestIntegerOverflowIsAnError(t *testing.T) {
	tests := []struct {
		name string
		op   ast.BinaryOp
		a, b int64
	}{
		{"add past max", ast.Add, math.MaxInt64, 1},
		{"add past min", ast.Add, math.MinInt64, -1},
		{"subtract past min", ast.Sub, math.MinInt64, 1},
		{"subtract past max", ast.Sub, math.MaxInt64, -1},
		{"subtract the most negative", ast.Sub, 0, math.MinInt64},
		{"multiply past max", ast.Mul, math.MaxInt64, 2},
		{"negate the most negative", ast.Div, math.MinInt64, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Apply(tt.op, Int(tt.a), Int(tt.b))
			if err == nil {
				t.Fatalf("%d %v %d succeeded, want an overflow error", tt.a, tt.op, tt.b)
			}
			if !strings.Contains(err.Error(), "overflow") {
				t.Errorf("error = %q, want it to mention overflow", err)
			}
		})
	}
}

func TestArithmeticNearTheLimitsStillWorks(t *testing.T) {
	// The overflow checks must not reject correct answers.
	tests := []struct {
		op   ast.BinaryOp
		a, b int64
		want int64
	}{
		{ast.Add, math.MaxInt64 - 1, 1, math.MaxInt64},
		{ast.Add, math.MinInt64 + 1, -1, math.MinInt64},
		{ast.Sub, math.MaxInt64, 1, math.MaxInt64 - 1},
		{ast.Sub, math.MinInt64, -1, math.MinInt64 + 1},
		{ast.Mod, math.MinInt64, -1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			got := apply(t, tt.op, Int(tt.a), Int(tt.b))
			n, _ := got.Int()
			if n != tt.want {
				t.Errorf("%d %v %d = %d, want %d", tt.a, tt.op, tt.b, n, tt.want)
			}
		})
	}
}

func TestDivisionByZero(t *testing.T) {
	for _, op := range []ast.BinaryOp{ast.Div, ast.Mod} {
		t.Run(op.String()+"_int", func(t *testing.T) {
			_, err := Apply(op, Int(1), Int(0))
			if err == nil || !strings.Contains(err.Error(), "division by zero") {
				t.Errorf("error = %v, want division by zero", err)
			}
		})
		t.Run(op.String()+"_float", func(t *testing.T) {
			_, err := Apply(op, Float(1), Float(0))
			if err == nil || !strings.Contains(err.Error(), "division by zero") {
				t.Errorf("error = %v, want division by zero", err)
			}
		})
	}
}

func TestArithmeticPropagatesNull(t *testing.T) {
	ops := []ast.BinaryOp{ast.Add, ast.Sub, ast.Mul, ast.Div, ast.Mod, ast.Concat}
	for _, op := range ops {
		t.Run(op.String(), func(t *testing.T) {
			// NULL on either side wins before the operator is consulted, so
			// even NULL / 0 is NULL rather than a division error.
			for _, pair := range [][2]Value{
				{Null(), Int(1)},
				{Int(1), Null()},
				{Null(), Null()},
				{Null(), Int(0)},
			} {
				got := apply(t, op, pair[0], pair[1])
				if !got.IsNull() {
					t.Errorf("%v %v %v = %v, want NULL", pair[0], op, pair[1], got)
				}
			}
		})
	}
}

func TestComparisonProducesBooleans(t *testing.T) {
	tests := []struct {
		op   ast.BinaryOp
		a, b Value
		want bool
	}{
		{ast.Equal, Int(1), Int(1), true},
		{ast.Equal, Int(1), Int(2), false},
		{ast.NotEqual, Int(1), Int(2), true},
		{ast.LessThan, Int(1), Int(2), true},
		{ast.LessOrEqual, Int(2), Int(2), true},
		{ast.GreaterThan, Int(2), Int(1), true},
		{ast.GreaterOrEqual, Int(1), Int(2), false},
		{ast.Equal, Text("a"), Text("a"), true},
		{ast.LessThan, Text("a"), Text("b"), true},
	}
	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			got := apply(t, tt.op, tt.a, tt.b)
			b, ok := got.Bool()
			if !ok {
				t.Fatalf("result kind = %v, want KindBool", got.Kind())
			}
			if b != tt.want {
				t.Errorf("%v %v %v = %v, want %v", tt.a, tt.op, tt.b, b, tt.want)
			}
		})
	}
}

func TestComparisonWithNullIsNull(t *testing.T) {
	ops := []ast.BinaryOp{
		ast.Equal, ast.NotEqual, ast.LessThan, ast.LessOrEqual, ast.GreaterThan, ast.GreaterOrEqual,
	}
	for _, op := range ops {
		t.Run(op.String(), func(t *testing.T) {
			for _, pair := range [][2]Value{{Null(), Int(1)}, {Int(1), Null()}, {Null(), Null()}} {
				got := apply(t, op, pair[0], pair[1])
				if !got.IsNull() {
					t.Errorf("%v %v %v = %v, want NULL", pair[0], op, pair[1], got)
				}
			}
		})
	}
}

func TestLogicalOperators(t *testing.T) {
	// AND and OR do not simply propagate NULL: false AND NULL is false.
	tests := []struct {
		name string
		op   ast.BinaryOp
		a, b Value
		want Value
	}{
		{"true and true", ast.LogicalAnd, Bool(true), Bool(true), Bool(true)},
		{"true and false", ast.LogicalAnd, Bool(true), Bool(false), Bool(false)},
		{"false and null", ast.LogicalAnd, Bool(false), Null(), Bool(false)},
		{"null and false", ast.LogicalAnd, Null(), Bool(false), Bool(false)},
		{"true and null", ast.LogicalAnd, Bool(true), Null(), Null()},
		{"null and null", ast.LogicalAnd, Null(), Null(), Null()},
		{"true or false", ast.LogicalOr, Bool(true), Bool(false), Bool(true)},
		{"true or null", ast.LogicalOr, Bool(true), Null(), Bool(true)},
		{"null or true", ast.LogicalOr, Null(), Bool(true), Bool(true)},
		{"false or null", ast.LogicalOr, Bool(false), Null(), Null()},
		{"false or false", ast.LogicalOr, Bool(false), Bool(false), Bool(false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apply(t, tt.op, tt.a, tt.b); got != tt.want {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogicalOperatorsRejectNonBooleans(t *testing.T) {
	for _, op := range []ast.BinaryOp{ast.LogicalAnd, ast.LogicalOr} {
		t.Run(op.String(), func(t *testing.T) {
			if _, err := Apply(op, Int(1), Bool(true)); err == nil {
				t.Error("an integer was accepted as a predicate")
			}
			if _, err := Apply(op, Bool(true), Text("x")); err == nil {
				t.Error("a string was accepted as a predicate")
			}
		})
	}
}

func TestConcat(t *testing.T) {
	got := apply(t, ast.Concat, Text("ab"), Text("cd"))
	if s, _ := got.Text(); s != "abcd" {
		t.Errorf("= %q, want %q", s, "abcd")
	}
	if _, err := Apply(ast.Concat, Text("a"), Int(1)); err == nil {
		t.Error("|| accepted a number")
	}
}

func TestArithmeticRejectsNonNumbers(t *testing.T) {
	for _, op := range []ast.BinaryOp{ast.Add, ast.Sub, ast.Mul, ast.Div, ast.Mod} {
		t.Run(op.String(), func(t *testing.T) {
			if _, err := Apply(op, Text("1"), Int(1)); err == nil {
				t.Error("a string was accepted as a number")
			}
			if _, err := Apply(op, Bool(true), Int(1)); err == nil {
				t.Error("a boolean was accepted as a number")
			}
		})
	}
}

func TestApplyUnary(t *testing.T) {
	tests := []struct {
		name string
		op   ast.UnaryOp
		v    Value
		want Value
	}{
		{"negate int", ast.Negate, Int(3), Int(-3)},
		{"negate negative int", ast.Negate, Int(-3), Int(3)},
		{"negate float", ast.Negate, Float(1.5), Float(-1.5)},
		{"negate null", ast.Negate, Null(), Null()},
		{"plus int", ast.Plus, Int(3), Int(3)},
		{"plus null", ast.Plus, Null(), Null()},
		{"not true", ast.LogicalNot, Bool(true), Bool(false)},
		{"not false", ast.LogicalNot, Bool(false), Bool(true)},
		{"not null", ast.LogicalNot, Null(), Null()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyUnary(tt.op, tt.v)
			if err != nil {
				t.Fatalf("returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyUnaryRejectsWrongKinds(t *testing.T) {
	if _, err := ApplyUnary(ast.Negate, Text("a")); err == nil {
		t.Error("negated a string")
	}
	if _, err := ApplyUnary(ast.Negate, Bool(true)); err == nil {
		t.Error("negated a boolean")
	}
	if _, err := ApplyUnary(ast.LogicalNot, Int(1)); err == nil {
		t.Error("NOT accepted an integer")
	}
	if _, err := ApplyUnary(ast.Plus, Text("a")); err == nil {
		t.Error("unary + accepted a string")
	}
}
