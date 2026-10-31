package ui

// 首页/榜单的**返回键**这一族（票 #706）。
//
// # 这一票改了什么
//
// 首页的「发现」总览里点磁贴会进到**榜单那一档**（热榜 / 原创榜）。进到那一档
// 之后，界面上曾经有**两个返回键**：
//
//  1. **榜单名称左边那个**（`homepage.go` 的 `viewTabs`）：一枚画着左箭头的
//     图标键，名字叫「返回发现」，按它回到总览。
//  2. **顶栏那个**（`navbar.go` 的 `viewNavBack`）：框架的 `ui.BackButton`，
//     走的是**路由历史**（`r.Go(-1)`）。
//
// 🔴 **关键事实**：榜单页**从不 push 新路由**（首页只有 `/` 一条路由，
// `router.go` 的 `routes` 表第一行），所以**顶栏那个返回键在首页上基本是失效
// 的**——路由历史里压根没有上一条。
//
// 用户拍板：**顶栏那个做，榜单旁边的那个去掉**。也就是：
//
//   - 榜单名称左边那枚「返回发现」**删掉**；
//   - 顶栏那个返回键**保留**，而在首页（`/`）上按它 ⇒ **回到「发现」总览**。
//
// # 为什么顶栏那个在别的页面上不能改
//
// 搜索页、设置页、UP 空间……那些地方**确实有路由历史可退**，所以那枚键在那些
// 页面上仍然是「退回上一页」。本票只改**首页上**的行为。
//
// # 三条判据
//
//   1. 榜单那一档**没有**那枚「返回发现」了（画面上找不到它）；
//   2. 顶栏返回键在首页上按了 ⇒ 回到「发现」总览（总览的内容可见）；
//   3. 顶栏返回键在**别的页面**上仍是路由历史（别弄坏了）。
//
// ⚠️ **总览自己那一档**（用户还没进任何一档）按顶栏返回键：**没反应**。
// 理由：总览已经是最上面那一档了，而首页**从不 push 新路由**——路由历史里
// 没有上一条可退。于是那枚键在总览上是**禁用**的（灰的、点不动），
// 而在榜单那一档上是**可用的**（它有活干：回总览）。
//
// ⚠️ **判据用几何和文字，不用精确比色**：画面上那道边线是**半透明混色**画上去
// 的（实测画面上的值和主题的 `Border` 不相等），精确比色会扫不到。位置和
// 文字（`tt.HasText` / `tt.Find`）才是稳的。
//
// ⚠️ **全部走内存里的假 B 站**（AGENTS.md §6）：一个真接口都不打。

import (
	"testing"
)

// TestTheBoardTierHasNoBackToDiscoverKey 钉住「榜单那一档**没有**那枚
// 「返回发现」」。
//
// ⚠️ **判据是「画面上找不到那个名字」**：`Tester.Find` 认的是元素的无障碍名字
// （`mygo/ui/headless.go` 的 `Find`），而图标键的名字就是它那一行代码里写的
// 那个字符串——「返回发现」是独一份的（改动之后整个应用里再没有第二处用它）。
func TestTheBoardTierHasNoBackToDiscoverKey(t *testing.T) {
	startOnOverview(t)
	_, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "排行榜") })

	// 进「热榜」那一档。
	if err := tt.Click("热榜"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "进了热榜那一档", func() bool { return showsText(tt, "鬼畜") })

	// 🔴 **它不该在了**。
	if _, ok := tt.Find("返回发现"); ok {
		t.Errorf("榜单那一档上还有一枚「返回发现」——这一票已经把它拿掉了（"+
			"回总览这件事交给顶栏那一枚），两枚键会做同一件事，文字有 %q", tt.Texts())
	}
}

// TestTheTopBarBackKeyGoesBackToTheOverviewOnTheHomePage 钉住顶栏那枚返回键在
// 首页上**回到「发现」总览**。
//
// 这一条是这一票的正题：顶栏那枚键在首页上从「失效」（路由历史里没有上一条）
// 变成「回总览」。
func TestTheTopBarBackKeyGoesBackToTheOverviewOnTheHomePage(t *testing.T) {
	startOnOverview(t)
	s, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "排行榜") })

	// 进「热榜」那一档：总览没了，Tab 那一排出来了。
	if err := tt.Click("热榜"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "进了热榜那一档", func() bool {
		return showsText(tt, "鬼畜") && !showsText(tt, "排行榜")
	})

	// 按顶栏那枚返回键。
	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("顶栏上没有后退，文字有 %q", tt.Texts())
	}
	tt.ClickAt(back.X+back.W/2, back.Y+back.H/2)
	tt.Frame()

	// 🔴 **回到总览**：总览的内容回来了，榜单那一档的内容没了。
	waitFrames(t, tt, "回到总览", func() bool {
		return showsText(tt, "排行榜") && !showsText(tt, "鬼畜")
	})

	// ⚠️ **路由一个字都不许动**：回总览是**页内切换**，不是换页。
	// （理由见文件头：首页只有 `/` 一条路由，而这一档的进法是 `openTab`，
	// 压根没 push 过新路由。）
	if got := s.router.Path(); got != homePath {
		t.Errorf("回总览之后路径变成了 %q，want %q——回总览是页内切换，不动路由", got, homePath)
	}
}

// TestTheTopBarBackKeyStillGoesBackInHistoryOnOtherPages 钉住「顶栏那枚返回键
// 在**别的页面**上仍然是退路由历史」。
//
// ⚠️ **这是这一票最容易弄坏的地方**：改成「在首页上回总览」之后，那枚键在别的
// 页面上如果也变成「回总览」，用户就在搜索页上被弹回首页——而那些页面**确实
// 有历史可退**。
func TestTheTopBarBackKeyStillGoesBackInHistoryOnOtherPages(t *testing.T) {
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

	// 按顶栏那枚返回键 ⇒ 退回上一条路由（首页）。
	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("顶栏上没有后退，文字有 %q", tt.Texts())
	}
	tt.ClickAt(back.X+back.W/2, back.Y+back.H/2)
	tt.Frame()

	if got := s.router.Path(); got != homePath {
		t.Errorf("在别的页面上按后退之后在 %q，want %q——那枚键在那些页面上仍然是"+
			"退路由历史", got, homePath)
	}
}

// TestTheTopBarBackKeyDoesNothingOnTheOverviewItself 钉住「总览自己那一档按
// 顶栏返回键**没反应**」。
//
// 🔴 **为什么是「没反应」而不是「换个说法」**：总览已经是最上面那一档了，而
// 首页**从不 push 新路由**——路由历史里没有上一条可退。于是：
//
//   - 按它**不回退**（路由一个字都不动）；
//   - 总览**照样画着**（不回退、不清空、不弹提示）；
//   - 它是**禁用的**（框架那一支在不能退的时候就是 `Disabled(true)`）。
//
// ⚠️ **判据分两半**：**行为**（按了路由不动、总览还在）和**画面**（那枚键
// 画出来是淡的）。
//
// ⚠️ **画面那一半用「同一个图形的两档明暗对比」断，不用「比某个固定颜色」**：
// 那道边线和箭头都是**半透明混色**画上去的（实测画面上的值和主题的 `Border`
// 不相等），拿主题里的数去比颜色是扫不到的。而**同一枚图形在两档之间的
// 明暗差**是相对量——跟着主题走，浅色深色都成立。
//
// 实测（浅色）：总览那一档 71、榜单那一档 198 —— 差着一个量级。
func TestTheTopBarBackKeyDoesNothingOnTheOverviewItself(t *testing.T) {
	startOnOverview(t)
	s, tt, _ := newHomeShell(t, homeDiscoverFake(t))
	waitFrames(t, tt, "总览", func() bool { return showsText(tt, "排行榜") })

	before := s.router.Path()
	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("总览上顶栏没有后退，文字有 %q", tt.Texts())
	}
	// 禁用那一档画出来的箭头是**淡的**（框架对禁用元素整个乘 0.5 不透明度）。
	dim := scanGlyph(tt, navBackLabel).contrast
	tt.ClickAt(back.X+back.W/2, back.Y+back.H/2)
	tt.Frame()
	tt.Frame()

	// ⚠️ **路由不许动**。
	if got := s.router.Path(); got != before {
		t.Errorf("在总览上按后退之后路径变成了 %q（原来是 %q）——总览已经是最上面"+
			"那一档了，退不了", got, before)
	}
	// ⚠️ **总览还在**：按一下不能把首页清空。
	if !showsText(tt, "排行榜") {
		t.Errorf("在总览上按后退之后总览不见了，文字有 %q", tt.Texts())
	}

	// ⚠️ **而且它必须是「禁用」的那一档**（不是「按了没反应」而已）。
	//
	// 用户得**看得见**它点不动——一枚亮着的键按了没反应，用户会以为坏了。
	if dim == 0 {
		t.Fatal("总览上那一枚返回键上根本没有图形——判据用不了")
	}
	// 进「热榜」那一档：那一枚**亮着**（它有活干：回总览）。
	if err := tt.Click("热榜"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "进了热榜那一档", func() bool { return showsText(tt, "鬼畜") })
	bright := scanGlyph(tt, navBackLabel).contrast
	if bright <= dim {
		t.Errorf("总览上那一枚返回键（明暗差 %d）和榜单那一档上的（%d）**一样亮**——"+
			"总览那一档上它该是**禁用**的（灰的、点不动），用户得看得见它点不动",
			dim, bright)
	}
}
