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
	// nextToken 是「第几趟写请求」的流水号：`FavoriteIntoBound` 发出去
	// 一趟就先取一个号，回来的结果带着它——界面拿它认「这条结果
	// 是替哪一首收歌那一趟回的」，不是「最新那条 pending」。
	nextToken uint64
	unread    []LikedResult
	// dropped 是「到达时绑定已经换了/没了」丢掉的结果。取结果那一侧在
	// `TakeLikedResult` 里做校验（见它的注释）；这份只给同包测试观察
	// 「旧结果确实被丢了」，不往界面去。
	dropped []LikedResult
}

func (r *likedResult) token() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextToken++
	return r.nextToken
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
	// Token 是「这是第几趟写请求的回执」：`FavoriteIntoBound` 发出去
	// 之前先领一个号，同一趟回来的结果带同一个号。界面拿它把回执
	// 对回发出那一趟的 pending——多首快速喜欢、旧结果乱序回来时，
	// 不能用「最新那条 pending」去处理一条早发出的回执。
	Token uint64
	// Rid / Type 是发出那一趟时替哪一首收歌（rid + 内容类型，同
	// `favTarget` 那一对）。`flushLikedResults` 拿它对回 pending：
	// 对不上的结果属于另一首（或已经清掉的 pending），不许拿它去
	// 解绑 / 开多选框。
	Rid  int64
	Type int
	// Folder 是发出那一趟写请求时绑定的那份歌单 id。换绑之后才回来的
	// 结果是旧绑定的回执——`TakeLikedResult` 拿它对照**此刻**的绑定，
	// 对不上就丢，不让一条旧结果去拆新绑定（见 `TakeLikedResult` 注释）。
	Folder int64
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
//
// ⚠️ **过期的结果在这里丢**：一趟写请求发出去之后绑定可能已经被换掉
// （设置页换绑）或解掉（绑定失效回多选框）。这时候才回来的结果属于
// **旧绑定**——界面的 Missing 分支会解绑此刻的绑定、弹回多选框，把一
// 条旧回执当成新绑定的判决是最坏的那一种错；旧成功那句「已加入『旧
// 的』」同样是错话。所以每条结果都带上「发请求时的绑定 id」，取走时
// 和此刻的绑定对不上就丢（顺带记进 dropped，给同包测试观察）。
func (a *App) TakeLikedResult() (LikedResult, bool) {
	c := a.collectionsSnapshot()
	if c == nil {
		return LikedResult{}, false
	}
	current, _ := a.LikedFolder()
	for {
		res, ok := c.likedResult.take()
		if !ok {
			return LikedResult{}, false
		}
		if res.Folder == current.ID {
			return res, true
		}
		c.likedResult.mu.Lock()
		c.likedResult.dropped = append(c.likedResult.dropped, res)
		c.likedResult.mu.Unlock()
	}
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
// `TakeLikedResult`；返回值是这一趟的 token——调用方把它存进 pending，
// 回来的结果才能对上号（见 `LikedResult.Token`）。
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
func (a *App) FavoriteIntoBound(rid int64, contentType int) uint64 {
	c := a.collectionsSnapshot()
	if c == nil {
		return 0
	}
	bound, ok := a.LikedFolder()
	if !ok {
		c.likedResult.add(LikedResult{
			Token:   c.likedResult.token(),
			Rid:     rid,
			Type:    contentType,
			Missing: true,
		})
		a.wakeUI()
		return 0
	}

	token := c.likedResult.token()

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
				Token:   token,
				Rid:     rid,
				Type:    contentType,
				Folder:  bound.ID,
				Text:    "绑定的歌单已经不在了，重新选一个",
				Missing: true,
			})
			a.wakeUI()
			return token
		}
	}

	go func() {
		err := c.refs.Client.DealFolderMedias(context.Background(), rid, contentType, []int64{bound.ID}, nil)
		switch {
		case err == nil:
			c.likedResult.add(LikedResult{
				Token: token, Rid: rid, Type: contentType,
				Folder: bound.ID, OK: true, Text: "已加入「" + bound.Title + "」",
			})
		case likedFolderGone(err):
			log.Printf("pudmusic: 绑定的收藏夹 %d 已经没了：%v", bound.ID, err)
			c.likedResult.add(LikedResult{
				Token:   token,
				Rid:     rid,
				Type:    contentType,
				Folder:  bound.ID,
				Text:    "绑定的歌单已经不在了，重新选一个",
				Missing: true,
			})
		default:
			log.Printf("pudmusic: 收进绑定歌单 %d 失败：%v", bound.ID, err)
			c.likedResult.add(LikedResult{
				Token: token, Rid: rid, Type: contentType,
				Folder: bound.ID, Text: "收藏不了：" + bili.Friendly(err),
			})
		}
		a.wakeUI()
	}()
	return token
}

// likedFolderGone 判这个错是不是「绑定的那一格已经没了」。
//
// B 站对不存在的收藏夹 deal 回的是 -400（参数错误）——它把「收藏夹 id 指
// 着一个没有的东西」当成参数问题。
//
// ⚠️ **-400 是 deal 的通用参数错误**，不只「收藏夹不存在」一种：两个
// 列表都空、rid 非法、内容已达上限等也回它（见 `DealFolderMedias` 的
// 注释）。已知的几种分别怎么摊：
//
//   - **rid 非法**：在调用方已经挡掉（`favTarget` 只收 rid > 0 的；
//     `DealFolderMedias` 自己又验一遍）——走到这里 rid 一定合法，
//     所以 -400 落在「收藏夹那一侧出问题」上。
//   - **add / del 两个列表都空**：`FavoriteIntoBound` 永远只发
//     `add = [bound.ID]`——「两个都空」那一档在结构上走不到这里。
//   - **收藏夹容量满了**：B 站同样回 -400。这一档**有意当成
//     「绑定的这一格装不下它」**——界面同样是「解绑 + 回到多选框
//     重选」，和普通「格子没了」是同一个出路，分开两种提示没有
//     差别可以讲给用户听。
//
// 所以这里把 -400 整个当「绑定的这一格不能再要」处理，不细分消息；
// 真正难办的「网络坏了」走的是别的码（见下面那道 `errors.As`）。
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
