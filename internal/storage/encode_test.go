package storage

import (
	"math"
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/types"
)

func TestEncodeRowRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		row  Row
	}{
		{"empty", row()},
		{"one int", row(types.Int(42))},
		{"negative int", row(types.Int(-42))},
		{"int limits", row(types.Int(math.MaxInt64), types.Int(math.MinInt64))},
		{"float", row(types.Float(1.5))},
		{"float limits", row(types.Float(math.MaxFloat64), types.Float(-math.MaxFloat64))},
		{"tiny float", row(types.Float(math.SmallestNonzeroFloat64))},
		{"text", row(types.Text("hello"))},
		{"empty text", row(types.Text(""))},
		{"text with quotes", row(types.Text("it's \"quoted\""))},
		{"multibyte text", row(types.Text("héllo wörld"))},
		{"text with a null byte", row(types.Text("a\x00b"))},
		{"booleans", row(types.Bool(true), types.Bool(false))},
		{"null", row(types.Null())},
		{"mixed", row(types.Int(1), types.Text("a"), types.Null(), types.Bool(true), types.Float(0.5))},
		{"all nulls", row(types.Null(), types.Null(), types.Null())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeRow(nil, tt.row)
			got, rest, err := DecodeRow(encoded)
			if err != nil {
				t.Fatalf("DecodeRow returned error: %v", err)
			}
			if len(rest) != 0 {
				t.Errorf("DecodeRow left %d bytes over", len(rest))
			}
			if !got.Equal(tt.row) {
				t.Errorf("round trip: got %v, want %v", got, tt.row)
			}
		})
	}
}

func TestEncodeRowPreservesKinds(t *testing.T) {
	// An int and a float with the same numeric value must stay distinct, or a
	// reload would silently change a column's type.
	original := row(types.Int(1), types.Float(1))
	encoded := EncodeRow(nil, original)
	got, _, err := DecodeRow(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Kind() != types.KindInt {
		t.Errorf("first value kind = %v, want KindInt", got[0].Kind())
	}
	if got[1].Kind() != types.KindFloat {
		t.Errorf("second value kind = %v, want KindFloat", got[1].Kind())
	}
}

func TestEncodeRowNaN(t *testing.T) {
	// NaN does not compare equal to itself, so the round trip is checked by
	// bit pattern rather than by Equal.
	encoded := EncodeRow(nil, row(types.Float(math.NaN())))
	got, _, err := DecodeRow(encoded)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := got[0].Float()
	if !ok || !math.IsNaN(f) {
		t.Errorf("NaN did not survive the round trip: got %v", got[0])
	}
}

func TestEncodeRowAppends(t *testing.T) {
	prefix := []byte{0xAA, 0xBB}
	encoded := EncodeRow(prefix, row(types.Int(1)))
	if encoded[0] != 0xAA || encoded[1] != 0xBB {
		t.Error("EncodeRow overwrote the bytes already in dst")
	}

	_, _, err := DecodeRow(encoded[2:])
	if err != nil {
		t.Errorf("the appended row did not decode: %v", err)
	}
}

func TestEncodeRowIsDeterministic(t *testing.T) {
	// The same row must encode to the same bytes every time, or a checksum
	// over a heap would be meaningless.
	r := row(types.Int(1), types.Text("a"), types.Null(), types.Bool(true), types.Float(0.5))
	first := string(EncodeRow(nil, r))
	for i := 0; i < 50; i++ {
		if got := string(EncodeRow(nil, r)); got != first {
			t.Fatalf("encoding %d differed from the first", i)
		}
	}
}

func TestDecodeRowRejectsCorruption(t *testing.T) {
	tests := []struct {
		name    string
		src     []byte
		wantMsg string
	}{
		{"empty", nil, "no column count"},
		{"count but no values", []byte{2}, "no tag"},
		{"truncated integer", []byte{1, tagInt, 0, 0}, "truncated integer"},
		{"truncated float", []byte{1, tagFloat, 0, 0}, "truncated float"},
		{"text with no length", []byte{1, tagText}, "no length"},
		{"text shorter than its length", []byte{1, tagText, 10, 'a'}, "truncated text"},
		{"unknown tag", []byte{1, 99}, "unknown value tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeRow(tt.src)
			if err == nil {
				t.Fatalf("DecodeRow(%v) succeeded, want an error", tt.src)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestDecodeRowDoesNotAllocateOnACorruptCount(t *testing.T) {
	// A huge column count must fail rather than try to allocate for it. The
	// varint here decodes to a very large number with no values behind it.
	src := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x7F}
	if _, _, err := DecodeRow(src); err == nil {
		t.Fatal("a corrupt column count was accepted")
	}
}

func TestEncodeRowsRoundTrip(t *testing.T) {
	rows := []Row{
		row(types.Int(1), types.Text("a")),
		row(types.Int(2), types.Null()),
		row(types.Int(3), types.Text("")),
	}
	got, err := DecodeRows(EncodeRows(rows))
	if err != nil {
		t.Fatalf("DecodeRows returned error: %v", err)
	}
	if len(got) != len(rows) {
		t.Fatalf("got %d rows, want %d", len(got), len(rows))
	}
	for i := range rows {
		if !got[i].Equal(rows[i]) {
			t.Errorf("row %d = %v, want %v", i, got[i], rows[i])
		}
	}
}

func TestEncodeRowsEmpty(t *testing.T) {
	got, err := DecodeRows(EncodeRows(nil))
	if err != nil {
		t.Fatalf("DecodeRows returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d rows, want 0", len(got))
	}
}

func TestDecodeRowsRejectsTrailingBytes(t *testing.T) {
	encoded := EncodeRows([]Row{row(types.Int(1))})
	encoded = append(encoded, 0xFF)

	_, err := DecodeRows(encoded)
	if err == nil {
		t.Fatal("trailing bytes were accepted")
	}
	if !strings.Contains(err.Error(), "trailing") {
		t.Errorf("error = %q, want it to mention trailing bytes", err)
	}
}

func TestDecodeRowsReportsWhichRowFailed(t *testing.T) {
	encoded := EncodeRows([]Row{row(types.Int(1)), row(types.Int(2))})
	// Corrupt the tag of the second row's only value.
	encoded[len(encoded)-9] = 99

	_, err := DecodeRows(encoded)
	if err == nil {
		t.Fatal("corruption was accepted")
	}
	if !strings.Contains(err.Error(), "row 1") {
		t.Errorf("error = %q, want it to name the failing row", err)
	}
}
