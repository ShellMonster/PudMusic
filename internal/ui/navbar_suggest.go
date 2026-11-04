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
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

const (
	// suggestTermsHeading 是那一档顶上那一句。2026-11 重设计照参考图叫「搜索联想」
	// （原来是「联想词」）。
	//
	// ⚠️ 它必须和「历史搜索」不一样，而**更要紧的是它必须和浮层里别的字都不一样**：
	// 无头 Tester 的 `Find` 认「画着这句字**或者**标着这个名字」的元素，两处叫同
	// 一个名字的话，按名字找会撞车（返回先建的那个）。这里和历史那一档、面板本体
	// （`suggestPanelLabel`）、搜索框那个 placeholder（「搜索」）互不相同——「搜索联想」
	// 和「搜索」是两个字串，`Find` 逐字相等，不撞。
	suggestTermsHeading = "搜索联想"

	// suggestTermsKey 是这一档那一块的键（铁律四）。它和历史那一档的
	// `suggest-history` 分开，于是两栏是两块而不是一栏。
	suggestTermsKey = "suggest-terms"

	// suggestHotHeading / suggestHotKey 是「大家在听」那一档的标题和键。
	suggestHotHeading = "大家在听"
	suggestHotKey     = "suggest-hot"

	// suggestFailedNote 是「这一趟没拉到」时那一档自己的一句话。
	//
	// ⚠️ **它不许是「没有联想词」**：那一句说的是「B 站没给联想」，而没拉到是另一
	// 回事（网络断了、登录态过期、风控）。两种「没有」长得一模一样的话，用户读到
	// 的是「B 站没有联想」，而真相是「这一趟没回来」（报告 246 第 12 条）。
	//
	// ⚠️ **它只是一句话，不弹错误、不转圈、不变红**：规格要的是「失败时静默」
	// （issue #97 第四条）——静默说的是「别拿一条红字吓用户」，不是「一个字都不许
	// 说」。而这一句是这一档**唯一**能说清「为什么是空的」的地方。
	//
	// ⚠️ **它说得够准**：不说「网络不通」——那会让用户去查自己的网，而真实原因也
	// 可能是登录态过期或者 B 站风控（`errSearchNotMounted` 那一句也是这个口径，
	// 见 `searchresults.go`）。
	suggestFailedNote = "没能取到联想词"

	// suggestTermPadY / suggestTermPadX 是联想词那一行的上下 / 左右留白。
	//
	// 比 chip 那圈大一点：chip 是小圆角的小块，而联想词是**一整行**，行高小一点
	// 的话指针扫过去时行与行之间几乎没有可停的地方。2026-11 重设计把上下从 5
	// 放到 8：行里多了一枚放大镜，参考图里每一行也更松（约 36 高）。
	suggestTermPadY, suggestTermPadX = space.Px8, space.Px8

	// suggestHeadingPadTop / Right / Bottom / Left 是三档标题行那一圈留白（历史
	// 那一档原来就是这几个数，现在三档共用）。
	suggestHeadingPadTop, suggestHeadingPadRight   = space.Px2, space.Px4
	suggestHeadingPadBottom, suggestHeadingPadLeft = space.Px6, space.Px4
	suggestTermsHeadingKey, suggestHotHeadingKey   = "suggest-terms-header", "suggest-hot-header"

	// suggestTermsGap 是历史那一档和联想词那一档之间留的那一道空隙。
	//
	// ⚠️ **只在一档都没少的时候才有**：没有历史可摆时凭空留一道，浮层顶上就是一条
	// 空边，看着像被切掉了一块。
	suggestTermsGap = space.Px8

	// suggestScrollKey 是面板里那个滚动容器的键（铁律四）。
	suggestScrollKey = "suggest-scroll"

	// suggestPanelTop 是面板顶边离窗口顶边多远。它挂在搜索框下面（搜索框在顶栏里，
	// 顶栏是 `navMinHeight` 那一高），这里多留几像素给那一小段间隙。
	suggestPanelTop = navMinHeight + 8

	// suggestPanelMargin 是面板底边离屏幕底边留的空白。贴着屏幕底边看起来像被切掉
	// 了半截。
	suggestPanelMargin = space.Px12

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

// suggestNav 是联想面板的**键盘导航**状态（2026-11 重设计）：↑↓ 在历史 chip
// 和联想词两档之间上下走，回车搜选中的那一条。
//
// # 为什么是「扁平下标」
//
// 面板上摆着的是**两块**（历史那一排折行的 chip、联想词那一列整行），而方向键
// 是**一条线**——用户按 ↓ 期望的是「走到下面那一条」，不管它在哪一档。所以这里
// 把两档摊平成一条 `[]string`（画出来的顺序：历史在上、联想在下），`sel` 是它
// 的下标。-1 = 没选中（回车还是搜框里的词）。
//
// ⚠️ **选中的是「词」不是「元素」**：chip 那一排是**折行**的，「林俊杰」在第几
// 行第几列由面板宽和词长决定，下标 `sel` 不知道也不该知道——画的时候按「这一条
// 词是不是 items[sel]」上选中底，布局的事交给布局。
//
// ⚠️ **`items` 每帧重建，词会换序**（联想词异步回来、历史增删）：`sel` 指的下标
// 可能还合法，但那条词已经不是用户按方向键时看到的那一条了——回车搜到的就不是
// 他选的那一条。所以武装时把那条词记在 `picked`，items 重建时验一下「items[sel]
// 还是不是 picked」，变了就当这条失效（sel/armed 都回 -1/false，回车还是搜框里的词）。
type suggestNav struct {
	// items 是这一帧可走的词，扁平、按画出来的顺序：历史在前、联想在后。
	// 每帧在 viewSuggest 里重建（那一摊词每一帧都可能变）。
	items []string
	// sel 是 items 里选中的下标，-1 = 没选中。
	sel int
	// armed 是「键盘导航被方向键激活过」。零值 `suggestNav{}` 的 `sel` 是 0，
	// 会把第一条误当成选中——所以选中与否看 `armed`，不看 `sel` 自己是不是
	// -1：只有按过方向键（置真）才算「用户选中了一条」，敲字 / 收浮层都把它
	// 放回 false。
	armed bool
	// picked 是 armed 那一刻 `items[sel]` 的词——items 重建时拿它验 sel 还指不
	// 指着同一条（上面那段注释），不是拿来显示的。
	picked string
	// reveal 是「刚用方向键挪到了这一条，把它滚到看得见」。
	// 只在按方向键那一帧置真，画的时候读一次就清。
	reveal bool
}

// syncSuggestPending 推进联想词那个防抖，**不碰收浮层那一支**。
//
// ⚠️ **它和 `syncSuggest` 的区别只有一处**：`syncSuggest` 在浮层关着 / 框里空了
// 的时候会把业务层那一摊收掉（`app.SearchSuggest("")`），而这一支不收——
// 收浮层这件事是**看得见的那一半**的活（浮层关不关由用户点的那一下决定），
// 满屏开着的时候顶栏不画（票 #161），那一支跟着一起停才对。
//
// 为什么要有这一支：满屏开着的时候顶栏不画，`syncSuggest` 也不跑，于是那个
// 500ms 的防抖**停在半路**——用户敲完词、开了满屏，那一趟联想词要等到关掉满屏
// 才问出去。挪到这里它就照常到点就问。
//
// ⚠️ **一帧里它和 `syncSuggest` 各跑一次**：这一支在画的前面（`syncUnderlay`），
// `syncSuggest` 在画的里面。跑两遍不冲突——这一支先走一遍「重打钟」，`syncSuggest`
// 再走一遍时看到的是「钟已经打好了」，于是两遍的结果一样。
func (s *Shell) syncSuggestPending(now time.Time) {
	q := strings.TrimSpace(s.searchKeyword)
	if q == "" || !s.searchSuggest {
		// ⚠️ **钟要清**：浮层关着 / 框里空了的时候，`s.terms.pendingSince` 必须回到
		// 零——不然 `syncSearchWatch` 会一直判「该醒着」，每 125ms 叫醒一次窗口，
		// 而满屏开着的时候那一帧画的是满屏（白醒）。
		//
		// ⚠️ **业务层那一摊（`s.app.SearchSuggest("")`）不在这里收**：收浮层是
		// 「看得见的那一半」的活（浮层关不关由用户点的那一下决定），它留在
		// `syncSuggest` 里。
		//
		// 浮层的滚动位置也一起收（理由同 `syncSuggest` 那一处）：面板每帧都在建，
		// 滚动位置每帧都重写，而面板**不画**的时候那一帧没人写它——所以这里把
		// 位置收掉，重新点开那个框时才是顶上。
		s.terms.seen, s.terms.pendingSince = "", time.Time{}
		s.terms.scroll = ui.ScrollState{}
		return
	}
	switch {
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

// viewSuggestHeading 是面板里一段的小标题（原型 `.suggest h4`：12 / 500 / text-3，
// 外边距 4/8/8/8），右边可挂一个操作。
func viewSuggestHeading(c *ui.Context, key, title string, trailing func()) {
	p := tok.Of(c.Theme())
	ui.Row(c).Key(key).FillWidth().AlignItems(ui.Center).
		Padding(space.Px4, space.Px8, space.Px8, space.Px8).
		Children(func() {
			ui.Text(c, title).FontSize(scale.Font(c, scale.Size12)).FontWeight(500).
				TextColor(p.Text3).SingleLine().Grow(1)
			if trailing != nil {
				trailing()
			}
		})
}

// viewSuggestSubmitRow 是输入了字之后的第一行（原型：放大镜 + 「搜索「xxx」」 + ↵ 键帽）。
// 它不进上下键的候选序列：回车本来就是提交输入框里的字。
func (s *Shell) viewSuggestSubmitRow(c *ui.Context) {
	kw := strings.TrimSpace(s.searchKeyword)
	if kw == "" {
		return
	}
	p := tok.Of(c.Theme())
	label := "搜索「" + kw + "」"
	row := ui.ButtonBase(c).Key("suggest-submit").Label(label).FillWidth().Radius(tok.R8).
		Padding(space.Px8).Gap(space.Px12).AlignItems(ui.Center)
	if row.Hovered() {
		row = row.Background(p.Hover)
	}
	row.Children(func() {
		ui.Icon(c, pud.Icon("search")).FontSize(tok.IconSm).TextColor(p.Text)
		ui.Text(c, label).FontSize(scale.Font(c, scale.Size14)).Grow(1).MaxLines(1).Ellipsis("…")
		pud.Kbd(c, "↵")
	})
	if row.Clicked() {
		s.submitSearch(kw)
	}
}

// viewSuggestTerms 是联想词那一段。
func (s *Shell) viewSuggestTerms(c *ui.Context, items []string, failed bool, idx *int) {
	if len(items) == 0 && !failed {
		return
	}
	p := tok.Of(c.Theme())
	ui.Column(c).Key(suggestTermsKey).FillWidth().Children(func() {
		viewSuggestHeading(c, suggestTermsHeadingKey, suggestTermsHeading, nil)
		if len(items) == 0 {
			ui.Text(c, suggestFailedNote).FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text3).
				Padding(space.None, space.Px8, space.Px8, space.Px8)
			return
		}
		for _, term := range items {
			s.viewSuggestTerm(c, term, *idx)
			*idx++
		}
	})
}

// viewSuggestHot 是「热门搜索」一段：序号（等宽 13，前三名 accent-text）+ 词。
// 原型右边的「N 首」是假数据，B 站热搜没有这个数，不画。
func (s *Shell) viewSuggestHot(c *ui.Context, titles []string, idx *int) {
	ui.Column(c).Key(suggestHotKey).FillWidth().Children(func() {
		viewSuggestHeading(c, suggestHotHeadingKey, suggestHotHeading, nil)
		for rank, title := range titles {
			s.viewSuggestHotRow(c, title, *idx, rank)
			*idx++
		}
	})
}

// suggestRow 是联想行的壳（原型 `.sug-row`：内边距 8、缝 12、圆角 8，悬停 / 选中 `hover` 底）。
func (s *Shell) suggestRow(c *ui.Context, term string, idx int) ui.Element {
	p := tok.Of(c.Theme())
	row := ui.ButtonBase(c).Label(term).FillWidth().Radius(tok.R8).
		Padding(space.Px8).Gap(space.Px12).AlignItems(ui.Center)
	picked := s.navPicked(idx)
	if row.Hovered() || picked {
		row = row.Background(p.Hover)
	}
	if picked && s.nav.reveal {
		row.ScrollIntoView()
	}
	return row
}

func (s *Shell) viewSuggestHotRow(c *ui.Context, title string, idx, rank int) {
	p := tok.Of(c.Theme())
	row := s.suggestRow(c, title, idx)
	row.Children(func() {
		col := p.Text3
		if rank < 3 {
			col = p.AccentText
		}
		typo.Mono(ui.Text(c, strconv.Itoa(rank+1))).FontSize(scale.Font(c, scale.Size13)).
			TextColor(col).Width(20).Shrink(0).TextAlign(ui.Center)
		ui.Text(c, title).FontSize(scale.Font(c, scale.Size14)).Grow(1).MaxLines(1).Ellipsis("…")
	})
	if row.Clicked() {
		s.searchKeyword = title
		s.submitSearch(title)
	}
}

// viewSuggestTerm 是一条联想词：放大镜 + 词（命中的字 accent-text）。
func (s *Shell) viewSuggestTerm(c *ui.Context, term string, idx int) {
	p := tok.Of(c.Theme())
	row := s.suggestRow(c, term, idx)
	row.Children(func() {
		ui.Icon(c, pud.Icon("search")).FontSize(tok.IconSm).TextColor(p.Text3)
		pre, hit, post := splitSuggestion(s.searchKeyword, term)
		size := scale.Font(c, scale.Size14)
		if hit == "" {
			ui.Text(c, term).FontSize(size).Grow(1).MaxLines(1).Ellipsis("…")
			return
		}
		ui.Row(c).Grow(1).Children(func() {
			ui.Text(c, pre).FontSize(size).Shrink(0)
			ui.Text(c, hit).FontSize(size).Shrink(0).TextColor(suggestHitColor(c.Theme()))
			ui.Text(c, post).FontSize(size).Grow(1).MaxLines(1).Ellipsis("…")
		})
	})
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

// suggestHitColor 是联想词那一行里「命中的那几个字」的颜色：**主色当字色**那一档
// （`ink.AccentOn`），2026-11 重设计照参考图定的——命中段跟主题主色走。
//
// ⚠️ **不能直接拿 `t.Accent`**：主色是用户可设的，浅色主题下默认那个亮绿压在白底
// 上不到 2:1，整行读不出来（理由在 `flatbutton.go` 的 `flatCurrent` 那一段，票
// #139 处理那一族）。`ink.AccentOn` 是从用户选的主色现算一个对底 ≥4.5:1 的变体，
// 深色下原样——颜色的身份留着，字读得出来。
//
// ⚠️ **也不是 `t.AccentText`**：那是「主色当**底**」时压在上面的黑 / 白字色
// （`applyAccent` 按主色亮度挑的），和这里「主色当**字**」是两件事（`ink.go` 的
// `AccentOn` 注释把两者的关系写清了）。
//
// 旧口径是上游写死的粉 `#F54180`（`src/app.css` 的 `.suggest_high_light`），浅色下
// 还得压暗一档才过 AA——一个不随主题走的颜色，用户换了主色它还是粉的，已废。
func suggestHitColor(t *ui.Theme) ui.Color {
	return tok.Of(t).AccentText
}
