package types

import (
	"math"
	"testing"

	"github.com/pedrovitti/quarry/internal/ast"
)

func TestZeroValueIsNull(t *testing.T) {
	var v Value
	if !v.IsNull() {
		t.Error("the zero Value is not NULL")
	}
	if v.Kind() != KindNull {
		t.Errorf("zero Value kind = %v, want KindNull", v.Kind())
	}
	if v != Null() {
		t.Error("the zero Value differs from Null()")
	}
}

func TestAccessorsReportTheirKind(t *testing.T) {
	tests := []struct {
		name string
		v    Value
		kind Kind
	}{
		{"null", Null(), KindNull},
		{"int", Int(1), KindInt},
		{"float", Float(1.5), KindFloat},
		{"text", Text("a"), KindText},
		{"bool", Bool(true), KindBool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.v.Kind() != tt.kind {
				t.Errorf("Kind() = %v, want %v", tt.v.Kind(), tt.kind)
			}

			// Each accessor answers for its own kind and declines the others.
			_, isInt := tt.v.Int()
			_, isFloat := tt.v.Float()
			_, isText := tt.v.Text()
			_, isBool := tt.v.Bool()
			got := map[Kind]bool{KindInt: isInt, KindFloat: isFloat, KindText: isText, KindBool: isBool}
			for kind, ok := range got {
				if want := kind == tt.kind; ok != want {
					t.Errorf("accessor for %v returned %v, want %v", kind, ok, want)
				}
			}
		})
	}
}

func TestBoolAccessorDistinguishesFalseFromWrongKind(t *testing.T) {
	b, ok := Bool(false).Bool()
	if !ok {
		t.Fatal("Bool(false).Bool() reported the wrong kind")
	}
	if b {
		t.Error("Bool(false).Bool() = true")
	}

	if _, ok := Int(0).Bool(); ok {
		t.Error("Int(0).Bool() reported a boolean")
	}
}

func TestAsFloatWidensIntegers(t *testing.T) {
	f, ok := Int(3).AsFloat()
	if !ok || f != 3.0 {
		t.Errorf("Int(3).AsFloat() = %v, %v; want 3, true", f, ok)
	}
	f, ok = Float(1.5).AsFloat()
	if !ok || f != 1.5 {
		t.Errorf("Float(1.5).AsFloat() = %v, %v; want 1.5, true", f, ok)
	}
	for _, v := range []Value{Null(), Text("1"), Bool(true)} {
		if _, ok := v.AsFloat(); ok {
			t.Errorf("%v.AsFloat() reported a number", v.Kind())
		}
	}
}

func TestValueString(t *testing.T) {
	tests := []struct {
		v    Value
		want string
	}{
		{Null(), "NULL"},
		{Int(0), "0"},
		{Int(-7), "-7"},
		{Float(1.5), "1.5"},
		{Float(3), "3.0"},
		{Float(-0.25), "-0.25"},
		{Text(""), ""},
		{Text("hello"), "hello"},
		{Text("it's"), "it's"},
		{Bool(true), "true"},
		{Bool(false), "false"},
		{Float(math.Inf(1)), "Infinity"},
		{Float(math.Inf(-1)), "-Infinity"},
		{Float(math.NaN()), "NaN"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.v.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValueSQLQuotesText(t *testing.T) {
	tests := []struct {
		v    Value
		want string
	}{
		{Text("hello"), "'hello'"},
		{Text("it's"), "'it''s'"},
		{Text(""), "''"},
		{Int(1), "1"},
		{Null(), "NULL"},
		{Bool(true), "true"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.v.SQL(); got != tt.want {
				t.Errorf("SQL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKindIsNumeric(t *testing.T) {
	for _, k := range []Kind{KindInt, KindFloat} {
		if !k.IsNumeric() {
			t.Errorf("%v.IsNumeric() = false, want true", k)
		}
	}
	for _, k := range []Kind{KindNull, KindText, KindBool} {
		if k.IsNumeric() {
			t.Errorf("%v.IsNumeric() = true, want false", k)
		}
	}
}

func TestKindOf(t *testing.T) {
	tests := []struct {
		declared ast.TypeName
		want     Kind
	}{
		{ast.IntType, KindInt},
		{ast.BigIntType, KindInt},
		{ast.FloatType, KindFloat},
		{ast.TextType, KindText},
		{ast.BoolType, KindBool},
	}
	for _, tt := range tests {
		t.Run(tt.declared.String(), func(t *testing.T) {
			if got := KindOf(tt.declared); got != tt.want {
				t.Errorf("KindOf(%v) = %v, want %v", tt.declared, got, tt.want)
			}
		})
	}
}

func TestFromLiteral(t *testing.T) {
	tests := []struct {
		name string
		lit  *ast.Literal
		want Value
	}{
		{"int", &ast.Literal{Kind: ast.IntLit, Text: "42"}, Int(42)},
		{"negative is not part of the literal", &ast.Literal{Kind: ast.IntLit, Text: "0"}, Int(0)},
		{"float", &ast.Literal{Kind: ast.FloatLit, Text: "1.5"}, Float(1.5)},
		{"exponent", &ast.Literal{Kind: ast.FloatLit, Text: "1e3"}, Float(1000)},
		{"text", &ast.Literal{Kind: ast.StringLit, Text: "hi"}, Text("hi")},
		{"empty text", &ast.Literal{Kind: ast.StringLit, Text: ""}, Text("")},
		{"null", &ast.Literal{Kind: ast.NullLit}, Null()},
		{"true", &ast.Literal{Kind: ast.BoolLit, Text: "TRUE"}, Bool(true)},
		{"false", &ast.Literal{Kind: ast.BoolLit, Text: "FALSE"}, Bool(false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromLiteral(tt.lit)
			if err != nil {
				t.Fatalf("FromLiteral returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("FromLiteral = %v (%v), want %v (%v)", got, got.Kind(), tt.want, tt.want.Kind())
			}
		})
	}
}

func TestFromLiteralOversizedIntBecomesFloat(t *testing.T) {
	// 2^64 does not fit in an int64 but is still a perfectly good number.
	got, err := FromLiteral(&ast.Literal{Kind: ast.IntLit, Text: "18446744073709551616"})
	if err != nil {
		t.Fatalf("FromLiteral returned error: %v", err)
	}
	if got.Kind() != KindFloat {
		t.Errorf("kind = %v, want KindFloat", got.Kind())
	}
}

func TestConvert(t *testing.T) {
	tests := []struct {
		name    string
		v       Value
		want    Kind
		result  Value
		wantErr bool
	}{
		{"same kind passes through", Int(1), KindInt, Int(1), false},
		{"null passes through", Null(), KindInt, Null(), false},
		{"int widens to float", Int(3), KindFloat, Float(3), false},
		{"float does not narrow", Float(1.5), KindInt, Null(), true},
		{"text is not a number", Text("1"), KindInt, Null(), true},
		{"number is not text", Int(1), KindText, Null(), true},
		{"bool is not a number", Bool(true), KindInt, Null(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Convert(tt.v, tt.want)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Convert succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Convert returned error: %v", err)
			}
			if got != tt.result {
				t.Errorf("Convert = %v, want %v", got, tt.result)
			}
		})
	}
}

func TestUnify(t *testing.T) {
	tests := []struct {
		a, b Kind
		want Kind
		ok   bool
	}{
		{KindInt, KindInt, KindInt, true},
		{KindText, KindText, KindText, true},
		{KindInt, KindFloat, KindFloat, true},
		{KindFloat, KindInt, KindFloat, true},
		{KindNull, KindText, KindText, true},
		{KindText, KindNull, KindText, true},
		{KindInt, KindText, KindNull, false},
		{KindBool, KindInt, KindNull, false},
	}
	for _, tt := range tests {
		t.Run(tt.a.String()+"_"+tt.b.String(), func(t *testing.T) {
			got, ok := Unify(tt.a, tt.b)
			if ok != tt.ok {
				t.Fatalf("Unify(%v, %v) ok = %v, want %v", tt.a, tt.b, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("Unify(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
