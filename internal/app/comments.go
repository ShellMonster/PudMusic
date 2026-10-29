// 评论这一摊在应用层的样子：当前这一首的评论、楼中楼、以及它们的缓存与退避。
//
// # 和「分集」是同一个形状
//
// `App.Parts()` / `ensureParts()`（`internal/app/fullplayer.go`）那一套是本仓库
// 的样板：**后台拉 + 缓存 + 退避 + 叫醒界面**，界面每一帧调一个方法。
// 评论这一摊照它写——**别自己发明另一种「每帧问一次」的法子**，那样「换歌之后
// 上一首的评论还留在界面上」这个 bug 就有了第二处可能发生的地方。
//
// # 三种条目能查的东西不一样
//
//	mv     有 bvid，而评论接口 `type=1` 的 oid 要的是**数字 avid** ⇒ 先换一次
//	audio  有 sid，而 `type=14` 的 oid **就是 auid** ⇒ 直接用
//	local  什么都没有 ⇒ 不摆入口，**一个请求都不发**
//
// 🔴 **mv 那一路漏了 avid ⇒ 静默拿到空列表**：B 站把一个认不出来的 oid 判成
// 「这个对象没有评论」，而界面上的表现只是一个空列表——没有报错、没有提示。
// （研究票 #537 报告 §4.1 实测：音乐库音频 `type=14` 的评论是真的在聊这首歌。）
//
// # 迟到的结果要作废
//
// 换歌之后，上一首那一趟可能还在路上。它回来的时候要认一认「我现在问的
// 还是不是它」——判据是**槽位键 + 代次**（和 `lyricGen` 那套是同一个道理）。
// 对不上就丢：不然用户会看到「刚换的歌上面挂着上一首的评论」。
package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/comments"
)

// commentsRetryAfter 是问失败之后隔多久再问一次。
//
// 15 秒是「网络抖动能缓过来、而用户还在盯着这个窗口」的量级（和
// `partsRetryAfter` 同一条理由）：界面每一帧都调 `Comments()`，所以这个数同时
// 也是**重试的上限频率**。
const commentsRetryAfter = 15 * time.Second

// commentsPageSize 是主列表一页问多少条。
//
// 30 是 B 站给的上限（`bili.commentPageSizeMax`），再大它会默默截断。
const commentsPageSize = 30

// commentsReplyPageSize 是楼中楼一页问多少条。20 是 B 站给的上限。
const commentsReplyPageSize = 20

// CommentsCacheDirName 是评论缓存目录的名字，挂在用户数据目录下。
//
// ⚠️ 评论缓存没有外部兼容格式，这一份从头到尾都是我们自己写的，
// 所以放在应用数据目录底下一个平级的文件夹。
const CommentsCacheDirName = "comments"

// errCommentsUnavailable 是「评论这一摊没挂上」（没挂上 B 站客户端）。
var errCommentsUnavailable = errors.New("评论暂时打不开")

// 那三个替身的类型。做成类型是为了让「取出来递进 goroutine」这件事在签名上
// 看得见（而不是三个长得一样的裸 func 字段）。
type (
	// commentsListFetchFunc 是问一页评论那一趟。
	//
	// ⚠️ **那个 `next` 是偏移量（字符串），不是 `cursor.next`**——理由见
	// `bili.CommentList` 的注释与 `bili.CommentPage.NextOffset`。
	//
	// 🔴 **那个 `mode` 是排序档**（`bili.CommentSort` 的值）：界面上要能切
	// 「热门 / 最新」两档，而两档给的是**两份不同的评论**。签名里没有它的话，
	// 替身（测试里的假 B 站）就看不见用户到底要了哪一档——那样「切档」这件事
	// 在数据层根本验不了。
	commentsListFetchFunc func(context.Context, int64, int64, int, string) (bili.CommentPage, error)
	// commentsRepliesFetchFunc 是问一层楼中楼那一趟。
	commentsRepliesFetchFunc func(context.Context, int64, int64, int64) ([]bili.Comment, error)
	// commentsArchiveIDFetchFunc 是 bvid → avid 那一趟。
	commentsArchiveIDFetchFunc func(context.Context, string) (int64, error)
)

// commentResult 是一次拉回来的结果。
type commentResult struct {
	// comments 是这一页的评论。
	comments []bili.Comment
	// next 是下一页的游标（**偏移量**，B 站给什么就是什么，不透明）。
	next string
	// isEnd 是「到底了」。
	isEnd bool
	// allCount 是评论总数。
	allCount int64
	// noComments 是「这个对象在，只是还没有人评论」。
	noComments bool
	// loaded 是「这一页问成功了」。
	loaded bool
	// err 是问失败的原因。
	err error
	// append 是「接在已有的后面」而不是「换掉」。
	append bool
}

// ─────────────────────────────────────────────────────────────────────────────
// 界面那一侧要的那几个方法
// ─────────────────────────────────────────────────────────────────────────────

// CommentsWorthAsking 报「当前这一首有没有评论可问」。
//
// 判据是「这是一个 B 站的对象」：mv（有 bvid）或音乐库音频（有 sid）都摆入口，
// **本地文件不摆**。
//
// ⚠️ **本地文件一个请求都不发**：本地音乐在 B 站上没有对应的对象，而问一个
// 认不出来的 oid 的症状是「静默拿到空列表」——于是界面上会摆一个永远空着的
// 评论页。
func (a *App) CommentsWorthAsking() bool {
	it, ok := a.CurrentItem()
	if !ok || !it.isOnline() {
		return false
	}
	return commentSlotKey(it) != ""
}

// CommentsSort 报当前这一首的评论按**哪一档**排。
//
// 🔴 **它只有两档**（`bili.CommentSorts()`）：「热门评论」（`mode=3`）和
// 「最新评论」（`mode=2`）。
//
// ⚠️ **「评论」（`mode=1`）那一档不在里面**：实测它给的是「最新评论」的
// **同一份内容**（逐条比过 rpid），而 B 站的 `cursor.support_mode` 恒为
// `[2, 3]` 不含它——摆成两个档的话，用户切过去看到**一模一样的内容**，于是
// 以为「切了档但没生效」（理由见 `bili.CommentSortDefault` 的注释）。
func (a *App) CommentsSort() bili.CommentSort {
	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	return a.commentsSortLocked()
}

// commentsSortLocked 报当前那一档。**零值 = 「还没设」= 最热那一档**。
//
// ⚠️ 零值不是「没有排序」：B 站不认 `mode=0`（它会当成 `mode=3` 处理），
// 而「没指定」要落到一个**认得出来的**档上（见 `bili.normalizeCommentMode`）。
func (a *App) commentsSortLocked() bili.CommentSort {
	if a.commentsSortMode == 0 {
		return bili.CommentSortHot
	}
	return bili.CommentSort(a.commentsSortMode)
}

// SetCommentsSort 换评论的排序档。**立刻返回**（那一趟在后台跑）。
//
// 换完**第一屏重新从第一条开始**：已经拿到的那些是上一档的，清掉重拉。
//
// 🔴 **只认 `bili.CommentSorts()` 里那两档**：给别的（特别是 `mode=1`）就
// 当没给——理由见 `CommentsSort` 的注释（那一档和「最新」是同一份内容，
// 而 B 站已经不给它能用的游标了）。
//
// ⚠️ **换档之后缓存要按档分**：同一个对象的两档是两份不同的评论，而缓存的
// 键只有 type + oid（理由见 `service/comments` 的 `Entry.Sort`）。
func (a *App) SetCommentsSort(sort bili.CommentSort) {
	if !commentsSortAllowed(sort) {
		return
	}
	it, ok := a.CurrentItem()
	if !ok {
		return
	}
	slot := commentSlotKey(it)
	if slot == "" {
		return
	}

	a.commentsMu.Lock()
	if a.commentsSortLocked() == sort && a.commentsSlot == slot {
		// 已经是这一档了，而且这一份就是这一档的 ⇒ 什么都不用做。
		//
		// ⚠️ **别在这里重拉一遍**：用户连点两下同一档，那两下会各发一趟，
		// 而界面上的差别是零。
		a.commentsMu.Unlock()
		return
	}
	a.commentsSortMode = int(sort)
	// 🔴 **整个清空**：已经拿到的那些是**上一档**的评论（两份不同的内容），
	// 留着的话界面上会出现「切了档但前几屏还是上一档的」。
	//
	// ⚠️ **代次也一起加**（`resetCommentsLocked` 里做的）：上一档那一趟还在
	// 路上，它回来的时候对不上代次，直接丢掉。
	a.resetCommentsLocked(slot)
	a.commentsMu.Unlock()

	// 走一遍正常那条路（这一趟发出去，在后台跑——铁律三：view 里不许发请求）。
	a.ensureComments(it, slot)
}

// commentsSortAllowed 报这一档是不是界面上该摆的那两档之一。
func commentsSortAllowed(sort bili.CommentSort) bool {
	for _, s := range bili.CommentSorts() {
		if s == sort {
			return true
		}
	}
	return false
}

// CommentsLoading 报「有一趟评论正在路上」。
//
// 界面上拿它说「正在拉…」：翻页那一趟发出去之后到它回来之前，末尾那一行
// 转着的圈就是它。
func (a *App) CommentsLoading() bool {
	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	return a.commentsWanted
}

// CommentsHasMore 报「后面还有没有」。
//
// ⚠️ **它是 B 站给的那一句**（`cursor.is_end`），不是「我们拿到多少条」——
// B 站说到底了就是到底了，而我们手上的条数可能比它少（分页只取了一部分）。
//
// ⚠️ **还没问到的时候它是真的**（B 站还没说「到底了」）：所以界面上的
// 「加载更多」那一行在第一页回来之前就在那儿——那一行说的正是「正在问」。
func (a *App) CommentsHasMore() bool {
	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	return !a.commentsIsEnd
}

// CommentsLoaded 报「已经拿到过至少一页评论」。
//
// ⚠️ **「一条评论都没有」也算拿到过**：那一档界面上的说法是「还没有人
// 评论」，而它**是问出来了**（B 站说这一首还没有人评论）——不是「还没问过」。
//
// 它是界面层「保障①首次渲染检查」的输入（见
// `internal/ui/widgets/list/paged.go` 的 `DecideFirstPage`）：拿到过就不用再问
// 第一页。
func (a *App) CommentsLoaded() bool {
	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	return a.commentsLoaded
}

// EnsureComments 保证正在问 / 已经问过当前这一首的评论。
//
// 🔴 **它是界面层那一半的出口**：界面（`internal/ui/comments.go`）只判
// 「该不该问」（那一层每帧的副作用，`pageSyncs`），真正发出去的是这里——
// 铁律三：view 里不发网络请求。
//
// ⚠️ **重复调它没有副作用**：内部已经判过「问过了 / 正在问」，
// 而且失败之后有 15 秒退避（`ensureComments`）——所以界面每一帧调一次
// 也没关系。
func (a *App) EnsureComments() {
	it, ok := a.CurrentItem()
	if !ok {
		return
	}
	slot := commentSlotKey(it)
	if slot == "" {
		// 这一首没有可问的评论（本地文件）——一个请求都不发。
		return
	}
	a.ensureComments(it, slot)
}

// Comments 返回当前这一首的评论，顺手把没取过的那一次取回来。
//
// **返回的是缓存**：第一次那一趟还没回来时给的是 nil，界面上是「正在问」。
// 问回来了会叫醒界面（铁律三），所以界面每一帧调一次就行，**不要自己记
// 「问过了没有」**——那份状态在这一层，换一首歌就该忘掉。
func (a *App) Comments() []bili.Comment {
	it, ok := a.CurrentItem()
	if !ok {
		return nil
	}
	slot := commentSlotKey(it)
	if slot == "" {
		return nil
	}
	a.ensureComments(it, slot)

	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	// ⚠️ **缓存不属于当前这一首时不能给**（换歌之后那一问还没回来，缓存里还
	// 留着上一首的评论）。
	if a.commentsSlot != slot {
		return nil
	}
	if len(a.comments) == 0 {
		return nil
	}
	// ⚠️ **这里不再拷一份**：界面每一帧都调它，而一帧拷 30 条评论就是每秒
	// 几十份垃圾（每条评论里还带着正文、头像地址、表情表）。
	//
	// **不拷为什么是安全的**：后台那一趟回来时**从不往已经发出去的那一段里写**
	// （翻页是**新拼一份**，见 finishComments），而发出去之后那些元素就不再改
	// ——所以界面拿着的那一块在它手里是只读的。
	return a.comments
}

// CommentsErr 是最近一次问评论失败的原因。界面上拿它说「问不出评论」。
func (a *App) CommentsErr() error {
	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	return a.commentsErr
}

// CommentsNoComments 报「这个对象在，只是还没有人评论」。
//
// ⚠️ **它和 `CommentsErr()` 是两回事，各有一句人话**（票 #571 的验收）：
//
//	没有人评论   → 它好好地在那儿，界面上说「还没有人评论」
//	问不出评论   → 拉失败了，界面上说「问不出评论」
//
// 混成一句话的话，用户看到「问不出评论」会以为歌没了，而歌好好地摆在那儿。
func (a *App) CommentsNoComments() bool {
	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	return a.commentsNoComments
}

// CommentsAllCount 是 B 站给的那个评论总数。
//
// 0 = 「还不知道」（这一趟还没回来）。B 站给的是它自己数的那一份，而**我们
// 已经拿到的条数可能比它少**（分页只取了一部分），所以界面上拿它当「共 N 条」
// 那一行是对的，拿它跟 `len(Comments())` 比就不对。
func (a *App) CommentsAllCount() int64 {
	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	return a.commentsAllCount
}

// CommentsSlot 报「当前这一首的评论」的槽位键。
//
// ⚠️ **界面层要它，是因为它得知道「换歌了没有」**：评论正文按 rpid 记着一份
// 排版缓存，而**rpid 会在两首歌之间撞号**（它只是个 int64）——不按槽位清，
// 上一首的排版就贴到这一首的评论上。
//
// 空串 = 「当前这一首没有可问的评论」（本地文件 / 没有歌）。
//
// 🔴 **口径只有这一处**（`commentSlotKey`）：界面层**不许**自己再写一份
// ——两份口径一旦分叉，表现是「界面上在清缓存、应用层没清」，而症状只是
// 「偶尔有一条评论的排版是错的」。
func (a *App) CommentsSlot() string {
	it, ok := a.CurrentItem()
	if !ok {
		return ""
	}
	return commentSlotKey(it)
}

// LoadMoreComments 按游标翻下一页。**发出去那一趟就返回 true**。
//
// 已经在翻、或者已经到底了，都给 false（界面上的「加载更多」据此按成不可用）。
func (a *App) LoadMoreComments() bool {
	it, ok := a.CurrentItem()
	if !ok {
		return false
	}
	slot := commentSlotKey(it)
	if slot == "" {
		return false
	}

	a.commentsMu.Lock()
	if a.commentsSlot != slot || !a.commentsLoaded || a.commentsIsEnd {
		a.commentsMu.Unlock()
		return false
	}
	// ⚠️ **游标是空串的时候不能再翻**：B 站的偏移量是**不透明游标**，而
	// `CommentList` 把空串当成「从头」（那是第一页的写法）——拿它去翻第二页会
	// 把第一页再要一遍，界面上就出现一份重复的评论。
	//
	// ⚠️ **别拿「非空」当「能翻」**：实测 B 站给一个**乱写的** offset 时**不
	// 报错**，它回的是第一页——所以「游标非空」不等于「这一页后面还有东西」。
	// 真正判「还有没有」的是 `commentsIsEnd`（`cursor.is_end`）。
	if a.commentsNext == "" {
		a.commentsMu.Unlock()
		return false
	}
	if a.commentsWanted {
		a.commentsMu.Unlock()
		return false
	}
	// 退避：翻页失败之后也要等一下，不然用户连按「加载更多」就是每秒一趟。
	//
	// ⚠️ **用自己那一个时刻**，不共用第一页那个：第一页是刚问成功的话，
	// 共用的话用户刚进这一页就点「加载更多」会被挡住 15 秒。
	if !a.commentsMoreTriedAt.IsZero() && time.Since(a.commentsMoreTriedAt) < commentsRetryAfter {
		a.commentsMu.Unlock()
		return false
	}
	a.commentsWanted = true
	a.commentsMoreTriedAt = time.Now()
	gen := a.commentsGen
	// 🔴 **排序档在这里取一份**：它由界面线程写（`SetCommentsSort`），而
	// goroutine 去读它就是数据竞争（铁律一）。理由同 `next`。
	mode := int(a.commentsSortLocked())
	next := a.commentsNext
	// 🔴 **在途序号**：翻页也是一趟，也要占一个序号（理由见 `ensureComments`）。
	//
	// ⚠️ **翻页那一趟写不了缓存**（缓存里放的是「第一页 + 它的游标」），但它
	// **会改界面**——而界面上的那一份必须是**最后发起的那一趟**的。
	a.commentsSeq++
	seq := a.commentsSeq
	if a.commentsSeqs == nil {
		a.commentsSeqs = map[string]int{}
	}
	a.commentsSeqs[slot] = seq
	listFetch, archiveFetch := a.commentsListFetch, a.commentsArchiveIDFetch
	ctx, cancel := context.WithCancel(context.Background())
	a.commentsCancel = cancel
	a.commentsMu.Unlock()
	go a.loadComments(ctx, it, slot, gen, seq, mode, next, true, listFetch, archiveFetch)

	return true
}

// CommentReplies 返回 root 那一条下面的楼中楼，顺手把没取过的那一次取回来。
//
// **返回的是缓存**：第一次那一趟还没回来时给的是 nil。
func (a *App) CommentReplies(root int64) []bili.Comment {
	if root <= 0 {
		return nil
	}
	it, ok := a.CurrentItem()
	if !ok {
		return nil
	}
	slot := commentSlotKey(it)
	if slot == "" {
		return nil
	}
	// ⚠️ **主列表那一趟也要保证在问**：楼中楼问的是同一个对象，而对象是谁
	// （avid / auid）得先有人认出来。所以这里也走一遍 `ensureComments`——
	// 它有防重入，界面那一帧调多少次都只发一趟。
	a.ensureComments(it, slot)

	a.commentsMu.Lock()
	if a.commentsSlot != slot {
		a.commentsMu.Unlock()
		return nil
	}
	if rs, hit := a.commentsReplies[root]; hit {
		// ⚠️ **不拷**：界面每一帧都调它，而楼中楼那一块**再也不被改**
		// （楼中楼只取一页，取回来之后那一格就定型了）。理由同 `Comments`。
		a.commentsMu.Unlock()
		return rs
	}
	// 退避：界面每一帧都可能调它，不等一下的话失败一次就是每秒一发。
	if tried, ok := a.commentsRepliesTried[root]; ok &&
		!tried.IsZero() && time.Since(tried) < commentsRetryAfter {
		a.commentsMu.Unlock()
		return nil
	}
	if a.commentsRepliesWanted[root] {
		a.commentsMu.Unlock()
		return nil
	}
	if a.commentsRepliesWanted == nil {
		a.commentsRepliesWanted = map[int64]bool{}
	}
	if a.commentsRepliesTried == nil {
		a.commentsRepliesTried = map[int64]time.Time{}
	}
	a.commentsRepliesWanted[root] = true
	a.commentsRepliesTried[root] = time.Now()
	gen := a.commentsGen
	a.commentsMu.Unlock()

	repliesFetch := a.commentsRepliesFetch
	archiveFetch := a.commentsArchiveIDFetch
	// 🔴 **楼中楼的每一趟有自己的 cancel，按 root 记**。
	//
	// 原来那一版把这一趟的 cancel 存进 `commentsCancel`（那是**主列表**的格子），
	// 然后**取消了这一趟自己的 ctx** ——于是楼中楼**一趟都没发出去过**
	// （`bili: 网络没通: … context canceled`），展开永远是空的。
	//
	// ⚠️ **不跟主列表共用一格**：主列表那一趟一结束就没人再碰 `commentsCancel`，
	// 而两趟楼中楼（展开两条评论）本来是并行的、互不相干。
	ctx, cancel := context.WithCancel(context.Background())
	// ⚠️ **记的是一个带身份的指针，不是裸的 cancel**（函数值只能和 nil 比）：
	// 收尾时靠身份认出「这一格还是我那一趟」，否则同一个 root 又起了一趟时，
	// 先收尾的那一趟会把新的那一格删掉——新的那一趟于是**再也取消不了**。
	req := &commentReplyRequest{cancel: cancel}
	a.commentsMu.Lock()
	if a.commentsRepliesCancel == nil {
		a.commentsRepliesCancel = map[int64]*commentReplyRequest{}
	}
	// 同一个 root 又问了一次：先取消上一趟（它的结果按代次丢掉，见
	// `finishReplies`）。
	if prev := a.commentsRepliesCancel[root]; prev != nil {
		prev.cancel()
	}
	a.commentsRepliesCancel[root] = req
	a.commentsMu.Unlock()
	go func() {
		defer func() {
			a.commentsMu.Lock()
			if a.commentsRepliesCancel[root] == req {
				delete(a.commentsRepliesCancel, root)
			}
			a.commentsMu.Unlock()
			cancel()
		}()
		a.loadReplies(ctx, it, slot, gen, root, repliesFetch, archiveFetch)
	}()
	return nil
}

// commentReplyRequest 是一层楼中楼那一趟的身份：它的 cancel 加一个指针身份
// （用来认出「缓存里那一格还是不是我那一趟」，理由见 `CommentReplies`）。
type commentReplyRequest struct{ cancel context.CancelFunc }

// RefreshComments 绕过缓存重拉一遍当前这一首的评论。**立刻返回**。
//
// 这是那一枚「刷新」：没有它，缓存一小时之内就一直给旧的那一份。
func (a *App) RefreshComments() {
	it, ok := a.CurrentItem()
	if !ok {
		return
	}
	slot := commentSlotKey(it)
	if slot == "" {
		return
	}
	a.commentsMu.Lock()
	if a.commentsSlot != slot {
		a.resetCommentsLocked(slot)
	}
	a.commentsRefresh = true
	a.commentsLoaded = false
	a.commentsWanted = false
	a.commentsTriedAt = time.Time{}
	a.commentsMoreTriedAt = time.Time{}
	a.commentsErr = nil
	a.commentsMu.Unlock()

	// 走一遍正常那条路（这一趟发出去）。⚠️ **顺序要紧**：上面先把
	// `commentsRefresh` 摆好，再让 `ensureComments` 发这一趟——反过来的话
	// `ensureComments` 会先清掉它，这一趟又去读缓存了。
	a.ensureComments(it, slot)
}

// MountComments 挂上评论缓存。**在开窗口之前调**（建目录那一步是阻塞 IO）。
//
// 没挂也能跑：评论每次都要重新问一遍（慢一点，没有别的坏处）。
func (a *App) MountComments(c *comments.Cache) {
	a.commentsMu.Lock()
	a.commentsCache = c
	a.commentsMu.Unlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 内部：槽位、代次、请求
// ─────────────────────────────────────────────────────────────────────────────

// commentSlotKey 是「当前问的是哪一个对象」的槽位键。
//
// ⚠️ **它按对象认，不按队列下标认**：同一首歌换了一个分集，评论还是那一份
// （分集在同一个稿件底下），而按队列下标认的话每换一集就重问一次。
//
// 空串 = 「这一首没有评论可问」（本地文件，或认不出来的条目）。
func commentSlotKey(it Item) string {
	switch it.Type {
	case "mv":
		if it.Bvid == "" {
			return ""
		}
		return "mv|" + it.Bvid
	case "audio":
		if it.Sid == "" {
			return ""
		}
		return "audio|" + it.Sid
	}
	return ""
}

// ensureComments 保证正在问 / 已经问过当前这一首的评论。
//
// 换歌就重置：`commentsSlot` 是这份缓存属于谁。迟到的结果回来发现对不上直接
// 丢（和在线流那边的代次是同一个道理）。
//
// 🔴 **失败之后要能再问**：失败只记下原因，隔 `commentsRetryAfter` 之后界面下一
// 帧调 `Comments()` 时会重新问一次——不是「一次失败就再也不问」。
func (a *App) ensureComments(it Item, slot string) {
	a.commentsMu.Lock()
	if a.commentsSlot != slot {
		a.resetCommentsLocked(slot)
	}
	if a.commentsLoaded || a.commentsWanted {
		a.commentsMu.Unlock()
		return
	}
	// 退避：界面每一帧都调 `Comments()`，不等一下的话失败一次就是每秒一发。
	if !a.commentsTriedAt.IsZero() && time.Since(a.commentsTriedAt) < commentsRetryAfter {
		a.commentsMu.Unlock()
		return
	}
	a.commentsWanted = true
	a.commentsTriedAt = time.Now()
	// 清掉上一次的失败原因：这一趟正在问，界面上那句「问不出评论」就该撤了。
	a.commentsErr = nil
	gen := a.commentsGen
	// 🔴 **排序档在这里取一份**：它由界面线程写（`SetCommentsSort`），而
	// goroutine 去读它就是数据竞争（铁律一）。
	mode := int(a.commentsSortLocked())
	// 🔴 **在途序号**：每发起一趟就加一（**不管槽位**）。
	//
	// 代次（`commentsGen`）答的是「这一趟还属不属于**当前这一首**」——它挡的是
	// 「旧歌的结果盖到新歌上」。而**这一趟**要挡的是另一件事：**同一个对象**上
	// 一趟旧的（先发出去的）和一趟新的（后发出去的）会**先后写同一个缓存文件**，
	// 而慢的那一趟写得更晚——于是它把新的那份盖掉了。
	//
	// ⚠️ **症状是「重新进这一首，拿到的是几分钟前的那一份」**：用户看到的是
	// 「评论怎么变了」，而他什么都没做。
	//
	// ⇒ 每一趟都带上它的序号，回来的时候对一遍：只有**最后发起的那一趟**能写
	// 界面、也只有它能写盘。
	a.commentsSeq++
	seq := a.commentsSeq
	if a.commentsSeqs == nil {
		a.commentsSeqs = map[string]int{}
	}
	a.commentsSeqs[slot] = seq
	a.commentsMu.Unlock()

	// ⚠️ **替身函数在这里就取出来**（界面那一侧），goroutine 不碰那些字段——
	// 和 `partsFetch` 同一条规矩（理由见 app.go 里那一段的注释）。
	listFetch, archiveFetch := a.commentsListFetch, a.commentsArchiveIDFetch
	ctx, cancel := context.WithCancel(context.Background())
	a.commentsCancel = cancel
	go a.loadComments(ctx, it, slot, gen, seq, mode, "", false, listFetch, archiveFetch)
}

// resetCommentsLocked 把评论这一摊整个清空，换成另一个槽位。
//
// ⚠️ **代次一定要加**：迟到的结果回来时靠它认出来（对不上就丢）。
func (a *App) resetCommentsLocked(slot string) {
	if a.commentsCancel != nil {
		a.commentsCancel()
		a.commentsCancel = nil
	}
	a.commentsSlot = slot
	a.comments = nil
	a.commentsErr = nil
	a.commentsLoaded = false
	a.commentsWanted = false
	a.commentsTriedAt = time.Time{}
	a.commentsNext = ""
	a.commentsIsEnd = false
	a.commentsAllCount = 0
	a.commentsNoComments = false
	a.commentsRefresh = false
	a.commentsOid = 0
	a.commentsTyp = 0
	// 🔴 **换歌就把在途的楼中楼全取消掉**：它们问的是上一首的对象，结果会被
	// 代次丢掉，留着它们只是白占着连接。
	for _, req := range a.commentsRepliesCancel {
		req.cancel()
	}
	a.commentsRepliesCancel = nil
	a.commentsReplies = map[int64][]bili.Comment{}
	a.commentsRepliesTried = map[int64]time.Time{}
	a.commentsRepliesWanted = map[int64]bool{}
	a.commentsGen++
}

// loadComments 拉一页评论（next 是空串 = 第一页，否则是翻页），回来写进缓存。
//
// listFetch / archiveFetch 是**界面那一侧取好的**替身（nil = 走真的那个 B 站
// 客户端）。goroutine 不碰 `App` 上那两个字段。
//
// mode 是**排序档**（`bili.CommentSort` 的值）：它在界面线程上取好递进来
// （理由见 `ensureComments` 里那一段）——goroutine 不许读界面线程写的字段。
//
// 🔴 **seq 是这一趟的在途序号**：它挡的是「同一个对象上有两趟」——较早的那一趟
// 回来时既不许盖掉界面上的新结果，也不许盖掉盘上的新缓存（理由见
// `ensureComments` 里那一段）。
func (a *App) loadComments(ctx context.Context, it Item, slot string, gen, seq, mode int, next string, more bool,
	listFetch commentsListFetchFunc, archiveFetch commentsArchiveIDFetchFunc) {
	typ, oid, ok, err := a.resolveCommentTarget(ctx, it, archiveFetch)
	if err != nil {
		// ⚠️ **换 avid 那一趟失败了**：**不报错**（票 #572：「取不到就当『没有
		// 评论』，不发请求，也不报错」），但**要能再问**——
		//
		// 把这一趟当成「问成功了、结果为空」的话，一次网络抖动之后这一首在
		// 本次播放期间就**永远没有评论**了，而界面上看不出任何异样。
		//
		// 所以这里**不置 `commentsLoaded`**：隔 `commentsRetryAfter` 之后界面
		// 下一帧调 `Comments()` 时会重新问一次（还是不发评论请求—— avid 换到
		// 了才发）。
		a.finishComments(slot, gen, seq, commentResult{})
		return
	}
	if !ok {
		// ⚠️ **这一首压根没有可问的身份**（sid 认不出来之类）：当「没有评论」
		// （不发请求、也不报错），而且**不再问**——这一条等下去也不会变。
		a.finishComments(slot, gen, seq, commentResult{loaded: true, noComments: true})
		return
	}
	a.rememberCommentOID(slot, gen, typ, oid)

	if !more {
		// 缓存里有就用缓存（TTL 一小时，判过期是缓存那一层的事）。
		// ⚠️ **「刷新」那枚绕过这一段**——它是给用户主动要新的一份用的。
		refresh := a.commentsRefreshFlag()
		if e, hit := a.loadCommentsCache(typ, oid, mode); hit && !refresh {
			a.finishComments(slot, gen, seq, commentResult{
				comments:   e.Comments,
				next:       e.NextOffset,
				isEnd:      e.IsEnd,
				allCount:   e.AllCount,
				noComments: e.NoComments,
				loaded:     true,
			})
			return
		}
	}

	page, err := a.fetchCommentList(ctx, typ, oid, mode, next, listFetch)

	if err != nil {
		// ⚠️ **「还没有人评论」不是失败**：B 站回的是 code 0 + 空列表，而这一页
		// 照常有值（`AllCount` / `UpperMid` 那些）。混成失败的话，界面上会说
		// 「问不出评论」，而真实情况是「还没有人评论」。
		if errors.Is(err, bili.ErrCommentNoComments) {
			// 🔴 **「还没有人评论」也要落缓存**：B 站说「这个对象在，只是没人
			// 评论」是**一份完整的答案**。不存的话每进一次就要重新问一次——而
			// 验收要求「同一首歌第二次进是**瞬时的**（不发请求）」。
			//
			// ⚠️ **存的是「没有人评论」这一条**（`NoComments: true`），不是一份
			// 空列表：空列表和「还没问」在界面上分不开，会一直显示「正在问」。
			if !more {
				a.saveCommentsCache(slot, seq, typ, oid, mode, commentResult{
					noComments: true,
					loaded:     true,
					next:       page.NextOffset,
					isEnd:      page.IsEnd,
					allCount:   page.AllCount,
				})
			}
			a.finishComments(slot, gen, seq, commentResult{
				comments:   page.Replies,
				next:       page.NextOffset,
				isEnd:      page.IsEnd,
				allCount:   page.AllCount,
				noComments: true,
				loaded:     true,
				append:     more,
			})
			return
		}
		a.finishComments(slot, gen, seq, commentResult{err: errors.New(bili.CommentFriendly(err))})
		return
	}

	// 第一页顺手写进缓存（翻页不写：缓存里放的是「第一页 + 它的游标」）。
	if !more {
		a.saveCommentsCache(slot, seq, typ, oid, mode, commentResult{
			comments: page.Replies,
			next:     page.NextOffset,
			isEnd:    page.IsEnd,
			allCount: page.AllCount,
			loaded:   true,
		})
	}
	a.finishComments(slot, gen, seq, commentResult{
		comments: page.Replies,
		next:     page.NextOffset,
		isEnd:    page.IsEnd,
		allCount: page.AllCount,
		loaded:   true,
		append:   more,
	})
}

// finishComments 记下一次拉回来的结果并叫醒界面。
//
// ⚠️ **对不上就丢**：换歌之后上一首那一趟可能还在路上，它回来的时候不能把
// 上一首的评论写到当前这一首的格子里。
//
// 🔴 **seq 是这一趟的在途序号**（`0` = 不参与这个判据，见 `loadComments` 的
// 调用点）：用户按了「刷新」，而首次加载那一趟还在途——两趟回来的时候，
// **较早的那一趟不许盖掉刷新拉回来的那一份**。
//
// ⚠️ **代次（`gen`）挡不住这个**：代次是「换歌才加」，而刷新不换歌。
func (a *App) finishComments(slot string, gen, seq int, res commentResult) {
	a.commentsMu.Lock()
	if a.commentsSlot != slot || a.commentsGen != gen {
		// ⚠️ **这一趟已经被换歌作废了**：它自己那条序号也用不上了——留着的话
		// 表会一直长（用户听过的每一首都会留一条）。
		//
		// ⚠️ **只删自己那一条**：A → B → A 时，新的 A 那一趟已经占了这个位置，
		// 删了它的话新 A 那一趟回来时会被当成「不是最新的一趟」⇒ **它拉回来的
		// 那份就落不了盘了**（而界面上还是对的——那就成了「界面对、盘上错」）。
		if seq != 0 && a.commentsSeqs[slot] == seq {
			delete(a.commentsSeqs, slot)
		}
		a.commentsMu.Unlock()
		return
	}
	// 🔴 **较早的那一趟不许盖掉较新的那一份**：界面上摆的是「最后发起的那一趟」
	// 的结果。
	//
	// ⚠️ **症状是「用户按了刷新，界面却又变回刷新之前的那一份」**——而刷新那一
	// 趟明明已经回来了。
	if seq != 0 && a.commentsSeqs[slot] != seq {
		// ⚠️ **「正在问」那个标记**这里**不动**：较新的那一趟还在途，它回来时
		// 自己会清（`commentsWanted = false`）。这里清了的话，那一趟还在途却
		// 没人标记「正在问」⇒ 界面下一帧就会**再发一趟**（变成三趟）。
		//
		// ⚠️ **而较新的那一趟已经回来了的情形也不受影响**：它回来时已经把标记
		// 清了。
		a.commentsMu.Unlock()
		return
	}
	// added 是「这一趟新加进来几条」（翻页那一趟用；见下面那句判据）。
	added := 0
	if res.append {
		// ⚠️ **新拼一份，不往已经发出去的那一段上追加**：`a.comments` 可能正被
		// 界面那一帧拿着（见 Comments 的注释）。往它的底层数组里写就是**后台在改
		// 界面正在读的东西**——数据竞争，而症状是界面上偶发地显示一条半截的评论。
		//
		// 🔴 **按 rpid 去重**：翻页重叠是常事——同一条评论挪了名次就会在两页
		// 里各出现一次（B 站的分页是按热度切的，不是按 rpid 切的）。不去重的
		// 话同一条评论会在界面上摆两行，而这一页的行身份就是 rpid（框架见同一
		// 个父节点下有两个一样的 key 就 panic，见 `internal/ui/comments.go`）。
		//
		// 第二个返回值是**这一页新加进来几条**：0 = 什么新东西都没带来（理由见
		// 下面那一句判据）。
		merged, n := mergeCommentPages(a.comments, res.comments)
		a.comments = merged
		added = n
	} else if res.comments != nil {
		// ⚠️ **第一页也去重**：一页之内出现两条一样的 rpid 是接口的事，但
		// 界面上那两行会共用一份状态（框架还会 panic）。
		merged, _ := mergeCommentPages(nil, res.comments)
		a.comments = merged
	}
	if res.loaded {
		a.commentsLoaded = true
		a.commentsErr = nil
		a.commentsRefresh = false
		// ⚠️ **翻页成功之后不再挡**：那一趟已经走了 15 秒的退避窗口，
		// 用户接着往下翻是**正常操作**，不是「失败之后的重试」。
		//
		// 挡了的话症状是「用户滑到底要点好几次『加载更多』才动一次」——
		// 而界面上那枚键还亮着（能点、点了没反应）。
		a.commentsMoreTriedAt = time.Time{}
	} else if res.err != nil {
		a.commentsErr = res.err
	}
	if res.loaded {
		a.commentsNext = res.next
		a.commentsIsEnd = res.isEnd
		// 🔴 **无进展保护：一页没有带来任何新东西就是到底了**。
		//
		// 两条坏处一样（用户看着末尾那一行转圈转到天荒地老）：
		//
		//   - 一页空的还说「还有」：接口在骗人，或者已经到头了。
		//   - 一页**全是重样的**：接口在重复给同一批，翻页翻不动了——而「还有」
		//     会让自动补屏一直拉下去（每次都要一个往返）。
		//
		// ⇒ **加不进去就是没有**（理由同 `widgets/list/paged.go` 的那一档）。
		//
		// ⚠️ **只对翻页那一趟这么判**：第一页本来就可能是空的（那一首还没有
		// 人评论），而那一档是 B 站自己说的 `is_end`。
		if res.append && added == 0 {
			a.commentsIsEnd = true
		}
		a.commentsAllCount = res.allCount
		// ⚠️ **「还没有人评论」只在第一页说**：翻页途中碰到一个空页而
		// `is_end` 还是 false，那是**翻页没翻完**——说成「还没有人评论」的话，
		// 界面上会在用户已经读到一半的时候忽然说这一句。
		if !res.append {
			a.commentsNoComments = res.noComments
		}
	}
	a.commentsWanted = false
	// ⚠️ **这一趟走完了，它那条序号也用不上了**——留着的话表会一直长（用户听过的
	// 每一首都会留一条）。
	//
	// ⚠️ **只删自己那一条**：删掉了新的一趟那条的话，它回来时就分不出「自己是
	// 最新的」了。
	//
	// ⚠️ **删掉之后「查不到」就等于「不是最新的一趟」**：那些旧的一趟照样被挡
	// 住（见 `saveCommentsCache` 与上面那个序号判据）。
	if seq != 0 && a.commentsSeqs[slot] == seq {
		delete(a.commentsSeqs, slot)
	}
	a.commentsMu.Unlock()

	a.wakeComments()
}

// mergeCommentPages 把新的一页接到旧的那一页后面，**按 rpid 去重**。
//
// 🔴 **去重是必须的**：翻页重叠是常事——B 站的分页是按热度切的，不是按 rpid
// 切的，所以同一条评论挪了名次就会在两页里各出现一次。而这一页的行身份就是
// rpid（`internal/ui/comments.go` 的 `commentRowKey`），两条一样的 rpid 摆成
// 两行的话，框架见同一个父节点下有两个一样的 key 就 panic（`ui: two elements
// under one parent have the key ...`）——症状是**整页变成错误页**。
//
// ⚠️ **保留的是第一次出现的那一条**：翻页重叠时两页里那一条内容是一样的，
// 而先来的那一页是用户已经读过的。
//
// 第二个返回值是**新加进去的条数**：0 = 这一页没有带来任何新东西（接口在重复
// 给同一批，或者这一页是空的）⇒ 那是「到底了」，不许再空转拉页（理由见
// `finishComments` 里那一句）。
func mergeCommentPages(old, page []bili.Comment) ([]bili.Comment, int) {
	merged := make([]bili.Comment, 0, len(old)+len(page))
	seen := make(map[int64]bool, len(old)+len(page))
	for _, c := range old {
		if seen[c.Rpid] {
			continue
		}
		seen[c.Rpid] = true
		merged = append(merged, c)
	}
	added := 0
	for _, c := range page {
		if seen[c.Rpid] {
			continue
		}
		seen[c.Rpid] = true
		merged = append(merged, c)
		added++
	}
	return merged, added
}

// wakeComments 叫醒界面（铁律三）。
//
// 句柄是**拷出来**再调的（`currentWake` 在锁里拷一份）：唤醒会去要界面那把锁，
// 拿着 `a.mu` 调它就是自己等自己。
func (a *App) wakeComments() {
	if w := a.currentWake(); w != nil {
		w()
	}
}

// resolveCommentTarget 认出「这一首要问的是哪个对象」。
//
// 返回 typ（B 站的对象类型）和 oid（视频是 avid、音频是 auid）。
//
// 🔴 **mv 那一路要走 `ArchiveID`**：队列里的条目只有 bvid，而 `type=1` 的 oid
// 要的是**数字 avid**。漏了这一步 ⇒ 静默拿到空列表。
//
// 第三个返回值是「认得出来吗」，第四个是「认的时候出了错吗」——两者必须分开：
// 「压根没有身份」（sid 认不出来）是**问一百次也一样**，而「换 avid 那一趟失败」
// 是**等一会儿再问有可能好**。
func (a *App) resolveCommentTarget(ctx context.Context, it Item, archiveFetch commentsArchiveIDFetchFunc) (typ, oid int64, ok bool, err error) {
	switch it.Type {
	case "audio":
		// ⚠️ **音乐库音频的 oid 就是 auid**（`type=14`），不用换。
		id, err := strconv.ParseInt(strings.TrimSpace(it.Sid), 10, 64)
		if err != nil || id <= 0 {
			return 0, 0, false, nil
		}
		return bili.CommentTypeAudio, id, true, nil
	case "mv":
		aid, err := a.commentArchiveID(ctx, it.Bvid, archiveFetch)
		if err != nil {
			return 0, 0, false, err
		}
		if aid <= 0 {
			return 0, 0, false, nil
		}
		return bili.CommentTypeVideo, aid, true, nil
	}
	return 0, 0, false, nil
}

// commentArchiveID 把 bvid 换成 avid。
//
// 换过的要记住（内存里 + 盘上），不然**每次进这一首都要多付一次往返**——
// 而验收要求「同一首歌第二次进是瞬时的（不发请求）」。
func (a *App) commentArchiveID(ctx context.Context, bvid string, fetch commentsArchiveIDFetchFunc) (int64, error) {
	a.commentsMu.Lock()
	if a.commentsAvids != nil {
		if id, ok := a.commentsAvids[bvid]; ok && id > 0 {
			a.commentsMu.Unlock()
			return id, nil
		}
	}
	// 🔴 **同一时刻只发一趟**：主列表那一趟和楼中楼那一趟可能同时问「这个 bvid
	// 的 avid 是多少」——两趟都发出去就是两次网络往返，而 B 站会当成两次
	// 独立的稿件详情请求。
	//
	// 后来的那一趟**等前面那一趟的结果**（而不是自己再发一趟）：等的结果就是
	// 同一个 avid。
	if a.commentsAvidWaiters == nil {
		a.commentsAvidWaiters = map[string]chan struct{}{}
	}
	if ch, ok := a.commentsAvidWaiters[bvid]; ok {
		a.commentsMu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
		a.commentsMu.Lock()
		id := a.commentsAvids[bvid]
		a.commentsMu.Unlock()
		if id > 0 {
			return id, nil
		}
		return 0, errCommentsUnavailable
	}
	ch := make(chan struct{})
	a.commentsAvidWaiters[bvid] = ch
	a.commentsMu.Unlock()

	if c := a.commentsCacheSnapshot(); c != nil {
		if id, ok := c.LoadAVID(bvid); ok && id > 0 {
			a.rememberAVID(bvid, id)
			a.finishAVIDWait(bvid, ch)
			return id, nil
		}
	}

	if fetch == nil {
		cli := a.commentsClient()
		if cli == nil {
			a.finishAVIDWait(bvid, ch)
			return 0, errCommentsUnavailable
		}
		fetch = cli.ArchiveID
	}
	id, err := fetch(ctx, bvid)
	if err != nil {
		a.finishAVIDWait(bvid, ch)
		return 0, err
	}
	if id > 0 {
		a.rememberAVID(bvid, id)
		if c := a.commentsCacheSnapshot(); c != nil {
			_ = c.SaveAVID(bvid, id)
		}
	}
	a.finishAVIDWait(bvid, ch)
	return id, nil
}

// finishAVIDWait 把等着这一趟的那些 goroutine 放出来。
func (a *App) finishAVIDWait(bvid string, ch chan struct{}) {
	a.commentsMu.Lock()
	if cur, ok := a.commentsAvidWaiters[bvid]; ok && cur == ch {
		delete(a.commentsAvidWaiters, bvid)
	}
	a.commentsMu.Unlock()
	close(ch)
}

// rememberAVID 记住 bvid → avid。
func (a *App) rememberAVID(bvid string, avid int64) {
	a.commentsMu.Lock()
	if a.commentsAvids == nil {
		a.commentsAvids = map[string]int64{}
	}
	a.commentsAvids[bvid] = avid
	a.commentsMu.Unlock()
}

// rememberCommentOID 记下当前这一首问的是哪个对象（楼中楼要用它）。
func (a *App) rememberCommentOID(slot string, gen int, typ, oid int64) {
	a.commentsMu.Lock()
	if a.commentsSlot == slot && a.commentsGen == gen {
		a.commentsTyp, a.commentsOid = typ, oid
	}
	a.commentsMu.Unlock()
}

// fetchCommentList 问一页评论。
//
// next 是**偏移量**（空串 = 第一页），不是 `cursor.next`——理由见
// `bili.CommentList` 的注释与 `bili.CommentPage.NextOffset`。
//
// mode 是**排序档**：`bili.CommentSortHot` / `bili.CommentSortLatest`
// （理由见 `bili.CommentSorts`——「评论」那一档不在里面）。
func (a *App) fetchCommentList(ctx context.Context, typ, oid int64, mode int, next string, fetch commentsListFetchFunc) (bili.CommentPage, error) {
	if fetch == nil {
		cli := a.commentsClient()
		if cli == nil {
			return bili.CommentPage{}, errCommentsUnavailable
		}
		fetch = func(ctx context.Context, typ, oid int64, mode int, next string) (bili.CommentPage, error) {
			return cli.CommentList(ctx, typ, oid, mode, next, commentsPageSize)
		}
	}
	return fetch(ctx, typ, oid, mode, next)
}

// loadReplies 拉一层楼中楼，回来写进缓存并叫醒界面。
func (a *App) loadReplies(ctx context.Context, it Item, slot string, gen int, root int64,
	repliesFetch commentsRepliesFetchFunc, archiveFetch commentsArchiveIDFetchFunc) {
	typ, oid, ok := a.commentOIDFor(ctx, it, slot, gen, archiveFetch)
	if !ok {
		a.finishReplies(slot, gen, root, nil)
		return
	}
	fetch := repliesFetch
	if fetch == nil {
		cli := a.commentsClient()
		if cli == nil {
			a.finishReplies(slot, gen, root, nil)
			return
		}
		fetch = func(ctx context.Context, typ, oid, root int64) ([]bili.Comment, error) {
			rs, _, err := cli.CommentReplies(ctx, typ, oid, root, 1, commentsReplyPageSize)
			return rs, err
		}
	}
	rs, err := fetch(ctx, typ, oid, root)
	if err != nil {
		a.finishReplies(slot, gen, root, nil)
		return
	}
	a.finishReplies(slot, gen, root, rs)
}

// commentOIDFor 拿当前这一首的对象类型和 id。
//
// 已经问过主列表的话直接用它（**别再换一次 avid**）；还没问过就现算。
func (a *App) commentOIDFor(ctx context.Context, it Item, slot string, gen int, archiveFetch commentsArchiveIDFetchFunc) (typ, oid int64, ok bool) {
	a.commentsMu.Lock()
	if a.commentsSlot == slot && a.commentsGen == gen && a.commentsOid > 0 {
		typ, oid = a.commentsTyp, a.commentsOid
		a.commentsMu.Unlock()
		return typ, oid, true
	}
	a.commentsMu.Unlock()
	typ, oid, ok, _ = a.resolveCommentTarget(ctx, it, archiveFetch)
	return typ, oid, ok
}

// finishReplies 记下一次楼中楼的结果并叫醒界面。
func (a *App) finishReplies(slot string, gen int, root int64, rs []bili.Comment) {
	a.commentsMu.Lock()
	if a.commentsSlot == slot && a.commentsGen == gen {
		if a.commentsReplies == nil {
			a.commentsReplies = map[int64][]bili.Comment{}
		}
		if rs != nil {
			a.commentsReplies[root] = rs
		}
		if a.commentsRepliesWanted != nil {
			a.commentsRepliesWanted[root] = false
		}
	}
	a.commentsMu.Unlock()

	a.wakeComments()
}

// ─────────────────────────────────────────────────────────────────────────────
// 缓存
// ─────────────────────────────────────────────────────────────────────────────

// commentsCacheSnapshot 拿一份配好的缓存。没挂上是 nil。
func (a *App) commentsCacheSnapshot() *comments.Cache {
	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	return a.commentsCache
}

// loadCommentsCache 读缓存。没有、坏了、过期了、**不是这一档排序的**，都是
// 「没有」。
//
// 🔴 **排序档要对得上**：同一个对象的两档排序给的是**两份不同的评论**
// （「热门评论」和「最新评论」），而缓存的键只有 type + oid（理由见
// `service/comments` 的文件头与 `Entry.Sort`）。
//
// 不对得上就当没有 ⇒ 重新拉一遍（拉回来的那份会按这一档写进盘上）。
func (a *App) loadCommentsCache(typ, oid int64, mode int) (comments.Entry, bool) {
	c := a.commentsCacheSnapshot()
	if c == nil {
		return comments.Entry{}, false
	}
	e, hit := c.Load(typ, oid)
	if !hit || e.Sort != mode {
		return comments.Entry{}, false
	}
	return e, true
}

// saveCommentsCache 把第一页写进缓存。
//
// 🔴 **seq 是这一趟的在途序号**，它挡的是「较早的那一趟把较新的那一份盖掉」：
//
//	同一首歌换了两次（A → B → A），A 的**旧**那一趟还在途，而新的 A 已经问完
//	并写进盘上了。旧那一趟回来时**在可见状态上**是被代次丢掉的（界面上摆的是
//	新 A 的）——但它**照样会写盘**，于是盘上那份被改回旧的。
//
// ⚠️ **症状是「重进这一首，拿到的是几分钟前的那一份」**：用户看到的是「评论怎么
// 变了」，而他什么都没做。
//
// ⇒ 只有**最后发起的那一趟**能写盘。
func (a *App) saveCommentsCache(slot string, seq int, typ, oid int64, mode int, res commentResult) {
	// 🔴 **写盘走这把专锁**：「检查我是不是最新的一趟」和「真的写下去」必须在
	// **同一个临界区**里——分开的话，中间发起的新一趟会让旧的那一趟以为自己
	// 还是最新的，于是它写得更晚、把新的盖掉。
	//
	// ⚠️ **它不是 `commentsMu`**：写盘是阻塞 IO，而 `commentsMu` 是界面每一帧
	// 都要的（`App.Comments`）。
	a.commentsWriteMu.Lock()
	defer a.commentsWriteMu.Unlock()

	a.commentsMu.Lock()
	c := a.commentsCache
	// 🔴 **对一遍这一趟的序号**：只有**最后发起的那一趟**能写盘。
	//
	// ⚠️ **不能拿全局序号对**：全局的序号会把「另一首的新一趟」也算成「比这一
	// 首新」，于是切到另一首之后，上一首那份就落不了盘了——而下次进上一首就得
	// 重新拉一次（那就不是「第二次进是瞬时的」了）。
	//
	// ⇒ 按**槽位**记（`slot`），而槽位与对象是一一对应的（同一个对象在同一个
	// 槽位上）。
	latest, known := a.commentsSeqs[slot]
	stale := !known || seq != latest
	a.commentsMu.Unlock()
	if c == nil || stale {
		// ⚠️ **过时的那一趟不许写**（它会把新写的那份盖掉）。
		return
	}
	_ = c.Save(typ, oid, comments.Entry{
		FetchedAt:  time.Now().Unix(),
		Sort:       mode,
		NextOffset: res.next,
		IsEnd:      res.isEnd,
		AllCount:   res.allCount,
		NoComments: res.noComments,
		Comments:   res.comments,
	})
}

// commentsRefreshFlag 报「用户要绕过缓存重拉一遍」。
func (a *App) commentsRefreshFlag() bool {
	a.commentsMu.Lock()
	defer a.commentsMu.Unlock()
	return a.commentsRefresh
}

// commentsClient 拿那个 B 站客户端。
//
// ⚠️ **优先用取流那一摊的**：全项目只有**一个** B 站客户端（见
// `internal/app/client.go`），而它由 `MountStreaming` 递进来。没挂在线播放的
// 时候退回登录那一摊递的那一个（同一个）。
func (a *App) commentsClient() *bili.Client {
	if s := a.streamingSnapshot(); s != nil && s.Client != nil {
		return s.Client
	}
	return a.BiliClient()
}
