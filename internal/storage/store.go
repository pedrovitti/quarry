package storage

import "sort"

// Store is the set of heaps in a database, one per table.
//
// It knows table names but not schemas: the catalog owns those. Keeping the
// two apart is what lets a scan run without consulting metadata it does not
// need.
type Store struct {
	heaps map[string]*Heap
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{heaps: make(map[string]*Heap)}
}

// Create adds an empty heap for a table, reporting an error if one exists.
func (s *Store) Create(table string) (*Heap, error) {
	if _, exists := s.heaps[table]; exists {
		return nil, errorf("storage for table %q already exists", table)
	}
	heap := NewHeap()
	s.heaps[table] = heap
	return heap, nil
}

// Get returns a table's heap.
func (s *Store) Get(table string) (*Heap, error) {
	heap, exists := s.heaps[table]
	if !exists {
		return nil, errorf("no storage for table %q", table)
	}
	return heap, nil
}

// Drop discards a table's rows.
func (s *Store) Drop(table string) error {
	if _, exists := s.heaps[table]; !exists {
		return errorf("no storage for table %q", table)
	}
	delete(s.heaps, table)
	return nil
}

// Rename moves a table's heap to a new name, keeping the rows and their ids.
func (s *Store) Rename(old, new string) error {
	heap, exists := s.heaps[old]
	if !exists {
		return errorf("no storage for table %q", old)
	}
	if _, taken := s.heaps[new]; taken {
		return errorf("storage for table %q already exists", new)
	}
	delete(s.heaps, old)
	s.heaps[new] = heap
	return nil
}

// Has reports whether a table has storage.
func (s *Store) Has(table string) bool {
	_, exists := s.heaps[table]
	return exists
}

// Tables returns every table with storage, sorted by name. Sorted because map
// iteration order is randomized and any output derived from this list would
// otherwise differ between runs.
func (s *Store) Tables() []string {
	names := make([]string, 0, len(s.heaps))
	for name := range s.heaps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Rows is the total number of live rows across every table.
func (s *Store) Rows() int {
	total := 0
	for _, heap := range s.heaps {
		total += heap.Len()
	}
	return total
}
