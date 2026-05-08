// Package storage keeps the rows of a database and hands them out in a stable
// order.
//
// There is one heap per table. A heap is a slice of slots with tombstones
// rather than a map, so that a scan always visits rows in insertion order:
// with no ORDER BY, SQL leaves row order unspecified, but an engine whose
// output changes between runs is untestable.
package storage

import (
	"fmt"
	"strings"

	"github.com/pedrovitti/quarry/internal/types"
)

// Row is one tuple, with one value per column of the table's schema.
type Row []types.Value

// Clone returns a copy that shares no storage with r.
func (r Row) Clone() Row {
	out := make(Row, len(r))
	copy(out, r)
	return out
}

// Equal reports whether two rows hold identical values. It compares NULL to
// NULL as equal, which SQL's = does not: this is Go equality for tests and
// deduplication, not a predicate.
func (r Row) Equal(other Row) bool {
	if len(r) != len(other) {
		return false
	}
	for i := range r {
		if r[i] != other[i] {
			return false
		}
	}
	return true
}

// String renders the row for debugging and test failures.
func (r Row) String() string {
	parts := make([]string, len(r))
	for i, v := range r {
		parts[i] = v.SQL()
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// Error is a storage error.
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func errorf(format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}
