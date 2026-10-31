package ui

// 顶栏那一枚返回键在**首页**上的行为（票 #706 的第一个 bug）。
//
// # 现象（实测，两边对比）
//
//	1. 点侧栏「本地音乐」   → path=/local-music
//	2. 点侧栏「推荐音乐」   → path=/        （路由历史里还留着 /local-music）
//	3. 按顶栏「后退」
//	   分支：path 仍是 "/"     ← **退不回去**
//	   基线：path 变成 "/local-music"  ← 退得回去
//
// # 根因
//
// `navbar.go` 的 `viewNavBack` 在首页上把框架的 `ui.BackButton`（走 `r.Go(-1)`）
// 换成了自绘的 `viewHomeBack`（只做「回发现总览」）⇒ **路由历史那条路被整个拿
// 掉了**。
//
// # 为什么是问题
//
// 用户在别的页面 → 回首页 → 想「后退」回刚才那个页面，**按顶栏那枚没反应**。而那
// 枚键画出来是**亮着的**（`canGo := !s.home.overview`）——用户会以为它坏了。
//
// 而需求只是「在首页上加一个『回发现总览』」，**并没有要求替换掉退历史**。
//
// # 这一族断的是「**两件事并存**」
//
//	路由历史退得回去 ⇒ 那一枚**退路由历史**（框架那一支）；
//	路由历史退不回去 ⇒ 那一枚**回发现总览**（自绘那一支）。
//
// ⚠️ **全部走内存里的假 B 站**（AGENTS.md §6）：一个真接口都不打。

import (
	"testing"
)

// TestTheTopBarBackKeyStillGoesBackInHistoryOnTheHomePage 钉住「在首页上按后
// 退，**退得回路由历史**」。
//
// 🔴 **这一条是那个 bug 的正题**：改之前首页上那一枚被换成了「回总览」，于是路
// 由历史那条路**整个没了**。
//
// ⚠️ **判据是路径**（`s.router.Path()`）——而「能不能退」那个凭据是
// `s.router.CanGoBack()`。
func TestTheTopBarBackKeyStillGoesBackInHistoryOnTheHomePage(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	// 切到「本地音乐」去：它有自己的一条路由（不进菜单参数路由，稳）。
	if err := tt.Click("本地音乐"); err != nil {
		t.Fatalf("点不到侧栏的「本地音乐」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Path(); got != localMusicPath {
		t.Fatalf("切到本地音乐失败，在 %q", got)
	}

	// 回首页：路由历史里**留着**「本地音乐」那一条。
	if err := tt.Click("推荐音乐"); err != nil {
		t.Fatalf("点不到侧栏的「推荐音乐」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Path(); got != homePath {
		t.Fatalf("回首页失败，在 %q", got)
	}
	// ⚠️ **前提**：路由历史退得回去（不然这一条就退化成「退不回去那一档」）。
	if !s.router.CanGoBack() {
		t.Fatalf("首页上路由历史退不回去（CanGoBack=false）——" +
			"这一条的前提不成立（历史里应该有「本地音乐」那一条）")
	}

	// 按顶栏那枚返回键。
	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("顶栏上没有后退，文字有 %q", tt.Texts())
	}
	tt.ClickAt(back.X+back.W/2, back.Y+back.H/2)
	tt.Frame()

	// 🔴 **退回去了**：路径变成「本地音乐」那一条。
	if got := s.router.Path(); got != localMusicPath {
		t.Errorf("在首页上按后退之后在 %q，want %q——"+
			"那一枚键在首页上也得**退路由历史**（用户在别的页面 → 回首页 → 想后退"+
			"回刚才那个页面）", got, localMusicPath)
	}
}

// TestTheTopBarBackKeyGoesBackToTheOverviewWhenThereIsNoHistory 钉住「路由历史
// **退不回去**的时候，那一枚改成『回发现总览』」。
//
// ⚠️ **这一条是「两件事并存」的那一半**：退不回去的时候没有历史可退，而用户还
// 是要能回总览（那是票 #706 的需求）。
//
// ⚠️ **判据是路径**：`backToHomeOverview` 是**页内切换**，路由一个字都不动——
// 所以路径**不变**，而**画面上**总览回来了。
func TestTheTopBarBackKeyGoesBackToTheOverviewWhenThereIsNoHistory(t *testing.T) {
	startOnOverview(t)
	s, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "排行榜") })

	// 进「热榜」那一档。
	if err := tt.Click("热榜"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "进了热榜那一档", func() bool {
		return showsText(tt, "鬼畜") && !showsText(tt, "排行榜")
	})

	// ⚠️ **前提**：路由历史退不回去（首页只有 `/` 一条路由，而榜单那一档是**页
	// 内切换**，压根没 push 过路由）。
	if s.router.CanGoBack() {
		t.Fatalf("首页上路由历史退得回去（CanGoBack=true）——" +
			"这一条的前提不成立")
	}

	// 按顶栏那枚返回键 ⇒ 回总览。
	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("顶栏上没有后退，文字有 %q", tt.Texts())
	}
	tt.ClickAt(back.X+back.W/2, back.Y+back.H/2)
	tt.Frame()

	waitFrames(t, tt, "回到总览", func() bool {
		return showsText(tt, "排行榜") && !showsText(tt, "鬼畜")
	})
	// ⚠️ **路由一个字都不许动**：回总览是**页内切换**。
	if got := s.router.Path(); got != homePath {
		t.Errorf("回总览之后路径变成了 %q，want %q——回总览是页内切换，不动路由",
			got, homePath)
	}
}
