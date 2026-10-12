package figure

import (
	"errors"
	"slices"
	"testing"
)

func TestKinds(t *testing.T) {
	t.Parallel()

	want := []Kind{Mandelbrot, Julia, Newton, Sierpinski, Koch, Lissajous}
	got := Kinds()
	if !slices.Equal(got, want) {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
	got[0] = "changed"
	if again := Kinds(); !slices.Equal(again, want) {
		t.Errorf("Kinds() after changing a previous result = %v, want %v", again, want)
	}
}

func TestParseKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		want    Kind
		wantErr bool
	}{
		{name: "mandelbrot", want: Mandelbrot},
		{name: "julia", want: Julia},
		{name: "newton", want: Newton},
		{name: "sierpinski", want: Sierpinski},
		{name: "koch", want: Koch},
		{name: "lissajous", want: Lissajous},
		{name: "", wantErr: true},
		{name: "nope", wantErr: true},
		{name: "Mandelbrot", wantErr: true},
		{name: " mandelbrot", wantErr: true},
		{name: "mandelbrot/", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseKind(tt.name)
			if tt.wantErr {
				if !errors.Is(err, ErrUnknownKind) {
					t.Fatalf("ParseKind(%q) error = %v, want %v", tt.name, err, ErrUnknownKind)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseKind(%q) unexpected error: %v", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("ParseKind(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestParseKindAllKinds(t *testing.T) {
	t.Parallel()

	for _, k := range Kinds() {
		if got, err := ParseKind(string(k)); err != nil || got != k {
			t.Errorf("ParseKind(%q) = %q, %v; want %q, nil", k, got, err, k)
		}
	}
}

func TestParseSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text    string
		want    Size
		wantErr bool
	}{
		{text: "640x480", want: Size{Width: 640, Height: 480}},
		{text: "480x640", want: Size{Width: 480, Height: 640}},
		{text: "32x32", want: Size{Width: 32, Height: 32}},
		{text: "65536x32", want: Size{Width: 65536, Height: 32}},
		{text: "65536x65536", want: Size{Width: 65536, Height: 65536}},
		{text: "0x0", wantErr: true},
		{text: "31x100", wantErr: true},
		{text: "100x31", wantErr: true},
		{text: "65537x32", wantErr: true},
		{text: "640X480", wantErr: true},
		{text: "640x", wantErr: true},
		{text: "x480", wantErr: true},
		{text: "x", wantErr: true},
		{text: "", wantErr: true},
		{text: "640", wantErr: true},
		{text: "+640x480", wantErr: true},
		{text: "640x+480", wantErr: true},
		{text: "-640x480", wantErr: true},
		{text: " 640x480", wantErr: true},
		{text: "640x480 ", wantErr: true},
		{text: "640 x480", wantErr: true},
		{text: "640x480x1", wantErr: true},
		{text: "99999x1", wantErr: true},
		{text: "0640x480", wantErr: true},
		{text: "640x0480", wantErr: true},
		{text: "99999999999x1", wantErr: true},
		{text: "1_024x1024", wantErr: true},
		{text: "0x640x480", wantErr: true},
		{text: "６４０x480", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			got, err := ParseSize(tt.text)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidSize) {
					t.Fatalf("ParseSize(%q) error = %v, want %v", tt.text, err, ErrInvalidSize)
				}
				if got != (Size{}) {
					t.Errorf("ParseSize(%q) = %+v with an error, want zero Size", tt.text, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSize(%q) unexpected error: %v", tt.text, err)
			}
			if got != tt.want {
				t.Errorf("ParseSize(%q) = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}

func TestSizeValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		size    Size
		wantErr bool
	}{
		{name: "min", size: Size{Width: MinSide, Height: MinSide}},
		{name: "max", size: Size{Width: MaxSide, Height: MaxSide}},
		{name: "default", size: Size{Width: DefaultSide, Height: DefaultSide}},
		{name: "zero", size: Size{}, wantErr: true},
		{name: "negative width", size: Size{Width: -640, Height: 480}, wantErr: true},
		{name: "width below min", size: Size{Width: MinSide - 1, Height: 480}, wantErr: true},
		{name: "height below min", size: Size{Width: 640, Height: MinSide - 1}, wantErr: true},
		{name: "width above max", size: Size{Width: MaxSide + 1, Height: 480}, wantErr: true},
		{name: "height above max", size: Size{Width: 640, Height: MaxSide + 1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.size.Validate()
			switch {
			case tt.wantErr && !errors.Is(err, ErrInvalidSize):
				t.Errorf("%+v.Validate() error = %v, want %v", tt.size, err, ErrInvalidSize)
			case !tt.wantErr && err != nil:
				t.Errorf("%+v.Validate() unexpected error: %v", tt.size, err)
			}
		})
	}
}

func TestSizePixels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		size Size
		want int64
	}{
		{size: Size{Width: 640, Height: 480}, want: 307200},
		{size: Size{Width: MinSide, Height: MinSide}, want: 1024},
		{size: Size{Width: MaxSide, Height: MaxSide}, want: 1 << 32},
	}

	for _, tt := range tests {
		if got := tt.size.Pixels(); got != tt.want {
			t.Errorf("%+v.Pixels() = %d, want %d", tt.size, got, tt.want)
		}
	}
}
