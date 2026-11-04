// 侧栏的宽与**内容区**的宽必须对上（票 #630 的对抗性审查发现）。
//
// # 这一组钉的是哪一个错
//
// `sideDrawnWidth` 原来取 `max(目标宽, s.sideWidth)`，而 `s.sideWidth` 是
// **上一次拖动的终点**——松手时 `commitSideWidth` 只写设置，**不清它**。
//
// 于是「拖到 300 → 折叠」这一路：
//
//	目标宽 = 64（折叠）　　s.sideWidth = 300（拖动的陈旧值）
//	⇒ sideDrawnWidth = 300
//
// 而画面上侧栏画出来的是 **64**（不拖时 `viewSide` 走
// `AnimateTo(sideWidthKey, target, …)`，画的是目标宽）。
//
// ⇒ **按 `sideDrawnWidth` 算的内容区比实际内容区窄 236 像素**：横幅那一格
// 按 900 要图、画成 900 宽，而它所在的内容区是 1136 宽——**右侧空出 236
// 像素的一条空白带**。
//
// # 为什么这条是「静默」的
//
// 没有报错、没有崩溃、没有日志。画面上只是「横幅右边空了一块」——而
// 「横幅没铺满」这件事**没有任何一条已有测试钉得住**（见
// `upspace_banner_test.go` 里那条覆盖率判据的注释）。
//
// # 这一组的判据是「画面上的像素」
//
// 不是「函数返回了多少」——那是实现细节。而是：**横幅那一带的红色像素占
// 那一带总面积的几成**。它铺不满就不是 100%。
//
// ⚠️ **全走假 B 站 + 假图床**（一个字都不许打真 B 站接口）。

package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// sideWidth300Collapsed 造一个「侧栏拖到 300、然后折叠」的外壳。
//
// ⚠️ **必须真的拖一遍**：那一路要留下 `s.sideWidth = 300` 这个**陈旧值**，
// 而那是 bug 的来源——只把设置写成 300 造不出这个状态（`s.sideWidth` 只在
// 拖动中才被赋值，见 `sideTrackDrag`）。
//
// ⚠️ **必须挂设置**：折叠按钮和松手都是**写设置**（`commitSideWidth` /
// `viewSideCollapse`），而没挂设置时那些写会被 `ErrSettingsNotMounted` 挡掉
// ——于是「拖到 300」和「折叠」两件事都落不了地，这一组的前提就搭不起来。
func sideWidth300Collapsed(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	s, tt := newShell(t, withSettings(app.DefaultSettings("")),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	tt.Frame()

	// 拖到 300（默认 220，往右 80）。
	gx := gripX(app.DefaultSideMenuWidth)
	dragFrom(tt, gx, 400, gx+80, 400)
	if got := s.app.SettingsSnapshot().SideMenuWidth; got != app.MaxSideMenuWidth {
		t.Fatalf("拖之后设置里的宽度是 %v，want %v——这一组的前提没搭起来",
			got, app.MaxSideMenuWidth)
	}

	// 折叠。
	if err := tt.Click("«"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !s.app.SettingsSnapshot().SideMenuCollapsed {
		t.Fatal("点了折叠按钮，设置里没记折叠——这一组的前提没搭起来")
	}
	return s, tt
}

// TestTheContentAreaIsNotNarrowedByAStaleDragWidth 钉住**内容区的宽**和
// **画面上侧栏的宽**对得上。
//
// # 判据是「差多少像素」
//
// 内容区 = 窗口宽 − 侧栏宽。侧栏那一档取自 `sideDrawnWidth`，而横幅那一格
// 是**内容区里的通栏**——所以两者必须一致，差 ≤ 1 像素。
//
// ⚠️ **为什么用「差 ≤ 1」而不是「差 = 0」**：布局是像素对齐的，而浮点数
// 有舍入——「完全相等」那种断言在别的窗口尺寸下会假红。
func TestTheContentAreaIsNotNarrowedByAStaleDragWidth(t *testing.T) {
	s, tt := sideWidth300Collapsed(t)

	// 画面上侧栏实际有多宽。
	onScreen := sideWidthOnScreen(t, tt)
	if onScreen != app.CollapsedSideMenuWidth {
		t.Fatalf("折叠之后画面上的侧栏是 %v 宽，want %v——这一组的前提没搭起来",
			onScreen, app.CollapsedSideMenuWidth)
	}

	// 内容区该有多宽（窗口宽 − 画面上的侧栏宽）。
	winW := float32(tt.Image().Rect.Max.X)
	wantContent := winW - onScreen

	// 而 `sideDrawnWidth` 给的侧栏宽——内容区要减掉的就是它。
	gotSide := s.sideDrawnWidth(s.app.SettingsSnapshot())
	if gotSide != onScreen {
		t.Errorf("内容区按 %v 算（窗口 %v − 侧栏 %v），而 sideDrawnWidth 报的是 %v——差 %v 像素\n"+
			"  （s.sideWidth=%v 是上一次拖动留下的陈旧值，它不该在没拖的时候算数）",
			winW-gotSide, winW, onScreen, gotSide, onScreen-gotSide, s.sideWidth)
	}
	// 内容区的宽也要跟着对得上（横幅那一格就是内容区里的通栏）。
	gotContent := winW - gotSide
	if gotContent != wantContent {
		t.Errorf("内容区是 %v 宽，want %v", gotContent, wantContent)
	}
}

// TestTheBannerFillsTheContentAreaAfterTheSidebarCollapses 钉住**画面上**的
// 后果：折叠之后横幅那一格**铺满内容区**。
//
// # 为什么这条不能由上一条代替
//
// 上一条断的是「算出来的宽对不对」（一个数），这一条断的是「画面上铺没铺满」
// （一片像素）。而**后者才是用户看见的**——横幅右边空出一条 236 像素的空白
// 带，而那个「算出来的宽」只活在代码里。
//
// ⚠️ **判据是覆盖率**：横幅那一带（头像上沿以上）里红色像素占那一带总面积
// 的几成。**不是**「> 100 个像素」那种——那一档只钉住「有图」，而实测把
// 横幅宽度砍一半它照样绿（详见 `bannerRedCoverage` 的注释）。
func TestTheBannerFillsTheContentAreaAfterTheSidebarCollapses(t *testing.T) {
	pics := &bannerPicServer{}
	s, tt := bannerSpaceShell(t, pics, "https://i0.hdslb.com/bfs/archive/banner.png")
	waitFrames(t, tt, "横幅画出来", func() bool {
		return bannerRedPixels(t, tt, bannerBox(t, tt)) > 100
	})

	// 拖到 300，再折叠。
	gx := gripX(app.DefaultSideMenuWidth)
	dragFrom(tt, gx, 400, gx+80, 400)
	if err := tt.Click("«"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// ⚠️ **等折叠那一段过渡走完**：折叠是 200ms 的动画，而无头 Tester 的钟
	// 站着不动——所以要**推着钟**画（`waitFramesClock`），光 `waitFrames`
	// 等不到（实测它等满 10 秒的保险丝）。
	waitFramesClock(t, tt, "折叠过渡走完", func() bool {
		return sideWidthOnScreen(t, tt) == app.CollapsedSideMenuWidth
	})

	// 横幅那一格要**铺满内容区**：它的右沿和内容区的右沿齐。
	box := bannerBox(t, tt)
	winW := float32(tt.Image().Rect.Max.X)
	onScreen := sideWidthOnScreen(t, tt)
	contentRight := winW
	gap := contentRight - (box.X + box.W)
	if gap > 1 {
		t.Errorf("横幅那一格的右沿离内容区右边还有 %.1f 像素（侧栏 %v 宽）——"+
			"它没铺满内容区", gap, onScreen)
	}

	// ⚠️ **还要断「图铺满了那一格」**：那一格的盒子是 `FillWidth()`（恒等于
	// 内容区宽），它满不满和**图**有没有铺满是两件事——而 bug 的后果正是
	// 「盒子满、图不满」。
	coverage := bannerRedCoverage(t, tt, box)
	if coverage < 0.9 {
		t.Errorf("横幅那一带的红色像素只占 %.1f%%（%.1f 像素 / %d）——"+
			"它没铺满那一格（要 ≥ 90%%）\n"+
			"  侧栏画面上是 %v 宽，内容区是 %.1f 宽，而 sideDrawnWidth 报的是 %.1f",
			coverage*100, float64(bannerRedPixels(t, tt, box)), bannerOwnArea(box),
			onScreen, winW-onScreen, s.sideDrawnWidth(s.app.SettingsSnapshot()))
	}
}
