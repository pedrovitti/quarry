package ast

import "strings"

// Dump renders a node as an S-expression, making the shape of the tree
// explicit. String normalizes parentheses away, so "1 + 2 * 3" and
// "(1 + 2) * 3" are told apart here and not there:
//
//	(+ 1 (* 2 3))
//	(* (+ 1 2) 3)
//
// It exists for tests and for debugging the planner, not for users.
func Dump(n Node) string {
	if n == nil {
		return "nil"
	}
	switch node := n.(type) {
	case *Literal:
		return node.String()
	case *ColumnRef:
		return qualify(node.Table, node.Name)
	case *Star:
		return node.String()
	case *Param:
		return node.String()

	case *BinaryExpr:
		return list(node.Op.String(), Dump(node.Left), Dump(node.Right))

	case *UnaryExpr:
		op := node.Op.String()
		if node.Op == Negate {
			op = "neg" // distinguish -x from the infix minus
		} else if node.Op == Plus {
			op = "pos"
		}
		return list(op, Dump(node.Operand))

	case *FuncCall:
		parts := []string{"call", QuoteIdent(node.Name)}
		for _, arg := range node.Args {
			parts = append(parts, Dump(arg))
		}
		return list(parts...)

	case *IsNullExpr:
		if node.Negated {
			return list("is-not-null", Dump(node.Operand))
		}
		return list("is-null", Dump(node.Operand))

	case *SubqueryExpr:
		return list("subquery", dumpSelect(node.Select))

	case *InExpr:
		parts := []string{negatedWord(node.Negated, "in"), Dump(node.Operand)}
		for _, item := range node.List {
			parts = append(parts, Dump(item))
		}
		return list(parts...)

	case *QuantifiedExpr:
		parts := []string{quantifiedWord(node.Quantifier, node.Op.String()), Dump(node.Operand)}
		for _, item := range node.List {
			parts = append(parts, Dump(item))
		}
		return list(parts...)

	case *ParenExpr:
		// Parentheses are already implied by the S-expression nesting.
		return Dump(node.Inner)

	case *TableName:
		if node.Alias == "" {
			return QuoteIdent(node.Name)
		}
		return list("as", QuoteIdent(node.Name), QuoteIdent(node.Alias))

	case *SubqueryTable:
		word := "as"
		if node.Lateral {
			word = "lateral-as"
		}
		return list(word, Dump(node.Select), QuoteIdent(node.Alias))

	case *Join:
		return list("join", Dump(node.Left), Dump(node.Right), Dump(node.On))

	case *Select:
		return dumpSelect(node)

	case *Insert:
		parts := []string{"insert", QuoteIdent(node.Table)}
		if len(node.Columns) > 0 {
			parts = append(parts, list(quoteAll(node.Columns)...))
		}
		for _, row := range node.Rows {
			parts = append(parts, list(dumpAll(row)...))
		}
		return list(parts...)

	case *Update:
		parts := []string{"update", QuoteIdent(node.Table)}
		for _, set := range node.Set {
			parts = append(parts, list("set", QuoteIdent(set.Column), Dump(set.Value)))
		}
		if node.Where != nil {
			parts = append(parts, list("where", Dump(node.Where)))
		}
		return list(parts...)

	case *Delete:
		parts := []string{"delete", QuoteIdent(node.Table)}
		if node.Where != nil {
			parts = append(parts, list("where", Dump(node.Where)))
		}
		return list(parts...)

	case *CreateTable:
		parts := []string{"create-table", QuoteIdent(node.Name)}
		for _, column := range node.Columns {
			def := []string{QuoteIdent(column.Name), column.Type.String()}
			if column.NotNull {
				def = append(def, "not-null")
			}
			if column.PrimaryKey {
				def = append(def, "primary-key")
			}
			if column.Unique {
				def = append(def, "unique")
			}
			if column.Default != nil {
				def = append(def, list("default", Dump(column.Default)))
			}
			if column.Check != nil {
				def = append(def, list("check", Dump(column.Check)))
			}
			parts = append(parts, list(def...))
		}
		for _, constraint := range node.Constraints {
			parts = append(parts, dumpConstraint(constraint))
		}
		return list(parts...)

	case *DropTable:
		return list("drop-table", QuoteIdent(node.Name))
	}
	return "?"
}

func dumpConstraint(c TableConstraint) string {
	switch c.Kind {
	case PrimaryKeyConstraint:
		return list(append([]string{"primary-key"}, quoteAll(c.Columns)...)...)
	case UniqueConstraint:
		return list(append([]string{"unique"}, quoteAll(c.Columns)...)...)
	}
	return list("check", Dump(c.Expr))
}

func dumpSelect(s *Select) string {
	parts := []string{"select"}
	if s.Distinct {
		parts = append(parts, "distinct")
	}

	columns := []string{"columns"}
	for _, item := range s.Columns {
		if item.Alias == "" {
			columns = append(columns, Dump(item.Expr))
		} else {
			columns = append(columns, list("as", Dump(item.Expr), item.Alias))
		}
	}
	parts = append(parts, list(columns...))

	if s.From != nil {
		parts = append(parts, list("from", Dump(s.From)))
	}
	if s.Where != nil {
		parts = append(parts, list("where", Dump(s.Where)))
	}
	if len(s.GroupBy) > 0 {
		group := []string{"group-by"}
		for _, expr := range s.GroupBy {
			group = append(group, Dump(expr))
		}
		parts = append(parts, list(group...))
	}
	if s.Having != nil {
		parts = append(parts, list("having", Dump(s.Having)))
	}
	if len(s.OrderBy) > 0 {
		order := []string{"order"}
		for _, item := range s.OrderBy {
			dir := "asc"
			if item.Desc {
				dir = "desc"
			}
			order = append(order, list(dir, Dump(item.Expr)))
		}
		parts = append(parts, list(order...))
	}
	if s.Limit != nil {
		parts = append(parts, list("limit", Dump(s.Limit)))
	}
	if s.Offset != nil {
		parts = append(parts, list("offset", Dump(s.Offset)))
	}
	return list(parts...)
}

func quantifiedWord(q Quantifier, word string) string {
	if q == AllOf {
		return "all-" + word
	}
	return "any-" + word
}

func negatedWord(negated bool, word string) string {
	if negated {
		return "not-" + word
	}
	return word
}

func quoteAll(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = QuoteIdent(name)
	}
	return out
}

func dumpAll(exprs []Expr) []string {
	out := make([]string, len(exprs))
	for i, e := range exprs {
		out[i] = Dump(e)
	}
	return out
}

func list(parts ...string) string {
	return "(" + strings.Join(parts, " ") + ")"
}
