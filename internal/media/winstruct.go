// 两个「按字节交给系统」的结构体：`THUMBBUTTON` 和 `BITMAPINFOHEADER`。
//
// **不带平台标签**，因为它们要防的那件事和平台无关：**Go 这边的尺寸和字段偏移
// 必须和 C 那边的结构体一模一样**，而这件事是能断的（见 winstruct_test.go）——
//
//	尺寸差一个字节 → 系统按它的尺寸去读我们的数组，于是读到的每一个按钮都是
//	别人的几个字节拼起来的。症状是「任务栏缩略图上有四个按钮，但图标是乱的、
//	按下去什么都不对」，而 `ThumbBarAddButtons` **照样返回 S_OK**。
//
// 同样的道理，`CreateDIBSection` 拿到一个尺寸不对的 `BITMAPINFOHEADER` 不会
// 报错，它只是按错的行数去读，于是图标变成一条斜的灰带。
//
// **形状和常量**都抄的是 `shobjidl.idl`（→ `shobjidl_core.h`）和 `commctrl.h`
// 那两份：**改字段顺序、改字段宽度、改任何一个常量之前先看那几条测试**——
//
//	结构体少一字节 → 系统按它自己的尺寸去读我们的数组（越界）
//	常量错一位    → 那一位是**别人的语义**（4 位色 / 图标 / 不画边框），
//	                而症状全是「某个键没反应」
//
// 之所以这些东西也放进不带平台标签的文件，就是为了让上面那两条在 macOS 上
// 断得动。

package media

// thumbbutton 是 Win32 的 `THUMBBUTTON`（shobjidl.idl，由 shobjidl_core.h 生成）。
//
// ⚠️ **`szTip` 是内联数组 `WCHAR[260]`，不是指针。** 这一点很要命：
//
//	写成指针 → 整个结构体 40 字节；那边是 552 字节。
//	系统按 3 × 552 去读我们那个 120 字节的切片 → **越界读 1536 字节**，
//	把相邻的堆当宽字符去扫、当 dwFlags 去用。那是 UB，不只是图标乱。
//
// 类型抄的是 mingw-w64 那份从 `Shobjidl.idl` 生成的声明（它自己是从微软的
// idl 生成的），原文：
//
//	typedef struct THUMBBUTTON {
//	  THUMBBUTTONMASK dwMask; UINT iId; UINT iBitmap; HICON hIcon;
//	  WCHAR szTip[260]; THUMBBUTTONFLAGS dwFlags;
//	} THUMBBUTTON,*LPTHUMBBUTTON;
//
// idl 在它外面套了 `#include <pshpack8.h>`——x64 上那就是默认的 8 字节对齐，
// 所以 `HICON` 前面有四字节填充、`dwFlags` 后面也有四字节，整个 552。
// Go 这边 `uintptr` 同样按 8 对齐，填充落在同一处（winstruct_test.go 逐字段断）。
type thumbbutton struct {
	dwMask  uint32
	iID     uint32
	iBitmap uint32
	hIcon   uintptr
	szTip   [260]uint16
	dwFlags uint32
}

// bitmapInfoHeader 是 Win32 的 `BITMAPINFOHEADER`（wingdi.h）。
//
// 它的每个字段都是 4 字节或 2 字节，加起来正好 40 字节，**中间没有填充**，
// 所以 Go 这边和 C 那边的布局是天然一致的（`biPlanes`/`biBitCount` 那两个
// `uint16` 紧挨着，各自 2 字节）。
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// ---- 跟上面这两个结构体配套的常量 ----

// `THUMBBUTTONMASK`（dwMask，shobjidl.idl）。抄的是 mingw-w64 那份 idl。
//
// ⚠️ **没有 `THBM_` 这个前缀**——那一套（`THBM_IMAGELIST` / `THBM_TOOLTIP`…）
// 是照着印象编的，真实的名字是下面这四个。编出来的那一套看着很像，
// 而症状全是「某个键没反应」：
//
//	0x2 是 `THB_ICON`（管 `hIcon`），**不是 tooltip**；`THB_TOOLTIP` 是 0x4。
//	  → 悬停不显示任何文字。
//	`THB_FLAGS`(0x8) 不置的话，`dwMask` 里没标的成员**整个被忽略**，
//	  也就是说 `dwFlags` 写了等于没写 → 没歌的时候三个按钮**完全可按**，
//	  按下去会真的换歌。
const (
	thbBitmap  = 0x00000001
	thbIcon    = 0x00000002
	thbToolTip = 0x00000004
	thbFlags   = 0x00000008
)

// `THUMBBUTTONFLAGS`（dwFlags）。
//
// ⚠️ **`THBF_DISABLED` 是 0x1，不是 0x4**：0x4 是 `THBF_NOBACKGROUND`
// （「不画按钮边框」）。写错的话灭的按钮只是没了边框，照样能按。
//
// 而 `THBF_ENABLED` 是 **0**——所以「能按」就是什么都不写，不能反过来
// 写一个 `THBF_ENABLED`（枚举里确实有这个名字，值 0，写上去也一样，
// 但读的人会以为它和别的旗子一样是个位）。
const (
	thbfEnabled  = 0x00000000
	thbfDisabled = 0x00000001
)

// ilcColor32 是 `ImageList_Create` 的 `ILC_COLOR32`。
//
// ⚠️ 它是 **0x20**。这一组是 4/8/10/18/20 —— 一串挨着的数（4 位、8 位、
// 16 位、24 位、32 位），写错一位就落到相邻的那个上：`0x4` 是 `ILC_COLOR4`
// （**4 位、共 16 色**）。症状是四个图标全被替换成调色板里的前几个颜色，
// 界面上是一排纯色方块。
//
//	ILC_COLOR4  0x00000004    ILC_COLOR8  0x00000008
//	ILC_COLOR16 0x00000010    ILC_COLOR24 0x00000018
//	ILC_COLOR32 0x00000020
const ilcColor32 = 0x00000020

// thbnClicked 是缩略图按钮被按时 `WM_COMMAND` 的通知码（在高位）。
//
// `THBN_CLICKED(id) = MAKEWPARAM(id, THBN_CLICKED)`——所以低位是按钮 ID、
// 高位是这个。**两段都要对**：只看低位的话，同一个窗口上别的 `WM_COMMAND`
// 只要低位撞上就被当成我们的按钮（见 thumbbar.go 里 ID 区间的理由）。
const thbnClicked = 0x1800
