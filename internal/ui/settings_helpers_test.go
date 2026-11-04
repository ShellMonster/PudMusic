package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 设置页测试的公共辅助。原来单独的 `/appearance` 路由并进了设置页的「外观」Tab，
// 所以「去外观页」现在是「去设置页 + 切到外观 Tab」。

// showTab 让设置页停在某一个 Tab 上（不经过点击，给只关心那一页内容的测试用）。
func showTab(s *Shell, tt *mygoui.Tester, tab int) {
	s.settings.tab = tab
	s.navigate(settingsPath)
	tt.Frame()
}

// showAppearance 是 `showTab(…, settingsTabAppearance)`。
func showAppearance(s *Shell, tt *mygoui.Tester) { showTab(s, tt, settingsTabAppearance) }

// openAppearance 从侧栏进设置页再点「外观」Tab：走的是用户真走的那条路。
func openAppearance(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	openSettings(t, tt)
	openSettingsTab(t, tt, "外观")
}

// shellOnAppearance 造一个停在设置页「外观」Tab 上的外壳。
func shellOnAppearance(t *testing.T, s app.Settings, opts ...shellOpt) (*mygoui.Tester, *Shell, *app.App, string) {
	t.Helper()
	tt, sh, a, path := shellOnSettings(t, s, opts...)
	openSettingsTab(t, tt, "外观")
	return tt, sh, a, path
}

// appearanceRect 把外观 Tab 滚到 label 那一处在可视区里为止，返回它的框。
func appearanceRect(t *testing.T, tt *mygoui.Tester, s *Shell, label string) mygoui.Rect {
	t.Helper()
	return settingRect(t, tt, s, label)
}
