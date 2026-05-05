// Package ast defines the syntax tree quarry parses SQL into.
//
// Nodes carry source positions but no resolved types: a Literal holds the text
// the lexer saw, not a typed value, and a ColumnRef holds a name rather than a
// column index. Resolution happens later, in the planner, so that parsing does
// not need a catalog.
package ast

import "github.com/pedrovitti/quarry/internal/lex"

// Node is any element of the tree.
type Node interface {
	// Pos is the position of the node's first token.
	Pos() lex.Position
	// String renders the node back to SQL. The output re-parses to an
	// equivalent tree, though not necessarily to the original text: quoting
	// and spacing are normalized.
	String() string
}

// Stmt is a complete statement.
type Stmt interface {
	Node
	stmtNode()
}

// Expr is anything that evaluates to a value.
type Expr interface {
	Node
	exprNode()
}

// TableRef is anything that can appear in a FROM clause.
type TableRef interface {
	Node
	tableRefNode()
}
