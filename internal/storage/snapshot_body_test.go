package storage

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

// A snapshot whose checksum matches can still be structurally wrong: the CRC
// says the bytes are the ones that were written, not that they describe a
// store. These build such files deliberately - editing the body and fixing the
// checksum afterwards - so that the reader's own bounds checks are what has to
// catch them, rather than the CRC standing in for all of it.

// bodyOf returns a snapshot's contents between the header and the checksum.
func bodyOf(data []byte) []byte {
	return data[len(snapshotMagic)+1 : len(data)-4]
}

// snapshotOf wraps a body in a valid header and checksum.
func snapshotOf(body []byte) []byte {
	out := make([]byte, 0, len(body)+9)
	out = append(out, snapshotMagic...)
	out = append(out, snapshotVersion)
	out = append(out, body...)
	return appendChecksum(out)
}

func TestSnapshotRejectsATruncatedBody(t *testing.T) {
	full := bodyOf(populated(t).WriteSnapshot())

	// Every proper prefix of the body is either short of a length, short of
	// the bytes that length promised, or missing rows. None may be read as a
	// store, and none may panic.
	for cut := 0; cut < len(full); cut++ {
		body := make([]byte, cut)
		copy(body, full[:cut])

		store, err := ReadSnapshot(snapshotOf(body))
		if err == nil {
			// A prefix that happens to describe fewer tables is legitimate
			// only if it ends exactly on a boundary and claims that many.
			if got := len(store.Tables()); got != 0 {
				t.Errorf("a body cut to %d bytes read as %d tables", cut, got)
			}
			continue
		}
		if strings.Contains(err.Error(), "panic") {
			t.Fatalf("cut %d: %v", cut, err)
		}
	}
}

func TestSnapshotRejectsAMissingTableCount(t *testing.T) {
	if _, err := ReadSnapshot(snapshotOf(nil)); err == nil {
		t.Fatal("a snapshot with no table count was accepted")
	} else if !strings.Contains(err.Error(), "no table count") {
		t.Errorf("error = %q", err)
	}
}

func TestSnapshotRejectsATruncatedName(t *testing.T) {
	tests := []struct {
		name    string
		body    []byte
		wantMsg string
	}{
		{
			// One table promised, nothing after the count.
			"nothing after the count",
			binary.AppendUvarint(nil, 1),
			"truncated name",
		},
		{
			// A name length of 10 with three bytes behind it.
			"a name shorter than its length says",
			append(binary.AppendUvarint(binary.AppendUvarint(nil, 1), 10), "abc"...),
			"truncated name: want 10 bytes, have 3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadSnapshot(snapshotOf(tt.body))
			if err == nil {
				t.Fatal("ReadSnapshot accepted it")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
			// The message names which table went wrong, since a snapshot may
			// hold many.
			if !strings.Contains(err.Error(), "table 0") {
				t.Errorf("error = %q, want it to name the table", err)
			}
		})
	}
}

func TestSnapshotRejectsMissingRows(t *testing.T) {
	// A well-formed name followed by nothing: the row count is missing.
	body := binary.AppendUvarint(nil, 1)
	body = binary.AppendUvarint(body, 2)
	body = append(body, "ab"...)

	_, err := ReadSnapshot(snapshotOf(body))
	if err == nil {
		t.Fatal("a table with no row count was accepted")
	}
	if !strings.Contains(err.Error(), "no count") {
		t.Errorf("error = %q", err)
	}
	if !strings.Contains(err.Error(), `table "ab"`) {
		t.Errorf("error = %q, want it to name the table", err)
	}
}

func TestSnapshotRejectsFewerRowsThanPromised(t *testing.T) {
	// Claim three rows and supply one.
	body := binary.AppendUvarint(nil, 1)
	body = binary.AppendUvarint(body, 1)
	body = append(body, "t"...)
	body = binary.AppendUvarint(body, 3)
	body = append(body, EncodeRow(nil, row(types.Int(1)))...)

	_, err := ReadSnapshot(snapshotOf(body))
	if err == nil {
		t.Fatal("a short table was accepted")
	}
	if !strings.Contains(err.Error(), "row 1") {
		t.Errorf("error = %q, want it to say which row ran out", err)
	}
}

func TestSnapshotRejectsTrailingBytes(t *testing.T) {
	// Everything parses, and then there is more. That means the file is not
	// what it says it is, even though the checksum is right.
	body := append(bodyOf(populated(t).WriteSnapshot()), 0x00)

	_, err := ReadSnapshot(snapshotOf(body))
	if err == nil {
		t.Fatal("a snapshot with trailing bytes was accepted")
	}
	if !strings.Contains(err.Error(), "trailing") {
		t.Errorf("error = %q", err)
	}
}

func TestSnapshotRejectsADuplicateTable(t *testing.T) {
	// Two tables of the same name: the store cannot hold both, and silently
	// keeping one would lose rows.
	one := binary.AppendUvarint(nil, 1)
	one = append(one, "t"...)
	one = binary.AppendUvarint(one, 0)

	body := binary.AppendUvarint(nil, 2)
	body = append(body, one...)
	body = append(body, one...)

	_, err := ReadSnapshot(snapshotOf(body))
	if err == nil {
		t.Fatal("a duplicate table was accepted")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q", err)
	}
}

func TestSnapshotDoesNotAllocateOnAHugeTableCount(t *testing.T) {
	// A count of a billion tables with no bytes behind it must fail on the
	// first missing name rather than reserving anything for the rest.
	body := binary.AppendUvarint(nil, 1<<30)

	if _, err := ReadSnapshot(snapshotOf(body)); err == nil {
		t.Fatal("an impossible table count was accepted")
	}
}

func TestSnapshotWithNoTablesIsValid(t *testing.T) {
	// The one body that is legitimately just a count.
	store, err := ReadSnapshot(snapshotOf(binary.AppendUvarint(nil, 0)))
	if err != nil {
		t.Fatalf("ReadSnapshot returned error: %v", err)
	}
	if got := len(store.Tables()); got != 0 {
		t.Errorf("the store has %d tables, want none", got)
	}
}
