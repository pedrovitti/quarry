// Package logictest runs SQL test files: plain text holding statements and
// the output they must produce.
//
// A file is a series of records separated by blank lines. Lines beginning with
// # are comments.
//
//	statement ok
//	CREATE TABLE t (a INT, b TEXT)
//
//	statement error must not be NULL
//	INSERT INTO t VALUES (NULL, 'x')
//
//	query
//	SELECT a, b FROM t ORDER BY a
//	----
//	a|b
//	1|x
//
// Keeping the cases in data rather than in Go is what makes them cheap to add:
// a new behaviour is a few lines in a file, and every record becomes its own
// named subtest so a failure names the file and line it came from.
package logictest

import (
	"fmt"
	"strings"
)

// Kind is what a record asserts.
type Kind int

const (
	// StatementOK runs SQL that must succeed. Its output is not checked.
	StatementOK Kind = iota
	// StatementError runs SQL that must fail with a message containing the
	// record's Expect text.
	StatementError
	// Query runs SQL and compares the rendered result against Expect.
	Query
)

func (k Kind) String() string {
	switch k {
	case StatementOK:
		return "statement ok"
	case StatementError:
		return "statement error"
	case Query:
		return "query"
	}
	return "unknown"
}

// Record is one assertion from a test file.
type Record struct {
	Kind Kind
	// SQL is the statement to run.
	SQL string
	// Expect is the awaited output for a Query, or the substring the error
	// must contain for a StatementError.
	Expect string
	// Line is where the record began, for failure messages.
	Line int
	// Label names the record in test output. It is the first line of the SQL,
	// shortened.
	Label string
}

// ParseError reports a malformed test file.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// Parse reads the records in a test file.
func Parse(source string) ([]Record, error) {
	lines := strings.Split(source, "\n")

	var records []Record
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			i++
			continue
		}

		record, next, err := parseRecord(lines, i)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
		i = next
	}
	return records, nil
}

// parseRecord reads one record starting at the directive on line start,
// returning the index of the first line after it.
func parseRecord(lines []string, start int) (Record, int, error) {
	directive := strings.TrimSpace(lines[start])
	record := Record{Line: start + 1}

	switch {
	case directive == "statement ok":
		record.Kind = StatementOK
	case strings.HasPrefix(directive, "statement error"):
		record.Kind = StatementError
		record.Expect = strings.TrimSpace(strings.TrimPrefix(directive, "statement error"))
		if record.Expect == "" {
			return Record{}, 0, &ParseError{
				Line: start + 1,
				Msg:  "statement error needs the text the message must contain",
			}
		}
	case directive == "query":
		record.Kind = Query
	default:
		return Record{}, 0, &ParseError{
			Line: start + 1,
			Msg:  fmt.Sprintf("unknown directive %q", directive),
		}
	}

	// SQL runs until the separator, a blank line, or the end of the file.
	var sql []string
	i := start + 1
	separated := false
	for ; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "----" {
			separated = true
			i++
			break
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		sql = append(sql, line)
	}

	if len(sql) == 0 {
		return Record{}, 0, &ParseError{Line: start + 1, Msg: "record has no SQL"}
	}
	record.SQL = strings.Join(sql, "\n")
	record.Label = label(sql[0])

	if record.Kind == Query {
		if !separated {
			return Record{}, 0, &ParseError{
				Line: start + 1,
				Msg:  "query needs a ---- separator before its expected output",
			}
		}
		var expect []string
		for ; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "" {
				break
			}
			expect = append(expect, lines[i])
		}
		record.Expect = strings.Join(expect, "\n")
	} else if separated {
		return Record{}, 0, &ParseError{
			Line: start + 1,
			Msg:  "only a query record takes a ---- separator",
		}
	}

	return record, i, nil
}

// label shortens the first line of a statement into a subtest name.
func label(line string) string {
	name := strings.TrimSpace(line)
	name = strings.TrimSuffix(name, ";")
	if len(name) > 60 {
		name = name[:60]
	}
	// Slashes would nest the subtest, and spaces become underscores anyway.
	return strings.ReplaceAll(name, "/", " ")
}
