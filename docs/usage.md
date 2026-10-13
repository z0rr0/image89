# Usage

This is the reference for the CLI and the HTTP API of Image89. [README](../README.md) has a short overview.

## CLI

```sh
./image89                                          # writes output.png: mandelbrot, 1024×1024
./image89 -w 640 -h 480 -t julia -f jpeg -o pic   # writes pic.jpg
./image89 -c config.toml -w 8192 -h 8192 -o big   # limits from a config, see below
```

`-h` sets the image height, so the usage is available only with `-help`.

### Output file

- If the `-o` name has no extension, Image89 adds the extension of the format: `.png` or `.jpg`.
  If the name has an extension, Image89 keeps it: `-o pic.png -f jpeg` writes a JPEG image into `pic.png`.
- `-o` must name a file. A value without a file name, for example `-o dir/`, causes an error.
- Image89 never overwrites a file. If the file exists, the run fails with `output file already exists`.
  The file does not change.
- If the generation fails, Image89 removes the unfinished file. This also occurs when you stop the run
  with Ctrl-C (SIGINT) or SIGTERM.

### Log and exit codes

After Image89 creates an image, it writes a log record to stderr: the path, the size, the number of bytes
and the duration.

| Exit code | When          |
|-----------|---------------|
| `0`       | success       |
| `1`       | an error      |
| `2`       | invalid flags |

### Limits

The CLI uses the same `[limits]` as the server. With the defaults, an image has at most
16 777 216 pixels, so `-w 8192 -h 8192` fails. Image89 must generate and encode the image within 15s.

For larger images, increase `limits.max_pixels` and `limits.timeout` in a config and pass it with `-c`.
The CLI checks the full config, so `server.write_timeout` must be more than
`limits.queue_timeout + limits.timeout` in CLI mode too:

```toml
[server]
write_timeout = "130s"

[limits]
max_pixels = 67108864 # 8192 × 8192
timeout = "120s"
```

## HTTP API

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
| `GET /favicon.ico`                        | site icon (`image/vnd.microsoft.icon`)     |

- `HEAD` works for each `GET` route.
- The type is necessary in the image path. The path `/` gives the index page.
- The `format` value is `png` or `jpeg` in any letter case.
- A path without the trailing slash, for example `/julia/640x480`, gets a `307` redirect to the path
  with the slash. The query stays the same.

### Status codes

| Status                   | When                                                                                                    |
|--------------------------|---------------------------------------------------------------------------------------------------------|
| `200`                    | image (`image/png` or `image/jpeg` with `Content-Length`) or page (`text/html; charset=utf-8`)          |
| `206`                    | part of a page or of the favicon for a request with `Range`                                             |
| `304`                    | the page did not change: `If-None-Match` contains its `ETag`                                            |
| `307`                    | the path has no trailing slash                                                                          |
| `400`                    | invalid size (`640X480`, a side outside 32–32768), unknown format, more than `limits.max_pixels` pixels |
| `404`                    | unknown type, unknown path                                                                              |
| `405`                    | a method other than `GET` and `HEAD`                                                                    |
| `412`                    | `If-Match` of a request for a page or the favicon does not match                                        |
| `416`                    | `Range` of a request for a page or the favicon is outside of it                                         |
| `503` + `Retry-After: 1` | all `limits.max_concurrent` slots are busy, and the queue is full or `limits.queue_timeout` has passed  |
| `503`                    | the server did not generate and encode the image within `limits.timeout`                                |
| `500`                    | unexpected error; the response has no details, the log has them                                         |

A `412` response has no body. Other `4xx` responses to `GET` requests contain a plain text error message.
If a client disconnects before its image is ready, the server sends no response. The access log records
this request with `status=0`.

## Headers and logs

The server sends images and pages with `Cache-Control: public, max-age=<seconds>`. The value is
`server.cache_max_age` rounded down to whole seconds: `"1h"` gives `max-age=3600`. If `cache_max_age` is
less than 1s, for example `"0s"`, the server does not send this header. For one version and one config,
the same URL always gives the same image or page, so caches can keep it. After a change of the presets or
of the version, clients get the new image or page only when `cache_max_age` expires.

Each page also has an `ETag`. When the cache time expires, the browser sends this tag in `If-None-Match`.
If the page did not change, the server returns `304` without a body. Without `Cache-Control`, the page has
no explicit cache time, and the browser can check it with its tag. Error responses, also `412` and `416`,
have no `Cache-Control` and no `ETag`.

The favicon is embedded in the binary. It uses the same cache header as generated images.

Each response has an `X-Request-ID` header. If the request has a valid `X-Request-ID`, the server uses it.
A valid ID has up to 64 letters, digits, `-` and `_`. If the ID is not valid or not present, the server
generates a UUID.

The server logs each request with its ID, status, size and duration. The log level is warn for `4xx`,
error for `5xx` and info for all other responses.

## Shutdown

The server stops on SIGINT or SIGTERM:

1. It does not accept new connections.
2. Active requests can finish within `server.shutdown_timeout` (10s by default).
3. After this time, the server cancels their generation and closes the connections.

With `-s`, the image flags `-w`, `-h`, `-t`, `-o` and `-f` have no effect. The server logs a warning about them.
