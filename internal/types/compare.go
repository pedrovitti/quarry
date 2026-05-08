package types

import "strings"

// Compare orders two values, returning -1, 0 or 1. The second result is false
// when the comparison has no answer: either operand is NULL, or the two are of
// kinds that cannot be compared.
//
// An integer and a float compare numerically; every other mixed pair does not
// compare at all, because SQL does not silently read '10' as a number.
func Compare(a, b Value) (int, bool) {
	if a.IsNull() || b.IsNull() {
		return 0, false
	}

	if a.kind == KindInt && b.kind == KindInt {
		return compareInt64(a.i, b.i), true
	}
	if a.kind.IsNumeric() && b.kind.IsNumeric() {
		x, _ := a.AsFloat()
		y, _ := b.AsFloat()
		return compareFloat64(x, y), true
	}
	if a.kind != b.kind {
		return 0, false
	}

	switch a.kind {
	case KindText:
		return strings.Compare(a.s, b.s), true
	case KindBool:
		// false sorts before true, matching the usual convention.
		return compareInt64(a.i, b.i), true
	}
	return 0, false
}

// Equal reports whether two values are equal under SQL's rules, where a
// comparison involving NULL is unknown rather than false.
func Equal(a, b Value) Bool3 {
	cmp, ok := Compare(a, b)
	if !ok {
		return Unknown
	}
	return Bool3Of(cmp == 0)
}

// Less reports whether a sorts before b, as a three-valued result.
func Less(a, b Value) Bool3 {
	cmp, ok := Compare(a, b)
	if !ok {
		return Unknown
	}
	return Bool3Of(cmp < 0)
}

// Comparable reports whether two values would compare, ignoring NULL. It is
// what the planner uses to reject "a = b" between a text and an int column
// before running anything.
func Comparable(a, b Kind) bool {
	if a == KindNull || b == KindNull {
		return true // NULL compares with anything, always to unknown
	}
	if a.IsNumeric() && b.IsNumeric() {
		return true
	}
	return a == b
}

// SortKey orders values for ORDER BY, where every pair must have an answer
// even when Compare declines. NULLs sort first and unlike kinds sort by kind,
// so that a sort is total and therefore deterministic.
//
// This is a display concern, not a semantic one: it never feeds a predicate.
func SortKey(a, b Value) int {
	switch {
	case a.IsNull() && b.IsNull():
		return 0
	case a.IsNull():
		return -1
	case b.IsNull():
		return 1
	}
	if cmp, ok := Compare(a, b); ok {
		return cmp
	}
	return compareInt64(int64(a.kind), int64(b.kind))
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func compareFloat64(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	case a == b:
		return 0
	}
	// NaN compares equal to nothing, including itself. Ordering it last keeps
	// SortKey total, which is all this result is used for.
	switch {
	case a != a && b != b:
		return 0
	case a != a:
		return 1
	}
	return -1
}
