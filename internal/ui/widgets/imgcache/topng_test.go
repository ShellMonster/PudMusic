package imgcache

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func solid() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{200, 30, 30, 255})
		}
	}
	return img
}

func TestToPNGConvertsJPEGAndKeepsPNGAsIs(t *testing.T) {
	var j, p bytes.Buffer
	if err := jpeg.Encode(&j, solid(), nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&p, solid()); err != nil {
		t.Fatal(err)
	}
	out, err := toPNG(j.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(out)); err != nil || cfg.Width != 4 || cfg.Height != 3 {
		t.Errorf("JPEG 转出来的不是 4x3 的 PNG：%v %+v", err, cfg)
	}
	same, err := toPNG(p.Bytes())
	if err != nil || !bytes.Equal(same, p.Bytes()) {
		t.Errorf("PNG 应原样返回：%v", err)
	}
	if _, err := toPNG([]byte("not an image")); err == nil {
		t.Error("坏字节应当报错，不能写出空图或坏图")
	}
}
