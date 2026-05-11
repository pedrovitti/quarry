package exec

import (
	"strings"

	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/catalog"
	"github.com/pedrovitti/quarry/internal/storage"
)

// Dump writes the database as the SQL that would recreate it: a CREATE TABLE
// for each table followed by the INSERTs for its rows.
//
// SQL rather than a binary format because it is the one representation that is
// already specified, already parseable by this engine, and readable when
// something goes wrong. Tables come out in name order and rows in insertion
// order, so dumping the same database twice gives the same bytes.
func (e *Engine) Dump() string {
	var sb strings.Builder

	for _, table := range e.catalog.Tables() {
		sb.WriteString("CREATE TABLE ")
		sb.WriteString(ast.QuoteIdent(table.Name))
		sb.WriteString(" (")
		sb.WriteString(table.Schema.String())
		sb.WriteString(");\n")

		heap, err := e.store.Get(table.Name)
		if err != nil {
			// A table in the catalog with no storage cannot happen through
			// the public API; skipping it keeps Dump total rather than
			// panicking on a database that is already broken.
			continue
		}
		writeInserts(&sb, table, heap)
	}
	return sb.String()
}

// insertBatch is how many rows share one INSERT statement. Batching keeps the
// dump small without producing a single statement the parser has to hold
// entirely in memory.
const insertBatch = 64

func writeInserts(sb *strings.Builder, table *catalog.Table, heap *storage.Heap) {
	rows := heap.Rows()
	if len(rows) == 0 {
		return
	}

	prefix := "INSERT INTO " + ast.QuoteIdent(table.Name) + " (" +
		strings.Join(quoteAll(table.Schema.Names()), ", ") + ") VALUES\n"

	for start := 0; start < len(rows); start += insertBatch {
		end := min(start+insertBatch, len(rows))

		sb.WriteString(prefix)
		for i, row := range rows[start:end] {
			if i > 0 {
				sb.WriteString(",\n")
			}
			sb.WriteString("  (")
			for j, v := range row {
				if j > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(v.SQL())
			}
			sb.WriteString(")")
		}
		sb.WriteString(";\n")
	}
}

func quoteAll(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = ast.QuoteIdent(name)
	}
	return out
}

// Load replaces the contents of the engine with a dump. It runs into a fresh
// database and only swaps it in once everything has applied, so a bad file
// leaves the engine as it was.
func (e *Engine) Load(dump string) error {
	fresh := New()
	if _, err := fresh.ExecAll(dump); err != nil {
		return err
	}
	e.catalog, e.store = fresh.catalog, fresh.store
	return nil
}
