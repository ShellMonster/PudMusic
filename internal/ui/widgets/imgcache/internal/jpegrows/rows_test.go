package jpegrows

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJPEGRowCorpusMatchesStandardDecoder(t *testing.T) {
	files, err := filepath.Glob("testdata/*.jpeg")
	if err != nil || len(files) != 30 {
		t.Fatalf("corpus: %d files, err=%v", len(files), err)
	}
	for _, name := range files {
		t.Run(filepath.Base(name), func(t *testing.T) {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			want, standardErr := jpeg.Decode(bytes.NewReader(data))
			next := 0
			err = Decode(bytes.NewReader(data), func(src image.RGBA64Image, ready int) {
				if standardErr != nil {
					return
				}
				for y := max(0, next-1); y < ready; y++ {
					for x := 0; x < src.Bounds().Dx(); x++ {
						r, g, b, a := want.At(x, y).RGBA()
						got := src.RGBA64At(x, y)
						if uint32(got.R) != r || uint32(got.G) != g || uint32(got.B) != b || uint32(got.A) != a {
							t.Fatalf("pixel differs at %d,%d", x, y)
						}
					}
				}
				next = ready
			})
			fallback := strings.Contains(name, "progressive") || strings.Contains(name, ".rgb.") || strings.Contains(name, ".cmyk.") || strings.Contains(name, "separate.dc")
			if fallback {
				if !errors.Is(err, ErrUnsupported) || next != 0 {
					t.Fatalf("fallback err=%v rows=%d", err, next)
				}
			} else if err != nil || standardErr != nil || next != want.Bounds().Dy() {
				t.Fatalf("decode err=%v standard=%v rows=%d", err, standardErr, next)
			}
		})
	}
}
