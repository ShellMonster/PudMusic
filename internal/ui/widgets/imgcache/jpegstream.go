package imgcache

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache/internal/jpegrows"
	xdraw "golang.org/x/image/draw"
)

// decodeJPEGStored handles a JPEG without EXIF orientation. Oriented JPEGs use
// decodeJPEGStoredOriented so the row decoder can map directly into the target.
func decodeJPEGStored(data []byte, maxSide int) (*image.RGBA, bool, error) {
	return decodeJPEGStoredOriented(data, maxSide, 1)
}

// decodeJPEGStoredOriented decodes an oriented JPEG into its final target-size
// pixels. It never materializes the un-oriented source image: jpegrows keeps
// only its MCU-row ring, while each completed source row is mapped into the
// destination rows or columns that depend on it.
func decodeJPEGStoredOriented(data []byte, maxSide, orientation int) (*image.RGBA, bool, error) {
	if maxSide <= 0 {
		maxSide = maxStoredSide
	}
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
	if orientation < 2 || orientation > 8 {
		if max(cfg.Width, cfg.Height) <= maxSide {
			return nil, false, nil
		}
		orientation = 1
	}
	if orientation == 1 && max(cfg.Width, cfg.Height) <= maxSide {
		return nil, false, nil
	}
	if orientation == 1 {
		dst := image.NewRGBA(boundsWithMaxSide(cfg.Width, cfg.Height, maxSide))
		return decodeJPEGScaledRows(data, cfg.Width, cfg.Height, dst)
	}

	// Scale while the JPEG is still in its native orientation, then apply the
	// EXIF transform to the target-sized RGBA image. Rotation and mirroring
	// commute with uniform scaling, so this keeps the pixel oracle identical
	// while avoiding a source-sized intermediate image.
	raw := image.NewRGBA(boundsWithMaxSide(cfg.Width, cfg.Height, maxSide))
	scaled, handled, err := decodeJPEGScaledRows(data, cfg.Width, cfg.Height, raw)
	if !handled || err != nil {
		return scaled, handled, err
	}
	return orientImage(scaled, orientation).(*image.RGBA), true, nil
}

func decodeJPEGScaledRows(data []byte, width, height int, dst *image.RGBA) (*image.RGBA, bool, error) {
	next := 0
	err := jpegrows.Decode(bytes.NewReader(data), func(src image.RGBA64Image, ready int) {
		end := next
		yscale := float64(height) / float64(dst.Rect.Dy())
		for end < dst.Rect.Dy() {
			sy := (float64(end)+0.5)*yscale - 0.5
			if min(height-1, int(sy)+1) >= ready {
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

func decodeJPEGOrientedRows(data []byte, width, height, orientation int, dst *image.RGBA) (*image.RGBA, bool, error) {
	ow, oh := width, height
	if orientation >= 5 {
		ow, oh = height, width
	}
	xmap := scaledAxis(ow, dst.Rect.Dx())
	ymap := scaledAxis(oh, dst.Rect.Dy())
	axisIsX := orientation >= 5
	axisLen := dst.Rect.Dy()
	if axisIsX {
		axisLen = dst.Rect.Dx()
	}
	done := make([]bool, axisLen)
	err := jpegrows.Decode(bytes.NewReader(data), func(src image.RGBA64Image, ready int) {
		for axis := range done {
			if done[axis] || !orientedAxisReady(width, height, orientation, xmap, ymap, axis, ready) {
				continue
			}
			if axisIsX {
				for y := 0; y < dst.Rect.Dy(); y++ {
					writeOrientedPixel(dst, src, width, height, orientation, xmap, ymap, axis, y)
				}
			} else {
				for x := 0; x < dst.Rect.Dx(); x++ {
					writeOrientedPixel(dst, src, width, height, orientation, xmap, ymap, x, axis)
				}
			}
			done[axis] = true
		}
	})
	if errors.Is(err, jpegrows.ErrUnsupported) {
		return nil, false, nil
	}
	return dst, true, err
}

type scaledSample struct {
	lo, hi         int
	loFrac, hiFrac float64
}

func scaledAxis(source, target int) []scaledSample {
	out := make([]scaledSample, target)
	scale := float64(source) / float64(target)
	for i := range out {
		s := (float64(i)+0.5)*scale - 0.5
		lo := int(s)
		hi := lo + 1
		loFrac, hiFrac := 1-(s-float64(lo)), s-float64(lo)
		if s < 0 {
			lo, hi, loFrac, hiFrac = 0, 0, 1, 0
		} else if hi >= source {
			lo, hi, loFrac, hiFrac = source-1, source-1, 1, 0
		}
		out[i] = scaledSample{lo: lo, hi: hi, loFrac: loFrac, hiFrac: hiFrac}
	}
	return out
}

func orientedAxisReady(width, height, orientation int, xmap, ymap []scaledSample, axis, ready int) bool {
	var a, b int
	if orientation >= 5 {
		a, b = xmap[axis].lo, xmap[axis].hi
	} else {
		a, b = ymap[axis].lo, ymap[axis].hi
	}
	_, ay := orientedSourcePoint(width, height, orientation, a, 0)
	_, by := orientedSourcePoint(width, height, orientation, b, 0)
	if orientation < 5 {
		_, ay = orientedSourcePoint(width, height, orientation, 0, a)
		_, by = orientedSourcePoint(width, height, orientation, 0, b)
	}
	return max(ay, by) < ready
}

func writeOrientedPixel(dst *image.RGBA, src image.RGBA64Image, width, height, orientation int, xmap, ymap []scaledSample, x, y int) {
	xs, ys := orientedSourcePoint(width, height, orientation, xmap[x].lo, ymap[y].lo)
	r00 := src.RGBA64At(xs, ys)
	xs, ys = orientedSourcePoint(width, height, orientation, xmap[x].hi, ymap[y].lo)
	r10 := src.RGBA64At(xs, ys)
	xs, ys = orientedSourcePoint(width, height, orientation, xmap[x].lo, ymap[y].hi)
	r01 := src.RGBA64At(xs, ys)
	xs, ys = orientedSourcePoint(width, height, orientation, xmap[x].hi, ymap[y].hi)
	r11 := src.RGBA64At(xs, ys)
	blend := func(a, b, c, d uint16) uint8 {
		top := xmap[x].loFrac*float64(a) + xmap[x].hiFrac*float64(b)
		bottom := xmap[x].loFrac*float64(c) + xmap[x].hiFrac*float64(d)
		return uint8((ymap[y].loFrac*top + ymap[y].hiFrac*bottom) / 256)
	}
	o := dst.PixOffset(x, y)
	dst.Pix[o+0] = blend(r00.R, r10.R, r01.R, r11.R)
	dst.Pix[o+1] = blend(r00.G, r10.G, r01.G, r11.G)
	dst.Pix[o+2] = blend(r00.B, r10.B, r01.B, r11.B)
	dst.Pix[o+3] = blend(r00.A, r10.A, r01.A, r11.A)
}

func orientedSourcePoint(width, height, orientation, x, y int) (int, int) {
	switch orientation {
	case 2:
		return width - 1 - x, y
	case 3:
		return width - 1 - x, height - 1 - y
	case 4:
		return x, height - 1 - y
	case 5:
		return y, x
	case 6:
		return y, height - 1 - x
	case 7:
		return width - 1 - y, height - 1 - x
	case 8:
		return width - 1 - y, x
	default:
		return x, y
	}
}
