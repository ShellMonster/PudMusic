package ui

// 迷你播放器这一组共用的**小工具**：建外壳、开 Tester、往队列里放歌、量像素。
//
// 为什么单独一个文件：版面那两条测试（`minilayout_test.go`）和窗口 / 状态同步
// 那些（`miniplayer_test.go`）都要用同一批，而同包的两个测试文件必须一起编译
// 得过——所以这些 helper 落在第三个文件里，好让提交能按「先工具、再一边」拆开。
//
// ⚠️ 这些函数全部在**界面线程**上跑（`NewTester` 的 view 只跑在主线程），除了
// `enqueueTwo` 里的 `a.Enqueue` 会同步开一次播放器。

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// newMiniShell 建一扇迷你播放器的外壳，挂在它自己的 App 上。
//
// 为什么不来一个 `newShellWidth` 那样的公共货：那边建的是**主窗口**（`New` +
// `s.View`，那一帧是导航 + 侧栏 + 页面 + 播放条）。这里要的是 `NewMini` 那一套，
// 而且迷你窗口自己那份进度条状态（`miniScrub`/`miniHeld`）只有走 `NewMini` 才有意义。
func newMiniShell(t *testing.T, toggle func()) (*Shell, *app.App) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	// ⚠️ **要先有一个主窗口的外壳**：`NewMini` 的第二个参数就是它——快捷键和搜索
	// 历史接的是**它那一份**（`adoptShared`）。而主窗口那个外壳自己开的快捷键要
	// 挂上盘，否则这一组测的就不是「用户配过的那一份」而是出厂默认值了
	// （`TestTheMiniWindowUsesTheUsersShortcuts` 正是靠这一点才抓得住那个缺陷）。
	// 走主干的 `newTestShell`：收尾无条件挂在 `Shell.Close` 上，直接 `New(a)` 会被
	// `shelltest_test.go` 那一条揪出来。
	main := newTestShell(t, onApp(a)).s

	s := NewMini(a, main, toggle)
	t.Cleanup(s.closePerWindow)
	s.router.Transition = mygoui.TransitionNone
	return s, a
}

// miniTester 开一扇 320×100 的 Tester，跑迷你窗口这一窗的 view。
func miniTester(s *Shell) *mygoui.Tester {
	return mygoui.NewTester(s.View, miniWidth, miniHeight)
}

// enqueueTwo 往队列里放两首**能真的出声**的本地文件，第一首开始放。
//
// ⚠️ **两首必须是两个不同的文件**：本地文件的歌名是取自**文件本身**的
// （`App.Open` 里 `a.title = 文件名去掉后缀`），不是队列项上那个 `Title`——
// 两首同一个文件的话界面上两首都叫同一个名字，而这一组好几条测试正是靠歌名分
// 「换没换歌」。
func enqueueTwo(a *app.App, t *testing.T) error {
	t.Helper()
	requireTestAudio(t)
	if err := a.Enqueue(app.Item{
		Source: "local", Type: "local", Path: testFLAC, Title: "第一首",
	}); err != nil {
		return err
	}
	return a.Enqueue(app.Item{
		Source: "local", Type: "local", Path: testMONO, Title: "第二首",
	})
}

// enqueueTwoWithCover 是 `enqueueTwo` 加上一张封面。
//
// ⚠️ **带封面才是紧的那一档**：封面占掉左边 64 宽，右边那一列从 308 缩到 244。
// 量版面的那两条测试必须走这一支（`app.Item.Cover` 是唯一一处封面地址的来源——
// 本地文件本身不读内嵌的图，见 `app.State.CoverURL` 的注释）。
func enqueueTwoWithCover(a *app.App, t *testing.T) error {
	t.Helper()
	requireTestAudio(t)
	g := newGatedCover(t)
	g.open()
	if err := a.Enqueue(app.Item{
		Source: "local", Type: "local", Path: testFLAC, Title: "第一首", Cover: g.url(),
	}); err != nil {
		return err
	}
	return a.Enqueue(app.Item{
		Source: "local", Type: "local", Path: testMONO, Title: "第二首",
	})
}

// progressRect 量迷你窗口上那条进度条的框（版面那两条测试用它当「右边那一列从
// 哪儿开始」的尺子）。
func progressRect(t *testing.T, tt *mygoui.Tester) mygoui.Rect {
	t.Helper()
	r, ok := tt.Find("播放进度")
	if !ok {
		t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
	}
	return r
}

// flacBaseName / monoBaseName 是那两首在界面上各自显示的歌名（文件名去掉后缀）。
// 写在测试里而不是现算，是为了让「断的是哪一个名字」一眼能对上。
const (
	flacBaseName = "stereo16"
	monoBaseName = "mono16"
)

// requireTestAudio 在素材不在的时候把整条测试跳过。
//
// 不用「让测试红成一句看不懂的『打不开文件』」：那一句话说的是被测逻辑坏了，而
// 实际上是这台机器上根本没有那份 FLAC。
func requireTestAudio(t *testing.T) {
	t.Helper()
	if testFLAC == "" || testMONO == "" {
		t.Skip("找不到 FLAC 测试素材，这一组要真的能出声（本地文件必须真的打开）")
	}
}

// testFLAC / testMONO 是这一组用的两个能出声的本地文件。
//
// 素材不在就整组跳过，而不是让测试红成一句看不懂的「打不开文件」。
var testFLAC, testMONO = func() (string, string) {
	dir := filepath.Join("..", "service", "audio", "flacdec", "testdata")
	a, b := filepath.Join(dir, "stereo16.flac"), filepath.Join(dir, "mono16.flac")
	if _, err := os.Stat(a); err != nil {
		return "", ""
	}
	if _, err := os.Stat(b); err != nil {
		return "", ""
	}
	return a, b
}()

// miniPixel 取一个像素（用 `snapshot` 先复制一份，免得被下一帧覆盖掉）。
func miniPixel(img *image.RGBA, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.RGBAAt(x, y)).(color.RGBA)
}

// miniIsDark 报一个颜色是不是暗底（按亮度，不是按某一根通道）。
func miniIsDark(c color.RGBA) bool {
	return 0.299*float64(c.R)+0.587*float64(c.G)+0.114*float64(c.B) < 110
}

// miniNaturalWidths 量每一串字在**不设限**的一行里各占多宽，也就是它们的自然宽度。
//
// 字号和内边距照迷你窗口里那几个键写死（13 号、`Padding(2, 6)`），量出来的那个框
// 就是「那几个字本身要多少」。
func miniNaturalWidths(t *testing.T, labels []string) map[string]float32 {
	t.Helper()
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Column(c).Width(miniWidth).Children(func() {
			for _, l := range labels {
				mygoui.Button(c, l).FontSize(13).Padding(2, 6).MaxLines(1)
			}
		})
	}, miniWidth, 200)
	tt.Frame()

	out := make(map[string]float32, len(labels))
	for _, l := range labels {
		r, ok := tt.Find(l)
		if !ok {
			t.Fatalf("连不设限的一行都量不出 %q 的宽度", l)
		}
		out[l] = r.W
	}
	return out
}

// settleMini 跑到 cond 成立为止。每一轮走一帧，顺带让后台那一支有机会跑。
//
// ⚠️ 不能只 `tt.Frame()` 一次：播放器的时长和解码都在后台 goroutine 里，结果要靠
// 唤醒才进下一帧。
//
// 名字带 `Mini` 是因为包级那个通用的 `settle` 已经被主干收走了（`4f86d76` 那一票
// 整理过测试套件），而各页各有各的 `settleXxx`——这一份是迷你这一组自己的。
func settleMini(t *testing.T, tt *mygoui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了五秒还没%s，文字有 %q", what, tt.Texts())
		}
		time.Sleep(time.Millisecond)
	}
}
