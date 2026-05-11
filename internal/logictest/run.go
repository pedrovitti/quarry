package logictest

import (
	"fmt"
	"strings"

	"github.com/pedrovitti/quarry/internal/exec"
)

// Run applies a record to an engine and reports what went wrong, or nil.
func Run(engine *exec.Engine, record Record) error {
	switch record.Kind {
	case StatementOK:
		if _, err := engine.ExecAll(record.SQL); err != nil {
			return fmt.Errorf("statement failed: %s", err)
		}
		return nil

	case StatementError:
		_, err := engine.ExecAll(record.SQL)
		if err == nil {
			return fmt.Errorf("statement succeeded, want an error containing %q", record.Expect)
		}
		if !strings.Contains(err.Error(), record.Expect) {
			return fmt.Errorf("error was %q, want it to contain %q", err, record.Expect)
		}
		return nil

	case Query:
		result, err := engine.Exec(record.SQL)
		if err != nil {
			return fmt.Errorf("query failed: %s", err)
		}
		got := Render(result)
		if got != record.Expect {
			return fmt.Errorf("output differs\n--- got\n%s\n--- want\n%s", got, record.Expect)
		}
		return nil
	}
	return fmt.Errorf("unknown record kind")
}

// Render turns a result into the pipe-separated form the files hold: the
// column names, then one line per row.
//
// Values are rendered the way the CLI prints them, so NULL is spelled out and
// a float always shows a decimal point. That means a test file distinguishes
// an integer 2 from a float 2.0 without needing a type annotation.
func Render(result *exec.Result) string {
	lines := make([]string, 0, len(result.Rows)+1)
	lines = append(lines, strings.Join(result.Columns, "|"))

	for _, row := range result.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = v.String()
		}
		lines = append(lines, strings.Join(cells, "|"))
	}
	return strings.Join(lines, "\n")
}

// RunAll applies every record in order against one engine, stopping at the
// first failure. It is for callers driving a file outside the test harness;
// the harness runs records individually so each is its own subtest.
func RunAll(engine *exec.Engine, records []Record) error {
	for _, record := range records {
		if err := Run(engine, record); err != nil {
			return fmt.Errorf("line %d: %s", record.Line, err)
		}
	}
	return nil
}
