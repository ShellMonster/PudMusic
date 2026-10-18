// 搜索浮层里那一档**联想词**（#97，A25）。业务层在 `app/searchsuggest.go`，
// 接口在 `service/bili/suggest.go`。
//
// # 它是浮层里的一档，不是另一个浮层
//
// 规格明写：两套浮层的关闭时机不一样，并排会出现「关了一个另一个还在」。所以这
// 一档**住在 `viewSuggest` 那个面板里**（navbar_search.go），跟着同一个
// `s.searchSuggest` 开关开合——Esc、失焦、回车收掉的是**两档一起**。
//
// # 和「搜索历史」分得开，是三件事一起做的
//
// 规格第二条：「联想词与历史建议在界面上区分得开」。只靠一句标题不够——两栏都
// 是「一串词」，标题一换用户就以为换了内容。这里是**形状**上也分开：
//
//	搜索历史   折行的一排 chip，每一条带一个 ×（能单删），顶上还有「清除全部」
//	联想词     竖着的一列整行，左对齐，没删的入口（那是 B 站给的，删不掉）
//
// 于是两栏在屏幕上不可能被看成同一栏，哪怕标题被用户扫过去。
//
// # 防抖：`librarypage.go` 那个 `searchDebounce`，**不另起一个**
//
// Biu 的 `debounceWait: 300` 是加在这一档上的（`search/index.tsx:49`）。规格里
// 写的是 500ms——和界面上另外两处「等用户把词敲完」（稍后再看 / 历史记录的搜索
// 框、歌词搜索弹窗）同一个值，也就是 `searchDebounce`。三处各自写一个 500 的话，
// 改一个忘了另一个，界面上一点也看不出来。
//
// ⚠️ **不许每敲一个字就问一次**：那是 B 站的风控，也是真的把每一笔都发出去。判据
// 在 `syncSuggest`，和 `libraryPage.keywordChanged` 是同一套（`seenDraft` 重打
// 那个钟、`pendingSince` 记起点）。
//
// # 那一个钟也是共用的
//
// `syncSearchWatch` 那个每 125ms 一跳的定时器由三处共用（列表页搜索框、歌词搜索、
// 这一档），**不许为这一档再排一个**——理由见 `syncSearchWatch` 自己的注释。

package ui

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	// suggestTermsHeading 是那一档顶上那一句。
	//
	// ⚠️ 它必须和「搜索历史」不一样，而**更要紧的是它必须和浮层里别的字都不一样**：
	// 无头 Tester 的 `Find` 认「画着这句字**或者**标着这个名字」的元素，两处叫同
	// 一个名字的话，按名字找会撞车（返回先建的那个）。这里和历史那一档、搜索框
	// 那个 placeholder（「搜索」）三处互不相同。
	suggestTermsHeading = "联想词"

	// suggestTermsKey 是这一档那一块的键（铁律四）。它和历史那一档的
	// `suggest-history` 分开，于是两栏是两块而不是一栏。
	suggestTermsKey = "suggest-terms"

	// suggestTermPadY / suggestTermPadX 是联想词那一行的上下 / 左右留白。
	//
	// 比 chip 那圈大一点：chip 是小圆角的小块，而联想词是**一整行**，行高小一点
	// 的话指针扫过去时行与行之间几乎没有可停的地方。
	suggestTermPadY, suggestTermPadX = 5, 8

	// suggestTermsGap 是历史那一档和联想词那一档之间留的那一道空隙。
	//
	// ⚠️ **只在一档都没少的时候才有**：没有历史可摆时凭空留一道，浮层顶上就是一条
	// 空边，看着像被切掉了一块。
	suggestTermsGap = 8
)

// suggestTerms 是这一档跨帧要活着的状态。**只由界面线程读写**（铁律一）。
//
// 它只有防抖那两样——「已经问过的那个词」住在业务层（`app.SuggestState.Query`），
// 在这里存一份就是两个真相，而两个真相迟早分家（业务层那份记着失败过的词，
// 这一份不记，于是界面每一帧都判「还没问过」）。
type suggestTerms struct {
	// seen 是上一帧框里的字。**防抖的钟靠它重打**：用户每敲一个字都要重新开始等
	// 那 500ms（上游 `useDebounceFn` 就是每个输入都重置那个钟）。从第一个字算起的
	// 话，慢打的时候会在中途问一个还没输完的词。
	seen string
	// pendingSince 是「敲下去、还没问出去」的起点。零值 = 没有待问的那一次，而
	// `syncSearchWatch` 那个钟认的就是它。
	pendingSince time.Time
}

// syncSuggest 收「这一帧要不要问 B 站要联想词」。**每帧调一次**（`viewSearchBox` 里）。
//
// 🔴 **浮层没开、框里没有词，都不问**：浮层关着的时候用户看不见联想词，为一个
// 看不见的东西发请求是白花钱；而空词那个请求连 B 站都不会回（`bili.SearchSuggest`
// 自己在门口就挡住了）。
//
// ⚠️ 「已经问过了」这一支**在失败之后也成立**——业务层失败时仍然记着那个词
// （`app.SearchSuggest` 的注释）。少了它就是自己养自己的循环：每一帧都判「还没
// 问过」、再问一次、再失败，界面上是搜索框卡一下、浮层底下一直刷网络。
//
// ⚠️ **收浮层、或者框里空了，都把业务层那一摊也收掉**（`app.SearchSuggest("")`
// 不会发请求，它只是把上一批词和「已经问过哪个词」清成空）。留着的话有两个后果：
// 上一个问题的词挂在一个已经和它无关的框底下；而用户删掉几个字再敲一遍同一个词
// 时会一直撞在「已经问过了」上——**上一次失败时尤其如此**（失败是静默的，界面上
// 一个提示都没有，用户唯一能做的就是再敲一遍）。
func (s *Shell) syncSuggest(now time.Time) {
	q := strings.TrimSpace(s.searchKeyword)
	switch {
	case !s.searchSuggest || q == "":
		s.terms.seen, s.terms.pendingSince = "", time.Time{}
		s.app.SearchSuggest("")
	case q != s.terms.seen:
		// 用户又敲了一个字：把钟重打，这一帧不问。
		s.terms.seen, s.terms.pendingSince = q, now
	case q == s.app.SuggestState().Query:
		s.terms.pendingSince = time.Time{}
	case now.Sub(s.terms.pendingSince) >= searchDebounce:
		s.terms.pendingSince = time.Time{}
		s.app.SearchSuggest(q)
	}
}

// suggestSearching 报「那个 500ms 的钟要不要醒着」（`syncSearchWatch` 的一个 `want`）。
func (s *Shell) suggestSearching() bool {
	return !s.terms.pendingSince.IsZero()
}

// viewSuggestTerms 画浮层里那一档「联想词」。**一个都没有就不摆**（调用方判）。
//
// ⚠️ **拉回来的那一刻不摆任何「正在找…」**：用户是在一个字一个字地打，联想是
// 一闪一闪补上来的，中间摆一句「正在搜…」比不给还吵——他自己知道在等什么。
func (s *Shell) viewSuggestTerms(c *ui.Context) {
	items := s.app.SuggestState().Items
	if len(items) == 0 {
		return
	}
	ui.Column(c).Key(suggestTermsKey).FillWidth().Gap(2).Children(func() {
		// 标题和「搜索历史」那一句同一个字号和粗细：两栏是平级的，谁也不比谁大。
		ui.Text(c, suggestTermsHeading).FontSize(13).Bold().Grow(1)
		for _, term := range items {
			s.viewSuggestTerm(c, term)
		}
	})
}

// viewSuggestTerm 是浮层里的一条联想词：整行都是它。
//
// ⚠️ **不能是 chip**：chip 是历史那一档的形状，而两栏长得一样的话「哪个是我搜过
// 的、哪个是 B 站给的」这件事就没有答案了（规格第二条）。所以这里是**一整行**、
// 左对齐、没有边框、悬停才显出底色——和那排带边框、带 × 的 chip 一眼能分开。
//
// Label 不只是给测试用的：读屏软件念出来的是这一条词，而正文（Children 里那个
// Text）不念。
func (s *Shell) viewSuggestTerm(c *ui.Context, term string) {
	t := c.Theme()
	row := ui.ButtonBase(c).Label(term).
		FillWidth().Radius(t.Radius).
		Padding(suggestTermPadY, suggestTermPadX).
		AlignItems(ui.Start)
	// 悬停才显底色——平时一列字摆在那儿，指针扫过去有反应，用户才知道点得动。
	if row.Hovered() {
		row = row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		ui.Text(c, term).FontSize(13).Grow(1).MaxLines(1).Ellipsis("…")
	})

	// ⚠️ **先把词填回框里**（和历史那一条同一件事，见 `viewHistoryChip` 的注释）：
	// 不填的话用户点完之后框里还是原来敲的半截字，而页面那边搜的是这一条——
	// 按返回再回来会看到那个错的词。
	if row.Clicked() {
		s.searchKeyword = term
		s.submitSearch(term)
	}
}
