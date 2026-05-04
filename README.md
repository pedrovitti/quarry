# quarry

A small SQL query engine, written in Go with no third-party dependencies.

Quarry parses SQL, plans it, and executes it over its own storage layer. It is
built to be read: each stage is a separate package with a narrow interface, and
the whole pipeline is exercised by golden tests that state a query and its
expected output.

## Pipeline

    SQL text
      -> lex      tokens
      -> parse    AST
      -> plan     logical plan
      -> opt      rewritten plan
      -> exec     row iterators
      -> storage  heap files and tuples

## Building

Everything runs in Docker; no local Go toolchain is needed.

    make image     # build the dev image (once)
    make test      # go test ./...
    make shell     # drop into the container

## Status

Early. The lexer is in progress; nothing executes yet.
