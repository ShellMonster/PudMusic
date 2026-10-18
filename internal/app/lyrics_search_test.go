package app

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
)

// fakeLyricSource 是一只可编排的歌词源：它数自己被调了几次、能按需要卡住、
// 能按需要失败。
//
// **它必须能数调用次数**：这一票最要紧的一条是「同一个词不搜第二遍」，而那条
// 判据就是「第二次搜的时候那个源一次都没被叫到」。只判「界面上出现了一样的候选」
// 抓不住它——缓存和源返回同一样东西时两者长得完全一样。
type fakeLyricSource struct {
	id, name string

	mu sync.Mutex
	// calls 是 Search 被调了几次。
	calls int
	// previews 是 Preview 被调了几次。
	previews int
	// cands 是 Search 每次都返回的这份。
	cands []lyrics.Candidate
	// doc / err 是 Preview 的返回值。
	doc lyrics.Doc
	err error
	// searchErr 是 Search 的返回值。nil 时正常。
	searchErr error
	// hold 是 Search 卡住多久。0 = 不卡。
	hold time.Duration
}

func (f *fakeLyricSource) ID() string   { return f.id }
func (f *fakeLyricSource) Name() string { return f.name }

func (f *fakeLyricSource) Search(_ context.Context, _ string) ([]lyrics.Candidate, error) {
	f.mu.Lock()
	f.calls++
	hold, err, cands := f.hold, f.searchErr, f.cands
	f.mu.Unlock()

	if hold > 0 {
		time.Sleep(hold)
	}
	if err != nil {
		return nil, err
	}
	return cands, nil
}

func (f *fakeLyricSource) Preview(_ context.Context, _ lyrics.Candidate) (lyrics.Doc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previews++
	return f.doc, f.err
}

func (f *fakeLyricSource) count() (searches, previews int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.previews
}

// waitSearch 轮询到 cond 成立。搜索在后台，只能这样等。
func waitSearch(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等了 %v，%s", d, what)
}

// searchApp 建一个挂好了缓存和假源的应用。
func searchApp(t *testing.T, srcs ...lyrics.Source) (*App, *lyrics.Cache) {
	t.Helper()
	a, c := lyricsApp(t)
	a.MountLyricSources(srcs...)
	return a, c
}

// sourceOf 是两个源之一：网易云那种「候选不带歌词、要另拉」的。
func sourceOf(id string, cands ...lyrics.Candidate) *fakeLyricSource {
	return &fakeLyricSource{id: id, name: id + "源", cands: cands}
}

// TestTheSameWordIsNotSearchedTwice 是这一票点名的那条：搜索结果写进缓存，
// **避免重复请求**。
//
// 上游没有这一层——切走再切回弹窗它就重新搜一遍。少了它用户的观感是「候选一行一行
// 冒出来」，而他明明上一次已经看过同一批了。
func TestTheSameWordIsNotSearchedTwice(t *testing.T) {
	src := sourceOf("netease", lyrics.Candidate{ID: "1", Title: "晴天"})
	a, _ := searchApp(t, src)

	a.SearchLyrics(0, "晴天")
	waitSearch(t, 2*time.Second, "第一批候选", func() bool {
		return len(a.LyricSearch().Candidates) == 1
	})

	// 同一个词再来一次：必须一个请求都不发，候选**当场**就在。
	a.SearchLyrics(0, "晴天")
	if n, _ := src.count(); n != 1 {
		t.Errorf("同一个词搜了 %d 次，want 1", n)
	}
	if got := a.LyricSearch().Candidates; len(got) != 1 {
		t.Errorf("第二次没有从缓存里把候选拿出来：%+v", got)
	}
}

// TestTheCacheIsKeyedBySourceAndByWord 缓存键必须是「源 + 词」。
//
// ⚠️ 用 `\x00` 分隔（见 `searchKey` 的注释）：用空格或 `/` 分隔的话「网易云 / 晴天」
// 和「网易云/晴天」会撞成同一个键，用户搜到的结果是**另一个词**的。
func TestTheCacheIsKeyedBySourceAndByWord(t *testing.T) {
	src := &fakeLyricSource{id: "netease", name: "网易云", cands: []lyrics.Candidate{{ID: "1"}}}
	a, _ := searchApp(t, src)
	a.SearchLyrics(0, "晴天")
	waitSearch(t, 2*time.Second, "晴天", func() bool { return len(a.LyricSearch().Candidates) == 1 })

	a.SearchLyrics(0, "阴天")
	// ⚠️ 等的是**源真的被叫了第二次**，不是 `Query` 变了：`Query` 是 `SearchLyrics`
	// 同步写进去的，比后台那一趟进 `src.Search` 早得多，照着它等的话这一条量到的是
	// 1——而 1 正是「两个词撞成了同一个键」的那个数。
	waitSearch(t, 2*time.Second, "第二个词真的搜了一次", func() bool {
		n, _ := src.count()
		return n == 2
	})
}

// TestEachSourceHasItsOwnCache：两个源搜同一个词是两次请求、两条缓存。
func TestEachSourceHasItsOwnCache(t *testing.T) {
	netease := sourceOf("netease", lyrics.Candidate{ID: "n1"})
	lrclib := sourceOf("lrclib", lyrics.Candidate{ID: "l1"})
	a, _ := searchApp(t, netease, lrclib)

	a.SearchLyrics(0, "晴天")
	a.SearchLyrics(1, "晴天")
	waitSearch(t, 2*time.Second, "两个源都回来", func() bool {
		return a.LyricSearch().Tab == 1 && len(a.LyricSearch().Candidates) == 1
	})
	a.SearchLyrics(0, "晴天")
	a.SearchLyrics(1, "晴天")
	if n, _ := netease.count(); n != 1 {
		t.Errorf("网易云搜了 %d 次，want 1", n)
	}
	if n, _ := lrclib.count(); n != 1 {
		t.Errorf("LRCLib 搜了 %d 次，want 1", n)
	}
}

// TestAFailedSearchIsNotRemembered：缓存里不许存失败。
//
// 存了的话用户按「重试」，界面上弹的还是上一次那句「拉不到」——他永远等不到一个
// 自己能看懂的结果，而按钮看起来是坏的。
func TestAFailedSearchIsNotRemembered(t *testing.T) {
	src := sourceOf("netease")
	src.searchErr = context.DeadlineExceeded
	a, _ := searchApp(t, src)

	a.SearchLyrics(0, "晴天")
	waitSearch(t, 2*time.Second, "失败落地", func() bool { return a.LyricSearch().Err != "" })

	// 用户点了重试：这一次必须真的发出去了。
	src.mu.Lock()
	src.searchErr = nil
	src.cands = []lyrics.Candidate{{ID: "1"}}
	src.mu.Unlock()
	a.SearchLyrics(0, "晴天")
	waitSearch(t, 2*time.Second, "重试成功", func() bool {
		return len(a.LyricSearch().Candidates) == 1
	})
	if n, _ := src.count(); n != 2 {
		t.Errorf("重试一共搜了 %d 次，want 2（第一次失败被缓存住的话就是 1）", n)
	}
}

// TestAStaleSearchNeverLands 是代次守卫的回归测试。
//
// 时序：搜「甲」（慢）→ 用户改成搜「乙」（快）→ 甲回来了。少了守卫的话最后一步
// 会把乙那一批盖成甲的——症状是「我搜的是乙，弹出来的是甲的歌」，而按多少次搜索
// 都一样。
func TestAStaleSearchNeverLands(t *testing.T) {
	slow := sourceOf("netease", lyrics.Candidate{ID: "slow"})
	slow.hold = 300 * time.Millisecond
	a, _ := searchApp(t, slow)

	a.SearchLyrics(0, "甲")
	a.SearchLyrics(0, "乙")

	waitSearch(t, 2*time.Second, "结果落地", func() bool { return len(a.LyricSearch().Candidates) == 1 })
	// 让那一趟慢的再跑一会儿（它还在路上）。
	time.Sleep(400 * time.Millisecond)

	st := a.LyricSearch()
	if st.Query != "乙" {
		t.Errorf("生效的词是 %q，want 乙", st.Query)
	}
	if len(st.Candidates) != 1 || st.Candidates[0].ID != "slow" {
		t.Errorf("迟到的「甲」盖掉了「乙」：%+v", st.Candidates)
	}
}

// TestEmptyingTheBoxClearsTheList 抄上游 `if (query === "") return` 的**界面上那一半**。
//
// 把框清空就是这一支：留着上一批候选的话，用户看着自己删干净的那一框下面还挂着
// 一批和它无关的歌，而他已经不打算搜这个词了。
func TestEmptyingTheBoxClearsTheList(t *testing.T) {
	src := sourceOf("netease", lyrics.Candidate{ID: "1"})
	a, _ := searchApp(t, src)
	a.SearchLyrics(0, "晴天")
	waitSearch(t, 2*time.Second, "第一批", func() bool { return len(a.LyricSearch().Candidates) == 1 })

	a.SearchLyrics(0, "   ")
	st := a.LyricSearch()
	if len(st.Candidates) != 0 || st.Searched {
		t.Errorf("清空之后还挂着 %+v", st)
	}
	if n, _ := src.count(); n != 1 {
		t.Errorf("空词也发了请求，一共 %d 次", n)
	}
}

// TestAdoptingLandsOnTheSongThatIsPlaying 是这一票的终点：找到 → 采用 → 那一首
// 的缓存里真的有它，而且**偏移和字号不动**（用户调过的值不该被换源带走，
// 上游换源只换歌词本身，见 `lyrics/index.tsx:269-278`）。
func TestAdoptingLandsOnTheSongThatIsPlaying(t *testing.T) {
	src := sourceOf("netease")
	src.doc = lyrics.Doc{LRC: "[00:01.00]找到的词", Translated: "[00:01.00]found"}
	a, c := searchApp(t, src)
	it := onlineSong("BV1", "1")
	a.setLyricTrack(it)
	a.SetLyricsOffset(-700)
	a.SetLyricsFontSize(33)

	a.OpenLyricPreview(0, lyrics.Candidate{ID: "11", Title: "晴天", Artist: "周杰伦"})
	waitSearch(t, 2*time.Second, "预览拉回来", func() bool { return a.LyricPreview().LRC != "" })

	if msg := a.AdoptLyricPreview(); msg != "" {
		t.Fatalf("采用失败：%s", msg)
	}
	got, ok := c.Load(lyricKeyOf(it))
	if !ok || got.Lyrics != "[00:01.00]找到的词" || got.TLyrics != "[00:01.00]found" {
		t.Fatalf("缓存里是 %+v（有没有=%v）", got, ok)
	}
	ls := a.Lyrics()
	if !ls.HasLyrics || ls.Rows[0].Text != "找到的词" {
		t.Errorf("界面上没有换上去：%+v", ls.Rows)
	}
	if ls.OffsetMS != -700 || ls.FontSize != 33 {
		t.Errorf("采用歌词把用户调的值带走了：%+v", ls)
	}
}

// TestAdoptingRefusesAfterTheSongChanged 🔴 换歌之后不许采用。
//
// 预览是给「按下预览那一刻在放的那首歌」看的，而这中间用户可能切了歌。照着当前这一首
// 写下去的话，画面上换到的是**歌名 A 配词 B**，而界面上从歌词到播放条每一个字都对得上
// ——那种错用户永远查不出来。
func TestAdoptingRefusesAfterTheSongChanged(t *testing.T) {
	src := sourceOf("netease")
	src.doc = lyrics.Doc{LRC: "[00:01.00]甲的词"}
	a, c := searchApp(t, src)
	first := onlineSong("BV1", "1")
	a.setLyricTrack(first)

	a.OpenLyricPreview(0, lyrics.Candidate{ID: "11"})
	waitSearch(t, 2*time.Second, "预览拉回来", func() bool { return a.LyricPreview().LRC != "" })

	a.setLyricTrack(onlineSong("BV2", "2")) // 预览开着的时候换歌
	if msg := a.AdoptLyricPreview(); msg == "" {
		t.Error("换歌之后居然采用成功了")
	}
	if _, ok := c.Load(lyricKeyOf(first)); ok {
		t.Error("甲的缓存里被写上了乙的歌名配的那一份词")
	}
}

// TestAcandidateThatCarriesItsLyricsIsPreviewedWithoutAnyRequest 是 LRCLib
// 那一路：`Candidate.LRC` 不是空的时**当场**填上，一个请求都不发。
//
// 🔴 判错了的代价是不对称的：反过来在界面线程上同步调一次 `Preview`，那就是**在界面
// 线程上等一个真的网络往返**（网易云那一次），点一条候选之后整块界面卡到它回来，而点
// 第二下的时候它还卡着。所以这一条**同步**断言，不等后台那一趟。
func TestAcandidateThatCarriesItsLyricsIsPreviewedWithoutAnyRequest(t *testing.T) {
	src := sourceOf("lrclib")
	a, _ := searchApp(t, src)
	a.setLyricTrack(onlineSong("BV1", "1"))

	a.OpenLyricPreview(0, lyrics.Candidate{ID: "7", LRC: "[00:01.00]词"})

	if got := a.LyricPreview().LRC; got != "[00:01.00]词" {
		t.Errorf("当场没有摆出来，got %q", got)
	}
	if _, p := src.count(); p != 0 {
		t.Errorf("自带歌词的那一条还去拉了一次（Preview 调了 %d 次）", p)
	}
}

// TestAdoptingIsRefusedWhileThePreviewIsStillLoading：「采用歌词」在拉回来之前是
// 禁着的——一个按下去不会有任何事发生的按钮，在界面上和坏了的按钮长得一模一样。
func TestAdoptingIsRefusedWhileThePreviewIsStillLoading(t *testing.T) {
	src := sourceOf("netease")
	src.doc = lyrics.Doc{LRC: "[00:01.00]词"}
	a, _ := searchApp(t, src)
	a.setLyricTrack(onlineSong("BV1", "1"))

	a.OpenLyricPreview(0, lyrics.Candidate{ID: "11"})
	waitSearch(t, 2*time.Second, "预览拉回来", func() bool { return a.LyricPreview().LRC != "" })

	if p := a.LyricPreview(); !p.Adoptable {
		t.Errorf("拉回来了却没有东西可采用：%+v", p)
	}
	a.CloseLyricPreview()
	if p := a.LyricPreview(); p.Open || p.Adoptable {
		t.Errorf("关掉之后预览还摆着：%+v", p)
	}
	if msg := a.AdoptLyricPreview(); msg != "" {
		t.Errorf("关掉之后采用返回了 %q，want 空串（什么都不该发生）", msg)
	}
}

// TestNoLyricsSourcesMeansNoSearch：没挂源时搜索是**空操作**，而不是 panic。
func TestNoLyricsSourcesMeansNoSearch(t *testing.T) {
	a, _ := searchApp(t)
	a.SearchLyrics(0, "晴天")
	if got := len(a.LyricSources()); got != 0 {
		t.Fatalf("没挂源却读出了 %d 个", got)
	}
	a.OpenLyricPreview(0, lyrics.Candidate{ID: "1"})
	if a.LyricPreview().Open {
		t.Error("没有源却把预览弹窗开出来了")
	}
}

// TestTheLyricSourcesNeverSeeBiliCredentials 是凭据边界的**端到端**那一条。
//
// 白名单本身在 `credentials_test.go` 里逐条验过（`music.163.com` / `lrclib.net`
// 被判成 referer 档）。这一条验的是**本项目新建的这两个客户端真的走了那一层**：
// 忘了把 transport 递进来，白名单就完全不起作用，而症状是「歌词源那边收到了
// 一枚不属于它的 cookie」，没有任何报错。
//
// 做法是让 jar **真的会命中**那个主机（cookiejar 按 URL 的 host 匹配，而 fixture
// 打在 127.0.0.1 上——给 `.bilibili.com` 存一枚的话 jar 压根不会带上，这一层就
// 验了个寂寞）。于是两半都能验出来：
//
//	过了凭据边界   → 那个 Cookie 到不了源那边
//	没过凭据边界   → 同一个 jar 的同一个 Cookie **到得了**（反证这一条不是空跑）
func TestTheLyricSourcesNeverSeeBiliCredentials(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Cookie"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"result":{"songs":[]}}`))
	}))
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	host, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// ⚠️ 值必须是 ASCII：`http.Cookie.String()` 会把非 ASCII 的值**整个丢掉**
	// （Cookie 的值只许可打印 ASCII），于是 jar 送出的是 `SESSDATA=`，这一条反证
	// 会「因为 jar 压根没带东西」而假通过。
	jar.SetCookies(host, []*http.Cookie{{Name: "SESSDATA", Value: "not-yours"}})

	// 反证：没过凭据边界的话，这个 Cookie 确实会被送到源那边。
	raw := &http.Client{Jar: jar}
	if _, err := raw.Get(srv.URL + "/api/search/get"); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 || !strings.Contains(seen[0], "not-yours") {
		t.Fatalf("反证没成立：不过凭据边界时 Cookie 也没送到（%v）——这一条验不到东西", seen)
	}
	seen = nil

	// 正路：和 `ui.MountLyrics` 里建的那两个源**同一个**网络层。
	transport := &CredentialTransport{Base: srv.Client().Transport, Policy: DefaultInjectPolicy()}
	netease := lyrics.NewNetease(transport.Client(jar))
	netease.TestOnlyPointHostsAtFixture(srv.URL)
	if _, err := netease.Search(context.Background(), "晴天"); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 {
		t.Fatal("一个请求都没发出去")
	}
	for i, c := range seen {
		if strings.Contains(c, "not-yours") {
			t.Errorf("第 %d 个请求带着一枚不属于它的 cookie：%q", i, c)
		}
	}
}
