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
//
// ## ⚠️ 这一组曾经是**假绿**的
//
// 第一版的 `TestThumbbuttonLayout` 断言「THUMBBUTTON 是 40 字节、`szTip`
// 是指针在第 24 字节」——而**代码里当时正好就是这么写的**，所以它是绿的。
// 真相是那边 `szTip` 是内联数组 `WCHAR[260]`，整个结构体 552 字节。
//
// 错成那样时系统按 3 × 552 去读一个 120 字节的切片：**越界读 1536 字节**。
//
// **教训**：把布局测试的期望值抄成实现的样子，等于没测。期望值必须从
// **另一个来源**抄过来——这一版的每个数字下面都写着它是从哪份文件哪一行
// 抄的，抄错的时候要去对着那份文件看，而不是对着自己的代码看。

// 抄来源：mingw-w64 从 `Shobjidl.idl` 生成的 `shobjidl_core.h`，
// `mingw-w64-headers/include/shobjidl.idl` 第 2099-2108 行原文：
//
//	#include <pshpack8.h>
//	typedef struct THUMBBUTTON {
//	  THUMBBUTTONMASK dwMask;   // 4
//	  UINT iId;                  // 4
//	  UINT iBitmap;              // 4
//	  HICON hIcon;               // 8，x64 上前面补 4（pshpack8 → 8 字节对齐）
//	  WCHAR szTip[260];          // 520，内联数组，不是指针
//	  THUMBBUTTONFLAGS dwFlags;  // 4，末尾补 4 到 8 的倍数
//	} THUMBBUTTON,*LPTHUMBBUTTON;
const (
	thumbbuttonWantSize  = 552
	thumbbuttonWantFlags = 544
	thumbbuttonWantTip   = 24
	thumbbuttonWantTipN  = 260
)

// TestThumbbuttonLayout 断 `THUMBBUTTON` 的布局。
//
// **在 macOS/arm64 上断同样算数**：两个平台都是 LP64，`uintptr` 都 8 字节、
// 都按 8 对齐，Go 的字段对齐规则和 C 完全一致（那个 `#pshpack8` 在 x64 上
// 就是 MSVC 的默认值）。
func TestThumbbuttonLayout(t *testing.T) {
	if got := unsafe.Sizeof(thumbbutton{}); got != thumbbuttonWantSize {
		t.Errorf("THUMBBUTTON 是 %d 字节，Windows 那边是 %d（szTip 是内联数组"+
			"WCHAR[260]，不是指针——写成指针这里就是 40）",
			got, thumbbuttonWantSize)
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
		// ⚠️ 这一格就是那个曾经要命的：`szTip` 从这里开始、占 520 字节。
		{"szTip", unsafe.Offsetof(thumbbutton{}.szTip), thumbbuttonWantTip},
		{"dwFlags", unsafe.Offsetof(thumbbutton{}.dwFlags), thumbbuttonWantFlags},
	} {
		if c.got != c.want {
			t.Errorf("THUMBBUTTON.%s 在第 %d 字节，Windows 那边是第 %d",
				c.name, c.got, c.want)
		}
	}
}

// TestThumbbuttonTipIsAnInlineArray 单独再断一次 `szTip` 是数组不是指针。
//
// 上面的 `TestThumbbuttonLayout` 已经能靠尺寸抓到它；这一条是为了**失败时
// 指得准**——尺寸对但类型错（比如写成 `*uint16`）的话，上一条只会说
// 「552 vs 某数」，而这一条会说「szTip 必须是内联的 260 个 WCHAR」。
func TestThumbbuttonTipIsAnInlineArray(t *testing.T) {
	var b thumbbutton
	var arr [thumbbuttonWantTipN]uint16
	if unsafe.Sizeof(b.szTip) != unsafe.Sizeof(arr) {
		t.Errorf("szTip 是 %d 字节，WCHAR[260] 是 %d 字节",
			unsafe.Sizeof(b.szTip), unsafe.Sizeof(arr))
	}
	if got := unsafe.Sizeof(b.dwFlags); got != 4 {
		t.Errorf("dwFlags 是 %d 字节，UINT 是 4", got)
	}
}

// 抄来源：mingw-w64 `mingw-w64-headers/include/commctrl.h` 里的
// `BITMAPINFOHEADER`（第 500-512 行），40 字节，**中间一块填充都没有**——
// 它每一个字段都是 4 字节或 2 字节，两个 `uint16` 紧挨着也正好对齐。
// 这是它和 `THUMBBUTTON` 最大的不同，所以两条得分开断。
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

// TestThumbbarFlagsUseTheRealNames 断 `dwMask` / `dwFlags` 那两套常量的取值。
//
// ⚠️ **这一条也是补上去的**：原来的 `THBM_IMAGE_LIST = 1 / THBM_TOOLTIP = 2 /
// THBM_DISABLED = 4` 是照着印象编的，真实的名字和值是：
//
//	THUMBBUTTONMASK： THB_BITMAP 1  THB_ICON 2  THB_TOOLTIP 4  THB_FLAGS 8
//	THUMBBUTTONFLAGS：THBF_ENABLED 0 THBF_DISABLED 1 THBF_NOBACKGROUND 4 …
//
// 编出来的那一套**每一项都撞在别人的语义上**，而症状全是「某个键没反应」：
// 悬停没有文字（0x2 其实是 THB_ICON）、`dwFlags` 整个被忽略（0x8 没置）
// 于是没歌的时候按钮按得动、`THBF_DISABLED` 写成了 0x4 而那是「不画边框」。
//
// 这些常量在 `_windows.go` 里，所以断不到「用它们的函数」；这里把取值本身
// 钉住，并写清楚每一个撞上了谁。
func TestThumbbarFlagsUseTheRealNames(t *testing.T) {
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
		note string
	}{
		{"THB_BITMAP", thbBitmap, 1, "THBM_IMAGE_LIST 是编出来的"},
		{"THB_ICON", thbIcon, 2, "照印象当成 THBM_TOOLTIP 就撞在这儿了"},
		{"THB_TOOLTIP", thbToolTip, 4, ""},
		{"THB_FLAGS", thbFlags, 8, "不置它的话 dwFlags 整个被忽略"},
		{"THBF_ENABLED", thbfEnabled, 0, ""},
		{"THBF_DISABLED", thbfDisabled, 1, "写 0x4 的话那是 THBF_NOBACKGROUND"},
	} {
		if c.got != c.want {
			t.Errorf("%s 是 %#x，那边是 %#x%s", c.name, c.got, c.want, c.note)
		}
	}
}

// TestILCColor32UsesTheRealFlag 断 `ILC_COLOR32` 的取值。
//
// 这一组是 4/8/10/18/20 一串挨着的数（4 位、8 位、16 位、24 位、32 位），
// 写错一位就落到相邻的那个上。写成 0x4 的话就是 `ILC_COLOR4`——**4 位共 16
// 色**——于是四个图标全被替换成调色板里的前几个颜色，界面上是一排纯色方块。
func TestILCColor32UsesTheRealFlag(t *testing.T) {
	if ilcColor32 != 0x20 {
		t.Errorf("ILC_COLOR32 是 %#x，那边是 0x20（0x4 是 ILC_COLOR4，只有 16 色）", ilcColor32)
	}
}

// TestTHBNClickedIsTheHighWord 断「按钮被按」那道通知码。
//
// `THBN_CLICKED(id) = MAKEWPARAM(id, THBN_CLICKED)`：低位是按钮 ID、高位是
// 这个通知码。**两段都要对**——只看低位的话，同一个窗口上别的 `WM_COMMAND`
// 只要低位撞上就被当成我们的按钮，而那一下会真的切歌。
func TestTHBNClickedIsTheHighWord(t *testing.T) {
	if thbnClicked != 0x1800 {
		t.Errorf("THBN_CLICKED 是 %#x，那边是 0x1800", thbnClicked)
	}
	// 我们那三个命令 ID（0x0200 起）必须和通知码不撞 —— 它们在不同的半边，
	// 但这条钉住「ID 没有大到窜进通知码那一段」。
	if thumbbarFirstID >= uint32(thbnClicked) {
		t.Errorf("命令 ID %#x 窜进了通知码 %#x 那一段", thumbbarFirstID, thbnClicked)
	}
}
