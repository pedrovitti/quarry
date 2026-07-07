IMAGE   := quarry-dev
CACHE   := quarry-gocache

# Everything runs in the container. There is no local Go dependency.
DOCKER = docker run --rm \
	-v "$(CURDIR)":/app \
	-v $(CACHE):/gocache \
	-w /app \
	$(IMAGE)

.PHONY: image test testv cover build fmt vet lint shell junit clean

image:
	docker build -t $(IMAGE) -f Dockerfile .

test:
	$(DOCKER) go test ./...

testv:
	$(DOCKER) go test -v ./...

# Per-package coverage, then the uncovered lines. The second half is the
# useful one: a percentage says how much, this says which.
cover:
	$(DOCKER) go test -coverprofile=/app/coverage.out ./...
	$(DOCKER) go tool cover -func=/app/coverage.out | grep -v "100.0%"

# Mirrors how the grader runs: no network, JUnit XML out.
junit:
	docker run --rm --network none \
		-v "$(CURDIR)":/app -v $(CACHE):/gocache -w /app $(IMAGE) \
		gotestsum --junitfile /app/report.xml --format testname -- ./...

build:
	$(DOCKER) go build ./...

fmt:
	$(DOCKER) gofmt -l -w .

vet:
	$(DOCKER) go vet ./...

shell:
	docker run --rm -it -v "$(CURDIR)":/app -v $(CACHE):/gocache -w /app $(IMAGE) bash

clean:
	docker volume rm -f $(CACHE)
	rm -f report.xml coverage.out
