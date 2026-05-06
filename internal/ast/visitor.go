package ast

// Visitor is called for each node encountered by Walk. Returning false stops
// the traversal from descending into that node's children; siblings are still
// visited.
type Visitor func(Node) bool

// Walk traverses the tree rooted at n in source order, calling visit for every
// node including n itself. Nil children are skipped rather than visited.
//
// Clauses that are not nodes -- select items, order items, assignments and
// column definitions -- are not visited themselves, but the expressions inside
// them are.
func Walk(n Node, visit Visitor) {
	if n == nil || !visit(n) {
		return
	}

	switch node := n.(type) {
	case *Select:
		for _, item := range node.Columns {
			Walk(item.Expr, visit)
		}
		if node.From != nil {
			Walk(node.From, visit)
		}
		walkExpr(node.Where, visit)
		for _, item := range node.OrderBy {
			Walk(item.Expr, visit)
		}
		walkExpr(node.Limit, visit)
		walkExpr(node.Offset, visit)

	case *Insert:
		for _, row := range node.Rows {
			for _, val := range row {
				Walk(val, visit)
			}
		}

	case *Update:
		for _, set := range node.Set {
			Walk(set.Value, visit)
		}
		walkExpr(node.Where, visit)

	case *Delete:
		walkExpr(node.Where, visit)

	case *Join:
		Walk(node.Left, visit)
		Walk(node.Right, visit)
		walkExpr(node.On, visit)

	case *BinaryExpr:
		Walk(node.Left, visit)
		Walk(node.Right, visit)

	case *UnaryExpr:
		Walk(node.Operand, visit)

	case *FuncCall:
		for _, arg := range node.Args {
			Walk(arg, visit)
		}

	case *IsNullExpr:
		Walk(node.Operand, visit)

	case *ParenExpr:
		Walk(node.Inner, visit)

	case *TableName, *Literal, *ColumnRef, *Star, *Param, *CreateTable, *DropTable:
		// Leaves.
	}
}

// walkExpr walks e unless it is a nil optional clause. A nil Expr interface
// cannot be passed to Walk directly, because a typed nil would not compare
// equal to nil inside it.
func walkExpr(e Expr, visit Visitor) {
	if e != nil {
		Walk(e, visit)
	}
}

// Inspect walks the tree and collects every node for which match returns true,
// in source order.
func Inspect(n Node, match func(Node) bool) []Node {
	var found []Node
	Walk(n, func(node Node) bool {
		if match(node) {
			found = append(found, node)
		}
		return true
	})
	return found
}

// ColumnRefs returns every column reference in the tree, in source order.
// Duplicates are kept: "a = a" yields two.
func ColumnRefs(n Node) []*ColumnRef {
	var refs []*ColumnRef
	Walk(n, func(node Node) bool {
		if ref, ok := node.(*ColumnRef); ok {
			refs = append(refs, ref)
		}
		return true
	})
	return refs
}

// TableNames returns every base table referenced in the tree, in source order.
func TableNames(n Node) []*TableName {
	var names []*TableName
	Walk(n, func(node Node) bool {
		if name, ok := node.(*TableName); ok {
			names = append(names, name)
		}
		return true
	})
	return names
}

// HasParam reports whether the tree contains a $N placeholder.
func HasParam(n Node) bool {
	found := false
	Walk(n, func(node Node) bool {
		if _, ok := node.(*Param); ok {
			found = true
			return false
		}
		return !found
	})
	return found
}
