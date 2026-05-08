package storage

// Cursor walks the live rows of a heap in insertion order.
//
// A cursor tolerates rows being deleted while it runs: it skips tombstones on
// the way past, so a DELETE that removes rows behind the cursor is invisible
// and one that removes rows ahead of it takes effect. Rows inserted during a
// scan are visited, since they land past the cursor -- callers that must not
// see their own writes take a snapshot first with Rows.
type Cursor struct {
	heap *Heap
	next int // index of the next slot to consider

	id  RowID
	row Row
}

// Next advances to the next live row, reporting false at the end of the heap.
func (c *Cursor) Next() bool {
	for c.next < len(c.heap.slots) {
		i := c.next
		c.next++
		if !c.heap.slots[i].live {
			continue
		}
		c.id = RowID(i + 1)
		c.row = c.heap.slots[i].row
		return true
	}
	c.id = InvalidRowID
	c.row = nil
	return false
}

// Row returns a copy of the current row. It is only valid after Next has
// returned true.
func (c *Cursor) Row() Row { return c.row.Clone() }

// RowID returns the id of the current row.
func (c *Cursor) RowID() RowID { return c.id }

// Reset returns the cursor to the start of the heap, so a nested loop join can
// rescan the inner side without allocating a new cursor per outer row.
func (c *Cursor) Reset() {
	c.next = 0
	c.id = InvalidRowID
	c.row = nil
}
