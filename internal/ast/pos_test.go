package ast

import (
	"testing"

	"github.com/pedrovitti/quarry/internal/lex"
)

func at(line, column int) lex.Position {
	return lex.Position{Offset: (line-1)*100 + column - 1, Line: line, Column: column}
}

// TestPosIsWhereTheNodeStarts checks the nodes that carry a position of their
// own. Every error message that points at a caret gets its column from here.
func TestPosIsWhereTheNodeStarts(t *testing.T) {
	p := at(3, 7)
	tests := []struct {
		name string
		node Node
	}{
		{"literal", &Literal{Kind: IntLit, Text: "1", Start: p}},
		{"column reference", &ColumnRef{Name: "a", Start: p}},
		{"star", &Star{Start: p}},
		{"parameter", &Param{Ordinal: 1, Start: p}},
		{"unary expression", &UnaryExpr{Op: Negate, Operand: lit(IntLit, "1"), Start: p}},
		{"function call", &FuncCall{Name: "upper", Start: p}},
		{"parenthesized expression", &ParenExpr{Inner: lit(IntLit, "1"), Start: p}},
		{"table name", &TableName{Name: "t", Start: p}},
		{"select", &Select{Start: p}},
		{"insert", &Insert{Table: "t", Start: p}},
		{"update", &Update{Table: "t", Start: p}},
		{"delete", &Delete{Table: "t", Start: p}},
		{"create table", &CreateTable{Name: "t", Start: p}},
		{"drop table", &DropTable{Name: "t", Start: p}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.node.Pos(); got != p {
				t.Errorf("Pos() = %v, want %v", got, p)
			}
			if got := tt.node.Pos(); !got.IsValid() {
				t.Errorf("Pos() = %v, which is not a valid position", got)
			}
		})
	}
}

// TestPosOfCompositesComesFromTheLeftmostChild covers the nodes with no Start
// field of their own: their position is the position of what they are built
// from, so an error about "a + b" points at the a.
func TestPosOfCompositesComesFromTheLeftmostChild(t *testing.T) {
	left := &Literal{Kind: IntLit, Text: "1", Start: at(1, 1)}
	right := &Literal{Kind: IntLit, Text: "2", Start: at(1, 5)}

	binary := &BinaryExpr{Op: Add, Left: left, Right: right}
	if got := binary.Pos(); got != left.Pos() {
		t.Errorf("BinaryExpr.Pos() = %v, want the left operand's %v", got, left.Pos())
	}

	isNull := &IsNullExpr{Operand: left}
	if got := isNull.Pos(); got != left.Pos() {
		t.Errorf("IsNullExpr.Pos() = %v, want the operand's %v", got, left.Pos())
	}

	leftTable := &TableName{Name: "a", Start: at(1, 10)}
	join := &Join{
		Left:  leftTable,
		Right: &TableName{Name: "b", Start: at(1, 20)},
		On:    lit(BoolLit, "true"),
	}
	if got := join.Pos(); got != leftTable.Pos() {
		t.Errorf("Join.Pos() = %v, want the left table's %v", got, leftTable.Pos())
	}
}

func TestPosOfANestedTreeIsTheOutermostLeft(t *testing.T) {
	// (a + b) + c reports the position of a, however deep it is.
	a := &Literal{Kind: IntLit, Text: "1", Start: at(1, 1)}
	tree := &BinaryExpr{
		Op:    Add,
		Left:  &BinaryExpr{Op: Add, Left: a, Right: lit(IntLit, "2")},
		Right: lit(IntLit, "3"),
	}
	if got := tree.Pos(); got != a.Pos() {
		t.Errorf("Pos() = %v, want %v", got, a.Pos())
	}
}

func TestOperatorNamesForInvalidOperators(t *testing.T) {
	// The zero value of an operator is not an operator. It should render as
	// something readable rather than as an empty string, since it reaches
	// users only through an error message.
	if got := InvalidBinary.String(); got != "invalid operator" {
		t.Errorf("InvalidBinary.String() = %q", got)
	}
	if got := InvalidUnary.String(); got != "invalid operator" {
		t.Errorf("InvalidUnary.String() = %q", got)
	}
	if got := InvalidType.String(); got != "invalid type" {
		t.Errorf("InvalidType.String() = %q", got)
	}
	if got := BinaryOp(999).String(); got != "invalid operator" {
		t.Errorf("BinaryOp(999).String() = %q", got)
	}
	if got := UnaryOp(999).String(); got != "invalid operator" {
		t.Errorf("UnaryOp(999).String() = %q", got)
	}
	if got := TypeName(999).String(); got != "invalid type" {
		t.Errorf("TypeName(999).String() = %q", got)
	}
}

func TestParamRendersItsOrdinal(t *testing.T) {
	// $N goes through a hand-rolled itoa, so the multi-digit and boundary
	// cases are worth stating.
	tests := []struct {
		ordinal int
		want    string
	}{
		{0, "$0"},
		{1, "$1"},
		{9, "$9"},
		{10, "$10"},
		{99, "$99"},
		{100, "$100"},
		{1234567890, "$1234567890"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := (&Param{Ordinal: tt.ordinal}).String(); got != tt.want {
				t.Errorf("= %q, want %q", got, tt.want)
			}
		})
	}
}
