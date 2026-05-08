package catalog

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/types"
)

func columns() []Column {
	return []Column{
		{Name: "id", Type: ast.IntType, NotNull: true},
		{Name: "name", Type: ast.TextType},
		{Name: "score", Type: ast.FloatType},
	}
}

func TestCreateAndGet(t *testing.T) {
	c := New()
	created, err := c.Create("customers", columns())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.Name != "customers" {
		t.Errorf("Name = %q, want customers", created.Name)
	}
	if created.Schema.Len() != 3 {
		t.Errorf("Len = %d, want 3", created.Schema.Len())
	}

	got, err := c.Get("customers")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != created {
		t.Error("Get returned a different table than Create")
	}
}

func TestCreateRejectsDuplicates(t *testing.T) {
	c := New()
	if _, err := c.Create("t", columns()); err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}
	_, err := c.Create("t", columns())
	if err == nil {
		t.Fatal("second Create succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to say the table exists", err)
	}
}

func TestCreateRejectsEmptyAndDuplicateColumns(t *testing.T) {
	c := New()
	if _, err := c.Create("t", nil); err == nil {
		t.Error("a table with no columns was accepted")
	}
	dup := []Column{{Name: "a", Type: ast.IntType}, {Name: "a", Type: ast.TextType}}
	_, err := c.Create("t", dup)
	if err == nil {
		t.Fatal("duplicate columns were accepted")
	}
	if !strings.Contains(err.Error(), "defined twice") {
		t.Errorf("error = %q, want it to mention the duplicate", err)
	}
}

func TestGetMissingTable(t *testing.T) {
	c := New()
	_, err := c.Get("nope")
	if err == nil {
		t.Fatal("Get on a missing table succeeded")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q, want it to say the table is missing", err)
	}
}

func TestDrop(t *testing.T) {
	c := New()
	if _, err := c.Create("t", columns()); err != nil {
		t.Fatal(err)
	}
	if !c.Has("t") {
		t.Fatal("Has = false after Create")
	}
	if err := c.Drop("t"); err != nil {
		t.Fatalf("Drop returned error: %v", err)
	}
	if c.Has("t") {
		t.Error("Has = true after Drop")
	}
	if err := c.Drop("t"); err == nil {
		t.Error("dropping a missing table succeeded")
	}
}

func TestNamesAreSorted(t *testing.T) {
	c := New()
	for _, name := range []string{"zebra", "apple", "mango", "banana"} {
		if _, err := c.Create(name, columns()); err != nil {
			t.Fatal(err)
		}
	}
	// Sorted output is what keeps anything printing this list stable across
	// runs, since map iteration order is randomized.
	want := []string{"apple", "banana", "mango", "zebra"}
	got := c.Names()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Names() = %v, want %v", got, want)
	}

	tables := c.Tables()
	if len(tables) != len(want) {
		t.Fatalf("Tables() returned %d, want %d", len(tables), len(want))
	}
	for i, table := range tables {
		if table.Name != want[i] {
			t.Errorf("Tables()[%d] = %q, want %q", i, table.Name, want[i])
		}
	}
}

func TestNamesIsStableAcrossCalls(t *testing.T) {
	c := New()
	for _, name := range []string{"d", "a", "c", "b", "e", "f", "g", "h"} {
		if _, err := c.Create(name, columns()); err != nil {
			t.Fatal(err)
		}
	}
	first := strings.Join(c.Names(), ",")
	for i := 0; i < 20; i++ {
		if got := strings.Join(c.Names(), ","); got != first {
			t.Fatalf("Names() differed between calls:\n  %s\n  %s", first, got)
		}
	}
}

func TestCreateFromStmt(t *testing.T) {
	stmt := &ast.CreateTable{
		Name: "t",
		Columns: []ast.ColumnDef{
			{Name: "a", Type: ast.IntType, NotNull: true},
			{Name: "b", Type: ast.TextType},
		},
	}
	c := New()
	table, err := c.CreateFromStmt(stmt)
	if err != nil {
		t.Fatalf("CreateFromStmt returned error: %v", err)
	}
	if table.Schema.Len() != 2 {
		t.Fatalf("Len = %d, want 2", table.Schema.Len())
	}
	if !table.Schema.Column(0).NotNull {
		t.Error("NOT NULL was lost")
	}
	if table.Schema.Column(1).Type != ast.TextType {
		t.Error("column type was lost")
	}
}

func TestSchemaLookup(t *testing.T) {
	schema, err := NewSchema(columns())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		want int
	}{
		{"id", 0},
		{"name", 1},
		{"score", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := schema.Index(tt.name)
			if !ok {
				t.Fatalf("Index(%q) reported missing", tt.name)
			}
			if got != tt.want {
				t.Errorf("Index(%q) = %d, want %d", tt.name, got, tt.want)
			}
			column, ok := schema.Find(tt.name)
			if !ok || column.Name != tt.name {
				t.Errorf("Find(%q) = %v, %v", tt.name, column, ok)
			}
		})
	}

	if _, ok := schema.Index("missing"); ok {
		t.Error("Index found a column that does not exist")
	}
	if _, ok := schema.Find("missing"); ok {
		t.Error("Find found a column that does not exist")
	}
}

func TestSchemaLookupIsCaseSensitive(t *testing.T) {
	schema, err := NewSchema([]Column{{Name: "id", Type: ast.IntType}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Index("ID"); ok {
		t.Error("Index matched a differently-cased name; quarry folds nothing")
	}
}

func TestSchemaAccessors(t *testing.T) {
	schema, err := NewSchema(columns())
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(schema.Names(), ","); got != "id,name,score" {
		t.Errorf("Names() = %s", got)
	}

	kinds := schema.Kinds()
	want := []types.Kind{types.KindInt, types.KindText, types.KindFloat}
	for i, kind := range kinds {
		if kind != want[i] {
			t.Errorf("Kinds()[%d] = %v, want %v", i, kind, want[i])
		}
	}

	if got, want := schema.String(), "id INT NOT NULL, name TEXT, score FLOAT"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestSchemaColumnsDoesNotAliasTheSchema(t *testing.T) {
	schema, err := NewSchema(columns())
	if err != nil {
		t.Fatal(err)
	}
	got := schema.Columns()
	got[0].Name = "clobbered"

	if schema.Column(0).Name != "id" {
		t.Error("modifying the result of Columns() changed the schema")
	}
}

func TestColumnKind(t *testing.T) {
	tests := []struct {
		declared ast.TypeName
		want     types.Kind
	}{
		{ast.IntType, types.KindInt},
		{ast.BigIntType, types.KindInt},
		{ast.FloatType, types.KindFloat},
		{ast.TextType, types.KindText},
		{ast.BoolType, types.KindBool},
	}
	for _, tt := range tests {
		t.Run(tt.declared.String(), func(t *testing.T) {
			c := Column{Name: "c", Type: tt.declared}
			if got := c.Kind(); got != tt.want {
				t.Errorf("Kind() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestColumnStringQuotesReservedNames(t *testing.T) {
	c := Column{Name: "order", Type: ast.IntType, NotNull: true}
	if got, want := c.String(), `"order" INT NOT NULL`; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
