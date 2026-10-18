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
	"net/http"
	"slices"
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

// TestSearchSuggestSkipsTheOnesThatAreNotWords 钉住两件事：空的丢掉、重复的只留
// 一个、顶上限。
//
// B 站偶尔给两遍同一个词（`term` 和 `name` 不同的那种）。浮层上摆两行一样的字，
// 用户会以为点错了；而超上限的那些留在文件里也没人看。
func TestSearchSuggestSkipsTheOnesThatAreNotWords(t *testing.T) {
	tags := []any{
		suggestTag("", ""),      // 空的：用户看见的是一排点不动的行
		suggestTag("甲", "甲"),    //
		suggestTag("乙", "乙"),    //
		suggestTag("甲", "另一个甲"), // 和上面那个重复
		suggestTag("  ", "空格的"), //
	}
	for i := range 20 {
		tags = append(tags, suggestTag(string(rune('a'+i)), "补的"))
	}
	_, c, _ := suggestFixture(t, map[string]any{"tag": tags})

	got, err := c.SearchSuggest(context.Background(), "甲")
	if err != nil {
		t.Fatalf("SearchSuggest: %v", err)
	}
	if len(got) != suggestLimit {
		t.Errorf("拿到 %d 条，want 顶上那 %d 条", len(got), suggestLimit)
	}
	for _, want := range []string{"甲", "乙"} {
		if !slices.Contains(got, want) {
			t.Errorf("结果里没有 %q：%q", want, got)
		}
	}
	if slices.Contains(got, "  ") || slices.Contains(got, "") {
		t.Errorf("空的那几条没被丢掉：%q", got)
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
