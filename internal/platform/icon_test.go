package platform

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// 框架从源图渲出来的尺寸集。抄的是 `mygo@v0.6.3/cmd/mygo/icon.go` 的
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
	// 源图有两种，两种都要认：
	//
	//  1. **满幅方图**：四个角完全不透明，形状由 `fillIcon` 切（缩进留白 + 圆角）；
	//  2. **自带形状**：四个角是透明的，形状就是图案的一部分（手绘圆角、插画轮廓）——
	//     那种**原样用**，再缩进一圈、再切一次角就把它毁了。
	//
	// 判据是**它自己的状态和代码的处理一致**：拿一个满幅的图源当自带形状用，
	// 或者拿一个自带形状的图源当满幅用，两种症状都是「Dock 上一个比别的应用小一圈
	// 的方块」——**没有任何地方会报错**。
	cornerTransparent := false
	for _, p := range [][2]int{{0, 0}, {b.Dx() - 1, 0}, {0, b.Dy() - 1}, {b.Dx() - 1, b.Dy() - 1}} {
		if _, _, _, a := img.At(p[0], p[1]).RGBA(); a < 0xff00 {
			cornerTransparent = true
		}
	}
	// ⚠️ `iconArtHasShape` 是 `iconArt()` 里顺手定的，而 `iconArt()` 是惰性的
	// （`sync.Once`）——**没先调过它，这个变量还是零值**。
	if _, err := iconArt(); err != nil {
		t.Fatalf("解 icon-source.png：%v", err)
	}
	if cornerTransparent != iconArtHasShape {
		t.Errorf("图源的角%s，但代码按%s处理——满幅图源（角不透明）由 fillIcon 缩进切角，"+
			"自带形状的图源（角透明）原样用，两种搞反了 Dock 上就是一个大小不对的方块",
			map[bool]string{true: "是透明的", false: "不透明"}[cornerTransparent],
			map[bool]string{true: "自带形状", false: "满幅"}[iconArtHasShape])
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

// TestEverySizeInTheSetStillReadsAsTheSubject 逐个尺寸渲染，断「主体还在、底还在」。
//
// 两个色系是这个图标的全部构图：暖黄的鸟和青绿的底。它们各自的占比在 16 和 1024
// 之间只差一个百分点（27.2~27.9% / 35.2~36.8%），所以判据给得宽松：暖 ≥15%、
// 冷 ≥20%。给这么宽不是因为怕飘——**是因为要抓的坏图正好落在两头**：主体被
// 平均没了（暖掉到 0，Dock 上是一块青绿的方块），或者底被吃掉（冷掉到 0）。
// 真正糊掉的中间地带用肉眼在 32px 上看过，那张图还认得出是一只张着嘴的鸟。
func TestEverySizeInTheSetStillReadsAsTheSubject(t *testing.T) {
	for _, s := range append(append([]int{}, macIconSizes...), winIconSizes...) {
		checkSizeStillReadsAsTheSubject(t, s)
	}
}

// checkSizeStillReadsAsTheSubject 是上面那条的一部分，按尺寸跑好让报错直接点名是哪一档。
func checkSizeStillReadsAsTheSubject(t *testing.T, size int) {
	t.Helper()
	img := AppIconShared(size)
	if got := img.Bounds(); got.Dx() != size || got.Dy() != size {
		t.Fatalf("%d：画出来是 %dx%d", size, got.Dx(), got.Dy())
	}

	var opaque, subject, accent, disc int
	for y := range size {
		for x := range size {
			c := img.NRGBAAt(x, y)
			if c.A < 128 {
				continue
			}
			opaque++
			switch {
			case isSubject(c):
				subject++
			case isAccent(c):
				accent++
			}
			if isDisc(c) {
				disc++
			}
		}
	}
	if opaque == 0 {
		t.Errorf("%d：整张图标一个不透明的像素都没有", size)
		return
	}
	w, o := float64(subject)/float64(opaque), float64(accent)/float64(opaque)
	d := float64(disc) / float64(opaque)
	// ⚠️ **阈值是按这张图标量的**：源图上红日（主体）占 67%、黑胶 13%、米色底 14%，
	// 各尺寸几乎不变（缩到最小那一档黑胶会被米色稀释到 7% 上下）。
	// 而**切完圆角留白之后**（图案只占画布的 0.824² ≈ 68%）再缩到最小那一档，
	// 三个色系都会被彼此稀释一档。
	//
	// **要抓的坏图在两头**：主体被平均没了（红掉到 0，Dock 上是一块米色方块），
	// 或者底被吃掉了（米色掉到 0，整张图糊成一种颜色）。中间地带靠肉眼在 32px 上看过。
	// 三个色系：**橙红的大圆**（最醒目，主体）、**米色的云**（底）、
	// **黑胶唱片**（细节）。
	//
	// ⚠️ **米色那条在 2026-10-26 重新量过**：那之前源图是大远景，米色底占 59%，
	// 阈值给的是 12%；后来把源图重新取景（**唱片摆到正中**，见
	// `TestTheSubjectSitsInTheMiddle`），唱片和红日占满了画面，
	// 米色底**实测 13.5%~18.4%**。阈值按同一套口径重取：**断在 10%**
	// （实测最低 13.5%，留两成余量，和黑胶那条一个档）。
	// ⚠️ **别把这条当成「可以随便放松」**：它抓的是「整张图糊成一种颜色」。
	if w < 0.20 {
		t.Errorf("%d：主体（橙红的大圆）只剩 %.1f%% 的不透明像素（要 ≥20%%）——"+
			"16 尺寸那一档最容不下它，糊过去就是一块底色", size, w*100)
	}
	if o < 0.10 {
		t.Errorf("%d：底色（米色的云）只剩 %.1f%% 的不透明像素（要 ≥10%%）——"+
			"整张图糊成一种颜色就不是图标了", size, o*100)
	}
	if d < 0.035 {
		t.Errorf("%d：唱片的深色只剩 %.1f%% 的不透明像素（要 ≥3.5%%）——"+
			"那就只剩一个橙红圆，看不出是张唱片了", size, d*100)
	}
}

// isWarm 认主体：暖黄到奶白那一族。
//
// 阈值不是从这张图上「量」出来的，是按人眼看画的：奶白 R≈250 G≈245 B≈225，
// 黄 R≈250 G≈200 B≈120，青绿底 R≈95 G≈160 B≈165——R 这一刀分得很开，两边都留
// 了十几的余量，所以换一张同风格的图也还成立。
// isSubject 认主体：那个**橙红的大圆**（R 明显高于 G 和 B）。
func isSubject(c color.NRGBA) bool { return c.R > 170 && c.R > c.G+40 && c.R > c.B+50 }

// isAccent 认底色：**米色的云**那一族。
func isAccent(c color.NRGBA) bool { return c.R > 200 && c.G > 185 && c.B > 150 }

// isDisc 认那张**黑胶唱片**（深色那一族）。
func isDisc(c color.NRGBA) bool { return c.R < 90 && c.G < 90 && c.B < 90 }

// TestTheSubjectSitsInTheMiddle 钉住「音乐的元素在画布正中」这件事。
//
// ⚠️ **量的是「唱片暗部范围内那些红色像素的中位数位置」**，不是唱片的暗部范围本身：
// 唱片的下半截被米色的浪盖住了一部分，**暗部的范围因此偏上偏左**（量出来是
// 42% 那一档），而**人眼看到的唱片中心**（中间那圈红标签）在正中。
// 为什么用中位数：红标签是那一块里红色像素**最多**的地方，红日的边只有零星几片；
// **中位数**落在标签上，用均值会被红日拉走。
//
// 2026-10-26 那次重新取景就是照这个口径做的：标签摆到正中，
// 唱片占画布约 **67%**（之前是 78%，用户嫌那一个太大）。
func TestTheSubjectSitsInTheMiddle(t *testing.T) {
	// 量的是**源图**（`iconArt()`），不是画出来的那一档：取景这件事发生在源图上，
	// 而画的时候图案方块是**居中**贴进画布的（`fillIcon`），源图的中心也就是
	// 图标的中心。拿 1024 那一档量，边缘的抗锯齿不会把小块吃掉。
	art, err := iconArt()
	if err != nil {
		t.Fatalf("读源图：%v", err)
	}
	cx, cy, ok := theRecordCenter(art)
	if !ok {
		t.Fatalf("源图上找不到唱片（暗部）或它中间的红标签")
	}
	if math.Abs(cx-0.5) > 0.04 || math.Abs(cy-0.5) > 0.04 {
		t.Errorf("唱片中心的红色像素中位数在 %.1f%% / %.1f%%，要落在 50%% ± 4%% ——"+
			"它偏了，菜单栏那一档看上去就是「挤在角落里」", cx*100, cy*100)
	}
}

// theRecordCenter 找唱片中间那圈红标签的位置（占画布的比例）。
//
// 找法：先量唱片的**暗部范围**（黑胶那一族），再把**落在那个范围之内**的
// **红色像素**（红标签那一族）取**中位数**——见上面那条测试的注释。
func theRecordCenter(art *image.NRGBA) (float64, float64, bool) {
	size := art.Bounds().Dx()
	dx0, dy0, dx1, dy1 := size, size, -1, -1
	for y := range size {
		for x := range size {
			c := art.NRGBAAt(x, y)
			if c.A < 128 || !isDisc(c) {
				continue
			}
			if x < dx0 {
				dx0 = x
			}
			if y < dy0 {
				dy0 = y
			}
			if x > dx1 {
				dx1 = x
			}
			if y > dy1 {
				dy1 = y
			}
		}
	}
	if dx1 < 0 {
		return 0, 0, false
	}
	var xs, ys []int
	for y := dy0; y <= dy1; y++ {
		for x := dx0; x <= dx1; x++ {
			c := art.NRGBAAt(x, y)
			if c.A < 128 || !isSubject(c) {
				continue
			}
			xs = append(xs, x)
			ys = append(ys, y)
		}
	}
	if len(xs) == 0 {
		return 0, 0, false
	}
	sort.Ints(xs)
	sort.Ints(ys)
	return float64(xs[len(xs)/2]) / float64(size), float64(ys[len(ys)/2]) / float64(size), true
}
