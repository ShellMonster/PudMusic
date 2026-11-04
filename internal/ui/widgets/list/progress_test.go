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
