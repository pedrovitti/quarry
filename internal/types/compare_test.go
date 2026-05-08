package types

import (
	"math"
	"sort"
	"testing"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b Value
		want int
		ok   bool
	}{
		{"ints equal", Int(1), Int(1), 0, true},
		{"ints less", Int(1), Int(2), -1, true},
		{"ints greater", Int(2), Int(1), 1, true},
		{"negative ints", Int(-2), Int(-1), -1, true},
		{"floats", Float(1.5), Float(2.5), -1, true},
		{"int against float", Int(1), Float(1.5), -1, true},
		{"float against int", Float(2.5), Int(2), 1, true},
		{"int equals float", Int(2), Float(2.0), 0, true},
		{"text equal", Text("a"), Text("a"), 0, true},
		{"text less", Text("a"), Text("b"), -1, true},
		{"text is byte ordered", Text("Z"), Text("a"), -1, true},
		{"empty text sorts first", Text(""), Text("a"), -1, true},
		{"false before true", Bool(false), Bool(true), -1, true},
		{"bools equal", Bool(true), Bool(true), 0, true},

		{"null against int", Null(), Int(1), 0, false},
		{"int against null", Int(1), Null(), 0, false},
		{"null against null", Null(), Null(), 0, false},
		{"text against int", Text("1"), Int(1), 0, false},
		{"bool against int", Bool(true), Int(1), 0, false},
		{"bool against text", Bool(true), Text("true"), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Compare(tt.a, tt.b)
			if ok != tt.ok {
				t.Fatalf("Compare(%v, %v) ok = %v, want %v", tt.a, tt.b, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("Compare(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCompareIsAntisymmetric(t *testing.T) {
	values := []Value{Int(1), Int(2), Float(1.5), Text("a"), Text("b"), Bool(true), Bool(false)}
	for _, a := range values {
		for _, b := range values {
			forward, okForward := Compare(a, b)
			back, okBack := Compare(b, a)
			if okForward != okBack {
				t.Errorf("Compare(%v, %v) and its reverse disagree on comparability", a, b)
				continue
			}
			if okForward && forward != -back {
				t.Errorf("Compare(%v, %v) = %d but the reverse = %d", a, b, forward, back)
			}
		}
	}
}

func TestEqualIsThreeValued(t *testing.T) {
	tests := []struct {
		name string
		a, b Value
		want Bool3
	}{
		{"equal ints", Int(1), Int(1), True},
		{"different ints", Int(1), Int(2), False},
		{"int against null", Int(1), Null(), Unknown},
		{"null against null", Null(), Null(), Unknown},
		{"equal text", Text("a"), Text("a"), True},
		{"incomparable kinds", Text("1"), Int(1), Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Equal(tt.a, tt.b); got != tt.want {
				t.Errorf("Equal(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestNullIsNotEqualToItself(t *testing.T) {
	// The single most consequential rule in SQL: this is why IS NULL exists.
	if got := Equal(Null(), Null()); got != Unknown {
		t.Errorf("NULL = NULL evaluated to %v, want unknown", got)
	}
}

func TestLess(t *testing.T) {
	tests := []struct {
		a, b Value
		want Bool3
	}{
		{Int(1), Int(2), True},
		{Int(2), Int(1), False},
		{Int(1), Int(1), False},
		{Text("a"), Text("b"), True},
		{Null(), Int(1), Unknown},
		{Int(1), Null(), Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.a.String()+"_"+tt.b.String(), func(t *testing.T) {
			if got := Less(tt.a, tt.b); got != tt.want {
				t.Errorf("Less(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestComparable(t *testing.T) {
	tests := []struct {
		a, b Kind
		want bool
	}{
		{KindInt, KindInt, true},
		{KindInt, KindFloat, true},
		{KindFloat, KindInt, true},
		{KindText, KindText, true},
		{KindBool, KindBool, true},
		{KindNull, KindText, true},
		{KindText, KindNull, true},
		{KindInt, KindText, false},
		{KindBool, KindInt, false},
		{KindText, KindBool, false},
	}
	for _, tt := range tests {
		t.Run(tt.a.String()+"_"+tt.b.String(), func(t *testing.T) {
			if got := Comparable(tt.a, tt.b); got != tt.want {
				t.Errorf("Comparable(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSortKeyIsTotal(t *testing.T) {
	// Every pair must have an answer, including ones Compare declines, or a
	// sort would depend on the order rows happened to arrive in.
	values := []Value{
		Null(), Int(2), Text("b"), Float(1.5), Bool(true), Text("a"), Int(1), Null(), Bool(false),
	}
	for _, a := range values {
		for _, b := range values {
			forward := SortKey(a, b)
			back := SortKey(b, a)
			if forward != -back {
				t.Errorf("SortKey(%v, %v) = %d but the reverse = %d", a, b, forward, back)
			}
		}
	}
}

func TestSortKeyPutsNullsFirst(t *testing.T) {
	values := []Value{Int(2), Null(), Int(1), Null()}
	sort.SliceStable(values, func(i, j int) bool { return SortKey(values[i], values[j]) < 0 })

	if !values[0].IsNull() || !values[1].IsNull() {
		t.Fatalf("NULLs did not sort first: %v", values)
	}
	if got, _ := values[2].Int(); got != 1 {
		t.Errorf("third value = %v, want 1", values[2])
	}
}

func TestSortKeyIsDeterministicAcrossKinds(t *testing.T) {
	// Sorting the same multiset from two different starting orders must give
	// the same sequence, which is what makes ORDER BY output stable.
	first := []Value{Text("a"), Int(1), Bool(true), Float(2.5)}
	second := []Value{Float(2.5), Bool(true), Int(1), Text("a")}

	byKey := func(vs []Value) {
		sort.SliceStable(vs, func(i, j int) bool { return SortKey(vs[i], vs[j]) < 0 })
	}
	byKey(first)
	byKey(second)

	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("sorting differed at %d: %v vs %v", i, first, second)
		}
	}
}

func TestSortKeyOrdersNaNLast(t *testing.T) {
	nan := Float(math.NaN())
	if SortKey(nan, Float(1)) <= 0 {
		t.Error("NaN did not sort after a real number")
	}
	if SortKey(nan, nan) != 0 {
		t.Error("NaN did not sort equal to itself, which would make sorting unstable")
	}
}
