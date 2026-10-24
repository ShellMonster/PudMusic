package imgcache

// 存进缓存的那一张图**有多大**。
//
// # 为什么这一格是「首页封面反复闪」的根因之一（issue #208）
//
// B 站的封面是 1920×1080 的原图（`service/bili` 那几个接口给的 `pic` 就是它，
// 没有带任何缩放参数）。解码之后是 RGBA 加 mipmap 链，一张 **11MB**。
// 而界面上最大的那一格是满屏播放器那张封面——1200×800 的窗口上 384 DIP，
// 也就是 768 个物理像素。
//
// 一屏十几张封面按原图存就是 **170MB**，而缓存的字节上限是几十 MB。
// 于是 LRU 只能把**正在显示着的**封面挤掉——挤掉的那一张下一帧就退回占位，
// 重新下一次、再解一次码；解完又挤掉别人……界面上就是「有图 ↔ 占位」反复
// 切换（实测：16 张封面被下了 7000 多次）。
//
// 所以这里把**存下来的那一份**缩到最长边 `maxStoredSide` 像素。缩的是缓存里
// 的副本，磁盘上那一份还是原图（下次启动读盘时缩同一遍，缩放是纯 CPU 的，
// 在后台的解码 worker 上做，不碰主线程）。
//
// # 为什么是 768 而不是「界面上最大的那个数」
//
// 界面最大的那一格是满屏播放器：1200×800 的窗口上是 384 DIP，768 = 384×2，
// 一台 Retina 屏上刚好 1:1（不糊）。窗口再大它最多到 520 DIP（1040 物理
// 像素），那一档从 768 放上去是 1.35 倍——**糊一点点**，换来的是内存账从
// 170MB 变成 25MB。这一档的取舍写在这里，别的地方看到「768」别以为是随手
// 填的数。
//
// 列表行（48 DIP）、网格卡片（约 250 DIP）、收藏夹头图（200×168）那一档
// 更小，768 对它们是绰绰有余——那些格子本来也用不着那么多像素，缩小的
// 时候是 GPU 用 mipmap 干的（见 `mygo/ui/bitmap.go` 的 `smaller`）。
//
// # 缩放用 ApproxBiLinear
//
// 那一处缩放（裁收藏夹封面）用的是 CatmullRom，**这里不用**：这一格每加载
// 一张封面就缩一次（一屏十几张），而 CatmullRom 缩一张 1920×1080 要
// 120~280ms，ApproxBiLinear 只要 10~15ms——十几倍。两者逐通道的平均差是
// 0.45/255（0.18%），看不出来。

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"  // 注册 GIF 解码器
	_ "image/jpeg" // 注册 JPEG 解码器
	_ "image/png"  // 注册 PNG 解码器

	"github.com/egoist/mygo/ui"
	_ "golang.org/x/image/bmp" // 注册 BMP 解码器
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // 注册 WebP 解码器
)

// maxStoredSide 是**存进内存缓存的那一份**的最长边有多少像素。
//
// ⚠️ 它和 `DefaultMaxBytes` 是一对：那个数是「多少字节」，这个数是「一张图
// 多大」。改这一个要顺手算一下另一个（见 cache.go 里那两个常量的注释）。
const maxStoredSide = 768

// decodeStored 解一张网络图，得到**存进缓存的那一份**。
//
// 和 `ui.DecodeBitmap` 的区别只有两个：
//
//   - 长边超过 `maxStoredSide` 的按比例缩到 `maxStoredSide`；
//   - 带 EXIF 方向信息的那一张**不缩**（见 hasExifOrientation）。
//
// ⚠️ **碰像素必须在造 Bitmap 之前**（`ui.Bitmap` 只能写不能读，
// `mygo/ui/bitmap.go`：没有 GetPixel、没有把像素取出来的那一支）：所以这一格
// 自己解码、自己缩，缩完的 `*image.RGBA` 才交给 `ui.NewBitmap`。
func decodeStored(data []byte) (*ui.Bitmap, error) {
	if len(data) == 0 {
		return nil, errors.New("imgcache: 没有字节")
	}
	// 带 EXIF 方向的走框架那一支：它会把方向摆正，而缩放这一支不认识方向，
	// 转错了就是一张躺着的封面。B 站的图床上没有带方向的图，这一支是兜底。
	if hasExifOrientation(data) {
		return ui.DecodeBitmap(data)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("imgcache: 这张图没有像素")
	}
	if max(w, h) <= maxStoredSide {
		return ui.NewBitmap(img), nil
	}

	// 按比例缩：长边落到 maxStoredSide，另一边按同一个比例走。
	k := float64(maxStoredSide) / float64(max(w, h))
	dw := max(1, int(float64(w)*k+0.5))
	dh := max(1, int(float64(h)*k+0.5))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	// ⚠️ **用 ApproxBiLinear 而不是 CatmullRom**（`internal/ui/covercrop.go` 那一处
	// 缩放用的是 CatmullRom）：实测一张 1920×1080 缩到 768×432，CatmullRom 要
	// 120~280ms（跟机器负载有关），ApproxBiLinear 只要 10~15ms——**十几倍**。
	//
	// 画质的差别是量出来的：两者逐通道的平均差 **0.45/255**（0.18%）——看不出来，
	// 而这一格每加载一张封面就缩一次（一屏十几张，全在后台 worker 上），十几倍的
	// CPU 差就是「封面半秒之后才出来」和「封面立刻出来」的差别。
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return ui.NewBitmap(dst), nil
}

// hasExifOrientation 报这张 JPEG 有没有 EXIF 方向信息。
//
// 扫的是 JPEG 的段结构：APP1（0xE1）那一段的头六个字节是 "Exif\x00\x00"。
// 找到就说明**可能**有方向信息——这里不读 TIFF 里的 Orientation 标签，
// 宁可多判一张（多判的后果只是「这一张不缩」），也不肯把一张躺着的封面
// 缩成一张躺着的封面。
//
// 这一段扫描抄的是 `mygo/ui/bitmap.go` 的 `exifOrientation`（那里读得更细，
// 要 TIFF 的那个标签）；抄它而不是自己另写一套，是因为 JPEG 的段结构就那
// 么几行，抄一遍比重新发明一份可靠。
func hasExifOrientation(data []byte) bool {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return false // 不是 JPEG
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return false
		}
		marker := data[i+1]
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xda || size < 2 || i+2+size > len(data) {
			return false // 图像数据开始，或者这段坏了
		}
		if marker == 0xe1 && size >= 8 && string(data[i+4:i+10]) == "Exif\x00\x00" {
			return true
		}
		i += 2 + size
	}
	return false
}
