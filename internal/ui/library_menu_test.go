package ui

import (
	"slices"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 稍后再看 / 历史记录这两页的行右键菜单（对照审计的 A9：各页菜单逐页补
// 「下一首播放」）。
//
// 这一票之前只有动态卡片那一页有这一项（它是顺手做的），内容列表和这两页都
// 没有——而用户最常按「下一首播放」的地方恰恰是这两页：他在翻「稍后再看」，
// 听见了一首不喜欢的，于是想把下一首先换掉。
//
// 断的是**菜单上真的有哪几项**（`tt.Menu()`，`-` 是分隔），不是「代码里写了
// 几个分支」：菜单是这一趟全部建出来的，少建一项那一项就永远点不到
// （`mygo/ui/context-menu.md`）。

// TestBothLibraryPagesOfferPlayNextInTheRowMenu 钉住两页都摆了这一项。
func TestBothLibraryPagesOfferPlayNextInTheRowMenu(t *testing.T) {
	for _, c := range []struct{ path, title string }{
		{laterPath, "稍后再看"},
		{historyPath, "历史记录"},
	} {
		t.Run(c.title, func(t *testing.T) {
			f := newLibFake(t).
				withLater(1, 2, "甲", "乙").
				withHistory(1, 10, 200, "甲", "乙")
			tt, s := libraryShell(t, f, c.path)
			settle(t, tt, "把内容拉回来", func() bool { return rowsShown(s.pageOf(c.path)) == 2 })

			if err := tt.RightClick("甲"); err != nil {
				t.Fatalf("右键那一行开不出菜单：%v，文字有 %q", err, tt.Texts())
			}
			got := tt.Menu()
			if !slices.Contains(got, "下一首播放") {
				t.Errorf("%s 的行菜单里没有「下一首播放」：%q", c.title, got)
			}
			// 它和「播放」「添加到队列」是同一件事的三档，得挨着摆
			// （Biu 的 `music-recommend/index.tsx:204-213` 就是这个顺序）。
			if i, j, k := slices.Index(got, "播放"), slices.Index(got, "下一首播放"), slices.Index(got, "添加到队列"); !(i < j && j < k) {
				t.Errorf("「下一首播放」不在「播放」和「添加到队列」之间：%q", got)
			}
		})
	}
}

// TestChoosingPlayNextPutsItRightAfterTheCurrentOne 钉住**真的插在当前后面**。
//
// 菜单上有这一项还不够——它必须接的是 `App.EnqueueNext` 而不是 `App.Enqueue`。
// 接成接队尾的话，界面上看不出任何异样，而用户要等到整张列表放完才听得见
// （症状是「按了，什么也没发生，直到一小时后」）。
//
// 队列先摆好三首本地文件（`Queue.Reset` 直接摆，不起播），于是「正在放的那首」
// 是第一首，而被插进去的应该是第二首。
func TestChoosingPlayNextPutsItRightAfterTheCurrentOne(t *testing.T) {
	f := newLibFake(t).
		withLater(1, 2, "甲", "乙").
		withHistory(1, 10, 200, "甲", "乙")
	tt, s := libraryShell(t, f, laterPath)
	settle(t, tt, "把内容拉回来", func() bool { return rowsShown(s.pageOf(laterPath)) == 2 })

	if _, ok := s.app.Queue().Reset(
		app.LocalItem("/听着的.flac"), app.LocalItem("/后面的.flac"),
	); !ok {
		t.Fatal("队列摆不进去")
	}
	before := len(s.app.Queue().Items())

	if err := tt.RightClick("甲"); err != nil {
		t.Fatalf("右键那一行开不出菜单：%v，文字有 %q", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem("下一首播放"); err != nil {
		t.Fatalf("选不了「下一首播放」：%v", err)
	}

	items := s.app.Queue().Items()
	if len(items) != before+1 {
		t.Fatalf("按了一下，队列里是 %d 首，want %d 首", len(items), before+1)
	}
	// 插进来的应该是第二首（第一首后面），不是队尾。
	if items[1].Title != "甲" && items[1].Title != "乙" {
		t.Errorf("第二首是 %q——它应该是被插进来的那一首（按的是「甲」）", items[1].Title)
	}
	if items[len(items)-1].Title == items[1].Title {
		t.Errorf("被插进来的那一首排在队尾了（%v）——"+
			"「下一首播放」接成了「添加到队列」", queueTitles(items))
	}
	// 当前项没被换：用户正在听的那首还在放。
	// `LocalItem` 会去掉扩展名，所以歌名是「听着的」。
	if cur, ok := s.app.Queue().Current(); !ok || cur.Title != "听着的" {
		t.Errorf("按了一下「下一首播放」，当前项不再是正在听的那首（现在是 %q）——"+
			"用户正在听的那一首不该因为按了「下一首」而被换掉", cur.Title)
	}
}

// queueTitles 把队列里几首的歌名列出来，好让报错那句话读得懂。
func queueTitles(items []app.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}
