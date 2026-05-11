package fn

import (
	"strings"
	"unicode/utf8"

	"github.com/pedrovitti/quarry/internal/types"
)

func init() {
	register(&Func{Name: "ltrim", MinArgs: 1, MaxArgs: 1, Call: ltrim})
	register(&Func{Name: "rtrim", MinArgs: 1, MaxArgs: 1, Call: rtrim})
	register(&Func{Name: "reverse", MinArgs: 1, MaxArgs: 1, Call: reverse})
	register(&Func{Name: "repeat", MinArgs: 2, MaxArgs: 2, Call: repeat})
	register(&Func{Name: "lpad", MinArgs: 2, MaxArgs: 3, Call: lpad})
	register(&Func{Name: "rpad", MinArgs: 2, MaxArgs: 3, Call: rpad})
	register(&Func{Name: "instr", MinArgs: 2, MaxArgs: 2, Call: instr})
	register(&Func{Name: "concat", MinArgs: 1, MaxArgs: -1, Call: concat})
	register(&Func{Name: "startswith", MinArgs: 2, MaxArgs: 2, Call: startsWith})
	register(&Func{Name: "endswith", MinArgs: 2, MaxArgs: 2, Call: endsWith})
}

// anyNull reports whether any argument is NULL, which is the first thing every
// propagating function checks.
func anyNull(args []types.Value) bool {
	for _, arg := range args {
		if arg.IsNull() {
			return true
		}
	}
	return false
}

// texts reads every argument as a string, for functions whose arguments are
// all text.
func texts(name string, args []types.Value) ([]string, error) {
	out := make([]string, len(args))
	for i, arg := range args {
		s, ok := arg.Text()
		if !ok {
			return nil, errorf("%s needs text, got %s", name, arg.Kind())
		}
		out[i] = s
	}
	return out, nil
}

func ltrim(args []types.Value) (types.Value, error) {
	if anyNull(args) {
		return types.Null(), nil
	}
	s, err := texts("ltrim", args)
	if err != nil {
		return types.Null(), err
	}
	return types.Text(strings.TrimLeft(s[0], " \t\n\r")), nil
}

func rtrim(args []types.Value) (types.Value, error) {
	if anyNull(args) {
		return types.Null(), nil
	}
	s, err := texts("rtrim", args)
	if err != nil {
		return types.Null(), err
	}
	return types.Text(strings.TrimRight(s[0], " \t\n\r")), nil
}

// reverse reverses by character, so a multibyte string comes back intact
// rather than as broken bytes.
func reverse(args []types.Value) (types.Value, error) {
	if anyNull(args) {
		return types.Null(), nil
	}
	s, err := texts("reverse", args)
	if err != nil {
		return types.Null(), err
	}
	runes := []rune(s[0])
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return types.Text(string(runes)), nil
}

// maxRepeat bounds repeat so a mistyped count cannot ask for a string that
// will not fit in memory.
const maxRepeat = 1 << 20

func repeat(args []types.Value) (types.Value, error) {
	if anyNull(args) {
		return types.Null(), nil
	}
	s, ok := args[0].Text()
	if !ok {
		return types.Null(), errorf("repeat needs text, got %s", args[0].Kind())
	}
	count, ok := args[1].Int()
	if !ok {
		return types.Null(), errorf("repeat needs an integer count, got %s", args[1].Kind())
	}
	if count < 0 {
		return types.Null(), errorf("repeat count must not be negative")
	}
	if count > 0 && int64(len(s))*count > maxRepeat {
		return types.Null(), errorf("repeat would produce more than %d bytes", maxRepeat)
	}
	return types.Text(strings.Repeat(s, int(count))), nil
}

func lpad(args []types.Value) (types.Value, error) { return pad(args, true) }
func rpad(args []types.Value) (types.Value, error) { return pad(args, false) }

// pad widens a string to a width in characters, truncating it if it is already
// longer. The fill defaults to a space.
func pad(args []types.Value, left bool) (types.Value, error) {
	name := "rpad"
	if left {
		name = "lpad"
	}
	if anyNull(args) {
		return types.Null(), nil
	}

	s, ok := args[0].Text()
	if !ok {
		return types.Null(), errorf("%s needs text, got %s", name, args[0].Kind())
	}
	width, ok := args[1].Int()
	if !ok {
		return types.Null(), errorf("%s needs an integer width, got %s", name, args[1].Kind())
	}
	if width < 0 {
		return types.Null(), errorf("%s width must not be negative", name)
	}
	if width > maxRepeat {
		return types.Null(), errorf("%s width must not exceed %d", name, maxRepeat)
	}

	fill := " "
	if len(args) == 3 {
		f, ok := args[2].Text()
		if !ok {
			return types.Null(), errorf("%s needs text to pad with, got %s", name, args[2].Kind())
		}
		if f == "" {
			return types.Null(), errorf("%s cannot pad with an empty string", name)
		}
		fill = f
	}

	runes := []rune(s)
	if int64(len(runes)) >= width {
		return types.Text(string(runes[:width])), nil
	}

	fillRunes := []rune(fill)
	padding := make([]rune, 0, int(width)-len(runes))
	for len(padding) < int(width)-len(runes) {
		padding = append(padding, fillRunes[len(padding)%len(fillRunes)])
	}

	if left {
		return types.Text(string(padding) + s), nil
	}
	return types.Text(s + string(padding)), nil
}

// instr returns the 1-based character position of a substring, or 0 when it
// does not occur. Zero rather than NULL, so the result is usable in
// arithmetic without a coalesce.
func instr(args []types.Value) (types.Value, error) {
	if anyNull(args) {
		return types.Null(), nil
	}
	s, err := texts("instr", args)
	if err != nil {
		return types.Null(), err
	}
	index := strings.Index(s[0], s[1])
	if index < 0 {
		return types.Int(0), nil
	}
	// Index is in bytes; the answer is in characters.
	return types.Int(int64(utf8.RuneCountInString(s[0][:index])) + 1), nil
}

// concat joins its arguments, skipping NULLs rather than propagating them.
// That is what makes it different from ||, which is NULL if either side is.
func concat(args []types.Value) (types.Value, error) {
	var sb strings.Builder
	for _, arg := range args {
		if arg.IsNull() {
			continue
		}
		s, ok := arg.Text()
		if !ok {
			return types.Null(), errorf("concat needs text, got %s", arg.Kind())
		}
		sb.WriteString(s)
	}
	return types.Text(sb.String()), nil
}

func startsWith(args []types.Value) (types.Value, error) {
	if anyNull(args) {
		return types.Null(), nil
	}
	s, err := texts("startswith", args)
	if err != nil {
		return types.Null(), err
	}
	return types.Bool(strings.HasPrefix(s[0], s[1])), nil
}

func endsWith(args []types.Value) (types.Value, error) {
	if anyNull(args) {
		return types.Null(), nil
	}
	s, err := texts("endswith", args)
	if err != nil {
		return types.Null(), err
	}
	return types.Bool(strings.HasSuffix(s[0], s[1])), nil
}
