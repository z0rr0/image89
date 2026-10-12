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
├── image89.go           # main: flags, version, logger, config loading, signal ctx, mode selection, runServer
├── internal/config/     # Config, Default(), Load(path), presets, Duration, Color, validation
├── internal/figure/     # Kind, Kinds(), ParseKind, Size, ParseSize, side limits, ForRows row scheduler
│   ├── fractal/         # Mandelbrot, Julia, Newton: viewport, palette, iterations, shared pixel loop (plane)
│   └── geometry/        # Sierpinski, Koch, Lissajous: Fit, Stroke into polygons, banded Fill with x/image/vector
├── internal/encode/     # Format, Parse, ContentType/Ext, ctx-aware Encode to PNG or JPEG
├── internal/render/     # Generator, Request, Renderer: registry, Validate, slots with a bounded queue, timeout
├── internal/cli/        # CLI mode: OutputName, Run into a new file (O_EXCL), removes the file on any error
├── internal/server/     # HTTP: routes, image handler, error statuses, middleware, Serve/Shutdown with fallback
├── internal/pages/      # embedded html/template pages (base, index, info/<kind>.html), rendered once by New
├── cmd/loadtest/        # HTTP load generator (stdlib): concurrent clients, slow reading, latency per status
├── docs/config.toml     # annotated config, every value equals config.Default()
├── docs/benchmarks.md   # measurements behind the defaults and internal constants, how to repeat them
├── docs/nginx.conf      # production nginx example: TLS, buffering, image cache with lock, limits per address
├── Makefile             # build, test, bench, lint, dist, docker targets
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
- `make bench` - Run benchmarks of `./internal/...` without tests; `BENCH=<regexp>` selects them, `BENCHFLAGS`
  adds flags (`BENCHFLAGS='-count=10 -benchtime=3x'`); compare runs with `go run golang.org/x/perf/cmd/benchstat@latest`
- `go run ./cmd/loadtest -c 4 -n 20 -path /mandelbrot/` - Load a running server (`-help` lists the flags)

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
- `figure.ForRows` passes a dense worker index in `[0, min(workers, blocks))`. For per-worker state, normalize
  `workers` to a positive `n` once (`0` means `GOMAXPROCS`, which the runtime may change at any time), allocate
  the slice of length `n` and pass the same `n` to `ForRows`, never `0` again.
  It checks `ctx` only between blocks and cannot stop a running `fn`: a generator checks `ctx` inside `fn` by a
  budget of work. It returns the first `fn` error, else `ctx.Err()`. A panic in `fn` is re-raised in the caller
  goroutine (as an error with the worker stack), so a recover there catches it; `runtime.Goexit` in `fn` is
  returned as an error.
- `encode.Encode` checks `ctx` before every write to the output, so cancellation granularity is output bytes, not
  rows: PNG writes about every 32 KiB of compressed data and stops at the first failed write; JPEG writes every
  4 KiB and keeps encoding the whole image after a failed write.
- `figure.MaxSide` (32768) is the only side limit: every format encodes it, so `render.Validate` has no check per
  format. `image/jpeg` refuses a side of `1<<16` or more; `TestEncodeMaxSide` fails if `MaxSide` reaches it.
  `limits.max_pixels` is at most `MaxSide²` (`1 << 30`).
- `png.Decode` returns `*image.RGBA` only for opaque truecolor images; tests that compare `Pix` use opaque images.
- gosec G115 reports `int` to `uint8` conversions; mask a value already in range with `& 0xff`.
- `go test -race -cover` makes every basic block update an atomic coverage counter through the race runtime, and
  goroutines contend on the same counters: a pixel loop gets 10–50× slower. Tests that generate images keep the
  total of pixels × iterations small (low preset iterations for size checks), run sequentially and use one worker;
  hot loops are written with one block per iteration (`escapeJulia`).
- Escape convention in `fractal`: `|z|² > r2` is checked before every update, `n` is the number of updates;
  `n < maxIter` means escaped, `n == maxIter` means inside. Products are wrapped in `float64()` against FMA.
- Pixel fractals share one unexported loop, `plane` in `plane.go`: a pixel function returns the color and the
  iterations spent (the ctx check budget). `escapeTime` in `escape.go` builds it for Mandelbrot and Julia from
  an escape function and the smooth coloring; `dupl` is on, so new pixel fractals reuse `plane`.
- The Mandelbrot escape function skips the main cardioid and the disk of period 2 (`inMainBulbs`) and returns
  `maxIter` for them; `escapeMandelbrot` itself still iterates. Skipped pixels count `maxIter` towards the ctx
  budget, so checks only come more often. `TestMandelbrotBulbsSameImage` compares the image byte for byte with
  the plain escape function; keep that when touching the check.
- Newton convention (`newtonRoot`): `|zⁿ − 1|² < eps²` is checked before every step, so a point at a root takes
  0 steps; a step with `|Δz|² < eps²` also converges and is counted; `|zⁿ⁻¹|² < newtonMinDeriv2` or `maxIter`
  steps give `k = −1` (background). The root index is the nearest root by angle. Color: a gradient from
  `colors[k]` to the background, at most `newtonFade` of the way, so a converged pixel never equals the background.
  `types.newton.epsilon` is at least 1e-12 (`minNewtonEpsilon`), a chosen bound with a margin over float64
  rounding: below about 1e-16 many orbits stagnate at the rounding level above the tolerance (an exact root still
  passes any positive `eps²`), and below about 1e-162 `eps²` underflows to 0, which no point passes.
- `render.Render` order: `Validate`, `ctx.Err()`, non-blocking slot try, then `ErrQueueFull` when `max_queued`
  requests wait, then a wait bounded by `queue_timeout` (`ErrBusy` joined with `DeadlineExceeded` or `Canceled`).
  `timeout` starts after the slot is taken; the slot is released after `sink` returns (also on a panic).
  Queue and timeout tests use `testing/synctest`, so they need no sleeps and run in virtual time.
- `cli.createFile` opens with `0o644` under `//nolint:gosec` (G302, G304) and uses the `-o` path as is. Never
  `filepath.Clean` a path that is later removed or reused: Clean is lexical and ignores symlinks, so with
  `alias -> other/child` the path `alias/../x` would be created at `./x` but removed at `other/x`, a foreign file.
  Read-only paths may go through `filepath.Clean` for G304 (`config.Load`, tests).
- Server lifecycle: `runServer` binds with `(&net.ListenConfig{}).Listen(ctx, ...)` (`noctx` forbids
  `net.Listen`) before serving, so a busy port fails at once. Requests get a base context of their own
  (`BaseContext`), never derived from the signal ctx: the signal must not cancel requests that `Shutdown` lets
  finish. `server.Shutdown` drains until its ctx is done, then cancels the base context (generation stops) and
  calls `Close`. `run(ctx, ...)` takes a parent ctx, so tests start and stop the server with a canceled ctx.
  `server.New` captures `slog.Default()` for the `http.Server` `ErrorLog`, so it must run after the final
  `slog.SetDefault` (the debug logger from the config).
- `docker-compose.yml` has `stop_grace_period: 15s`, longer than the default `server.shutdown_timeout` (10s):
  Docker's own default grace period is also 10s, and SIGKILL would cut the graceful shutdown. Keep the grace
  period above `shutdown_timeout` when changing either.
- Routes: `GET /{kind}/{$}`, `/{kind}/{size}/{$}`, `/{kind}/info/{$}` (the literal `info` wins over `{size}`).
  Go 1.27 `ServeMux` redirects a path without the trailing slash with **307**, keeping the query.
  `GET` patterns serve `HEAD` too; the image sink skips the body for `HEAD`.
- Image errors map to statuses in `errorStatus`, `context.Canceled` first (a canceled queue wait carries
  `ErrBusy` too), `ErrBusy`/`ErrQueueFull` before `DeadlineExceeded` (503 with and without `Retry-After`).
  A canceled request writes nothing, and the access log records it with `status=0` at the info level.
- Pages are embedded and rendered into bytes once by `pages.New`; the index lists the kinds of the renderer
  and links an info page only if `templates/info/<kind>.html` exists. MathML in templates uses literal UTF-8
  (`−`, `→`, `∞`), no named entities. The base template has `<link rel="icon" href="data:,">`, otherwise
  every page view asks for `/favicon.ico` and logs a 404 warning.
- Server tests: queue and cancel tests call `Handler.ServeHTTP` directly inside `testing/synctest` (no network)
  with a `fakeWriter` that blocks in `Write`; lifecycle tests use a real listener on `127.0.0.1:0` and real time.
- `golang.org/x/image/vector` sums signed coverage and clamps `|sum|` to 1: polygons of opposite orientations
  cancel where they overlap. Every polygon goes through `polygonSet`, which gives it a positive signed area.
  An edge shared by two polygons cancels exactly only if both take its ends from the same `Point` values:
  `Stroke` computes the shifted ends of a segment once and reuses them in the rectangle (6 points, the ends
  of the center line are vertices too), the joins and the caps. Joins and caps are `arc` sectors, a bevel is
  an arc of one chord; the inner side of a turn is left to the overlapping rectangles.
- `Stroke` sizes its points exactly: `strokePoints` walks the turns once and counts 6 points per rectangle and
  `chords + 2` per join or cap, with the chord angle (`chordAngle`) computed once per line. A change of `join`,
  `arc` or the rectangle must change `strokePoints` too; `TestStrokePoints` fails when the count is short.
- `Fill` rasterizes bands of rows, one mask per worker. `Draw` indexes the mask from `r.Min` and needs `r` of
  exactly the mask size (the full image with the band rectangle, no `SubImage`); `Draw` consumes the mask, so
  `Reset` comes before every band. Masks are fixed point (4 B/px) up to 512 px per side, float (8 B/px)
  above. The band shift is subtracted after the `float32` conversion. The output does not depend on the
  number of workers, but bands of another height change anti-aliased pixels by ±1: the accumulator carries
  the rounding residue of a row into the next one until the end of the mask. Tests compare the painted area
  per row, not bytes. `fill(…, rows, scratch)` takes the band height and the scratch cap for tests and
  returns the number of rasterizers it created. `bandPixels = 1<<19` keeps 8 rasterizers within `scratchCap`
  at any width (`docs/benchmarks.md`); changing it changes anti-aliased pixels of images wider than 4096.
- The center of a Sierpinski image lies on the top side of the removed middle triangle, an anti-aliased edge;
  tests check the centroid `(1/2, √3/6)`, which is inside the removed triangle at every depth from 1 on.
- Geometry tests are parallel: their hot loops run inside `x/image/vector`, which `-cover` does not
  instrument, so the package takes about 2 s under `-race -cover`.
- Do not add `png.Encoder.BufferPool`: `png.EncoderBuffer` keeps the last image and writer it encoded, so a
  pool pins RGBA images of up to 64 MiB until two GC cycles pass. Measured in `docs/benchmarks.md`.
- Benchmarks: `make bench` (`BENCH`, `BENCHFLAGS`), compared with benchstat. Runs at different times differ by
  up to 10% on a laptop, so a decision compares variants inside one run. Sub-benchmark names are not
  `key=value`; rename them with `sed` before `benchstat -col`. `BenchmarkCancel` needs `-benchtime=3x`.
- Fuzz targets: `FuzzParseSize` (`figure`), `FuzzSanitizeRequestID` and `FuzzImageRoute` (`server`). `make test`
  runs their seeds; `make fuzz` fuzzes each for `FUZZTIME`. A failing input lands in `testdata/fuzz/<target>/`
  and is committed with the fix. Set headers with `Header.Set`: a raw map key `X-Request-ID` is not canonical,
  and `Header.Get` does not see it. Fuzz targets replace the default logger, so they must not call `t.Parallel`.
- A slot is held until the response is written, so a slow client holds it until `server.write_timeout` (30s);
  the server must run behind a proxy that buffers responses (nginx `proxy_buffering on`). `cmd/loadtest -rate`
  reproduces it. The process RSS is up to twice the peak heap (`GOGC`); `GOMEMLIMIT` lowers it.
- `docs/nginx.conf` follows the config defaults: `proxy_read_timeout` above `queue_timeout + timeout` and at most
  `write_timeout`, `proxy_cache_lock_timeout` about one generation, upstream `keepalive_timeout` below
  `server.idle_timeout`. nginx caches only responses with `Cache-Control`, i.e. images; keep pages and errors
  without it. Change the file with those values and check it with `nginx -t` (e.g. in `nginx:alpine`).

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