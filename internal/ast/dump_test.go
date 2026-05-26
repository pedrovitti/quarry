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

// dumpSelect covers the clauses one at a time, so a change to any one of them
// shows up as a single failure rather than in one long expected string.
func TestDumpOfSelectClauses(t *testing.T) {
	base := func() *Select {
		return &Select{
			Columns: []SelectItem{{Expr: col("", "id")}},
			From:    &TableName{Name: "t"},
		}
	}

	tests := []struct {
		name string
		stmt func(*Select) *Select
		want string
	}{
		{
			"columns and from",
			func(s *Select) *Select { return s },
			"(select (columns id) (from t))",
		},
		{
			"distinct comes first",
			func(s *Select) *Select { s.Distinct = true; return s },
			"(select distinct (columns id) (from t))",
		},
		{
			"an aliased column",
			func(s *Select) *Select {
				s.Columns = []SelectItem{{Expr: col("", "id"), Alias: "n"}}
				return s
			},
			"(select (columns (as id n)) (from t))",
		},
		{
			"several columns",
			func(s *Select) *Select {
				s.Columns = []SelectItem{{Expr: col("", "a")}, {Expr: col("", "b")}}
				return s
			},
			"(select (columns a b) (from t))",
		},
		{
			"a star",
			func(s *Select) *Select {
				s.Columns = []SelectItem{{Expr: &Star{}}}
				return s
			},
			"(select (columns *) (from t))",
		},
		{
			"where",
			func(s *Select) *Select {
				s.Where = &BinaryExpr{Op: Equal, Left: col("", "id"), Right: lit(IntLit, "1")}
				return s
			},
			"(select (columns id) (from t) (where (= id 1)))",
		},
		{
			"order by ascending",
			func(s *Select) *Select {
				s.OrderBy = []OrderItem{{Expr: col("", "id")}}
				return s
			},
			"(select (columns id) (from t) (order (asc id)))",
		},
		{
			"order by descending",
			func(s *Select) *Select {
				s.OrderBy = []OrderItem{{Expr: col("", "id"), Desc: true}}
				return s
			},
			"(select (columns id) (from t) (order (desc id)))",
		},
		{
			"several order keys keep their direction each",
			func(s *Select) *Select {
				s.OrderBy = []OrderItem{{Expr: col("", "a"), Desc: true}, {Expr: col("", "b")}}
				return s
			},
			"(select (columns id) (from t) (order (desc a) (asc b)))",
		},
		{
			"limit",
			func(s *Select) *Select { s.Limit = lit(IntLit, "10"); return s },
			"(select (columns id) (from t) (limit 10))",
		},
		{
			"offset",
			func(s *Select) *Select { s.Offset = lit(IntLit, "5"); return s },
			"(select (columns id) (from t) (offset 5))",
		},
		{
			"limit before offset, whatever the source order",
			func(s *Select) *Select {
				s.Offset = lit(IntLit, "5")
				s.Limit = lit(IntLit, "10")
				return s
			},
			"(select (columns id) (from t) (limit 10) (offset 5))",
		},
		{
			"an aliased table",
			func(s *Select) *Select { s.From = &TableName{Name: "t", Alias: "x"}; return s },
			"(select (columns id) (from (as t x)))",
		},
		{
			"a join",
			func(s *Select) *Select {
				s.From = &Join{
					Left:  &TableName{Name: "a"},
					Right: &TableName{Name: "b"},
					On:    &BinaryExpr{Op: Equal, Left: col("a", "id"), Right: col("b", "id")},
				}
				return s
			},
			"(select (columns id) (from (join a b (= a.id b.id))))",
		},
		{
			"a chain of joins nests to the left",
			func(s *Select) *Select {
				s.From = &Join{
					Left: &Join{
						Left:  &TableName{Name: "a"},
						Right: &TableName{Name: "b"},
						On:    lit(BoolLit, "true"),
					},
					Right: &TableName{Name: "c"},
					On:    lit(BoolLit, "true"),
				}
				return s
			},
			"(select (columns id) (from (join (join a b TRUE) c TRUE)))",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Dump(tt.stmt(base())); got != tt.want {
				t.Errorf("=    %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestDumpOfSelectWithoutFrom(t *testing.T) {
	// SELECT 1 has no FROM, and the clause is omitted rather than dumped empty.
	s := &Select{Columns: []SelectItem{{Expr: lit(IntLit, "1")}}}
	if got, want := Dump(s), "(select (columns 1))"; got != want {
		t.Errorf("= %q, want %q", got, want)
	}
}

func TestDumpOfEverySelectClauseAtOnce(t *testing.T) {
	s := &Select{
		Distinct: true,
		Columns:  []SelectItem{{Expr: col("t", "a"), Alias: "x"}},
		From:     &TableName{Name: "t"},
		Where:    &IsNullExpr{Operand: col("t", "b"), Negated: true},
		OrderBy:  []OrderItem{{Expr: col("t", "a"), Desc: true}},
		Limit:    lit(IntLit, "10"),
		Offset:   lit(IntLit, "20"),
	}
	want := "(select distinct (columns (as t.a x)) (from t) (where (is-not-null t.b)) " +
		"(order (desc t.a)) (limit 10) (offset 20))"
	if got := Dump(s); got != want {
		t.Errorf("=    %s\nwant %s", got, want)
	}
}
