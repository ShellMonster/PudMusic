// 多 P 投稿的「换集」整链用例（票 #683）。
//
// 这一份是 `package app_test`：它要真的取流、真的出声，所以用 `online_test.go`
// 那台假 B 站和播放器夹具（`newStreamFixture` / `newOnlineApp` / `playing`）。
//
// 判重那一半（`trackKey` / `Identity` / `SameTrack`）在
// `multipart_identity_test.go`，那一份是 `package app`（要读未导出的 `trackKey`）。
package app_test

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// twoEpisodes 造队列里那两个「同一个 bvid、不同 cid」的项。
//
// 历史记录就是这种形状：一集一条，而 bvid 每一集都一样。
func twoEpisodes() []app.Item {
	return []app.Item{
		{Source: "online", Type: "mv", Bvid: "BV1", Cid: "111", Title: "第一集"},
		{Source: "online", Type: "mv", Bvid: "BV1", Cid: "222", Title: "第二集"},
	}
}

// TestPlayAtSwitchesBetweenTwoEpisodesOfOneVideo 钉住 `PlayAt` 真能换集。
//
// 队列里是同一个 bvid 的两集。点第二项要**真的放第二集**——原来 `sameSong` 判真，
// 于是只把指针挪过去、播放器还在放第一集（`Snapshot().Title` 还是第一集），
// 用户按了没反应。
func TestPlayAtSwitchesBetweenTwoEpisodesOfOneVideo(t *testing.T) {
	f := newStreamFixture(t)
	// ⚠️ **素材要够长**：短的（那份 FLAC 不到两秒）放完会自动切到下一首，
	// 于是「切过去了吗」这一问会被自动切歌冒充成「切过去了」——判据就失效了。
	f.media = shortWav(120)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.wav", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(twoEpisodes()...); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一集")

	if err := a.PlayAt(1); err != nil {
		t.Fatalf("PlayAt(1)：%v", err)
	}
	playing(t, a, "第二集")

	cur, ok := a.Queue().Current()
	if !ok || cur.Cid != "222" {
		t.Errorf("队列当前项是 %+v，want cid=222", cur)
	}
}

// TestEnqueueAndPlayNowPlaysTheEpisodeAskedFor 钉住「点第二集放第二集」。
//
// 原来按 bvid 找到队列里的第一集，于是用户点第二集放的是
// 第一集——而播放条上的名字也是第一集，用户唯一能察觉的是「我点的不是这首」。
func TestEnqueueAndPlayNowPlaysTheEpisodeAskedFor(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(120)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.wav", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(twoEpisodes()[0]); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一集")

	// 用户点第二集：它不在队列里（判重标识不同了），所以**追加**一项再播。
	second := twoEpisodes()[1]
	if err := a.EnqueueAndPlayNow(second); err != nil {
		t.Fatalf("EnqueueAndPlayNow：%v", err)
	}
	playing(t, a, "第二集")

	items := a.Queue().Items()
	if len(items) != 2 {
		t.Fatalf("队列里有 %d 项，want 2（第二集是另外一个身份）：%+v", len(items), items)
	}
	if cur := a.Queue().CurrentIndex(); cur != 1 {
		t.Errorf("当前项是第 %d 项，want 1（刚追加的那一项）", cur)
	}
}
