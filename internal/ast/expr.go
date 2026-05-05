package ast

import "github.com/pedrovitti/quarry/internal/lex"

// BinaryOp identifies an infix operator. The set is deliberately independent
// of lex.Kind: the parser translates, so the tree does not inherit the
// lexer's spelling decisions (<> and != are one operator here).
type BinaryOp int

const (
	InvalidBinary BinaryOp = iota

	// Arithmetic.
	Add
	Sub
	Mul
	Div
	Mod

	// Comparison.
	Equal
	NotEqual
	LessThan
	LessOrEqual
	GreaterThan
	GreaterOrEqual

	// Logical.
	LogicalAnd
	LogicalOr

	// String.
	Concat
)

var binaryOpNames = map[BinaryOp]string{
	Add:            "+",
	Sub:            "-",
	Mul:            "*",
	Div:            "/",
	Mod:            "%",
	Equal:          "=",
	NotEqual:       "<>",
	LessThan:       "<",
	LessOrEqual:    "<=",
	GreaterThan:    ">",
	GreaterOrEqual: ">=",
	LogicalAnd:     "AND",
	LogicalOr:      "OR",
	Concat:         "||",
}

func (op BinaryOp) String() string {
	if name, ok := binaryOpNames[op]; ok {
		return name
	}
	return "invalid operator"
}

// IsComparison reports whether op yields a boolean by comparing two values.
func (op BinaryOp) IsComparison() bool {
	switch op {
	case Equal, NotEqual, LessThan, LessOrEqual, GreaterThan, GreaterOrEqual:
		return true
	}
	return false
}

// IsLogical reports whether op combines two booleans.
func (op BinaryOp) IsLogical() bool {
	return op == LogicalAnd || op == LogicalOr
}

// BinaryOpFor maps a token kind to the operator it denotes, reporting false
// for kinds that are not infix operators.
func BinaryOpFor(kind lex.Kind) (BinaryOp, bool) {
	op, ok := binaryOpsByKind[kind]
	return op, ok
}

var binaryOpsByKind = map[lex.Kind]BinaryOp{
	lex.Plus:      Add,
	lex.Minus:     Sub,
	lex.Star:      Mul,
	lex.Slash:     Div,
	lex.Percent:   Mod,
	lex.Eq:        Equal,
	lex.NotEq:     NotEqual,
	lex.Less:      LessThan,
	lex.LessEq:    LessOrEqual,
	lex.Greater:   GreaterThan,
	lex.GreaterEq: GreaterOrEqual,
	lex.And:       LogicalAnd,
	lex.Or:        LogicalOr,
	lex.Concat:    Concat,
}

// UnaryOp identifies a prefix operator.
type UnaryOp int

const (
	InvalidUnary UnaryOp = iota
	Negate               // -x
	Plus                 // +x
	LogicalNot           // NOT x
)

func (op UnaryOp) String() string {
	switch op {
	case Negate:
		return "-"
	case Plus:
		return "+"
	case LogicalNot:
		return "NOT"
	}
	return "invalid operator"
}

// LiteralKind classifies a constant.
type LiteralKind int

const (
	InvalidLiteral LiteralKind = iota
	IntLit
	FloatLit
	StringLit
	BoolLit
	NullLit
)

// Literal is a constant. Text holds the source spelling for numbers and the
// decoded value for strings; for BoolLit it is "TRUE" or "FALSE", and for
// NullLit it is empty. Converting Text to a typed value is the planner's job.
type Literal struct {
	Kind  LiteralKind
	Text  string
	Start lex.Position
}

func (l *Literal) Pos() lex.Position { return l.Start }
func (*Literal) exprNode()           {}

// ColumnRef names a column, optionally qualified by a table or alias.
type ColumnRef struct {
	Table string // "" when unqualified
	Name  string
	Start lex.Position
}

func (c *ColumnRef) Pos() lex.Position { return c.Start }
func (*ColumnRef) exprNode()           {}

// Star is * or t.*, valid only in a select list.
type Star struct {
	Table string // "" for a bare *
	Start lex.Position
}

func (s *Star) Pos() lex.Position { return s.Start }
func (*Star) exprNode()           {}

// Param is a $N placeholder. Ordinal is 1-based, as written.
type Param struct {
	Ordinal int
	Start   lex.Position
}

func (p *Param) Pos() lex.Position { return p.Start }
func (*Param) exprNode()           {}

// BinaryExpr applies an infix operator.
type BinaryExpr struct {
	Op    BinaryOp
	Left  Expr
	Right Expr
}

func (b *BinaryExpr) Pos() lex.Position { return b.Left.Pos() }
func (*BinaryExpr) exprNode()           {}

// UnaryExpr applies a prefix operator.
type UnaryExpr struct {
	Op      UnaryOp
	Operand Expr
	Start   lex.Position
}

func (u *UnaryExpr) Pos() lex.Position { return u.Start }
func (*UnaryExpr) exprNode()           {}

// FuncCall is a function invocation such as length(name).
type FuncCall struct {
	Name  string
	Args  []Expr
	Start lex.Position
}

func (f *FuncCall) Pos() lex.Position { return f.Start }
func (*FuncCall) exprNode()           {}

// IsNullExpr is "x IS NULL" or, when Negated, "x IS NOT NULL". It is a node of
// its own rather than a comparison because NULL = NULL is not true and this is.
type IsNullExpr struct {
	Operand Expr
	Negated bool
}

func (i *IsNullExpr) Pos() lex.Position { return i.Operand.Pos() }
func (*IsNullExpr) exprNode()           {}

// ParenExpr preserves explicit parentheses so that String round-trips without
// having to re-derive them from precedence.
type ParenExpr struct {
	Inner Expr
	Start lex.Position
}

func (p *ParenExpr) Pos() lex.Position { return p.Start }
func (*ParenExpr) exprNode()           {}

// Unparen returns the innermost expression, stripping any parentheses.
func Unparen(e Expr) Expr {
	for {
		p, ok := e.(*ParenExpr)
		if !ok {
			return e
		}
		e = p.Inner
	}
}
