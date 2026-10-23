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
// 虚拟滚动**不用自己写**：`ui.List` 只建看得见的那几行（两头各多两行，
// `mygo/ui/list.go` 的 listOverscan）。行高它也能量（量出来的高度会用来校正它
// 对未量到的行的估计，同一处的 listHeights）——但**列表这一档的行高是定死的**
// （64，见 `card.go` 的文件头），走量出来那条路的是**网格那一档**（歌名两行，
// 格子的高跟着内容走）。
//
// ⚠️ 本包不认识 app.App 之外的世界，也不碰网络：封面走 imgcache，音乐条目是
// 调用方给的 Music，操作走 Actions（接在哪儿见 actions.go）。
//
// 包里有三层，一层比一层往上是「同一份数据、不同的摆法」：
//
//	list.go  虚拟列表，一行一首（虚拟滚动 + 定高的行 + 行身份）
//	grid.go  网格，一行几首，列数按 Biu 的四个断点分档换
//	paged.go 翻页那一层：拉页、防重入、取消、容器没填满自动续拉、回顶按钮
//
// 列表和网格共用的那几件（查重、兜住画崩的一行、放队列、说一句话、行尾那枚
// 「⋯」）在 shared.go 和 operation.go，各只写一遍。
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
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrollbar"
	"github.com/egoist/mygo/ui"
)

// Music 是列表里的一首歌：这一行要显示的，和操作要用的。
//
// 字段照 Biu 的 `src/components/music-list-item/index.tsx` 那一行取：
// 封面、标题、UP 名，外加列表里定位一首歌要用的标识。Biu 那行还有播放量和
// 投稿时间两列——它们要 B 站接口的数据才有值，所以**哪一页画不画由那一页
// 自己的 `Columns` 决定**（见那个类型）：本地音乐两样都没有，硬画两列空的
// 是给用户看一片空白。
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

	// Title 是歌名。**列表那一行只占一行**（上游那一行是 `truncate`），网格
	// 卡片那一格是两行（上游的 `line-clamp-2`）——见 card.go。
	Title string

	// Cover 是封面地址。**空串是「没有封面」，不是「加载失败」**——imgcache
	// 的 drawCover 里这是分开的两支。本地文件永远是空的：Biu 上游也不读音频
	// 里内嵌的图。
	Cover string

	// Owner 是 UP 名。空串时那一行不画它。
	Owner string

	// Duration 是时长，零值时那一列不画。本地文件要扫过才有值。
	Duration time.Duration

	// View 是播放量。**零值是「这一页没有这个数」**——本地音乐（文件没有播放量）、
	// 历史记录（那个接口不给）、搜索的「用户」那一档（用户不是视频）、收藏夹里的
	// 音轨（`cnt_info` 那一条本来就不带播放数）都是零，而网格卡片封面底部那一条
	// 渐变上**就不画那半边**（见 `hover.CardCover`），列表那一列也留空。
	//
	// ⚠️ **它不是身份的一部分**：同一首歌在不同页面上播放量可能不一样（收藏夹里那份是
	// 收藏夹那一刻的），而铁律四要的是稳定且唯一的**身份**。别把它并进 ID。
	//
	// ⚠️ **哪些页面有它**（往这一行填它的地方就是那五处，这一层不替谁猜）：
	//
	//	有：首页 / 新歌速递（`homeMusicOf`）、搜索歌曲与视频（`searchVideoMusic`）、
	//	    UP 空间投稿（`fetchVideos`）、稍后再看（`laterRowOf`）、收藏夹 / 合集 /
	//	    系列（`fetchCollectionPage`，收藏夹那一路的数据在 `bili.Media.View`，
	//	    来源是接口的 `cnt_info.play`）。
	//	没有（而且本来也没有）：本地音乐（文件没有播放量）、历史记录（那个接口
	//	    不给）、搜索的「用户」那一档（用户不是视频）。
	//
	// 所以「哪一格空着」是**数据源的事**，不是某一页忘了填——而空着的效果是那半边
	// 不画（理由见 `hover.CardCover`）。
	View int64

	// PubTime 是这一行**时间那一列**要画的时间。零值是「这一页没有这个数」——
	// 那一格留空，不拿别的时间顶上。
	//
	// ⚠️ **各页的时间不是同一个字段**（票 #142 要求逐页核，不许拿一个页的
	// 字段套另一个页）：
	//
	//	搜索歌曲 / 视频	`pubdate`（`bili.VideoHit.PubDate`）
	//	UP 空间投稿	`created`（`bili.UpVideo.Created`，不是 pubdate）
	//	稍后再看		`pubdate`（`bili.ToViewItem.PubDate`）
	//	收藏夹		`fav_time`（收藏时间，那一页的表头也写「收藏时间」）
	//	合集 / 系列	`pubtime`（`bili.Media.PubTime`）
	//	首页		没有——上游首页就是 `hidePubTime`，而且那一层没把 pubdate
	//			带到 RecommendItem 上。
	//	本地音乐 / 历史记录 / 搜索的「用户」	没有。
	//
	// 哪些页真的画这一列由那一页的 `Columns.PubTime` 决定；画了而这一条是零值
	// 时那一格空着（比如收藏夹里一条没给 fav_time 的音轨）。
	PubTime time.Time

	// Item 是播放和右键菜单真正用的那一项。B 站的歌以后由接口填，
	// 现在先有本地文件（app.LocalItem）。
	Item app.Item
}

// Columns 是一页的行**除了**序号 / 封面 / 标题 / 时长 / 行尾那枚「⋯」之外
// 还画哪两列。
//
// ⚠️ **行和表头读同一份**：只给表头加一列、或者只给行加一列，右边那几列
// 就整整错开一列的宽——而那种错在画面上只是「表头歪了一点」。所以一页
// 只调一次 `SetColumns`，两边都从这里读。
//
// 零值是两列都不画：本地音乐、搜索的「用户」那一档都是这样（历史记录自己
// 画行，压根不走这一个）——「摆一列这一页没有内容的」比不摆更糟（见
// `header.go` 文件头）。
type Columns struct {
	// PlayCount 报播放量那一列画不画。
	PlayCount bool
	// PubTime 报时间那一列画不画。
	PubTime bool
	// PubTimeTitle 是时间那一列的表头字。空串是「投稿时间」——收藏夹那一页
	// 写的是「收藏时间」，因为它画的是 `fav_time`（见 `Music.PubTime`）。
	PubTimeTitle string
	// Now 是判「几天前」用的「现在」。nil 是墙钟。
	//
	// ⚠️ **传一个读外壳时钟的闭包，别传 `s.clock.Now` 这个值**：测试是在
	// 外壳建好之后才把 `s.clock` 换成假时钟的，值在那一刻就已经定住了。
	Now func() time.Time
}

// pubTimeTitle 是时间那一列表头该写的字（空串的默认值只写这一处）。
func (c Columns) pubTimeTitle() string {
	if c.PubTimeTitle == "" {
		return "投稿时间"
	}
	return c.PubTimeTitle
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

	// emptyView 是「一行都没有时那一格」的画法（见 SetEmptyView）。nil 是没装，
	// 那一格走 emptyText() 那句话。
	emptyView func(c *ui.Context)

	// hot 是上一帧画完后量到的「指针压在滚动条那一带」（见 `Hot`）。
	hot bool

	// view 是这一份歌此刻摆成什么样，见 SetView。
	//
	// 🔴 **只有列表和紧凑两档真的走这里**：网格那一档不走 `List`，走
	// `SwitchView` 里的另一个控件（`grid.go`）。`List` 看见 `ViewCard` 时的行为
	// 是**退回列表**而不是画半张网格——理由见 SetView 的注释。
	view View

	// cols 是这一页要多画哪两列（播放量 / 时间），见 SetColumns。
	cols Columns

	// width 是这个列表**上一帧**量出来的宽，用来判「那两列放不放得下」。
	//
	// ⚠️ 量的是 `ui.List` 自己的盒子（`Bounds` 读的是上一帧的盒子）。所以
	// 窄窗口的头一帧还是按全画，下一帧才收——和网格换列数同一个节奏（见
	// `Grid.width`）。`View` 末尾写它，`Columns()` 读它。
	width float32

	// failed 是画崩了的行（按 ID 记），下一帧不重试。
	//
	// 为什么要记：MyGo 的事件循环没有 recover，行里一个 panic 会一路穿出去
	// 把整个进程带走（本包自己 recover 住，见 viewRow）。不记的话这一行
	// 每帧崩一次，日志被刷爆、帧率拖垮——比画不出来糟得多。
	failed map[string]bool

	// keys 记「上一次输入是键盘还是指针」（理由见 `selection.go` 的 KeyboardMode）。
	//
	// ⚠️ **焦点环靠它**：框架那个 `focusVisible` 只被 Tab 设回来，方向键不会——
	// 于是「鼠标点过一行之后再按方向键」那一路上，键盘走到哪一行都没有指示。
	keys KeyboardMode
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
	l := &List{cache: cache, acts: acts, sel: -1}
	// 「↑ 到顶时把焦点交回侧栏」那一档（票 #245 的 E4）。接的是接线里的那份，
	// 为 nil 时列表到了顶也不管——单独造这个包的测试没有界面层，那种情况下
	// 「往回走」这件事压根不发生。
	l.keys.upAtTop = acts.FocusOut
	return l
}

// TakeFocus 把键盘焦点接到这张列表上，并把光标放到第一行。
//
// ⚠️ **光标放到第一行**：用户是「从侧栏走进内容区」的。进来之后光标停在
// 「还没选中任何一行」的话，方向键要先按一下才开始挪——进来就停在第一首上，
// 才是「走进来就在第一首歌上」。
//
// ⚠️ **下一帧才生效**：框架的焦点请求是在一帧的末尾统一兑现的。
func (l *List) TakeFocus() {
	if len(l.songs) > 0 {
		l.sel = 0
	}
	l.state.Handle.Focus()
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

// SetEmptyView 换掉「一行都没有时**那一格**」的画法。
//
// ⚠️ **它和 `SetEmpty` 不是一回事**：`SetEmpty` 换的是那一格里**那句话**
// （13px 的一行淡灰字），而这个换的是**整格**——外面画成什么样就是什么样。
//
// 为什么要有这个口子：应用层要「加载中」那一格和「没有结果」那一格是**同一个
// 形状**（居中、15px、同一套留白），而那一格住在应用层（`internal/ui`），这一层
// 够不着它的画法——所以由外面把画法递进来。
//
// ⚠️ **传 nil 是退回那句话**（`SetEmpty` 那一句，或默认那句）。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (l *List) SetEmptyView(fn func(c *ui.Context)) { l.emptyView = fn }

// SetView 换这一份歌的摆法（设置里的「数据显示样式」）。
//
// **只认两档**：列表和紧凑。收到 `ViewCard` 时**按列表画**——网格那一档走的是
// `Grid`（`SwitchView` 在两者之间分发），而一个 `List` 硬画网格的话得自己算列数
// 自己守滚动锚点，而那套东西已经在 `grid.go` 里了（`keepAnchor`）。在这里退回列表
// 而不是画半张，是为了让「忘了分发」这件事的后果是**看得见的旧样子**而不是一张
// 排坏的网格。
//
// ⚠️ **每帧调**：设置随时会变，而这一份状态是界面线程的（铁律一）。
//
// ⚠️ **换档不动滚动位置**。紧凑和列表的行高差得很远，同一个行号落在屏幕上的位置
// 不一样——用户改完设置看到「自己盯着的那首跳到别处」比看到「行变矮了」更火大。
// 而框架的 `ListState` 记的是「第几行 + 那一行离顶多远」（`mygo/ui/list.go`），
// 行的身份又是 ID（铁律四），所以那一首**自己会回到它在屏幕上的位置**。
func (l *List) SetView(v View) {
	if v == ViewCard {
		v = ViewList
	}
	l.view = v
}

// Layout 返回这一份歌此刻的摆法。页面和测试都要读它（见 SwitchView）。
func (l *List) Layout() View { return l.view }

// SetColumns 换这一页多画哪两列（播放量 / 时间）。零值是两列都不画。
//
// ⚠️ **它同时决定行和表头**（见 Columns 的注释）：只换一边的话右边那几列
// 整整错开一列的宽。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (l *List) SetColumns(c Columns) { l.cols = c }

// Columns 返回这一页**这一帧真的会画**哪几列。
//
// 它不一定等于 `SetColumns` 装的那一份：窗口窄到放不下时，这里会把「时间」
// 先收掉、再不行把「播放量」也收掉（见 `minTitleWidth` 和 `fixedWidth`）。
// 表头那一层和行读的都是它，所以两边永远一致。
//
// ⚠️ **判据是上一帧量到的列表宽**（`l.width`）：第一帧还没量，照全画；
// 第二帧起按真的宽度算。和网格按容器宽换列数是同一个节奏。
func (l *List) Columns() Columns {
	c := l.cols
	if l.width <= 0 {
		return c
	}
	if c.PubTime && l.width-l.fixedWidth(c) < minTitleWidth {
		c.PubTime = false
	}
	if c.PlayCount && l.width-l.fixedWidth(c) < minTitleWidth {
		c.PlayCount = false
	}
	return c
}

// minTitleWidth 是**标题那一列至少留多宽**。
//
// 两列是这一票加的，而列表的宽是窗口减去侧栏和页边距来的——最小窗口 900
// 配最宽侧栏 480 时只剩 380，全画的话标题那一列是 0 甚至负数，歌名和表头
// 「标题」都只剩「…」（评审量到过）。80 是「还读得出几个字」的量：font 16
// 下大约四个汉字，够认出是哪一首。到这个宽度以下就先把「投稿时间」收掉，
// 它有 80 宽，正好换回一条像样的歌名；再不够才收播放量（64），那时标题
// 回到基线的样子。
//
// ⚠️ **收的是「这一帧画不画」，不是把 `SetColumns` 改掉**：窗口拖宽之后
// 下一帧自己回来。
const minTitleWidth = 80

// fixedWidth 是这一行**除标题那一列以外**的宽：序号、封面、可选的两列、
// 时长、行尾那枚「⋯」，加上列间距和行的左右内边距。
//
// 它和 `viewCard` 里摆的那些盒子是同一批常量（`MetricsOf`），所以不会出现
// 「判据算的和真摆的不是一个数」。
func (l *List) fixedWidth(c Columns) float32 {
	m := MetricsOf(l.view)
	items := 5 // 序号 / 封面 / 标题 / 时长 / 行尾「⋯」
	w := m.Index + m.Cover + m.Clock + OperationColWidth + 2*m.PadX
	if c.PlayCount {
		items++
		w += m.Play
	}
	if c.PubTime {
		items++
		w += m.Date
	}
	return w + float32(items-1)*m.Gap
}

// now 是判「几天前」用的「现在」。没装 `Columns.Now` 时是墙钟。
//
// ⚠️ **nil 时退回 `time.Now()` 是有意的**：只有界面那一层知道该用哪颗时钟，
// 而 `widgets/list` 的独立测试没有外壳。装上 `Columns.Now` 的页面（以及界
// 面层的测试）都走那颗假时钟。
func (l *List) now() time.Time {
	if l.cols.Now != nil {
		return l.cols.Now()
	}
	return time.Now()
}

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
// 列表是在布局时才滚过去的，而那一条落在哪儿是布局那一步才定的（列表这一档的行高
// 是定死的，网格那一档是量出来的——量完才知道）。
func (l *List) ScrollTo(row int, align ui.Align) { l.state.ScrollTo(row, align) }

// ScrollTop 回到顶上。切路由、点回顶按钮、以及 `SwitchView` 换摆法时都走它。
//
// ⚠️ 只能由界面线程调，同 ScrollTo。
func (l *List) ScrollTop() { l.state.ScrollTo(0, ui.Start) }

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
	// 登记自己是「**此刻内容区里是这一个**」：界面层靠它把焦点交进来
	// （票 #245 的 E4 的「进内容区」）。理由见 focus.go 的文件头。
	//
	// ⚠️ **每一帧都登记**：切页、切 Tab 之后登记的就是新那一个——登记的是
	// 「此刻」，不是「建过」。
	if l.acts.Focus != nil {
		l.acts.Focus(l)
	}

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
	// Selected 让点击能选中一行，双击提交。
	//
	// ⚠️ **它不再画东西了**：框架会给选中的行铺一整块实心主色底（`ui/table.go`
	// 的 node.Selected），票 #197 拍板去掉它——那一层在库里改不动，所以选中的行
	// 自己刷一层底色盖上去（见 `card.go` 的 `rowCover`）。
	l.state.Selected = &l.sel

	// 选中挪了没有？挪了而且不是指针挪的，就是键盘挪的（焦点环靠它，理由见
	// `selection.go` 的 KeyboardMode）。
	//
	// ⚠️ **要在画行之前问**：焦点环是画在行上的，这一句晚了那一帧就少一圈环。
	l.keys.Moved(l.sel)

	list := ui.List(c, &l.state, l.rows()).
		Grow(1).
		FillWidth().
		// ⚠️ **听一听输入**：只为了知道「上一次是键盘还是指针」（焦点环靠它，
		// 理由见 `selection.go` 的 KeyboardMode）。
		//
		// 返回 false 是刻意的：自己不吃这个事件，方向键挪游标、点击选行、Tab 换
		// 焦点都还在。
		HandleInput(l.keys.Input).
		// 双击这一行 = 播放它（Biu 的 `onDoubleClick`，不是单击：
		// `biu/src/components/music-list-item/index.tsx`）。
		OnSubmit(func() { l.playFrom(l.sel) }).
		// ⚠️ **焦点环只留在「还没选中任何一行」的时候**（票 #197）。
		//
		// 框架默认给**整张列表**画一圈焦点环（`mygo/ui/inline.go` 的 ringShown），
		// 而票面要的是「键盘走到**哪一行**都要有可见焦点指示」——指示得落在那一行
		// 上（见 `card.go` 的 `rowFocusRing`）。两处都画就是两圈环。
		//
		// 所以：**还没选中行时环在列表上**（Tab 刚进来的那一瞬间，焦点在列表上，
		// 列表就是焦点所在的地方），**选中行之后环跟着那一行走**（列表那一圈收
		// 起来，免得两处都亮）。鼠标点过之后焦点不是从键盘来的，两边都不画。
		FocusRing(l.sel < 0).
		// 没有歌时列表是空的，Children 里那一支就是它的空状态。
		// 一句都不说的时候连这一支都不建（见 SetEmpty）。
		Children(func() {
			if len(l.songs) == 0 {
				// ⚠️ **先看外面递进来的画法**（`SetEmptyView`），没有才退回那句
				// 13px 的话——两者的形状差得很远（一个是居中带插画位的一格，一个是
				// 缩在左上角的一行字）。
				if l.emptyView != nil {
					l.emptyView(c)
					return
				}
				if s := l.emptyText(); s != "" {
					ui.Text(c, s).FontSize(scale.Font(c, 13)).
						TextColor(c.Theme().TextMuted).Padding(16)
				}
			}
		}).
		Rows(func(r ui.ListRow) { l.viewRow(r) })

	// 滚动位置写回调用方的句柄（见 SetScroll）。
	if l.scroll != nil {
		list.TrackScroll(l.scroll)
	}

	// 量这一帧的列表宽，下一帧判那两列放不放得下（`Columns`）。`Bounds` 读的
	// 是**上一帧**量好的盒子，所以窄窗口的头一帧还是照全画——和网格换列数
	// 同一个节奏，见 `Grid.width`。
	if bw := list.Bounds().W; bw > 0 {
		l.width = bw
	}

	// 量滚动条的悬停。界面层下一帧据此把主题的滚动条色换深一档（`Hot` 的
	// 注释；框架只有一个滚动条颜色）。
	l.hot = scrollbar.Hot(list)
}

// Hot 报上一次 View 时指针是不是压在这个列表的滚动条上。
//
// 页面每帧在 `View` 之后把它交给壳（`Shell.noteScrollbar`）——主题是下一帧
// 开头装的，所以读的正是这个「上一帧」的值。
func (l *List) Hot() bool { return l.hot }

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
	ui.Text(c, "这一行画不出来").FontSize(scale.Font(c, 13)).TextColor(c.Theme().TextMuted).
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
