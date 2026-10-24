package ui

import (
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// seedThreeSongs 用三个不同路径的本地音频填队列，并确认旧歌确实在播。
func seedThreeSongs(t *testing.T, a *app.App) []app.Item {
	t.Helper()
	songs := localSongs(t, 3)
	items := []app.Item{songs[0].Item, songs[1].Item, songs[2].Item}
	if err := a.Enqueue(items...); err != nil {
		t.Fatal(err)
	}
	assertQueueAndPlaying(t, a, items, 0)
	return items
}

// mountQueueStreaming 的客户端和媒体请求都指向本地夹具，不读真实账号。
//
// ⚠️ **另起一台夹具**：调用点的页面（搜索页）用的是自己的假拉页器，它不伺服
// 取流与音频。页面**已经**挂好取流的地方（收藏夹侧栏、动态卡片）不要调这个
// ——那会把页面正在用的那个客户端换掉。
func mountQueueStreaming(t *testing.T, a *app.App) {
	t.Helper()
	f := newBiliFixture(t, 1)
	a.MountStreaming(app.Streaming{Client: f.client(), Media: f.Client(), TempDir: t.TempDir()})
}

func assertQueueAndPlaying(t *testing.T, a *app.App, want []app.Item, current int) {
	t.Helper()
	waitFor(t, "目标音频真正开始播放", func() bool {
		st := a.Snapshot()
		it := want[current]
		if !st.HasTrack || !st.Playing || st.Source != it.Source {
			return false
		}
		if it.IsLocal() {
			return st.Path == it.Path
		}
		return st.Title == it.Title
	})
	got := a.Queue().Items()
	if len(got) != len(want) {
		t.Fatalf("队列有 %d 首，want %d：%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Identity() != want[i].Identity() {
			t.Errorf("队列第 %d 项是 %q，want %q", i, got[i].Identity(), want[i].Identity())
		}
	}
	if got := a.Queue().CurrentIndex(); got != current {
		t.Errorf("当前项 = %d，want %d", got, current)
	}
}

// queuePlus 返回「原来的队列 + 追加这一首」——一份新的切片，不动 old 本身。
func queuePlus(old []app.Item, it app.Item) []app.Item {
	out := make([]app.Item, 0, len(old)+1)
	out = append(out, old...)
	return append(out, it)
}

// 批量入口里的另三处；首页、搜索页与 UP 空间共走 playAllSongs，在
// ticket244 / ticket299 里点真实按钮验。
func TestOtherBatchEntriesReplaceQueueAndStartFirst(t *testing.T) {
	t.Run("本地页", func(t *testing.T) {
		songs := localSongs(t, 2)
		tt, s, a, _ := shellOnLocalMusic(t, filepath.Dir(songs[0].Item.Path))
		seedThreeSongs(t, a)
		waitFrames(t, tt, "本地两首扫描并显示", func() bool { return len(s.local.rows) == 2 })
		want := s.local.itemsOf(s.local.rows)
		if err := tt.Click("播放全部"); err != nil {
			t.Fatal(err)
		}
		assertQueueAndPlaying(t, a, want, 0)
	})
	t.Run("收藏夹页", func(t *testing.T) {
		_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
		// ⚠️ **取流要另挂**：这个收藏夹的夹具只伺服收藏夹内容，不伺服取流
		// （`/x/player/pagelist` 那边它 404）。
		mountQueueStreaming(t, s.app)
		waitSongs(t, tt, s, 2)
		seedThreeSongs(t, s.app)
		want := itemsOfMedias(s.allMedias(s.collection))
		if err := tt.Click("播放全部"); err != nil {
			t.Fatal(err)
		}
		assertQueueAndPlaying(t, s.app, want, 0)
	})
	t.Run("侧栏收藏夹", func(t *testing.T) {
		// ⚠️ **取流要另挂**：`favWireShell` 挂的那一份是收藏夹内容的客户端，
		// 取流与音频它不伺服。
		_, a, tt, _ := favWireShell(t, 2, 0)
		mountQueueStreaming(t, a)
		seedThreeSongs(t, a)
		if err := tt.RightClick("我建的 1"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem("播放"); err != nil {
			t.Fatal(err)
		}
		want := []app.Item{
			{Source: app.SourceOnline, Type: "mv", Bvid: "BV11", Title: "歌 一"},
			{Source: app.SourceOnline, Type: "mv", Bvid: "BV12", Title: "歌 二"},
		}
		assertQueueAndPlaying(t, a, want, 0)
	})
}

// 四档真实菜单从同样的三首旧歌出发；除立即播放外，另外三档必须保持既有行为。
func TestTheFourRowQueueActions(t *testing.T) {
	for _, action := range []string{"立即播放", "从这一首往后听", "下一首播放", "添加到队列"} {
		t.Run(action, func(t *testing.T) {
			b := newTestShell(t, withSettings(app.DefaultSettings("")))
			old := seedThreeSongs(t, b.a)
			songs := localSongs(t, 3)
			b.s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: songs}})
			homeFrame(b.tt)
			if err := b.tt.RightClick(songs[1].Title); err != nil {
				t.Fatal(err)
			}
			if err := b.tt.ChooseMenuItem(action); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "立即播放":
				// 队列变成四首：旧的三首 + 新点的那首（追加到队尾），正在放的是第四首。
				assertQueueAndPlaying(t, b.a, queuePlus(old, songs[1].Item), 3)
			case "从这一首往后听":
				// 换成从这一首到本页末尾那一段（两首），正在放的是第一首。
				assertQueueAndPlaying(t, b.a, []app.Item{songs[1].Item, songs[2].Item}, 0)
			case "下一首播放":
				// 插在**正在放的那首后面**（第一首之后），队列还是四首，当前项不动。
				assertQueueAndPlaying(t, b.a, []app.Item{old[0], songs[1].Item, old[1], old[2]}, 0)
			case "添加到队列":
				// 加到**队尾**，不打断：队列四首，当前项还是第一首。
				assertQueueAndPlaying(t, b.a, queuePlus(old, songs[1].Item), 0)
			}
		})
	}
}

func TestSingleSongCardsPreserveTheQueue(t *testing.T) {
	t.Run("新歌速递", func(t *testing.T) {
		b := newTestShell(t, withSettings(app.DefaultSettings("")))
		old := seedThreeSongs(t, b.a)
		songs := localSongs(t, 3)
		_, tt := newGridTester(t, list.AppActions(b.a, b.s.addToast), songs, 800, 900)
		if err := tt.Click(songs[1].Title); err != nil {
			t.Fatal(err)
		}
		assertQueueAndPlaying(t, b.a, queuePlus(old, songs[1].Item), 3)
	})
	t.Run("动态视频", func(t *testing.T) {
		// ⚠️ **取流要另挂**：`dynamicCardShell` 那个假 B 站不伺服取流与音频
		// （分 P 列表那一条它返回的是数组，读不懂）。
		_, tt, a := dynamicCardShell(t, newUIExplore(t), uiDynamicArchive("v1", "BV1play"))
		mountQueueStreaming(t, a)
		old := seedThreeSongs(t, a)
		if err := tt.Click("投稿 BV1play"); err != nil {
			t.Fatal(err)
		}
		song := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BV1play", Title: "投稿 BV1play"}
		assertQueueAndPlaying(t, a, queuePlus(old, song), 3)
	})
}

func TestLibraryPlayNowPreservesTheQueue(t *testing.T) {
	for _, path := range []string{laterPath, historyPath} {
		t.Run(path, func(t *testing.T) {
			f := newLibFake(t).withLater(1, 2, "甲", "乙").withHistory(1, 10, 200, "甲", "乙")
			tt, s := libraryShell(t, f, path)
			mountQueueStreaming(t, s.app)
			waitFrames(t, tt, "两行歌曲显示", func() bool { return rowsShown(s.pageOf(path)) == 2 })
			old := seedThreeSongs(t, s.app)
			row, ok := s.pageOf(path).rows.Row(1)
			if !ok {
				t.Fatal("没有第二行")
			}
			if err := tt.RightClick("乙"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem("立即播放"); err != nil {
				t.Fatal(err)
			}
			assertQueueAndPlaying(t, s.app, queuePlus(old, row.Item), 3)
		})
	}
}
