package ast

import (
	"strings"
	"testing"
)

// sampleSelect is a statement touching every clause Walk descends into.
func sampleSelect() *Select {
	return &Select{
		Columns: []SelectItem{
			{Expr: col("o", "id")},
			{Expr: &FuncCall{Name: "upper", Args: []Expr{col("c", "name")}}, Alias: "name"},
		},
		From: &Join{
			Left:  &TableName{Name: "orders", Alias: "o"},
			Right: &TableName{Name: "customers", Alias: "c"},
			On:    &BinaryExpr{Op: Equal, Left: col("o", "customer_id"), Right: col("c", "id")},
		},
		Where: &BinaryExpr{
			Op:    LogicalAnd,
			Left:  &BinaryExpr{Op: GreaterThan, Left: col("o", "total"), Right: lit(IntLit, "100")},
			Right: &IsNullExpr{Operand: col("c", "deleted_at")},
		},
		OrderBy: []OrderItem{{Expr: col("o", "id"), Desc: true}},
		Limit:   lit(IntLit, "10"),
		Offset:  &Param{Ordinal: 1},
	}
}

func TestWalkVisitsRoot(t *testing.T) {
	stmt := &Delete{Table: "t"}
	var seen []Node
	Walk(stmt, func(n Node) bool {
		seen = append(seen, n)
		return true
	})
	if len(seen) != 1 || seen[0] != Node(stmt) {
		t.Fatalf("Walk visited %d nodes, want just the root", len(seen))
	}
}

func TestWalkSkipsNilClauses(t *testing.T) {
	// A select with no FROM, WHERE, LIMIT or OFFSET must not panic or visit
	// a nil node.
	stmt := &Select{Columns: []SelectItem{{Expr: lit(IntLit, "1")}}}
	count := 0
	Walk(stmt, func(n Node) bool {
		if n == nil {
			t.Fatal("Walk visited a nil node")
		}
		count++
		return true
	})
	if count != 2 { // the select and the literal
		t.Errorf("visited %d nodes, want 2", count)
	}
}

func TestWalkDescendsIntoEveryClause(t *testing.T) {
	stmt := sampleSelect()
	kinds := map[string]int{}
	Walk(stmt, func(n Node) bool {
		switch n.(type) {
		case *Select:
			kinds["select"]++
		case *Join:
			kinds["join"]++
		case *TableName:
			kinds["table"]++
		case *ColumnRef:
			kinds["column"]++
		case *BinaryExpr:
			kinds["binary"]++
		case *FuncCall:
			kinds["call"]++
		case *IsNullExpr:
			kinds["isnull"]++
		case *Literal:
			kinds["literal"]++
		case *Param:
			kinds["param"]++
		}
		return true
	})

	want := map[string]int{
		"select":  1,
		"join":    1,
		"table":   2,
		"column":  7,
		"binary":  3, // the ON clause, the AND, and the > inside it
		"call":    1,
		"isnull":  1,
		"literal": 2,
		"param":   1,
	}
	for kind, n := range want {
		if kinds[kind] != n {
			t.Errorf("visited %d %s nodes, want %d", kinds[kind], kind, n)
		}
	}
}

func TestWalkStopsDescendingOnFalse(t *testing.T) {
	stmt := sampleSelect()

	// Refusing to descend into the join must hide the two table names and the
	// two columns of the ON clause, but not the rest of the statement.
	var tables, columns int
	Walk(stmt, func(n Node) bool {
		switch n.(type) {
		case *Join:
			return false
		case *TableName:
			tables++
		case *ColumnRef:
			columns++
		}
		return true
	})
	if tables != 0 {
		t.Errorf("visited %d table names below a skipped join, want 0", tables)
	}
	if columns != 5 {
		t.Errorf("visited %d columns, want 5 (7 total minus the 2 in the ON clause)", columns)
	}
}

func TestWalkVisitsInSourceOrder(t *testing.T) {
	stmt := sampleSelect()
	var order []string
	Walk(stmt, func(n Node) bool {
		if ref, ok := n.(*ColumnRef); ok {
			order = append(order, ref.String())
		}
		return true
	})
	want := []string{
		"o.id",          // select list
		"c.name",        // select list, inside upper()
		"o.customer_id", // join condition
		"c.id",          // join condition
		"o.total",       // where
		"c.deleted_at",  // where
		"o.id",          // order by
	}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("column order =\n  %v\nwant\n  %v", order, want)
	}
}

func TestColumnRefsKeepsDuplicates(t *testing.T) {
	stmt := &Select{
		Columns: []SelectItem{{Expr: &Star{}}},
		From:    &TableName{Name: "t"},
		Where:   &BinaryExpr{Op: Equal, Left: col("", "a"), Right: col("", "a")},
	}
	refs := ColumnRefs(stmt)
	if len(refs) != 2 {
		t.Fatalf("ColumnRefs returned %d refs, want 2", len(refs))
	}
	for _, ref := range refs {
		if ref.Name != "a" {
			t.Errorf("ref name = %q, want a", ref.Name)
		}
	}
}

func TestTableNames(t *testing.T) {
	names := TableNames(sampleSelect())
	if len(names) != 2 {
		t.Fatalf("TableNames returned %d, want 2", len(names))
	}
	if names[0].Name != "orders" || names[1].Name != "customers" {
		t.Errorf("TableNames = %q, %q; want orders, customers", names[0].Name, names[1].Name)
	}
}

func TestHasParam(t *testing.T) {
	if !HasParam(sampleSelect()) {
		t.Error("HasParam = false on a statement with $1")
	}
	noParam := &Select{
		Columns: []SelectItem{{Expr: &Star{}}},
		From:    &TableName{Name: "t"},
		Where:   &BinaryExpr{Op: Equal, Left: col("", "a"), Right: lit(IntLit, "1")},
	}
	if HasParam(noParam) {
		t.Error("HasParam = true on a statement with no placeholder")
	}
}

func TestInspect(t *testing.T) {
	found := Inspect(sampleSelect(), func(n Node) bool {
		b, ok := n.(*BinaryExpr)
		return ok && b.Op.IsComparison()
	})
	// The = in the ON clause and the > in the WHERE. The AND is logical, not a
	// comparison, and IS NULL is a node of its own.
	if len(found) != 2 {
		t.Fatalf("Inspect found %d comparisons, want 2", len(found))
	}
}

func TestWalkInsertAndUpdate(t *testing.T) {
	ins := &Insert{
		Table:   "t",
		Columns: []string{"a", "b"},
		Rows: [][]Expr{
			{lit(IntLit, "1"), lit(IntLit, "2")},
			{lit(IntLit, "3"), &BinaryExpr{Op: Add, Left: lit(IntLit, "4"), Right: lit(IntLit, "5")}},
		},
	}
	literals := 0
	Walk(ins, func(n Node) bool {
		if _, ok := n.(*Literal); ok {
			literals++
		}
		return true
	})
	if literals != 5 {
		t.Errorf("Walk over INSERT visited %d literals, want 5", literals)
	}

	upd := &Update{
		Table: "t",
		Set: []Assignment{
			{Column: "a", Value: col("", "b")},
			{Column: "c", Value: lit(IntLit, "1")},
		},
		Where: &BinaryExpr{Op: Equal, Left: col("", "id"), Right: &Param{Ordinal: 1}},
	}
	if refs := ColumnRefs(upd); len(refs) != 2 {
		t.Errorf("Walk over UPDATE found %d column refs, want 2", len(refs))
	}
	if !HasParam(upd) {
		t.Error("HasParam over UPDATE = false, want true")
	}
}
