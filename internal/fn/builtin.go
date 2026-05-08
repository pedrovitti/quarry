package fn

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/pedrovitti/quarry/internal/types"
)

func init() {
	register(&Func{Name: "upper", MinArgs: 1, MaxArgs: 1, Call: upper})
	register(&Func{Name: "lower", MinArgs: 1, MaxArgs: 1, Call: lower})
	register(&Func{Name: "length", MinArgs: 1, MaxArgs: 1, Call: length})
	register(&Func{Name: "trim", MinArgs: 1, MaxArgs: 1, Call: trim})
	register(&Func{Name: "substr", MinArgs: 2, MaxArgs: 3, Call: substr})
	register(&Func{Name: "replace", MinArgs: 3, MaxArgs: 3, Call: replace})

	register(&Func{Name: "abs", MinArgs: 1, MaxArgs: 1, Call: abs})
	register(&Func{Name: "round", MinArgs: 1, MaxArgs: 1, Call: round})
	register(&Func{Name: "floor", MinArgs: 1, MaxArgs: 1, Call: floorFn})
	register(&Func{Name: "ceil", MinArgs: 1, MaxArgs: 1, Call: ceilFn})

	register(&Func{Name: "coalesce", MinArgs: 1, MaxArgs: -1, Call: coalesce})
	register(&Func{Name: "ifnull", MinArgs: 2, MaxArgs: 2, Call: coalesce})
	register(&Func{Name: "typeof", MinArgs: 1, MaxArgs: 1, Call: typeOf})
}

// text reads a string argument, propagating NULL by reporting ok=false with a
// nil error through the caller's own NULL check.
func text(name string, v types.Value) (string, error) {
	s, ok := v.Text()
	if !ok {
		return "", errorf("%s needs text, got %s", name, v.Kind())
	}
	return s, nil
}

func upper(args []types.Value) (types.Value, error) {
	if args[0].IsNull() {
		return types.Null(), nil
	}
	s, err := text("upper", args[0])
	if err != nil {
		return types.Null(), err
	}
	return types.Text(strings.ToUpper(s)), nil
}

func lower(args []types.Value) (types.Value, error) {
	if args[0].IsNull() {
		return types.Null(), nil
	}
	s, err := text("lower", args[0])
	if err != nil {
		return types.Null(), err
	}
	return types.Text(strings.ToLower(s)), nil
}

// length counts characters, not bytes: "héllo" is 5.
func length(args []types.Value) (types.Value, error) {
	if args[0].IsNull() {
		return types.Null(), nil
	}
	s, err := text("length", args[0])
	if err != nil {
		return types.Null(), err
	}
	return types.Int(int64(utf8.RuneCountInString(s))), nil
}

func trim(args []types.Value) (types.Value, error) {
	if args[0].IsNull() {
		return types.Null(), nil
	}
	s, err := text("trim", args[0])
	if err != nil {
		return types.Null(), err
	}
	return types.Text(strings.TrimSpace(s)), nil
}

// substr takes a 1-based start and an optional length, both counted in
// characters. A start past the end gives an empty string rather than an error,
// which is what makes it safe to use on rows of varying width.
func substr(args []types.Value) (types.Value, error) {
	for _, arg := range args {
		if arg.IsNull() {
			return types.Null(), nil
		}
	}
	s, err := text("substr", args[0])
	if err != nil {
		return types.Null(), err
	}
	start, ok := args[1].Int()
	if !ok {
		return types.Null(), errorf("substr needs an integer start, got %s", args[1].Kind())
	}

	runes := []rune(s)
	if start < 1 {
		start = 1
	}
	from := int(start) - 1
	if from >= len(runes) {
		return types.Text(""), nil
	}

	to := len(runes)
	if len(args) == 3 {
		count, ok := args[2].Int()
		if !ok {
			return types.Null(), errorf("substr needs an integer length, got %s", args[2].Kind())
		}
		if count < 0 {
			return types.Null(), errorf("substr length must not be negative")
		}
		if from+int(count) < to {
			to = from + int(count)
		}
	}
	return types.Text(string(runes[from:to])), nil
}

func replace(args []types.Value) (types.Value, error) {
	for _, arg := range args {
		if arg.IsNull() {
			return types.Null(), nil
		}
	}
	s, err := text("replace", args[0])
	if err != nil {
		return types.Null(), err
	}
	old, err := text("replace", args[1])
	if err != nil {
		return types.Null(), err
	}
	replacement, err := text("replace", args[2])
	if err != nil {
		return types.Null(), err
	}
	return types.Text(strings.ReplaceAll(s, old, replacement)), nil
}

func abs(args []types.Value) (types.Value, error) {
	v := args[0]
	if v.IsNull() {
		return types.Null(), nil
	}
	if n, ok := v.Int(); ok {
		if n == math.MinInt64 {
			return types.Null(), errorf("abs overflows for the most negative integer")
		}
		if n < 0 {
			return types.Int(-n), nil
		}
		return types.Int(n), nil
	}
	f, ok := v.Float()
	if !ok {
		return types.Null(), errorf("abs needs a number, got %s", v.Kind())
	}
	return types.Float(math.Abs(f)), nil
}

// round returns an integer, since rounding a float to a float is rarely what
// anyone means.
func round(args []types.Value) (types.Value, error) {
	v := args[0]
	if v.IsNull() {
		return types.Null(), nil
	}
	if n, ok := v.Int(); ok {
		return types.Int(n), nil
	}
	f, ok := v.Float()
	if !ok {
		return types.Null(), errorf("round needs a number, got %s", v.Kind())
	}
	return types.Int(int64(math.Round(f))), nil
}

func floorFn(args []types.Value) (types.Value, error) {
	return roundWith(args[0], "floor", math.Floor)
}

func ceilFn(args []types.Value) (types.Value, error) {
	return roundWith(args[0], "ceil", math.Ceil)
}

func roundWith(v types.Value, name string, round func(float64) float64) (types.Value, error) {
	if v.IsNull() {
		return types.Null(), nil
	}
	if n, ok := v.Int(); ok {
		return types.Int(n), nil
	}
	f, ok := v.Float()
	if !ok {
		return types.Null(), errorf("%s needs a number, got %s", name, v.Kind())
	}
	return types.Int(int64(round(f))), nil
}

// coalesce returns its first argument that is not NULL, or NULL if they all
// are. It is the one function here that does not propagate NULL.
func coalesce(args []types.Value) (types.Value, error) {
	for _, arg := range args {
		if !arg.IsNull() {
			return arg, nil
		}
	}
	return types.Null(), nil
}

// typeOf names the runtime kind of a value, which is the only way to see the
// difference between an integer and a float that print the same.
func typeOf(args []types.Value) (types.Value, error) {
	return types.Text(args[0].Kind().String()), nil
}
