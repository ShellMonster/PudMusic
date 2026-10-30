// 网格那一档的「⌘C 复制选中那一格的歌名」（票 #685）。
//
// # 为什么网格也要
//
// 收藏夹 / 搜索 / UP 空间按设置可以在列表和网格之间切（`SwitchView`），而
// 「选一首、⌘C」是同一件事——只在列表那一档做的话，同一个页面换个摆法 ⌘C
// 就哑了。
//
// ⚠️ **网格的「选中」和列表不是同一个东西**：网格的游标是自己的字段
// （`g.sel`，格子是布局，框架那套行游标按不了格子），所以判据走
// `Grid.Selected()` ——同一个出口，不另读 `g.sel`。
package list

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/egoist/mygo/ui"
)

// TestCmdCCopiesTheSelectedCell 断**网格里选中一格之后 ⌘C 复制的是那一格的歌名**。
//
// 判据是**剪贴板里那段字**，两条入口（按键 / 编辑菜单）都走一遍——理由同
// `copy_title_test.go` 的 `TestCmdCCopiesTheSelectedRowTitle`。
func TestCmdCCopiesTheSelectedCell(t *testing.T) {
	for _, route := range []struct {
		name  string
		press func(*ui.Tester)
	}{
		{"按键", func(tt *ui.Tester) { tt.Key(ui.Cmd, ui.KeyC) }},
		{"编辑菜单", func(tt *ui.Tester) { tt.Command("copy") }},
	} {
		t.Run(route.name, func(t *testing.T) {
			var copied []string
			acts := Actions{
				Copy:   func(text string) { copied = append(copied, text) },
				Notify: func(string, notice.Tone) {},
			}
			g, tt := newGrid(t, acts, []Music{
				{ID: "a", Title: "第一首歌", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "第一首歌"}},
				{ID: "b", Title: "第二首歌", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV2", Title: "第二首歌"}},
			}, 900, 700)

			// 用坐标点第二格：网格卡片里那枚歌名是 `Find` 找得到的。
			r, ok := tt.Find("第二首歌")
			if !ok {
				t.Fatalf("画面上找不到第二张卡，文字有 %q", tt.Texts())
			}
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			tt.Frame()
			if got, ok := g.Selected(); !ok || got.ID != "b" {
				t.Fatalf("点了第二张卡之后 Selected() = (%q, %v)，want (b, true)", got.ID, ok)
			}

			route.press(tt)
			tt.Frame()

			if len(copied) != 1 || copied[0] != "第二首歌" {
				t.Errorf("%s之后复制出来的是 %q，want 一条「第二首歌」", route.name, copied)
			}
		})
	}
}

// TestCmdCInTheGridCopiesNothingWithoutASelection 断**网格没选中时就别动剪贴板**。
func TestCmdCInTheGridCopiesNothingWithoutASelection(t *testing.T) {
	var copied []string
	g, tt := newGrid(t, Actions{Copy: func(text string) { copied = append(copied, text) }}, []Music{
		{ID: "a", Title: "第一首歌", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "第一首歌"}},
	}, 900, 700)

	if _, ok := g.Selected(); ok {
		t.Fatalf("刚画出来就选中了一格")
	}
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	tt.Command("copy")
	tt.Frame()

	if len(copied) != 0 {
		t.Errorf("没有选中任何一格，却复制了 %q", copied)
	}
}
