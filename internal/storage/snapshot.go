package storage

import (
	"encoding/binary"
	"hash/crc32"
)

// A snapshot is the whole store as bytes:
//
//	"QRRY"      magic
//	uint8       format version
//	uvarint     table count
//	per table:  uvarint name length, name, encoded rows
//	uint32      CRC32 of everything above
//
// The checksum is the reason this exists alongside the SQL dump. A dump is
// readable and portable; a snapshot is exact and detects corruption, which
// matters for a file nobody is going to read.
const (
	snapshotMagic   = "QRRY"
	snapshotVersion = 1
)

// WriteSnapshot serializes the store.
//
// Tables are written in name order so that snapshotting the same store twice
// gives the same bytes, which is what makes the checksum comparable and a
// snapshot diffable.
func (s *Store) WriteSnapshot() []byte {
	var out []byte
	out = append(out, snapshotMagic...)
	out = append(out, snapshotVersion)

	names := s.Tables()
	out = binary.AppendUvarint(out, uint64(len(names)))

	for _, name := range names {
		out = binary.AppendUvarint(out, uint64(len(name)))
		out = append(out, name...)

		heap := s.heaps[name]
		out = append(out, EncodeRows(heap.Rows())...)
	}

	return appendChecksum(out)
}

// appendChecksum closes a snapshot with the CRC32 of everything before it.
func appendChecksum(body []byte) []byte {
	return binary.LittleEndian.AppendUint32(body, crc32.ChecksumIEEE(body))
}

// ReadSnapshot rebuilds a store from bytes written by WriteSnapshot.
func ReadSnapshot(data []byte) (*Store, error) {
	body, err := verifySnapshot(data)
	if err != nil {
		return nil, err
	}

	count, n := binary.Uvarint(body)
	if n <= 0 {
		return nil, errorf("snapshot is truncated: no table count")
	}
	body = body[n:]

	store := NewStore()
	for i := uint64(0); i < count; i++ {
		name, rest, err := readName(body)
		if err != nil {
			return nil, errorf("table %d: %s", i, err)
		}
		body = rest

		rows, rest, err := readRows(body)
		if err != nil {
			return nil, errorf("table %q: %s", name, err)
		}
		body = rest

		heap, err := store.Create(name)
		if err != nil {
			return nil, errorf("table %q: %s", name, err)
		}
		for _, row := range rows {
			heap.Insert(row)
		}
	}

	if len(body) != 0 {
		return nil, errorf("snapshot has %d trailing bytes", len(body))
	}
	return store, nil
}

// verifySnapshot checks the magic, version and checksum, returning the body
// between the header and the checksum.
func verifySnapshot(data []byte) ([]byte, error) {
	const headerLen = len(snapshotMagic) + 1
	const checksumLen = 4

	if len(data) < headerLen+checksumLen {
		return nil, errorf("snapshot is too short to be valid")
	}
	if string(data[:len(snapshotMagic)]) != snapshotMagic {
		return nil, errorf("not a quarry snapshot")
	}

	version := data[len(snapshotMagic)]
	if version != snapshotVersion {
		return nil, errorf("snapshot is version %d; this build reads version %d", version, snapshotVersion)
	}

	split := len(data) - checksumLen
	want := binary.LittleEndian.Uint32(data[split:])
	if got := crc32.ChecksumIEEE(data[:split]); got != want {
		return nil, errorf("snapshot is corrupt: checksum %08x, expected %08x", got, want)
	}
	return data[headerLen:split], nil
}

// readName reads a length-prefixed table name.
func readName(src []byte) (string, []byte, error) {
	length, n := binary.Uvarint(src)
	if n <= 0 {
		return "", nil, errorf("truncated name")
	}
	src = src[n:]
	if uint64(len(src)) < length {
		return "", nil, errorf("truncated name: want %d bytes, have %d", length, len(src))
	}
	return string(src[:length]), src[length:], nil
}

// readRows reads one table's rows, returning what is left over. It mirrors
// EncodeRows, which writes a count followed by that many rows.
func readRows(src []byte) ([]Row, []byte, error) {
	count, n := binary.Uvarint(src)
	if n <= 0 {
		return nil, nil, errorf("truncated rows: no count")
	}
	src = src[n:]

	rows := make([]Row, 0, min(int(count), 1024))
	for i := uint64(0); i < count; i++ {
		row, rest, err := DecodeRow(src)
		if err != nil {
			return nil, nil, errorf("row %d: %s", i, err)
		}
		rows = append(rows, row)
		src = rest
	}
	return rows, src, nil
}
