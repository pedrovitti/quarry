# golang:1.25-bookworm (Go 1.25.12), pinned by index digest so it resolves
# identically on arm64 and amd64.
FROM golang@sha256:908f8ff2ec296df2f349563072c7925775cd28b50361a52ed834a8a37399b9bf

# GOTOOLCHAIN=local keeps Go from downloading a different toolchain when go.mod
# names a newer version -- that would need the network at test time.
ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOPATH=/go \
    GOCACHE=/gocache

# JUnit XML reporter. Build-time network is fine; test-time has none.
RUN go install gotest.tools/gotestsum@v1.12.3

# Warm the stdlib build cache so an offline `go test ./...` does not pay for it.
RUN go build std && chmod -R 0777 /gocache

WORKDIR /app
