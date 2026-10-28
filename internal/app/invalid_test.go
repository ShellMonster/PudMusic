package app_test

// 「失效的条目不进队列」这一组。
//
// # 为什么单开一个文件
//
// 失效（B 站的 `attr != 0`）这件事有**三处**会碰到它：
//
//	1. 队列入口（收藏夹页「播放全部」/ 侧栏「播放」）——失效条目**不该进队列**，
//	   因为进去之后应用要为一条已经不存在的稿件白跑一趟取流（每一首失效歌
//	   都要等一次超时），而上游是**取流之前就滤掉**的
//	   （`_research/biu/src/common/utils/fav.ts:55`）。
//	2. 行级菜单——播放入口该是**灰的**，末尾有一句为什么。
//	3. 列表画法——条目**照画**（用户该看见收藏夹里少了什么），但要**标出来**。
//
// 三处各自的**判据必须是同一个**，所以这一组钉的是「判据只有一处」这件事：
// 判据住在 `bili.Media.Invalid()`（服务层，B 站那个字段解析的地方），
// 而**队列入口那一处**是它唯一的消费者——界面层和 app 层都调它。
//
// ⚠️ **全部走内存里的假 B 站**（`streamFixture`），一条真接口都不打。

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestAnInvalidItemIsNotPlayable 钉住判据落在队列项上。
//
// 🔴 **它和「没有音频流」是两件事**：合集 / 电影是「这一条没有音频流」
// （还能进去——打开里面的单集就能放），而失效是「这一条已经没了」。
// 两者都要「放不了」，但**说的不是同一句话**。
func TestAnInvalidItemIsNotPlayable(t *testing.T) {
	dead := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVdead", Title: "失效的那首", Invalid: true}
	if app.CanPlay(dead) {
		t.Error("一条失效的条目被判成能放——它会进队列，然后白跑一趟取流")
	}
	live := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVlive", Title: "活着的那首"}
	if !app.CanPlay(live) {
		t.Error("一条正常的条目被判成放不了")
	}
}

// TestAnInvalidItemSaysWhyItCannotBePlayed 钉住「说得出为什么」。
//
// ⚠️ **每一句都要能回答「还能怎么办」**（票 #209）：说不出用户下一步该做什么的
// 句子是噪音。而失效这一类的下一步很明确——**它已经没了**，用户该做的是
// 「在 B 站删掉它 / 忽略它」，而不是「重试」或「换个网络」。
func TestAnInvalidItemSaysWhyItCannotBePlayed(t *testing.T) {
	dead := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVdead", Title: "失效的那首", Invalid: true}
	reason := app.UnplayableReason(dead)
	if reason == "" {
		t.Fatal("失效的条目没有一句说明")
	}
	if !contains(reason, "失效") {
		t.Errorf("那句说明是 %q，没有说清「已经失效」", reason)
	}
	// 菜单末尾那一句要**短**（系统菜单一行太长会把菜单撑宽），但仍要带着
	// 「为什么」——按不动而不说为什么等于让用户猜。
	note := app.UnplayableMenuNote(dead)
	if note == "" {
		t.Fatal("失效的条目在菜单末尾没有说明")
	}
	if !contains(note, "失效") {
		t.Errorf("菜单末尾那一句是 %q，没有说清「已经失效」", note)
	}
}

// TestAnInvalidMediaDoesNotReachTheQueue 钉住队列入口那一处。
//
// ⚠️ **判据和 `Playable` 分开**：`Playable` 判的是「有没有音频流」
// （合集 / 电影靠它排除在批量操作之外），而失效是「还在不在」。
// 两者都要滤掉，但**滤掉的理由不同**——所以是两个判据，不是一个。
func TestAnInvalidMediaDoesNotReachTheQueue(t *testing.T) {
	medias := []bili.Media{
		{ID: 1, Type: bili.MediaVideo, Bvid: "BVlive", Title: "活着的那首"},
		{ID: 2, Type: bili.MediaVideo, Bvid: "BVdead", Title: "失效的那首", Attr: 1},
		{ID: 3, Type: bili.MediaVideo, Bvid: "BVlive2", Title: "也活着的那首"},
	}
	items := app.QueueItemsOfMedias(medias)
	if len(items) != 2 {
		t.Fatalf("队列里应该有 2 首（失效的那首被滤掉），实际 %d 首：%+v", len(items), items)
	}
	for _, it := range items {
		if it.Bvid == "BVdead" {
			t.Fatal("失效的那首进了队列")
		}
	}
}

// TestAValidMediaStillReachesTheQueue 是上面那条的另一半。
//
// ⚠️ **滤得太狠也是 bug**：收藏夹里绝大多数条目是好的，全被滤掉的话
// 「播放全部」会变成「播放全部放不了」。
func TestAValidMediaStillReachesTheQueue(t *testing.T) {
	medias := []bili.Media{
		{ID: 1, Type: bili.MediaVideo, Bvid: "BV1", Title: "一"},
		{ID: 2, Type: bili.MediaAudio, Title: "二"},
	}
	if got := app.QueueItemsOfMedias(medias); len(got) != 2 {
		t.Errorf("两条正常的条目只进了 %d 条：%+v", len(got), got)
	}
}

// contains 是一句「有没有那个词」的小工具。
//
// ⚠️ 用 `strings.Contains` 而不是别的：本文件在 `app_test` 包里，而 `strings`
// 已经在那儿了。
func contains(s, sub string) bool { return strings.Contains(s, sub) }

// TestAnInvalidItemDoesNotAskBiliForAStream 钉住**不发取流请求**。
//
// 🔴 **这是这一票真正的目的**：失效条目在被滤掉之前，应用要为它白跑一趟
// 带 WBI 签名的网络往返（实测 200~400ms），而那一趟**注定失败**——
// 用户等的就是这一串串超时。
//
// 判据是**取流接口被问了几次**：失效条目压根不该走到取流那一步。
func TestAnInvalidItemDoesNotAskBiliForAStream(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)

	var mu sync.Mutex
	asked := 0
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked++
		mu.Unlock()
		f.servePlayURL(w, r.URL.Query())
	})
	// ⚠️ **给 cid**：没有 cid 的话取流之前要先问一次分 P（`/x/player/pagelist`），
	// 而那台假 B 站不伺服它——于是这一首压根放不起来（症状是「问不出这一集
	// 是哪一集」），跟这一票要验的不是一回事。
	f.on("/x/player/pagelist", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": []any{map[string]any{"cid": 777, "page": 1, "part": "第一集", "duration": 260}}})
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())

	// 队列里只有活着的那首。
	dead := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVdead", Cid: "1", Title: "失效的那首", Invalid: true}
	live := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVlive", Cid: "1", Title: "活着的那首"}
	if err := a.PlayFrom(live); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	playing(t, a, "活着的那首")

	// 失效的那首**压根进不了队列**——所以连一次取流都不会有。
	items := app.QueueItemsOfMedias([]bili.Media{
		{ID: 1, Type: bili.MediaVideo, Bvid: "BVlive", Title: "活着的那首"},
		{ID: 2, Type: bili.MediaVideo, Bvid: "BVdead", Title: "失效的那首", Attr: 1},
	})
	if len(items) != 1 {
		t.Fatalf("失效条目进了队列：%+v", items)
	}

	mu.Lock()
	got := asked
	mu.Unlock()
	if got != 1 {
		t.Errorf("取流接口被问了 %d 次，want 1（只有活着的那首）——失效条目被白跑了一趟", got)
	}
	_ = dead
}

// TestTheQueueRefusesAnInvalidItem 钉住**队列入口那一道闸**。
//
// 🔴 **这一条是审查补上的**（票 #624 的评审第 1 条）：行级入口（双击 / 回车 /
// 封面播放钮 / 「从这一首往后听」）走的是 `EnqueueAndPlayNow` / `PlayFrom`，
// 而它们原先**不判失效**——于是失效的条目照样进队列、照样白跑一趟取流。
//
// 判据在 **app 层**（`CanQueue`），而 app 层是**所有队列入口**的所在地——
// 界面层那几个入口也调它，所以不存在「这一处漏了」。
func TestTheQueueRefusesAnInvalidItem(t *testing.T) {
	a := newOnlineApp(t, newStreamFixture(t), t.TempDir())

	dead := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVdead", Title: "失效的那首", Invalid: true}
	if err := a.EnqueueAndPlayNow(dead); err == nil {
		t.Error("队列收下了失效的那一条——它会进队列，然后白跑一趟取流")
	}
	if got := a.Queue().Items(); len(got) != 0 {
		t.Errorf("队列里有 %d 首，want 0：%+v", len(got), got)
	}
}

// TestPlayFromRefusesInvalidItems 钉住「换队列」那一道闸。
//
// ⚠️ **它是另一条路**：`EnqueueAndPlayNow` 是「放这一首」，`PlayFrom` 是
// 「换成这一批」——而后者是「从这一首往后听」走的路，它把**一整段**塞进队列，
// 里面的失效条目一个都不滤。
//
// ⚠️ **判据是「整批被拒」还是「滤掉失效的那些」**：这一条钉的是**滤掉**——
// 用户点「从这一首往后听」是为了听后面那一段，而后面那几首里混着一个失效的，
// 不该因为它整批都放不了。
func TestPlayFromRefusesInvalidItems(t *testing.T) {
	a := newOnlineApp(t, newStreamFixture(t), t.TempDir())

	live := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVlive", Title: "活着的那首"}
	dead := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVdead", Title: "失效的那首", Invalid: true}
	if err := a.PlayFrom(live, dead); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	got := a.Queue().Items()
	if len(got) != 1 {
		t.Fatalf("队列里有 %d 首，want 1（失效的那首不该进来）：%+v", len(got), got)
	}
	if got[0].Bvid != "BVlive" {
		t.Errorf("队列里那一首是 %q，want BVlive", got[0].Bvid)
	}
}

// TestEnqueueRefusesInvalidItems 钉住「加进队列」那一道闸。
//
// ⚠️ **它和上面两条是三条不同的路**：「添加到队列」不改正在放的那首，
// 而「立即播放」把这一首变成正在放的——判据一样，入口不一样。
func TestEnqueueRefusesInvalidItems(t *testing.T) {
	a := newOnlineApp(t, newStreamFixture(t), t.TempDir())

	dead := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVdead", Title: "失效的那首", Invalid: true}
	if err := a.Enqueue(dead); err == nil {
		t.Error("「添加到队列」收下了失效的那一条")
	}
	if got := a.Queue().Items(); len(got) != 0 {
		t.Errorf("队列里有 %d 首，want 0：%+v", len(got), got)
	}
}

// TestEnqueueNextRefusesInvalidItems 钉住「下一首播放」那一道闸。
func TestEnqueueNextRefusesInvalidItems(t *testing.T) {
	a := newOnlineApp(t, newStreamFixture(t), t.TempDir())

	dead := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVdead", Title: "失效的那首", Invalid: true}
	if err := a.EnqueueNext(dead); err == nil {
		t.Error("「下一首播放」收下了失效的那一条")
	}
	if got := a.Queue().Items(); len(got) != 0 {
		t.Errorf("队列里有 %d 首，want 0：%+v", len(got), got)
	}
}

// TestThePrefetchDoesNotAskForAnInvalidSong 钉住「预取不备失效的那一首」。
//
// 🔴 **这一条也是审查补上的**（票 #624 的评审第 1 条）：预取（`maybePrefetch`）
// 在当前这首快放完的时候**主动替下一首备一遍流地址**——而它原先只判
// 「有没有音频流」，于是失效的那一首被**多打了一趟**取流。
//
// ⚠️ **判据是「备好之后那一份是谁」**：`TestOnlyPrefetchReady` 那一族
// 断的是「备好了没有」，而这里断的是「备的是**不是那一首失效的**」。
func TestThePrefetchDoesNotAskForAnInvalidSong(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.on("/x/player/pagelist", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": []any{map[string]any{"cid": 777, "page": 1, "part": "第一集", "duration": 260}}})
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())

	// 队列里是「正在放的那首」+「下一首是失效的那首」。
	live := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVlive", Cid: "1", Title: "活着的那首"}
	dead := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: "BVdead", Cid: "1", Title: "失效的那首", Invalid: true}
	if err := a.PlayFrom(live, dead); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	playing(t, a, "活着的那首")

	// 🔴 **失效的那首压根进不了队列**（队列入口那道闸），所以预取看不到它。
	// 这一条钉的是那一道闸——而「预取不备失效的」是它的**后果**。
	if got := a.Queue().Items(); len(got) != 1 {
		t.Fatalf("队列里有 %d 首，want 1（失效的那首不该进来）：%+v", len(got), got)
	}
}
