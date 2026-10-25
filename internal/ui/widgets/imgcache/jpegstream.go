package imgcache

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache/internal/jpegrows"
	xdraw "golang.org/x/image/draw"
)

// Keep JPEG's original Huffman/IDCT precision and the existing ApproxBiLinear
// samples. Only the decoder's storage changes: two MCU rows replace the full
// YCbCr/Gray source. The callback consumes completed rows before their reuse.
func decodeJPEGStored(data []byte) (*image.RGBA, bool, error) {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return nil, false, nil
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, true, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, true, errors.New("imgcache: JPEG has no pixels")
	}
	if max(cfg.Width, cfg.Height) <= maxStoredSide {
		return nil, false, nil
	}
	var dst *image.RGBA
	next := 0
	err = jpegrows.Decode(bytes.NewReader(data), func(src image.RGBA64Image, ready int) {
		if dst == nil {
			dst = image.NewRGBA(storedBounds(cfg.Width, cfg.Height))
		}
		end := next
		yscale := float64(cfg.Height) / float64(dst.Rect.Dy())
		for end < dst.Rect.Dy() {
			// Same source coordinates as ApproxBiLinear. Both adjacent samples
			// must be available, including across MCU-row boundaries.
			sy := (float64(end)+0.5)*yscale - 0.5
			if min(cfg.Height-1, int(sy)+1) >= ready {
				break
			}
			end++
		}
		if end > next {
			band := dst.SubImage(image.Rect(0, next, dst.Rect.Dx(), end)).(*image.RGBA)
			xdraw.ApproxBiLinear.Scale(band, dst.Rect, src, src.Bounds(), xdraw.Src, nil)
			next = end
		}
	})
	if errors.Is(err, jpegrows.ErrUnsupported) {
		return nil, false, nil
	}
	return dst, true, err
}
