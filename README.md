# Image89

![Go](https://github.com/z0rr0/image89/workflows/Go/badge.svg)
![Version](https://img.shields.io/github/tag/z0rr0/image89.svg)
![License](https://img.shields.io/github/license/z0rr0/image89.svg)

Image89 draws six mathematical figures of any size from 32 to 32768 pixels per side: the Mandelbrot and
Julia sets, the Newton fractal, the Sierpiński triangle, the Koch snowflake and a Lissajous curve. One binary
works as a CLI tool that writes an image file and as a web server that serves the images, with a page
about the history and the math of every figure.

## Quick start

```sh
make build
./image89 -t julia -w 1920 -h 1080 -o julia      # writes julia.png
./image89 -s                                     # open http://localhost:8080/
docker run --rm -p 8080:8080 z0rr0/image89      # the server in Docker
```

## Usage

### CLI

```sh
./image89                                          # creates output.png, 1024×1024 mandelbrot
./image89 -w 640 -h 480 -t julia -f jpeg -o pic   # creates pic.jpg
./image89 -w 1920 -h 1080 -o wall.png             # the extension is kept as given
./image89 -v                                       # print version and exit
./image89 -help                                    # print usage
```

| Flag | Description                                                                              |
|------|------------------------------------------------------------------------------------------|
| `-w` | image width (default 1024, min 32, max 32768)                                            |
| `-h` | image height (default 1024, min 32, max 32768)                                           |
| `-t` | image type: `mandelbrot` (default), `julia`, `newton`, `sierpinski`, `koch`, `lissajous` |
| `-o` | output file (default `output.<ext>`); an existing file is never overwritten              |
| `-f` | output format: `png` (default) or `jpeg`                                                 |
| `-c` | path to a TOML config (optional)                                                         |
| `-s` | run as web server                                                                        |
| `-v` | print version and exit                                                                   |

`-h` is the image height, so help is available only via `-help`.

The output name gets the extension of the format (`.png` or `.jpg`) only if it has none, so
`-o pic.png -f jpeg` writes a JPEG image into `pic.png`. `-o` names a file: a value without a
file name such as `-o dir/` is rejected. An existing file is never overwritten:
the run fails with `output file already exists` and the file stays as it was. If generation fails
or is interrupted with Ctrl-C (SIGINT or SIGTERM), the unfinished file is removed. The log record
of a created image (path, size, bytes, duration) goes to stderr; the exit code is 0 on success,
1 on an error and 2 on invalid flags.

The CLI obeys the same `[limits]` as the server: with the defaults an image has at most
16 777 216 pixels (`-w 8192 -h 8192` is rejected) and must be generated and encoded within 15s.
Raise `limits.max_pixels` and `limits.timeout` in a config passed with `-c` for larger images;
the config check still requires `server.write_timeout` to exceed `limits.queue_timeout + limits.timeout`:

```toml
[server]
write_timeout = "130s"

[limits]
max_pixels = 67108864 # 8192 × 8192
timeout = "120s"
```

### Server

```sh
./image89 -s                       # listens on :8080 with the built-in defaults
./image89 -s -c config.toml        # address, timeouts, limits and presets from a config
curl -o pic.jpg 'http://localhost:8080/julia/640x480/?format=jpeg'
```

| Request                                   | Response                                  |
|-------------------------------------------|-------------------------------------------|
| `GET /<type>/<W>x<H>/?format=<png\|jpeg>` | image of the given size, PNG by default   |
| `GET /<type>/`                            | 1024×1024 image                           |
| `GET /<type>/info/`                       | page about the figure                     |
| `GET /`                                   | index of figures with thumbnails          |
| `GET /health`                             | `200 OK`, used by the compose healthcheck |
| `GET /version`                            | application version                       |

`HEAD` works for every `GET` route. The type is required in the server (the index takes `/`), and the
format is `png` or `jpeg` in any case. A path without the trailing slash, such as `/julia/640x480`,
is redirected with `307` to the path with the slash, keeping the query.

| Status                   | When                                                                                                    |
|--------------------------|---------------------------------------------------------------------------------------------------------|
| `200`                    | image (`image/png` or `image/jpeg` with `Content-Length`) or page (`text/html; charset=utf-8`)          |
| `307`                    | the path has no trailing slash                                                                          |
| `400`                    | invalid size (`640X480`, a side outside 32–32768), unknown format, more than `limits.max_pixels` pixels |
| `404`                    | unknown type, unknown path                                                                              |
| `405`                    | a method other than `GET` and `HEAD`                                                                    |
| `503` + `Retry-After: 1` | all `limits.max_concurrent` slots are busy and the queue is full or `limits.queue_timeout` passed       |
| `503`                    | the image was not generated and encoded within `limits.timeout`                                         |
| `500`                    | unexpected error; the response has no details, the log has them                                         |

A `4xx` response body is a plain text error message. A client that disconnects before its image is
ready gets no response, and the access log records the request with `status=0`.

Images are sent with `Cache-Control: public, max-age=<server.cache_max_age>` (no header when it is
`"0s"`). The same URL always gives the same image for one version and config, so caches may keep it;
a change of presets or of the version reaches clients only after `cache_max_age` expires.

Every response has an `X-Request-ID` header: a valid incoming one (up to 64 letters, digits, `-` and `_`)
is reused, otherwise a UUID is generated. Each request is logged with its ID, status, size and duration;
the level is info, warn for `4xx` and error for `5xx`.

The server stops on SIGINT or SIGTERM: it stops accepting connections and lets active requests
finish within `server.shutdown_timeout`; after that their generation is canceled and the connections
are closed. Image flags (`-w -h -t -o -f`) are ignored with `-s`, and a warning is logged.

Generation is CPU-bound, so the server limits it itself: at most `limits.max_concurrent` images are
generated and sent at once, at most `limits.max_queued` requests wait for a slot. A rate limit in a
reverse proxy is a useful addition, but it bounds requests per second, not concurrent work, so it does
not replace these limits.

**Run the server behind a reverse proxy that buffers responses.** A slot is held until the whole image
is sent, so a client that reads slowly holds a slot until `server.write_timeout` (30s) cuts its response:
two clients reading 4096×4096 images at 128 KiB/s make every other request get `503` for 30 seconds
with the default two slots. nginx buffers proxied responses by default (`proxy_buffering on`): it takes
the image at once, frees the slot and sends the image to the slow client by itself. Keep the buffering
on.

[docs/nginx.conf](docs/nginx.conf) is a complete example for `/etc/nginx/conf.d/`, checked with nginx 1.31:

- TLS and a redirect from HTTP;
- buffering of responses, so slow clients do not hold generation slots;
- a shared cache of the images: Image89 sends them with `Cache-Control`, so nginx keeps them for
  `server.cache_max_age`, and `proxy_cache_lock` lets concurrent requests of one image wait for a single
  generation (8 requests of a new 4096×4096 image made one); pages and errors are not cached;
- 5 requests per second with a burst of 20 and 8 concurrent requests per client address, `429` above them;
- timeouts that match the defaults of Image89 and its request ID in `X-Request-ID`.

## Figures

| Type         | Figure                                         | Drawing                                                                        |
|--------------|------------------------------------------------|--------------------------------------------------------------------------------|
| `mandelbrot` | Mandelbrot set                                 | every pixel is a point of the complex plane, colored by its smooth escape time |
| `julia`      | Julia set of z² + c                            | the same with `c` fixed by the preset and the pixel as the start value         |
| `newton`     | Newton fractal of zⁿ − 1                       | every pixel is colored by the root that Newton's method reaches from it        |
| `sierpinski` | Sierpiński triangle                            | 3^d filled triangles                                                           |
| `koch`       | Koch snowflake                                 | an outline of 3·4^d segments drawn as a line                                   |
| `lissajous`  | Lissajous curve x = sin(a·t + δ), y = sin(b·t) | a closed line through points of the curve                                      |

The pixel fractals show the whole visible area of the preset at any proportions: one axis shows more
of the plane, nothing is stretched. The other three figures are built as geometry, fitted into the image
without distortion with a padding and anti-aliased. Their detail follows the image size: the depth of
`sierpinski` drops until the smallest triangles are at least 4 px wide (a 32×32 image shows depth 2), the
depth of `koch` until every segment is at least 1 px long. Lines are `line_width` of the shorter side
wide, but at least 1 px, with round joins. With `points = 0` the Lissajous curve takes
⌈π·R·max (a, b)⌉ points, where R is half the shorter side, from 256 to 65 536, so that neighboring points
are about 2 px apart. Every type has an info page at `/<type>/info/` with its history and formulas.

## Configuration

Both modes work without a config file. `-c config.toml` loads a TOML file on top of the built-in
defaults, so the file lists only the values it changes; unknown keys and invalid values are errors.
Without `-c` no file is read. [docs/config.toml](docs/config.toml) is an annotated example where
every value equals the default.

| Section          | Contents                                                                                                                                                                                                     |
|------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `[server]`       | `addr`, `debug`, HTTP timeouts, `shutdown_timeout`, `cache_max_age` of images (`"0s"` disables the header)                                                                                                   |
| `[limits]`       | `max_pixels` per image, `max_concurrent` images, `max_queued` requests and their `queue_timeout`, generation `timeout`, `workers` per image (0 = `GOMAXPROCS`), `max_iterations` and `max_depth` for presets |
| `[types.<type>]` | fixed preset of each figure: iterations or depth, visible area, colors as `"#rrggbb"`, line width and padding as fractions of the shorter image side                                                         |

Durations are Go duration strings such as `"10s"`. `server.read_header_timeout` must not exceed
`server.read_timeout`, and `server.write_timeout` must exceed `limits.queue_timeout + limits.timeout`.

A generation slot is held until the response is written, so the peak heap for images is about

```text
max_concurrent × (4 · max_pixels + 32 MiB + polygons + 2 × encoded image size)
```

where `4 · max_pixels` is the RGBA buffer, 32 MiB caps the rasterizer masks of a contour figure, the
polygons take a few MiB with the default presets (up to 17 MiB at 4096×4096 with depth 8) and the
encoded image grows its buffer by doubling (a 4096×4096 PNG takes up to 6.4 MB). With the defaults
(`max_concurrent = 2`, `max_pixels = 16777216`) that is about 2 × 125 MiB. The process takes up to about
twice the peak heap, because the Go GC lets the heap grow by `GOGC=100` between collections: 0.42–0.51 GB
were measured under load. Set the `GOMEMLIMIT` environment variable near the formula, e.g.
`GOMEMLIMIT=300MiB`, to make the GC collect earlier: that gave 0.34 GB at the same throughput.

PNG and JPEG encoding runs in one goroutine and takes half or more of a request, so slots add throughput
even though every image uses all cores (`limits.workers = 0`). On 8 cores `max_concurrent = 4` serves 44%
more 1024×1024 images per second than 2 and takes up to about 0.75 GB at `max_pixels`; on fewer cores two
slots are enough. [docs/benchmarks.md](docs/benchmarks.md) has the measurements behind the defaults.

## Development

Requires Go 1.27 and [golangci-lint](https://golangci-lint.run/) v2. Besides the standard library the
code uses [go-toml](https://github.com/pelletier/go-toml) for the config and
[golang.org/x/image/vector](https://pkg.go.dev/golang.org/x/image/vector) to rasterize the contour figures.

```sh
make build   # ./image89 with version info from git
make test    # linters, govulncheck and tests with -race
make bench   # benchmarks; BENCH=<regexp> selects them, BENCHFLAGS adds flags such as -count=10
make fuzz    # fuzzing of the parsers of client input, FUZZTIME (30s) per target
make dist    # cross-compiled binaries in dist/
make run     # build and start the server
make clean
go run ./cmd/loadtest -c 4 -n 20 -path /mandelbrot/   # load a running server, -help lists the flags
```

`make test` runs the seed corpus of the fuzz targets as ordinary tests. A failing input found by
`make fuzz` is saved in `testdata/fuzz/<target>/` and is committed with the fix as a regression test.
[docs/benchmarks.md](docs/benchmarks.md) has the measurements behind the default limits and the internal
constants, and the commands to repeat them.

## Docker

```sh
make docker                          # build z0rr0/image89 for the host arch
docker run --rm z0rr0/image89 -v
cp docs/config.toml config.toml      # compose mounts ./config.toml
docker compose up -d
```

Create `./config.toml` before `docker compose up`: if it is missing, Docker creates a directory
in its place.

Docker sends SIGTERM on stop and SIGKILL after a grace period, 10s by default, which equals the
default `server.shutdown_timeout`. The compose file sets `stop_grace_period: 15s`, so a graceful
shutdown has time to finish; raise it together with `shutdown_timeout`, and pass
`--stop-timeout` (`docker run`) or `-t` (`docker stop`) when running the image without compose.

`make docker-push` publishes a linux/amd64 + linux/arm64 image; it requires a git tag.

## License

[MIT](LICENSE)
