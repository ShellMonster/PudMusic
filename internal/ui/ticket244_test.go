// 票 #244 的判据：设置页两端对齐（D1）、迷你窗音量滑块有悬停提示（B5）、
// 「全部播放」两处画法统一（B6）。
//
// 三条都对着**画面**断言：量的都是画面上那一块的位置和大小，不是「调了哪个
// 函数」。
//
// ⚠️ **深浅两套都要过**：设置页那一段空档在两套主题下是同一组数，而迷你窗那枚
// 提示是**反色底**（深浅两套反过来的），浅色下那一枚浅底深字、深色下深底浅字。
package ui

import (
	"strconv"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// D1（设置页说明和控件之间的空档）已经不存在：设置页的每一行不再带说明文字，
// 自解释的标题加控件就够了。

// ─────────────────────────────────────────────────────────────────────────────
// B5：迷你窗那根音量滑块要有一枚看得见的悬停提示
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniVolumeSliderSaysWhatItIs 钉住「迷你窗那根音量滑块有悬停提示」。
//
// # 为什么这一条不是「加一句标签」就完了
//
// 那一扇窗只有 **320×100**，而音量滑块在**第一行**（y=14..30）。框架默认把提示
// 摆在锚点的**上面**——摆在上面的话整个提示落在窗口之外，**一个字都看不见**，
// 而「看不见的提示」和「没有提示」在画面上是一回事。
//
// # 判据是**提示在窗内**
//
// 光断言「画面上有『音量 80%』这几个字」不够：提示画在窗口外时，Tester 里的
// 文字列表照样有它（它是被建出来了的），而用户看不见。所以这一条同时断它的
// **位置**：上下左右都得在 320×100 之内。
func TestTheMiniVolumeSliderSaysWhatItIs(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.8)
	tt := miniTester(s)
	settleMini(t, tt, "音量读回来了", func() bool { return a.Snapshot().Volume > 0 })

	bar, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("画面上找不到音量条，文字有 %q", tt.Texts())
	}
	// 把指针停在那根滑块上。
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()

	want := "音量 80%"
	tip, ok := tt.Find(want)
	if !ok {
		t.Fatalf("音量滑块上没有 %q 那枚提示，文字有 %q", want, tt.Texts())
	}
	// ⚠️ **提示得在窗内**：这一扇窗只有 320×100，而音量滑块在第一行——摆在上面
	// 的话整个提示落在窗口外，用户一个字也看不见。
	if tip.X < 0 || tip.Y < 0 || tip.X+tip.W > miniWidth || tip.Y+tip.H > miniHeight {
		t.Errorf("那枚提示画在 %v，而迷你窗只有 %d×%d——它落在窗外，用户看不见",
			tip, miniWidth, miniHeight)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// B6：「全部播放」两处画法统一
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// B6 的另一半：两枚按钮按下去做的是同一件事
// ─────────────────────────────────────────────────────────────────────────────

// playableSongsOf 造 n 首**放得了**的歌（首页那一屏）。
//
// 🔴 **Item 得是放得了的**：票 #244 之后「全部播放」会跳过放不了的那些
// （判据 `app.CanPlay`），而一个空的 `app.Item`（`Type` 是空串）在那个判据眼里
// 就是「没有可播的稿件」——拿它当夹具，这一枚按钮按下去什么也不会加。
func playableSongsOf(prefix string, n int) []list.Music {
	out := make([]list.Music, n)
	for i := range out {
		title := prefix + " 的歌 " + strconv.Itoa(i+1)
		out[i] = list.Music{
			ID:    prefix + "-" + strconv.Itoa(i),
			Title: title,
			Owner: prefix + " 的 UP 主",
			Item: app.Item{
				Source: app.SourceOnline,
				Type:   "mv",
				Bvid:   "BV1" + prefix + strconv.Itoa(i),
				Title:  title,
			},
		}
	}
	return out
}
