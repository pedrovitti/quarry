package logictest

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/exec"
	"github.com/pedrovitti/quarry/internal/types"
)

// The data files cover what the three predicates answer. These cover what they
// are: expressions, so they carry a heading, hold parameters, join two tables
// and reach the same errors from every statement that has a WHERE.

func newWords(t *testing.T) *exec.Engine {
	t.Helper()
	engine := exec.New()
	for _, sql := range []string{
		"CREATE TABLE words (id INT, w TEXT)",
		"INSERT INTO words VALUES (1, 'apple'), (2, 'apricot'), (3, 'banana'), (4, NULL)",
	} {
		if _, err := engine.Exec(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	return engine
}

func queryOne(t *testing.T, engine *exec.Engine, sql string, args ...types.Value) *exec.Result {
	t.Helper()
	result, err := engine.Exec(sql, args...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return result
}

func errorFrom(t *testing.T, engine *exec.Engine, sql string, args ...types.Value) string {
	t.Helper()
	if _, err := engine.Exec(sql, args...); err != nil {
		return err.Error()
	}
	t.Fatalf("%s: succeeded, want an error", sql)
	return ""
}

func rowsOf(t *testing.T, result *exec.Result) []string {
	t.Helper()
	out := make([]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = v.String()
		}
		out = append(out, strings.Join(cells, "|"))
	}
	return out
}

func TestAPredicateIsHeadedByTheSqlThatWroteIt(t *testing.T) {
	tests := []string{
		"w IN ('apple')",
		"w NOT IN ('apple', 'pear')",
		"id IN (1 + 1, 3 * 2)",
		"id IN (id - 1, id + 1)",
	}
	engine := newWords(t)
	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			result := queryOne(t, engine, "SELECT "+expr+" FROM words")
			if len(result.Columns) != 1 {
				t.Fatalf("got %d columns, want 1", len(result.Columns))
			}
			if result.Columns[0] != expr {
				t.Fatalf("heading = %q, want %q", result.Columns[0], expr)
			}

			again := queryOne(t, engine, "SELECT "+result.Columns[0]+" FROM words")
			if again.Columns[0] != expr {
				t.Errorf("the heading did not parse back to itself: %q", again.Columns[0])
			}
		})
	}
}

func TestAPredicateInsideAnExpressionKeepsItsOwnHeading(t *testing.T) {
	engine := newWords(t)
	result := queryOne(t, engine, "SELECT NOT id IN (1), (id IN (1, 2)) AND w IN ('apricot') FROM words")

	want := []string{"NOT id IN (1)", "(id IN (1, 2)) AND w IN ('apricot')"}
	for i, heading := range want {
		if result.Columns[i] != heading {
			t.Errorf("heading %d = %q, want %q", i, result.Columns[i], heading)
		}
	}
}

func TestParametersInsideAPredicateAreCounted(t *testing.T) {
	tests := []struct {
		sql  string
		want int
	}{
		{"SELECT id FROM words WHERE id IN ($1, $2)", 2},
		{"SELECT id FROM words WHERE id NOT IN ($1, $2, $3)", 3},
		{"SELECT id FROM words WHERE id IN ($1 + $2, $3)", 3},
		{"SELECT id FROM words WHERE w IN ($1)", 1},
		{"SELECT $1 IN ($2) FROM words", 2},
	}
	engine := newWords(t)
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			prepared, err := engine.Prepare(tt.sql)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if got := prepared.NumParams(); got != tt.want {
				t.Errorf("NumParams = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAPreparedPredicateRunsAgainWithOtherValues(t *testing.T) {
	engine := newWords(t)
	prepared, err := engine.Prepare("SELECT id FROM words WHERE id IN ($1, $2) ORDER BY id")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	first, err := prepared.Exec(types.Int(1), types.Int(3))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := rowsOf(t, first); strings.Join(got, ",") != "1,3" {
		t.Errorf("first run = %v, want 1,3", got)
	}

	second, err := prepared.Exec(types.Int(2), types.Int(2))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := rowsOf(t, second); strings.Join(got, ",") != "2" {
		t.Errorf("second run = %v, want 2", got)
	}
}

func TestAParameterSettlesTheKindsBeforeAnyRowIsRead(t *testing.T) {
	engine := newWords(t)
	tests := []struct {
		name string
		sql  string
		arg  types.Value
		want string
	}{
		{"a list item of another kind", "SELECT id FROM words WHERE id IN ($1)", types.Text("x"),
			"cannot compare INT with TEXT"},
		{"a subject of another kind", "SELECT id FROM words WHERE $1 IN (id)", types.Text("x"),
			"cannot compare TEXT with INT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorFrom(t, engine, tt.sql, tt.arg); got != tt.want {
				t.Errorf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAParameterThatIsNullLeavesTheKindsOpen(t *testing.T) {
	engine := newWords(t)
	result := queryOne(t, engine, "SELECT id FROM words WHERE w IN ($1)", types.Null())
	if len(result.Rows) != 0 {
		t.Errorf("got %d rows, want none: a NULL item is unknown", len(result.Rows))
	}
}

func TestTheKindsThatCannotBeComparedAreNamedBeforeRunning(t *testing.T) {
	engine := newWords(t)
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"a list of another kind", "SELECT id FROM words WHERE id IN (1, 'x')",
			"cannot compare INT with TEXT"},
		{"a text list against a number", "SELECT id FROM words WHERE id IN ('a', 'b')",
			"cannot compare INT with TEXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorFrom(t, engine, tt.sql); got != tt.want {
				t.Errorf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrefixNotStillReadsTheWholePredicate(t *testing.T) {
	engine := newWords(t)
	result := queryOne(t, engine, "SELECT id FROM words WHERE NOT w IN ('apple', 'apricot') ORDER BY id")
	if got := strings.Join(rowsOf(t, result), ","); got != "3" {
		t.Errorf("rows = %q, want 3: NOT applies to the whole predicate", got)
	}
}

func TestAPredicateJoinsTwoTables(t *testing.T) {
	engine := exec.New()
	for _, sql := range []string{
		"CREATE TABLE stock (sku TEXT, qty INT)",
		"CREATE TABLE wanted (name TEXT, low INT, high INT)",
		"INSERT INTO stock VALUES ('bolt-1', 5), ('bolt-2', 60), ('nut-1', 5)",
		"INSERT INTO wanted VALUES ('bolt-1', 1, 10), ('nut-1', 40, 60)",
	} {
		if _, err := engine.Exec(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	result := queryOne(t, engine,
		"SELECT sku FROM stock JOIN wanted ON sku IN (name) AND qty IN (low, 5) ORDER BY sku")
	if got := strings.Join(rowsOf(t, result), ","); got != "bolt-1,nut-1" {
		t.Errorf("rows = %q, want bolt-1,nut-1", got)
	}

	result = queryOne(t, engine,
		"SELECT sku FROM stock JOIN wanted ON qty IN (low, high) ORDER BY sku")
	if got := strings.Join(rowsOf(t, result), ","); got != "bolt-2" {
		t.Errorf("rows = %q, want bolt-2", got)
	}
}

func TestUpdateAndDeleteChooseRowsTheSameWay(t *testing.T) {
	engine := newWords(t)

	if _, err := engine.Exec("UPDATE words SET w = 'seen' WHERE w IN ('apple', 'apricot')"); err != nil {
		t.Fatalf("UPDATE: %v", err)
	}
	result := queryOne(t, engine, "SELECT w FROM words ORDER BY id")
	if got := strings.Join(rowsOf(t, result), ","); got != "seen,seen,banana,NULL" {
		t.Errorf("after the update: %q", got)
	}

	deleted, err := engine.Exec("DELETE FROM words WHERE id NOT IN (2, 3)")
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	if deleted.Affected != 2 {
		t.Errorf("deleted %d rows, want 2", deleted.Affected)
	}
	if count, err := engine.RowCount("words"); err != nil || count != 2 {
		t.Errorf("RowCount = %d (err %v), want 2", count, err)
	}
}

func TestUnknownKeepsARowOutOfEveryAnswer(t *testing.T) {
	engine := newWords(t)
	tests := []struct {
		name string
		sql  string
	}{
		{"IN", "SELECT id FROM words WHERE w IN ('apple')"},
		{"NOT IN", "SELECT id FROM words WHERE w NOT IN ('apple')"},
		{"IN a longer list", "SELECT id FROM words WHERE w IN ('a', 'b', 'c')"},
		{"NOT IN a longer list", "SELECT id FROM words WHERE w NOT IN ('a', 'b', 'c')"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, row := range rowsOf(t, queryOne(t, engine, tt.sql)) {
				if row == "4" {
					t.Errorf("%s returned the row whose value is NULL", tt.name)
				}
			}
		})
	}
}

func TestTheSameQueryAnswersTheSameWayEveryTime(t *testing.T) {
	engine := newWords(t)
	sql := "SELECT id, w IN ('apple', NULL) AS a, w IN ('apple', 'apricot', 'banana') AS b, id IN (2, 3) AS c FROM words ORDER BY id"

	first := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";")
	for i := 0; i < 10; i++ {
		if got := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";"); got != first {
			t.Fatalf("run %d gave %q, want %q", i, got, first)
		}
	}
	if first != "1|true|true|false;2|NULL|true|true;3|NULL|true|true;4|NULL|NULL|false" {
		t.Errorf("= %q", first)
	}
}

func TestAQuantifiedPredicateIsHeadedByTheSqlThatWroteIt(t *testing.T) {
	tests := []string{
		"id = ANY (1, 2)",
		"id <> ALL (1, 2)",
		"id < ANY (1 + 1, 3)",
	}
	engine := newWords(t)
	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			result := queryOne(t, engine, "SELECT "+expr+" FROM words")
			if len(result.Columns) != 1 {
				t.Fatalf("got %d columns, want 1", len(result.Columns))
			}
			if result.Columns[0] != expr {
				t.Fatalf("heading = %q, want %q", result.Columns[0], expr)
			}

			again := queryOne(t, engine, "SELECT "+result.Columns[0]+" FROM words")
			if again.Columns[0] != expr {
				t.Errorf("the heading did not parse back to itself: %q", again.Columns[0])
			}
		})
	}
}

func TestParametersInsideAQuantifierAreCounted(t *testing.T) {
	tests := []struct {
		sql  string
		want int
	}{
		{"SELECT id FROM words WHERE id = ANY ($1, $2)", 2},
		{"SELECT id FROM words WHERE id < ALL ($1, $2, $3)", 3},
		{"SELECT id FROM words WHERE id > ANY ($1)", 1},
		{"SELECT $1 = ALL ($2) FROM words", 2},
	}
	engine := newWords(t)
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			prepared, err := engine.Prepare(tt.sql)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if got := prepared.NumParams(); got != tt.want {
				t.Errorf("NumParams = %d, want %d", got, tt.want)
			}

			args := make([]types.Value, tt.want)
			for i := range args {
				args[i] = types.Int(1)
			}
			if _, err := prepared.Exec(args...); err != nil {
				t.Errorf("Exec: %v, want the quantifier to be a predicate rather than a call", err)
			}
		})
	}
}

func TestAPreparedQuantifierRunsAgainWithOtherValues(t *testing.T) {
	engine := newWords(t)
	prepared, err := engine.Prepare("SELECT id FROM words WHERE id = ANY ($1, $2) ORDER BY id")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	first, err := prepared.Exec(types.Int(1), types.Int(3))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.Join(rowsOf(t, first), ","); got != "1,3" {
		t.Errorf("first run = %q, want 1,3", got)
	}

	second, err := prepared.Exec(types.Int(4), types.Int(4))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := strings.Join(rowsOf(t, second), ","); got != "4" {
		t.Errorf("second run = %q, want 4", got)
	}
}

func TestAParameterSettlesTheKindsOfAQuantifier(t *testing.T) {
	engine := newWords(t)
	tests := []struct {
		name string
		sql  string
		arg  types.Value
		want string
	}{
		{"an item of another kind", "SELECT id FROM words WHERE id = ANY ($1)", types.Text("x"),
			"cannot compare INT with TEXT"},
		{"a bound of another kind", "SELECT id FROM words WHERE id < ALL ($1)", types.Text("x"),
			"cannot compare INT with TEXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorFrom(t, engine, tt.sql, tt.arg); got != tt.want {
				t.Errorf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnknownKeepsARowOutOfEveryQuantifiedAnswer(t *testing.T) {
	engine := newWords(t)
	tests := []struct {
		name string
		sql  string
	}{
		{"= ANY", "SELECT id FROM words WHERE w = ANY ('apple')"},
		{"<> ALL", "SELECT id FROM words WHERE w <> ALL ('apple')"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, row := range rowsOf(t, queryOne(t, engine, tt.sql)) {
				if row == "4" {
					t.Errorf("%s returned the row whose value is NULL", tt.name)
				}
			}
		})
	}
}

func TestAQuantifiedPredicateJoinsTwoTables(t *testing.T) {
	engine := exec.New()
	for _, sql := range []string{
		"CREATE TABLE stock (sku TEXT, qty INT)",
		"CREATE TABLE wanted (first TEXT, second TEXT, low INT, high INT)",
		"INSERT INTO stock VALUES ('bolt-1', 10), ('nut-1', 40), ('cap-1', 5)",
		"INSERT INTO wanted VALUES ('bolt-1', 'screw-1', 1, 10), ('nut-1', 'cap-1', 40, 60)",
	} {
		if _, err := engine.Exec(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	result := queryOne(t, engine,
		"SELECT sku FROM stock JOIN wanted ON sku = ANY (first, second) AND qty = ANY (low, high) ORDER BY sku")
	if got := strings.Join(rowsOf(t, result), ","); got != "bolt-1,nut-1" {
		t.Errorf("rows = %q, want bolt-1,nut-1", got)
	}
}

func TestUpdateAndDeleteReachTheNewPredicatesToo(t *testing.T) {
	engine := newWords(t)

	if _, err := engine.Exec("UPDATE words SET w = 'seen' WHERE w = ANY ('apple', 'apricot')"); err != nil {
		t.Fatalf("UPDATE: %v", err)
	}
	result := queryOne(t, engine, "SELECT w FROM words ORDER BY id")
	if got := strings.Join(rowsOf(t, result), ","); got != "seen,seen,banana,NULL" {
		t.Errorf("after the update: %q", got)
	}

	deleted, err := engine.Exec("DELETE FROM words WHERE w NOT IN ('seen')")
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	if deleted.Affected != 1 {
		t.Errorf("deleted %d rows, want 1", deleted.Affected)
	}
	if count, err := engine.RowCount("words"); err != nil || count != 3 {
		t.Errorf("RowCount = %d (err %v), want 3", count, err)
	}
}

func TestTheSameQuantifiedQueryAnswersTheSameWayEveryTime(t *testing.T) {
	engine := newWords(t)
	sql := "SELECT id, w = ANY ('apple', NULL) AS a, w <> ALL ('zebra') AS b, " +
		"id = ANY (2, 3) AS c FROM words ORDER BY id"

	first := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";")
	for i := 0; i < 10; i++ {
		if got := strings.Join(rowsOf(t, queryOne(t, engine, sql)), ";"); got != first {
			t.Fatalf("run %d gave %q, want %q", i, got, first)
		}
	}
	if first != "1|true|true|false;2|NULL|true|true;3|NULL|true|true;4|NULL|NULL|false" {
		t.Errorf("= %q", first)
	}
}
