// Command image89 generates images of mathematical figures. By default it
// writes one image to a file; with -s it serves images over HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/z0rr0/image89/internal/cli"
	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/encode"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/pages"
	"github.com/z0rr0/image89/internal/render"
	"github.com/z0rr0/image89/internal/server"
)

// name is the service name shown in the build info line.
const name = "Image89"

// Exit codes returned by run.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

var errUnexpectedArgs = errors.New("unexpected positional arguments")

// Build metadata: Version, Revision and BuildDate are injected at build time via
// -ldflags "-X main.Version=... -X main.Revision=... -X main.BuildDate=...", so
// they must stay package-level variables — the linker cannot write anywhere else.
//
//nolint:gochecknoglobals // written by the linker at link time
var (
	// Version is a git version.
	Version = "v0.0.0"
	// Revision is a revision number.
	Revision = "git:0000000"
	// BuildDate is a build date.
	BuildDate = "1970-01-01T00:00:00"
	// GoVersion is a runtime Go language version.
	GoVersion = runtime.Version() // "go1.00.0"
)

// versionInfo returns the one-line build info, e.g.
// "Image89: v0.0.1 git:abc1234 go1.27.2 2026-10-11T10:00:00".
func versionInfo() string {
	return fmt.Sprintf("%s: %s %s %s %s", name, Version, Revision, GoVersion, BuildDate)
}

// options holds the parsed command line.
type options struct {
	width   int
	height  int
	kind    string
	output  string
	format  string
	config  string
	serve   bool
	version bool
	// set contains the names of the flags given explicitly on the command line.
	set map[string]bool
}

// parseFlags parses the command line arguments without the program name.
// Errors and usage are printed to stderr; -help returns flag.ErrHelp.
// Defining -h as the image height disables the flag package's built-in -h
// help shortcut, so help is available only via -help.
func parseFlags(args []string, stderr io.Writer) (options, error) {
	fs := flag.NewFlagSet("image89", flag.ContinueOnError)
	fs.SetOutput(stderr)

	opts := options{set: make(map[string]bool)}
	sides := fmt.Sprintf("(min %d, max %d)", figure.MinSide, figure.MaxSide)
	fs.IntVar(&opts.width, "w", figure.DefaultSide, "image width "+sides)
	fs.IntVar(&opts.height, "h", figure.DefaultSide, "image height "+sides)
	fs.StringVar(&opts.kind, "t", string(figure.DefaultKind), "image type: "+kindNames())
	fs.StringVar(&opts.output, "o", "", "output file (default output.<ext>; an existing file is never overwritten)")
	fs.StringVar(&opts.format, "f", string(encode.PNG), "output format: png or jpeg")
	fs.StringVar(&opts.config, "c", "", "path to TOML config (optional)")
	fs.BoolVar(&opts.serve, "s", false, "run as web server")
	fs.BoolVar(&opts.version, "v", false, "print version and exit")
	fs.Usage = func() {
		out := fs.Output()
		_, _ = fmt.Fprintf(out, "Usage: %s [flags]\n\n", fs.Name())
		_, _ = fmt.Fprintln(out, "Generates an image of a math figure into a file, or serves images over HTTP with -s.")
		_, _ = fmt.Fprintln(out, "Use -help to show this message: -h is the image height and requires a value.")
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Flags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %q\n", fs.Args())
		fs.Usage()
		return options{}, errors.Join(errUnexpectedArgs, fmt.Errorf("%q", fs.Args()))
	}

	fs.Visit(func(f *flag.Flag) { opts.set[f.Name] = true })
	return opts, nil
}

// kindNames returns the figure kinds as a comma-separated list for the usage.
func kindNames() string {
	kinds := figure.Kinds()
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, string(k))
	}
	return strings.Join(names, ", ")
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the program and returns its exit code. The version goes to
// stdout, logs and usage go to stderr. Canceling ctx stops the run as SIGINT
// and SIGTERM do.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	if opts.version {
		_, _ = fmt.Fprintln(stdout, versionInfo())
		return exitOK
	}

	// The config decides the debug mode, so its own errors are logged at the
	// default level and the logger is replaced once the config is loaded.
	slog.SetDefault(setupLogger(stderr, false))
	cfg, err := loadConfig(opts.config)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return exitError
	}
	slog.SetDefault(setupLogger(stderr, cfg.Server.Debug))

	// SIGINT or SIGTERM cancels ctx: the CLI stops generating and removes the
	// unfinished file, the server shuts down. Signals stay caught until run returns.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.serve {
		err = runServer(ctx, cfg, opts)
	} else {
		err = runCLI(ctx, cfg, opts)
	}
	if err != nil {
		slog.Error("failed to run", "error", err)
		return exitError
	}
	return exitOK
}

// loadConfig reads the config file given by -c or returns the defaults
// without looking for a file.
func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		return config.Default(), nil
	}
	return config.Load(path)
}

// runCLI generates one image into a new file.
func runCLI(ctx context.Context, cfg *config.Config, opts options) error {
	return cli.Run(ctx, render.New(cfg), cli.Options{
		Width:  opts.width,
		Height: opts.height,
		Kind:   opts.kind,
		Output: opts.output,
		Format: opts.format,
	})
}

// runServer serves images over HTTP until ctx is canceled. The address is
// bound before serving, so a busy port fails the run at once.
func runServer(ctx context.Context, cfg *config.Config, opts options) error {
	if ignored := ignoredFlags(opts.set); len(ignored) > 0 {
		slog.Warn("image flags are ignored in server mode", "flags", ignored)
	}

	r := render.New(cfg)
	p, err := pages.New(Version, r.Kinds())
	if err != nil {
		return fmt.Errorf("render pages: %w", err)
	}
	// The requests get a base context of their own, not derived from ctx: the
	// stop signal must not cancel the requests that Shutdown lets finish.
	srv := server.New(cfg, r, p, versionInfo()) //nolint:contextcheck // see above

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", srv.Addr())
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	slog.Info("server started", "addr", ln.Addr().String(), "version", Version, "kinds", r.Kinds())
	err = serve(ctx, srv, ln, cfg.Server.ShutdownTimeout.Timed())
	slog.Info("server stopped")
	return err
}

// serve runs srv on ln until ctx is done or serving fails, then shuts srv
// down: the active requests may finish within timeout, after that they are
// canceled. A failed shutdown is logged, only a serving error is returned.
func serve(ctx context.Context, srv *server.Server, ln net.Listener, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()

	var serveErr error
	select {
	case serveErr = <-done:
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	// ctx is usually done here, so the shutdown deadline needs a fresh context.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", "error", err)
	}
	if serveErr == nil {
		serveErr = <-done
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve: %w", serveErr)
}

// ignoredFlags returns the image flags given explicitly, which the server
// ignores, in alphabetical order.
func ignoredFlags(set map[string]bool) []string {
	var names []string
	for _, name := range []string{"f", "h", "o", "t", "w"} {
		if set[name] {
			names = append(names, name)
		}
	}
	return names
}

// setupLogger returns a slog text logger writing to w. Debug mode lowers the
// level and adds source locations; timestamps use RFC3339Nano and source paths
// are shortened to the base file name.
func setupLogger(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: debug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.SourceKey:
				if src, ok := a.Value.Any().(*slog.Source); ok {
					src.File = filepath.Base(src.File)
					return slog.Any(slog.SourceKey, src)
				}
			case slog.TimeKey:
				return slog.String(slog.TimeKey, a.Value.Time().Format(time.RFC3339Nano))
			}
			return a
		},
	}
	return slog.New(slog.NewTextHandler(w, opts))
}
