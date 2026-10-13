# Deployment

To run the Image89 server in production, configure the generation limits, the reverse proxy,
the memory and Docker. The values in parentheses are the defaults from [config.toml](config.toml).

## Generation limits

Generation uses a lot of CPU, so the server limits it:

- The server generates and sends at most `limits.max_concurrent` (2) images at a time.
- At most `limits.max_queued` (8) requests wait for a free slot. Each request waits for up to
  `limits.queue_timeout` (2s).
- After a request gets a slot, the server must generate and encode the image within `limits.timeout` (15s).

If the queue is full or the wait time ends, the request gets `503` with `Retry-After: 1`.
If the generation takes too long, the request gets `503` without this header.

A rate limit in the reverse proxy is also useful. But it limits requests per second, not concurrent work,
so it does not replace these limits.

## Reverse proxy

Run the server behind a reverse proxy that buffers responses.

A generation slot stays busy until the server sends the whole image. A client that reads slowly holds
the slot until `server.write_timeout` (30s) stops its response. For example, two clients read 4096×4096
images at 128 KiB/s. With the default two slots, all other image requests get `503` for 30 seconds.

nginx buffers proxied responses by default (`proxy_buffering on`). It receives the image at once and
releases the slot. Then nginx sends the image to the slow client. Keep the buffering on.

[nginx.conf](nginx.conf) is a full example for `/etc/nginx/conf.d/`, checked with nginx 1.31. It has:

- TLS and a redirect from HTTP.
- Buffering of responses, so slow clients do not hold generation slots.
- A shared cache of images. Image89 sends images with `Cache-Control`, so nginx keeps them for
  `server.cache_max_age`. With `proxy_cache_lock`, concurrent requests for one image wait for one generation:
  8 requests for a new 4096×4096 image caused one generation. nginx does not cache pages and errors.
- Limits for each client address: 5 requests per second with a burst of 20, and 8 concurrent requests.
  Above these limits, nginx returns `429`.
- Timeouts that match the defaults of Image89, and the request ID in `X-Request-ID`.

## Memory

A generation slot stays busy until the server writes the response. The peak heap for images is about:

```text
max_concurrent × (4 · max_pixels + 32 MiB + polygons + 2 × encoded image size)
```

- `4 · max_pixels` is the RGBA buffer.
- 32 MiB is the maximum size of the rasterizer masks for a geometric figure.
- The polygons take a few MiB with the default presets, and up to 17 MiB at 4096×4096 with depth 8.
- The buffer of the encoded image grows by doubling. A 4096×4096 PNG takes up to 6.4 MB.

With the defaults (`max_concurrent = 2`, `max_pixels = 16777216`), this is about 2 × 125 MiB.

The process takes up to about twice the peak heap. The cause is the Go GC: with `GOGC=100`,
the heap can grow by 100% between two collections. In the load tests of [benchmarks.md](benchmarks.md)
with the default limits and 2 to 8 clients, the peak RSS was 0.42–0.51 GB.

To decrease it, set the `GOMEMLIMIT` environment variable near the value of the formula,
for example `GOMEMLIMIT=300MiB`. Then the GC collects earlier. With this value and 8 clients, the peak RSS
was 449 MB and the memory footprint was 338 MB, at about the same throughput.

## Sizing

PNG and JPEG encoding runs in one goroutine and takes half or more of the request time. So more slots
give more throughput, although each image already uses all cores (`limits.workers = 0`).

On the 8-core test machine of the benchmarks, `max_concurrent = 4` served 44% more 1024×1024 images
per second than 2. It took up to about 0.75 GB at `max_pixels`. On fewer cores, two slots are enough.
[benchmarks.md](benchmarks.md) has the measurements behind the defaults.

## Docker

When Docker stops a container, it sends SIGTERM. After a grace period, it sends SIGKILL.
The default grace period is 10s. This is equal to the default `server.shutdown_timeout`, so SIGKILL can
stop the graceful shutdown before it ends.

[docker-compose.yml](../docker-compose.yml) sets `stop_grace_period: 15s`. If you increase
`shutdown_timeout`, increase `stop_grace_period` too. Without compose, set the grace period with
`docker run --stop-timeout` or `docker stop -t`.

`make docker` builds the image only for the host architecture. `make docker-push` builds images for
linux/amd64 and linux/arm64 and pushes them to Docker Hub. It needs a git tag.
