BINARY_NAME=pg-monk
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: build run test clean fmt lint vet check snapshot install init-config help \
	demo-db demo-load demo-reset demo-down media

## build: Build the binary
build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/pg-monk

## run: Build and run with optional ARGS
run: build
	./$(BINARY_NAME) $(ARGS)

## test: Run all tests
test:
	go test -race -v ./...

## fmt: Format code
fmt:
	go fmt ./...
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

## vet: Run go vet
vet:
	go vet ./...

## lint: Run linter (requires golangci-lint)
lint:
	golangci-lint run ./...

## check: Run all checks (fmt, vet, test, build)
check: fmt vet test build
	@echo "✓ All checks passed"

## snapshot: Build snapshot with goreleaser
snapshot:
	goreleaser release --snapshot --clean

## clean: Remove build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -rf dist/

## install: Install to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/pg-monk

## init-config: Write a default config file
init-config: build
	./$(BINARY_NAME) init-config

# ---- Demo database & README media (requires Docker) ----
DEMO_NET  ?= pgmonk-demo
DEMO_PORT ?= 55433
DEMO_DSN  ?= postgresql://postgres:demo@localhost:$(DEMO_PORT)/shop
VHS_IMAGE ?= ghcr.io/charmbracelet/vhs
GIF_FILTER = fps=12,scale=1100:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle

## demo-db: Start a seeded PostgreSQL 17 demo database in Docker
demo-db:
	-docker network create $(DEMO_NET) >/dev/null 2>&1
	docker run -d --name pgmonk-pg --network $(DEMO_NET) -p $(DEMO_PORT):5432 \
		-e POSTGRES_PASSWORD=demo -e POSTGRES_DB=shop postgres:17 \
		-c shared_preload_libraries=pg_stat_statements -c track_io_timing=on \
		-c pg_stat_statements.track=all -c max_connections=50
	@until docker exec pgmonk-pg pg_isready -U postgres -d shop >/dev/null 2>&1; do sleep 1; done; sleep 2
	psql "$(DEMO_DSN)" -X -q -f scripts/demo/seed.sql
	@echo "demo db ready: $(DEMO_DSN)"

## demo-load: Run a realistic (slightly unhealthy) workload against the demo db
demo-load:
	DSN="$(DEMO_DSN)" ./scripts/demo/load.sh

## demo-reset: Kill demo sessions and reset pg_stat_statements
demo-reset:
	DSN="$(DEMO_DSN)" ./scripts/demo/reset.sh

## demo-down: Remove the demo database container
demo-down:
	-docker rm -f pgmonk-pg
	-docker network rm $(DEMO_NET)

## media: Re-record README screenshots and demo video (run demo-load first)
media:
	GOOS=linux GOARCH=$$(go env GOARCH) CGO_ENABLED=0 go build -ldflags "-s -w" -o scripts/demo/bin/pg-monk ./cmd/pg-monk
	docker run --rm --network $(DEMO_NET) -v "$$PWD":/vhs $(VHS_IMAGE) scripts/demo/screenshots.tape
	docker run --rm --network $(DEMO_NET) -v "$$PWD":/vhs $(VHS_IMAGE) scripts/demo/demo.tape
	docker run --rm -v "$$PWD":/vhs -w /vhs --entrypoint ffmpeg $(VHS_IMAGE) -loglevel error -y \
		-i docs/media/demo.mp4 -vf "$(GIF_FILTER)" docs/media/demo.gif

## help: Show this help
help:
	@echo "Usage:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'
