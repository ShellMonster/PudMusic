// 票 #417 的画面验收：「名字型可点击元素」的下划线。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderNameLinkShots
//
// # 这一族看的是「一道下划线」
//
// 改之前那个名字被一个**胶囊**包着（左右各 14 像素的内边距 + 圆角），指针落上去
// 时那层半透明的前景色才显出来——像一块底牌。改完是**一个字 + 一道下划线**。
//
// # 深浅两套都要出
//
// 下划线画在 `ink.AccentOn(主题)` 上——浅色下那是**压深过**的一档（默认主色压
// 浅底只有 1.9:1，直接用等于看不见），深色下就是主色本身。只出浅色的话深色下
// 那一道会淡成一根灰线。
//
// # 主题方案也要各看一眼
//
// 这一族里没有一个写死的颜色：下划线是**从当前主色现算**的，而主色跟着方案走
// （暖纸是赤陶、暮光是琥珀）。**经典那一套过了不等于方案那一套也过了**。
//
// ⚠️ **一个字都不许打真 B 站接口**：全是内存夹具 + 无头渲染。
package ui

import (
	"net/http"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// nameLinkShotSettings 是这一族用的设置：方案可换，其余照默认。
func nameLinkShotSettings(preset app.ThemePreset) app.Settings {
	st := app.DefaultSettings("")
	st.ThemePreset = preset
	return st
}

// nameLinkDynamicShell 造一个停在 UP 空间「动态」那一页的壳，**指针停在作者名
// 上面**（要看的就是悬停那一下）。
func nameLinkDynamicShell(t *testing.T, dark bool, preset app.ThemePreset) (*mygoui.Tester, *Shell) {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "Biu 的用户", 0, 123456)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamicWithFace("1", "这一条是纯文字动态，用来看作者名那一格。",
				"https://f/author-11.jpg"),
		}, "offset": "", "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, true, "77",
		systemIsDark(dark), withSettings(nameLinkShotSettings(preset)),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态到了", func() bool { return sp.dyn.started })
	tt.Frame()
	// 指针停在作者名上——这一族要看的就是悬停那一下。
	r, ok := tt.Find("作者1")
	if !ok {
		t.Fatalf("画面上找不到作者名，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	homeFrame(tt)
	return tt, s
}

// nameLinkPlaybarShell 造一个只画播放条的外壳，**指针停在 UP 名上面**。
func nameLinkPlaybarShell(t *testing.T, dark bool, preset app.ThemePreset) (*mygoui.Tester, *Shell) {
	t.Helper()
	b := newTestShell(t, at(1200, coverWinH), systemIsDark(dark),
		withSettings(nameLinkShotSettings(preset)),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.applyFrameTheme(c)
				s.viewPlayBar(c, app.State{
					HasTrack: true, Title: "一首歌", Artist: "某个 UP 主", ArtistMid: 12345,
				})
			}
		}))
	tt := b.tt
	tt.Frame()
	r, ok := tt.Find("某个 UP 主")
	if !ok {
		t.Fatalf("画面上找不到 UP 名，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	homeFrame(tt)
	return tt, b.s
}

// TestRenderNameLinkShots 把「名字型链接」那几处存成 PNG。
func TestRenderNameLinkShots(t *testing.T) {
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
			// 01 动态条目上的作者名（暖纸）：**用户点名的那一处**。
			{
				tt, _ := nameLinkDynamicShell(t, dark, app.ThemePresetWarmPaper)
				save(t, dir, "90-动态条目-作者名悬停-暖纸-"+name+".png", tt)
			}
			// 02 同一处（暮光·琥珀）：另一套色相——下划线跟着主色走。
			{
				tt, _ := nameLinkDynamicShell(t, dark, app.ThemePresetDuskAmber)
				save(t, dir, "91-动态条目-作者名悬停-暮光-"+name+".png", tt)
			}
			// 03 播放条上的 UP 名（暖纸）。
			{
				tt, _ := nameLinkPlaybarShell(t, dark, app.ThemePresetWarmPaper)
				save(t, dir, "92-播放条-UP名悬停-暖纸-"+name+".png", tt)
			}
			// 04 同一处（暮光·琥珀）。
			{
				tt, _ := nameLinkPlaybarShell(t, dark, app.ThemePresetDuskAmber)
				save(t, dir, "93-播放条-UP名悬停-暮光-"+name+".png", tt)
			}
		})
	}
}
