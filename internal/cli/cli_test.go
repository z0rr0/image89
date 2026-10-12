package cli

import (
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/z0rr0/image89/internal/config"
	"github.com/z0rr0/image89/internal/encode"
	"github.com/z0rr0/image89/internal/figure"
	"github.com/z0rr0/image89/internal/render"
)

var errBoom = errors.New("boom")

// fakeRenderer accepts every request and renders with fn.
type fakeRenderer struct {
	fn func(ctx context.Context, sink func([]byte) error) error
}

func (fakeRenderer) Validate(render.Request) error { return nil }

func (f fakeRenderer) Render(ctx context.Context, _ render.Request, sink func([]byte) error) error {
	return f.fn(ctx, sink)
}

// writeData is a render function that hands over a few bytes.
func writeData(_ context.Context, sink func([]byte) error) error {
	return sink([]byte("image data"))
}

// failingFile is a real file whose Write or Close fails on demand.
type failingFile struct {
	*os.File

	writeErr error
	closeErr error
}

func (f failingFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.File.Write(p)
}

func (f failingFile) Close() error {
	err := f.File.Close()
	if f.closeErr != nil {
		return f.closeErr
	}
	return err
}

// failingOpener creates the file like createFile and wraps it in failingFile.
func failingOpener(writeErr, closeErr error) opener {
	return func(name string) (io.WriteCloser, error) {
		w, err := createFile(name)
		if err != nil {
			return nil, err
		}
		f, ok := w.(*os.File)
		if !ok {
			return nil, errors.New("createFile did not return *os.File")
		}
		return failingFile{File: f, writeErr: writeErr, closeErr: closeErr}, nil
	}
}

// options returns valid 64×48 options writing to output.
func options(output, format string) Options {
	return Options{Width: 64, Height: 48, Kind: string(figure.Julia), Output: output, Format: format}
}

func assertNoFile(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Stat(name); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file %s exists or cannot be checked: %v", name, err)
	}
}

func TestOutputName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format encode.Format
		want   string
	}{
		{name: "", format: encode.PNG, want: "output.png"},
		{name: "", format: encode.JPEG, want: "output.jpg"},
		{name: "pic", format: encode.JPEG, want: "pic.jpg"},
		{name: "pic.png", format: encode.JPEG, want: "pic.png"},
		{name: "pic.jpeg", format: encode.JPEG, want: "pic.jpeg"},
		{name: "dir/pic", format: encode.PNG, want: "dir/pic.png"},
		{name: "dir.d/pic", format: encode.PNG, want: "dir.d/pic.png"},
	}

	for _, tt := range tests {
		if got := OutputName(tt.name, tt.format); got != tt.want {
			t.Errorf("OutputName(%q, %s) = %q, want %q", tt.name, tt.format, got, tt.want)
		}
	}
}

func TestRunCreatesImage(t *testing.T) {
	t.Parallel()

	r := render.New(config.Default())
	tests := []struct {
		format string
		file   string
		decode func(io.Reader) (image.Image, error)
	}{
		{format: "png", file: "pic.png", decode: png.Decode},
		{format: "jpeg", file: "pic.jpg", decode: jpeg.Decode},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if err := Run(t.Context(), r, options(filepath.Join(dir, "pic"), tt.format)); err != nil {
				t.Fatalf("Run error: %v", err)
			}
			f, err := os.Open(filepath.Clean(filepath.Join(dir, tt.file)))
			if err != nil {
				t.Fatalf("open the output: %v", err)
			}
			defer func() { _ = f.Close() }()
			img, err := tt.decode(f)
			if err != nil {
				t.Fatalf("decode the output: %v", err)
			}
			if want := image.Rect(0, 0, 64, 48); img.Bounds() != want {
				t.Errorf("bounds = %v, want %v", img.Bounds(), want)
			}
		})
	}
}

func TestRunKeepsExistingFile(t *testing.T) {
	t.Parallel()

	name := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(name, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write the existing file: %v", err)
	}

	err := Run(t.Context(), fakeRenderer{fn: writeData}, options(name, "png"))
	if !errors.Is(err, ErrOutputExists) {
		t.Errorf("Run error = %v, want %v", err, ErrOutputExists)
	}
	data, err := os.ReadFile(filepath.Clean(name))
	if err != nil || string(data) != "keep" {
		t.Errorf("existing file = %q, %v, want %q", data, err, "keep")
	}
}

func TestRunRejectsBeforeCreating(t *testing.T) {
	t.Parallel()

	r := render.New(config.Default())
	tests := []struct {
		name string
		opts func(Options) Options
		want error
	}{
		{name: "small size", opts: func(o Options) Options { o.Width = 10; return o }, want: figure.ErrInvalidSize},
		{name: "unknown kind", opts: func(o Options) Options { o.Kind = "nope"; return o }, want: figure.ErrUnknownKind},
		{name: "unknown format", opts: func(o Options) Options { o.Format = "gif"; return o }, want: encode.ErrUnknownFormat},
		{
			name: "output with trailing separator",
			opts: func(o Options) Options { o.Output = filepath.Dir(o.Output) + string(filepath.Separator); return o },
			want: ErrInvalidOutput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			err := Run(t.Context(), r, tt.opts(options(filepath.Join(dir, "pic"), "png")))
			if !errors.Is(err, tt.want) {
				t.Errorf("Run error = %v, want %v", err, tt.want)
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
				t.Errorf("output dir has %d entries, %v; want none", len(entries), err)
			}
		})
	}
}

func TestRunCanceledRemovesFile(t *testing.T) {
	t.Parallel()

	name := filepath.Join(t.TempDir(), "pic.png")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	existed := false
	// The renderer sees the created file, then waits for the cancellation.
	r := fakeRenderer{fn: func(ctx context.Context, _ func([]byte) error) error {
		_, err := os.Stat(name)
		existed = err == nil
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}}

	if err := Run(ctx, r, options(name, "png")); !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want %v", err, context.Canceled)
	}
	if !existed {
		t.Error("the output file did not exist during rendering")
	}
	assertNoFile(t, name)
}

func TestRunErrorsRemoveFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func(context.Context, func([]byte) error) error
		open opener
		want error
	}{
		{
			name: "render",
			fn:   func(context.Context, func([]byte) error) error { return errBoom },
			open: createFile,
			want: errBoom,
		},
		{name: "write", fn: writeData, open: failingOpener(errBoom, nil), want: errBoom},
		{name: "close", fn: writeData, open: failingOpener(nil, errBoom), want: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			name := filepath.Join(t.TempDir(), "pic.png")
			err := run(t.Context(), fakeRenderer{fn: tt.fn}, options(name, "png"), tt.open)
			if !errors.Is(err, tt.want) {
				t.Errorf("run error = %v, want %v", err, tt.want)
			}
			assertNoFile(t, name)
		})
	}
}

func TestRunRemoveError(t *testing.T) {
	t.Parallel()

	name := filepath.Join(t.TempDir(), "pic.png")
	// The file disappears before Run can remove it.
	r := fakeRenderer{fn: func(context.Context, func([]byte) error) error {
		if err := os.Remove(name); err != nil {
			return err
		}
		return errBoom
	}}

	err := Run(t.Context(), r, options(name, "png"))
	if !errors.Is(err, errBoom) || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Run error = %v, want %v and %v", err, errBoom, fs.ErrNotExist)
	}
}

func TestRunOpenError(t *testing.T) {
	t.Parallel()

	name := filepath.Join(t.TempDir(), "missing", "pic.png")
	err := Run(t.Context(), fakeRenderer{fn: writeData}, options(name, "png"))
	if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrOutputExists) {
		t.Errorf("Run error = %v, want %v only", err, fs.ErrNotExist)
	}
}

// TestRunPathThroughSymlink resolves "alias/../name" through the symlink
// alias -> other/child, like the OS does, so the file is created and removed
// at other/name and nothing else in dir is touched.
func TestRunPathThroughSymlink(t *testing.T) {
	t.Parallel()

	failing := fakeRenderer{fn: func(context.Context, func([]byte) error) error { return errBoom }}
	tests := []struct {
		name     string
		existing bool
		want     error
	}{
		{name: "existing.png", existing: true, want: ErrOutputExists},
		{name: "new.png", want: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "other", "child"), 0o750); err != nil {
				t.Fatalf("create dirs: %v", err)
			}
			if err := os.Symlink(filepath.Join("other", "child"), filepath.Join(dir, "alias")); err != nil {
				t.Skipf("symlinks are not supported: %v", err)
			}
			target := filepath.Join(dir, "other", tt.name)
			if tt.existing {
				if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
					t.Fatalf("write the existing file: %v", err)
				}
			}

			output := filepath.Join(dir, "alias") + string(filepath.Separator) + ".." + string(filepath.Separator) + tt.name
			if err := Run(t.Context(), failing, options(output, "png")); !errors.Is(err, tt.want) {
				t.Errorf("Run(%q) error = %v, want %v", output, err, tt.want)
			}

			assertNoFile(t, filepath.Join(dir, tt.name))
			if !tt.existing {
				assertNoFile(t, target)
				return
			}
			if data, err := os.ReadFile(filepath.Clean(target)); err != nil || string(data) != "keep" {
				t.Errorf("existing file = %q, %v, want %q", data, err, "keep")
			}
		})
	}
}
