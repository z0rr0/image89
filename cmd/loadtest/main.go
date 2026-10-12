// Command loadtest sends GET requests to a running Image89 server from
// concurrent clients and prints the latency percentiles per response status.
// Every client sends its requests one after another; -rate makes the clients
// read the responses slowly. docs/benchmarks.md describes the scenarios.
//
//	go run ./cmd/loadtest -c 4 -n 20 -path /mandelbrot/ -path /koch/
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Exit codes returned by run.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// defaultPath is the requested path when no -path is given.
const defaultPath = "/mandelbrot/"

var (
	errUnexpectedArgs = errors.New("unexpected positional arguments")
	errInvalidFlag    = errors.New("invalid flag value")
)

// options holds the parsed command line.
type options struct {
	url      string
	paths    []string
	clients  int
	requests int
	// rate is the number of response bytes a client reads per second; 0 means no limit.
	rate    int
	timeout time.Duration
}

// parseFlags parses the command line arguments without the program name.
// Errors and usage are printed to stderr; -help returns flag.ErrHelp.
func parseFlags(args []string, stderr io.Writer) (options, error) {
	fs := flag.NewFlagSet("loadtest", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var opts options
	fs.StringVar(&opts.url, "url", "http://127.0.0.1:8080", "server base URL")
	fs.Func("path", "request path, repeatable; clients take the paths in turn (default "+defaultPath+")", func(s string) error {
		opts.paths = append(opts.paths, s)
		return nil
	})
	fs.IntVar(&opts.clients, "c", 1, "concurrent clients")
	fs.IntVar(&opts.requests, "n", 10, "requests per client")
	fs.IntVar(&opts.rate, "rate", 0, "response bytes a client reads per second, 0 means no limit")
	fs.DurationVar(&opts.timeout, "timeout", 2*time.Minute, "limit of one request, including reading the response")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %q\n", fs.Args())
		fs.Usage()
		return options{}, errors.Join(errUnexpectedArgs, fmt.Errorf("%q", fs.Args()))
	}
	if opts.clients < 1 || opts.requests < 1 || opts.rate < 0 || opts.timeout <= 0 {
		err := errors.Join(errInvalidFlag, errors.New("want -c >= 1, -n >= 1, -rate >= 0 and -timeout > 0"))
		_, _ = fmt.Fprintln(stderr, err)
		return options{}, err
	}
	if len(opts.paths) == 0 {
		opts.paths = []string{defaultPath}
	}
	opts.url = strings.TrimSuffix(opts.url, "/")
	return opts, nil
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the program and returns its exit code. The report goes to
// stdout, usage and errors to stderr. SIGINT and SIGTERM stop the clients and
// the requests done so far are reported with exit code 1.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The timeout of a request is a deadline of its ctx (see fetch), which
	// also ends the pauses of -rate; Client.Timeout would not.
	client := &http.Client{
		Transport: &http.Transport{MaxIdleConnsPerHost: opts.clients, DisableCompression: true},
	}
	defer client.CloseIdleConnections()

	start := time.Now()
	results := load(ctx, client, opts)
	if err = report(stdout, results, time.Since(start)); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitError
	}
	if ctx.Err() != nil {
		return exitError
	}
	return exitOK
}
