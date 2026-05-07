package parse

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/ast"
)

func TestParseInsert(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"single row",
			"INSERT INTO t VALUES (1, 'a')",
			"(insert t (1 'a'))",
		},
		{
			"named columns",
			"INSERT INTO t (id, name) VALUES (1, 'a')",
			"(insert t (id name) (1 'a'))",
		},
		{
			"several rows",
			"INSERT INTO t (id, name) VALUES (1, 'a'), (2, 'b'), (3, NULL)",
			"(insert t (id name) (1 'a') (2 'b') (3 NULL))",
		},
		{
			"expressions as values",
			"INSERT INTO t VALUES (1 + 2, upper('a'))",
			"(insert t ((+ 1 2) (call upper 'a')))",
		},
		{
			"parameters as values",
			"INSERT INTO t VALUES ($1, $2)",
			"(insert t ($1 $2))",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dumpStmt(t, tt.src); got != tt.want {
				t.Errorf("Parse(%q) =\n  %s\nwant\n  %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseInsertErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"missing into", "INSERT t VALUES (1)", "INTO after INSERT"},
		{"missing values", "INSERT INTO t (1)", "a column name"},
		{"missing value list", "INSERT INTO t VALUES", "a parenthesized list of values"},
		{"empty value list", "INSERT INTO t VALUES ()", "expected an expression"},
		{"count mismatch with columns", "INSERT INTO t (a, b) VALUES (1)", "1 values listed but 2 columns named"},
		{"rows of different widths", "INSERT INTO t VALUES (1, 2), (3)", "row 2 has 1 values but row 1 has 2"},
		{"star as a value", "INSERT INTO t VALUES (*)", `unexpected "*"`},
		{"unclosed value list", "INSERT INTO t VALUES (1, 2", "closing parenthesis"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseError(t, tt.src)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Parse(%q) error = %q, want it to contain %q", tt.src, err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestParseUpdate(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"single assignment",
			"UPDATE t SET a = 1",
			"(update t (set a 1))",
		},
		{
			"several assignments",
			"UPDATE t SET a = 1, b = 'x'",
			"(update t (set a 1) (set b 'x'))",
		},
		{
			"assignment from a column",
			"UPDATE t SET a = b + 1",
			"(update t (set a (+ b 1)))",
		},
		{
			"with a where clause",
			"UPDATE t SET a = 1 WHERE id = 7",
			"(update t (set a 1) (where (= id 7)))",
		},
		{
			"where with a compound predicate",
			"UPDATE t SET a = 1 WHERE b > 1 AND c IS NULL",
			"(update t (set a 1) (where (AND (> b 1) (is-null c))))",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dumpStmt(t, tt.src); got != tt.want {
				t.Errorf("Parse(%q) =\n  %s\nwant\n  %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseDelete(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"every row", "DELETE FROM t", "(delete t)"},
		{"with a where clause", "DELETE FROM t WHERE a = 1", "(delete t (where (= a 1)))"},
		{"where is null", "DELETE FROM t WHERE a IS NULL", "(delete t (where (is-null a)))"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dumpStmt(t, tt.src); got != tt.want {
				t.Errorf("Parse(%q) = %s, want %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseDMLErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"update without set", "UPDATE t a = 1", "expected SET"},
		{"update without value", "UPDATE t SET a =", "unexpected end of input"},
		{"update without equals", "UPDATE t SET a 1", "= after the column name"},
		{"delete without from", "DELETE t", "FROM after DELETE"},
		{"delete without table", "DELETE FROM", "a table name"},
		{"delete with reserved table", "DELETE FROM select", "reserved word"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseError(t, tt.src)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Parse(%q) error = %q, want it to contain %q", tt.src, err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestParseCreateTable(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"one column",
			"CREATE TABLE t (id INT)",
			"(create-table t (id INT))",
		},
		{
			"several columns",
			"CREATE TABLE t (id INT, name TEXT, score FLOAT)",
			"(create-table t (id INT) (name TEXT) (score FLOAT))",
		},
		{
			"not null",
			"CREATE TABLE t (id INT NOT NULL, name TEXT)",
			"(create-table t (id INT not-null) (name TEXT))",
		},
		{
			"type aliases normalize",
			"CREATE TABLE t (a INTEGER, b VARCHAR, c BOOLEAN, d DOUBLE)",
			"(create-table t (a INT) (b TEXT) (c BOOL) (d FLOAT))",
		},
		{
			"types are case insensitive",
			"CREATE TABLE t (a int, b Text)",
			"(create-table t (a INT) (b TEXT))",
		},
		{
			"quoted column names",
			`CREATE TABLE t ("order" INT, "select" TEXT)`,
			`(create-table t ("order" INT) ("select" TEXT))`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dumpStmt(t, tt.src); got != tt.want {
				t.Errorf("Parse(%q) =\n  %s\nwant\n  %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestParseDropTable(t *testing.T) {
	if got, want := dumpStmt(t, "DROP TABLE t"), "(drop-table t)"; got != want {
		t.Errorf("Parse = %s, want %s", got, want)
	}
	if got, want := dumpStmt(t, `DROP TABLE "order"`), `(drop-table "order")`; got != want {
		t.Errorf("Parse = %s, want %s", got, want)
	}
}

func TestParseDDLErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"create without table", "CREATE t (a INT)", "TABLE after CREATE"},
		{"create without columns", "CREATE TABLE t", "a parenthesized list of columns"},
		{"create with empty columns", "CREATE TABLE t ()", "a column name"},
		{"column without type", "CREATE TABLE t (a)", "a column type"},
		{"unknown type", "CREATE TABLE t (a BLOB)", `unknown column type "BLOB"`},
		{"unknown type lists the known ones", "CREATE TABLE t (a BLOB)", "BIGINT, BOOL"},
		{"duplicate column", "CREATE TABLE t (a INT, a TEXT)", `column "a" is defined twice`},
		{"not without null", "CREATE TABLE t (a INT NOT)", "NULL after NOT"},
		{"unclosed column list", "CREATE TABLE t (a INT", "closing parenthesis"},
		{"drop without table", "DROP t", "TABLE after DROP"},
		{"drop without name", "DROP TABLE", "a table name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseError(t, tt.src)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Parse(%q) error = %q, want it to contain %q", tt.src, err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestParseAll(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"one statement", "SELECT 1", 1},
		{"one statement with semicolon", "SELECT 1;", 1},
		{"two statements", "SELECT 1; SELECT 2", 2},
		{"trailing semicolon", "SELECT 1; SELECT 2;", 2},
		{"mixed kinds", "CREATE TABLE t (a INT); INSERT INTO t VALUES (1); SELECT * FROM t", 3},
		{"newline separated", "SELECT 1;\nSELECT 2;\n", 2},
		{"empty input", "", 0},
		{"only whitespace", "   \n  ", 0},
		{"only a comment", "-- nothing", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmts, err := ParseAll(tt.src)
			if err != nil {
				t.Fatalf("ParseAll(%q) returned error: %v", tt.src, err)
			}
			if len(stmts) != tt.want {
				t.Errorf("ParseAll(%q) returned %d statements, want %d", tt.src, len(stmts), tt.want)
			}
		})
	}
}

func TestParseAllErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"missing separator", "SELECT 1 SELECT 2", "a semicolon between statements"},
		{"error in the second statement", "SELECT 1; SELECT FROM", "unexpected keyword FROM"},
		{"lone semicolon", ";", "the start of a statement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAll(tt.src)
			if err == nil {
				t.Fatalf("ParseAll(%q) succeeded, want error", tt.src)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestParseRejectsTrailingInput(t *testing.T) {
	// Parse takes exactly one statement, so a second one is an error even
	// though ParseAll would accept it.
	err := parseError(t, "SELECT 1; SELECT 2")
	if !strings.Contains(err.Error(), "end of statement") {
		t.Errorf("error = %q, want it to mention end of statement", err.Error())
	}
}

// TestRoundTrip is the parser's main invariant: printing a tree and parsing
// the result gives back the same tree. It catches printer output that does not
// re-parse, such as a missing quote or a dropped clause.
func TestRoundTrip(t *testing.T) {
	sources := []string{
		"SELECT 1",
		"SELECT * FROM t",
		"SELECT DISTINCT a, b FROM t",
		"SELECT a AS x, b AS y FROM t AS u",
		"SELECT * FROM t WHERE a = 1 AND b <> 2",
		"SELECT * FROM t WHERE NOT a",
		"SELECT * FROM t WHERE a IS NULL OR b IS NOT NULL",
		"SELECT * FROM t WHERE a + 1 > b * 2",
		"SELECT * FROM t WHERE (a + 1) * 2 > 3",
		"SELECT * FROM t WHERE a || b = 'xy'",
		"SELECT * FROM t ORDER BY a DESC, b ASC LIMIT 10 OFFSET 5",
		"SELECT * FROM a JOIN b ON a.id = b.a_id",
		"SELECT * FROM a JOIN b ON a.x = b.x JOIN c ON b.y = c.y",
		"SELECT upper(name), substr(s, 1, 3) FROM t",
		"SELECT t.* FROM t",
		`SELECT "order"."select" FROM "order" AS "from"`,
		"SELECT * FROM t WHERE a = $1 LIMIT $2",
		"SELECT -a, +b, NOT c FROM t",
		"INSERT INTO t VALUES (1, 'a')",
		"INSERT INTO t (id, name) VALUES (1, 'a'), (2, NULL)",
		"INSERT INTO t VALUES (1 + 2, upper('x'))",
		"UPDATE t SET a = 1",
		"UPDATE t SET a = 1, b = 'x' WHERE id = 7",
		"DELETE FROM t",
		"DELETE FROM t WHERE a IS NULL",
		"CREATE TABLE t (id INT NOT NULL, name TEXT, score FLOAT)",
		`CREATE TABLE "order" ("select" INT NOT NULL)`,
		"DROP TABLE t",
		"SELECT 'it''s' FROM t",
		"SELECT * FROM t WHERE s = 'a''b'",
	}
	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			first, err := Parse(src)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", src, err)
			}
			printed := first.String()

			second, err := Parse(printed)
			if err != nil {
				t.Fatalf("re-parsing %q returned error: %v", printed, err)
			}

			if got, want := ast.Dump(second), ast.Dump(first); got != want {
				t.Errorf("round trip changed the tree:\n  source  %s\n  printed %s\n  before  %s\n  after   %s",
					src, printed, want, got)
			}
			if reprinted := second.String(); reprinted != printed {
				t.Errorf("printing is not idempotent:\n  first  %s\n  second %s", printed, reprinted)
			}
		})
	}
}
