package storage

// RowID identifies a row within a heap. It stays valid until the row is
// deleted, and is never reused: a deleted slot keeps its tombstone so that an
// iterator holding the id can tell "gone" from "never existed".
type RowID int64

// InvalidRowID is the zero value, which no live row ever has.
const InvalidRowID RowID = 0

type slot struct {
	row  Row
	live bool
}

// Heap stores the rows of one table.
//
// Not safe for concurrent use; the session owning it serializes access.
type Heap struct {
	slots []slot
	live  int
}

// NewHeap returns an empty heap.
func NewHeap() *Heap { return &Heap{} }

// Insert adds a row and returns its id. The row is copied, so the caller may
// reuse the slice it passed.
func (h *Heap) Insert(row Row) RowID {
	h.slots = append(h.slots, slot{row: row.Clone(), live: true})
	h.live++
	return RowID(len(h.slots)) // ids are 1-based so that 0 stays invalid
}

// Get returns a row by id, and false if the id is out of range or the row has
// been deleted.
func (h *Heap) Get(id RowID) (Row, bool) {
	i, ok := h.index(id)
	if !ok || !h.slots[i].live {
		return nil, false
	}
	return h.slots[i].row.Clone(), true
}

// Update replaces a row in place, keeping its id. It reports an error if the
// row is gone, which is how a concurrent delete surfaces to an UPDATE that had
// already matched the row.
func (h *Heap) Update(id RowID, row Row) error {
	i, ok := h.index(id)
	if !ok {
		return errorf("row %d does not exist", id)
	}
	if !h.slots[i].live {
		return errorf("row %d has been deleted", id)
	}
	h.slots[i].row = row.Clone()
	return nil
}

// Delete removes a row, leaving a tombstone. It reports an error if the row
// was already gone.
func (h *Heap) Delete(id RowID) error {
	i, ok := h.index(id)
	if !ok {
		return errorf("row %d does not exist", id)
	}
	if !h.slots[i].live {
		return errorf("row %d has already been deleted", id)
	}
	h.slots[i].row = nil
	h.slots[i].live = false
	h.live--
	return nil
}

// Len is the number of live rows.
func (h *Heap) Len() int { return h.live }

// Slots is the number of slots including tombstones, which is what a scan
// walks. It is the measure Vacuum improves.
func (h *Heap) Slots() int { return len(h.slots) }

// Truncate deletes every row and forgets every tombstone. Ids start over
// afterwards, so no id taken before a truncate may be used after one.
func (h *Heap) Truncate() {
	h.slots = nil
	h.live = 0
}

// Vacuum drops tombstones and renumbers the rows that remain, returning the
// number of slots reclaimed. Every RowID taken before a vacuum is invalid
// afterwards.
func (h *Heap) Vacuum() int {
	if h.live == len(h.slots) {
		return 0
	}
	kept := make([]slot, 0, h.live)
	for _, s := range h.slots {
		if s.live {
			kept = append(kept, s)
		}
	}
	reclaimed := len(h.slots) - len(kept)
	h.slots = kept
	return reclaimed
}

// index converts a RowID to a slot index.
func (h *Heap) index(id RowID) (int, bool) {
	i := int(id) - 1
	if i < 0 || i >= len(h.slots) {
		return 0, false
	}
	return i, true
}

// Scan returns a cursor over the live rows in insertion order.
func (h *Heap) Scan() *Cursor {
	return &Cursor{heap: h, next: 0}
}

// Rows collects every live row in insertion order. It is a convenience for
// tests and for callers that want the whole table at once; the executor uses
// Scan so that it does not materialize.
func (h *Heap) Rows() []Row {
	out := make([]Row, 0, h.live)
	cursor := h.Scan()
	for cursor.Next() {
		out = append(out, cursor.Row())
	}
	return out
}
