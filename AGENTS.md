# AGENTS: AI Assistants Guide for Image89

This document provides information for AI agents working on the **Image89** codebase, including:

- Project overview
- Directory structure
- Build and development workflow
- Code style conventions
- Testing style guidelines

## Project Overview

```
.
├── image89.go           # main: flags, version, logger, config loading, mode selection
├── internal/config/     # Config, Default(), Load(path), presets, Duration, Color, validation
├── internal/figure/     # Kind, Kinds(), ParseKind, Size, ParseSize, side limits, ForRows row scheduler
├── internal/encode/     # Format, Parse, ContentType/Ext/MaxSide, ctx-aware Encode to PNG or JPEG
├── docs/config.toml     # annotated config, every value equals config.Default()
├── Makefile             # build, test, lint, dist, docker targets
├── Dockerfile           # multi-stage image z0rr0/image89
├── docker-compose.yml   # service with /health healthcheck
├── .github/workflows/   # CI: build, vet, test, golangci-lint, govulncheck
├── .golangci.yml        # linter and formatter configuration
├── README.md            # project overview, usage and configuration reference
└── AGENTS.md            # this guide
```

## Build & Development Workflow

Build and test commands:

- `make build` - Build `./image89` with version info from git
- `make test` - Run `make lint` (gofmt, vet, golangci-lint, govulncheck) and tests
- `go fmt ./...`
- `go vet ./...`
- `golangci-lint fmt ./...` - Format code and group imports (gofmt + gci)
- `golangci-lint run ./...`
- `go test -race -cover ./...`
- `go test -v -race ./internal/config` - Run tests for specific package
- `go test -v -run TestValidate ./internal/config` - Run specific test
- `go test -bench . -benchmem -run '^$' ./internal/figure ./internal/encode` - Run benchmarks

Gotchas:

- `docs/config.toml` must stay equal to `config.Default()`; `TestLoadDocsConfig` compares them.
- The Go version lives in three places: `toolchain` in `go.mod` (CI reads it via `go-version-file`),
  `GOLANG_VERSION` in `Dockerfile` and `go-version-input` of govulncheck in `.github/workflows/go.yml`.
  Bump them together.
- go-toml flattens errors returned by `UnmarshalText`: `errors.Is` sees only `config.ErrDecode`, not
  `ErrInvalidColor` or `ErrNegativeDuration`. Test those sentinels on the types directly.
- go-toml calls `UnmarshalText` only for TOML strings and decodes other values straight into the Go type
  (`timeout = 30` would be 30ns, `color = {R = 255}` would set RGB fields). `config.Load` rejects them first
  with `checkTextFields`, which covers every `encoding.TextUnmarshaler` field found by its `toml` tag, so
  every config field needs a `toml` tag. Keys are matched as go-toml does, by `strings.ToLower` of key and
  tag, not `strings.EqualFold`: they differ for keys like `"tİmeout"` (U+0130).
- `figure.ForRows` passes a dense worker index in `[0, min(workers, blocks))`, so per-worker state fits a slice.
  It checks `ctx` only between blocks and cannot stop a running `fn`: a generator checks `ctx` inside `fn` by a
  budget of work. It returns the first `fn` error, else `ctx.Err()`. A panic in `fn` is re-raised in the caller
  goroutine (as an error with the worker stack), so a recover there catches it.
- `encode.Encode` checks `ctx` before every write to the output, so cancellation granularity is output bytes, not
  rows: PNG writes about every 32 KiB of compressed data and stops at the first failed write; JPEG writes every
  4 KiB and keeps encoding the whole image after a failed write. `Encode` does not check sides against `MaxSide`.
- `png.Decode` returns `*image.RGBA` only for opaque truecolor images; tests that compare `Pix` use opaque images.
- gosec G115 reports `int` to `uint8` conversions; mask a value already in range with `& 0xff`.

## Code Style Conventions

- **Imports**: Standard library first, then third-party, then local packages
- **Error Handling**: Use `errors.Join()` with sentinel errors for error wrapping
- **Formatting**: Run `gofmt` before commits (enforced by `make lint`)
- **Naming**: CamelCase for exported items, lower camelCase for unexported items
- **Types**: Use strong typing with custom types (e.g., Duration, SubPath)
- **Comments**: Add comments for exported functions and types
- **Code Structure**: Place interfaces near their implementations
- **Error Constants**: Define sentinel error variables at package level
- **Logging**: Use structured logging with slog package

## Testing Style Guidelines

- Write table-driven tests with descriptive test names
- Check returned errors explicitly and compare against expected sentinel errors
- Place test files alongside the code they cover, using the `_test.go` suffix
- Run tests with `go test -race -cover` to ensure coverage and race-condition detection

## Additional Agent Guidelines

- Focus on fixing issues at the root cause; avoid superficial patches
- Keep changes minimal and consistent with existing patterns
- Update documentation (e.g., README.md) when introducing new functionality or behavioral changes
- Save important notes to this file `AGENTS.md` if it is relevant to the codebase or project.