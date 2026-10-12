package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

var errBoom = errors.New("boom")

// testServer answers /ok/ with 1000 bytes, /busy/ with 503 and /broken/ with
// a body shorter than its Content-Length.
func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 1000))
	})
	mux.HandleFunc("GET /busy/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("GET /broken/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write(make([]byte, 10))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestPercentile(t *testing.T) {
	t.Parallel()

	ten := make([]time.Duration, 10)
	for i := range ten {
		ten[i] = time.Duration(i+1) * time.Millisecond
	}
	tests := []struct {
		name   string
		sorted []time.Duration
		p      int
		want   time.Duration
	}{
		{name: "one p50", sorted: ten[:1], p: 50, want: time.Millisecond},
		{name: "one p99", sorted: ten[:1], p: 99, want: time.Millisecond},
		{name: "ten p50", sorted: ten, p: 50, want: 5 * time.Millisecond},
		{name: "ten p95", sorted: ten, p: 95, want: 10 * time.Millisecond},
		{name: "ten p10", sorted: ten, p: 10, want: time.Millisecond},
		{name: "ten p100", sorted: ten, p: 100, want: 10 * time.Millisecond},
		{name: "two p50", sorted: ten[:2], p: 50, want: time.Millisecond},
		{name: "two p51", sorted: ten[:2], p: 51, want: 2 * time.Millisecond},
	}
	for _, tt := range tests {
		if got := percentile(tt.sorted, tt.p); got != tt.want {
			t.Errorf("%s: percentile = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestThrottle(t *testing.T) {
	t.Parallel()

	r := strings.NewReader("abc")
	if got := throttle(t.Context(), r, 0); got != r {
		t.Error("throttle with rate 0 changed the reader")
	}

	const size, rate = 64 << 10, 32 << 10
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		n, err := io.Copy(io.Discard, throttle(t.Context(), bytes.NewReader(make([]byte, size)), rate))
		if err != nil || n != size {
			t.Fatalf("copy = %d, %v; want %d, nil", n, err, size)
		}
		if elapsed := time.Since(start); elapsed != 2*time.Second {
			t.Errorf("reading %d bytes at %d B/s took %v, want 2s", size, rate, elapsed)
		}
	})
	// A deadline ends a pause at once instead of after the whole body.
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := io.Copy(io.Discard, throttle(ctx, bytes.NewReader(make([]byte, size)), rate))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("copy with a deadline = %v, want %v", err, context.DeadlineExceeded)
		}
		if elapsed := time.Since(start); elapsed != 500*time.Millisecond {
			t.Errorf("copy with a deadline of 500ms took %v", elapsed)
		}
	})
}

func TestFetchTimeout(t *testing.T) {
	t.Parallel()

	// 1000 bytes at 100 B/s would take 10s; the timeout stops the pauses too.
	srv := testServer(t)
	start := time.Now()
	r := fetch(t.Context(), srv.Client(), srv.URL+"/ok/", 100, 100*time.Millisecond)
	if !errors.Is(r.err, context.DeadlineExceeded) {
		t.Errorf("fetch with a timeout = %v, want %v", r.err, context.DeadlineExceeded)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("fetch with a timeout of 100ms took %v", elapsed)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	srv := testServer(t)
	opts := options{url: srv.URL, paths: []string{"/ok/", "/busy/", "/broken/"}, clients: 2, requests: 3, timeout: time.Minute}
	results := load(t.Context(), srv.Client(), opts)
	if len(results) != 6 {
		t.Fatalf("got %d results, want 6", len(results))
	}

	got := make(map[string]int)
	var okBytes int64
	for _, s := range summarize(results) {
		got[s.key] = s.count
		if s.key == "200" {
			okBytes = s.bytes
		}
	}
	// Both clients take every path once, the second one from /busy/ on.
	want := map[string]int{"200": 2, "503": 2, errorKey: 2}
	for key, count := range want {
		if got[key] != count {
			t.Errorf("%s: %d requests, want %d (all: %v)", key, got[key], count, got)
		}
	}
	if okBytes != 2000 {
		t.Errorf("200 responses have %d bytes, want 2000", okBytes)
	}
}

func TestLoadCanceled(t *testing.T) {
	t.Parallel()

	srv := testServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opts := options{url: srv.URL, paths: []string{"/ok/"}, clients: 3, requests: 5, timeout: time.Minute}
	if results := load(ctx, srv.Client(), opts); len(results) != 0 {
		t.Errorf("a canceled load sent %d requests, want 0", len(results))
	}
}

func TestFetchErrors(t *testing.T) {
	t.Parallel()

	srv := testServer(t)
	client := srv.Client()
	srv.Close()
	if r := fetch(t.Context(), client, srv.URL+"/ok/", 0, time.Second); r.err == nil || r.status != 0 {
		t.Errorf("fetch from a closed server = status %d, %v; want no response and an error", r.status, r.err)
	}
	if r := fetch(t.Context(), client, "://bad", 0, time.Second); r.err == nil {
		t.Error("fetch of an invalid URL has no error")
	}
}

func TestReport(t *testing.T) {
	t.Parallel()

	results := []result{
		{status: http.StatusOK, latency: 10 * time.Millisecond, bytes: 1 << 20},
		{status: http.StatusOK, latency: 30 * time.Millisecond, bytes: 1 << 20},
		{status: http.StatusServiceUnavailable, latency: 2 * time.Millisecond},
		{latency: time.Millisecond, err: errBoom},
	}
	var buf bytes.Buffer
	if err := report(&buf, results, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	wantLines := [][]string{
		{"status", "requests", "p50 ms", "p95 ms", "p99 ms", "max ms", "MiB"},
		{"200", "2", "10.0", "30.0", "30.0", "30.0", "2.0"},
		{"503", "1", "2.0", "2.0", "2.0", "2.0", "0.0"},
		{"error", "1", "1.0", "1.0", "1.0", "1.0", "0.0"},
		{"4 requests in 2s, 2.00 req/s"},
		{"first error: boom"},
	}
	if len(lines) != len(wantLines) {
		t.Fatalf("report has %d lines, want %d:\n%s", len(lines), len(wantLines), buf.String())
	}
	for i, want := range wantLines {
		for _, field := range want {
			if !strings.Contains(lines[i], field) {
				t.Errorf("line %d %q has no %q", i, lines[i], field)
			}
		}
	}
}

// failWriter fails every write.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errBoom }

// failAfter fails the writes after the first n; calls counts all of them.
type failAfter struct {
	n     int
	calls int
}

func (w *failAfter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > w.n {
		return 0, errBoom
	}
	return len(p), nil
}

func TestReportWriteErrors(t *testing.T) {
	t.Parallel()

	results := []result{{status: http.StatusOK, latency: time.Millisecond}, {err: errBoom}}
	all := &failAfter{n: 1 << 30}
	if err := report(all, results, time.Second); err != nil {
		t.Fatal(err)
	}
	// Every write of the report, from the table to the error line, may fail.
	for n := range all.calls {
		if err := report(&failAfter{n: n}, results, time.Second); !errors.Is(err, errBoom) {
			t.Errorf("report failing after %d of %d writes = %v, want %v", n, all.calls, err, errBoom)
		}
	}
}

func TestParseFlags(t *testing.T) {
	t.Parallel()

	opts, err := parseFlags([]string{"-url", "http://host:1/", "-path", "/a/", "-path", "/b/", "-c", "3", "-n", "4", "-rate", "100", "-timeout", "5s"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := options{url: "http://host:1", paths: []string{"/a/", "/b/"}, clients: 3, requests: 4, rate: 100, timeout: 5 * time.Second}
	if opts.url != want.url || strings.Join(opts.paths, ",") != strings.Join(want.paths, ",") ||
		opts.clients != want.clients || opts.requests != want.requests || opts.rate != want.rate || opts.timeout != want.timeout {
		t.Errorf("parseFlags = %+v, want %+v", opts, want)
	}

	opts, err = parseFlags(nil, io.Discard)
	if err != nil || len(opts.paths) != 1 || opts.paths[0] != defaultPath || opts.clients != 1 || opts.requests != 10 {
		t.Errorf("parseFlags defaults = %+v, %v", opts, err)
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	srv := testServer(t)
	tests := []struct {
		name   string
		args   []string
		code   int
		stdout string
	}{
		{name: "load", args: []string{"-url", srv.URL, "-path", "/ok/", "-c", "2", "-n", "2"}, code: exitOK, stdout: "4 requests in"},
		{name: "help", args: []string{"-help"}, code: exitOK},
		{name: "unknown flag", args: []string{"-x"}, code: exitUsage},
		{name: "positional", args: []string{"extra"}, code: exitUsage},
		{name: "no clients", args: []string{"-c", "0"}, code: exitUsage},
		{name: "no requests", args: []string{"-n", "0"}, code: exitUsage},
		{name: "negative rate", args: []string{"-rate", "-1"}, code: exitUsage},
		{name: "zero timeout", args: []string{"-timeout", "0s"}, code: exitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			if code := run(t.Context(), tt.args, &stdout, &stderr); code != tt.code {
				t.Fatalf("run(%q) = %d, want %d; stderr: %s", tt.args, code, tt.code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.stdout) {
				t.Errorf("stdout %q has no %q", stdout.String(), tt.stdout)
			}
		})
	}
}

func TestRunCanceled(t *testing.T) {
	t.Parallel()

	srv := testServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout bytes.Buffer
	if code := run(ctx, []string{"-url", srv.URL, "-path", "/ok/"}, &stdout, io.Discard); code != exitError {
		t.Errorf("run with a canceled ctx = %d, want %d", code, exitError)
	}
	if !strings.Contains(stdout.String(), "0 requests in") {
		t.Errorf("stdout %q does not report 0 requests", stdout.String())
	}
}

func TestRunReportError(t *testing.T) {
	t.Parallel()

	srv := testServer(t)
	var stderr bytes.Buffer
	if code := run(t.Context(), []string{"-url", srv.URL, "-path", "/ok/", "-n", "1"}, failWriter{}, &stderr); code != exitError {
		t.Errorf("run into a failing stdout = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), errBoom.Error()) {
		t.Errorf("stderr %q has no %q", stderr.String(), errBoom)
	}
}

func TestStatusKeys(t *testing.T) {
	t.Parallel()

	results := []result{{status: http.StatusServiceUnavailable}, {err: errBoom}, {status: http.StatusOK}}
	keys := make([]string, 0, 3)
	for _, s := range summarize(results) {
		keys = append(keys, s.key)
	}
	if got, want := strings.Join(keys, ","), strconv.Itoa(http.StatusOK)+",503,"+errorKey; got != want {
		t.Errorf("keys = %s, want %s", got, want)
	}
}
