package imgcache

import (
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"io"

	xdraw "golang.org/x/image/draw"
)

// decodePNGStored handles non-interlaced 8-bit RGB/RGBA PNGs larger than the
// stored image. The BMG-263 fixture's 1920×1080 RGB PNG used to allocate an
// 8,294,400-byte RGBA in image/png.readImagePass, then throw it away after scaling.
//
// Keep only two unfiltered scanlines and let the SAME ApproxBiLinear scaler
// sample them via RGBA64Image (no per-pixel interface allocations). Its shrink
// path visits adjacent source rows in increasing order. Tests compare every
// output byte with image/png + ApproxBiLinear, including alpha and odd sizes.
//
// Other PNG variants and other formats still use their standard decoders. This
// is deliberately not a replacement PNG codec. Chunk CRCs, filters, the zlib
// checksum and even un-sampled trailing rows must validate before publishing.
func decodePNGStored(data []byte, maxSide int) (*image.RGBA, bool, error) {
	if maxSide <= 0 {
		maxSide = maxStoredSide
	}
	if len(data) < 33 || string(data[:8]) != "\x89PNG\r\n\x1a\n" || string(data[12:16]) != "IHDR" {
		return nil, false, nil
	}
	w, h := int64(binary.BigEndian.Uint32(data[16:20])), int64(binary.BigEndian.Uint32(data[20:24]))
	if data[24] != 8 || (data[25] != 2 && data[25] != 6) || data[28] != 0 || max(w, h) <= int64(maxSide) {
		return nil, false, nil
	}
	// Bound scratch rows; leave unusually wide/tall images to the existing
	// decoder, just as for other PNG variants, rather than rejecting them.
	if w > 1<<16 || h > 1<<16 {
		return nil, false, nil
	}
	bad := errors.New("imgcache: invalid streaming PNG")
	if binary.BigEndian.Uint32(data[8:12]) != 13 || data[26] != 0 || data[27] != 0 || w == 0 || h == 0 {
		return nil, true, bad
	}
	// Validate chunks without copying their payloads. tRNS needs the standard
	// decoder's transparency handling; indexed, 16-bit and Adam7 also go there.
	seenIDAT, endedIDAT, seenPLTE, ended := false, false, false, false
	for rest, first := data[8:], true; len(rest) > 0; first = false {
		if len(rest) < 12 {
			return nil, true, bad
		}
		n := int64(binary.BigEndian.Uint32(rest[:4]))
		if n > int64(len(rest)-12) {
			return nil, true, bad
		}
		chunk := rest[4 : 8+n]
		if crc32.ChecksumIEEE(chunk) != binary.BigEndian.Uint32(rest[8+n:12+n]) {
			return nil, true, bad
		}
		kind := string(chunk[:4])
		if seenIDAT && kind != "IDAT" {
			endedIDAT = true
		}
		switch kind {
		case "IHDR":
			if !first || n != 13 {
				return nil, true, bad
			}
		case "PLTE":
			if seenPLTE || seenIDAT || n == 0 || n > 768 || n%3 != 0 {
				return nil, true, bad
			}
			seenPLTE = true
		case "tRNS":
			return nil, false, nil
		case "IDAT":
			if endedIDAT {
				return nil, true, bad
			}
			seenIDAT = true
		case "IEND":
			if !seenIDAT || n != 0 {
				return nil, true, bad
			}
			ended = true
		default:
			if chunk[0]&0x20 == 0 { // unknown critical chunk
				return nil, true, bad
			}
		}
		rest = rest[12+n:]
		if ended {
			break // like image/png, ignore bytes after IEND
		}
	}
	if !ended {
		return nil, true, bad
	}
	zr, err := zlib.NewReader(&pngIDATReader{rest: data[8:]})
	if err != nil {
		return nil, true, err
	}
	defer zr.Close()
	bpp := 3
	if data[25] == 6 {
		bpp = 4
	}
	src := &pngRows{r: zr, rect: image.Rect(0, 0, int(w), int(h)), bpp: bpp, y: -1}
	src.rows[0] = make([]byte, int(w)*bpp+1)
	src.rows[1] = make([]byte, int(w)*bpp+1)
	dst := image.NewRGBA(boundsWithMaxSide(int(w), int(h), maxSide))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	for src.y < int(h)-1 && src.err == nil {
		src.next()
	}
	if src.err != nil {
		return nil, true, src.err
	}
	var extra [1]byte
	if n, err := io.ReadFull(zr, extra[:]); n != 0 || err != io.EOF {
		return nil, true, bad // checks zlib checksum and rejects surplus scanlines
	}
	return dst, true, nil
}

// All chunk boundaries have already been validated; join IDATs without a copy
// or one allocation per chunk (a network image can contain many tiny chunks).
type pngIDATReader struct{ rest, chunk []byte }

func (r *pngIDATReader) Read(p []byte) (int, error) {
	for len(r.chunk) == 0 {
		if len(r.rest) == 0 {
			return 0, io.EOF
		}
		n := int(binary.BigEndian.Uint32(r.rest[:4]))
		if string(r.rest[4:8]) == "IEND" {
			r.rest = nil
			return 0, io.EOF
		}
		if string(r.rest[4:8]) == "IDAT" {
			r.chunk = r.rest[8 : 8+n]
		}
		r.rest = r.rest[12+n:]
	}
	n := copy(p, r.chunk)
	r.chunk = r.chunk[n:]
	return n, nil
}

type pngRows struct {
	r    io.Reader
	rect image.Rectangle
	rows [2][]byte // filter byte followed by one scanline
	bpp  int
	y    int
	err  error
}

func (s *pngRows) Bounds() image.Rectangle { return s.rect }
func (s *pngRows) ColorModel() color.Model { return color.NRGBAModel }
func (s *pngRows) At(x, y int) color.Color { return s.RGBA64At(x, y) }
func (s *pngRows) RGBA64At(x, y int) color.RGBA64 {
	if !image.Pt(x, y).In(s.rect) {
		return color.RGBA64{}
	}
	for s.y < y && s.err == nil {
		s.next()
	}
	if s.err != nil {
		return color.RGBA64{}
	}
	if y < s.y-1 {
		s.err = errors.New("imgcache: PNG scaler requested a discarded row")
		return color.RGBA64{}
	}
	p := s.rows[y%2][1+x*s.bpp:]
	a := uint32(0xffff)
	if s.bpp == 4 {
		a = uint32(p[3]) * 0x101
	}
	return color.RGBA64{R: uint16(uint32(p[0]) * 0x101 * a / 0xffff), G: uint16(uint32(p[1]) * 0x101 * a / 0xffff), B: uint16(uint32(p[2]) * 0x101 * a / 0xffff), A: uint16(a)}
}

func (s *pngRows) next() {
	s.y++
	row, prev := s.rows[s.y%2], s.rows[(s.y+1)%2][1:]
	if _, s.err = io.ReadFull(s.r, row); s.err != nil {
		return
	}
	filter, row := row[0], row[1:]
	if filter > 4 {
		s.err = errors.New("imgcache: invalid PNG filter")
		return
	}
	// PNG filters: https://www.w3.org/TR/png-3/#9Filters . Arithmetic wraps
	// modulo 256; Paeth's tie order is left, up, then upper-left.
	for i := range row {
		var left, upperLeft byte
		if i >= s.bpp {
			left, upperLeft = row[i-s.bpp], prev[i-s.bpp]
		}
		up := prev[i]
		switch filter {
		case 1:
			row[i] += left
		case 2:
			row[i] += up
		case 3:
			row[i] += byte((int(left) + int(up)) / 2)
		case 4:
			p := int(left) + int(up) - int(upperLeft)
			a, b, c := pngAbs(p-int(left)), pngAbs(p-int(up)), pngAbs(p-int(upperLeft))
			switch {
			case a <= b && a <= c:
				row[i] += left
			case b <= c:
				row[i] += up
			default:
				row[i] += upperLeft
			}
		}
	}
}

func pngAbs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
