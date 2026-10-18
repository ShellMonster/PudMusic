package bili

// 联想词那一层的测试：发出去的参数、回来的词怎么认、以及「拉不到」不许装成
// 「一个都没有」。
//
// fixture 的形状来自 B 站搜索站那个接口的回包（`data.tag[]`，每一项一个 `term`
// 一个 `name`）。**没有任何一条断言是在证明「B 站现在真这么返回」**，那要靠真机
// 回归——见任务报告「需要真网验」那一节。

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// suggestFixture 建一台会回联想词的假 B 站，返回录下来的请求。
func suggestFixture(t *testing.T, data any) (*fakeBili, *Client, func() []recordedRequest) {
	t.Helper()
	f := newFakeBili(t)
	f.on(searchSuggestPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", data))
	})
	return f, f.client(t, Options{}), f.requests
}

// suggestTag 造一条联想词。B 站把命中的部分包在 `name` 里，而 `term` 是干净的。
func suggestTag(term, name string) map[string]any {
	return map[string]any{"term": term, "name": name, "type": "huashu", "ref": "", "mid": 0}
}

// suggestRequest 找出录下来的那一次联想词请求。
func suggestRequest(t *testing.T, seen []recordedRequest) recordedRequest {
	t.Helper()
	for _, r := range seen {
		if r.Path == searchSuggestPath {
			return r
		}
	}
	t.Fatalf("没有发过 %s，录到的是 %v", searchSuggestPath, seen)
	return recordedRequest{}
}

// TestSearchSuggestSendsTheWordOnTheSearchHost 盯请求发对了地方、发对了参数。
//
// ⚠️ **签名不许带**：WBI 是 `api` 那台的规矩，搜索站不认。发过去它照收不误——
// 而依赖一个接口「不介意多余的参数」是另一种风险：哪天它开始校验，多余的参数就
// 变成一道查不出来的错。所以这里钉住「没有 w_rid」。
func TestSearchSuggestSendsTheWordOnTheSearchHost(t *testing.T) {
	_, c, seen := suggestFixture(t, map[string]any{"tag": []any{suggestTag("周杰伦", "周杰伦")}})

	if _, err := c.SearchSuggest(context.Background(), " 周杰伦 "); err != nil {
		t.Fatalf("SearchSuggest: %v", err)
	}

	r := suggestRequest(t, seen())
	if r.Method != "GET" {
		t.Errorf("用的是 %s，want GET", r.Method)
	}
	if got := r.Query.Get("term"); got != "周杰伦" {
		t.Errorf("term 是 %q，want 周杰伦（首尾的空格要掐掉）", got)
	}
	if got := r.Query.Get("w_rid"); got != "" {
		t.Errorf("带了 WBI 签名（w_rid=%q）——搜索站不认它，带上去只是赌它不介意", got)
	}
	// 也不该先去 nav 拿 WBI 的 key（`signWBI` 会那么干）：这一趟不该发两个请求。
	for _, other := range seen() {
		if other.Path == pathNav {
			t.Errorf("为了一个不签名的请求去取了一次 WBI 的 key：%v", other.Path)
		}
	}
}

// hostRecorder 是一层「记下每个请求发去了哪台机器」的网络。
type hostRecorder struct {
	// hosts / paths 按到达顺序记下来（一个请求一条）。
	hosts, paths []string
	// body 是答给每一个请求的那一段。
	body string
}

func (h *hostRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	h.hosts = append(h.hosts, r.URL.Host)
	h.paths = append(h.paths, r.URL.Path)
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(h.body)),
		Request:    r,
	}, nil
}

// TestSearchSuggestGoesToTheSearchHostNotTheApiOne 钉住**发去哪一台**。
//
// ⚠️ 这一条是本票最要紧、又最验不出来的一环，而「最验不出来」正是本票自己的报告
// 承认的：fixture 那台假 B 站把**每一个**主机都指到同一个本地 URL（那正是
// `TestOnlyPointHostsAtFixture` 干的事），所以「请求打到了 `/main/suggest`」这一句
// 对 `api.bilibili.com` 和 `s.search.bilibili.com` **一样成立**——把 `hostSearch`
// 写成 `hostAPI`，整包测试照样全绿。
//
// 而地址错了的**症状在界面上和「拉不到」一模一样**：联想词那一档永远是空的。
// 于是判据必须是**请求真正的那个 host**，而不是路径。
func TestSearchSuggestGoesToTheSearchHostNotTheApiOne(t *testing.T) {
	rec := &hostRecorder{body: jsonData(t, 0, "0", map[string]any{"tag": []any{suggestTag("甲", "甲")}})}

	// 生产上那一份客户端（默认主机，一个都没指到 fixture 上）——这一层要量的正是
	// **默认值**，因为 fixture 一指就把主机换掉了。
	c := NewClient(Options{Transport: rec})
	if _, err := c.SearchSuggest(context.Background(), "周杰伦"); err != nil {
		t.Fatalf("SearchSuggest: %v", err)
	}

	if len(rec.hosts) != 1 {
		t.Fatalf("发了 %d 个请求（%v），want 一个", len(rec.hosts), rec.hosts)
	}
	if got := rec.hosts[0]; got != mustHost(HostSearch) {
		t.Errorf("请求发去了 %q，want 搜索站那一台（%q）——"+
			"打成 api 那一台的话症状是「联想词永远是空的」，和真的拉不到一模一样", got, HostSearch)
	}
	if got := rec.paths[0]; got != searchSuggestPath {
		t.Errorf("打的是 %q，want %q", got, searchSuggestPath)
	}
	// 而「没有 w_rid / 不先跑一趟 nav」是**另一条**（那条 fixture 版的量的是
	// 参数和请求个数），所以这里顺手钉住一件这里才看得见的事：它**只发一个请求**。
	if len(rec.hosts) != 1 {
		t.Errorf("为了一个不签名的请求跑了 %v", rec.hosts)
	}
}

// TestTheSuggestHostIsNotTheApiHost 把「两个常量不许是同一个」写成断言。
//
// 上一条量的是「请求发去了哪台」，那靠的是 `hostSearch` 这个字段；这一条量的是
// **常量本身**——有人把 `HostSearch` 的值抄成 `HostAPI`（两行挨着，一个域名改
// 错了肉眼扫不出来）的话，上一条会跟着一起红，而这里会先说清是哪一处抄错了。
func TestTheSuggestHostIsNotTheApiHost(t *testing.T) {
	if mustHost(HostSearch) == mustHost(HostAPI) {
		t.Errorf("HostSearch 和 HostAPI 是同一台（%q）——联想词那个接口不在 api 那台上", HostAPI)
	}
	if !strings.HasPrefix(HostSearch, "https://") {
		t.Errorf("HostSearch 是 %q，链接要用 https", HostSearch)
	}
	if mustHost(HostSearch) != "s.search.bilibili.com" {
		t.Errorf("HostSearch 是 %q，want s.search.bilibili.com（搜索站那一台）", HostSearch)
	}
}

// mustHost 剥掉一个主机常量前面那段 `https://`（拿 URL 的 host 比，少一层改错的机会）。
func mustHost(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	return u.Host
}

// TestSearchSuggestGivesBackThePlainWords 钉住「摆上去的和搜下去的是同一个词」。
//
// B 站两套字：`term` 是干净的，`name` 里把命中的部分包进了
// `<em class="keyword">`。拿 `name` 显示就是一排尖括号，拿它去搜就是把标签当
// 关键词发出去，而 B 站给回来的和用户想搜的完全不是一回事。
func TestSearchSuggestGivesBackThePlainWords(t *testing.T) {
	_, c, _ := suggestFixture(t, map[string]any{"tag": []any{
		suggestTag("周杰伦 晴天", `<em class="keyword">周杰伦</em> 晴天`),
		suggestTag("周杰伦", `<em class="keyword">周杰伦</em>`),
	}})

	got, err := c.SearchSuggest(context.Background(), "周杰")
	if err != nil {
		t.Fatalf("SearchSuggest: %v", err)
	}
	want := []string{"周杰伦 晴天", "周杰伦"}
	if len(got) != len(want) {
		t.Fatalf("拿到 %q，want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个是 %q，want %q", i, got[i], want[i])
		}
	}
}

// TestSearchSuggestDropsTheOnesThatAreNotWords 钉住两件事：空的丢掉、重复的只留
// 一个。
//
// ⚠️ **重复的那一条排在最前面，后面不再垫东西**：早先这一条把「甲」放在第 4 位、
// 后面补了 20 条，于是结果被 `suggestLimit`(10) 切掉时，重复的那一条**根本进不了
// 结果**——把去重判据整个删掉，那条用例照样绿（评审实测：*** SURVIVED ***）。
// 判据因此改成**数出现次数**（「甲」只该有一个），它和「上限在哪」互不相干。
//
// B 站偶尔给两遍同一个词（`term` 和 `name` 不同的那种）。浮层上摆两行一样的字，
// 用户会以为点错了。
func TestSearchSuggestDropsTheOnesThatAreNotWords(t *testing.T) {
	_, c, _ := suggestFixture(t, map[string]any{"tag": []any{
		suggestTag("甲", "甲"),
		suggestTag("乙", "乙"),
		suggestTag("甲", "另一个甲"), // 和第一条重复
		suggestTag("丙", "丙"),
		// 下面这四条 `term` 和 `name` 都认不出来一个字——摆出去是浮层上一行
		// 点不动的空气（其中两条是纯空白：早先 `name` 顶上来的那一条没掐头尾空白，
		// 「   」就这么摆出去了）。
		suggestTag("", ""),
		suggestTag("  ", "   "),
		suggestTag("", "   "),
		suggestTag(" ", " "),
	}})

	got, err := c.SearchSuggest(context.Background(), "甲")
	if err != nil {
		t.Fatalf("SearchSuggest: %v", err)
	}
	want := []string{"甲", "乙", "丙"}
	if len(got) != len(want) {
		t.Fatalf("拿到 %q，want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条是 %q，want %q（去掉脏的那几条不许把好的也一起去掉）",
				i, got[i], want[i])
		}
	}
	// 去重单独钉一次「数出现次数」：上面那条整体比较已经盖住了它，而这一句让
	// 「去重判据没了」时的失败信息直接指着这一件事。
	if n := count(got, "甲"); n != 1 {
		t.Errorf("结果里「甲」有 %d 个，want 1：%q", n, got)
	}
}

// count 数一个词在结果里出现了几次。
func count(list []string, want string) int {
	n := 0
	for _, v := range list {
		if v == want {
			n++
		}
	}
	return n
}

// TestSearchSuggestKeepsOnlyTheFirstFew 钉住上限。
//
// 单独一条而不是和去重混在一起：混在一起时**上限会把重复的那一条切掉**，于是去重
// 那一半验不到（见上一条的注释）。上限也有它自己的坏法（省掉了最后几条用户真正
// 想要的），所以它得单独被钉一次。
func TestSearchSuggestKeepsOnlyTheFirstFew(t *testing.T) {
	tags := make([]any, 0, suggestLimit+5)
	for i := range suggestLimit + 5 {
		tags = append(tags, suggestTag(strconv.Itoa(i), "第"+strconv.Itoa(i)))
	}
	_, c, _ := suggestFixture(t, map[string]any{"tag": tags})

	got, err := c.SearchSuggest(context.Background(), "0")
	if err != nil {
		t.Fatalf("SearchSuggest: %v", err)
	}
	if len(got) != suggestLimit {
		t.Fatalf("拿到 %d 条，want 顶上那 %d 条：%q", len(got), suggestLimit, got)
	}
	for i := range suggestLimit {
		want := strconv.Itoa(i)
		if got[i] != want {
			t.Errorf("第 %d 条是 %q，want %q（要的是**前** %d 条，顺序也不许变）",
				i, got[i], want, suggestLimit)
		}
	}
}

// TestSearchSuggestFallsBackToTheNameWhenThereIsNoTerm 钉住「只有 name 也认」。
//
// 少了这一支的话，用户看到的是「B 站明明给了联想，浮层上却只有历史那一档」，
// 而界面上没有任何地方能让他知道少了什么。
func TestSearchSuggestFallsBackToTheNameWhenThereIsNoTerm(t *testing.T) {
	_, c, _ := suggestFixture(t, map[string]any{"tag": []any{
		suggestTag("", `<em class="keyword">薛之谦</em>演员`),
	}})

	got, err := c.SearchSuggest(context.Background(), "薛")
	if err != nil {
		t.Fatalf("SearchSuggest: %v", err)
	}
	if len(got) != 1 || got[0] != "薛之谦演员" {
		t.Errorf("拿到 %q，want [薛之谦演员]（name 要剥掉标签再用）", got)
	}
}

// TestSearchSuggestAsksForNothingWithoutAWord 钉住「没有词一个请求都不发」。
//
// 上游是 `if (query === "") return`（`search/index.tsx:54` 那个门槛）。少这一句的
// 症状是：用户点开搜索框（浮层开着、框里空的）就发一次，翻一晚上历史能发几十次。
func TestSearchSuggestAsksForNothingWithoutAWord(t *testing.T) {
	for _, q := range []string{"", "   ", "\t\n"} {
		_, c, seen := suggestFixture(t, map[string]any{"tag": []any{suggestTag("甲", "甲")}})
		got, err := c.SearchSuggest(context.Background(), q)
		if err != nil {
			t.Fatalf("SearchSuggest(%q): %v", q, err)
		}
		if got != nil {
			t.Errorf("SearchSuggest(%q) 回的是 %q，want nil", q, got)
		}
		if n := len(seen()); n != 0 {
			t.Errorf("SearchSuggest(%q) 发了 %d 个请求", q, n)
		}
	}
}

// TestSearchSuggestSaysSoWhenItCannotAsk 钉住「拉不到」不许装成「一个都没有」。
//
// 静默是**界面那一层**的决定（规格：「请求失败时静默，浮层保持只有历史那一档」），
// 而这一层该做的是把两种情况分开——这一条要是返回空列表，界面就再也分不出
// 「B 站没给联想」和「网断了」，而前者不该让用户去检查网络。
func TestSearchSuggestSaysSoWhenItCannotAsk(t *testing.T) {
	f := newFakeBili(t)
	f.on(searchSuggestPath, func(w http.ResponseWriter, r *http.Request) {
		// B 站的业务错都在 HTTP 200 里面。
		writeJSON(w, jsonData(t, -412, "请求过于频繁", nil))
	})

	got, err := f.client(t, Options{}).SearchSuggest(context.Background(), "周杰伦")
	if err == nil {
		t.Fatalf("拉不到却回的是 %q（没有错误）", got)
	}
	if !errors.Is(err, ErrRiskControl) {
		t.Errorf("错是 %v，want 一个认得出的（ErrRiskControl）", err)
	}
	if got != nil {
		t.Errorf("失败时还回了一批词 %q", got)
	}
}

// TestSearchSuggestSaysSoWhenTheAnswerIsUnreadable 同上，只是形状不认识。
//
// 回的是 200、code 也是 0，而 data 不是 `{"tag": […]}`。重试没用——这是 B 站改
// 接口改出来的，而界面上的说法得是「拉不到」而不是「没有联想」。
func TestSearchSuggestSaysSoWhenTheAnswerIsUnreadable(t *testing.T) {
	f := newFakeBili(t)
	f.on(searchSuggestPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", "这不是那个形状"))
	})

	if _, err := f.client(t, Options{}).SearchSuggest(context.Background(), "周杰伦"); err == nil {
		t.Fatal("读不懂的返回居然没报错")
	}
}
