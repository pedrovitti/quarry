package lex

import "strings"

// Kind classifies a token. Keyword kinds are grouped between keywordBegin and
// keywordEnd so that IsKeyword is a range check rather than a map lookup.
type Kind int

const (
	// Invalid is the zero value and never produced by a successful scan.
	Invalid Kind = iota
	EOF

	Ident  // customers, c1
	Int    // 42
	Float  // 4.2, 1e9
	String // 'text'
	Param  // $1

	// Operators.
	Plus
	Minus
	Star
	Slash
	Percent
	Concat // ||
	Eq
	NotEq // <> or !=
	Less
	LessEq
	Greater
	GreaterEq

	// Punctuation.
	LParen
	RParen
	Comma
	Semicolon
	Dot

	keywordBegin

	And
	As
	Asc
	By
	Create
	Delete
	Desc
	Distinct
	Drop
	False
	From
	Insert
	Into
	Is
	Join
	Limit
	Not
	Null
	Offset
	On
	Or
	Order
	Select
	Set
	Table
	True
	Update
	Values
	Where

	keywordEnd
)

var kindNames = map[Kind]string{
	Invalid: "invalid",
	EOF:     "end of input",

	Ident:  "identifier",
	Int:    "integer",
	Float:  "float",
	String: "string",
	Param:  "parameter",

	Plus:      "+",
	Minus:     "-",
	Star:      "*",
	Slash:     "/",
	Percent:   "%",
	Concat:    "||",
	Eq:        "=",
	NotEq:     "<>",
	Less:      "<",
	LessEq:    "<=",
	Greater:   ">",
	GreaterEq: ">=",

	LParen:    "(",
	RParen:    ")",
	Comma:     ",",
	Semicolon: ";",
	Dot:       ".",

	And:      "AND",
	As:       "AS",
	Asc:      "ASC",
	By:       "BY",
	Create:   "CREATE",
	Delete:   "DELETE",
	Desc:     "DESC",
	Distinct: "DISTINCT",
	Drop:     "DROP",
	False:    "FALSE",
	From:     "FROM",
	Insert:   "INSERT",
	Into:     "INTO",
	Is:       "IS",
	Join:     "JOIN",
	Limit:    "LIMIT",
	Not:      "NOT",
	Null:     "NULL",
	Offset:   "OFFSET",
	On:       "ON",
	Or:       "OR",
	Order:    "ORDER",
	Select:   "SELECT",
	Set:      "SET",
	Table:    "TABLE",
	True:     "TRUE",
	Update:   "UPDATE",
	Values:   "VALUES",
	Where:    "WHERE",
}

// String returns a human-readable name for the kind, suitable for dropping
// into an error message after "expected".
func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return "unknown token"
}

// IsKeyword reports whether k is a reserved word.
func (k Kind) IsKeyword() bool {
	return k > keywordBegin && k < keywordEnd
}

// IsLiteral reports whether k is a self-describing constant.
func (k Kind) IsLiteral() bool {
	switch k {
	case Int, Float, String, True, False, Null:
		return true
	}
	return false
}

// Token is a single lexed unit. Text is the source lexeme with quoting and
// escapes already resolved, so a String token holds the string's value and a
// quoted identifier holds the bare name.
type Token struct {
	Kind Kind
	Text string
	Pos  Position
}

// String renders the token for test failures and error messages.
func (t Token) String() string {
	switch {
	case t.Kind == EOF:
		return "end of input"
	case t.Kind == String:
		return "'" + t.Text + "'"
	case t.Kind.IsKeyword():
		return strings.ToUpper(t.Text)
	case t.Text == "":
		return t.Kind.String()
	}
	return t.Text
}
