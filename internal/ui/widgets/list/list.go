// Package list 是所有内容页的底座：一个虚拟列表、音乐卡片，和它们的右键菜单。
//
// 首页、收藏夹、搜索结果、UP 空间、稍后再看全都建在它上面。所以这里最要紧的
// 不是画得好不好看，是下面两条约定：
//
//   - 🔴 铁律四（界面状态管理四条铁律的第 4 条）：**行的身份是 Music.ID，
//     不是序号**。MyGo 默认拿「父 ID + 兄弟序号」当身份，在前面插入一行会让
//     后面所有行的状态整体错配——实测：头部插入后，不带 key 的列表选中项会
//     变成旁边另一首。`ListState.Key` 就是为这件事存在的，本包每一帧都按 ID
//     装它（见 View），并且有一条测试钉住它。
//
//   - 🔴 铁律二（`SetTheme` 每帧在 view 开头调）：**本包一次都不调
//     `SetTheme`**。主题由页面在每一帧开头装好（`internal/ui.ApplyTheme`），
//     放到切换按钮回调里只调一次会被 MyGo 在帧开头静默重置掉。这里一律用
//     `c.Theme()` 读当前这一帧的那份。
//
// 虚拟滚动和变高行**不用自己写**：`ui.List` 只建看得见的那几行（两头各多两行，
// `mygo/ui/list.go` 的 listOverscan），行高按实际量出来、还没量到的按量过的
// 平均值估（同一处的 listHeights）。本包要做的只是**别把行高写死**——见
// card.go 里标题为什么折行而不是截断。
//
// ⚠️ 本包不认识 app.App 之外的世界，也不碰网络：封面走 imgcache，音乐条目是
// 调用方给的 Music，操作走 Actions（接在哪儿见 actions.go）。
//
// 包里有三层，一层比一层往上是「同一份数据、不同的摆法」：
//
//	list.go  虚拟列表，一行一首（虚拟滚动 + 变高行 + 行身份）
//	grid.go  网格，一行几首，列数按 Biu 的四个断点分档换
//	paged.go 翻页那一层：拉页、防重入、取消、容器没填满自动续拉、回顶按钮
//
// 列表和网格共用的那几件（查重、兜住画崩的一行、放队列、说一句话）在
// shared.go，只写一遍。
//
// ⚠️ `AppActions`、`BiliLink`、`NewPaged` 和 `NewGrid` 现在还没有调用方，这是
// 有意的：挂它们的是 PLAN.md 阶段 5 的那 14 个内容页，那一张做出来时直接用，
// 别再写一份接线。
package list

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
)

// Music 是列表里的一首歌：这一行要显示的，和操作要用的。
//
// 字段照 Biu 的 `src/components/music-list-item/index.tsx` 那一行取：
// 封面、标题、UP 名，外加列表里定位一首歌要用的标识。Biu 那行还有播放量和
// 投稿时间两列，这里没画——它们要 B 站接口的数据才有值（本地音乐没有这两个），
// 空着画一列是给用户看一片空白。等接了 B 站再加，别提前画。
type Music struct {
	// ID 是这一行的身份，**必须稳定且唯一**。
	//
	// 🔴 铁律四：填这一首歌自己的 ID（B 站的 bvid / sid、本地文件的完整
	// 路径），**不要填序号**。序号是「父 ID + 兄弟序号」那一套，在前面插一行
	// 会让后面所有行的状态整体错配。
	//
	// ID 变了就是另一首歌了（换了个来源），这是有意的：界面要保住的是
	// 「用户选中的那一首」，不是「第几行」。
	//
	// 重复的 ID 在 Tester 里当场 panic 并报出代码行号（`ui/diagnostics.go`），
	// 真窗口里则是一首歌两行的状态串在一起。宁可崩也不要静默串行。
	ID string

	// Title 是歌名。一行摆不下时折到第二行（Biu 的 `line-clamp-2`），
	// 这是本列表行高会变的唯一原因，见 card.go。
	Title string

	// Cover 是封面地址。**空串是「没有封面」，不是「加载失败」**——imgcache
	// 的 drawCover 里这是分开的两支。本地文件永远是空的：Biu 上游也不读音频
	// 里内嵌的图。
	Cover string

	// Owner 是 UP 名。空串时那一行不画它。
	Owner string

	// Duration 是时长，零值时那一列不画。本地文件要扫过才有值。
	Duration time.Duration

	// Item 是播放和右键菜单真正用的那一项。B 站的歌以后由接口填，
	// 现在先有本地文件（app.LocalItem）。
	Item app.Item
}

// List 是一个虚拟列表：几千行也只建看得见的那几行。
//
// **建一次，存进页面自己的状态里，每一帧调 View。不要每一帧新建一个**：
// `ui.ListState` 记着滚动位置和选中项，新建的等于每帧都跳回顶上（而且框架
// 会因为「同一个 ListState 被两个 List 显示」而 panic，`ui/list.go` 的
// buildList）。
type List struct {
	songs []Music
	state ui.ListState

	// sel 是选中的行，-1 是没选。帧里的点击由 ui.List 自己写进来。
	sel int

	// cache 是封面缓存。整个窗口共用一个，壳（Shell）建好之后传进来。
	cache *imgcache.Cache

	// acts 是这个列表会做的操作，见 actions.go。
	acts Actions

	// sel2 是「多选」那一摊接线，为零值时列表只有单选。
	//
	// ⚠️ 名字和 `sel`（选中的行号）差一个字是因为它们确实是两件事：
	// `sel` 是**光标**（按了哪一行、右键菜单弹在哪、播放从哪儿起），
	// `sel2` 是**勾选**（批量操作作用在哪些行）。Biu 的歌单页两个同时存在，
	// 拖动滚轮刷过几百行不会改勾选，点勾也不会改光标。
	sel2 Selection

	// footer 是末尾多出来的那一行的画法，为 nil 时没有这一行。
	//
	// 它是**哨兵行**：翻页的列表用它占住「下面还有」的位置，滚到它就拉下一页
	// （见 paged.go 的三重保障之二）。没有它，「滚到底」就得靠另外的办法去猜
	// 「内容是不是到底了」，而末尾那一行本身就是这个答案。
	//
	// 它不是歌，所以行身份是 footerKey：一个歌 ID 不可能撞上的类型。
	footer func(c *ui.Context)

	// scroll 是这个列表把滚动位置写回来的那个句柄，为 nil 时不写。
	scroll *ui.ScrollState

	// empty 是一行歌都没有时说的话（见 SetEmpty）。emptySet 为 false 是没调过
	// SetEmpty（用 defaultEmpty 那句）。
	empty    string
	emptySet bool

	// failed 是画崩了的行（按 ID 记），下一帧不重试。
	//
	// 为什么要记：MyGo 的事件循环没有 recover，行里一个 panic 会一路穿出去
	// 把整个进程带走（本包自己 recover 住，见 viewRow）。不记的话这一行
	// 每帧崩一次，日志被刷爆、帧率拖垮——比画不出来糟得多。
	failed map[string]bool
}

// New 建一个列表。
//
// cache 是封面缓存（整个窗口共用一个），acts 是操作。两者都要传：封面走
// imgcache.View，操作走右键菜单和双击，缺一个这个列表就只能看不能动。
//
// ⚠️ cache 不能是 nil：imgcache.View 一拿到非空的封面地址就在它上面取状态，
// nil 会让**带封面的每一行**画不出来（每行各 recover 一次并记一行日志）。
// 没有封面的行照画（imgcache 对空地址早返回），所以看到「一部分行挂了、一部分
// 好好的」就是这个原因。
func New(cache *imgcache.Cache, acts Actions) *List {
	return &List{cache: cache, acts: acts, sel: -1}
}

// SetSongs 换掉列表里的歌。
//
// 🔴 换完之后行身份跟着 ID 走：原来选中的那一首，选的还是它（见
// TestInsertingARowAtTheHeadKeepsTheChosenSong）。这里直接接管调用方给的
// 切片，不复制——调用方要换就整份换新，别在画着的时候改它。
//
// ⚠️ 只能由界面线程调（就是跑 view 的那个线程）：它改的正是 view 每帧读的
// 那份状态。
func (l *List) SetSongs(songs []Music) {
	l.songs = songs
	warnDuplicateIDs(songs)
}

// SetFooter 装末尾那一行的画法（通常是转圈 + 重试），传 nil 去掉。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (l *List) SetFooter(fn func(c *ui.Context)) { l.footer = fn }

// SetEmpty 换掉「没有歌时说的那句话」。
//
// **空串是不画**：一句都不说（列表就是空的）。不调它是用默认那句
// （defaultEmpty）。分页列表两种都要——第一页在途时说「正在加载…」，而
// 失败时页面上只留那一个重试按钮，不该再摆一句「这里还没有歌」（见
// paged.go 的 syncList）。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (l *List) SetEmpty(text string) { l.empty, l.emptySet = text, true }

// SetScroll 让这个列表把滚动位置写进 s（每帧写一次），调用方读它就知道滚到
// 哪儿了，也能靠改它把列表滚到某处。传 nil 就断开。
//
// MyGo 的 List 自己按行记位置（`ListState` 注释：「A List keeps its place by
// its rows instead」），但**滚动到哪儿**这个数它不往外说 —— 要判「滚过多少
// 像素」「是不是在顶上」就得靠它（见 paged.go 的回顶按钮与三重保障）。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (l *List) SetScroll(s *ui.ScrollState) { l.scroll = s }

// Selection 是「多选」那一摊接线。
//
// **零值就是没有多选**：勾选框一格都不画，列表回到只有单选的样子。
// 装了之后每一行最左边多一个勾选框，点它改的是 `Toggle` 指的那个下标。
//
// ⚠️ `On` 和 `Toggle` 都要判空用：`On` 为 nil 时画出来的永远是没勾上的框，
// 而 `Toggle` 为 nil 时点了没反应——按了没反应比不画更难解释。
type Selection struct {
	// On 报第 i 行此刻有没有被勾上。**每一帧每一行都会问一次**，所以它要便宜。
	On func(i int) bool

	// Toggle 是用户点了第 i 行的勾。框架已经把勾翻过来了，页面照自己的那
	// 份状态翻转回去就行。
	Toggle func(i int)
}

// SetSelection 装多选那一摊接线，传零值去掉。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (l *List) SetSelection(s Selection) { l.sel2 = s }

// Len 是这个列表有几行**歌**（末尾那一行哨兵不算）。
//
// 页面上「全选」那个判定要拿它和已加载的行数比，所以这个数得能从外面读到。
func (l *List) Len() int { return len(l.songs) }

// At 返回第 i 行那一首，没有这一行是 ok=false。
//
// 收的是**下标**而不是那一首：调用方要问的是「列表里现在有哪些歌」
// （全选、清空勾选、批量操作收集），问的是位置。
//
// ⚠️ 下标只在**同一份 songs** 里有意义。换了歌之后旧的下标指另一首（铁律四），
// 所以别把下标存起来隔几帧再用——要记就记 ID。
func (l *List) At(i int) (Music, bool) {
	if i < 0 || i >= len(l.songs) {
		return Music{}, false
	}
	return l.songs[i], true
}

// HasSelection 报这个列表有没有装多选接线。
func (l *List) HasSelection() bool { return l.sel2.On != nil }

// footerKey 是末尾那一行的身份。**不用字符串**：歌的 ID 是 bvid / 完整路径
// 之类的用户数据，理论上能撞上一个叫 "footer" 的；类型撞不上。
type footerKey struct{}

// footerLabel 是末尾那一行给无障碍和「打字选中」用的名字。固定一句话，不跟着
// 状态变（转圈 / 重试）——它在无障碍里念出来总比一句会变的话稳。
const footerLabel = "加载更多"

// defaultEmpty 是一行歌都没有时默认说的话。
const defaultEmpty = "这里还没有歌"

// footerFailed 是末尾那一行在 failed 里用的键。它和歌的 ID（用户数据，bvid 或
// 完整路径）不是一个来源，所以带一个前缀。
const footerFailed = "biu:footer"

// emptyText 是这一帧该说的那句空状态，空串是「什么都不说」。
func (l *List) emptyText() string {
	if !l.emptySet {
		return defaultEmpty
	}
	return l.empty
}

// rows 是这个列表有几行，含末尾那一行（没装 footer 就不算）。
//
// paged.go 的「末尾进了画面没有」问的是它，所以行数只有一个算法（就是这里）。
func (l *List) rows() int {
	n := len(l.songs)
	if l.footer != nil {
		n++
	}
	return n
}

// ScrollTo 把列表滚到第 row 行，align 是 ui.Start（顶上）、ui.Center（中间）
// 或 ui.End（底下）。
//
// 页面要它做两件事：换路由回来时回到原位，以及 PLAN 4.1 里那个「滚了 400 像素
// 才出现的回顶按钮」。
//
// ⚠️ 只能由界面线程调（就是跑 view 的那个线程），并且**要连着几帧**看效果：
// 列表是在布局时才滚过去的，而行高是量出来的，量完才知道那一条落在哪儿。
func (l *List) ScrollTo(row int, align ui.Align) { l.state.ScrollTo(row, align) }

// Selected 返回选中的那首，没有选中是 ok=false。
//
// 返回的是**歌**不是序号：序号在插一行之后就指另一首了，调用方拿着它去干什么
// 都是错的（这就是铁律四）。
func (l *List) Selected() (Music, bool) {
	if l.sel < 0 || l.sel >= len(l.songs) {
		return Music{}, false
	}
	return l.songs[l.sel], true
}

// View 画这个列表。页面在每一帧开头调一次。
//
// 列表自己占了 Grow(1)：**调用方要给它一块地方**，把它放进一个 Grow(1) 的
// Column（纵向页面）或一个 Grow(1) 配 FillHeight 的 Row（旁边还有东西的页面）里。
// 直接摆在根上不给高度的话，它会按自己的行数去要高度，几千行直接撑爆窗口。
func (l *List) View(c *ui.Context) {
	// 🔴 铁律四：这一行是整个包存在的理由。Key 每一帧都重装，装的是 ID。
	//
	// 为什么不装在 New 里：Key 是一个闭包，闭包捕获的是 l.songs 这一个切片
	// 变量而不是当时的内容，换歌之后它照样读得到新的——所以**装一次就够**，
	// 这里每帧重装是为了让「忘了装」这件事不可能发生：漏了的话
	// TestInsertingARowAtTheHeadKeepsTheChosenSong 会红。
	//
	// ⚠️ 别改成序号：那正是实测踩过的坑（头部插入后选中项变成旁边另一首）。
	//
	// 末尾那一行（footer）不是歌，给它一个歌不可能撞上的身份，别在这里越界。
	l.state.Key = func(i int) any {
		if i >= len(l.songs) {
			return footerKey{}
		}
		return l.songs[i].ID
	}
	// Label 是这一行给无障碍和「打字选中」用的名字。
	l.state.Label = func(i int) string {
		if i >= len(l.songs) {
			return footerLabel
		}
		return l.songs[i].Title
	}
	// Selected 让点击能选中一行（画成主色），双击提交。
	l.state.Selected = &l.sel

	list := ui.List(c, &l.state, l.rows()).
		Grow(1).
		FillWidth().
		// 双击这一行 = 播放它（Biu 的 `onDoubleClick`，不是单击：
		// `biu/src/components/music-list-item/index.tsx`）。
		OnSubmit(func() { l.playFrom(l.sel) }).
		// 没有歌时列表是空的，Children 里那一支就是它的空状态。
		// 一句都不说的时候连这一支都不建（见 SetEmpty）。
		Children(func() {
			if len(l.songs) == 0 {
				if s := l.emptyText(); s != "" {
					ui.Text(c, s).FontSize(13).
						TextColor(c.Theme().TextMuted).Padding(16)
				}
			}
		}).
		Rows(func(r ui.ListRow) { l.viewRow(r) })

	// 滚动位置写回调用方的句柄（见 SetScroll）。
	if l.scroll != nil {
		list.TrackScroll(l.scroll)
	}
}

// viewRow 画一行。
//
// 🔴 行里 panic 会一路穿出 Cocoa 事件循环把整个进程带走（框架的 Dispatch 和
// drain 都没有 recover）。所以每一行自己兜一层，**只废掉这一行**——兜法
// （含崩了不再重试的理由）在 shared.go 的 guardFailed 里，网格那一行共用它。
func (l *List) viewRow(r ui.ListRow) {
	c, i := r.Context, r.Index

	// 末尾那一行：翻页的哨兵 / 转圈 / 重试（见 paged.go）。它同样要兜住，
	// 只是不参与选中（Selected 越界时是 ok=false）。
	if i >= len(l.songs) {
		l.viewFooter(c)
		return
	}

	m := l.songs[i]
	if l.failed[m.ID] {
		l.failedRowText(c)
		return
	}

	guardFailed(&l.failed, "列表行", m.ID, func() { l.viewCard(r) })
}

// viewFooter 画末尾那一行，崩了也只废掉这一行（同一套兜法，同一张表）。
func (l *List) viewFooter(c *ui.Context) {
	if l.failed[footerFailed] {
		l.failedRowText(c)
		return
	}
	guardFailed(&l.failed, "列表末尾那一行", footerFailed, func() { l.footer(c) })
}

// failedRowText 是「这一行画不出来」那一行的占位。
func (l *List) failedRowText(c *ui.Context) {
	ui.Text(c, "这一行画不出来").FontSize(13).TextColor(c.Theme().TextMuted).
		Padding(cardPadY, cardPadX).MaxLines(1)
}

// playFrom 从第 i 首往后接进队列并开始播。
//
// 给的是**从这一首起的全部**，和 Biu 的「在 B 站打开这一首从这儿听」一致：
// 点了一首，接下来的队列就应该是它和它后面那些，不是整个歌单从头。
// 实现和网格共用（shared.go），两个视图的「播放」语义不可能对不上。
func (l *List) playFrom(i int) { playFrom(l.songs, l.acts, i) }

// indexText 是这一行前面那个序号。Biu 是 1 起的（`#` 列）。
func indexText(i int) string { return strconv.Itoa(i + 1) }

// formatDuration 是时长那一列的字。Biu 用 `tabular-nums`，所以宽度要定住，
// 不然 3:05 和 10:05 两列会左右跳。
//
// 超过一小时带小时（Biu 的 formatDuration 一样）：不写的话一小时的歌会显示成
// 「61:30」，用户得在心里换算一次才知道那是多长。
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := int(d.Round(time.Second).Seconds())
	if h := s / 3600; h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
