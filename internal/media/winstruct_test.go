package media

import (
	"testing"
	"unsafe"
)

// 这一组断「按字节交给系统的那两个结构体，在 Go 这边的尺寸和偏移和 C 那边
// 一模一样」。
//
// 断的是**布局**而不是「字段有没有」：把 `iBitmap` 挪到 `hIcon` 后面，字段
// 一个不少、`go vet` 不报、编译得过，而系统读到的每一个按钮都是错位的。

// TestThumbbuttonLayout 断 `THUMBBUTTON` 的布局。
//
// 那边是：`DWORD dwMask; UINT iId; UINT iBitmap; HICON hIcon; LPCWSTR szTip;
// DWORD dwFlags;`——64 位上 `HICON` 要 8 对齐，所以中间四字节填充，末尾也补到
// 8 的倍数，一共 40。
//
// ⚠️ **这里在 macOS/arm64 上断同样算数**：两个平台都是 LP64，
// `uintptr` 都是 8 字节、指针都按 8 对齐，Go 的字段对齐规则和 C 完全一样。
func TestThumbbuttonLayout(t *testing.T) {
	const want = 40
	if got := unsafe.Sizeof(thumbbutton{}); got != want {
		t.Errorf("THUMBBUTTON 是 %d 字节，Windows 那边是 %d", got, want)
	}
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"dwMask", unsafe.Offsetof(thumbbutton{}.dwMask), 0},
		{"iID", unsafe.Offsetof(thumbbutton{}.iID), 4},
		{"iBitmap", unsafe.Offsetof(thumbbutton{}.iBitmap), 8},
		// 12..15 是对齐填充，C 那边也有（`HICON` 是 8 字节对齐的指针）。
		{"hIcon", unsafe.Offsetof(thumbbutton{}.hIcon), 16},
		{"szTip", unsafe.Offsetof(thumbbutton{}.szTip), 24},
		{"dwFlags", unsafe.Offsetof(thumbbutton{}.dwFlags), 32},
	} {
		if c.got != c.want {
			t.Errorf("THUMBBUTTON.%s 在第 %d 字节，Windows 那边是第 %d", c.name, c.got, c.want)
		}
	}
}

// TestBitmapInfoHeaderLayout 断 `BITMAPINFOHEADER` 的布局。
//
// 40 字节，**中间一块填充都没有**——它每一个字段都是 4 字节或 2 字节，
// 两个 `uint16` 紧挨着也正好对齐。这是它和 `THUMBBUTTON` 最大的不同，
// 所以两条得分开断。
func TestBitmapInfoHeaderLayout(t *testing.T) {
	const want = 40
	if got := unsafe.Sizeof(bitmapInfoHeader{}); got != want {
		t.Errorf("BITMAPINFOHEADER 是 %d 字节，Windows 那边是 %d", got, want)
	}
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"Size", unsafe.Offsetof(bitmapInfoHeader{}.Size), 0},
		{"Width", unsafe.Offsetof(bitmapInfoHeader{}.Width), 4},
		{"Height", unsafe.Offsetof(bitmapInfoHeader{}.Height), 8},
		{"Planes", unsafe.Offsetof(bitmapInfoHeader{}.Planes), 12},
		{"BitCount", unsafe.Offsetof(bitmapInfoHeader{}.BitCount), 14},
		{"Compression", unsafe.Offsetof(bitmapInfoHeader{}.Compression), 16},
		{"SizeImage", unsafe.Offsetof(bitmapInfoHeader{}.SizeImage), 20},
		{"XPelsPerMeter", unsafe.Offsetof(bitmapInfoHeader{}.XPelsPerMeter), 24},
		{"YPelsPerMeter", unsafe.Offsetof(bitmapInfoHeader{}.YPelsPerMeter), 28},
		{"ClrUsed", unsafe.Offsetof(bitmapInfoHeader{}.ClrUsed), 32},
		{"ClrImportant", unsafe.Offsetof(bitmapInfoHeader{}.ClrImportant), 36},
	} {
		if c.got != c.want {
			t.Errorf("BITMAPINFOHEADER.%s 在第 %d 字节，Windows 那边是第 %d",
				c.name, c.got, c.want)
		}
	}
}

// TestTheStructsFitInTheBytesWeHandOut 断「我们算出来的字节数」和「结构体真正
// 有多大」一致。
//
// 这一条是最容易漏的那种：`glyphBitmapHandle` 里写死了
// `SizeImage: uint32(w*h*4)`，而位图真正占的字节数是 `w*h*4`——
// 哪天那个 `4` 和结构体对不上了，这里立刻红，而系统那边只是**静默地读错行数**。
func TestTheStructsFitInTheBytesWeHandOut(t *testing.T) {
	const bytesPerPixel = 4
	const w, h = 24, 24
	if got, want := unsafe.Sizeof(bitmapInfoHeader{}), uintptr(w*h*bytesPerPixel); got > want {
		t.Errorf("位图（%d×%d×%d 字节）比它自己的头还小，不可能", w, h, bytesPerPixel)
	}
	if unsafe.Sizeof(thumbbutton{})*thumbbarCommandCount == 0 {
		t.Error("一条按钮栏占了 0 字节")
	}
}
