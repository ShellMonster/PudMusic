package list

import (
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// 这一族钉住列表行下面那条「看到哪儿」进度条（票 #703 的 C 项）。
//
// # 为什么它值得有测试
//
// 它原先是**手搓的两个 `ui.Box`**：既没有角色，也没有值——对辅助技术根本不存在，
// 读屏软件读不出「这一首听到哪儿了」。换成框架的 `ui.Progress` 之后，它在无障碍
// 树里是一个**值多少的进度条**，而且**有名字**（框架那一版自己没有文字，名字只能
// 由我们给，见 `highlight.go` 的 `progressBar`）。
//
// ⚠️ **判据分两层**：画面上那一层（宽度、填充比例）量像素；无障碍那一层
// （角色、值、名字）走**真窗口的 automation 树**——那才是这一票真正的收益，
// 而它在无头 Tester 里看不到（框架只在辅助技术问起时才描述那一帧，
// `mygo/ui/access.go` 的 `accessibilityOn`）。

// progressSong 是「一首有进度」的歌。
func progressSong(progress, duration time.Duration) Music {
	return Music{
		ID: "a", Title: "一首歌", Owner: "甲 UP",
		Duration: duration, Progress: progress,
	}
}

// progressList 造一张只画一行、有进度的那张列表，主题是给定的。
//
// ⚠️ **主题要自己装**：列表这一层**不装主题**（`list.go` 的文件头：主题由页面在
// 每一帧开头装好，`internal/ui.ApplyTheme`），所以无头 Tester 用的是框架默认的
// 那一份——而量像素要对着**画面上真的那个主色**。
func progressList(t *testing.T, th *ui.Theme, m Music) *ui.Tester {
	t.Helper()
	l := New(newCache(t), Actions{})
	l.SetSongs([]Music{m})
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(th)
		l.View(c)
	}, testerW, testerH)
	tt.Frame()
	return tt
}

// TestTheProgressBarHasAName 钉住「那条进度条**有名字**」。
//
// 判据是 **Tester 找得到它**：框架那一版没有文字，所以名字只能来自 `Label`
// ——而 `Label` 同时也是读屏软件读的那一句（`mygo/ui/access.go` 的
// `accessDetails`：名字要么来自 `Label`，要么来自元素里面的文字）。
//
// ⚠️ **名字里带着歌名**：光说「播放进度」的话，读屏软件在一屏十几行里读到的
// 是十几次同一句话，分不出是哪一首（写法照 `operation.go` 的 `OperationLabel`）。
func TestTheProgressBarHasAName(t *testing.T) {
	tt := progressList(t, ui.LightTheme(), progressSong(30*time.Second, 60*time.Second))

	want := progressLabel("一首歌")
	if _, ok := tt.Find(want); !ok {
		t.Errorf("画面上找不到那条进度条（名字 %q），文字有 %q", want, tt.Texts())
	}
}

// TestTheProgressBarIsAsWideAsItWas 钉住「**宽度没变**」。
//
// 框架那一版**只给高度**（`coreProgress` 里没有宽度），不给宽的话它按内容算宽
// ——而它没有内容，宽就是 0，**整条看不见**。所以这一条钉住「我们给了 120」。
func TestTheProgressBarIsAsWideAsItWas(t *testing.T) {
	tt := progressList(t, ui.LightTheme(), progressSong(30*time.Second, 60*time.Second))

	bar, ok := tt.Find(progressLabel("一首歌"))
	if !ok {
		t.Fatalf("画面上找不到那条进度条，文字有 %q", tt.Texts())
	}
	if bar.W != progressBarW {
		t.Errorf("那条进度条宽 %v，want %v（框架那一版不自己定宽，不给就画不出来）",
			bar.W, progressBarW)
	}
}

// TestTheProgressBarFillsProportionally 钉住「**值对不对**」。
//
// 判据是画面上那一段**主色填充**有多长：它占整条的比例就是看到了哪儿。
//
// ⚠️ **量的是「主色那一段的宽度」，不是「和底色不同的那一段」**：轨道那一档
// （`t.Border`）和底色也不同，所以差集量出来的是整条。
func TestTheProgressBarFillsProportionally(t *testing.T) {
	for _, c := range []struct {
		name             string
		progress, total  time.Duration
		wantFillFraction float64
	}{
		{"一半", 30 * time.Second, 60 * time.Second, 0.5},
		{"四分之一", 15 * time.Second, 60 * time.Second, 0.25},
		{"满了", 90 * time.Second, 60 * time.Second, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			th := ui.LightTheme()
			th.Accent = ui.Hex("#1ed760") // Biu 的绿（应用的默认主色）
			tt := progressList(t, th, progressSong(c.progress, c.total))

			bar, ok := tt.Find(progressLabel("一首歌"))
			if !ok {
				t.Fatalf("画面上找不到那条进度条，文字有 %q", tt.Texts())
			}
			fill := barFillWidth(t, tt, bar, th.Accent)
			want := float32(c.wantFillFraction * progressBarW)
			// 允许 1.5 像素：填充那一段的边界是抗锯齿出来的。
			if d := fill - want; d > 1.5 || d < -1.5 {
				t.Errorf("填充那一段宽 %.1f，want %.1f（%.0f%%）——值没对上",
					fill, want, c.wantFillFraction*100)
			}
		})
	}
}

// TestTheProgressBarIsTheFrameworkOne 钉住「这一条**就是框架那一枚**」。
//
// 判据是**它的高度**：框架的 `ui.Progress` 高 `t.Space(1.5)`（= 6，默认间距 4），
// 而手搓的那一版是 **3**。
//
// ⚠️ **这一条钉的是一次故意的视觉变更**（票 #703 的 C 项：「这一项要按视觉变更
// 处理」）：框架那一版比手搓的粗一倍，轨道色也从 `t.Text.Alpha(0.1)` 换成了
// `t.Border`。所以**要钉住它现在长这样**——不然哪天有人「看着太粗」把它调回 3，
// 那一处就和全应用另外两处进度条（更新弹窗、下载列表）不是一套外观了。
//
// ⚠️ **量的是盒子高度，不是「有几行不是底色」**：抗锯齿会让边界那一行只差几个
// 灰阶，量「不是底色」会多出两行（实测 6 高量出 6 行，而手搓那版 3 高量出 3 行
// ——这次刚好对得上，但那是运气：手搓那版没有圆角抗锯齿，框架这版有）。
func TestTheProgressBarIsTheFrameworkOne(t *testing.T) {
	th := ui.LightTheme()
	th.Accent = ui.Hex("#1ed760")
	tt := progressList(t, th, progressSong(30*time.Second, 60*time.Second))

	bar, ok := tt.Find(progressLabel("一首歌"))
	if !ok {
		t.Fatalf("画面上找不到那条进度条，文字有 %q", tt.Texts())
	}
	want := th.Space(1.5)
	if bar.H != want {
		t.Errorf("那条进度条高 %v，want %v（框架的 ui.Progress 是 t.Space(1.5)）",
			bar.H, want)
	}
}

// TestTheProgressBarDoesNotPushTheRowApart 钉住「**行高没变**」。
//
// 这一列是「歌名 + UP 名 + 进度条」三段，而框架那一版的进度条高 6（改之前是 3）——
// 所以要确认这一行**还是 64 高**（`card.go` 的 `listRowHeight`），下一行还是从
// y=64 开始。
//
// ⚠️ **量的是第二行那一行的上边缘**：虚拟列表按行摆，两行之间是 64 就说明行高
// 没被内容撑开。
func TestTheProgressBarDoesNotPushTheRowApart(t *testing.T) {
	th := ui.LightTheme()
	th.Accent = ui.Hex("#1ed760")
	l := New(newCache(t), Actions{})
	l.SetSongs([]Music{
		{ID: "a", Title: "第一首", Owner: "甲", Duration: time.Minute, Progress: 30 * time.Second},
		{ID: "b", Title: "第二首", Owner: "乙", Duration: time.Minute, Progress: 30 * time.Second},
	})
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(th)
		l.View(c)
	}, testerW, 200)
	tt.Frame()

	first, ok := tt.Find(progressLabel("第一首"))
	if !ok {
		t.Fatalf("画面上找不到第一行那条进度条，文字有 %q", tt.Texts())
	}
	second, ok := tt.Find(progressLabel("第二首"))
	if !ok {
		t.Fatalf("画面上找不到第二行那条进度条，文字有 %q", tt.Texts())
	}
	if d := second.Y - first.Y; d != listRowHeight {
		t.Errorf("第二行比第一行低 %v，want %v（行高被进度条撑开了？）", d, listRowHeight)
	}
}

// TestNoProgressMeansNoBar 钉住「没有进度（或者没有时长）时**一个元素都不建**」。
//
// 这一条是原来那一版就有的行为，换成框架那一版不能丢：画一条空的进度条出来，
// 用户看到的是「这一首听完了」，而它其实是「这一页没给进度」。
func TestNoProgressMeansNoBar(t *testing.T) {
	for _, c := range []struct {
		name            string
		progress, total time.Duration
	}{
		{"没有进度", 0, 60 * time.Second},
		{"没有时长", 30 * time.Second, 0},
		{"两个都没有", 0, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			tt := progressList(t, ui.LightTheme(), progressSong(c.progress, c.total))
			if _, ok := tt.Find(progressLabel("一首歌")); ok {
				t.Errorf("没有进度却画了一条进度条：%q", tt.Texts())
			}
		})
	}
}

// barFillWidth 量画面上那一段**主色填充**有多宽。
//
// ⚠️ **扫的是那一行里主色最靠右的那一段**：进度条是横向的，主色那一段从左边
// 开始、到「看到了哪儿」为止，所以它的**右边缘**就是那个位置。
func barFillWidth(t *testing.T, tt *ui.Tester, bar ui.Rect, accent ui.Color) float32 {
	t.Helper()
	img := tt.Image()
	want := color.RGBA{R: accent.R, G: accent.G, B: accent.B, A: accent.A}
	right := -1
	for x := int(bar.X); x < int(bar.X+bar.W); x++ {
		for y := int(bar.Y); y < int(bar.Y+bar.H); y++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			if colorDiff(img.RGBAAt(x, y), want) <= 12 {
				right = x
				break
			}
		}
	}
	if right < 0 {
		t.Fatalf("那条进度条上一个主色的像素都没有（%v）", want)
	}
	return float32(right) - bar.X + 1
}
