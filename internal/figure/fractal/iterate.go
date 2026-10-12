package fractal

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
