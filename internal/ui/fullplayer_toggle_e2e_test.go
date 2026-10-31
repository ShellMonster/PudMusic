package ui

// 满屏播放器「点空白处切换界面显隐」的 E2E：从**开满屏**开始，到**界面回来**
// 为止，中间不许有替身把 ctx 吞掉。
//
// # 这一条和单元测试差在哪
//
// 单元测试量的是「藏起来之后画面上一个亮像素都不剩」。这一条量的是**用户
// 那一串动作连起来**：开满屏 → 点空白 → 界面没了 → 再点 → 界面回来了。
//
// 差别在于**中间那一步**：单元测试可以自己把状态拨过去（`s.fullHidden()`
// 那一层），而这一条**只**点。中间任何一环断了（探针没接上点击、状态没记
// 住、回来那一支没走），它就红——而那些在单元测试里各自都能绿。
//
// ⚠️ **全程假时钟**：满屏那一档的三秒是在 `s.clock` 上算的（见 `advanceFull`）。
//
// ⚠️ **一个字都不许打真 B 站**：夹具是本地假服务器（`uiExplore`）。

import (
	"testing"
	"time"
)

// TestTogglingTheFullPlayerInterfaceByClickingBlank 是这一票的 E2E 钉子。
//
// 用户那三下：
//
//	① 点播放条上那张封面 → 满屏开
//	② 点空白 → 界面（顶栏 + 底栏）没了
//	③ 再点空白 → 界面回来了
func TestTogglingTheFullPlayerInterfaceByClickingBlank(t *testing.T) {
	s, tt, fc := fullTester(t)
	// ⚠️ **歌词要关掉**：歌词是内容不是界面，界面藏起来之后歌词**还在**
	// （票 #236）——而这一条量的是「界面上还有没有亮像素」，歌词的字会搅浑。
	fullNoLyricsPrefs(t, s)

	// ① 开满屏。
	openFull(t, tt)
	tt.Move(fullToggleBlankX, fullToggleBlankY)
	tt.Frame()
	if n := whitePixels(tt); n == 0 {
		t.Fatal("满屏开了，界面上却一个亮像素都没有")
	}

	// ② 点空白 → 界面没了。
	//
	// ⚠️ **等它淡完**：淡出是 500ms 的弹簧（`ui.Smooth`），而 Tester 的钟
	// 站着不动——不推钟的话画面上仍然有字（正在淡出的按钮当然还有像素），
	// 看着像「没藏起来」。
	tt.ClickAt(fullToggleBlankX, fullToggleBlankY)
	tt.Frame()
	tt.Frame()
	advanceFull(s, tt, fc, time.Second)
	tt.Frame()
	if n := whitePixels(tt); n != 0 {
		t.Fatalf("点了空白之后界面上还有 %d 个亮像素 —— 界面没有藏起来", n)
	}
	// ⚠️ **状态也得是「藏着的」**：像素是画面上的事，状态是这一票的新机制，
	// 两条得一起钉——像素对了而状态错了的话，下一次点空白就露不出来了
	// （那正是「切换」断成单向的症状）。
	if !s.fullHidden() {
		t.Fatal("点了空白之后 `fullHidden()` 说不藏 —— 像素对了而状态错了")
	}
	// 而这一档是「**手动**藏的」——指针动不把它叫回来（那是另一档）。
	if !s.full.uiHiddenByUser {
		t.Fatal("点了空白之后「手动藏着」那一档没记上")
	}

	// ③ 再点空白 → 界面回来了。
	tt.ClickAt(fullToggleBlankX, fullToggleBlankY)
	tt.Frame()
	tt.Frame()
	advanceFull(s, tt, fc, time.Second)
	tt.Frame()
	if n := whitePixels(tt); n == 0 {
		t.Fatal("再点一次空白，界面没有回来 —— 这得是「切换」，不是「点一下就藏」")
	}
	if s.fullHidden() {
		t.Error("再点一次空白，`fullHidden()` 还说藏着 —— 这不是「切换」")
	}
	// 回来的那一版得**真的画出来了**：顶栏那两枚键得找得到。
	if _, ok := tt.Find("关闭全屏"); !ok {
		t.Errorf("界面回来了，可画面上找不到「关闭全屏」，文字有 %q", tt.Texts())
	}
}
