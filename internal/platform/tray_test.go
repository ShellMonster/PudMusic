package platform

import (
	"bytes"
	"image/png"
	"runtime"
	"testing"
)

// TestEveryPlatformHasATrayIcon 钉住三个平台都有托盘 / 菜单栏图标。
func TestEveryPlatformHasATrayIcon(t *testing.T) {
	if !TraySupported() {
		t.Error("TraySupported() = false，三个平台都该有托盘 / 菜单栏图标")
	}
	b, tmpl := TrayIcon()
	if len(b) == 0 || tmpl != (runtime.GOOS == "darwin") {
		t.Errorf("TrayIcon() = %d 字节，模板=%v", len(b), tmpl)
	}
}

// TestTheTemplateIconIsBlackAndTransparent 验模板图只有黑和透明：系统按菜单栏
// 的明暗给它着色，带颜色的像素会被当成形状的一部分染掉。
func TestTheTemplateIconIsBlackAndTransparent(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(TrayTemplatePNG()))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 36 || b.Dy() != 36 {
		t.Errorf("模板图是 %dx%d，want 36x36（菜单栏 18pt 的 Retina 尺寸）", b.Dx(), b.Dy())
	}
	opaque := 0
	for y := 0; y < 36; y++ {
		for x := 0; x < 36; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a > 0 {
				opaque++
				if r|g|bl != 0 {
					t.Fatalf("(%d,%d) 不是黑色：%v", x, y, img.At(x, y))
				}
			}
		}
	}
	if opaque < 80 || opaque > 36*36/2 {
		t.Errorf("不透明像素 %d 个，图形不对（太空或太满）", opaque)
	}
}

// TestTrayIconIsAValidPNG 验现画的那张图标真的能解码。
//
// 托盘图标是一次性的：建图标失败不会崩，只会在通知区域留一个空位，所以这类
// 坏法在真机上极难看出来（用户看到「没有图标」，不会想到是 PNG 坏了）。这里
// 把它钉住。
func TestTrayIconIsAValidPNG(t *testing.T) {
	b := TrayIconPNG()
	if len(b) == 0 {
		t.Fatal("托盘图标是空的")
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("托盘图标不是合法的 PNG：%v", err)
	}
	// 32×32 是菜单栏 16pt 的 Retina 尺寸；Biu 的托盘图标也是 16pt。
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
		t.Errorf("托盘图标是 %dx%d，want 32x32", b.Dx(), b.Dy())
	}
	// 至少要有一个不透明的像素，不然画出来是全空的（编码对了但画错了）。
	var opaque int
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				opaque++
			}
		}
	}
	if opaque == 0 {
		t.Error("托盘图标一个不透明的像素都没有，画出来是全空的")
	}
}
