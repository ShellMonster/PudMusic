// 歌词搜索与预览那两个弹窗（Biu 的
// `src/components/lyrics-search-modal/`，#21 拆出来的界面那一半）。
//
// # 搜索、缓存、防抖都不在这一层
//
// 底下那一层（`internal/app/lyrics_search.go`）已经做完的是：两个源、结果的
// 缓存、搜索与预览各自的代次守卫、采用时的换歌校验。这一层只做四件事——**摆**、
// **收输入**、**把状态画出来**、**把用户的动作翻译成那九个方法上的一次调用**。
//
// 🔴 **界面线程上一次网络请求都不许有**（铁律三）。看得见的网络只有两处，都在
// `app` 那一层的 goroutine 里：搜索（`SearchLyrics`）和预览（`OpenLyricPreview`）。
// 这一层调它们都是**立刻返回**的。
//
// # 三个状态都要有，搜不到要给交代
//
// 候选区一共五种说法，少一种界面上就是一片空白，而空白和「加载中」在用户那边是
// 同一句话（都是「这个界面没在回答我」）：
//
//	没搜过     状态行「X · 还没搜过」 + 「输入歌名开始搜索」
//	搜索中     状态行「X · 正在搜…」
//	搜到了     状态行「X · 搜到 N 条」 + 候选表
//	搜到了空的 状态行「X · 没搜到这一首」 + 「没搜到」+ 换个词 / 换个源
//	拉失败了   状态行 + 失败那句话 + 一个**重试按钮**
//
// 那一行**状态行**答的是「这批是哪个源给的、搜到没有」，底下那一片答的是「现在能
// 做什么」。分开写之后每一支里都不必把同一句话说两遍。
//
// ⚠️ 「没搜过」和「没搜到」不许合成一句：那是用户完全不同的两种处境，合成之后他
// 看到的是「我明明还没搜，它就说我搜不到」。
//
// ⚠️ 失败那一支必须带按钮：不带的话用户能做的只有把窗口关掉再打开一次，而那和
// 「拉不到」是一件事。
//
// # 三个「不许」
//
//   - **不许每帧重发**。`app.failLocked` 失败时**留着** `Query` 就是给这里挡着的：
//     那个词仍是「生效的那个」，于是 `lyricsQueryChanged` 的 `draft == applied`
//     那一支每一帧都判它已经搜过了，一个请求都不再发。真要重搜，用户得**自己按**
//     那个按钮（`TestAFailedSearchSaysItIsAFailureAndDoesNotFireAgain`）。
//   - **不许在敲字时每一笔都发**。防抖是 `librarypage.go` 那个 `searchDebounce`
//     （500ms），**不是这里另写一个**：两处各自写一个 500 的话，改一个忘了另一个，
//     而它们行为不一致这件事界面上一点也看不出来。
//   - **不许写第二套定时器**。`Shell.searchWatchStop` 那一个钟由
//     `syncSearchWatch` 统一管（`lyricsSearching` 是它的一个 `want`）。
package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/leading"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// lyricSearchHeading 是搜索弹窗顶上那一句，抄上游 `index.tsx:148` 的
	// `搜索歌词`。
	lyricSearchHeading = "搜索歌词"

	// lyricSearchBoxLabel 是那个关键词框，抄上游 `index.tsx:154` 的
	// `placeholder="请输入歌曲或歌手名称"`。
	//
	// ⚠️ 它同时是 Label 和 Placeholder：没有 Label 的话读屏软件读不出这是一个
	// 搜歌名的输入框（占位文字不是标签），无头 Tester 也只认 label / text。
	lyricSearchBoxLabel = "请输入歌曲或歌手名称"

	// lyricSearchGoLabel 是那个「搜索」按钮，抄上游 `index.tsx:158`。
	lyricSearchGoLabel = "搜索"

	// lyricSearchRetryLabel 是失败之后那个按钮。
	//
	// ⚠️ 它得是个**动作**而不是「知道了」：失败之后用户能做的事只有「再试一次」，
	// 摆一个不能推进任何事的按钮等于让那一支和没有一样。
	lyricSearchRetryLabel = "重试"

	// lyricSearchKnobLabel 是右下角那一列里那个旋钮的**名字**。
	//
	// ⚠️ **钮上不再有这三个字**（票 #194：那一枚现在是 ⌀32 的圆钮，圆里只有
	// 一枚 T 形框的图标），它是**无障碍名**——读屏软件念它，无头 Tester 找它。
	//
	// ⚠️ **必须和弹窗顶上那一句不一样**：Tester 的 `Find` 认「画着这句文字**或者**
	// 标着这个名字」的元素，两处都叫「搜索歌词」的话按名字找会撞车（返回的是先
	// 建的那个，也就是旋钮，而测试要点的恰恰是弹窗里的东西）。
	lyricSearchKnobLabel = "找歌词"

	// 弹窗与候选区的尺寸。候选区 340 抄上游 `netease-tab.tsx:69` 的 `h-[340px]`；
	// 预览正文 420 抄 `lyrics-preview-modal.tsx:69` 的 `max-h-[420px]`。
	//
	// ⚠️ **两个宽度是我们自己定的**：上游是 HeroUI 的 `size="4xl"` / `size="xl"`，
	// 框架没有那个尺寸，得给个数。720 在我们最窄的窗口（`WindowOptions.MinWidth`
	// 900）下连弹窗自己的内边距都放得下；预览那一个更窄，因为它是**套在搜索弹窗
	// 里面**的（上游也是这么嵌的，见 `netease-tab.tsx:104`），不能比外面那个宽。
	lyricSearchWidth     = 720.0
	lyricSearchListH     = 340.0
	lyricPreviewWidth    = 560.0
	lyricPreviewBodyH    = 420.0
	lyricPreviewAdopt    = "采用歌词"
	lyricPreviewClose    = "关闭"
	lyricPreviewHeading  = "歌词预览"
	lyricPreviewOrigTab  = "歌词原文"
	lyricPreviewTransTab = "歌词翻译"

	// lyricColArtist / lyricColDuration 是候选表右边那两列的宽。
	//
	// ⚠️ **要定宽**：不定的话「3:05」和「10:05」两行左右跳，而这一列是竖着对齐
	// 看的东西（理由和 `later.go` 的 `colClock` 一样）。
	lyricColArtist   = 140.0
	lyricColDuration = 56.0

	// lyricSearchBodyKey 是候选区那一块的键（铁律四）。
	lyricSearchBodyKey = "lyrics:search:body"
	// lyricPreviewBodyKey 是预览弹窗里正文那一块的键。
	lyricPreviewBodyKey = "lyrics:preview:body"
)

// 这几句是候选区与预览区在「还没搜 / 搜到了但是空的 / 正在拉」时说的话。
const (
	// lyricSearchIdle 是「还没搜过」。
	lyricSearchIdle = "输入歌名开始搜索"
	// lyricSearchEmpty 是「搜过了，一个候选都没有」。
	lyricSearchEmpty = "没搜到"
	// lyricNoHitsHint 是紧跟着它的那一句：**怎么才能不空**。
	//
	// ⚠️ 它得给用户一个能做的下一步（见 `emptyState.Hint` 的那一条规矩）：光一句
	// 「没搜到」的话，用户不知道是该把词改短一点还是该换个源。
	lyricNoHitsHint = "换个词再搜一次，或者切到另一个源看看"
	// lyricPreviewBusy 是预览里「正在拉这一条」。
	lyricPreviewBusy = "正在拉歌词…"
)

// lyricSearchUI 是这两个弹窗此刻的全部状态。**只由界面线程读写**（铁律一）。
type lyricSearchUI struct {
	// open 是搜索弹窗开着没有。关掉时下面那些都不作数。
	open bool
	// preview 是预览弹窗开着没有。**每一帧从 `app.LyricPreview().Open` 重装**，
	// 因为那才是「预览开着」这件事的真相；这一份只是 `ui.Modal` 要的那个 `*bool`。
	preview bool

	// draft 是搜索框里正在敲的字，applied 是**已经在搜**的那个词。
	//
	// 分成两个的理由和 `librarypage.go` 的 draft/applied 一模一样：即时模式每一帧
	// 都重画，直接拿框里的字去搜的话用户每敲一个字就发一次请求——而防抖解决的是
	// 频率，解决不了「框里的字和生效的词是同一个」这个混淆。
	draft, applied string

	// seenDraft 是上一帧框里的字。**防抖的钟靠它重打**：用户每敲一个字都要重新
	// 开始等那 500ms（上游 `useDebounceFn(wait: 500)` 就是每个输入都重置那个钟）。
	// 从第一个字算起的话，用户慢慢打的时候会在中途就发出去一个还没输完的词。
	seenDraft string

	// pendingSince 是「敲下去、还没发出去」的起点。零值 = 没有待发的搜索，
	// 而 `Shell.searchWatchStop` 那个钟认的就是它（见 `lyricsSearching`）。
	pendingSince time.Time

	// tab 是切在哪个源上（`LyricSources` 给的顺序下标）。
	tab int
	// previewTab 是预览里切在「歌词原文」还是「歌词翻译」。
	previewTab int

	// ls 是候选表的 `ListState`（滚动位置、行身份）。**建在壳上而不是每帧新建**：
	// 新建等于每帧回到顶上，用户往下翻两屏就白翻了。
	ls ui.ListState
	// rowKeys 记着候选表每一行的身份（**预先装箱好的那一份**）。
	//
	// ⚠️ 装箱那一步是要分配的（`runtime.convTstring`），现算的话每帧每行一次
	// （票 #313）。**它自己知道「数据换了」**（见 `rowkey.Memo` 的文件头）：
	// 换源、换一批候选——身份对不上就重算。
	rowKeys rowkey.Memo[lyricRowIdent, any]
	// previewScroll 是预览里正文那一段的滚动位置。换一份歌词要回到顶上。
	previewScroll ui.ScrollState
}

// reset 把这一摊收成「没开过」。
//
// ⚠️ **列表状态也要一起收**：留着的话下一次打开弹窗，用户看到的是上一次那一批
// 候选的滚动位置（列表按 `Key` 认得那些行），而界面上摆的是**这一次**搜出来的。
func (l *lyricSearchUI) reset() {
	l.draft, l.applied, l.seenDraft = "", "", ""
	l.pendingSince = time.Time{}
	l.tab, l.previewTab = 0, 0
	l.ls = ui.ListState{}
	l.previewScroll = ui.ScrollState{}
}

// ─────────────────────────────────────────────────────────────────────────────
// 开关
// ─────────────────────────────────────────────────────────────────────────────

// openLyricSearch 开搜索弹窗，并**立刻按屏幕上的这首歌搜一次**。
//
// 🔴 **一个源都没挂时不许开**（`TestNoSourcesMeansTheSearchDialogNeverOpens`）：
// 开着的弹窗里用户输完词、按下搜索，看到的会是一句「没有歌词源」，而那不是他
// 做了什么导致的。这跟「提交那一下什么都不会发生」是同一件。
func (s *Shell) openLyricSearch() {
	if len(s.app.LyricSources()) == 0 {
		s.addToast("这一版还没有接上歌词源", notice.Warning)
		return
	}
	st := s.app.Snapshot()
	if !st.HasTrack {
		// `app.OpenLyricPreview` 没有歌的时候什么都不做（没有能采用给谁），
		// 所以在这儿说清比让用户对着一个空的弹窗发呆好。
		s.addToast("先放一首歌，再去找它的歌词", notice.Warning)
		return
	}

	l := &s.lyricSearch
	l.open, l.tab, l.previewTab = true, 0, 0
	// ⚠️ 上一批的滚动位置要收掉（同 reset 的理由）。
	l.ls = ui.ListState{}
	// 框里先填屏幕上这一首的歌名（上游 `index.tsx:47-54`：
	// `setKeyword(playItem.pageTitle || playItem.title)`）——用户多半就是冲着
	// 「正在放的这首没有歌词」才点进来的，让他先打一遍这串字没有道理。
	l.draft = st.Title
	s.applyLyricsSearch(l.draft)
}

// lyricsSearching 报「那个 500ms 的钟要不要醒着」。
//
// ⚠️ **要连着 `open` 一起看**：弹窗关掉之后那个钟必须跟着停，而
// `pendingSince` 是要留着下次打开用的（不清它的话，下次一打开就当「早就等够了」
// 立刻发一次）。
func (s *Shell) lyricsSearching() bool {
	return s.lyricSearch.open && !s.lyricSearch.pendingSince.IsZero()
}

// applyLyricsSearch 真的按当前 Tab 去搜 q。
//
// ⚠️ **它把「待发」和「上一次敲的字」都清干净**：不清的话下一帧 `draft` 仍然
// 认得出「用户刚敲的」，于是每帧都要判一遍防抖——而失败之后那一支会把用户锁死
// 在「已经搜过了」里出不来（见 `lyricsQueryChanged` 的第二条 🔴）。
func (s *Shell) applyLyricsSearch(q string) {
	l := &s.lyricSearch
	q = strings.TrimSpace(q)
	l.applied, l.seenDraft, l.pendingSince = q, q, time.Time{}
	// 立刻返回：请求在 `app` 的后台 goroutine 里（铁律三）。
	s.app.SearchLyrics(l.tab, q)
}

// lyricsQueryChanged 收「这一帧要不要搜」。
//
// 🔴 **回车不等那 500ms**（上游 `index.tsx:155-157` 的 `onKeyDown Enter` 直接调
// `handleSubmit`）。用户按下回车就是想现在搜，让他等半秒是另一回事。
//
// 🔴 **`draft == applied` 那一支是「失败之后不许每帧重发」的全部**：失败时
// `app.failLocked` 留着 `Query`，于是 `applied` 仍是刚才那个词，这一支每一帧
// 都判它「已经搜过了」并把 `pendingSince` 清成零值。少了它，`now - 零值`
// （一大截）恒大于防抖窗口，于是下一帧立刻又发一次、再失败、再发——而生产里
// 每次投递都 `wake()` 叫醒下一帧，那个循环**自己养自己**（实测 20 帧打了 22 次）。
func (s *Shell) lyricsQueryChanged(now time.Time, submitted bool) {
	l := &s.lyricSearch
	draft := strings.TrimSpace(l.draft)
	switch {
	case submitted:
		l.seenDraft = draft
		s.applyLyricsSearch(draft)
	case draft != l.seenDraft:
		// 用户又敲了一个字：把钟重打，这一帧不搜。
		l.seenDraft, l.pendingSince = draft, now
	case draft == l.applied:
		l.pendingSince = time.Time{}
	case now.Sub(l.pendingSince) >= searchDebounce:
		s.applyLyricsSearch(draft)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 画
// ─────────────────────────────────────────────────────────────────────────────

// viewLyricDialogs 画那两个弹窗。**每帧调一次**（`viewMain` 里）。
//
// 🔴 外面包着 Guard（铁律四）：它挂在 `viewMain` 的最后，整页的输入都被遮罩
// 吃掉，所以它自己坏了坏掉的是整个窗口。
func (s *Shell) viewLyricDialogs(c *ui.Context) {
	l := &s.lyricSearch
	if !l.open {
		return
	}
	s.viewLyricSearchDialog(c)
	if !l.open {
		// 搜索弹窗这一帧被关掉了（Esc / 点外面）。**预览必须一起收**：它在应用层
		// 还开着，而界面上已经没有一个弹窗罩着它了——下一次打开搜索弹窗就会看到
		// 上一次那一份没看完的歌词。
		s.app.CloseLyricPreview()
		l.reset()
		return
	}
	if pv := s.app.LyricPreview(); pv.Open {
		s.viewLyricPreviewDialog(c, pv)
	}
}

// viewLyricSearchDialog 画搜索弹窗。
func (s *Shell) viewLyricSearchDialog(c *ui.Context) {
	l := &s.lyricSearch
	srcs := s.app.LyricSources()

	ui.Modal(c, &l.open, func() {
		ui.Column(c).Key("lyrics:search").Width(lyricSearchWidth).Gap(space.Px10).Children(func() {
			// ⚠️ **字重不写**（400）：弹窗标题是 400（DESIGN.md §二）。
			ui.Text(c, lyricSearchHeading).FontSize(scale.Font(c, scale.Size14))
			// 关键词框 + 「搜索」按钮（上游 `index.tsx:153-161`）。
			ui.Row(c).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
				box := ui.TextInput(c, &l.draft).
					Grow(1).
					// ⚠️ Label 不能省：它不只是给测试用的——没有它读屏软件读不出
					// 这是一个搜歌名的输入框（占位文字不是标签），无头 Tester 也
					// 只认 label / text。
					Label(lyricSearchBoxLabel).
					Placeholder(lyricSearchBoxLabel)
				submitted := box.Submitted()
				if ui.PrimaryButton(c, lyricSearchGoLabel).Clicked() {
					submitted = true
				}
				// 收防抖的这一趟**放在这一行画完之后**：框里的字这一帧才刚被用户改
				// 掉，而「搜索」按钮可能也在同一帧被点了，两样都要认。
				s.lyricsQueryChanged(s.clock.Now(), submitted)
			})

			// 两个源各一个 Tab（上游 `index.tsx:163-175`）。
			//
			// ⚠️ **`ui.Tabs` 是建到的时候才写 `*selected`**（`mygo/ui/base.go` 的
			// `tabsParts.Tab` 的 `choose`），所以判据是**画完之后 tab 变了没有**，
			// 不是 `Changed()`。理由照抄 `searchpage.go` 里那两排 Tabs 的注释：
			// 顺序反过来的话用户点了「另一个源」，这一帧画的还是上一个源的候选。
			prevTab := l.tab
			ui.Tabs(c, &l.tab, lyricSourceNames(srcs)...)
			if l.tab != prevTab {
				// 切源立刻用**当前这个词**重搜（上游那个 `useEffect([activeTab])`）。
				// 不重搜的话用户切过去看到的是上一个源的候选——而那两个源的候选连
				// ID 的取值范围都不一样（网易云是歌曲 id，LRCLib 是它的数字 id）。
				s.applyLyricsSearch(l.draft)
			}

			// ⚠️ **状态要在上面两行之后才取**：换 Tab 和按「搜索」都是这一帧刚发生
			// 的事，提前取的话这一帧画的是**上一次**那一批候选。
			st := s.app.LyricSearch()
			s.viewLyricCandidates(c, srcs, st)
		})
	})
}

// lyricSourceNames 是两个 Tab 上的字，抄上游（`index.tsx:177-178` 的
// `网易云` / `LrcLib`）。
//
// ⚠️ **用的是 `Name` 而不是 `ID`**：`LyricSource` 两个字段都在（见它的注释），
// 机器名是行身份和缓存键、Tab 上那个字是给人看的——拿机器名当 Tab 上的字，用户
// 看到的就是「lrclib」而不是「LrcLib」。
func lyricSourceNames(srcs []app.LyricSource) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, s.Name)
	}
	return out
}

// lyricSourceID 是第 tab 个源的**机器名**（越界时给空串）。
//
// ⚠️ 它和下面那个 `lyricSourceLabel` 只差一个字，而两样东西**不能互换**：机器名
// 是行身份和缓存键，标签是给用户看的。拿标签当行键的话改一次文案就把所有行的
// 身份改了（见 `app.LyricSource` 的注释）。
func lyricSourceID(srcs []app.LyricSource, tab int) string {
	if tab < 0 || tab >= len(srcs) {
		return ""
	}
	return srcs[tab].ID
}

// viewLyricCandidates 画候选区：顶上那一条**状态行** + 底下那一片**内容**。
//
// 这么分是因为两者问的不是一件事：状态行答「这批是哪个源给的、搜到没有」，内容
// 答「现在能做什么」。分开写之后每一支里都不必把同一句话说两遍（上面写着「正在
// 搜」、下面又写一遍「正在搜」的那种写法，这一层不写）。
//
// ⚠️ **判据的次序不能换**：`Loading` 必须在 `!Searched` 前面——搜索中的时候
// `Searched` 也是真的（`app` 那边一发搜索就把它立起来），次序反了的话搜索中会
// 被画成「没搜过」。
func (s *Shell) viewLyricCandidates(c *ui.Context, srcs []app.LyricSource, st app.LyricSearchState) {
	l := &s.lyricSearch
	t := c.Theme()
	id := lyricSourceID(srcs, st.Tab)

	// 状态行永远在。
	//
	// ⚠️ **它是「两个源分别标明」那句话的落点**：Tab 上那两个字扫一眼就过去了，
	// 而候选是**按源搜出来的**，用户必须能一眼分清「这批是哪个源给的」——他多半
	// 就是为了换个源才来搜的。失败时的那一句来自底下那一层（`lyricsErrText`），
	// 它已经点了名。
	ui.Text(c, lyricSourceLine(srcs, st)).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)

	switch {
	case st.Err != "":
		// 🔴 **失败不许静默**，也不许每帧重发（见文件头那三条）。
		ui.Column(c).Key(lyricSearchBodyKey).Grow(1).Center().Gap(space.Px10).Padding(space.Px24).
			Children(func() {
				// ⚠️ **不要再加一句前缀**：底下那一层给的已经是一整句了（它分得清
				// 「拉不到」和「这一条没有歌词」，而且**点了名是哪个源**）。外面再套
				// 一句「搜索失败：」就变成「搜索失败：网易云 搜不到：…」——同一个意思
				// 说两遍，而用户读第二遍的那几秒正是在等他点重试。
				ui.Text(c, st.Err).FontSize(scale.Font(c, scale.Size13)).TextColor(dangerText(t)).MaxLines(3)
				if ui.Button(c, lyricSearchRetryLabel).Clicked() {
					// ⚠️ **重搜 `st.Query` 而不是 `l.draft`**：`app` 失败时留着
					// `Query` 就是给这一下用的（`failLocked` 的注释）。拿框里此刻的
					// 字去搜的话，用户敲到一半按重试就是搜一个他还没输完的词。
					s.applyLyricsSearch(st.Query)
				}
			})

	case st.Loading:
		// 这一支**一个字都不画**：「正在搜」已经写在上面那一条状态行里了，
		// 这里再写一遍就是把同一句话说两次。
		ui.Column(c).Key(lyricSearchBodyKey).Grow(1)

	case !st.Searched:
		ui.Column(c).Key(lyricSearchBodyKey).Grow(1).Center().Padding(space.Px24).Children(func() {
			ui.Text(c, lyricSearchIdle).FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
		})

	case len(st.Candidates) == 0:
		ui.Column(c).Key(lyricSearchBodyKey).Grow(1).Center().Gap(space.Px6).Padding(space.Px24).
			Children(func() {
				ui.Text(c, lyricSearchEmpty).FontSize(scale.Font(c, scale.Size15)).TextColor(t.Text)
				ui.Text(c, lyricNoHitsHint).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			})

	default:
		// 🔴 铁律四：行身份是候选自己的 ID（`lyricCandidateRowKey`），不是下标。
		//
		// ⚠️ **身份是预先装箱好的那一份**（`l.rowKeys`）：理由见那个字段的注释。
		l.ls.Key = func(i int) any {
			return l.rowKeys.At(i, lyricRowIdent{src: id, cand: st.Candidates[i].ID}, lyricRowIdentity)
		}
		l.ls.Label = func(i int) string {
			return lyricCell(st.Candidates[i].Title)
		}
		e := ui.List(c, &l.ls, len(st.Candidates), func(i int) {
			s.viewLyricCandidateRow(c, i, id, st.Tab, st.Candidates[i])
		}).FillWidth().Height(lyricSearchListH)
		s.trackScrollbar(e)
	}
}

// lyricSourceLine 是候选区顶上那一条状态行：点名这一次搜的是哪个源，外加搜到了
// 几条。
//
// ⚠️ **它也是「正在搜」那句话唯一出现的地方**：内容区那一支在搜索中是空的，
// 同一句话说两遍的事这一层不写（理由见 `viewLyricCandidates` 顶上）。
//
// 🔴 **失败要单独一支，不许掉进「没搜到」那一支**：失败时 `Searched` 仍然是真、
// `Candidates` 已经被 `app.failLocked` 清空了，于是它俩长得和「真的一个候选都
// 没有」一模一样。那两件事要用户做的事完全相反——「没搜到」要他换个词或者换个源，
// 「拉不到」要他再试一次。状态行先说了「没搜到这一首」、下面又红字写着「网络不通」，
// 用户读到的是界面自己前后打架。
func lyricSourceLine(srcs []app.LyricSource, st app.LyricSearchState) string {
	name := lyricSourceLabel(srcs, st.Tab)
	if name == "" {
		return ""
	}
	switch {
	case st.Err != "":
		return "「" + name + "」· 结果拉不到"
	case !st.Searched && st.Query == "":
		return "「" + name + "」· 还没搜过"
	case st.Loading:
		return "「" + name + "」· 正在搜…"
	case len(st.Candidates) == 0:
		return "「" + name + "」· 没搜到这一首"
	default:
		return "「" + name + "」· 搜到 " + strconv.Itoa(len(st.Candidates)) + " 条"
	}
}

// lyricSourceLabel 是第 tab 个源**给用户看的那个字**（越界时给空串）。
func lyricSourceLabel(srcs []app.LyricSource, tab int) string {
	if tab < 0 || tab >= len(srcs) {
		return ""
	}
	return srcs[tab].Name
}

// viewLyricCandidateRow 是候选表里的一行。
//
// ⚠️ **点整行等于预览那一条**（上游 `netease-tab.tsx:71` 的 `onClick` 在
// `TableRow` 上，而那一行就是「点开看看完整歌词再决定用哪版」的入口）。
func (s *Shell) viewLyricCandidateRow(c *ui.Context, i int, srcID string, tab int, item lyrics.Candidate) {
	t := c.Theme()
	// ⚠️ **这一行的元素键是记好的那一份**（`l.rowKeys`）：现算的话每帧每行一次
	// 字符串拼接（票 #313）。它和上面那个 `ListState.Key` 是同一份身份。
	row := ui.Column(c).Key(s.lyricSearch.rowKeys.At(i,
		lyricRowIdent{src: srcID, cand: item.ID}, lyricRowIdentity)).Grow(1).
		Radius(t.Radius).Padding(space.Px6, space.Px8).Gap(space.Px2)
	if row.Hovered() {
		row = row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		ui.Row(c).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, lyricCell(item.Title)).Grow(1).FontSize(scale.Font(c, scale.Size13)).
				TextColor(t.Text).MaxLines(1).Ellipsis("…")
			ui.Text(c, lyricCell(item.Artist)).Width(lyricColArtist).FontSize(scale.Font(c, scale.Size12)).
				TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
			ui.Text(c, lyricDuration(item.DurationMS)).Width(lyricColDuration).FontSize(scale.Font(c, scale.Size12)).
				TextColor(t.TextMuted).MaxLines(1)
		})
		// 专辑单独一行（上游那一列在标题右边，我们把它放到下面——那一列最宽，
		// 放右边的话标题和歌手就都没地方了）。
		if a := strings.TrimSpace(item.Album); a != "" {
			ui.Text(c, a).FontSize(scale.Font(c, scale.Size11)).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
		}
	})
	if row.Clicked() {
		s.openLyricPreview(tab, item)
	}
}

// lyricCandidateRowKey 是候选表里一行的键（铁律四）。
//
// 🔴 **用候选自己的 ID，不下标**：下标在头里插进去一行之后整体错一位，于是后面
// 每一行的滚动位置和选中都串到**别的歌**上——而界面上每一个字都还是对的，用户
// 永远查不出来。
//
// ⚠️ **带上源 id**：两个源的 id 都是十进制数字（网易云是歌曲 id，LRCLib 是它的
// 数字 id），不加前缀的话理论上会撞；而 Tester 撞 key 是直接 panic 的。
func lyricCandidateRowKey(srcID, candID string) string {
	return lyricRowIdentity(lyricRowIdent{src: srcID, cand: candID}).(string)
}

// lyricRowIdent 是候选表一行的身份的原材。
//
// ⚠️ **可比较**：它要当「记着算好的那一份」那把钥匙（见 `rowkey.Memo` 的
// 文件头），所以里面不能有切片。
type lyricRowIdent struct {
	src  string
	cand string
}

// lyricRowIdentity 是候选表里一行的键（铁律四）。
//
// 这一步会分配（拼字符串 + 装箱）——所以它只在身份变了的时候才走
// （见 `rowkey.Memo` 的文件头）。
func lyricRowIdentity(k lyricRowIdent) any {
	return "lyrics:row:" + k.src + ":" + k.cand
}

// lyricCell 是一个字段的空值写法，抄上游两个 tab 里那个 `--`
// （`song.name || "--"`、`renderArtists` 的 `return "--"`）。
//
// ⚠️ 给「--」而不是把那一格空着：空着的话那一列少一个字，整张表就对不齐，而
// 「这一条源没给」是一个事实，不是一个空格。
func lyricCell(s string) string {
	if strings.TrimSpace(s) == "" {
		return "--"
	}
	return s
}

// lyricDuration 把候选的时长写成 `m:ss`。
//
// ⚠️ **零值给「--」而不是「0:00」**（抄上游 `netease-tab.tsx:26-30` 的
// `renderDuration`）：0 在这一层是「源没给」，写成 0:00 是在说「这首歌一秒都没有」。
func lyricDuration(ms int) string {
	if ms <= 0 {
		return "--"
	}
	return durText(time.Duration(ms) * time.Millisecond)
}

// ─────────────────────────────────────────────────────────────────────────────
// 预览
// ─────────────────────────────────────────────────────────────────────────────

// openLyricPreview 开预览。**立刻返回**：拉歌词那一趟在 `app` 的后台 goroutine 里。
//
// ⚠️ **预览的 Tab 每次都回「歌词原文」**（上游 `lyrics-preview-modal.tsx:27` 的
// `useEffect([isOpen, tlyrics])`）：连着看两条时留在「歌词翻译」上，那一栏里是
// 上一条的歌。
func (s *Shell) openLyricPreview(tab int, c lyrics.Candidate) {
	s.lyricSearch.previewTab = 0
	s.lyricSearch.previewScroll = ui.ScrollState{}
	s.app.OpenLyricPreview(tab, c)
}

// viewLyricPreviewDialog 画预览弹窗。
func (s *Shell) viewLyricPreviewDialog(c *ui.Context, pv app.LyricPreviewState) {
	l := &s.lyricSearch

	// 每一帧以应用层为准：那才是「预览开着」这件事的真相（采用和关闭都在那边
	// 收口，见 `app.AdoptLyricPreview`）。
	l.preview = pv.Open

	modal := ui.Modal(c, &l.preview, func() {
		ui.Column(c).Key("lyrics:preview").Width(lyricPreviewWidth).Gap(space.Px10).Children(func() {
			heading := pv.Title
			if strings.TrimSpace(heading) == "" {
				heading = lyricPreviewHeading
			}
			// ⚠️ **字重不写**（400）：弹窗标题是 400（DESIGN.md §二）。
			ui.Text(c, heading).FontSize(scale.Font(c, scale.Size14)).MaxLines(2).Ellipsis("…")
			// 「歌词原文 / 歌词翻译」两个 Tab（上游 `lyrics-preview-modal.tsx:53-56`）。
			// ⚠️ **译文那一个只在真的有译文时才有**（上游是 `{Boolean(tlyrics) &&
			// <Tab …/>}`）：没有译文的歌摆一个点开是空的 Tab，比不摆更糟。
			if strings.TrimSpace(pv.Translated) != "" {
				ui.Tabs(c, &l.previewTab, lyricPreviewOrigTab, lyricPreviewTransTab)
			}
			s.viewLyricPreviewBody(c, pv)
			s.viewLyricPreviewFooter(c, pv)
		})
	})

	if modal.Dismissed() {
		l.preview = false
		s.app.CloseLyricPreview()
	}
}

// viewLyricPreviewBody 画预览里那一段歌词。
//
// ⚠️ **三种「没有」是三句话**，混成一句用户就会去查错的方向（见
// `app.LyricPreviewState.Err` 那一条）：拉不到 / 这一条在源上没有歌词 /
// 正在拉。底下那一层已经把它们分开了，这里照实画，不加也不减。
func (s *Shell) viewLyricPreviewBody(c *ui.Context, pv app.LyricPreviewState) {
	l := &s.lyricSearch
	t := c.Theme()

	switch {
	case pv.Loading:
		ui.Text(c, lyricPreviewBusy).FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted).Padding(space.Px20)

	case pv.Err != "":
		// ⚠️ 「未找到歌词内容」和「网络不通」长得很像，可它们要用户做的事完全相反：
		// 前者要他换一条候选或者换个源，后者要他再试一次。这一句是底下那一层
		// `lyricsErrText` 拼的（它点名了是哪个源）。
		ui.Text(c, pv.Err).FontSize(scale.Font(c, scale.Size13)).TextColor(dangerText(t)).Padding(space.Px20).MaxLines(3)

	default:
		// ⚠️ **只给 Height、不给 Grow**：这一列是弹窗面板，而面板的高度是**由内容撑
		// 着的**（`ui.Modal` 自己只给 MaxHeight）。两个都给的话这一段会去抢面板的
		// 高度，而它抢到的那个高度和它自己的滚动位置对不上。
		preview := ui.Scroll(c).Key(lyricPreviewBodyKey).FillWidth().
			Height(lyricPreviewBodyH).TrackScroll(&l.previewScroll)
		preview.Children(func() {
			// ⚠️ **一行一句，不要把整份 LRC 塞进一个 `ui.Text`**：`ui.Text` 按
			// 拿到的那一框宽折行，不认 `\n`，于是整首歌词挤成一大坨、还带一堆
			// 看得见的时间戳。一句一个元素是唯一能把 LRC 的分行原样摆出来的
			// 画法。
			ui.Column(c).Gap(space.Px2).Children(func() {
				for _, line := range strings.Split(lyricPreviewText(pv, l.previewTab), "\n") {
					if strings.TrimSpace(line) == "" {
						// 空行不画：LRC 里每一句之间本来就有一个空行，全画出来
						// 就是两倍的行数。
						continue
					}
					ui.Text(c, strings.TrimRight(line, " \t")).FontSize(scale.Font(c, scale.Size13)).
						LineHeight(leading.Result).TextColor(t.Text)
				}
			})
		})
		s.trackScrollbar(preview)
	}
}

// lyricPreviewText 是这一帧要摆的那一份（原文还是译文）。
//
// ⚠️ **译文那一支还要看一眼 `previewTab`**：切到了「歌词翻译」而这一条**恰好**
// 没有译文时（Tab 是在上一个有译文的那一条上摆着的），摆原文才是对的——摆一个
// 空面板的话，用户读到的是「这歌没有翻译」。
func lyricPreviewText(pv app.LyricPreviewState, tab int) string {
	if tab != 0 && strings.TrimSpace(pv.Translated) != "" {
		return pv.Translated
	}
	return pv.LRC
}

// viewLyricPreviewFooter 画预览底下那两按钮。
func (s *Shell) viewLyricPreviewFooter(c *ui.Context, pv app.LyricPreviewState) {
	ui.Row(c).Gap(space.Px8).Justify(ui.End).Children(func() {
		if ui.Button(c, lyricPreviewClose).Clicked() {
			s.lyricSearch.preview = false
			s.app.CloseLyricPreview()
		}
		// ⚠️ **禁用的判据是底下那一层给的 `Adoptable`**（它把「拉失败 / 正在拉 /
		// 没有歌词」三种都算成不能采用）。禁着的按钮在界面上和「按了没反应」长得
		// 一样，所以它得画成禁用的，而不能让用户按下去等一个什么都不会发生的
		// 结果。
		if ui.PrimaryButton(c, lyricPreviewAdopt).
			Disabled(!pv.Adoptable).Clicked() {
			s.adoptLyricPreview()
		}
	})
}

// adoptLyricPreview 采用预览的那一份：写进当前这首歌的缓存，**两个弹窗一起关**。
//
// ⚠️ 返回的那句话非空就是**没成功**（歌换了 / 还在拉 / 这一条没有歌词），那一句
// 要给用户看——它是这一票里少数几个「按了有反应但不是他想要的那个反应」的情形。
func (s *Shell) adoptLyricPreview() {
	if msg := s.app.AdoptLyricPreview(); msg != "" {
		s.addToast(msg, notice.Danger)
		return
	}
	s.lyricSearch.preview = false
	s.lyricSearch.open = false
	s.lyricSearch.reset()
	s.addToast("已经换上这一版歌词", notice.Success)
}
