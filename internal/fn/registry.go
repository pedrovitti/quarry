// Package fn holds quarry's scalar functions.
//
// A function declares how many arguments it takes and gets them already
// evaluated. Functions decide for themselves what to do with NULL: most
// propagate it, but coalesce exists precisely to not.
package fn

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pedrovitti/quarry/internal/types"
)

// Error is a call error: an unknown function, the wrong number of arguments,
// or an argument of the wrong kind.
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func errorf(format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// Func is a scalar function.
type Func struct {
	Name string
	// MinArgs and MaxArgs bound the argument count. MaxArgs of -1 means any
	// number, which is what coalesce wants.
	MinArgs int
	MaxArgs int
	Call    func(args []types.Value) (types.Value, error)
}

// registry maps lower-cased names to functions. Function names are matched
// case insensitively, like keywords.
var registry = map[string]*Func{}

func register(f *Func) {
	registry[strings.ToLower(f.Name)] = f
}

// Lookup finds a function by name, reporting false if there is none.
func Lookup(name string) (*Func, bool) {
	f, ok := registry[strings.ToLower(name)]
	return f, ok
}

// Names returns every function name, sorted. Sorted because it feeds error
// messages, and map order would make those differ between runs.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Call looks up a function, checks the argument count, and invokes it.
func Call(name string, args []types.Value) (types.Value, error) {
	f, ok := Lookup(name)
	if !ok {
		return types.Null(), errorf("no function %q; quarry has %s", name, strings.Join(Names(), ", "))
	}
	if err := f.checkArity(len(args)); err != nil {
		return types.Null(), err
	}
	return f.Call(args)
}

func (f *Func) checkArity(n int) error {
	if n < f.MinArgs {
		return errorf("%s takes %s, got %d", f.Name, f.arity(), n)
	}
	if f.MaxArgs >= 0 && n > f.MaxArgs {
		return errorf("%s takes %s, got %d", f.Name, f.arity(), n)
	}
	return nil
}

// arity describes the accepted argument count in prose.
func (f *Func) arity() string {
	switch {
	case f.MaxArgs < 0:
		return fmt.Sprintf("at least %d arguments", f.MinArgs)
	case f.MinArgs == f.MaxArgs && f.MinArgs == 1:
		return "1 argument"
	case f.MinArgs == f.MaxArgs:
		return fmt.Sprintf("%d arguments", f.MinArgs)
	}
	return fmt.Sprintf("%d to %d arguments", f.MinArgs, f.MaxArgs)
}
