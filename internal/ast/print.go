package ast

import (
	"strings"

	"github.com/pedrovitti/quarry/internal/lex"
)

// QuoteIdent renders name so it re-parses as the same identifier: bare when it
// can be, double quoted otherwise, with embedded quotes doubled.
func QuoteIdent(name string) string {
	if lex.IsBareIdentifier(name) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// QuoteString renders s as a SQL string literal.
func QuoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// qualify renders an optionally-qualified name such as c.id.
func qualify(table, name string) string {
	if table == "" {
		return QuoteIdent(name)
	}
	return QuoteIdent(table) + "." + QuoteIdent(name)
}

func (l *Literal) String() string {
	switch l.Kind {
	case StringLit:
		return QuoteString(l.Text)
	case NullLit:
		return "NULL"
	case BoolLit:
		return strings.ToUpper(l.Text)
	}
	return l.Text
}

func (c *ColumnRef) String() string { return qualify(c.Table, c.Name) }

func (s *Star) String() string {
	if s.Table == "" {
		return "*"
	}
	return QuoteIdent(s.Table) + ".*"
}

func (p *Param) String() string { return "$" + itoa(p.Ordinal) }

func (b *BinaryExpr) String() string {
	return b.Left.String() + " " + b.Op.String() + " " + b.Right.String()
}

func (u *UnaryExpr) String() string {
	if u.Op == LogicalNot {
		return "NOT " + u.Operand.String()
	}
	return u.Op.String() + u.Operand.String()
}

func (f *FuncCall) String() string {
	args := make([]string, len(f.Args))
	for i, arg := range f.Args {
		args[i] = arg.String()
	}
	return QuoteIdent(f.Name) + "(" + strings.Join(args, ", ") + ")"
}

func (i *IsNullExpr) String() string {
	if i.Negated {
		return i.Operand.String() + " IS NOT NULL"
	}
	return i.Operand.String() + " IS NULL"
}

func (p *ParenExpr) String() string { return "(" + p.Inner.String() + ")" }

func (i SelectItem) String() string {
	if i.Alias == "" {
		return i.Expr.String()
	}
	return i.Expr.String() + " AS " + QuoteIdent(i.Alias)
}

func (i OrderItem) String() string {
	if i.Desc {
		return i.Expr.String() + " DESC"
	}
	return i.Expr.String() + " ASC"
}

func (t *TableName) String() string {
	if t.Alias == "" {
		return QuoteIdent(t.Name)
	}
	return QuoteIdent(t.Name) + " AS " + QuoteIdent(t.Alias)
}

func (j *Join) String() string {
	return j.Left.String() + " JOIN " + j.Right.String() + " ON " + j.On.String()
}

func (s *Select) String() string {
	var sb strings.Builder
	sb.WriteString("SELECT ")
	if s.Distinct {
		sb.WriteString("DISTINCT ")
	}
	for i, item := range s.Columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(item.String())
	}
	if s.From != nil {
		sb.WriteString(" FROM ")
		sb.WriteString(s.From.String())
	}
	if s.Where != nil {
		sb.WriteString(" WHERE ")
		sb.WriteString(s.Where.String())
	}
	if len(s.OrderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		for i, item := range s.OrderBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(item.String())
		}
	}
	if s.Limit != nil {
		sb.WriteString(" LIMIT ")
		sb.WriteString(s.Limit.String())
	}
	if s.Offset != nil {
		sb.WriteString(" OFFSET ")
		sb.WriteString(s.Offset.String())
	}
	return sb.String()
}

func (i *Insert) String() string {
	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(QuoteIdent(i.Table))
	if len(i.Columns) > 0 {
		sb.WriteString(" (")
		for n, col := range i.Columns {
			if n > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(QuoteIdent(col))
		}
		sb.WriteString(")")
	}
	sb.WriteString(" VALUES ")
	for n, row := range i.Rows {
		if n > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(")
		for k, val := range row {
			if k > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(val.String())
		}
		sb.WriteString(")")
	}
	return sb.String()
}

func (a Assignment) String() string {
	return QuoteIdent(a.Column) + " = " + a.Value.String()
}

func (u *Update) String() string {
	var sb strings.Builder
	sb.WriteString("UPDATE ")
	sb.WriteString(QuoteIdent(u.Table))
	sb.WriteString(" SET ")
	for i, set := range u.Set {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(set.String())
	}
	if u.Where != nil {
		sb.WriteString(" WHERE ")
		sb.WriteString(u.Where.String())
	}
	return sb.String()
}

func (d *Delete) String() string {
	out := "DELETE FROM " + QuoteIdent(d.Table)
	if d.Where != nil {
		out += " WHERE " + d.Where.String()
	}
	return out
}

func (c ColumnDef) String() string {
	out := QuoteIdent(c.Name) + " " + c.Type.String()
	if c.NotNull {
		out += " NOT NULL"
	}
	return out
}

func (c *CreateTable) String() string {
	cols := make([]string, len(c.Columns))
	for i, col := range c.Columns {
		cols[i] = col.String()
	}
	return "CREATE TABLE " + QuoteIdent(c.Name) + " (" + strings.Join(cols, ", ") + ")"
}

func (d *DropTable) String() string { return "DROP TABLE " + QuoteIdent(d.Name) }

// itoa avoids pulling strconv in for the one place a small non-negative
// integer is rendered.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
