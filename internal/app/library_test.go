package app

// 「稍后再看 / 历史记录」这一层盯的是三件事，全是界面上看得出来的：
//
//  1. **行身份**（🔴 铁律四）。历史记录里同一个 bvid 会出现好几条（同一首歌看过
//     两次），拿 bvid 当身份就是那几行共用一份状态——点一行，旁边几行一起亮。
//  2. **还有没有**。B 站给的是**总条数**，不是「这一页满不满」。
//  3. **没挂上就说没挂上**，而不是发一个注定失败的请求。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// fakeBili 是一台会照抄固定答案的假 B 站，顺便把收到的请求录下来。
type fakeBili struct {
	*httptest.Server
	mu       sync.Mutex
	handlers map[string]http.HandlerFunc
	seen     []*http.Request
	bodies   []string
}

func newFakeLibraryBili(t *testing.T) *fakeBili {
	t.Helper()
	f := &fakeBili{handlers: map[string]http.HandlerFunc{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sb strings.Builder
		buf := make([]byte, 1024)
		for {
			n, err := r.Body.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		f.mu.Lock()
		f.seen = append(f.seen, r)
		f.bodies = append(f.bodies, sb.String())
		h := f.handlers[r.URL.Path]
		f.mu.Unlock()

		if h != nil {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":-404,"message":"没有这个接口","ttl":1}`))
	}))
	t.Cleanup(f.Close)
	// nav 一定要答：稍后再看那个接口要 WBI 签名，而签名之前先打一次 nav 拿密钥。
	// nav 打不通的话签名这步直接报错，连真正的请求都发不出去。
	f.on("/x/web-interface/nav", `{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,
	  "wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
	             "sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`)
	return f
}

// on 加一个回答，另外**顺手把 nav 也答上**：稍后再看那个接口要 WBI 签名，
// 签名之前先打一次 nav 拿密钥；nav 打不通的话签名这步直接报错，连真正的请求
// 都发不出去。
func (f *fakeBili) on(path string, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// calls 记下打给某一类请求的次数。
func (f *fakeBili) calls(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.seen {
		if r.URL.Path == path {
			n++
		}
	}
	return n
}

func (f *fakeBili) bodyOf(path string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.seen {
		if r.URL.Path == path {
			return f.bodies[i]
		}
	}
	return ""
}

// libraryOn 建一个挂着假客户端的 Library。
func libraryOn(t *testing.T, f *fakeBili) *Library {
	t.Helper()
	cli := bili.NewClient(bili.Options{
		// 用 httptest 自带的那份 transport——它认识那张自签证书。
		Transport: f.Server.Client().Transport,
		CSRF:      func() string { return "the-jct" },
	})
	// ⚠️ 不指到 fixture 上的话，这些用例会打到**真的** B 站：不报错、结果还挺
	// 合理，于是测试变成「取决于能不能连上 B 站」。
	cli.TestOnlyPointHostsAtFixture(f.URL)
	return &Library{Client: cli}
}

const laterOnePage = `{"code":0,"message":"0","ttl":1,"data":{"count":45,"list":[
  {"aid":170001,"bvid":"BV1xx411c7m0A","cid":3001,"title":"甲","pic":"http://h/a.jpg",
   "duration":185,"progress":60,"pubdate":1700000000,"add_at":1700000100,"is_pgc":false,
   "owner":{"mid":11,"name":"UP 甲"},"stat":{"view":123456}}]}}`

// TestLaterRowsCarryEverythingTheRowDraws 验一行该有的都有。
func TestLaterRowsCarryEverythingTheRowDraws(t *testing.T) {
	f := newFakeLibraryBili(t)
	f.on("/x/v2/history/toview/web", laterOnePage)

	items, more, err := libraryOn(t, f).ToView(context.Background(), 1, LibraryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("解出来 %d 行，want 1", len(items))
	}
	it := items[0]
	switch {
	case it.Title != "甲" || it.Owner != "UP 甲":
		t.Errorf("标题/UP = %q/%q", it.Title, it.Owner)
	case it.Duration.String() != "3m5s":
		t.Errorf("时长 %v，want 3m5s", it.Duration)
	case it.View != 123456:
		t.Errorf("播放量 %d，want 123456", it.View)
	case !it.CanPlay:
		t.Error("不是 PGC 却说不能放")
	case it.Item.Source != "online" || it.Item.Bvid != "BV1xx411c7m0A" || it.Item.Cid != "3001":
		t.Errorf("队列项不对：%+v", it.Item)
	}
	// 总条数原样透出：「还有没有」由界面层拿它和已加载行数比。
	if more != 45 {
		t.Errorf("count = %d，want 45", more)
	}
}

// TestToViewReportsTheTotalCount 钉住「总条数」原样透出去。
//
// 「还有没有」是**调用方**拿它和「已经加载了几行」比的（Biu 的
// `later/index.tsx:59`）：用「这一页满不满」去推在筛选之后是错的——筛完只剩
// 两页，第二页往往正好是满的，于是永远认为还有 → 一直转圈拉空页。
func TestToViewReportsTheTotalCount(t *testing.T) {
	for _, count := range []int{0, 1, 45, 12345} {
		f := newFakeLibraryBili(t)
		f.on("/x/v2/history/toview/web",
			`{"code":0,"message":"0","ttl":1,"data":{"count":`+strconv.Itoa(count)+`,"list":[
			  {"aid":1,"bvid":"BV1","title":"x","owner":{"mid":1,"name":"u"},"stat":{"view":1}}]}}`)

		_, got, err := libraryOn(t, f).ToView(context.Background(), 1, LibraryQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if got != count {
			t.Errorf("count = %d，want %d", got, count)
		}
	}
}

const historyTwoOfSameVideo = `{"code":0,"message":"0","ttl":1,"data":{"has_more":true,"page":{"pn":1,"total":2},"list":[
 {"title":"第一次","cover":"c1","author_name":"UP","author_mid":1,"view_at":1700000000,
  "progress":100,"duration":600,"kid":"archive_170001",
  "history":{"bvid":"BV1SAME","cid":1,"business":"archive"}},
 {"title":"第二次","cover":"c1","author_name":"UP","author_mid":1,"view_at":1700086400,
  "progress":200,"duration":600,"kid":"archive_170001",
  "history":{"bvid":"BV1SAME","cid":1,"business":"archive"}}]}}`

// TestHistoryRowsOfTheSameVideoGetDifferentIDs 钉住 🔴 铁律四。
//
// 同一首歌在 B 站的历史记录里是**好几条**（每一遍观看一条）。拿 bvid 当身份，
// 那几行的状态就串在一起：选中一行，旁边那几行也亮；而用户要点「删除」的
// 到底是哪一条，完全说不清。
func TestHistoryRowsOfTheSameVideoGetDifferentIDs(t *testing.T) {
	f := newFakeLibraryBili(t)
	f.on("/x/web-interface/history/search", historyTwoOfSameVideo)

	items, more, err := libraryOn(t, f).History(context.Background(), 1, LibraryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("解出来 %d 行，want 2", len(items))
	}
	if items[0].ID == items[1].ID {
		t.Fatalf("两行共用同一个身份 %q——状态会串在一起", items[0].ID)
	}
	if items[0].Item.Bvid != items[1].Item.Bvid {
		t.Fatal("测试的 fixture 不对：两行的 bvid 应该一样")
	}
	if !more {
		t.Error("has_more = false，接口给的是 true")
	}
	// 播放进度是这一页存在的理由。
	if items[0].Progress.String() != "1m40s" || items[0].Duration.String() != "10m0s" {
		t.Errorf("进度/时长 = %v/%v", items[0].Progress, items[0].Duration)
	}
}

// TestHistoryWithoutAKidStillGetsAnID 盯 kid 拼不出来的那一种。
//
// 直播、专栏那几类 B 站不给 kid。**身份还是要有的**——没有身份就是默认的「兄弟
// 序号」，在前面插一行会让后面所有行的状态错配（铁律四）。所以退回
// bvid + 观看时刻：同一时刻的同一场不会有两条。
func TestHistoryWithoutAKidStillGetsAnID(t *testing.T) {
	f := newFakeLibraryBili(t)
	f.on("/x/web-interface/history/search", `{"code":0,"message":"0","ttl":1,"data":{
		"has_more":false,"page":{"pn":1,"total":1},"list":[
	 {"title":"一场直播","view_at":1700000000,
	  "history":{"bvid":"BVLIVE","business":"live"}}]}}`)

	items, _, err := libraryOn(t, f).History(context.Background(), 1, LibraryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("解出来 %d 行，want 1", len(items))
	}
	if items[0].ID == "" {
		t.Error("没有 kid 的那一行分到了空身份")
	}
	if items[0].CanPlay {
		t.Error("直播标成了能放")
	}
}

// TestRowsWithoutABvidAreDropped 盯「没有 bvid 就跳过」。
//
// 没有 bvid 的条目画得出来但放不了，而它也没有行身份（两个列表的身份里都要有
// bvid）。摆一行没有身份的行，正是铁律四要消掉的那种错。
func TestRowsWithoutABvidAreDropped(t *testing.T) {
	f := newFakeLibraryBili(t)
	f.on("/x/web-interface/history/search", `{"code":0,"message":"0","ttl":1,"data":{
		"has_more":false,"page":{"pn":1,"total":2},"list":[
	 {"title":"专栏","kid":"article_1","history":{"business":"article"}},
	 {"title":"投稿","kid":"archive_2","history":{"bvid":"BV1OK","business":"archive"}}]}}`)

	items, _, err := libraryOn(t, f).History(context.Background(), 1, LibraryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "投稿" {
		t.Fatalf("没有 bvid 的那一行没被跳过：%+v", items)
	}
}

// TestRemoveLaterSendsTheAidOutOfTheID 盯删除走的是 ID 里那一位稿件 id。
func TestRemoveLaterSendsTheAidOutOfTheID(t *testing.T) {
	f := newFakeLibraryBili(t)
	f.on("/x/v2/history/toview/del", `{"code":0,"message":"0","ttl":1}`)

	l := libraryOn(t, f)
	id := laterID("BV1xx411c7m0A", 170001)
	if err := l.RemoveLater(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if f.calls("/x/v2/history/toview/del") != 1 {
		t.Fatalf("没有发出移除的请求")
	}
	if got := f.bodyOf("/x/v2/history/toview/del"); !strings.Contains(got, "aid=170001") {
		t.Errorf("body 里没有 aid=170001，原文是 %q", got)
	}
}

// TestRemoveRefusesAnIDItCannotRead 盯「认不出来就说删不了」。
//
// 发一个错的过去，B 站会回 code 0（成功）而什么都没删——用户看着那一行消失了、
// 下次刷新它又回来了。这比报错糟得多。
func TestRemoveRefusesAnIDItCannotRead(t *testing.T) {
	f := newFakeLibraryBili(t)
	f.on("/x/v2/history/toview/del", `{"code":0,"message":"0","ttl":1}`)
	f.on("/x/v2/history/delete", `{"code":0,"message":"0","ttl":1}`)

	l := libraryOn(t, f)
	for _, id := range []string{
		"",
		"later:BV1",                        // 没有 # 那一段
		"later:BV1#abc",                    // 稿件 id 不是数
		"later:BV1#0",                      // 稿件 id 是 0（B 站不会给）
		historyID("BVLIVE@1700000000", ""), // 没有 kid 的那种，删不了
	} {
		if err := l.RemoveLater(context.Background(), id); err == nil {
			t.Errorf("RemoveLater(%q) 没有报错——会发一个 B 站回 0 而什么都没删的请求", id)
		}
	}
	// 拿稍后再看的身份去删历史记录：# 后面那一位是稿件 id，不是 kid。
	if err := l.RemoveHistory(context.Background(), laterID("BV1xx411c7m0A", 170001)); err == nil {
		t.Error("RemoveHistory 拿了一个稍后再看的身份，没有报错")
	}
	if err := l.RemoveHistory(context.Background(), ""); err == nil {
		t.Error("RemoveHistory(\"\") 没有报错")
	}
	if f.calls("/x/v2/history/toview/del") != 0 || f.calls("/x/v2/history/delete") != 0 {
		t.Error("认不出来的身份仍然发了请求")
	}
}

// TestAnUnmountedLibrarySaysSo 钉住「没挂上」这一条路。
//
// 没挂上时**不能**返回空列表 + nil error：那在界面上就是「这里什么都没有」，
// 用户会去猜是不是自己 B 站上真的一条都没有。
func TestAnUnmountedLibrarySaysSo(t *testing.T) {
	var l *Library
	if _, _, err := l.ToView(context.Background(), 1, LibraryQuery{}); err == nil {
		t.Error("nil 的 Library 没有报错")
	}
	if _, _, err := l.History(context.Background(), 1, LibraryQuery{}); err == nil {
		t.Error("nil 的 Library 没有报错")
	}
	if err := l.RemoveLater(context.Background(), "x"); err == nil {
		t.Error("nil 的 Library 移除时没有报错")
	}
	if (&Library{}).Mounted() {
		t.Error("空的 Library 报成已挂上")
	}
	if l := (&Library{}); l.Mounted() {
		t.Error("空 Client 报成已挂上")
	}
}

// TestLibraryMountIsPerApp 盯挂载表是按 App 分的。
//
// 一个进程里可能有不止一个 App（测试里就有好几个）。挂到一个上不该让另一个也
// 以为挂上了——那会让「这个应用坏了」的排障从「查这一个的挂载」变成「查全局」。
func TestLibraryMountIsPerApp(t *testing.T) {
	f := newFakeLibraryBili(t)
	f.on("/x/v2/history/toview/web", laterOnePage)

	a1, a2 := New(), New()
	t.Cleanup(a1.Close)
	t.Cleanup(a2.Close)

	if a1.LibraryOf().Mounted() || a2.LibraryOf().Mounted() {
		t.Fatal("还没挂就报成已挂上")
	}
	a1.MountLibrary(libraryOn(t, f).Client)
	if !a1.LibraryOf().Mounted() {
		t.Error("挂了还报成没挂")
	}
	if a2.LibraryOf().Mounted() {
		t.Error("挂到 a1 上，a2 也报成已挂上了")
	}
	if a1.LibraryOf() != a1.LibraryOf() {
		t.Error("两次 LibraryOf 给了两个不同的摊——挂载表被换掉了")
	}
}
