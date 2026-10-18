// 搜索联想词：Biu 的 `getSearchSuggestMain`（`layout/navbar/search/index.tsx:49`
// 那个 `debounceWait` 挂的就是它）。
//
// # 这个接口在**另一台主机**上
//
// 它是 `s.search.bilibili.com` 而不是 `api.bilibili.com`——搜索站自己有一套
// 接口，不在 api 那台上。凭据边界照样认它（白名单按 `.bilibili.com` 后缀，见
// `app.DefaultInjectPolicy` 的注释，那儿本来就列着 `s.search.`）。
//
// # 不签名
//
// WBI 是 `api` 那台的规矩，这台不认。发一个带 `w_rid` 过去它照收不误——
// 但那是**多带的**，而依赖一个接口「不介意多余的参数」是另一种风险：哪天它开始
// 校验，多余的参数就会变成一道查不出来的错。所以不发。
//
// # 联想词不是搜索结果
//
// 这一个接口给的是**「别人打过的词」**：它不认 bvid、不认 cid、也不知道哪首歌是
// 音乐分区。点一条就是拿这个词去走一遍真搜索（`app.SubmitSearch` 那条路），
// 于是这一层只需要「这个词」和「界面上摆的那几个字」。
//
// ⚠️ **回包里有两套字**：`term` 是干净的关键词（拿去搜的那一个），`name` 是给
// 人看的（B 站会把命中的部分包进 `<em class="keyword">`）。直接显示 `name` 就是
// 一排尖括号，直接拿 `name` 去搜则带上标签。

package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// searchSuggestPath 是搜索联想词那个接口。Biu 上游那一个也是它。
const searchSuggestPath = "/main/suggest"

// suggestLimit 是浮层上最多摆几条。
//
// B 站这一档自己就只给十来条，而这里**再压一道**：联想是「打几个字就能选到」，
// 十几条要滚两屏，反而不如摆着的那几条好找。历史那一档是 10（`searchHistoryShown`），
// 两档摆一样的数量在浮层上也齐整。
const suggestLimit = 10

// searchSuggestRaw 是这个接口的 `data` 段。
//
// 只声明用得着的两个字段：B 站这一项还有 `ref`（跳转地址）、`rid`、`mid`、
// `goto`、`url`、`type` 等十几个，而我们只用得上「词」。
type searchSuggestRaw struct {
	Tag []struct {
		// Term 是干净的关键词——**拿去搜的就是它**。
		Term string `json:"term"`
		// Name 是给 人看的 那一份：B 站会把命中的部分包进 `<em class="keyword">`。
		Name string `json:"name"`
	} `json:"tag"`
}

// SearchSuggest 查一个词的联想，回的是**摆上去就能用**的那些词。
//
// ⚠️ 拿的是 `term` 而不是 `name`：`term` 是干净的一个词，而 `name` 是带标签的展示
// 形式（拿它去搜等于把 `<em class="keyword">` 当关键词发出去，B 站给回来的
// 结果和用户想搜的完全不是一回事）。两者不一样时以 `term` 为准——界面上摆的
// 和点下去搜的必须是同一个词。
//
// **没有词就一个请求都不发**（上游的 `if (query === "") return` 同一个门槛）。
//
// 失败原样往上抛，**不在这一层静默**：静默是业务层/界面那一层的决定（规格要求
// 「请求失败时静默」），而这一层该做的是把「拉不到」和「一个都没有」分开——前者
// 还有救，后者重试一百次也是没有。
func (c *Client) SearchSuggest(ctx context.Context, keyword string) ([]string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostSearch + searchSuggestPath,
		Query:  map[string][]string{"term": {keyword}},
	})
	if err != nil {
		return nil, err
	}

	var d searchSuggestRaw
	if err := json.Unmarshal(raw, &d); err != nil {
		// 回的是 200、code 也是 0，但 data 的形状不认识。同 search.go 那一支：
		// 这是 B 站改接口改出来的，重试没用。
		return nil, fmt.Errorf("bili: 联想词接口的返回读不懂：%w", err)
	}

	out := make([]string, 0, len(d.Tag))
	seen := make(map[string]bool, len(d.Tag))
	for _, tag := range d.Tag {
		term := strings.TrimSpace(tag.Term)
		// ⚠️ **term 认不出来就用剥过标签的 name 顶上**，不是跳过这一条：B 站的
		// 回包偶尔只有 name（而它是能搜的）。跳过去的话用户看到的是「B 站明明给了
		// 联想，浮层上却只有历史那一档」，而界面上没有任何地方能让他知道少了什么。
		if term == "" {
			term = stripHTML(tag.Name)
		}
		// 去重：B 站偶尔把同一个词给两遍（`term` 和 `name` 不同的那种），浮层上
		// 摆两行一样的字，用户会以为点错了。
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
		if len(out) == suggestLimit {
			break
		}
	}
	return out, nil
}
