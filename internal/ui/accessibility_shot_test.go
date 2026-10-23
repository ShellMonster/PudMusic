package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一族把票 #139 改的那几处存成 PNG，留给人眼和上游并排看：
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderAccessibilityShots
//
// ⚠️ **深浅两套都要出**：禁用态最坏的一档是**深色**（选中那一项的字原来是主色绿），
// 只出浅色的话这张票等于没验。
//
// 四组：
//
//	40/41  设置页（可写 / 只读）——禁用态那三个主题按钮
//	42     高对比度（关 / 开）——边框和次要文字
//	43     字号缩放（1 / 1.4）——页标题和正文
func TestRenderAccessibilityShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			save(t, dir, "40-"+name+"-设置-可写.png", openSettingsFor(t, dark, false))
			save(t, dir, "41-"+name+"-设置-只读.png", openSettingsFor(t, dark, true))
			save(t, dir, "42-"+name+"-高对比度-关.png", contrastShot(t, dark, false))
			save(t, dir, "42-"+name+"-高对比度-开.png", contrastShot(t, dark, true))
			save(t, dir, "43-"+name+"-字号-1.png", scaleShot(t, dark, 1))
			save(t, dir, "43-"+name+"-字号-1.4.png", scaleShot(t, dark, 1.4))
		})
	}
}

// contrastShot 是一个只画边框和两行字的窗口：高对比度改的就是这两样。
func contrastShot(t *testing.T, dark, high bool) *mygoui.Tester {
	t.Helper()
	s := app.DefaultSettings("")
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, s, dark)
		th := c.Theme()
		ui.Column(c).Padding(24).Gap(16).Children(func() {
			ui.Box(c).Width(200).Height(48).Border(1, th.Border)
			ui.Text(c, "正文").FontSize(scale.Font(c, 16)).TextColor(th.Text)
			ui.Text(c, "次要文字：说明、提示、占位").
				FontSize(scale.Font(c, 12)).TextColor(th.TextMuted)
		})
	}, 400, 220)
	if dark {
		tt.SetDark(true)
	}
	if high {
		tt.SetPreferences(ui.Preferences{HighContrast: true})
	}
	tt.Frame()
	return tt
}

// scaleShot 是一个字号缩放前后的窗口。
func scaleShot(t *testing.T, dark bool, factor float32) *mygoui.Tester {
	t.Helper()
	s := app.DefaultSettings("")
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, s, dark)
		th := c.Theme()
		ui.Column(c).Padding(24).Gap(8).Children(func() {
			ui.Text(c, "页标题").FontSize(scale.Font(c, scale.PageTitle))
			ui.Text(c, "正文一行，用来对比字号。").
				FontSize(scale.Font(c, 14)).TextColor(th.Text)
		})
	}, 400, 220)
	if dark {
		tt.SetDark(true)
	}
	tt.SetPreferences(ui.Preferences{TextScale: factor})
	tt.Frame()
	return tt
}
