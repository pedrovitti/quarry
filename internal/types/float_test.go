package types

import (
	"math"
	"testing"

	"github.com/pedrovitti/quarry/internal/ast"
)

// applyFloat is Apply for the mixed and float cases, which all go through
// floatArithmetic.
func applyFloat(t *testing.T, op ast.BinaryOp, a, b Value) Value {
	t.Helper()
	got, err := Apply(op, a, b)
	if err != nil {
		t.Fatalf("%v %s %v returned error: %v", a, op, b, err)
	}
	return got
}

// Division is the one operator where widening changes the answer rather than
// just the kind: 3 / 2 is 1 and 3 / 2.0 is 1.5.
func TestDivisionWidensToFloat(t *testing.T) {
	if got := applyFloat(t, ast.Div, Int(3), Int(2)); got != Int(1) {
		t.Errorf("3 / 2 = %v, want 1", got)
	}
	for _, tt := range []struct {
		name string
		a, b Value
	}{
		{"a float on the right", Int(3), Float(2)},
		{"a float on the left", Float(3), Int(2)},
		{"floats both sides", Float(3), Float(2)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyFloat(t, ast.Div, tt.a, tt.b); got != Float(1.5) {
				t.Errorf("= %v, want 1.5", got)
			}
		})
	}
}

func TestFloatArithmetic(t *testing.T) {
	tests := []struct {
		name string
		op   ast.BinaryOp
		a, b Value
		want float64
	}{
		{"add", ast.Add, Float(1.5), Float(2.25), 3.75},
		{"subtract", ast.Sub, Float(1.5), Float(0.25), 1.25},
		{"multiply", ast.Mul, Float(1.5), Float(2), 3},
		{"divide", ast.Div, Float(3), Float(2), 1.5},
		{"modulo", ast.Mod, Float(7.5), Float(2), 1.5},
		{"modulo keeps the sign of the left", ast.Mod, Float(-7.5), Float(2), -1.5},
		{"a negative divisor", ast.Div, Float(3), Float(-2), -1.5},
		{"zero on the left divides fine", ast.Div, Float(0), Float(2), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyFloat(t, tt.op, tt.a, tt.b); got != Float(tt.want) {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFloatDivisionByZeroIsAnError(t *testing.T) {
	// Not an infinity: quarry has no value for that, so the alternative to an
	// error would be a NULL that hides the mistake.
	tests := []struct {
		name string
		op   ast.BinaryOp
		a, b Value
	}{
		{"divide by a float zero", ast.Div, Float(1), Float(0)},
		{"divide by a negative zero", ast.Div, Float(1), Float(math.Copysign(0, -1))},
		{"modulo by a float zero", ast.Mod, Float(1), Float(0)},
		{"an int divided by a float zero", ast.Div, Int(1), Float(0)},
		{"a float divided by an int zero", ast.Div, Float(1), Int(0)},
		{"zero divided by zero", ast.Div, Float(0), Float(0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Apply(tt.op, tt.a, tt.b)
			if err == nil {
				t.Fatal("succeeded, want a division by zero error")
			}
			if got, want := err.Error(), "division by zero"; got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
		})
	}
}

func TestFloatArithmeticRejectsUnknownOperators(t *testing.T) {
	// Apply routes comparisons and the logical operators elsewhere, so an
	// operator reaching the arithmetic path that is not arithmetic is a bug
	// rather than a user error - but it has to come back as an error, not a
	// wrong answer.
	if _, err := Apply(ast.InvalidBinary, Float(1), Float(2)); err == nil {
		t.Error("an invalid operator on floats succeeded")
	}
	if _, err := Apply(ast.InvalidBinary, Int(1), Int(2)); err == nil {
		t.Error("an invalid operator on integers succeeded")
	}
}

func TestFloatArithmeticOnLargeValues(t *testing.T) {
	// Floats have no overflow check, since the format has infinities of its
	// own. What matters is that they do not silently become something else.
	got, err := Apply(ast.Mul, Float(math.MaxFloat64), Float(2))
	if err != nil {
		t.Fatalf("returned error: %v", err)
	}
	f, ok := got.AsFloat()
	if !ok {
		t.Fatalf("= %v, want a float", got.Kind())
	}
	if !math.IsInf(f, 1) {
		t.Errorf("= %v, want +Inf", f)
	}
}
