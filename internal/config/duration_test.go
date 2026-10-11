package config

import (
	"errors"
	"testing"
	"time"
)

func TestDurationUnmarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text    string
		want    time.Duration
		wantErr bool
		errIs   error
	}{
		{text: "10s", want: 10 * time.Second},
		{text: "1m30s", want: 90 * time.Second},
		{text: "0s", want: 0},
		{text: "-1s", wantErr: true, errIs: ErrNegativeDuration},
		{text: "10", wantErr: true},
		{text: "abc", wantErr: true},
		{text: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			var d Duration
			err := d.UnmarshalText([]byte(tt.text))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("UnmarshalText(%q) error = nil, want error", tt.text)
				}
				if tt.errIs != nil && !errors.Is(err, tt.errIs) {
					t.Errorf("UnmarshalText(%q) error = %v, want %v", tt.text, err, tt.errIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalText(%q) unexpected error: %v", tt.text, err)
			}
			if d.Timed() != tt.want {
				t.Errorf("UnmarshalText(%q) = %v, want %v", tt.text, d, tt.want)
			}
		})
	}
}

func TestDurationMarshalText(t *testing.T) {
	t.Parallel()

	d := Duration(90 * time.Second)
	text, err := d.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(text) != "1m30s" || d.String() != "1m30s" {
		t.Errorf("MarshalText = %q, String = %q, want 1m30s", text, d.String())
	}

	var back Duration
	if err = back.UnmarshalText(text); err != nil || back != d {
		t.Errorf("round trip = %v, %v; want %v", back, err, d)
	}
}
