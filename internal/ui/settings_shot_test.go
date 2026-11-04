package ui

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 设置页、登录页和两个对话框的画面验收（视觉审查）。
//
//	BIU_SHOT_DIR=/Users/daozhang/Trae_AI/PudMusic-redesign/shots/T6 \
//	  go test ./internal/ui/ -run 'TestRenderSettingsShots|TestRenderLoginAndDialogShots'
//
// 文件名和原型参考图（`ref/{dark,light}-settings-*.png`、`*-login.png`）一一对应；窗口 1200×800。
// 图是给人看的，判据在各自的测试里。

func shotDir(t *testing.T) string {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	return dir
}

func shotTheme(dark bool) string {
	if dark {
		return "dark"
	}
	return "light"
}

// TestRenderSettingsShots 把设置页六个 Tab 深浅各存一张。
func TestRenderSettingsShots(t *testing.T) {
	dir := shotDir(t)
	tabs := []struct {
		file string
		tab  int
		prep func(t *testing.T, tt *mygoui.Tester, s *Shell)
	}{
		{"general", settingsTabGeneral, nil},
		{"appearance", settingsTabAppearance, nil},
		{"menu", settingsTabMenu, nil},
		{"shortcut", settingsTabShortcut, nil},
		{"proxy", settingsTabProxy, nil},
		{"about", settingsTabAbout, nil},
		// 额外几张：外观往下滚（字体列表、数据显示样式）、代理选了自定义。
		{"appearance-2", settingsTabAppearance, func(t *testing.T, tt *mygoui.Tester, s *Shell) {
			s.settings.scroll.Y = 600
			tt.Frame()
		}},
		{"proxy-custom", settingsTabProxy, func(t *testing.T, tt *mygoui.Tester, s *Shell) {
			s.settings.proxy.custom = true
			tt.Frame()
		}},
	}
	for _, dark := range []bool{true, false} {
		for _, c := range tabs {
			t.Run(fmt.Sprintf("%s-%s", shotTheme(dark), c.file), func(t *testing.T) {
				st := app.DefaultSettings("")
				st.ThemeMode = app.ThemeModeLight
				if dark {
					st.ThemeMode = app.ThemeModeDark
				}
				b := newTestShell(t, withSettings(st), at(1200, 800), systemIsDark(dark))
				b.tt.Frame()
				showTab(b.s, b.tt, c.tab)
				settlePage(t, b.tt, "常规")
				if c.prep != nil {
					c.prep(t, b.tt, b.s)
				}
				// 开关、卡片描边都是 160ms 的渐变，钟不推就停在上一状态。
				b.tt.Advance(400 * time.Millisecond)
				b.tt.Frame()
				save(t, dir, fmt.Sprintf("%s-settings-%s.png", shotTheme(dark), c.file), b.tt)
				b.Close()
			})
		}
	}
}

// loginTesterAt 同 `loginTester`，窗口 1200×800、深浅可选。
func loginTesterAt(t *testing.T, a *app.App, dark bool) (*mygoui.Tester, *Shell) {
	t.Helper()
	b := newTestShell(t, onApp(a), at(1200, 800), systemIsDark(dark), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.router.Push("/login")
			s.View(c)
		}
	}))
	return b.tt, b.s
}

// TestRenderLoginAndDialogShots 存登录页（等扫码 / 已扫码）、确认框和更新框。
func TestRenderLoginAndDialogShots(t *testing.T) {
	dir := shotDir(t)
	for _, dark := range []bool{true, false} {
		name := shotTheme(dark)
		t.Run(name, func(t *testing.T) {
			// 登录页：二维码有效（等扫码）。
			b := newUIBili(t, 86101)
			a := newLoginApp(t, b)
			tt, _ := loginTesterAt(t, a, dark)
			tt.Frame()
			a.BeginLogin()
			waitQRCode(t, a)
			tt.Frame()
			save(t, dir, name+"-login.png", tt)

			// 已扫码：黑罩 + 对勾。
			b2 := newUIBili(t, 86090)
			a2 := newLoginApp(t, b2)
			tt2, _ := loginTesterAt(t, a2, dark)
			tt2.Frame()
			a2.BeginLogin()
			waitQRCode(t, a2)
			waitLogin(t, a2, app.LoginScanned)
			tt2.Frame()
			save(t, dir, name+"-login-scanned.png", tt2)

			// 确认框（危险）：走真实的那条路——收藏夹里选几首点「批量删除」。
			_, cs, ctt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
			ctt.SetDark(dark)
			waitSongs(t, ctt, cs, 3)
			openBatchConfirm(t, ctt, "批量删除")
			ctt.Frame()
			save(t, dir, name+"-confirm.png", ctt)

			// 更新框：有新版本（带日志）。
			f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0",
				Notes: "## 新增\n\n- 新增「深夜唱片行」主题方案，深浅两套独立配色\n- 播放条显示点赞、收藏、评论数\n- 满屏播放器支持弹幕和视频影院模式\n- 歌词可以搜索替换，并显示翻译"},
				at(1200, 800), systemIsDark(dark))
			f.checkUpdate(t)
			f.openDialog(t)
			f.tt.Frame()
			save(t, dir, name+"-update.png", f.tt)
		})
	}
}
