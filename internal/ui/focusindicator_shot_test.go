// 票 #453 的出图：顶栏搜索框的焦点指示、网格那一格的焦点环、侧栏折叠态那一列，
// 深浅两套 + 几套主题方案各看一眼。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFocusIndicatorShots
//
// ⚠️ **深浅两套都要出**：焦点环是**半透明的主色**压在底色上的，浅色下和深色下
// 深浅不一；而搜索框那一圈描边在聚焦时换成主色——浅色下主色压在浅底上，深色下
// 压在深底上，两边读出来的亮度差得很远。只看一套就只验了一半。
//
// ⚠️ **主题方案也要各看一眼**：主色是**跟着方案走**的（暖纸是赤陶、经典是绿），
// 而这一票动的三处焦点指示里有两处直接用主色——所以「这一圈看得见吗」这件事
// 在不同方案下是不同的答案。
//
// ⚠️ **图是给人看的，判据在 `navbar_search_focus_test.go` /
// `sidebar_focus_test.go` / `widgets/list/gridfocus_test.go` 里**：这里只负责把
// 画面摆出来，免得「像素对了但画面难看得没人要」。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// TestRenderFocusIndicatorShots 把这一票动的三处各存一张 PNG。
func TestRenderFocusIndicatorShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, preset := range []app.ThemePreset{app.ThemePresetWarmPaper, app.ThemePresetClassic} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			name := string(preset) + "-" + shade
			t.Run(name, func(t *testing.T) {
				st := app.DefaultSettings("")
				st.ThemePreset = preset
				st.ThemeMode = app.ThemeModeLight
				if dark {
					st.ThemeMode = app.ThemeModeDark
				}

				// ① 顶栏那个搜索框：**没聚焦**和**聚焦**各一张。
				//
				// ⚠️ **聚焦那一档要把指针挪开**：指针压在上面时底色也抬到 30%，
				// 两档混在一起就看不出「聚焦」那一条是哪儿变的。
				{
					_, tt := newShell(t, systemIsDark(dark), withSettings(st),
						withPrefs(ui.Preferences{ReduceMotion: true, TextScale: 1}))
					tt.SetFocused(true)
					tt.Move(900, 500)
					tt.Frame()
					saveRect(t, dir, "焦点指示-顶栏搜索框-没聚焦-"+name+".png", tt, 280, 8, 320, 48)

					if err := tt.Click(searchLabel); err != nil {
						t.Fatalf("点不到搜索框：%v", err)
					}
					tt.Move(900, 500)
					tt.Frame()
					saveRect(t, dir, "焦点指示-顶栏搜索框-聚焦-"+name+".png", tt, 280, 8, 320, 48)
				}

				// ② 网格那一档：键盘走到哪一格，**那一格**有一圈焦点环。
				//
				// ⚠️ **用键盘走位**（Tab + 方向键）：鼠标点一格之后那一格什么标记
				// 都没有（票 #197），那是另一张图（`selection_shot_test.go` 存过）。
				{
					st2 := st
					st2.DisplayMode = app.DisplayModeCard
					b := newTestShell(t, systemIsDark(dark), withSettings(st2))
					s := b.s
					s.home.SetSongs(fullSongs())
					homeFrame(b.tt)
					tt := b.tt
					tt.SetFocused(true)
					// Tab 进网格，方向键走到第三格。
					for range 40 {
						tt.Key(0, ui.KeyTab)
						tt.Frame()
						if tt.Focused(songName(homeTabs[0], 1)) {
							break
						}
					}
					tt.Key(0, ui.KeyRight)
					tt.Frame()
					tt.Key(0, ui.KeyRight)
					tt.Frame()
					save(t, dir, "焦点指示-网格-键盘焦点环-"+name+".png", tt)
				}

				// ③ 侧栏展开态：焦点落在**收藏夹那一行**上——那一行原来
				// 鼠标点得动、键盘走不到（票 #453 补的）。
				{
					_, _, tt, _ := favShell(t, 2, 1, withSettings(st))
					tt.SetFocused(true)
					tt.Frame()
					for range 40 {
						tt.Key(0, ui.KeyTab)
						tt.Frame()
						if tt.Focused("我建的 1") {
							break
						}
					}
					saveRect(t, dir, "焦点指示-侧栏展开态-收藏夹行焦点环-"+name+".png", tt, 0, 200, 260, 220)
				}

				// ④ 侧栏折叠态那一列：焦点落在那一列的某一项上。
				{
					st3 := st
					st3.SideMenuCollapsed = true
					_, tt := newShell(t, systemIsDark(dark), withSettings(st3),
						withPrefs(ui.Preferences{ReduceMotion: true, TextScale: 1}))
					tt.SetFocused(true)
					tt.Frame()
					// Tab 到侧栏的第一项上。
					for range 40 {
						tt.Key(0, ui.KeyTab)
						tt.Frame()
						if tt.Focused("推荐音乐") {
							break
						}
					}
					saveRect(t, dir, "焦点指示-侧栏折叠态-焦点环-"+name+".png", tt, 0, 60, 120, 200)
				}
			})
		}
	}
}
