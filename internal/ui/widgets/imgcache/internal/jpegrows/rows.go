package jpegrows

import (
	"errors"
	"image"
	"image/color"
	"io"
)

// ErrUnsupported asks the caller to use the standard decoder. No whole-image
// source pixels or progressive coefficients have been allocated at that point.
var ErrUnsupported = errors.New("jpegrows: requires one baseline grayscale or YCbCr scan")

// Decode calls consume after each complete MCU row. The image has the full
// bounds, but only its last two MCU rows may be sampled. ready is the exclusive
// bottom row. consume must finish reading before returning and must not retain
// the image. No goroutines or full-size pixel buffers are used.
// A result may only be published if Decode returns nil (including the EOI check).
func Decode(r io.Reader, consume func(image.RGBA64Image, int)) error {
	d := decoder{consume: consume}
	_, err := d.decode(r, false)
	return err
}

type rows struct{ d *decoder }

func (r *rows) Bounds() image.Rectangle { return image.Rect(0, 0, r.d.width, r.d.height) }
func (r *rows) ColorModel() color.Model { return color.RGBA64Model }
func (r *rows) At(x, y int) color.Color { return r.RGBA64At(x, y) }
func (r *rows) RGBA64At(x, y int) color.RGBA64 {
	y %= 16 * r.d.maxV
	if r.d.img1 != nil {
		return r.d.img1.RGBA64At(x, y)
	}
	return r.d.img3.RGBA64At(x, y)
}
