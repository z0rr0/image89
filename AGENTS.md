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
├── .golangci.yml  # linter and formatter configuration
├── README.md      # project overview, usage and configuration reference
└── AGENTS.md      # this guide
```

## Build & Development Workflow

Build and test commands:

- `go fmt ./...`
- `go vet ./...`
- `golangci-lint fmt ./...` - Format code and group imports (gofmt + gci)
- `golangci-lint run ./...`
- `go test -race -cover ./...`
- `go test -v -race ./cfg` - Run tests for specific package
- `go test -v -run TestGroupValidate ./cfg` - Run specific test

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