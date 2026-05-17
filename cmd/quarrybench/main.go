// Command quarrybench times the engine over a generated table.
//
//	quarrybench                 run every case at the default size
//	quarrybench -rows 100000    use a larger table
//	quarrybench -case join      run one case
//
// The data is generated from a fixed seed, so two runs measure the same work
// and the numbers can be compared.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/pedrovitti/quarry/internal/exec"
	"github.com/pedrovitti/quarry/internal/types"
)

func main() {
	rows := flag.Int("rows", 20000, "number of rows to generate")
	only := flag.String("case", "", "run only the case with this name")
	repeat := flag.Int("repeat", 3, "how many times to run each case")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: quarrybench [-rows n] [-repeat n] [-case name]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if err := run(*rows, *repeat, *only); err != nil {
		fmt.Fprintf(os.Stderr, "quarrybench: %s\n", err)
		os.Exit(1)
	}
}

// benchCase is one measured query.
type benchCase struct {
	name string
	sql  string
}

func cases() []benchCase {
	return []benchCase{
		{"scan", "SELECT * FROM facts"},
		{"filter", "SELECT id FROM facts WHERE bucket = 7"},
		{"filter-compound", "SELECT id FROM facts WHERE bucket = 7 AND amount > 500.0"},
		{"project-expr", "SELECT id, amount * 2 + 1 FROM facts"},
		{"function", "SELECT upper(label) FROM facts"},
		{"sort", "SELECT id FROM facts ORDER BY amount"},
		{"sort-two-keys", "SELECT id FROM facts ORDER BY bucket, amount DESC"},
		{"distinct", "SELECT DISTINCT bucket FROM facts"},
		{"limit", "SELECT id FROM facts ORDER BY amount LIMIT 10"},
		{"join", "SELECT f.id, b.name FROM facts f JOIN buckets b ON f.bucket = b.id"},
		{"join-filtered", "SELECT f.id FROM facts f JOIN buckets b ON f.bucket = b.id WHERE b.name = 'bucket-3'"},
	}
}

func run(rows, repeat int, only string) error {
	if rows < 1 {
		return fmt.Errorf("-rows must be at least 1")
	}
	if repeat < 1 {
		return fmt.Errorf("-repeat must be at least 1")
	}

	engine := exec.New()
	loadStart := time.Now()
	if err := generate(engine, rows); err != nil {
		return err
	}
	loadTook := time.Since(loadStart)

	fmt.Printf("%d rows loaded in %s\n\n", rows, round(loadTook))
	fmt.Printf("%-18s %10s %10s %8s\n", "case", "best", "median", "rows")
	fmt.Println(strings.Repeat("-", 50))

	for _, bench := range cases() {
		if only != "" && bench.name != only {
			continue
		}
		took, count, err := measure(engine, bench.sql, repeat)
		if err != nil {
			return fmt.Errorf("%s: %s", bench.name, err)
		}
		fmt.Printf("%-18s %10s %10s %8d\n", bench.name, round(took[0]), round(median(took)), count)
	}
	return nil
}

// measure runs a query repeat times and returns the timings, sorted fastest
// first, along with how many rows it produced.
func measure(engine *exec.Engine, sql string, repeat int) ([]time.Duration, int, error) {
	prepared, err := engine.Prepare(sql)
	if err != nil {
		return nil, 0, err
	}

	timings := make([]time.Duration, 0, repeat)
	count := 0
	for i := 0; i < repeat; i++ {
		start := time.Now()
		rows, err := prepared.Query()
		if err != nil {
			return nil, 0, err
		}
		collected, err := exec.Collect(rows)
		if err != nil {
			return nil, 0, err
		}
		timings = append(timings, time.Since(start))
		count = len(collected)
	}

	sort.Slice(timings, func(a, b int) bool { return timings[a] < timings[b] })
	return timings, count, nil
}

func median(sorted []time.Duration) time.Duration {
	return sorted[len(sorted)/2]
}

// round trims a duration to something readable at a glance.
func round(d time.Duration) time.Duration {
	switch {
	case d > time.Second:
		return d.Round(10 * time.Millisecond)
	case d > time.Millisecond:
		return d.Round(10 * time.Microsecond)
	}
	return d.Round(time.Microsecond)
}

// generate fills the database. Values come from a small linear congruential
// generator rather than math/rand so the data is identical on every run and
// between Go versions.
func generate(engine *exec.Engine, rows int) error {
	setup := []string{
		"CREATE TABLE buckets (id INT NOT NULL, name TEXT)",
		"CREATE TABLE facts (id INT NOT NULL, bucket INT, amount FLOAT, label TEXT)",
	}
	for _, sql := range setup {
		if _, err := engine.Exec(sql); err != nil {
			return err
		}
	}

	insertBucket, err := engine.Prepare("INSERT INTO buckets VALUES ($1, $2)")
	if err != nil {
		return err
	}
	for i := 0; i < buckets; i++ {
		name := fmt.Sprintf("bucket-%d", i)
		if _, err := insertBucket.Exec(types.Int(int64(i)), types.Text(name)); err != nil {
			return err
		}
	}

	insertFact, err := engine.Prepare("INSERT INTO facts VALUES ($1, $2, $3, $4)")
	if err != nil {
		return err
	}
	state := uint64(seed)
	for i := 0; i < rows; i++ {
		state = next(state)
		bucket := int64(state % buckets)
		amount := float64(state%100000) / 100.0
		label := fmt.Sprintf("row-%d", i)

		if _, err := insertFact.Exec(
			types.Int(int64(i)),
			types.Int(bucket),
			types.Float(amount),
			types.Text(label),
		); err != nil {
			return err
		}
	}
	return nil
}

const (
	// buckets is how many distinct values the join key takes.
	buckets = 16
	// seed fixes the generated data so runs are comparable.
	seed = 0x2545F4914F6CDD1D
)

// next is one step of a linear congruential generator. The constants are
// Knuth's; any full-period pair would do, since this only needs to be spread
// out and repeatable, not statistically sound.
func next(state uint64) uint64 {
	return state*6364136223846793005 + 1442695040888963407
}
