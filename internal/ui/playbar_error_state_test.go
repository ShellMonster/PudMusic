package ui

// 播放条上的**出错那一档**（票：左下角出错那三行太碎）。
//
// # 原来的形态（要改掉的）
//
// 歌名 / 作者名 / **「打不开：……」** ——三行竖着堆在 76 高的播放条里，而第三行
// 只有 **75.7 像素**宽（实测 1200 宽的窗口）。一句 26 个字的话在屏幕上只剩十来个
// 字，剩下那一截挤成碎片；而它后面还紧跟一枚 62 像素宽的「在 B 站打开」按钮。
//
// 出图看得很清楚：红色那一句被压成一小截，和上面两行挤在一起。
//
// # 改成的形态
//
//  1. **出错那一行写的是失败的那一首**（不是正在放的那一首）——见
//     `State.FailedTitle`；
//  2. **错误文案不再占那一行**：它变成播放键上的一枚**红色的**键（出错那一
//     档），而「能做的事」（重试 / 在 B 站里听它）在它的「详情」面板里；
//  3. 那一行**只留一句**——错误详情进悬停提示和「详情」面板（那是既有设施）。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestTheErrorLineIsNotACrushedThirdLine 盯**那一列出错时不是三行**。
//
// 🔴 **判据是「那一句在不在画面上」**：改之前错误文案是那一列的第三行，而它
// 只有 75.7 像素宽——一句长话上屏只剩碎片。
//
// ⚠️ **判据是「画面上有没有那句完整的话」而不是「有几行」**：行数不是用户
// 看得见的差别，「那句话被截成碎片」才是。
func TestTheErrorLineIsNotACrushedThirdLine(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "新·与天齐",
		Artist: "指尖笑", ArtistMid: 1,
		Err: "B 站拒绝了这次请求",
	}, 1200)

	// 那一列上**不该**有被截碎的错误文案。
	for _, s := range tt.Texts() {
		if len(s) > 4 && s[:4] == "打不开" {
			t.Errorf("画面上还有那一句被截碎的错误文案 %q——它该从那一列里"+
				"搬走（那一列只有 75.7 像素宽，26 个字只剩十来个）", s)
		}
	}
}

// TestTheFailedSongIsWhatTheColumnShows 盯**那一列写的是失败的那一首**。
//
// 🔴 **这是这一票最要紧的一条**：出错那一列本来显示的是**正在放的那一首**——
// 用户读到的是「这一首正在放，但它打不开」，而打不开的其实是另一首。
func TestTheFailedSongIsWhatTheColumnShows(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "第一首",
		Artist: "第一位作者", ArtistMid: 1,
		FailedTitle: "第二首", FailedArtist: "第二位作者",
		Err: "B 站拒绝了这次请求",
	}, 1200)

	if !tt.HasText("第二首") {
		t.Errorf("那一列上没有失败的那一首 %q，文字有 %q", "第二首", tt.Texts())
	}
	if !tt.HasText("第二位作者") {
		t.Errorf("那一列上没有失败那一首的作者 %q，文字有 %q", "第二位作者", tt.Texts())
	}
}

// TestThePlayKeyTurnsIntoAnErrorKey 盯**播放键在出错时变成「出错了」那一枚**。
//
// 🔴 **这是「哪出错了」的那一半**：错误文案搬走了，「出错了」这个信号就
// 得长在**出问题的那个东西**上——而那就是播放键。
//
// ⚠️ **它必须可点**：出错时播放器压根没装上（`HasTrack` 可能是 true，但那是
// **上一首**在响），所以播放键那一枚是**禁用态**——用户看到它变红想按它重试，
// **按不动**。
func TestThePlayKeyTurnsIntoAnErrorKey(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "第一首", Artist: "第一位作者", ArtistMid: 1,
		FailedTitle: "第二首",
		Err:         "连不上 B 站，检查一下网络",
		CanRetry:    true,
	}, 1200)

	if !tt.HasText(errPlayKeyLabel) {
		t.Errorf("出错时播放键没有变成那个出错的键，文字有 %q", tt.Texts())
	}
}

// TestThePlayKeyIsNotAnErrorKeyWhenNothingFailed 盯**没出错时播放键照旧**。
//
// ⚠️ **这一条是上面那一条的反面**：把播放键无条件改成「重试」，那界面在
// 正常播放时显示的是一枚「重试」键——那不是「出错态」，是「永远在出错」。
func TestThePlayKeyIsNotAnErrorKeyWhenNothingFailed(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "一首歌", Artist: "某个 UP 主", ArtistMid: 1,
	}, 1200)
	if tt.HasText(errPlayKeyLabel) {
		t.Errorf("没出错却有一个出错的键，文字有 %q", tt.Texts())
	}
	if !tt.HasText("播放") && !tt.HasText("暂停") {
		t.Errorf("没出错时播放键不见了，文字有 %q", tt.Texts())
	}
}

// TestAnUnretryableErrorStillShowsOnThePlayKey 盯**不可重试的那一档**。
//
// 🔴 **不可重试的错不给「重试」键**（`bili.Recoverable` 那张表）：给一个按了
// 也没用的重试比不给更糟——用户按两次发现还是那样，就不再信这个应用了。
//
// ⚠️ **但那一档仍然要说「出错了」**：错误文案搬走之后，播放键就是那个信号——
// 所以它要**变色**（危险色），只是不点。
func TestAnUnretryableErrorStillShowsOnThePlayKey(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "第一首", Artist: "第一位作者", ArtistMid: 1,
		FailedTitle: "第二首",
		Err:         "B 站拒绝了这次请求",
		CanRetry:    false,
	}, 1200)

	if !tt.HasText(errPlayKeyLabel) {
		t.Errorf("不可重试的那一档连「出错了」都没说，文字有 %q", tt.Texts())
	}
}
