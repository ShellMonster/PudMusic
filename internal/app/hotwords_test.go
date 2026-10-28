package app

// 搜索框热词这一摊的业务层测试。全部走本地 fixture，不碰真的 music.163.com。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/hotwords"
)

// hotFixture 是一台回固定热搜词的假网易云，数着被叫了几次。
type hotFixture struct {
	*httptest.Server
	mu    sync.Mutex
	body  string
	calls int
}

func newHotFixture(t *testing.T, body string) *hotFixture {
	t.Helper()
	f := &hotFixture{body: body}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.calls++
		body := f.body
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *hotFixture) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func hotsBody(words ...string) string {
	out := `{"code":200,"result":{"hots":[`
	for i, w := range words {
		if i > 0 {
			out += ","
		}
		out += `{"first":` + jsonEscape(w) + `}`
	}
	return out + `]}}`
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func hotwordsOn(t *testing.T, f *hotFixture) *Hotwords {
	t.Helper()
	c := hotwords.New(nil)
	c.TestOnlyPointHostsAtFixture(f.URL)
	h := &Hotwords{client: c}
	return h
}

// TestHotwordsFetchOnceAndCacheForAWhile 钉基本节奏：拉一次就有词，缓存期内
// 再怎么 Refresh 都不再发请求。和热榜歌名那一条是同一个形状（recommend_discover_test.go）。
func TestHotwordsFetchOnceAndCacheForAWhile(t *testing.T) {
	f := newHotFixture(t, hotsBody("季候风", "我不难过"))
	h := hotwordsOn(t, f)
	done := make(chan struct{}, 4)
	wake := func() { done <- struct{}{} }

	h.Refresh(wake)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("热词没拉回来")
	}
	if got := h.Words(); len(got) != 2 || got[0] != "季候风" || got[1] != "我不难过" {
		t.Errorf("热词 = %q，want [季候风 我不难过]", got)
	}
	n := f.count()
	h.Refresh(wake)
	time.Sleep(50 * time.Millisecond)
	if f.count() != n {
		t.Error("缓存期内又去拉了一遍热词")
	}
}

// TestHotwordsSilentlyRetryAfterAFailure 钉失败之后的退避：拉不到时 Words 是空、
// Refresh 不再发请求（记了失败时刻，退避期内白问是白问），界面上表现为占位词
// 停在「搜索」——静默，和联想词那一档同一个口径。
func TestHotwordsSilentlyRetryAfterAFailure(t *testing.T) {
	f := newHotFixture(t, `{"code":500,"msg":"挂了"}`)
	h := hotwordsOn(t, f)
	done := make(chan struct{}, 4)
	wake := func() { done <- struct{}{} }

	h.Refresh(wake)
	<-done
	if got := h.Words(); len(got) != 0 {
		t.Errorf("失败之后竟然有词：%q", got)
	}
	n := f.count()
	h.Refresh(wake)
	time.Sleep(50 * time.Millisecond)
	if f.count() != n {
		t.Error("失败之后立刻又去拉了一遍（该退避）")
	}
}

// TestHotwordsComeBackAfterAFailure 钉恢复：失败过一次之后，退避期过了再 Refresh
// 能把词接回来。用短退避等不动墙钟，这里直接造「第一趟失败、第二趟成功」的
// 服务器，验的是「失败没有把这一摊锁死」。
func TestHotwordsComeBackAfterAFailure(t *testing.T) {
	f := newHotFixture(t, `{"code":500,"msg":"挂了"}`)
	h := hotwordsOn(t, f)
	done := make(chan struct{}, 8)
	wake := func() { done <- struct{}{} }

	h.Refresh(wake)
	<-done
	if len(h.Words()) != 0 {
		t.Fatal("第一趟就该失败")
	}
	f.mu.Lock()
	f.body = hotsBody("我不难过")
	f.mu.Unlock()
	// 失败的退避只挡住「立刻重试」，退避过了就该再问。墙钟等不动一分钟，
	// 这里把失败时刻抹掉等价于「等到了退避点」。
	h.mu.Lock()
	h.at = time.Time{}
	h.mu.Unlock()
	h.Refresh(wake)
	<-done
	if got := h.Words(); len(got) != 1 || got[0] != "我不难过" {
		t.Errorf("热词 = %q，want 恢复成 [我不难过]", got)
	}
}

// TestHotwordsShapeForThePlaceholder 钉给界面的那一道筛：按 rune 数丢长词
// （搜索框最窄那档只有八十来像素，长词会被框边硬切）、按条数封顶。
// 顺序不动——那是热度顺序。
func TestHotwordsShapeForThePlaceholder(t *testing.T) {
	f := newHotFixture(t, hotsBody(
		"短词",
		"一个特别特别特别长的词肯定放不下",
		"八字的词",
		"又一首超出了十二个字的二字热词歌名",
	))
	h := hotwordsOn(t, f)
	done := make(chan struct{}, 4)
	wake := func() { done <- struct{}{} }
	h.Refresh(wake)
	<-done

	got := h.Words()
	if len(got) != 2 || got[0] != "短词" || got[1] != "八字的词" {
		t.Errorf("热词 = %q，want [短词 八字的词]（长词被筛掉）", got)
	}
}

// TestHotwordsKeepTheOldPoolWhenNothingFits 钉筛完只剩空的那个边角：网易云某一趟
// 给的全是摆不下的长词时，上一份好词池**不许**被换空——不然占位词闪回「搜索」
// 整整一个退避期（1 分钟），而上一份明明是能用的。
func TestHotwordsKeepTheOldPoolWhenNothingFits(t *testing.T) {
	f := newHotFixture(t, hotsBody("短词"))
	h := hotwordsOn(t, f)
	done := make(chan struct{}, 8)
	wake := func() { done <- struct{}{} }
	h.Refresh(wake)
	<-done
	if len(h.Words()) != 1 {
		t.Fatalf("第一趟就该有词：%q", h.Words())
	}

	f.mu.Lock()
	f.body = hotsBody("一个特别特别特别长的词肯定放不下", "这一首超长超长超长超长超长超长")
	f.mu.Unlock()
	h.mu.Lock()
	h.at = time.Time{} // 抹掉退避起点，等价于「等到了再问的时刻」
	h.mu.Unlock()
	h.Refresh(wake)
	<-done

	if got := h.Words(); len(got) != 1 || got[0] != "短词" {
		t.Errorf("全是长词的那一趟把词池换掉了：%q，want 保留 [短词]", got)
	}
}

// TestHotwordsWithoutAClientIsFree 钉没挂上的那一支：不 panic、不发请求、没词。
// 界面据此把占位词停在「搜索」，而不是打一个注定失败的网络请求。
func TestHotwordsWithoutAClientIsFree(t *testing.T) {
	var h *Hotwords
	if h.Mounted() {
		t.Error("nil 报挂上了")
	}
	h.Refresh(nil) // 不许 panic
	if got := h.Words(); got != nil {
		t.Errorf("没挂的竟然有词：%q", got)
	}

	empty := &Hotwords{}
	if empty.Mounted() {
		t.Error("没挂客户端却报挂上了")
	}
	empty.Refresh(nil)
	if got := empty.Words(); got != nil {
		t.Errorf("没挂客户端的竟然有词：%q", got)
	}
}

// TestHotwordsMountsPerApp 盯挂载是按 App 分的——两个窗口不串。
func TestHotwordsMountsPerApp(t *testing.T) {
	a1, a2 := New(), New()
	t.Cleanup(a1.Close)
	t.Cleanup(a2.Close)

	if a1.HotwordsOf().Mounted() {
		t.Error("没挂过的 App 报挂上了")
	}
	f := newHotFixture(t, hotsBody("季候风"))
	a1.MountHotwords(hotClient(t, f))
	if !a1.HotwordsOf().Mounted() {
		t.Error("挂上了却报没挂")
	}
	if a2.HotwordsOf().Mounted() {
		t.Error("另一个 App 也报挂上了——挂载是按 App 分的")
	}
}

func hotClient(t *testing.T, f *hotFixture) *hotwords.Client {
	t.Helper()
	c := hotwords.New(nil)
	c.TestOnlyPointHostsAtFixture(f.URL)
	return c
}
