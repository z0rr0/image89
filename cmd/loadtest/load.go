package main

import (
	"context"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"
)

// readChunk bounds one read of a throttled response, so the pauses between
// the reads stay short.
const readChunk = 4096

// result is the outcome of one request.
type result struct {
	// status is the response status, 0 when no response came.
	status int
	// latency lasts from sending the request to the end of the response body.
	latency time.Duration
	// bytes is the number of body bytes read.
	bytes int64
	// err is a failed request or a broken response body.
	err error
}

// load runs opts.clients clients, each sending opts.requests requests one
// after another, and returns all results. Client c takes the paths from
// index c on, so concurrent clients ask for different paths. The clients stop
// early when ctx is done.
func load(ctx context.Context, client *http.Client, opts options) []result {
	perClient := make([][]result, opts.clients)
	var wg sync.WaitGroup
	for c := range opts.clients {
		wg.Go(func() {
			results := make([]result, 0, opts.requests)
			for i := range opts.requests {
				if ctx.Err() != nil {
					break
				}
				path := opts.paths[(c+i)%len(opts.paths)]
				results = append(results, fetch(ctx, client, opts.url+path, opts.rate, opts.timeout))
			}
			perClient[c] = results
		})
	}
	wg.Wait()
	return slices.Concat(perClient...)
}

// fetch sends a GET request to url and reads the whole response body at
// most rate bytes per second (0 means no limit). The request, the body and
// the pauses of the rate take at most timeout together. The url comes from
// the -url flag: requests to the server the operator names are the purpose
// of the tool, so gosec G704 (SSRF) does not apply.
func fetch(ctx context.Context, client *http.Client, url string, rate int, timeout time.Duration) result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody) //nolint:gosec // G704: see the doc comment
	if err != nil {
		return result{err: err}
	}
	resp, err := client.Do(req) //nolint:gosec // G704: see the doc comment
	if err != nil {
		return result{latency: time.Since(start), err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	n, err := io.Copy(io.Discard, throttle(ctx, resp.Body, rate))
	return result{status: resp.StatusCode, latency: time.Since(start), bytes: n, err: err}
}

// throttle returns r limited to rate bytes per second on average, or r itself
// when rate is not positive. A pause of the rate ends early with the error
// of ctx when ctx is done.
func throttle(ctx context.Context, r io.Reader, rate int) io.Reader {
	if rate <= 0 {
		return r
	}
	wait := func(d time.Duration) error { return sleep(ctx, d) }
	return &throttled{r: r, rate: int64(rate), start: time.Now(), wait: wait}
}

// sleep waits for d or until ctx is done, whichever comes first, and returns
// the error of ctx in the second case.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// throttled reads at most readChunk bytes at a time and waits until the
// bytes read so far are due at the rate.
type throttled struct {
	r     io.Reader
	rate  int64
	start time.Time
	read  int64
	wait  func(time.Duration) error
}

// Read reads from the underlying reader and waits for the rate.
func (t *throttled) Read(p []byte) (int, error) {
	if len(p) > readChunk {
		p = p[:readChunk]
	}
	n, err := t.r.Read(p)
	t.read += int64(n)
	due := t.start.Add(time.Duration(t.read * int64(time.Second) / t.rate))
	if waitErr := t.wait(time.Until(due)); waitErr != nil {
		return n, waitErr
	}
	return n, err
}
