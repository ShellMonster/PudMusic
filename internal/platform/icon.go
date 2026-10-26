// 应用图标与托盘图标。
//
// ## 图案：一张挑好的画，加一段代码切的形
//
// 图标是 `internal/platform/icon-source.png`（1024×1024，人工挑定的那一张）。
// **换图案就是换那一张文件**，本文件只负责把它变成各平台上真正要用的
// 那些尺寸。
//
// 为什么图源在本包里面而不是 `resources/`：`//go:embed` 不许跨目录往上取
// （`../` 是编译错误），而图案得在**运行时**也能取到——托盘图标是启动时现画的，
// 那时既没有工作目录可依赖、也没有一个能保证存在的资源目录（`go test` 与
// `go run` 下 `PathResources` 指的是工作目录，别的目录下跑就找不到）。
//
// MyGo 的 `mygo build` 从 `resources/icon.png`（1024×1024）出发，自己把 macOS 的
// `.icns`（8 个尺寸）和 Windows 的 `.ico`（7 个尺寸）都渲出来，见
// `mygo@v0.6.3/cmd/mygo/icon.go` 的 `pngToICNS` / `pngToICO`。而 `resources/icon.png`
// 是 `go generate ./internal/platform/` 从本文件生成的——**它和托盘上那一张是同一段
// 代码出来的**，所以两边不可能漂移（改一张忘另一张的症状是「Dock 上是新图标、
// 通知区域还是旧图案」，而没有任何一处会报错）。
//
// ## 为什么源图有两种，形状怎么处理
//
// 源图有两种，代码都认：
//
//  1. **满幅方图**：四个角不透明、图案一直铺到边——那种由代码缩进
//     `iconShapeRatio` 那个方块里、四周留白，再把方块四角切成圆角；
//  2. **自带形状**：四个角是透明的，形状本身就是图案的一部分（手绘的圆角、
//     插画里的轮廓）——那种**原样用**：再缩进一圈、再切一次角就把它毁了。
//
// 走哪一条是**代码看源图自己判的**（`cornersTransparent`），不写死成常量——
// 换图不用改代码。
//
// ⚠️ **macOS 不会替你切圆角。** Big Sur 改的是 Apple 自家那一批图标，而且那道
// 圆角是**画进 .icns 里**的，不是系统渲染时裁的；第三方应用的图标形状仍然完全由
// 素材决定（所以才有 IconSur 这类专门补这一步的第三方工具）。MyGo 的 `pngToICNS`
// 也只缩放，一个像素都不动。满幅方图直接进包，Dock 上就是一个硬边方块。
//
// 满幅那一档的留白不是装饰：满幅图贴边会让主体被形状切掉。
// 这正是 macOS 图标网格的排法（1024 画布里图案占 824）。
//
// ## 判据是「缩到 16 之后还认得出」，不是「和某张截图像」
//
// `.ico` 的最小一档是 16×16（Windows 任务栏和资源管理器真正用的尺寸），托盘的
// 32×32 是菜单栏 16pt 的 Retina 图。满幅照片比 Biu 原来那台带天线的电视机更容易
// 在小尺寸上糊成一团色块，所以 `icon_test.go` 那几条逐尺寸断的就是「主体还在、
// 底和主体分得开」，不是像素级的相似度。
package platform

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
)

// iconSource 是挑定的那张图，编进二进制。
//
// ⚠️ **别把它缩到 512 或更小来「省事」。** 下采样是从源图做的，源图只有 512 的话
// `.icns` 最大的 1024 那档就是插值出来的模糊图——这正是 Biu 自己踩的坑（它的
// `electron/icons/logo.png` 只有 512×512）。`icon_test.go` 的
// `TestTheSourceIconIsBigEnoughForEverySize` 盯着这个下限。
//
//go:embed icon-source.png
var iconSource []byte

// 图标几何，按边长取比例，换尺寸不用改常量。
const (
	// iconShapeRatio 是图案方块的边长 ÷ 画布边长。
	//
	// 0.824 是 macOS 图标网格那一格：1024 的画布里图案占 824，四边各留 100 的
	// 透明边。留白不是装饰——满幅图贴边会让主体被形状切掉。
	iconShapeRatio = 0.824

	// iconCornerRatio 是圆角半径 ÷ **图案方块**的边长（不是画布的）。
	// 0.2237 是 macOS 连续圆角在正方形上的常用近似（`the concentric shape`
	// 的经典取值），与 macOS 自己的图标对得上。
	iconCornerRatio = 0.2237

	// iconMaskSamples 是判「这个像素在不在圆角里」时，一个像素边上取几个采样点。
	//
	// 没有它的话圆角是一条硬台阶：1024 上要放大才看得见，但 32 和 16 那两档上
	// 它就是四个缺了角的方块，正好是最要命的两档。
	iconMaskSamples = 4
)

// iconArtOnce / iconArt 缓存解出来的那张 1024 源图。
//
// # 为什么缓存
//
// 解一张 1024² 的 PNG 要走一遍整图的滤波，比一次逐像素的光栅化还贵；而每画一个
// 尺寸都要用一次它。生产运行时要的只有 32 那一档（`TrayIconPNG`），平时就一直
// 只驻留这一张（`TestTheShippedIconIsTheOneTheCodeDraws` 那类测试才要 1024）。
//
// ⚠️ **和 `AppIconShared` 一样，这份是进程内共享的只读图**：改它等于改掉之后
// 所有的调用方，而图案形状那几条测试断不出来（它们读到的是「改完之后」那张图）。
var (
	iconArtOnce sync.Once
	iconArtPNG  *image.NRGBA
	iconArtErr  error
	// iconArtHasShape 记着源图是不是**自带形状**（四个角是透明的）。
	//
	// 源图有两种：一种是**满幅方图**（四个角不透明、图案一直铺到边）——那种由
	// 代码缩进留白再切圆角（见 `fillIcon`）；另一种是**自带形状**（四个角透明，
	// 形状本身就是图案的一部分，比如带手绘圆角的插画）——那种**原样用**：
	// 再缩进一圈、再切一次角，就把它毁了。
	//
	// 判据是**源图自己的四个角透不透明**（`cornersTransparent`），不写死成常量，
	// 这样换图不用改代码。
	iconArtHasShape bool
)

// cornersTransparent 判源图的四角有没有透明像素（取每个角上 4×4 那一小块，
// 全透明才算这个角是透明的）。
func cornersTransparent(img *image.NRGBA) bool {
	b := img.Bounds()
	const probe = 4
	corners := []image.Point{
		{b.Min.X, b.Min.Y},
		{b.Max.X - probe, b.Min.Y},
		{b.Min.X, b.Max.Y - probe},
		{b.Max.X - probe, b.Max.Y - probe},
	}
	for _, c := range corners {
		transparent := true
		for dy := 0; dy < probe; dy++ {
			for dx := 0; dx < probe; dx++ {
				if img.NRGBAAt(c.X+dx, c.Y+dy).A != 0 {
					transparent = false
				}
			}
		}
		if !transparent {
			return false
		}
	}
	return true
}

// iconArt 解出那张 1024 源图（同尺寸多次调用给的是同一张，只读）。
//
// 失败过一次就一直是那个错误：它是嵌入进来的字节，坏掉只可能是编译期就出了事
// （文件没进包、被裁掉），重试一万次的结果也一样，而每次都重解一遍是白花时间。
func iconArt() (*image.NRGBA, error) {
	iconArtOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(iconSource))
		if err != nil {
			iconArtErr = fmt.Errorf("解 icon-source.png：%w", err)
			return
		}
		iconArtPNG = toNRGBA(img)
		iconArtHasShape = cornersTransparent(iconArtPNG)
	})
	return iconArtPNG, iconArtErr
}

// AppIconPNG 画应用图标，返回 size×size 的 PNG 字节。
//
// size 会被夹到 [16, 4096]：0 或负数会让 `image.NewNRGBA` 拿到非法矩形而 panic，
// 而 4096 以上白占地方——`.icns` 最大的那档就是 1024（`icon_test.go` 里抄了框架的
// 尺寸表）。
func AppIconPNG(size int) ([]byte, error) {
	size = min(max(size, 16), 4096)
	// 只读就行——马上要 png.Encode，而 png.Encode 不改源图。
	img := AppIconShared(size)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// iconCache 是 `AppIconShared` 画好的成品图，键是边长。
//
// # 为什么缓存
//
// 画图是**纯函数**：同一个边长画出来永远是同一张，没有随机、没有外部状态，
// 读的是 `iconSource` 和代码里的那几个比例。而它不便宜——1024 那一档要把一千六
// 百多万个源像素缩回来。
//
// 重复问同一档的地方是现成的：`cmd/genicon` 的尺寸表里 256 和 512 各出现两次
// （`.icns` 和 `.ico` 各要一档），于是每次生成图标都把最贵的两张图白画一遍。
//
// # 为什么不是「按尺寸攒满一整套」
//
// 缓存是懒的：谁问谁填。而**产品运行时要的只有 32 那一档**（`TrayIconPNG`），
// 平时就一直只驻留这一张小图（几 KB）。整套尺寸表加起来有几兆，只有 `genicon`
// 那种一次性跑完就退出的工具才会把它填满——对那种进程，几兆无关紧要。
//
// # ⚠️ 这里存的那张图是**进程内共享的一份**，谁拿到都**不能改**
//
// 改了就等于改了整个进程里之后所有的调用方——包括托盘图标——而且没有任何
// 测试会红（后面那几条测试读到的还是「改完之后」那张图，它们只断图案形状，
// 断不出「这张图被别人改过」）。
//
// 所以这个缓存**只由 `AppIconShared` 用**，它的返回值在签名和文档上都标了
// 「只读」。要一张自己能改的图请用 `AppIcon`——它每次都新画一张。
var iconCache sync.Map // int → *image.NRGBA

// AppIcon 画应用图标，返回 size×size 的图（已经是缩好的最终尺寸）。
//
// 缩放在内部完成，调用方拿到的是干净的边。返回 `*image.NRGBA`：PNG 要的是
// 非预乘的 alpha，`image.NRGBA` 直接就是。
//
// 每次调用都**新画一张**，调用方可以随意改它，不会影响别处（也包括之后
// 再调一次拿到的图）。代价是同一尺寸画几次就画几次——只需要读一遍的那些地方
// （编码、逐像素检查）用 `AppIconShared`。
func AppIcon(size int) *image.NRGBA {
	size = min(max(size, 16), 4096)
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	fillIcon(dst)
	return dst
}

// AppIconShared 是 `AppIcon` 的只读版本：同一个尺寸多次调用给的是**同一张**，
// 省掉重复的逐像素光栅化。
//
// ⚠️ **返回值不能改。** 它是进程内共享的（见 `iconCache` 上那段为什么），
// 在返回值上 `SetNRGBA` 会把之后所有调用方（包括托盘图标）一起改掉，
// 而且没有任何测试会红。要一张能改的用 `AppIcon`。
func AppIconShared(size int) *image.NRGBA {
	if v, ok := iconCache.Load(size); ok {
		return v.(*image.NRGBA)
	}
	dst := AppIcon(size)
	iconCache.Store(size, dst)
	return dst
}

// fillIcon 在整张 n×n 的画布上画出图标：图案方块居中，四周透明，方块四角是圆角。
//
// ⚠️ **源图自带形状时（`iconArtHasShape`）不做那两件事**：不缩进留白、不切圆角。
// 那张图的形状是它自己的一部分（手绘的圆角、插画里的轮廓），再套一层规则圆角
// 只会把它毁掉——而 macOS 也不替你切（见文件头「为什么源图是满幅的，圆角却是代码切的」
// 那段：第三方应用的图标形状**完全由素材决定**）。
func fillIcon(dst *image.NRGBA) {
	n := dst.Bounds().Dx()
	art, err := iconArt()
	if err != nil {
		return // 画不出图标时留一张全透明的，托盘那边也是这么处理的
	}
	side := iconSide(n)
	if iconArtHasShape {
		// 自带形状 → 满幅：图案就是整个画布，四周那圈是它自己透明的地方。
		side = n
	}

	// 满幅源图 → 图案方块。面积平均（`downsample`），所以缩到 32 那一档时
	// 主体和底色是被**平均**出来的，不是最近邻挑几个像素。
	inner := image.NewNRGBA(image.Rect(0, 0, side, side))
	downsample(art, inner)
	if !iconArtHasShape {
		roundCorners(inner, int(math.Round(iconCornerRatio*float64(side))))
	}

	// 居中贴上去，四周那一圈保持透明。整块复制而不是再缩一次：这一步的缩放
	// 已经做在 inner 上（那里有抗锯齿的余地），再缩一次只会平白多一道滤波。
	off := (n - side) / 2
	for y := range side {
		for x := range side {
			dst.SetNRGBA(off+x, off+y, inner.NRGBAAt(x, y))
		}
	}
}

// iconSide 是 n×n 画布里图案方块的边长。
//
// 下限 8：源图 1024，按 0.824 缩到 size=16 时方块是 13，而更小的尺寸（例如将来
// 有人要 8×8 的 favicon）再按比例就只剩几格，掩码的采样点会不够用。
func iconSide(n int) int {
	return max(8, int(math.Round(iconShapeRatio*float64(n))))
}

// roundCorners 把 img 四角切圆：圆角之外的像素 alpha 归零，边上那几个按覆盖率
// 半透明（不然 32 和 16 那两档上圆角是四个硬台阶）。
func roundCorners(img *image.NRGBA, r int) {
	const s = iconMaskSamples
	side := img.Bounds().Dx()
	for y := range side {
		for x := range side {
			var inside int
			for j := range s {
				for i := range s {
					// 采样点用整数、坐标整体放大 s 倍：这样判形状用的还是同一个
					// `inRoundedRect`，不用为「半个像素」再写一份浮点版。
					if inRoundedRect(x*s+i, y*s+j, side*s, r*s) {
						inside++
					}
				}
			}
			if inside == s*s {
				continue // 整格都在里面，一个像素都不用动
			}
			c := img.NRGBAAt(x, y)
			img.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B,
				A: uint8((int(c.A)*inside + s*s/2) / (s * s))})
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
			// 两端都夹进源图：fx1 只夹上界，而 fx0 夹的是**最后一列**而不是宽度，
			// 这样放大时（dst 比 src 大，AppIcon(4096) 就是）最右/最下那一格仍能取到
			// 样本。不夹的话它一个样本都取不到、落成全透明，图标边上会出现一圈白。
			fx0, fx1 = min(fx0, sb.Dx()-1), min(fx1, sb.Dx())
			fy0, fy1 = min(fy0, sb.Dy()-1), min(fy1, sb.Dy())
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

// toNRGBA 把任意 `image.Image` 变成 `*image.NRGBA`（非预乘）。
//
// PNG 要的是非预乘 alpha，而解码出来可能是 `image.RGBA`（预乘）也可能是
// `image.NRGBA`/`image.Gray`/`image.Paletted`……统一转一遍，下游就只认一种。
func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dst.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
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

// 托盘图标：32×32 的一次性 PNG。
var (
	trayIconOnce sync.Once
	trayIconPNG  []byte
)

// TrayIconPNG 是托盘那个 PNG，32×32（Retina 下正好是菜单栏 16pt）。
//
// 它就是 `AppIconPNG(32)`——**托盘和应用用同一张图**，不是两份各画各的。
// 两份各画各的必然漂移：改了应用图标忘了改托盘，用户看到的就是一张新图案
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
