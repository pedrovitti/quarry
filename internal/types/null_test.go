package types

import "testing"

// all3 is the three truth values, for exhaustive truth-table tests.
var all3 = []Bool3{True, False, Unknown}

func TestBool3String(t *testing.T) {
	tests := []struct {
		b    Bool3
		want string
	}{
		{True, "true"},
		{False, "false"},
		{Unknown, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsTrueIsStrict(t *testing.T) {
	if !True.IsTrue() {
		t.Error("True.IsTrue() = false")
	}
	if False.IsTrue() {
		t.Error("False.IsTrue() = true")
	}
	if Unknown.IsTrue() {
		t.Error("Unknown.IsTrue() = true -- unknown must not pass a filter")
	}
}

func TestAndTruthTable(t *testing.T) {
	// false dominates: the result cannot depend on the unknown side.
	tests := []struct {
		a, b, want Bool3
	}{
		{True, True, True},
		{True, False, False},
		{True, Unknown, Unknown},
		{False, True, False},
		{False, False, False},
		{False, Unknown, False},
		{Unknown, True, Unknown},
		{Unknown, False, False},
		{Unknown, Unknown, Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.a.String()+"_and_"+tt.b.String(), func(t *testing.T) {
			if got := tt.a.And(tt.b); got != tt.want {
				t.Errorf("%v AND %v = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestOrTruthTable(t *testing.T) {
	// true dominates, by the same argument.
	tests := []struct {
		a, b, want Bool3
	}{
		{True, True, True},
		{True, False, True},
		{True, Unknown, True},
		{False, True, True},
		{False, False, False},
		{False, Unknown, Unknown},
		{Unknown, True, True},
		{Unknown, False, Unknown},
		{Unknown, Unknown, Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.a.String()+"_or_"+tt.b.String(), func(t *testing.T) {
			if got := tt.a.Or(tt.b); got != tt.want {
				t.Errorf("%v OR %v = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestNotTruthTable(t *testing.T) {
	tests := []struct{ b, want Bool3 }{
		{True, False},
		{False, True},
		{Unknown, Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.b.String(), func(t *testing.T) {
			if got := tt.b.Not(); got != tt.want {
				t.Errorf("NOT %v = %v, want %v", tt.b, got, tt.want)
			}
		})
	}
}

func TestAndOrAreCommutative(t *testing.T) {
	for _, a := range all3 {
		for _, b := range all3 {
			if a.And(b) != b.And(a) {
				t.Errorf("AND is not commutative for %v, %v", a, b)
			}
			if a.Or(b) != b.Or(a) {
				t.Errorf("OR is not commutative for %v, %v", a, b)
			}
		}
	}
}

func TestDeMorgan(t *testing.T) {
	// NOT (a AND b) == (NOT a) OR (NOT b), which holds in three-valued logic
	// and is worth pinning: it is the identity the optimizer will rely on when
	// it pushes negation down.
	for _, a := range all3 {
		for _, b := range all3 {
			if got, want := a.And(b).Not(), a.Not().Or(b.Not()); got != want {
				t.Errorf("NOT(%v AND %v) = %v, want %v", a, b, got, want)
			}
			if got, want := a.Or(b).Not(), a.Not().And(b.Not()); got != want {
				t.Errorf("NOT(%v OR %v) = %v, want %v", a, b, got, want)
			}
		}
	}
}

func TestDoubleNegation(t *testing.T) {
	for _, b := range all3 {
		if got := b.Not().Not(); got != b {
			t.Errorf("NOT NOT %v = %v, want %v", b, got, b)
		}
	}
}

func TestTruth(t *testing.T) {
	tests := []struct {
		name string
		v    Value
		want Bool3
		ok   bool
	}{
		{"true", Bool(true), True, true},
		{"false", Bool(false), False, true},
		{"null is unknown", Null(), Unknown, true},
		{"int is not a predicate", Int(1), Unknown, false},
		{"zero is not false", Int(0), Unknown, false},
		{"text is not a predicate", Text("true"), Unknown, false},
		{"float is not a predicate", Float(1), Unknown, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Truth(tt.v)
			if ok != tt.ok {
				t.Fatalf("Truth(%v) ok = %v, want %v", tt.v, ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("Truth(%v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestBool3RoundTripsThroughValue(t *testing.T) {
	for _, b := range all3 {
		got, ok := Truth(b.Value())
		if !ok {
			t.Fatalf("Truth rejected the value of %v", b)
		}
		if got != b {
			t.Errorf("%v round tripped to %v", b, got)
		}
	}
	if !Unknown.Value().IsNull() {
		t.Error("Unknown.Value() is not NULL")
	}
}
