// 票 #370 的画面验收：首页那三张 Tab 的「无限下滑」——
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket370Shots
//
// # 深浅两套都要出
//
// 这一票新加的两样东西都是**底色上的字**：末尾那一行「没有更多了」和右下角
// 那一枚「回到顶部」。它们在深浅两套里用的是同一套字号和同一套灰度，但**周围
// 那圈底色**换了——所以「它浮在哪」「它压在谁上面」在深色下才看得出。
//
// ⚠️ **一个字都不许打真 B 站接口**：歌是内存夹具（`uiExplore` 那台假 B 站）。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// TestRenderTicket370Shots 把这一票改过的那几处存成 PNG，深浅各一套。
func TestRenderTicket370Shots(t *testing.T) {
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
			b := newUIExplore(t)
			homePagedArchives(b, 2) // 两页：第三页是空的（那就是「到底了」）
			s, tt, _ := newHomeShell(t, b, systemIsDark(dark))
			_ = s

			waitFrames(t, tt, "第一页到了", func() bool {
				return showsText(tt, homePagedSong("音乐", "1", 1))
			})

			// 滚到底：第二页被拉起来，末尾还留着「还有更多」那一行（哨兵）。
			tt.Scroll(homeW/2, homeH/2, 0, 100000)
			waitFrames(t, tt, "第二页到了", func() bool {
				return showsText(tt, homePagedSong("音乐", "2", 1))
			})
			save(t, dir, "109-首页-滚到底-还在加载-"+name+".png", tt)

			// 再滚到底：第三页是空的，于是末尾那一行换成「没有更多了」。
			tt.Scroll(homeW/2, homeH/2, 0, 100000)
			waitFrames(t, tt, "说到头了", func() bool {
				return showsText(tt, "没有更多了")
			})
			save(t, dir, "110-首页-没有更多了-"+name+".png", tt)

			// 回顶那一枚：滚过 400 像素才出现，浮在整窗右下。
			tt.Scroll(homeW/2, homeH/2, 0, 1200)
			waitFrames(t, tt, "回顶按钮出现", func() bool {
				return showsText(tt, backTopLabel)
			})
			save(t, dir, "111-首页-回到顶部-"+name+".png", tt)
		})
	}
}

// TestRenderTicket370PresetShots 把末尾那两行（哨兵 / 「没有更多了」）在**四套
// 主题方案**下各存一张——它们是**次要色**的字和一枚浮在内容上面的按钮，而
// 方案换的是「底色那一族」，所以「它压在什么上面」在每一套里都得看一眼。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket370PresetShots
func TestRenderTicket370PresetShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, preset := range []app.ThemePreset{
		app.ThemePresetClassic,
		app.ThemePresetWarmPaper,
		app.ThemePresetDuskAmber,
		app.ThemePresetSoftPeach,
	} {
		t.Run(string(preset), func(t *testing.T) {
			st := app.DefaultSettings("")
			st.ThemePreset = preset
			sh := newTestShell(t, withSettings(st))
			s, tt := sh.s, sh.tt

			// 外面塞一批歌（比真接口快，也让画面稳定）：塞进去的那一批算
			// 「已经有一页、后面没有」，于是末尾那一行就是「没有更多了」。
			s.home.SetSongs(homeSongs{
				tabs: [homeTabCount][]list.Music{0: homeSongsOf("音乐", 30)},
			})
			homeFrame(tt)
			tt.Scroll(homeW/2, homeH/2, 0, 100000)
			homeFrame(tt)
			save(t, dir, "112-首页-末尾那一行-"+string(preset)+".png", tt)
		})
	}
}
