package storage

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

// populated returns a store with two tables holding a mix of value kinds.
func populated(t *testing.T) *Store {
	t.Helper()
	s := NewStore()

	first, err := s.Create("customers")
	if err != nil {
		t.Fatal(err)
	}
	first.Insert(row(types.Int(1), types.Text("ada"), types.Null()))
	first.Insert(row(types.Int(2), types.Text("bob"), types.Bool(true)))

	second, err := s.Create("amounts")
	if err != nil {
		t.Fatal(err)
	}
	second.Insert(row(types.Float(1.5)))
	second.Insert(row(types.Float(-0.25)))

	return s
}

func TestSnapshotRoundTrip(t *testing.T) {
	original := populated(t)
	restored, err := ReadSnapshot(original.WriteSnapshot())
	if err != nil {
		t.Fatalf("ReadSnapshot returned error: %v", err)
	}

	if got, want := strings.Join(restored.Tables(), ","), strings.Join(original.Tables(), ","); got != want {
		t.Fatalf("tables = %s, want %s", got, want)
	}
	if restored.Rows() != original.Rows() {
		t.Fatalf("rows = %d, want %d", restored.Rows(), original.Rows())
	}

	for _, name := range original.Tables() {
		before, err := original.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		after, err := restored.Get(name)
		if err != nil {
			t.Fatal(err)
		}

		beforeRows, afterRows := before.Rows(), after.Rows()
		if len(beforeRows) != len(afterRows) {
			t.Fatalf("%s: %d rows, want %d", name, len(afterRows), len(beforeRows))
		}
		for i := range beforeRows {
			if !beforeRows[i].Equal(afterRows[i]) {
				t.Errorf("%s row %d = %v, want %v", name, i, afterRows[i], beforeRows[i])
			}
		}
	}
}

func TestSnapshotOfAnEmptyStore(t *testing.T) {
	restored, err := ReadSnapshot(NewStore().WriteSnapshot())
	if err != nil {
		t.Fatalf("ReadSnapshot returned error: %v", err)
	}
	if len(restored.Tables()) != 0 {
		t.Errorf("got %d tables, want none", len(restored.Tables()))
	}
}

func TestSnapshotOfAnEmptyTable(t *testing.T) {
	s := NewStore()
	if _, err := s.Create("t"); err != nil {
		t.Fatal(err)
	}
	restored, err := ReadSnapshot(s.WriteSnapshot())
	if err != nil {
		t.Fatalf("ReadSnapshot returned error: %v", err)
	}
	if !restored.Has("t") {
		t.Error("the empty table was lost")
	}
	if restored.Rows() != 0 {
		t.Errorf("got %d rows, want none", restored.Rows())
	}
}

func TestSnapshotIsDeterministic(t *testing.T) {
	// Two snapshots of the same store must be byte identical, or the checksum
	// would be meaningless as a comparison.
	s := populated(t)
	first := string(s.WriteSnapshot())
	for i := 0; i < 20; i++ {
		if got := string(s.WriteSnapshot()); got != first {
			t.Fatalf("snapshot %d differed from the first", i)
		}
	}
}

func TestSnapshotDropsTombstones(t *testing.T) {
	// A snapshot holds live rows, so restoring one compacts the heap.
	s := NewStore()
	heap, err := s.Create("t")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		heap.Insert(row(types.Int(int64(i))))
	}
	if err := heap.Delete(2); err != nil {
		t.Fatal(err)
	}

	restored, err := ReadSnapshot(s.WriteSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	after, err := restored.Get("t")
	if err != nil {
		t.Fatal(err)
	}
	if after.Slots() != 4 {
		t.Errorf("Slots = %d after restore, want 4", after.Slots())
	}
	if after.Len() != 4 {
		t.Errorf("Len = %d after restore, want 4", after.Len())
	}
}

func TestSnapshotDetectsCorruption(t *testing.T) {
	data := populated(t).WriteSnapshot()

	// Flip a byte in the middle of the body; the checksum must catch it.
	corrupt := make([]byte, len(data))
	copy(corrupt, data)
	corrupt[len(corrupt)/2] ^= 0xFF

	_, err := ReadSnapshot(corrupt)
	if err == nil {
		t.Fatal("a corrupt snapshot was accepted")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error = %q, want it to mention corruption", err)
	}
}

func TestSnapshotDetectsATruncatedFile(t *testing.T) {
	data := populated(t).WriteSnapshot()
	if _, err := ReadSnapshot(data[:len(data)-8]); err == nil {
		t.Error("a truncated snapshot was accepted")
	}
}

func TestSnapshotRejectsForeignData(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantMsg string
	}{
		{"empty", nil, "too short"},
		{"short", []byte("QR"), "too short"},
		{"wrong magic", append([]byte("NOPE\x01"), make([]byte, 8)...), "not a quarry snapshot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadSnapshot(tt.data)
			if err == nil {
				t.Fatal("ReadSnapshot accepted it")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestSnapshotRejectsAnotherVersion(t *testing.T) {
	data := populated(t).WriteSnapshot()
	data[len(snapshotMagic)] = 99

	// Fix the checksum so the version check is what rejects it, not the CRC.
	data = rechecksum(data)

	_, err := ReadSnapshot(data)
	if err == nil {
		t.Fatal("a future version was accepted")
	}
	if !strings.Contains(err.Error(), "version 99") {
		t.Errorf("error = %q, want it to name the version", err)
	}
}

// rechecksum recomputes the trailing CRC of a snapshot whose body was edited.
func rechecksum(data []byte) []byte {
	body := data[:len(data)-4]
	out := make([]byte, 0, len(data))
	out = append(out, body...)
	return appendChecksum(out)
}

func TestSnapshotPreservesValueKinds(t *testing.T) {
	// An integer 1 and a float 1 print the same but must not merge.
	s := NewStore()
	heap, err := s.Create("t")
	if err != nil {
		t.Fatal(err)
	}
	heap.Insert(row(types.Int(1), types.Float(1), types.Text("1"), types.Bool(true), types.Null()))

	restored, err := ReadSnapshot(s.WriteSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	after, _ := restored.Get("t")
	got := after.Rows()[0]

	want := []types.Kind{types.KindInt, types.KindFloat, types.KindText, types.KindBool, types.KindNull}
	for i, kind := range want {
		if got[i].Kind() != kind {
			t.Errorf("column %d kind = %v, want %v", i, got[i].Kind(), kind)
		}
	}
}
