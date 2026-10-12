package fractal

import (
	"fmt"
	"math"
	"testing"

	"github.com/z0rr0/image89/internal/figure"
)

func TestViewportOddCenter(t *testing.T) {
	t.Parallel()

	v := newViewport(0, 3.5, 2.5, figure.Size{Width: 641, Height: 481})
	if got := v.At(320, 240); got != 0 {
		t.Errorf("At(320, 240) = %v, want exactly 0", got)
	}
}

func TestViewportEvenCenter(t *testing.T) {
	t.Parallel()

	v := newViewport(0, 3.5, 2.5, figure.Size{Width: 640, Height: 480})
	topLeft := v.At(319, 239)
	if real(topLeft) >= 0 || imag(topLeft) <= 0 {
		t.Fatalf("At(319, 239) = %v, want the upper left quadrant", topLeft)
	}
	tests := []struct {
		x, y int
		want complex128
	}{
		{x: 320, y: 239, want: complex(-real(topLeft), imag(topLeft))},
		{x: 319, y: 240, want: complex(real(topLeft), -imag(topLeft))},
		{x: 320, y: 240, want: -topLeft},
	}
	for _, tt := range tests {
		if got := v.At(tt.x, tt.y); got != tt.want {
			t.Errorf("At(%d, %d) = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}
}

func TestViewportStep(t *testing.T) {
	t.Parallel()

	const (
		center = -0.75 + 0.25i
		width  = 3.5
		height = 2.5
	)
	tests := []struct {
		size figure.Size
		want float64
	}{
		{size: figure.Size{Width: 640, Height: 480}, want: width / 640},
		{size: figure.Size{Width: 480, Height: 640}, want: width / 480},
		{size: figure.Size{Width: 640, Height: 640}, want: width / 640},
		{size: figure.Size{Width: 1000, Height: 100}, want: height / 100},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%dx%d", tt.size.Width, tt.size.Height), func(t *testing.T) {
			t.Parallel()

			v := newViewport(center, width, height, tt.size)
			if v.step != tt.want {
				t.Errorf("step = %v, want %v", v.step, tt.want)
			}
			origin := v.At(0, 0)
			stepX := real(v.At(1, 0) - origin)
			stepY := imag(origin - v.At(0, 1))
			const eps = 1e-12
			if math.Abs(stepX-tt.want) > eps || math.Abs(stepY-tt.want) > eps {
				t.Errorf("pixel steps = (%v, %v), want %v along both axes", stepX, stepY, tt.want)
			}

			// The image edges enclose the whole area.
			w, h := float64(tt.size.Width), float64(tt.size.Height)
			left, right := real(center)-w/2*v.step, real(center)+w/2*v.step
			bottom, top := imag(center)-h/2*v.step, imag(center)+h/2*v.step
			if left > real(center)-width/2+eps || right < real(center)+width/2-eps ||
				bottom > imag(center)-height/2+eps || top < imag(center)+height/2-eps {
				t.Errorf("image [%v, %v]×[%v, %v] does not enclose the area", left, right, bottom, top)
			}
		})
	}
}
