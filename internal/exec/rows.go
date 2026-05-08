package exec

import (
	"sort"

	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// Rows is a stream of result rows.
//
// Next reports false when the stream is done. Operators pull one row at a
// time, so a LIMIT stops the scan under it rather than reading a table and
// discarding most of it.
type Rows interface {
	// Next advances to the next row, returning false at the end.
	Next() (storage.Row, bool, error)
	// Binding describes the columns each row holds.
	Binding() *Binding
}

// Collect drains a stream into a slice. Callers that need every row anyway --
// tests, the CLI, and the operators that must materialize -- use it rather
// than writing the loop again.
func Collect(rows Rows) ([]storage.Row, error) {
	var out []storage.Row
	for {
		row, ok, err := rows.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, row)
	}
}

// scanOp reads a base table in insertion order.
type scanOp struct {
	cursor  *storage.Cursor
	binding *Binding
}

func newScan(heap *storage.Heap, binding *Binding) *scanOp {
	return &scanOp{cursor: heap.Scan(), binding: binding}
}

func (o *scanOp) Binding() *Binding { return o.binding }

func (o *scanOp) Next() (storage.Row, bool, error) {
	if !o.cursor.Next() {
		return nil, false, nil
	}
	return o.cursor.Row(), true, nil
}

// valuesOp yields a fixed set of rows. It is what a SELECT with no FROM runs
// over: exactly one row, with no columns.
type valuesOp struct {
	rows    []storage.Row
	next    int
	binding *Binding
}

func newSingleRow() *valuesOp {
	return &valuesOp{rows: []storage.Row{{}}, binding: NewBinding(nil)}
}

func (o *valuesOp) Binding() *Binding { return o.binding }

func (o *valuesOp) Next() (storage.Row, bool, error) {
	if o.next >= len(o.rows) {
		return nil, false, nil
	}
	row := o.rows[o.next]
	o.next++
	return row, true, nil
}

// filterOp drops rows whose predicate is not true.
type filterOp struct {
	input     Rows
	predicate *Predicate
}

func (o *filterOp) Binding() *Binding { return o.input.Binding() }

func (o *filterOp) Next() (storage.Row, bool, error) {
	for {
		row, ok, err := o.input.Next()
		if err != nil || !ok {
			return nil, false, err
		}
		keep, err := o.predicate.Keep(row)
		if err != nil {
			return nil, false, err
		}
		if keep {
			return row, true, nil
		}
	}
}

// projectOp evaluates the select list.
type projectOp struct {
	input   Rows
	exprs   []Expr
	binding *Binding
}

func (o *projectOp) Binding() *Binding { return o.binding }

func (o *projectOp) Next() (storage.Row, bool, error) {
	row, ok, err := o.input.Next()
	if err != nil || !ok {
		return nil, false, err
	}

	out := make(storage.Row, len(o.exprs))
	for i, expr := range o.exprs {
		v, err := expr.Eval(row)
		if err != nil {
			return nil, false, err
		}
		out[i] = v
	}
	return out, true, nil
}

// limitOp passes through a window of its input. A limit of -1 means no limit,
// which is how OFFSET without LIMIT is expressed.
type limitOp struct {
	input   Rows
	limit   int64
	offset  int64
	skipped int64
	taken   int64
}

func (o *limitOp) Binding() *Binding { return o.input.Binding() }

func (o *limitOp) Next() (storage.Row, bool, error) {
	for o.skipped < o.offset {
		_, ok, err := o.input.Next()
		if err != nil || !ok {
			return nil, false, err
		}
		o.skipped++
	}
	if o.limit >= 0 && o.taken >= o.limit {
		return nil, false, nil
	}

	row, ok, err := o.input.Next()
	if err != nil || !ok {
		return nil, false, err
	}
	o.taken++
	return row, true, nil
}

// distinctOp drops repeated rows, keeping the first occurrence.
//
// It remembers the encoding of every row it has emitted, which is bounded by
// the number of distinct rows rather than by the input size.
type distinctOp struct {
	input Rows
	seen  map[string]bool
}

func (o *distinctOp) Binding() *Binding { return o.input.Binding() }

func (o *distinctOp) Next() (storage.Row, bool, error) {
	if o.seen == nil {
		o.seen = make(map[string]bool)
	}
	for {
		row, ok, err := o.input.Next()
		if err != nil || !ok {
			return nil, false, err
		}
		// Two NULLs count as the same row here. DISTINCT groups NULLs
		// together even though NULL = NULL is unknown -- one of the places
		// SQL treats NULL as a value rather than as an absence.
		key := string(storage.EncodeRow(nil, row))
		if o.seen[key] {
			continue
		}
		o.seen[key] = true
		return row, true, nil
	}
}

// trimOp drops the trailing columns a sort needed but the query did not ask
// for, so "SELECT name FROM t ORDER BY city" does not return the city.
type trimOp struct {
	input   Rows
	keep    int
	binding *Binding
}

func (o *trimOp) Binding() *Binding { return o.binding }

func (o *trimOp) Next() (storage.Row, bool, error) {
	row, ok, err := o.input.Next()
	if err != nil || !ok {
		return nil, false, err
	}
	return row[:o.keep], true, nil
}

// sortKey is one ORDER BY term.
type sortKey struct {
	expr Expr
	desc bool
}

// sortOp orders its input. It has to read everything before it can emit
// anything, which is the cost of ORDER BY.
type sortOp struct {
	input  Rows
	keys   []sortKey
	sorted []storage.Row
	next   int
	done   bool
}

func (o *sortOp) Binding() *Binding { return o.input.Binding() }

func (o *sortOp) Next() (storage.Row, bool, error) {
	if !o.done {
		if err := o.materialize(); err != nil {
			return nil, false, err
		}
		o.done = true
	}
	if o.next >= len(o.sorted) {
		return nil, false, nil
	}
	row := o.sorted[o.next]
	o.next++
	return row, true, nil
}

func (o *sortOp) materialize() error {
	rows, err := Collect(o.input)
	if err != nil {
		return err
	}

	// Sort keys are evaluated once per row rather than inside the comparison,
	// so a sort of n rows evaluates each expression n times and not n log n.
	keys := make([][]types.Value, len(rows))
	for i, row := range rows {
		values := make([]types.Value, len(o.keys))
		for j, key := range o.keys {
			v, err := key.expr.Eval(row)
			if err != nil {
				return err
			}
			values[j] = v
		}
		keys[i] = values
	}

	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}

	// SliceStable so that rows tied on every key keep the order they arrived
	// in, which makes the output of an ambiguous ORDER BY reproducible.
	sort.SliceStable(order, func(a, b int) bool {
		left, right := keys[order[a]], keys[order[b]]
		for i, key := range o.keys {
			cmp := types.SortKey(left[i], right[i])
			if cmp == 0 {
				continue
			}
			if key.desc {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})

	o.sorted = make([]storage.Row, len(rows))
	for i, index := range order {
		o.sorted[i] = rows[index]
	}
	return nil
}

// nestedLoopOp joins two inputs by rescanning the right side for every row of
// the left. The right side is materialized once, since a stream cannot be
// replayed.
type nestedLoopOp struct {
	left      Rows
	right     []storage.Row
	predicate *Predicate
	binding   *Binding

	current storage.Row
	next    int
	started bool
}

func (o *nestedLoopOp) Binding() *Binding { return o.binding }

func (o *nestedLoopOp) Next() (storage.Row, bool, error) {
	for {
		if !o.started || o.next >= len(o.right) {
			row, ok, err := o.left.Next()
			if err != nil || !ok {
				return nil, false, err
			}
			o.current = row
			o.next = 0
			o.started = true
		}

		for o.next < len(o.right) {
			right := o.right[o.next]
			o.next++

			joined := make(storage.Row, 0, len(o.current)+len(right))
			joined = append(joined, o.current...)
			joined = append(joined, right...)

			keep, err := o.predicate.Keep(joined)
			if err != nil {
				return nil, false, err
			}
			if keep {
				return joined, true, nil
			}
		}
	}
}
