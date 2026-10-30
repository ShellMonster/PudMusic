// 评论页：Biu 没有这一页（上游 `biu/src/pages/` 里没有），所以口径我们自己定。
//
// # 这一页最要紧的是「这些评论到底是谁的评论」
//
// 三种条目拿到的评论**语义不同**：
//
//	audio（音乐库音频，type=14）  这一**首歌**的评论（实测原文是「太短了」
//	                             「求全部」——在聊这首歌本身）
//	mv   （B 站视频，type=1）      **这个视频**的评论（在聊视频本身、UP 主、
//	                             弹幕梗，**不一定是这首歌**）
//	local（本地文件）             **没有评论** ⇒ 入口不摆，一个请求都不发
//
// ⇒ 页头那一枚角标就是这件事的全部：**「这首歌的评论」**和**「这个视频的
// 评论」**是两句话，摆错一个用户就以为自己在读歌的评论（而他在读视频的）。
//
// # 排序只有两档（不是三档）
//
// B 站那一档叫「评论」（`mode=1`）的**内容与「最新评论」（`mode=2`）逐条相同**
// （2026-10-27 真机回归实测，票 #591），而 `cursor.support_mode` 恒为 `[2, 3]`
// 不含它——**那一档早就名存实亡**。
//
// ⇒ 界面上**只摆两档**（`bili.CommentSorts()`：最热 `mode=3` / 最新 `mode=2`）。
// 摆成三个「档位」而其中两个内容一模一样，用户切过去会以为「切了档但没生效」。
//
// # 铁律三：网络请求一律不在 view 里发
//
// 排序切换 / 刷新 / 翻页 / 展开楼中楼，四件都要发请求——**而这一页的 view 一件
// 都不发**。view 只**记下用户的意图**（`p.wantRefresh` / `p.sortIdx` /
// `p.expanded`），真正发出去的是 `syncComments`（每帧的副作用那一层，
// `internal/ui/underlay.go` 的 `pageSyncs`）。
//
// 🔴 **这不是洁癖**：view 是界面线程，在它里面同步发请求就是界面卡住。
// 应用层那一层（`internal/app/comments.go`）已经把请求递到后台 goroutine 上
// 了，而 view 这一层连「起一趟」都不做——职责分得干净。
//
// # 行高不固定 ⇒ 不能套定高 64 的 `widgets/list`
//
// `internal/ui/widgets/list` 那一族是**音乐行**（定高 64，封面 48 撑着），
// 而评论行的正文会折行、还有楼中楼缩进——**行高是量出来的**。所以这一页用
// 框架原生的 `ui.List`（它的行高是量出来的：`mygo/ui/list.go` 的 `listHeights`
// 量一行记一行，没量过的按平均估），行画法自己写。
//
// ⚠️ **底座不一样，但三重保障的判据是同一份**：这一页用不了
// `widgets/list`（那一档是定高 64 的音乐行，而这一页是**变高正文 + 表情图**
// ——行高是量出来的），但「这一帧该不该拉下一页」判的是同一件事
// ⇒ **判据复用 `widgets/list/paged.go` 里那一份**（`DecideFirstPage` /
// `DecideNextPage` / `PagingState`），不另写一套。
//
// 两份判据迟早分叉，而分叉出来的那种差异极难查（一个列表会补屏，另一个
// 不会，而且两边看着都「对」）——见 `syncComments`。
//
// # 正文是「自己画」的（表情要成图）
//
// 🔴 **框架的文本控件画不出 inline object 那一格**：`mygo/ui/paint.go` 的
// `textForeground` 跳过 `g.Object` 的字形——它只给那一格**留位置**（
// `Span.ObjectWidth` / `ObjectHeight`），不画东西。而表情就是靠那一格摆进去的
// （正文里的 `[妙啊]` 换成一枚 U+FFFC，那一格贴上那张表情图）。
//
// ⇒ 正文**自己排版、自己画**（`ui.ShapeRichText` + `Painter.TextLayout` +
// `Painter.Image`）。而文字**照样交给辅助技术和无头 Tester**：那一格用
// `.Label(...)` 把正文交给框架，于是屏幕阅读器读得到它，`Tester.Find` 也
// 找得到它——不然这一页的正文对它们是不存在的。
package ui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/guard"
	"github.com/ShellMonster/PudMusic/internal/ui/songname"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// commentsPath 是评论页那条路由。
//
// ⚠️ **它不进侧栏菜单**（`InMenu: false`）：它是「当前这一首」的附属页，不是
// 一个栏目——摆进侧栏的话，用户会以为那是一个能一直待着的页面，而它讲的
// 东西每换一首就全变了。
const commentsPath = "/comments"

// commentsSortLabels 是排序那两档的字。
//
// ⚠️ **只有两档**（理由见文件头）：B 站那个「评论」（`mode=1`）和「最新」是
// 同一份内容，摆成三档就是骗人。
var commentsSortLabels = []string{"最热", "最新"}

// 这一页的几个尺寸。
const (
	// commentAvatar 是主评论那一枚头像的边长。
	commentAvatar = 32
	// commentReplyAvatar 是楼中楼那一枚头像的边长（比主评论小一号）。
	commentReplyAvatar = 24
	// commentEmote 是正文里一枚表情占位符的边长。
	commentEmote = 18
	// commentBodyLines 是主评论正文最多几行（超出的截断加省略号）。
	commentBodyLines = 6
	// commentReplyBodyLines 是楼中楼正文最多几行。
	commentReplyBodyLines = 4
	// commentBodyGap 是正文那一列和头像之间的距离（跟第一行同一档）。
	commentBodyGap = space.Px12
)

// commentsMentionScanMax 是「筛选开着而一条都没命中」时**最多翻多少条评论**。
//
// 🔴 **为什么要一个上限**：那句话「这个视频的 N 条评论里没有提到这首歌」只有
// 在**翻完**这一首的评论之后才说得出来（只翻了第一页就说「没有」，那是撒谎）。
// 而 B 站那个视频可能有十万条评论——全翻一遍就是几千趟请求（打的是 B 站的
// 评论接口，那是别人的服务器）。
//
// ⇒ 翻到上限就停手，那一句话改成老实的说法（「已翻到的 N 条评论里还没有提到
// 这首歌」）。
//
// ⚠️ **600 条 = 20 页**（一页 30 条，`internal/app` 那一档）。
const commentsMentionScanMax = 600

// commentsMentionCovered 报「这一首的评论我们已经查完了吗」。
//
// 🔴 **不能用「B 站说到底了」当查完了**（票 #574 的评审）：B 站说到底了只
// 说的是「没有下一页了」，而它自己的总数（`cursor.all_count`）可能还有十万
// 条——实测有一档是「翻了 12 条，B 站说到底了，而它给的总数是 184226」。
// 那一档里说「这个视频的 184226 条评论里没有提到这首歌」是**撒谎**：我们只
// 看过 12 条。
//
// ⇒ **查完的判据是「翻到的条数盖住了它给的总数」**：
//
//	给了总数   翻到的 >= 总数 ⇒ 查完了
//	没给总数   B 站说到底了 ⇒ 查完了（它给 0 就是「还不知道有多少」，
//	           而那时候「没有下一页」就是全部）
func commentsMentionCovered(loaded int, allCount int64, hasMore bool) bool {
	if allCount > 0 {
		return int64(loaded) >= allCount
	}
	return !hasMore
}

// commentsPage 是评论页自己的一摊状态。
//
// ⚠️ **它挂在壳上而不是每帧新建**：列表的滚动位置、行身份、展开的楼中楼
// 都得跨帧活着（理由同 `laterPage` / `home`）。
type commentsPage struct {
	// state 是列表自己的状态（滚动位置、行锚点）。
	state ui.ListState

	// sortIdx 是排序 Tab 选中的下标（0 = 最热，1 = 最新）。
	//
	// ⚠️ **它是 `ui.Tabs` 绑着的那个 int**：框架在**建到的时候**才写它
	// （`mygo/ui/base.go` 的 `tabsParts.Tab` 的 `choose`），所以 view 画完
	// 那一排之后才读得到用户刚点的那一档（理由见 `syncComments`）。
	sortIdx int
	// sortInit 记着「排序档已经从应用层取过一次了」。
	//
	// ⚠️ **不记它的话，第一次进这一页会用零值（最热）把用户上次挑的那一档
	// 盖掉**——而应用层那一档是跨歌记着的。
	sortInit bool

	// expanded 是「哪几条的楼中楼展开了」。
	expanded map[int64]bool
	// expRev 是展开集合的版本号：改一次加一（用来认出「行要重建了」）。
	expRev int

	// wantRefresh 是「用户按了刷新」这个意图。**view 只记它，发请求的是
	// `syncComments`**（理由见文件头「铁律三」）。
	wantRefresh bool

	// mentionOnly 是「只看正文里提到这首歌的评论」那一枚开关（票 #574）。
	//
	// ⚠️ **默认是关的**：这一页默认摆的是这一首的**全部**评论——筛选是用户
	// 自己挑出来的窄视图，不是默认口径（而 `mv` 那一档默认摆的是**这个视频
	// 的评论**，页头那一枚角标已经说清它是谁的评论了）。
	//
	// ⚠️ **它是 `springSwitch` 绑着的那个 bool**：框架在**建到的时候**才写它
	// （理由同 `sortIdx`），所以 `syncComments` 读到的是上一帧的那一次点击。
	mentionOnly bool
	// srcMention 是「上一次建行时那一枚开关是什么值」——变了就重建行。
	srcMention bool

	// names 是「这一首的别名表」（哪些写法算提到了这首歌）。
	//
	// ⚠️ **它是 `internal/ui/songname` 那一处算出来的**（那里是唯一的一处口
	// 径）：界面层不自己再拼一份歌名或别名——两份口径迟早分叉，而分叉出来
	// 的那种差异极难查。
	names songname.Table
	// srcTitle / srcArtist 是「上一次算别名表时那首歌叫什么」——变了就重算。
	//
	// ⚠️ **不每帧重算**：别名表是建行和筛选用的一次性产物，而每帧重算就是
	// 每帧一次分配（理由同 `framebudget_test.go` 盯着的「每帧现算」那族）。
	srcTitle  string
	srcArtist string
	// hits 是「这一轮筛出来几条提到歌名的评论」。
	hits int
	// loaded 是「已经拿到多少条评论了」（这一页一共拿到了多少条）。
	loaded int
	// covered 是「这一首的评论我们已经查完了吗」。
	//
	// 🔴 **它是「那句『没有提到这首歌』能不能说」的判据**（票 #574 的评审）：
	// B 站说「到底了」（`CommentsHasMore` 为假）**不等于**我们查完了——它
	// 自己的总数（`cursor.all_count`）说还有十万条，而我们就翻到了 12 条
	// （接口重复给同一批的时候，应用层那一档「无进展保护」会把它当成到底）。
	//
	// ⇒ 查完了才说那句完整的话，没查完只说「已翻到的 N 条评论里还没有提到
	// 这首歌」（理由见 `commentsMentionCovered`）。
	covered bool

	// rows 是摊平之后的行：主评论 + 展开的楼中楼。
	rows []commentRow
	// srcLen / srcFirst / srcSort 是「上一次建行时评论列表的样子」。
	srcLen   int
	srcFirst int64
	srcSort  bili.CommentSort
	// rowsExpRev 是「上一次建行时展开集合的版本」。
	rowsExpRev int
	// replySig 是「上一次建行时各条楼中楼的条数之和」。
	replySig int

	// bodies 是正文排版的缓存（按 rpid）。
	bodies map[int64]*commentBody
	// listW 是列表容器上一帧的宽（正文排版按它算宽度）。
	listW float32
	// mark 是这一帧的高亮配色（筛选关着时是零值）。
	//
	// ⚠️ **一帧算一次**（`viewComments` 开头），不是每条评论算一次：`ink.Ensure`
	// 是一趟二十步的二分，而这一页一屏十来条评论——每条各算一遍是白花的。
	mark commentMark
	// markTheme / markOn 记着「上面那一份配色是按哪一份主题、哪个开关算的」。
	//
	// ⚠️ **主题不变就不重算**：`ink.Ensure` 那一趟是二十步二分（每步三次
	// WCAG 亮度换算，每次三个 `math.Pow`）——每帧算一遍虽然不分配，但那是
	// 白烧的 CPU（理由同 `themeCache`：主题只在设置变了的时候才变）。
	//
	// ⚠️ **比的是指针**：`themeCache.of` 在键没变时返回**同一份**主题
	// （`internal/ui/theme.go` 那一段），所以一次 `==` 就够。
	markTheme *ui.Theme
	markOn    bool

	// slot 是「上一次是给哪一首建的」——换歌就清缓存（rpid 会撞）。
	slot string
}

// commentRow 是列表里的一行。
type commentRow struct {
	// key 是这一行的身份。**必须稳定且唯一**（铁律四）：前面插一行会让后面
	// 所有行的状态整体错配。
	//
	// ⚠️ **它是提前装箱好的 `any`**：框架每帧要问 `ListState.Key(i)`，而
	// `int64 → any` 那一步每次都要在堆上分配一次（票 #339 那族的道理）。
	//
	// ⚠️ **它是 `commentRowKey` 算出来的那个 int64**（不是 rpid 本身）：
	// 主评论和楼中楼要分开算，而框架那边见**同一个父节点下有两个一样的
	// key** 就 panic（`ui: two elements under one parent have the key ...`）——
	// 症状是整页变成错误页。
	key any
	// cm 是这一条评论。
	cm bili.Comment
	// reply 是「这是楼中楼」。
	reply bool
	// root 是楼中楼属于哪一条根评论。
	root int64

	// ⚠️ **下面这四样是「建行的时候算好、每一帧直接用」的**——
	// 它们原来都是**每帧每一行现算**的（`strconv.FormatInt` 拼身份、
	// 拼「LV6」、拼点赞数、拼回复数），而一屏十来行就是**几十次分配**。
	//
	// 理由和票 #313 / #339 同源（见 `internal/ui/framebudget_test.go` 那一档）：
	// 「每帧现算」是这一族测试盯着的坏处。

	// elemKey 是这一行自己的身份（「comment:123」/「comment-reply:123」）。
	elemKey string
	// levelText 是等级徽章上的字（「LV6」）；空串 = 没有等级。
	levelText string
	// likeText 是点赞数那几个字。
	likeText string
	// replyText 是回复数那几个字（「共52条回复」）；空串 = 这一条没有楼中楼。
	replyText string
}

// commentRowKey 算一行的身份。
//
// 🔴 **主评论和楼中楼必须分开**：rpid 各自唯一，但**行身份要在同一个父节点
// 下唯一**——所以主评论用 `rpid<<1`，楼中楼用 `rpid<<1|1`。
//
// ⚠️ **必须是 int64，不能是结构体**：框架算 key 的哈希时，走结构体那条路要
// `fmt.Sprintf`（`mygo/ui/context.go` 的 `keyHash`）——那是**每帧每一行一次
// 分配**，而这一页的每帧分配预算只剩几个（见 `framebudget_test.go`）。
//
// ⚠️ **左移不会溢出**：rpid 是 B 站的 int64（实测在 10^11 那一档），左移
// 一位离 int64 的上限还远得很。
func commentRowKey(rpid int64, reply bool) int64 {
	if reply {
		return rpid<<1 | 1
	}
	return rpid << 1
}

// commentBody 是一条评论正文的排版结果。
type commentBody struct {
	// layout 是排好版的那一份（`ui.ShapeRichText` 的结果）。
	layout *ui.TextLayout
	// objects 是正文里那些表情占位符的位置（相对排版原点）。
	objects []ui.TextObject
	// emotes 是表情图的地址，与 objects 一一对应。
	emotes []string
	// plain 是**给辅助技术和无头 Tester 的那份正文**（表情占位符换成
	// 「［表情］」）。
	//
	// ⚠️ **它不是画出来的那一份**：画出来的是 `layout`，而这一份是**文字**。
	// 两者必须是同一句话——不然屏幕阅读器读到的和眼睛看到的不一样。
	plain string
	// w 是排版时的宽度，h 是排出来的高度。
	w, h float32
	// lines 是最多几行（主评论和楼中楼不一样）。
	lines int
	// lineH 是**一行**有多高（字体的 ascent + descent + line gap）。
	//
	// ⚠️ **它记下来是因为「折起来」那一档要拿它算上限**：正文最多几行是
	// `lines`，于是这一格最多有多高就是 `lines × lineH`——而那个数**只
	// 排版的时候算得出来**（`font.Metrics()` 那一趟）。
	lineH float32
	// paint 是画这一格的那一支。
	//
	// ⚠️ **它记下来是因为闭包每帧新建一次就是一次分配**：而这一格每一帧都要
	// 画（正文是「自己排版自己画」的），所以一屏十来行就是十来次。记下来之后
	// 它**只在正文重排一次的时候新建一次**。
	//
	// ⚠️ **颜色在里面现取**（`pr.Theme()`）：主题是用户能改的，而这一份
	// 缓存跨帧活着——写死颜色的话，用户换了深浅两套之后正文还是旧的颜色。
	paint func(*ui.Painter, ui.Rect)
	// mark 是排这一份时用的高亮配色（零值 = 没高亮）。
	//
	// 🔴 **它是缓存键的一部分**：高亮那几段的字色和底色是**烘进 span 里**的
	// （`ui.Span.Color` / `Background`），而其余的字色是画的时候从主题现取的
	// ——所以主题一变（深浅、主色），这一份就得重排。不重排的话，那几段会一直
	// 是上一次主题的颜色。
	//
	// ⚠️ **`commentMark` 可比较**（两个颜色 + 一个 float32），所以判「配色变
	// 了没有」就是一次 `==`，不分配任何东西。
	mark commentMark
}

// viewComments 是评论页。
func (s *Shell) viewComments(c *ui.Context, _ *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题。
	s.applyFrameTheme(c)

	p := &s.comments
	it, has := s.app.CurrentItem()

	// 🔴 **高亮那一份配色一帧算一次**（票 #684）：筛选开着才算，关着时是零值
	// （`commentBodyOf` 见它是零值就不跑匹配、不带底色）。
	//
	// ⚠️ **它必须在 `applyFrameTheme` 之后算**：`ink.Ensure` 按当前主题的主色
	// 和底色现算。
	//
	// ⚠️ **主题没变、开关没变就复用**：那一趟是二十步二分，而主题只在设置变了
	// 的时候才变（`themeCache` 那一层）。
	on := p.mentionOnly && !p.names.Empty()
	if on != p.markOn || c.Theme() != p.markTheme {
		p.markOn, p.markTheme = on, c.Theme()
		if on {
			p.mark = commentMarkOf(c.Theme())
		} else {
			p.mark = commentMark{}
		}
	}

	// 🔴 **本地文件不摆入口**（`CommentsWorthAsking`）：它在 B 站上没有对应
	// 的对象，而问一个认不出来的 oid 的症状是「静默拿到空列表」——于是界面上
	// 会摆一个永远空着的评论页。
	if !has || !s.app.CommentsWorthAsking() {
		s.viewEmpty(c, "page:"+commentsPath, emptyState{
			Title: "这一首没有评论",
			Hint:  "在播放条上放一首 B 站的歌，再点右上角的「评论」",
			Icon:  iconEmptyMusic,
		})
		return
	}

	ui.Column(c).Key("page:"+commentsPath).Grow(1).FillWidth().
		Padding(space.None, pagePadX).Children(func() {
		// 页头**永远摆**：它就是「这些评论是谁的评论」那一块（文件头）。
		// 列表空着的时候它也得在——不然用户看到的是一片白，而不是「这一首
		// 的评论还没拉回来」。
		s.viewCommentsHeader(c, it)

		if len(p.rows) == 0 {
			s.viewCommentsBody(c, p)
			return
		}

		// 🔴 **行身份必须稳定且唯一**（铁律四）：行会随楼中楼展开而变长，
		// 而 `Key` 认的是**这一条评论**（rpid），不是行号。
		p.state.Key = func(i int) any {
			if i < 0 || i >= len(p.rows) {
				return nil
			}
			return p.rows[i].key
		}
		// 🔴 **行级一道 Guard**（铁律二）：一页几十行，坏一行不该把整页变成
		// 错误页。
		//
		// ⚠️ **它是建一次、每一行都用它**（不是在回调里现建）：`guard.Wrap`
		// 建出来的是**两个闭包**，而在每帧每一行的回调里现建一遍就是**每帧
		// 几十次分配**（一屏十来行 × 两个）——理由同 `framebudget_test.go`
		// 盯着的「每帧现算」那族坏处（票 #313 / #339 就是修那个的）。
		// ⚠️ **行号是「回调里写进去」的**：那个 Guard 是**建一次**的（见上面
		// 那段），而它要画的是**当前这一行**——所以行号由回调写进这个变量，
		// Guard 从那儿读。框架的 `Rows` 回调是**同步**调的（一行调一次），
		// 所以这个变量在 Guard 跑的时候准得很。
		rowIdx := 0
		guarded := guard.Wrap(func(c *ui.Context) {
			if rowIdx >= len(p.rows) {
				s.viewCommentsSentinel(c, p)
				return
			}
			s.viewCommentRow(c, p, p.rows[rowIdx])
		}, func(rec any) { s.onPanicAt("评论那一行", rec) })
		lst := ui.List(c, &p.state, len(p.rows)+1).
			Grow(1).FillWidth().
			// ⚠️ **行间距由列表给**：行与行之间 12（`space.Px12`）。
			GapY(space.Px12).
			Rows(func(r ui.ListRow) {
				rowIdx = r.Index
				guarded(r.Context)
			})
		s.trackScrollbar(lst)

		// 🔴 **登记焦点**（`focus.go` 的 `registerContentFocus`）：这一页**自己
		// 画列表**（不是 `widgets/list` 那个包建的），所以**登记也得自己写
		// 一遍**——漏一遍的症状是「侧栏按 ↓ 进不去这一页的内容区」，而用户
		// 只以为这个应用有时候键盘不好使（见 `focus_test.go` 的
		// `TestEveryPageThatDrawsItsOwnListRegistersIt`）。
		//
		// ⚠️ **它是裸的 `ui.List`**（行是自己画的），所以焦点句柄是框架的
		// `ui.ListState.Handle`——跟 `dynamicfeed.go` 那一档同一个写法。
		s.registerContentFocus(list.HandleFocus(&p.state.Handle))

		// 量这一帧的容器宽，**下一帧**用（`Bounds` 报的是上一帧的盒子）。
		// 正文排版按它算宽度——而排版是**按宽度折行**的，宽度不对就折错位置。
		if bw := lst.Bounds().W; bw > 0 {
			p.listW = bw
		}
	})
}

// viewCommentsBody 是「一条评论都还没有」时画的那一块。
//
// ⚠️ **五档是五句话**：正在拉 / 拉失败了 / 还没有人评论 / 压根没有评论 /
// 筛选开着而一条都没命中。
// 混成一句话的话，用户读到「问不出评论」会以为歌没了，而歌好好地摆在那儿。
func (s *Shell) viewCommentsBody(c *ui.Context, p *commentsPage) {
	switch {
	case s.app.CommentsErr() != nil:
		s.viewLoadError(c, "comments:error", "这一首的评论", "重试",
			s.app.CommentsErr(), func() {
				// ⚠️ **这里只记意图**：发请求的是 `syncComments`（铁律三）。
				p.wantRefresh = true
			})
	case p.mentionOnly && p.hits == 0 && p.loaded > 0:
		// ⚠️ **这一档是「筛选开着，而这一首的评论里一条都没提到歌名」**：
		// 它跟「还没有人评论」是两件事——这一首**有**评论（有几百条），
		// 只是没有一条在聊这首歌。
		s.viewCommentsNoMention(c, p)
	case s.app.CommentsNoComments():
		s.viewEmpty(c, "page:comments:none", emptyState{
			Title: "还没有人评论",
			Hint:  "换一首试试，或者点右上角的「刷新」再问一次",
			Icon:  iconEmptyMusic,
		})
	default:
		s.viewEmpty(c, "page:comments:loading", loadingState("正在拉评论…"))
	}
}

// viewCommentsNoMention 是「筛选开着，而这一首的评论里没有提到这首歌」那一块。
//
// 🔴 **这句话必须说得准**：说「这个视频的 388 条评论里没有提到这首歌」的前提
// 是**这一首的评论已经翻完了**——只翻了第一页就说「没有」，那是撒谎（而用户
// 翻下去就看到一条提到歌名的评论，于是他觉得这个功能在骗人）。
//
// ⇒ 还在翻的时候说老实的那一句（「已翻到的 30 条评论里还没有提到这首歌」），
// 翻完了才说那一句完整的话。
//
// ⚠️ **三种「没查完」各有一句**（票 #574 的评审）：
//
//	还在翻        「还在往下翻这一首的评论，翻到底才知道有没有」
//	翻到上限      「这一页最多翻 600 条评论，剩下的没翻」
//	B 站提前说到底 「B 站说这一页之后就没有了，可它给的总数是 N 条」
//
// ⚠️ **说「还在往下翻」而其实已经停手了，用户会一直等下去**——所以那三种
// 得分开说。
func (s *Shell) viewCommentsNoMention(c *ui.Context, p *commentsPage) {
	// ⚠️ **「这个视频」还是「这一首」**：看的是这一条是什么条目——
	// `mv` 那一档的评论是**这个视频**的，而音乐库音频那一档是**这一首**的
	// （页头那一枚角标是同一句口径，见 `viewCommentsScopeBadge`）。
	scope := "这一首"
	if it, ok := s.app.CurrentItem(); ok && it.Type == "mv" {
		scope = "这个视频"
	}
	// ⚠️ **那一枚键是「怎么才能不空」**：这一格空着是因为用户自己把筛选按上
	// 了，而按一下就能看回全部评论——不用他去页头找那枚开关。
	//
	// ⚠️ **它只改界面上的那一枚开关**（不发请求）：评论已经拿回来了，关掉筛
	// 选只是把它们重新摆出来（铁律三）。
	showAll := &emptyAction{Label: "看全部评论", Do: func() {
		p.mentionOnly = false
	}}
	if !p.covered {
		// 还没查完：这一句只说**翻到的那几条**。
		//
		// ⚠️ **三种没查完各有一句**：说「还在往下翻」而其实已经停手了，用户
		// 会一直等下去——而这一页等下去也不会有下文（上限就是上限）。
		hint := "还在往下翻这一首的评论，翻到底才知道有没有"
		switch {
		case p.loaded >= commentsMentionScanMax:
			hint = fmt.Sprintf("这一页最多翻 %d 条评论，剩下的没翻", commentsMentionScanMax)
		case !s.app.CommentsHasMore():
			// 🔴 **B 站说到底了，而它自己的总数说还有更多**（接口重复给同一
			// 批的时候就是这样）：那句话不能按「查完了」说。
			hint = fmt.Sprintf("B 站说这一页之后就没有了，可它给的总数是 %d 条",
				s.app.CommentsAllCount())
		}
		s.viewEmpty(c, "page:comments:nomention", emptyState{
			Title: fmt.Sprintf("已翻到的 %d 条评论里还没有提到这首歌", p.loaded),
			Hint:  hint,
			Info:  true,
			Icon:  iconEmptyMusic,
			// ⚠️ **还在翻的时候也给这一枚**：用户等不及的时候得有个退路，
			// 而这一枚按下去那趟扫描就停了（筛选关掉 ⇒ 不再自己翻）。
			Action: showAll,
		})
		return
	}
	// 查完了：B 站给的评论总数就是这一首的条数（它给 0 就是还不知道）。
	n := s.app.CommentsAllCount()
	if n <= 0 {
		n = int64(p.loaded)
	}
	s.viewEmpty(c, "page:comments:nomention", emptyState{
		Title:  fmt.Sprintf("%s的 %d 条评论里没有提到这首歌", scope, n),
		Action: showAll,
		Icon:   iconEmptyMusic,
	})
}

// viewCommentsHeader 是页头：**这一首是什么 + 这些评论是谁的评论**，
// 右边是排序两档和一枚「刷新」。
func (s *Shell) viewCommentsHeader(c *ui.Context, it app.Item) {
	t := c.Theme()
	p := &s.comments

	ui.Column(c).Key("comments:header").FillWidth().Gap(space.Px6).
		Padding(space.Px12, space.None, space.Px8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(space.Px12).Children(func() {
			// 左：标题 + 那一枚角标。
			//
			// ⚠️ **角标不许省**（文件头）：它是「这些评论是谁的评论」那句话
			// 唯一的落点，而角标跟标题挨着，用户一眼就分得清。
			//
			// 🔴 **角标必须真的「挨着」标题**（视觉审查量出来的）：标题那一格
			// 之前是 `Grow(1)`，于是**角标被推到了那一行的最右端**——在
			// 1400 宽的窗口上它离标题九百多像素，而那一行的右边摆着排序档
			// 和刷新键，于是角标**看着像是给排序那一组贴的标签**（「这些评论
			// 是最热的这一档？」）。用户一眼分不清「这是这首歌的评论」还是
			// 「这是这个视频的评论」，而那正是这一票最要紧的一件事。
			//
			// ⇒ 标题**不 `Grow`**（它自己按自然宽摆，超出那一格就省略号），
			// 角标紧跟在它后面。
			ui.Row(c).Grow(1).MinWidth(0).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, it.Title).FontSize(scale.Font(c, scale.Size18)).TextColor(t.Text).
					MaxLines(1).Ellipsis("…")
				s.viewCommentsScopeBadge(c, it)
			})

			// 右：排序两档 + 一枚「刷新」。
			//
			// ⚠️ **排序是次级排序**：用框架那个下划线的 `ui.Tabs`，不是主 Tab
			// 那一套轨道 + 滑块（票 #138：主 Tab 和次级排序不是同一个样子）。
			ui.Row(c).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
				ui.Tabs(c, &p.sortIdx, commentsSortLabels...)
				// 🔴 **刷新只记意图**（铁律三）：真正发请求的是 `syncComments`。
				if s.withTip(c, iconKey(c, iconRefresh, "刷新评论", iconMd, false).
					Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7),
					"刷新评论").Clicked() {
					p.wantRefresh = true
				}
			})
		})

		// 第二行：作者 / UP 名 + 总条数 + 那一枚「只看提到歌名的」。
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(space.Px12).Children(func() {
			ui.Text(c, commentsAuthorLine(it)).FontSize(scale.Font(c, scale.Size13)).
				TextColor(t.TextMuted).MaxLines(1).Ellipsis("…").Grow(1)
			// ⚠️ **筛选开着的时候这一行换一句话**：摆「共 N 条」而列表里只
			// 有筛选后的那几条，用户会以为另外那些评论不见了。
			switch {
			case p.mentionOnly && p.hits > 0:
				ui.Text(c, strconv.Itoa(p.hits)+" 条提到这首歌").
					FontSize(scale.Font(c, scale.Size12)).
					TextColor(t.TextMuted).MaxLines(1)
			case s.app.CommentsAllCount() > 0:
				// ⚠️ **一条都没命中时还是摆总数**：那一档空状态里已经说了
				// 「这个视频的 N 条评论里没有提到这首歌」，而这里摆的是同一个
				// 数（两处说的是同一件事）。
				n := s.app.CommentsAllCount()
				ui.Text(c, "共 "+strconv.FormatInt(n, 10)+" 条").FontSize(scale.Font(c, scale.Size12)).
					TextColor(t.TextMuted).MaxLines(1)
			}
			s.viewCommentsMentionSwitch(c, p)
		})
	})
}

// viewCommentsMentionSwitch 是页头那一枚「只看提到歌名的」开关（票 #574）。
//
// 🔴 **默认是关的**（`commentsPage.mentionOnly` 的零值）：这一页默认摆的是
// 这一首的**全部**评论——筛选是用户自己挑出来的窄视图。
//
// ⚠️ **开关自己一个字都没有**（它是 `springSwitch`），所以旁边的字是给它
// 兜着的；而它自己的 `Label` 是给屏幕阅读器和无头 Tester 用的（框架那一颗
// 开关没有可见文字）。
//
// ⚠️ **左边是字、右边是开关**：跟设置页和满屏偏好面板上那些开关是同一副
// 样子（`fullSwitchRow`：「左边标题，右边开关」）——同一个控件只有一种样子。
//
// ⚠️ **这一枚不许加 Key**：开关的旋钮是**动画**出来的（`switch.go` 顶上那一
// 段），而动画记在节点的键上——每帧换一个新节点，动画每帧从头开始。
func (s *Shell) viewCommentsMentionSwitch(c *ui.Context, p *commentsPage) {
	t := c.Theme()
	// ⚠️ **这一首没有名字就没法筛**：那时候按下去只会把评论全筛掉，而界面
	// 上说不清为什么（空表不筛，见 `songname.Table.Empty`）。
	noName := p.names.Empty()
	ui.Row(c).Gap(space.Px6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "只看提到歌名的评论").FontSize(scale.Font(c, scale.Size12)).
			TextColor(t.TextMuted).MaxLines(1)
		// ⚠️ **开关是在这个回调里建的**：框架把建出来的元素挂到**当前正在
		// 建的那一格**上（`ui.SwitchBase` 自己会挂），而这一格是上面那个
		// Row——在外面建的话它就成了外层那一列的孩子。
		sw := springSwitch(c, &p.mentionOnly).Label("开关：只看提到歌名的评论")
		if noName {
			sw = sw.Disabled(true)
			// ⚠️ **按灰的时候要说清为什么**（`withTip`）：一个按不动的开关，
			// 不说理由的话用户只以为这个应用有时候控件会坏。
			// 而 `withTip` 返回的就是锚点本身（提示挂在它身上），所以返回值
			// 可以丢——元素已经在树里了。
			s.withTip(c, sw, "这一首没有名字，没法按歌名筛")
		}
	})
}

// commentsAuthorLine 是页头第二行的那句话。
//
// ⚠️ **两种条目两种说法**：`mv` 是「UP：xxx」（那一栏就是 UP 名），
// `audio` 是「作者：xxx」。
func commentsAuthorLine(it app.Item) string {
	if it.Artist == "" {
		return ""
	}
	switch it.Type {
	case "mv":
		return "UP：" + it.Artist
	case "audio":
		return "作者：" + it.Artist
	}
	return it.Artist
}

// viewCommentsScopeBadge 是页头那一枚角标：**「这首歌的评论」** /
// **「这个视频的评论」**。
//
// 🔴 **两句话不许合并**：它们说的是两件不同的事（研究票 #537 报告 §4 实测
// 两种条目的评论语义不同），而合成一句「评论」的话，用户分不出自己在读
// 歌的评论还是视频的评论。
func (s *Shell) viewCommentsScopeBadge(c *ui.Context, it app.Item) {
	t := c.Theme()
	label := ""
	switch it.Type {
	case "audio":
		label = "这首歌的评论"
	case "mv":
		label = "这个视频的评论"
	default:
		return
	}
	ui.Box(c).Key("comments:scope").Radius(t.Radius).Background(t.Surface).
		Padding(space.Px4, space.Px8).Shrink(0).Children(func() {
		// 🔴 **这一句不许被挤掉**（视觉审查量出来的）：标题很长的时候（超长
		// 视频标题那一档），它是**整页最要紧的一句话**——「这是这个视频的评论」
		// 还是「这是这首歌的评论」——而它被截成「这个视频的…」的话，那句话就
		// 没了。`Shrink(0)` 是「不许缩」，于是**该截的是标题**（它自己带
		// 省略号），不是这一句。
		ui.Text(c, label).FontSize(scale.Font(c, scale.Size12)).TextColor(t.Text).
			MaxLines(1).Shrink(0)
	})
}

// viewCommentRow 画一条评论（主评论或者楼中楼）。
func (s *Shell) viewCommentRow(c *ui.Context, p *commentsPage, row commentRow) {
	t := c.Theme()
	cm := row.cm

	if row.reply {
		s.viewCommentReplyRow(c, p, row)
		return
	}

	ui.Row(c).Key(row.elemKey).
		AlignSelf(ui.Stretch).AlignItems(ui.Start).Gap(commentBodyGap).
		Padding(space.Px4, space.None).Children(func() {
		// 头像：B 站的评论头像是圆的。
		imgcache.ViewRadius(c, s.images, commentImageURL(cm.Avatar),
			commentAvatar, commentAvatar, commentAvatar/2)

		ui.Column(c).Grow(1).MinWidth(0).Gap(space.Px6).Children(func() {
			// 第一行：昵称 + 等级 + 徽章 + 时间。
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(space.Px6).Children(func() {
				ui.Text(c, cm.Uname).FontSize(scale.Font(c, scale.Size14)).TextColor(t.Text).
					MaxLines(1).Ellipsis("…")
				// ⚠️ **徽章是「一段带底色的字」，不是一个盒子套一段字**：
				// 后者要多一个闭包（= 一次分配），而这一行每一帧都要重建一次
				// ——一屏十来行就是十来次（理由同 `framebudget_test.go` 盯着的
				// 「每帧现算」那族坏处）。
				if row.levelText != "" {
					ui.Text(c, row.levelText).FontSize(scale.Font(c, scale.Size11)).
						TextColor(t.TextMuted).MaxLines(1).
						Background(t.Surface).Radius(space.Px2).
						Padding(space.Px2, space.Px4)
				}
				// 🔴 **「UP 主」只按 `IsUpper`**（`mid == upper.mid`）——
				// **不是** `reply_control.up_reply`（那个是「UP 主回复过这一条」，
				// 实测拿它当判据会给几十条普通用户的评论挂上 UP 主标记）。
				if cm.IsUpper {
					s.viewCommentBadge(c, "UP 主", t.Accent, t.AccentText)
				}
				// 官方认证：`member.official_verify.type`（-1 = 没有）。
				if cm.OfficialType >= 0 {
					s.viewCommentOfficial(c, cm)
				}
				ui.Spacer(c)
				if cm.TimeDesc != "" {
					ui.Text(c, cm.TimeDesc).FontSize(scale.Font(c, scale.Size12)).
						TextColor(t.TextMuted).MaxLines(1)
				}
			})

			// 正文（表情占位符换成了图）。
			s.viewCommentBodyEl(c, p, cm, commentBodyLines, s.commentBodyWidth(0))

			// 第三行：点赞数 + 回复数。
			ui.Row(c).Gap(space.Px16).AlignItems(ui.Center).Children(func() {
				s.viewCommentLike(c, row.likeText)
				s.viewCommentReplies(c, p, row)
			})
		})
	})
}

// viewCommentReplyRow 画一条楼中楼。
//
// ⚠️ **它是只读的**：这一页**不发任何写接口**（票 #573 的纪律），所以楼中楼
// 摆的是「看」，不是「回」。
//
// ⚠️ **缩进 12**（`space.Px12`）：楼中楼比主评论往里让一格，于是「它是回复
// 哪一条」一眼看得出（而头像本身小一号，24 而不是 32）。
func (s *Shell) viewCommentReplyRow(c *ui.Context, p *commentsPage, row commentRow) {
	t := c.Theme()
	cm := row.cm

	ui.Row(c).Key(row.elemKey).
		AlignSelf(ui.Stretch).AlignItems(ui.Start).Gap(space.Px8).
		MarginX(space.Px12).Children(func() {
		imgcache.ViewRadius(c, s.images, commentImageURL(cm.Avatar),
			commentReplyAvatar, commentReplyAvatar, commentReplyAvatar/2)
		ui.Column(c).Grow(1).MinWidth(0).Gap(space.Px4).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(space.Px6).Children(func() {
				ui.Text(c, cm.Uname).FontSize(scale.Font(c, scale.Size13)).TextColor(t.Text).
					MaxLines(1).Ellipsis("…")
				if cm.IsUpper {
					s.viewCommentBadge(c, "UP 主", t.Accent, t.AccentText)
				}
				if cm.OfficialType >= 0 {
					s.viewCommentOfficial(c, cm)
				}
				ui.Spacer(c)
				if cm.TimeDesc != "" {
					ui.Text(c, cm.TimeDesc).FontSize(scale.Font(c, scale.Size11)).
						TextColor(t.TextMuted).MaxLines(1)
				}
			})
			s.viewCommentBodyEl(c, p, cm, commentReplyBodyLines, s.commentBodyWidth(1))
		})
	})
}

// viewCommentBadge 是一枚小徽章（「UP 主」）。
//
// ⚠️ **它也是「一段带底色的字」**：理由同 `viewCommentRow` 里那一段。
//
// 🔴 **主色底上的字取 `t.AccentText`，不是 `inkAccentText`**（视觉审查量出来
// 的）：`ink.AccentOn` 答的是「**主色当字色**压在页面上读不读得出」，它给出
// 的是主色的**深**一档；而这里是「**主色当底色**」——深字压深底，那句话就
// 看不见了（实测浅色下深橙压深橙，深色下主色压主色，整枚是空的）。
// 「底是主色、字是 `t.AccentText`」是仓库里已经拍板的那一档（`flatbutton.go`
// 的 `flatSolid`）。
func (s *Shell) viewCommentBadge(c *ui.Context, label string, bg, fg ui.Color) {
	ui.Text(c, label).FontSize(scale.Font(c, scale.Size11)).TextColor(fg).MaxLines(1).
		Background(bg).Radius(space.Px2).Padding(space.Px2, space.Px4)
}

// viewCommentOfficial 是「官方认证」那一枚徽章。
//
// ⚠️ **判据是 `member.official_verify.type`**：-1 = 没有认证，0 = 个人认证，
// 1 = 机构认证。B 站给的那句说明（「某某官方账号」）放在提示里。
func (s *Shell) viewCommentOfficial(c *ui.Context, cm bili.Comment) {
	t := c.Theme()
	tip := cm.OfficialDesc
	if tip == "" {
		tip = "官方认证"
	}
	s.withTip(c, ui.Text(c, "官方认证").FontSize(scale.Font(c, scale.Size11)).
		TextColor(t.TextMuted).MaxLines(1).
		Background(t.Surface).Radius(space.Px2).Padding(space.Px2, space.Px4), tip)
}

// viewCommentLike 是点赞数。
//
// ⚠️ **图标走 `likeIcon`，不直接摆 `iconLike`**：状态图标只有那四个函数是
// 出口（`icons_test.go` 的
// `TestTheStateIconsOnlyReachTheScreenThroughTheirFunction` 盯着这条）——
// 绕过去的话，「哪一档配哪一张」那张表就管不到画面上。
// 而这一页**是只读的**（赞一条评论要打写接口，这一票不打），所以恒是
// **没赞过**那一档（空心）。
func (s *Shell) viewCommentLike(c *ui.Context, likeText string) {
	t := c.Theme()
	ui.Row(c).Gap(space.Px4).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon(likeIcon(false))).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, likeText).FontSize(scale.Font(c, scale.Size12)).
			TextColor(t.TextMuted).MaxLines(1)
	})
}

// viewCommentReplies 是那一枚「共 N 条回复」——**点它展开楼中楼**。
//
// ⚠️ **点它只改界面状态**（`p.expanded`）：**不发请求**（铁律三）。真正去问
// 楼中楼的是 `syncComments`（它下一帧建行时调 `CommentReplies`，那一句自己
// 会把请求递到后台）。
func (s *Shell) viewCommentReplies(c *ui.Context, p *commentsPage, row commentRow) {
	t := c.Theme()
	if row.replyText == "" {
		return
	}
	open := p.expanded[row.cm.Rpid]
	if commentTextButton(c, row.replyText, scale.Font(c, scale.Size12), t.TextMuted).Clicked() {
		if p.expanded == nil {
			p.expanded = map[int64]bool{}
		}
		p.expanded[row.cm.Rpid] = !open
		p.expRev++
	}
}

// commentTextButton 是「一句话按钮」：一块可点的小字（回复数、失败重试）。
//
// ⚠️ **不能直接 `flatButton(...).FontSize(...)`**：`flat.Button` 内部画的是
// `ui.Text(c, label).SingleLine()`，**字号和颜色不继承按钮上的**——于是两者
// 都得在**内容里**给（这就是 `flat.ButtonContent` 存在的理由，见
// `widgets/flat` 的注释）。
func commentTextButton(c *ui.Context, label string, size float32, color ui.Color) ui.Element {
	return flat.ButtonContent(c, func(c *ui.Context) {
		ui.Text(c, label).FontSize(size).TextColor(color).MaxLines(1)
	})
}

// viewCommentsSentinel 是列表末尾那一行。
//
// ⚠️ **它就是拉页的触发器**：这一行进画面了，就说明用户滚到底了（或者容器
// 没填满）⇒ 该拉下一页了。那件事由 `syncComments` 做（理由见文件头）。
func (s *Shell) viewCommentsSentinel(c *ui.Context, p *commentsPage) {
	t := c.Theme()
	switch {
	case s.app.CommentsErr() != nil:
		ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(space.Px8).Padding(space.Px8).Children(func() {
			if commentTextButton(c, "加载失败，点此重试", scale.Font(c, scale.Size13), t.Text).Clicked() {
				p.wantRefresh = true
			}
		})
	case s.app.CommentsLoading():
		// 🔴 **「正在拉」要有样子**（验收项）：一枚转着的圈 + 一句话。
		ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(space.Px8).Padding(space.Px8).Children(func() {
			ui.Spinner(c).Label("正在拉")
			ui.Text(c, "正在拉…").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
		})
	case s.app.CommentsHasMore():
		// 还有下一页：这一行**什么都不画**（它是触发器，不是内容）。
		ui.Box(c).Height(space.Px8)
	default:
		ui.Text(c, "没有更多了").FontSize(scale.Font(c, scale.Size12)).
			TextColor(t.TextMuted).Padding(space.Px8)
	}
}

// viewCommentBodyEl 画一条评论的正文。
//
// 🔴 **表情占位符换成了图**（验收项）：正文里的 `[妙啊]` 画出来是**那张图**，
// 不是那五个字。
//
// ⚠️ **正文是「自己排版、自己画」的**（理由见文件头）：框架的文本控件给
// inline object 留了位置却**不画它**，而表情就靠那一格。
//
// ⚠️ **`.Label(...)` 把正文交给框架**：屏幕阅读器读得到它，无头 Tester 也
// 找得到它——不然这一页的正文对它们是不存在的。
//
// 🔴 **高亮只在筛选开着的时候做**（票 #684）：关着的时候这一条走的是**原来
// 那条路**——不跑那次匹配、不带任何底色（正文是**连续几行的字**，常驻的色块
// 会让用户以为自己是拿鼠标选中了几段）。
func (s *Shell) viewCommentBodyEl(c *ui.Context, p *commentsPage, cm bili.Comment, lines int, w float32) {
	body := s.commentBodyOf(c, p, cm, lines, w)
	if body == nil {
		return
	}
	ui.Box(c).FillWidth().Height(body.h).Label(body.plain).DrawOver(body.paint)
}

// commentBodyOf 拿一条评论正文的排版结果（**排一次，缓存着**）。
//
// ⚠️ **排版不便宜**（`text.Shared().Shape`），而这一页每一帧都要画十几条
// 评论——每帧重排一遍就是十几倍的浪费。所以按 (rpid, 宽度, 行数) 记一份。
//
// 🔴 **命中区间也在这一次排版里算掉**（票 #684）：`songname.Table.Matches`
// 每条评论跑一次就够（排版本来就缓存着），而这里是**唯一**需要它的地方
// ——所以它是**排的时候**跑一趟，不是每帧跑。筛选关着的时候**根本不跑**
// （高亮是筛选的一部分，见 `viewCommentBodyEl`）。
func (s *Shell) commentBodyOf(c *ui.Context, p *commentsPage, cm bili.Comment, lines int, w float32) *commentBody {
	// 🔴 **高亮的配色和区间跟着排版一起缓存**（见 `commentBody.mark`）：
	// 主题变了（深浅 / 主色）那一份要重排，因为高亮那几段的字色和底色是**烘进
	// span 里**的。
	//
	// ⚠️ **配色是 `viewComments` 开头算好的那一份**（一帧一次）：这里只是取
	// 过来用——每一条评论各算一遍 `ink.Ensure` 是白花的。
	//
	// ⚠️ **筛选关着时 mark 是零值**：这时候 `Matches` 也不跑。
	mark := p.mark
	// 🔴 **缓存先查、匹配后跑**：`Matches` 每条评论分配十几次（它要拼一份归一化
	// 串 + 一张映射表），而这一页每一帧都要问一遍每一条评论——先查缓存的话，
	// 稳态下（正文排好了）它**一次都不跑**。缓存键里的 `mark` 已经能答「这一份
	// 是不是同一个配色下排的」，所以命中就说明区间也是同一批。
	if b, ok := p.bodies[cm.Rpid]; ok && b.w == w && b.lines == lines && b.mark == mark {
		return b
	}
	var hl []songname.Range
	if mark.Bg.A != 0 && !p.names.Empty() {
		hl = p.names.Matches(cm.Message)
	}
	b := shapeCommentBody(c, cm, w, lines, hl, mark)
	// 🔴 **画这一格的那一支记在这一份里**（而不是每帧现建一个闭包）：
	// 这一格每一帧都要画，而闭包每帧新建一次就是一次分配——一屏十来行就是
	// 来十次（理由同 `framebudget_test.go` 盯着的「每帧现算」那族坏处）。
	//
	// ⚠️ **颜色在里面现取**（`pr.Theme()`）：这一份缓存跨帧活着，而主题是
	// 用户能改的——写死颜色的话，用户换了深浅两套之后正文还是旧的颜色。
	cache := s.images
	b.paint = func(pr *ui.Painter, r ui.Rect) {
		pr.TextLayout(b.layout, r.X, r.Y, pr.Theme().Text)
		for i, o := range b.objects {
			if i >= len(b.emotes) {
				break
			}
			st := cache.Get(b.emotes[i])
			if st.Bitmap == nil {
				// 图还没下来：那一格先空着（框架的 inline object 本来就不画
				// 任何东西），图好了缓存会叫醒这一扇窗。
				continue
			}
			pr.Image(st.Bitmap, ui.Rect{X: r.X + o.Rect.X, Y: r.Y + o.Rect.Y,
				W: o.Rect.W, H: o.Rect.H}, ui.Contain)
		}
	}
	if p.bodies == nil {
		p.bodies = map[int64]*commentBody{}
	}
	p.bodies[cm.Rpid] = b
	return b
}

// commentBodyWidth 是正文那一列有多宽。
//
// ⚠️ **按容器宽算，不按内容量算**：正文的宽度决定它在哪几个字那里折行，
// 而折行位置决定这一行多高——**宽度错了，整张列表的高度就错了**。
//
// ⚠️ **量的是上一帧的宽**（`list.Bounds()` 报的是上一帧的盒子）：这一帧
// 还没排版，拿不到这一帧的宽。差一帧的代价是刚进这一页时正文按一个猜的
// 宽度排一次，下一帧就对了。
//
// kind 是 0（主评论）还是 1（楼中楼）：楼中楼往左缩进，还用小一号的头像，
// 于是它那一列更窄。
func (s *Shell) commentBodyWidth(kind int) float32 {
	w := s.comments.listW
	if w <= 0 {
		// 还没量过容器：先按一个常见的宽度排，下一帧重排。
		w = 640
	}
	if kind == 0 {
		// 减去头像和它后面那道缝。
		return w - commentAvatar - commentBodyGap
	}
	// 楼中楼：缩进 + 小头像 + 它自己的缝。
	return w - space.Px12*2 - commentReplyAvatar - space.Px8
}

// shapeCommentBody 把一条评论的正文排成版。
//
// ⚠️ **表情占位符换成一枚 inline object**（`Span.ObjectWidth` /
// `ObjectHeight`）：框架的文本引擎给那枚 U+FFFC 留一个 18×18 的位置，而
// **它自己一个字都不画**（`mygo/ui/paint.go` 跳过 object 字形）——所以那一格
// 是空的，得我们自己把那张表情图画上去。
//
// hl 是正文里命中歌名的那些字符区间（按**原文**的 rune，见 `songname.Range`），
// mark 是它们的画法。两样都为零值时，排出来的就是原来那一份。
//
// 🔴 **截断之后要重新裁一遍区间**：截断点之后的那一截正文根本不在画面上
// （换成了省略号），留着它只会去切一段不存在的文字。
func shapeCommentBody(c *ui.Context, cm bili.Comment, w float32, lines int, hl []songname.Range, mark commentMark) *commentBody {
	font := ui.Font{Family: c.Theme().Font, Size: scale.Font(c, scale.Size14)}
	m := font.Metrics()
	lineH := m.Ascent + m.Descent + m.LineGap
	maxH := float32(lines) * lineH

	spans, emotes, plain := commentSpansMarked(cm.Message, cm.Emotes, hl, mark)
	layout := ui.ShapeRichText(spans, font, w)
	_, h := layout.Size()
	if h > maxH+0.5 {
		// 🔴 **太长了就截断**（验收项：「一条评论被折叠时（正文很长）不破版」）。
		//
		// 截断的办法是**二分**：按字符数试排，看排出来有几行。二分是
		// O(log n) 次排版，而这一次排版的结果是**记着的**（见 `commentBodyOf`），
		// 所以代价只付一次。
		runes := []rune(cm.Message)
		// 🔴 **截断点不许落在表情占位符里面**（理由见 `safeCommentCut`）。
		ranges := commentEmoteRanges(cm.Message, cm.Emotes)
		lo, hi := 0, len(runes)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			// ⚠️ **量的是「安全切点」那一段**：切点落在占位符里面的话，量出来
			// 的那一份和最后摆出来的那一份不是同一句话（一个是半个占位符的
			// 字面量，一个是整枚表情）——二分就会照着一个不存在的东西收敛。
			//
			// ⚠️ **量的时候不带高亮**：底色和字色**不改字宽**（`Span.Background`
			// 画在字形后面），所以带不带它排出来的行数一模一样——而带上它的话
			// 每一趟二分都要重切所有 span，白花。
			sp, _, _ := commentSpans(string(runes[:safeCommentCut(ranges, mid)]), cm.Emotes)
			sp = append(sp, ui.Span{Text: "…"})
			l := ui.ShapeRichText(sp, font, w)
			_, hh := l.Size()
			if hh <= maxH {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		cut := safeCommentCut(ranges, lo)
		// 🔴 **区间按新的 rune 数裁**（截断点之后的高亮丢掉）。
		hl = clipMarks(hl, cut)
		spans, emotes, plain = commentSpansMarked(string(runes[:cut]), cm.Emotes, hl, mark)
		spans = append(spans, ui.Span{Text: "…"})
		plain = plain + "…"
		layout = ui.ShapeRichText(spans, font, w)
	}
	_, h = layout.Size()
	return &commentBody{
		layout:  layout,
		objects: layout.Objects(),
		emotes:  emotes,
		plain:   plain,
		w:       w,
		h:       h,
		lines:   lines,
		lineH:   lineH,
		mark:    mark,
	}
}

// commentSpans 把正文拆成一段一段：**文字是文字，表情占位符换成一枚
// inline object**。
//
// 返回的第二样是那些表情图的地址，**与 inline object 一一对应**（按出现的
// 顺序）——画的时候靠这个对应关系把图贴到位置上。第三样是**给辅助技术和
// 无头 Tester 的那份文字**（表情占位符换成「［表情］」）。
//
// 🔴 **认占位符的口径只有这一处**：截断那一档（`safeCommentCut`）要认的是
// **同一批**占位符——认错了的话，截断会把一段普通文字里的方括号也当成表情
// 切掉。
//
// ⚠️ **高亮是这一支的另一档**（`commentSpansMarked`）：`hl` 为空、`mark` 是
// 零值的时候，两处的 span 逐段相同——所以「认占位符」仍然只有这一处。
func commentSpans(msg string, emotes map[string]bili.CommentEmote) ([]ui.Span, []string, string) {
	return commentSpansMarked(msg, emotes, nil, commentMark{})
}

// commentSpansMarked 是 `commentSpans` 加「按区间把命中的那几段染上底色」
// （票 #684）。
//
// 🔴 **表情占位符那一格不染色**：`hl` 用的是**原文**的 rune 坐标（
// `songname.Range`），所以一枚表情占位符 `[妙啊]` 可能整个落在命中区间里——
// 而这里的切法是**先按占位符切成 token**，文字段各自按自己的起点套色 ⇒ 那一格
// 自然让出去（画出来是一张图，不是一块有底色的字）。
//
// ⚠️ **文字段的起点按原文的 rune 数累加**（`base`）：`plain` 里表情换成
// 「［表情］」四个字，长度和原文的 `[妙啊]` 对不上——所以坐标只跟**原文**走。
func commentSpansMarked(msg string, emotes map[string]bili.CommentEmote, hl []songname.Range, mark commentMark) ([]ui.Span, []string, string) {
	var spans []ui.Span
	var urls []string
	var plain strings.Builder
	// base 是 rest 之前已经过去了多少个**原文的** rune。
	base := 0
	text := func(s string) {
		if s == "" {
			return
		}
		spans = appendMarkedText(spans, s, base, hl, mark)
		plain.WriteString(s)
		base += utf8.RuneCountInString(s)
	}
	rest := msg
	for len(rest) > 0 {
		i := strings.IndexByte(rest, '[')
		if i < 0 {
			text(rest)
			break
		}
		if i > 0 {
			text(rest[:i])
		}
		j := strings.IndexByte(rest[i:], ']')
		if j < 0 {
			// 没有收尾的 ']'：后面整段都是普通文字。
			text(rest[i:])
			break
		}
		token := rest[i : i+j+1]
		e, ok := emotes[token]
		if !ok {
			// 不是表情：这一段就是普通文字。
			text(token)
			rest = rest[i+j+1:]
			continue
		}
		spans = append(spans, ui.Span{Text: "\uFFFC", ObjectWidth: commentEmote, ObjectHeight: commentEmote})
		urls = append(urls, commentImageURL(e.URL))
		plain.WriteString("［表情］")
		base += utf8.RuneCountInString(token)
		rest = rest[i+j+1:]
	}
	return spans, urls, plain.String()
}

// commentEmoteRange 是正文里一枚表情占位符占的字符区间（**按 rune 数**，
// 左闭右开）。
type commentEmoteRange struct {
	start int
	end   int
}

// commentEmoteRanges 找出正文里那些**表情占位符**占的字符区间。
//
// 🔴 **口径必须和 `commentSpans` 一模一样**（那里怎么认占位符，这里就怎么
// 认）：那一处是「`[` 到下一个 `]` 之间的那一段，**在表情表里**」才算占位符。
//
// ⚠️ **区间按 rune 数算**：截断是按 rune 切的（`[]rune`），而按字节算的
// 区间跟切点对不上——中文一个字三个字节。
func commentEmoteRanges(msg string, emotes map[string]bili.CommentEmote) []commentEmoteRange {
	var out []commentEmoteRange
	rest := msg
	// base 是 rest 之前已经过去了多少个 rune。
	base := 0
	for len(rest) > 0 {
		i := strings.IndexByte(rest, '[')
		if i < 0 {
			break
		}
		j := strings.IndexByte(rest[i:], ']')
		if j < 0 {
			break
		}
		token := rest[i : i+j+1]
		head := utf8.RuneCountInString(rest[:i])
		tok := utf8.RuneCountInString(token)
		if _, ok := emotes[token]; ok {
			out = append(out, commentEmoteRange{
				start: base + head,
				end:   base + head + tok,
			})
		}
		// ⚠️ **不管是不是表情，都从这枚 token 后面接着找**：那一段已经被
		// `commentSpans` 吃掉了（不是表情就是普通文字），找法得一样。
		rest = rest[i+j+1:]
		base += head + tok
	}
	return out
}

// safeCommentCut 把截断点**挪到占位符外面**。
//
// 🔴 **切点不许落在占位符里面**：落在里面的话，正文里会剩下半个 `[妙啊`——
// 表情不再成图，而正文里出现字面量（那是「表情成图」那条验收直接破掉的
// 形状，评审在票 #573 上抓到过）。
//
// ⇒ 落在里面就**退回占位符的开头**（整枚占位符不出现），而不是留在半个
// 占位符上。退回只会让正文更短 ⇒ 高度更小 ⇒ 「最多几行」那一档还成立。
//
// ⚠️ **切在占位符的前后都合法**：切在开头 = 整枚不出现，切在结尾 = 整枚
// 出现（表情成图）。
func safeCommentCut(ranges []commentEmoteRange, n int) int {
	for _, r := range ranges {
		if n > r.start && n < r.end {
			return r.start
		}
	}
	return n
}

// commentImageURL 把 B 站给的地址补成能直接拉的。
//
// ⚠️ **B 站给的是 `http://`**（表情图和配图），而我们只拉 https——照抄那个
// 地址的话，图永远拉不下来（而界面上只是那一格空着）。
func commentImageURL(raw string) string {
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return imgcache.FormatURLProtocol(raw)
}

// inkAccentText 是主色底上的字色（浅色下不是白的）。
func inkAccentText(c *ui.Context) ui.Color {
	return ink.AccentOn(c.Theme())
}

// syncComments 是评论页每帧的**副作用**。
//
// 🔴 **三件要发网络请求的事全在这里**（铁律三）：排序切换 / 刷新 / 翻页，
// 外加展开楼中楼之后去问那一层。view 一件都不做（文件头）。
//
// # 翻页那三重保障在这里是**同一份判据**
//
//	① 首次渲染检查：内容一换就查一次容器填没填满
//	② 滚到底：末尾那一行（哨兵）进了画面就拉下一页
//	③ 加载结束后再查一次
//
// ⚠️ **判据复用 `widgets/list/paged.go` 里那一份**（`DecideFirstPage` /
// `DecideNextPage`）：这一页的底座不一样（行高是量出来的），但「这一帧该不该
// 拉下一页」判的是同一件事——**两份判据迟早分叉**，而分叉出来的那种差异极难
// 查（一个列表会补屏，另一个不会，而且两边看着都「对」）。
//
// 三条都是「**末尾那一行进没进画面**」——`ListState.Visible()` 报的是**上一帧**
// 的行（`mygo/ui/list.go` 的 `Visible` 注释），而这个判据每帧问一次，
// 于是①和③是它自己成立的（内容短于容器时哨兵一直看得见；新一页到了以后
// 下一帧再问一次）。
func (s *Shell) syncComments(c *ui.Context) {
	p := &s.comments

	// 换歌了：清掉上一首的缓存（rpid 会撞，正文排版也不能留着）。
	slot := s.app.CommentsSlot()
	if slot != p.slot {
		p.slot = slot
		p.bodies = nil
		p.rows = nil
		p.srcLen = -1
		p.expanded = nil
		p.expRev++
	}

	// 🔴 **刷新**：view 只记了意图，真正发出去的是这里。
	if p.wantRefresh {
		p.wantRefresh = false
		p.bodies = nil
		p.srcLen = -1
		p.rows = nil
		s.app.RefreshComments()
	}

	// 排序：第一次进来时**认应用层那一档**（用户上次挑的那一档是跨歌记着的，
	// 而 `sortIdx` 的零值是最热——不认一次就把用户的选择盖掉了）。
	cur := s.app.CommentsSort()
	if !p.sortInit {
		p.sortInit = true
		p.sortIdx = commentsSortIndex(cur)
	}
	// 用户点了另一档：`ui.Tabs` 在**建到的时候**才写 `p.sortIdx`，所以这里
	// 读到的是上一帧的那一次点击。
	if p.sortIdx != commentsSortIndex(cur) {
		s.app.SetCommentsSort(commentsSortAt(p.sortIdx))
		// 换档之后**第一屏重新从第一条开始**（验收项）。
		p.state.ScrollTo(0, ui.Start)
		p.bodies = nil
		p.srcLen = -1
		p.rows = nil
		p.expanded = nil
		p.expRev++
	}

	// 别名表：这一首叫什么变了就重算（它只在建行和筛选用，所以不是每帧算）。
	//
	// ⚠️ **不每帧重算**：别名表是一次性的产物，而每帧重算就是每帧一次分配
	// （理由同 `framebudget_test.go` 盯着的「每帧现算」那族坏处）。
	if it, ok := s.app.CurrentItem(); ok &&
		(it.Title != p.srcTitle || it.Artist != p.srcArtist) {
		p.srcTitle, p.srcArtist = it.Title, it.Artist
		p.names = songname.New(it.Title, it.Artist)
		p.srcLen = -1
		// 🔴 **正文排版缓存也要清**（票 #684）：它的键里**没有别名表**
		// （`commentBodyOf` 按 rpid / 宽 / 行数 / 配色记）——留着的话，筛选用
		// 的是新歌名，正文里高亮的却还是旧歌名的那几段。
		//
		// ⚠️ **只在别名表真的换了时清**（就是这一支）：每帧清的话排版缓存整个
		// 作废，`framebudget_test.go` 那一档会红。而换歌本来就伴随
		// `p.srcLen = -1`（下一帧重建行），重排一次排版是同一件事的一部分。
		p.bodies = nil
		// 🔴 **换成一首没有名字的歌，筛选就做不了**：空表会把这一首的评论
		// 全筛掉（`songname.Table.Empty`），而界面上一句话都说不清为什么。
		//
		// ⇒ 把开关按回去（它那一枚也是按灰的，用户看得出按不动）。
		if p.names.Empty() && p.mentionOnly {
			p.mentionOnly = false
		}
	}

	// 🔴 **筛选开关变了**：重建一遍行（筛完的行数变了），并回到第一条。
	//
	// ⚠️ **它读到的是上一帧的那一次点击**（框架在**建到的时候**才写那个
	// bool，理由同 `sortIdx`）。
	if p.mentionOnly != p.srcMention {
		p.srcMention = p.mentionOnly
		p.srcLen = -1
		p.state.ScrollTo(0, ui.Start)
	}

	// 评论列表：问一次（`Comments()` 自己会保证在问），顺便把楼中楼也问上。
	cm := s.app.Comments()
	p.loaded = len(cm)
	// 🔴 **查完了没有**：那句「这个视频的 N 条评论里没有提到这首歌」只有查完
	// 了才说得出来（理由见 `commentsMentionCovered`）。
	p.covered = commentsMentionCovered(len(cm), s.app.CommentsAllCount(), s.app.CommentsHasMore())
	sig := 0
	for _, root := range p.expandedRoots() {
		sig += len(s.app.CommentReplies(root))
	}
	if len(cm) != p.srcLen || (len(cm) > 0 && cm[0].Rpid != p.srcFirst) ||
		cur != p.srcSort || p.expRev != p.rowsExpRev || sig != p.replySig {
		p.srcLen = len(cm)
		p.srcFirst = 0
		if len(cm) > 0 {
			p.srcFirst = cm[0].Rpid
		}
		p.srcSort = cur
		p.rowsExpRev = p.expRev
		p.replySig = sig
		p.rebuildRows(cm, s.app.CommentReplies)
	}

	// 🔴 **三重保障**：判据是 `widgets/list/paged.go` 里那一份（不另写一套）。
	//
	// ⚠️ **Blocked 恒为 false**：应用层那一档**没有**「挡住等人点重试」这个
	// 状态——失败之后它自己按 15 秒退避再问（`ensureComments` /
	// `LoadMoreComments`）。映成 true 的话，「翻页失败之后自动重试同一页」这
	// 条就没有了（用户只能点末尾那行的「重试」，而那一键走
	// `RefreshComments` ⇒ 回到第一页 ⇒ 体验倒退）。
	st := list.PagingState{
		// 「已经拿到过至少一页」——**一条评论都没有也算拿到过**（那一档是
		// 「还没有人评论」，不是「还没问过」）。
		Fetched: s.app.CommentsLoaded(),
		// 「有一趟在路上」：它挡的是重复发（应用层本来也拒，挡住只是少调
		// 一次）。
		Loading: s.app.CommentsLoading(),
		Blocked: false,
		HasMore: s.app.CommentsHasMore(),
	}
	// ① 首次渲染检查：还没拿到过第一页就去问（问的动作在应用层，界面只
	// 记下意图——铁律三）。
	if list.DecideFirstPage(st).Action == list.PagingLoadFirst {
		s.app.EnsureComments()
	}
	// ②③ 末尾那一行（哨兵）进了画面就拉下一页。
	//
	// ⚠️ **哨兵是列表多出来的那一行**（`len(p.rows)+1`），它的行号就是
	// `len(p.rows)`——和 `paged.go` 的口径一样（那边哨兵也占一行）。
	//
	// 🔴 **筛选开着而一条都没命中时，不走这一条**：那一档翻页是「筛选自己
	// 在翻」（`commentsMentionScanMax` 那个上限），不是「用户滚到底了」——
	// 两边都翻的话上限就是空的（列表空着，哨兵那一行一直看得见 ⇒ 一直翻）。
	//
	// 🔴 **判据是 `wantScan`（筛选开着而一条都没命中），不是 `scanning`**
	// （那一档**还没查完**）：哨兵那一路（用户自己滚到底了）在筛选开着而一
	// 条都没命中的时候**整个关掉**——查完了也一样（查完了就不用再翻，而它
	// 自己说的那句话是「N 条评论里没有提到这首歌」）。
	//
	// ⚠️ **查完了就不翻了**：查完的判据是「翻到的条数盖住了 B 站给的总数」
	// （`commentsMentionCovered`），不是「B 站说没有下一页了」——那一档说过
	// 头的话，这一句就不诚实。
	wantScan := p.mentionOnly && p.hits == 0 && !p.names.Empty()
	scanning := wantScan && !p.covered
	if !wantScan {
		first, last := p.state.Visible()
		if list.DecideNextPage(st, first, last, len(p.rows)+1).Action == list.PagingLoadNext {
			s.app.LoadMoreComments()
		}
	}
	// 🔴 **筛选开着而一条都没命中：把这一首的评论翻完**。
	//
	// 理由：那句话「这个视频的 N 条评论里没有提到这首歌」只有在**翻完**之后
	// 才说得出来（只翻了第一页就说「没有」，那是撒谎）。
	//
	// ⚠️ **发出去的是应用层那一趟**（铁律三）：这里只判「该不该翻」。
	if scanning && len(cm) < commentsMentionScanMax && s.app.CommentsHasMore() {
		s.app.LoadMoreComments()
	}
}

// rebuildRows 把评论摊平成行：一条主评论一行，它展开了就把楼中楼接在下面。
//
// ⚠️ **楼中楼是从 `replies` 那个函数拿的**，不在这里发请求（铁律三）：
// 应用层那一句 `CommentReplies` 自己会把请求递到后台。
//
// 🔴 **行身份必须唯一**（铁律四）：框架见**同一个父节点下有两个一样的 key**
// 就 panic（`ui: two elements under one parent have the key ...`）——而翻页
// 重叠会让同一条评论进两页（应用层那一层已经去过重了，这里再挡一道：
// **两行共用一份状态**和**整页变成错误页**，症状都比「多一行」严重得多）。
//
// 🔴 **筛选开着就只留「正文里提到这首歌」的那些**（票 #574）：`mv` 那一档
// 拿到的是**这个视频**的评论，里面大部分不是在聊这首歌（研究票 #537 报告
// §4.1 实测）——而这一枚开关是用户自己挑出来的窄视图。
//
// 🔴 **楼中楼按它自己的正文判**（票 #574 的评审）：
//
//	主评论提到了、楼中楼没提到   楼中楼**不摆**（它跟这首歌没关系）
//	主评论没提到、楼中楼提到了   主评论**照摆**（它是那段对话的上下文），
//	                          摆的是那条提到了的楼中楼
//
// ⚠️ **没展开的主评论看不到它的楼中楼**：楼中楼是**展开的时候**才问的
// （`replies` 那一趟请求），而问遍所有主评论的楼中楼就是几百趟请求（打的是
// B 站的服务器）——所以没展开的那些只按主评论自己的正文判。
//
// ⚠️ **空表不筛**：这一首没有名字的时候（`songname.Table.Empty`）按下去只会
// 把评论全筛掉，而界面上一句话都说不清为什么。
func (p *commentsPage) rebuildRows(cm []bili.Comment, replies func(int64) []bili.Comment) {
	rows := make([]commentRow, 0, len(cm))
	seen := make(map[int64]bool, len(cm))
	filter := p.mentionOnly && !p.names.Empty()
	hits := 0
	for _, c := range cm {
		// 楼中楼：**展开了的才有**（没展开的那一条我们没问过它）。
		var rs []bili.Comment
		if p.expanded[c.Rpid] {
			rs = replies(c.Rpid)
		}
		// ⚠️ **筛选开着的时候，楼中楼按它自己的正文留**（票 #574 的评审）。
		kept := rs
		rootHit := true
		if filter {
			rootHit = p.names.Mentions(c.Message)
			kept = nil
			for _, r := range rs {
				if p.names.Mentions(r.Message) {
					kept = append(kept, r)
					hits++
				}
			}
			// 🔴 **只有楼中楼提到了，主评论也照摆**：它是那段对话的上下文，
			// 而整条藏起来的话，用户看不到「有人在这条下面提到了这首歌」。
			if !rootHit && len(kept) == 0 {
				continue
			}
			if rootHit {
				hits++
			}
		}
		key := commentRowKey(c.Rpid, false)
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, newCommentRow(c, false, 0))
		for _, r := range kept {
			key := commentRowKey(r.Rpid, true)
			if seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, newCommentRow(r, true, c.Rpid))
		}
	}
	p.rows = rows
	p.hits = hits
}

// newCommentRow 建一行，**顺手把那一行要显示的字都算好**。
//
// ⚠️ **它们原来都是每帧现算的**（身份串、`LV6`、点赞数、回复数），而一屏
// 十来行就是**几十次分配**——理由同票 #313 / #339（见
// `internal/ui/framebudget_test.go` 那一档盯着的「每帧现算」）。
func newCommentRow(c bili.Comment, reply bool, root int64) commentRow {
	row := commentRow{
		key:      commentRowKey(c.Rpid, reply),
		cm:       c,
		reply:    reply,
		root:     root,
		elemKey:  "comment:" + strconv.FormatInt(c.Rpid, 10),
		likeText: strconv.FormatInt(c.Like, 10),
	}
	if reply {
		row.elemKey = "comment-reply:" + strconv.FormatInt(c.Rpid, 10)
	}
	if c.Level > 0 {
		row.levelText = "LV" + strconv.Itoa(c.Level)
	}
	// ⚠️ **回复数那一栏是「条件出现」的**（`reply_control.sub_reply_entry_text`）：
	// 实测 1064 条评论里只有 608 条带它。而**没有它但 `count` 不为零**的那些
	// 条也有楼中楼——所以文案自己拼一份（「共N条回复」）。
	if c.SubReplyEntryText != "" {
		row.replyText = c.SubReplyEntryText
	} else if c.Count > 0 {
		row.replyText = "共" + strconv.FormatInt(c.Count, 10) + "条回复"
	}
	return row
}

// expandedRoots 是「哪几条的楼中楼展开了」。
func (p *commentsPage) expandedRoots() []int64 {
	if len(p.expanded) == 0 {
		return nil
	}
	out := make([]int64, 0, len(p.expanded))
	for root, open := range p.expanded {
		if open {
			out = append(out, root)
		}
	}
	return out
}

// commentsSortIndex 报某一档在排序 Tab 里是第几个。
func commentsSortIndex(sort bili.CommentSort) int {
	for i, s := range bili.CommentSorts() {
		if s == sort {
			return i
		}
	}
	return 0
}

// commentsSortAt 报排序 Tab 里第几个是哪一档。
func commentsSortAt(i int) bili.CommentSort {
	sorts := bili.CommentSorts()
	if i < 0 || i >= len(sorts) {
		return sorts[0]
	}
	return sorts[i]
}
