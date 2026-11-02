package logictest

import (
	"strings"
	"testing"

	"github.com/pedrovitti/quarry/internal/exec"
	"github.com/pedrovitti/quarry/internal/types"
)

func aggregateEngine(t *testing.T) *exec.Engine {
	t.Helper()
	engine := exec.New()
	setup := []string{
		"CREATE TABLE sales (id INT NOT NULL, city TEXT, amount INT, region TEXT)",
		"INSERT INTO sales VALUES (1, 'oslo', 5, 'north'), (2, 'lima', 7, 'south')," +
			" (3, 'oslo', 15, 'north'), (4, 'cairo', 3, 'south'), (5, 'lima', 3, 'south')," +
			" (6, 'oslo', 10, 'north'), (7, NULL, 4, 'south'), (8, NULL, 6, 'north')",
		"CREATE TABLE empty (a INT, b TEXT)",
	}
	for _, sql := range setup {
		if _, err := engine.Exec(sql); err != nil {
			t.Fatalf("setup %q: %v", sql, err)
		}
	}
	return engine
}

func mustQuery(t *testing.T, engine *exec.Engine, sql string, params ...types.Value) *exec.Result {
	t.Helper()
	result, err := engine.Exec(sql, params...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return result
}

func TestAggregateHeadingsAreTheWrittenExpression(t *testing.T) {
	engine := aggregateEngine(t)
	result := mustQuery(t, engine,
		"SELECT count(*), avg(amount) AS mean, sum(amount), min(amount) + max(amount) FROM sales")

	want := []string{"count(*)", "mean", "sum(amount)", "min(amount) + max(amount)"}
	if len(result.Columns) != len(want) {
		t.Fatalf("got %d columns %v, want %d", len(result.Columns), result.Columns, len(want))
	}
	for i, name := range want {
		if result.Columns[i] != name {
			t.Errorf("column %d = %q, want %q", i, result.Columns[i], name)
		}
	}
}

func TestAggregateHeadingKeepsTheCaseThatWasWritten(t *testing.T) {
	engine := aggregateEngine(t)
	result := mustQuery(t, engine, "SELECT COUNT(*), Sum(amount) FROM sales")

	if got := strings.Join(result.Columns, "|"); got != "COUNT(*)|Sum(amount)" {
		t.Errorf("columns = %q", got)
	}
}

func TestGroupsComeOutInFirstAppearanceOrder(t *testing.T) {
	engine := aggregateEngine(t)
	const sql = "SELECT city, count(*) FROM sales GROUP BY city"
	const want = "oslo|3\nlima|2\ncairo|1\nNULL|2"

	for run := 0; run < 25; run++ {
		result := mustQuery(t, engine, sql)
		if got := renderRows(result); got != want {
			t.Fatalf("run %d returned\n%s\nwant\n%s", run, got, want)
		}
	}
}

func TestGroupOrderIsUnchangedByHaving(t *testing.T) {
	engine := aggregateEngine(t)
	const sql = "SELECT city, count(*) FROM sales GROUP BY city HAVING count(*) > 1"
	const want = "oslo|3\nlima|2\nNULL|2"

	for run := 0; run < 25; run++ {
		result := mustQuery(t, engine, sql)
		if got := renderRows(result); got != want {
			t.Fatalf("run %d returned\n%s\nwant\n%s", run, got, want)
		}
	}
}

func renderRows(result *exec.Result) string {
	lines := make([]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = v.String()
		}
		lines = append(lines, strings.Join(cells, "|"))
	}
	return strings.Join(lines, "\n")
}

func TestGroupingStreamsThroughQuery(t *testing.T) {
	engine := aggregateEngine(t)
	rows, err := engine.Query("SELECT region, sum(amount) FROM sales GROUP BY region")
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if labels := rows.Binding().Labels(); strings.Join(labels, "|") != "region|sum(amount)" {
		t.Errorf("labels = %v", labels)
	}

	collected, err := exec.Collect(rows)
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if len(collected) != 2 {
		t.Fatalf("got %d rows, want 2", len(collected))
	}
	if got := collected[0][0].String(); got != "north" {
		t.Errorf("first group = %q, want north", got)
	}
	if n, ok := collected[0][1].Int(); !ok || n != 36 {
		t.Errorf("first sum = %v, want 36", collected[0][1])
	}
}

func TestAverageOfIntegersIsAFloat(t *testing.T) {
	engine := aggregateEngine(t)
	result := mustQuery(t, engine, "SELECT avg(amount) FROM sales WHERE city = 'oslo'")

	value := result.Rows[0][0]
	if value.Kind() != types.KindFloat {
		t.Fatalf("avg returned kind %s, want FLOAT", value.Kind())
	}
	if got, _ := value.Float(); got != 10 {
		t.Errorf("avg = %v, want 10", got)
	}
	if got := value.String(); got != "10.0" {
		t.Errorf("avg printed as %q, want 10.0", got)
	}
}

func TestSumOfIntegersStaysAnInteger(t *testing.T) {
	engine := aggregateEngine(t)
	result := mustQuery(t, engine, "SELECT sum(amount) FROM sales")

	value := result.Rows[0][0]
	if value.Kind() != types.KindInt {
		t.Fatalf("sum returned kind %s, want INT", value.Kind())
	}
	if n, _ := value.Int(); n != 53 {
		t.Errorf("sum = %d, want 53", n)
	}
}

func TestSumWidensWhenAFloatEntersIt(t *testing.T) {
	engine := aggregateEngine(t)
	result := mustQuery(t, engine, "SELECT sum(amount + 0.5) FROM sales")

	value := result.Rows[0][0]
	if value.Kind() != types.KindFloat {
		t.Fatalf("sum returned kind %s, want FLOAT", value.Kind())
	}
	if got, _ := value.Float(); got != 57 {
		t.Errorf("sum = %v, want 57", got)
	}
}

func TestSumReportsIntegerOverflow(t *testing.T) {
	engine := exec.New()
	if _, err := engine.Exec("CREATE TABLE big (a INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Exec("INSERT INTO big VALUES (9223372036854775807), (1)"); err != nil {
		t.Fatal(err)
	}

	_, err := engine.Exec("SELECT sum(a) FROM big")
	if err == nil {
		t.Fatal("sum overflowed silently")
	}
	if !strings.Contains(err.Error(), "integer overflow") {
		t.Errorf("error = %q, want it to mention integer overflow", err)
	}
}

func TestUngroupedAggregateOverNoRowsStillAnswers(t *testing.T) {
	engine := aggregateEngine(t)
	result := mustQuery(t, engine, "SELECT count(*), count(a), sum(a), avg(a), min(a), max(b) FROM empty")

	if len(result.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(result.Rows))
	}
	if got := renderRows(result); got != "0|0|NULL|NULL|NULL|NULL" {
		t.Errorf("row = %q", got)
	}
}

func TestGroupedAggregateOverNoRowsReturnsNothing(t *testing.T) {
	engine := aggregateEngine(t)
	result := mustQuery(t, engine, "SELECT a, count(*) FROM empty GROUP BY a")

	if len(result.Rows) != 0 {
		t.Fatalf("got %d rows, want none", len(result.Rows))
	}
	if strings.Join(result.Columns, "|") != "a|count(*)" {
		t.Errorf("columns = %v", result.Columns)
	}
}

func TestHavingWithNoGroupByJudgesTheWholeInput(t *testing.T) {
	engine := aggregateEngine(t)

	kept := mustQuery(t, engine, "SELECT count(*) FROM sales HAVING count(*) > 0")
	if len(kept.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(kept.Rows))
	}

	dropped := mustQuery(t, engine, "SELECT count(*) FROM sales HAVING count(*) > 100")
	if len(dropped.Rows) != 0 {
		t.Fatalf("got %d rows, want none", len(dropped.Rows))
	}
}

func TestPreparedGroupedStatementCountsItsParameters(t *testing.T) {
	engine := aggregateEngine(t)
	prepared, err := engine.Prepare(
		"SELECT city, count(*) AS n FROM sales GROUP BY city, $1 HAVING count(*) > $2")
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if got := prepared.NumParams(); got != 2 {
		t.Fatalf("NumParams = %d, want 2", got)
	}

	result, err := prepared.Exec(types.Int(0), types.Int(2))
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if got := renderRows(result); got != "oslo|3" {
		t.Errorf("rows = %q, want oslo|3", got)
	}
}

func TestPreparedHavingReusesTheStatement(t *testing.T) {
	engine := aggregateEngine(t)
	prepared, err := engine.Prepare(
		"SELECT city, count(*) AS n FROM sales GROUP BY city HAVING count(*) > $1")
	if err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	loose, err := prepared.Exec(types.Int(0))
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if got := renderRows(loose); got != "oslo|3\nlima|2\ncairo|1\nNULL|2" {
		t.Errorf("rows = %q", got)
	}

	tight, err := prepared.Exec(types.Int(2))
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if got := renderRows(tight); got != "oslo|3" {
		t.Errorf("rows = %q", got)
	}
}

func TestScalarMinAndMaxSurviveAtTwoArguments(t *testing.T) {
	engine := aggregateEngine(t)

	scalar := mustQuery(t, engine,
		"SELECT min(amount, 1), max(amount, 100) FROM sales WHERE id = 3")
	if got := renderRows(scalar); got != "1|100" {
		t.Errorf("scalar row = %q, want 1|100", got)
	}

	mixed := mustQuery(t, engine,
		"SELECT min(amount), max(amount), min(max(amount), 1), max(min(amount), 100) FROM sales")
	if got := renderRows(mixed); got != "3|15|1|100" {
		t.Errorf("mixed row = %q, want 3|15|1|100", got)
	}
}

func TestAggregateErrors(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{
			"aggregate in where",
			"SELECT count(*) FROM sales WHERE count(*) > 1",
			"an aggregate is not allowed in WHERE",
		},
		{
			"aggregate in group by",
			"SELECT count(*) FROM sales GROUP BY sum(amount)",
			"an aggregate is not allowed in GROUP BY",
		},
		{
			"aggregate in limit",
			"SELECT count(*) FROM sales LIMIT count(*)",
			"an aggregate is not allowed in LIMIT",
		},
		{
			"nested aggregate",
			"SELECT sum(count(amount)) FROM sales",
			"an aggregate must not contain another aggregate",
		},
		{
			"ungrouped column",
			"SELECT city, count(*) FROM sales",
			`column "city" must appear in GROUP BY or be used in an aggregate`,
		},
		{
			"ungrouped column beside a group key",
			"SELECT city, amount, count(*) FROM sales GROUP BY city",
			`column "amount" must appear in GROUP BY or be used in an aggregate`,
		},
		{
			"ungrouped column in having",
			"SELECT count(*) FROM sales GROUP BY city HAVING amount > 1",
			`column "amount" must appear in GROUP BY or be used in an aggregate`,
		},
		{
			"output name in group by",
			"SELECT city AS c, count(*) FROM sales GROUP BY c",
			`no column "c"`,
		},
		{
			"output name in having",
			"SELECT city, count(*) AS n FROM sales GROUP BY city HAVING n > 1",
			`no column "n"`,
		},
		{
			"having before group by",
			"SELECT city, count(*) FROM sales HAVING count(*) > 1 GROUP BY city",
			"GROUP BY must come before HAVING",
		},
		{
			"sum of text",
			"SELECT sum(city) FROM sales",
			"sum needs a number, got TEXT",
		},
		{
			"avg of text",
			"SELECT avg(city) FROM sales",
			"avg needs a number, got TEXT",
		},
		{
			"count with two arguments",
			"SELECT count(amount, id) FROM sales",
			"count takes 1 argument, got 2",
		},
		{
			"count with no arguments",
			"SELECT count() FROM sales",
			"count takes 1 argument, got 0",
		},
		{
			"star outside count",
			"SELECT sum(*) FROM sales",
			"* is only allowed in count(*)",
		},
		{
			"qualified star in count",
			"SELECT count(sales.*) FROM sales",
			"* is only allowed in count(*)",
		},
		{
			"non boolean having",
			"SELECT count(*) FROM sales HAVING 'yes'",
			"is not a condition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := aggregateEngine(t)
			_, err := engine.Exec(tt.sql)
			if err == nil {
				t.Fatalf("%s succeeded, want an error", tt.sql)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}
