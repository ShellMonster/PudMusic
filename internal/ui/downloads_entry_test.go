package ui

// 这一组盯的是**下载那一摊的入口**：
//
//   - 页头那一句「还没有下载目录」点得动吗？点完到得了能配的地方吗？
//   - 中央那一格在「没有目录」的时候说的是不是同一件事（它和页头那一句
//     打架的话，用户照着做，然后失败）？
//   - 改了下载目录之后，用户知不知道它在哪儿生效了？
//
// 出处是 UX 审计报告的 F3 / C1 / D3（报告在
// `~/.herdr-projects/reports/Biu_MyGo/125/report.md`，只查不改）。

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// TestTheDownloadPageDoesNotTellYouToDownloadWhenThereIsNowhereToPutIt 断
// 「没有下载目录」那一档里两句话**不打架**。
//
// 页头写着「还没有下载目录」，中央写着「搜一首歌，在它旁边点「下载」，下完的
// 会出现在这里」——两句说的不是一回事，而中央那句更具体、更像行动指南，于是
// 用户照着做，下完发现没地方放（报告 F3）。
//
// ⚠️ **反过来的那一半也要断**：有目录的时候中央那一格要变回那句行动指南——
// 不然这一票就把「怎么才能不空」那句话弄丢了。
func TestTheDownloadPageDoesNotTellYouToDownloadWhenThereIsNowhereToPutIt(t *testing.T) {
	_, tt := newRows(t)

	if !tt.HasText(noDownloadDir.Hint) {
		t.Errorf("没有目录时中央那一格没说 %q，文字有 %q", noDownloadDir.Hint, tt.Texts())
	}
	if tt.HasText(noDownloads.Hint) {
		t.Errorf("没有目录却还在说「搜一首歌…」（页头已经写着没有目录了，两句"+
			"打架，用户照着做然后失败），文字有 %q", tt.Texts())
	}
	// 而它给的是**一条路**：一枚「去设置」的按钮。
	if !tt.HasText("去设置") {
		t.Errorf("没有目录时中央那一格没有「去设置」那枚按钮，文字有 %q", tt.Texts())
	}

	// 有目录的时候，那一句行动指南回来。
	_, tt2 := newRowsWithDir(t)
	if !tt2.HasText(noDownloads.Hint) {
		t.Errorf("有目录时中央那一格没说「怎么才能不空」%q，文字有 %q",
			noDownloads.Hint, tt2.Texts())
	}
	if tt2.HasText(noDownloadDir.Hint) {
		t.Errorf("有目录却还在说「还没有下载目录」，文字有 %q", tt2.Texts())
	}
}

// TestTheMissingDownloadDirectoryTakesYouToTheSetting 断页头那一句
// **点得动**，而且点完真的到了能配的地方。
//
// ⚠️ **只跳到设置页是不够的**：设置页有四个 Tab，而「下载目录」在「常规设置」
// 那一格的第三段（要往下滚才看得到）。只跳过去的话，等于让用户自己把「下载
// 记录页的一句话」翻译成「设置页第 N 个 Tab 的第 M 行」（报告 C1）。
func TestTheMissingDownloadDirectoryTakesYouToTheSetting(t *testing.T) {
	s, tt := newRows(t)

	if err := tt.Click("还没有下载目录"); err != nil {
		t.Fatalf("点页头那一句：%v（文字有 %q）", err, tt.Texts())
	}
	dlFrame(tt)

	if got := s.router.Path(); got != settingsPath {
		t.Fatalf("点完停在 %q，want %q（用户得能去配）", got, settingsPath)
	}
	if s.settings.tab != settingsTabGeneral {
		t.Errorf("落到了第 %d 个 Tab，want %d（下载目录在「常规设置」那一格）",
			s.settings.tab, settingsTabGeneral)
	}
	if s.settings.focus != settingsFocusDownload {
		t.Errorf("没有指着「下载目录」那一行，focus=%q", s.settings.focus)
	}
}

// TestTheDownloadDirectoryIsScrolledIntoView 断「指着那一行」不是一句空话：
// 那一行真的被滚进视野了。
//
// ⚠️ **判据是「它在不在可视区里」**：设置页那一格有几十行，「下载目录」在第三
// 段——不滚过去的话用户看到的还是「外观」那一段，而他要配的东西在下面。
//
// ⚠️ **滚进视野要跨两帧**：量的是上一帧的盒子（`Element.Bounds()` 的规矩），
// 而刚跳过来那一帧画的是别的页——所以这里等的是「它进了视野」，不是「跳过去了」。
func TestTheDownloadDirectoryIsScrolledIntoView(t *testing.T) {
	_, tt := newRows(t)
	if err := tt.Click("还没有下载目录"); err != nil {
		t.Fatalf("点页头那一句：%v", err)
	}
	dlFrame(tt)

	waitFrames(t, tt, "「下载目录」滚进视野", func() bool {
		r, ok := tt.Find("下载目录")
		if !ok || r.H <= 0 {
			return false
		}
		return r.Y >= pageTop && r.Y+r.H <= pageBottom
	})
}

// TestChangingTheDownloadDirectorySaysWhereItTakesEffect 断改完下载目录之后
// **说一声它在哪儿生效**。
//
// 设置页和下载记录页是两页：用户在这一页改完，看不见另一页变了——而「改了一
// 个地方、另一个地方不变」是用户最容易当成「设置没生效」的那一种形态（报告
// D3）。
func TestChangingTheDownloadDirectorySaysWhereItTakesEffect(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))

	fake := &fakePicker{path: "/music/downloads"}
	s.picker = fake
	clickSetting(t, tt, s, "选择")
	waitFor(t, "下载目录写进设置", func() bool {
		return a.SettingsSnapshot().DownloadPath == "/music/downloads"
	})
	waitFor(t, "说清楚它在哪儿生效", func() bool {
		return tt.HasText("下载记录页上写的就是它")
	})
}
