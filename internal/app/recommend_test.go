package app

// 首页（推荐页）这一摊的测试。
//
// 这一层做的是**「界面不该知道、但两边必须用同一个答案」**那三件事，所以这一
// 组盯的就是它们：
//
//   - **行身份**（🔴 铁律四）：挑哪个字段、挑不到怎么办、重了怎么办；
//   - **三个 Tab 各走哪条路**（音乐/鬼畜是分区推荐，「流行」是音乐榜）；
//   - **新歌速递怎么合成**（头图位在前、半边失败不算全败）。
//
// **画出来是什么样**由界面那一组钉（`internal/ui/homepage_feed_test.go`），
// 这里不重复。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// homeBili 是一台照着给定答案回答的假 B 站，把收到的路径和查询串都录下来。
//
// ⚠️ 和 `newFakeLibraryBili` 分开是因为这里要**按查询串回答**：三个 Tab 里
// 有两个打的是同一条路径（分区推荐），区别只在 `from_region` 上——按路径
// 回答的话「鬼畜那一档发的是音乐分区」这件事压根测不到。
type homeBili struct {
	*httptest.Server
	mu      sync.Mutex
	handler func(path string, q url.Values) (int, string)
	seen    []homeSeen
}

// homeSeen 是录下来的一次请求。
type homeSeen struct {
	Path  string
	Query url.Values
}

func newHomeBili(t *testing.T) *homeBili {
	t.Helper()
	b := &homeBili{}
	b.handler = func(string, url.Values) (int, string) {
		return -404, "没有这个接口"
	}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WBI 密钥：签名接口先打 nav 拿 key，打不通的话签名那一步就直接报错，
		// 连真正的请求都发不出去（`signWBI` 的注释）。
		if r.URL.Path == "/x/web-interface/nav" {
			writeHomeJSON(w, 0, map[string]any{"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleHomeWBIKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleHomeWBISub + ".png",
			}})
			return
		}
		b.mu.Lock()
		b.seen = append(b.seen, homeSeen{Path: r.URL.Path, Query: r.URL.Query()})
		h := b.handler
		b.mu.Unlock()
		code, body := h(r.URL.Path, r.URL.Query())
		writeHomeJSON(w, code, json.RawMessage(body))
	}))
	t.Cleanup(b.Close)
	return b
}

// writeHomeJSON 回一份 B 站外壳。
func writeHomeJSON(w http.ResponseWriter, code int, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		raw = []byte("null")
	}
	body, _ := json.Marshal(map[string]any{"code": code, "message": "0", "ttl": 1, "data": json.RawMessage(raw)})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// sampleHomeWBIKey / sampleHomeWBISub 是签名的两把密钥。它们只在这一组里
// 出现：这一组验的是「签名发不发得出去」，不是「签名算得对不对」
// （那一条在 wbi_test.go，对着公布的那组向量）。
const (
	sampleHomeWBIKey = "7cd084941338484aae1ad9425b84077c"
	sampleHomeWBISub = "4932caff0ff746eab6f01bf08b70ac45"
)

// on 换掉整台假 B 站的回答规则。
func (b *homeBili) on(h func(path string, q url.Values) (int, string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handler = h
}

// calls 是打给某条路径的请求数。
func (b *homeBili) calls(path string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, s := range b.seen {
		if s.Path == path {
			n++
		}
	}
	return n
}

// lastQuery 是最后一次打给某条路径时的查询串。
func (b *homeBili) lastQuery(path string) url.Values {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out url.Values
	for _, s := range b.seen {
		if s.Path == path {
			out = s.Query
		}
	}
	return out
}

// homeClient 建一个指着假 B 站的客户端。
func homeClient(t *testing.T, b *homeBili) *bili.Client {
	t.Helper()
	cli := bili.NewClient(bili.Options{
		Transport: b.Server.Client().Transport,
		CSRF:      func() string { return "the-jct" },
	})
	// ⚠️ 不指到 fixture 上的话这些用例会打到**真的** B 站：不报错、结果还挺
	// 合理，于是测试变成「取决于能不能连上 B 站」。
	cli.TestOnlyPointHostsAtFixture(b.URL)
	return cli
}

// recommendOn 建一个挂着假客户端的首页那一摊。
func recommendOn(t *testing.T, b *homeBili) *Recommend {
	t.Helper()
	return &Recommend{Client: homeClient(t, b)}
}

const (
	homeRegionFeedPath = "/x/web-interface/region/feed/rcmd"
	homeMusicRankPath  = "/x/centralization/interface/music/comprehensive/web/rank"
	homeNewMusicPath   = "/x/centralization/interface/new/music"
	homeNewBannerPath  = "/x/centralization/interface/new/music/banner"
	// ⚠️ aid / cid **写成字符串**：这一族的接口真会这么发（Biu 上游
	// `src/service/service.md` 里音乐榜那份真实返回，详见 `bili.flexInt64`）。
	// 这一组原来全写数字，于是真网上「流行」整档解不出来时，这一组照样全绿。
	homeArchivedOneJSON = `{"aid":"115182114246511","bvid":"BV1a","cid":"32447137298","title":"一","cover":"https://c/1.jpg","author":{"mid":1,"name":"UP 一"},"stat":{"view":10}}`
	homeArchivedTwoJSON = `{"aid":"115182114246512","bvid":"BV2b","cid":"32447137299","title":"二","cover":"https://c/2.jpg","author":{"mid":2,"name":"UP 二"},"stat":{"view":20}}`
)

// archivesBody 造一份分区推荐的 data 段。
func archivesBody(entries ...string) string {
	return `{"archives":[` + strings.Join(entries, ",") + `]}`
}

// TestEachTabAsksForItsOwnThing 盯三个 Tab 各走哪条路。
//
// 这一条钉的是**「流行」不是第三个分区**：它打的是音乐榜，参数也是那一套。
// 写成三分区推荐的话这一条会红，而红得正是时候——界面上看不出来。
func TestEachTabAsksForItsOwnThing(t *testing.T) {
	f := newHomeBili(t)
	f.on(func(path string, _ url.Values) (int, string) {
		switch path {
		case homeRegionFeedPath:
			return 0, archivesBody(homeArchivedOneJSON)
		case homeMusicRankPath:
			return 0, `{"list":[{"id":7,"music_title":"榜上那首","bvid":"BV1r","aid":"115182114246511","cid":"32447137298",` +
				`"related_archive":{"bvid":"BV1ra","cid":"115219862986158","title":"榜上那首的稿件",` +
				`"username":"榜作者","uid":3,"vv_count":5,"duration":100}}]}`
		default:
			return -404, "没有这个接口"
		}
	})
	r := recommendOn(t, f)

	for _, tc := range []struct {
		tab      int
		wantPath string
		wantFrom string // 只有分区推荐才有
	}{
		{HomeTabMusic, homeRegionFeedPath, "1003"},
		{HomeTabGuichu, homeRegionFeedPath, "1007"},
		{HomeTabPop, homeMusicRankPath, ""},
	} {
		items, _, err := r.Feed(context.Background(), tc.tab, 1)
		if err != nil {
			t.Fatalf("第 %d 档取数失败：%v", tc.tab, err)
		}
		if len(items) != 1 {
			t.Errorf("第 %d 档拿到 %d 条，want 1", tc.tab, len(items))
			continue
		}
		q := f.lastQuery(tc.wantPath)
		if tc.wantFrom == "" {
			if q.Get("pn") != "1" {
				t.Errorf("「流行」那一档没发音乐榜的 pn：%v", q)
			}
			continue
		}
		if q.Get("from_region") != tc.wantFrom {
			t.Errorf("第 %d 档的 from_region = %q，want %q（查串 %v）",
				tc.tab, q.Get("from_region"), tc.wantFrom, q)
		}
		// 音乐榜那一档**不该**碰过分区推荐，反过来也一样。
		if tc.tab != HomeTabPop && f.calls(homeMusicRankPath) != 0 {
			t.Errorf("第 %d 档打了音乐榜那个接口", tc.tab)
		}
	}
}

// TestTheRowIdentityIsBvidThenAidThenTheFallback 钉行身份那一条链。
//
// 三个字段 B 站都给得出，但**挑的顺序只有一处**（`recommendID`）。挑错了的
// 症状是「同一首歌在推荐页和稍后再看是两个身份」，而界面上一个错都看不出来。
func TestTheRowIdentityIsBvidThenAidThenTheFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   bili.RecommendItem
		want string
	}{
		{"bvid 优先", bili.RecommendItem{Bvid: "BV1", Aid: 7, TrackID: "t1"}, "BV1"},
		{"没有 bvid 认 aid", bili.RecommendItem{Aid: 7, TrackID: "t1"}, "aid:7"},
		{"只剩兜底 id", bili.RecommendItem{TrackID: "t1"}, "id:t1"},
		{"三个都没有就是没有", bili.RecommendItem{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := recommendID(tc.in); got != tc.want {
				t.Errorf("身份 = %q，want %q", got, tc.want)
			}
		})
	}
}

// TestWhatCannotBeIdentifiedIsDropped 盯「画不出来的那几条」被丢掉，且能数出来。
//
// 三个都没有身份的那一条**不能拿序号顶上**（铁律四）：序号是「父 ID + 兄弟
// 序号」那一套，头上多一首歌会让后面所有行的状态整体错配。而丢掉的那几条
// 要能数出来——否则「这一档一个格子都没有」分不清是「B 站没给」还是
// 「给的全都画不出来」。
func TestWhatCannotBeIdentifiedIsDropped(t *testing.T) {
	items, dropped := recommendItems([]bili.RecommendItem{
		{Bvid: "BV1", Title: "一"},
		{Title: "没有身份"},            // 丢掉
		{Bvid: "BV1", Title: "重了"}, // 丢掉（同一个身份会让两行共用状态）
		{Aid: 9, Title: "有 aid"},
	})
	if len(items) != 2 {
		t.Fatalf("收了 %d 条，want 2", len(items))
	}
	if dropped != 2 {
		t.Errorf("丢掉 %d 条，want 2", dropped)
	}
	if items[0].ID != "BV1" || items[1].ID != "aid:9" {
		t.Errorf("身份 = %q / %q", items[0].ID, items[1].ID)
	}
	// ⚠️ 拿不出 bvid 的那一行**画得出来、放不了**（`RecommendItem.Item` 上写了
	// 为什么界面上没有把它画灰）。这里钉住的是「它确实没有 bvid」，不是「它被判成
	// 能放」——那个判据在共用控件里，首页给不了。
	if items[1].Item.Bvid != "" {
		t.Errorf("没有身份的那一条却带着 bvid：%+v", items[1].Item)
	}
	if items[0].Item.Bvid != "BV1" {
		t.Errorf("播放项没有带上 bvid：%+v", items[0].Item)
	}
	if items[0].Item.Source != "online" {
		t.Errorf("播放项的来源 = %q，want online", items[0].Item.Source)
	}
}

// TestNewMusicPutsTheBannerFirstAndDropsWhatRepeats 盯新歌速递的合成。
//
// 头图位在前、列表在后（上游 `new-music-top.tsx:96-104` 的合并顺序），重复的
// 按身份去掉——那一档是横向网格，两张卡片用同一个 key 会共用一份焦点状态。
func TestNewMusicPutsTheBannerFirstAndDropsWhatRepeats(t *testing.T) {
	f := newHomeBili(t)
	f.on(func(path string, _ url.Values) (int, string) {
		switch path {
		case homeNewBannerPath:
			return 0, `{"list":[{"music_id":"m1","archive_title":"头图那首","bvid":"BV1ban","aid":"115219862986158","cid":"115219862986158","author":"头图作者"}]}`
		case homeNewMusicPath:
			// 第一条和头图位是同一首（B 站两个接口会给出重叠的那一批）。
			return 0, `{"list":[{"id":5,"music_title":"列表那首","bvid":"BV1ban","aid":"115219862986158","cid":"115219862986158"},` +
				`{"id":6,"music_title":"第二首","bvid":"BV1new","aid":"115182114246511","cid":"32447137298"}]}`

		default:
			return -404, "没有这个接口"
		}
	})

	items, dropped, err := recommendOn(t, f).NewMusic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("合成出来 %d 条，want 2（重复的那一条要去掉）", len(items))
	}
	if dropped != 1 {
		t.Errorf("丢掉 %d 条，want 1", dropped)
	}
	if items[0].Title != "头图那首" {
		t.Errorf("第一条 = %q，want 头图那首（头图位排在前面）", items[0].Title)
	}
	if items[1].Title != "第二首" {
		t.Errorf("第二条 = %q，want 第二首", items[1].Title)
	}
}

// TestOneHalfOfNewMusicFailingStillShowsTheOther 盯半边失败。
//
// 上游两个请求各自 catch（`new-music-top.tsx:59`），一半拿到了就画一半。这一
// 层照它——把两半合成「全有或全无」的话，头图位那个接口一挂，整块新歌速递
// 就没了，而列表那一半明明是好的。
func TestOneHalfOfNewMusicFailingStillShowsTheOther(t *testing.T) {
	f := newHomeBili(t)
	f.on(func(path string, _ url.Values) (int, string) {
		switch path {
		case homeNewBannerPath:
			return -500, "头图位挂了"
		case homeNewMusicPath:
			return 0, `{"list":[{"id":6,"music_title":"还能看的那首","bvid":"BV1new","aid":7,"cid":7}]}`
		default:
			return -404, "没有这个接口"
		}
	})

	items, _, err := recommendOn(t, f).NewMusic(context.Background())
	if err != nil {
		t.Fatalf("一半失败不该整块失败：%v", err)
	}
	if len(items) != 1 || items[0].Title != "还能看的那首" {
		t.Errorf("拿到 %+v，want 还能看的那首", items)
	}
}

// TestBothHalvesFailingIsAFailure 盯两半都挂时它确实是个失败。
//
// 反过来那一支也要钉：半边失败吞掉的话，两边都挂时界面上是「暂无数据」，
// 用户看不出是网络的问题。
func TestBothHalvesFailingIsAFailure(t *testing.T) {
	f := newHomeBili(t)
	f.on(func(string, url.Values) (int, string) { return -500, "挂了" })

	_, _, err := recommendOn(t, f).NewMusic(context.Background())
	if err == nil {
		t.Fatal("两半都挂了却当成成功")
	}
}

// TestRecommendWithoutAClientSaysSo 盯没挂上客户端。
//
// 这一支的**表现必须是能说的**，不是发一个注定 -404 的请求：界面据此画的是
// 「还没接上」，而那一句和「B 站没有推荐」在用户那边是两件事。
func TestRecommendWithoutAClientSaysSo(t *testing.T) {
	var r *Recommend
	if r.Mounted() {
		t.Error("没挂客户端却报挂上了")
	}
	if _, _, err := r.Feed(context.Background(), HomeTabMusic, 1); !errors.Is(err, errRecommendMissing) {
		t.Errorf("取数的错 = %v，want errRecommendMissing", err)
	}
	if _, _, err := r.NewMusic(context.Background()); !errors.Is(err, errRecommendMissing) {
		t.Errorf("新歌的错 = %v，want errRecommendMissing", err)
	}
}

// TestRecommendMountsPerApp 盯挂载是按 App 分的。
//
// 两个窗口 / 两次测试之间不串——串了的症状是「测试 A 的假 B 站答了测试 B 的
// 首页」。
func TestRecommendMountsPerApp(t *testing.T) {
	a1, a2 := New(), New()
	t.Cleanup(a1.Close)
	t.Cleanup(a2.Close)

	if a1.RecommendOf().Mounted() {
		t.Error("没挂过的 App 报挂上了")
	}
	f := newHomeBili(t)
	f.on(func(string, url.Values) (int, string) { return 0, archivesBody(homeArchivedOneJSON) })
	a1.MountRecommend(homeClient(t, f))

	if !a1.RecommendOf().Mounted() {
		t.Error("挂上了却报没挂")
	}
	if a2.RecommendOf().Mounted() {
		t.Error("另一个 App 也报挂上了——挂载是按 App 分的")
	}
	// 没挂的那个发请求的话是 errRecommendMissing，不是打到真 B 站上去。
	if _, _, err := a2.RecommendOf().Feed(context.Background(), HomeTabMusic, 1); err == nil {
		t.Error("没挂客户端的 App 竟然取到了数")
	}
}
