package platform

import (
	"bytes"
	"image/png"
	"runtime"
	"testing"
)

// TestTraySupportedSkipsMacOS 钉住「macOS 上没有托盘」。
//
// 这是与 Biu 一致的地方（`biu/electron/main.ts` 把 createTray 整个包在
// `process.platform !== "darwin"` 里），也是这个平台「隐藏之后怎么回来」的唯一
// 答案：收进 Dock，点图标，不是托盘。谁哪天在 macOS 上顺手建了个托盘，这条会红。
func TestTraySupportedSkipsMacOS(t *testing.T) {
	if got, want := TraySupported(), runtime.GOOS != "darwin"; got != want {
		t.Errorf("TraySupported() = %v，want %v（darwin 上不该有托盘）", got, want)
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
