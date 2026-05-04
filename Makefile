IMAGE   := quarry-dev
CACHE   := quarry-gocache

# Everything runs in the container. There is no local Go dependency.
DOCKER = docker run --rm \
	-v "$(CURDIR)":/app \
	-v $(CACHE):/gocache \
	-w /app \
	$(IMAGE)

.PHONY: image test testv build fmt vet lint shell junit clean

image:
	docker build -t $(IMAGE) -f Dockerfile .

test:
	$(DOCKER) go test ./...

testv:
	$(DOCKER) go test -v ./...

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
	rm -f report.xml
