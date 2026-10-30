// 「稍后再看」和「历史记录」这两页要的那些接口（PLAN.md 的 P-03 / P-04）。
//
// ## 为什么这两页要一个自己的中间层
//
// SPEC 的四层依赖是单向的（`ui` → `app` → 服务层），界面层**不发网络请求**。
// 所以这两页不能直接把 `bili.Client` 拿在手里——中间必须有这一层。它做三件事，
// 每一件都是「界面不该知道、但两边都要用同一个答案」的那种事：
//
//   - **把 B 站的两份模型合成一行**（`LibraryItem`）。稍后再看给 `add_at` 和
//     `stat.view`，历史记录给 `view_at` 和 `progress`；界面上一行是一行，
//     不是两张结构体拼起来。
//   - **给出行身份**（`LibraryItem.ID`）。🔴 铁律四要的是「稳定且唯一」，
//     而「什么算一条」这两页不一样：稍后再看按稿件，历史记录按
//     `{业务}_{目标}`（同一首歌看过两次就是两条）。这个判断放在这一层，
//     界面层就永远不会搞错。
//   - **把秒翻成 `time.Duration`**。B 站给的是整数秒，直接透出去的话界面层
//     每一处都得记得乘 time.Second，漏一处就画成「0:00」或者「35年」。
//
// ## 挂载
//
// 跟 `MountStreaming` / `MountLogin` 一样在**开窗口之前**挂，只递零件不发请求
// （见 `internal/ui/library_mount.go`）。挂没挂由 `Library()` 返回 nil 说话，
// 界面据此说清「没接上」，而不是画一个永远空的页面。
package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// errLibraryMissing 是「这一页还没挂上 B 站客户端」。
//
// ⚠️ **这句话会原样弹到用户面前**（失败态走 `viewLoadError`，它画的就是
// `err.Error()`），所以不许说「客户端」（票 #242，UX 报告 §3-F 的 F2）。
var errLibraryMissing = errors.New("app: 这一页暂时加载不出来")

// Library 是这两页要的那几个接口的依赖。零值不挂——界面要判空。
type Library struct {
	// Client 是 B 站客户端。**全项目那一份**（`MountLogin` 建的那一个）。
	Client *bili.Client
}

// LibraryQuery 是这两页的筛选条件。零值 = 不筛。
//
// From / To 是闭区间，零值是「那头不筛」。传 `time.Time` 而不是 Unix 秒是
// 有意的：秒数只在这一层换（见文件头「把秒翻成 Duration」那条）。
type LibraryQuery struct {
	Keyword  string
	From, To time.Time
}

// libraryPageSize 是一页几条。Biu 那两页都是 20（later/index.tsx:21，
// history 那边压根没传 ps、由 B 站给 20）。
const libraryPageSize = 20

// LibraryItem 是这两页的一行。
//
// 画得出来的字段直接来自 `list.Music` 那一组（Title / Cover / Owner /
// Duration / Item），这里**不用嵌入**它——这一层不认识界面那一层的类型，
// 界面层负责翻（见 `internal/ui/library_rows.go` 的 `libraryRow`）。
type LibraryItem struct {
	// ID 是这一行的身份，🔴 铁律四：稳定且唯一。**不要拿序号**。
	ID string

	Title string
	Cover string
	// Owner 是 UP 名，空串时那一行不画它。
	Owner    string
	OwnerMid int64
	Duration time.Duration
	// Progress 是已经看了多少。稍后再看有 `progress` 字段，历史记录有；
	// 两页都画，只是位置不一样（见各自的页面）。
	Progress time.Duration
	// ViewedAt 是「什么时候加进稍后再看」/「什么时候看的」。两个时间在两页
	// 上说的是同一件事——这一条出现在这个列表里的时间。
	ViewedAt time.Time
	// PubDate 是投稿时间。**只有稍后再看有**（历史记录那个接口不给，
	// 上游的历史页画的也是观看时间，不是投稿时间）。
	//
	// ⚠️ 它和 ViewedAt 是两个时间，别拿一个顶另一个：稍后再看那一页的
	// 日期筛选筛的是 `add_at`（ViewedAt），而列表上**另外**有一列画的是
	// 投稿时间（上游 `later/list.tsx:68`）。
	PubDate time.Time
	// View 是播放量。**只有稍后再看有**（历史记录的接口不给）。
	View int64
	// CanPlay 报这一条能不能放。稍后再看里的 PGC 放不了（取流那一套对 PGC
	// 不通，见 `playableOnline`）；界面上据此不给「播放」，而不是让用户按了
	// 才听一句「还不能放」。
	CanPlay bool
	// CannotPlay 是放不了的理由（空串 = 放得了）。
	//
	// 它和 `CanPlay` 是一对：`CanPlay` 判「能不能」，这一句说「为什么」。
	// 界面上菜单项按不动的时候，把这一句摆在旁边——**按不动而不说为什么
	// 等于让用户猜**，而用户猜不出来（他不会知道 PGC 是什么）。
	//
	// ⚠️ 它得能独立回答「还能怎么办」，所以写的是一整句，不是「PGC」。
	CannotPlay string
	// Item 是播放和右键菜单真正用的那一项。已是 `Source: "online"`。
	Item Item
}

// ToView 拉一页稍后再看。
//
// 返回这一页的行和 B 站给的**总条数**。「还有没有」由调用方算——
// 它得拿「已经加载了多少行」来比（照 Biu 的 `later/index.tsx:59`：
// `newList.length < count`），而那个数只有翻页那一层知道。
func (l *Library) ToView(ctx context.Context, page int, q LibraryQuery) ([]LibraryItem, int, error) {
	if l == nil || l.Client == nil {
		return nil, 0, errLibraryMissing
	}
	pg, err := l.Client.ToViewList(ctx, bili.ToViewQuery{
		Page: page, PageSize: libraryPageSize,
		Keyword: q.Keyword, From: q.From, To: q.To,
	})
	if err != nil {
		return nil, 0, err
	}

	items := make([]LibraryItem, 0, len(pg.Items))
	for _, it := range pg.Items {
		if it.Bvid == "" {
			// 没有 bvid 的条目：画得出来但放不了，而没有 bvid 也拼不出行
			// 身份。跳过，不给它占一行。
			continue
		}
		canPlay := !it.IsPGC
		items = append(items, LibraryItem{
			ID:       laterID(it.Bvid, it.Aid),
			Title:    it.Title,
			Cover:    it.Pic,
			Owner:    it.OwnerName,
			OwnerMid: it.OwnerMid,
			Duration: it.Duration,
			Progress: it.Progress,
			ViewedAt: it.AddAt,
			PubDate:  it.PubDate,
			View:     it.View,
			CanPlay:  canPlay,
			// 🔴 **按不动的那一行也得说清为什么**：PGC（番剧 / 纪录片）没有
			// 单独的音频流，取流那一套对它不通。Biu 上游也是不给这一类入口
			// （`cannotPlay = is_pgc`），而它没解释——我们解释一句，
			// 因为用户看到的是「一行灰的」，他不知道灰的是哪一种。
			CannotPlay: pgcCannotPlayReason(canPlay),
			Item:       onlineItem(it.Bvid, it.Cid, it.Title, it.Pic, it.OwnerName, it.OwnerMid),
		})
	}
	return items, pg.Count, nil
}

// pgcCannotPlayReason 是 PGC 那一类放不了时的理由。
//
// 放得了的话给空串。
//
// 🔴 **它和收藏夹里合集/电影那一类说的是同一句**（同一个 kind
// `UnplayableNoAudioFlow`）：那两类在 B 站那边是同一件事（不是投稿、
// 没有单独的音频流），用户照着做的下一步也一样（打开里面的单集）。
// 两处各写一个字面量的话，同一个类在两个界面就说成两句不同的话。
func pgcCannotPlayReason(canPlay bool) string {
	if canPlay {
		return ""
	}
	return unplayableOf(UnplayableNoAudioFlow).Error()
}

// History 拉一页历史记录。
//
// has_more 是 B 站直接给的，照抄——别改成「这一页不满就当到底」。
func (l *Library) History(ctx context.Context, page int, q LibraryQuery) ([]LibraryItem, bool, error) {
	if l == nil || l.Client == nil {
		return nil, false, errLibraryMissing
	}
	pg, err := l.Client.HistorySearch(ctx, bili.HistoryQuery{
		Page: page, Keyword: q.Keyword, From: q.From, To: q.To,
	})
	if err != nil {
		return nil, false, err
	}

	items := make([]LibraryItem, 0, len(pg.Items))
	for _, it := range pg.Items {
		if it.Bvid == "" {
			continue // 同 ToView：没有 bvid 就没有身份，也没有能播的一首。
		}
		// 🔴 铁律四：身份是 **bvid + 观看时刻**，不是 bvid。
		//
		// 同一首歌看过两次在 B 站就是两条记录，两个条目共用一个 bvid——拿 bvid
		// 当身份，这两行的选中状态会串在一起（点一行，两行都亮）。上游也是这么
		// 做的：`pages/history/list.tsx:71` 的
		// `key={`${item.history.oid}-${item.view_at}`}`。
		//
		// ⚠️ **不是 `kid`**：kid 认的是**资源**（`archive_{稿件id}`），两遍
		// 观看是同一个 kid。Biu 删除时拼的那个 kid 就是这个（`pages/history/
		// index.tsx:223`），所以它是删除的凭据，不是行的身份。
		id := historyID(it.Bvid+"@"+strconv.FormatInt(it.ViewAt.Unix(), 10), it.Kid)
		canPlay := it.Business == "archive"
		items = append(items, LibraryItem{
			ID:       id,
			Title:    it.Title,
			Cover:    it.Cover,
			Owner:    it.AuthorName,
			OwnerMid: it.AuthorMid,
			Duration: it.Duration,
			Progress: it.Progress,
			ViewedAt: it.ViewAt,
			// 只有投稿能放：Biu 对 PGC 不给播放入口（history/menu.tsx 的
			// `canPlay = business === "archive"`）。
			CanPlay: canPlay,
			// 同上：按不动的那一行要说清是**哪一种**。历史里放不了的是
			// 直播、专栏、番剧那些不是投稿的东西——它们压根没有音频轨。
			CannotPlay: historyCannotPlayReason(canPlay),
			Item:       onlineItem(it.Bvid, it.Cid, it.Title, it.Cover, it.AuthorName, it.AuthorMid),
		})
	}
	return items, pg.HasMore, nil
}

// RemoveLater 从稍后再看里移除一行。id 是那一行的 `LibraryItem.ID`。
func (l *Library) RemoveLater(ctx context.Context, id string) error {
	if l == nil || l.Client == nil {
		return errLibraryMissing
	}
	aid, ok := laterAidOf(id)
	if !ok {
		return fmt.Errorf("app: 「%s」不是一条稍后再看，移除不了", id)
	}
	return l.Client.ToViewRemove(ctx, aid)
}

// ClearViewedLater 清掉稍后再看里**已看完**的那些（上游页头右上角那个
// 「清除已看完」，`later/index.tsx:218-224`）。
//
// ⚠️ **它是一次不可恢复的批量删除**：B 站那边没有「撤销」，清掉的就再也回不来
// （只有用户自己在 B 站 APP 里加回来的那条路）。所以界面层必须先问一句
// （`internal/ui/confirmmodal.go` 那一套），这一层只管发请求。
func (l *Library) ClearViewedLater(ctx context.Context) error {
	if l == nil || l.Client == nil {
		return errLibraryMissing
	}
	return l.Client.ToViewRemoveViewed(ctx)
}

// RemoveHistory 删掉一条历史记录。id 是那一行的 `LibraryItem.ID`。
func (l *Library) RemoveHistory(ctx context.Context, id string) error {
	if l == nil || l.Client == nil {
		return errLibraryMissing
	}
	kid, ok := historyKidOf(id)
	if !ok {
		return fmt.Errorf("app: 「%s」不是一条历史记录，删除不了", id)
	}
	return l.Client.HistoryDelete(ctx, kid)
}

// ─────────────────────────────────────────────────────────────────────────────
// 行身份
//
// 两页的行身份都带页名前缀，免得两个列表的行 ID 撞上——它们在同一个进程里
// 同时活着（用户从侧栏来回切），撞上的话串的是**另一页**的状态，而那种错在
// 界面上完全看不出来。
// ─────────────────────────────────────────────────────────────────────────────

const (
	laterIDPrefix   = "later:"
	historyIDPrefix = "history:"
	// laterIDSeparator 分开 bvid 和稿件 id（见 laterID）。
	laterIDSeparator = "#"
)

// laterID 是稍后再看一行的身份：`later:{bvid}#{aid}`。
//
// ⚠️ **带稿件 id 不是多余的**：移除接口只收 `aid`（B 站的
// `/x/v2/history/toview/del`），而从 bvid 反解出 aid 要 B 站那套 base58 表，
// 抄错一位就是删掉别人的东西。把两者一起编进行身份，界面手上就有能删的那一个，
// 这一层也不用在内存里留一张「id → aid」的表（那一表在删完之后就成脏数据了）。
//
// bvid 在前是因为它才是这一行的自然身份；# 后面那一位是附带的，不影响唯一性
// （bvid 定了 aid 就定了）。
func laterID(bvid string, aid int64) string {
	return laterIDPrefix + bvid + laterIDSeparator + strconv.FormatInt(aid, 10)
}

// laterAidOf 从稍后再看那一行的身份里取回稿件 id。
func laterAidOf(id string) (int64, bool) {
	body, ok := strings.CutPrefix(id, laterIDPrefix)
	if !ok {
		return 0, false
	}
	// 取**最后**一个 #：bvid 是 B 站给的用户数据，理论上可能自己带一个。
	_, tail, ok := strings.Cut(body, laterIDSeparator)
	if !ok {
		return 0, false
	}
	aid, err := strconv.ParseInt(tail, 10, 64)
	if err != nil || aid <= 0 {
		return 0, false
	}
	return aid, true
}

// historyIDSeparator 分开「身份」和「删除凭据」（见 historyID）。
const historyIDSeparator = "#"

// historyID 是历史记录一行的身份：`history:{bvid}@{观看时刻}[#{kid}]`。
//
// `#` 后面那一段是**删除凭据**，不参与唯一性（kid 认资源不认次数，见 ToView
// 里那段）。它挂在身份里是因为删除要的就是它，而界面上只应该拿 ID 去删——
// 界面上多一个「kid」字段，就多一个可能拿它当身份用的地方。
//
// kid 为空（直播 / 专栏那几类）时身份里就没有 `#` 那一段，于是删不了（见
// historyKidOf），而**行照样画得出来**。
func historyID(body, kid string) string {
	id := historyIDPrefix + body
	if kid != "" {
		id += historyIDSeparator + kid
	}
	return id
}

// historyKidOf 从历史记录那一行的身份里切出 B 站那个 kid。
//
// 认不出来（没有 `#` 那一段、或者前缀不对）就说删不了——发一个空的 kid 过去，
// B 站回 0（成功）而什么都没删，用户以为删掉了。
func historyKidOf(id string) (string, bool) {
	body, kid, ok := strings.Cut(id, historyIDSeparator)
	if !ok || kid == "" {
		return "", false
	}
	// 身份那一段也必须是历史记录的：拿稍后再看的身份来删，# 后面那一位是稿件
	// id，不是 kid——发过去 B 站会回 0 而什么都没删。
	if _, ok := strings.CutPrefix(body, historyIDPrefix); !ok {
		return "", false
	}
	return kid, true
}

// historyCannotPlayReason 是历史记录里放不了的那一类的理由。
//
// 放得了的话给空串。
func historyCannotPlayReason(canPlay bool) string {
	if canPlay {
		return ""
	}
	return "这一条不是投稿（直播、专栏、番剧那一类），B 站没有给它音频流。搜同名的歌再放"
}

// onlineItem 造一个在线队列项.
//
// ownerName / ownerMid 是这一首的 UP 主，播放条上那一句要显示它、点它进他的
// 空间（见 `Item.Artist`）。**两个都要传**：只给名字的话播放条上那一句点不动，
// 而调用方手上通常两个都有（B 站这几条接口都带 `upper`/`owner`）。
//
// 🔴 **cid 是 0 时要空着，不是字符串 `"0"`**（票：首页榜单点不动）：
// `Item.Cid` 是字符串，而在线播放那一路判的是「空串 = 缺，去问一次」
// （`withCid`）。零拼成字符串是 `"0"`，它**冒充一个真的分 P id**——
//
//	withCid 看到 "0" ≠ "" → 不去问分 P
//	PlayURL 看到 "0" ≠ "" → 把 cid=0 真发出去 → B 站回 -400「请求错误」
//
// 而 B 站有一批接口就是不给分 P id（音乐榜的 `creation_first_cid`、新歌速递
// 的 `cid`），那批接口给的数字是**零**。所以这里判的是「小于等于零」。
//
// ⚠️ **别改成「等于零」**：负数拼出来是 `"-1"`，它一样冒充真的分 P id。
func onlineItem(bvid string, cid int64, title, cover, ownerName string, ownerMid int64) Item {
	return Item{
		Source:    "online",
		Type:      "mv",
		Bvid:      bvid,
		Cid:       bili.ParseCid(cid),
		Title:     title,
		Cover:     cover,
		Artist:    ownerName,
		ArtistMid: ownerMid,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 挂到 App 上
//
// 和 `FavoritesOf` 同一个道理：挂载关系记在包级的表里，App 的结构体写在
// app.go 里，那一行不在这一票的改动范围（见 favorites.go 上面的那段说明）。
// ─────────────────────────────────────────────────────────────────────────────

// MountLibrary 把这两页要的那几个接口挂到 App 上。**在开窗口之前调**。
func (a *App) MountLibrary(cli *bili.Client) {
	a.mu.Lock()
	a.library = &Library{Client: cli}
	a.mu.Unlock()
}

// Mounted 报这一摊挂上没有。界面靠它说清「还没接上」，而不是发一个注定失败
// 的请求、再把那个失败当成用户的错报出去。
func (l *Library) Mounted() bool { return l != nil && l.Client != nil }

// LibraryOf 是这个 App 的「稍后再看 / 历史记录」这一摊。还没挂过就现建一个
// 空的：查询会回 errLibraryMissing，界面据此说清「没接上」，而不是画一个
// 永远空的列表（空列表和「没接上」在用户眼里是两件事）。
func (a *App) LibraryOf() *Library {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.library == nil {
		a.library = &Library{}
	}
	return a.library
}
