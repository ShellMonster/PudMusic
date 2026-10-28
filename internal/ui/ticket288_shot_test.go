// 票 #288 的画面验收：「全部播放」那一枚——贴到内容区右沿、114×40、带一枚
// 18 的 ▶ 图标（UX 报告 C-04 + C-05）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket288Shots
//
// # 深浅两套都要出
//
// 这一枚是**实心主色**，而主色底上的字和图标都是**黑的**（`t.AccentText`）——
// 深浅两套里那一片绿是同一个数，但**周围那圈底色**换了，所以「它贴在哪」在
// 深色下才看得出贴不贴得住。
//
// ⚠️ **一个字都不许打真 B 站接口**：歌是内存夹具，封面走本机 `httptest`。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// TestRenderTicket288Shots 把这一票改过的那几处存成 PNG，深浅各一套。
func TestRenderTicket288Shots(t *testing.T) {
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
			// ── 首页那一枚：贴到内容区右沿 ─────────────────────────
			//
			// ⚠️ **得有歌**：没有歌时那一枚是**禁用**的（框架给禁用控件乘 0.5
			// 不透明度），那时候画面上是一片淡绿，看不出它贴在哪。
			b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
			s, tt := b.s, b.tt
			s.home.SetSongs(homeSongs{
				tabs: [homeTabCount][]list.Music{0: homeSongsOf("音乐", 12)},
			})
			homeFrame(tt)
			save(t, dir, "106-首页-全部播放-"+name+".png", tt)
			// 右上角那一块放大：那枚按钮和它的右沿。
			saveRect(t, dir, "107-首页-全部播放-放大-"+name+".png", tt, 1000, 52, 200, 70)
			b.Close()

			// ── 搜索页那一枚：同一个出口，尺寸一样 ───────────────────
			//
			// ⚠️ **主题要真的跟着换**：这一支得把深浅传进去，否则「深色」那一张
			// 和「浅色」那一张逐字节相同（那是同一张图存了两遍）。
			st := newSearchStub(10, 20)
			_, searchTT := newSearchPage(t, "周杰伦", st, systemIsDark(dark))
			waitFor(t, "搜索结果出来了", func() bool {
				searchTT.Frame()
				return searchTT.HasText(playAllLabel)
			})
			save(t, dir, "108-搜索页-全部播放-"+name+".png", searchTT)
		})
	}
}
