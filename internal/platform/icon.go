// 应用图标与托盘图标。
//
// ## 图标为什么是现画的，不是塞进 resources 的二进制
//
// MyGo 的 `mygo build` 从**一张方形 PNG**（`resources/icon.png`，1024×1024）出发，
// 自己把 macOS 的 `.icns`（8 个尺寸）和 Windows 的 `.ico`（7 个尺寸）都渲出来。
// 见 `_research/mygo/cmd/mygo/icon.go` 的 `pngToICNS` / `pngToICO`。
//
// 所以本仓库的图标责任只有一件：**给一张够大的、干净的源图**。
//
// ⚠️ **不要把源图缩到 512 或更小来「省事」。** 下采样是从源图做的，源图只有 512
// 的话 1024 那档就是插值出来的模糊图——这正是 Biu 自己踩的坑（它的
// `electron/icons/logo.png` 只有 512×512）。源图必须是 1024。
//
// ## 图案为什么这么简
//
// Biu 的原图标是一台带天线的电视机（`biu/electron/icons/logo.png`）。在 1024 上
// 它很好看，但 `.ico` 的最小一档是 **16×16**：天线的两根细线会糊成一根、屏幕的
// 描边会连成一片实心，最后只剩一个说不清是什么的绿块。
//
// 所以这里取它的语言、砍掉细节：**Biu 的绿圆角方块 + 白色播放三角**。同一套形状
// 也用在托盘上（`tray.go`），托盘的 32×32 比图标的 16×16 还要小。
//
// 字号取舍的判据是「缩到 16 之后还认得出是个播放器」，不是「和 Biu 的截图像」。
package platform

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
)

// 应用与托盘图标的主色：Biu 的 Spotify 绿（`app.DefaultPrimaryColor`）。
// 抄一份而不是 import `internal/app`：`internal/platform` 在依赖方向的**下游**，
// 反过来 import 会成环。
var (
	iconGreen = color.NRGBA{R: 0x1e, G: 0xd7, B: 0x60, A: 0xff}
	iconWhite = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// 图标几何，全部按边长取比例，换尺寸不用改常量。
const (
	// iconCornerRatio 是圆角半径 ÷ 边长。0.2237 是 macOS 连续圆角在正方形上的
	// 常用近似（`the concentric shape` 的经典取值）。
	iconCornerRatio = 0.2237

	// 播放三角的宽和高各占边长的比例。三角形重心不在外接框中心，视觉上会显得
	// 偏左，所以横向按 0.02 边长右移补偿。
	iconTriangleWidth  = 0.46
	iconTriangleHeight = 0.52
	iconTriangleShiftX = 0.02

	// iconSupersample 是超采样倍数：先按 4 倍边长画，再面积平均缩回来。
	//
	// 没有它的话 32×32 的斜边是一格一格的台阶，托盘上能看见；有了它小尺寸是
	// 平滑的灰边。1024 那档看不出差别，但**尺寸集里的小尺寸才是问题所在**。
	iconSupersample = 4
)

// AppIconPNG 画应用图标，返回 size×size 的 PNG 字节。
//
// size 会被夹到 [16, 4096]：0 或负数会让 `image.NewNRGBA` 拿到非法矩形而 panic，
// 而 4096 以上的画布（4 倍超采样后是 16384²）要 1 GB 内存，一个图标而已。
func AppIconPNG(size int) ([]byte, error) {
	size = min(max(size, 16), 4096)
	img := AppIcon(size)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// iconCache 是画好的成品图，键是边长。
//
// # 为什么缓存
//
// `AppIcon` 是**纯函数**：同一个边长画出来永远是同一张图，没有随机、没有外部状态、
// 读的是代码里的常量（`iconGreen` / `iconWhite` 和那几个比例）。而它不便宜——1024
// 那一档是在 4096² 的超采样画布上逐像素画的，一千六百多万次几何判定。
//
// 重复问同一档的地方是现成的：`cmd/genicon` 的尺寸表里 256 和 512 各出现两次
// （`.icns` 和 `.ico` 各要一档），于是每次生成图标都把最贵的两张图白画一遍。
//
// # 为什么不是「按尺寸攒满一整套」
//
// 缓存是懒的：谁问谁填。而**产品运行时要的只有 32 那一档**（`TrayIconPNG`），
// 平时就一直只驻留这一张小图（4 KB）。整套尺寸表加起来有五兆多，只有 `genicon`
// 那种一次性跑完就退出的工具才会把它填满——对那种进程，五兆无关紧要。
//
// # 共享的那张图只能读
//
// 返回的是缓存里那一份。本仓库里没有任何地方改画完的图（往画布上写的只有
// `fillIcon` / `downsample`，它们写的是各自新建的那张）。这条约束写在这里，是
// 为了下一个想改这张图的人别就地改。
var iconCache sync.Map // int → *image.NRGBA

// AppIcon 画应用图标，返回 size×size 的图（已经是缩好的最终尺寸）。
//
// 超采样在内部完成，调用方拿到的是干净的边。返回 `*image.NRGBA`：PNG 要的是
// 非预乘的 alpha，`image.NRGBA` 直接就是。
//
// 同一尺寸画多次给的是**同一张**（见 `iconCache`）——调用方只能读它。
func AppIcon(size int) *image.NRGBA {
	if v, ok := iconCache.Load(size); ok {
		return v.(*image.NRGBA)
	}
	big := size * iconSupersample
	src := image.NewNRGBA(image.Rect(0, 0, big, big))
	fillIcon(src)

	// 面积平均缩回来。框不满就留透明，不补边——canvas 永远是 size×size。
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	downsample(src, dst)
	iconCache.Store(size, dst)
	return dst
}

// fillIcon 在整张 n×n 的画布上画不透明的图案（圆角方块 + 三角）。
func fillIcon(dst *image.NRGBA) {
	n := dst.Bounds().Dx()
	r := int(math.Round(iconCornerRatio * float64(n)))

	// 三角的外接框。左右顶点对称地落在 cx 两侧，整块按 n 的比例摆。
	//
	// ⚠️ 三个顶点的顺序是「上左、下左、右中」，不是「上左、右中、下右」。
	// 后者画出来是一块直角三角形（左边一条斜边、右边一条竖边），看着像三角形，
	// 但**不是播放键**——而且 1024 上看得很清楚，只有缩到 16 才发现哪儿不对。
	tw := iconTriangleWidth * float64(n)
	th := iconTriangleHeight * float64(n)
	cx := float64(n)/2 + iconTriangleShiftX*float64(n)
	cy := float64(n) / 2
	x0, x1 := cx-tw/2, cx+tw/2
	y0, y1 := cy-th/2, cy+th/2

	for y := range n {
		for x := range n {
			switch {
			case !inRoundedRect(x, y, n, r):
				// 圆角外面：透明。不铺底色——图标的形状就是那块绿方块。
			case inTriangleF(float64(x), float64(y), x0, y0, x0, y1, x1, cy):
				dst.SetNRGBA(x, y, iconWhite)
			default:
				dst.SetNRGBA(x, y, iconGreen)
			}
		}
	}
}

// downsample 把 src 面积平均缩成 dst。dst 可以比 src 小任意倍。
//
// 逐目标像素枚举源像素并累加颜色和 alpha，最后除以**实际取到的样本数**（不是
// src 的总像素数）：边缘那一圈本来就只有部分源像素落在框内，除以总数会把边缘
// 整体变淡，缩得越小越明显。
func downsample(src, dst *image.NRGBA) {
	sb := src.Bounds()
	sx := float64(sb.Dx()) / float64(dst.Bounds().Dx())
	sy := float64(sb.Dy()) / float64(dst.Bounds().Dy())
	for y := range dst.Bounds().Dy() {
		for x := range dst.Bounds().Dx() {
			fx0, fx1 := int(float64(x)*sx), int(math.Ceil(float64(x+1)*sx))
			fy0, fy1 := int(float64(y)*sy), int(math.Ceil(float64(y+1)*sy))
			fx1, fy1 = min(fx1, sb.Dx()), min(fy1, sb.Dy())
			var r, g, b, a, n float64
			for yy := fy0; yy < fy1; yy++ {
				for xx := fx0; xx < fx1; xx++ {
					c := src.NRGBAAt(sb.Min.X+xx, sb.Min.Y+yy)
					r += float64(c.R) * float64(c.A)
					g += float64(c.G) * float64(c.A)
					b += float64(c.B) * float64(c.A)
					a += float64(c.A)
					n++
				}
			}
			if a == 0 {
				continue // 整块透明，留 0
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r/a + 0.5), G: uint8(g/a + 0.5), B: uint8(b/a + 0.5),
				A: uint8(a/n + 0.5),
			})
		}
	}
}

// inRoundedRect 报 (x,y) 在不在一个 n×n 的圆角方块里（角半径 r）。
//
// 先把点夹到方块正中那个 r×r 的区域，夹完的点到它的距离就是到最近那个圆心的
// 距离——这是画圆角方块的标准做法，不用真的去解圆的方程。
func inRoundedRect(x, y, n, r int) bool {
	if r < 0 {
		r = 0
	}
	if x < 0 || y < 0 || x >= n || y >= n {
		return false
	}
	cx := min(max(x, r-1), n-r)
	cy := min(max(y, r-1), n-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// inTriangle 报点落在不在这三个点围成的三角形里（标准叉积判据）。
func inTriangle(px, py, x1, y1, x2, y2, x3, y3 int) bool {
	d := func(ax, ay, bx, by int) float64 { return float64((px-bx)*(ay-by) - (ax-bx)*(py-by)) }
	a, b, c := d(x1, y1, x2, y2), d(x2, y2, x3, y3), d(x3, y3, x1, y1)
	return !((a < 0 || b < 0 || c < 0) && (a > 0 || b > 0 || c > 0))
}

// inTriangleF 是 inTriangle 的浮点版（坐标带小数）。超采样画布上是 4096 这种
// 整数，但三角顶点是 `0.46*n` 这种小数，差半像素在 4 倍超采样下就是一道可见的
// 毛边。
func inTriangleF(px, py, x1, y1, x2, y2, x3, y3 float64) bool {
	d := func(ax, ay, bx, by float64) float64 { return (px-bx)*(ay-by) - (ax-bx)*(py-by) }
	a, b, c := d(x1, y1, x2, y2), d(x2, y2, x3, y3), d(x3, y3, x1, y1)
	return !((a < 0 || b < 0 || c < 0) && (a > 0 || b > 0 || c > 0))
}

// 托盘图标：32×32 的一次性 PNG。
var (
	trayIconOnce sync.Once
	trayIconPNG  []byte
)

// TrayIconPNG 是托盘那个 PNG，32×32（Retina 下正好是菜单栏 16pt）。
//
// 它就是 `AppIconPNG(32)`——**托盘和应用用同一张图**，不是两份各画各的。
// 两份各画各的必然漂移：改了应用图标忘了改托盘，用户看到的就是一个绿方块
// 配一个旧图案，而且没有任何一处会报错。
//
// 仍然现画而不塞进 `resources`：托盘图标要的是模板图（macOS）和小尺寸
// （Windows），走资源那一套反而要给两种格式各备一份文件。
func TrayIconPNG() []byte {
	trayIconOnce.Do(func() {
		b, err := AppIconPNG(32)
		if err != nil {
			return // 画不出图标时托盘没图标，应用其余部分照常
		}
		trayIconPNG = b
	})
	return trayIconPNG
}

//go:generate go run ./cmd/genicon
