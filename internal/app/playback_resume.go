// 「断点续播」：退出时记住正在放哪一首、放到哪儿了，下次打开**接着放**
// （往回退几秒接上下文），而不是停在原位等用户按播放。
//
// ## 它和 `playback` 那一格是两件事
//
// `playback`（`playback_state.go`）恢复的是**队列的样子**：顺序、当前项、
// 位置——停在那儿、**不出声**（上游 Biu 和 Dopamine 都是这样，启动时突然
// 出声是最糟的一种）。
//
// 这一格（`playbackResume`）恢复的是**正在听这件事**：上次退出时在放
// 哪一首、放到第几秒。下次启动**接着放**（出声），并且往回退几秒——
// 长音频（合集、广播剧、直播录像）中途关应用不再丢进度。
//
// 两个都在：用户想「接着听」时它出声；想「先看看队列」时那一格照旧停着。
// 这一格跑在**那一格之后**：队列已经按 `playback` 恢复出来了，
// 这里只是在里面找到那一首、把它放起来。
//
// ## 身份用 `lyricKeyOf` 那同一套
//
// 记的是 `lyricKeyOf` 拼出来的键（`bili-<bvid>-<cid>` / `bili-audio-<sid>` /
// `local-<路径>`），不是内存对象：重启之后队列条目是新拉的，只能按 id 对。
// 它和 `playback.items` 的 `id`（`mv:` / `audio:` / `local:`）是同一份
// 口径的两半（见 `resumeKeyOf`）——判「队列里有没有那一首」时先严格比
// 字段（bvid+cid / sid / 路径），再退化到主 id（旧文件没有 cid 那一趟）。
package app

import (
	"time"
)

// PlaybackResume 是「断点续播」落盘的那一份。
//
// ⚠️ **它整个住在设置文件的一个键里**（`playbackResume`），和 `playback`
// 是邻居——同一个文件、同一次原子写，所以「队列是新的、进度是旧的」这种
// 拼不出来的组合不会发生。
type PlaybackResume struct {
	// TrackKey 是上次在放的那一条的键（`lyricKeyOf` 那一套，见 `resumeKeyOf`）。
	// 空串 = 没有可续播的那一首。
	TrackKey string `json:"trackKey"`
	// PositionMs 是那一首已经播到哪儿（毫秒）。
	PositionMs int64 `json:"positionMs"`
	// QueueID 是（可选的）队列身份：将来想验「这份队列还是不是当时那份」时
	// 用它。现在只落不读。
	QueueID string `json:"queueId,omitempty"`
}

// resumeRewind 是「接着放」时往回退的那几秒：让用户接住上下文。
// 下限是 0（开头），见 `resumeAtFor`。
const resumeRewind = 3 * time.Second

// resumeKeyOf 是这一项的「断点续播」键：**和 `lyricKeyOf` 同一套身份**
// （bvid / sid / 本地路径），但拼法照 `playback.items` 的 `id` 那一套
// （`mv:` / `audio:` / `local:`），好让两份键在同一个文件里互相认得出来。
//
// 认不出身份的项返回空串（见 `trackKey` 的注释）。
func resumeKeyOf(it Item) string { return it.Identity() }

// resumeAtFor 把落盘的毫秒位置换算成「该从哪儿开始放」：退 resumeRewind 秒，
// 下限 0。
func resumeAtFor(positionMs int64) time.Duration {
	at := time.Duration(positionMs)*time.Millisecond - resumeRewind
	if at < 0 {
		at = 0
	}
	return at
}

// rememberPlaybackResume 把「此刻在放哪一首、放到哪儿了」写进 `playbackResume`。
//
// 调它的两处：
//
//   - **切歌那一刻**（`openLocal` / `installOnlinePlayer` 换上新播放器之前）：
//     记的是**上一首**（key + 位置），位置清零之前。
//   - **`App.Close` 退出前**：记的是最后一首。
//
// **不每拍写**：50ms 一次的写盘是纯浪费，还会把设置存储的防抖窗口一直顶掉。
// 歌在自己往前走的那一段不记——丢了最坏是「接着放」回到切歌那个点，
// 而切歌那一下本来就记了。
func (a *App) rememberPlaybackResume() {
	a.mu.Lock()
	key, pos := a.playbackResumeLocked()
	a.mu.Unlock()
	a.persistPlaybackResume(key, pos)
}

// rememberPlaybackResumeLocked 同上，但**调用方已经拿着 `a.mu`**。
//
// 装在 `installOnlinePlayer`（换上新播放器那一刻，锁已经在手里）：
// 那里不能解锁再拿（中间那几行状态正在换），所以拼快照这一半在锁里做、
// 落盘这一半排到锁外做（见 `installOnlinePlayer` 里 `resumeWrite` 那一段）。
//
// ⚠️ **串行落盘**：快速连续切两首时，两个 goroutine 的 `UpdateSettings`
// 写顺序不保证和切歌顺序一致——后发的写可能先落、先发的写可能后落，
// 最终记的是「第一首」而非「第二首」。所以落盘走一条 channel，谁来都
// 只往一个 goroutine 里排，保证写的顺序和切的顺序一致。
func (a *App) rememberPlaybackResumeLocked() {
	key, pos := a.playbackResumeLocked()
	a.enqueuePlaybackResume(key, pos)
}

// enqueuePlaybackResume 把「要落盘的那一格」排进串行队列。
//
// ⚠️ **goroutine 是懒起的**：第一次用到才 `go`——`installOnlinePlayer` 在
// 每次切歌都走，而它不一定有人真的退登过，懒起可以省一个常驻 goroutine。
// 通道容量 1：满了就把旧的丢掉——「上一首的位置」晚于「这一首的」就没用了，
// 丢旧留新。
func (a *App) enqueuePlaybackResume(key string, pos time.Duration) {
	a.resumeOnce.Do(func() {
		a.resumeCh = make(chan PlaybackResume, 1)
		go func() {
			for r := range a.resumeCh {
				a.persistPlaybackResume(r.TrackKey, time.Duration(r.PositionMs)*time.Millisecond)
			}
		}()
	})
	select {
	case a.resumeCh <- PlaybackResume{TrackKey: key, PositionMs: pos.Milliseconds()}:
	default:
		// 满了：丢掉旧的那一格，塞新的——「上一首」晚于「这一首」就没用了。
		select {
		case <-a.resumeCh:
		default:
		}
		a.resumeCh <- PlaybackResume{TrackKey: key, PositionMs: pos.Milliseconds()}
	}
}

// playbackResumeLocked 读「此刻在放哪一首、放到哪儿了」。**必须在持锁时调**。
func (a *App) playbackResumeLocked() (key string, pos time.Duration) {
	pos = a.position
	if a.online != nil {
		key = resumeKeyOf(a.online.item)
	} else if a.path != "" {
		key = resumeKeyOf(LocalItem(a.path))
	}
	return key, pos
}

// persistPlaybackResume 把拼好的那一格写进设置。
//
// ⚠️ **写的是整个当前值，不是增量**（和 `playback` 同一条规矩）：谁最后写
// 谁赢，所以「切歌那一刻」和「退出前」两处各写各的、互不覆盖错。
func (a *App) persistPlaybackResume(key string, pos time.Duration) {
	slot := a.settingsSlot()
	if slot == nil || slot.store == nil {
		return
	}
	if key == "" {
		return
	}
	resume := PlaybackResume{TrackKey: key, PositionMs: pos.Milliseconds()}
	if _, err := a.UpdateSettings(func(s *Settings) { s.PlaybackResume = resume }); err != nil {
		// 存不住不算错（理由同 `persistPlayback`）：设置层自己有入口能看到
		// 「这次改的设置存不住」，在这儿拦只会让「记住上次听到哪儿」本身失败。
		a.logf("biu: 「断点续播」存不住：%v", err)
	}
}

// ResumePlayback 恢复「断点续播」：把上次在放的那一首**接着放**。
//
// **排在 `RestorePlayback` 之后调**（队列已经按 `playback` 恢复出来了）。
// 这一格里没有（`trackKey` 空）、或者队列里找不到那一条 → 什么都不做，
// 队列照 `playback` 那一格停着。
//
// 恢复出来是**出声**的，位置是 `positionMs - 3s`（退几秒接上下文，下限 0）。
// 取流失败照 `unplayable.go` 那一套说原因（在线的那一路有它自己的失败分类），
// 不静默卡在 0:00。
//
// 返回的 error 只给日志看，**不给用户看**：恢复失败不该在界面上冒一句。
func (a *App) ResumePlayback() error {
	resume := a.SettingsSnapshot().PlaybackResume
	if resume.TrackKey == "" {
		return nil
	}
	items := a.queue.Items()
	if len(items) == 0 {
		// 队列还没恢复出来（`RestorePlayback` 还没跑、或者那份队列恢复不了）：
		// 没什么可续的，安静走人。
		return nil
	}
	at := resumeAtFor(resume.PositionMs)
	idx := -1
	for i, it := range items {
		if it.Identity() == resume.TrackKey {
			idx = i
			break
		}
	}
	if idx < 0 {
		// 退化那一趟（和 `RestorePlayback` 同一个理由）：旧文件里 `mv:` 那段
		// 可能没有 `|<cid>`——严格相等找不到时按「种类 + 主 id」再认一次。
		//
		// ⚠️ **只给 `mv:` 退化**：`local:` 的主 id 是完整路径，两个不同路径
		// 永远不相等，所以**严格那一趟找不到就是真的找不到**——不该再退；
		// 退了反而会把另一条本地文件错认成它（`local:<a>` 和 `local:<b>` 的
		// 种类相同、主 id 不同，退化比还是会落到**第一个 local 项**上——
		// `splitIdentity` 给 `local:` 拆出来的 key 是路径本身，而路径不等
		// 时 kind 还一样，`v == id` 不成立就不会命中，但那要求每一条都比完。
		// 真正会把另一条错认的是 `audio:`：sid 唯一、不用退；`local:` 路径
		// 不等时也不该落到别的那一条上）。所以这里只放 `mv:` 那一档进来。
		if kind, id := splitIdentity(resume.TrackKey); id != "" {
			if kind == playbackIDMV {
				for i, it := range items {
					if k, v := splitIdentity(it.Identity()); k == kind && v == id {
						idx = i
						break
					}
				}
			}
		}
	}
	if idx < 0 {
		// 那一条已经不在队列里了（用户删了、文件没了、B 站那边对不上了）：
		// 安静跳过——这不是错，是这一格本来就有的边界。
		return nil
	}
	it, _ := a.queue.itemAt(idx)
	a.queue.selectAt(idx)
	a.logf("biu: 断点续播：接着放 %q（位置 %v）", it.Title, at)

	if it.IsLocal() {
		// 本地文件：离线就能开。放不出来也安静（文件在、但这台机器解不开，
		// 和 `openLocal` 的 openRestore 同一条理由——启动时冒红字是应用在说自己）。
		if err := a.openLocal(it.Path, at, openResume); err != nil {
			return nil
		}
		return nil
	}
	// 在线的那一首：要取流才能开。`playOnlineFrom` 的 at/paused 语义正好
	// 装得下「从哪儿开始」；**出声**这一半由 openResume / paused=false 那一档给。
	if err := a.playOnlineFrom(it, at, false); err != nil {
		a.logf("biu: 断点续播取不了流：%q：%v", it.Title, err)
	}
	return nil
}
