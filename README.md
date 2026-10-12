# Image89

![Go](https://github.com/z0rr0/image89/workflows/Go/badge.svg)
![Version](https://img.shields.io/github/tag/z0rr0/image89.svg)
![License](https://img.shields.io/github/license/z0rr0/image89.svg)

It is a web application and CLI tool to generate math images.

> **In development.** The CLI and the HTTP server generate `mandelbrot`, `julia` and `newton` images;
> `sierpinski`, `koch` and `lissajous` are not implemented yet and give an "unknown figure kind" error (404 over HTTP).
> Only the Mandelbrot set has an info page so far.

## Usage

### CLI

```sh
./image89                                          # creates output.png, 1024×1024 mandelbrot
./image89 -w 640 -h 480 -t julia -f jpeg -o pic   # creates pic.jpg
./image89 -w 1920 -h 1080 -o wall.png             # the extension is kept as given
./image89 -v                                       # print version and exit
./image89 -help                                    # print usage
```

| Flag | Description |
|---|---|
| `-w` | image width (default 1024, min 32, max 65536) |
| `-h` | image height (default 1024, min 32, max 65536) |
| `-t` | image type: `mandelbrot` (default), `julia`, `newton`, `sierpinski`, `koch`, `lissajous` |
| `-o` | output file (default `output.<ext>`); an existing file is never overwritten |
| `-f` | output format: `png` (default) or `jpeg` |
| `-c` | path to a TOML config (optional) |
| `-s` | run as web server |
| `-v` | print version and exit |

`-h` is the image height, so help is available only via `-help`.

The output name gets the extension of the format (`.png` or `.jpg`) only if it has none, so
`-o pic.png -f jpeg` writes a JPEG image into `pic.png`. `-o` names a file: a value without a
file name such as `-o dir/` is rejected. An existing file is never overwritten:
the run fails with `output file already exists` and the file stays as it was. If generation fails
or is interrupted with Ctrl-C (SIGINT or SIGTERM), the unfinished file is removed. The log record
of a created image (path, size, bytes, duration) goes to stderr; the exit code is 0 on success,
1 on an error and 2 on invalid flags.

The CLI obeys the same `[limits]` as the server: with the defaults an image has at most
16 777 216 pixels (`-w 8192 -h 8192` is rejected) and must be generated and encoded within 30s.
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

| Request | Response |
|---|---|
| `GET /<type>/<W>x<H>/?format=<png\|jpeg>` | image of the given size, PNG by default |
| `GET /<type>/` | 1024×1024 image |
| `GET /<type>/info/` | page about the figure |
| `GET /` | index of figures with thumbnails |
| `GET /health` | `200 OK`, used by the compose healthcheck |
| `GET /version` | application version |

`HEAD` works for every `GET` route. The type is required in the server (the index takes `/`), and the
format is `png` or `jpeg` in any case. A path without the trailing slash, such as `/julia/640x480`,
is redirected with `307` to the path with the slash, keeping the query.

| Status | When |
|---|---|
| `200` | image (`image/png` or `image/jpeg` with `Content-Length`) or page (`text/html; charset=utf-8`) |
| `307` | the path has no trailing slash |
| `400` | invalid size (`640X480`, a side outside 32–65536), unknown format, more than `limits.max_pixels` pixels, a JPEG side over 65535 |
| `404` | unknown or not implemented type, unknown path |
| `405` | a method other than `GET` and `HEAD` |
| `503` + `Retry-After: 1` | all `limits.max_concurrent` slots are busy and the queue is full or `limits.queue_timeout` passed |
| `503` | the image was not generated and encoded within `limits.timeout` |
| `500` | unexpected error; the response has no details, the log has them |

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
not replace these limits. An nginx example:

```nginx
limit_conn_zone $binary_remote_addr zone=image89_conn:10m;
limit_req_zone  $binary_remote_addr zone=image89_req:10m rate=5r/s;

server {
    location / {
        limit_conn image89_conn 4;
        limit_req  zone=image89_req burst=20 nodelay;
        proxy_set_header X-Request-ID $request_id;
        proxy_pass http://127.0.0.1:8080;
    }
}
```

## Configuration

Both modes work without a config file. `-c config.toml` loads a TOML file on top of the built-in
defaults, so the file lists only the values it changes; unknown keys and invalid values are errors.
Without `-c` no file is read. [docs/config.toml](docs/config.toml) is an annotated example where
every value equals the default.

| Section | Contents |
|---|---|
| `[server]` | `addr`, `debug`, HTTP timeouts, `shutdown_timeout`, `cache_max_age` of images (`"0s"` disables the header) |
| `[limits]` | `max_pixels` per image, `max_concurrent` images, `max_queued` requests and their `queue_timeout`, generation `timeout`, `workers` per image (0 = `GOMAXPROCS`), `max_iterations` and `max_depth` for presets |
| `[types.<type>]` | fixed preset of each figure: iterations or depth, visible area, colors as `"#rrggbb"`, line width and padding as fractions of the shorter image side |

Durations are Go duration strings such as `"10s"`. `server.read_header_timeout` must not exceed
`server.read_timeout`, and `server.write_timeout` must exceed `limits.queue_timeout + limits.timeout`.

A generation slot is held until the response is written, so the peak memory for images is about

```text
max_concurrent × (4 · max_pixels + 32 MiB + encoded image size)
```

where `4 · max_pixels` is the RGBA buffer and 32 MiB caps the rasterizer scratch memory.
With the defaults (`max_concurrent = 2`, `max_pixels = 16777216`) that is about 2 × (64 + 32) MiB
plus the encoded images.

## Build

Requires Go 1.27 and [golangci-lint](https://golangci-lint.run/) v2.

```sh
make build   # ./image89 with version info from git
make test    # linters, govulncheck and tests with -race
make dist    # cross-compiled binaries in dist/
make run     # build and start the server
make clean
```

## Docker

```sh
make docker                          # build z0rr0/image89 for the host arch
docker run --rm z0rr0/image89 -v
cp docs/config.toml config.toml      # compose mounts ./config.toml
docker compose up -d
```

Create `./config.toml` before `docker compose up`: if it is missing, Docker creates a directory
in its place.

`make docker-push` publishes a linux/amd64 + linux/arm64 image; it requires a git tag.

## License

[MIT](LICENSE)
