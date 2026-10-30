// 票 #683 复审里「落盘恢复」和「cid 写回」那两条（MAJOR 3、MINOR 8）。
//
// 这一份是 `package app_test`：两条都要真的走一遍起播（取流 + 解码），用的是
// `online_test.go` 那台假 B 站和真的 FLAC / WAV 素材。
package app_test

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestTheOldStoredCurrentStillMatchesTheEpisodeThatNowCarriesACid 钉住「升级后
// 第一次启动，上一次听的那一集和它的位置都还在」。
//
// 旧格式落盘的那一格长这样：条目的 ID 是 `mv:<bvid>`（票 #683 之前没有 `|<cid>`），
// cid 在单独的 `Cid` 栏里；`Current` 写的是 `mv:<bvid>`。而恢复出来的那一项
// `Identity()` 已经是 `mv:<bvid>|<cid>`——按严格相等找当前项必然找不到，
// 于是 idx 落到 0：**当前项变成第一首，位置也套到它身上**。
func TestTheOldStoredCurrentStillMatchesTheEpisodeThatNowCarriesACid(t *testing.T) {
	f := newStreamFixture(t)
	// ⚠️ **素材要够长**：位置断言要落在 60 秒那儿，短的会在恢复之前就放完。
	f.media = shortWav(120)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.wav", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := app.New()
	t.Cleanup(a.Close)
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.CloseSettings() })

	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir()})

	// 旧格式的那一份：ID 里没有 cid，当前项写的是 `mv:BV1`。
	if _, err := a.UpdateSettings(func(s *app.Settings) {
		s.Playback = app.PlaybackSnapshot{
			Items: []app.PlaybackEntry{
				{ID: "mv:BV0ther", Title: "另一首"},
				{ID: "mv:BV1", Cid: "333", Title: "上次那一集"},
			},
			Current:  "mv:BV1",
			Position: 60000,
		}
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.RestorePlayback(); err != nil {
		t.Fatalf("恢复不该报错：%v", err)
	}

	cur, ok := a.Queue().Current()
	if !ok {
		t.Fatal("恢复之后队列里没有当前项")
	}
	if cur.Identity() != "mv:BV1|333" {
		t.Errorf("恢复出来的当前项是 %q，want mv:BV1|333（旧格式的 `mv:BV1` 要按 bvid 对上"+
			"带 cid 的那一项，不能退回第一首）", cur.Identity())
	}

	waitFor(t, 30*time.Second, "恢复的那一集放起来", func() bool {
		return a.Snapshot().HasTrack
	})
	// 位置也是那一集的，不是第一首的。
	if got := a.Snapshot().Position; got < 55*time.Second || got > 65*time.Second {
		t.Errorf("恢复出来的位置是 %v，want 60s 附近（位置跟着当前项走，不能套到别的歌上）", got)
	}
}

// TestWritingBackTheCidCoversEveryCopyOfTheEpisode 钉住「队列里同一集排了两份时，
// 写回 cid 不会把抽屉那一行拆成两行」。
//
// 两份都还不知道是哪一集（`mv:<bvid>`），抽屉按身份去重只画一行。起播之后只补
// **当前那一份**的 cid 的话，它的身份变成 `mv:<bvid>|<cid>`，而另一份还是
// `mv:<bvid>`——抽屉立刻多出一行；用户按「从列表删除」也只删得掉其中一份。
func TestWritingBackTheCidCoversEveryCopyOfTheEpisode(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(60)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.wav", 2*time.Hour), int(bili.QualityHigh192), 110836))
	f.on("/x/player/pagelist", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": []any{map[string]any{"cid": 111, "page": 1, "part": "第一集", "duration": 60}}})
	})

	a := newOnlineApp(t, f, t.TempDir())
	// 两份同一集（同一个 bvid、都没有 cid）：用户从两个页面各加了一次。
	dup := onlineItem("BV1", "", "多 P 的一首")
	if err := a.PlayFrom(dup, dup); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "多 P 的一首")

	waitFor(t, 15*time.Second, "cid 写回队列", func() bool {
		items := a.Queue().Items()
		return len(items) == 2 && items[0].Cid != "" && items[1].Cid != ""
	})

	items := a.Queue().Items()
	if items[0].Identity() != items[1].Identity() {
		t.Fatalf("两份同一集的身份变成了 %q / %q——抽屉会把它画成两行",
			items[0].Identity(), items[1].Identity())
	}

	// 删那一行要真的删干净（抽屉那一行代表的是两份）。
	cur, _ := a.Queue().Current()
	if _, err := a.RemoveSongFromList(cur); err != nil {
		t.Fatalf("从列表删除：%v", err)
	}
	if n := a.Queue().Len(); n != 0 {
		t.Errorf("删掉那一行之后队列里还剩 %d 项，want 0（两份都是它）", n)
	}
}

// TestTheVideoRowIsNotMarkedWhileTheAudioWithTheSameNumberPlays 是「先判种类」的
// 整链版：真的把音乐库那一首放起来，再看收藏夹里 avid 恰好相同的视频行。
//
// ⚠️ 走的是真播放器（`newMusicFixture` 回音乐库取流 + 真 FLAC），断言的是
// `Snapshot().HasTrack` 之后 `IsPlaying` 的答案——不是「某个函数被调到了」。
func TestTheVideoRowIsNotMarkedWhileTheAudioWithTheSameNumberPlays(t *testing.T) {
	f := newMusicFixture(t)
	a := newOnlineApp(t, f, t.TempDir())

	if err := a.Enqueue(musicItem("1293494", "音乐库那一首")); err != nil {
		t.Fatalf("音乐库条目被拒了：%v", err)
	}
	playing(t, a, "音乐库那一首")

	if a.IsPlaying(app.Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD",
		Sid: "1293494", Title: "收藏夹里的视频"}) {
		t.Error("正在放的是音乐库那一首，而 avid=1293494 的视频行被标成了正在播放")
	}
	if !a.IsPlaying(musicItem("1293494", "音乐库那一首")) {
		t.Error("正在放的音乐库那一首自己没被认出来")
	}
}
