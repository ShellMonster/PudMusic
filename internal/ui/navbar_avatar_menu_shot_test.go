package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一族把头像下面那一层下拉存成 PNG，留给人眼看。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderAvatarMenuShots
//
// ⚠️ **深浅两套 × 四个方案都要出**：这一层的底色走 `surface1`、圆角走
// `radiusLg`、选中那一项的淡底走 `accentSoft`——这三个都**跟着主题走**，
// 而经典和三套方案给的数不是同一个（经典是控件层 + 1.5 倍圆角，方案里是
// surface1 + radiusLg，accentSoft 的深浅两档也不同）。只看一套的话，
// 「深色下是不是一块浮起来的板」「暖纸那一套的淡底还看不看得出来」都验不到。
//
// ⚠️ 一个字都不许打真 B 站接口：这一层只画界面上的东西。
func TestRenderAvatarMenuShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, preset := range []app.ThemePreset{
		app.ThemePresetClassic, app.ThemePresetWarmPaper,
		app.ThemePresetDuskAmber, app.ThemePresetSoftPeach,
	} {
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
				b := newTestShell(t, withSettings(st))
				tt := b.tt
				tt.SetFocused(true)
				tt.Frame()

				openAvatarMenu(t, tt)
				// 指针挪开：头像悬停会放大 5%，而那一格缩着的时候画面上
				// 看不清它和浮层的关系。
				tt.Move(600, 400)
				tt.Frame()
				save(t, dir, "头像下拉-"+name+".png", tt)

				// 指针停在最后一项（「退出登录」）上：当前项铺的是 accentSoft
				// 淡底，而**那一项的危险字要按这层底现算**——这一张就是看
				// 那两个字在淡底上还读不读得出来。
				row, ok := avatarMenuRow(tt, len(avatarMenuItems)-1)
				if !ok {
					t.Fatalf("画面上找不到下拉里最后一项")
				}
				tt.Move(row.X+row.W/2, row.Y+row.H/2)
				tt.Frame()
				save(t, dir, "头像下拉-悬停-"+name+".png", tt)
			})
		}
	}
}
