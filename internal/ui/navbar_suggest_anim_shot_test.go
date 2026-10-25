package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一族把联想面板的**进场那几帧**存成 PNG，留给人眼看（票 #259）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSuggestEnterShots
//
//	01 起手：面板刚要出现的那一帧（0 毫秒——它一个字都还没画）
//	02 四分之一：25 毫秒（曲线走到 57.3%——**唯一能一眼看出过渡的一帧**）
//	03 半程：100 毫秒（曲线走完 95.4%）
//	04 落位：200 毫秒之后
//
// ⚠️ **深浅两套都要出**：面板的底和页面的底是同一个色，面板浮起来靠的是**影**
// 和**描边**，而影在深浅两套里差得很远（`widgets/shadow` 的 Popover 那一档：
// 浅色 18% 黑、深色 35% 黑）。
//
// ⚠️ **两套主题方案也要各看一眼**：面板的影落在什么底上，暖纸和经典那两套
// 差得不少（经典是 Biu 绿 + 框架默认底色，暖纸是设计规范那套）。
func TestRenderSuggestEnterShots(t *testing.T) {
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
				s, tt := newShell(t, systemIsDark(dark), withSettings(st))
				tt.SetFocused(true)
				s.hist.add("林俊杰")
				s.hist.add("五月天 温柔")
				tt.Frame()

				if err := tt.Click(searchLabel); err != nil {
					t.Fatalf("点不到搜索框：%v", err)
				}
				tt.Frame()
				// 指针挪开，别让它停在框上（悬停那一档会混进来）。
				tt.Move(900, 500)
				tt.Frame()
				save(t, dir, "联想面板进场-01-起手-"+name+".png", tt)
				saveRect(t, dir, "联想面板进场-01-起手-局部-"+name+".png", tt, 300, 0, 520, 180)

				// ② 25 毫秒（1/8 的那一段）：曲线在这一刻走到 57.3%——
				// **这一帧是唯一能一眼看出「它在淡、它在升」的一帧**（半程那一帧
				// 已经走完 95.4%，和落位几乎看不出来）。
				tt.Advance(25 * time.Millisecond)
				tt.Frame()
				save(t, dir, "联想面板进场-02-四分之一-"+name+".png", tt)
				saveRect(t, dir, "联想面板进场-02-四分之一-局部-"+name+".png", tt, 300, 0, 520, 180)

				tt.Advance(75 * time.Millisecond)
				tt.Frame()
				save(t, dir, "联想面板进场-03-半程-"+name+".png", tt)
				saveRect(t, dir, "联想面板进场-03-半程-局部-"+name+".png", tt, 300, 0, 520, 180)

				tt.Advance(100 * time.Millisecond)
				tt.Frame()
				save(t, dir, "联想面板进场-04-落位-"+name+".png", tt)
				saveRect(t, dir, "联想面板进场-04-落位-局部-"+name+".png", tt, 300, 0, 520, 180)
			})
		}
	}
}
