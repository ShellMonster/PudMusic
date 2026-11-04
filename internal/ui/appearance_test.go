package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 皮肤页（/appearance）：顶栏 palette 图标进的一页，设置页「常规 → 外观」那
// 一整块搬来的（深浅模式 + 强调色 + 封面取色 + 字体 + 背景清晰度；主题方案、
// 自定义颜色、圆角 2026-11 随「深夜唱片行」删掉，见 DESIGN.md §0′）。

// openAppearance 从顶栏那枚 palette 图标进皮肤页。走的是用户真的走的那条路。
func openAppearance(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click("皮肤"); err != nil {
		t.Fatalf("顶栏上点不到「皮肤」：%v，文字有 %q", err, tt.Texts())
	}
	settlePage(t, tt, "深浅模式") // 等切页动画停，皮肤页认「深浅模式」那一组
	if !tt.HasText("深浅模式") {
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
	for _, want := range []string{"皮肤", "深浅模式", "强调色", "封面取色", "字体", "背景清晰度"} {
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

// TestTheAppearancePageHasNoPresetGridOrCustomColors：方案网格、自定义主色/底色、
// 圆角滑块、「定时」档都不在了（§0′ 用户拍板）；封面取色开着时强调色那一行也不画。
func TestTheAppearancePageHasNoPresetGridOrCustomColors(t *testing.T) {
	tt, _, _, _ := shellOnAppearance(t, app.DefaultSettings(""))
	for _, gone := range []string{"主题方案", "暖纸", "经典", "主色", "全局圆角"} {
		if tt.HasText(gone) {
			t.Errorf("皮肤页上还有 %q", gone)
		}
	}
	// 深浅模式只有三档（「定时」删了；画面上别处还有「睡眠定时」，所以按卡数）。
	for _, want := range []string{"朱砂", "靛青", "松绿", "琥珀"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("皮肤页上没有强调色 %q", want)
		}
	}

	st := app.DefaultSettings("")
	st.CoverAccent = true
	tt2, _, _, _ := shellOnAppearance(t, st)
	if _, ok := tt2.Find("朱砂"); ok {
		t.Error("封面取色开着时强调色那一行不该画（原型 appearance() 的条件）")
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
