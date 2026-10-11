package config

import (
	"errors"
	"image/color"
	"testing"
)

func TestColorUnmarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text    string
		want    Color
		wantErr bool
	}{
		{text: "#FF8800", want: Color{0xff, 0x88, 0x00}},
		{text: "#ff8800", want: Color{0xff, 0x88, 0x00}},
		{text: "#AbCdEf", want: Color{0xab, 0xcd, 0xef}},
		{text: "#000000", want: Color{}},
		{text: "ff8800", wantErr: true},
		{text: "#ggg", wantErr: true},
		{text: "#fff", wantErr: true},
		{text: "#gggggg", wantErr: true},
		{text: "#ff88001", wantErr: true},
		{text: " #ff8800", wantErr: true},
		{text: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			var c Color
			err := c.UnmarshalText([]byte(tt.text))
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidColor) {
					t.Fatalf("UnmarshalText(%q) error = %v, want %v", tt.text, err, ErrInvalidColor)
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalText(%q) unexpected error: %v", tt.text, err)
			}
			if c != tt.want {
				t.Errorf("UnmarshalText(%q) = %+v, want %+v", tt.text, c, tt.want)
			}
		})
	}
}

func TestColorMarshalText(t *testing.T) {
	t.Parallel()

	c := Color{0xab, 0x0c, 0xff}
	text, err := c.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(text) != "#ab0cff" {
		t.Errorf("MarshalText = %q, want #ab0cff", text)
	}

	var back Color
	if err = back.UnmarshalText(text); err != nil || back != c {
		t.Errorf("round trip = %+v, %v; want %+v", back, err, c)
	}
}

func TestColorRGBA(t *testing.T) {
	t.Parallel()

	got := Color{1, 2, 3}.RGBA()
	if want := (color.RGBA{R: 1, G: 2, B: 3, A: 0xff}); got != want {
		t.Errorf("RGBA() = %+v, want %+v", got, want)
	}
}
