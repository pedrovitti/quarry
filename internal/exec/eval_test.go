package exec

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/ast"
	"github.com/pedrovitti/quarry/internal/parse"
	"github.com/pedrovitti/quarry/internal/storage"
	"github.com/pedrovitti/quarry/internal/types"
)

// compileExpr compiles the expression from "SELECT <expr>" against a binding.
func compileExpr(t *testing.T, sql string, binding *Binding, params ...types.Value) Expr {
	t.Helper()
	compiled, err := compileExprErr(sql, binding, params...)
	if err != nil {
		t.Fatalf("compiling %q: %v", sql, err)
	}
	return compiled
}

func compileExprErr(sql string, binding *Binding, params ...types.Value) (Expr, error) {
	stmt, err := parse.Parse("SELECT " + sql)
	if err != nil {
		return nil, err
	}
	return Compile(stmt.(*ast.Select).Columns[0].Expr, binding, params)
}

// rowBinding describes a row of one column of each kind, for the cases that
// need a column to refer to.
func rowBinding() *Binding {
	return NewBinding([]ColumnInfo{
		{Table: "t", Name: "n", Kind: types.KindInt},
		{Table: "t", Name: "f", Kind: types.KindFloat},
		{Table: "t", Name: "s", Kind: types.KindText},
		{Table: "t", Name: "b", Kind: types.KindBool},
	})
}

// TestCompiledKinds covers the type an expression reports before any row is
// read. The planner uses it to reject comparisons that cannot work, so a kind
// that is wrong here becomes an error at the wrong time - or no error at all.
func TestCompiledKinds(t *testing.T) {
	tests := []struct {
		sql  string
		want types.Kind
	}{
		{"1", types.KindInt},
		{"1.5", types.KindFloat},
		{"'a'", types.KindText},
		{"TRUE", types.KindBool},
		{"NULL", types.KindNull},

		{"n", types.KindInt},
		{"f", types.KindFloat},
		{"s", types.KindText},
		{"b", types.KindBool},

		// Parentheses are transparent: the kind is the inner expression's.
		{"(n)", types.KindInt},
		{"((n))", types.KindInt},

		// Arithmetic keeps the kind where both sides agree and widens where
		// they do not.
		{"n + 1", types.KindInt},
		{"f + 1", types.KindFloat},
		{"n + f", types.KindFloat},
		{"n * n", types.KindInt},

		// A comparison or a logical operator is a boolean whatever it is over.
		{"n = 1", types.KindBool},
		{"s < 'b'", types.KindBool},
		{"b AND b", types.KindBool},
		{"NOT b", types.KindBool},
		{"n IS NULL", types.KindBool},
		{"n IS NOT NULL", types.KindBool},

		// Concatenation is text whatever went in.
		{"s || s", types.KindText},

		// Unary minus keeps the operand's kind, and unary plus with it.
		{"-n", types.KindInt},
		{"-f", types.KindFloat},
		{"+n", types.KindInt},

		// NULL unifies with anything, so an expression involving it reports
		// the other side.
		{"n + NULL", types.KindInt},
		{"NULL + f", types.KindFloat},
		{"NULL + NULL", types.KindNull},

		// A function's result kind is not known before it runs, so it is
		// reported as unknown rather than guessed at.
		{"upper(s)", types.KindNull},
		{"abs(n)", types.KindNull},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			if got := compileExpr(t, tt.sql, rowBinding()).Kind(); got != tt.want {
				t.Errorf("Kind() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompileRejectsArithmeticOnKindsThatCannotUnify(t *testing.T) {
	// Text and a number have no kind in common, and the kinds are known
	// before any row is read, so this fails at compile time rather than
	// per row.
	_, err := compileExprErr("s + n", rowBinding())
	if err == nil {
		t.Fatal("compiled, want an error")
	}
	if !strings.Contains(err.Error(), "cannot apply + to TEXT and INT") {
		t.Errorf("error = %q", err)
	}
}

func TestCompileResolvesColumnsToPositions(t *testing.T) {
	binding := rowBinding()
	row := storage.Row{types.Int(1), types.Float(2.5), types.Text("x"), types.Bool(true)}

	tests := []struct {
		sql  string
		want types.Value
	}{
		{"n", types.Int(1)},
		{"f", types.Float(2.5)},
		{"s", types.Text("x")},
		{"b", types.Bool(true)},
		{"t.n", types.Int(1)},
		{"n + 1", types.Int(2)},
		{"s || 'y'", types.Text("xy")},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			got, err := compileExpr(t, tt.sql, binding).Eval(row)
			if err != nil {
				t.Fatalf("Eval returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompileRejectsWhatItCannotResolve(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{"an unknown column", "nosuch", `no column "nosuch"`},
		{"an unknown qualifier", "x.n", `no column "x.n"`},
		{"an unknown function", "nosuch(1)", `no function "nosuch"`},
		{"a function with the wrong arity", "upper('a', 'b')", "upper takes 1 argument"},
		{"a star", "*", "* cannot be evaluated"},
		{"a qualified star", "t.*", "* cannot be evaluated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileExprErr(tt.sql, rowBinding())
			if err == nil {
				t.Fatal("compiled, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestCompileFoldsParametersInAsConstants(t *testing.T) {
	expr := compileExpr(t, "$1 + $2", rowBinding(), types.Int(2), types.Int(3))
	got, err := expr.Eval(storage.Row{})
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	if got != types.Int(5) {
		t.Errorf("= %v, want 5", got)
	}
	// Folded in as constants, so the kind is known.
	if got := expr.Kind(); got != types.KindInt {
		t.Errorf("Kind() = %v, want INT", got)
	}
}

func TestCompileRejectsAParameterWithNoValue(t *testing.T) {
	_, err := compileExprErr("$2", rowBinding(), types.Int(1))
	if err == nil {
		t.Fatal("compiled, want an error")
	}
	if !strings.Contains(err.Error(), "$2 has no value") {
		t.Errorf("error = %q", err)
	}
}

func TestEvalReportsARowShorterThanTheBinding(t *testing.T) {
	// The binding says four columns; the row has one. This cannot happen
	// through the public API, but the check is what turns a bug into a
	// message rather than an index panic.
	expr := compileExpr(t, "b", rowBinding())
	_, err := expr.Eval(storage.Row{types.Int(1)})
	if err == nil {
		t.Fatal("Eval succeeded on a short row")
	}
	if !strings.Contains(err.Error(), "wanted column 4") {
		t.Errorf("error = %q", err)
	}
}

func TestEvalPropagatesErrorsFromEitherSide(t *testing.T) {
	// A row error - as opposed to a compile error - surfaces from wherever it
	// happened, including out of a function argument or the right operand.
	binding := rowBinding()
	row := storage.Row{types.Int(0), types.Float(0), types.Text("x"), types.Bool(true)}

	for _, sql := range []string{
		"1 / n",
		"n + (1 / n)",
		"(1 / n) + n",
		"abs(1 / n)",
		"-(1 / n)",
	} {
		t.Run(sql, func(t *testing.T) {
			if _, err := compileExpr(t, sql, binding).Eval(row); err == nil {
				t.Error("Eval succeeded, want a division by zero")
			}
		})
	}
}

func TestPredicateTreatsUnknownAsNotKept(t *testing.T) {
	binding := rowBinding()
	stmt, err := parse.Parse("SELECT 1 FROM t WHERE n = 1")
	if err != nil {
		t.Fatal(err)
	}
	predicate, err := CompilePredicate(stmt.(*ast.Select).Where, binding, nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		n    types.Value
		want bool
	}{
		{"true keeps the row", types.Int(1), true},
		{"false drops it", types.Int(2), false},
		{"unknown drops it too", types.Null(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := storage.Row{tt.n, types.Null(), types.Null(), types.Null()}
			got, err := predicate.Keep(row)
			if err != nil {
				t.Fatalf("Keep returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Keep = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPredicateRejectsAConditionThatIsNotABoolean(t *testing.T) {
	binding := rowBinding()
	stmt, err := parse.Parse("SELECT 1 FROM t WHERE s")
	if err != nil {
		t.Fatal(err)
	}
	predicate, err := CompilePredicate(stmt.(*ast.Select).Where, binding, nil)
	if err != nil {
		t.Fatal(err)
	}

	row := storage.Row{types.Int(1), types.Float(1), types.Text("x"), types.Bool(true)}
	_, err = predicate.Keep(row)
	if err == nil {
		t.Fatal("a text condition was accepted")
	}
	if !strings.Contains(err.Error(), "not a condition") {
		t.Errorf("error = %q", err)
	}
}
