package catalog

import (
	"strings"
	"testing"
)

// Len, Names and Tables are three views of the same map, and .stats and .tables
// in the shell read them separately. These check they agree - a count that
// disagrees with the list beside it is worse than either being wrong alone.

func TestLenFollowsCreateAndDrop(t *testing.T) {
	c := New()
	if got := c.Len(); got != 0 {
		t.Fatalf("a new catalog has %d tables, want 0", got)
	}

	for i, name := range []string{"a", "b", "c"} {
		if _, err := c.Create(name, []Column{{Name: "x"}}); err != nil {
			t.Fatal(err)
		}
		if got, want := c.Len(), i+1; got != want {
			t.Errorf("after creating %s, Len = %d, want %d", name, got, want)
		}
	}

	if err := c.Drop("b"); err != nil {
		t.Fatal(err)
	}
	if got := c.Len(); got != 2 {
		t.Errorf("after a drop, Len = %d, want 2", got)
	}

	// A failed create leaves the count alone, and so does a failed drop.
	if _, err := c.Create("a", []Column{{Name: "x"}}); err == nil {
		t.Fatal("a duplicate table was created")
	}
	if err := c.Drop("nosuch"); err == nil {
		t.Fatal("a missing table was dropped")
	}
	if got := c.Len(); got != 2 {
		t.Errorf("after two failures, Len = %d, want 2", got)
	}
}

func TestLenAgreesWithNamesAndTables(t *testing.T) {
	c := New()
	for _, name := range []string{"zebra", "apple", "mango"} {
		if _, err := c.Create(name, []Column{{Name: "x"}}); err != nil {
			t.Fatal(err)
		}
	}

	if got, want := len(c.Names()), c.Len(); got != want {
		t.Errorf("Names has %d entries, Len says %d", got, want)
	}
	if got, want := len(c.Tables()), c.Len(); got != want {
		t.Errorf("Tables has %d entries, Len says %d", got, want)
	}

	// And they are in the same order, so a listing built from either matches.
	names := c.Names()
	for i, table := range c.Tables() {
		if table.Name != names[i] {
			t.Errorf("Tables[%d] is %q, Names[%d] is %q", i, table.Name, i, names[i])
		}
	}
	if got, want := strings.Join(names, ","), "apple,mango,zebra"; got != want {
		t.Errorf("Names = %q, want %q", got, want)
	}
}

func TestTablesCarryTheirSchemas(t *testing.T) {
	c := New()
	if _, err := c.Create("t", []Column{{Name: "a"}, {Name: "b"}}); err != nil {
		t.Fatal(err)
	}

	tables := c.Tables()
	if len(tables) != 1 {
		t.Fatalf("Tables returned %d entries", len(tables))
	}
	if got := tables[0].Schema.Len(); got != 2 {
		t.Errorf("the schema has %d columns, want 2", got)
	}
	// The same table, not a copy of the name with an empty schema beside it.
	got, err := c.Get("t")
	if err != nil {
		t.Fatal(err)
	}
	if tables[0] != got {
		t.Error("Tables returned a different table than Get")
	}
}

func TestAnEmptyCatalogListsNothing(t *testing.T) {
	c := New()
	if got := c.Names(); len(got) != 0 {
		t.Errorf("Names = %v, want empty", got)
	}
	if got := c.Tables(); len(got) != 0 {
		t.Errorf("Tables = %v, want empty", got)
	}
	if c.Has("anything") {
		t.Error("an empty catalog claims to hold a table")
	}
}
