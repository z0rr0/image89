// Package config is the single source of Image89 settings and figure presets.
// Default returns a complete valid configuration; Load decodes a TOML file on
// top of it and validates the result, so a file only lists what it changes.
package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Validation bounds.
const (
	maxTimeout     = Duration(24 * time.Hour)
	maxCacheMaxAge = Duration(365 * 24 * time.Hour)
	minMaxPixels   = 32 * 32
	maxMaxPixels   = 1 << 30 // figure.MaxSide²
)

// Sentinel errors returned by Load. A decode error from go-toml carries the
// text of ErrInvalidColor or ErrNegativeDuration, but not the error itself:
// go-toml flattens errors returned by UnmarshalText.
var (
	// ErrDecode means the file is not valid TOML or a value has a wrong type or format.
	ErrDecode = errors.New("invalid config file")
	// ErrUnknownField means the file has a key that Config does not define.
	ErrUnknownField = errors.New("unknown config field")
	// ErrRequired means a required value is empty.
	ErrRequired = errors.New("required value is empty")
	// ErrOutOfRange means a number, a duration or a list length is outside its allowed range.
	ErrOutOfRange = errors.New("value out of range")
	// ErrNotFinite means a float is NaN or infinite.
	ErrNotFinite = errors.New("value is not finite")
	// ErrTooFewColors means a palette or a color list is shorter than required.
	ErrTooFewColors = errors.New("too few colors")
	// ErrInvalidColor means a color is not written as "#rrggbb".
	ErrInvalidColor = errors.New("invalid color")
	// ErrNegativeDuration means a duration is negative.
	ErrNegativeDuration = errors.New("negative duration")
	// ErrHeaderTimeout means server.read_header_timeout exceeds server.read_timeout.
	ErrHeaderTimeout = errors.New("read_header_timeout exceeds read_timeout")
	// ErrWriteTimeout means server.write_timeout does not exceed limits.queue_timeout + limits.timeout.
	ErrWriteTimeout = errors.New("write_timeout does not exceed queue_timeout + timeout")
)

// Config is the root configuration shared by the CLI and the server.
// It is treated as immutable once passed to the consumers.
type Config struct {
	Server Server `toml:"server"`
	Limits Limits `toml:"limits"`
	Types  Types  `toml:"types"`
}

// Server holds the HTTP server settings.
type Server struct {
	Addr  string `toml:"addr"`
	Debug bool   `toml:"debug"`
	// ReadTimeout limits reading an entire request.
	ReadTimeout Duration `toml:"read_timeout"`
	// ReadHeaderTimeout limits reading the request headers; it must not exceed ReadTimeout.
	ReadHeaderTimeout Duration `toml:"read_header_timeout"`
	// WriteTimeout limits writing a response. It must exceed Limits.QueueTimeout + Limits.Timeout,
	// because a request may wait for a slot and then generate before the response is written.
	WriteTimeout Duration `toml:"write_timeout"`
	// IdleTimeout limits how long keep-alive connections stay open.
	IdleTimeout Duration `toml:"idle_timeout"`
	// ShutdownTimeout is the graceful shutdown deadline.
	ShutdownTimeout Duration `toml:"shutdown_timeout"`
	// CacheMaxAge is the Cache-Control max-age of images; 0 means no header.
	CacheMaxAge Duration `toml:"cache_max_age"`
}

// Limits bounds the work of a single image and the number of concurrent images.
type Limits struct {
	// MaxPixels caps width × height of one image.
	MaxPixels int64 `toml:"max_pixels"`
	// MaxConcurrent is the number of images generated at the same time.
	MaxConcurrent int `toml:"max_concurrent"`
	// MaxQueued is the number of requests waiting for a slot; others are rejected at once.
	MaxQueued int `toml:"max_queued"`
	// QueueTimeout limits waiting for a slot; 0 means no waiting.
	QueueTimeout Duration `toml:"queue_timeout"`
	// Timeout limits generating and encoding one image after a slot is taken.
	Timeout Duration `toml:"timeout"`
	// Workers is the number of goroutines per image; 0 means GOMAXPROCS.
	Workers int `toml:"workers"`
	// MaxIterations caps the iterations of the pixel fractal presets.
	MaxIterations int `toml:"max_iterations"`
	// MaxDepth caps the recursion depth of the outline fractal presets.
	MaxDepth int `toml:"max_depth"`
}

// Default returns the complete default configuration. Every call builds new
// slices, so decoding a file on top of the result never changes other copies.
func Default() *Config {
	return &Config{
		Server: Server{
			Addr:              ":8080",
			Debug:             false,
			ReadTimeout:       Duration(10 * time.Second),
			ReadHeaderTimeout: Duration(5 * time.Second),
			WriteTimeout:      Duration(30 * time.Second),
			IdleTimeout:       Duration(60 * time.Second),
			ShutdownTimeout:   Duration(10 * time.Second),
			CacheMaxAge:       Duration(time.Hour),
		},
		Limits: Limits{
			MaxPixels:     4096 * 4096,
			MaxConcurrent: 2,
			MaxQueued:     8,
			QueueTimeout:  Duration(2 * time.Second),
			Timeout:       Duration(15 * time.Second),
			Workers:       0,
			MaxIterations: 4096,
			MaxDepth:      8,
		},
		Types: defaultTypes(),
	}
}

// Load reads the TOML file at path on top of Default and validates the result.
// Keys missing from the file keep their default values.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var doc map[string]any
	if err = toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, decodeError(err))
	}
	if err = checkTextFields(doc, reflect.TypeFor[Config](), ""); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}

	cfg := Default()
	if err = toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, decodeError(err))
	}
	if err = cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config %s: %w", path, err)
	}
	return cfg, nil
}

// decodeError adds a sentinel error and the position in the file to a go-toml error.
func decodeError(err error) error {
	if strictErr, ok := errors.AsType[*toml.StrictMissingError](err); ok {
		fields := make([]string, 0, len(strictErr.Errors))
		for _, e := range strictErr.Errors {
			row, _ := e.Position()
			fields = append(fields, fmt.Sprintf("%s (line %d)", strings.Join(e.Key(), "."), row))
		}
		return errors.Join(ErrUnknownField, errors.New(strings.Join(fields, ", ")))
	}

	if decodeErr, ok := errors.AsType[*toml.DecodeError](err); ok {
		row, column := decodeErr.Position()
		return errors.Join(ErrDecode, fmt.Errorf("line %d, column %d: %w", row, column, err))
	}
	return errors.Join(ErrDecode, err)
}

// validate checks every section and accumulates all problems.
func (c *Config) validate() error {
	return errors.Join(
		c.Server.validate(),
		c.Limits.validate(),
		c.Types.validate(&c.Limits),
		c.crossValidate(),
	)
}

func (s *Server) validate() error {
	var v validator
	if s.Addr == "" {
		v.addf(ErrRequired, "server.addr is empty")
	}
	v.positiveDuration("server.read_timeout", s.ReadTimeout)
	v.positiveDuration("server.read_header_timeout", s.ReadHeaderTimeout)
	v.positiveDuration("server.write_timeout", s.WriteTimeout)
	v.positiveDuration("server.idle_timeout", s.IdleTimeout)
	v.positiveDuration("server.shutdown_timeout", s.ShutdownTimeout)
	checkRange(&v, "server.cache_max_age", s.CacheMaxAge, 0, maxCacheMaxAge)
	return v.err()
}

func (l *Limits) validate() error {
	var v validator
	checkRange(&v, "limits.max_pixels", l.MaxPixels, minMaxPixels, maxMaxPixels)
	checkMin(&v, "limits.max_concurrent", l.MaxConcurrent, 1)
	checkMin(&v, "limits.max_queued", l.MaxQueued, 0)
	checkRange(&v, "limits.queue_timeout", l.QueueTimeout, 0, maxTimeout)
	v.positiveDuration("limits.timeout", l.Timeout)
	checkMin(&v, "limits.workers", l.Workers, 0)
	checkMin(&v, "limits.max_iterations", l.MaxIterations, 1)
	checkMin(&v, "limits.max_depth", l.MaxDepth, 0)
	return v.err()
}

func (c *Config) crossValidate() error {
	var v validator
	s, l := &c.Server, &c.Limits
	// ReadTimeout starts before the headers are read, so a longer header
	// timeout would leave an already expired deadline for the request body.
	if s.ReadHeaderTimeout > s.ReadTimeout {
		v.addf(ErrHeaderTimeout, "server.read_header_timeout (%s) > server.read_timeout (%s)",
			s.ReadHeaderTimeout, s.ReadTimeout)
	}
	// A request may wait for a slot and then generate until its deadline;
	// the response must still have time to be written. The comparison avoids
	// the sum, which could overflow for values not checked by range yet.
	if s.WriteTimeout <= l.QueueTimeout || s.WriteTimeout-l.QueueTimeout <= l.Timeout {
		v.addf(ErrWriteTimeout, "server.write_timeout (%s) <= limits.queue_timeout (%s) + limits.timeout (%s)",
			s.WriteTimeout, l.QueueTimeout, l.Timeout)
	}
	return v.err()
}

// validator accumulates validation errors; each one joins a sentinel error
// with a message naming the field.
type validator struct {
	errs []error
}

func (v *validator) addf(sentinel error, format string, args ...any) {
	v.errs = append(v.errs, errors.Join(sentinel, fmt.Errorf(format, args...)))
}

func (v *validator) err() error {
	return errors.Join(v.errs...)
}

// positiveDuration checks that d is in (0, maxTimeout].
func (v *validator) positiveDuration(field string, d Duration) {
	if d <= 0 || d > maxTimeout {
		v.addf(ErrOutOfRange, "%s = %s, want (0s, %s]", field, d, maxTimeout)
	}
}

// finite checks that value is neither NaN nor infinite and reports whether it is.
func (v *validator) finite(field string, value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		v.addf(ErrNotFinite, "%s = %v", field, value)
		return false
	}
	return true
}

// positive checks that value is finite and greater than zero.
func (v *validator) positive(field string, value float64) {
	if v.finite(field, value) && value <= 0 {
		v.addf(ErrOutOfRange, "%s = %v, want > 0", field, value)
	}
}

// fraction checks that value is finite and in (0, maxFraction].
func (v *validator) fraction(field string, value float64) {
	if v.finite(field, value) && (value <= 0 || value > maxFraction) {
		v.addf(ErrOutOfRange, "%s = %v, want (0, %v]", field, value, maxFraction)
	}
}

// point checks that p is a pair of finite numbers.
func (v *validator) point(field string, p []float64) {
	if len(p) != 2 {
		v.addf(ErrOutOfRange, "%s has %d values, want 2", field, len(p))
		return
	}
	for i, value := range p {
		v.finite(fmt.Sprintf("%s[%d]", field, i), value)
	}
}

// colors checks that the list has at least minLen colors.
func (v *validator) colors(field string, colors []Color, minLen int) {
	if len(colors) < minLen {
		v.addf(ErrTooFewColors, "%s has %d colors, want at least %d", field, len(colors), minLen)
	}
}

// checkRange checks that value is in [lo, hi].
func checkRange[T cmp.Ordered](v *validator, field string, value, lo, hi T) {
	if value < lo || value > hi {
		v.addf(ErrOutOfRange, "%s = %v, want [%v, %v]", field, value, lo, hi)
	}
}

// checkMin checks that value is at least lo.
func checkMin[T cmp.Ordered](v *validator, field string, value, lo T) {
	if value < lo {
		v.addf(ErrOutOfRange, "%s = %v, want >= %v", field, value, lo)
	}
}
