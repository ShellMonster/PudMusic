// 歌单内容页的业务层：收藏夹 / 合集 / 系列（PLAN 阶段 5 的 P-17~P-22）。
//
// # 这一层管三件事
//
//   - **头图区那份元信息的缓存**。界面每一帧读它，后台拉回来写它——
//     没有缓存的话头图区每帧都要发一次请求。
//   - **收藏 / 取消收藏，以及收藏之后那 500 毫秒**（`favoriteSettleDelay`）。
//     这是 Biu 上游绕 B 站最终一致性的一段绕行，理由写在那个常量上。
//   - **后台操作完了要跟界面说的话**（`CollectionNotice`）。即时模式下后台
//     改了状态不叫醒界面，界面就永远停在旧帧（铁律三）。
//
// # 这一层不管什么
//
// 勾选、排序下拉开没开、批量操作条在不在——这些是**界面状态**，留在界面层
// （铁律一：界面状态只在主线程上动）。这里只留「换一页容器就要重来一次」
// 的那些。
//
// # 网络往返一律在后台
//
// 拉列表、拉元信息、写操作都是带凭据的网络请求（实测 200~400 ms）。
// 直接在界面线程上调就是每点一次冻一下窗口，所以没有一个方法是同步的——
// 除了 `LoadCollectionPage`，而它是被 `widgets/list` 的 `Fetcher` 调用的，
// 那本来就在后台 goroutine 里。
package app

import (
	"context"
	"errors"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// CollectionRefs 是歌单这一摊事的依赖。零值不挂——界面要判空。
//
// ⚠️ `Settle` 和 `Downloader` 都不是必填的：
//
//   - `Settle` 零值用 `favoriteSettleDelay`；测试会把它设小，见 clock.go 的
//     「假时钟」那一套。
//   - `Downloader` 为 nil 是**正常状态**：批量下载要接的是 issue #29 那一票，
//     它还没开工。界面拿到 nil 会把按钮按成不可用并说清楚，而不是假装能下。
type CollectionRefs struct {
	Client *bili.Client

	// Downloader 是批量下载那一摊。留接口不实现，理由见 Downloader。
	Downloader Downloader

	// Settle 是收藏 / 取消收藏之后等多久才去刷新元信息。
	Settle time.Duration
}

// Downloader 是「批量下载」要接的那一份服务。
//
// **这一票只留接口，一行实现都没有**：issue #29（下载服务）还没开工，而
// 下载要碰的东西（分 P、并发、断点续传、磁盘配额）没有一样属于这一页。
// 现在把**接线**定死——谁调它、传什么、没挂上时界面说什么——那一票来了只要
// 补一个实现，不用再回头改这一页。
//
// 为什么是接口而不是一个函数字段：实现那一侧要能挂自己的状态（队列、进度），
// 函数字段装不下，而整包先定义接口是 Go 的惯例。
type Downloader interface {
	// EnqueueDownload 把这些条目排进下载队列。folder 是它们来自哪个容器
	// （下载任务要在界面上写「来自哪个收藏夹」）。
	//
	// 它是**后台**调的（见 CollectionRefs 的包注释），所以实现里不许碰界面
	// 状态，要报进度就走别的路。
	EnqueueDownload(ctx context.Context, folder CollectionRef, medias []bili.Media) error
}

// favoriteSettleDelay 是收藏 / 取消收藏之后等多久才去刷新元信息。
//
// 🔴 **这 500 毫秒不是玄学，是 B 站那边的最终一致性**：收藏接口返回成功之后，
// `folder/info` 有一小段时间仍然报着旧状态。不等就直接刷新，用户看到的是
// 「点了收藏，界面上立刻又变回没收藏」——而它下一次自己变过来要等到用户
// 手动刷新页面。Biu 上游就在这儿等了这 500ms
// （`biu/src/pages/video-collection/favorites/index.tsx` 的 toggleFavorite：
// `await new Promise(resolve => setTimeout(() => { refreshInfo(); resolve(null) }, 500))`）。
//
// 少等不会报错、不会崩，只是界面上那个星标会说谎——所以它是 500 而不是 0。
const favoriteSettleDelay = 500 * time.Millisecond

// CollectionRef 指向一个容器（收藏夹 / 合集 / 系列）。
type CollectionRef struct {
	// ID 是 `media_id`（收藏夹的 mlid，不是原始 fid——完整 id）。
	ID int64
	// Type 是哪一类容器。
	Type bili.CollectionType
}

// Key 是这一份状态属于哪个容器。界面拿它认出「换容器了，该重建了」。
//
// Type 也要进去：同一个 id 在两种类型下不是同一个东西（B 站的 mlid 里已经
// 带了创建者，只按 id 认的话改一次类型会读到上一个容器的缓存）。
func (r CollectionRef) Key() string {
	return strconv.FormatInt(r.ID, 10) + ":" + strconv.Itoa(int(r.Type))
}

// CollectionInfo 是这一页要读的那份元信息（顶部头图区画的就是它）。
//
// ⚠️ Loaded 为 false 是**还在拉**，不是「没有」：头图区这时候要画占位，
// 而画成「这个收藏夹叫空名字」会让用户以为接口回的是空标题。
type CollectionInfo struct {
	Ref CollectionRef

	Title      string
	Intro      string
	Cover      string
	MediaCount int
	OwnerName  string
	Private    bool
	Faved      bool

	Loaded bool
}

// collectionCache 是容器元信息的一份缓存，按容器分格。
//
// 为什么要有缓存：界面每一帧都要画头图区，而不画就发请求的话一秒能发
// 六十次（真窗口里 Invalidate 会被 50ms 的播放条时钟带着跑）。
type collectionCache struct {
	mu    sync.RWMutex
	items map[string]CollectionInfo
}

// get 取一份。没拉过是零值（Loaded=false）。
func (c *collectionCache) get(ref CollectionRef) CollectionInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items[ref.Key()]
}

// put 存一份。
func (c *collectionCache) put(info CollectionInfo) {
	c.mu.Lock()
	if c.items == nil {
		c.items = map[string]CollectionInfo{}
	}
	c.items[info.Ref.Key()] = info
	c.mu.Unlock()
}

// collectionNotice 一句话，给界面读的。
type collectionNotice struct {
	mu     sync.Mutex
	nextID uint64
	// unread 是还没被界面取走的那几条。
	unread []CollectionNotice
}

// take 取走属于 key 的最早那一条，取走过就没有了。
func (n *collectionNotice) take(key string) (CollectionNotice, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, c := range n.unread {
		if c.Ref.Key() == key {
			n.unread = append(n.unread[:i:i], n.unread[i+1:]...)
			return c, true
		}
	}
	return CollectionNotice{}, false
}

// add 记一句要跟用户说的话。界面下一帧会取走它（取走时才发 Toast）。
//
// ⚠️ **同一条内容说第二遍时 ID 一定要变**：界面是「取到新的一条才发 Toast」
// 的，拿 ID 判重、而 ID 不变的话，第二遍就不会说了——「已收藏」只出现一次，
// 用户会以为第二次点击没生效。
func (n *collectionNotice) add(ref CollectionRef, text string) {
	n.mu.Lock()
	n.nextID++
	n.unread = append(n.unread, CollectionNotice{ID: n.nextID, Ref: ref, Text: text})
	n.mu.Unlock()
}

// CollectionNotice 是后台操作完了之后要跟用户说的一句话。
type CollectionNotice struct {
	// ID 是一次性的：界面靠它认出「这是新的一条」。见 add 的注释。
	ID   uint64
	Ref  CollectionRef
	Text string
}

// errCollectionsMissing 是「歌单这一摊没挂上」。
//
// ⚠️ 别拿 `errStreamingMissing`（在线播放那个）顶替：它那句文案是「在线播放
// 还没接上」，而这一页跟在线播放一点关系没有——用户在歌单页看到一句「在线
// 播放还没接上」会以为是播放坏了。
var errCollectionsMissing = errors.New("biu: 歌单接口还没挂上")

// collections 是 App 挂着的歌单这一摊。
type collections struct {
	refs   CollectionRefs
	cache  collectionCache
	notice collectionNotice
}

// MountCollections 把歌单内容页要用的零件挂上。**在开窗口之前调**。
//
// 没有它这一页画得出来（头图区是空的、列表一直转圈），所以界面要判空
// （`CollectionsMounted`）。
func (a *App) MountCollections(refs CollectionRefs) {
	a.mu.Lock()
	a.collections = &collections{refs: refs}
	a.mu.Unlock()
}

// CollectionsMounted 报告歌单这一摊挂上没有。
func (a *App) CollectionsMounted() bool { return a.collectionsSnapshot() != nil }

// DownloaderMounted 报告批量下载的服务接上没有（issue #29）。
func (a *App) DownloaderMounted() bool {
	c := a.collectionsSnapshot()
	return c != nil && c.refs.Downloader != nil
}

// wakeUI 叫醒界面。
//
// ⚠️ **没装回调时什么都不做**：界面层装 wake 的时机是 `Shell.Attach`（建窗口
// 的时候），而这一层的后台那一趟可能跑在它之前（`App.New` 就起了播放条的
// 时钟）。直接 `a.wake()` 在那时候是空指针——崩在一个跟被测逻辑毫无关系的
// goroutine 里，而症状是「拉收藏夹拉崩了」。
func (a *App) wakeUI() {
	if w := a.wakeLocked(); w != nil {
		w()
	}
}

// collectionsSnapshot 拿一份配好的零件。没挂或者缺客户端时是 nil。
func (a *App) collectionsSnapshot() *collections {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.collections == nil || a.collections.refs.Client == nil {
		return nil
	}
	return a.collections
}

// Collection 读头图区此刻那一份元信息。
//
// 没拉过是零值（`Loaded` 为 false），界面这时候画占位。
func (a *App) Collection(ref CollectionRef) CollectionInfo {
	c := a.collectionsSnapshot()
	if c == nil {
		return CollectionInfo{Ref: ref}
	}
	if info := c.cache.get(ref); info.Loaded {
		return info
	}
	return CollectionInfo{Ref: ref}
}

// TakeCollectionNotice 取走这一页待显示的那句话，取走之后就轮到下一条。
//
// 界面在每一帧开头调一次，取到了就发 Toast。**必须取走而不是读**：读而不取
// 的话同一条会在每一帧都发一次 Toast。
func (a *App) TakeCollectionNotice(ref CollectionRef) (CollectionNotice, bool) {
	c := a.collectionsSnapshot()
	if c == nil {
		return CollectionNotice{}, false
	}
	return c.notice.take(ref.Key())
}

// settle 是收藏之后要等的那一段。零值用 favoriteSettleDelay。
func (c *collections) settle() time.Duration {
	if c.refs.Settle > 0 {
		return c.refs.Settle
	}
	return favoriteSettleDelay
}

// LoadCollection 后台拉一次元信息。**立刻返回**。
//
// 页面切到某个容器时调它一次。拉回来写进缓存并叫醒界面，所以头图区是
// 「先占位、后填上」而不是「空白到突然有」。
func (a *App) LoadCollection(ref CollectionRef) {
	c := a.collectionsSnapshot()
	if c == nil {
		return
	}
	go func() {
		info, err := a.fetchCollection(ref)
		if err != nil {
			log.Printf("biu: 拉收藏夹 %s 的信息失败：%v", ref.Key(), err)
			c.notice.add(ref, "打不开这个收藏夹："+bili.Friendly(err))
			a.wakeUI()
			return
		}
		c.cache.put(info)
		a.wakeUI()
	}()
}

// fetchCollection 拉一次元信息并翻成界面那一层用的形状。
func (a *App) fetchCollection(ref CollectionRef) (CollectionInfo, error) {
	c := a.collectionsSnapshot()
	if c == nil {
		return CollectionInfo{}, errCollectionsMissing
	}
	raw, err := c.refs.Client.CollectionInfo(context.Background(), ref.ID, ref.Type)
	if err != nil {
		return CollectionInfo{}, err
	}
	return CollectionInfo{
		Ref:        ref,
		Title:      raw.Title,
		Intro:      raw.Intro,
		Cover:      raw.Cover,
		MediaCount: raw.MediaCount,
		OwnerName:  raw.Owner.Name,
		Private:    raw.Private,
		Faved:      raw.Faved,
		Loaded:     true,
	}, nil
}

// SetFavorite 收藏 / 取消收藏这个容器。**立刻返回**，网络往返在后台。
//
// 🔴 **成功之后等 settle 这么长再刷新元信息**（见 favoriteSettleDelay）。
// 少等那一段的后果是界面上那个星标会说一次谎——点了收藏，立刻又变回没收藏。
//
// 这一条同时是**取消收藏**那一侧的要求：B 站两边都是最终一致性的，而且
// 取消收藏之后列表要跟着变（那一行该从收藏夹里没了），所以它顺手重新拉一遍
// 元信息，界面看到 `MediaCount` 变了就知道该刷新列表。
func (a *App) SetFavorite(ref CollectionRef, fav bool) {
	c := a.collectionsSnapshot()
	if c == nil {
		a.sayCollectionsMissing()
		return
	}
	settle := c.settle()
	go func() {
		ctx := context.Background()
		var err error
		if fav {
			err = c.refs.Client.FavoriteCollection(ctx, ref.ID)
		} else {
			err = c.refs.Client.UnfavoriteCollection(ctx, ref.ID)
		}
		if err != nil {
			log.Printf("biu: 收藏夹 %s 收藏状态改成 %v 失败：%v", ref.Key(), fav, err)
			c.notice.add(ref, favoriteFailText(fav, err))
			a.wakeUI()
			return
		}

		// B 站那边要一会儿才改过来（favoriteSettleDelay）。这时候界面上的
		// 星标已经按用户的意图变了，先别让它去刷新——刷回来的是旧状态。
		if settle > 0 {
			time.Sleep(settle)
		}

		info, err := a.fetchCollection(ref)
		if err != nil {
			log.Printf("biu: 收藏之后刷新收藏夹 %s 的信息失败：%v", ref.Key(), err)
			c.notice.add(ref, favoriteFailText(fav, err))
			a.wakeUI()
			return
		}
		c.cache.put(info)
		if fav {
			c.notice.add(ref, "已收藏这个收藏夹")
		} else {
			c.notice.add(ref, "已取消收藏")
		}
		a.wakeUI()
	}()
}

// favoriteFailText 是收藏 / 取消收藏失败时说的那句话。
func favoriteFailText(fav bool, err error) string {
	if fav {
		return "收藏不了：" + bili.Friendly(err)
	}
	return "取消不了收藏：" + bili.Friendly(err)
}

// RemoveMedias 把这些条目从这个容器里删掉。**同步**（调用方在后台 goroutine 里）。
//
// 一条都没给是 no-op：空请求 B 站回 -400，而「没勾任何东西就点删除」根本
// 不该发请求（`bili.RemoveCollectionMedias` 里也挡了一道）。
func (a *App) RemoveMedias(ctx context.Context, ref CollectionRef, medias []bili.Media) error {
	c := a.collectionsSnapshot()
	if c == nil {
		return errCollectionsMissing
	}
	if len(medias) == 0 {
		return nil
	}
	if err := c.refs.Client.RemoveCollectionMedias(ctx, ref.ID, medias); err != nil {
		return err
	}
	// 删完了要刷新两样：条目数（头图区写着「N 条视频」）和列表本身。
	// 列表由界面收到通知之后重新拉，这里只负责把元信息拉回来。
	go a.LoadCollection(ref)
	return nil
}

// DownloadMedias 把这些条目排进下载队列。**同步**（调用方在后台 goroutine 里）。
//
// 没挂下载服务（issue #29 还没开工）时如实报错——**不假装成功**：假装成功
// 的话界面上会弹一句「下载任务已添加」，而用户等一小时什么也不会发生。
func (a *App) DownloadMedias(ctx context.Context, ref CollectionRef, medias []bili.Media) error {
	c := a.collectionsSnapshot()
	if c == nil {
		return errCollectionsMissing
	}
	if c.refs.Downloader == nil {
		return errDownloaderMissing
	}
	if len(medias) == 0 {
		return nil
	}
	return c.refs.Downloader.EnqueueDownload(ctx, ref, medias)
}

// errDownloaderMissing 是「批量下载还没接上」。
//
// 单独一个哨兵错误（而不是拿一句字符串去比）是因为界面要拿它判「这个按钮
// 是不是可用」，而那句话的措辞以后会改。
var errDownloaderMissing = errors.New("biu: 下载服务还没接上（issue #29）")

// DownloaderMissing 报这个错是不是「下载服务还没接上」。
func DownloaderMissing(err error) bool { return errors.Is(err, errDownloaderMissing) }

// LoadCollectionPage 拉容器里的一页内容。
//
// 它是 `widgets/list` 的 `Fetcher` 背后那一支：**在后台 goroutine 里调**，
// 所以它不许碰界面状态，要通知界面就把结果按返回去。
//
// 返回的第二项是「后面还有没有」。
func (a *App) LoadCollectionPage(ctx context.Context, ref CollectionRef, sort bili.CollectionSort, page int) ([]bili.Media, bool, error) {
	c := a.collectionsSnapshot()
	if c == nil {
		return nil, false, errCollectionsMissing
	}
	pg, err := c.refs.Client.CollectionMedias(ctx, ref.ID, sort, page, 0)
	if err != nil {
		return nil, false, err
	}
	return pg.Medias, pg.HasMore, nil
}

// sayCollectionsMissing 说一句「还没挂上」。挂不上这一页画得出来，
// 所以这句话只在用户真的点了什么的时候才说。
func (a *App) sayCollectionsMissing() {
	if c := a.collectionsSnapshot(); c != nil {
		c.notice.add(CollectionRef{}, "歌单接口还没挂上")
	}
	a.wakeUI()
}
