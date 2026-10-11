# Image89

It is a web application and CLI tool to generate math images.

> **In development.** The flags and endpoints below describe the target behavior;
> image generation and the HTTP server are not implemented yet.

## Usage

### CLI

```sh
./image89 -w 640 -h 480 -t julia -f jpeg -o pic   # creates pic.jpg
./image89 -v                                       # print version and exit
./image89 -help                                    # print usage
```

| Flag | Description |
|---|---|
| `-w` | image width (default 1024, min 32, max 65536) |
| `-h` | image height (default 1024, min 32, max 65536) |
| `-t` | image type (default `mandelbrot`) |
| `-o` | output file (default `output.<ext>`); an existing file is never overwritten |
| `-f` | output format: `png` (default) or `jpeg` |
| `-c` | path to a TOML config (optional) |
| `-s` | run as web server |
| `-v` | print version and exit |

`-h` is the image height, so help is available only via `-help`.

### Server

```sh
./image89 -s
```

| Request | Response |
|---|---|
| `GET /<type>/<W>x<H>/?format=<png\|jpeg>` | image of the given size |
| `GET /<type>/` | 1024×1024 image |
| `GET /<type>/info/` | page about the figure |
| `GET /` | index of figures |
| `GET /health` | `200 OK` |
| `GET /version` | application version |

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
