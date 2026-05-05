package ast

import "github.com/pedrovitti/quarry/internal/lex"

// Select is a SELECT statement.
type Select struct {
	Distinct bool
	Columns  []SelectItem
	From     TableRef // nil for a FROM-less select such as SELECT 1
	Where    Expr     // nil when absent
	OrderBy  []OrderItem
	Limit    Expr // nil when absent
	Offset   Expr // nil when absent
	Start    lex.Position
}

func (s *Select) Pos() lex.Position { return s.Start }
func (*Select) stmtNode()           {}

// SelectItem is one entry in a select list.
type SelectItem struct {
	Expr  Expr
	Alias string // "" when the item is unaliased
}

// OrderItem is one entry in an ORDER BY clause.
type OrderItem struct {
	Expr Expr
	Desc bool
}

// TableName references a base table, optionally aliased.
type TableName struct {
	Name  string
	Alias string // "" when unaliased
	Start lex.Position
}

func (t *TableName) Pos() lex.Position { return t.Start }
func (*TableName) tableRefNode()       {}

// Ref returns the name a column qualifier must use to reach this table: the
// alias when there is one, otherwise the table name.
func (t *TableName) Ref() string {
	if t.Alias != "" {
		return t.Alias
	}
	return t.Name
}

// Join is an inner join of two table references. Quarry has only one join
// type, so there is no join-kind field to switch on yet.
type Join struct {
	Left  TableRef
	Right TableRef
	On    Expr
}

func (j *Join) Pos() lex.Position { return j.Left.Pos() }
func (*Join) tableRefNode()       {}

// Insert is an INSERT statement. Columns may be empty, meaning every column of
// the table in declaration order.
type Insert struct {
	Table   string
	Columns []string
	Rows    [][]Expr
	Start   lex.Position
}

func (i *Insert) Pos() lex.Position { return i.Start }
func (*Insert) stmtNode()           {}

// Assignment is one "column = expr" pair in an UPDATE.
type Assignment struct {
	Column string
	Value  Expr
}

// Update is an UPDATE statement.
type Update struct {
	Table string
	Set   []Assignment
	Where Expr // nil means every row
	Start lex.Position
}

func (u *Update) Pos() lex.Position { return u.Start }
func (*Update) stmtNode()           {}

// Delete is a DELETE statement.
type Delete struct {
	Table string
	Where Expr // nil means every row
	Start lex.Position
}

func (d *Delete) Pos() lex.Position { return d.Start }
func (*Delete) stmtNode()           {}
