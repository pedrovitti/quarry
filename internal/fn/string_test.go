package fn

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

func TestTrimSides(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		in   string
		want string
	}{
		{"ltrim leaves the right alone", "ltrim", "  a  ", "a  "},
		{"rtrim leaves the left alone", "rtrim", "  a  ", "  a"},
		{"ltrim tabs and newlines", "ltrim", "\t\n a", "a"},
		{"rtrim tabs and newlines", "rtrim", "a \r\n", "a"},
		{"ltrim with nothing to trim", "ltrim", "a", "a"},
		{"rtrim with nothing to trim", "rtrim", "a", "a"},
		{"ltrim of all spaces", "ltrim", "   ", ""},
		{"rtrim of all spaces", "rtrim", "   ", ""},
		{"ltrim of empty", "ltrim", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := call(t, tt.fn, types.Text(tt.in))
			if got != types.Text(tt.want) {
				t.Errorf("%s(%q) = %v, want %q", tt.fn, tt.in, got, tt.want)
			}
		})
	}
}

func TestReverseWorksOnCharactersNotBytes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"a", "a"},
		{"abc", "cba"},
		{"héllo", "olléh"},
		{"日本語", "語本日"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := call(t, "reverse", types.Text(tt.in)); got != types.Text(tt.want) {
				t.Errorf("reverse(%q) = %v, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRepeat(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		count int64
		want  string
	}{
		{"three times", "ab", 3, "ababab"},
		{"once", "ab", 1, "ab"},
		{"zero times", "ab", 0, ""},
		{"an empty string", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := call(t, "repeat", types.Text(tt.in), types.Int(tt.count))
			if got != types.Text(tt.want) {
				t.Errorf("= %v, want %q", got, tt.want)
			}
		})
	}
}

func TestRepeatIsBounded(t *testing.T) {
	// A mistyped count must fail rather than try to allocate the result.
	_, err := Call("repeat", []types.Value{types.Text("ab"), types.Int(1 << 40)})
	if err == nil {
		t.Fatal("a huge repeat count was accepted")
	}
	if !strings.Contains(err.Error(), "bytes") {
		t.Errorf("error = %q", err)
	}

	if _, err := Call("repeat", []types.Value{types.Text("a"), types.Int(-1)}); err == nil {
		t.Error("a negative repeat count was accepted")
	}
}

func TestPad(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		args []types.Value
		want string
	}{
		{"lpad with the default space", "lpad", []types.Value{types.Text("ab"), types.Int(5)}, "   ab"},
		{"rpad with the default space", "rpad", []types.Value{types.Text("ab"), types.Int(5)}, "ab   "},
		{"lpad with a fill", "lpad", []types.Value{types.Text("7"), types.Int(3), types.Text("0")}, "007"},
		{"rpad with a fill", "rpad", []types.Value{types.Text("7"), types.Int(3), types.Text("0")}, "700"},
		{"a multi-character fill repeats", "rpad", []types.Value{types.Text("x"), types.Int(6), types.Text("ab")}, "xababa"},
		{"a multi-character fill on the left", "lpad", []types.Value{types.Text("x"), types.Int(4), types.Text("ab")}, "abax"},
		{"already the right width", "lpad", []types.Value{types.Text("abc"), types.Int(3)}, "abc"},
		{"longer than the width is truncated", "lpad", []types.Value{types.Text("abcdef"), types.Int(3)}, "abc"},
		{"rpad truncates from the same end", "rpad", []types.Value{types.Text("abcdef"), types.Int(3)}, "abc"},
		{"a width of zero", "rpad", []types.Value{types.Text("abc"), types.Int(0)}, ""},
		{"padding an empty string", "lpad", []types.Value{types.Text(""), types.Int(2), types.Text("-")}, "--"},
		{"width counts characters", "lpad", []types.Value{types.Text("é"), types.Int(3), types.Text("-")}, "--é"},
		{"truncation counts characters", "rpad", []types.Value{types.Text("héllo"), types.Int(2)}, "hé"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := call(t, tt.fn, tt.args...)
			if got != types.Text(tt.want) {
				t.Errorf("= %v, want %q", got, tt.want)
			}
		})
	}
}

func TestPadRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		args []types.Value
	}{
		{"a negative width", []types.Value{types.Text("a"), types.Int(-1)}},
		{"a width past the bound", []types.Value{types.Text("a"), types.Int(1 << 40)}},
		{"an empty fill", []types.Value{types.Text("a"), types.Int(5), types.Text("")}},
		{"a non-text fill", []types.Value{types.Text("a"), types.Int(5), types.Int(0)}},
		{"a text width", []types.Value{types.Text("a"), types.Text("5")}},
		{"non-text input", []types.Value{types.Int(1), types.Int(5)}},
	}
	for _, tt := range tests {
		for _, name := range []string{"lpad", "rpad"} {
			t.Run(name+" "+tt.name, func(t *testing.T) {
				if _, err := Call(name, tt.args); err == nil {
					t.Error("succeeded, want an error")
				}
			})
		}
	}
}

func TestInstrIsOneBasedAndCountsCharacters(t *testing.T) {
	tests := []struct {
		name     string
		haystack string
		needle   string
		want     int64
	}{
		{"at the start", "abc", "a", 1},
		{"in the middle", "abc", "b", 2},
		{"at the end", "abc", "c", 3},
		{"a longer needle", "abcdef", "cd", 3},
		{"absent is zero, not NULL", "abc", "z", 0},
		{"the first occurrence wins", "abab", "ab", 1},
		{"an empty needle matches at one", "abc", "", 1},
		{"an empty haystack", "", "a", 0},
		{"past a multibyte character", "héllo", "llo", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := call(t, "instr", types.Text(tt.haystack), types.Text(tt.needle))
			if got != types.Int(tt.want) {
				t.Errorf("instr(%q, %q) = %v, want %d", tt.haystack, tt.needle, got, tt.want)
			}
		})
	}
}

func TestConcatSkipsNullRatherThanPropagating(t *testing.T) {
	tests := []struct {
		name string
		args []types.Value
		want string
	}{
		{"two strings", []types.Value{types.Text("a"), types.Text("b")}, "ab"},
		{"one string", []types.Value{types.Text("a")}, "a"},
		{"many strings", []types.Value{types.Text("a"), types.Text("b"), types.Text("c")}, "abc"},
		{"a NULL in the middle", []types.Value{types.Text("a"), types.Null(), types.Text("b")}, "ab"},
		{"a NULL at the front", []types.Value{types.Null(), types.Text("b")}, "b"},
		{"all NULL gives an empty string", []types.Value{types.Null(), types.Null()}, ""},
		{"one NULL", []types.Value{types.Null()}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := call(t, "concat", tt.args...); got != types.Text(tt.want) {
				t.Errorf("= %v, want %q", got, tt.want)
			}
		})
	}
}

func TestStartsAndEndsWith(t *testing.T) {
	tests := []struct {
		name  string
		fn    string
		s     string
		affix string
		want  bool
	}{
		{"a prefix", "startswith", "abcdef", "abc", true},
		{"not a prefix", "startswith", "abcdef", "bcd", false},
		{"a suffix is not a prefix", "startswith", "abcdef", "def", false},
		{"a suffix", "endswith", "abcdef", "def", true},
		{"not a suffix", "endswith", "abcdef", "cde", false},
		{"the whole string is both", "startswith", "abc", "abc", true},
		{"the whole string ends itself", "endswith", "abc", "abc", true},
		{"the empty string is a prefix of everything", "startswith", "abc", "", true},
		{"the empty string is a suffix of everything", "endswith", "abc", "", true},
		{"nothing is a prefix of the empty string", "startswith", "", "a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := call(t, tt.fn, types.Text(tt.s), types.Text(tt.affix))
			if got != types.Bool(tt.want) {
				t.Errorf("%s(%q, %q) = %v, want %v", tt.fn, tt.s, tt.affix, got, tt.want)
			}
		})
	}
}

func TestStringFunctionsPropagateNull(t *testing.T) {
	// concat is the exception, and has its own test.
	oneArg := []string{"ltrim", "rtrim", "reverse"}
	for _, name := range oneArg {
		t.Run(name, func(t *testing.T) {
			if got := call(t, name, types.Null()); !got.IsNull() {
				t.Errorf("%s(NULL) = %v, want NULL", name, got)
			}
		})
	}

	twoArg := []string{"repeat", "lpad", "rpad", "instr", "startswith", "endswith"}
	for _, name := range twoArg {
		t.Run(name, func(t *testing.T) {
			if got := call(t, name, types.Null(), types.Int(2)); !got.IsNull() {
				t.Errorf("%s(NULL, ...) = %v, want NULL", name, got)
			}
			if got := call(t, name, types.Text("a"), types.Null()); !got.IsNull() {
				t.Errorf("%s(..., NULL) = %v, want NULL", name, got)
			}
		})
	}

	t.Run("a NULL fill", func(t *testing.T) {
		if got := call(t, "lpad", types.Text("a"), types.Int(3), types.Null()); !got.IsNull() {
			t.Errorf("= %v, want NULL", got)
		}
	})
}

func TestStringFunctionsRejectNonText(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		args []types.Value
	}{
		{"ltrim on a number", "ltrim", []types.Value{types.Int(1)}},
		{"rtrim on a boolean", "rtrim", []types.Value{types.Bool(true)}},
		{"reverse on a number", "reverse", []types.Value{types.Float(1.5)}},
		{"repeat on a number", "repeat", []types.Value{types.Int(1), types.Int(2)}},
		{"repeat with a text count", "repeat", []types.Value{types.Text("a"), types.Text("2")}},
		{"instr on a number", "instr", []types.Value{types.Int(1), types.Text("a")}},
		{"instr with a number needle", "instr", []types.Value{types.Text("a"), types.Int(1)}},
		{"concat on a number", "concat", []types.Value{types.Text("a"), types.Int(1)}},
		{"startswith on a boolean", "startswith", []types.Value{types.Bool(true), types.Text("a")}},
		{"endswith with a number suffix", "endswith", []types.Value{types.Text("a"), types.Int(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Call(tt.fn, tt.args); err == nil {
				t.Error("succeeded, want a type error")
			}
		})
	}
}
