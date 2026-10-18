package lyrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// neteaseFixture 是网易云那个搜索接口的一份应答。形状逐条照
// `_research/biu/shared/types/lyrics.d.ts` 的 `SearchSongByNeteaseResponse`。
const neteaseFixture = `{
  "result": {
    "songs": [
      {"id": 11, "name": "晴天", "duration": 269000,
       "artists": [{"name": "周杰伦"}, {"name": "费玉清"}],
       "album": {"name": "叶惠美"}},
      {"id": 22, "name": "七里香", "duration": 299000,
       "artists": [], "album": {"name": ""}}
    ],
    "hasMore": false
  },
  "code": 200
}`

// fixtureServer 起一台只回 body 的服务器，并把**收到的每一个查询参数**记下来。
//
// 记参数而不是记地址：这两个源的全部分别就在那几个参数上（网易云那几个
// `-1`、`_nmclfl`，LRCLib 的 `q`），而少一个的症状是「译文是空的」或者
// 「搜出来的是别的分区」——那种错在界面上看不出任何异样。
func fixtureServer(t *testing.T, body string, hits *int32, got *url.Values) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		if got != nil {
			*got = r.URL.Query()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBothSourcesCanSearch 钉住「两个源都能搜」：各自发得出请求、各自解得出候选。
//
// 顺带钉住**单位**：网易云的 duration 本来就是毫秒，LRCLib 的是秒，而界面上
// 只画一个「时:分」。少乘那 1000 的症状是「每一条候选的时长都是 0:00」，
// 而那看起来像「这个源没给时长」。
func TestBothSourcesCanSearch(t *testing.T) {
	var nq, lq url.Values
	var nhits, lhits int32
	nsrv := fixtureServer(t, neteaseFixture, &nhits, &nq)
	lsrv := fixtureServer(t, `[{"id":7,"trackName":"T","artistName":"A","albumName":"B",
	  "duration":215,"syncedLyrics":"[00:01.00]词"}]`, &lhits, &lq)

	n := NewNetease(nil)
	n.TestOnlyPointHostsAtFixture(nsrv.URL)
	l := NewLRCLib(nil)
	l.TestOnlyPointHostsAtFixture(lsrv.URL)

	got, err := n.Search(context.Background(), "晴天")
	if err != nil {
		t.Fatalf("网易云搜索失败：%v", err)
	}
	if len(got) != 2 {
		t.Fatalf("网易云出了 %d 条，want 2", len(got))
	}
	if got[0].ID != "11" || got[0].Title != "晴天" || got[0].Album != "叶惠美" {
		t.Errorf("第一条是 %+v", got[0])
	}
	if want := "周杰伦 / 费玉清"; got[0].Artist != want {
		t.Errorf("艺人是 %q，want %q", got[0].Artist, want)
	}
	if got[0].DurationMS != 269000 {
		t.Errorf("时长是 %dms，want 269000（网易云本来给的就是毫秒）", got[0].DurationMS)
	}
	// 源没给的那几样写「--」：界面上「不知道」要**写出来**，空着看着像渲染坏了。
	if got[1].Artist != "--" || got[1].Album != "--" {
		t.Errorf("空的那几样没有写成「--」：%+v", got[1])
	}

	lcands, err := l.Search(context.Background(), "晴天")
	if err != nil {
		t.Fatalf("LRCLib 搜索失败：%v", err)
	}
	if len(lcands) != 1 || lcands[0].ID != "7" {
		t.Fatalf("LRCLib 出了 %+v", lcands)
	}
	if lcands[0].DurationMS != 215000 {
		t.Errorf("时长是 %dms，want 215000（LRCLib 给的是秒）", lcands[0].DurationMS)
	}
	if !lcands[0].HasLyrics() {
		t.Error("LRCLib 的候选没有带上它的 syncedLyrics——那一份是白搜的")
	}

	// 网易云那几个参数**逐个**对一遍上游 `netease-lyric.ts`。
	for k, want := range map[string]string{
		"s": "晴天", "type": "1", "limit": "20", "offset": "0",
	} {
		if got := nq.Get(k); got != want {
			t.Errorf("网易云搜索的 %s 是 %q，want %q", k, got, want)
		}
	}
	if got := lq.Get("q"); got != "晴天" {
		t.Errorf("LRCLib 的 q 是 %q，want 晴天", got)
	}
}

// TestNeteasePreviewSendsThoseFiveParams 钉住预览那一次请求的参数。
//
// 那五个 `-1` 和 `_nmclfl` 是逐字抄上游的：少 `_nmclfl` 的话译文那一栏是空的，
// 而空译文在界面上长得和「这首歌没有翻译」一模一样。
func TestNeteasePreviewSendsThoseFiveParams(t *testing.T) {
	var q url.Values
	srv := fixtureServer(t, `{"code":200,"lrc":{"lyric":"[00:01.00]词"},
	  "tlyric":{"lyric":"[00:01.00]word"}}`, nil, &q)
	n := NewNetease(nil)
	n.TestOnlyPointHostsAtFixture(srv.URL)

	doc, err := n.Preview(context.Background(), Candidate{ID: "11"})
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"id": "11", "tv": "-1", "lv": "-1", "rv": "-1", "kv": "-1", "_nmclfl": "1",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("预览请求的 %s 是 %q，want %q", k, got, want)
		}
	}
	if doc.LRC != "[00:01.00]词" || doc.Translated != "[00:01.00]word" {
		t.Errorf("拿到的是 %+v", doc)
	}
}

// TestNeteaseFallsBackToTheKaraokeLyric：有些歌只有 klyric（逐字那份）。
// 抄上游 `netease-tab.tsx:53` 那个 `||`，少了它那部分歌显示成「未找到歌词内容」。
func TestNeteaseFallsBackToTheKaraokeLyric(t *testing.T) {
	srv := fixtureServer(t, `{"code":200,"lrc":{"lyric":"   "},
	  "klyric":{"lyric":"[00:02.00]逐字"}}`, nil, nil)
	n := NewNetease(nil)
	n.TestOnlyPointHostsAtFixture(srv.URL)

	doc, err := n.Preview(context.Background(), Candidate{ID: "11"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.LRC != "[00:02.00]逐字" {
		t.Errorf("拿到的是 %q，want 逐字那份", doc.LRC)
	}
}

// TestLRCLibPreviewIssuesNoRequest 钉住「预览不发请求」。
//
// 上游 `lrclib-tab.tsx:27-37` 直接读搜索结果里的 `syncedLyrics`：它的搜索响应
// 本来就是**带歌词的完整记录**。多发一次 `/api/get/:id` 拿的是同一份东西，
// 代价是每点一条候选就多一个网络往返——而界面上那一帧是「点了一下什么都没发生」。
func TestLRCLibPreviewIssuesNoRequest(t *testing.T) {
	var hits int32
	srv := fixtureServer(t, `[{"id":7,"trackName":"T","syncedLyrics":"[00:01.00]词"}]`, &hits, nil)
	l := NewLRCLib(nil)
	l.TestOnlyPointHostsAtFixture(srv.URL)

	doc, err := l.Preview(context.Background(),
		Candidate{ID: "7", LRC: "[00:01.00]词"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.LRC != "[00:01.00]词" {
		t.Errorf("拿到的是 %q", doc.LRC)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Errorf("预览发了 %d 个请求，want 0（歌词本来就跟着候选一起回来了）", n)
	}
}

// TestAPreviewThatHasNoLyricsSaysSo 断的是**那一句说的是「这一条没有歌词」**，
// 而不是一句笼统的失败。
//
// 🔴 判据是 `errors.Is(err, ErrNoLyrics)`，不是「err != nil」：只断非空的话，
// 界面上那句「网络不通或者对方没理」照样能过——而它骗人的地方正在这里（用户会
// 去反复重试，而这一条重试一万次也不会有歌词）。`lyricsErrText` 按这个哨兵
// 分流，`TestThePreviewSaysItIsNotAFetchFailure` 在界面上钉那一句。
func TestAPreviewThatHasNoLyricsSaysSo(t *testing.T) {
	l := NewLRCLib(nil)
	l.TestOnlyPointHostsAtFixture("http://127.0.0.1:1")

	_, err := l.Preview(context.Background(), Candidate{ID: "7"})
	if !errors.Is(err, ErrNoLyrics) {
		t.Fatalf("一条没有歌词的候选，报的是 %v，want ErrNoLyrics", err)
	}
}

// TestAnEmptyWordSendsNothing 抄上游 `if (query === "") return`。
//
// 界面上把框清空就会走到这里，而那是最容易变成「每敲一个字发一次」的那个入口。
func TestAnEmptyWordSendsNothing(t *testing.T) {
	var hits int32
	srv := fixtureServer(t, neteaseFixture, &hits, nil)
	n := NewNetease(nil)
	n.TestOnlyPointHostsAtFixture(srv.URL)

	for _, q := range []string{"", "   "} {
		got, err := n.Search(context.Background(), q)
		if err != ErrNoQuery {
			t.Errorf("搜 %q 的结果是 %v，want ErrNoQuery", q, err)
		}
		if len(got) != 0 {
			t.Errorf("搜 %q 搜出了 %d 条", q, len(got))
		}
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Errorf("空词也发了 %d 个请求，want 0", n)
	}
}

// TestAFailedSearchIsAnErrorAndNotAnEmptyList 是**负路径**那一条。
//
// 「搜不到」和「搜到了但是空的」在界面上必须是两句话：合成一句的话，用户读到
// 「没搜到」会以为那个源上没有这首歌，于是永远不会去按那个本该在那儿的重试。
// 网易云用体里的 code 报失败、不用状态码，所以两处都得认。
func TestAFailedSearchIsAnErrorAndNotAnEmptyList(t *testing.T) {
	for name, srv := range map[string]*httptest.Server{
		"体里的 code 不是 200": fixtureServer(t, `{"code":-460,"result":{}}`, nil, nil),
		"状态码不是 200": func() *httptest.Server {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			}))
			t.Cleanup(s.Close)
			return s
		}(),
	} {
		n := NewNetease(nil)
		n.TestOnlyPointHostsAtFixture(srv.URL)
		got, err := n.Search(context.Background(), "晴天")
		if err == nil {
			t.Errorf("%s：当成搜到了 %d 条", name, len(got))
			continue
		}
		if len(got) != 0 {
			t.Errorf("%s：失败的时候还带回了 %d 条", name, len(got))
		}
		if !strings.Contains(err.Error(), "网易云") && !strings.Contains(err.Error(), "lyrics") {
			t.Errorf("%s：%v 里没有点名是哪个源", name, err)
		}
	}
}
