package platform

import (
	"bytes"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// 框架从源图渲出来的尺寸集。抄的是 `_research/mygo/cmd/mygo/icon.go` 的
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

// iconSourcePath 是源图的位置（相对仓库根）。
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

// TestTheSourceIconIsBigEnoughForEverySize 钉住「源图至少 1024」。
//
// 这就是任务里说的**不要照抄 Biu 的缺陷**：它的 `electron/icons/logo.png` 只有
// 512×512，而 `.icns` 里最大的一档是 1024——那一档只能靠插值放大，出来的图标在
// 「显示简介」和放大预览里是糊的。
//
// ⚠️ 这条断的是**下限**，不是「必须正好是 1024」：源图给 2048 没问题（只是白占
// 几 MB），给 512 则一定是缺陷。
func TestTheSourceIconIsBigEnoughForEverySize(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(readIconSource(t)))
	if err != nil {
		t.Fatalf("%s 不是合法的 PNG：%v", iconSourcePath, err)
	}
	b := img.Bounds()
	if b.Dx() != b.Dy() {
		t.Fatalf("%s 是 %dx%d，不是正方形（框架的 validateIcon 会直接报错）", iconSourcePath, b.Dx(), b.Dy())
	}
	// .icns 最大的那档是 1024，源图比它小就一定是插值出来的。
	const need = 1024
	if b.Dx() < need {
		t.Errorf("%s 只有 %dx%d，%s 那一档只能靠插值放大（Biu 的 logo.png 就是 512）",
			iconSourcePath, b.Dx(), b.Dy(), "icns 1024")
	}
}

// TestTheShippedIconIsTheOneTheCodeDraws 钉住「提交的那张就是代码画的那张」。
//
// PNG 是二进制，没法 review 出它过时了；图案改完忘了重新生成，仓库里躺着的是旧图，
// 而且**没有任何地方会报错**——MyGo 照样渲出全部尺寸，只是图案已经不是代码里
// 描述的那个。修法：`go generate ./internal/platform/`。
func TestTheShippedIconIsTheOneTheCodeDraws(t *testing.T) {
	want, err := AppIconPNG(1024)
	if err != nil {
		t.Fatalf("画图标：%v", err)
	}
	got := readIconSource(t)
	if !bytes.Equal(got, want) {
		t.Errorf("%s 与 `platform.AppIconPNG(1024)` 不一致——图案改了但没重新生成，"+
			"跑 `go generate ./internal/platform/`", iconSourcePath)
	}
}

// TestEverySizeInTheSetStillReadsAsAPlayer 逐个尺寸渲染，钉住「缩到最小也还认得出」。
//
// 「完整尺寸集」这件事有两个容易塌的地方：
//
//  1. 源图不够大——`TestTheSourceIconIsBigEnoughForEverySize` 管这个。
//  2. **图案在小尺寸上糊成一团**——这条管这个。`.ico` 的 16×16 是任务栏和资源
//     管理器里真正用的尺寸，一个整块绿色方块在那里没人知道那是什么。
//
// 判据是「白三角还在，且没有把整张图标吃掉」：白色像素太少 = 三角在缩采样里
// 被抹掉了；太多 = 三角形画成了大色块（图案画错了，不是尺寸问题）。
func TestEverySizeInTheSetStillReadsAsAPlayer(t *testing.T) {
	for _, s := range append(append([]int{}, macIconSizes...), winIconSizes...) {
		checkSizeReadsAsAPlayer(t, s)
	}
}

// checkSizeReadsAsAPlayer 是上面那条的一部分，按尺寸跑好让报错直接点名是哪一档。
func checkSizeReadsAsAPlayer(t *testing.T, size int) {
	t.Helper()
	img := AppIconShared(size)
	if got := img.Bounds(); got.Dx() != size || got.Dy() != size {
		t.Fatalf("%d：画出来是 %dx%d", size, got.Dx(), got.Dy())
	}

	var white, opaque int
	for y := range size {
		for x := range size {
			c := img.NRGBAAt(x, y)
			if c.A < 128 {
				continue
			}
			opaque++
			// 近白而不是纯白：边缘是抗锯齿出来的灰。判据要容得下它们。
			if c.R > 200 && c.G > 200 && c.B > 200 {
				white++
			}
		}
	}
	if opaque == 0 {
		t.Errorf("%d：整张图标一个不透明的像素都没有", size)
		return
	}
	f := float64(white) / float64(opaque)
	if f < 0.05 {
		t.Errorf("%d：白色三角只剩 %.1f%% 的不透明像素（要 ≥5%%）——16 尺寸那一档最容不下它", size, f*100)
	}
	if f > 0.45 {
		t.Errorf("%d：白色三角占了 %.1f%% 的不透明像素（要 ≤45%%）——它不是把整张图标吃掉了", size, f*100)
	}
}

// TestTheIconIsNotOneFlatColor 钉住「不是一张纯色图」。
//
// 纯色图能通过上面所有尺寸的检查（不透明像素 plenty、白色比例也可能碰巧落在区间
// 里……不对，纯绿的话白色比例是 0，会被上一条抓住）。但**全透明**和**只有一种
// 颜色**这两类坏图是打包之后才看得出来的，症状是 Dock 上一个空白图标或者一块
// 纯色方块——没有一处报错。
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
	if distinct < 100 {
		t.Errorf("整张图标只有 %d 个采样点和左上角的颜色不同，它是一张纯色图", distinct)
	}
}

// TestTheCornersAreTransparent 钉住圆角：四个角必须是透明的。
//
// macOS 的图标形状**就是那块圆角方块**，不是系统去裁的。角不透明的话，Dock 上
// 就是一个没有圆角的绿方块（Windows 上反而不难看，macOS 上很显眼）。
func TestTheCornersAreTransparent(t *testing.T) {
	img := AppIconShared(1024)
	pts := [][2]int{{0, 0}, {1023, 0}, {0, 1023}, {1023, 1023}}
	for _, p := range pts {
		if c := img.NRGBAAt(p[0], p[1]); c.A != 0 {
			t.Errorf("角 (%d,%d) 不透明（alpha=%d），圆角没画出来", p[0], p[1], c.A)
		}
	}
	// 方块里必须是实心的绿——不然就是圆画过头了，或者什么都没画。
	// 取 (512,40) 而不是正中心：正中心落在白色播放三角里（见 TestTheTrianglePointsRight）。
	if c := img.NRGBAAt(512, 40); c.A != 0xff || c.R != iconGreen.R || c.G != iconGreen.G || c.B != iconGreen.B {
		t.Errorf("(512,40) 是 %v，want 不透明的 Biu 绿 %v", c, iconGreen)
	}
}

// TestTheTrianglePointsRight 钉住播放三角朝向。
//
// ⚠️ 这条是照着一处**真的画错**写的：三个顶点写成「上左、右中、下右」时，画出来
// 是一块直角三角形——左边一条斜边、右边一条竖边。它**看起来仍然像个三角形**，
// 于是「有没有白三角」那类判据照样通过，只有肉眼在 1024 上才发现哪儿不对。
//
// 判据是左右两半的白像素数：右尖的播放键右侧几乎没有白像素，左半边是整条竖边。
func TestTheTrianglePointsRight(t *testing.T) {
	img := AppIconShared(512)
	var left, right int
	for y := range 512 {
		for x := range 512 {
			c := img.NRGBAAt(x, y)
			if c.A > 128 && c.R > 200 && c.G > 200 && c.B > 200 {
				if x < 256 {
					left++
				} else {
					right++
				}
			}
		}
	}
	if left == 0 || right == 0 {
		t.Fatalf("白像素只占了一边（左 %d、右 %d），图案画出来是半个", left, right)
	}
	// 竖边在左边，尖端在右边，所以右半边一定更少。差得太少就是画成了正方形之类的
	// 别的东西，差得太多就是三角整个跑到一边去了。
	if left <= right {
		t.Errorf("左边白像素 %d 不多于右边的 %d——三角的竖边不在左边，它不是播放键", left, right)
	}
	if left < right*2 {
		t.Errorf("左 %d 对右 %d，竖边没占住左半边——三角可能画成了正方形或指向反了", left, right)
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
// 包括托盘上那一张，而图案形状那几条测试断的是「还认不认得出是播放器」，
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
// 这是故意的——共享图被改是**跨测试**才看得出来的破坏：改一个大块，图案形状那几条
// 会红；改一个像素（比如顺手擦了个高光），它们一条都不红，而托盘图标从那一刻起
// 在整个进程生命周期里都是错的。`AppIconShared` 的文档说了不能改，这里让它真会红。
func TestTheSharedIconIsStillUntouched(t *testing.T) {
	const size = 64
	if !bytes.Equal(AppIconShared(size).Pix, AppIcon(size).Pix) {
		t.Error("AppIconShared 给的那张和现画的不是一张——有人改过共享的那份。" +
			"改一张图会改掉整个进程之后的每一次调用（包括托盘图标），" +
			"而图案形状那几条测试断不出来。要一张能改的请用 AppIcon")
	}
}
