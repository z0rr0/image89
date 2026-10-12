NAME=image89
TS=$(shell date -u +"%FT%T")
TAG=$(shell git tag | sort -V | tail -1 | grep . || echo "v0.0.0")
REVISION=$(shell git rev-parse --short HEAD 2>/dev/null || echo "0000000")
# GO_LDFLAGS, not LDFLAGS: the latter conventionally carries C linker flags and
# is commonly exported by the host shell, which would leak into the build.
GO_LDFLAGS=-X main.Version=$(TAG) -X main.Revision=git:$(REVISION) -X main.BuildDate=$(TS)
DIST=dist
PLATFORMS=darwin/arm64 linux/amd64
IMAGE=z0rr0/image89
BUILDER=image89-multiarch
DOCKER_PLATFORMS=linux/amd64,linux/arm64
# make bench BENCH=Generate BENCHFLAGS='-count=10 -benchtime=3x'
BENCH ?= .
BENCHFLAGS ?=
# make fuzz FUZZTIME=5m
FUZZTIME ?= 30s

.PHONY: all fmt lint vuln test bench fuzz build dist run docker docker-push clean

all: build

# gofmt plus gci import grouping, both configured in .golangci.yml.
fmt:
	golangci-lint fmt ./...

lint:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "gofmt: files need formatting" && exit 1)
	go vet ./...
	golangci-lint run ./...
	$(MAKE) vuln

vuln:
	-govulncheck ./...

test: lint
	go test -race -cover ./...

# Benchmarks only, without -race; docs/benchmarks.md has the results.
bench:
	go test -run '^$$' -bench '$(BENCH)' -benchmem $(BENCHFLAGS) ./internal/...

# Fuzzing of the parsers of client input, FUZZTIME per target: go test -fuzz
# takes one target at a time. `make test` runs only the seed corpus. A failing
# input is saved in testdata/fuzz/<target>/ and must be committed with the fix.
fuzz:
	go test -run '^$$' -fuzz '^FuzzParseSize$$' -fuzztime $(FUZZTIME) ./internal/figure
	go test -run '^$$' -fuzz '^FuzzSanitizeRequestID$$' -fuzztime $(FUZZTIME) ./internal/server
	go test -run '^$$' -fuzz '^FuzzImageRoute$$' -fuzztime $(FUZZTIME) ./internal/server

build:
	go build -ldflags "$(GO_LDFLAGS)" -o $(NAME) .

# Cross-compiled release binaries; `build` above stays native to the host.
# CGO_ENABLED=0 is what makes cross-compiling work without a C toolchain.
dist:
	@mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -ldflags "$(GO_LDFLAGS)" -o $(DIST)/$(NAME)-$$os-$$arch . || exit 1; \
	done

run: build
	./$(NAME) -s -c docs/config.toml

# Host-arch only: the default Docker Desktop builder uses the overlay2 image
# store, which cannot --load a multi-arch manifest list.
docker:
	docker buildx build \
		-t $(IMAGE):latest -t $(IMAGE):$(TAG) \
		--build-arg GO_LDFLAGS="$(GO_LDFLAGS)" \
		--load .

# Multi-arch goes straight to the registry: the `docker` driver cannot build a
# manifest list at all, so this needs its own docker-container builder, created
# on first use and reused afterwards. Not a dependency of `docker` — that would
# build the image twice.
# TAG is the latest tag of the repository, not necessarily on HEAD: pushing an
# untagged HEAD overwrites the image of that tag. This is intentional.
docker-push:
	@test "$(TAG)" != "v0.0.0" || (echo "no git tag found, refusing to push $(IMAGE):v0.0.0" && exit 1)
	@docker buildx inspect $(BUILDER) >/dev/null 2>&1 || docker buildx create --name $(BUILDER) --driver docker-container
	docker buildx build --builder $(BUILDER) \
		--platform $(DOCKER_PLATFORMS) \
		-t $(IMAGE):latest -t $(IMAGE):$(TAG) \
		--build-arg GO_LDFLAGS="$(GO_LDFLAGS)" \
		--push .

clean:
	rm -f $(NAME)
	rm -rf $(DIST)
