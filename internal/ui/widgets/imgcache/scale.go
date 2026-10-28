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
// 和 `ui.DecodeBitmap` 的区别有三个：
//
//   - 长边超过 `maxStoredSide` 的按比例缩到 `maxStoredSide`（EXIF 要转的那一张
//     也是：先摆正再缩）；
//   - 摆正和缩放都在这里做完，交给 `ui.NewBitmap` 的只是最终的像素；
//   - **不捕获原始字节**（见下面 EXIF 那一支的注释）。
//
// ⚠️ **碰像素必须在造 Bitmap 之前**（`ui.Bitmap` 只能写不能读，
// `mygo/ui/bitmap.go`：没有 GetPixel、没有把像素取出来的那一支）：所以这一格
// 自己解码、自己缩，缩完的 `*image.RGBA` 才交给 `ui.NewBitmap`。
func decodeStored(data []byte) (*ui.Bitmap, error) {
	if len(data) == 0 {
		return nil, errors.New("imgcache: 没有字节")
	}
	// 需要旋转/翻转的：自己解、自己摆正、自己缩。
	//
	// ⚠️ **不能交给 `ui.DecodeBitmap`**（0.7.1 起）：它为了 `ReleasePixels`
	// 能重新解码，会把**整份原始字节**留在 bitmap 上不放（`bitmap.go` 的
	// `decode` 字段是个闭包，捕获了 `data`）。一张封面几百 KB，缓存里几百张
	// 就是几十 MB——而这一支要的只是一张 768 长的图。0.6.4 那份 `DecodeBitmap`
	// 用完就丢，所以这个坑是升级带来的，不是本来就有的。
	//
	// 摆正和缩放都在这里做完，交给 `ui.NewBitmap` 的只是最终的 `*image.RGBA`：
	// 它不捕获任何字节，缓存挤掉它时像素就跟着走了。
	if o := exifOrientation(data); o >= 2 {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return ui.NewBitmap(shrinkToStored(orientImage(img, o))), nil
	}
	if img, handled, err := decodeJPEGStored(data); handled {
		if err != nil {
			return nil, err
		}
		return ui.NewBitmap(img), nil
	}
	// 普通大 PNG 逐行解：只留缩放要用的两行，不建整张原图。
	if img, handled, err := decodePNGStored(data); handled {
		if err != nil {
			return nil, err
		}
		return ui.NewBitmap(img), nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return ui.NewBitmap(shrinkToStored(img)), nil
}

// shrinkToStored 把一张图缩到最长边 `maxStoredSide`（已经够小就原样返回）。
//
// ⚠️ **用 ApproxBiLinear 而不是 CatmullRom**（`internal/ui/covercrop.go` 那一处
// 缩放用的是 CatmullRom）：实测一张 1920×1080 缩到 768×432，CatmullRom 要
// 120~280ms（跟机器负载有关），ApproxBiLinear 只要 10~15ms——**十几倍**。
//
// 画质的差别是量出来的：两者逐通道的平均差 **0.45/255**（0.18%）——看不出来，
// 而这一格每加载一张封面就缩一次（一屏十几张，全在后台 worker 上），十几倍的
// CPU 差就是「封面半秒之后才出来」和「封面立刻出来」的差别。
func shrinkToStored(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return img
	}
	if max(w, h) <= maxStoredSide {
		return img
	}
	// 按比例缩：长边落到 maxStoredSide，另一边按同一个比例走。
	dst := image.NewRGBA(storedBounds(w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

func storedBounds(w, h int) image.Rectangle {
	k := min(1, float64(maxStoredSide)/float64(max(w, h)))
	return image.Rect(0, 0, max(1, int(float64(w)*k+0.5)), max(1, int(float64(h)*k+0.5)))
}

// decodeOriented 解一张图，并按 EXIF 摆正。
//
// # 为什么和 `decodeStored` 分成两个
//
// `decodeStored` 给的是**存进缓存的那一份**：缩到最长边 768，而且 EXIF 要转的
// 那些**不缩**（`ui.Bitmap` 的像素读不回来，转了就没法再缩，见那个函数的注释）。
// 这一条给的是**原图**：不缩，但摆正。
//
// # 摆正的规则和框架一致
//
// 映射照 `mygo/ui/bitmap.go` 的 `orient`：2 水平翻转、3 转 180°、4 垂直翻转、
// 5/7 转置、6 顺时针 90°、8 逆时针 90°。**只对 JPEG 摆**（EXIF 在 JPEG 里），
// 其余格式原样。
func decodeOriented(data []byte) (image.Image, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if format != "jpeg" {
		return img, nil
	}
	o := exifOrientation(data)
	if o <= 1 {
		return img, nil
	}
	return orientImage(img, o), nil
}

// orientImage 按 EXIF 方向 o 转 / 翻一张图。
//
// 映射照 `mygo/ui/bitmap.go` 的 `orient`（那一处是像素级的，这一处是 `image.Image`
// 级的，规则一样）。
func orientImage(img image.Image, o int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return img
	}
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			default:
				sx, sy = x, y
			}
			dst.Set(x, y, img.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}

// exifOrientation 报一张 JPEG 的 EXIF 方向（1~8），没有就报 0。
//
// 解析照 `mygo/ui/bitmap.go` 的 `exifOrientation`：找 APP1 里的 Exif 段，
// 再读 TIFF 第一个 IFD 的 Orientation 标签。
func exifOrientation(jpg []byte) int {
	if len(jpg) < 4 || jpg[0] != 0xff || jpg[1] != 0xd8 {
		return 0
	}
	for i := 2; i+4 <= len(jpg); {
		if jpg[i] != 0xff {
			return 0
		}
		marker := jpg[i+1]
		size := int(binary.BigEndian.Uint16(jpg[i+2:]))
		if marker == 0xda || size < 2 || i+2+size > len(jpg) {
			return 0
		}
		if marker == 0xe1 && size >= 8 && string(jpg[i+4:i+10]) == "Exif\x00\x00" {
			return tiffOrientation(jpg[i+10 : i+2+size])
		}
		i += 2 + size
	}
	return 0
}

// tiffOrientation 读 TIFF 第一个 IFD 里的 Orientation 标签（1~8）。
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	ifd := int64(bo.Uint32(t[4:8]))
	if ifd < 8 || ifd+2 > int64(len(t)) {
		return 0
	}
	n := int(bo.Uint16(t[ifd : ifd+2]))
	for e := int(ifd) + 2; n > 0 && e+12 <= len(t); e, n = e+12, n-1 {
		if bo.Uint16(t[e:e+2]) == 0x0112 && bo.Uint16(t[e+2:e+4]) == 3 {
			o := int(bo.Uint16(t[e+8 : e+10]))
			if o >= 1 && o <= 8 {
				return o
			}
			return 0
		}
	}
	return 0
}

// hasExifOrientation reports an actual rotation/flip (EXIF 2..8), not merely
// the presence of metadata. Parsing follows MyGo's bitmap.go orientation rules;
// malformed or absent tags mean no transform, just as in ui.DecodeBitmap.
func hasExifOrientation(data []byte) bool {
	return exifOrientation(data) >= 2
}

func rotatedTIFF(t []byte) bool {
	if len(t) < 8 {
		return false
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return false
	}
	ifd := int64(bo.Uint32(t[4:8]))
	if ifd < 8 || ifd+2 > int64(len(t)) {
		return false
	}
	n := int(bo.Uint16(t[ifd : ifd+2]))
	for e := int(ifd) + 2; n > 0 && e+12 <= len(t); e, n = e+12, n-1 {
		if bo.Uint16(t[e:e+2]) == 0x0112 && bo.Uint16(t[e+2:e+4]) == 3 {
			o := bo.Uint16(t[e+8 : e+10])
			return o >= 2 && o <= 8
		}
	}
	return false
}
