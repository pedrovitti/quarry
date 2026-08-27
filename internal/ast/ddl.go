package ast

import (
	"strings"

	"github.com/pedrovitti/quarry/internal/lex"
)

// TypeName is a column type as written in a CREATE TABLE. Type names are not
// reserved words, so the parser reads them as identifiers and normalizes here.
type TypeName int

const (
	InvalidType TypeName = iota
	IntType
	BigIntType
	FloatType
	TextType
	BoolType
)

var typeNames = map[TypeName]string{
	IntType:    "INT",
	BigIntType: "BIGINT",
	FloatType:  "FLOAT",
	TextType:   "TEXT",
	BoolType:   "BOOL",
}

func (t TypeName) String() string {
	if name, ok := typeNames[t]; ok {
		return name
	}
	return "invalid type"
}

// aliases maps every accepted spelling to its canonical type. INTEGER and INT
// are the same type; VARCHAR and STRING are spellings of TEXT.
var typeAliases = map[string]TypeName{
	"INT":     IntType,
	"INTEGER": IntType,
	"BIGINT":  BigIntType,
	"FLOAT":   FloatType,
	"DOUBLE":  FloatType,
	"REAL":    FloatType,
	"TEXT":    TextType,
	"VARCHAR": TextType,
	"STRING":  TextType,
	"BOOL":    BoolType,
	"BOOLEAN": BoolType,
}

// LookupType resolves a type name written in any case, reporting false if the
// name is not a type quarry knows.
func LookupType(name string) (TypeName, bool) {
	t, ok := typeAliases[strings.ToUpper(name)]
	return t, ok
}

// TypeNames returns every accepted type spelling, for error messages. The
// result is freshly allocated and unsorted.
func TypeNames() []string {
	names := make([]string, 0, len(typeAliases))
	for name := range typeAliases {
		names = append(names, name)
	}
	return names
}

// ColumnDef is one column in a CREATE TABLE.
type ColumnDef struct {
	Name    string
	Type    TypeName
	NotNull bool
	Start   lex.Position
}

// CreateTable is a CREATE TABLE statement.
type CreateTable struct {
	Name    string
	Columns []ColumnDef
	Start   lex.Position
}

func (c *CreateTable) Pos() lex.Position { return c.Start }
func (*CreateTable) stmtNode()           {}

// DropTable is a DROP TABLE statement.
type DropTable struct {
	Name  string
	Start lex.Position
}

func (d *DropTable) Pos() lex.Position { return d.Start }
func (*DropTable) stmtNode()           {}

// AlterTable is an ALTER TABLE statement: one table, one or more actions
// applied to it left to right.
type AlterTable struct {
	Table   string
	Actions []AlterAction
	Start   lex.Position
}

func (a *AlterTable) Pos() lex.Position { return a.Start }
func (*AlterTable) stmtNode()           {}

// AlterAction is one action in an ALTER TABLE list.
type AlterAction interface {
	alterAction()
}

// RenameTable is RENAME TO: the table gets a new name.
type RenameTable struct {
	To string
}

// RenameColumn is RENAME COLUMN ... TO: a column gets a new name.
type RenameColumn struct {
	From string
	To   string
}

// AddColumn is ADD COLUMN: a new column, with an optional expression that
// backfills the rows the table already has.
type AddColumn struct {
	Def     ColumnDef
	Default Expr
}

// DropColumn is DROP COLUMN.
type DropColumn struct {
	Name string
}

// SetNotNull is ALTER COLUMN ... SET NOT NULL or DROP NOT NULL, depending on
// NotNull.
type SetNotNull struct {
	Column  string
	NotNull bool
}

// SetColumnType is ALTER COLUMN ... TYPE, with an optional USING expression
// that computes each stored value's replacement.
type SetColumnType struct {
	Column string
	Type   TypeName
	Using  Expr
}

func (*RenameTable) alterAction()   {}
func (*RenameColumn) alterAction()  {}
func (*AddColumn) alterAction()     {}
func (*DropColumn) alterAction()    {}
func (*SetNotNull) alterAction()    {}
func (*SetColumnType) alterAction() {}
