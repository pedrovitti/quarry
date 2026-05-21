package fn

import (
	"math"
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

func TestSignIsAlwaysAnInteger(t *testing.T) {
	tests := []struct {
		name string
		arg  types.Value
		want int64
	}{
		{"a positive int", types.Int(42), 1},
		{"a negative int", types.Int(-42), -1},
		{"zero", types.Int(0), 0},
		{"a positive float", types.Float(0.5), 1},
		{"a negative float", types.Float(-0.5), -1},
		{"a float zero", types.Float(0), 0},
		{"the smallest int", types.Int(math.MinInt64), -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The answer is a direction, so it is an INT even for a float
			// argument.
			if got := call(t, "sign", tt.arg); got != types.Int(tt.want) {
				t.Errorf("sign(%v) = %v, want %d", tt.arg, got, tt.want)
			}
		})
	}
}

func TestSqrt(t *testing.T) {
	tests := []struct {
		name string
		arg  types.Value
		want float64
	}{
		{"of a perfect square", types.Int(9), 3},
		{"of zero", types.Int(0), 0},
		{"of one", types.Int(1), 1},
		{"of a float", types.Float(2.25), 1.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(t, "sqrt", tt.arg); got != types.Float(tt.want) {
				t.Errorf("sqrt(%v) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestSqrtOfANegativeIsAnError(t *testing.T) {
	// NaN is not a value quarry has, so this is an error rather than a result.
	_, err := Call("sqrt", []types.Value{types.Int(-1)})
	if err == nil {
		t.Fatal("sqrt(-1) succeeded")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Errorf("error = %q", err)
	}
}

func TestPowerIsAlwaysAFloat(t *testing.T) {
	tests := []struct {
		name string
		base types.Value
		exp  types.Value
		want float64
	}{
		{"integers", types.Int(2), types.Int(10), 1024},
		{"to the zero", types.Int(5), types.Int(0), 1},
		{"to the one", types.Int(5), types.Int(1), 5},
		{"a negative exponent is not an integer", types.Int(2), types.Int(-1), 0.5},
		{"a negative base", types.Int(-2), types.Int(3), -8},
		{"a fractional exponent", types.Float(9), types.Float(0.5), 3},
		{"zero to the zero", types.Int(0), types.Int(0), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(t, "power", tt.base, tt.exp); got != types.Float(tt.want) {
				t.Errorf("power(%v, %v) = %v, want %v", tt.base, tt.exp, got, tt.want)
			}
		})
	}
}

func TestPowerRejectsResultsThatAreNotNumbers(t *testing.T) {
	tests := []struct {
		name    string
		base    types.Value
		exp     types.Value
		wantMsg string
	}{
		{"an overflow", types.Float(10), types.Float(400), "overflow"},
		{"a fractional power of a negative", types.Float(-1), types.Float(0.5), "not a number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Call("power", []types.Value{tt.base, tt.exp})
			if err == nil {
				t.Fatal("succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestMinAndMaxIgnoreNull(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		args []types.Value
		want types.Value
	}{
		{"min of two", "min", []types.Value{types.Int(3), types.Int(1)}, types.Int(1)},
		{"max of two", "max", []types.Value{types.Int(3), types.Int(1)}, types.Int(3)},
		{"min of many", "min", []types.Value{types.Int(3), types.Int(1), types.Int(2)}, types.Int(1)},
		{"max of many", "max", []types.Value{types.Int(3), types.Int(1), types.Int(2)}, types.Int(3)},
		{"min of equals", "min", []types.Value{types.Int(1), types.Int(1)}, types.Int(1)},
		{"min of text", "min", []types.Value{types.Text("b"), types.Text("a")}, types.Text("a")},
		{"max of text", "max", []types.Value{types.Text("b"), types.Text("a")}, types.Text("b")},
		// A NULL in the list does not erase a real answer.
		{"a leading NULL", "min", []types.Value{types.Null(), types.Int(2)}, types.Int(2)},
		{"a trailing NULL", "max", []types.Value{types.Int(2), types.Null()}, types.Int(2)},
		{"a NULL in the middle", "min", []types.Value{types.Int(5), types.Null(), types.Int(3)}, types.Int(3)},
		{"all NULL", "min", []types.Value{types.Null(), types.Null()}, types.Null()},
		{"all NULL for max", "max", []types.Value{types.Null(), types.Null()}, types.Null()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(t, tt.fn, tt.args...); got != tt.want {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMinAndMaxAcrossNumericKinds(t *testing.T) {
	// INT and FLOAT compare with each other, so the smaller wins whichever
	// kind it is.
	if got := call(t, "min", types.Int(2), types.Float(1.5)); got != types.Float(1.5) {
		t.Errorf("min(2, 1.5) = %v, want 1.5", got)
	}
	if got := call(t, "max", types.Int(2), types.Float(1.5)); got != types.Int(2) {
		t.Errorf("max(2, 1.5) = %v, want 2", got)
	}
}

func TestMinAndMaxRejectIncomparableKinds(t *testing.T) {
	for _, name := range []string{"min", "max"} {
		t.Run(name, func(t *testing.T) {
			_, err := Call(name, []types.Value{types.Int(1), types.Text("a")})
			if err == nil {
				t.Fatal("succeeded, want a comparison error")
			}
			if !strings.Contains(err.Error(), "compare") {
				t.Errorf("error = %q", err)
			}
		})
	}
}

func TestNullIf(t *testing.T) {
	tests := []struct {
		name string
		args []types.Value
		want types.Value
	}{
		{"equal gives NULL", []types.Value{types.Int(0), types.Int(0)}, types.Null()},
		{"unequal gives the first", []types.Value{types.Int(5), types.Int(0)}, types.Int(5)},
		{"equal text", []types.Value{types.Text("a"), types.Text("a")}, types.Null()},
		{"unequal text", []types.Value{types.Text("a"), types.Text("b")}, types.Text("a")},
		{"across kinds, equal", []types.Value{types.Int(1), types.Float(1)}, types.Null()},
		{"across kinds, unequal", []types.Value{types.Int(1), types.Text("1")}, types.Int(1)},
		// NULL = NULL is unknown, not true, but a NULL first argument is NULL
		// either way.
		{"a NULL first argument", []types.Value{types.Null(), types.Int(1)}, types.Null()},
		{"both NULL", []types.Value{types.Null(), types.Null()}, types.Null()},
		{"a NULL second argument does not match", []types.Value{types.Int(1), types.Null()}, types.Int(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(t, "nullif", tt.args...); got != tt.want {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMathFunctionsPropagateNull(t *testing.T) {
	for _, name := range []string{"sign", "sqrt"} {
		t.Run(name, func(t *testing.T) {
			if got := call(t, name, types.Null()); !got.IsNull() {
				t.Errorf("%s(NULL) = %v, want NULL", name, got)
			}
		})
	}
	t.Run("power", func(t *testing.T) {
		if got := call(t, "power", types.Null(), types.Int(2)); !got.IsNull() {
			t.Errorf("= %v, want NULL", got)
		}
		if got := call(t, "power", types.Int(2), types.Null()); !got.IsNull() {
			t.Errorf("= %v, want NULL", got)
		}
	})
}

func TestMathFunctionsRejectNonNumbers(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		args []types.Value
	}{
		{"sign on text", "sign", []types.Value{types.Text("1")}},
		{"sign on a boolean", "sign", []types.Value{types.Bool(true)}},
		{"sqrt on text", "sqrt", []types.Value{types.Text("4")}},
		{"power on a text base", "power", []types.Value{types.Text("2"), types.Int(2)}},
		{"power on a text exponent", "power", []types.Value{types.Int(2), types.Text("2")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Call(tt.fn, tt.args)
			if err == nil {
				t.Fatal("succeeded, want a type error")
			}
			if !strings.Contains(err.Error(), "needs a number") {
				t.Errorf("error = %q", err)
			}
		})
	}
}

func TestMathArityErrors(t *testing.T) {
	tests := []struct {
		name    string
		fn      string
		args    []types.Value
		wantMsg string
	}{
		{"sign with none", "sign", nil, "sign takes 1 argument"},
		{"sqrt with two", "sqrt", []types.Value{types.Int(1), types.Int(2)}, "sqrt takes 1 argument"},
		{"power with one", "power", []types.Value{types.Int(1)}, "power takes 2 arguments"},
		{"nullif with three", "nullif", []types.Value{types.Int(1), types.Int(2), types.Int(3)}, "nullif takes 2 arguments"},
		{"min with one", "min", []types.Value{types.Int(1)}, "at least 2 arguments"},
		{"max with one", "max", []types.Value{types.Int(1)}, "at least 2 arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Call(tt.fn, tt.args)
			if err == nil {
				t.Fatal("succeeded, want an arity error")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}
