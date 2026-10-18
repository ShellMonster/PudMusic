// 歌词这一摊在业务层的样子：当前这首歌的歌词、用户调过的偏移和字号。
//
// ## 切歌时先重置为默认值，再从缓存恢复
//
// 这是上游 `components/lyrics/index.tsx:94-102` 那一段的逐字照搬：`playId` 一
// 变就先 `setOffset(DEFAULT_OFFSET)` + `setFontSize(DEFAULT_FONT_SIZE)`，**然后**
// 才去读缓存，读到了才盖回去（`index.tsx:121-123`）。
//
// 顺序反了会怎样：换歌那一帧还带着上一首的偏移。新歌的歌词整体前移几秒，用户看着
// 是「这首歌的歌词对不上」——而他刚做的唯一一件事是换歌，不可能会想到是上一首留下
// 的那几秒。所以**重置必须同步、恢复必须异步**：读盘是阻塞 IO，放在换歌那条路上
// 就是每点一次歌冻一下窗口。
//
// ## 那 500ms 的防抖窗口用的是可换的钟
//
// 偏移和字号每改一下就写一次盘的话，拖一次滑块就是每秒 20 次。所以落盘前先等
// `lyricsSaveDelay`（500ms，照上游 `debounce(…, 500)`），等的过程中又改了就重新排。
// 这个等待走 `App.after`（见 app.go），测试换成手动推进的假钟——`PLAN.md` 的验收项
// 明写「假时钟测 500ms 防抖，不靠 time.Sleep」。
//
// ## 界面线程上不读盘
//
// 缓存是磁盘上的一小份 JSON。读它走后台 goroutine，读完**必须叫醒界面**
// （铁律三，见 `App.SetWake` 的注释）——只投递不唤醒的话表现是「歌词永远出不来」，
// 而且没有任何报错。
//
// ## 四个数字是死的
//
// 偏移 ±5000ms 步长 50、字号 12~48px（上游 `offset-control.tsx:18` 与
// `font-size-control.tsx:16`，`PLAN.md` 的 C-33/C-34 也是照抄它们），默认字号 20、
// 默认偏移 0（`index.tsx:31-32`）。**都别自己挑**——调偏了用户在 Biu 里习惯的那个范围
// 就在这儿对不上了。
package app

import (
	"log"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
)

const (
	// DefaultLyricsFontSize 是默认字号，抄 `lyrics/index.tsx:31` 的 DEFAULT_FONT_SIZE。
	DefaultLyricsFontSize = 20.0
	// MinLyricsFontSize / MaxLyricsFontSize 是字号的两个端点，抄
	// `lyrics/font-size-control.tsx:16` 的 `min = 12, max = 48`。
	MinLyricsFontSize = 12.0
	MaxLyricsFontSize = 48.0

	// MinLyricsOffsetMS / MaxLyricsOffsetMS 是偏移的两个端点，抄
	// `lyrics/offset-control.tsx:18` 的 `min = -5000, max = 5000`。
	MinLyricsOffsetMS = -5000
	MaxLyricsOffsetMS = 5000
	// LyricsOffsetStepMS 是偏移的步长，抄 `offset-control.tsx:20` 的 `step = 50`。
	LyricsOffsetStepMS = 50

	// lyricsSaveDelay 是调完值到写盘之间的防抖窗口，抄上游的 `debounce(…, 500)`
	// （`index.tsx:191`）。
	//
	// 拖偏移滑块时每 50ms 就变一次值，没有防抖就是每秒 20 次写盘。
	lyricsSaveDelay = 500 * time.Millisecond
)

// LyricsSnapshot 是歌词区一帧要读的全部状态。
//
// 拿的是一份自洽的拷贝（拷贝里 Rows 那一段是同一个切片，只读不改）。
type LyricsSnapshot struct {
	// Key 是当前这首歌的缓存键。切歌时变，界面靠它认出「换歌了」。
	Key string
	// Rows 是画出来的那一行行，已经把译文叠好了。空的 = 没有歌词。
	Rows []lyrics.Row
	// OffsetMS 是用户调过的歌词偏移（毫秒），正数 = 歌词整体往后挪。
	OffsetMS int
	// FontSize 是用户调过的字号（像素）。
	FontSize float32
	// HasLyrics 报有没有歌词。界面据此在「歌词」和「暂无歌词」之间选。
	HasLyrics bool
}

// Lyrics 拿当前歌词状态的拷贝。**每一帧调一次**。
func (a *App) Lyrics() LyricsSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return LyricsSnapshot{
		Key:       a.lyricKey,
		Rows:      a.lyricRows,
		OffsetMS:  a.lyricOffsetMS,
		FontSize:  a.lyricFontSize,
		HasLyrics: len(a.lyricRows) > 0,
	}
}

// MountLyrics 挂上歌词缓存。**在开窗口之前调**（建目录那一步是阻塞 IO）。
//
// 没挂也能跑：歌词区一直显示「暂无歌词」，调偏移和字号不报错也不生效。
func (a *App) MountLyrics(c *lyrics.Cache) {
	a.mu.Lock()
	a.lyricCache = c
	a.lyricGen++
	gen := a.lyricGen
	key := a.lyricKey
	a.mu.Unlock()

	// 挂着的时候已经有歌在放（测试里很常见），那就把这一首补上。
	if key != "" {
		a.loadLyrics(key, gen)
	}
}

// SetLyricsOffset 调歌词偏移，ms 会被夹进 [MinLyricsOffsetMS, MaxLyricsOffsetMS]。
func (a *App) SetLyricsOffset(ms int) {
	ms = min(max(ms, MinLyricsOffsetMS), MaxLyricsOffsetMS)
	a.mu.Lock()
	if a.lyricOffsetMS == ms {
		a.mu.Unlock()
		return
	}
	a.lyricOffsetMS = ms
	a.armLyricsSaveLocked()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// SetLyricsFontSize 调歌词字号，px 会被夹进 [MinLyricsFontSize, MaxLyricsFontSize]。
func (a *App) SetLyricsFontSize(px float32) {
	px = ClampLyricsFontSize(px)
	a.mu.Lock()
	if a.lyricFontSize == px {
		a.mu.Unlock()
		return
	}
	a.lyricFontSize = px
	a.armLyricsSaveLocked()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// ClampLyricsFontSize 把字号拉进 [MinLyricsFontSize, MaxLyricsFontSize]。
//
// 零值不是「字号 0」，是「**没调过**」——那是缓存里一条没有 lyrics 之外内容的
// 记录长什么样（见 `lyrics.Entry.FontSize` 的注释）。当成 0 的话界面上一个字都
// 看不见，而用户什么也没做。
func ClampLyricsFontSize(px float32) float32 {
	if px <= 0 {
		return DefaultLyricsFontSize
	}
	return min(max(px, MinLyricsFontSize), MaxLyricsFontSize)
}

// clampLyricsOffset 把偏移夹回那两个端点。零值合法（就是默认）。
func clampLyricsOffset(ms int) int {
	return min(max(ms, MinLyricsOffsetMS), MaxLyricsOffsetMS)
}

// lyricKeyOf 是这首歌的歌词缓存键。B 站在线的歌是 `${bvid}-${cid}`（上游
// `index.tsx:90` 带 cid 是因为同一首分 P 的每一集歌词不一样），本地文件没有这两个，给一
// 个自己的前缀。
//
// 本地那一条的键是**完整路径**：同名不同目录的两首歌必须分开，而用户挪了路径就是一个
// 新文件，本该重新找歌词。路径里的分隔符由 `lyrics.Cache.Path` 的哈希挡住。
func lyricKeyOf(it Item) string {
	if it.Bvid != "" && it.Cid != "" {
		return "bili-" + it.Bvid + "-" + it.Cid
	}
	if it.Path != "" {
		return "local-" + it.Path
	}
	if it.Sid != "" {
		return "bili-audio-" + it.Sid
	}
	return ""
}

// setLyricTrack 报「现在放的是这一首」，走一遍**先重置、再从缓存恢复**。同一首重报
// （比如换流之后重新报一次）直接返回：重置会把用户刚调好的偏移抹掉，而换流对用户来说
// 是「歌没断过」。
func (a *App) setLyricTrack(it Item) {
	key := lyricKeyOf(it)

	a.mu.Lock()
	same := a.lyricKey == key
	a.mu.Unlock()
	if same {
		return
	}

	// 🔴 换歌之前先把**上一首**欠着的那一次落盘做掉。
	//
	// 那道防抖窗口是 500ms，用户在这半秒里换歌的话，窗口到期时 `flushLyrics`
	// 读到的是**新歌**那条（歌词还空着），于是上一首的那一次调整被静默丢掉——
	// 界面上看不出任何异样：拖完偏移、换歌、回头再放上一首，偏移是 0。
	//
	// 上游不是这样：`debounce` 的那次调用把 playItem 当参数传进去（`index.tsx:191`），
	// 落盘仍然写换歌前那一首。这里在换状态**之前**落盘，效果一样。
	a.flushLyrics()
	a.stopLyricsSave()

	a.mu.Lock()
	a.lyricKey = key
	a.lyricGen++
	gen := a.lyricGen
	a.lyricRows = nil
	a.lyricEntry = lyrics.Entry{}
	// 同步重置到默认值。恢复要等读盘回来，中间这一段界面上是默认值——
	// 见文件头「切歌时先重置为默认值」那一节讲的是哪一条。
	a.lyricOffsetMS = 0
	a.lyricFontSize = DefaultLyricsFontSize
	a.mu.Unlock()

	a.loadLyrics(key, gen)
}

// loadLyrics 在后台读缓存，读回来装上状态并叫醒界面。
//
// 🔴 `gen` 是**歌词代次**，和在线取流那个 `onlineGen` 是一回事：换歌、换歌词源各加一，
// 迟到的结果回来发现代次变了直接走人。少了它会怎样：换歌 → 后台开始读缓存 → 用户刚
// 换了一份歌词 → 那次迟到的读回来把刚换的盖成缓存里原来的（多半是空的）。症状是「刚
// 找到的歌词一眨眼就没了」，而且按多少次搜索都一样。
func (a *App) loadLyrics(key string, gen int) {
	a.mu.Lock()
	c := a.lyricCache
	wake := a.wake
	a.mu.Unlock()

	if c == nil || key == "" {
		// 没挂缓存：就是没有歌词，界面会照实说「暂无歌词」。
		if wake != nil {
			wake()
		}
		return
	}

	go func() {
		entry, ok := c.Load(key)
		rows := lyrics.Rows(entry.Lyrics, entry.TLyrics)

		a.mu.Lock()
		if a.lyricKey != key || a.lyricGen != gen || a.closed {
			// 又换歌了 / 歌词被换过了 / 应用在退。这一份已经过时，丢掉。
			a.mu.Unlock()
			return
		}
		a.lyricEntry = entry
		if ok {
			a.lyricRows = rows
			// 只有真的调过才盖回去。缓存里没这两个字段时（老记录、别的源写的）
			// 保持上面刚重置的默认值——这就是「先重置、再恢复」的后半段。
			a.lyricOffsetMS = clampLyricsOffset(entry.OffsetMS)
			a.lyricFontSize = ClampLyricsFontSize(entry.FontSize)
		}
		wake := a.wake
		a.mu.Unlock()

		// 🔴 铁律三：后台投递完状态**必须**叫醒界面。只投递不唤醒的话，
		// 歌词永远停在「暂无歌词」这一帧，而没有任何报错。
		if wake != nil {
			wake()
		}
	}()
}

// PutLyrics 把一份歌词写进缓存，并立刻显示出来。这是给「找到了歌词」那一条路用的
// （`AdoptLyricPreview` 走的是 `putLyricsLocked`，见 lyrics_search.go），缓存键取
// 当前这一首。
//
// ⚠️ 译文**整个换掉**：旧源那份得跟着走，否则界面上叠着新原文和旧译文——两行对不上
// 的字，比没有译文更难懂。
func (a *App) PutLyrics(lrc, translated string) {
	a.mu.Lock()
	key := a.lyricKey
	if key == "" {
		a.mu.Unlock()
		return
	}
	entry, c := a.putLyricsLocked(key, lrc, translated)
	wake := a.wake
	a.mu.Unlock()
	a.saveLyrics(c, key, entry, wake)
}

// putLyricsLocked 把一份歌词装进状态，返回要落盘的那一份和缓存。**必须拿着 a.mu 调**；
// **落盘不在这里做**（见 `saveLyrics`）。
//
// 🔴 **key 是调用方给的，不是这里现读的**。原因在 `AdoptLyricPreview`：它要先验
// 「当前那首歌就是预览的那首」再落盘，而那次验和这次读**必须落在同一个临界区**里
// ——分两次取锁，中间落一次换歌，就会把甲的歌词写进乙的缓存文件，而且函数还返回
// 空串（成功）。写成「验一次、放锁、再调一个现读 key 的函数」正好就是那个窗口。
func (a *App) putLyricsLocked(key, lrc, translated string) (lyrics.Entry, *lyrics.Cache) {
	// 代次加一：正在回来的那次后台读盘（见 loadLyrics 的注释）到这时候已经过时了，
	// 它要是回来就会把刚换的这一份盖掉。
	a.lyricGen++
	// 偏移和字号是**用户调的值**，换一份歌词不该把它们带走（上游换源只换
	// `setLyrics`/`setTranslatedLyrics`，不动这两个，见 `index.tsx:269-278`）。
	entry := lyrics.Entry{
		Lyrics:   lrc,
		TLyrics:  translated,
		OffsetMS: a.lyricOffsetMS,
		FontSize: a.lyricFontSize,
	}
	a.lyricEntry = entry
	a.lyricRows = lyrics.Rows(lrc, translated)
	return entry, a.lyricCache
}

// saveLyrics 把 entry 写到 key 那一条上，然后叫醒界面。**锁外调**。
//
// ⚠️ 用**传进来的 entry**，不要回去读 `a.lyricEntry`：防抖落盘跑在
// `time.AfterFunc` 自己的 goroutine 上（见 flushLyrics），它会在锁里写
// `a.lyricEntry`；而 `lyrics.Entry` 里装的是 `string` 字段，撕裂读出来就是一个野
// 指针——`-race` 抓得到，产线上是随机崩。
func (a *App) saveLyrics(c *lyrics.Cache, key string, entry lyrics.Entry, wake func()) {
	if c != nil {
		if err := c.Save(key, entry); err != nil {
			log.Printf("biu: 歌词缓存写不下去：%v", err)
		}
	}
	if wake != nil {
		wake()
	}
}

// armLyricsSaveLocked 排一次防抖写盘。**必须拿着 a.mu 调**。
//
// 上一次那个要先停掉：拖滑块时每 50ms 改一次值，每次都新起一个定时器就是每秒 20 个
// goroutine，500ms 之后它们才陆续醒过来看一眼然后走掉——拖十秒就是两百个。
func (a *App) armLyricsSaveLocked() {
	if a.lyricStopTimer != nil {
		a.lyricStopTimer()
	}
	a.lyricStopTimer = a.after(lyricsSaveDelay, a.flushLyrics)
}

// flushLyrics 是防抖窗口到期之后的那一下：把偏移和字号连同歌词一起写回缓存。
//
// ⚠️ 写盘在锁**外面**做：定时器跑在自己的 goroutine 上，持有 `a.mu` 去等磁盘
// 会把整个应用的界面冻住（`Snapshot` 每帧都要这把锁）。
func (a *App) flushLyrics() {
	a.mu.Lock()
	c, key := a.lyricCache, a.lyricKey
	var entry lyrics.Entry
	if c != nil && key != "" {
		entry = a.lyricEntry
		entry.OffsetMS = a.lyricOffsetMS
		entry.FontSize = a.lyricFontSize
		a.lyricEntry = entry
	}
	a.mu.Unlock()

	if c == nil || key == "" || entry.Lyrics == "" {
		// 没有歌词就不写。偏移和字号是**附着在歌词上的**用户值，一首歌都没有
		// 歌词时它们无处可附——写下去就是一个只有 `{offset, fontSize}` 的空壳文件，
		// 用户数据目录里每听一首就积一个，而界面上那个值根本没有出处。
		return
	}
	if err := c.Save(key, entry); err != nil {
		// 写失败只记日志不弹提示：歌词是从别的源拿的（见 `OpenLyricPreview`），
		// 缓存写不下去不影响这一次听歌，而一个「歌词保存失败」的提示会让
		// 用户以为歌词没找到。
		log.Printf("biu: 歌词偏移/字号写不进缓存：%v", err)
	}
}

// closeLyrics 在应用退出时把还欠着的偏移和字号落完。
//
// 仿设置那边的做法（`settings_mount.go` 的 OnWillQuit + Flush）：后台写是防抖
// 合并的，最后一下有可能还等在窗口里，用户改完直接退出就丢了。
func (a *App) closeLyrics() {
	a.stopLyricsSave()
	a.flushLyrics()
}

// stopLyricsSave 停掉那个还在等的防抖窗口。
func (a *App) stopLyricsSave() {
	a.mu.Lock()
	stop := a.lyricStopTimer
	a.lyricStopTimer = nil
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
}
