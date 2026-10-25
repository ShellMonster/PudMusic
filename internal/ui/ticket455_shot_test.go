package ui

// 票 #455（BMG-252）的画面验收：把「失败」和「没有」分开说。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket455Shots
//
// # 深浅两套都要出
//
// 这一票改的每一处都是**颜色 / 形状**：
//
//   - 「用户」那一档封面上的那一枚图形（原来是一枚播放三角，现在是一个「人」）；
//   - 失败那一格（原来没有这一格）。
//
// 浅色和深色下那一枚图形的对比度不一样（白的一层压在压暗的封面上，深色那一套
// 的封面本来就暗），而失败那一格用的是 `t.Warning`——深浅两套那是两个数。所以
// 只出一套看不出问题。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// TestRenderTicket455Shots 存这一票改过的那三处，深浅各一套。
func TestRenderTicket455Shots(t *testing.T) {
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
			// ① 搜索页「用户」那一档：指针停在第一行上——封面上那一枚是「一个人」，
			// 不是播放三角了。
			st := newSearchStub(6, 6)
			s, tt := newSearchPage(t, "周杰伦", st, systemIsDark(dark))
			settleText(t, tt, "第 1 页 第 1 首")
			if err := tt.Click("用户"); err != nil {
				t.Fatalf("点不到「用户」那个 Tab：%v", err)
			}
			waitFrames(t, tt, "用户那一页进来", loadedTo(s, searchTabUser, 1))
			hoverShellRow(t, tt, "第 1 页 第 1 首")
			save(t, dir, "455-1-用户那一档-进他的空间-"+name+".png", tt)

			// ①b 同一件事的**网格**那一档：右下角那枚主色圆钮里是一个「人」。
			set := app.DefaultSettings("")
			set.DisplayMode = app.DisplayModeCard
			stCard := newSearchStub(6, 6)
			sCard, ttCard := newSearchPage(t, "周杰伦", stCard, withSettings(set), systemIsDark(dark))
			settleText(t, ttCard, "第 1 页 第 1 首")
			if err := ttCard.Click("用户"); err != nil {
				t.Fatalf("点不到「用户」那个 Tab：%v", err)
			}
			waitFrames(t, ttCard, "用户那一页进来", loadedTo(sCard, searchTabUser, 1))
			// ⚠️ **悬停第二格**：网格那一档里框架给**每一行**标了那一行第一首歌名，
			// 于是第一首给回的是整行那个盒子（指针落在行边上），第二格才是自己
			// 那一小块。
			hoverShellRow(t, ttCard, "第 1 页 第 2 首")
			save(t, dir, "455-1b-用户那一档-网格-"+name+".png", ttCard)

			// ② 搜索页拉失败：失败自己的说法 + 一个「重试搜索」。
			st2 := newSearchStub(24, 24)
			st2.fail = searchFetchFail
			s2, tt2 := newSearchPage(t, "周杰伦", st2, systemIsDark(dark))
			waitFrames(t, tt2, "拉失败了", func() bool {
				return s2.search.lists[searchTabSong].list.Paged().Err != nil
			})
			save(t, dir, "455-2-搜索页拉失败-"+name+".png", tt2)

			// ③ 联想词那一档拉失败：那一档说自己的话（「没能取到联想词」）。
			f := newBiliFixture(t, 1)
			f.setSuggestFail(-412)
			s3, tt3 := newSuggestShell(t, f, systemIsDark(dark))
			s3.hist.add("林俊杰")
			tt3.Frame()
			typeInSearch(t, tt3, "林俊杰")
			settleSuggestDebounce(s3, tt3)
			waitFor(t, "那一次有问有答", func() bool { return f.suggestServed() >= 1 })
			waitFrames(t, tt3, "失败之后那一帧", func() bool { return !s3.app.SuggestState().Loading })
			if !tt3.HasText(suggestFailedNote) {
				t.Fatalf("联想词那一档没有那句自己的话，文字有 %q", tt3.Texts())
			}
			save(t, dir, "455-3-联想词拉失败-"+name+".png", tt3)
		})
	}
}
