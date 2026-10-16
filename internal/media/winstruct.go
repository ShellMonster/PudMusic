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
// 形状抄的是 `shobjidl_core.h` 的 `THUMBBUTTON` 和 `wingdi.h` 的
// `BITMAPINFOHEADER`。**改字段顺序或改字段宽度之前先看那两条测试。**

package media

// thumbbutton 是 Win32 的 `THUMBBUTTON`（shobjidl_core.h）。
//
// 字段顺序和类型与那个结构体一致，**不能改顺序也不能改宽度**。
// 64 位上它中间和末尾各有四字节对齐填充，那是 C 那边 `HICON` 8 字节对齐
// 自动加上的 —— Go 这边 `uintptr` 同样要 8 对齐，所以填充的位置一样。
type thumbbutton struct {
	dwMask  uint32
	iID     uint32
	iBitmap uint32
	hIcon   uintptr
	szTip   uintptr
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
