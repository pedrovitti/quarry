package fn

import (
	"math"
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

func call(t *testing.T, name string, args ...types.Value) types.Value {
	t.Helper()
	got, err := Call(name, args)
	if err != nil {
		t.Fatalf("%s(%v) returned error: %v", name, args, err)
	}
	return got
}

func TestStringFunctions(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		args []types.Value
		want types.Value
	}{
		{"upper", "upper", []types.Value{types.Text("aBc")}, types.Text("ABC")},
		{"upper is idempotent", "upper", []types.Value{types.Text("ABC")}, types.Text("ABC")},
		{"upper multibyte", "upper", []types.Value{types.Text("héllo")}, types.Text("HÉLLO")},
		{"lower", "lower", []types.Value{types.Text("AbC")}, types.Text("abc")},
		{"trim", "trim", []types.Value{types.Text("  a b  ")}, types.Text("a b")},
		{"trim tabs and newlines", "trim", []types.Value{types.Text("\t a \n")}, types.Text("a")},
		{"replace", "replace", []types.Value{types.Text("aXbXc"), types.Text("X"), types.Text("-")}, types.Text("a-b-c")},
		{"replace nothing", "replace", []types.Value{types.Text("abc"), types.Text("z"), types.Text("-")}, types.Text("abc")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(t, tt.fn, tt.args...); got != tt.want {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLengthCountsCharactersNotBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"abc", 3},
		{"héllo", 5},
		{"日本語", 3},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := call(t, "length", types.Text(tt.in))
			n, ok := got.Int()
			if !ok {
				t.Fatalf("length returned %v, want an integer", got.Kind())
			}
			if n != tt.want {
				t.Errorf("length(%q) = %d, want %d", tt.in, n, tt.want)
			}
		})
	}
}

func TestSubstr(t *testing.T) {
	tests := []struct {
		name string
		args []types.Value
		want string
	}{
		{"from the start", []types.Value{types.Text("abcdef"), types.Int(1)}, "abcdef"},
		{"from the middle", []types.Value{types.Text("abcdef"), types.Int(3)}, "cdef"},
		{"with a length", []types.Value{types.Text("abcdef"), types.Int(2), types.Int(3)}, "bcd"},
		{"length past the end", []types.Value{types.Text("abcdef"), types.Int(5), types.Int(99)}, "ef"},
		{"start past the end", []types.Value{types.Text("abc"), types.Int(99)}, ""},
		{"start at the end", []types.Value{types.Text("abc"), types.Int(4)}, ""},
		{"zero length", []types.Value{types.Text("abc"), types.Int(1), types.Int(0)}, ""},
		{"start below one is clamped", []types.Value{types.Text("abc"), types.Int(0)}, "abc"},
		{"negative start is clamped", []types.Value{types.Text("abc"), types.Int(-5)}, "abc"},
		{"counted in characters", []types.Value{types.Text("héllo"), types.Int(2), types.Int(2)}, "él"},
		{"empty input", []types.Value{types.Text(""), types.Int(1)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := call(t, "substr", tt.args...)
			s, ok := got.Text()
			if !ok {
				t.Fatalf("substr returned %v, want text", got.Kind())
			}
			if s != tt.want {
				t.Errorf("= %q, want %q", s, tt.want)
			}
		})
	}
}

func TestSubstrRejectsNegativeLength(t *testing.T) {
	_, err := Call("substr", []types.Value{types.Text("abc"), types.Int(1), types.Int(-1)})
	if err == nil {
		t.Fatal("a negative length was accepted")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Errorf("error = %q", err)
	}
}

func TestNumericFunctions(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		arg  types.Value
		want types.Value
	}{
		{"abs of a negative int", "abs", types.Int(-5), types.Int(5)},
		{"abs of a positive int", "abs", types.Int(5), types.Int(5)},
		{"abs of zero", "abs", types.Int(0), types.Int(0)},
		{"abs of a float", "abs", types.Float(-1.5), types.Float(1.5)},
		{"round down", "round", types.Float(1.4), types.Int(1)},
		{"round up", "round", types.Float(1.6), types.Int(2)},
		{"round half away from zero", "round", types.Float(2.5), types.Int(3)},
		{"round a negative", "round", types.Float(-1.6), types.Int(-2)},
		{"round an int passes through", "round", types.Int(7), types.Int(7)},
		{"floor", "floor", types.Float(1.9), types.Int(1)},
		{"floor a negative", "floor", types.Float(-1.1), types.Int(-2)},
		{"ceil", "ceil", types.Float(1.1), types.Int(2)},
		{"ceil a negative", "ceil", types.Float(-1.9), types.Int(-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(t, tt.fn, tt.arg); got != tt.want {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAbsOverflowsOnTheMostNegativeInteger(t *testing.T) {
	// There is no positive counterpart to MinInt64, so this has to fail
	// rather than wrap around to itself.
	_, err := Call("abs", []types.Value{types.Int(math.MinInt64)})
	if err == nil {
		t.Fatal("abs(MinInt64) succeeded")
	}
	if !strings.Contains(err.Error(), "overflow") {
		t.Errorf("error = %q", err)
	}
}

func TestFunctionsPropagateNull(t *testing.T) {
	// Every function except coalesce and typeof returns NULL for NULL.
	propagating := []string{"upper", "lower", "length", "trim", "abs", "round", "floor", "ceil"}
	for _, name := range propagating {
		t.Run(name, func(t *testing.T) {
			if got := call(t, name, types.Null()); !got.IsNull() {
				t.Errorf("%s(NULL) = %v, want NULL", name, got)
			}
		})
	}

	t.Run("substr", func(t *testing.T) {
		if got := call(t, "substr", types.Null(), types.Int(1)); !got.IsNull() {
			t.Errorf("= %v, want NULL", got)
		}
		if got := call(t, "substr", types.Text("abc"), types.Null()); !got.IsNull() {
			t.Errorf("a NULL start did not propagate: %v", got)
		}
	})

	t.Run("replace", func(t *testing.T) {
		if got := call(t, "replace", types.Text("a"), types.Null(), types.Text("b")); !got.IsNull() {
			t.Errorf("= %v, want NULL", got)
		}
	})
}

func TestCoalesceDoesNotPropagateNull(t *testing.T) {
	tests := []struct {
		name string
		args []types.Value
		want types.Value
	}{
		{"first is not null", []types.Value{types.Int(1), types.Int(2)}, types.Int(1)},
		{"first is null", []types.Value{types.Null(), types.Int(2)}, types.Int(2)},
		{"several nulls", []types.Value{types.Null(), types.Null(), types.Text("x")}, types.Text("x")},
		{"all null", []types.Value{types.Null(), types.Null()}, types.Null()},
		{"one argument", []types.Value{types.Int(1)}, types.Int(1)},
		{"mixed kinds", []types.Value{types.Null(), types.Text("a"), types.Int(1)}, types.Text("a")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(t, "coalesce", tt.args...); got != tt.want {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIfNullIsCoalesceWithTwoArguments(t *testing.T) {
	if got := call(t, "ifnull", types.Null(), types.Int(2)); got != types.Int(2) {
		t.Errorf("= %v, want 2", got)
	}
	if _, err := Call("ifnull", []types.Value{types.Int(1)}); err == nil {
		t.Error("ifnull accepted one argument")
	}
	if _, err := Call("ifnull", []types.Value{types.Int(1), types.Int(2), types.Int(3)}); err == nil {
		t.Error("ifnull accepted three arguments")
	}
}

func TestTypeOf(t *testing.T) {
	tests := []struct {
		v    types.Value
		want string
	}{
		{types.Int(1), "INT"},
		{types.Float(1), "FLOAT"},
		{types.Text("a"), "TEXT"},
		{types.Bool(true), "BOOL"},
		{types.Null(), "NULL"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := call(t, "typeof", tt.v)
			s, _ := got.Text()
			if s != tt.want {
				t.Errorf("= %q, want %q", s, tt.want)
			}
		})
	}
}

func TestNamesAreCaseInsensitive(t *testing.T) {
	for _, spelling := range []string{"upper", "UPPER", "Upper"} {
		if _, ok := Lookup(spelling); !ok {
			t.Errorf("Lookup(%q) found nothing", spelling)
		}
	}
}

func TestUnknownFunction(t *testing.T) {
	_, err := Call("nosuch", nil)
	if err == nil {
		t.Fatal("an unknown function was accepted")
	}
	if !strings.Contains(err.Error(), `no function "nosuch"`) {
		t.Errorf("error = %q", err)
	}
	// The message lists what does exist, which is only useful if it is stable.
	if !strings.Contains(err.Error(), "upper") {
		t.Errorf("error = %q, want it to list the known functions", err)
	}
}

func TestNamesAreSorted(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("Names() returned nothing")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("Names() is not sorted at %d: %q then %q", i, names[i-1], names[i])
		}
	}
	// Stable across calls, or the error messages built from it would differ
	// between runs.
	first := strings.Join(Names(), ",")
	for i := 0; i < 20; i++ {
		if got := strings.Join(Names(), ","); got != first {
			t.Fatal("Names() differed between calls")
		}
	}
}

func TestArityErrors(t *testing.T) {
	tests := []struct {
		name    string
		fn      string
		args    []types.Value
		wantMsg string
	}{
		{"too few", "upper", nil, "upper takes 1 argument"},
		{"too many", "upper", []types.Value{types.Text("a"), types.Text("b")}, "upper takes 1 argument"},
		{"substr too few", "substr", []types.Value{types.Text("a")}, "substr takes 2 to 3 arguments"},
		{"substr too many", "substr", []types.Value{types.Text("a"), types.Int(1), types.Int(1), types.Int(1)}, "substr takes 2 to 3 arguments"},
		{"replace wrong count", "replace", []types.Value{types.Text("a")}, "replace takes 3 arguments"},
		{"coalesce with none", "coalesce", nil, "at least 1 argument"},
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

func TestWrongArgumentKinds(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		args []types.Value
	}{
		{"upper on a number", "upper", []types.Value{types.Int(1)}},
		{"length on a boolean", "length", []types.Value{types.Bool(true)}},
		{"abs on text", "abs", []types.Value{types.Text("1")}},
		{"round on text", "round", []types.Value{types.Text("1")}},
		{"substr with a text start", "substr", []types.Value{types.Text("abc"), types.Text("1")}},
		{"replace with a number", "replace", []types.Value{types.Int(1), types.Text("a"), types.Text("b")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Call(tt.fn, tt.args); err == nil {
				t.Error("succeeded, want a type error")
			}
		})
	}
}
