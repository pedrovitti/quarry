package cli

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pedrovitti/quarry/internal/exec"
	"github.com/pedrovitti/quarry/internal/types"
)

// Mode is how results are rendered.
type Mode int

const (
	// TableMode draws a bordered table. It is the default, and the only mode
	// meant for reading rather than for piping somewhere.
	TableMode Mode = iota
	// ListMode writes one row per line with a separator, which is what a
	// script wants.
	ListMode
	// CSVMode writes comma separated values with quoting.
	CSVMode
	// JSONMode writes one JSON object per row.
	JSONMode
	// ColumnMode writes fixed-width columns with no borders.
	ColumnMode
)

var modeNames = map[Mode]string{
	TableMode:  "table",
	ListMode:   "list",
	CSVMode:    "csv",
	JSONMode:   "json",
	ColumnMode: "column",
}

func (m Mode) String() string {
	if name, ok := modeNames[m]; ok {
		return name
	}
	return "unknown"
}

// LookupMode resolves a mode by name.
func LookupMode(name string) (Mode, bool) {
	for mode, modeName := range modeNames {
		if modeName == strings.ToLower(name) {
			return mode, true
		}
	}
	return TableMode, false
}

// ModeNames lists the modes, sorted, for .help and error messages.
func ModeNames() []string {
	names := make([]string, 0, len(modeNames))
	for _, name := range modeNames {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Render formats a result in the given mode. separator is used by ListMode.
func Render(result *exec.Result, mode Mode, separator string) string {
	switch mode {
	case ListMode:
		return renderList(result, separator)
	case CSVMode:
		return renderCSV(result)
	case JSONMode:
		return renderJSON(result)
	case ColumnMode:
		return renderColumns(result)
	}
	return Format(result)
}

func renderList(result *exec.Result, separator string) string {
	if separator == "" {
		separator = "|"
	}
	var sb strings.Builder
	sb.WriteString(strings.Join(result.Columns, separator))
	sb.WriteString("\n")
	for _, row := range result.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = v.String()
		}
		sb.WriteString(strings.Join(cells, separator))
		sb.WriteString("\n")
	}
	return sb.String()
}

func renderCSV(result *exec.Result) string {
	var sb strings.Builder
	writeCSVRow(&sb, result.Columns)
	for _, row := range result.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			// An empty CSV field is the natural spelling of NULL, and a
			// quoted empty string is the spelling of an empty string, so the
			// two stay distinguishable.
			if v.IsNull() {
				cells[i] = ""
				continue
			}
			cells[i] = v.String()
		}
		writeCSVRow(&sb, cells)
	}
	return sb.String()
}

func writeCSVRow(sb *strings.Builder, cells []string) {
	for i, cell := range cells {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(quoteCSV(cell))
	}
	sb.WriteString("\n")
}

// quoteCSV quotes a field if it holds a comma, a quote, a newline or leading
// or trailing space, doubling any quotes inside.
func quoteCSV(s string) string {
	needsQuote := strings.ContainsAny(s, `",`+"\n\r") ||
		strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ")
	if !needsQuote {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func renderJSON(result *exec.Result) string {
	var sb strings.Builder
	for _, row := range result.Rows {
		sb.WriteString("{")
		for i, v := range row {
			if i > 0 {
				sb.WriteString(", ")
			}
			if i < len(result.Columns) {
				sb.WriteString(quoteJSON(result.Columns[i]))
			} else {
				sb.WriteString(quoteJSON(fmt.Sprintf("column%d", i+1)))
			}
			sb.WriteString(": ")
			sb.WriteString(jsonValue(v))
		}
		sb.WriteString("}\n")
	}
	return sb.String()
}

// jsonValue renders a value as JSON: NULL is null, numbers and booleans are
// bare, and text is a string. Keeping numbers unquoted is the point of having
// this mode at all.
func jsonValue(v types.Value) string {
	switch {
	case v.IsNull():
		return "null"
	case v.Kind() == types.KindBool, v.Kind().IsNumeric():
		return v.String()
	}
	return quoteJSON(v.String())
}

// runeLen is the display width of a cell, counted in characters.
func runeLen(s string) int { return utf8.RuneCountInString(s) }

func quoteJSON(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				sb.WriteString(fmt.Sprintf(`\u%04x`, r))
				continue
			}
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func renderColumns(result *exec.Result) string {
	cells := make([][]string, 0, len(result.Rows)+1)
	cells = append(cells, result.Columns)
	for _, row := range result.Rows {
		cells = append(cells, renderRow(row))
	}

	widths := columnWidths(cells)
	var sb strings.Builder
	for _, row := range cells {
		for i, cell := range row {
			if i > 0 {
				sb.WriteString("  ")
			}
			sb.WriteString(cell)
			if i < len(row)-1 {
				sb.WriteString(strings.Repeat(" ", widths[i]-runeLen(cell)))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
