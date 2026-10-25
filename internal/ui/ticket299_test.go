package ui

// 票 #337 覆盖 #299：全部播放替换这一批；立即播放追加单首并起播。
// 本地音频夹具走真实播放器，不访问真 B 站。

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

// TestThePlayAllButtonReplacesQueueAndStartsAtItsFirstSong 钉住「全部播放」的语义：
// **换成这一页的歌，并从这一页的第一首开始播**（票 #337）。
//
// 先排三首旧歌，再点批量入口；旧队列必须消失，新的三首必须按页面顺序成为
// 完整队列，当前项是新批次第一首。
func TestThePlayAllButtonReplacesQueueAndStartsAtItsFirstSong(t *testing.T) {
	home := newTestShell(t, withSettings(app.DefaultSettings("")))
	old := localSongs(t, 3)
	page := localSongs(t, 3)

	if err := home.s.app.Enqueue(old[0].Item, old[1].Item, old[2].Item); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "旧队列第一首开始播", func() bool { return home.s.app.Snapshot().HasTrack })

	home.s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: page}})
	homeFrame(home.tt)
	btn, ok := home.tt.Find(playAllLabel)
	if !ok {
		t.Fatalf("首页上没有 %q 那一枚，文字有 %q", playAllLabel, home.tt.Texts())
	}
	home.tt.ClickAt(btn.X+btn.W/2, btn.Y+btn.H/2)
	home.tt.Frame()

	got := home.s.app.Queue().Items()
	want := []string{page[0].Item.Path, page[1].Item.Path, page[2].Item.Path}
	if len(got) != len(want) {
		t.Fatalf("按了 %q 之后队列里有 %d 首，want %d（旧队列应被这一批替换）", playAllLabel, len(got), len(want))
	}
	for i, w := range want {
		if got[i].Path != w {
			t.Errorf("队列第 %d 首是 %q，want %q", i+1, got[i].Path, w)
		}
	}
	waitFor(t, "这一页的第一首开始播", func() bool {
		home.tt.Frame()
		st := home.s.app.Snapshot()
		return st.HasTrack && st.Playing && st.Path == page[0].Item.Path
	})
	if got := home.s.app.Queue().CurrentIndex(); got != 0 {
		t.Errorf("正在放的是队列第 %d 首，want 第 1 首", got+1)
	}
	if !home.tt.HasText("已切换到 3 首") {
		t.Errorf("没有说已经切换到这一批，文字有 %q", home.tt.Texts())
	}
}

// TestThePlayNowEntryAppendsAndStartsThatSong 钉住行级「立即播放」：**加这一首
// 并立即播，队列里原来有的歌保留**（票 #337）。
func TestThePlayNowEntryAppendsAndStartsThatSong(t *testing.T) {
	home := newTestShell(t, withSettings(app.DefaultSettings("")))
	old := localSongs(t, 3)
	page := localSongs(t, 3)

	if err := home.s.app.Enqueue(old[0].Item, old[1].Item, old[2].Item); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "旧队列第一首开始播", func() bool { return home.s.app.Snapshot().HasTrack })

	home.s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: page}})
	homeFrame(home.tt)
	if err := home.tt.RightClick(page[1].Title); err != nil {
		t.Fatalf("右键点不到那一行：%v，文字有 %q", err, home.tt.Texts())
	}
	if err := home.tt.ChooseMenuItem("立即播放"); err != nil {
		t.Fatalf("菜单里点不了「立即播放」：%v", err)
	}
	home.tt.Frame()

	got := home.s.app.Queue().Items()
	want := []string{old[0].Item.Path, old[1].Item.Path, old[2].Item.Path, page[1].Item.Path}
	if len(got) != len(want) {
		t.Fatalf("按了「立即播放」之后队列里有 %d 首，want 4（原来的三首 + 新歌）: %v", len(got), got)
	}
	for i, w := range want {
		if got[i].Path != w {
			t.Errorf("队列第 %d 首是 %q，want %q", i+1, got[i].Path, w)
		}
	}
	waitFor(t, "那一首开始播", func() bool {
		home.tt.Frame()
		st := home.s.app.Snapshot()
		return st.HasTrack && st.Playing && st.Path == page[1].Item.Path
	})
	if got := home.s.app.Queue().CurrentIndex(); got != 3 {
		t.Errorf("正在放的是队列第 %d 首，want 第 4 首", got+1)
	}
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
