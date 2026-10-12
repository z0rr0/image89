package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/encode"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/server"
)

func TestVersionInfo(t *testing.T) {
	t.Parallel()

	got := versionInfo()
	want := "Image89: " + Version + " " + Revision + " " + GoVersion + " " + BuildDate
	if got != want {
		t.Errorf("versionInfo() = %q, want %q", got, want)
	}
	if fields := strings.Fields(got); len(fields) != 5 {
		t.Errorf("versionInfo() has %d fields, want 5: %q", len(fields), got)
	}
	if !strings.HasPrefix(GoVersion, "go") {
		t.Errorf("GoVersion = %q, want go prefix", GoVersion)
	}
}

func TestParseFlags(t *testing.T) {
	t.Parallel()

	defaults := func(set ...string) options {
		opts := options{
			width:  figure.DefaultSide,
			height: figure.DefaultSide,
			kind:   string(figure.DefaultKind),
			format: string(encode.PNG),
			set:    make(map[string]bool, len(set)),
		}
		for _, name := range set {
			opts.set[name] = true
		}
		return opts
	}

	tests := []struct {
		name string
		args []string
		want func() options
	}{
		{
			name: "defaults",
			want: func() options { return defaults() },
		},
		{
			name: "height",
			args: []string{"-h", "480"},
			want: func() options {
				opts := defaults("h")
				opts.height = 480
				return opts
			},
		},
		{
			name: "server",
			args: []string{"-s"},
			want: func() options {
				opts := defaults("s")
				opts.serve = true
				return opts
			},
		},
		{
			name: "config",
			args: []string{"-c", "path/config.toml"},
			want: func() options {
				opts := defaults("c")
				opts.config = "path/config.toml"
				return opts
			},
		},
		{
			name: "height does not trigger help",
			args: []string{"-h", "480", "-v"},
			want: func() options {
				opts := defaults("h", "v")
				opts.height = 480
				opts.version = true
				return opts
			},
		},
		{
			name: "all image flags",
			args: []string{"-w", "640", "-h", "480", "-t", "julia", "-o", "pic", "-f", "jpeg"},
			want: func() options {
				opts := defaults("w", "h", "t", "o", "f")
				opts.width, opts.height = 640, 480
				opts.kind, opts.output, opts.format = "julia", "pic", "jpeg"
				return opts
			},
		},
		{
			name: "explicit default value is still set",
			args: []string{"-w", "1024"},
			want: func() options { return defaults("w") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			got, err := parseFlags(tt.args, &stderr)
			if err != nil {
				t.Fatalf("parseFlags(%q) unexpected error: %v", tt.args, err)
			}
			if want := tt.want(); !reflect.DeepEqual(got, want) {
				t.Errorf("parseFlags(%q) = %+v, want %+v", tt.args, got, want)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestParseFlagsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		errIs error // nil means any error except flag.ErrHelp
	}{
		{name: "help", args: []string{"-help"}, errIs: flag.ErrHelp},
		{name: "unknown flag", args: []string{"-x"}},
		{name: "height without value", args: []string{"-h"}},
		{name: "invalid width", args: []string{"-w", "wide"}},
		{name: "positional argument", args: []string{"extra"}, errIs: errUnexpectedArgs},
		{name: "positional argument after flags", args: []string{"-s", "extra"}, errIs: errUnexpectedArgs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			_, err := parseFlags(tt.args, &stderr)
			switch {
			case err == nil:
				t.Fatalf("parseFlags(%q) error = nil, want error", tt.args)
			case tt.errIs != nil && !errors.Is(err, tt.errIs):
				t.Errorf("parseFlags(%q) error = %v, want %v", tt.args, err, tt.errIs)
			case tt.errIs == nil && errors.Is(err, flag.ErrHelp):
				t.Errorf("parseFlags(%q) error = %v, want a non-help error", tt.args, err)
			}
			if !strings.Contains(stderr.String(), "Usage: image89") {
				t.Errorf("stderr has no usage: %q", stderr.String())
			}
		})
	}
}

func TestParseFlagsUsage(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	if _, err := parseFlags([]string{"-help"}, &stderr); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseFlags(-help) error = %v, want flag.ErrHelp", err)
	}
	usage := stderr.String()
	for _, want := range []string{"-help", "-h int", "image height", "-w int", "-t string", "-c string", "-s", "-v", "min 32, max 65536"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage does not contain %q:\n%s", want, usage)
		}
	}
	for _, k := range figure.Kinds() {
		if !strings.Contains(usage, string(k)) {
			t.Errorf("usage does not list the image type %q:\n%s", k, usage)
		}
	}
}

func TestSetupLogger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		debug      bool
		wantDebug  bool
		wantSource bool
	}{
		{name: "info", debug: false},
		{name: "debug", debug: true, wantDebug: true, wantSource: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := setupLogger(&buf, tt.debug)
			logger.Debug("debug message")
			logger.Info("info message", "key", "value")
			out := buf.String()

			if got := strings.Contains(out, "debug message"); got != tt.wantDebug {
				t.Errorf("debug message logged = %v, want %v:\n%s", got, tt.wantDebug, out)
			}
			if !strings.Contains(out, "info message") || !strings.Contains(out, "key=value") {
				t.Errorf("info message is missing:\n%s", out)
			}
			if got := strings.Contains(out, "source=image89_test.go:"); got != tt.wantSource {
				t.Errorf("short source logged = %v, want %v:\n%s", got, tt.wantSource, out)
			}
			if strings.Contains(out, "/image89_test.go") {
				t.Errorf("source path is not shortened:\n%s", out)
			}
		})
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// listen returns a listener on a free local port, closed by the test cleanup.
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// TestRun is sequential: run replaces the global slog logger.
func TestRun(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")
	invalid := writeFile(t, "invalid.toml", "[limits]\nmax_concurrent = 0\n")
	debug := writeFile(t, "debug.toml", "[server]\ndebug = true\n")
	existing := writeFile(t, "existing.png", "keep")
	// The server cases run with a canceled ctx: the server starts and stops at once.
	serverConfig := writeFile(t, "server.toml", "[server]\naddr = \"127.0.0.1:0\"\n")
	busy := writeFile(t, "busy.toml", fmt.Sprintf("[server]\naddr = %q\n", listen(t).Addr().String()))
	// Images are small and go to the temp dir, never to output.png in the repo.
	image := func(name string, args ...string) []string {
		return append([]string{"-w", "64", "-h", "48", "-o", filepath.Join(dir, name)}, args...)
	}

	tests := []struct {
		name       string
		args       []string
		canceled   bool
		code       int
		wantStdout string
		wantStderr string
		wantFile   string
	}{
		{name: "version", args: []string{"-v"}, code: exitOK, wantStdout: versionInfo() + "\n"},
		{name: "version with height", args: []string{"-h", "480", "-v"}, code: exitOK, wantStdout: versionInfo() + "\n"},
		{name: "help", args: []string{"-help"}, code: exitOK, wantStderr: "-h int"},
		{name: "positional argument", args: []string{"extra"}, code: exitUsage, wantStderr: "unexpected arguments"},
		{name: "unknown flag", args: []string{"-x"}, code: exitUsage, wantStderr: "flag provided but not defined"},
		{name: "cli", args: image("pic"), code: exitOK, wantStderr: "image created", wantFile: "pic.png"},
		{name: "cli exists", args: []string{"-w", "64", "-h", "48", "-o", existing}, code: exitError, wantStderr: "already exists"},
		{name: "cli invalid size", args: image("small", "-w", "10"), code: exitError, wantStderr: "invalid image size"},
		{
			name: "cli output dir", args: []string{"-w", "64", "-h", "48", "-o", dir + string(filepath.Separator)},
			code: exitError, wantStderr: "output name has no file name",
		},
		{name: "server", args: []string{"-s", "-c", serverConfig}, canceled: true, code: exitOK, wantStderr: "server stopped"},
		{
			name: "server ignores image flags", args: []string{"-s", "-c", serverConfig, "-w", "64", "-o", "pic"}, canceled: true,
			code: exitOK, wantStderr: `msg="image flags are ignored in server mode" flags="[o w]"`,
		},
		{name: "server busy port", args: []string{"-s", "-c", busy}, code: exitError, wantStderr: "address already in use"},
		{name: "version ignores config", args: []string{"-v", "-c", missing}, code: exitOK, wantStdout: versionInfo() + "\n"},
		{
			name: "docs config", args: image("docs", "-c", "docs/config.toml", "-t", "julia", "-f", "jpeg"),
			code: exitOK, wantStderr: "image created", wantFile: "docs.jpg",
		},
		{name: "missing config", args: []string{"-c", missing}, code: exitError, wantStderr: "failed to load config"},
		{name: "invalid config", args: []string{"-s", "-c", invalid}, code: exitError, wantStderr: "limits.max_concurrent"},
		// The run fails in image89.go, so its log record has the short source.
		{name: "debug config", args: image("debug", "-c", debug, "-t", "nope"), code: exitError, wantStderr: "source=image89.go:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.canceled {
				cancel()
			}
			var stdout, stderr bytes.Buffer
			if code := run(ctx, tt.args, &stdout, &stderr); code != tt.code {
				t.Errorf("run(%q) = %d, want %d; stderr:\n%s", tt.args, code, tt.code, stderr.String())
			}
			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", got, tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr does not contain %q:\n%s", tt.wantStderr, stderr.String())
			}
			if tt.wantFile != "" {
				if _, err := os.Stat(filepath.Join(dir, tt.wantFile)); err != nil {
					t.Errorf("output file: %v", err)
				}
			}
		})
	}
}

func TestServeFails(t *testing.T) {
	ln := listen(t)
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	srv := server.New(config.Default(), nil, nil, versionInfo())

	err := serve(t.Context(), srv, ln, time.Second)
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		t.Errorf("serve on a closed listener error = %v, want a serving error", err)
	}
}
