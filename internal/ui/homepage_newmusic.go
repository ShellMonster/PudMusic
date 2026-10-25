// 新歌速递：推荐页顶部那一块横向网格（P-02）。
//
// 抄的是 Biu 的 `src/pages/music-recommend/new-music-top.tsx`。
//
// # 它画在哪儿
//
// ⚠️ **它画在「流行」那一档列表的第 0 行**（`list.Lead`，页内头部）——所以它在
// 列表的滚动区里，跟着内容一起滚走（上游那一页的顶图和下面的列表在**同一个滚动
// 容器**里，`music-recommend/index.tsx:295`）。此前它是列表的**兄弟**，于是滚动
// 列表时它一动不动——票 #369 修的就是这个。
//
// ⚠️ **只有「流行」那一档有它**（上游 `index.tsx:295`），另外两档上没有。
//
// ## 它和分页网格不是同一张表
//
// 「新歌速递」的列数断点是 **640 / 768 / 1024 / 1280 → 3 / 4 / 5 / 6**（更窄
// 那一档 2），和 `widgets/list` 里那个分页网格的 **1536 / 1280 / 1024 / 768 →
// 6 / 5 / 4 / 3**（更窄 2）是**两张不同的表**。Biu 上游这两处各写各的：
// `new-music-top.tsx:135-141` 是这一张，`virtual-grid-page-list/index.tsx:57-64`
// 是那一张。抄错一张，用户在同一个窗口宽度下看到的列数就和 Biu 不一致。
//
// ⚠️ **量的是窗口宽（上游 `window.innerWidth`），不是容器宽**：上游的**网格列数**
// 是 Tailwind 的**视口**断点（`new-music-top.tsx:209` 的
// `sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6`，那四个断点就是
// 640/768/1024/1280，和这张表一样），而**页大小**（`colCount * 2`）量的是同一个
// 数（`:135` 的 `window.innerWidth`）——**两边都是窗口宽**。
//
// 我们此前量的是**容器**的宽：侧栏把内容区窄掉一截，于是卡片比上游**大**一档
// （1200 宽时格子 228 而不是 180），**一页只摆得下 4 张**、这一块也更高，把下面
// 那一档的列表整个挤出视口——票 #475 说的「只有 1 行、这一行只有 4 个」就是它。
// 改回窗口宽之后，卡片占**容器**的比例和上游一样（侧栏那一份宽度就是窄掉的那么
// 一块），而一页摆几首也和上游一样。
//
// ## 一页摆多少
//
// 上游 `pageSize = colCount * 2`：一页两行，左右两个箭头翻页（`new-music-top.tsx:157`
// 与 `:182-199`）。**上游永远是两行**——那个文件只量 `window.innerWidth`，**从不量
// 高度**——而整页是一个滚动容器（`music-recommend/index.tsx:261`），所以矮窗口下
// 这一块照样占两行，下面的列表被推到更下面（**滚得到，不是被挤没**）。
//
// ⚠️ **我们此前多了一条「窗口矮就减成一行」的降级**：那不是上游的，是我们的。
// 它就是「只有 1 行、这一行只有 4 个」的来源，而它同时把下面那一档的列表挤没了。
// 现在这一块**永远两行**，封面**永远是格子的宽**（上游 `aspect-square w-full`）。
//
// ## 🔴 行身份
//
// 卡片是动态的一批元素，**每一张按它那一首的 ID 给 Key**（`viewCard` 里
// `.Key(m.ID)`），不是按它在第几格——默认的身份是「父 ID + 兄弟序号」，头上
// 多一首歌会让后面所有卡片的焦点、选中这类状态整体错配（铁律四的第 4 条）。
// `TestInsertingAtTheHeadKeepsTheFocusOnTheSameSong` 钉的就是这一条。
package ui

import (
	"log"
	"runtime/debug"

	"github.com/ShellMonster/Biu_Go/internal/ui/guard"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrollbar"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// newMusicRows 是**一页摆几行**。上游是列数乘 2（`new-music-top.tsx:157`）。
	//
	// ⚠️ **它不再跟着窗口高变**：上游那个文件**只量 `window.innerWidth`、从不量
	// 高度**，所以它永远是两行——矮窗口下这一块比视口高，下面那一档的列表被推到
	// 更下面（**滚得到**），不是被挤没。
	//
	// 我们此前有一条「窗口矮就减成一行」的降级（那是我们自己加的，上游没有），
	// 它就是票 #475 说的「只有 1 行、这一行只有 4 个」的来源。**去掉它。**
	newMusicRows = 2

	// newMusicHeadH 是「新歌速递」那一行标题占的高。24 号字的行高约 29，取 32
	// 留一点余量。
	newMusicHeadH = 32

	// newMusicCardTextH 是封面下面那两行字占的高：歌名 16（行高 ~20）、UP 名
	// 14（行高 ~18）、两道 6 的缝。取 52 留一点余量。
	newMusicCardTextH = 52

	// newMusicGap 是格子之间的间距。上游是 `gap-3`（12 像素）。
	newMusicGap = space.Px12

	// newMusicEmptyH 是「暂无数据」那一块占多高。上游是 `h-[200px]`。
	newMusicEmptyH = 200

	// newMusicPageW 是「几 / 几」那个读数定住宽度用的。上游是 `text-sm`，宽度随
	// 内容走，两位数翻成三位数时后面的按钮会左右跳一格——翻页时最不该跳的就是它。
	newMusicPageW = 52

	// newMusicArrowW 是左右两个翻页键的点击区边长：32×32。
	//
	// 图标本身还是 16（上游那两枚 `RiArrowLeftSLine` / `RiArrowRightSLine` 的
	// size 是 16，`new-music-top.tsx:188` 与 `:197`），32 是**点击区**（票 #170 的
	// 数）。原来它是 84（两个汉字加内边距），图标化之后 84×84 的一枚图标键在这个页头上
	// 会是一块方砖，比旁边那个读数还大。
	newMusicArrowW = 32
)

// newMusicColsFor 是**窗口**有 w 那么宽时新歌速递摆几列。
//
// ⚠️ **量的是窗口宽**：上游的网格列数是 Tailwind 的视口断点、页大小量的是
// `window.innerWidth`（`new-music-top.tsx:135`），**两边都是窗口宽**。见文件头
// 「它和分页网格不是同一张表」那一段。
//
// ⚠️ 这张表和 `widgets/list` 里那张**不是同一张**，抄的时候别抄串了（见文件头）。
var newMusicColsFor = func(w float32) int {
	switch {
	case w >= 1280:
		return 6
	case w >= 1024:
		return 5
	case w >= 768:
		return 4
	case w >= 640:
		return 3
	default:
		return 2 // 比 640 窄（上游 `:140` 的 `return 2`）
	}
}

// newMusicCellWidth 是一格有多宽：容器减去列间的缝，平分。
//
// 和 CSS grid 的 `repeat(n, minmax(0, 1fr))` 同一个算法（`ui.Grid` 的 `Columns`
// 就是这么分的），所以算出来的数正好是这一格实际拿到的宽，封面按它画不会差那
// 半像素。
func newMusicCellWidth(w float32, cols int) float32 {
	if w <= 0 || cols <= 1 {
		return w
	}
	return (w - float32(cols-1)*newMusicGap) / float32(cols)
}

// newMusicPageSize 是**一页摆几首**：列数乘行数。
//
// 行数是定死的两行（上游），所以「一共几页」只跟着**列数**变——而列数是按**窗口
// 宽**分的档（见 newMusicColsFor）。翻页的箭头和「几 / 几」那个读数算的都是它，
// 页码别的地方也照它（见 clampPage）。
func newMusicPageSize(cols, rows int) int {
	if cols < 1 || rows < 1 {
		return 0
	}
	return cols * rows
}

// newMusicGrid 是新歌速递那一块：两行，左右翻页。
//
// **建一次，存进首页的状态里，每一帧调 View**。同 widgets/list 的 List：新建一个
// 等于每帧回到第一页（这里就是每帧都回到第一页，翻页翻了个寂寞）。
type newMusicGrid struct {
	// songs 是这一块要画的那一摊歌。
	songs []list.Music

	// cache 是封面缓存。整个窗口共用一个，壳建好之后传进来。
	//
	// ⚠️ 不能是 nil：`imgcache.View` 一拿到非空的封面地址就在它上面取状态，nil
	// 会让**每一格都画不出来**（理由和 list.New 的 cache 参数一样）。
	cache *imgcache.Cache

	// acts 是点一下那一格会做的事（放队列）。见 actions.go。
	acts list.Actions

	// ownerTip 记着每一格那句「打开 X 的空间」（票 #360）。
	//
	// ⚠ **理由和 `widgets/list` 那几个一模一样**：它是「每帧每格算一遍、结果
	// 不变」的那一种（现算的话每帧每格一次字符串拼接，见 `rowkey.Memo`）。
	ownerTip rowkey.Memo[string, string]

	// draw 是包了 recover 的画法，见 View。建的时候包一次就够，每帧包一次是
	// 白花开闭包。
	draw func(*ui.Context)

	// ── 下面这一摊只由界面线程读写（铁律一）────────────────────────────

	// page 是现在第几页，从 1 起。0 是还没画过一帧。
	page int

	// width 是这一块**上一帧**量出来的**容器**宽，用来算格子宽（见 view 里那两句）。
	//
	// ⚠️ **它不是分列的判据**（分列看的是窗口宽，见 newMusicColsFor）：格子宽是
	// 「容器宽按列数平分」，而列数是按窗口宽定的。
	width float32

	// coverSide 是这一帧封面那个正方形的边长（= 格子宽，见 view）。
	//
	// ⚠️ **它是估的**（见 `Height`）：回顶按钮判断「滚过多少像素」靠它算这一块的
	// 高，而它跟着窗口的宽走。
	coverSide float32

	// cols 是**上一帧**摆了几列，用来在换歌的时候夹页码（见 clampPage）。0 是还没
	// 画过一帧。
	//
	// ⚠️ 只有它被记着：窗口宽每一帧现读 `c.Size()`，不存（见 view 里那三段）。
	cols int

	// ── 下面这一摊是取数状态（homepage_feed.go 递进来的）──────────────

	// loading 是那一摊正在拉。画一个转圈，**不画「暂无数据」**——那是
	// 「B 站真的没有」的说法，而正在拉的时候说它就是在骗用户。
	loading bool
	// err 是上一次失败的原因（已经翻成人话，见 homeFailureOf）。空 = 没失败。
	err error
	// retry 是「重试」按钮按下去要干的活。首页给的闭包（`homePage.retryNew`）。
	// nil 时那一块画不出重试按钮。
	retry func()

	// tip 是带延迟的悬停提示（tooltip.go），由 Shell 在首页建好之后接上。
	// 为零时（单独造这一块的测试）退回框架那枚。
	tip tipFunc
}

// newNewMusic 建新歌速递那一块。
//
// cache 和 acts 的意思同 list.New。wake 是「要一帧新画面」的动作，崩了靠它
// 要下一帧（recover 接住的是 build pass 中间，那一帧剩下的部分没建，画出来是
// 半张）。
func newNewMusic(cache *imgcache.Cache, acts list.Actions, wake func()) *newMusicGrid {
	g := &newMusicGrid{cache: cache, acts: acts}
	g.draw = guard.Wrap(g.view, func(rec any) {
		log.Printf("biu: 新歌速递画崩了：%v\n%s", rec, debug.Stack())
		if wake != nil {
			wake()
		}
	})
	return g
}

// tipWith 是这一块的悬停提示入口：状态挂在 Shell 上（tooltip.go），这里只是
// 把首页拿到的那份递过去；单独造这一块的测试没有那份，退回框架那枚。
func (g *newMusicGrid) tipWith(c *ui.Context, e ui.Element, text string) ui.Element {
	return tipFrom(g.tip, c, e, text, tipDefault)
}

// SetSongs 换掉新歌速递那一摊歌，页码夹回还存在的范围内。
//
// 夹页码是有原因的：换歌之前停在第 5 页，换完之后只剩一页，那一页的第 5 页就
// 是**一片空白**（上游也是这么处理的，`new-music-top.tsx:160-165` 的那个
// effect 干的同一件事）。留着不动的话用户看到的是一个空的网格加两个转不动的
// 箭头。
//
// ⚠️ 只能由界面线程调，同 homePage.SetSongs。
func (g *newMusicGrid) SetSongs(songs []list.Music) {
	g.songs = songs
	g.clampPage()
}

// SetStatus 递进那一摊的取数状态。首页每一帧画到这一块之前调一次。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (g *newMusicGrid) SetStatus(loading bool, err error) {
	g.loading, g.err = loading, err
}

// SetRetry 装「重试」按钮按下去要干的活。首页在 newHomePage 里装一次。
//
// ⚠️ **这一块自己不管取数**（它只管画），所以重试不是它发请求，是它回调
// 首页那一摊——两边的状态合成一处，只有首页知道。
func (g *newMusicGrid) SetRetry(fn func()) { g.retry = fn }

// clampPage 把页码夹进 [1, 总页数]。总页数为 0（一首都没有）时是 1。
func (g *newMusicGrid) clampPage() {
	total := g.totalPages()
	if g.page > total {
		g.page = total
	}
	if g.page < 1 {
		g.page = 1
	}
}

// totalPages 一共有几页。
//
// 每页 size 首（size 是**上一帧**的列数乘两行），一首都没有时是 0 页而不是 1 页：
// 空着的时候画的是「暂无数据」那一块，画一个 1 / 0 的读数比不画更让人以为界面坏了。
func (g *newMusicGrid) totalPages() int {
	// 还没画过一帧时 cols 是 0，按最窄的那个（两列）算：夹页码只需要一个够用的
	// 分母，而画第一帧时 view 里会拿真的列数重算一遍。
	cols := g.cols
	if cols <= 0 {
		cols = 2
	}
	size := newMusicPageSize(cols, newMusicRows)
	if len(g.songs) == 0 || size <= 0 {
		return 0
	}
	return (len(g.songs) + size - 1) / size
}

// View 画新歌速递这一块。首页在每一帧画到它时调一次。
//
// 🔴 外面包着 recover（`guard.Wrap`，建的时候包一次，见 newNewMusic）：view 里
// panic 会一路穿出 Cocoa 事件循环把整个进程带走。**这一块自己这一道必须有**，
// 包一层是这里最便宜的保险——页级那道（router.go 的 `Shell.page`）接得住整页，
// 但换上去的是**整张窗口**的错误页，为首页顶上那一块换掉整个窗口不划算。
//
// ⚠️ 崩了不换错误页：坏的是这一个控件，页面上别的东西照常用。这和 widgets/list
// 里那个「几千行里坏一行」是同一个取舍。
func (g *newMusicGrid) View(c *ui.Context) { g.draw(c) }

// view 是真的画法，View 是它外面那道 recover。
func (g *newMusicGrid) view(c *ui.Context) {
	t := c.Theme()

	// 容器宽。头一帧还没量出来（`Bounds` 读的是上一帧的盒子），先拿窗口的宽
	// 顶着——那一帧的格子宽是**估的**（窗口比容器宽，估出来偏大），下一帧
	// 量到真的就换过去了。反过来（第一帧量到 0 就当最窄）的话会先闪一下两列，
	// 宁可宽一帧，也不要窄一帧。理由和 widgets/list 的 Grid.View 里那几句一样。
	//
	// ⚠️ **列数按窗口宽分档，格子宽按容器宽分**（上游就是这么两样分的：列数是
	// Tailwind 的视口断点，格子是容器里 `repeat(n, minmax(0, 1fr))` 平分的）。
	//
	// ⚠️ **窗口宽每一帧都现读，不记**（`g.windowW` 那个字段是踩过坑才删的）：记
	// 下来之后用户改窗口宽度就不生效了——实测把 1200 记在字段里、把窗口拖到 900，
	// 这一块还是五列，而上游在 900 宽上是四列。`c.Size()` 读的是两个字段，读它不
	// 花钱。
	ww, _ := c.Size()
	w := g.width
	if w <= 0 {
		w = ww
	}
	cols := newMusicColsFor(ww)
	cell := newMusicCellWidth(w, cols)
	coverSide := cell
	g.width, g.cols = w, cols
	g.coverSide = coverSide

	g.clampPage()
	total := g.totalPages()
	size := newMusicPageSize(cols, newMusicRows)

	ui.Column(c).Key("new-music").FillWidth().Gap(space.Px12).Children(func() {
		g.viewHeader(c, total)

		// ⚠️ **三态分开画**（理由同 `viewTabBody`）：正在拉 / 拉不到 / 真的
		// 没有，是三件不同的事。合成一句「暂无数据」的话，用户在风控、断网、
		// 登录态过期这三样上读到的都是「B 站没有新歌」。
		switch {
		case g.err != nil && len(g.songs) == 0:
			// 高度照「暂无数据」那一支（`new-music-top.tsx:212` 的 h-[200px]）：
			// 失败之后网格整个换掉，这一块的高度跟着换会让下面那个列表跳一格。
			//
			// ⚠️ **底色是页面底色 + 一圈影**（`shadow.HeroSmall`），不是「比背景
			// 亮一级」的板（票 #239，审计 F-14）：DESIGN.md §四「卡片与背景几乎同色，
			// 靠阴影分层」。这一块原来取 `t.Surface`——同一屏里于是既有「同色靠影」
			// 的卡（关注卡、下载表那张），又有「亮一级」的板。
			shadow.HeroSmall(ui.Column(c).FillWidth().Height(newMusicEmptyH).Radius(t.Radius).
				Background(t.Background).Center().Gap(space.Px8), t).Children(func() {
				ui.Text(c, "新歌速递没拉出来").FontSize(scale.Font(c, scale.Size13)).TextColor(t.Text)
				ui.Text(c, g.err.Error()).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(2)
				if g.retry != nil && flatButton(c, "重试加载新歌速递").Clicked() {
					g.retry()
				}
			})
			return

		case g.loading && len(g.songs) == 0:
			// 上游 `new-music-top.tsx:203-205`：拉的时候就是一个转圈。
			shadow.HeroSmall(ui.Column(c).FillWidth().Height(newMusicEmptyH).Radius(t.Radius).
				Background(t.Background).Center(), t).Children(func() {
				ui.Spinner(c).Label("正在加载新歌速递")
			})
			return

		case total == 0:
			// 上游 `new-music-top.tsx:211-214`：整块宽的一张卡片，正中间一句
			// 「暂无数据」。这里照它（那一句是上游的文案，别自己编）。
			shadow.HeroSmall(ui.Column(c).FillWidth().Height(newMusicEmptyH).Radius(t.Radius).
				Background(t.Background).Center(), t).Children(func() {
				ui.Text(c, "暂无数据").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
			})
			return
		}

		grid := ui.Grid(c).FillWidth().Columns(cols).Gap(newMusicGap).Children(func() {
			for i := range size {
				n := (g.page-1)*size + i
				if n >= len(g.songs) {
					// 最后一页不满：剩下的格子空着（CSS grid 也是这么摆的，
					// 空格子不占位也不画边框）。
					return
				}
				g.viewCard(c, n, coverSide)
			}
		})
		// 量这一帧的容器宽，下一帧用它分列。`Bounds` 读的是**上一帧**量好的盒子，
		// 所以窗口改宽之后列数晚一帧换——和 Biu 那边 ResizeObserver 改 state 再
		// 重画是同一个节奏。
		if bw := grid.Bounds().W; bw > 0 {
			g.width = bw
		}
	})
}

// viewHeader 画那一块的头：左边标题，右边翻页（上游 `new-music-top.tsx:175-201`）。
//
// ⚠️ 一首都没有时**整个翻页那一块都不画**（上游那时候画的是「1 / 0」）：空网格
// 上摆一个 1 / 0 的读数和两个转不动的箭头，用户只会以为界面坏了。
//
// ⚠️ **右边让出列表滚动条那一条**（票 #369 的评审）：这一块在列表的**滚动区**里
// （它是「流行」那一档列表的第 0 行），而列表的浮动滚动条就贴在它的右边——指针
// 一悬停滚动条就出现，**按下被滚动条抢走**（框架的 `pointerDown` 先问
// `scrollbarPress`），于是那两枚翻页箭头按了没反应，用户翻不到第三页。
//
// 让出的宽度是滚动条那条**轨道**的宽（滚动条本身 + 框架在它左边留的 6 像素，见
// `widgets/scrollbar` 的 `HotZone`）——箭头整个落在轨道外面才点得动。
//
// ⚠️ **只有这一行让**：卡片那一块不动（它和下面那些歌一样齐着右边，让了的话整块
// 网格比下面那几行窄一截 ✗ ——那比点不动的箭头更显眼）。卡片最右边那一条封面被
// 滚动条盖住，和下面那些歌行尾的「⋯」一样（那是列表一直就有的那件事）。
func (g *newMusicGrid) viewHeader(c *ui.Context, total int) {
	ui.Row(c).Key("new-music:head").FillWidth().Gap(space.Px12).AlignItems(ui.Center).
		// ⚠️ **是 Padding 不是 Margin**：Margin 在 flex 里是**盒子外面**的那一圈，
		// 它不缩内容区（内容区 = 盒子减去 Padding）——用 Margin 的话翻页那一块
		// 还是贴在最右边，一样被滚动条压着。
		Padding(space.None, scrollbar.HotZone, space.None, space.None).Children(func() {
		ui.Text(c, "新歌速递").FontSize(scale.Font(c, scale.NewMusicTitle))

		if total == 0 {
			return
		}

		ui.Row(c).Grow(1).Gap(space.Px8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			// ⚠️ 翻页不重置网格那一块的横向位置——它本来就没有横向滚动，
			// 页码就是它唯一的「位置」。
			// 上游那两枚是 `variant="flat"` 的 `RiArrowLeftSLine` / `RiArrowRightSLine`
			// 16（`new-music-top.tsx:188` 与 `:197`）。图标键 + 悬停说出它做什么。
			//
			// ⚠️ **走 `iconKey`（悬停时图形变主色）**，不是 `iconButton`（票 #249）：
			// 翻页是这一块唯一的交互，而原来那一档只有 8% 的淡底、图形不换色——
			// 用户最需要在这里知道「还有下一页」的时候看不出来。
			//
			// 🔴 **禁用的状态走 `iconKey` 的 `disabled` 那一栏**（不能靠链上的
			// `.Disabled(...)`）：悬停色是**建元素的时候**问的（`Hovered()`），而链式
			// 调用是返回之后才把禁用设上去——问早了，一枚**禁用**的键会用主色告诉
			// 用户「这里点得动」（票 #217 的评审查出来过同款的一条）。
			prev := g.tipWith(c, iconKey(c, iconSideCollapse, "上一页", 16, g.page <= 1).
				Size(newMusicArrowW, newMusicArrowW).Padding(space.None, space.Px8),
				"上一页")
			if prev.Clicked() {
				g.page--
			}
			ui.Textf(c, "%d / %d", g.page, total).FontSize(scale.Font(c, scale.Size13)).Width(newMusicPageW).
				TextAlign(ui.Center).TextColor(c.Theme().TextMuted)
			next := g.tipWith(c, iconKey(c, iconSideExpand, "下一页", 16, g.page >= total).
				Size(newMusicArrowW, newMusicArrowW).Padding(space.None, space.Px8),
				"下一页")
			if next.Clicked() {
				g.page++
			}
		})
	})
}

// viewCard 画一格：封面 + 歌名 + UP 名，外加点一下就放队列。
//
// 结构照上游 `new-music-top.tsx:216-259` 那个 `role="button" tabIndex={0}` 的
// 卡片：封面**占满一格的宽、宽高相等**（上游 `:225` 的 `aspect-square w-full`——
// **永远是格子的宽**，不因为窗口矮就压小：上游那个文件只量宽度、不量高度），
// 下面 `mt-2` 是歌名，再下面一行小字（上游那一行是 `作者 · 日期`，我们这一块的
// `list.Music` 没有日期字段，所以只有 UP 名）。
//
// ⚠️ 歌名和 UP 名都是 `MaxLines(1)`（上游两行都是 `line-clamp-1`），所以这一块
// **每一格都一样高**：卡片的高就是「封面 + 两行字 + 两道缝」。widgets/list 的
// 列表行现在也是**定死行高**的（`card.go` 的文件头），而这一块是它自己的一档
// ——别顺手把 MaxLines 去掉，那一格会比旁边的高，整块网格的边就对不齐了。
func (g *newMusicGrid) viewCard(c *ui.Context, i int, coverSide float32) {
	m := g.songs[i]
	t := c.Theme()

	// 🔴 铁律四：**按 ID 给身份，不是按第几格**。默认的身份是「父 ID + 兄弟
	// 序号」，新歌速递换歌时新的一首会插在头上（上游按发布时间排），那样后面
	// 所有卡片的焦点、这类状态就整体错配了——用户 Tab 到第三首，刷新一下，焦点
	// 跑到别的那首上去了。
	card := ui.Column(c).Key(m.ID).AlignItems(ui.Start).Gap(space.Px6).
		// 上游那张卡片是 `role="button" tabIndex={0}`：能点也能 Tab 到。焦点
		// 落在它身上时它才认得出「用户现在盯的是哪一首」，而这也正是铁律四那一
		// 条要保住的东西有一个能看见的地方（`Focused` + 头插一首，见上面那条
		// 测试）。Label 是给读屏软件用的名字，也是测试唯一认得出它的东西。
		Focusable().Role(ui.RoleButton).Label(m.Title).
		// 点一下放这一首。**单击就播**（和网格/列表里「单击选中、双击播放」不
		// 同）：上游这一块就是单击 `handlePlay`，桌面端在这里跟着它。
		OnClick(func() { g.play(i) })

	// ⚠️ **hover 要在 `Children` 之前问**（同 widgets/list 的 grid.go）：它报的是
	// 这一帧指针在不在这一格上，而封面上面那圈反馈要等 Children 才建得出来。
	onCard := card.Hovered()

	card.Children(func() {
		// ① 封面。边长是 `coverSide`（= 格子的宽，见 view），
		// 上面那一圈 hover 反馈是**和网格卡片同一套**：封面底部一条 black/90
		// 渐变、右下角一个主色圆播放钮、200ms 淡入。四态由 imgcache.View 画：
		// 空地址画空框，不画「失败」。
		//
		// ⚠️ 播放钮按下去是 `g.play(i)`（追加这一首并立即播），不是网格卡片那枚「从这一
		// 首往后接」——所以走 `CardCoverOne`，它的标签和提示说的也是这一件事。
		hover.CardCoverOne(c, g.cache, m.Cover, coverSide, coverSide, onCard, m.Title,
			func() { g.play(i) })

		// ② 歌名 + UP 名。上游两行都是 `line-clamp-1`，这里照它。字号走 DESIGN.md
		// 的网格那一档：歌名 16、UP 名 14。
		//
		// ⚠️ **显示的字是清洗过的**（`list.ShowOf`，规则见 `widgets/title`）：B 站
		// 视频标题里全是「【4K修复】」「官方MV」这种视频的痕迹，而这里是音乐 App
		// （规范 §0）。副标题那一行带上从方括号里挪出来的歌手 / 专辑名，现场版
		// 带一枚「现场」小标签——**和下面那一档的列表是同一套**（同一个出口，
		// 见 `widgets/list` 的 `Shown`）。
		//
		// ⚠️ **正在放的那一首、以及指针压着的那一格，都把歌名染成主色**：这一块
		// 和上面三个 Tab 是同一页，判据也是同一份（`Actions.IsPlaying`，按
		// `app.Item.Identity`）。同一屏上「正在播放」在两个地方说两套话，用户只能
		// 靠猜。hover 变主色是上游那张卡片的 `group-hover:text-primary`（票 #170）。
		sh := list.ShowOf(m)
		titleColor := t.Text
		if onCard || (g.acts.IsPlaying != nil && g.acts.IsPlaying(m.Item)) {
			// 主色当**字**：浅色下换成与底色 ≥4.5:1 的变体（理由见
			// `widgets/ink`）。这一处和列表 / 网格那三档说的是同一件事，
			// 颜色也必须走同一个出口——漏了这里，同一屏上「正在播放」
			// 在下面那一排是深绿、在这一块是读不出来的亮绿。
			titleColor = ink.AccentOn(t)
		}
		ui.Text(c, sh.Title).FontSize(scale.Font(c, scale.Size16)).TextColor(titleColor).MaxLines(1).Ellipsis("…")
		if sh.Owner != "" || sh.Live {
			// ⚠ **副标题那一行和列表里是同一套**（`list.OwnerLine`）：现场标签（有就
			// 画）+ UP 名——有编号时那一枚**可点**，点了进他的空间（票 #360），没有
			// 时照旧是普通字（名字照样显示，只是点不动）。
			//
			// ⚠ **提示是记好的那一份**（`g.ownerTip`）：现算的话每帧每一格一次字符串
			// 拼接（票 #313 那一类）。
			//
			// ⚠ **这一块原来没有设行高**：所以 `lh` 传 0（`list.OwnerLine` 的
			// 约定是「不给就不设」，而 `LineHeight(0)` 是真的把行高设成 0）。
			list.OwnerLine(c, sh, m,
				g.ownerTip.At(i, m.Owner, list.OwnerTipText),
				scale.Font(c, scale.Size14), 0, t.TextMuted, g.acts)
		}
	})
}

// Height 是这一块有多高（像素）。
//
// ⚠️ **它是估的**：卡片的文字那几行是量出来的，而这里算的是「封面 + 文字那一块」
// 的估高（`newMusicCardTextH`）。差几个像素的后果只是那枚「回到顶部」早一点或
// 晚一点出现——而它本来就是按 400 像素那个门槛估的（`list.List.ScrollOffset`）。
//
// 用处只有一个：这一块是「流行」那一档列表的**页内头部**（列表的第 0 行），
// 而回顶按钮判断「滚过 400 像素没有」要把这一块的高度算进去（它也是滚过去的
// 内容的一部分）。
func (g *newMusicGrid) Height() float32 {
	// 空状态 / 正在拉 / 拉不到：那一块是定高的那一格（`newMusicEmptyH`）。
	if g.err != nil || g.loading || g.totalPages() == 0 {
		return newMusicHeadH + newMusicGap + newMusicEmptyH
	}
	// 标题 + 一道缝 + 两行卡片（每行：封面 + 文字）+ 行间的缝。
	return newMusicHeadH + newMusicGap +
		float32(newMusicRows)*(g.coverSide+newMusicCardTextH) + float32(newMusicRows-1)*newMusicGap
}

// play 把第 i 首追加到队尾并立即播放，保留用户原队列（票 #337）。
func (g *newMusicGrid) play(i int) {
	if i < 0 || i >= len(g.songs) || g.acts.PlayNow == nil {
		return
	}
	g.acts.PlayNow(g.songs[i].Item)
}
