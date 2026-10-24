package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
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

// TestTheLoadingKeyIsASpinner 盯播放/暂停那一枚在取流时是一枚转圈。
//
// ⚠️ **判据是它的无障碍名字**：转圈没有可见的字，而它是唯一能认出「这一枚
// 现在是转圈」的凭据（无头 Tester 也只认字和名字）。
func TestTheLoadingKeyIsASpinner(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		Loading: true, LoadingTitle: "取不来的那一首",
	}, 1000)

	if _, ok := tt.Find("正在取流"); !ok {
		t.Errorf("播放/暂停那一枚不是转圈：%q", tt.Texts())
	}
	// 平时那一枚是「播放」/「暂停」，取流时不许还摆着它——那会让用户以为
	// 「按一下就能出声」，而这时候按它什么也不会发生。
	//
	// ⚠️ **判据用 `Find`（逐字相等）而不是 `HasText`（子串）**：画面上还有
	// 「播放进度」那一个（进度条的无障碍名字），子串匹配会把它算进来。
	if _, ok := tt.Find("播放"); ok {
		t.Errorf("取流时那一枚还写着「播放」：%q", tt.Texts())
	}
	if _, ok := tt.Find("暂停"); ok {
		t.Errorf("取流时那一枚还写着「暂停」：%q", tt.Texts())
	}
}

// TestAFailedPlayGivesASentenceInsteadOfASpinner 盯「失败之后是一句话，不是一直转圈」。
//
// 这一条是票 #282 的验收：坏的那一首要么能出声，要么**给一句能看懂的话**，
// **不许无限转圈**。
func TestAFailedPlayGivesASentenceInsteadOfASpinner(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		Err: "取流超时了（15s 内没有开始出声）。可能是网络慢，或者这一首的流取不到——换一首放，或者点重试再试一次。",
	}, 1000)

	if tt.HasText("正在取流") {
		t.Errorf("失败了还在转圈：%q", tt.Texts())
	}
	if !tt.HasText("取流超时了") {
		t.Errorf("失败之后界面上没有那句话：%q", tt.Texts())
	}
}
