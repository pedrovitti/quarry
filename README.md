# quarry

A small SQL query engine, written in Go with no third-party dependencies.

Quarry parses SQL, plans it, and executes it over its own storage layer. It is
built to be read: each stage is a separate package with a narrow interface, and
the whole pipeline is exercised by test files that state a query and the output
it must produce.

```
$ quarry
quarry> CREATE TABLE customers (id INT NOT NULL, name TEXT, city TEXT, spend FLOAT);
(CREATE TABLE)
quarry> INSERT INTO customers VALUES
  ..>   (1, 'ada', 'london', 1250.5),
  ..>   (2, 'bob', 'paris', 89.0),
  ..>   (3, 'cy', NULL, 4300.25);
(INSERT 3 rows)
quarry> SELECT name, coalesce(city, 'unknown') AS place FROM customers ORDER BY spend DESC;
 name | place
------+---------
 cy   | unknown
 ada  | london
 bob  | paris
(3 rows)
```

## Pipeline

    SQL text
      -> lex      tokens
      -> parse    AST
      -> exec     row iterators
      -> storage  heaps and tuples

Operators are iterators in the volcano style: each pulls rows from the one
below it, so a `LIMIT` stops the scan under it rather than reading a table and
throwing most of it away. Sorting and `DISTINCT` are the two that have to
materialize.

## What it does

- `SELECT` with `DISTINCT`, `WHERE`, `ORDER BY`, `LIMIT` and `OFFSET`
- Inner `JOIN`, including chains of them
- `INSERT`, `UPDATE`, `DELETE`, `CREATE TABLE`, `DROP TABLE`
- `INT`, `BIGINT`, `FLOAT`, `TEXT`, `BOOL`, with `NOT NULL`
- Three-valued logic: `NULL = NULL` is unknown, and unknown does not pass a
  filter
- Around thirty scalar functions
- `$1` parameters, and statements prepared once and run many times

It does not do aggregates, subqueries, indexes or transactions.

## Building

Everything runs in Docker; no local Go toolchain is needed.

    make image     # build the dev image (once)
    make test      # go test ./...
    make junit     # run the tests offline, writing report.xml
    make shell     # drop into the container

## Using it

    quarry                    read statements from the terminal
    quarry script.sql         run a file
    quarry -c "SELECT 1"      run one statement
    quarry -f db.sql          load a dump first

Inside the shell, `.help` lists the dot commands. `.mode csv|json|list|column`
changes the output format, `.save` and `.open` write and read a database as
SQL, and `.read` runs a file.

## Tests

Two kinds. Go tests live beside the code they cover. Behavioural cases live in
`internal/logictest/testdata` as text files that state a statement and its
result:

```
query
SELECT id FROM customers WHERE city IS NULL
----
id
3
```

Each record becomes its own named subtest carrying the file and line it came
from, so a failure points somewhere useful. `TestFilesAreDeterministic` runs
every file ten times over a fresh database, because output that depends on map
iteration order would otherwise show up much later as an intermittent failure.

## Measuring

    quarrybench -rows 100000

Generates a table from a fixed seed and times each query shape, so two runs
measure the same work.
