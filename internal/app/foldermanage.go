// 收藏夹的管理这一摊：拉全量、新建 / 修改、上传封面、往里放东西。
//
// # 为什么挂在 `collections` 上而不是新开一个 Mount
//
// 三个弹窗（收藏夹编辑 / 封面裁剪 / 收藏夹选择）用的都是**同一个** B 站
// 客户端和同一张凭据白名单，也就是 `MountCollections` 已经递进来的那一个。
// 再开一个 `MountFolders(a, cli)` 只会多一处「这个挂了那个没挂」的判断，
// 而那种判断出错时的症状是「弹窗打开了，提交之后报 -101」。
//
// # 后台的结果从哪儿回到界面上
//
// 四个弹窗的提交全是**异步**的（铁律三：界面线程上一次网络往返都不能有）。
// 后台那一趟做完要往界面递两样东西：一句话（Toast）和一份「成了没」的
// 判据（弹窗该不该关）。它们走同一个出口 `folderResult`：
//
//   - **取走而不是读**（见 `TakeFolderResult`）。读而不取的话同一条会在
//     每一帧都发一次 Toast、而弹窗会在第一帧就关掉（那一次是对的，之后
//     每一帧都白关一次——用户连改好的名字都来不及看）。
//   - **带 ID 且每次 +1**。理由同 `collectionNotice.add`。
//
// # 界面状态不在这一层
//
// 裁剪框在哪儿、弹窗开没开、勾了哪些——那些留在界面层（铁律一）。
package app

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// folderChoices 是「我创建的收藏夹」拉回来之后的那一份，按问的是哪个内容分组。
//
// ⚠️ **分组键里必须带上 rid**：选择弹窗问的是「这条内容在不在每个收藏夹里」，
// 而那个答案**因内容而异**。只用 rid=0 拉一份的话，第二首歌打开弹窗时看到
// 的勾选是上一首歌的——用户看到的是「这首歌已经在里面了」，而点确认之后
// B 站会把一条 del_media_ids 发出去把别的歌踢掉。
type folderChoices struct {
	mu sync.RWMutex
	// rid 是这一份是替哪个内容拉的（0 表示不关心）。
	rid int64
	// loaded 是这一份是不是**完整的**（翻完了，不是只拿到第一页）。
	loaded bool
	list   []bili.CreatedFolder
}

// get 读一份。没拉过是空 + loaded=false。
func (f *folderChoices) get(rid int64) ([]bili.CreatedFolder, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.rid != rid {
		return nil, false
	}
	return append([]bili.CreatedFolder(nil), f.list...), f.loaded
}

// put 存一份完整的。
func (f *folderChoices) put(rid int64, list []bili.CreatedFolder) {
	f.mu.Lock()
	f.rid, f.loaded, f.list = rid, true, append([]bili.CreatedFolder(nil), list...)
	f.mu.Unlock()
}

// FolderResult 是后台那一趟做完之后回来的结果。
//
// **必须取走**（见 `TakeFolderResult`）。ID 每次 +1，界面拿它认出「这是新的
// 一条」——两个弹窗可能同时开着（编辑弹窗里点上传封面会开出裁剪弹窗），
// 靠内容判重的话失败的那条和成功的那条长得一样。
type FolderResult struct {
	ID uint64
	// OK 是成了没有。!OK 时 Text 是要说的那句人话。
	OK bool
	// Text 是要跟用户说的话（无论成败都要说：成了说「已保存」）。
	Text string
	// ID 是新建 / 改的是哪一个收藏夹。失败时是 0。
	Folder int64
}

// folderResult 是三个弹窗共用的结果出口。
type folderResult struct {
	mu     sync.Mutex
	nextID uint64
	// unread 是还没被界面取走的那几条。
	unread []FolderResult
}

func (r *folderResult) add(res FolderResult) {
	r.mu.Lock()
	r.nextID++
	res.ID = r.nextID
	r.unread = append(r.unread, res)
	r.mu.Unlock()
}

// take 取走最早那一条，取走过就没有了。
func (r *folderResult) take() (FolderResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.unread) == 0 {
		return FolderResult{}, false
	}
	res := r.unread[0]
	r.unread = append(r.unread[:0:0], r.unread[1:]...)
	return res, true
}

// TakeFolderResult 取走后台攒下的第一条结果，取走之后就轮到下一条。
//
// 界面在每一帧开头调一次。**取走而不是读**：读而不取的话同一条会在每一帧
// 都发一次 Toast，而弹窗的「提交中」会在第一帧就解除——用户看着弹窗关掉了
// 却还以为没生效，又点一次提交，于是建出来两个同名收藏夹。
func (a *App) TakeFolderResult() (FolderResult, bool) {
	c := a.collectionsSnapshot()
	if c == nil {
		return FolderResult{}, false
	}
	return c.folderResult.take()
}

// ── 拉全量 ─────────────────────────────────────────────────────────────────

// LoadCreatedFolders 后台拉全量「我创建的收藏夹」。**立刻返回**。
//
// rid 是「问的是这条内容在不在里面」（0 表示不关心）。选择弹窗开着的时候
// 调它，传的是当前那一首的 rid。
//
// 🔴 **翻页的循环在这一层，不在 `bili`**：`bili` 那一层的规矩是「发一次请求
// 就返回」（见 collection.go 的包注释）。步长是 `bili.CreatedFolderPageSize`
// ——照抄那个常量，别在这儿再写一个 100，两处一漂就是「翻到第二页时步长变了」。
func (a *App) LoadCreatedFolders(rid int64) {
	c := a.collectionsSnapshot()
	if c == nil {
		return
	}
	go func() {
		list, err := a.fetchCreatedFolders(c, rid)
		if err != nil {
			log.Printf("biu: 拉收藏夹列表失败：%v", err)
			c.folderResult.add(FolderResult{Text: errTextFolder(err)})
			a.wakeUI()
			return
		}
		c.folderChoices.put(rid, list)
		a.wakeUI()
	}()
}

// fetchCreatedFolders 一页一页翻到完成为止。
//
// ⚠️ **翻页的终止条件是「这一页少于一步长」和 `has_more` 一起判**：只认
// `has_more` 的话，B 站在最后一页上说 true（它偶尔会）就会多翻一页，
// 而那一页是空的——表现是界面在收藏夹后面多出一行「空白的收藏夹」，
// 勾它等于往一个不存在的收藏夹里放东西。
func (a *App) fetchCreatedFolders(c *collections, rid int64) ([]bili.CreatedFolder, error) {
	cli := c.refs.Client
	mid := cli.SelfMid()
	if mid <= 0 {
		return nil, errNotLoggedInForFolders
	}

	var all []bili.CreatedFolder
	for page := 1; ; page++ {
		batch, hasMore, err := cli.CreatedFolders(context.Background(), mid, rid, page)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if !hasMore || len(batch) < bili.CreatedFolderPageSize {
			return all, nil
		}
		// ⚠️ 别加「翻到第 N 页就认输」那一道：收藏夹超过一万个是理论情况，
		// 而一个上界会让「用户有 201 个收藏夹」时静默丢掉第 201 个——
		// 界面完全看不出异样，而用户发现自己刚建的收藏夹不在列表里。
	}
}

// errNotLoggedInForFolders 是「拉不了自己的收藏夹，因为没登录」。
//
// 单独一个哨兵错误：界面拿它判「该提示登录」而不是「该提示网络错误」，
// 而这句话的措辞以后还会改。
var errNotLoggedInForFolders = errors.New("biu: 还没登录，拉不了自己的收藏夹")

// errTextFolder 把这一层会冒出来的错误翻成一句人话。
func errTextFolder(err error) string {
	if errors.Is(err, errNotLoggedInForFolders) {
		return "还没登录，登录之后才能用收藏夹"
	}
	return "拉不到收藏夹列表：" + bili.Friendly(err)
}

// CreatedFolderChoices 读「我创建的收藏夹」此刻那一份。
//
// 返回的第二项是「翻完了没有」。**没翻完时界面画的是正在拉**，而不是空的
// 那个当结果——两者的区别是用户会不会以为自己的收藏夹全没了。
func (a *App) CreatedFolderChoices(rid int64) ([]bili.CreatedFolder, bool) {
	c := a.collectionsSnapshot()
	if c == nil {
		return nil, false
	}
	return c.folderChoices.get(rid)
}

// ── 写 ─────────────────────────────────────────────────────────────────────

// CreateFolder 新建一个收藏夹。**立刻返回**，网络往返在后台。
//
// 成功之后把新收藏夹塞进侧栏（`FavoritesOf().Upsert`）并把结果递回去。
func (a *App) CreateFolder(draft bili.FolderDraft) {
	a.submitFolder(0, draft)
}

// EditFolder 改一个收藏夹。**立刻返回**。
func (a *App) EditFolder(id int64, draft bili.FolderDraft) {
	a.submitFolder(id, draft)
}

// submitFolder 是新建和修改背后那一个。
func (a *App) submitFolder(id int64, draft bili.FolderDraft) {
	c := a.collectionsSnapshot()
	if c == nil {
		return
	}
	go func() {
		var (
			info *bili.FolderInfo
			err  error
		)
		if id <= 0 {
			info, err = c.refs.Client.CreateFolder(context.Background(), draft)
		} else {
			info, err = c.refs.Client.EditFolder(context.Background(), id, draft)
		}
		if err != nil {
			log.Printf("biu: 改收藏夹 %d 失败：%v", id, err)
			c.folderResult.add(FolderResult{OK: false, Text: folderFailText(id, err)})
			a.wakeUI()
			return
		}

		// 侧栏立刻能看到它。⚠️ **用接口回回来的 title 而不是用户敲的那个**：
		// B 站会改（B 站对某些名字会加后缀），而侧栏下次刷新拉的是同一个值，
		// 两边不一致的话用户会看到「弹窗里叫 A，侧栏里叫 B」直到下次刷新。
		//
		// ⚠️ **封面也带上**（`bili.FolderInfo.Cover` 是有的）：用户刚换完封面，
		// 侧栏上那一枚就该是新的——下一次刷新要等网络往返，而那之前用户看到
		// 的是旧封面。
		//
		// ⚠️ **数量不在这里给**（`FolderInfo` 没有 media_count 那一格）：0 在
		// 这里是「不知道」，`Favorites.Upsert` 会沿用原来那个数（见它的注释）。
		a.FavoritesOf().Upsert(FavoritesCreated, Favorite{
			ID: info.ID, Title: info.Title, Mid: info.Mid, Type: info.Type,
			Cover: info.Cover,
		})
		text := "收藏夹建好了"
		if id > 0 {
			text = "收藏夹改好了"
		}
		c.folderResult.add(FolderResult{OK: true, Text: text, Folder: info.ID})
		a.wakeUI()
	}()
}

// folderFailText 是新建 / 修改失败时说的那句话。
func folderFailText(id int64, err error) string {
	verb := "建不了收藏夹："
	if id > 0 {
		verb = "改不了这个收藏夹："
	}
	return verb + bili.Friendly(err)
}

// UploadCover 上传一张封面。**立刻返回**，地址回来之后从 `TakeFolderResult`
// 的 `Folder` 那一栏拿不到——封面地址单独一个出口，见 `TakeCoverURL`。
func (a *App) UploadCover(dataURL string) {
	c := a.collectionsSnapshot()
	if c == nil {
		return
	}
	go func() {
		url, err := c.refs.Client.UploadCover(context.Background(), dataURL)
		if err != nil {
			log.Printf("biu: 上传封面失败：%v", err)
			c.folderResult.add(FolderResult{OK: false, Text: "封面传不上去：" + bili.Friendly(err)})
			a.wakeUI()
			return
		}
		c.coverURL.put(url)
		a.wakeUI()
	}()
}

// coverURL 是最近一次上传成功回来的封面地址。
type coverURL struct {
	mu     sync.Mutex
	nextID uint64
	have   bool
	url    string
}

func (u *coverURL) put(v string) {
	u.mu.Lock()
	u.nextID++
	u.have, u.url = true, v
	u.mu.Unlock()
}

// take 取走那个地址。取走过就没有了——下一次传别的图时会重新来一份。
func (u *coverURL) take() (string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.have {
		return "", false
	}
	u.have = false
	return u.url, true
}

// TakeCoverURL 取走最近一次上传成功回来的封面地址。
//
// **和 `TakeFolderResult` 分开是两个出口**：封面上传成功**不是**一句话要说的
// 提示，它是一份要给表单用的数据。混在一个出口里的话，界面就得从每一条
// Toast 结果里猜「这一条是不是封面」——而失败的提示长得和成功的一样。
func (a *App) TakeCoverURL() (string, bool) {
	c := a.collectionsSnapshot()
	if c == nil {
		return "", false
	}
	return c.coverURL.take()
}

// DealFolderMedias 往收藏夹里放几个内容、从里面拿出几个。
//
// **同步**（调用方在后台 goroutine 里）。界面那一侧要的是「点完确认弹窗
// 不立刻关、按钮转圈、等结果回来再关」——那是界面层的 `submitting`。
func (a *App) DealFolderMedias(rid int64, contentType int, add, del []int64) {
	c := a.collectionsSnapshot()
	if c == nil {
		return
	}
	go func() {
		if err := c.refs.Client.DealFolderMedias(context.Background(), rid, contentType, add, del); err != nil {
			log.Printf("biu: 改收藏夹 %d 的内容失败：%v", rid, err)
			c.folderResult.add(FolderResult{OK: false, Text: "改不了收藏夹：" + bili.Friendly(err)})
			a.wakeUI()
			return
		}
		c.folderResult.add(FolderResult{OK: true, Text: dealDoneText(len(add), len(del))})
		a.wakeUI()
	}()
}

// dealDoneText 是改完收藏夹内容之后说的那句话。
//
// 分三档，照 Biu（`components/favorites-select-modal/index.tsx:125-138`）：
// 原来一个都没勾、现在有 → 「已添加」；现在一个都没勾 → 「已移除」；
// 其余 → 「修改成功」。三句话对三件事，说错的话用户会以为自己的勾白勾了。
func dealDoneText(added, removed int) string {
	switch {
	case added > 0 && removed == 0:
		return "已添加到收藏夹"
	case added == 0 && removed > 0:
		return "已从收藏夹中移除"
	case added == 0 && removed == 0:
		return "收藏夹没有变化"
	}
	return "修改成功"
}

// ── 改之前先把原来那份拉回来 ───────────────────────────────────────────────

// FolderDraftView 是一个收藏夹现在的样子，填进编辑弹窗的表单。
//
// ⚠️ **`Public` 是「是不是公开」，不是 B 站的 `privacy`**：服务层那边是 0 公开
// 1 私密，而这里问的是界面那个开关。翻两次的后果是「界面上选了公开、传过去
// 的是 1」，B 站那边不报错——用户建出来的是私密收藏夹，而界面上写着公开。
type FolderDraftView struct {
	ID     int64
	Title  string
	Intro  string
	Cover  string
	Public bool
}

// folderDraft 是后台拉回来那一份的存放处（带锁：写它的是后台 goroutine）。
type folderDraft struct {
	mu     sync.Mutex
	loaded bool
	view   FolderDraftView
}

func (d *folderDraft) put(v FolderDraftView) {
	d.mu.Lock()
	d.loaded, d.view = true, v
	d.mu.Unlock()
}

// take 取走。**取走过就没有了**——理由同 `TakeFolderResult`：读而不取的话
// 表单会被填第二遍，而第二遍会把用户在这中间敲的字冲掉。
func (d *folderDraft) take() (FolderDraftView, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.loaded {
		return FolderDraftView{}, false
	}
	d.loaded = false
	return d.view, true
}

// LoadFolder 后台拉一个收藏夹现在的样子。**立刻返回**。
//
// 「修改」弹窗打开时调它。侧栏那一行只有标题，名称之外的三个字段（简介、封面、
// 公开私密）一个都推不出来——照着标题填进去，其余三项会**存成空**，而用户
// 以为他只是改了名字。
func (a *App) LoadFolder(id int64) {
	c := a.collectionsSnapshot()
	if c == nil {
		return
	}
	go func() {
		raw, err := c.refs.Client.CollectionInfo(context.Background(), id, bili.CollectionFavorite)
		if err != nil {
			log.Printf("biu: 拉收藏夹 %d 的信息失败：%v", id, err)
			c.folderResult.add(FolderResult{OK: false, Text: "打不开这个收藏夹：" + bili.Friendly(err)})
			a.wakeUI()
			return
		}
		c.folderDraft.put(FolderDraftView{
			ID: raw.ID, Title: raw.Title, Intro: raw.Intro, Cover: raw.Cover,
			// attr 第 0 位是「私密」，界面那个开关问的是「是不是公开」。
			Public: !raw.Private,
		})
		a.wakeUI()
	}()
}

// TakeFolderDraft 取走后台拉回来的那一份表单，取走之后就轮到下一份。
func (a *App) TakeFolderDraft() (FolderDraftView, bool) {
	c := a.collectionsSnapshot()
	if c == nil {
		return FolderDraftView{}, false
	}
	return c.folderDraft.take()
}
