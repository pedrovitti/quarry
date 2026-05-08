package storage

import (
	"encoding/binary"
	"math"

	"github.com/pedrovitti/quarry/internal/types"
)

// Row encoding. A row is a count followed by one encoded value per column;
// each value is a one-byte tag and, for the kinds that need it, a payload.
// Integers and floats are fixed width and little endian; text is a length
// prefix followed by its bytes.
//
// The format exists so a heap can be written out and read back byte for byte.
// It is deliberately not self-describing beyond the tags: the schema says what
// the columns mean, and duplicating that here would let the two disagree.
const (
	tagNull  byte = 0
	tagInt   byte = 1
	tagFloat byte = 2
	tagText  byte = 3
	tagFalse byte = 4
	tagTrue  byte = 5
)

// EncodeRow appends the encoding of a row to dst and returns the result.
func EncodeRow(dst []byte, row Row) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(row)))
	for _, v := range row {
		dst = appendValue(dst, v)
	}
	return dst
}

func appendValue(dst []byte, v types.Value) []byte {
	switch v.Kind() {
	case types.KindNull:
		return append(dst, tagNull)

	case types.KindInt:
		n, _ := v.Int()
		dst = append(dst, tagInt)
		return binary.LittleEndian.AppendUint64(dst, uint64(n))

	case types.KindFloat:
		f, _ := v.Float()
		dst = append(dst, tagFloat)
		return binary.LittleEndian.AppendUint64(dst, math.Float64bits(f))

	case types.KindText:
		s, _ := v.Text()
		dst = append(dst, tagText)
		dst = binary.AppendUvarint(dst, uint64(len(s)))
		return append(dst, s...)

	case types.KindBool:
		b, _ := v.Bool()
		if b {
			return append(dst, tagTrue)
		}
		return append(dst, tagFalse)
	}
	return append(dst, tagNull)
}

// DecodeRow reads one row from src, returning it and the bytes left over.
func DecodeRow(src []byte) (Row, []byte, error) {
	count, n := binary.Uvarint(src)
	if n <= 0 {
		return nil, nil, errorf("truncated row: no column count")
	}
	src = src[n:]

	// A corrupt count must not become a huge allocation, so the row is grown
	// as it is read rather than sized up front.
	row := make(Row, 0, min(int(count), 64))
	for i := uint64(0); i < count; i++ {
		v, rest, err := decodeValue(src)
		if err != nil {
			return nil, nil, errorf("column %d: %s", i, err)
		}
		row = append(row, v)
		src = rest
	}
	return row, src, nil
}

func decodeValue(src []byte) (types.Value, []byte, error) {
	if len(src) == 0 {
		return types.Null(), nil, errorf("truncated value: no tag")
	}
	tag, src := src[0], src[1:]

	switch tag {
	case tagNull:
		return types.Null(), src, nil

	case tagInt:
		if len(src) < 8 {
			return types.Null(), nil, errorf("truncated integer")
		}
		n := int64(binary.LittleEndian.Uint64(src))
		return types.Int(n), src[8:], nil

	case tagFloat:
		if len(src) < 8 {
			return types.Null(), nil, errorf("truncated float")
		}
		f := math.Float64frombits(binary.LittleEndian.Uint64(src))
		return types.Float(f), src[8:], nil

	case tagText:
		length, n := binary.Uvarint(src)
		if n <= 0 {
			return types.Null(), nil, errorf("truncated text: no length")
		}
		src = src[n:]
		if uint64(len(src)) < length {
			return types.Null(), nil, errorf("truncated text: want %d bytes, have %d", length, len(src))
		}
		return types.Text(string(src[:length])), src[length:], nil

	case tagFalse:
		return types.Bool(false), src, nil

	case tagTrue:
		return types.Bool(true), src, nil
	}
	return types.Null(), nil, errorf("unknown value tag %d", tag)
}

// EncodeRows encodes a batch of rows.
func EncodeRows(rows []Row) []byte {
	var dst []byte
	dst = binary.AppendUvarint(dst, uint64(len(rows)))
	for _, row := range rows {
		dst = EncodeRow(dst, row)
	}
	return dst
}

// DecodeRows reads a batch written by EncodeRows.
func DecodeRows(src []byte) ([]Row, error) {
	count, n := binary.Uvarint(src)
	if n <= 0 {
		return nil, errorf("truncated batch: no row count")
	}
	src = src[n:]

	rows := make([]Row, 0, min(int(count), 1024))
	for i := uint64(0); i < count; i++ {
		row, rest, err := DecodeRow(src)
		if err != nil {
			return nil, errorf("row %d: %s", i, err)
		}
		rows = append(rows, row)
		src = rest
	}
	if len(src) != 0 {
		return nil, errorf("batch has %d trailing bytes", len(src))
	}
	return rows, nil
}
