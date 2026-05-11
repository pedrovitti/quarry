// Command quarry is a SQL shell over an in-memory database.
//
//	quarry                  read statements from the terminal
//	quarry -c "SELECT 1"    run one statement and exit
//	quarry script.sql       run a file and exit
//	quarry -f db.sql        load a database first, then read the terminal
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pedrovitti/quarry/internal/cli"
)

func main() {
	command := flag.String("c", "", "run one statement and exit")
	load := flag.String("f", "", "load a database dump before starting")
	echo := flag.Bool("echo", false, "print each statement before running it")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: quarry [-c statement] [-f dump.sql] [-echo] [script.sql]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if err := run(*command, *load, *echo, flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "quarry: %s\n", err)
		os.Exit(1)
	}
}

func run(command, load string, echo bool, args []string) error {
	shell := cli.New(os.Stdout, os.Stderr)
	shell.SetEcho(echo)

	if load != "" {
		dump, err := os.ReadFile(load)
		if err != nil {
			return err
		}
		if err := shell.Engine().Load(string(dump)); err != nil {
			return fmt.Errorf("%s: %s", load, err)
		}
	}

	if command != "" {
		if !shell.RunStatements(command) {
			return errFailed
		}
		return nil
	}

	if len(args) > 0 {
		for _, path := range args {
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !shell.RunStatements(string(source)) {
				return fmt.Errorf("%s failed", path)
			}
		}
		return nil
	}

	// A terminal gets prompts; a pipe does not, so that piped output is just
	// the results.
	return shell.Run(os.Stdin, isTerminal(os.Stdin))
}

// errFailed reports a statement that ran but failed, where the message has
// already been printed.
var errFailed = &silentError{}

type silentError struct{}

func (*silentError) Error() string { return "statement failed" }

// isTerminal reports whether f is an interactive terminal. Checking the mode
// avoids a dependency for the one thing this needs to know.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
