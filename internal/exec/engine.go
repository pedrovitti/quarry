package exec

import (
	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/catalog"
	"github.com/pedrovitti/quarry/internal/parse"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// Engine runs statements against a catalog and a store.
//
// It is not safe for concurrent use. One engine is one database.
type Engine struct {
	catalog *catalog.Catalog
	store   *storage.Store
}

// New returns an engine over an empty database.
func New() *Engine {
	return &Engine{catalog: catalog.New(), store: storage.NewStore()}
}

// Catalog exposes the schemas, for callers that introspect.
func (e *Engine) Catalog() *catalog.Catalog { return e.catalog }

// Result is what running a statement produced.
//
// A SELECT fills Columns and Rows. Everything else fills Affected and leaves
// Rows empty, so a caller can tell a query from a command by RowsReturned.
type Result struct {
	Columns []string
	Rows    []storage.Row
	// Affected is the number of rows an INSERT, UPDATE or DELETE touched.
	Affected int
	// Tag names what ran: "SELECT", "INSERT", "CREATE TABLE" and so on.
	Tag string
}

// RowsReturned reports whether the statement produced a result set.
func (r *Result) RowsReturned() bool { return r.Tag == "SELECT" }

// Exec parses and runs a single statement.
func (e *Engine) Exec(sql string, params ...types.Value) (*Result, error) {
	stmt, err := parse.Parse(sql)
	if err != nil {
		return nil, err
	}
	return e.Run(stmt, params...)
}

// ExecAll parses and runs every statement in sql, returning the last result.
// It stops at the first failure, leaving the statements before it applied.
func (e *Engine) ExecAll(sql string, params ...types.Value) ([]*Result, error) {
	stmts, err := parse.ParseAll(sql)
	if err != nil {
		return nil, err
	}
	results := make([]*Result, 0, len(stmts))
	for _, stmt := range stmts {
		result, err := e.Run(stmt, params...)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// Run executes an already-parsed statement.
func (e *Engine) Run(stmt ast.Stmt, params ...types.Value) (*Result, error) {
	switch node := stmt.(type) {
	case *ast.Select:
		return e.runSelect(node, params)
	case *ast.Insert:
		return e.runInsert(node, params)
	case *ast.Update:
		return e.runUpdate(node, params)
	case *ast.Delete:
		return e.runDelete(node, params)
	case *ast.CreateTable:
		return e.runCreateTable(node)
	case *ast.DropTable:
		return e.runDropTable(node)
	}
	return nil, errorf("cannot execute this kind of statement")
}

// Query runs a SELECT and returns its stream, for callers that want to consume
// rows without materializing them.
func (e *Engine) Query(sql string, params ...types.Value) (Rows, error) {
	stmt, err := parse.Parse(sql)
	if err != nil {
		return nil, err
	}
	sel, ok := stmt.(*ast.Select)
	if !ok {
		return nil, errorf("Query needs a SELECT")
	}
	return e.buildSelect(sel, params)
}

func (e *Engine) runSelect(stmt *ast.Select, params []types.Value) (*Result, error) {
	rows, err := e.buildSelect(stmt, params)
	if err != nil {
		return nil, err
	}
	collected, err := Collect(rows)
	if err != nil {
		return nil, err
	}
	return &Result{Tag: "SELECT", Columns: rows.Binding().Labels(), Rows: collected}, nil
}

func (e *Engine) runCreateTable(stmt *ast.CreateTable) (*Result, error) {
	if _, err := e.catalog.CreateFromStmt(stmt); err != nil {
		return nil, errorf("%s", err)
	}
	if _, err := e.store.Create(stmt.Name); err != nil {
		// The catalog entry would otherwise outlive the failure and leave a
		// table that cannot be read.
		_ = e.catalog.Drop(stmt.Name)
		return nil, errorf("%s", err)
	}
	return &Result{Tag: "CREATE TABLE"}, nil
}

func (e *Engine) runDropTable(stmt *ast.DropTable) (*Result, error) {
	if err := e.catalog.Drop(stmt.Name); err != nil {
		return nil, errorf("%s", err)
	}
	if err := e.store.Drop(stmt.Name); err != nil {
		return nil, errorf("%s", err)
	}
	return &Result{Tag: "DROP TABLE"}, nil
}
