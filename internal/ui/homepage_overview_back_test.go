package ui

// 回总览那一档的**状态复位**（票 #706 的第二个 bug）。
//
// # 现象（实测）
//
//	1. 总览 → 点「热榜」进榜单那一档
//	2. 点另一档触发横滑，推进 25ms（滑到一半）
//	3. 按顶栏「后退」回总览
//	   ⇒ overview=true  swap.phase=1(homePagerSlide)  shown=3  want=1
//	4. 再推 700ms + 1s
//	   ⇒ swap.phase=1  ← **永远卡着**
//
// # 根因
//
// `backToOverview()` 只设 `h.overview = true`，**不重置 `h.swap`**。而
// `syncPager` 的 slide 分支只在 `h.want == h.shown` 时才把 `swap` 清掉——
// 回总览时 `want`(1) ≠ `shown`(3)，所以**不取消**。
//
// # 后果
//
//   - `syncPager` 每次进 slide 分支都会走 `c.AnimationFrame()` ⇒ **界面一直要帧**
//     （空转耗电）；
//   - 那一段还**每帧画两档**（oldTab/newTab 都 ≥0），比稳态多一倍分配；
//   - 好在 `view()` 里 `h.overview` 为真时走 `viewOverview` 分支（早退），所以
//     **画面上没坏**；但 `swap.phase` 卡住后**没有任何东西会再推进它** ⇒ 一次误触
//     之后那一档的 `shown` **永远停在旧档**，直到用户再点一次 Tab。
//
// ⚠️ **全部走内存里的假 B 站**（AGENTS.md §6）：一个真接口都不打。

import (
	"testing"
	"time"
)

// TestGoingBackToTheOverviewCancelsTheSlide 钉住「回总览会**取消**那一段横滑」。
//
// 🔴 **这一条是那个 bug 的正题**：`swap.phase` 必须回到 `homePagerIdle`。
//
// ⚠️ **判据是状态，不是画面**：画面上那一帧看不出差别（`view()` 在总览时早退），
// 而**状态卡住**的后果是「界面一直要帧」和「那一档永远停在旧档」。
func TestGoingBackToTheOverviewCancelsTheSlide(t *testing.T) {
	startOnOverview(t)
	s, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "排行榜") })

	// 进「热榜」那一档。
	if err := tt.Click("热榜"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "进了热榜那一档", func() bool { return showsText(tt, "鬼畜") })

	// ⚠️ **触发一次横滑**（点另一档），然后推到**一半**。
	//
	// ⚠️ **推 25ms**（不是推到走完）：走完之后 `swap` 自己就清了（`syncPager`
	// 那一支），**钉不住**这个 bug——要停在**半路上**。
	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[1], err, tt.Texts())
	}
	tt.Advance(25 * time.Millisecond)
	tt.Frame()

	// ⚠️ **确认真的在滑**：不然下面那句断言是「没滑过所以没卡」。
	if s.home.swap.phase != homePagerSlide {
		t.Fatalf("横滑没有开始（phase=%d，want %d）——"+
			"这一条要钉的是「滑到一半回总览」那一条路", s.home.swap.phase, homePagerSlide)
	}

	// 按顶栏「后退」回总览。
	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("顶栏上没有后退，文字有 %q", tt.Texts())
	}
	tt.ClickAt(back.X+back.W/2, back.Y+back.H/2)
	tt.Frame()

	// 🔴 **那一段横滑必须被取消**。
	if got := s.home.swap.phase; got != homePagerIdle {
		t.Errorf("回总览之后 swap.phase = %d（want %d，idle）——"+
			"那一段横滑还挂着 ⇒ 界面一直要帧（空转耗电），"+
			"而那一档的 shown 永远停在旧档",
			got, homePagerIdle)
	}
}
