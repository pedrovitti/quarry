package storage

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

func TestStoreCreateAndGet(t *testing.T) {
	s := NewStore()
	heap, err := s.Create("t")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if heap == nil {
		t.Fatal("Create returned a nil heap")
	}
	got, err := s.Get("t")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != heap {
		t.Error("Get returned a different heap than Create")
	}
	if !s.Has("t") {
		t.Error("Has = false after Create")
	}
}

func TestStoreCreateRejectsDuplicates(t *testing.T) {
	s := NewStore()
	if _, err := s.Create("t"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Create("t")
	if err == nil {
		t.Fatal("creating twice succeeded")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q", err)
	}
}

func TestStoreGetMissing(t *testing.T) {
	s := NewStore()
	if _, err := s.Get("nope"); err == nil {
		t.Error("Get on a missing table succeeded")
	}
	if s.Has("nope") {
		t.Error("Has = true for a missing table")
	}
}

func TestStoreDrop(t *testing.T) {
	s := NewStore()
	if _, err := s.Create("t"); err != nil {
		t.Fatal(err)
	}
	if err := s.Drop("t"); err != nil {
		t.Fatalf("Drop returned error: %v", err)
	}
	if s.Has("t") {
		t.Error("Has = true after Drop")
	}
	if err := s.Drop("t"); err == nil {
		t.Error("dropping twice succeeded")
	}
}

func TestStoreTablesAreSorted(t *testing.T) {
	s := NewStore()
	for _, name := range []string{"zebra", "apple", "mango"} {
		if _, err := s.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := strings.Join(s.Tables(), ","), "apple,mango,zebra"; got != want {
		t.Errorf("Tables() = %s, want %s", got, want)
	}
}

func TestStoreRowsCountsEveryTable(t *testing.T) {
	s := NewStore()
	first, _ := s.Create("a")
	second, _ := s.Create("b")

	first.Insert(row(types.Int(1)))
	first.Insert(row(types.Int(2)))
	second.Insert(row(types.Int(3)))

	if got := s.Rows(); got != 3 {
		t.Errorf("Rows = %d, want 3", got)
	}

	// Deleted rows stop counting even though their slots remain.
	if err := first.Delete(1); err != nil {
		t.Fatal(err)
	}
	if got := s.Rows(); got != 2 {
		t.Errorf("Rows = %d after a delete, want 2", got)
	}
}
