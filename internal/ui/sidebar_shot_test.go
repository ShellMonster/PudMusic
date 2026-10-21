package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一族把**侧栏与收藏夹**这一块存成 PNG，留给人眼和 Biu 上游的截图并排看。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSidebarShots
//
//	01 折叠态的侧栏，鼠标停在其中一项上（**提示要说清它是什么**）
//	02 收藏夹页的操作条：那个按歌名找的输入框和排序并排
//	03 侧栏上一条很长的收藏夹名（**单行 + 省略号**，不换行）
//
// ⚠️ **深浅两套都要出**：提示那一块是反色底（`Inverse`），深浅两套的底色
// 完全不同，而收藏夹名的省略号落在侧栏那条底色上——只看浅色那一套，
// 「看不看得见」这件事就只验了一半。
//
// ⚠️ **一个字都不许打真 B 站接口**：收藏夹页那个是内存里的假 B 站
// （`favBili`），其余全是内存夹具 + 无头渲染。
func TestRenderSidebarShots(t *testing.T) {
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
			// 01 折叠态 + 悬停提示。
			//
			// ⚠️ 提示要**指针停够 0.6 秒**才出（`mygo/ui/tooltip.go` 的
			// tooltipDelay），所以是 Move + Advance + Frame。
			{
				st := app.DefaultSettings("")
				st.SideMenuCollapsed = true
				b := newTestShell(t, systemIsDark(dark), withSettings(st),
					withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
				tt := b.tt
				tt.Frame()
				r, ok := tt.Find("下")
				if !ok {
					t.Fatalf("折叠态的侧栏上没有「下载记录」那一个字，文字有 %q", tt.Texts())
				}
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				tt.Advance(700 * time.Millisecond)
				tt.Frame()
				save(t, dir, "01-折叠提示-"+name+".png", tt)
				b.Close()
			}

			// 02 收藏夹页：那个输入框和排序并排。
			{
				_, s, tt := collectionShellOpts(t, []shellOpt{systemIsDark(dark)}, "", "12345",
					mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
				waitSongs(t, tt, s, 3)
				tt.Move(700, 500)
				homeFrame(tt)
				save(t, dir, "02-收藏夹搜索框-"+name+".png", tt)
			}

			// 03 侧栏上一条很长的收藏夹名：单行 + 省略号，不换行。
			{
				st := app.DefaultSettings("")
				b := newTestShell(t, systemIsDark(dark), withSettings(st),
					withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
				tt := b.tt
				b.a.SetFavorites(app.FavoritesCreated, []app.Favorite{
					{ID: 1, Title: "【长期主义】2026 年度歌单合集·最终版", Mid: 42, Type: 2},
					{ID: 2, Title: "短名字", Mid: 42, Type: 2},
				})
				tt.Frame()
				tt.Move(700, 500)
				homeFrame(tt)
				save(t, dir, "03-长收藏夹名-"+name+".png", tt)
				b.Close()
			}
		})
	}
}
