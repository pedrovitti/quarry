package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/pedrovitti/quarry/internal/exec"
	"github.com/pedrovitti/quarry/internal/lex"
	"github.com/pedrovitti/quarry/internal/parse"
)

// Shell runs statements against an engine and writes the results.
type Shell struct {
	engine *exec.Engine
	out    io.Writer
	errOut io.Writer

	// pending holds a statement typed across several lines, until a semicolon
	// finishes it.
	pending strings.Builder
	// echo prints each statement before running it, which is what makes the
	// output of a script readable on its own.
	echo bool

	// mode is how results are rendered, and separator is what list mode puts
	// between fields.
	mode      Mode
	separator string
	// timer prints how long each statement took.
	timer bool
}

// New returns a shell over a fresh database.
func New(out, errOut io.Writer) *Shell {
	return &Shell{engine: exec.New(), out: out, errOut: errOut}
}

// Engine exposes the database, for callers that drive it directly.
func (s *Shell) Engine() *exec.Engine { return s.engine }

// SetEcho turns statement echoing on or off.
func (s *Shell) SetEcho(on bool) { s.echo = on }

// SetMode chooses how results are rendered.
func (s *Shell) SetMode(mode Mode) { s.mode = mode }

// Mode is the current output mode.
func (s *Shell) Mode() Mode { return s.mode }

// Prompt is what the shell would print before the next line: the primary
// prompt normally, a continuation prompt inside an unfinished statement.
func (s *Shell) Prompt() string {
	if s.pending.Len() > 0 {
		return "  ..> "
	}
	return "quarry> "
}

// Feed hands the shell one line of input. It returns ErrQuit when the session
// should end. A line that does not finish a statement is buffered and produces
// no output.
func (s *Shell) Feed(line string) error {
	if s.pending.Len() == 0 {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return nil
		}
		if IsMeta(trimmed) {
			output, err := s.Meta(trimmed)
			if err != nil {
				return err
			}
			if output != "" {
				fmt.Fprintln(s.out, output)
			}
			return nil
		}
	}

	s.pending.WriteString(line)
	s.pending.WriteString("\n")

	if !complete(s.pending.String()) {
		return nil
	}

	statement := s.pending.String()
	s.pending.Reset()
	s.RunStatements(statement)
	return nil
}

// Pending reports whether the shell is part way through a statement.
func (s *Shell) Pending() bool { return s.pending.Len() > 0 }

// Flush runs whatever is buffered, for input that ended without a final
// semicolon.
func (s *Shell) Flush() {
	if s.pending.Len() == 0 {
		return
	}
	statement := s.pending.String()
	s.pending.Reset()
	s.RunStatements(statement)
}

// RunStatements parses and runs every statement in the input, printing each
// result. It reports whether everything succeeded.
func (s *Shell) RunStatements(source string) bool {
	stmts, err := parse.ParseAll(source)
	if err != nil {
		s.reportError(source, err)
		return false
	}

	for _, stmt := range stmts {
		if s.echo {
			fmt.Fprintf(s.out, "%s;\n", stmt)
		}
		started := time.Now()
		result, err := s.engine.Run(stmt)
		if err != nil {
			fmt.Fprintf(s.errOut, "error: %s\n", err)
			return false
		}
		if rendered := Render(result, s.mode, s.separator); rendered != "" {
			fmt.Fprint(s.out, rendered)
		}
		// The trailing count is for a person reading along; the machine
		// readable modes leave it out so their output stays parseable.
		if s.mode == TableMode || s.mode == ColumnMode {
			fmt.Fprintf(s.out, "(%s)\n", Summary(result))
		}
		if s.timer {
			fmt.Fprintf(s.out, "took %s\n", time.Since(started).Round(time.Microsecond))
		}
	}
	return true
}

// reportError prints a parse error with a caret under the offending column
// where the error carries a position.
func (s *Shell) reportError(source string, err error) {
	type annotator interface{ Annotate(string) string }

	if a, ok := err.(annotator); ok {
		fmt.Fprintf(s.errOut, "error: %s\n", a.Annotate(strings.TrimRight(source, "\n")))
		return
	}
	fmt.Fprintf(s.errOut, "error: %s\n", err)
}

// complete reports whether the buffered input holds a finished statement,
// which is to say it ends in a semicolon outside a string, an identifier or a
// comment. Scanning is what decides it: a semicolon inside 'a;b' does not end
// anything.
func complete(source string) bool {
	l := lex.New(source)
	last := lex.Token{Kind: lex.EOF}
	for {
		tok, err := l.Next()
		if err != nil {
			// An unterminated string is an unfinished statement, not a
			// broken one: the user is still typing it.
			return !unterminated(err)
		}
		if tok.Kind == lex.EOF {
			return last.Kind == lex.Semicolon
		}
		last = tok
	}
}

// unterminated reports whether a lexical error is one more input could fix.
func unterminated(err error) bool {
	lexErr, ok := err.(*lex.Error)
	if !ok {
		return false
	}
	return strings.HasPrefix(lexErr.Msg, "unterminated")
}

// Run reads lines from r until it runs out or a command quits.
func (s *Shell) Run(r io.Reader, interactive bool) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for {
		if interactive {
			fmt.Fprint(s.out, s.Prompt())
		}
		if !scanner.Scan() {
			break
		}
		if err := s.Feed(scanner.Text()); err != nil {
			if err == ErrQuit {
				return nil
			}
			fmt.Fprintf(s.errOut, "error: %s\n", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	s.Flush()
	if interactive {
		fmt.Fprintln(s.out)
	}
	return nil
}

func (s *Shell) save(path string) (string, error) {
	if err := os.WriteFile(path, []byte(s.engine.Dump()), 0o644); err != nil {
		return "", err
	}
	return "wrote " + path, nil
}

func (s *Shell) open(path string) (string, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := s.engine.Load(string(source)); err != nil {
		return "", fmt.Errorf("%s: %s", path, err)
	}
	return "opened " + path, nil
}
