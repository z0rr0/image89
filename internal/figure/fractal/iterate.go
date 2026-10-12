package fractal

import "math"

// mandelbrotRadius2 is the squared escape radius of the Mandelbrot set: an
// orbit that leaves the disk of radius 2 never comes back.
const mandelbrotRadius2 = 4

// escapeJulia iterates z ← z² + c starting from z. Before every update it
// checks |z|² > r2, so it returns the number of updates done and the first z
// outside the radius: a result below maxIter means the point escaped, maxIter
// means it is treated as a point of the set. The squares and the product are
// rounded explicitly, so the compiler does not fuse them into FMA
// instructions and every architecture gets the same orbit. The loop body is a
// single block, so under -race -cover an iteration costs one atomic coverage
// counter update instead of two.
func escapeJulia(z, c complex128, r2 float64, maxIter int) (int, complex128) {
	x, y := real(z), imag(z)
	cx, cy := real(c), imag(c)
	x2, y2 := float64(x*x), float64(y*y)
	n := 0
	for ; n < maxIter && x2+y2 <= r2; n++ {
		xy := float64(x * y)
		x, y = x2-y2+cx, xy+xy+cy
		x2, y2 = float64(x*x), float64(y*y)
	}
	return n, complex(x, y)
}

// escapeMandelbrot iterates z ← z² + c from z = 0 with the escape radius 2;
// the result means the same as for escapeJulia.
func escapeMandelbrot(c complex128, maxIter int) (int, complex128) {
	return escapeJulia(0, c, mandelbrotRadius2, maxIter)
}

// abs2 returns |z|² with the products rounded explicitly, as in escapeJulia.
func abs2(z complex128) float64 {
	return float64(real(z)*real(z)) + float64(imag(z)*imag(z))
}

// newtonMinDeriv2 is the smallest |zⁿ⁻¹|² of a Newton step: closer to zero the
// derivative of zⁿ − 1 vanishes, the step would throw z beyond 1e100, and the
// point is treated as converging to no root.
const newtonMinDeriv2 = 1e-200

// newtonRoot runs Newton's method for zⁿ − 1 with n = degree from z and
// returns the index k of the root exp(2πik/n) it converges to and the number
// of steps done. Before every step it checks |zⁿ − 1|² < eps2, so a point at a
// root gives 0 steps; a step shorter than eps (|Δz|² < eps2) also means
// convergence and is counted. A vanishing derivative or maxIter steps without
// convergence give k = −1. The arithmetic is real with explicitly rounded
// products, as in escapeJulia, so every architecture gets the same result.
func newtonRoot(z complex128, degree int, eps2 float64, maxIter int) (int, int) {
	x, y := real(z), imag(z)
	scale := float64(degree)
	for n := range maxIter {
		// w = zⁿ⁻¹, f = zⁿ − 1 = w·z − 1.
		wx, wy := 1.0, 0.0
		for range degree - 1 {
			wx, wy = float64(wx*x)-float64(wy*y), float64(wx*y)+float64(wy*x)
		}
		fx, fy := float64(wx*x)-float64(wy*y)-1, float64(wx*y)+float64(wy*x)
		if float64(fx*fx)+float64(fy*fy) < eps2 {
			return newtonRootIndex(x, y, degree), n
		}
		w2 := float64(wx*wx) + float64(wy*wy)
		if w2 < newtonMinDeriv2 {
			return -1, n
		}
		// Δz = f / (n·w) = f·conj(w) / (n·|w|²).
		s := float64(scale * w2)
		dx := (float64(fx*wx) + float64(fy*wy)) / s
		dy := (float64(fy*wx) - float64(fx*wy)) / s
		x, y = x-dx, y-dy
		if float64(dx*dx)+float64(dy*dy) < eps2 {
			return newtonRootIndex(x, y, degree), n + 1
		}
	}
	return -1, maxIter
}

// newtonRootIndex returns the index k in [0, degree) of the root exp(2πik/n)
// nearest by angle to the point (x, y).
func newtonRootIndex(x, y float64, degree int) int {
	k := int(math.Round(math.Atan2(y, x) * float64(degree) / (2 * math.Pi)))
	return (k%degree + degree) % degree
}
