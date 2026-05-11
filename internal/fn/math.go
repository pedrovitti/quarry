package fn

import (
	"math"

	"github.com/pedrovitti/quarry/internal/types"
)

func init() {
	register(&Func{Name: "sign", MinArgs: 1, MaxArgs: 1, Call: sign})
	register(&Func{Name: "sqrt", MinArgs: 1, MaxArgs: 1, Call: sqrt})
	register(&Func{Name: "power", MinArgs: 2, MaxArgs: 2, Call: power})
	register(&Func{Name: "min", MinArgs: 2, MaxArgs: -1, Call: least})
	register(&Func{Name: "max", MinArgs: 2, MaxArgs: -1, Call: greatest})
	register(&Func{Name: "nullif", MinArgs: 2, MaxArgs: 2, Call: nullIf})
}

// number reads a numeric argument as a float.
func number(name string, v types.Value) (float64, error) {
	f, ok := v.AsFloat()
	if !ok {
		return 0, errorf("%s needs a number, got %s", name, v.Kind())
	}
	return f, nil
}

// sign returns -1, 0 or 1, as an integer whatever the argument was: the answer
// is a direction, not a quantity.
func sign(args []types.Value) (types.Value, error) {
	if args[0].IsNull() {
		return types.Null(), nil
	}
	f, err := number("sign", args[0])
	if err != nil {
		return types.Null(), err
	}
	switch {
	case f > 0:
		return types.Int(1), nil
	case f < 0:
		return types.Int(-1), nil
	}
	return types.Int(0), nil
}

func sqrt(args []types.Value) (types.Value, error) {
	if args[0].IsNull() {
		return types.Null(), nil
	}
	f, err := number("sqrt", args[0])
	if err != nil {
		return types.Null(), err
	}
	if f < 0 {
		return types.Null(), errorf("sqrt of a negative number")
	}
	return types.Float(math.Sqrt(f)), nil
}

// power raises one number to another. The result is always a float, since an
// integer base with a negative exponent is not an integer.
func power(args []types.Value) (types.Value, error) {
	if anyNull(args) {
		return types.Null(), nil
	}
	base, err := number("power", args[0])
	if err != nil {
		return types.Null(), err
	}
	exponent, err := number("power", args[1])
	if err != nil {
		return types.Null(), err
	}

	result := math.Pow(base, exponent)
	if math.IsNaN(result) {
		return types.Null(), errorf("power(%v, %v) is not a number", base, exponent)
	}
	if math.IsInf(result, 0) {
		return types.Null(), errorf("power(%v, %v) overflows", base, exponent)
	}
	return types.Float(result), nil
}

// least and greatest ignore NULL rather than propagating it, so a NULL in the
// list does not erase a real answer. All-NULL gives NULL.
func least(args []types.Value) (types.Value, error) { return extreme(args, true) }

func greatest(args []types.Value) (types.Value, error) { return extreme(args, false) }

func extreme(args []types.Value, wantSmallest bool) (types.Value, error) {
	name := "max"
	if wantSmallest {
		name = "min"
	}

	best := types.Null()
	for _, arg := range args {
		if arg.IsNull() {
			continue
		}
		if best.IsNull() {
			best = arg
			continue
		}

		cmp, ok := types.Compare(arg, best)
		if !ok {
			return types.Null(), errorf("%s cannot compare %s with %s", name, arg.Kind(), best.Kind())
		}
		if (wantSmallest && cmp < 0) || (!wantSmallest && cmp > 0) {
			best = arg
		}
	}
	return best, nil
}

// nullif returns NULL when its arguments are equal, and the first otherwise.
// It is the usual way to turn a sentinel into a NULL: nullif(qty, 0).
func nullIf(args []types.Value) (types.Value, error) {
	if args[0].IsNull() {
		return types.Null(), nil
	}
	if types.Equal(args[0], args[1]) == types.True {
		return types.Null(), nil
	}
	return args[0], nil
}
