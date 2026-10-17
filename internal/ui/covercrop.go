// 封面裁剪的**纯逻辑**那一半：几何和像素。
//
// 这个文件里没有一个 view、没有一个 Shell、没有一次网络请求——全是能直接
// 调的函数。原因不是洁癖：裁剪算错了的**症状在界面上完全看不出来**
//（框画在那儿、点上传也成功了、只是传上去的图不对），所以它只能靠单测钉，
// 而单测要能钉就必须是纯函数。
//
// # 🔴 一条硬规矩：碰像素必须在造 Bitmap 之前
//
// `ui.Bitmap` **只能写不能读**（`mygo/ui/bitmap.go`：没有 GetPixel、没有
// Pix 取出来的那一支）。所以「裁剪」「缩放」「编 JPEG」全在这一层用 Go 的
// `image` 包做完，做完的 `*image.RGBA` 才交给 `ui.NewBitmap` 当预览。
//
// 反过来做（先建 Bitmap 再想裁）就只剩一条路：整张图建一个 Bitmap，然后
// 让界面缩着画——那样画出来的框和传上去的图就**对不上**，而且没有任何报错。
//
// # 裁剪框记的是百分比，不是像素
//
// 记百分比是为了窗口缩放、图缩放之后不用重算（框跟着图走）。四个数是
// 左、上、宽、高，都占原图的百分比。

package ui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // 注册 PNG 解码器，见 decodeCover
	"math"

	xdraw "golang.org/x/image/draw" // 缩放用的 CatmullRom，见 cropToJPEG
)

// 裁剪台的几何。
//
// ⚠️ **台子是固定 16:9 的 320×180**，和 Biu 上游那个 322×202 不是一回事：
// 上游那个是 16:10 的容器、图在里面 `object-contain`，裁剪框另有比例。照抄它
// 会让「框看起来是 16:9」这件事变成假的——框是 16:9 但台子不是，用户拖到的
// 「16:9」在像素上从来就不是 16:9。
const (
	cropStageW = 320
	cropStageH = 180
)

// cropAspect 是裁剪框固定的比例。
//
// 🔴 **B 站的收藏夹封面就是 16:9 的**（头图区那边画的封面 200×168 那个是
// 另外一回事，见 `collectionpage.go`）。所以这里是「固定」，不是「可调」。
const cropAspect = 16.0 / 9.0

// DefaultCrop 是裁剪框打开时的初始位置：10/10/80/80（百分比）。
//
// 照抄 Biu（`components/image-upload/crop-modal.tsx:28-34` 的
// `setCrop({unit:"%", x:10, y:10, width:80, height:80})`）。
//
// ⚠️ 对一张**不是 16:9** 的图，这四个数过一遍 `fitCrop` 之后 H 会变小
// （见那个函数的注释）。16:9 的图上它原样成立——这一点有测试钉着。
var DefaultCrop = cropBox{X: 10, Y: 10, W: 80, H: 80}

// cropBox 是裁剪框，占原图的百分比。
type cropBox struct{ X, Y, W, H float32 }

// coverAspect 是这张图的宽高比。
func coverAspect(iw, ih float32) float32 {
	if iw <= 0 || ih <= 0 {
		return cropAspect
	}
	return iw / ih
}

// fitImage 把 iw×ih 的图**等比缩放居中**放进裁剪台，返回它在台子里的位置
// 和大小（台子坐标）。
//
// 台子是 16:9，图不一定是——所以两边都有黑边（`ui.Contain` 那个位置）。
func fitImage(iw, ih float32) (x, y, w, h float32) {
	if iw <= 0 || ih <= 0 {
		return 0, 0, cropStageW, cropStageH
	}
	s := min(cropStageW/iw, cropStageH/ih)
	w, h = iw*s, ih*s
	return (cropStageW - w) / 2, (cropStageH - h) / 2, w, h
}

// fitCrop 把用户记的那四个百分数**掰成一个真的是 16:9、而且整个在图里**的框。
//
// 以左上角为准往下算：宽度取 W，高由 16:9 推出来（`h = w/iw*ih*9/16`）。
//
// ⚠️ **对一张不是 16:9 的图，H 会跟用户记的不一样**——这是故意的，也是唯一
// 能同时满足「固定 16:9」和「左上角不动」的做法。Biu 的 `react-image-crop`
// 在 `aspect` 上做的是同一件事（初始那个 80×80 会被它自己按比例改掉）。
// 反过来（保住 H、按 W 推）会让**宽度**溢出到图的右边，那才是真的错。
//
// 越界时先把左上角拉回图里，再把宽高夹到图里。
func fitCrop(b cropBox, iw, ih float32) cropBox {
	w, h := cropWH(b.W, iw, ih)
	return cropBox{X: clampCropPct(b.X, 0, 100-w), Y: clampCropPct(b.Y, 0, 100-h), W: w, H: h}
}

// minCropPct 是裁剪框最小多宽（百分比）。太小了拖起来手指会挡住整张图。
const minCropPct = 10

// cropWH 由**宽度**推出这一组裁剪框该有的宽高（百分比）。
//
// ⚠️ 只认宽度、不认高度：16:9 是固定比例，一个自由度就够。高度是推出来的，
// 而推出来之后要夹上界——一张极端细长的图上，0.8 的宽推出来的高度会超过整
// 张图，这时候夹到 100 会让实际比例不是 16:9。那种图（宽高比超过 16:9 * 5）
// 传上来本来就没什么可裁的，比例差一点也不影响用户看。
func cropWH(wPercent, iw, ih float32) (float32, float32) {
	w := clampCropPct(wPercent, minCropPct, 100)
	h := min(w*coverAspect(iw, ih)/cropAspect, 100)
	return w, h
}

// clampCropPct 把 v 夹进 [lo, hi]。hi 比 lo 还小的时候返回 hi（宁可最小也不能反）。
func clampCropPct(v, lo, hi float32) float32 {
	if hi < lo {
		return hi
	}
	return min(max(v, lo), hi)
}

// stageRect 把裁剪框翻成台子坐标（用来画、用来判命中）。
func stageRect(b cropBox, iw, ih float32) (x, y, w, h float32) {
	fx, fy, fw, fh := fitImage(iw, ih)
	return fx + b.X/100*fw, fy + b.Y/100*fh, b.W / 100 * fw, b.H / 100 * fh
}

// cropHandle 是把手的大小（台子像素）。
//
// 12 是「手指按得中」的下限。给 6 的话触控板上能点中、鼠标要瞄，而瞄不准
// 的结果是「用户拖不动框，只能重新打开弹窗」。
const cropHandle = 12

// cropCorner 是四个角里的一角。
type cropCorner uint8

const (
	cornerTL cropCorner = iota
	cornerTR
	cornerBR
	cornerBL
	// cornerMove 是「按在框里面」——移动而不是缩放。
	cornerMove
	cornerNone
)

// cornerAt 判 (x, y) 落在台子上的哪个位置。
//
// ⚠️ **先判把手再判框里面**：把手压在框的边上，两个区域重叠。不先判把手的话
// 用户想缩框时命中的是「移动」，框会跟着走而不是变大——缩框这个动作在这个
// 弹窗里比移动常用得多，判反了它就基本没法用。
func cornerAt(x, y float32, rx, ry, rw, rh float32) cropCorner {
	h := float32(cropHandle) / 2
	for _, c := range []struct {
		cropCorner
		hx, hy float32
	}{
		{cornerTL, rx, ry},
		{cornerTR, rx + rw, ry},
		{cornerBR, rx + rw, ry + rh},
		{cornerBL, rx, ry + rh},
	} {
		if absF32(x-c.hx) <= h && absF32(y-c.hy) <= h {
			return c.cropCorner
		}
	}
	if x >= rx && x <= rx+rw && y >= ry && y <= ry+rh {
		return cornerMove
	}
	return cornerNone
}

// absF32 是 float32 的绝对值。
//
// ⚠️ **名字里带着 `F32` 不是啰嗦**：`upspace_test.go`（#65 带来的）里也有一个
// `abs32`，同包同名，合并时 Git 看不出来（不同的文件），编译才报——而一个
// 测试文件里声明的辅助函数和一个**生产文件**里声明的撞在一起，编译器报的那
// 行数会指着测试文件，而真因是生产文件里那个。
func absF32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// moveCrop 把框按台子上的位移挪一下。
//
// ⚠️ **位移量要除以缩放系数**：台子上挪 10 个点，在一张 4000 宽的图上是几
// 百个像素。不换算的话在缩过的图上框会「跑得比手指快」，在小图上又几乎
// 挪不动——两种症状都是「拖不动」。
//
// 夹住：整个框必须在图里（不能拖出黑边）。
func moveCrop(start cropBox, dx, dy, iw, ih float32) cropBox {
	_, _, fw, fh := fitImage(iw, ih)
	b := fitCrop(start, iw, ih)
	b.X = clampCropPct(start.X+dx/fw*100, 0, 100-b.W)
	b.Y = clampCropPct(start.Y+dy/fh*100, 0, 100-b.H)
	return fitCrop(b, iw, ih)
}

// resizeCrop 按着 corner 那个角，把框的宽度拉到指针在台子上的位置 x。
//
// ⚠️ **高度完全不管**：16:9 是固定的，一个自由度就够（见 cropWH）。
//
// ⚠️ **台子上量出来的宽高比不是 16:9**：台子是 16:9 而图不一定是，而框按的是
// **原图**上的 16:9。所以这里只取宽度换算成百分比，高度交给 `cropWH` 按图
// 的比例推——那个差值才是原图上正确的 16:9。
//
// **按着的那个角钉住不动**，另外那三个跟着走：从左边两个角拉时左边不动，
// 从右边两个角拉时右边不动，从下边拉时下边不动。少了这一步的话框会从按着
// 的那个角**一起长**——用户按住左下角想「只把左边拉出去」，看到的却是整个
// 框往右下角跑。
func resizeCrop(start cropBox, corner cropCorner, x, iw, ih float32) cropBox {
	_, _, fw, _ := fitImage(iw, ih)
	b := fitCrop(start, iw, ih)
	rx, _, rw, _ := stageRect(b, iw, ih)

	// 左边两个角钉住左边缘，右边两个角钉住右边缘。
	var px float32
	if corner == cornerTL || corner == cornerBL {
		px = x - rx
	} else {
		px = (rx + rw) - x
	}
	w, h := cropWH(px/fw*100, iw, ih)

	if corner == cornerTR || corner == cornerBR {
		b.X += b.W - w // 右边不动：宽度变了就把左边挪回来
	}
	if corner == cornerBL || corner == cornerBR {
		b.Y += b.H - h // 下边不动：高度变了就把上边挪回来
	}
	b.W, b.H = w, h
	return fitCrop(b, iw, ih)
}

// boxOriginX 是「按着这个角时，框的左边缘在台子上的位置」。
func boxOriginX(b cropBox, corner cropCorner, iw, ih float32) float32 {
	rx, _, _, _ := stageRect(fitCrop(b, iw, ih), iw, ih)
	return rx
}

// ── 像素 ───────────────────────────────────────────────────────────────────

// 封面文件大小的上限。Biu 那边也是 5MB（`image-upload/index.tsx:88`）。
//
// ⚠️ **这个上限不是防「撑爆内存」**（`Client.Do` 里那个 32MB 才是）。它是防
// 一张 8000×8000 的原图：裁剪要在内存里把它整个解码出来，而一张手机随手
// 拍的照片解码完是 8000×8000×4 = 256MB——用户选错一张文件就能让应用卡住
// 十几秒。按错了的时候说一句人话比让应用卡住好。
const maxCoverBytes = 5 << 20

// jpegQuality 是裁完之后编 JPEG 的质量。
//
// 0.9 照抄 Biu（`crop-modal.tsx:72` 的 `toDataURL("image/jpeg", 0.9)`）。
// ⚠️ 必须是 JPEG 不是 PNG：B 站的封面接口只认 JPEG/PNG，而 PNG 编出来是
// 原图大小——一张 960×600 的照片 PNG 要 1MB 多，JPEG 只有 150KB。
const jpegQuality = 90

// decodeCover 解一张封面文件，返回解码后的像素。
//
// ⚠️ **在这里就把像素拿在手里，不碰 `ui.Bitmap`**：Bitmap 只能写不能读，
// 造出来之后就再也拿不回来了（见包注释）。
func decodeCover(data []byte) (*image.RGBA, error) {
	if len(data) == 0 {
		return nil, errors.New("biu: 没读到那个文件")
	}
	if len(data) > maxCoverBytes {
		return nil, errors.New("biu: 图片太大，要小于等于 5MB")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// ⚠️ 说是「只支持 jpeg 或 png」而不是把 err 拼上去：Go 的解码器报错
		// 是英文的（"image: unknown format"），而用户看到的是一句自己看得懂的话。
		return nil, errors.New("biu: 只支持 jpeg 或 png 格式的图片")
	}
	// 统一成 RGBA：下面缩放那一步要按通道取值，而不同格式的
	// `image.Image` 取颜色的快慢差几十倍，逐像素拷贝一次反而更快也更简单。
	out := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out, nil
}

// cropToDataURL 按裁剪框裁、按 dpr 放大、编成 JPEG 的 data 地址。
//
// dpr 是这台机器的**像素密度**（Retina 是 2）。为什么必须乘它：B 站的收藏夹
// 封面是在网页上按 CSS 像素摆的，而 CSS 像素在 Retina 上是两个物理像素。传
// 一张按逻辑像素裁的图上去，用户在自己电脑上看着清楚、发到手机上（密度更低
// 或者按原始像素显示）就糊了。乘上 dpr 等于「在这台机器上看着不糊」。
//
// ⚠️ **dpr 要夹一道**：Tester 里那台机器的缩放可以是任意值，而 dpr=8 的
// 输出是 64 倍面积——一张 1920×1080 的图会编成 15000×8000 的 JPEG，那一步
// 光是分配就要几百 MB。夹在 1~3 之间覆盖了真实世界里的全部情况
// （1 = 普通屏，2 = 绝大多数笔记本，3 = 少数高分屏）。
func cropToDataURL(src *image.RGBA, b cropBox, dpr float32) (string, error) {
	if src == nil {
		return "", errors.New("biu: 没有图可以裁")
	}
	iw, ih := int32(src.Bounds().Dx()), int32(src.Bounds().Dy())
	if iw <= 0 || ih <= 0 {
		return "", errors.New("biu: 那张图是空的")
	}
	dpr = min(max(dpr, 1), 3)

	b = fitCrop(b, float32(iw), float32(ih))
	// 百分比 → 原图像素。四舍五入而不是截断：截断会让小图上裁出来的东西
	// 比框里看到的少一列，而那一列在放大之后是可见的一条白边。
	sx := int32(math.Round(float64(b.X) / 100 * float64(iw)))
	sy := int32(math.Round(float64(b.Y) / 100 * float64(ih)))
	sw := max(int32(math.Round(float64(b.W)/100*float64(iw))), 1)
	sh := max(int32(math.Round(float64(b.H)/100*float64(ih))), 1)

	// ⚠️ 再夹一次到图里：浮点误差会让 sx+sw 比 iw 大一两个像素，而
	// `draw.Scaler` 遇到源矩形超出源图会**直接 panic**（不是返回错误）——
	// 那一条 panic 会穿透 Cocoa 事件循环杀掉整个进程。
	sx = min(max(sx, 0), iw-1)
	sy = min(max(sy, 0), ih-1)
	sw = min(sw, iw-sx)
	sh = min(sh, ih-sy)
	if sw < 1 || sh < 1 {
		return "", errors.New("biu: 裁剪框太小了")
	}

	dw := max(int(math.Round(float64(sw)*float64(dpr))), 1)
	dh := max(int(math.Round(float64(sh)*float64(dpr))), 1)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src,
		image.Rect(int(sx), int(sy), int(sx+sw), int(sy+sh)), xdraw.Src, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return "", err
	}
	// ⚠️ **必须带 `data:image/jpeg;base64,` 这个前缀**：B 站的封面上传接口
	// 是从前缀里认格式的，只给 base64 它会当成一段没意义的字符，然后回一个
	// 打不开的地址（见 `bili.UploadCover`）。
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
