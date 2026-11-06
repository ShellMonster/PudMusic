// 点赞：当前这一首赞了没有、收藏了没有，以及那两次写（赞 / 一键三连）。
//
// # 和分集是同一个形状
//
// 「分集列表」是「现算的、缓存在 App 上、换歌就作废」的一摊（见 `fullplayer.go` 的
// `Parts` / `ensureParts`）：界面每一帧问 App 要一次，App 顺手把没问过的那一趟发出去，
// 回来之后叫醒界面。赞 / 藏状态是**一模一样**的一件事——它属于某一首，不属于用户，
// 所以也必须按槽位键（`likeKey`：mv 按 bvid、audio 按 sid）缓存、换歌就重来。抄那一
// 套而不是另发明一种，是为了让「换歌之后界面上还留着上一首的赞」这个 bug 只有一处
// 可能发生。
//
// # 为什么不放进 `State`
//
// `State` 是播放器那一摊的快照（`Snapshot()` 在 `a.mu` 里读），而赞 / 藏是**另一个锁**
// （`likeMu`）下的一份带缓存的东西——每帧去问 `Parts()` 那一类东西本来就不进 State。
// 混进去的话 `Snapshot()` 得多拿一把锁，或者赞 / 藏得改在 `a.mu` 下写；后者会让一次
// 网络往返的回调去抢播放器那把锁。
//
// # 三个入口各自的形状
//
//	LikeState()   每帧问，返回缓存（必要时触发那一趟）——**不阻塞**
//	SetLike()     点一下星标。**先本地改，后台写**；写失败改回去
//	TripleLike()  长按 2.5 秒。一趟三件事，返回 B 站说的最终状态
//
// ⚠️ **SetLike 是乐观的**：本地那一改是**同步**的（调用方那一帧回去重画就变亮了），
// 写失败会改回去并把原因交给界面。这是 Biu 的行为（`music-thumb` 那边
// `setIsThumb(nextValue)` 在 `await` 之前），理由很实在：一次往返几百毫秒，等它回来
// 再亮星星，用户会以为没按到，于是按第二次。
package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// errLikeUnavailable 是「赞不了」：没挂在线播放（也就没挂上那个 B 站 client）。
var errLikeUnavailable = errors.New("赞要连着 B 站账号用")

// likeRetryAfter 是问失败之后隔多久再问一次（和 `partsRetryAfter` 同一条理由：
// 界面每一帧都调 `LikeState`，所以这个数同时也是重试的上限频率）。
const likeRetryAfter = 15 * time.Second

// Likeable 报「当前这一首能不能赞」。
//
// 判据是「这是一首 B 站能收进收藏夹的歌」：**本地文件一律不可**（它没有
// B 站身份），在线的 mv（avid / bvid）和在线 audio（音乐库 sid）都算——
// `resource/deal` 两类都收（`MediaVideo` / `MediaAudio`），`favTarget` 认的
// 也是这一对。界面上照它摆不摆那颗心。
//
// ⚠️ **audio 靠 `Sid` 不靠 `Bvid`**：音乐库条目给的是 sid 不是 bvid——拿
// `Type == "mv"` 当判据会把整个音乐库的心形判死，而那条路径在 deal 层
// 明明收（`MediaAudio = 12`）。`favTarget` 和这里必须是同一把尺：一处松
// 一处紧，心就成了「摆在那儿却点了说『还没有可以收藏的身份』」的死键。
func (a *App) Likeable() bool {
	it, ok := a.CurrentItem()
	if !ok || !it.isOnline() {
		return false
	}
	return it.likeTarget() != 0
}

// likeTarget 是「这一首能不能走喜欢 / 收藏那一条路」的判定：返回它的
// B 站身份类型码，0 是「不能」。它和 `favTarget`（`folderselect.go`）是
// **同一把尺的两个切面**——这里只判「有没有一份够收藏的身份」，不真去
// 解析 rid（`Sid` 串不一定是个数，那一半交给 favTarget）。
//
//	mv   → 有 Bvid 或 Sid 就行（avid 可以事后问出来，见 ResolveFavoriteID）
//	audio → 要有 Sid（音乐库没有 avid 可问，sid 本身就是 deal 要的）
func (it Item) likeTarget() int {
	switch it.Type {
	case "mv":
		if it.Bvid != "" || it.Sid != "" {
			return 1
		}
	case "audio":
		if it.Sid != "" {
			return 1
		}
	}
	return 0
}

// LikeState 返回当前这一首的赞 / 藏状态，顺手把没问过的那一次问回来。
//
// **返回的是缓存**：第一次那一趟还没回来时给的是零值（没赞没藏），界面上是「没赞」
// ——而 Biu 那边也是这个形状（`refreshIsFav` 之前一直是 `false`）。
//
// ⚠️ **零值和「问过之后是没赞」在界面上长得一样**，而它们对长按三连的意义完全相反：
// 前者「还不知道能不能赞」，后者「赞过了，长按要禁用」。所以界面那一层**不要**只拿它
// 判「能不能长按」，要连 `LikeAsked` 一起看（见 `likeArmed`）。
func (a *App) LikeState() bili.ArchiveRelation {
	rel, _ := a.likeSnapshot()
	return rel
}

// likeKey 是「这份赞 / 藏缓存属于谁」的槽位键（和 `commentSlotKey` 同一个
// 拼法）：mv 按 bvid、audio 按 sid。**认不出的返回空串**——空串是「这一首
// 没有关系可问」，缓存那一侧照它走「不可赞」那支。
func likeKey(it Item) string {
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

// LikeAsked 报「当前这一首的赞 / 藏问过了没有」。
//
// 它只认**当前这一首**：换歌之后那一问还没回来时它也是 false，而那正是「还没问」
// 的意思。界面用它决定长按三连要不要开着。
func (a *App) LikeAsked() bool {
	it, ok := a.CurrentItem()
	if !ok || !it.isOnline() || likeKey(it) == "" {
		return true // 压根不可赞的那一种，不存在「还没问」
	}
	a.likeMu.Lock()
	defer a.likeMu.Unlock()
	return a.likeKey == likeKey(it) && a.likeLoaded
}

// LikeErr 是最近一次问赞 / 藏失败的原因。界面拿它说「问不出赞没赞」。
func (a *App) LikeErr() error {
	a.likeMu.Lock()
	defer a.likeMu.Unlock()
	return a.likeErr
}

// likeSnapshot 读缓存，并顺手保证正在问 / 已经问过当前这一首。
//
// 第二返回值是「缓存属于当前这一首没有」。**假的那一份不能给界面**：换歌之后、
// 那一问还没回来时，缓存里还留着上一首的赞。
func (a *App) likeSnapshot() (bili.ArchiveRelation, bool) {
	it, ok := a.CurrentItem()
	key := likeKey(it)
	if !ok || !it.isOnline() || key == "" {
		return bili.ArchiveRelation{}, true
	}
	a.ensureLike(it, key)
	a.likeMu.Lock()
	defer a.likeMu.Unlock()
	if a.likeKey != key {
		return bili.ArchiveRelation{}, false
	}
	return a.like, true
}

// ensureLike 保证正在问 / 已经问过这一首的赞藏状态。
//
// 换稿就重置（和 `ensureParts` 同一条）：`likeKey` 是这份缓存属于谁。
// 迟到的结果发现键对不上直接丢。
//
// 🔴 **audio 和 mv 问的不是同一个接口**：mv 走 `archive/relation`（bvid
// 能换来 avid），而 audio 在 B 站那边是 `sid`——relation 一个都不收它，
// 它「收没收藏」要问 `coll/resource/check`（上游 `music-fav.ts` 的
// `refreshIsFav` 对 audio 走的就是那一支）。拿 bvid 当唯一键的话，
// audio 的 `Sid` 进不了 `likeBvid`，关系永远是空——心形摆在那儿、
// 加进绑定歌单之后还是空心。
func (a *App) ensureLike(it Item, key string) {
	a.likeMu.Lock()
	if a.likeKey != key {
		a.likeKey, a.like, a.likeLoaded, a.likeWanted = key, bili.ArchiveRelation{}, false, false
		a.likeErr, a.likeTriedAt = nil, time.Time{}
	}
	if a.likeLoaded || a.likeWanted {
		a.likeMu.Unlock()
		return
	}
	// 退避：界面每一帧都调，不等一下的话失败一次就是每秒一发。
	if !a.likeTriedAt.IsZero() && time.Since(a.likeTriedAt) < likeRetryAfter {
		a.likeMu.Unlock()
		return
	}
	a.likeWanted = true
	a.likeTriedAt = time.Now()
	// 清掉上一次的失败原因：这一趟正在问，界面上那句「问不出赞没赞」就该撤了。
	a.likeErr = nil
	a.likeMu.Unlock()

	// audio 那一条走 coll/resource/check：sid 本身就是它要的 rid，没有
	// bvid → avid 那一趟可问。结果只有「收没收藏」一栏（relation 的 like
	// 那一栏 B 站对 audio 不给——Biu 那边照样给的是 false）。
	if it.Type == "audio" {
		a.askAudioLike(it, key)
		return
	}

	// 后台那一趟。取流零件没挂上（`MountStreaming` 还没跑）也算失败：照样记下原因、
	// 照样隔一会儿再问，而不是「当问过了」——那会让挂着零件之后再也不问。
	//
	// `likeFetch` 在这里就被取出来递给那个 goroutine，goroutine 不碰这个字段，
	// 所以它不需要锁（和 `partsFetch` 同一条）。
	fetch := a.likeFetch
	if fetch == nil {
		s := a.streamingSnapshot()
		if s == nil || s.Client == nil {
			a.finishLike(key, bili.ArchiveRelation{}, errLikeUnavailable)
			return
		}
		fetch = s.Client.ArchiveRelation
	}
	go func() {
		rel, err := fetch(context.Background(), it.Bvid)
		if err != nil {
			a.finishLike(key, bili.ArchiveRelation{}, errors.New(bili.Friendly(err)))
			return
		}
		a.finishLike(key, rel, nil)
	}()
}

// askAudioLike 是 audio 那一支：问「这条 sid 收进收藏夹了没有」。
//
// ⚠️ **`collCheck` 和 `likeFetch` 同一条规矩**：只由界面那一侧读写，测试里
// 替成假函数；生产上永远是 nil（走收藏夹零件上的 `bili.Client.CollResourceCheck`
// ——挂在 `MountCollections` 上的那一个，不是取流那个）。
func (a *App) askAudioLike(it Item, key string) {
	rid, err := strconv.ParseInt(it.Sid, 10, 64)
	if err != nil || rid <= 0 {
		// sid 不是一个合法 id：这不是「问失败」，是「压根没有可问的身份」——
		// 记成问过（关系是零值），不然它每 `likeRetryAfter` 都要空转一次。
		a.finishLike(key, bili.ArchiveRelation{}, nil)
		return
	}
	check := a.collCheck
	if check == nil {
		c := a.collectionsSnapshot()
		if c == nil || c.refs.Client == nil {
			a.finishLike(key, bili.ArchiveRelation{}, errLikeUnavailable)
			return
		}
		check = c.refs.Client.CollResourceCheck
	}
	go func() {
		faved, err := check(context.Background(), rid, bili.MediaAudio)
		if err != nil {
			a.finishLike(key, bili.ArchiveRelation{}, errors.New(bili.Friendly(err)))
			return
		}
		a.finishLike(key, bili.ArchiveRelation{Favorited: faved}, nil)
	}()
}

// finishLike 记下一次问赞藏的结果并叫醒界面。
func (a *App) finishLike(key string, rel bili.ArchiveRelation, err error) {
	a.likeMu.Lock()
	if a.likeKey == key {
		a.like, a.likeErr = rel, err
		a.likeLoaded, a.likeWanted = err == nil, false
	}
	a.likeMu.Unlock()

	if wake := a.currentWake(); wake != nil {
		wake()
	}
}

// SetLike 点一下星标：把当前这一首赞成 want。**立刻返回**，结果走 done。
//
// ⚠️ **本地先改**：调用方那一帧回去重画星星就亮了。写失败会**改回去**，并把原因
// 交给 done。
//
// ⚠️ **这是一个写操作**：它真的改用户 B 站账号上的赞。真机验证只能用测试账号。
func (a *App) SetLike(want bool, done func(error)) {
	it, ok := a.CurrentItem()
	if !ok || !it.isOnline() || it.Bvid == "" || it.Type != "mv" {
		if done != nil {
			done(errors.New("这一首在 B 站上没法赞（本地文件和音乐库条目没有投稿）"))
		}
		return
	}
	bvid := it.Bvid
	key := likeKey(it)

	// 乐观：先把界面上那一栏改掉。
	//
	// ⚠️ **只有缓存已经属于这一首时才改**：没问过就改，等于把一个「还没问」写成
	// 「问过了、没赞」，而下一趟 relation 问回来会把它盖掉——中间那几帧界面上是
	// 一个我们编出来的答案。没问过的那一种就**不动缓存**：这一帧星标还是暗的，
	// 而写成功之后 `setAskedLike` 会把它点亮（写还没成功，亮起来是在撒谎）。
	a.likeMu.Lock()
	asked := a.likeKey == key
	prev := a.like
	if asked {
		a.like.Liked = want
	}
	a.likeMu.Unlock()
	if asked {
		a.wakeLike()
	}

	go func() {
		s := a.streamingSnapshot()
		if s == nil || s.Client == nil {
			if asked {
				a.rollbackLike(key, prev)
			}
			if done != nil {
				done(errLikeUnavailable)
			}
			return
		}
		if err := s.Client.LikeArchive(context.Background(), bvid, want); err != nil {
			if asked {
				a.rollbackLike(key, prev)
			}
			if done != nil {
				done(errors.New(bili.Friendly(err)))
			}
			return
		}
		// 写成功了：**答案我们知道是哪一栏**（这一次写成功 = 赞 / 取消赞成立），
		// 不必再等一趟 relation。
		a.setAskedLike(key, want)
		if done != nil {
			done(nil)
		}
	}()
}

// markAudioFavorited 是「收进绑定歌单那一趟成了」之后把关系缓存里的
// Favorited 标上：**只对当前这首 audio**。
//
// rid 是那一趟收歌的内容 id（audio 的 sid 本身）。它和当前项的 sid
// 对得上才改：多首快速喜欢时，歌 A 的成功不许把歌 B 的心形点亮。
//
// ⚠️ 它只在**问过之后**把那一栏置上（`likeLoaded` 已经为 true 才动）：
// 没问过就写，等于把「还没问」写成「问过了、收过」——而写成功本身
// 确实就是答案，所以更确切的做法是「只要缓存还是这首就把 loaded
// 立上、Favorited 置上」：这和 `setAskedLike` 是同一个理由（写成功 =
// 答案已知，不必再等一问）。
func (a *App) markAudioFavorited(rid int64) {
	it, ok := a.CurrentItem()
	if !ok || it.Type != "audio" {
		return
	}
	sid, err := strconv.ParseInt(it.Sid, 10, 64)
	if err != nil || sid != rid {
		return
	}
	key := likeKey(it)
	a.likeMu.Lock()
	if a.likeKey == key {
		a.like.Favorited = true
		a.likeLoaded = true
	}
	a.likeMu.Unlock()
	if wake := a.currentWake(); wake != nil {
		wake()
	}
}

// setAskedLike 是一次成功的写之后把答案记进缓存，并叫醒界面。
func (a *App) setAskedLike(key string, liked bool) {
	a.likeMu.Lock()
	if a.likeKey == key {
		a.like.Liked = liked
		a.likeLoaded = true
	}
	a.likeMu.Unlock()
	if wake := a.currentWake(); wake != nil {
		wake()
	}
}

// rollbackLike 是写失败之后把界面上那一栏改回去。
//
// ⚠️ 只在**缓存仍然属于同一首**时才改：失败的那几百毫秒里用户可能已经换歌了，
// 那时候把上一首的答案写回新一首的缓存上，界面上会出现「我在听 A，A 却显示成我
// 没赞过 A」——而换歌那一瞬最容易出这个错。
func (a *App) rollbackLike(key string, prev bili.ArchiveRelation) {
	a.likeMu.Lock()
	if a.likeKey == key {
		a.like = prev
	}
	a.likeMu.Unlock()
	if wake := a.currentWake(); wake != nil {
		wake()
	}
}

// TripleLike 长按 2.5 秒：一键三连。**立刻返回**，结果走 done。
//
// done 收到的是**B 站说的最终状态**（赞 / 投币 / 收藏），不是我们请求的那三个
// true——B 站可能拒绝了投币（硬币不够、这一首投过了），那时候界面上必须写「没投成」
// 而不是「投了」（理由见 `bili.TripleResult`）。
//
// 赞和藏两边都按 B 站的答案更新：长按那一把**顺手收藏了这一首**，所以播放条上
// 收藏那一格也要跟着变（照抄 `music-thumb` 的 `setIsThumb` + `setIsFav`）。
//
// ⚠️ **这是一个写操作**：它真的会花掉两枚硬币。真机验证只能用测试账号，而且
// 不要对着同一首连按（第二次 B 站会拒绝投币）。
func (a *App) TripleLike(done func(bili.TripleResult, error)) {
	it, ok := a.CurrentItem()
	if !ok || !it.isOnline() || it.Bvid == "" || it.Type != "mv" {
		if done != nil {
			done(bili.TripleResult{}, errors.New("这一首在 B 站上没法三连（本地文件和音乐库条目没有投稿）"))
		}
		return
	}
	bvid := it.Bvid

	go func() {
		s := a.streamingSnapshot()
		if s == nil || s.Client == nil {
			if done != nil {
				done(bili.TripleResult{}, errLikeUnavailable)
			}
			return
		}
		res, err := s.Client.LikeTriple(context.Background(), bvid)
		if err != nil {
			if done != nil {
				done(bili.TripleResult{}, errors.New(bili.Friendly(err)))
			}
			return
		}
		// 三样全按 B 站的答案记（收藏那一栏也记，见上面那段）。
		a.finishLike(likeKey(it), bili.ArchiveRelation{Liked: res.Liked, Favorited: res.Favorited}, nil)
		if done != nil {
			done(res, nil)
		}
	}()
}

// wakeLike 叫醒界面（铁律三的后半句：只投递不唤醒，界面永远停在旧帧）。
//
// 句柄是**拷出来**再调的（`currentWake` 在锁里拷一份）：唤醒会去要界面那把锁，
// 拿着 `a.mu` 调它就是自己等自己。
func (a *App) wakeLike() {
	if w := a.currentWake(); w != nil {
		w()
	}
}
