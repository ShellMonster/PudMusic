package ui

// 票 #299：「全部播放」与「立即播放」两枚的语义（2026-10-24 人定的）。
//
//	「全部播放」= 把这一页的歌按顺序加进队列并播放（从这一页的第一首开始）
//	「立即播放」= 只放当前这一首
//
// 这一组盯的是**两件事**：按下去之后队列变成什么样、正在放的是哪一首。
//
// ⚠️ **判据全在队列上**，不是「调了哪个函数」：接线换了而行为没变（或者反过来）
// 都是静默的，而队列是用户看得见的那一面。
//
// ⚠️ **夹具用本地歌**：在线的那些在测试里放不了（`internal/app` 的在线取流要真
// 网络，而真网络是**真账号**——铁律：不打真 B 站）。本地歌走的是开文件那一路，
// 放得起来，「正在放的是哪一首」才断得准。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// localSongs 造 n 首**本地**的歌：歌名是「本地 1」到「本地 n」，文件是同一个
// FLAC 素材的 n 份拷贝（路径不同——路径就是本地歌的身份，两首一样的路径在队列
// 里是同一首）。
func localSongs(t *testing.T, n int) []list.Music {
	t.Helper()
	b, err := os.ReadFile(flac(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := make([]list.Music, n)
	for i := range out {
		name := filepath.Join(dir, string(rune('a'+i))+".flac")
		if err := os.WriteFile(name, b, 0o644); err != nil {
			t.Fatal(err)
		}
		out[i] = list.Music{
			ID:    "local-" + string(rune('a'+i)),
			Title: "本地 " + string(rune('1'+i)),
			Owner: "本地的 UP 主",
			Item:  app.LocalItem(name),
		}
	}
	return out
}

// TestThePlayAllButtonAppendsThePageAndStartsAtItsFirstSong 钉住「全部播放」
// 的**那一句字**说的是什么：这一页的歌**按顺序加进队列**，并**从这一页的第一首
// 开始播**。
//
// 三个判据，缺一个都不算对：
//
//   - **队列里多了这一页的歌**，而且是这一页的**顺序**（打乱了的话用户听到的
//     顺序和屏幕上看到的不一样）。
//   - **队列里原来有的那几首还在**（这一枚是**追加**，不是换队列）。
//   - **正在放的是这一页的第一首**——不是队列里第一首（旧的那首），也不是最后
//     一首。
func TestThePlayAllButtonAppendsThePageAndStartsAtItsFirstSong(t *testing.T) {
	home := newTestShell(t, withSettings(app.DefaultSettings("")))
	old := localSongs(t, 1)
	page := localSongs(t, 3)

	// 队列里先放一首（旧的）：这一枚是**追加**，它不该把旧的挤掉。
	if err := home.s.app.Enqueue(old[0].Item); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "队列里那首开始播", func() bool { return home.s.app.Snapshot().HasTrack })

	home.s.home.SetSongs(homeSongs{
		tabs: [homeTabCount][]list.Music{0: page},
	})
	homeFrame(home.tt)
	btn, ok := home.tt.Find(playAllLabel)
	if !ok {
		t.Fatalf("首页上没有 %q 那一枚，文字有 %q", playAllLabel, home.tt.Texts())
	}
	home.tt.ClickAt(btn.X+btn.W/2, btn.Y+btn.H/2)
	home.tt.Frame()

	// 队列：旧的在前，这一页的三首按顺序在后。
	got := home.s.app.Queue().Items()
	want := []string{old[0].Item.Path, page[0].Item.Path, page[1].Item.Path, page[2].Item.Path}
	if len(got) != len(want) {
		t.Fatalf("按了 %q 之后队列里有 %d 首，want %d（先放的那一首 + 这一页的三首）",
			playAllLabel, len(got), len(want))
	}
	for i, w := range want {
		if got[i].Path != w {
			t.Errorf("队列第 %d 首是 %q，want %q（这一页的歌要按顺序进队列）", i+1, got[i].Path, w)
		}
	}

	// 正在放的是**这一页的第一首**。
	waitFor(t, "这一页的第一首开始播", func() bool {
		home.tt.Frame()
		return home.s.app.Snapshot().Title == page[0].Item.Title
	})
	if got := home.s.app.Queue().CurrentIndex(); got != 1 {
		t.Errorf("正在放的是队列第 %d 首，want 第 2 首（旧的那首之后就是这一页的第一首）", got+1)
	}
	// ⚠️ **说一句话**：按下去之后界面上要看得见「加了首、开始播了」——不然用户
	// 只看见队列悄悄长了一截（队列在播放条那儿，未必看得到）。
	if !home.tt.HasText("已添加 3 首到播放列表") {
		t.Errorf("没有说加了几首，文字有 %q", home.tt.Texts())
	}
}

// TestThePlayNowEntryPlaysOnlyThatSong 钉住「立即播放」的**那一句字**说的是什么：
// **只放当前这一首**。
//
// ⚠️ **它和「全部播放」差在队列里剩下什么**：那一枚把这一页的歌都排上，这一枚
// **只放按下去的那一首**——用户正在听的那首不该被顺带排到这一页后面去。
func TestThePlayNowEntryPlaysOnlyThatSong(t *testing.T) {
	home := newTestShell(t, withSettings(app.DefaultSettings("")))
	old := localSongs(t, 1)
	page := localSongs(t, 3)

	if err := home.s.app.Enqueue(old[0].Item); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "队列里那首开始播", func() bool { return home.s.app.Snapshot().HasTrack })

	home.s.home.SetSongs(homeSongs{
		tabs: [homeTabCount][]list.Music{0: page},
	})
	homeFrame(home.tt)

	if err := home.tt.RightClick(page[1].Title); err != nil {
		t.Fatalf("右键点不到那一行：%v，文字有 %q", err, home.tt.Texts())
	}
	if err := home.tt.ChooseMenuItem("立即播放"); err != nil {
		t.Fatalf("菜单里点不了「立即播放」：%v", err)
	}
	home.tt.Frame()

	// 队列里**只有**按下去的那一首。
	got := home.s.app.Queue().Items()
	if len(got) != 1 {
		t.Fatalf("按了「立即播放」之后队列里有 %d 首，want 1（只放当前这一首）：%v", len(got), got)
	}
	if got[0].Path != page[1].Item.Path {
		t.Errorf("放的是 %q，want %q（用户按下去的那一首）", got[0].Path, page[1].Item.Path)
	}
	waitFor(t, "那一首开始播", func() bool {
		home.tt.Frame()
		return home.s.app.Snapshot().Title == page[1].Item.Title
	})
}

// TestBothPlayEntriesAreOnTheRowMenu 钉住**两枚入口都在**。
//
// 这一条不是废话：菜单是这一趟全部建出来的，少建一项那一项就永远点不到
// （`mygo/ui/context-menu.md`）——而「两枚都在」正是这一票要的。
func TestBothPlayEntriesAreOnTheRowMenu(t *testing.T) {
	home := newTestShell(t, withSettings(app.DefaultSettings("")))
	page := localSongs(t, 1)
	home.s.home.SetSongs(homeSongs{
		tabs: [homeTabCount][]list.Music{0: page},
	})
	homeFrame(home.tt)

	if err := home.tt.RightClick(page[0].Title); err != nil {
		t.Fatalf("右键点不到那一行：%v，文字有 %q", err, home.tt.Texts())
	}
	items := home.tt.Menu()
	for _, want := range []string{"立即播放", "从这一首往后听"} {
		found := false
		for _, it := range items {
			if it == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("菜单上没有 %q 那一项，菜单上有 %q", want, items)
		}
	}
}
