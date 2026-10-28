// 关于面板（应用菜单里「关于布丁音乐」点开的那一块，票 #539）。
//
// 这一族断的是**面板上有哪几样**和**点了有没有反应**。
//
// ⚠️ **版本号这一条断的是「和设置页那一句是同一句」**：两处读的是同一份
// 快照（`s.app.UpdateSnapshot()`）、走的是同一个画法（`s.versionLine`）——
// 各写一个版本号的话，改了打包配置而忘了改另一处，用户在面板上看到的还是
// 上一个版本，而设置页上已经是新的了。
package ui

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// aboutTestShell 造一个挂着假更新器（版本号已知）的外壳。
//
// 假更新器是**必须**的：版本号来自更新器那一摊（`app.UpdateState.Current`），
// 而没挂更新器时那一栏是空的，面板上就写着「未知」——那样这条测试什么也
// 断不到。
func aboutTestShell(t *testing.T, version string, opts ...shellOpt) *testShell {
	t.Helper()
	gu := newFakeUpdate(version, nil)
	all := append([]shellOpt{mounted(func(_ *testing.T, a *app.App) {
		a.MountUpdates(gu, filepath.Join(t.TempDir(), "app-update.json"))
	})}, opts...)
	return newTestShell(t, all...)
}

// TestTheAboutPanelShowsTheNameTheVersionAndTheTwoEntries 断面板上有哪几样。
//
// 应用名、版本号、仓库链接、第三方许可的入口——这四样是「关于」面板的
// 全部内容（票 #539 的验收第 4 条）。
func TestTheAboutPanelShowsTheNameTheVersionAndTheTwoEntries(t *testing.T) {
	b := aboutTestShell(t, "9.9.9")
	tt := b.tt

	// 没开的时候面板不在画面上。
	tt.Frame()
	if tt.HasText(aboutRepoLabel) {
		t.Fatalf("还没点「关于」，面板就已经在了，文字有 %q", tt.Texts())
	}

	b.s.openAbout()
	tt.Frame()

	for _, want := range []string{
		appName,           // 应用名
		"当前版本 9.9.9",      // 版本号（和设置页同一个来源）
		aboutRepoLabel,    // 「代码仓库」
		aboutRepoText,     // 仓库地址
		aboutLicenseLabel, // 「第三方许可」
		aboutLicenseText,  // 许可清单的入口
	} {
		if !tt.HasText(want) {
			t.Errorf("关于面板上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

// TestTheAboutPanelVersionIsTheSameAsTheSettingsPage 断两处的版本号是同一句。
//
// 设置页末尾那一段「关于应用」和关于面板是**两处**显示同一个版本号的地方。
// 两处各写一份的话，它们会各说各话。
func TestTheAboutPanelVersionIsTheSameAsTheSettingsPage(t *testing.T) {
	b := aboutTestShell(t, "9.9.9")
	tt := b.tt

	// 设置页那一段。
	b.s.navigate(settingsPath)
	tt.Frame()
	if !tt.HasText("当前版本 9.9.9") {
		t.Fatalf("设置页的「关于应用」那一段上没有版本号，文字有 %q", tt.Texts())
	}

	// 关于面板那一段。
	b.s.openAbout()
	tt.Frame()
	if !tt.HasText("当前版本 9.9.9") {
		t.Errorf("关于面板上的版本号和设置页对不上，文字有 %q", tt.Texts())
	}
}

// TestTheAboutPanelRepoLinkOpensTheRepository 断仓库链接点得动、而且开的是
// 那个地址。
func TestTheAboutPanelRepoLinkOpensTheRepository(t *testing.T) {
	b := aboutTestShell(t, "9.9.9")
	tt := b.tt

	b.s.openAbout()
	tt.Frame()
	if err := tt.Click(aboutRepoText); err != nil {
		t.Fatalf("点不到仓库链接：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	urls := tt.OpenedURLs()
	if len(urls) == 0 {
		t.Fatal("点了仓库链接，一个地址也没打开")
	}
	if got := urls[len(urls)-1]; got != repoURL {
		t.Errorf("打开的是 %q，want %q", got, repoURL)
	}
}

// TestTheAboutPanelLicenseEntryOpensTheShippedNotices 断「第三方许可」那一项
// 打开的是**随包发出去的那一份清单**。
//
// ⚠️ **判据是「打开的是那个文件」而不是「打开了一个地址」**：清单文件是
// 随包发出去的（`internal/licensing` 生成的那一份），点开它用户才看得到
// 每一个依赖的许可证原文。
func TestTheAboutPanelLicenseEntryOpensTheShippedNotices(t *testing.T) {
	b := aboutTestShell(t, "9.9.9")
	tt := b.tt

	// 随包资源目录 → 清单文件那一个位置。测试里造一份假的顶上去。
	dir := t.TempDir()
	notices := filepath.Join(dir, "licenses", "THIRD-PARTY-NOTICES.txt")
	if err := os.MkdirAll(filepath.Dir(notices), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notices, []byte("# 清单\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := noticesFile
	noticesFile = func() string { return notices }
	t.Cleanup(func() { noticesFile = orig })

	b.s.openAbout()
	tt.Frame()
	if err := tt.Click(aboutLicenseText); err != nil {
		t.Fatalf("点不到「第三方许可」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	urls := tt.OpenedURLs()
	if len(urls) == 0 {
		t.Fatal("点了「第三方许可」，一个地址也没打开")
	}
	// 期望的那个地址用标准库拼（不是用被测的那个函数）——不然这条断言就是
	// 「它等于它自己」。
	want := (&url.URL{Scheme: "file", Path: notices}).String()
	if got := urls[len(urls)-1]; got != want {
		t.Errorf("打开的是 %q，want %q", got, want)
	}
}

// TestTheAboutPanelClosesOnOK 断「好」真的关得掉面板。
func TestTheAboutPanelClosesOnOK(t *testing.T) {
	b := aboutTestShell(t, "9.9.9")
	tt := b.tt

	b.s.openAbout()
	tt.Frame()
	if !tt.HasText(aboutRepoLabel) {
		t.Fatalf("面板没开出来，文字有 %q", tt.Texts())
	}

	if err := tt.Click(aboutOK); err != nil {
		t.Fatalf("点不到「好」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	if tt.HasText(aboutRepoLabel) {
		t.Errorf("按了「好」，面板还在，文字有 %q", tt.Texts())
	}
}

// TestTheAboutMenuItemBringsTheMainWindowBack 断「关于」会把主窗口叫回来。
//
// ⚠️ **菜单是应用级的**（`SetMenu` 设在 `mygo.App` 上），迷你播放器那一扇
// 和主窗口共用同一列菜单——而迷你开着的时候主窗口是收起来的（两个窗口
// 互斥）。面板画在主窗口上，所以「关于」得先把迷你让开。
func TestTheAboutMenuItemBringsTheMainWindowBack(t *testing.T) {
	b := aboutTestShell(t, "9.9.9")
	main := b.s
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)

	m := appMenu(main.openAbout, main.openSettingsFromMenu)
	it := m.ItemByID(menuAboutID)
	if it == nil {
		t.Fatal("菜单里没有「关于」那一项")
	}
	// 回调拿到的 `win` 可能是迷你那一扇——这里传 nil（最狠的一种）：
	// 一个会去用 `win` 的实现当场就崩了。
	it.Click(it, nil)

	if main.mini != nil {
		t.Error("迷你播放器还在前面：面板画在主窗口上，而主窗口是收着的")
	}
	if !main.about.wanted {
		t.Error("面板没有开")
	}
}
