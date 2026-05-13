package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/fn"
)

// IsMeta reports whether a line is a dot command rather than SQL.
func IsMeta(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), ".")
}

// ErrQuit is returned by a meta command that ends the session.
var ErrQuit = fmt.Errorf("quit")

// Meta runs a dot command and returns what to print.
func (s *Shell) Meta(line string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return "", nil
	}
	command, args := fields[0], fields[1:]

	switch command {
	case ".quit", ".exit":
		return "", ErrQuit

	case ".help":
		return helpText(), nil

	case ".tables":
		return s.listTables(), nil

	case ".schema":
		return s.showSchema(args)

	case ".functions":
		return strings.Join(fn.Names(), ", "), nil

	case ".mode":
		return s.setMode(args)

	case ".separator":
		if len(args) != 1 {
			return "", fmt.Errorf(".separator takes one string")
		}
		s.separator = args[0]
		return "separator is " + s.separator, nil

	case ".read":
		if len(args) != 1 {
			return "", fmt.Errorf(".read takes a file name")
		}
		return s.readFile(args[0])

	case ".save":
		if len(args) != 1 {
			return "", fmt.Errorf(".save takes a file name")
		}
		return s.save(args[0])

	case ".open":
		if len(args) != 1 {
			return "", fmt.Errorf(".open takes a file name")
		}
		return s.open(args[0])
	}
	return "", fmt.Errorf("unknown command %q; try .help", command)
}

func helpText() string {
	return strings.Join([]string{
		".help              show this message",
		".tables            list the tables",
		".schema [table]    show the schema of one table, or of every table",
		".functions         list the scalar functions",
		".mode MODE         set the output format (" + strings.Join(ModeNames(), ", ") + ")",
		".separator S       set the field separator used by list mode",
		".read FILE         run the statements in a file",
		".save FILE         write the database to a file",
		".open FILE         replace the database with the contents of a file",
		".quit              leave",
	}, "\n")
}

// setMode changes the output format, or reports the current one.
func (s *Shell) setMode(args []string) (string, error) {
	if len(args) == 0 {
		return "mode is " + s.mode.String(), nil
	}
	if len(args) > 1 {
		return "", fmt.Errorf(".mode takes one name")
	}
	mode, ok := LookupMode(args[0])
	if !ok {
		return "", fmt.Errorf("unknown mode %q; quarry has %s",
			args[0], strings.Join(ModeNames(), ", "))
	}
	s.mode = mode
	return "mode is " + mode.String(), nil
}

func (s *Shell) listTables() string {
	names := s.engine.Catalog().Names()
	if len(names) == 0 {
		return "no tables"
	}
	return strings.Join(names, "\n")
}

func (s *Shell) showSchema(args []string) (string, error) {
	catalog := s.engine.Catalog()

	if len(args) == 1 {
		table, err := catalog.Get(args[0])
		if err != nil {
			return "", err
		}
		return createStatement(table.Name, table.Schema.String()), nil
	}
	if len(args) > 1 {
		return "", fmt.Errorf(".schema takes at most one table name")
	}

	tables := catalog.Tables()
	if len(tables) == 0 {
		return "no tables", nil
	}
	lines := make([]string, len(tables))
	for i, table := range tables {
		lines[i] = createStatement(table.Name, table.Schema.String())
	}
	return strings.Join(lines, "\n"), nil
}

func createStatement(name, columns string) string {
	return "CREATE TABLE " + ast.QuoteIdent(name) + " (" + columns + ");"
}

// readFile runs the statements in a file, which is how a schema and its
// fixtures are loaded without pasting them in.
func (s *Shell) readFile(path string) (string, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	results, err := s.engine.ExecAll(string(source))
	if err != nil {
		return "", fmt.Errorf("%s: %s", path, err)
	}
	return plural(len(results), "statement"), nil
}
