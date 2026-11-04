// 听歌统计和应用层之间的接线：挂上、每一拍记一笔、退出前落盘。
//
// # 为什么单独一个文件
//
// `app.go` 已经两千行了，而这一摊是**一整块**：挂载、每拍记一笔、退出收尾。
// 放一起的话这三件事会散在 `app.go` 的三处（挂载在 Mount* 那一批、每拍在
// tick 里、收尾在 Close 里），而它们共享同一份状态。

package app

import (
	"log"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

// MountListeningStats 挂上听歌统计。
//
// ⚠️ **要在开窗口之前调**：读盘是阻塞 IO，而这一份要在**第一帧就有数**
// ——用户点开统计页不能看到「加载中」。
//
// ⚠️ **读盘失败不是致命的**：拿不到应用数据目录、文件坏了、文件太大——
// 这三种都只是「这一份统计从空开始」，而应用照常起来。
func (a *App) MountListeningStats(path string) error {
	store := newStatsStore(path)
	tr := newListeningTracker()
	if err := store.loadInto(tr); err != nil {
		// 只有「来自更新的版本」值得说一声——那意味着这一版的统计是只读的。
		log.Printf("pudmusic: 这份听歌统计来自更新的版本，这次只读：%v", err)
	}
	a.mu.Lock()
	a.stats = store
	a.mu.Unlock()
	return nil
}

// statsTick 是 tick 上那一拍：把「现在在放什么、放到哪儿了」记进统计。
//
// ⚠️ **它每一拍都调**（哪怕此刻没有歌）：没歌的时候它什么都不记。
//
// ⚠️ **它必须在 `a.mu` 外面调**：它要拿的 `a.mu` 里的东西不多，但
// `reportTick` 那段已经写清楚了锁的次序规矩（`App.mu` → 统计器自己的锁
// 是允许的方向，反过来不行）。
//
// ⚠️ **`fresh` 说「这一拍真的读到位置了」**：暂停的时候播放器没被问过
// 位置（读出来是零值），而那和「真的回到开头了」是两件事。
func (a *App) statsTick(p *audio.Player, pos, dur time.Duration, fresh, finished bool, now time.Time) {
	a.mu.Lock()
	store := a.stats
	a.mu.Unlock()
	if store == nil {
		return
	}
	tr := store.Tracker()

	in := statsTickIn{At: now, Finished: finished, Dur: dur}
	// ⚠️ **位置这一栏只在真的读到的时候才算数**。
	if p != nil && fresh {
		in.FreshPos = true
		in.Pos = pos
		in.Player = p
	}
	a.fillStatsIdentity(&in)
	tr.Tick(in)

	// 到点了就落一次盘（30 秒一次）。
	store.saveIfDue(tr, now)
}

// fillStatsIdentity 填上「正在放的是哪一首」。
//
// ⚠️ **在线歌和本地文件是两条路**：在线的看 `a.online.item`，本地的看
// `a.path`（见 app.go 的 `openLocal`——本地文件不进队列，`a.online` 是 nil）。
func (a *App) fillStatsIdentity(in *statsTickIn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.online != nil {
		it := a.online.item
		in.Kind = it.Type
		in.Bvid = it.Bvid
		in.Cid = it.Cid
		in.Sid = it.Sid
		in.Title = it.Title
		in.Artist = it.Artist
		in.ArtistMid = it.ArtistMid
		return
	}
	if a.path != "" {
		in.Kind = "local"
		in.Path = a.path
		in.Title = a.title
		// ⚠️ **本地文件没有 UP 主**（见 app.go 的 `a.artist = ""`），
		// 所以不进艺人榜。
		return
	}
}

// ListeningStats 是统计的一份只读快照。
//
// ⚠️ **它是一次值拷贝**：界面层在**进页面时**拿一次，那一页自己留着。
// 每帧拿一份会把「进统计页」这一下的开销变成 O(n)。
func (a *App) ListeningStats() (ListeningStats, bool) {
	a.mu.Lock()
	store := a.stats
	a.mu.Unlock()
	if store == nil {
		return ListeningStats{}, false
	}
	return store.Tracker().Snapshot(), true
}

// FlushListeningStats 立刻把统计落一次盘。
//
// ⚠️ **退出前调它**：落盘是定时触发的（30 秒一次），而用户听了一会儿直接
// 退出，那一段就丢了——丢了的症状是「重启之后统计少了几分钟」。
func (a *App) FlushListeningStats() {
	a.mu.Lock()
	store := a.stats
	a.mu.Unlock()
	if store == nil {
		return
	}
	if err := store.save(store.Tracker()); err != nil {
		log.Printf("pudmusic: 退出时保存听歌统计失败：%v", err)
	}
	// 🔴 **关掉它**：之后不再落盘。
	//
	// ⚠️ **排在落盘之后**：而 `App.Close` 是在 `close(a.done)` 之后调它的
	// （后台时钟已经停了），所以此刻落下去的就是**最后那个状态**。
	store.close()
}

// SeedListeningStatsForTest 直接塞一份统计进去。
//
// ⚠️ **它只给测试用**：界面层那一族要验的是「画出来是什么样」，而「怎么
// 攒出来的」是应用层那一族的事——为了画一个页面而去造一个真播放器不划算。
//
// ⚠️ **它会覆盖那一份统计**：塞完就回不去，所以只在一个测试里用一次。
func (a *App) SeedListeningStatsForTest(s ListeningStats) {
	a.mu.Lock()
	store := a.stats
	a.mu.Unlock()
	if store == nil {
		return
	}
	store.Tracker().replaceWithSnapshot(s)
}
