// 喜欢歌单绑定：第一次点「喜欢」时选的那一个收藏夹，之后的喜欢默认放进它。
//
// # 「喜欢」和「收藏」在这里是同一个动作
//
// 列表行上那颗心是「喜欢」，播放条上那颗星是「收藏」——两边最终都是把这一
// 首放进一个收藏夹（`resource/deal`）。区别只在**放进哪一个**：
//
//   - 收藏 → 弹「放进哪些收藏夹」的多选框（`folderselect.go`）；
//   - 喜欢 → 有绑定时直接进绑定的那一格；没绑过先问一次（三选一，见
//     `ui/likedbind.go`），问出来的那一格就记下来。
//
// # 绑定记在哪儿
//
// **设置里**（`Settings.LikedFolder`）。它和「侧栏收藏夹的顺序」是同一种
// 东西——用户本地的一个选择，B 站那边没有对应的字段，所以只能存本地；
// 而它必须**落盘**，不然每次启动都被问一遍。读写走 `UpdateSettings`
// （后台合并写 + 原子落盘），不走 App 上的内存字段。
//
// ⚠️ **存的是收藏夹 id + 一份名字副本**。名字是为了设置页能直接显示
// 「我的喜欢 · 私密」而不用先发一次请求；它只是**显示用的副本**，判断
// 「绑定还在不在」靠 id（见 `FavoriteIntoBound`）。
//
// # 「绑定失效」和「这次没写进去」是两件事
//
// 绑定的那一格可能已经没了：用户在 B 站网页上删了它、或者换了一个账号。
// 这一档必须和普通失败（断网、风控）分开——前者要解绑并回到「选哪个
// 收藏夹」，后者重试就好、绑定要留着。判据两层（见 `FavoriteIntoBound`）。
package app

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// LikedFolder 是「喜欢默认放进哪个收藏夹」那份绑定。
//
// ID == 0 表示「没绑」——它是 B 站收藏夹 id，不可能是 0，所以不用指针
// 也能把「没设过」和「设了」分开（和 `FavTarget.FolderID` 同一个约定）。
type LikedFolder struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Private 是那份名字副本的一部分：设置页显示「我的喜欢 · 私密」。
	Private bool `json:"private"`
}

// likedResult 是后台那一趟「收进绑定歌单」跑完回来的结果。
//
// ⚠️ **取走而不是读**（同 `folderResult`）：读而不取的话同一条会在每一帧
// 都发一次提示。
type likedResult struct {
	mu     sync.Mutex
	nextID uint64
	unread []LikedResult
}

func (r *likedResult) add(res LikedResult) {
	r.mu.Lock()
	r.nextID++
	res.ID = r.nextID
	r.unread = append(r.unread, res)
	r.mu.Unlock()
}

func (r *likedResult) take() (LikedResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.unread) == 0 {
		return LikedResult{}, false
	}
	res := r.unread[0]
	r.unread = append(r.unread[:0:0], r.unread[1:]...)
	return res, true
}

// LikedResult 是「收进绑定歌单」那一趟的回执。
type LikedResult struct {
	ID uint64
	// OK 是成了没有。
	OK bool
	// Text 是要跟用户说的那句话（成了是「已加入『我的喜欢』」）。
	Text string
	// Missing 是「绑定的那一格已经没了」——界面拿到它要解绑并回到
	// 收藏夹选择。和普通失败（断网）是两件事，见文件头。
	Missing bool
}

// TakeLikedResult 取走后台攒下的第一条喜欢结果。
//
// 界面在每一帧开头调一次（`flushLikedResults`）。**取走而不是读**，
// 理由同 `TakeFolderResult`。
func (a *App) TakeLikedResult() (LikedResult, bool) {
	c := a.collectionsSnapshot()
	if c == nil {
		return LikedResult{}, false
	}
	return c.likedResult.take()
}

// LikedFolder 读此刻的绑定。第二个返回值是「绑了没有」。
func (a *App) LikedFolder() (LikedFolder, bool) {
	f := a.SettingsSnapshot().LikedFolder
	return f, f.ID > 0
}

// BindLikedFolder 记下「喜欢默认放进这一格」。**同步改设置**，落盘走
// `UpdateSettings` 的后台合并写。
func (a *App) BindLikedFolder(f LikedFolder) {
	if f.ID <= 0 {
		return
	}
	if _, err := a.UpdateSettings(func(s *Settings) { s.LikedFolder = f }); err != nil {
		log.Printf("pudmusic: 记喜欢歌单绑定失败：%v", err)
	}
}

// UnbindLikedFolder 解绑：回到「喜欢时选收藏夹」。
func (a *App) UnbindLikedFolder() {
	if _, err := a.UpdateSettings(func(s *Settings) { s.LikedFolder = LikedFolder{} }); err != nil {
		log.Printf("pudmusic: 解喜欢歌单绑定失败：%v", err)
	}
}

// FavoriteIntoBound 把这一条收进**绑定的**那个收藏夹。**立刻返回**，结果走
// `TakeLikedResult`。
//
// rid / contentType 是这一条在 B 站那边的身份（和选择弹窗用的同一对，
// 见 `favTarget`）——调用方负责先把稿件 id 问出来，这一层不做 bvid → avid
// 的换算。
//
// # 绑定失效的两层判据
//
//   - **侧栏那份列表已经拉过**、里面没有绑定的 id → 当场判失效，一个写
//     请求都不发（发了也是白错）。
//   - **列表还没拉过** → 照写。「还没拉」和「真没有」是两件事
//     （AGENTS.md §4），不能拿一份没拉过的列表当判据；B 站说「这一格
//     不存在」（-400，参数错——deal 对不存在的收藏夹按参数处理）才算失效。
//
// ⚠️ **普通失败不清绑定**：断网时回 `OK=false, Missing=false`，绑定留着，
// 用户重试就好——一次断网就把用户配好的歌单弄丢是最坏的那一种。
func (a *App) FavoriteIntoBound(rid int64, contentType int) {
	c := a.collectionsSnapshot()
	if c == nil {
		return
	}
	bound, ok := a.LikedFolder()
	if !ok {
		c.likedResult.add(LikedResult{Missing: true})
		a.wakeUI()
		return
	}

	// 第一层判据：列表拉过了、里面没有它。`FavoriteState.Loaded` 正是
	// 「拉过、且拉到了」——失败的那一份 Loaded 也是 true（`Commit` 不清
	// 它，见它的注释），而那时列表还是上一次拉到的，仍然能当判据。
	if st := a.FavoriteState(FavoritesCreated); st.Loaded {
		alive := false
		for _, f := range a.SideFavorites(FavoritesCreated) {
			if f.ID == bound.ID {
				alive = true
				break
			}
		}
		if !alive {
			c.likedResult.add(LikedResult{
				Text:    "绑定的歌单已经不在了，重新选一个",
				Missing: true,
			})
			a.wakeUI()
			return
		}
	}

	go func() {
		err := c.refs.Client.DealFolderMedias(context.Background(), rid, contentType, []int64{bound.ID}, nil)
		switch {
		case err == nil:
			c.likedResult.add(LikedResult{OK: true, Text: "已加入「" + bound.Title + "」"})
		case likedFolderGone(err):
			log.Printf("pudmusic: 绑定的收藏夹 %d 已经没了：%v", bound.ID, err)
			c.likedResult.add(LikedResult{
				Text:    "绑定的歌单已经不在了，重新选一个",
				Missing: true,
			})
		default:
			log.Printf("pudmusic: 收进绑定歌单 %d 失败：%v", bound.ID, err)
			c.likedResult.add(LikedResult{Text: "收藏不了：" + bili.Friendly(err)})
		}
		a.wakeUI()
	}()
}

// likedFolderGone 判这个错是不是「绑定的那一格已经没了」。
//
// B 站对不存在的收藏夹 deal 回的是 -400（参数错误）——它把「收藏夹 id 指
// 着一个没有的东西」当成参数问题。
//
// ⚠️ **-400 是 deal 的通用参数错误**，不只「收藏夹不存在」一种：两个列表
// 都空、rid 非法、内容已达上限等也回它（见 `DealFolderMedias` 的注释）。
// rid 在调用方已经验过（>0），而「收藏夹上限」那一档在界面上同样值得回到
// 多选框重选——所以这里把 -400 整个当「绑定的这一格装不下它」处理，不细
// 分消息；真正难办的「网络坏了」走的是别的码（见下面那道 `errors.As`）。
// **只在写失败之后问它**：拿它去拦别的错（比如 -500）会把网络问题错判成
// 绑定失效，那一次断网就把用户配好的歌单解绑了。
func likedFolderGone(err error) bool {
	var ae *bili.APIError
	if !errors.As(err, &ae) {
		return false
	}
	if ae.Code == -400 {
		return true
	}
	return false
}
