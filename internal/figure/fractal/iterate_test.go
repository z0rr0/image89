package fractal

import (
	"testing"
)

func TestEscapeMandelbrot(t *testing.T) {
	t.Parallel()

	const maxIter = 256
	tests := []struct {
		name    string
		c       complex128
		escaped bool
		maxN    int // upper bound of n for an escaped point
	}{
		{name: "origin", c: 0},
		{name: "period two", c: -1},
		{name: "cardioid", c: -0.1 + 0.1i},
		{name: "one", c: 1, escaped: true, maxN: 3},
		{name: "two", c: 2, escaped: true, maxN: 2},
		{name: "far", c: 10i, escaped: true, maxN: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n, z := escapeMandelbrot(tt.c, maxIter)
			if !tt.escaped {
				if n != maxIter {
					t.Errorf("escapeMandelbrot(%v) = %d, want %d (inside)", tt.c, n, maxIter)
				}
				return
			}
			if n > tt.maxN {
				t.Errorf("escapeMandelbrot(%v) = %d, want <= %d", tt.c, n, tt.maxN)
			}
			if abs2(z) <= mandelbrotRadius2 {
				t.Errorf("escapeMandelbrot(%v) last z = %v, want |z| > 2", tt.c, z)
			}
		})
	}
}

func TestEscapeJulia(t *testing.T) {
	t.Parallel()

	const maxIter = 256
	tests := []struct {
		name    string
		z, c    complex128
		r2      float64
		escaped bool
		wantN   int
	}{
		{name: "zero inside", z: 0, c: 0, r2: 4},
		{name: "unit circle inside", z: 1i, c: 0, r2: 4},
		{name: "two escapes", z: 2, c: 0, r2: 4, escaped: true, wantN: 1},
		{name: "outside at once", z: 3, c: 0, r2: 4, escaped: true, wantN: 0},
		{name: "radius of large c", z: 2.5, c: 2.5, r2: 6.25, escaped: true, wantN: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n, z := escapeJulia(tt.z, tt.c, tt.r2, maxIter)
			if !tt.escaped {
				if n != maxIter {
					t.Errorf("escapeJulia(%v, %v) = %d, want %d (inside)", tt.z, tt.c, n, maxIter)
				}
				return
			}
			if n != tt.wantN {
				t.Errorf("escapeJulia(%v, %v) = %d, want %d", tt.z, tt.c, n, tt.wantN)
			}
			if abs2(z) <= tt.r2 {
				t.Errorf("escapeJulia(%v, %v) last z = %v, want |z|² > %v", tt.z, tt.c, z, tt.r2)
			}
		})
	}
}

func TestEscapeZeroIterations(t *testing.T) {
	t.Parallel()

	if n, _ := escapeMandelbrot(10, 0); n != 0 {
		t.Errorf("escapeMandelbrot with maxIter 0 = %d, want 0", n)
	}
}
