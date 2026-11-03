package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 皮肤页（/appearance）：顶栏 palette 图标进的一页，设置页「常规 → 外观」那
// 一整块搬来的（主题方案 + 主题模式 + 颜色 + 封面取色 + 字体 + 圆角 +
// 背景清晰度）。

// openAppearance 从顶栏那枚 palette 图标进皮肤页。走的是用户真的走的那条路。
func openAppearance(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click("皮肤"); err != nil {
		t.Fatalf("顶栏上点不到「皮肤」：%v，文字有 %q", err, tt.Texts())
	}
	settlePage(t, tt, "主题方案") // 等切页动画停，皮肤页认「主题方案」那一组
	if !tt.HasText("主题方案") {
		t.Fatalf("切到皮肤页了但没画出内容，文字有 %q", tt.Texts())
	}
}

// shellOnAppearance 造一个停在皮肤页上的外壳。
func shellOnAppearance(t *testing.T, s app.Settings, opts ...shellOpt) (*mygoui.Tester, *Shell, *app.App, string) {
	t.Helper()
	b := newTestShell(t, append([]shellOpt{withSettings(s)}, opts...)...)
	tt := b.tt
	tt.Frame()
	openAppearance(t, tt)
	return tt, b.s, b.a, b.settingsPath
}

// TestTheAppearancePageHasTheWholeThemeBlock 验那一整块控件都在页上。
func TestTheAppearancePageHasTheWholeThemeBlock(t *testing.T) {
	tt, _, _, _ := shellOnAppearance(t, app.DefaultSettings(""))
	for _, want := range []string{"皮肤", "主题方案", "暖纸", "主题", "颜色", "封面取色", "字体", "圆角", "背景清晰度"} {
		if !tt.HasText(want) {
			t.Errorf("皮肤页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

// TestTheAppearanceButtonIsInTheNavbar 验顶栏那枚 palette 键在、能按。
func TestTheAppearanceButtonIsInTheNavbar(t *testing.T) {
	_, s, _, _ := shellOnAppearance(t, app.DefaultSettings(""))
	if got := s.router.Path(); got != appearancePath {
		t.Fatalf("按顶栏那一枚之后路由是 %q，want %q", got, appearancePath)
	}
}

// TestPickingAPresetOnTheAppearancePage：皮肤页点一档方案 → 设置里变了、
// 主题下一帧换了。（`TestPickingAPresetOnTheSettingsPage` 的迁移——那一整块
// 控件搬到这一页了。）
func TestPickingAPresetOnTheAppearancePage(t *testing.T) {
	start := app.DefaultSettings("")
	start.ThemePreset = app.ThemePresetWarmPaper
	tt, s, a, _ := shellOnAppearance(t, start)

	if got := a.SettingsSnapshot().ThemePreset; got != app.ThemePresetWarmPaper {
		t.Fatalf("起点方案是 %q，want 暖纸", got)
	}
	clickSetting(t, tt, s, "暮光")
	tt.Frame()
	if got := a.SettingsSnapshot().ThemePreset; got != app.ThemePresetDuskAmber {
		t.Fatalf("点「暮光」之后方案是 %q", got)
	}
}

// TestTheAppearancePageIsNotInTheSidebar 验侧栏上没有「皮肤」这一条
// （顶栏图标就是它的入口，侧栏不摆）。
//
// ⚠️ **「皮肤」这个字画面上不止一处**：页标题一处、顶栏那枚图标的 Label 一处、
// `Route.Title` 一处（页名也走文本层）。所以判据不是「有几处」——侧栏上那条
// 菜单项的文字样式是**侧栏那一份**（`menuTitle`），而页标题是「PageTitle」
// 字号、按钮是 Label。要数的是**侧栏菜单项里**有没有它——侧栏菜单项的字都是
// 侧栏分组下的那一行（`viewSideMenu`），而路由表 `routes` 里 `InMenu` 的那七条
// 是那张清单的唯一来源，皮肤页不在里面。
func TestTheAppearancePageIsNotInTheSidebar(t *testing.T) {
	for _, rt := range routes {
		if rt.Match == appearancePath {
			if rt.InMenu {
				t.Errorf("皮肤页挂进了侧栏菜单（Route.InMenu=true）——顶栏图标就是它的入口，侧栏不摆")
			}
			return
		}
	}
	t.Fatalf("路由表里没有 %q 这一条", appearancePath)
}
