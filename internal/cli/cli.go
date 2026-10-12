// Package cli implements the CLI mode: it renders one image into a new file
// and never overwrites an existing one.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/z0rr0/image89/internal/encode"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/render"
)

// defaultName is the output file name used without -o, before the extension.
const defaultName = "output"

// Sentinel errors of Run.
var (
	// ErrOutputExists means the output file already exists; it is left untouched.
	ErrOutputExists = errors.New("output file already exists")
	// ErrInvalidOutput means the output name has no file name: it ends with a
	// path separator, or it is a bare volume such as "C:" on Windows.
	ErrInvalidOutput = errors.New("output name has no file name")
)

// Options are the image options of the command line.
type Options struct {
	Width  int
	Height int
	Kind   string
	Output string
	Format string
}

// Renderer validates and renders images; *render.Renderer implements it.
type Renderer interface {
	Validate(req render.Request) error
	Render(ctx context.Context, req render.Request, sink func([]byte) error) error
}

// opener creates a new file for writing and fails with fs.ErrExist if the
// file already exists.
type opener func(name string) (io.WriteCloser, error)

// OutputName returns the output file name: an empty name gives "output" with
// the extension of f, a name without an extension gets the extension of f,
// and a name with any extension is kept as is.
func OutputName(name string, f encode.Format) string {
	if name == "" {
		name = defaultName
	}
	if filepath.Ext(name) == "" {
		name += f.Ext()
	}
	return name
}

// Run renders the image described by opts into a new file named by
// OutputName. The request and the output name are validated before the file
// is created; a name without a file name, such as "dir/", gives ErrInvalidOutput. An
// existing file gives ErrOutputExists and is not changed; if rendering,
// writing or closing fails, including a done ctx, the new file is removed.
func Run(ctx context.Context, r Renderer, opts Options) error {
	return run(ctx, r, opts, createFile)
}

func run(ctx context.Context, r Renderer, opts Options, open opener) error {
	req, err := request(opts)
	if err != nil {
		return err
	}
	if err = r.Validate(req); err != nil {
		return err
	}
	// "dir/" (or "C:" on Windows) would otherwise become the hidden file
	// "dir/.png"; an empty name means the default one.
	if out := opts.Output; out != "" {
		if _, file := filepath.Split(out); file == "" {
			return errors.Join(ErrInvalidOutput, fmt.Errorf("%q", out))
		}
	}

	// The same name is opened, removed and logged, so open and remove resolve
	// it by the same OS rules; see createFile. Removal goes by path, so another
	// process changing the directories or symlinks in between is not guarded.
	name := OutputName(opts.Output, req.Format)
	f, err := open(name)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return errors.Join(ErrOutputExists, err)
		}
		return fmt.Errorf("create output: %w", err)
	}

	start := time.Now()
	written := 0
	err = r.Render(ctx, req, func(data []byte) error {
		n, writeErr := f.Write(data)
		written = n
		return writeErr
	})
	if closeErr := f.Close(); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("close output: %w", closeErr))
	}
	if err != nil {
		if removeErr := os.Remove(name); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove output: %w", removeErr))
		}
		return err
	}

	slog.Info("image created",
		"path", name,
		"kind", req.Kind,
		"size", req.Size.String(),
		"format", req.Format,
		"bytes", written,
		"duration", time.Since(start),
	)
	return nil
}

// request parses the options into a render request.
func request(opts Options) (render.Request, error) {
	kind, err := figure.ParseKind(opts.Kind)
	if err != nil {
		return render.Request{}, err
	}
	format, err := encode.Parse(opts.Format)
	if err != nil {
		return render.Request{}, err
	}
	return render.Request{Kind: kind, Size: figure.Size{Width: opts.Width, Height: opts.Height}, Format: format}, nil
}

// createFile creates a new file and fails with fs.ErrExist if it exists.
//
// G302: an image is an ordinary user file, readable by others like the output
// of any CLI tool. G304: the path is the user's own -o flag. It is used as is,
// without filepath.Clean: Clean is lexical and ignores symlinks, so "alias/../x"
// would be created at one place and removed by run at another, which the OS
// resolves through the symlink.
func createFile(name string) (io.WriteCloser, error) {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // G302 and G304, see above
	if err != nil {
		return nil, err
	}
	return f, nil
}
