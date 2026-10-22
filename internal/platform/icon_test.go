package platform

import (
	"bytes"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// 框架从源图渲出来的尺寸集。抄的是 `mygo@v0.6.2/cmd/mygo/icon.go` 的
// `pngToICNS` 与 `pngToICO` 两张表——**故意抄一份在这里**。
//
// 为什么不直接去问框架：这个包 import 不到 `cmd/mygo`（它是 `package main`）。
// 而这两张表是「源图要多大才够用」的判据，把它写下来才有地方钉。
var (
	// macOS .icns 的八个尺寸（ic11/ic12/ic07/ic13/ic08/ic14/ic09/ic10）。
	macIconSizes = []int{32, 64, 128, 256, 256, 512, 512, 1024}

	// Windows .ico 的七个尺寸。
	winIconSizes = []int{16, 24, 32, 48, 64, 128, 256}
)

// iconSourcePath 是**发出去**那张源图的位置（相对仓库根）。
//
// ⚠️ 它和 `internal/platform/icon-source.png`（图案本身，`icon.go` 里 embed 的
// 那一张）不是同一件事：这一张是**产物**，由 `go generate` 从那一张生成。图源
// 不在本测试的管辖范围内——它由下面那条 `TestTheArtworkIsBigEnoughFor…` 管。
const iconSourcePath = "resources/icon.png"

// repoRoot 往上找 `go.mod`。`go test` 的工作目录是**包目录**，不是仓库根。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("拿不到工作目录：%v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("从工作目录往上找不到 go.mod")
		}
		dir = parent
	}
}

func readIconSource(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), iconSourcePath))
	if err != nil {
		t.Fatalf("读不到 %s：%v", iconSourcePath, err)
	}
	return b
}

// TestTheArtworkIsBigEnoughForEverySize 钉住「图源至少 1024」。
//
// 这就是任务里说的**不要照抄 Biu 的缺陷**：它的 `electron/icons/logo.png` 只有
// 512×512，而 `.icns` 里最大的一档是 1024——那一档只能靠插值放大，出来的图标在
// 「显示简介」和放大预览里是糊的。
//
// ⚠️ **这一条读的是 `icon-source.png`（图源），不是 `resources/icon.png`（产物）。**
// 产物永远是 1024×1024，因为 `genicon` 就是拿 1024 去画的——拿产物去断这个下限
// 断的是「genicon 还在画 1024」，而真正会出事的是**有人换了一张 512 的图**。
// 那时产物仍然是 1024（插值出来的模糊图），产物那条检查照样绿。
//
// 这条断的是**下限**，不是「必须正好是 1024」：图源给 2048 没问题（只是白占
// 几 MB），给 512 则一定是缺陷。
func TestTheArtworkIsBigEnoughForEverySize(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(iconSource))
	if err != nil {
		t.Fatalf("icon-source.png 不是合法的 PNG：%v", err)
	}
	b := img.Bounds()
	if b.Dx() != b.Dy() {
		t.Fatalf("icon-source.png 是 %dx%d，不是正方形（缩进方块那一步会按方形取样，另一边就没了）", b.Dx(), b.Dy())
	}
	const need = 1024
	if b.Dx() < need {
		t.Errorf("icon-source.png 只有 %dx%d，.icns 最大的那一档只能靠插值放大"+
			"（Biu 的 logo.png 就是 512）", b.Dx(), b.Dy())
	}
}

// TestTheArtworkIsFullBleed 钉住「图源是满幅的，不自带圆角和留白」。
//
// 形状是 `fillIcon` 切的：满幅图缩进 `iconShapeRatio` 那个方块里、四周留白、再切
// 圆角（理由见 `icon.go` 顶上那段——**macOS 不会替你切**）。所以图源必须是一张
// **铺到四个角**的方图。
//
// 换个满幅不满幅的图源，症状不是报错：多留一圈白、或者圆角被切两次，出来的图标
// 在 Dock 上是一块比别的应用小一圈的方块——**没有任何测试会红**。
func TestTheArtworkIsFullBleed(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(iconSource))
	if err != nil {
		t.Fatalf("解 icon-source.png：%v", err)
	}
	b := img.Bounds()
	for _, p := range [][2]int{{0, 0}, {b.Dx() - 1, 0}, {0, b.Dy() - 1}, {b.Dx() - 1, b.Dy() - 1}} {
		_, _, _, a := img.At(p[0], p[1]).RGBA()
		if a < 0xff00 {
			t.Errorf("图源的角 (%d,%d) 是半透明的（alpha=%d）——满幅图源的四个角应当完全不透明，"+
				"形状由 fillIcon 切，图源自己再切一次就切重了", p[0], p[1], a>>8)
		}
	}
}

// TestTheShippedIconIsTheOneTheCodeDraws 钉住「提交的那张就是代码画的那张」。
//
// PNG 是二进制，没法 review 出它过时了；图源换了却忘了重新生成，仓库里躺着的是旧图，
// 而且**没有任何地方会报错**——MyGo 照样渲出全部尺寸，只是图案已经不是代码里
// 描述的那个。修法：`go generate ./internal/platform/`。
func TestTheShippedIconIsTheOneTheCodeDraws(t *testing.T) {
	want, err := AppIconPNG(1024)
	if err != nil {
		t.Fatalf("画图标：%v", err)
	}
	got := readIconSource(t)
	if !bytes.Equal(got, want) {
		t.Errorf("%s 与 `platform.AppIconPNG(1024)` 不一致——图源换了但没重新生成，"+
			"跑 `go generate ./internal/platform/`", iconSourcePath)
	}
	img, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("%s 不是合法的 PNG：%v", iconSourcePath, err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 1024 {
		t.Errorf("%s 是 %dx%d，want 1024x1024（genicon 写死 1024；比它小的那一档就是插值了）",
			iconSourcePath, b.Dx(), b.Dy())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 「缩到最小还认得出来」
//
// 这件事有两个容易塌的地方：
//
//  1. 图源不够大——`TestTheArtworkIsBigEnoughForEverySize` 管这个。
//  2. **图案在小尺寸上糊成一团**——下面这组管这个。
//
// 判据是「主体和底色还在、还分得开」。`.ico` 的 16×16 是 Windows 任务栏和资源
// 管理器里真正用的尺寸，托盘的 32×32 是菜单栏 16pt 的 Retina 图；这两档上如果
// 满幅照片被平均成一块说不清是什么的色块，Dock 上正常、用户桌面上是一坨。
//
// 之所以不用「白三角占了百分之几」那种像素判据：那只对「两三个纯色的几何图案」
// 成立，而这图标是一张插画，唯一的通用判据是**构图还在不在**。

// TestEverySizeInTheSetStillReadsAsABird 逐个尺寸渲染，断「主体还在、底还在」。
//
// 两个色系是这个图标的全部构图：暖黄的鸟和青绿的底。它们各自的占比在 16 和 1024
// 之间只差一个百分点（27.2~27.9% / 35.2~36.8%），所以判据给得宽松：暖 ≥15%、
// 冷 ≥20%。给这么宽不是因为怕飘——**是因为要抓的坏图正好落在两头**：主体被
// 平均没了（暖掉到 0，Dock 上是一块青绿的方块），或者底被吃掉（冷掉到 0）。
// 真正糊掉的中间地带用肉眼在 32px 上看过，那张图还认得出是一只张着嘴的鸟。
func TestEverySizeInTheSetStillReadsAsABird(t *testing.T) {
	for _, s := range append(append([]int{}, macIconSizes...), winIconSizes...) {
		checkSizeStillReadsAsABird(t, s)
	}
}

// checkSizeStillReadsAsABird 是上面那条的一部分，按尺寸跑好让报错直接点名是哪一档。
func checkSizeStillReadsAsABird(t *testing.T, size int) {
	t.Helper()
	img := AppIconShared(size)
	if got := img.Bounds(); got.Dx() != size || got.Dy() != size {
		t.Fatalf("%d：画出来是 %dx%d", size, got.Dx(), got.Dy())
	}

	var opaque, warm, cool int
	for y := range size {
		for x := range size {
			c := img.NRGBAAt(x, y)
			if c.A < 128 {
				continue
			}
			opaque++
			switch {
			case isWarm(c):
				warm++
			case isCool(c):
				cool++
			}
		}
	}
	if opaque == 0 {
		t.Errorf("%d：整张图标一个不透明的像素都没有", size)
		return
	}
	w, o := float64(warm)/float64(opaque), float64(cool)/float64(opaque)
	if w < 0.15 {
		t.Errorf("%d：主体（暖黄）只剩 %.1f%% 的不透明像素（要 ≥15%%）——"+
			"16 尺寸那一档最容不下它，糊过去就是一块底色", size, w*100)
	}
	if o < 0.20 {
		t.Errorf("%d：底色（青绿）只剩 %.1f%% 的不透明像素（要 ≥20%%）——"+
			"整张图糊成一种颜色就不是图标了", size, o*100)
	}
}

// isWarm 认主体：暖黄到奶白那一族。
//
// 阈值不是从这张图上「量」出来的，是按人眼看画的：奶白 R≈250 G≈245 B≈225，
// 黄 R≈250 G≈200 B≈120，青绿底 R≈95 G≈160 B≈165——R 这一刀分得很开，两边都留
// 了十几的余量，所以换一张同风格的图也还成立。
func isWarm(c color.NRGBA) bool { return c.R > 190 && c.G > 150 && c.B < 170 }

// isCool 认底色：青绿那一族。
func isCool(c color.NRGBA) bool { return c.B > 140 && c.G > 120 && c.R < 140 }

// TestTheIconIsNotOneFlatColor 钉住「不是一张纯色图」。
//
// 纯色图能通过上面所有尺寸的构图检查的一半（不透明像素 plenty），但**全透明**和
// **只有一种颜色**这两类坏图是打包之后才看得出来的，症状是 Dock 上一个空白图标
// 或者一块纯色方块——没有一处报错。
func TestTheIconIsNotOneFlatColor(t *testing.T) {
	img := AppIconShared(1024)
	first := true
	var firstColor [4]uint8
	distinct := 0
	for y := 0; y < img.Bounds().Dy(); y += 7 {
		for x := 0; x < img.Bounds().Dx(); x += 7 {
			c := img.NRGBAAt(x, y)
			if first {
				first, firstColor = false, [4]uint8{c.R, c.G, c.B, c.A}
				continue
			}
			if [4]uint8{c.R, c.G, c.B, c.A} != firstColor {
				distinct++
			}
		}
	}
	if distinct < 1000 {
		t.Errorf("整张图标只有 %d 个采样点和左上角的颜色不同，它是一张纯色图", distinct)
	}
}

// TestTheIconIsAPaddedRoundedSquare 钉住形状：macOS 图标网格那一格 + 圆角。
//
// 三件事合在一条里，因为它们是同一句话的三个部分——「一张 macOS 风格的图标」：
//
//  1. **四角透明**（形状不是铺满画布的方块）；
//  2. **留白是 macOS 那一格**（图案占 0.824，四周各留一点）——少了它，图标在
//     Dock 上比同排别的应用大一号，比多了它则小一号，两种都是一眼看得出来的东西；
//  3. **方块的四角是圆角**（不是硬边），而边上仍然是实的（不是圆过头画成了个蛋）。
//
// ⚠️ 第 3 条是这里最容易悄悄塌的一条：**macOS 不会替你切圆角**（Big Sur 改的是
// Apple 自家那一批，圆角是画进 .icns 里的），MyGo 的 `pngToICNS` 也只缩放、一个
// 像素都不动。所以忘了切的结果是 Dock 上一个硬边方块——照旧没有一处会报错。
func TestTheIconIsAPaddedRoundedSquare(t *testing.T) {
	const n = 1024
	img := AppIconShared(n)
	side := iconSide(n)
	off := (n - side) / 2

	// 1. 画布四角透明。
	for _, p := range [][2]int{{0, 0}, {n - 1, 0}, {0, n - 1}, {n - 1, n - 1}} {
		if a := img.NRGBAAt(p[0], p[1]).A; a != 0 {
			t.Errorf("角 (%d,%d) 不透明（alpha=%d），图标没有留白", p[0], p[1], a)
		}
	}
	// 2. 留白是 macOS 图标网格那一格（1024 画布里图案占 824，即 8.8%）。
	//    断的是一个**区间**而不是那个 8.8%：图源换一张可能就得挪一点，而挪成
	//    「铺满」或「只剩中间一小块」是要当场抓住的两种坏法。
	if pct := off * 100 / n; pct < 7 || pct > 12 {
		t.Errorf("留白是 %d%%（macOS 那一格是 8.8%%）——铺满的话图标在 Dock 上偏大，"+
			"留太多则偏小", pct)
	}
	// 留白这一圈的边线上必须**只有透明**，否则「留白」是个说法而不是事实。
	for _, m := range []int{off - 1, off / 2} {
		if a := img.NRGBAAt(n/2, m).A; a != 0 {
			t.Errorf("留白那一圈的第 %d 行不透明（alpha=%d）", m, a)
		}
	}
	// 3. 方块四角切了，而四条边的中点还是实的。
	if a := img.NRGBAAt(off, off).A; a != 0 {
		t.Errorf("图案方块的角 (%d,%d) 不透明（alpha=%d），圆角没切出来", off, off, a)
	}
	if a := img.NRGBAAt(off+side/2, off+side/2).A; a != 0xff {
		t.Errorf("图案方块的正中是半透明的（alpha=%d），它必须是实心的", a)
	}
	for _, p := range [][2]int{{off + side/2, off}, {off + side/2, off + side - 1},
		{off, off + side/2}, {off + side - 1, off + side/2}} {
		if a := img.NRGBAAt(p[0], p[1]).A; a != 0xff {
			t.Errorf("图案方块的边中点 (%d,%d) 是半透明的（alpha=%d）——圆角切过头了", p[0], p[1], a)
		}
	}
}

// TestTheSubjectIsNotMirrored 钉住构图方向：主体在右、嘴朝左。
//
// 这是老图标那条 `TestTheTrianglePointsRight` 的位置：一张图案**朝向错了**的图，
// 每一项「还认不认得出」的检查都照样通过（形状、颜色、比例全对），只有人一眼
// 看出「这张图和设计稿反了」。
//
// 判据是左右两半的主体像素数：这张图里鸟的身子和翅膀压在右边（左半边只剩尖尖的
// 翅根），实测比值 3~3.7 倍。水平翻转之后会掉到 1/3 左右，所以断在 2 倍上——
// 两边离各自的实测值都还远，而翻转一定跨过去。
func TestTheSubjectIsNotMirrored(t *testing.T) {
	const n = 512
	img := AppIconShared(n)
	var left, right int
	for y := range n {
		for x := range n {
			c := img.NRGBAAt(x, y)
			if c.A < 128 || !isWarm(c) {
				continue
			}
			if x < n/2 {
				left++
			} else {
				right++
			}
		}
	}
	if left == 0 || right == 0 {
		t.Fatalf("主体只占了一边（左 %d、右 %d），这张图根本不是那只鸟", left, right)
	}
	if right < left*2 {
		t.Errorf("主体在左边 %d、右边 %d（右至少要是左的两倍，实测是 3~3.7 倍）——"+
			"这张图被水平翻转了，或者换成了另一张", left, right)
	}
}

// TestTheTrayIconIsTheAppIcon 钉住「托盘和应用是同一张图」。
//
// 同一张图是**故意的**：两份各画各的必然漂移，而症状是「Dock 上是新图标、通知
// 区域还是旧图案」——那种错只有用户看得见，而**没有任何一处会报错**。这里把
// 「同一张」变成会红的东西。
func TestTheTrayIconIsTheAppIcon(t *testing.T) {
	want, err := AppIconPNG(32)
	if err != nil {
		t.Fatalf("画应用图标：%v", err)
	}
	if !bytes.Equal(TrayIconPNG(), want) {
		t.Error("托盘图标与应用图标不是同一张——它们必须同源，否则改了应用图标忘了改托盘，" +
			"用户看到的就是两张不一样的图案，而没有任何一处会报错")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 两个入口的契约
//
// `AppIcon` 每次新画、`AppIconShared` 给同一张共享的只读图。这两条是**契约**，
// 不是实现细节：弄反了要么是性能白扔，要么是有人改一张图改掉整个进程。
// 下面这两条把契约变成会红的东西，而不只是注释里的一句话。

// TestAppIconHandsOutAnIndependentImageEveryTime 钉住 `AppIcon` 每次独立。
//
// 它的契约是「返回的图归调用方，随便改」。改过的那张**不能**漏进下一次的结果
// ——否则第一个拿到图的人（比如想加个角标）会静悄悄地改掉后面所有人拿到的图标，
// 包括托盘上那一张，而构图形状那几条测试断的是「还认不认得出」，
// 改一个像素断不出来。
func TestAppIconHandsOutAnIndependentImageEveryTime(t *testing.T) {
	const size = 64
	mark := color.NRGBA{R: 0xff, G: 0x00, B: 0x00, A: 0xff}

	img := AppIcon(size)
	if img == AppIcon(size) {
		t.Fatal("AppIcon 两次调用是同一张——它必须每次新画，否则改一张就改掉全进程")
	}
	img.SetNRGBA(0, 0, mark)
	if c := AppIcon(size).NRGBAAt(0, 0); c == mark {
		t.Error("改过的 AppIcon 结果漏进了下一次调用")
	}
}

// TestAppIconSharedHandsOutTheSameReadOnlyImage 钉住 `AppIconShared` 共享。
//
// 共享是它的全部意义（`cmd/genicon` 的尺寸表里 256 和 512 各出现两次，
// 不共享就把最贵的两张图白画一遍）。缓存没了的话这条立刻红。
func TestAppIconSharedHandsOutTheSameReadOnlyImage(t *testing.T) {
	if AppIconShared(64) != AppIconShared(64) {
		t.Error("AppIconShared 两次调用不是同一张——缓存没了")
	}
}

// TestTheSharedIconIsStillUntouched 盯「这一整包跑下来，没人改过共享的那份」。
//
// ⚠️ **依赖执行顺序**：它得跑在本文件所有读图测试的**后面**，才看得到「跑完之后」。
// 这是故意的——共享图被改是**跨测试**才看得出来的破坏：改一大块，构图那几条
// 会红；改一个像素，它们一条都不红，而托盘图标从那一刻起在整个进程生命周期里
// 都是错的。`AppIconShared` 的文档说了不能改，这里让它真会红。
func TestTheSharedIconIsStillUntouched(t *testing.T) {
	const size = 64
	if !bytes.Equal(AppIconShared(size).Pix, AppIcon(size).Pix) {
		t.Error("AppIconShared 给的那张和现画的不是一张——有人改过共享的那份。" +
			"改一张图会改掉整个进程之后的每一次调用（包括托盘图标），" +
			"而构图形状那几条测试断不出来。要一张能改的请用 AppIcon")
	}
}
