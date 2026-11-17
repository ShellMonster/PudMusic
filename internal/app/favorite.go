// 喜欢状态：这一条在不在**绑定的那个收藏夹**里。
//
// # 它和 `LikeState` 不是一回事
//
// `LikeState`（like.go）问的是 B 站稿件的关系：赞了没有、收藏在**任意**收藏夹
// 里没有。这一页问的是一个更窄的问题——**用户绑定的那一格**里有没有它。
// 界面上所有心形（播放条 / 满屏 / 迷你 / 刘海 / 列表菜单）都只认这一个答案：
// 第一下加进去，第二下拿出来。
//
// # 为什么按内容身份缓存，而不是「当前这一首」一格
//
// 列表菜单要在悬停某一行时预取，而悬停的那一行不一定是正在放的那一首。
// 所以缓存按 `favoriteKey` 分格（mv 按 bvid、audio 按 sid；mv 只有 sid 时
// 退回 `mv|sid:`），界面每一帧问、后台回来写，换歌不会把别人的答案顶掉。
package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// favoriteEntry 是某一条内容在绑定歌单里的状态。
type favoriteEntry struct {
	known     bool
	favorited bool
	// wanted 是「那一趟正在路上」，挡住同一格的重复查询。
	wanted bool
	// gen 是这一格的代次：发起查询时领一个号，写操作成功时也 +1。
	// 迟到的查询结果对不上就丢（旧答案不许盖新答案）。
	gen     uint64
	triedAt time.Time
}

// favoriteKey 是这条内容在喜欢缓存里的槽位键。
//
// 复用 `likeKey`（mv 按 bvid、audio 按 sid），只补一种它认不出、而喜欢这条路
// 认得出的身份：**没有 bvid 的 mv 只有 sid**（音乐库条目）。空串是「没有可问
// 的关系」（本地文件、用户行）。
func favoriteKey(it Item) string {
	if k := likeKey(it); k != "" {
		return k
	}
	if it.Type == "mv" && it.Sid != "" {
		return "mv|sid:" + it.Sid
	}
	return ""
}

// CanFavorite 报这一条能不能进喜欢歌单。
//
// 判据和 `Likeable` 的同一半一样：在线的、有 B 站身份（mv 的 bvid/sid、
// audio 的 sid）。列表菜单拿它决定摆不摆「喜欢」那一项。
func CanFavorite(it Item) bool {
	return it.isOnline() && it.likeTarget() != 0
}

// FavoriteInBound 返回这一条在不在绑定的那个歌单里。**不阻塞**。
//
// 第二个返回值是「知道了没有」：第一次问、那一趟还没回来时是 false，界面上
// 按「没喜欢」画（和 `LikeState` 的零值同一条理由）。问失败也留在「不知道」，
// 过 `likeRetryAfter` 再问一次；问成功之后 `known` 一直为真。
//
// 没绑定、或者身份认不出（本地文件、用户行）都返回 `false, true`——那不是
// 「还在问」，是「确实没有」。
func (a *App) FavoriteInBound(it Item) (favorited, known bool) {
	key := favoriteKey(it)
	if key == "" || !it.isOnline() {
		return false, true
	}
	bound, ok := a.LikedFolder()
	if !ok {
		return false, true
	}

	a.favMu.Lock()
	if a.fav == nil {
		a.fav = map[string]*favoriteEntry{}
	}
	e := a.fav[key]
	if e == nil {
		e = &favoriteEntry{}
		a.fav[key] = e
	}
	if e.known {
		v := e.favorited
		a.favMu.Unlock()
		return v, true
	}
	if e.wanted || (!e.triedAt.IsZero() && time.Since(e.triedAt) < likeRetryAfter) {
		a.favMu.Unlock()
		return false, false
	}
	e.wanted = true
	e.triedAt = time.Now()
	e.gen++
	gen := e.gen
	entry := e
	hook := a.favMember
	a.favMu.Unlock()

	go a.queryFavorite(key, it, bound.ID, gen, entry, hook)
	return false, false
}

// queryFavorite 是后台那一趟「这一条在不在绑定歌单里」。
func (a *App) queryFavorite(key string, it Item, folderID int64, gen uint64, entry *favoriteEntry, hook func(context.Context, int64, int, int64) (bool, error)) {
	fav, err := a.favoriteMember(context.Background(), it, folderID, hook)
	a.finishFavorite(key, entry, gen, fav, err)
}

// favoriteMember 问一次会员：先解析内容 id（audio 的 sid 本身；mv 有 sid 就
// 用 sid，没有才去问 bvid → avid），再在「我创建的收藏夹」里翻到绑定的那一格。
func (a *App) favoriteMember(ctx context.Context, it Item, folderID int64, hook func(context.Context, int64, int, int64) (bool, error)) (bool, error) {
	var cli *bili.Client
	if c := a.collectionsSnapshot(); c != nil {
		cli = c.refs.Client
	}
	rid, typ, err := favoriteQueryTarget(ctx, it, cli)
	if err != nil {
		return false, err
	}
	if hook != nil {
		return hook(ctx, rid, typ, folderID)
	}
	if cli == nil {
		return false, errCollectionsMissing
	}
	mid := cli.SelfMid()
	if mid <= 0 {
		return false, errNotLoggedInForFolders
	}
	// 翻页的终止条件和 `fetchCreatedFolders` 一样：`has_more` 和「这一页不满」
	// 一起判，免得 B 站在最后一页上说 true 时多翻一页。
	for page := 1; ; page++ {
		batch, hasMore, err := cli.CreatedFolders(ctx, mid, rid, page)
		if err != nil {
			return false, err
		}
		for _, f := range batch {
			if f.ID == folderID {
				return f.Faved, nil
			}
		}
		if !hasMore || len(batch) < bili.CreatedFolderPageSize {
			// 翻完了都没有绑定那一格：它确实不在里面（也许已经没了）。
			// 「没找到」不是「问失败」——写的时候那一层会单独判绑定失效。
			return false, nil
		}
	}
}

// favoriteQueryTarget 认出一条内容在收藏夹接口里的 rid 和内容类型。
func favoriteQueryTarget(ctx context.Context, it Item, cli *bili.Client) (int64, int, error) {
	typ := bili.MediaVideo
	if it.Type == "audio" {
		typ = bili.MediaAudio
	}
	if it.Sid != "" {
		if n, err := strconv.ParseInt(it.Sid, 10, 64); err == nil && n > 0 {
			return n, typ, nil
		}
	}
	if it.Type == "mv" && it.Bvid != "" && cli != nil {
		aid, err := cli.ArchiveID(ctx, it.Bvid)
		if err != nil {
			return 0, 0, err
		}
		return aid, bili.MediaVideo, nil
	}
	return 0, 0, errors.New("这一首还没有可以收藏的身份")
}

// finishFavorite 记下一次查询的结果。**条目身份和代次都对得上才落账**：
//
//   - 条目指针管「换绑 / 换账号清过缓存」：清过之后同一个 key 会有一份新的
//     entry，代次又都从 1 开始，只比 key + gen 的话旧答案会被当成新答案；
//   - gen 管「同一份缓存、问过之后又写过」。
func (a *App) finishFavorite(key string, entry *favoriteEntry, gen uint64, fav bool, err error) {
	a.favMu.Lock()
	if e := a.fav[key]; e != nil && e == entry && e.gen == gen {
		e.wanted = false
		if err == nil {
			e.known, e.favorited = true, fav
		}
	}
	a.favMu.Unlock()
	if wake := a.currentWake(); wake != nil {
		wake()
	}
}

// markFavorite 是一次写成功之后把答案记进缓存。
//
// 写操作自己知道最终状态（加成功 = 在里面，删成功 = 不在），不必再等一趟
// 查询。换代次是为了让还在路上的旧查询作废（同 `setAskedLike`）。
func (a *App) markFavorite(it Item, on bool) {
	key := favoriteKey(it)
	if key == "" {
		return
	}
	a.favMu.Lock()
	if a.fav == nil {
		a.fav = map[string]*favoriteEntry{}
	}
	e := a.fav[key]
	if e == nil {
		e = &favoriteEntry{}
		a.fav[key] = e
	}
	e.known, e.favorited = true, on
	e.wanted = false
	e.gen++
	a.favMu.Unlock()
}

// clearFavorites 扔掉整份缓存：绑定换了 / 没了 / 换了账号之后，旧答案说的都是
// 另一份绑定或另一个账号里的事。
//
// 代次一起 +1：已经发出去的写请求回来时，光比绑定 id 拦不住「换了账号但 id
// 碰巧一样」那一档，得拿这个代次对号（见 `SetBoundFavorite`）。
func (a *App) clearFavorites() {
	a.favMu.Lock()
	a.fav = nil
	a.favEpoch++
	a.favMu.Unlock()
}

// favoriteEpoch 是缓存生命周期代号，清一次 +1。写请求发出去时领一个，成功
// 回填前对号。
func (a *App) favoriteEpoch() uint64 {
	a.favMu.Lock()
	defer a.favMu.Unlock()
	return a.favEpoch
}

// forgetFavoritesIfBound 在 add / del 里有绑定那一格时作废缓存。
//
// 其它收藏入口（「收藏」多选框、收藏夹页删内容）改的是同一份绑定歌单；
// 不作废的话，红心会一直保留改之前那个答案。返回有没有清。
func (a *App) forgetFavoritesIfBound(add, del []int64) bool {
	bound, ok := a.LikedFolder()
	if !ok {
		return false
	}
	for _, ids := range [][]int64{add, del} {
		for _, id := range ids {
			if id == bound.ID {
				a.clearFavorites()
				return true
			}
		}
	}
	return false
}

// forgetFavoritesIfFolder 在改的正是绑定歌单时作废缓存。返回有没有清。
func (a *App) forgetFavoritesIfFolder(id int64) bool {
	bound, ok := a.LikedFolder()
	if !ok || bound.ID != id {
		return false
	}
	a.clearFavorites()
	return true
}

// boundFolderListed 报侧栏那份「我创建的」里还有没有绑定这一格。
//
// 只在**列表已经拉过**时才算数：「还没拉」不是「真没有」（AGENTS.md 的同一
// 条判据）。它用来把 deal 的 `-400` 分开：列表里还摆着这一格时，-400 更可能
// 是容量满或参数问题，不该把用户的绑定解掉。
func (a *App) boundFolderListed(id int64) bool {
	if st := a.FavoriteState(FavoritesCreated); !st.Loaded {
		return false
	}
	for _, f := range a.SideFavorites(FavoritesCreated) {
		if f.ID == id {
			return true
		}
	}
	return false
}
