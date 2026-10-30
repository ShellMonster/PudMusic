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
// # 键的构成：曲目 + 档
//
// 🔴 **字节按「实际那一档」存，不按「用户要的那一档」**：
// 用户点了「无损」而这一首没有无损，B 站会给 192K（降档，票 #581）。
// 于是「同一个偏好」在不同的歌上解析到不同的档，而「不同的偏好」在某些歌上
// 解析到同一个档——
//
//	按偏好存 → 同一段字节在盘上存两份、下两遍（用户那句话要的正是别这样）
//	按实际档存 → 一份字节只存一份，「无损」和「高音质」在**同一首歌**上
//	             解析到同一档时**指向同一份**
//
// 所以盘上有两类记录（见 `service/streamaudio`）：字节按「曲目 + 档」，
// 而「偏好 → 档」的映射单独一张表。
//
// # 「下完了」怎么判
//
// ⚠️ **不靠文件大小**：字节源的文件布局是「顺序下载的前沿 + 并行取回来的
// 末尾那一段 + 中间可能的洞」（见 `service/audio/stream_source.go` 的
// `availAt`）。一个下到一半的文件大小可能和整首**一样**（末尾那一段先到了），
// 而它的中间是**洞**——洞里的字节是零，解出来是噪声。
//
// 所以完整性靠**旁挂的元数据**（`streamaudio.Bytes.Complete`），
// 而元数据是**下完了那一刻**才写的。

package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/streamaudio"
)

// byteCacheTrack 是这一首的「曲目身份」（不含音质）。
//
// ⚠️ **它和 `streamKey` 是同一个东西**（`source|type|bvid|cid|sid`）：
// 投稿那一路靠 bvid/cid，音乐库那一路靠 sid——两者不会撞。
func byteCacheTrack(it Item) string { return streamKey(it) }

// byteCacheKeyReady 报「这一首的缓存键现在算得出来吗」。
//
// ⚠️ **判据是「身份齐了」**，不是「cid 非空」：
//
//	投稿那一路	要 bvid **和 cid**（cid 是分 P）
//	音乐库那一路	要 sid（它压根没有 cid）
//
// 用「cid 非空」当判据的话，**音乐库那一路永远算不出键**——
// 于是音乐库的条目一次都命中不了缓存。
func byteCacheKeyReady(it Item) bool {
	if it.Type == "audio" {
		return it.Sid != ""
	}
	return it.Bvid != "" && it.Cid != ""
}

// byteCachePref 是「用户要的那一档」的标识。
//
// ⚠️ **用数值不用 `Label()`**：越界的 `AudioQuality`（99 / -1）都会落到
// `Label()` 的兜底「高音质」，而那和真的 `AudioHigh` 撞键——于是两个不同的
// 偏好会共用一份字节。
func byteCachePref(q bili.AudioQuality) string { return strconv.Itoa(int(q)) }

// byteCacheTier 是**实际那一档**的标识（从 `StreamRef` 算）。
//
// ⚠️ **投稿那一路和音乐库那一路是两套编号**（见 `bili.SameTier`）：
// 投稿的档在 `Quality`（30280 这种），音乐库的档在 `MusicTier`（0..3）。
// 用哪一套要看 `it.Type`——而 `ref` 自己会告诉你它是哪一路
// （音乐库那一路 `MusicTier` 非零）。
func byteCacheTier(ref *bili.StreamRef) string {
	if ref == nil {
		return ""
	}
	if ref.MusicTier != 0 {
		return "m" + strconv.Itoa(int(ref.MusicTier))
	}
	return "q" + strconv.Itoa(int(ref.Quality))
}

// byteCacheSnapshot 拿当前那一份字节缓存（没有就 nil）。
func (a *App) byteCacheSnapshot() *streamaudio.Cache {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.byteCache
}

// cachedAudio 查这一首（在这个偏好下）有没有一份**完整**的缓存字节。
//
// ⚠️ **它只查盘，不发请求**：这是「连地址都不要」的前提。
//
// ⚠️ **它要连元数据一起拿**：命中缓存时不问 B 站，那么作者名、封面、
// 字幕那一对的 bvid/cid 都得**从上一次存下来的**拿（见 `streamaudio.Meta`），
// 否则那些格子会空着。
func (a *App) cachedAudio(it Item, q bili.AudioQuality) (streamaudio.Bytes, bool) {
	c := a.byteCacheSnapshot()
	if c == nil {
		return streamaudio.Bytes{}, false
	}
	return c.Lookup(byteCacheTrack(it), byteCachePref(q))
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
func (a *App) buildFromCachedBytes(path string, run *loadRun) (*audio.Player, error) {
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
func (a *App) byteCachePathFor(track, tier string) string {
	c := a.byteCacheSnapshot()
	if c == nil {
		return ""
	}
	return c.BytesPath(track, tier)
}

// markByteCacheComplete 在这一首**下完**之后，把缓存标成完整的。
//
// ⚠️ **它只在「真的下完了」时调**：字节源报了 `io.EOF` 才是下完
// （中途断了不算——那种情况下盘上是「一半 + 洞」，见 `availAt`）。
//
// ⚠️ **size <= 0 表示「不知道有多长」**：那样的记录**不算完整**
// （见 `streamaudio.Bytes.Complete`）。服务器不给 `Content-Length` 时，
// 字节源会用「下到了多少」当总长——而那是**截断的长度**，
// 拿它当「这一份是完整的」会让用户切回来听到放了一半的歌。
//
// ⚠️ **它不写字节，只写元数据**：字节是字节源自己写进那个文件的
// （`StreamSourceOptions.CachePath`），这一层只记「它齐了」。
func (a *App) markByteCacheComplete(it Item, q bili.AudioQuality, ref *bili.StreamRef, size int64) {
	c := a.byteCacheSnapshot()
	if c == nil {
		return
	}
	// 🔴 **size <= 0 表示「不知道有多长」**：那一份**不算完整**
	// （见 `streamaudio.Bytes.Complete`）。
	//
	// ⚠️ **服务器不给 `Content-Length` 时会走到这里**：那一份的字节可能是
	// **截断的**（连接中途结束，而客户端看不出「结束」和「被掐了」的区别）——
	// 把它当完整，用户切回来听到的就是放了一半的歌。
	//
	// ⚠️ **不记这一份**：下次重新下一遍，那只是「没省下流量」，
	// 而不会让用户听到坏东西。
	if size <= 0 {
		return
	}
	track := byteCacheTrack(it)
	tier := byteCacheTier(ref)
	if tier == "" {
		return
	}
	// 🔴 **「偏好 → 档」那张表**：下一次按同一个偏好来的时候，
	// 直接指向这一份字节（不重新问 B 站要档）。
	_ = c.SavePref(track, byteCachePref(q), tier)
	_ = c.SaveBytes(track, tier, size, ref.Label, streamaudio.Meta{
		Title:        it.Title,
		Cover:        it.Cover,
		Artist:       it.Artist,
		ArtistMid:    it.ArtistMid,
		SubtitleBvid: it.SubtitleBvid,
		SubtitleCid:  it.SubtitleCid,
	})
}

// dropBrokenByteCache 把「打不开的那一份」从缓存里删掉。
//
// 🔴 **它必须做**：盘上那份字节可能坏了（磁盘出过错、用户手工改过、
// 杀毒软件改写）。而**下一次**再查缓存时，它还是会被命中——
// 于是「打不开 → 走网络 → 打不开 → 走网络」会**无限循环**，
// 而用户看到的是「点了歌完全没反应，CDN 一次都没被拉」。
//
// ⚠️ **删的是「这一份字节」和「指向它的映射表」**：映射表指向一个已经不
// 能播的档，那它也不能留。
func (a *App) dropBrokenByteCache(it Item, q bili.AudioQuality, tier string) {
	c := a.byteCacheSnapshot()
	if c == nil {
		return
	}
	c.Remove(byteCacheTrack(it), tier)
	c.RemovePref(byteCacheTrack(it), byteCachePref(q))
}

// protectByteCache 标住「这一份正在用，不许淘汰」。
func (a *App) protectByteCache(track, tier string) {
	c := a.byteCacheSnapshot()
	if c == nil {
		return
	}
	c.Protect(track, tier)
}

// unprotectByteCache 解除「这一份正在用」。
func (a *App) unprotectByteCache(track, tier string) {
	c := a.byteCacheSnapshot()
	if c == nil {
		return
	}
	c.Unprotect(track, tier)
}

// releaseByteCacheProtect 解除「这一份正在用」的保护。
//
// 🔴 **它必须在「这一份不再被播」的时候调**：留着那个保护的话，它永远
// 不会被淘汰——用户听过的每一首歌都会一直占着盘，2 GB 上限形同虚设。
//
// ⚠️ **nil 是安全的**：走网络那一路的那一条没有盘上缓存（`cacheTrack` 是空串）。
func (a *App) releaseByteCacheProtect(tr *onlineTrack) {
	if tr == nil || tr.cacheTrack == "" || tr.cacheTier == "" {
		return
	}
	a.unprotectByteCache(tr.cacheTrack, tr.cacheTier)
}

// playFromByteCache 用盘上那份字节把这一首放起来。
//
// 🔴 **它不发任何请求**：字节在盘上，地址也不要（那才是用户要的：
// 「应该是没有才跑网络请求啊」）。
//
// ⚠️ **失败了就走网络那一路**：盘上那份字节可能坏了（用户手工改过、
// 磁盘出过错），而那不该让这一首放不出来——照旧去问 B 站要一条新的。
//
// ⚠️ **它绝不递归**：`runOnline` 里那个缓存判定会再次命中同一份坏缓存，
// 于是「打不开 → 走网络 → 缓存判定又命中 → 打不开」会**无限循环**
// （用户看到的是「点了歌完全没反应」）。所以这里走的是
// `runOnlineWithoutCache`——**跳过缓存判定**的那一条路。
func (a *App) playFromByteCache(it Item, gen int, rec streamaudio.Bytes, at time.Duration, paused bool, run *loadRun) {
	s := a.streamingSnapshot()
	p, err := a.buildFromCachedBytes(a.byteCachePathFor(rec.Track, rec.Tier), run)
	if err != nil {
		// 🔴 **盘上那份打不开就当它没有**：照旧走网络那一路。
		//
		// ⚠️ **先把这一份删掉**：不删的话下一次还是命中它，
		// 而那就是一个死循环（见上面的注释）。
		a.logf("biu: 盘上那份字节打不开（当它没有，重新取流）：%q：%v", it.Title, err)
		a.dropBrokenByteCache(it, a.currentQuality(), rec.Tier)
		a.runOnlineWithoutCache(it, gen, at, paused)
		return
	}
	// 🔴 **音质徽章照实写**：缓存里记的是**实际那一档**
	// （用户点了无损而 B 站给了 192K，那徽章就该写 192K）。
	ref := &bili.StreamRef{
		Bvid:    it.Bvid,
		Cid:     it.Cid,
		Sid:     it.Sid,
		Label:   rec.Label,
		Quality: bili.QualityUnknown,
	}
	// ⚠️ **档位要填对**：填 0 的话「选回当前这一档」就不再是无操作——
	// 用户点一下音质 chip 就会打断正在播的歌并重新下一遍（见
	// `bili.SameTier`）。
	if rec.Tier != "" {
		if len(rec.Tier) > 1 && rec.Tier[0] == 'm' {
			if n, err := strconv.Atoi(rec.Tier[1:]); err == nil {
				ref.MusicTier = bili.MusicTier(n)
			}
		} else if len(rec.Tier) > 1 && rec.Tier[0] == 'q' {
			if n, err := strconv.Atoi(rec.Tier[1:]); err == nil {
				ref.Quality = bili.Quality(n)
			}
		}
	}
	// 🔴 **这一份正在用，不许淘汰**（见 `streamaudio.Cache.Protect`）。
	a.protectByteCache(rec.Track, rec.Tier)
	// ⚠️ **`a.online.source` 是 nil**：这一路没有字节源（字节已经在盘上了）。
	// 读它的地方都判 nil（见 `reconnect.go` 和 `online.go` 的那几处）。
	a.installOnlinePlayer(it, gen, ref, nil, p, at, paused, s, rec.Track, rec.Tier)
}

// currentQuality 报当前那一份音质偏好。
func (a *App) currentQuality() bili.AudioQuality {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streaming == nil {
		return bili.AudioHigh
	}
	return a.streaming.Quality
}

// byteCacheTierLabel 报一档的徽章文字（从档标识反推）。
//
// ⚠️ **认不出来就给空串**：徽章只有一个来源（见 `bili.Quality.Label` 的注释），
// 而一个错的徽章是在撒谎。
func byteCacheTierLabel(tier string) string {
	switch tier {
	case "":
		return ""
	case "m3":
		return "无损 FLAC"
	case "m2":
		return "320K"
	case "m1":
		return "192K"
	case "m0":
		return "128K"
	case "q30251":
		return "Hi-Res 无损"
	case "q30250":
		return "杜比全景声"
	case "q30280":
		return "192K"
	case "q30232":
		return "128K"
	case "q30216":
		return "64K"
	}
	return ""
}

// fmtTier 报一档的标识（给日志看）。
func fmtTier(tier string) string {
	if tier == "" {
		return "（未知档）"
	}
	return fmt.Sprintf("%s（%s）", tier, byteCacheTierLabel(tier))
}
