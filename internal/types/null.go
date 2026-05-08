package types

// Bool3 is SQL's three-valued logic: a predicate is true, false, or unknown.
// Unknown is what a comparison against NULL produces, and it is why WHERE and
// a CHECK constraint treat a NULL predicate differently -- WHERE keeps only
// rows where the predicate is true, so unknown filters the row out.
type Bool3 int

const (
	Unknown Bool3 = iota
	True
	False
)

func (b Bool3) String() string {
	switch b {
	case True:
		return "true"
	case False:
		return "false"
	}
	return "unknown"
}

// IsTrue reports whether b is definitely true. Unknown is not true.
func (b Bool3) IsTrue() bool { return b == True }

// Bool3Of lifts a Go boolean.
func Bool3Of(b bool) Bool3 {
	if b {
		return True
	}
	return False
}

// Truth reads a value as a predicate. NULL is unknown; a boolean is itself.
// Anything else reports false in the second result, because SQL has no
// implicit conversion from a number or a string to a truth value.
func Truth(v Value) (Bool3, bool) {
	if v.IsNull() {
		return Unknown, true
	}
	b, ok := v.Bool()
	if !ok {
		return Unknown, false
	}
	return Bool3Of(b), true
}

// Value converts back to a SQL value, with unknown becoming NULL.
func (b Bool3) Value() Value {
	switch b {
	case True:
		return Bool(true)
	case False:
		return Bool(false)
	}
	return Null()
}

// And is three-valued conjunction. False dominates: "false AND unknown" is
// false, because the result cannot depend on what the unknown turns out to be.
func (b Bool3) And(other Bool3) Bool3 {
	if b == False || other == False {
		return False
	}
	if b == Unknown || other == Unknown {
		return Unknown
	}
	return True
}

// Or is three-valued disjunction. True dominates, by the same argument.
func (b Bool3) Or(other Bool3) Bool3 {
	if b == True || other == True {
		return True
	}
	if b == Unknown || other == Unknown {
		return Unknown
	}
	return False
}

// Not is three-valued negation. Unknown negates to unknown.
func (b Bool3) Not() Bool3 {
	switch b {
	case True:
		return False
	case False:
		return True
	}
	return Unknown
}
