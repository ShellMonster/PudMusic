package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	mygoui "github.com/egoist/mygo/ui"
)

// 播放条上的「正在取流」（票 #282）。
//
// # 这一组盯的是「点了歌一直不开始」时用户看到什么
//
// 用户实测：点了一首，**加载动画转了好几秒，始终没出声，也没有任何报错提示**。
// 改之前这一档在界面上**一个字都没有**——用户既不知道应用在干什么，也不知道
// 出了什么事。
//
// 改之后是两句：
//
//	「正在取流：<歌名>」	在等的这一段（灰字，不是红字——红字是「出错了」）
//	一句失败的话		等够了（阈值见 app.onlinePlayTimeout）
//
// 两条互斥：出错的那句优先——一句错比一枚转圈信息量大，而转圈还在转说明还有
// 希望。

// TestThePlayBarSaysItIsLoading 盯「正在取流」那一句话。
//
// 🔴 **判据是画面上有没有那句话**（不是「有没有转圈」）：转圈是动着的，
// 无头 Tester 抓不住它；而那句话是用户真正读到的东西（「它在准备」）。
func TestThePlayBarSaysItIsLoading(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		Loading: true, LoadingTitle: "取不来的那一首",
	}, 1000)

	if !tt.HasText("正在取流") {
		t.Errorf("播放条上没有「正在取流」：%q", tt.Texts())
	}
	// 说清是**哪一首**——用户刚点的和正在放的不是同一首时，光说「正在取流」
	// 是不够的。
	if !tt.HasText("取不来的那一首") {
		t.Errorf("不知道在取哪一首：%q", tt.Texts())
	}
}

// TestTheLoadingLineHasASpinner 盯「正在取流」那一行里有一枚转圈。
//
// ⚠️ **判据是它的无障碍名字**：转圈没有可见的字，而它是唯一能认出「这里在
// 等」的凭据（无头 Tester 也只认字和名字）。
func TestTheLoadingLineHasASpinner(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		Loading: true, LoadingTitle: "取不来的那一首",
	}, 1000)

	if _, ok := tt.Find("正在取流"); !ok {
		t.Errorf("「正在取流」那一行里没有转圈：%q", tt.Texts())
	}
}

// TestThePauseKeyIsStillThereWhileLoading 盯**取流期间还能按暂停**。
//
// 🔴 **这一条是评审挑出来的**：取流这一趟里**上一首还在放**（在线流是后台
// 建的），而把播放/暂停那一枚换成转圈的话，用户就按不了暂停了——迷你窗和
// 满屏播放器里还有，播放条上没有了。
func TestThePauseKeyIsStillThereWhileLoading(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "还在放的那一首", Playing: true,
		Loading: true, LoadingTitle: "取不来的那一首",
	}, 1000)

	if _, ok := tt.Find("暂停"); !ok {
		t.Errorf("取流时播放条上按不了暂停：%q", tt.Texts())
	}
	// 反过来：没在放的时候那一枚是「播放」，不能因为在取流就变成别的。
	tt2, _ := metaBarTester(t, app.State{
		Loading: true, LoadingTitle: "取不来的那一首",
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

// TestTheSlowLoadingLineSaysItIsSlow 盯「慢的时候界面上有一句能看懂的话」
// （票 #220）。
//
// 🔴 **这是这一票的验收项**：用户抱怨的是「取流非常慢」——而「正在取流：
// <歌名>」那句话从头到尾不变，他等三秒和等三十秒看到的都是同一句，于是
// 「慢」这件事在界面上**根本不存在**。
//
// 慢的时候屏幕上换成「取流很慢：<歌名>」：一样长（歌名那一截不变），但
// 多了一件**新**的事——用户第一次知道「它慢」。
func TestTheSlowLoadingLineSaysItIsSlow(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		Loading: true, LoadingTitle: "取不来的那一首",
		LoadingSlow: true, LoadingStage: "下音频", LoadingWait: 12 * time.Second,
	}, 1000)

	if !tt.HasText("取流很慢") {
		t.Errorf("慢的时候界面上没有说「慢」：%q", tt.Texts())
	}
	// 🔴 **歌名不许丢**：用户要知道自己在等的是哪一首。
	if !tt.HasText("取不来的那一首") {
		t.Errorf("慢的时候不知道在等哪一首：%q", tt.Texts())
	}
	// 🔴 **转圈还得在**：它还是「还在等」的凭据。
	if _, ok := tt.Find("正在取流"); !ok {
		t.Errorf("「正在取流」那一行里没有转圈：%q", tt.Texts())
	}
}

// TestTheSlowLineIsNoLongerThanTheNormalOne 盯**慢的那一句不比正常那句长**。
//
// 🔴 **播放条上那一行只有 140 像素宽**（1200 宽的窗口，实测），12 号字放得下
// 十来个字。「取流很慢：」和「正在取流：」一样是五个字——所以换成「慢」的
// 那一版**不花掉歌名那截的任何宽度**。要是慢的那句更长，挤掉的就是歌名，
// 而用户恰恰在等的时候最需要知道自己在等哪一首。
func TestTheSlowLineIsNoLongerThanTheNormalOne(t *testing.T) {
	var slow, normal float32
	mygoui.NewTester(func(c *mygoui.Context) {
		slow, _ = c.MeasureText(0, mygoui.Span{
			Text: "取流很慢：那一首", Size: scale.Font(c, scale.Size12)})
		normal, _ = c.MeasureText(0, mygoui.Span{
			Text: "正在取流：那一首", Size: scale.Font(c, scale.Size12)})
	}, 300, 60).Frame()

	if slow > normal {
		t.Errorf("慢的那一句更宽（%.1f > %.1f）——挤掉的是歌名那截", slow, normal)
	}
}
