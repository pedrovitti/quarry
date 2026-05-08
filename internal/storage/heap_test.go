package storage

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

func row(vs ...types.Value) Row { return Row(vs) }

func fill(t *testing.T, h *Heap, n int) []RowID {
	t.Helper()
	ids := make([]RowID, n)
	for i := 0; i < n; i++ {
		ids[i] = h.Insert(row(types.Int(int64(i))))
	}
	return ids
}

func TestInsertAndGet(t *testing.T) {
	h := NewHeap()
	id := h.Insert(row(types.Int(1), types.Text("a")))

	if id == InvalidRowID {
		t.Fatal("Insert returned the invalid id")
	}
	got, ok := h.Get(id)
	if !ok {
		t.Fatal("Get reported the row missing")
	}
	if !got.Equal(row(types.Int(1), types.Text("a"))) {
		t.Errorf("Get = %v", got)
	}
	if h.Len() != 1 {
		t.Errorf("Len = %d, want 1", h.Len())
	}
}

func TestInsertCopiesTheRow(t *testing.T) {
	h := NewHeap()
	original := row(types.Int(1))
	id := h.Insert(original)

	original[0] = types.Int(999)

	got, _ := h.Get(id)
	if n, _ := got[0].Int(); n != 1 {
		t.Errorf("stored row changed with the caller's slice: got %d", n)
	}
}

func TestGetCopiesTheRow(t *testing.T) {
	h := NewHeap()
	id := h.Insert(row(types.Int(1)))

	got, _ := h.Get(id)
	got[0] = types.Int(999)

	again, _ := h.Get(id)
	if n, _ := again[0].Int(); n != 1 {
		t.Errorf("modifying a returned row changed the heap: got %d", n)
	}
}

func TestGetRejectsUnknownIDs(t *testing.T) {
	h := NewHeap()
	h.Insert(row(types.Int(1)))

	for _, id := range []RowID{InvalidRowID, -1, 2, 1000} {
		if _, ok := h.Get(id); ok {
			t.Errorf("Get(%d) found a row", id)
		}
	}
}

func TestUpdate(t *testing.T) {
	h := NewHeap()
	id := h.Insert(row(types.Int(1)))

	if err := h.Update(id, row(types.Int(2))); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	got, _ := h.Get(id)
	if n, _ := got[0].Int(); n != 2 {
		t.Errorf("row = %d, want 2", n)
	}
	if h.Len() != 1 {
		t.Errorf("Len = %d, want 1 -- update must not add a row", h.Len())
	}
}

func TestUpdateRejectsMissingRows(t *testing.T) {
	h := NewHeap()
	id := h.Insert(row(types.Int(1)))

	if err := h.Update(99, row(types.Int(2))); err == nil {
		t.Error("updating a nonexistent row succeeded")
	}
	if err := h.Delete(id); err != nil {
		t.Fatal(err)
	}
	err := h.Update(id, row(types.Int(2)))
	if err == nil {
		t.Fatal("updating a deleted row succeeded")
	}
	if !strings.Contains(err.Error(), "deleted") {
		t.Errorf("error = %q, want it to say the row was deleted", err)
	}
}

func TestDelete(t *testing.T) {
	h := NewHeap()
	ids := fill(t, h, 3)

	if err := h.Delete(ids[1]); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if h.Len() != 2 {
		t.Errorf("Len = %d, want 2", h.Len())
	}
	if _, ok := h.Get(ids[1]); ok {
		t.Error("Get found a deleted row")
	}
	if err := h.Delete(ids[1]); err == nil {
		t.Error("deleting twice succeeded")
	}
}

func TestDeletePreservesOtherIDs(t *testing.T) {
	// Ids are not reused and deleting one must not shift the others, or an
	// executor holding an id would touch the wrong row.
	h := NewHeap()
	ids := fill(t, h, 5)

	if err := h.Delete(ids[2]); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if i == 2 {
			continue
		}
		got, ok := h.Get(id)
		if !ok {
			t.Fatalf("row %d vanished after deleting a different row", i)
		}
		if n, _ := got[0].Int(); n != int64(i) {
			t.Errorf("id %d now holds %d, want %d", id, n, i)
		}
	}
}

func TestScanVisitsInsertionOrder(t *testing.T) {
	h := NewHeap()
	fill(t, h, 5)

	var seen []int64
	cursor := h.Scan()
	for cursor.Next() {
		n, _ := cursor.Row()[0].Int()
		seen = append(seen, n)
	}

	for i, n := range seen {
		if n != int64(i) {
			t.Fatalf("scan order = %v, want 0..4", seen)
		}
	}
	if len(seen) != 5 {
		t.Errorf("scan returned %d rows, want 5", len(seen))
	}
}

func TestScanSkipsTombstones(t *testing.T) {
	h := NewHeap()
	ids := fill(t, h, 5)
	for _, i := range []int{0, 2, 4} {
		if err := h.Delete(ids[i]); err != nil {
			t.Fatal(err)
		}
	}

	var seen []int64
	cursor := h.Scan()
	for cursor.Next() {
		n, _ := cursor.Row()[0].Int()
		seen = append(seen, n)
	}
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 3 {
		t.Errorf("scan = %v, want [1 3]", seen)
	}
}

func TestScanIsRepeatable(t *testing.T) {
	// The same heap scanned twice must give the same order, which is what
	// makes a query without ORDER BY still deterministic.
	h := NewHeap()
	fill(t, h, 10)

	first := h.Rows()
	for i := 0; i < 10; i++ {
		next := h.Rows()
		if len(next) != len(first) {
			t.Fatalf("scan %d returned %d rows, want %d", i, len(next), len(first))
		}
		for j := range first {
			if !first[j].Equal(next[j]) {
				t.Fatalf("scan %d differed at row %d: %v vs %v", i, j, first[j], next[j])
			}
		}
	}
}

func TestCursorReportsRowID(t *testing.T) {
	h := NewHeap()
	ids := fill(t, h, 3)

	cursor := h.Scan()
	for i := 0; cursor.Next(); i++ {
		if cursor.RowID() != ids[i] {
			t.Errorf("row %d id = %d, want %d", i, cursor.RowID(), ids[i])
		}
	}
	if cursor.RowID() != InvalidRowID {
		t.Error("cursor did not reset its id at the end of the scan")
	}
}

func TestCursorReset(t *testing.T) {
	h := NewHeap()
	fill(t, h, 3)

	cursor := h.Scan()
	count := 0
	for cursor.Next() {
		count++
	}
	cursor.Reset()
	for cursor.Next() {
		count++
	}
	if count != 6 {
		t.Errorf("two passes visited %d rows, want 6", count)
	}
}

func TestCursorSeesRowsInsertedDuringAScan(t *testing.T) {
	h := NewHeap()
	h.Insert(row(types.Int(0)))

	cursor := h.Scan()
	count := 0
	for cursor.Next() {
		count++
		if count == 1 {
			h.Insert(row(types.Int(1)))
		}
		if count > 10 {
			t.Fatal("scan did not terminate")
		}
	}
	if count != 2 {
		t.Errorf("visited %d rows, want 2", count)
	}
}

func TestVacuum(t *testing.T) {
	h := NewHeap()
	ids := fill(t, h, 5)
	for _, i := range []int{0, 1, 3} {
		if err := h.Delete(ids[i]); err != nil {
			t.Fatal(err)
		}
	}

	if h.Slots() != 5 {
		t.Errorf("Slots = %d before vacuum, want 5", h.Slots())
	}
	reclaimed := h.Vacuum()
	if reclaimed != 3 {
		t.Errorf("Vacuum reclaimed %d, want 3", reclaimed)
	}
	if h.Slots() != 2 {
		t.Errorf("Slots = %d after vacuum, want 2", h.Slots())
	}
	if h.Len() != 2 {
		t.Errorf("Len = %d after vacuum, want 2", h.Len())
	}

	// The surviving rows keep their values and their order.
	rows := h.Rows()
	first, _ := rows[0][0].Int()
	second, _ := rows[1][0].Int()
	if first != 2 || second != 4 {
		t.Errorf("rows after vacuum = %v, want 2 then 4", rows)
	}
}

func TestVacuumOnACleanHeapDoesNothing(t *testing.T) {
	h := NewHeap()
	fill(t, h, 3)
	if got := h.Vacuum(); got != 0 {
		t.Errorf("Vacuum reclaimed %d on a heap with no tombstones", got)
	}
	if h.Slots() != 3 {
		t.Errorf("Slots = %d, want 3", h.Slots())
	}
}

func TestTruncate(t *testing.T) {
	h := NewHeap()
	fill(t, h, 5)
	h.Truncate()

	if h.Len() != 0 || h.Slots() != 0 {
		t.Errorf("after Truncate: Len = %d, Slots = %d; want 0, 0", h.Len(), h.Slots())
	}
	// Ids start over, which is why the doc says old ids must not be reused.
	id := h.Insert(row(types.Int(9)))
	if id != 1 {
		t.Errorf("first id after Truncate = %d, want 1", id)
	}
}

func TestRowEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b Row
		want bool
	}{
		{"identical", row(types.Int(1), types.Text("a")), row(types.Int(1), types.Text("a")), true},
		{"different values", row(types.Int(1)), row(types.Int(2)), false},
		{"different lengths", row(types.Int(1)), row(types.Int(1), types.Int(2)), false},
		{"empty rows", row(), row(), true},
		{"nulls match", row(types.Null()), row(types.Null()), true},
		{"null against value", row(types.Null()), row(types.Int(1)), false},
		{"int is not float", row(types.Int(1)), row(types.Float(1)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("Equal = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRowString(t *testing.T) {
	r := row(types.Int(1), types.Text("it's"), types.Null())
	if got, want := r.String(), "(1, 'it''s', NULL)"; got != want {
		t.Errorf("String() = %s, want %s", got, want)
	}
}
