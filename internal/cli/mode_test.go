package cli

import (
	"strings"
	"testing"
)

// modeShell returns a shell in a given mode holding one small table.
func modeShell(t *testing.T, mode Mode) (*Shell, func() (string, string)) {
	t.Helper()
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE t (id INT, name TEXT, ok BOOL, note TEXT)")
	run(t, s, drain, `INSERT INTO t VALUES (1, 'ada', TRUE, NULL), (2, 'b,ob', FALSE, 'has "quote"')`)
	s.SetMode(mode)
	return s, drain
}

func TestLookupMode(t *testing.T) {
	tests := []struct {
		name string
		want Mode
	}{
		{"table", TableMode},
		{"list", ListMode},
		{"csv", CSVMode},
		{"json", JSONMode},
		{"column", ColumnMode},
		{"CSV", CSVMode},
		{"Json", JSONMode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LookupMode(tt.name)
			if !ok {
				t.Fatalf("LookupMode(%q) found nothing", tt.name)
			}
			if got != tt.want {
				t.Errorf("LookupMode(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
	if _, ok := LookupMode("nonsense"); ok {
		t.Error("LookupMode accepted an unknown name")
	}
}

func TestModeNamesAreSorted(t *testing.T) {
	names := ModeNames()
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("ModeNames() is not sorted at %d: %q then %q", i, names[i-1], names[i])
		}
	}
}

func TestCSVMode(t *testing.T) {
	s, drain := modeShell(t, CSVMode)
	got := run(t, s, drain, "SELECT * FROM t")

	want := strings.Join([]string{
		"id,name,ok,note",
		"1,ada,true,",
		`2,"b,ob",false,"has ""quote"""`,
		"",
	}, "\n")
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestCSVModeDistinguishesNullFromEmpty(t *testing.T) {
	// A bare field is NULL and a quoted empty one is the empty string.
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE e (a TEXT, b TEXT)")
	run(t, s, drain, "INSERT INTO e VALUES (NULL, '')")
	s.SetMode(CSVMode)

	got := run(t, s, drain, "SELECT * FROM e")
	if !strings.Contains(got, "\n,\n") {
		t.Errorf("NULL and empty did not render distinguishably:\n%q", got)
	}
}

func TestCSVQuoting(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"has,comma", `"has,comma"`},
		{`has"quote`, `"has""quote"`},
		{"has\nnewline", "\"has\nnewline\""},
		{" leading", `" leading"`},
		{"trailing ", `"trailing "`},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := quoteCSV(tt.in); got != tt.want {
				t.Errorf("quoteCSV(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestJSONMode(t *testing.T) {
	s, drain := modeShell(t, JSONMode)
	got := run(t, s, drain, "SELECT * FROM t")

	want := strings.Join([]string{
		`{"id": 1, "name": "ada", "ok": true, "note": null}`,
		`{"id": 2, "name": "b,ob", "ok": false, "note": "has \"quote\""}`,
		"",
	}, "\n")
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestJSONModeLeavesNumbersUnquoted(t *testing.T) {
	// Quoting them would defeat the point of having the mode.
	s, drain := shell(t)
	run(t, s, drain, "CREATE TABLE n (i INT, f FLOAT, s TEXT)")
	run(t, s, drain, "INSERT INTO n VALUES (1, 1.5, '2')")
	s.SetMode(JSONMode)

	got := run(t, s, drain, "SELECT * FROM n")
	want := `{"i": 1, "f": 1.5, "s": "2"}` + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestQuoteJSONEscapes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain", `"plain"`},
		{`a"b`, `"a\"b"`},
		{`a\b`, `"a\\b"`},
		{"a\nb", `"a\nb"`},
		{"a\tb", `"a\tb"`},
		{"a\rb", `"a\rb"`},
		{"a\x01b", `"a\u0001b"`}, // control characters take the \u form
		{"héllo", `"héllo"`},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := quoteJSON(tt.in); got != tt.want {
				t.Errorf("quoteJSON(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestListMode(t *testing.T) {
	s, drain := modeShell(t, ListMode)
	got := run(t, s, drain, "SELECT id, name FROM t")

	want := "id|name\n1|ada\n2|b,ob\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestListModeSeparator(t *testing.T) {
	s, drain := modeShell(t, ListMode)
	if _, err := s.Meta(".separator ,"); err != nil {
		t.Fatal(err)
	}
	got := run(t, s, drain, "SELECT id, name FROM t")
	if !strings.Contains(got, "1,ada") {
		t.Errorf("the separator was not used: %q", got)
	}
}

func TestColumnMode(t *testing.T) {
	s, drain := modeShell(t, ColumnMode)
	got := run(t, s, drain, "SELECT id, name FROM t")

	if !strings.Contains(got, "id  name") {
		t.Errorf("headings are not aligned:\n%q", got)
	}
	if strings.Contains(got, "|") {
		t.Errorf("column mode drew borders:\n%q", got)
	}
}

func TestMachineReadableModesOmitTheRowCount(t *testing.T) {
	// The trailing "(2 rows)" would make csv and json unparseable.
	for _, mode := range []Mode{CSVMode, JSONMode, ListMode} {
		t.Run(mode.String(), func(t *testing.T) {
			s, drain := modeShell(t, mode)
			got := run(t, s, drain, "SELECT id FROM t")
			if strings.Contains(got, "rows)") {
				t.Errorf("%v mode printed a row count:\n%q", mode, got)
			}
		})
	}
	for _, mode := range []Mode{TableMode, ColumnMode} {
		t.Run(mode.String(), func(t *testing.T) {
			s, drain := modeShell(t, mode)
			got := run(t, s, drain, "SELECT id FROM t")
			if !strings.Contains(got, "(2 rows)") {
				t.Errorf("%v mode omitted the row count:\n%q", mode, got)
			}
		})
	}
}

func TestMetaMode(t *testing.T) {
	s, _ := shell(t)

	got, err := s.Meta(".mode")
	if err != nil {
		t.Fatal(err)
	}
	if got != "mode is table" {
		t.Errorf(".mode reported %q", got)
	}

	if _, err := s.Meta(".mode csv"); err != nil {
		t.Fatal(err)
	}
	if s.Mode() != CSVMode {
		t.Errorf("Mode() = %v after .mode csv", s.Mode())
	}

	_, err = s.Meta(".mode nonsense")
	if err == nil {
		t.Fatal(".mode accepted an unknown name")
	}
	if !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("error = %q", err)
	}
	if !strings.Contains(err.Error(), "csv") {
		t.Errorf("error = %q, want it to list the known modes", err)
	}

	if _, err := s.Meta(".mode csv json"); err == nil {
		t.Error(".mode accepted two names")
	}
}

func TestEmptyResultInEachMode(t *testing.T) {
	// No rows must not panic or emit a stray separator in any mode.
	for _, mode := range []Mode{TableMode, ListMode, CSVMode, JSONMode, ColumnMode} {
		t.Run(mode.String(), func(t *testing.T) {
			s, drain := shell(t)
			run(t, s, drain, "CREATE TABLE e (a INT)")
			s.SetMode(mode)

			got := run(t, s, drain, "SELECT * FROM e")
			if mode == JSONMode {
				if got != "" {
					t.Errorf("json mode wrote %q for no rows", got)
				}
				return
			}
			if !strings.Contains(got, "a") {
				t.Errorf("%v mode lost the heading: %q", mode, got)
			}
		})
	}
}
