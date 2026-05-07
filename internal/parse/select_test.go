package parse

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/ast"
)

// dumpStmt parses one statement and returns its S-expression form.
func dumpStmt(t *testing.T, src string) string {
	t.Helper()
	stmt, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) returned error: %v", src, err)
	}
	return ast.Dump(stmt)
}

func TestParseSelect(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"constant",
			"SELECT 1",
			"(select (columns 1))",
		},
		{
			"star",
			"SELECT * FROM t",
			"(select (columns *) (from t))",
		},
		{
			"several columns",
			"SELECT a, b, c FROM t",
			"(select (columns a b c) (from t))",
		},
		{
			"qualified star",
			"SELECT t.* FROM t",
			"(select (columns t.*) (from t))",
		},
		{
			"distinct",
			"SELECT DISTINCT city FROM t",
			"(select distinct (columns city) (from t))",
		},
		{
			"explicit alias",
			"SELECT a AS x FROM t",
			"(select (columns (as a x)) (from t))",
		},
		{
			"implicit alias",
			"SELECT a x FROM t",
			"(select (columns (as a x)) (from t))",
		},
		{
			"table alias",
			"SELECT c.id FROM customers AS c",
			"(select (columns c.id) (from (as customers c)))",
		},
		{
			"implicit table alias",
			"SELECT c.id FROM customers c",
			"(select (columns c.id) (from (as customers c)))",
		},
		{
			"where",
			"SELECT * FROM t WHERE a = 1",
			"(select (columns *) (from t) (where (= a 1)))",
		},
		{
			"order by defaults to ascending",
			"SELECT * FROM t ORDER BY a",
			"(select (columns *) (from t) (order (asc a)))",
		},
		{
			"order by descending",
			"SELECT * FROM t ORDER BY a DESC",
			"(select (columns *) (from t) (order (desc a)))",
		},
		{
			"order by several keys",
			"SELECT * FROM t ORDER BY a DESC, b ASC, c",
			"(select (columns *) (from t) (order (desc a) (asc b) (asc c)))",
		},
		{
			"order by an expression",
			"SELECT * FROM t ORDER BY a + b DESC",
			"(select (columns *) (from t) (order (desc (+ a b))))",
		},
		{
			"limit",
			"SELECT * FROM t LIMIT 10",
			"(select (columns *) (from t) (limit 10))",
		},
		{
			"limit and offset",
			"SELECT * FROM t LIMIT 10 OFFSET 5",
			"(select (columns *) (from t) (limit 10) (offset 5))",
		},
		{
			"offset alone",
			"SELECT * FROM t OFFSET 5",
			"(select (columns *) (from t) (offset 5))",
		},
		{
			"parameters in limit",
			"SELECT * FROM t LIMIT $1 OFFSET $2",
			"(select (columns *) (from t) (limit $1) (offset $2))",
		},
		{
			"every clause at once",
			"SELECT DISTINCT a AS x, b FROM t WHERE a > 1 ORDER BY b DESC LIMIT 10 OFFSET 5",
			"(select distinct (columns (as a x) b) (from t) (where (> a 1)) (order (desc b)) (limit 10) (offset 5))",
		},
		{
			"no from clause",
			"SELECT 1 + 2",
			"(select (columns (+ 1 2)))",
		},
		{
			"function in the select list",
			"SELECT upper(name) AS n FROM t",
			"(select (columns (as (call upper name) n)) (from t))",
		},
		{
			"quoted identifiers",
			`SELECT "order"."select" FROM "order"`,
			`(select (columns "order"."select") (from "order"))`,
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

func TestParseJoin(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"single join",
			"SELECT * FROM a JOIN b ON a.id = b.a_id",
			"(select (columns *) (from (join a b (= a.id b.a_id))))",
		},
		{
			"join with aliases",
			"SELECT * FROM orders o JOIN customers c ON o.customer_id = c.id",
			"(select (columns *) (from (join (as orders o) (as customers c) (= o.customer_id c.id))))",
		},
		{
			"joins associate to the left",
			"SELECT * FROM a JOIN b ON a.x = b.x JOIN c ON b.y = c.y",
			"(select (columns *) (from (join (join a b (= a.x b.x)) c (= b.y c.y))))",
		},
		{
			"compound join condition",
			"SELECT * FROM a JOIN b ON a.x = b.x AND a.y = b.y",
			"(select (columns *) (from (join a b (AND (= a.x b.x) (= a.y b.y)))))",
		},
		{
			"join with a where clause",
			"SELECT * FROM a JOIN b ON a.x = b.x WHERE a.z > 1",
			"(select (columns *) (from (join a b (= a.x b.x))) (where (> a.z 1)))",
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

func TestParseSelectErrors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"no select list", "SELECT FROM t", "unexpected keyword FROM"},
		{"trailing comma", "SELECT a, FROM t", "unexpected keyword FROM"},
		{"missing table", "SELECT * FROM", "a table name"},
		{"missing where predicate", "SELECT * FROM t WHERE", "unexpected end of input"},
		{"order without by", "SELECT * FROM t ORDER a", "expected BY"},
		{"join without on", "SELECT * FROM a JOIN b", "ON followed by a join condition"},
		{"join without condition", "SELECT * FROM a JOIN b ON", "unexpected end of input"},
		{"alias on star", "SELECT * AS everything FROM t", "* cannot be given an alias"},
		{"star inside an expression", "SELECT * + 1 FROM t", "cannot appear inside an expression"},
		{"offset before limit", "SELECT * FROM t OFFSET 5 LIMIT 10", "LIMIT must come before OFFSET"},
		{"reserved word as alias", "SELECT a AS from FROM t", "reserved word"},
		{"missing limit count", "SELECT * FROM t LIMIT", "unexpected end of input"},
		{"two tables without join", "SELECT * FROM a, b", "end of statement"},
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

func TestKeywordsAreCaseInsensitive(t *testing.T) {
	upper := dumpStmt(t, "SELECT DISTINCT a FROM t WHERE b IS NOT NULL ORDER BY a DESC LIMIT 1")
	lower := dumpStmt(t, "select distinct a from t where b is not null order by a desc limit 1")
	mixed := dumpStmt(t, "Select Distinct a From t Where b Is Not Null Order By a Desc Limit 1")

	if upper != lower {
		t.Errorf("upper and lower case parsed differently:\n  %s\n  %s", upper, lower)
	}
	if upper != mixed {
		t.Errorf("upper and mixed case parsed differently:\n  %s\n  %s", upper, mixed)
	}
}

func TestCommentsAreIgnored(t *testing.T) {
	withComments := dumpStmt(t, `
		SELECT a, -- the key
		       b  /* the value */
		FROM t
		WHERE a > 1 -- only the big ones
	`)
	plain := dumpStmt(t, "SELECT a, b FROM t WHERE a > 1")
	if withComments != plain {
		t.Errorf("comments changed the parse:\n  %s\n  %s", withComments, plain)
	}
}
