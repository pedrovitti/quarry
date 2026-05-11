// Package cli is the terminal front end: it formats results, interprets dot
// commands, and drives the read-eval-print loop.
//
// It is a package rather than a main so that the formatting and the command
// handling can be tested without a terminal.
package cli

import (
	"strings"
	"unicode/utf8"

	"github.com/pedrovitti/quarry/internal/exec"
	"github.com/pedrovitti/quarry/internal/storage"
)

// Format renders a result set as a bordered table:
//
//	 id | name
//	----+------
//	  1 | ada
//
// Numbers are right aligned and everything else is left aligned, so a column
// of figures reads down its digits.
func Format(result *exec.Result) string {
	if len(result.Columns) == 0 {
		return ""
	}

	cells := make([][]string, 0, len(result.Rows)+1)
	cells = append(cells, result.Columns)
	for _, row := range result.Rows {
		cells = append(cells, renderRow(row))
	}

	widths := columnWidths(cells)
	rightAlign := numericColumns(result.Rows, len(result.Columns))

	var sb strings.Builder
	writeRow(&sb, cells[0], widths, nil) // headings always read left to right
	writeRule(&sb, widths)
	for _, row := range cells[1:] {
		writeRow(&sb, row, widths, rightAlign)
	}
	return sb.String()
}

// renderRow turns a row into display strings. NULL is spelled out so an empty
// string and a NULL do not look the same.
func renderRow(row storage.Row) []string {
	out := make([]string, len(row))
	for i, v := range row {
		out[i] = v.String()
	}
	return out
}

// columnWidths returns the display width of the widest cell in each column,
// counted in runes so a multibyte value does not skew the layout.
func columnWidths(cells [][]string) []int {
	if len(cells) == 0 {
		return nil
	}
	widths := make([]int, len(cells[0]))
	for _, row := range cells {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

// numericColumns reports which columns hold only numbers, and so should be
// right aligned. A column with no rows is not numeric, which keeps an empty
// result left aligned rather than arbitrarily.
func numericColumns(rows []storage.Row, count int) []bool {
	if len(rows) == 0 {
		return make([]bool, count)
	}
	numeric := make([]bool, count)
	for i := range numeric {
		numeric[i] = true
	}
	for _, row := range rows {
		for i, v := range row {
			if i >= count {
				break
			}
			// NULL does not decide the alignment either way.
			if !v.IsNull() && !v.Kind().IsNumeric() {
				numeric[i] = false
			}
		}
	}
	return numeric
}

func writeRow(sb *strings.Builder, cells []string, widths []int, rightAlign []bool) {
	for i, cell := range cells {
		if i > 0 {
			sb.WriteString(" | ")
		} else {
			sb.WriteString(" ")
		}
		pad := widths[i] - utf8.RuneCountInString(cell)
		if rightAlign != nil && i < len(rightAlign) && rightAlign[i] {
			sb.WriteString(strings.Repeat(" ", pad))
			sb.WriteString(cell)
			continue
		}
		sb.WriteString(cell)
		if i < len(cells)-1 {
			sb.WriteString(strings.Repeat(" ", pad))
		}
	}
	sb.WriteString("\n")
}

func writeRule(sb *strings.Builder, widths []int) {
	for i, width := range widths {
		if i > 0 {
			sb.WriteString("+")
		}
		sb.WriteString(strings.Repeat("-", width+2))
	}
	sb.WriteString("\n")
}

// Summary is the line printed after a statement: a row count for a query, an
// affected count for a command.
func Summary(result *exec.Result) string {
	if result.RowsReturned() {
		return plural(len(result.Rows), "row")
	}
	switch result.Tag {
	case "INSERT", "UPDATE", "DELETE":
		return result.Tag + " " + plural(result.Affected, "row")
	}
	return result.Tag
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
