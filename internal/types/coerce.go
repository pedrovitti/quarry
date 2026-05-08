package types

import (
	"fmt"
	"strconv"

	"github.com/pedrovitti/quarry/internal/ast"
)

// Error is a runtime type or value error, such as adding a string to a number
// or dividing by zero.
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func errorf(format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// KindOf maps a declared column type to the runtime kind its values have.
func KindOf(t ast.TypeName) Kind {
	switch t {
	case ast.IntType, ast.BigIntType:
		return KindInt
	case ast.FloatType:
		return KindFloat
	case ast.TextType:
		return KindText
	case ast.BoolType:
		return KindBool
	}
	return KindNull
}

// FromLiteral converts a parsed literal into a value. It is the one place
// where the text the lexer captured becomes a typed datum.
func FromLiteral(lit *ast.Literal) (Value, error) {
	switch lit.Kind {
	case ast.NullLit:
		return Null(), nil

	case ast.IntLit:
		n, err := strconv.ParseInt(lit.Text, 10, 64)
		if err != nil {
			// Out of int64 range: keep the value rather than fail, since the
			// literal is still a perfectly good number.
			f, ferr := strconv.ParseFloat(lit.Text, 64)
			if ferr != nil {
				return Null(), errorf("cannot read %q as a number", lit.Text)
			}
			return Float(f), nil
		}
		return Int(n), nil

	case ast.FloatLit:
		f, err := strconv.ParseFloat(lit.Text, 64)
		if err != nil {
			return Null(), errorf("cannot read %q as a number", lit.Text)
		}
		return Float(f), nil

	case ast.StringLit:
		return Text(lit.Text), nil

	case ast.BoolLit:
		return Bool(lit.Text == "TRUE" || lit.Text == "true"), nil
	}
	return Null(), errorf("unknown literal kind")
}

// Convert adapts a value to a column's kind on the way into storage. It widens
// an integer to a float, since writing 1 into a FLOAT column is ordinary, but
// it does not narrow a float to an integer or read a string as a number:
// those lose information silently and SQL requires an explicit cast.
func Convert(v Value, want Kind) (Value, error) {
	if v.IsNull() || v.kind == want {
		return v, nil
	}
	if v.kind == KindInt && want == KindFloat {
		return Float(float64(v.i)), nil
	}
	return Null(), errorf("cannot store %s value in %s column", v.kind, want)
}

// Unify returns the kind two operands should be computed in, for operators
// that need both sides in the same type. It reports false when there is none.
func Unify(a, b Kind) (Kind, bool) {
	if a == KindNull {
		return b, true
	}
	if b == KindNull {
		return a, true
	}
	if a == b {
		return a, true
	}
	if a.IsNumeric() && b.IsNumeric() {
		return KindFloat, true
	}
	return KindNull, false
}
