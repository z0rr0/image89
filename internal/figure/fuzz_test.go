package figure

import (
	"errors"
	"testing"
)

// FuzzParseSize checks the parser of the size in image URLs. Signs, spaces,
// leading zeros and other spellings are rejected, so an accepted text is the
// canonical form of its size: String gives it back exactly.
func FuzzParseSize(f *testing.F) {
	for _, s := range []string{
		"640x480", "32x32", "32768x32768", "32769x32", "31x32", "0x0", "0640x480", "+640x480",
		" 640x480", "640x480 ", "640X480", "640x480x1", "x", "", "６４０x480", "1_024x1024", "99999999999x1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		size, err := ParseSize(s)
		if err != nil {
			if !errors.Is(err, ErrInvalidSize) {
				t.Fatalf("ParseSize(%q) error %v is not %v", s, err, ErrInvalidSize)
			}
			if size != (Size{}) {
				t.Fatalf("ParseSize(%q) = %v with an error, want the zero size", s, size)
			}
			return
		}
		if err = size.Validate(); err != nil {
			t.Fatalf("ParseSize(%q) = %v, which is invalid: %v", s, size, err)
		}
		if got := size.String(); got != s {
			t.Fatalf("ParseSize(%q) = %v, String() = %q: the accepted text is not canonical", s, size, got)
		}
	})
}
