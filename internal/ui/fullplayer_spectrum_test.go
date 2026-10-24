package ui

// 频谱画成 N 根离散的柱（票 #374）。
//
// # 为什么摆一份手摆的条高，而不是用真频谱
//
// 真频谱来自解码器挂点（`internal/service/audio/tap.go`），它**一直在变**（解码器
// 那条 goroutine 一直在吐新的谱）。拿「这一帧的数字」去核「上一帧画出来的东西」，
// 核的是两样不同的东西——而那种测试红起来的时候，看不出是画错了还是数据变了。
// 所以这里摆一份**手摆的条高**：画什么就该看到什么。
//
// # 这一组量的是「画面」
//
// 判据全是**像素**：柱子在自己的位置上、两根之间留着缝、每根的高度是它自己那一格
// 的值、2 像素的底排还在。这些是用户看见的东西，不是「代码长什么样」。
//
// # 颜色
//
// 柱子的颜色是频谱的默认色 `#666`（`spectrumDefaultColor`），底色是纯黑——
// 「有柱子」和「没柱子」在黑底上分得最清，而这一组量的正是「哪里有柱子、
// 哪里没有」。

import (
	"image"
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

const (
	// spectrumTestW/H 是这一组里频谱那一块的尺寸（真频谱是 400~640 宽、40 高）。
	spectrumTestW = 400
	spectrumTestH = 40
	// spectrumTestX/Y 是它摆在画面上的位置。
	spectrumTestX = 100
	spectrumTestY = 100
)

// spectrumTestColor 是这一组里柱子的颜色：频谱的默认色（`#666`）。
var spectrumTestColor = ui.Color{R: 0x66, G: 0x66, B: 0x66, A: 255}

// spectrumTester 造一个只画频谱的画面：一块黑底 + 那几根柱。
func spectrumTester(t *testing.T, bars []float32) *ui.Tester {
	t.Helper()
	b := newTestShell(t, withView(func(*Shell) func(*ui.Context) {
		return func(c *ui.Context) {
			area := ui.Box(c).Key("频谱").
				Absolute().Left(spectrumTestX).Top(spectrumTestY).
				Width(spectrumTestW).Height(spectrumTestH).
				Background(ui.Color{A: 255})
			area.Draw(func(p *ui.Painter, r ui.Rect) {
				paintSpectrum(p, r, bars, spectrumTestColor)
			})
		}
	}))
	b.tt.Frame()
	return b.tt
}

// TestTheSpectrumIsDrawnAsDiscreteBars 断「画的是 N 根离散的柱，中间留着缝」。
//
// 上游 Biu（`audio-waveform/index.tsx`）：
//
//	computedBarWidth = width / barCount
//	barGap  = computedBarWidth * 0.2
//	barWidth = computedBarWidth - barGap
//
// 也就是**柱宽八成、缝两成**。这一条量的是画面上真的看得见那道缝。
func TestTheSpectrumIsDrawnAsDiscreteBars(t *testing.T) {
	const n = 20
	bars := make([]float32, n)
	for i := range bars {
		// 每根一样高：最容易看出「一根一根」，而不是一条起伏的线。
		bars[i] = 0.5
	}
	tt := spectrumTester(t, bars)
	img := tt.Image()

	cell := float32(spectrumTestW) / n
	// 扫每根柱子中间那一行（避开两头的圆角，也避开顶边）。
	y := spectrumTestY + spectrumTestH - spectrumTestH/4
	lens, colored := spectrumRuns(img, y, spectrumTestX, spectrumTestX+spectrumTestW)

	// 一根柱子 + 一道缝，来回 n 次（最后一根右边也有一道：那块地方到头就是框边）。
	if len(lens) != 2*n {
		t.Fatalf("那一行切成了 %d 段（want %d = %d 根柱子加 %d 道缝）：%v",
			len(lens), 2*n, n, n, lens)
	}
	for i := range n {
		if !colored[2*i] {
			t.Errorf("第 %d 段不是柱子（want 柱子）：%v", 2*i, colored)
		}
		if colored[2*i+1] {
			t.Errorf("第 %d 段是柱子（want 缝）：%v", 2*i+1, colored)
		}
		// 柱宽八成、缝两成。差一个像素是取整（框边按整像素对齐），差两个就不是。
		if d := lens[2*i] - int(cell*spectrumBarRatio); d > 1 || d < -1 {
			t.Errorf("第 %d 根柱子宽 %d 像素，want %d（一格的八成）",
				i, lens[2*i], int(cell*spectrumBarRatio))
		}
		if d := lens[2*i+1] - int(cell*(1-spectrumBarRatio)); d > 1 || d < -1 {
			t.Errorf("第 %d 道缝宽 %d 像素，want %d（一格的两成）",
				i, lens[2*i+1], int(cell*(1-spectrumBarRatio)))
		}
	}
}

// TestEachSpectrumBarUsesItsOwnValue 断「每根柱子用的是它自己那一格的值」。
//
// 折线那套（`chart.MinMax`）是**按像素列取最低/最高**：窄峰被抹平，柱子的高度
// 也不再是它自己那一格的值。这里摆一份一根比一根高的，看画出来的是不是**一根
// 比一根高，而且高度就是那一格的值**。
func TestEachSpectrumBarUsesItsOwnValue(t *testing.T) {
	const n = 20
	bars := make([]float32, n)
	for i := range bars {
		// 高度给成整数像素（2~21），免得「差半个像素」那一档混进来。
		bars[i] = float32(i+2) / spectrumTestH
	}
	tt := spectrumTester(t, bars)
	img := tt.Image()

	cell := float32(spectrumTestW) / n
	for i, v := range bars {
		// 扫每根柱子中间那一列（避开两头的圆角）。
		x := int(spectrumTestX + float32(i)*cell + cell*spectrumBarRatio/2)
		top := spectrumTopAt(img, x, spectrumTestY, spectrumTestY+spectrumTestH)
		want := spectrumTestY + spectrumTestH - int(v*spectrumTestH)
		if top != want {
			t.Errorf("第 %d 根柱子的顶在第 %d 行，want %d（它自己那一格的值是 %v）",
				i, top, want, v)
		}
	}
}

// TestTheSpectrumKeepsTheTwoPixelBaseRow 断「静的时候底下那条 2 像素的排还在」。
//
// 上游（`index.tsx:104`）：`barHeight = Math.max((value / 255) * height, 2)`。
// 那个「至少 2 像素」是 `spectrum.Bars` 算的（`BaseDip`），而这一层画的时候
// 要**照着那个比例画**，不能自己再算一遍（算错一处就是 1 像素，而 1 像素看着
// 就是「没有底排」）。
func TestTheSpectrumKeepsTheTwoPixelBaseRow(t *testing.T) {
	const n = 20
	// 静的时候每一格都是那个底排比例（2 像素 / 40 像素高）。
	bars := make([]float32, n)
	for i := range bars {
		bars[i] = 2.0 / spectrumTestH
	}
	tt := spectrumTester(t, bars)
	img := tt.Image()

	cell := float32(spectrumTestW) / n
	x := int(spectrumTestX + cell*spectrumBarRatio/2)
	top := spectrumTopAt(img, x, spectrumTestY, spectrumTestY+spectrumTestH)
	want := spectrumTestY + spectrumTestH - 2
	if top != want {
		t.Errorf("底排的顶在第 %d 行，want %d（那一条是 2 像素）", top, want)
	}
	// 底排上面那一行必须是空的：底排就是 2 像素，不是 3 像素也不是一整条。
	if isSpectrumColor(img.RGBAAt(x, want-1)) {
		t.Errorf("底排上面第 %d 行也有颜色——底排不止 2 像素", want-1)
	}
}

// TestTheSpectrumDoesNotAllocateEveryFrame 钉住「画频谱不每帧分配」。
//
// 频谱是**每帧**画的（放歌的时候每一帧都要下一帧），所以这一层每帧多分配一次
// 就是每分钟白烧几十 KB。判据是**分配次数**（不是时间）：它不随机器快慢变
// （AGENTS.md §2）。
func TestTheSpectrumDoesNotAllocateEveryFrame(t *testing.T) {
	const n = 48
	bars := make([]float32, n)
	for i := range bars {
		bars[i] = 0.5
	}
	tt := spectrumTester(t, bars)
	got := measureFrames(t, tt, budgetFrames)
	t.Logf("画频谱：每帧 %.0f 次分配 / %.0f 字节 / 存活堆 %.0f 字节",
		got.allocs, got.bytes, got.live)
	// 上限 20 次 / 2000 字节：这一块一共就建了一个盒子 + 48 根柱子，
	// 而「每帧新建一份柱子的框」那种回归一次就是几十次。
	if got.allocs > 20 {
		t.Errorf("画频谱每帧分配了 %.0f 次，上限 20 次——多半是每帧新建了什么",
			got.allocs)
	}
	if got.bytes > 2000 {
		t.Errorf("画频谱每帧分配了 %.0f 字节，上限 2000 字节", got.bytes)
	}
	if got.live > liveHeapBudget {
		t.Errorf("画频谱每帧留下了 %.0f 字节（上限 %d）", got.live, liveHeapBudget)
	}
}

// ---- 帮手 ----

// spectrumRuns 把 [x0, x1) 这一行按「接近 #666 的」和「不是的」切成一段段，
// 返回每段的长度和它有没有颜色。
func spectrumRuns(img *image.RGBA, y, x0, x1 int) (lens []int, colored []bool) {
	start := x0
	inRun := isSpectrumColor(img.RGBAAt(x0, y))
	for x := x0 + 1; x < x1; x++ {
		c := isSpectrumColor(img.RGBAAt(x, y))
		if c == inRun {
			continue
		}
		lens = append(lens, x-start)
		colored = append(colored, inRun)
		start, inRun = x, c
	}
	lens = append(lens, x1-start)
	colored = append(colored, inRun)
	return lens, colored
}

// spectrumTopAt 是那一列里第一个接近 #666 的行（没有就是 -1）。
func spectrumTopAt(img *image.RGBA, x, y0, y1 int) int {
	for y := y0; y < y1; y++ {
		if isSpectrumColor(img.RGBAAt(x, y)) {
			return y
		}
	}
	return -1
}

// isSpectrumColor 是不是「接近 #666」。
//
// 容差 4 是抗锯齿边的余量：边上那几个像素是柱子色和底色混出来的，而**内部**
// 的像素是纯粹的 #666。
func isSpectrumColor(p color.RGBA) bool {
	return absDiff(p.R, 0x66) <= 4 && absDiff(p.G, 0x66) <= 4 && absDiff(p.B, 0x66) <= 4
}
