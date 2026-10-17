package app

// 这一组只验**界面那条路走不到**的三件事：删不掉的时候不许发请求、没挂上就说
// 没挂上、挂载按 App 分。
//
// **行身份本身（🔴 铁律四）是界面那一组钉的**——头部插一行之后选中项还是那一首、
// 同一首歌看过两次是两行，见 `internal/ui/librarypage_test.go`。那里走的是真外壳 +
// 真客户端 + 假服务器，同一条路径，本组再验一遍是重复。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// fakeBili 是一台照着给定答案回答的假 B 站，顺便把收到的请求和 body 录下来。
type fakeBili struct {
	*httptest.Server
	mu       sync.Mutex
	handlers map[string]string
	seen     []string
	bodies   []string
}

func newFakeLibraryBili(t *testing.T) *fakeBili {
	t.Helper()
	f := &fakeBili{handlers: map[string]string{}}
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
		f.seen = append(f.seen, r.URL.Path)
		f.bodies = append(f.bodies, sb.String())
		body := f.handlers[r.URL.Path]
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if body == "" {
			body = `{"code":-404,"message":"没有这个接口","ttl":1}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeBili) on(path, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[path] = body
}

// calls 是打给某一条路径的请求数。
func (f *fakeBili) calls(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.seen {
		if p == path {
			n++
		}
	}
	return n
}

// bodyOf 是打给某一条路径的那一次的 body 原文。
func (f *fakeBili) bodyOf(path string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.seen {
		if p == path {
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
	// ⚠️ 不指到 fixture 上的话这些用例会打到**真的** B 站：不报错、结果还挺合理，
	// 于是测试变成「取决于能不能连上 B 站」。
	cli.TestOnlyPointHostsAtFixture(f.URL)
	return &Library{Client: cli}
}

// TestRemoveLaterSendsTheAidOutOfTheID 盯删除走的是 ID 里那一位稿件 id。
func TestRemoveLaterSendsTheAidOutOfTheID(t *testing.T) {
	f := newFakeLibraryBili(t)
	f.on("/x/v2/history/toview/del", `{"code":0,"message":"0","ttl":1}`)

	if err := libraryOn(t, f).RemoveLater(context.Background(), laterID("BV1xx411c7m0A", 170001)); err != nil {
		t.Fatal(err)
	}
	if got := f.calls("/x/v2/history/toview/del"); got != 1 {
		t.Fatalf("移除发了 %d 次请求，want 1", got)
	}
	// ⚠️ 断言 **body 原文**而不是解析后的表单：bili.Client 发表单时不带
	// Content-Type，假服务器那边的 ParseForm 解析不出 body。
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
}

// TestLibraryMountIsPerApp 盯挂载表是按 App 分的。
//
// 一个进程里可能有不止一个 App（测试里就有好几个）。挂到一个上不该让另一个也
// 以为挂上了——那会让「这个应用坏了」的排障从「查这一个的挂载」变成「查全局」。
func TestLibraryMountIsPerApp(t *testing.T) {
	f := newFakeLibraryBili(t)

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
