package exec

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/parse"
	"github.com/pedrovitti/quarry/internal/types"
)

// Prepared is a statement parsed once and run many times with different
// parameters.
//
// Only the parse is shared. Expressions are compiled per execution, because a
// compiled expression folds its parameters in as constants and the schema it
// resolved against may have changed since. Reusing the parse is still most of
// the win for a statement run in a loop.
type Prepared struct {
	engine *Engine
	stmt   ast.Stmt
	sql    string
	params int
}

// Prepare parses a statement and works out how many parameters it takes.
func (e *Engine) Prepare(sql string) (*Prepared, error) {
	stmt, err := parse.Parse(sql)
	if err != nil {
		return nil, err
	}

	count, err := countParams(stmt)
	if err != nil {
		return nil, err
	}
	return &Prepared{engine: e, stmt: stmt, sql: sql, params: count}, nil
}

// SQL is the statement as it was written.
func (p *Prepared) SQL() string { return p.sql }

// NumParams is how many values Exec expects.
func (p *Prepared) NumParams() int { return p.params }

// Stmt is the parsed statement, for callers that want to inspect it.
func (p *Prepared) Stmt() ast.Stmt { return p.stmt }

// Exec runs the statement with the given parameters, which must number exactly
// NumParams.
func (p *Prepared) Exec(args ...types.Value) (*Result, error) {
	if len(args) != p.params {
		return nil, errorf("statement takes %s, got %d", plural(p.params, "parameter"), len(args))
	}
	return p.engine.Run(p.stmt, args...)
}

// Query runs a prepared SELECT and returns its stream.
func (p *Prepared) Query(args ...types.Value) (Rows, error) {
	if len(args) != p.params {
		return nil, errorf("statement takes %s, got %d", plural(p.params, "parameter"), len(args))
	}
	sel, ok := p.stmt.(*ast.Select)
	if !ok {
		return nil, errorf("Query needs a SELECT")
	}
	return p.engine.buildSelect(sel, args)
}

// countParams returns the number of parameters a statement takes.
//
// Parameters are numbered from 1 and must not skip: $1, $3 with no $2 is
// rejected, because there is no sensible thing for a caller to pass in the
// gap.
func countParams(stmt ast.Stmt) (int, error) {
	seen := map[int]bool{}
	highest := 0

	for _, node := range ast.Inspect(stmt, func(n ast.Node) bool {
		_, ok := n.(*ast.Param)
		return ok
	}) {
		param := node.(*ast.Param)
		if param.Ordinal < 1 {
			return 0, errorf("parameter numbers start at 1")
		}
		seen[param.Ordinal] = true
		if param.Ordinal > highest {
			highest = param.Ordinal
		}
	}

	for i := 1; i <= highest; i++ {
		if !seen[i] {
			return 0, errorf("statement uses $%d but never $%d", highest, i)
		}
	}
	return highest, nil
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
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
