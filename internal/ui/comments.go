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
// ⚠️ **拉页那三重保障也没有直接复用** `widgets/list/paged.go`：那一档的底座
// 是「音乐卡片 + 三档视图切换」，而这一页是**变高正文 + 表情图**。三条保障
// （首次渲染检查 / 滚到底 / 加载完再查一次）在这里是同一个判据（末尾那一行
// 进没进画面），自己写更直白——见 `syncComments`。
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
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/guard"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/flat"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
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
}

// viewComments 是评论页。
func (s *Shell) viewComments(c *ui.Context, _ *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题。
	s.applyFrameTheme(c)

	p := &s.comments
	it, has := s.app.CurrentItem()

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
// ⚠️ **四档是四句话**：正在拉 / 拉失败了 / 还没有人评论 / 压根没有评论。
// 混成一句话的话，用户读到「问不出评论」会以为歌没了，而歌好好地摆在那儿。
func (s *Shell) viewCommentsBody(c *ui.Context, p *commentsPage) {
	switch {
	case s.app.CommentsErr() != nil:
		s.viewLoadError(c, "comments:error", "这一首的评论", "重试",
			s.app.CommentsErr(), func() {
				// ⚠️ **这里只记意图**：发请求的是 `syncComments`（铁律三）。
				p.wantRefresh = true
			})
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

		// 第二行：作者 / UP 名 + 总条数。
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(space.Px12).Children(func() {
			ui.Text(c, commentsAuthorLine(it)).FontSize(scale.Font(c, scale.Size13)).
				TextColor(t.TextMuted).MaxLines(1).Ellipsis("…").Grow(1)
			if n := s.app.CommentsAllCount(); n > 0 {
				ui.Text(c, "共 "+strconv.FormatInt(n, 10)+" 条").FontSize(scale.Font(c, scale.Size12)).
					TextColor(t.TextMuted).MaxLines(1)
			}
		})
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
func (s *Shell) viewCommentLike(c *ui.Context, likeText string) {
	t := c.Theme()
	ui.Row(c).Gap(space.Px4).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon(iconLike)).FontSize(14).TextColor(t.TextMuted)
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
func (s *Shell) commentBodyOf(c *ui.Context, p *commentsPage, cm bili.Comment, lines int, w float32) *commentBody {
	if b, ok := p.bodies[cm.Rpid]; ok && b.w == w && b.lines == lines {
		return b
	}
	b := shapeCommentBody(c, cm, w, lines)
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
func shapeCommentBody(c *ui.Context, cm bili.Comment, w float32, lines int) *commentBody {
	font := ui.Font{Family: c.Theme().Font, Size: scale.Font(c, scale.Size14)}
	m := font.Metrics()
	lineH := m.Ascent + m.Descent + m.LineGap
	maxH := float32(lines) * lineH

	spans, emotes, plain := commentSpans(cm.Message, cm.Emotes)
	layout := ui.ShapeRichText(spans, font, w)
	_, h := layout.Size()
	if h > maxH+0.5 {
		// 🔴 **太长了就截断**（验收项：「一条评论被折叠时（正文很长）不破版」）。
		//
		// 截断的办法是**二分**：按字符数试排，看排出来有几行。二分是
		// O(log n) 次排版，而这一次排版的结果是**记着的**（见 `commentBodyOf`），
		// 所以代价只付一次。
		runes := []rune(cm.Message)
		lo, hi := 0, len(runes)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			sp, _, _ := commentSpans(string(runes[:mid]), cm.Emotes)
			sp = append(sp, ui.Span{Text: "…"})
			l := ui.ShapeRichText(sp, font, w)
			_, hh := l.Size()
			if hh <= maxH {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		spans, emotes, plain = commentSpans(string(runes[:lo]), cm.Emotes)
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
	}
}

// commentSpans 把正文拆成一段一段：**文字是文字，表情占位符换成一枚
// inline object**。
//
// 返回的第二样是那些表情图的地址，**与 inline object 一一对应**（按出现的
// 顺序）——画的时候靠这个对应关系把图贴到位置上。第三样是**给辅助技术和
// 无头 Tester 的那份文字**（表情占位符换成「［表情］」）。
func commentSpans(msg string, emotes map[string]bili.CommentEmote) ([]ui.Span, []string, string) {
	var spans []ui.Span
	var urls []string
	var plain strings.Builder
	rest := msg
	for len(rest) > 0 {
		i := strings.IndexByte(rest, '[')
		if i < 0 {
			spans = append(spans, ui.Span{Text: rest})
			plain.WriteString(rest)
			break
		}
		if i > 0 {
			spans = append(spans, ui.Span{Text: rest[:i]})
			plain.WriteString(rest[:i])
		}
		j := strings.IndexByte(rest[i:], ']')
		if j < 0 {
			// 没有收尾的 ']'：后面整段都是普通文字。
			spans = append(spans, ui.Span{Text: rest[i:]})
			plain.WriteString(rest[i:])
			break
		}
		token := rest[i : i+j+1]
		e, ok := emotes[token]
		if !ok {
			// 不是表情：这一段就是普通文字。
			spans = append(spans, ui.Span{Text: token})
			plain.WriteString(token)
			rest = rest[i+j+1:]
			continue
		}
		spans = append(spans, ui.Span{Text: "\uFFFC", ObjectWidth: commentEmote, ObjectHeight: commentEmote})
		urls = append(urls, commentImageURL(e.URL))
		plain.WriteString("［表情］")
		rest = rest[i+j+1:]
	}
	return spans, urls, plain.String()
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
// # 翻页那三重保障在这里是**同一个判据**
//
//	① 首次渲染检查：内容一换就查一次容器填没填满
//	② 滚到底：末尾那一行（哨兵）进了画面就拉下一页
//	③ 加载结束后再查一次
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

	// 评论列表：问一次（`Comments()` 自己会保证在问），顺便把楼中楼也问上。
	cm := s.app.Comments()
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

	// 🔴 **翻页**：末尾那一行进画面了就拉下一页。
	if _, last := p.state.Visible(); last >= len(p.rows) && s.app.CommentsHasMore() {
		s.app.LoadMoreComments()
	}
}

// rebuildRows 把评论摊平成行：一条主评论一行，它展开了就把楼中楼接在下面。
//
// ⚠️ **楼中楼是从 `replies` 那个函数拿的**，不在这里发请求（铁律三）：
// 应用层那一句 `CommentReplies` 自己会把请求递到后台。
func (p *commentsPage) rebuildRows(cm []bili.Comment, replies func(int64) []bili.Comment) {
	rows := make([]commentRow, 0, len(cm))
	for _, c := range cm {
		rows = append(rows, newCommentRow(c, false, 0))
		if p.expanded[c.Rpid] {
			for _, r := range replies(c.Rpid) {
				rows = append(rows, newCommentRow(r, true, c.Rpid))
			}
		}
	}
	p.rows = rows
}

// newCommentRow 建一行，**顺手把那一行要显示的字都算好**。
//
// ⚠️ **它们原来都是每帧现算的**（身份串、`LV6`、点赞数、回复数），而一屏
// 十来行就是**几十次分配**——理由同票 #313 / #339（见
// `internal/ui/framebudget_test.go` 那一档盯着的「每帧现算」）。
func newCommentRow(c bili.Comment, reply bool, root int64) commentRow {
	row := commentRow{
		key:      c.Rpid,
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
