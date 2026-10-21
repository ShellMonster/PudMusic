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

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
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

	// suggestScrollKey 是面板里那个滚动容器的键（铁律四）。
	suggestScrollKey = "suggest-scroll"

	// suggestPanelTop 是面板顶边离窗口顶边多远。它挂在搜索框下面（搜索框在顶栏里，
	// 顶栏是 `navMinHeight` 那一高），这里多留几像素给那一小段间隙。
	suggestPanelTop = navMinHeight + 8

	// suggestPanelMargin 是面板底边离屏幕底边留的空白。贴着屏幕底边看起来像被切掉
	// 了半截。
	suggestPanelMargin = 12

	// suggestPanelFloor 是面板的最小高度：窗口再矮也不能矮成三四行。
	suggestPanelFloor = 160
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
	// scroll 是面板里那个滚动容器滚到哪儿了。**建一次存着**：它是跨帧的状态，
	// 每帧新建等于每帧回到顶上（用户往下翻两屏就白翻了）。
	//
	// ⚠️ 浮层收掉时清零（见 `syncSuggest`）：下一次点开那个框看到的是**顶上**那几
	// 条，而那一框里的词多半已经和上一次不是同一个问题了。
	scroll ui.ScrollState
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
		// 面板每帧都在建，滚动位置每帧都重写（`TrackScroll` 的常态），而面板
		// **不画**的时候那一帧没人写它——所以这里把位置收掉，重新点开时才是顶上。
		s.terms.scroll = ui.ScrollState{}
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
		ui.Text(c, suggestTermsHeading).FontSize(scale.Font(c, 13)).Bold().Grow(1)
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
// ⚠️ **命中的那几个字染成上游那个粉**（票 #138 的顺带那一条）：上游把 B 站回包里
// 命中的部分染成粉色（回包里的 `name` 把命中的字包在 `<em>` 里，上游渲染成粉
// 色），而我们取的是干净的 `term`——**十行全是同一种灰，用户得逐行读完才知道
// 哪条对得上自己敲的那几个字**。
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
		// ⚠️ **三段：前缀 / 命中段 / 后缀**。命中段是**自己算的**（拿用户已经
		// 敲的词在 `term` 里找）——B 站回包里的 `name` 带 `<em>` 标签，而**服务层
		// 取的是干净的 `term`**（那个判断是对的：拿带标签的 `name` 去搜，等于把
		// `<em>` 当关键词发出去，真会搜错）。
		pre, hit, post := splitSuggestion(s.searchKeyword, term)
		if hit == "" {
			// 找不到命中段（用户敲的词这一条里没有）：整行一个颜色，和原来一样。
			ui.Text(c, term).FontSize(scale.Font(c, 13)).Grow(1).MaxLines(1).Ellipsis("…")
			return
		}
		ui.Row(c).Grow(1).Children(func() {
			ui.Text(c, pre).FontSize(scale.Font(c, 13)).Shrink(0)
			ui.Text(c, hit).FontSize(scale.Font(c, 13)).Shrink(0).TextColor(suggestHitColor(t))
			ui.Text(c, post).FontSize(scale.Font(c, 13)).Grow(1).MaxLines(1).Ellipsis("…")
		})
	})

	// ⚠️ **先把词填回框里**（和历史那一条同一件事，见 `viewHistoryChip` 的注释）：
	// 不填的话用户点完之后框里还是原来敲的半截字，而页面那边搜的是这一条——
	// 按返回再回来会看到那个错的词。
	if row.Clicked() {
		s.searchKeyword = term
		s.submitSearch(term)
	}
}

// splitSuggestion 把联想词那一行拆成「前缀 / 命中段 / 后缀」。
//
// ⚠️ **命中段是自己算的**：B 站回包里的 `name` 把命中的字包在 `<em>` 里，而
// 我们取的是干净的 `term`——所以命中段是拿**用户已经敲的词**在 `term` 里找出来
// 的（服务层那个判断是对的：拿带标签的 `name` 去搜，等于把 `<em>` 当关键词发
// 出去，真会搜错）。
//
// 找不到就返回「前缀空、命中段空、后缀是整条」——那一行就整行一个颜色，和原来
// 一样。
//
// ⚠️ **按 rune 找，不按字节**：大小写折叠会改字节长度（少见的 Unicode 字母），
// 按字节切会切在字符中间，屏幕上就是半个字。
func splitSuggestion(typed, term string) (pre, hit, post string) {
	typed = strings.TrimSpace(typed)
	if typed == "" || term == "" {
		return "", "", term
	}
	rs := []rune(term)
	qs := []rune(typed)
	// 原样找一遍，找不到再按小写找一遍（英文的大小写：用户敲 "iphone"，
	// B 站回 "iPhone"）。
	for _, lower := range []bool{false, true} {
		hay, needle := rs, qs
		if lower {
			hay = []rune(strings.ToLower(term))
			needle = []rune(strings.ToLower(typed))
		}
		for i := 0; i+len(needle) <= len(hay); i++ {
			if string(hay[i:i+len(needle)]) != string(needle) {
				continue
			}
			return string(rs[:i]), string(rs[i : i+len(needle)]), string(rs[i+len(needle):])
		}
	}
	return "", "", term
}

// suggestHitColor 是联想词那一行里「命中的那几个字」的颜色。
//
// ⚠️ **上游那个粉在浅色底上不够**：#F54180 压在白底上是 3.5:1（AA 的门槛是
// 4.5:1），而这一行是 13px 的正文。所以**浅色那一档压暗一档**（5.2:1），深色
// 那一档用上游那个数（压在深色底上是 5.0:1）。
//
// ⚠️ **别拿主色当这一段的字色**：那个亮绿压在白底上不到 2:1，整行读不出来
// （理由在 `flatbutton.go` 的 `flatCurrent` 那一段，票 #139 处理那一族）。
//
// 出处：上游 `src/app.css` 的 `.suggest_high_light,.keyword{color:#F54180}`。
func suggestHitColor(t *ui.Theme) ui.Color {
	pink := ui.Color{R: 0xF5, G: 0x41, B: 0x80, A: 255}
	if t.Dark {
		return pink
	}
	return pink.Mix(ui.Color{A: 255}, 0.2)
}
