// 收藏：把队列里的一首收进用户的 B 站收藏夹。
//
// # 判断全在服务层，这一层只做接线
//
// 「放哪一格」「是不是已经收藏过了」「收藏接口要的是 avid 不是 bvid」——三样都在
// `internal/service/bili/favorite.go` 里（那里有一整段为什么）。这一层做的是它做不了的
// 三件：**把 `Item` 翻译成 `FavTarget`**、**给它一个 client**、**异步跑**。
//
// 界面那层于是只剩「点了 → 调它 → 把结果说给用户」，没有一句 B 站的知识。
//
// # 为什么不在界面线程上做
//
// 这一条要问 B 站四到五次（nav 拿 mid → view 把 bvid 换成 aid → list-all 挑格子 →
// ids 判重复 → deal 写），几百毫秒到几秒。在界面线程上等就是窗口白着；而菜单点下去
// 之后**屏幕上什么也不会变**，用户会以为没按（再按一次，于是两次收藏）。
//
// 所以它**立刻返回**，结果走回调。
//
// # 回调跑在后台 goroutine 上
//
// 界面层拿 `done` 弹 Toast 是安全的（Toast 自己有锁），但**别指望那时候还能读 App 的
// 状态**：后台 goroutine 和界面线程是并发的。这一层在回调返回之后叫一帧（见下面那句
// wake），那才是「结果到了，该重画了」的时机。
package app

import (
	"context"
	"errors"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// errFavoriteUnavailable 是「收藏没接上」——没挂在线播放（也就没挂上那个 B 站
// client），或者 client 在但这一项认不出来。
var errFavoriteUnavailable = errors.New("收藏要连着 B 站账号用")

// FavoriteSong 把 it 收进用户的收藏夹。**立刻返回**，结果走 done。
//
// done 收到的是收进了哪一格；出错时 folder 是零值，err 已经过 `bili.Friendly`
// 说成了用户看得懂的一句话。
//
// ⚠️ **这是一个写操作**：它真的改用户 B 站账号里的收藏夹。真机验证只能在**测试账号
// 自己的夹子**上做，别动用户收藏的东西。
func (a *App) FavoriteSong(it Item, done func(folder bili.FavFolder, err error)) {
	go func() {
		folder, err := a.favoriteSongNow(it)
		if done != nil {
			done(folder, err)
		}
		// 🔴 结果交出去了，**必须叫一帧**（铁律三的后半句：只投递不唤醒，界面永远
		// 停在旧帧）。提示是加锁的、弹得出来，但没人叫它重画的话，屏幕上要等到用户
		// 碰一下鼠标才变——那时候用户早就以为收藏失败了。
		wake := a.currentWake()
		if wake != nil {
			wake()
		}
	}()
}

// favoriteSongNow 是 FavoriteSong 的同步那一半。测试直接调它（省掉 goroutine）。
func (a *App) favoriteSongNow(it Item) (bili.FavFolder, error) {
	if !it.isOnline() || it.Bvid == "" {
		// 本地文件：B 站上没有对应的东西，所以**不是**「先传一份上去再收藏」。
		return bili.FavFolder{}, errors.New("这一首是本地文件，B 站上没有对应的东西")
	}
	s := a.streamingSnapshot()
	if s == nil {
		return bili.FavFolder{}, errFavoriteUnavailable
	}
	folder, err := s.Client.FavoriteSong(context.Background(), bili.FavTarget{
		Bvid: it.Bvid,
		Cid:  it.Cid,
		Type: it.Type,
	})
	if err != nil {
		return bili.FavFolder{}, errors.New(bili.Friendly(err))
	}
	return folder, nil
}

// currentWake 读出唤醒句柄（锁里拷一份）。
func (a *App) currentWake() func() {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wake
}
