// 「上次听到哪儿」：队列 + 当前项 + 播放位置。
//
// ## 这一格是什么
//
// 应用重启之后回到上次听到的地方，**停在原位而且是暂停的**——启动时不该突然
// 出声（上游 Biu 和 Dopamine 都是这样）。
//
// 参照（BMG-163 调研报告 §5.2 第 33 条 / B17）：
//
//   - 上游 Biu：`play-list.ts:280` 的 persist（`list` / `playId` / `volume` /
//     `isMuted` / `playMode`）+ `play-progress.ts:13-14` 的
//     `localStorage["play-current-time"]`。
//   - Dopamine：`queue-persister.ts` —— 改队列后防抖落盘、退出时强制保存、
//     启动恢复以暂停状态停在原进度、**恢复前有守卫**。
//
// ## 落在哪儿：设置文件里的一个键
//
// 这一格落在 `settings.json` 的 `playback` 键里，**不另开一份文件**。理由和
// 「音量 / 播放模式为什么也在设置里」是同一条（见 settings.go 里 `Settings`
// 类型上面那一段）：设置存储已经把 schema 版本、原子写（tmp + rename）、
// 防抖合并写、退出前 flush、只读保护（文件来自更新的版本时一个字都不许写
// 回去）这几件都做好了，另开一份等于把这些重造一遍，还少了只读保护。
//
// ⚠️ **同一个文件还有第二层好处**：这一格和音量、播放模式在**同一次原子写**里
// 落盘，所以它们之间没有「谁先谁后」的竞态——而竞态的后果是「音量是新的、
// 队列是旧的」这种说不清的状态（见下面那张表）。
//
// ## 身份用稳定标识，不用下标
//
// 每一项存的是 `Item.Identity()`（`mv:<bvid>` / `audio:<sid>` / `local:<路径>`），
// **不是它在队列里的下标**。下标在「插一行」「删一行」之后指着另一首，而用户
// 记住的是「那首歌」。
//
// ⚠️ **当前项也是存标识**（上游 Biu 存的是 `playId`，同一个道理）。所以队列里
// 同一首歌排了两遍的时候，恢复出来的是**第一份**——这是跟着上游的一个已知
// 边界，不影响「回到上次那首歌」。
//
// ## 「启动恢复」和「用户显式操作」的优先级（这一票的规矩，写死在这儿）
//
// 恢复出来的每一个值都可能被用户后来的操作改掉。规矩只有一条：
//
//	**后写的盖先写的；「用户显式操作」永远晚于「启动恢复」。**
//
// 具体到每一格：
//
//	| 落的东西        | 落在哪个键            | 谁写它                                   | 恢复时谁盖谁                                                             |
//	|-----------------|-----------------------|------------------------------------------|--------------------------------------------------------------------------|
//	| 队列顺序        | `playback.items`      | 队列变了（加 / 删 / 换集）                | **恢复出来的顺序先落成队列**；用户之后任何一次队列操作都盖掉它             |
//	| 当前项          | `playback.current`    | 切歌（下一首 / 上一首 / 点一行 / 放完）  | **恢复出来的当前项先选中**；用户切歌就盖掉它                               |
//	| 播放位置        | `playback.positionMs` | 位置一变就写。**用户动了一下**（拖进度条 / 切歌 / 按了暂停或停止）→ **半秒之内**就写；**只是歌在往前走** → 最多每 **5 秒**一次 | **恢复出来的位置先落成「从这儿继续」**；用户拖动 / 切歌就盖掉它 |
//	| 音量            | `volume`（既有那一格）| 用户动音量 / 静音                        | **恢复不碰它**：它在另一个键里，写它的是用户自己                           |
//	| 静音            | `isMuted`（既有那一格）| 同上                                    | 同上                                                                       |
//	| 播放模式        | `playMode`（既有那一格）| 用户切档                                | 同上                                                                       |
//
// ⚠️ **「用户刚拖过队列、又改了音量、又切了歌，然后退出」这一串**：三件事各写
// 各的键，而**每一次落盘写的是整个当前值**（不是增量），所以谁最后写谁赢；
// 四件事又都落在**同一个文件、同一次原子写**里，所以恢复出来的是**三件事各自
// 最后那个值**——不是某一处的旧值，也不会出现「音量是新的、队列是旧的」。
//
// 这也是为什么这一格**不能**另开一份文件：两份文件各有各的防抖窗口和落盘
// 时刻，「队列改了」和「音量改了」谁先落盘是不定的，恢复出来就可能拼出一个
// 从来没有过的组合。
//
// ⚠️ **恢复只发生一次**（启动时）。之后用户的任何一次操作都是直接改内存状态、
// 重新落盘——恢复出来的值只是那一份状态的**初值**，不是「要被保住的东西」。
package app

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ---- 落盘的那一份 ----

// PlaybackSnapshot 是「上次听到哪儿」落盘的那一份。
//
// ⚠️ **它整个住在设置文件的一个键里**，不是一份独立文件（理由见文件头）。
type PlaybackSnapshot struct {
	// Items 是队列，**按播放顺序**。
	//
	// ⚠️ **它存的是稳定标识，不是下标**（见 PlaybackEntry.ID）。
	Items []PlaybackEntry `json:"items"`

	// Current 是**当前项**的稳定标识。空串 = 队列里没有当前项。
	Current string `json:"current"`

	// Position 是当前这一首已经播到哪儿（毫秒）。
	Position int64 `json:"positionMs"`
}

// PlaybackEntry 是队列里的一项，落盘用。
//
// ⚠️ **ID 是这一项的身份**（`mv:<bvid>` / `audio:<sid>` / `local:<路径>`），
// 其余的字段是**显示用的快照**，不是身份——它们只为了让恢复出来的队列在抽屉里
// 画得出来（歌名、封面、UP 名）。
//
// 为什么连显示用的字段一起存：恢复**不许**在启动时挨个问 B 站要元信息（那是
// 几十次网络往返，而用户只是想让应用开起来）。上游 Biu 也是把整个 `list` 存
// 下来的（`play-list.ts:280` 的 persist）。
type PlaybackEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title,omitempty"`
	Cover     string `json:"cover,omitempty"`
	Artist    string `json:"artist,omitempty"`
	ArtistMid int64  `json:"artistMid,omitempty"`
	Cid       string `json:"cid,omitempty"`
	PageIndex int    `json:"pageIndex,omitempty"`
	TotalPage int    `json:"totalPage,omitempty"`
	// CollectionID 是「这一条是哪一个合集」（收藏夹里 type=21 那一行）。
	//
	// ⚠️ **它也得存**（票 #683 的复审）：那一行没有 bvid，`CollectionID` 是它
	// **唯一的「去别处」身份**（菜单里「打开这个合集」靠它拼
	// `/collection/{id}?type=21`）。不存的话，任何一次经过这一格的搬运都会把它
	// 丢掉，而症状是那一项在菜单里**摆不出来**——界面上看不出少了什么。
	CollectionID int64 `json:"collectionId,omitempty"`
}

// playbackEntryOf 把一项变成落盘用的那一格。
//
// 认不出身份的项（`Item.Identity()` 是空串）照样落盘，ID 是空的——恢复的时候
// 它会被守卫跳过（见 itemOfPlaybackEntry），而队列里其余项的**相对顺序**不受
// 影响。
func playbackEntryOf(it Item) PlaybackEntry {
	return PlaybackEntry{
		ID:           it.Identity(),
		Title:        it.Title,
		Cover:        it.Cover,
		Artist:       it.Artist,
		ArtistMid:    it.ArtistMid,
		Cid:          it.Cid,
		PageIndex:    it.PageIndex,
		TotalPage:    it.TotalPage,
		CollectionID: it.CollectionID,
	}
}

// itemOfPlaybackEntry 把落盘的那一格变回一项。**认不出来时返回 false**。
//
// # 这一段就是「恢复的守卫」
//
// Dopamine 那边的 queue-persister 也有这么一层（恢复前有守卫）。三种跳过的
// 理由，每一种都是「这一条已经没法要了」：
//
//   - **标识不是这三种之一**（或者前缀后面是空的）：这份文件不是我们写的，
//     或者是被手改过的。跳过。
//   - **本地文件的路径已经没了**（用户删了它、换了机器、拔了外接盘）：跳过。
//     留着它的话，抽屉里会有一行点开就是「打不开」的歌。
//   - **标识认得出来但那一项没法放**（mv 没有 bvid 之类）：跳过。
//
// ⚠️ **跳过一条不许把整个恢复搞崩**：恢复的是一整个队列，一条坏掉不该让
// 用户连其余那几条都听不到。
func itemOfPlaybackEntry(e PlaybackEntry) (Item, bool) {
	switch {
	case strings.HasPrefix(e.ID, playbackIDLocal):
		path := strings.TrimPrefix(e.ID, playbackIDLocal)
		if path == "" {
			return Item{}, false
		}
		if _, err := os.Stat(path); err != nil {
			return Item{}, false
		}
		it := LocalItem(path)
		// 存的歌名优先（用户可能在 B 站那边看到的名字和文件名不一样）。
		if e.Title != "" {
			it.Title = e.Title
		}
		it.Cover = e.Cover
		return it, true

	case strings.HasPrefix(e.ID, playbackIDMV):
		bvid, cidFromID := splitMVIdentity(strings.TrimPrefix(e.ID, playbackIDMV))
		if bvid == "" {
			return Item{}, false
		}
		// ⚠️ **cid 以 `Cid` 那一栏为准**：标识里那一段是票 #683 之后才有的，
		// 旧文件里的标识是 `mv:<bvid>`、cid 只在 `e.Cid` 里。两处都在时它们是
		// 同一个数（`trackKey` 就是从同一个 `Cid` 拼的），所以取哪个都不会错
		// ——但只读一处更不容易漂。
		cid := cidFromID
		if e.Cid != "" {
			cid = e.Cid
		}
		return Item{
			Source:       SourceOnline,
			Type:         "mv",
			Bvid:         bvid,
			Cid:          cid,
			Title:        e.Title,
			Cover:        e.Cover,
			Artist:       e.Artist,
			ArtistMid:    e.ArtistMid,
			PageIndex:    e.PageIndex,
			TotalPage:    e.TotalPage,
			CollectionID: e.CollectionID,
		}, true

	case strings.HasPrefix(e.ID, playbackIDAudio):
		sid := strings.TrimPrefix(e.ID, playbackIDAudio)
		if sid == "" {
			return Item{}, false
		}
		return Item{
			Source:    SourceOnline,
			Type:      "audio",
			Sid:       sid,
			Title:     e.Title,
			Cover:     e.Cover,
			Artist:    e.Artist,
			ArtistMid: e.ArtistMid,
			PageIndex: e.PageIndex,
			TotalPage: e.TotalPage,
		}, true
	}
	return Item{}, false
}

// ---- 落盘 ----

// 两个落盘节奏。
//
// 🔴 **两个都必须有，而且不能只有一个**：落盘是往整个设置文件里写一遍，而设置
// 存储的防抖是「每次改动都把 120ms 的窗口往后推」——落得太勤就把那个窗口一直
// 顶掉，**暂停期间用户改的任何设置都不落盘**（直到退出那一次），而界面还被
// 叫醒 20 次/秒（一次叫醒 = 一帧）。
const (
	// playbackSaveEvery 是「位置自己在往前走」时多久落一次。
	//
	// 位置一直在变（在放的时候每一拍都在变），而落盘是往整个设置文件里写一遍。
	// 不限速的话就是每 120ms 写一次文件（设置存储的防抖窗口），那是白花的磁盘
	// IO，还把设置存储的防抖窗口一直顶掉。
	//
	// 5 秒是个中间值：最坏丢掉最后 5 秒的进度——而退出前那一次是强制落的
	// （见 `App.Close` 和界面层的 OnWillQuit）。
	playbackSaveEvery = 5 * time.Second

	// playbackWriteMinGap 是「用户动了一下」之后落盘的下限间隔。
	//
	// 「用户动了一下」= 改了队列 / 切了歌 / 拖了进度条 / 按了暂停或停止
	// （那几处都调 `markPlaybackDirty`）。那些**尽快**落（不等
	// playbackSaveEvery 那个窗口），但也不能连着写——拖动重排那一类操作是每一
	// 帧都改队列的，每一帧都落一次就是上面说的那个坑。
	//
	// 500ms 离设置存储的防抖窗口（120ms）够远：那 120ms 里文件确实会落一次，
	// 所以**别的设置不会被这一格饿死**。
	playbackWriteMinGap = 500 * time.Millisecond
)

// markPlaybackDirty 记下「播放状态变了，该落盘了」。
//
// 它**只改一个标志**，不落盘：落盘由后台时钟那一拍去做（见 savePlaybackIfDue），
// 所以界面线程上改队列不会等一次文件写。
func (a *App) markPlaybackDirty() {
	a.mu.Lock()
	a.playbackDirty = true
	a.mu.Unlock()
}

// savePlaybackIfDue 在「该落盘的时候」落一次盘。**后台时钟那一拍调**
// （见 app.go 的 tick）。
//
// 什么时候落：
//
//	用户动了一下（dirty：改了队列 / 切了歌 / 拖了进度条 / 按了暂停）→ 尽快，
//	  但至少隔 playbackWriteMinGap
//	只是位置在往前走（歌在放）→ 最多每 playbackSaveEvery 一次
//
// ⚠️ **两边的位置比较必须是同一个单位**：`a.position` 是纳秒，而落盘那一格是
// 毫秒（`pos.Milliseconds()`）。拿落盘那个毫秒值去比纳秒的位置，**永远不相
// 等**——于是「位置变了没有」这一条恒真，每一拍都落一次盘。
func (a *App) savePlaybackIfDue(now time.Time) {
	a.mu.Lock()
	pos := a.position
	savedPos := a.playbackSavedPos
	savedAt := a.playbackSavedAt
	dirty := a.playbackDirty
	a.mu.Unlock()

	// 位置变了没有。⚠️ **两边都是纳秒**（见上面那段）。
	moved := pos != savedPos
	if !dirty && !moved {
		return
	}
	gap := playbackSaveEvery
	if dirty {
		gap = playbackWriteMinGap
	}
	if now.Sub(savedAt) < gap {
		return
	}
	a.persistPlayback()
}

// persistPlayback 把现在的队列、当前项和位置写进设置。
//
// ⚠️ **写的是整个当前值，不是增量**：每一格都是「现在是什么样」，所以谁最后
// 写谁赢（见文件头那张表）。
func (a *App) persistPlayback() {
	// 没挂设置：没什么可写的（测试里很常见——那一层挂不挂与这一格无关）。
	//
	// ⚠️ **要在拼快照之前就问**：拼一次快照要拷一遍整个队列，而这一格在没挂
	// 设置时永远是写不下去的。
	slot := a.settingsSlot()
	if slot == nil || slot.store == nil {
		return
	}

	snap, pos := a.playbackSnapshot()
	a.mu.Lock()
	a.playbackDirty = false
	a.playbackSavedAt = time.Now()
	// ⚠️ **存的是那个精确的纳秒位置**，不是落盘那个毫秒值：这一格是拿来跟
	// `a.position` 比的（见 savePlaybackIfDue），单位不一样的话永远不相等。
	a.playbackSavedPos = pos
	a.mu.Unlock()

	if _, err := a.UpdateSettings(func(s *Settings) { s.Playback = snap }); err != nil {
		// 存不住不算错（理由同 `rememberPlayMode`）：设置层自己有入口能看到
		// 「这次改的设置存不住」（见 App.TakeSettingsError），在这儿拦一次只会
		// 让「记住上次听到哪儿」这件事本身失败——那比不记住更糟。
		a.logf("biu: 「上次听到哪儿」存不住：%v", err)
	}
}

// SavePlayback 立刻把「上次听到哪儿」落一次盘。
//
// **退出前调它**（见 `App.Close` 和界面层的 OnWillQuit）：落盘是防抖合并写，
// 最后那一下有可能还等在退避窗口里，用户听了一会儿直接退出就丢了——而这一格
// 丢了的症状是「重启之后回到空队列」，用户只会觉得「这应用记不住东西」。
//
// ⚠️ **它绕过了那两个间隔**（见 savePlaybackIfDue）：退出这一刻就该把最后那个
// 状态写下去，等一个窗口就是丢一次。
func (a *App) SavePlayback() {
	a.persistPlayback()
}

// playbackSnapshot 拼出现在的队列、当前项和位置。
//
// 第二个返回值是**那个精确的纳秒位置**：落盘那一格是毫秒（那是给文件看的），
// 而落盘那一格的判据要拿它跟 `a.position` 比（见 savePlaybackIfDue）——单位
// 不一样的话永远不相等。
//
// ⚠️ **当前项存的是标识，不是下标**（见 PlaybackSnapshot.Current）。
func (a *App) playbackSnapshot() (PlaybackSnapshot, time.Duration) {
	a.mu.Lock()
	pos := a.position
	a.mu.Unlock()

	items := a.queue.Items()
	out := PlaybackSnapshot{
		Items:    make([]PlaybackEntry, 0, len(items)),
		Position: pos.Milliseconds(),
	}
	for _, it := range items {
		out.Items = append(out.Items, playbackEntryOf(it))
	}
	if cur, ok := a.queue.Current(); ok {
		out.Current = cur.Identity()
	}
	return out, pos
}

// ---- 恢复 ----

// RestorePlayback 恢复「上次听到哪儿」。
//
// **在开窗口之前调**（或者在一个 goroutine 里调完再要一帧）：本地文件那一条要
// 开解码器。
//
// ⚠️ **排在挂完在线取流之后**：在线的那一首要取流零件，而那要登录先挂好。
//
// 恢复出来是**暂停**的，停在原来那个位置（不自动出声）。三条路：
//
//   - 本地文件：离线就能开（解码器），停在那儿。
//   - 在线的那一首：要取流才能开（网络）。开不起来就**安静地跳过**——启动时
//     在播放条上冒一句红字是「应用在说自己」，不是在说给用户听。
//   - 一条都恢复不了：就什么都不做（应用照常开着，空队列）。
//
// 返回的 error 只给日志看，**不给用户看**：恢复失败不该在界面上冒一句。
//
// ⚠️ **它只报「整份都恢复不了」那一种**：个别条目认不出来是**正常**的（守卫
// 跳过了它，见 itemOfPlaybackEntry），那不是错，是这一格本来就有的边界。
func (a *App) RestorePlayback() error {
	snap := a.SettingsSnapshot().Playback
	if len(snap.Items) == 0 {
		return nil
	}

	items := make([]Item, 0, len(snap.Items))
	skipped := 0
	for _, e := range snap.Items {
		it, ok := itemOfPlaybackEntry(e)
		if !ok {
			// 🔴 **守卫**：安静地跳过那一条（理由见 itemOfPlaybackEntry）。
			skipped++
			continue
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return fmt.Errorf("上次的队列一条都恢复不了（%d 条都认不出来了），这一次从空队列开始", skipped)
	}

	// 队列换成恢复出来的那一份，当前项指到它该指的那一项。
	//
	// ⚠️ **当前项按标识找，不按下标**：恢复出来的顺序和当初保存的顺序可能
	// 不一样（中间有几条被守卫跳过了），按下标找会指着另一首。
	//
	// 🔴 **两趟：先严格、后退化到「种类 + 主 id」**（票 #683 的复审）。旧文件里
	// `Current` 写的是 `mv:<bvid>`（票 #683 之前没有 `|<cid>`），而恢复出来的
	// 那一项身份已经是 `mv:<bvid>|<cid>`（cid 从 `e.Cid` 来）——严格相等找不到，
	// idx 落到 0：**当前项变成第一首，播放位置也套到它身上**。升级后第一次启动
	// 的用户会听到队列第一首从「上次的 60 秒」开始。
	//
	// 退化那一趟只比「种类 + 主 id」（mv 取 bvid、audio 取 sid、本地取路径）：
	// 不同分集的主 id 相同，所以**只有在严格匹配失败时**才用——队列里同一视频
	// 的两集都存着的话，严格那一趟会先认对。
	a.queue.Reset(items...)
	idx := -1
	if snap.Current != "" {
		for i, it := range items {
			if it.Identity() == snap.Current {
				idx = i
				break
			}
		}
		if idx < 0 {
			// 退化那一趟：两边都拆到「种类 + 主 id」再比（理由见上）。
			if kind, id := splitIdentity(snap.Current); id != "" {
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
		// 当前项对不上（它自己被守卫跳过了，或者文件里那一格是空的）：
		// 落在第一首——**不落成「没有当前项」**：那样恢复出来的队列看着是空的，
		// 而用户明明看到过它。
		idx = 0
	}
	a.queue.selectAt(idx)

	cur, ok := a.queue.Current()
	if !ok {
		return nil
	}
	at := time.Duration(snap.Position) * time.Millisecond

	if cur.IsLocal() {
		// 本地文件：离线就能开，**停在那儿、不出声**。
		//
		// ⚠️ **打不开要安静**（见 openLocal 的 openRestore 那一档）：文件在、
		// 但这台机器解不开，也是「取不到」——启动时冒一句红字是应用在说自己。
		if err := a.openLocal(cur.Path, at, openRestore); err != nil {
			return nil
		}
		a.logf("biu: 恢复了上次的队列（%d 首，当前是 %q，位置 %v，跳过 %d 条）",
			len(items), cur.Title, at, skipped)
		return nil
	}

	// 在线的那一首：要取流才能开。开不起来就安静地跳过（见 failOnline 里
	// 「这一趟是恢复」那一处）。
	if err := a.playOnlineFrom(cur, at, true); err != nil {
		a.logf("biu: 恢复上次那一首取不了流：%q：%v", cur.Title, err)
		return nil
	}
	a.logf("biu: 恢复了上次的队列（%d 首，当前是 %q，位置 %v，跳过 %d 条，正在取流）",
		len(items), cur.Title, at, skipped)
	return nil
}
