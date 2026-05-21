package ast

import "testing"

func TestDumpOfNilIsNotACrash(t *testing.T) {
	// Dump is a debugging aid, so a nil node has to render rather than panic.
	if got := Dump(nil); got != "nil" {
		t.Errorf("Dump(nil) = %q, want %q", got, "nil")
	}
}

func TestDumpOfLeaves(t *testing.T) {
	tests := []struct {
		name string
		node Node
		want string
	}{
		{"an integer", lit(IntLit, "42"), "42"},
		{"a float", lit(FloatLit, "1.5"), "1.5"},
		{"a string is quoted", lit(StringLit, "a'b"), `'a''b'`},
		{"a boolean is upper case", lit(BoolLit, "true"), "TRUE"},
		{"null", lit(NullLit, ""), "NULL"},
		{"an unqualified column", col("", "id"), "id"},
		{"a qualified column", col("c", "id"), "c.id"},
		{"a column needing quotes", col("", "order by"), `"order by"`},
		{"a bare star", &Star{}, "*"},
		{"a qualified star", &Star{Table: "c"}, "c.*"},
		{"a parameter", &Param{Ordinal: 3}, "$3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Dump(tt.node); got != tt.want {
				t.Errorf("= %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDumpMakesGroupingExplicit(t *testing.T) {
	// This is the whole point of Dump: String() normalizes parentheses away,
	// so these two render identically there and differently here.
	oneTwo := &BinaryExpr{Op: Add, Left: lit(IntLit, "1"), Right: lit(IntLit, "2")}
	twoThree := &BinaryExpr{Op: Mul, Left: lit(IntLit, "2"), Right: lit(IntLit, "3")}

	precedence := &BinaryExpr{Op: Add, Left: lit(IntLit, "1"), Right: twoThree}
	grouped := &BinaryExpr{Op: Mul, Left: &ParenExpr{Inner: oneTwo}, Right: lit(IntLit, "3")}

	if got, want := Dump(precedence), "(+ 1 (* 2 3))"; got != want {
		t.Errorf("= %q, want %q", got, want)
	}
	if got, want := Dump(grouped), "(* (+ 1 2) 3)"; got != want {
		t.Errorf("= %q, want %q", got, want)
	}
	if Dump(precedence) == Dump(grouped) {
		t.Error("Dump did not distinguish the two trees")
	}
}

func TestDumpDropsRedundantParentheses(t *testing.T) {
	// The nesting already says what the parentheses said, so ParenExpr adds
	// nothing and is not rendered - however deeply it is stacked.
	inner := lit(IntLit, "1")
	if got := Dump(&ParenExpr{Inner: &ParenExpr{Inner: inner}}); got != "1" {
		t.Errorf("= %q, want %q", got, "1")
	}
}

func TestDumpOfOperators(t *testing.T) {
	one, two := lit(IntLit, "1"), lit(IntLit, "2")
	tests := []struct {
		name string
		node Node
		want string
	}{
		{"add", &BinaryExpr{Op: Add, Left: one, Right: two}, "(+ 1 2)"},
		{"subtract", &BinaryExpr{Op: Sub, Left: one, Right: two}, "(- 1 2)"},
		{"divide", &BinaryExpr{Op: Div, Left: one, Right: two}, "(/ 1 2)"},
		{"modulo", &BinaryExpr{Op: Mod, Left: one, Right: two}, "(% 1 2)"},
		{"equal", &BinaryExpr{Op: Equal, Left: one, Right: two}, "(= 1 2)"},
		{"not equal", &BinaryExpr{Op: NotEqual, Left: one, Right: two}, "(<> 1 2)"},
		{"less or equal", &BinaryExpr{Op: LessOrEqual, Left: one, Right: two}, "(<= 1 2)"},
		{"and", &BinaryExpr{Op: LogicalAnd, Left: one, Right: two}, "(AND 1 2)"},
		{"or", &BinaryExpr{Op: LogicalOr, Left: one, Right: two}, "(OR 1 2)"},
		{"concat", &BinaryExpr{Op: Concat, Left: one, Right: two}, "(|| 1 2)"},

		// Prefix minus is a different node from the infix one, and the dump
		// has to say which it is.
		{"negate", &UnaryExpr{Op: Negate, Operand: one}, "(neg 1)"},
		{"unary plus", &UnaryExpr{Op: Plus, Operand: one}, "(pos 1)"},
		{"not", &UnaryExpr{Op: LogicalNot, Operand: one}, "(NOT 1)"},

		{"is null", &IsNullExpr{Operand: col("", "x")}, "(is-null x)"},
		{"is not null", &IsNullExpr{Operand: col("", "x"), Negated: true}, "(is-not-null x)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Dump(tt.node); got != tt.want {
				t.Errorf("= %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDumpDistinguishesUnaryFromBinaryMinus(t *testing.T) {
	binary := &BinaryExpr{Op: Sub, Left: lit(IntLit, "0"), Right: lit(IntLit, "1")}
	unary := &UnaryExpr{Op: Negate, Operand: lit(IntLit, "1")}
	if Dump(binary) == Dump(unary) {
		t.Errorf("both dumped as %q", Dump(binary))
	}
}

func TestDumpOfFunctionCalls(t *testing.T) {
	tests := []struct {
		name string
		node Node
		want string
	}{
		{"no arguments", &FuncCall{Name: "now"}, "(call now)"},
		{"one argument", &FuncCall{Name: "upper", Args: []Expr{col("", "name")}}, "(call upper name)"},
		{
			"several arguments",
			&FuncCall{Name: "substr", Args: []Expr{col("", "s"), lit(IntLit, "1"), lit(IntLit, "2")}},
			"(call substr s 1 2)",
		},
		{
			"a nested call",
			&FuncCall{Name: "upper", Args: []Expr{&FuncCall{Name: "trim", Args: []Expr{col("", "s")}}}},
			"(call upper (call trim s))",
		},
		{"a name needing quotes", &FuncCall{Name: "odd name"}, `(call "odd name")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Dump(tt.node); got != tt.want {
				t.Errorf("= %q, want %q", got, tt.want)
			}
		})
	}
}
