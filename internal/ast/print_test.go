package ast

import (
	"testing"

	"github.com/pedrovitti/quarry/internal/lex"
)

func lit(kind LiteralKind, text string) *Literal {
	return &Literal{Kind: kind, Text: text}
}

func col(table, name string) *ColumnRef {
	return &ColumnRef{Table: table, Name: name}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"customers", "customers"},
		{"_id", "_id"},
		{"a1", "a1"},
		{"select", `"select"`},           // reserved
		{"FROM", `"FROM"`},               // reserved regardless of case
		{"order total", `"order total"`}, // space
		{"1abc", `"1abc"`},               // leading digit
		{"", `""`},                       // empty
		{`a"b`, `"a""b"`},                // embedded quote is doubled
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QuoteIdent(tt.name); got != tt.want {
				t.Errorf("QuoteIdent(%q) = %s, want %s", tt.name, got, tt.want)
			}
		})
	}
}

func TestQuoteString(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hello", "'hello'"},
		{"", "''"},
		{"it's", "'it''s'"},
		{"''", "''''''"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := QuoteString(tt.in); got != tt.want {
				t.Errorf("QuoteString(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestExprString(t *testing.T) {
	tests := []struct {
		name string
		expr Expr
		want string
	}{
		{"int literal", lit(IntLit, "42"), "42"},
		{"float literal", lit(FloatLit, "4.2"), "4.2"},
		{"string literal", lit(StringLit, "it's"), "'it''s'"},
		{"null literal", lit(NullLit, ""), "NULL"},
		{"bool literal", lit(BoolLit, "true"), "TRUE"},
		{"unqualified column", col("", "id"), "id"},
		{"qualified column", col("c", "id"), "c.id"},
		{"reserved column name", col("", "order"), `"order"`},
		{"bare star", &Star{}, "*"},
		{"qualified star", &Star{Table: "c"}, "c.*"},
		{"param", &Param{Ordinal: 3}, "$3"},
		{
			"binary",
			&BinaryExpr{Op: Add, Left: lit(IntLit, "1"), Right: lit(IntLit, "2")},
			"1 + 2",
		},
		{
			"comparison",
			&BinaryExpr{Op: GreaterOrEqual, Left: col("", "a"), Right: lit(IntLit, "1")},
			"a >= 1",
		},
		{
			"logical",
			&BinaryExpr{
				Op:    LogicalAnd,
				Left:  &BinaryExpr{Op: Equal, Left: col("", "a"), Right: lit(IntLit, "1")},
				Right: &BinaryExpr{Op: NotEqual, Left: col("", "b"), Right: lit(IntLit, "2")},
			},
			"a = 1 AND b <> 2",
		},
		{"negate", &UnaryExpr{Op: Negate, Operand: col("", "x")}, "-x"},
		{"unary plus", &UnaryExpr{Op: Plus, Operand: col("", "x")}, "+x"},
		{"not", &UnaryExpr{Op: LogicalNot, Operand: col("", "x")}, "NOT x"},
		{"call with no args", &FuncCall{Name: "now"}, "now()"},
		{
			"call with args",
			&FuncCall{Name: "substr", Args: []Expr{col("", "s"), lit(IntLit, "1"), lit(IntLit, "3")}},
			"substr(s, 1, 3)",
		},
		{"is null", &IsNullExpr{Operand: col("", "a")}, "a IS NULL"},
		{"is not null", &IsNullExpr{Operand: col("", "a"), Negated: true}, "a IS NOT NULL"},
		{
			"parens",
			&ParenExpr{Inner: &BinaryExpr{Op: Add, Left: lit(IntLit, "1"), Right: lit(IntLit, "2")}},
			"(1 + 2)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.expr.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectString(t *testing.T) {
	tests := []struct {
		name string
		stmt Stmt
		want string
	}{
		{
			"select constant",
			&Select{Columns: []SelectItem{{Expr: lit(IntLit, "1")}}},
			"SELECT 1",
		},
		{
			"select star from table",
			&Select{
				Columns: []SelectItem{{Expr: &Star{}}},
				From:    &TableName{Name: "customers"},
			},
			"SELECT * FROM customers",
		},
		{
			"distinct",
			&Select{
				Distinct: true,
				Columns:  []SelectItem{{Expr: col("", "city")}},
				From:     &TableName{Name: "customers"},
			},
			"SELECT DISTINCT city FROM customers",
		},
		{
			"aliased column and table",
			&Select{
				Columns: []SelectItem{{Expr: col("c", "id"), Alias: "customer_id"}},
				From:    &TableName{Name: "customers", Alias: "c"},
			},
			"SELECT c.id AS customer_id FROM customers AS c",
		},
		{
			"where and order and limit",
			&Select{
				Columns: []SelectItem{{Expr: &Star{}}},
				From:    &TableName{Name: "t"},
				Where:   &BinaryExpr{Op: Equal, Left: col("", "a"), Right: lit(IntLit, "1")},
				OrderBy: []OrderItem{{Expr: col("", "b"), Desc: true}, {Expr: col("", "c")}},
				Limit:   lit(IntLit, "10"),
				Offset:  lit(IntLit, "5"),
			},
			"SELECT * FROM t WHERE a = 1 ORDER BY b DESC, c ASC LIMIT 10 OFFSET 5",
		},
		{
			"join",
			&Select{
				Columns: []SelectItem{{Expr: &Star{}}},
				From: &Join{
					Left:  &TableName{Name: "orders", Alias: "o"},
					Right: &TableName{Name: "customers", Alias: "c"},
					On:    &BinaryExpr{Op: Equal, Left: col("o", "customer_id"), Right: col("c", "id")},
				},
			},
			"SELECT * FROM orders AS o JOIN customers AS c ON o.customer_id = c.id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stmt.String(); got != tt.want {
				t.Errorf("String() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

func TestDMLString(t *testing.T) {
	tests := []struct {
		name string
		stmt Stmt
		want string
	}{
		{
			"insert without column list",
			&Insert{Table: "t", Rows: [][]Expr{{lit(IntLit, "1"), lit(StringLit, "a")}}},
			"INSERT INTO t VALUES (1, 'a')",
		},
		{
			"insert with columns and two rows",
			&Insert{
				Table:   "t",
				Columns: []string{"id", "name"},
				Rows: [][]Expr{
					{lit(IntLit, "1"), lit(StringLit, "a")},
					{lit(IntLit, "2"), lit(NullLit, "")},
				},
			},
			"INSERT INTO t (id, name) VALUES (1, 'a'), (2, NULL)",
		},
		{
			"update all rows",
			&Update{Table: "t", Set: []Assignment{{Column: "a", Value: lit(IntLit, "1")}}},
			"UPDATE t SET a = 1",
		},
		{
			"update with where",
			&Update{
				Table: "t",
				Set:   []Assignment{{Column: "a", Value: lit(IntLit, "1")}, {Column: "b", Value: lit(IntLit, "2")}},
				Where: &BinaryExpr{Op: Equal, Left: col("", "id"), Right: lit(IntLit, "7")},
			},
			"UPDATE t SET a = 1, b = 2 WHERE id = 7",
		},
		{"delete all rows", &Delete{Table: "t"}, "DELETE FROM t"},
		{
			"delete with where",
			&Delete{Table: "t", Where: &IsNullExpr{Operand: col("", "a")}},
			"DELETE FROM t WHERE a IS NULL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stmt.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDDLString(t *testing.T) {
	create := &CreateTable{
		Name: "customers",
		Columns: []ColumnDef{
			{Name: "id", Type: IntType, NotNull: true},
			{Name: "name", Type: TextType},
		},
	}
	want := "CREATE TABLE customers (id INT NOT NULL, name TEXT)"
	if got := create.String(); got != want {
		t.Errorf("CreateTable.String() = %q, want %q", got, want)
	}

	drop := &DropTable{Name: "customers"}
	if got, want := drop.String(), "DROP TABLE customers"; got != want {
		t.Errorf("DropTable.String() = %q, want %q", got, want)
	}
}

func TestBinaryOpFor(t *testing.T) {
	tests := []struct {
		kind lex.Kind
		want BinaryOp
	}{
		{lex.Plus, Add},
		{lex.Minus, Sub},
		{lex.Star, Mul},
		{lex.Slash, Div},
		{lex.Percent, Mod},
		{lex.Eq, Equal},
		{lex.NotEq, NotEqual},
		{lex.Less, LessThan},
		{lex.LessEq, LessOrEqual},
		{lex.Greater, GreaterThan},
		{lex.GreaterEq, GreaterOrEqual},
		{lex.And, LogicalAnd},
		{lex.Or, LogicalOr},
		{lex.Concat, Concat},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got, ok := BinaryOpFor(tt.kind)
			if !ok {
				t.Fatalf("BinaryOpFor(%v) reported not-an-operator", tt.kind)
			}
			if got != tt.want {
				t.Errorf("BinaryOpFor(%v) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}

	for _, kind := range []lex.Kind{lex.Select, lex.LParen, lex.Comma, lex.EOF, lex.Ident} {
		if _, ok := BinaryOpFor(kind); ok {
			t.Errorf("BinaryOpFor(%v) reported an operator, want none", kind)
		}
	}
}

func TestBinaryOpClassification(t *testing.T) {
	comparisons := []BinaryOp{Equal, NotEqual, LessThan, LessOrEqual, GreaterThan, GreaterOrEqual}
	for _, op := range comparisons {
		if !op.IsComparison() {
			t.Errorf("%v.IsComparison() = false, want true", op)
		}
		if op.IsLogical() {
			t.Errorf("%v.IsLogical() = true, want false", op)
		}
	}
	for _, op := range []BinaryOp{LogicalAnd, LogicalOr} {
		if !op.IsLogical() {
			t.Errorf("%v.IsLogical() = false, want true", op)
		}
		if op.IsComparison() {
			t.Errorf("%v.IsComparison() = true, want false", op)
		}
	}
	for _, op := range []BinaryOp{Add, Sub, Mul, Div, Mod, Concat} {
		if op.IsComparison() || op.IsLogical() {
			t.Errorf("%v classified as comparison or logical, want neither", op)
		}
	}
}

func TestLookupType(t *testing.T) {
	tests := []struct {
		name string
		want TypeName
	}{
		{"INT", IntType},
		{"int", IntType},
		{"Integer", IntType},
		{"BIGINT", BigIntType},
		{"float", FloatType},
		{"DOUBLE", FloatType},
		{"real", FloatType},
		{"TEXT", TextType},
		{"varchar", TextType},
		{"STRING", TextType},
		{"bool", BoolType},
		{"BOOLEAN", BoolType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LookupType(tt.name)
			if !ok {
				t.Fatalf("LookupType(%q) reported unknown", tt.name)
			}
			if got != tt.want {
				t.Errorf("LookupType(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}

	for _, name := range []string{"blob", "", "intt", "numeric"} {
		if _, ok := LookupType(name); ok {
			t.Errorf("LookupType(%q) reported known, want unknown", name)
		}
	}
}

func TestTypeNamesAreAllResolvable(t *testing.T) {
	names := TypeNames()
	if len(names) == 0 {
		t.Fatal("TypeNames() returned nothing")
	}
	for _, name := range names {
		if _, ok := LookupType(name); !ok {
			t.Errorf("TypeNames() returned %q, which LookupType rejects", name)
		}
	}
}

func TestTableNameRef(t *testing.T) {
	if got := (&TableName{Name: "customers"}).Ref(); got != "customers" {
		t.Errorf("Ref() with no alias = %q, want customers", got)
	}
	if got := (&TableName{Name: "customers", Alias: "c"}).Ref(); got != "c" {
		t.Errorf("Ref() with alias = %q, want c", got)
	}
}

func TestUnparen(t *testing.T) {
	inner := col("", "a")
	nested := &ParenExpr{Inner: &ParenExpr{Inner: &ParenExpr{Inner: inner}}}
	if got := Unparen(nested); got != Expr(inner) {
		t.Errorf("Unparen did not reach the innermost expression, got %v", got)
	}
	if got := Unparen(inner); got != Expr(inner) {
		t.Errorf("Unparen changed an unparenthesized expression")
	}
}
