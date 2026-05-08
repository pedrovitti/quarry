// Package types defines the values quarry computes with and the rules for
// comparing, converting and combining them.
//
// Every value can be NULL, and NULL is not a value of its own type: a NULL
// integer and a NULL string are the same value. That is why Kind reports
// KindNull rather than the column's declared type -- what a NULL "would have
// been" belongs to the schema, not to the datum.
package types

import (
	"math"
	"strconv"
	"strings"
)

// Kind is the runtime type of a value.
type Kind int

const (
	KindNull Kind = iota
	KindInt
	KindFloat
	KindText
	KindBool
)

var kindNames = map[Kind]string{
	KindNull:  "NULL",
	KindInt:   "INT",
	KindFloat: "FLOAT",
	KindText:  "TEXT",
	KindBool:  "BOOL",
}

func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return "unknown type"
}

// IsNumeric reports whether k is one of the number types.
func (k Kind) IsNumeric() bool { return k == KindInt || k == KindFloat }

// Value is a single SQL datum.
//
// The zero Value is NULL, which is deliberate: a freshly allocated row is a
// row of NULLs, and code that forgets to set a column produces NULL rather
// than a zero integer.
type Value struct {
	kind Kind
	i    int64   // KindInt, and KindBool as 0 or 1
	f    float64 // KindFloat
	s    string  // KindText
}

// Null returns the NULL value.
func Null() Value { return Value{} }

// Int returns an integer value.
func Int(n int64) Value { return Value{kind: KindInt, i: n} }

// Float returns a floating point value.
func Float(f float64) Value { return Value{kind: KindFloat, f: f} }

// Text returns a string value.
func Text(s string) Value { return Value{kind: KindText, s: s} }

// Bool returns a boolean value.
func Bool(b bool) Value {
	v := Value{kind: KindBool}
	if b {
		v.i = 1
	}
	return v
}

// Kind reports the value's runtime type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether the value is NULL.
func (v Value) IsNull() bool { return v.kind == KindNull }

// Int returns the integer in v, and false if v is not an integer.
func (v Value) Int() (int64, bool) {
	if v.kind != KindInt {
		return 0, false
	}
	return v.i, true
}

// Float returns the float in v, and false if v is not a float.
func (v Value) Float() (float64, bool) {
	if v.kind != KindFloat {
		return 0, false
	}
	return v.f, true
}

// Text returns the string in v, and false if v is not text.
func (v Value) Text() (string, bool) {
	if v.kind != KindText {
		return "", false
	}
	return v.s, true
}

// Bool returns the boolean in v, and false if v is not a boolean. The second
// result distinguishes "v holds false" from "v is not a boolean".
func (v Value) Bool() (bool, bool) {
	if v.kind != KindBool {
		return false, false
	}
	return v.i != 0, true
}

// AsFloat returns v as a float, widening an integer if necessary. It reports
// false for values that are not numeric, including NULL.
func (v Value) AsFloat() (float64, bool) {
	switch v.kind {
	case KindInt:
		return float64(v.i), true
	case KindFloat:
		return v.f, true
	}
	return 0, false
}

// String renders the value the way the CLI prints it: NULL is spelled out,
// text is unquoted, and floats keep the shortest representation that round
// trips.
func (v Value) String() string {
	switch v.kind {
	case KindNull:
		return "NULL"
	case KindInt:
		return strconv.FormatInt(v.i, 10)
	case KindFloat:
		return formatFloat(v.f)
	case KindText:
		return v.s
	case KindBool:
		if v.i != 0 {
			return "true"
		}
		return "false"
	}
	return "?"
}

// SQL renders the value as a literal that would parse back to it, which is
// what error messages and EXPLAIN output want.
func (v Value) SQL() string {
	if v.kind == KindText {
		return "'" + strings.ReplaceAll(v.s, "'", "''") + "'"
	}
	return v.String()
}

// formatFloat renders a float without an exponent where that is reasonable,
// and always with something after the decimal point so that a float column
// does not print as though it held integers.
func formatFloat(f float64) string {
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if math.IsNaN(f) {
		return "NaN"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
