# Benchmarks

The measurements behind the default limits and the internal constants of Image89, and how to repeat them.
Numbers are medians; `±` and `p` come from [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat).

## Environment

|        |                                                                             |
|--------|-----------------------------------------------------------------------------|
| CPU    | Apple M2, 8 cores (4 performance + 4 efficiency), `GOMAXPROCS` 8            |
| Memory | 16 GiB                                                                      |
| OS     | macOS 27.0, darwin/arm64                                                    |
| Go     | go1.27.2                                                                    |
| Docker | Docker Desktop 29.8.2, VM with 6 CPUs and 6 GB (container checks only)      |
| Code   | `cfc1ac4` with the changes of this calibration, default config unless noted |

Runs at different times differ by up to about 10% on this machine, so every decision compares variants of one
run, never numbers of separate runs.

## How to repeat

```sh
make bench                                         # all benchmarks once
make bench BENCH=FillBands BENCHFLAGS='-count=10 -benchtime=20x'
go install golang.org/x/perf/cmd/benchstat@latest  # not a dependency of the module
benchstat old.txt new.txt
```

| Benchmark                                     | Package    | Measures                                                                                                       |
|-----------------------------------------------|------------|----------------------------------------------------------------------------------------------------------------|
| `Generate/<kind>/<W>x<H>`                     | `render`   | generation of every figure at 640×480, 480×640, 640×640, 1920×1080, 4096×4096                                  |
| `Encode/<kind>/<variant>`                     | `render`   | PNG `DefaultCompression` and `BestSpeed`, JPEG quality 85 and 90 at 1920×1080; `B/img` is the output size      |
| `Workers/<kind>/{1,max}`                      | `render`   | one goroutine against `GOMAXPROCS` per image at 1920×1080                                                      |
| `Cancel/<kind>`                               | `render`   | delay from a canceled ctx to the return of `Generate` at 4096² with the heaviest presets of the default limits |
| `Render/<kind>/<format>`                      | `render`   | the whole pipeline at 1024²                                                                                    |
| `BlockRows/<kind>/<W>x<H>/<rows>`             | `fractal`  | blocks of 8, 16 and 64 rows of the pixel fractals                                                              |
| `FillBands/<kind>/<W>x<H>/<budget>/<scratch>` | `geometry` | band pixel budget and scratch limit of the rasterizer; `rasters` is the number of rasterizers                  |
| `StrokeKoch/<line width>`                     | `geometry` | stroke of the Koch snowflake of depth 7 at 4096²; `points` is the number of polygon points                     |
| `GeometryAtMaxSide/<kind>/<W>x<H>`            | `geometry` | polygons and bands at the deepest level the resolution allows, without the image                               |

HTTP load runs the server under `/usr/bin/time -l`, loads it with `cmd/loadtest` and stops it with SIGTERM, so
`time` prints the peak memory of the process:

```sh
make build
/usr/bin/time -l ./image89 -s -c scenario.toml &
go run ./cmd/loadtest -url http://127.0.0.1:8089 -c 8 -n 24 \
  -path /mandelbrot/ -path /julia/ -path /newton/ -path /sierpinski/ -path /koch/ -path /lissajous/
kill -TERM "$(pgrep -f '^./image89 -s')"   # not the time process, it has to report
```

Every scenario config sets `[server] addr = "127.0.0.1:8089"` and changes only the named limits. `-rate` makes a
client read its responses at the given bytes per second.

## Generation

`Generate`, ms per image, default presets, `workers = 0`:

| Kind       | 640×480 | 480×640 | 640×640 | 1920×1080 | 4096×4096 |
|------------|--------:|--------:|--------:|----------:|----------:|
| mandelbrot |     4.2 |     3.7 |     5.1 |      25.0 |       199 |
| julia      |     8.1 |     5.9 |     8.8 |      43.6 |       346 |
| newton     |     6.7 |     6.7 |     8.6 |      42.6 |       319 |
| sierpinski |    0.73 |    0.52 |     1.0 |       3.1 |      16.6 |
| koch       |     1.3 |     1.2 |     1.4 |       3.6 |      18.9 |
| lissajous  |     1.0 |    0.94 |     1.4 |       4.0 |      22.5 |

A pixel fractal of 4096² takes the 64 MiB RGBA buffer; a contour figure takes about 96–102 MiB, the buffer plus
the rasterizer masks (at most `scratchCap`, 32 MiB) and the polygons.

`Render` at 1024², ms per request, generation and encoding:

|      | mandelbrot | julia | newton | sierpinski | koch | lissajous |
|------|-----------:|------:|-------:|-----------:|-----:|----------:|
| PNG  |       46.1 |  63.0 |   51.1 |       24.4 | 26.4 |      27.1 |
| JPEG |       23.9 |  36.9 |   32.0 |       12.9 | 12.1 |      12.0 |

Encoding is a single goroutine and takes half or more of a request: PNG of 1920×1080 takes 44–78 ms, while the
contour figures are generated in 4 ms.

## Encoding

1920×1080, time per image and output size:

| Kind       | PNG default       | PNG best speed        | JPEG 85                        | JPEG 90          |
|------------|-------------------|-----------------------|--------------------------------|------------------|
| mandelbrot | 63.7 ms, 548 KiB  | 51.3 ms, 649 KiB      | 21.1 ms, 114 KiB               | 21.7 ms, 140 KiB |
| julia      | 77.7 ms, 1093 KiB | 62.5 ms, 1255 KiB     | 26.2 ms, 298 KiB               | 27.2 ms, 358 KiB |
| newton     | 52.1 ms, 314 KiB  | 41.8 ms, 336 KiB      | 18.8 ms, 72 KiB                | 19.2 ms, 89 KiB  |
| sierpinski | 46.2 ms, 245 KiB  | 35.6 ms, 249 KiB      | 19.5 ms, 171 KiB               | 19.9 ms, 199 KiB |
| koch       | 44.2 ms, 138 KiB  | 34.2 ms, 135 KiB      | 17.6 ms, 92 KiB                | 17.5 ms, 105 KiB |
| lissajous  | 45.4 ms, 126 KiB  | 35.5 ms, 122 KiB      | 17.1 ms, 85 KiB                | 17.3 ms, 97 KiB  |
| geomean    |                   | −21% time, +5.7% size | −15% size, −2% time against 90 |                  |

## Internal constants

**Blocks of rows** of the pixel fractals, time against 16 rows (`-count=10`):

|                                | 8 rows    | 64 rows      |
|--------------------------------|-----------|--------------|
| mandelbrot 640×480 / 1920×1080 | ~ / ~     | +58% / +8.6% |
| julia 640×480 / 1920×1080      | −3.8% / ~ | +51% / +7.8% |
| newton 640×480 / 1920×1080     | −4.5% / ~ | +13% / +8.8% |
| geomean                        | −1.5%     | +23%         |

`~` means no significant difference (p ≥ 0.05).

**Bands of the rasterizer.** A band has `min(maxBandRows, bandPixels / W)` rows, at most 128, so the band budget
matters only for images wider than `bandPixels / 128`. Time against the budget of `1<<19` pixels (512K):

| Image               | Scratch          | 512K                  | 1024K               | 2048K                 |
|---------------------|------------------|-----------------------|---------------------|-----------------------|
| koch 4096×4096      | 16 / 32 / 64 MiB | 19.4 / 15.3 / 15.3 ms | ~ / ~ / +0.8%       | ~ / ~ / +1.9%         |
| koch 32768×256      | 16 / 32 / 64 MiB | 10.8 / 8.8 / 9.0 ms   | +73% / +18% / +5.3% | +229% / +110% / +17%  |
| lissajous 4096×4096 | 16 / 32 / 64 MiB | 22.7 / 17.3 / 17.4 ms | ~ / +0.8% / +2.4%   | +1.0% / +5.0% / +5.3% |
| lissajous 32768×256 | 16 / 32 / 64 MiB | 10.1 / 8.3 / 8.3 ms   | +85% / +24% / +10%  | +257% / +124% / +25%  |

With 32 MiB of scratch, 512K bands give 8 rasterizers at any width, 1024K bands only 4 above 4096 pixels.
A scratch of 16 MiB halves the rasterizers of 4096² and is 26–31% slower; 64 MiB gives nothing over 32 MiB.

**Workers per image** at 1920×1080: `GOMAXPROCS` goroutines are 5.3–5.7 times faster than one for the pixel
fractals and 2.8–3.0 times for the contour figures.

**Cancellation.** Delay from the cancel to the return of `Generate` at 4096², heaviest presets of the default
limits (4096 iterations, Koch of depth 8 with a line of 2%, Lissajous of 65 536 points), 5 runs:

|          | mandelbrot | julia | newton | sierpinski | koch | lissajous |
|----------|-----------:|------:|-------:|-----------:|-----:|----------:|
| mean, ms |        1.7 |   4.3 |    8.2 |        3.0 |  3.7 |       3.1 |
| max, ms  |        2.1 |   4.7 |   10.1 |        3.2 |  4.2 |       3.7 |

Newton takes the longest because one of its steps costs more than an iteration of the escape-time fractals,
and `checkEvery` counts steps.

A pixel fractal checks ctx only between pixels, after every `checkEvery` = 2²⁰ iterations, so a stop takes up
to `checkEvery + max_iterations` iterations of one worker. An iteration costs 3.75 ns for Julia and 16–25 ns
for a Newton step of degree 3–8 here. Without a bound on `limits.max_iterations` one pixel of 10⁸ iterations
ran 0.4 s past the deadline (found in review), so the limit is at most 2²⁰: a `Render` of 64×64 with a
`timeout` of 1 ms then returns within 8 ms for Julia with every point inside and within 35 ms for Newton of
degree 8 with points that never converge.

**Geometry at the largest side.** Polygons and their bands at the deepest level the resolution allows, without
the RGBA buffer:

|                                   | 4096×4096         | 32768×32768      |
|-----------------------------------|-------------------|------------------|
| sierpinski                        | depth 9, 2.9 MiB  | depth 12, 82 MiB |
| koch                              | depth 7, 16.5 MiB | depth 9, 566 MiB |
| lissajous (65 536 points at most) | 5.5 MiB           | 32 MiB           |

At 32768² the RGBA buffer alone is 4 GiB, so the deepest Koch snowflake adds 14% and the others less.

## Optimizations

Both keep the images byte for byte (tests compare the pixels), except the band change, see the decisions.

**Main bulbs of the Mandelbrot set.** Points of the main cardioid and of the disk of period 2 are inside the
set; the generator gives them the color of the set without iterating. They hold most of the iterations of the
default view.

| Mandelbrot              |         before |          after |             |
|-------------------------|---------------:|---------------:|-------------|
| 640×480                 |        11.0 ms |         4.2 ms | −62%        |
| 1920×1080               |        58.7 ms |        25.0 ms | −57%        |
| 4096×4096               |         449 ms |         199 ms | −56%        |
| Render 1024² PNG / JPEG | 60.6 / 39.2 ms | 46.1 / 23.9 ms | −24% / −39% |

**Capacity of the stroke polygons.** `Stroke` estimated 9 points per segment and grew the slice by `append`,
which copied it and kept the old arrays until the next GC. It now counts the points of every join and cap in a
pass over the turns, which checks ctx like the main loop, and computes the chord angle once per line instead
of once per join:

|                                              | before           | after                           |
|----------------------------------------------|------------------|---------------------------------|
| Koch depth 7, 4096², line 0.4%               | 6.4 ms, 29.5 MiB | 6.5 ms (~), 12.5 MiB (−58%)     |
| Koch depth 7, 4096², line 2%                 | 7.9 ms, 42.7 MiB | 7.6 ms (−4.4%), 14.8 MiB (−65%) |
| Koch geometry at 32768²                      | 870 MiB          | 566 MiB (−35%)                  |
| Lissajous of 4826 points (`BenchmarkStroke`) | 0.37 ms          | 0.39 ms (+5.4%), same memory    |

**Rejected: `png.Encoder.BufferPool`.** A pool of encoder buffers saves 1.1 MiB of allocations per PNG image
and 0.4–2.5% of time. But `png.EncoderBuffer` keeps the last image and writer it encoded, and the pool keeps the
buffer until two GC cycles pass: every pooled buffer would pin an RGBA image of up to 64 MiB and its encoded
output. That costs more peak memory than it saves.

## HTTP load

`cmd/loadtest`, latency of a request including the wait for a slot. 1024² means the six `/<kind>/` URLs in turn.
The scenarios ran with the former `timeout = "30s"` and `write_timeout = "60s"`, except the last row of the slow
clients. No request came near these limits (the longest took 3.1 s), so the numbers hold for the new
`15s` and `30s` too.

**Clients**, 1024², default config, 24 requests per client:

| Clients | req/s | p50 ms | p95 ms | 503 | Peak RSS |
|--------:|------:|-------:|-------:|----:|---------:|
|       1 |  24.9 |     28 |     64 |   0 |    46 MB |
|       2 |  43.7 |     39 |     79 |   0 |    64 MB |
|       4 |  42.9 |     90 |    123 |   0 |    57 MB |
|       8 |  42.8 |    185 |    227 |   0 |    66 MB |

**Concurrency** at 8 clients, 1024²:

| Limits                              | req/s | p50 ms | p95 ms | Peak RSS |
|-------------------------------------|------:|-------:|-------:|---------:|
| `max_concurrent = 2` (default)      |  42.8 |    185 |    227 |    66 MB |
| `max_concurrent = 3`                |  54.4 |    141 |    207 |    73 MB |
| `max_concurrent = 4`                |  61.8 |    122 |    192 |    85 MB |
| `max_concurrent = 8`                |  65.2 |    110 |    240 |   118 MB |
| `max_concurrent = 8`, `workers = 1` |  63.7 |     74 |    252 |   117 MB |

Two slots give 1.75 times the throughput of one although every image uses all cores: the encoder runs in one
goroutine. Four slots are the knee on 8 cores.

**Large images**, 4096² Julia, Mandelbrot, Koch and Sierpiński in turn, 4 requests per client:

| Clients | Limits                                    | req/s | p50 ms | p95 ms |     503 |                  Peak RSS |
|--------:|-------------------------------------------|------:|-------:|-------:|--------:|--------------------------:|
|       1 | default                                   |  1.75 |    371 |    913 |       0 |                    242 MB |
|       2 | default                                   |  2.96 |    513 |   1123 |       0 |                    419 MB |
|       4 | default                                   |  3.08 |   1245 |   1768 |       0 |                    423 MB |
|       8 | default                                   |  3.38 |   2314 |   2886 | 5 of 32 |                    507 MB |
|       8 | `max_concurrent = 4`                      |  4.15 |   1599 |   2977 |       0 |                    755 MB |
|       8 | default, `GOMEMLIMIT=300MiB`              |  3.59 |   2047 |   2861 | 5 of 32 | 449 MB (footprint 338 MB) |
|       8 | `max_concurrent = 4`, `GOMEMLIMIT=450MiB` |  4.23 |   1720 |   2541 |       0 | 616 MB (footprint 548 MB) |

The 503s come after `queue_timeout`: six requests of about a second queue for two slots.

**Heaviest images** of the default limits (4096², 4096 iterations, Koch of depth 8 and Lissajous of 65 536
points with lines of 2%), one CLI run each: 1.03 s at most (Mandelbrot PNG), JPEG 0.73 s. Four clients of the
three pixel fractals with 4096 iterations take at most 3.1 s per request with the wait for a slot.

**Burst** of 20 clients, 3 requests each, 1024²: 29 responses 200, 31 immediate 503 with a p50 of 11 ms (`ErrQueueFull`:
2 slots and 8 waiting requests).

**Index page**: 6 clients of the six 320×240 thumbnails, 20 requests each: no 503, p95 18 ms.

**Slow clients.** Two clients read 4096² Julia images (6.1 MiB) at 128 KiB/s, four clients ask for 1024².
The first four rows ran with the former `write_timeout = "60s"`, long enough for the slow clients to finish:

| Setup                                                                  | Normal clients             | Slow clients                                    |
|------------------------------------------------------------------------|----------------------------|-------------------------------------------------|
| `max_concurrent = 2`, server reached directly                          | 40 of 40 get 503 after 2 s | 50 s each                                       |
| `max_concurrent = 4`, directly                                         | 40 × 200, p95 136 ms       | 50 s each                                       |
| `max_concurrent = 2`, in Docker, directly                              | 40 of 40 get 503           | 50 s each                                       |
| `max_concurrent = 2`, in Docker behind nginx with `proxy_buffering on` | 40 × 200, p95 171 ms       | 50 s each                                       |
| defaults (`write_timeout = "30s"`), directly                           | 40 of 40 get 503           | cut at 30.0 s, the client sees `unexpected EOF` |

The slot is held until the response is written, so a client that reads slowly holds it until
`server.write_timeout` cuts the response; the server logs `image response interrupted` with an i/o timeout.
Behind nginx with buffering (its default) nginx takes the whole response at once and the slot is free.

## Memory

A slot holds the RGBA buffer, the rasterizer masks of a contour figure, its polygons and the encoded image,
whose buffer grows by doubling:

```text
peak heap ≈ max_concurrent × (4·max_pixels + 32 MiB + polygons + 2 × encoded size)
```

The polygons take a few MiB with the default presets and up to 17 MiB at 4096² with `max_depth = 8`; a PNG of
4096² takes up to 6.4 MB. With the defaults that is about 2 × 125 MiB. The process RSS is up to about twice the
peak heap, because the GC lets the heap grow by `GOGC = 100` between collections: 419–507 MB measured.
`GOMEMLIMIT` near the formula makes the GC collect earlier: 338 MB of footprint with `GOMEMLIMIT=300MiB`
at the same throughput.

## Decisions

| Value                                              | Before            | After | Why                                                                                                                                                                                      |
|----------------------------------------------------|-------------------|-------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `figure.MaxSide`                                   | 65536             | 32768 | 32768² RGBA is 4 GiB. Every format encodes the side, so the check per format (`ErrSideTooLarge`) is gone                                                                                 |
| max `limits.max_pixels`                            | 2³²               | 2³⁰   | `MaxSide²`                                                                                                                                                                               |
| `limits.max_pixels`                                | 16 777 216        | kept  | 4096² is generated in 0.02–0.35 s and served within about a second; peak RSS 0.5 GB with two slots                                                                                       |
| `limits.max_concurrent`                            | 2                 | kept  | Chosen for memory: 4 slots give +44% throughput at 1024² and +23% at 4096² on 8 cores and survive two slow clients, but take 0.75 GB at `max_pixels` (README: raise it on 8+ cores)      |
| `limits.max_queued`                                | 8                 | kept  | The six thumbnails of the index never get 503; a burst beyond 2 + 8 requests gets 503 in about 10 ms                                                                                     |
| `limits.queue_timeout`                             | 2s                | kept  | Waits at 1024² stay below 0.25 s with 8 clients; large images over the queue get 503 with `Retry-After` instead of waiting                                                               |
| `limits.timeout`                                   | 30s               | 15s   | About 5 times the heaviest image of the default limits under contention (3.1 s with the wait)                                                                                            |
| `server.write_timeout`                             | 60s               | 30s   | Exceeds `queue_timeout + timeout` (17 s) and halves the time a slow client holds a slot; a 6.4 MB image needs about 215 KB/s                                                             |
| `server.shutdown_timeout`, `stop_grace_period`     | 10s, 15s          | kept  | The grace period stays above the shutdown timeout                                                                                                                                        |
| `limits.workers`                                   | 0                 | kept  | `GOMAXPROCS` goroutines are 2.8–5.7 times faster per image                                                                                                                               |
| upper bound of `limits.max_iterations`             | none              | 2²⁰   | A canceled generator stops between pixels, so one pixel bounds the delay: at 2²⁰ at most 8 ms (Julia) and 35 ms (Newton of degree 8), while 10⁸ iterations ran 0.4 s past the deadline   |
| upper bound of `limits.max_depth`                  | none              | none  | The resolution limits the depth (Sierpiński 12, Koch 9 at 32768²), and those polygons take at most 14% of the RGBA buffer                                                                |
| PNG compression                                    | default           | kept  | `BestSpeed` is only 21% faster (the bar was 2×) and up to 18% larger                                                                                                                     |
| `jpegQuality`                                      | 90                | kept  | 85 is only 15% smaller (the bar was 20%)                                                                                                                                                 |
| `blockRows`                                        | 16                | kept  | 8 rows are within 5%; 64 rows are 8–58% slower                                                                                                                                           |
| `defaultRowsPerBlock`                              | 16                | kept  | Fallback of `figure.ForRows` for `rowsPerBlock <= 0`; the generators pass their own                                                                                                      |
| `bandPixels`                                       | 1<<20             | 1<<19 | 18–24% faster at 32768 wide with the same scratch; no change up to 4096 wide. Images wider than 4096 get other anti-aliased pixels: at most 1 per channel in tens of pixels of 6000×1000 |
| `maxBandRows`                                      | 128               | kept  | Bounds the masks of narrow images; a 1024² image still has 8 bands for 8 workers                                                                                                         |
| `scratchCap`                                       | 32 MiB            | kept  | 16 MiB is 26–31% slower at 4096², 64 MiB gives nothing more                                                                                                                              |
| `maskBytes`                                        | 8                 | kept  | A property of `x/image/vector` above 512 px, not a setting                                                                                                                               |
| `checkEvery`, `fillCheckEvery`, `strokeCheckEvery` | 1<<20, 4096, 1024 | kept  | Cancellation within 10 ms at 4096² with the default `max_iterations`, within 35 ms at its bound                                                                                          |
| `maxChords`                                        | 1024              | kept  | A guard: the widest line allowed (a quarter of 32768) needs 143 chords for a half circle                                                                                                 |

Not calibrated, because they set the look and not the cost: `maxSagitta`, `newtonFade`, `paletteSize`,
`sierpinskiMinSide`, `kochMinSegment`, the Lissajous point limits, `newtonMinDeriv2` and `minNewtonEpsilon`.
