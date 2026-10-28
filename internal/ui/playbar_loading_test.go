package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	mygoui "github.com/egoist/mygo/ui"
)

// 播放条上的取流反馈（票 #282 立的「要说话」，票 #581 改成「光圈说话」）。
//
// # 这一组盯的是「点了歌一直不开始」时用户看到什么
//
// 取流这一趟里界面的反馈只有**播放键上那一圈光**（loadhalo_test 那一族断的是
// 光圈本身），播放条左下角**不写字**——那一行留给出错/重连/停住那三档。
// 等够了（阈值见 app 的 onlinePlayTimeout）上屏的是一句失败的话，不是一直转圈。

// TestThePlayBarStaysQuietWhileLoading 盯「取流的时候那一行不说话」（票 #581）。
//
// 🔴 **判据是「没有那句话」**：以前这里有「正在取流：<歌名>」（票 #282）和
// 「取流很慢」（票 #220），用户不要这两行——反馈集中在播放键的光圈上。
// 歌名和 UP 名照旧，位置不挪。
func TestThePlayBarStaysQuietWhileLoading(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "还在放的那一首", Loading: true,
	}, 1000)

	if tt.HasText("正在取流") || tt.HasText("取流很慢") {
		t.Errorf("取流的时候播放条上不该有取流的字：%q", tt.Texts())
	}
	// 上一首的歌名照旧：取流这一趟里正在放的还是它。
	if !tt.HasText("还在放的那一首") {
		t.Errorf("取流的时候歌名不见了：%q", tt.Texts())
	}
}

// TestThePauseKeyIsStillThereWhileLoading 盯**取流期间还能按暂停**。
//
// 🔴 **这一条是票 #282 的评审挑出来的**：取流这一趟里**上一首还在放**（在线流
// 是后台建的），而把播放/暂停那一枚换成转圈的话，用户就按不了暂停了——光圈
// 绕的是那一枚键，键本身照旧。
func TestThePauseKeyIsStillThereWhileLoading(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "还在放的那一首", Playing: true,
		Loading: true,
	}, 1000)

	if _, ok := tt.Find("暂停"); !ok {
		t.Errorf("取流时播放条上按不了暂停：%q", tt.Texts())
	}
	// 反过来：没在放的时候那一枚是「播放」，不能因为在取流就变成别的。
	tt2, _ := metaBarTester(t, app.State{
		Loading: true,
	}, 1000)
	if _, ok := tt2.Find("播放"); !ok {
		t.Errorf("取流时播放条上那一枚不是「播放」：%q", tt2.Texts())
	}
}

// TestAFailedPlayGivesASentenceInsteadOfASpinner 盯「失败之后是一句话，不是一直转圈」。
//
// 这一条是票 #282 的验收：坏的那一首要么能出声，要么**给一句能看懂的话**，
// **不许无限转圈**。
func TestAFailedPlayGivesASentenceInsteadOfASpinner(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		Err: "取流超时了", ErrDetail: "取流超时了（20s 内这一段网络没回）。可能是网络慢，也可能是这一首的流取不到——换一首放，或者点重试再试一次。",
	}, 1000)

	if tt.HasText("正在取流") {
		t.Errorf("失败了还在转圈：%q", tt.Texts())
	}
	if !tt.HasText("取流超时了") {
		t.Errorf("失败之后界面上没有那句话：%q", tt.Texts())
	}
}

// TestTheFailureSentenceFitsOnOneLine 盯**失败那句话一行放得下**。
//
// 🔴 **判据是「这句话本身有多宽」比「那一格有多宽」**（不是「字在不在」）：
// 播放条上那一行只有 140 像素宽（1200 宽的窗口，实测），12 号字放得下十来个
// 字。一句 50 个字的话上屏只剩「打不开：取流…」这样的碎片——那不是「一句能
// 看懂的话」（票 #282 的评审）。
//
// ⚠️ **完整那一句在悬停提示里**（`State.ErrDetail`），所以屏幕上短、提示里
// 全，两边合起来才是一句能看懂的话。
func TestTheFailureSentenceFitsOnOneLine(t *testing.T) {
	const short = "取流超时了"
	const detail = "取流超时了（20s 内这一段网络没回）。可能是网络慢，也可能是这一首的流取不到——换一首放，或者点重试再试一次。"
	tt, _ := metaBarTester(t, app.State{Err: short, ErrDetail: detail}, 1200)

	box, ok := tt.Find("打不开：" + short)
	if !ok {
		t.Fatalf("画面上没有那句话：%q", tt.Texts())
	}
	// 量一下这句话本身有多宽（和界面上用的是同一个字号）。
	var measured float32
	mygoui.NewTester(func(c *mygoui.Context) {
		measured, _ = c.MeasureText(0, mygoui.Span{
			Text: "打不开：" + short, Size: scale.Font(c, scale.Size12)})
	}, 300, 60).Frame()

	if measured > box.W {
		t.Errorf("「打不开：%s」宽 %.1f，而那一格只有 %.1f——放不下，屏幕上只剩碎片",
			short, measured, box.W)
	}
}
