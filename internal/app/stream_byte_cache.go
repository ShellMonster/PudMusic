// 音频字节的落盘缓存：**下过的字节留在盘上**，切回来直接读盘。
//
// # 这条解决的是真问题（2026-10-30 用户提出来的）
//
// 用户的原话：「前面比如取到了的，那应该是在本地电脑缓存上啊，
// 为啥还要再去跑网络请求；应该是没有才跑网络请求啊」。
//
// 原来的两层缓存都只解决了「**地址**」：
//
//	streamCache（地址）	PR #691：来回切歌不重复问 B 站要地址
//	byteCache（本文件）	下过的字节留在盘上，切回来连地址都不要
//
// 而**字节**那一层原来是没有的：`StreamSource` 用 `os.CreateTemp` 建一个
// 随机名的临时文件，`Close` 把它删掉。于是 A → B → A 这样切：
//
//	A 第一次播 → 整首下到盘上 → 切走 → **文件删了**
//	A 第二次播 → 又从零下一个整首 → CDN 又被拉一遍
//
// 用户每切回去一次就白下一整首。
//
// # 缓存键
//
// `source|type|bvid|cid|sid` **加上音质档**（`streamKey` + 音质）。
//
// ⚠️ **音质必须在键里**：同一首歌在 128K 和无损下是**两段不同的字节**，
// 共用一个文件的话，第二档会把第一档的字节覆盖掉，而第一档的文件名还
// 指向它——于是切回第一档时听到的是第二档的声音。
//
// ⚠️ **键必须在本地算得出来**：命中缓存时要**连地址都不要**（不调 playurl），
// 所以键里不能有「只有取流之后才知道」的东西。
//
// # 「下完了」怎么判
//
// ⚠️ **不靠文件大小**：字节源的文件布局是「顺序下载的前沿 + 并行取回来的
// 末尾那一段 + 中间可能的洞」（见 `service/audio/stream_source.go` 的
// `availAt`）。一个下到一半的文件大小可能和整首**一样**（末尾那一段先到了），
// 而它的中间是**洞**——洞里的字节是零，解出来是噪声。
//
// 所以完整性靠**旁挂的元数据**（`streamaudio.Entry.Complete`），
// 而元数据是**下完了那一刻**才写的（见 `markByteCacheComplete`）。

package app

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/streamaudio"
)

// byteCacheKey 是这一首（这一档）的字节缓存键。
//
// ⚠️ **它必须在不发任何请求的前提下算得出来**：命中缓存时要连 playurl
// 都不调（那才是「连地址都不要」），所以键里不能有「只有取流之后才知道」
// 的东西。
func byteCacheKey(it Item, q bili.AudioQuality) string {
	return streamKey(it) + "|" + q.Label()
}

// byteCacheSnapshot 拿当前那一份字节缓存（没有就 nil）。
func (a *App) byteCacheSnapshot() *streamaudio.Cache {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.byteCache
}

// cachedAudio 查这一首有没有一份**完整**的缓存字节，有就返回它的元数据和路径。
//
// ⚠️ **它只查盘，不发请求**：这是「连地址都不要」的前提。
//
// 返回 ok = false 表示「没有可用的缓存」，调用方照旧走网络那一路。
//
// ⚠️ **元数据要一起拿**：播放条上的音质徽章照实写缓存里**实际那一档**
// （用户点了无损而 B 站给了 192K，那徽章就该写 192K——写「无损」是骗人）。
func (a *App) cachedAudio(it Item, q bili.AudioQuality) (streamaudio.Entry, string, bool) {
	c := a.byteCacheSnapshot()
	if c == nil {
		return streamaudio.Entry{}, "", false
	}
	return c.CompleteEntry(byteCacheKey(it, q))
}

// buildFromCachedBytes 用盘上那份字节建播放链路。
//
// ⚠️ **它走的是本地文件那一条解码链**（`audio.Open`），不是在线流那条
// （`audio.OpenStream`）：字节已经在盘上了，再包一层字节源是没有意义的
// ——而那样会多一个下载 goroutine 和一个「关掉时要收拾」的源。
//
// ⚠️ **`a.online.source` 因此是 nil**：本地播的在线歌没有字节源。
// 读它的地方（`reconnect.go` 的「正在重连」那一档、`online.go` 的
// 「这条流为什么停了」）都要认这个情况（它们本来就在判 nil）。
func (a *App) buildFromCachedBytes(it Item, gen int, path string,
	run *loadRun,
) (*audio.Player, error) {
	start := time.Now()
	p, err := audio.Open(path)
	opened := time.Now()
	if run != nil {
		// 🔴 **缓存命中时，「等字节」和「等解码器」都是零**：
		// 字节在盘上，解码器开本地文件不需要网络。
		run.timing.Bytes = 0
		run.timing.Decode = opened.Sub(start)
	}
	if err != nil {
		return nil, err
	}
	// 音量和倍率**现读**（和 `buildFromRef` 同一套规矩）：建播放链路这中间
	// 用户完全可能按了音量或者换了倍速。
	a.mu.Lock()
	vol, rate, norm, eq := a.gainLocked(), a.rate, a.normalize, a.eqGains
	a.applyFadeInLocked(p)
	a.mu.Unlock()
	p.SetVolume(vol).SetRate(rate).SetNormalize(norm).SetEQ(eq)
	return p, nil
}

// byteCachePathFor 这一首（这一档）的缓存字节该落在哪儿。
//
// ⚠️ **返回空串表示「不落盘」**：没有缓存目录（拿不到用户数据目录）时，
// 在线播放照旧走临时文件那一路（那是「切歌不遗留」那道门）。
func (a *App) byteCachePathFor(it Item, q bili.AudioQuality) string {
	c := a.byteCacheSnapshot()
	if c == nil {
		return ""
	}
	return c.Path(byteCacheKey(it, q))
}

// markByteCacheComplete 在这一首**下完**之后，把缓存标成完整的。
//
// ⚠️ **它只在「真的下完了」时调**：字节源报了 `io.EOF` 才是下完
// （中途断了不算——那种情况下盘上是「一半 + 洞」，见 `availAt`）。
//
// ⚠️ **它不写字节，只写元数据**：字节是字节源自己写进那个文件的
// （`StreamSourceOptions.CachePath`），这一层只记「它齐了」。
func (a *App) markByteCacheComplete(it Item, q bili.AudioQuality, size int64, label string) {
	c := a.byteCacheSnapshot()
	if c == nil {
		return
	}
	key := byteCacheKey(it, q)
	now := time.Now()
	_ = c.Save(key, streamaudio.Entry{
		Key:       key,
		Size:      size,
		Complete:  true,
		FetchedAt: now,
		TouchedAt: now,
		Label:     label,
	})
}

// playFromByteCache 用盘上那份字节把这一首放起来。
//
// 🔴 **它不发任何请求**：字节在盘上，地址也不要（那才是用户要的：
// 「应该是没有才跑网络请求啊」）。
//
// ⚠️ **失败了就走网络那一路**：盘上那份字节可能坏了（用户手工改过、
// 磁盘出过错），而那不该让这一首放不出来——照旧去问 B 站要一条新的。
func (a *App) playFromByteCache(it Item, gen int, path string, run *loadRun) {
	s := a.streamingSnapshot()
	p, err := a.buildFromCachedBytes(it, gen, path, run)
	if err != nil {
		// 🔴 **盘上那份打不开就当它没有**：照旧走网络那一路。
		//
		// ⚠️ **不报错**：这一首**多半是能播的**（B 站那边还有），
		// 而用户看到的是「切回来放不出来」——那比多下一次烂缓存糟得多。
		a.logf("biu: 盘上那份字节打不开（当它没有，重新取流）：%q：%v", it.Title, err)
		a.runOnline(it, gen, 0, false)
		return
	}
	// 🔴 **音质徽章照实写**：缓存里记的是**实际那一档**
	// （用户点了无损而 B 站给了 192K，那徽章就该写 192K）。
	label := a.cachedQualityLabel(it)
	ref := &bili.StreamRef{
		Bvid:    it.Bvid,
		Cid:     it.Cid,
		Sid:     it.Sid,
		Label:   label,
		Quality: bili.QualityUnknown,
	}
	// ⚠️ **`a.online.source` 是 nil**：这一路没有字节源（字节已经在盘上了）。
	// 读它的地方都判 nil（见 `reconnect.go` 和 `online.go` 的那几处）。
	a.installOnlinePlayer(it, gen, ref, nil, p, 0, false, s)
}

// cachedQualityLabel 报盘上那一份的音质徽章文字。
//
// ⚠️ **读不出来就退回「用户要的那一档」**：那是**偏好的名字**，
// 不一定是实际那一档——但空徽章更糟（徽章只有一个来源，见
// `bili.Quality.Label` 的注释）。
func (a *App) cachedQualityLabel(it Item) string {
	c := a.byteCacheSnapshot()
	if c == nil {
		return ""
	}
	e, ok := c.Load(byteCacheKey(it, a.currentQuality()))
	if ok && e.Label != "" {
		return e.Label
	}
	return ""
}

// currentQuality 报当前那一份音质偏好。**必须在持锁时调**。
func (a *App) currentQuality() bili.AudioQuality {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streaming == nil {
		return bili.AudioHigh
	}
	return a.streaming.Quality
}
