package config

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestDefaultIsValid(t *testing.T) {
	t.Parallel()

	if err := Default().validate(); err != nil {
		t.Fatalf("Default() is invalid: %v", err)
	}
}

func TestDefaultReturnsNewSlices(t *testing.T) {
	t.Parallel()

	a, b := Default(), Default()
	a.Types.Mandelbrot.Palette[0] = Color{1, 2, 3}
	a.Types.Mandelbrot.Center[0] = 1
	a.Types.Julia.C[0] = 1
	a.Types.Julia.Palette[0] = Color{1, 2, 3}
	a.Types.Newton.Colors[0] = Color{1, 2, 3}
	if !reflect.DeepEqual(b, Default()) {
		t.Fatal("changing one Default() result changed another one")
	}
}

func TestLoadDocsConfig(t *testing.T) {
	t.Parallel()

	cfg, err := Load("../../docs/config.toml")
	if err != nil {
		t.Fatalf("Load(docs/config.toml) error: %v", err)
	}
	if want := Default(); !reflect.DeepEqual(cfg, want) {
		t.Fatalf("docs/config.toml differs from Default():\n got %+v\nwant %+v", cfg, want)
	}
}

func TestLoadPartial(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    func(cfg *Config)
	}{
		{
			name:    "empty file keeps defaults",
			content: "",
			want:    func(*Config) {},
		},
		{
			name:    "scalar in a section",
			content: "[server]\ndebug = true\n",
			want:    func(cfg *Config) { cfg.Server.Debug = true },
		},
		{
			name:    "array replaces the default slice",
			content: "[types.mandelbrot]\npalette = [\"#000000\", \"#FFFFFF\"]\n",
			want: func(cfg *Config) {
				cfg.Types.Mandelbrot.Palette = []Color{{0, 0, 0}, {0xff, 0xff, 0xff}}
			},
		},
		{
			name:    "integer into a float field",
			content: "[types.newton]\nwidth = 5\n",
			want:    func(cfg *Config) { cfg.Types.Newton.Width = 5 },
		},
		{
			name:    "zero durations",
			content: "[server]\ncache_max_age = \"0s\"\n[limits]\nqueue_timeout = \"0s\"\nmax_queued = 0\n",
			want: func(cfg *Config) {
				cfg.Server.CacheMaxAge = 0
				cfg.Limits.QueueTimeout = 0
				cfg.Limits.MaxQueued = 0
			},
		},
		{
			name:    "several sections",
			content: "[limits]\nworkers = 4\ntimeout = \"10s\"\n[types.lissajous]\npoints = 500\ndelta = 0.0\n",
			want: func(cfg *Config) {
				cfg.Limits.Workers = 4
				cfg.Limits.Timeout = Duration(10 * time.Second)
				cfg.Types.Lissajous.Points = 500
				cfg.Types.Lissajous.Delta = 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Load(writeConfig(t, tt.content))
			if err != nil {
				t.Fatalf("Load error: %v", err)
			}
			want := Default()
			tt.want(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Load:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load(missing) error = %v, want fs.ErrNotExist", err)
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		errIs    error
		contains string
	}{
		{name: "unknown top-level key", content: "port = 80\n", errIs: ErrUnknownField, contains: "port (line 1)"},
		{name: "unknown section key", content: "[server]\nport = 80\n", errIs: ErrUnknownField, contains: "server.port (line 2)"},
		{name: "unknown section", content: "[types.spiral]\ndepth = 1\n", errIs: ErrUnknownField, contains: "types.spiral"},
		{name: "syntax error", content: "[server\n", errIs: ErrDecode, contains: "line 1"},
		{name: "wrong type", content: "[limits]\nmax_concurrent = \"2\"\n", errIs: ErrDecode, contains: "line 2"},
		{name: "float into int", content: "[limits]\nmax_depth = 1.5\n", errIs: ErrDecode, contains: "line 2"},
		{name: "invalid color", content: "[types.koch]\ncolor = \"#fff\"\n", errIs: ErrDecode, contains: ErrInvalidColor.Error()},
		{name: "negative duration", content: "[limits]\ntimeout = \"-1s\"\n", errIs: ErrDecode, contains: ErrNegativeDuration.Error()},
		{name: "invalid duration", content: "[limits]\ntimeout = \"10\"\n", errIs: ErrDecode, contains: "parse duration"},
		{name: "out of range", content: "[limits]\nmax_concurrent = 0\n", errIs: ErrOutOfRange, contains: "limits.max_concurrent"},
		{name: "nan", content: "[types.mandelbrot]\nwidth = nan\n", errIs: ErrNotFinite, contains: "types.mandelbrot.width"},
		{name: "inf", content: "[types.julia]\nheight = -inf\n", errIs: ErrNotFinite, contains: "types.julia.height"},
		{name: "short point", content: "[types.mandelbrot]\ncenter = [1.0]\n", errIs: ErrOutOfRange, contains: "center has 1 values"},
		{name: "long point", content: "[types.julia]\nc = [1.0, 2.0, 3.0]\n", errIs: ErrOutOfRange, contains: "c has 3 values"},
		{name: "cross check", content: "[server]\nwrite_timeout = \"17s\"\n", errIs: ErrWriteTimeout, contains: "server.write_timeout"},
		// go-toml decodes non-string values into Duration and Color bypassing UnmarshalText.
		{name: "integer duration", content: "[limits]\ntimeout = 30\n", errIs: ErrDecode, contains: "limits.timeout: got integer"},
		{name: "float duration", content: "[server]\ncache_max_age = 1.5\n", errIs: ErrDecode, contains: "server.cache_max_age: got float"},
		{name: "boolean duration", content: "[server]\nidle_timeout = true\n", errIs: ErrDecode, contains: "server.idle_timeout: got boolean"},
		{name: "array duration", content: "[limits]\nqueue_timeout = [1]\n", errIs: ErrDecode, contains: "limits.queue_timeout: got array"},
		{name: "date duration", content: "[server]\nread_timeout = 1979-05-27\n", errIs: ErrDecode, contains: "server.read_timeout: got date or time"},
		{name: "integer color", content: "[types.koch]\ncolor = 255\n", errIs: ErrDecode, contains: "types.koch.color: got integer"},
		{name: "inline table color", content: "[types.koch]\ncolor = {R = 255}\n", errIs: ErrDecode, contains: "types.koch.color: got table"},
		{name: "empty inline table color", content: "[types.koch]\ncolor = {}\n", errIs: ErrDecode, contains: "types.koch.color: got table"},
		{name: "table header color", content: "[types.koch.color]\nR = 255\n", errIs: ErrDecode, contains: "types.koch.color: got table"},
		{name: "table in palette", content: "[types.mandelbrot]\npalette = [{R = 255}, {}]\n", errIs: ErrDecode, contains: "types.mandelbrot.palette[0]: got table"},
		{name: "integer in colors", content: "[types.newton]\ncolors = [\"#000000\", 16711680]\n", errIs: ErrDecode, contains: "types.newton.colors[1]: got integer"},
		{name: "keys in other case", content: "[Types.Koch]\nColor = {R = 1}\n", errIs: ErrDecode, contains: "Types.Koch.Color: got table"},
		// U+0130 lowers to "i" as go-toml matches keys, but strings.EqualFold does not fold it.
		{name: "dotted capital I in key", content: "[limits]\n\"t\u0130meout\" = 30\n", errIs: ErrDecode, contains: "limits.t\u0130meout: got integer"},
		{name: "dotted capital I in table", content: "[\"l\u0130mits\"]\ntimeout = 30\n", errIs: ErrDecode, contains: "l\u0130mits.timeout: got integer"},
		{
			name:     "dotted capital I in nested table",
			content:  "[types.\"l\u0130ssajous\"]\ncolor = {R = 255}\n",
			errIs:    ErrDecode,
			contains: "types.l\u0130ssajous.color: got table",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Load(writeConfig(t, tt.content))
			if !errors.Is(err, tt.errIs) {
				t.Fatalf("Load error = %v, want %v", err, tt.errIs)
			}
			if cfg != nil {
				t.Errorf("Load returned a config with error: %+v", cfg)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("Load error %q does not contain %q", err, tt.contains)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(cfg *Config)
		errIs  error // nil means valid
		field  string
	}{
		// server
		{"empty addr", func(c *Config) { c.Server.Addr = "" }, ErrRequired, "server.addr"},
		{"zero read_timeout", func(c *Config) { c.Server.ReadTimeout = 0 }, ErrOutOfRange, "server.read_timeout"},
		{"long read_timeout", func(c *Config) { c.Server.ReadTimeout = maxTimeout + 1 }, ErrOutOfRange, "server.read_timeout"},
		{"zero read_header_timeout", func(c *Config) { c.Server.ReadHeaderTimeout = 0 }, ErrOutOfRange, "server.read_header_timeout"},
		{"zero write_timeout", func(c *Config) { c.Server.WriteTimeout = 0 }, ErrOutOfRange, "server.write_timeout"},
		{"zero idle_timeout", func(c *Config) { c.Server.IdleTimeout = 0 }, ErrOutOfRange, "server.idle_timeout"},
		{"zero shutdown_timeout", func(c *Config) { c.Server.ShutdownTimeout = 0 }, ErrOutOfRange, "server.shutdown_timeout"},
		{"negative cache_max_age", func(c *Config) { c.Server.CacheMaxAge = -1 }, ErrOutOfRange, "server.cache_max_age"},
		{"long cache_max_age", func(c *Config) { c.Server.CacheMaxAge = maxCacheMaxAge + 1 }, ErrOutOfRange, "server.cache_max_age"},
		{"zero cache_max_age", func(c *Config) { c.Server.CacheMaxAge = 0 }, nil, ""},
		// limits
		{"small max_pixels", func(c *Config) { c.Limits.MaxPixels = 1023 }, ErrOutOfRange, "limits.max_pixels"},
		{"large max_pixels", func(c *Config) { c.Limits.MaxPixels = 1<<30 + 1 }, ErrOutOfRange, "limits.max_pixels"},
		{"min max_pixels", func(c *Config) { c.Limits.MaxPixels = 1024 }, nil, ""},
		{"max max_pixels", func(c *Config) { c.Limits.MaxPixels = 1 << 30 }, nil, ""},
		{"zero max_concurrent", func(c *Config) { c.Limits.MaxConcurrent = 0 }, ErrOutOfRange, "limits.max_concurrent"},
		{"negative max_queued", func(c *Config) { c.Limits.MaxQueued = -1 }, ErrOutOfRange, "limits.max_queued"},
		{"zero max_queued", func(c *Config) { c.Limits.MaxQueued = 0 }, nil, ""},
		{"negative queue_timeout", func(c *Config) { c.Limits.QueueTimeout = -1 }, ErrOutOfRange, "limits.queue_timeout"},
		{"long queue_timeout", func(c *Config) { c.Limits.QueueTimeout = maxTimeout + 1 }, ErrOutOfRange, "limits.queue_timeout"},
		{"zero queue_timeout", func(c *Config) { c.Limits.QueueTimeout = 0 }, nil, ""},
		{"zero timeout", func(c *Config) { c.Limits.Timeout = 0 }, ErrOutOfRange, "limits.timeout"},
		{"negative workers", func(c *Config) { c.Limits.Workers = -1 }, ErrOutOfRange, "limits.workers"},
		{"zero max_iterations", func(c *Config) { c.Limits.MaxIterations = 0 }, ErrOutOfRange, "limits.max_iterations"},
		{"large max_iterations", func(c *Config) { c.Limits.MaxIterations = 1<<20 + 1 }, ErrOutOfRange, "limits.max_iterations"},
		{"max max_iterations", func(c *Config) { c.Limits.MaxIterations = 1 << 20 }, nil, ""},
		{"negative max_depth", func(c *Config) { c.Limits.MaxDepth = -1 }, ErrOutOfRange, "limits.max_depth"},
		// cross checks
		{"header timeout above read timeout", func(c *Config) {
			c.Server.ReadTimeout = Duration(5 * time.Second)
			c.Server.ReadHeaderTimeout = Duration(6 * time.Second)
		}, ErrHeaderTimeout, "server.read_header_timeout"},
		{"header timeout equals read timeout", func(c *Config) {
			c.Server.ReadHeaderTimeout = c.Server.ReadTimeout
		}, nil, ""},
		{"write timeout equals queue + timeout", func(c *Config) {
			c.Server.WriteTimeout = c.Limits.QueueTimeout + c.Limits.Timeout
		}, ErrWriteTimeout, "server.write_timeout"},
		{"write timeout below queue timeout", func(c *Config) {
			c.Server.WriteTimeout = Duration(time.Second)
		}, ErrWriteTimeout, "limits.queue_timeout"},
		{"write timeout just above queue + timeout", func(c *Config) {
			c.Server.WriteTimeout = c.Limits.QueueTimeout + c.Limits.Timeout + 1
		}, nil, ""},
		// mandelbrot
		{"mandelbrot zero iterations", func(c *Config) { c.Types.Mandelbrot.Iterations = 0 }, ErrOutOfRange, "types.mandelbrot.iterations"},
		{"mandelbrot iterations above limit", func(c *Config) { c.Types.Mandelbrot.Iterations = 4097 }, ErrOutOfRange, "types.mandelbrot.iterations"},
		{"mandelbrot iterations at limit", func(c *Config) { c.Types.Mandelbrot.Iterations = 4096 }, nil, ""},
		{"mandelbrot short center", func(c *Config) { c.Types.Mandelbrot.Center = []float64{1} }, ErrOutOfRange, "types.mandelbrot.center"},
		{"mandelbrot nan center", func(c *Config) { c.Types.Mandelbrot.Center[1] = math.NaN() }, ErrNotFinite, "types.mandelbrot.center[1]"},
		{"mandelbrot zero width", func(c *Config) { c.Types.Mandelbrot.Width = 0 }, ErrOutOfRange, "types.mandelbrot.width"},
		{"mandelbrot negative height", func(c *Config) { c.Types.Mandelbrot.Height = -1 }, ErrOutOfRange, "types.mandelbrot.height"},
		{"mandelbrot inf width", func(c *Config) { c.Types.Mandelbrot.Width = math.Inf(1) }, ErrNotFinite, "types.mandelbrot.width"},
		{"mandelbrot short palette", func(c *Config) { c.Types.Mandelbrot.Palette = []Color{{}} }, ErrTooFewColors, "types.mandelbrot.palette"},
		// julia
		{"julia zero iterations", func(c *Config) { c.Types.Julia.Iterations = 0 }, ErrOutOfRange, "types.julia.iterations"},
		{"julia empty c", func(c *Config) { c.Types.Julia.C = nil }, ErrOutOfRange, "types.julia.c"},
		{"julia inf c", func(c *Config) { c.Types.Julia.C[0] = math.Inf(-1) }, ErrNotFinite, "types.julia.c[0]"},
		{"julia zero width", func(c *Config) { c.Types.Julia.Width = 0 }, ErrOutOfRange, "types.julia.width"},
		{"julia zero height", func(c *Config) { c.Types.Julia.Height = 0 }, ErrOutOfRange, "types.julia.height"},
		{"julia short palette", func(c *Config) { c.Types.Julia.Palette = nil }, ErrTooFewColors, "types.julia.palette"},
		// newton
		{"newton iterations above limit", func(c *Config) { c.Types.Newton.Iterations = 4097 }, ErrOutOfRange, "types.newton.iterations"},
		{"newton small degree", func(c *Config) { c.Types.Newton.Degree = 2 }, ErrOutOfRange, "types.newton.degree"},
		{"newton large degree", func(c *Config) { c.Types.Newton.Degree = 9 }, ErrOutOfRange, "types.newton.degree"},
		{"newton max degree", func(c *Config) { c.Types.Newton.Degree = 8 }, nil, ""},
		{"newton zero width", func(c *Config) { c.Types.Newton.Width = 0 }, ErrOutOfRange, "types.newton.width"},
		{"newton zero height", func(c *Config) { c.Types.Newton.Height = 0 }, ErrOutOfRange, "types.newton.height"},
		{"newton zero epsilon", func(c *Config) { c.Types.Newton.Epsilon = 0 }, ErrOutOfRange, "types.newton.epsilon"},
		{"newton nan epsilon", func(c *Config) { c.Types.Newton.Epsilon = math.NaN() }, ErrNotFinite, "types.newton.epsilon"},
		{"newton inf epsilon", func(c *Config) { c.Types.Newton.Epsilon = math.Inf(1) }, ErrNotFinite, "types.newton.epsilon"},
		{"newton tiny epsilon", func(c *Config) { c.Types.Newton.Epsilon = 1e-200 }, ErrOutOfRange, "types.newton.epsilon"},
		{"newton epsilon below min", func(c *Config) { c.Types.Newton.Epsilon = 9e-13 }, ErrOutOfRange, "types.newton.epsilon"},
		{"newton min epsilon", func(c *Config) { c.Types.Newton.Epsilon = minNewtonEpsilon }, nil, ""},
		{"newton fewer colors than degree", func(c *Config) {
			c.Types.Newton.Degree = 5
			c.Types.Newton.Colors = c.Types.Newton.Colors[:4]
		}, ErrTooFewColors, "types.newton.colors"},
		{"newton colors equal degree", func(c *Config) { c.Types.Newton.Colors = c.Types.Newton.Colors[:3] }, nil, ""},
		// sierpinski
		{"sierpinski negative depth", func(c *Config) { c.Types.Sierpinski.Depth = -1 }, ErrOutOfRange, "types.sierpinski.depth"},
		{"sierpinski depth above limit", func(c *Config) { c.Types.Sierpinski.Depth = 9 }, ErrOutOfRange, "types.sierpinski.depth"},
		{"sierpinski zero depth", func(c *Config) { c.Types.Sierpinski.Depth = 0 }, nil, ""},
		{"sierpinski zero padding", func(c *Config) { c.Types.Sierpinski.Padding = 0 }, ErrOutOfRange, "types.sierpinski.padding"},
		{"sierpinski large padding", func(c *Config) { c.Types.Sierpinski.Padding = 0.26 }, ErrOutOfRange, "types.sierpinski.padding"},
		{"sierpinski max padding", func(c *Config) { c.Types.Sierpinski.Padding = 0.25 }, nil, ""},
		{"sierpinski nan padding", func(c *Config) { c.Types.Sierpinski.Padding = math.NaN() }, ErrNotFinite, "types.sierpinski.padding"},
		// koch
		{"koch depth above limit", func(c *Config) { c.Types.Koch.Depth = 9 }, ErrOutOfRange, "types.koch.depth"},
		{"koch zero line_width", func(c *Config) { c.Types.Koch.LineWidth = 0 }, ErrOutOfRange, "types.koch.line_width"},
		{"koch large line_width", func(c *Config) { c.Types.Koch.LineWidth = 0.3 }, ErrOutOfRange, "types.koch.line_width"},
		{"koch zero padding", func(c *Config) { c.Types.Koch.Padding = 0 }, ErrOutOfRange, "types.koch.padding"},
		// lissajous
		{"lissajous zero a", func(c *Config) { c.Types.Lissajous.A = 0 }, ErrOutOfRange, "types.lissajous.a"},
		{"lissajous large a", func(c *Config) { c.Types.Lissajous.A = 33 }, ErrOutOfRange, "types.lissajous.a"},
		{"lissajous zero b", func(c *Config) { c.Types.Lissajous.B = 0 }, ErrOutOfRange, "types.lissajous.b"},
		{"lissajous large b", func(c *Config) { c.Types.Lissajous.B = 33 }, ErrOutOfRange, "types.lissajous.b"},
		{"lissajous max frequencies", func(c *Config) { c.Types.Lissajous.A, c.Types.Lissajous.B = 32, 32 }, nil, ""},
		{"lissajous nan delta", func(c *Config) { c.Types.Lissajous.Delta = math.NaN() }, ErrNotFinite, "types.lissajous.delta"},
		{"lissajous negative delta", func(c *Config) { c.Types.Lissajous.Delta = -math.Pi }, nil, ""},
		{"lissajous few points", func(c *Config) { c.Types.Lissajous.Points = 15 }, ErrOutOfRange, "types.lissajous.points"},
		{"lissajous many points", func(c *Config) { c.Types.Lissajous.Points = 65537 }, ErrOutOfRange, "types.lissajous.points"},
		{"lissajous negative points", func(c *Config) { c.Types.Lissajous.Points = -1 }, ErrOutOfRange, "types.lissajous.points"},
		{"lissajous min points", func(c *Config) { c.Types.Lissajous.Points = 16 }, nil, ""},
		{"lissajous max points", func(c *Config) { c.Types.Lissajous.Points = 65536 }, nil, ""},
		{"lissajous zero line_width", func(c *Config) { c.Types.Lissajous.LineWidth = 0 }, ErrOutOfRange, "types.lissajous.line_width"},
		{"lissajous large padding", func(c *Config) { c.Types.Lissajous.Padding = 0.3 }, ErrOutOfRange, "types.lissajous.padding"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Default()
			tt.mutate(cfg)
			err := cfg.validate()
			if tt.errIs == nil {
				if err != nil {
					t.Fatalf("validate() unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.errIs) {
				t.Fatalf("validate() error = %v, want %v", err, tt.errIs)
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Errorf("validate() error %q does not name %q", err, tt.field)
			}
		})
	}
}

func TestLoadKeysIgnoreCase(t *testing.T) {
	t.Parallel()

	got, err := Load(writeConfig(t, "[Types.Koch]\nColor = \"#010203\"\n[LIMITS]\n\"T\u0130meout\" = \"10s\"\n"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	want := Default()
	want.Types.Koch.Color = Color{1, 2, 3}
	want.Limits.Timeout = Duration(10 * time.Second)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load:\n got %+v\nwant %+v", got, want)
	}
}

// An invalid degree is reported once: the color count is checked against the
// degree clamped to its valid range.
func TestValidateNewtonDegreeOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		degree int
		colors int
	}{
		{name: "small degree", degree: 2, colors: 3},
		{name: "large degree", degree: 9, colors: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Default()
			cfg.Types.Newton.Degree = tt.degree
			cfg.Types.Newton.Colors = cfg.Types.Newton.Colors[:tt.colors]
			err := cfg.validate()
			if !errors.Is(err, ErrOutOfRange) {
				t.Errorf("validate() error = %v, want %v", err, ErrOutOfRange)
			}
			if errors.Is(err, ErrTooFewColors) {
				t.Errorf("validate() error = %v, want no %v", err, ErrTooFewColors)
			}
		})
	}
}

func TestValidateAccumulatesErrors(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.Server.Addr = ""
	cfg.Limits.MaxConcurrent = 0
	cfg.Types.Koch.LineWidth = math.NaN()
	cfg.Server.ReadHeaderTimeout = cfg.Server.ReadTimeout + 1

	err := cfg.validate()
	for _, want := range []error{ErrRequired, ErrOutOfRange, ErrNotFinite, ErrHeaderTimeout} {
		if !errors.Is(err, want) {
			t.Errorf("validate() error = %v, want it to include %v", err, want)
		}
	}
}
