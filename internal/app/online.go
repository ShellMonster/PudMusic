// 在线播放：把收藏夹里的一个条目变成正在响的一首歌。
//
// # 三条换流路径，分工在这一层和 audio 那一层
//
//	播放前预判   开之前先看手上那条流还能不能用（这一层）
//	报错后重试   源下不动 → 换备用地址 → 换流（audio 层）
//	换流续播     换流之后从**同一个字节偏移**接着下，位置一点不动（audio 层）
//
// 为什么换流的机制测试在 `audio` 包里：那里能精确造出「下到第 N 字节时
// 这条地址失效」，而这里测的是它有没有被正确地接上——代次对不对、换到的
// 还是不是同一首、界面上的音质徽章有没有跟着变。
//
// # 界面线程上不碰网络
//
// 取流是一次带 WBI 签名的网络往返（实测 200~400 ms）。`play` 是界面线程
// 直接调的，在它里面同步取流就是每点一次歌冻一下窗口。所以在线这一支是
// **开后台 goroutine** 做的，靠**播放代次**（`gen`）把迟到的结果丢掉：
// 连点五首，前四首的取流结果回来时发现代次已经变了，直接关掉走人。
//
// 这一条也是 `spec.md`「播放状态机」要求的：**异步结果必须带代次，
// 代次不匹配的丢弃**。

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// errStreamingMissing 是「在线播放没挂上」。见 `MountStreaming`。
//
// ⚠️ **这句是给用户看的**（播放失败时它就是失败原因），不许说「还没接上」
// （票 #242，UX 报告 §3-F 的 F2）。
var errStreamingMissing = errors.New("在线播放暂时用不了")

// onlinePlayTimeout 是「**这一段**网络最多等多久」，超过就当它失败了。
//
// # 为什么是 20 秒
//
// 20 秒不是随手挑的：它是**字节源自己的「这条流拉不动了」判定**
// （`service/audio/stream_source.go` 的 `defaultStallTimeout`）。
//
// 关键性质是**不比它早**：比它早的话，应用会在一条**慢而没死**的流上先动手——
//
//	接口慢 8 秒（还在 B 站接口那一头）+ CDN 的头几个字节 8 秒 = 16 秒
//
// 这一条本来是放得出来的，而一个 15 秒的闹钟会在它放出来之前就把它掐了
// （票 #282 的评审指出来的）。所以判据是「**不比下面那一层更早放弃**」。
//
// # 🔴 **每一段都重新起算**，不是「从点下去起算 20 秒」
//
// 取流这一趟是**三段**串行的网络往返：
//
//	问哪一集 / 补元信息	最多 10 秒（`bili.Client` 自己的超时）
//	取流接口		最多 10 秒（同上）
//	CDN 的头几个字节	最多 20 秒（字节源的卡死判定）
//
// 一次性的闹钟会把**前两段**的时间算进去：接口慢 10 秒（到顶）+ CDN 的头
// 11 秒 = 21 秒，本来放得出来，却在一个 20 秒的闹钟上先响了。所以每一段
// 结束都**重新起算**——判据是「**这一段**最多等 20 秒」。
//
// 最坏情况因此是 10 + 20 = **30 秒**（接口那一段自己封顶 10 秒），而用户
// 从头到尾看得见「正在取流：<歌名>」——不是一片沉默。
const onlinePlayTimeout = 20 * time.Second

// defaultPlayLog 是播放路径的默认日志出口（**每个 App 一份**，见 App.playLog）。
//
// 生产上它就是标准 logger——`main.go` 把标准 logger 指到日志文件（见
// `platform.OpenLogFile`），于是这一层写进去的东西**事后查得到**（票 #282）。
//
// ⚠️ **为什么不是一个包级变量**：包级变量会让**两个并行的应用实例**（测试里
// 常有）互相看见对方的日志，而这一层的日志正是「哪一次播放出的事」的唯一
// 凭据——串了的话，事后排查会指着另一首歌的日志找原因。
func defaultPlayLog(format string, args ...any) { log.Printf(format, args...) }

// Streaming 是取流这一摊事的依赖。零值不挂——界面要判空。
type Streaming struct {
	// Client 是 B 站客户端。**全项目那一份**（`bili.NewClient`）。
	Client *bili.Client
	// Media 是取音频字节用的 http.Client。
	//
	// ⚠️ 它**必须**是 `app.CredentialTransport.Client(nil)`——带凭据边界
	// 但**不挂 jar**。B 站 CDN 跟 B 站 API 不是同一个凭据域，把 SESSDATA
	// 发给一台存音频文件的机器没有任何理由（`spec.md`「网络」一节）。
	// Referer 照给：B 站 CDN 不给直接 403（真机验过）。
	Media *http.Client
	// Quality 是默认音质偏好。零值是 AudioAuto（有无损给无损）。
	Quality bili.AudioQuality
	// TempDir 是下载缓存落在哪。空 = 系统临时目录。
	TempDir string
}

// onlineTrack 是「当前正在放的那条在线流」。
type onlineTrack struct {
	item Item
	// ref 是这条流**当前**的引用。换流之后要换掉——它同时是「换回来的
	// 还是不是同一首」的比对基准（见 `refetch`）。
	ref    *bili.StreamRef
	source *audio.StreamSource
	// reopening 是「已经因为换了流而重开过一次了」。
	//
	// 防止「换流 → 又失败 → 再换流 → 再重开」绕圈。一首歌只重开一次：
	// 第二次还不行就该老老实实报错，用户听得到「这首歌放不了了」，
	// 比程序一直自己折腾强。
	reopening bool
	// quality 是当前这条流的音质徽章。换流之后要跟着更新。
	quality string
}

// streamKey 是取流缓存的键。
//
// 用 Biu 的判重标识（`source/type/bvid/cid/sid`）而不是队列下标：换了歌
// 回来时同一条流还能复用，省一次带签名的往返。
func streamKey(it Item) string {
	return it.Source + "|" + it.Type + "|" + it.Bvid + "|" + it.Cid + "|" + it.Sid
}

// MountStreaming 挂上在线播放。**在开窗口之前调**（它自己不读盘，但让
// 第一���点歌不要替「开窗」买单）。
func (a *App) MountStreaming(s Streaming) {
	a.mu.Lock()
	a.streaming = &s
	a.streamCache = map[string]*bili.StreamRef{}
	a.mu.Unlock()
	// 播放记录上报**跟着在线播放起**：只有在线流才有得报（本地文件在 B 站
	// 那边没有对应的东西），所以它挂在这一摊上而不是挂在登录上——挂登录
	// 会让「没挂在线播放也照发」那条路变成真的。
	a.mountPlayReport(s.Client)
}

// StreamingMounted 报告在线播放挂上没有。
func (a *App) StreamingMounted() bool { return a.streamingSnapshot() != nil }

// SetAudioQuality 换音质偏好，并且**立刻生效**。
//
// 「立刻生效」指的是正在放的那首在线流：重新按新档取一条流，从**原来的
// 播放位置**接着播。不需要切歌，也不需要重启应用——用户在播放条上点一下就
// 换好了，那正是这个控件存在的意义。
//
// 三件事一起做：
//
//	偏好落到设置里（下次启动还在）
//	缓存清掉（否则下一首还会拿旧地址）
//	正在放的那首重新取流（这是「立刻」两个字的意思）
//
// 拿不到新档（要无损但没有大会员）时**不打断正在放的歌**：取流失败就把
// 原因报到播放条上，原来那条流继续放着。用户为无损付了钱，静默降级比报错
// 更糟，而报错不该以「歌突然停了」的形式出现。
func (a *App) SetAudioQuality(q bili.AudioQuality) {
	a.mu.Lock()
	if a.streaming == nil {
		a.mu.Unlock()
		return
	}
	a.streaming.Quality = q
	a.streamCache = map[string]*bili.StreamRef{}
	tr := a.online
	pos, want, gen := a.position, a.wantPlay, a.onlineGen
	wake := a.wake
	a.mu.Unlock()

	a.rememberAudioQuality(q)
	if tr != nil {
		a.restreamOnline(tr, q, pos, want, gen)
	}
	if wake != nil {
		wake()
	}
}

// rememberAudioQuality 把偏好落到设置里，下次启动还是这一档。
//
// 存不住不算错：设置层自己有入口能看到「这次改的设置存不住」，
// 在这里拦一次只会让换音质这件事本身失败——那比不记住更糟。
func (a *App) rememberAudioQuality(q bili.AudioQuality) {
	pref := BiliToPref(q)
	_, _ = a.UpdateSettings(func(s *Settings) { s.AudioQuality = pref })
}

// restreamOnline 按新档重新取流，从原来的播放位置接着播。
//
// 先判「其实没变」：用户点的那一档拿回来的还是同一档（B 站只给了 192K，
// 用户从自动点「高音质」），那就什么都别做——为一个听不出区别的改动
// 打断一首歌，比点了没反应更糟。
//
// 换流之后的处理和 `refreshOnlineNow` 一样：跳回原来的**播放位置**，那一步
// 是「换流后从原进度继续」的全部意义。
func (a *App) restreamOnline(tr *onlineTrack, q bili.AudioQuality, pos time.Duration, want bool, gen int) {
	s := a.streamingSnapshot()
	if s == nil {
		a.failOnline(gen, tr.item, "换音质（取流零件没挂）", errStreamingMissing)
		return
	}

	go func() {
		ref, err := a.resolve(context.Background(), s, tr.item, q)
		if err != nil {
			a.failQualitySwitch(gen, q, err)
			return
		}
		if ref.SameTier(tr.ref) {
			// 同一档：换过去听不出区别，不打断。
			//
			// 🔴 但**顺手把上一句红字清掉**。上一次换档失败留下的那句话
			// （「这一首没有无损」）到这一步已经是**错的**：用户点的档拿回来的
			// 正是现在正在放的那一档，歌好好地在放，而那句话会一直挂到下一首
			// 歌为止。`failQualitySwitch` 那一大段注释说的就是「一句错的、且
			// 不会自己清掉的红字」——那是失败的那一半，这一条是成功的那一半，
			// 当时漏了。
			a.clearStaleQualityErr(gen, tr)
			return
		}
		src, p, err := a.buildFromRef(context.Background(), s, tr.item, gen, ref)
		if err != nil {
			a.failQualitySwitch(gen, q, err)
			return
		}

		a.mu.Lock()
		if a.onlineGen != gen || a.closed || a.online != tr {
			// 已经不是这一条在线流了：切歌了，或者出错那条路
			// （`refreshOnlineNow`）已经换过一遍。装上去就是两份播放器
			// 抢一个播放位置。
			cur := a.onlineGen
			a.mu.Unlock()
			p.Close()
			a.logf("biu: 换音质的结果被作废了（已经不是这一条流了）：%q（来的代次 %d，现在的代次 %d）",
				tr.item.Title, gen, cur)
			return
		}
		if a.streaming == nil || a.streaming.Quality != q {
			// 用户又换了一档，而这一次的结果还没回来：作废。
			// 不看这一条的话，连点两下时先发后到的那个会赢——
			// 界面上是 A 档，声音是 B 档。
			a.mu.Unlock()
			p.Close()
			a.logf("biu: 换音质的结果被作废了（用户又换了一档）：%q", tr.item.Title)
			return
		}
		old := a.player
		a.player = p
		a.duration = p.Duration()
		a.position = pos
		a.quality = ref.Label
		a.online = &onlineTrack{
			item: tr.item, ref: ref, source: src,
			quality: ref.Label,
		}
		// 源归播放器管了（见 App.pendingSrc）。
		a.pendingSrc = nil
		a.reportedErr = nil
		a.clearErrLocked()
		wake := a.wake
		a.mu.Unlock()

		if old != nil {
			old.Close()
		}
		if pos > 0 && pos < p.Duration() {
			p.Seek(pos)
		}
		if want {
			p.Play()
		}
		if wake != nil {
			wake()
		}
	}()
}

// clearStaleQualityErr 清掉一句已经不成立的换档提示。
//
// **只在歌好好地在放的时候清**（播放器还活着、`reportedErr` 是空的）：那时候
// 挂在播放条上的那句话必定是上一次换档留下的，而它说的已经是上一档的事了。
//
// ⚠️ 真出了播放错误（网络断、字节源拉不动）时那句话是**真的**，不许清——
// 清了就成了「静悄悄地停着」，那比留一句过时的话糟得多。这和成功那一支里
// `a.clearErrLocked()` 不一样：那一支是先换好了流再清，那一句一定是旧的。
func (a *App) clearStaleQualityErr(gen int, tr *onlineTrack) {
	a.mu.Lock()
	stale := a.onlineGen != gen || a.closed || a.online != tr ||
		a.reportedErr != nil || a.player == nil || a.errText == ""
	a.mu.Unlock()
	if stale {
		return
	}
	a.setErr("")
}

// failQualitySwitch 报一次换音质的失败，**只在用户要的还是这一档时报**。
//
// 为什么成功的那一半要判「用户是不是又换档了」（`restreamOnline` 里
// `a.streaming.Quality != q` 那一条），失败的这一半也得判——不然会留下
// 一句**错的、且不会自己清掉**的红字：
//
//	点「无损」→ 取流慢，700ms 后失败
//	点「高音质」→ 立刻成功，errText 被清空
//	700ms 后第一条的失败才回来 → 播放条上写着「这一首没有无损」
//
// 而歌正按高音质好好地放着。`errText` 只在下一次成功播放/换流时才清，
// 那可能是几首歌之后的事了。
func (a *App) failQualitySwitch(gen int, q bili.AudioQuality, err error) {
	a.mu.Lock()
	stale := a.onlineGen != gen || a.streaming == nil || a.streaming.Quality != q
	var it Item
	if a.online != nil {
		it = a.online.item
	}
	a.mu.Unlock()
	if stale {
		// 被后来的选择取代了：这一次的结果没人要了，报出去只会误导。
		a.logf("biu: 换音质的结果被作废了（用户又换档了）：%q：%v", it.Title, err)
		return
	}
	a.failOnline(gen, it, "换音质", err)
}

// AudioQuality 返回当前偏好。
func (a *App) AudioQuality() bili.AudioQuality {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streaming == nil {
		return bili.AudioAuto
	}
	return a.streaming.Quality
}

// PrefToBili 把设置里那一档翻译成取流用的那一档。
//
// 设置里存的是字符串（落盘给人看、跨版本稳），取流那边要的是 B 站的编号。
// 认不出来的值一律当「自动」——宁可给最好的那一档，也不要因为设置文件里
// 多了一个字符就整条在线播放放不出来。
func PrefToBili(q AudioQuality) bili.AudioQuality {
	switch q {
	case AudioQualityLossless:
		return bili.AudioLossless
	case AudioQualityHigh:
		return bili.AudioHigh
	case AudioQualityMedium:
		return bili.AudioMedium
	case AudioQualityLow:
		return bili.AudioLow
	}
	return bili.AudioAuto
}

// BiliToPref 是 `PrefToBili` 的反方向：取流那边那一档在设置里写成什么。
// 界面拿它把「当前偏好」和菜单里的项对上。
func BiliToPref(q bili.AudioQuality) AudioQuality {
	switch q {
	case bili.AudioLossless:
		return AudioQualityLossless
	case bili.AudioHigh:
		return AudioQualityHigh
	case bili.AudioMedium:
		return AudioQualityMedium
	case bili.AudioLow:
		return AudioQualityLow
	}
	return AudioQualityAuto
}

// streamingSnapshot 拿一份配好的取流零件。没挂或者缺零件时是 nil。
func (a *App) streamingSnapshot() *Streaming {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streaming == nil || a.streaming.Client == nil || a.streaming.Media == nil {
		return nil
	}
	s := *a.streaming
	return &s
}

// isOnline 报告这个条目是不是一条在线流。
func (it Item) isOnline() bool { return it.Source == "online" }

// playableOnline 报告这个在线条目现在有没有足够的信息去取流。
//
// 两条取流路径各认一种身份：
//
//	mv     bvid（cid 缺了能补，见下）
//	audio  sid（B 站音乐库的 auid）
//
// ⚠️ **判据必须和「真能放」一致**。这一条判「可放」的东西，界面上就会照着画、
// 照着进队列——所以这里放行一种而那一路没接上，表现就是「收藏夹里的条目能看见、
// 能进队列，按播放必然报错」。`bili.Media.Playable` 当初就是那个情况（判据抄了
// Biu 的 `[2, 12]`，而 sid 那一路没实现）。加一条路径进来的时候，取流那一段
// （`streamFor`）要一起加；`TestEveryPlayableEntryCanActuallyBeStreamed` 盯着这件事。
//
// ⚠️ **cid 不再是「有没有」的判据**：B 站的搜索结果只给 bvid，而搜索页那一整个
// Tab 都是这样的条目（见 `service/bili/search.go` 的文件头）。以前这里要求 cid，
// 表现是搜出来的歌一首都放不了，播放条上红字写「缺稿件 id 或分 P id」（那时 cid 还是判据之一）——用户按的
// 就是搜索结果，说他缺一个他压根拿不到的东西，这句话是错的。
//
// cid 缺失时不是「放不了」而是「放哪一集还不知道」：那是**要问一次**才能知道的
// 事情，不是当场能判出来的。`runOnline` 在后台问一次分 P（`bili.VideoPages`），
// 问不到才退到第一集。同步能判出来的仍然是「压根没有 bvid」和「不是投稿」这两种。
//
// 🔴 **拒它的理由现在是一句带类型的话**（`Unplayable`），不是「这一首还不能放：
// 缺稿件 id」：那句话说得像应用没接完，而这一条其实**接完了**——是这一条
// 压根没有可播的稿件。界面上要靠这个类型决定「给不给播放入口」，也要靠它
// 说清「还能怎么办」（见 `unplayable.go`）。
func playableOnline(it Item) error {
	switch {
	case it.Type == "audio":
		if it.Sid == "" {
			return unplayableOf(UnplayableNoSid)
		}
		return nil
	case it.Type == "mv" && it.Bvid != "":
		return nil
	case it.Type == ItemTypeUser:
		// ⚠️ **它不是一首歌**：搜索结果的用户 Tab 那一行，「播放」是进 UP 主
		// 空间。它压根没有「能不能放」这回事——界面据此照常给那个入口
		// （见 `CanPlay`），而这里只是拦住「把它塞进队列」。
		return unplayableOf(UnplayableNotSong)
	case it.Type == "collection":
		// 合集 / 电影 / 番剧那一类：它们**不是投稿**，也没有单独的音频流。
		//
		// ⚠️ **它和「没挂稿件的推荐条目」不是同一种**：那一种是「B 站没给
		// 它挂可播的稿件」，而这一种是「这一条本来就是合集，里面才有单集」。
		// 用户照着做的下一步也不一样（搜同名 vs 打开里面的单集）。
		return unplayableOf(UnplayableNoAudioFlow)
	}
	return unplayableOf(UnplayableNoArchive)
}

// OnlineReady 报告这一项在线播放的身份齐了没。
//
// 它是给**测试**用的：那条不变量要钉的正是「`Playable` 说的和播放器认的认的是
// 不是同一件事」，而在测试里比对的就是 `bili.Media.Playable()` 和这一步
// （`ui.TestEveryPlayableCollectionEntryIsAcceptedByThePlayer`）。
//
// ⚠️ **界面层那一处走的是 `app.CanPlay`**（同一个判据，但先判本地文件）：
// 菜单里「给不给播放入口」问的是它（见 `internal/ui/widgets/list/menu.go`）。
// 这一条留着是因为测试要一个能直接问「这一项放不放得了」的地方。
//
// 它和 `playableOnline` 是同一个判据，分成两个是因为一个给断言用、一个要在
// 播放那一步报出原因。
func (it Item) OnlineReady() bool { return playableOnline(it) == nil }

// playOnline 开一条在线流。**立刻返回**，真正的活儿在后台 goroutine 里。
//
// 返回的错误只包含「同步就能看出来」的那几种（条目不完整、没挂取流），
// 它们会让调用方把队列滚回去；网络上的失败走播放条上的红字。
func (a *App) playOnline(it Item) error { return a.playOnlineFrom(it, 0, false) }

// playOnlineFrom 是「开一条在线流」的那一条路，可以指定「从哪儿开始」和「开了
// 之后**别出声**」。
//
// 那两个参数只有「恢复上次听到哪儿」那一趟会带（见 playback_state.go）：at 是
// 要停到的位置，paused 是「开了之后别出声」。
//
// ⚠️ **它不能靠「先放再停」**：出声是 `p.Play()` 那一刻就发生的，而启动时突然
// 出声是最糟的一种（用户刚打开应用，房间里突然有声音）。
func (a *App) playOnlineFrom(it Item, at time.Duration, paused bool) error {
	if err := playableOnline(it); err != nil {
		return err
	}
	if !a.StreamingMounted() {
		return errStreamingMissing
	}

	// 代次在这里加。后面每一次往状态上写东西都要对一遍。
	a.mu.Lock()
	a.onlineGen++
	gen := a.onlineGen
	a.clearErrLocked()
	// 🔴 **记下用户刚才按的是哪一首**：重试要放的是它，而不是队列里当前的
	// 那一首——取流失败时队列常常已经滚回去了（见 `App.Retry`）。
	a.pending = it
	// 🔴 **「正在取流」从这一刻开始**（票 #282）：取流要跑一趟网络，而那一趟
	// 里界面上什么都不会变——不说这一句，用户看到的就是「点了没反应」。
	// 超时由 `loadTimedOut` 兜着，不让它变成另一种「什么都不说」。
	a.beginLoadingLocked(gen, it.Title)
	// 🔴 **这一趟是「恢复」**：它放不起来的时候**不许**在播放条上冒一句红字
	// ——启动时用户什么都没做就被骂一句「打不开」，那是应用在说自己，不是在
	// 说给用户听。见 `failOnline` 里认这一代次的那一处。
	if paused {
		a.restoreGen = gen
	}
	wake := a.wake
	a.mu.Unlock()

	a.logf("biu: 开始取流：%q（%s/%s bvid=%s cid=%s sid=%s）代次 %d",
		it.Title, it.Source, it.Type, it.Bvid, it.Cid, it.Sid, gen)
	if wake != nil {
		wake()
	}
	go a.runOnline(it, gen, at, paused)
	return nil
}

// beginLoadingLocked 记下「正在取流」，并把超时的闹钟挂上。**必须在持锁时调**。
//
// 闹钟带的是**这一次**的代次和歌名：超时那一刻要判「还在不在这一趟里」，
// 而歌名是要写进那句话和日志里的（用户要知道是哪一首超时了）。
func (a *App) beginLoadingLocked(gen int, title string) {
	a.loading = true
	a.loadingTitle = title
	// 挂新闹钟之前先停掉上一个：连点两首时，前一个的闹钟不能替后一个喊。
	if a.loadStop != nil {
		a.loadStop()
		a.loadStop = nil
	}
	a.loadStop = a.after(onlinePlayTimeout, func() { a.loadTimedOut(gen, title) })
}

// rearmLoading 重新起算「这一段最多等多久」。**必须在持锁时调**。
//
// 🔴 **它不碰「正在取流」那一档**，只重挂闹钟：那一档从头到尾都是「这一首
// 正在准备」，而闹钟量的是「**这一段**网络还没回」。
//
// ⚠️ **代次不对就不动**：那一趟已经被取代了（切歌 / 超时），它的闹钟早就
// 被收掉了，别去动**新的**那一趟的。
func (a *App) rearmLoading(gen int, title string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.onlineGen != gen || a.closed || !a.loading {
		return
	}
	a.beginLoadingLocked(gen, title)
}

// endLoadingLocked 把「正在取流」这一档收掉，并把闹钟停掉。
// **必须在持锁时调**。
func (a *App) endLoadingLocked() {
	a.loading = false
	a.loadingTitle = ""
	if a.loadStop != nil {
		a.loadStop()
		a.loadStop = nil
	}
}

// loadTimedOut 是「点了歌之后一直没开始」的闹钟响了。
//
// 🔴 **它按失败处理**：超了阈值就当这一首放不起来，给用户一句能看懂的话，
// 而不是让界面上那枚转圈一直转（票 #282）。
//
// **作废在途的那一次**：代次加一，那条还在跑的取流回来时对不上代次，自己走人
// ——它要是回来得比这个闹钟晚，装上来的话用户会看到「说了失败、结果又放起来了」。
func (a *App) loadTimedOut(gen int, title string) {
	a.mu.Lock()
	if a.onlineGen != gen || a.closed || !a.loading {
		// 这一趟已经不是当前那一趟了（用户又点了一首，或者它已经成了）。
		a.mu.Unlock()
		return
	}
	a.onlineGen++
	a.endLoadingLocked()
	restore := a.restoreGen == gen
	wake := a.wake
	a.mu.Unlock()

	// 🔴 **把在途的下载掐掉**：光在界面上说「超时」不够——那条字节源还在
	// 后台下载（而播放器还没建起来，`App.Close` 也管不到它）。不掐掉的话，
	// 这一次「已经判它失败」的取流会一直下到天亮。
	a.abortPendingSource()

	a.logf("biu: 取流超时：%q（代次 %d，%v 内这一段还没回）", title, gen, onlinePlayTimeout)
	// 🔴 **恢复那一趟：安静地跳过**（理由同 `failOnline` 里那一处）。启动时在
	// 播放条上冒一句「取流超时了」，用户会以为是自己点的歌放不出来——他这一
	// 次连歌都还没点。
	if restore {
		a.restoreGen = 0
		if wake != nil {
			wake()
		}
		return
	}
	// 🔴 **上屏的那一句必须短**（票 #282 的评审）：播放条上那一行只有 140 像素宽
	// （1200 宽的窗口，实测），12 号字放得下十来个字，而前面还顶着「打不开：」
	// 四个字。一句 50 个字的话在屏幕上只剩「打不开：取流…」这样的碎片——
	// 那不是「一句能看懂的话」。所以**短的那句上屏，长的那句进悬停提示**
	// （`State.ErrDetail`）。
	a.setErrTextDetail("取流超时了",
		fmt.Sprintf("取流超时了（%v 内这一段网络没回）。可能是网络慢，也可能是这一首的流取不到——换一首放，或者点重试再试一次。",
			onlinePlayTimeout), true)
	if wake != nil {
		wake()
	}
}

// runOnline 是后台那条线：建一套播放链路，然后（代次还对得上的话）换上。
//
// at / paused 只有「恢复上次听到哪儿」那一趟会带（见 playback_state.go）：
// at 是要停到的位置，paused 是「开了之后别出声」。
func (a *App) runOnline(it Item, gen int, at time.Duration, paused bool) {
	s := a.streamingSnapshot()
	if s == nil {
		a.failPlay(gen, it, "取流零件没挂", errStreamingMissing)
		return
	}

	// 🔴 缺 cid 的条目（搜索结果全都是）先问一次该放哪一集，再去取流。
	//
	// 为什么在这一层问、而不是在搜索那一层把每一条结果都补上：用户多半只会听其中
	// 一首，为另外 23 首各发一次带签名的往返不划算（上游 Biu 是进队列时问的，见
	// `store/play-list.ts:129` 的 `getMVData`）。为什么不在 `playableOnline` 里问：
	// 那是在界面线程上调的，一次网络往返会冻住窗口。
	//
	// 🔴 **问不到就报错，不硬发**。上游把 cid 列为必填（`player-playurl.ts:9`），
	// 而「B 站能不能接受一个没有 cid 的请求」没有任何人能验——本地 fixture 放不
	// 放假参数，所以拿它当「反正会按第一集给」的依据是不成立的。发一个缺 cid 的
	// 请求，换回来的是一段不知道是哪一集的音频：能出声，但可能不是用户点的那个。
	// 说清「问不出这一集是哪一集」，比放错一集好。
	//
	// 音乐库条目走的是另一半（补元信息）：它没有 cid 要补，见 `withSource`。
	it, err := a.withSource(context.Background(), s, it)
	if err != nil {
		a.failPlay(gen, it, "问哪一集/补元信息", err)
		return
	}
	// 🔴 **代次变了就别再往下走**：问分 P / 元信息是**一次网络往返**，这一趟
	// 等它的时候用户完全可能已经切了歌、或者超时闹钟已经响了。那就没人要这
	// 个结果了——继续往下走只会白花一次取流（票 #282）。
	if a.genStale(gen) {
		a.failPlay(gen, it, "问哪一集/补元信息", errStaleGen)
		return
	}
	// 🔴 **这一段过了，闹钟重新起算**：取流这一趟是三段串行的网络往返，
	// 判据是「**这一段**最多等 20 秒」，不是「从点下去起 20 秒」——
	// 理由见 `onlinePlayTimeout` 那段注释。
	a.rearmLoading(gen, it.Title)

	// 🔴 **取流和开播放器是分开报的**：两者坏法不一样，用户要做的下一件事也
	// 不一样（取流失败是网络/地址，开不了播放器是这一条的格式/这台机器解不了）。
	// 合成一句「取流失败」的话，用户分不出该换网还是该换歌。
	ref, err := a.resolve(context.Background(), s, it, s.Quality)
	if err != nil {
		a.failPlay(gen, it, "取流", err)
		return
	}
	// 🔴 **代次变了就别再往下走**（和上面那一次同一个道理）：取流也是**一次网络
	// 往返**，等它的时候用户完全可能已经切了歌、或者**应用已经退出**了。
	//
	// ⚠️ **这一处不是可有可无的**：下面 `buildFromRef` 建字节源是要**在盘上落一
	// 个临时文件**的，而代次变了之后没人要这一趟——那个文件于是落在一个已经不
	// 会有人收拾的目录里（应用退出之后落，测试里就是落在已经被删掉的临时目录
	// 里，于是那条用例的收尾报「directory not empty」）。
	if a.genStale(gen) {
		a.failPlay(gen, it, "取流", errStaleGen)
		return
	}
	// 🔴 **再起算一次**：从这儿往后等的是 CDN 的头几个字节（字节源自己的
	// 卡死判定是 20 秒）。
	a.rearmLoading(gen, it.Title)
	src, p, err := a.buildFromRef(context.Background(), s, it, gen, ref)
	if err != nil {
		a.failPlay(gen, it, "开播放器", err)
		return
	}

	a.mu.Lock()
	if a.onlineGen != gen || a.closed {
		// 代次已经变了（切歌了），或者应用正在退出。手上这些一样都不留。
		cur, closed := a.onlineGen, a.closed
		a.mu.Unlock()
		p.Close()
		// 🔴 **这里以前是一声不吭地走人**（票 #282）：症状是「点了歌，什么都
		// 没有，连一句为什么都没有」——用户完全看不出是自己按错了还是应用坏了。
		// 现在日志里有一句说清是「代次变了，在途的这次作废了」。
		if closed {
			a.logf("biu: 在途的播放被作废了（应用退出了）：%q（代次 %d）", it.Title, gen)
		} else {
			a.logf("biu: 在途的播放被作废了（代次变了）：%q（来的代次 %d，现在的代次 %d）",
				it.Title, gen, cur)
		}
		return
	}
	old := a.player
	a.player = p
	a.title = it.Title
	a.path = ""
	a.cover = it.Cover
	a.artist = it.Artist
	a.artistMid = it.ArtistMid
	a.duration = p.Duration()
	// 位置：正常起播是 0；恢复那一趟是 at。
	//
	// ⚠️ **夹进 [0, 时长]，但时长拿不到的时候不夹**：时长是 0 的时候夹下去就把
	// 位置清成 0 了——恢复出来的那一首会停在开头，而用户明明停在中间。
	// （时长拿不到不算致命，见 `internal/service/audio/extaf` 的 Len()：
	// 拿不到总帧数时它返回 -1，只是进度条没有终点。）
	//
	// ⚠️ **和 `restreamOnline` / `refreshOnlineNow` 同一个规矩**：那两处也是
	// `a.position = pos` 不夹，只在 `pos < p.Duration()` 时才 Seek。
	pos := time.Duration(0)
	if paused {
		pos = at
		if pos < 0 {
			pos = 0
		}
		if d := a.duration; d > 0 && pos > d {
			pos = d
		}
	}
	a.position = pos
	if paused {
		// 恢复：**停在那个位置，不出声**。
		//
		// ⚠️ **`wantPlay` 记成 false**：它说的是「用户按过播放」，而恢复出来
		// 的时候用户还没按过——这一格要和一个普通的暂停**一模一样**。
		// 用户按了播放之后 `Toggle` 会把它记回 true（见 app.go 的 Toggle）。
		a.wantPlay, a.playing = false, false
	} else {
		a.wantPlay, a.playing = true, true
	}
	a.stopped = false
	a.quality = ref.Label
	a.online = &onlineTrack{item: it, ref: ref, source: src, quality: ref.Label}
	// 源归播放器管了（`audio.Player` 的 closers 会关它），App 这一栏清空。
	a.pendingSrc = nil
	a.reportedErr = nil
	a.clearErrLocked()
	// 「正在取流」到此为止：播放器装上了，歌开始放了。
	a.endLoadingLocked()
	// ⚠️ **`pending` 不在这里清**：它是「当前这一首」，而中途断流之后
	// 用户要的就是**重放这一首**（`App.Retry` 靠它）。在这里清掉的话，
	// 「播放中途断网」那一类的「重试」键按下去只会得到一句
	// 「没有可以重试的那一首」——那枚键是死的。
	//
	// 换歌不会让它过期：`playOnline` 每放一首都会把它换成新的那首。
	//
	// 🔴 **恢复那一趟到此为止**：它已经放起来了（或者放起来了又坏了），之后
	// 的错就该照常报给用户了——「安静」只罩住「启动时把上次那一首恢复出来」
	// 这一趟，见 `failOnline` 里认这一代次的那一处。
	if paused {
		a.restoreGen = 0
	}
	wake := a.wake
	a.mu.Unlock()

	// 换了一首 → 落盘那一格该跟着变（见 playback_state.go）。
	a.markPlaybackDirty()

	a.logf("biu: 这一首放起来了：%q（代次 %d，%s）", it.Title, gen, ref.Label)

	// 念出去（无障碍）：**「加载完成」**——在线流要跑一趟网络（取流 + 开解码器），
	// 那一趟里界面上一句话都没有，而读屏软件用户既看不见播放条上转的那枚圈，
	// 也听不见「歌放起来了」。
	//
	// ⚠️ **这一句是「发生了什么」，不是「这一枚是什么」**：它说的是刚刚过去的一件
	// 事（这一首备好了），不是任何一个控件的名字。
	//
	// ⚠️ **恢复那一趟说的是另一句话**：「正在放」对一首**没有在放**的歌是一句
	// 谎话，而读屏软件会照着念给用户听。
	if paused {
		a.announce("已恢复上次的位置：《" + it.Title + "》")
	} else {
		a.announce("加载完成，正在放《" + it.Title + "》")
	}

	// 先把新歌换上，再关旧的：换歌期间 tick 拿到的一直是有效的播放器。
	if old != nil {
		old.Close()
	}
	if paused {
		// 停在那个位置，**不出声**。
		//
		// ⚠️ **位置已经夹过了**（时长拿得到的时候夹进 [0, 时长]），所以这里
		// 直接 seek 就好——MyGo 的 Seek 自己也会夹（见 mygo 的 audio.go）。
		if pos > 0 {
			p.Seek(pos)
		}
		p.Pause()
	} else {
		p.Play()
	}
	a.publishMedia()
	// 歌词跟着换歌一起换：先重置成默认值，再从缓存恢复（见 lyrics.go）。
	// 放在换上播放器**之后**——歌词区问的是「现在放的是哪一首」，那一刻
	// 播放器已经换成新的了。
	a.setLyricTrack(it)
	if wake != nil {
		wake()
	}
}

// withSource 按这一项的类型，补上取流要 / 界面要的东西。**只在这一层问一次**。
//
//	mv     缺 cid 就问一次分 P（`withCid`）
//	audio  问一次元信息，把空的作者名补上（`withSongInfo`）
//
// 放在这里而不是 `playableOnline` 里的理由和补 cid 那一条一样：那是在界面线程上
// 调的，一次网络往返会冻住窗口。也不放在列表那一层：用户多半只会听其中一首。
func (a *App) withSource(ctx context.Context, s *Streaming, it Item) (Item, error) {
	if it.Type == "audio" {
		return a.withSongInfo(ctx, s, it)
	}
	return a.withCid(ctx, s, it)
}

// withSongInfo 问一次音乐库的元信息，把这一项缺的东西补上（作者名为主）。
//
// 照上游 Biu 的做法（`store/play-list.ts:157` 的 `getAudioData`）：放一个
// sid 条目时先问一次 `song/info`，拿 title / cover / duration / author。
//
// 🔴 **问不到不拦播放**：取流只要 sid，元信息是**锦上添花**。把它变成硬依赖的
// 后果是「网络抖一下就放不出歌」，而那时候歌其实取得到。问不到就少一个作者名，
// 播放条上那一句空着——空着是不好看，装错了是骗人。
//
// 只补**空着**的字段：收藏夹列表已经给了标题和封面，不该被一次网络往返改掉
// 用户眼前的东西。
func (a *App) withSongInfo(ctx context.Context, s *Streaming, it Item) (Item, error) {
	// ⚠️ **s 可能是 nil**，理由同 `withCid` 那一条：没挂取流时
	// `streamingSnapshot()` 给的就是 nil。从播放进来时它一定非 nil，
	// 但这一层不该靠「现在的调用方恰好都判过了」活着。
	if s == nil || s.Client == nil {
		return it, errStreamingMissing
	}
	info, err := s.Client.SongInfo(ctx, it.Sid)
	if err != nil {
		// 吞掉，但留一行字：排障时「作者名怎么是空的」只有一个解释。
		log.Printf("biu: 音乐库元信息问不到（不挡播放）：%v", err)
		return it, nil
	}
	if it.Title == "" {
		it.Title = info.Title
	}
	if it.Cover == "" {
		it.Cover = info.Cover
	}
	// 作者名优先用 `Author`：Biu 那边占 `ownerName` 的也是它（一个 UP 主的
	// 昵称和这首歌的作者多半不是同一个人）。
	if it.Artist == "" {
		it.Artist = info.Author
		if it.Artist == "" {
			it.Artist = info.Uname
		}
	}
	return it, nil
}

// withCid 是「缺分 P id 就去问一次」的那一步，**只在这一层问一次**。
//
// 已经有 cid 的原样返回（收藏夹、稍后再看、UP 空间来的条目接口都给 cid，问它们是
// 白跑一趟）。问不到就**把错原样交出去**，不去编一个 cid——编出来的那个取回来的是
// 别人的集（见 `service/bili/video.go`）。
//
// 返回的错会被 `failOnline` 报到播放条上，所以那句话要说清「是问不出这一集」，而不是
// 「取不到流」：用户在播放条上看到的得是他能照着做的下一步（换个词重搜 / 等网络好）。
//
// ⚠️ **`s` 可能是 nil**：没挂取流时 `streamingSnapshot()` 给的就是 nil，而这一
// 层原本只判了 `s.Client == nil`。从播放那一条路进来时 `s` 一定非 nil
// （`playOnline` 先判了 `StreamingMounted`），所以那个空判断一直没被走到；
// 从下载那一路进来就直接崩在「读一个 nil 指针的字段」上，而症状是**整个批量
// 下载一按就闪退**，不是一个能说给用户听的错。
func (a *App) withCid(ctx context.Context, s *Streaming, it Item) (Item, error) {
	if it.Cid != "" {
		return it, nil
	}
	if s == nil || s.Client == nil {
		return it, errStreamingMissing
	}
	cid, err := s.Client.FirstVideoCid(ctx, it.Bvid)
	if err != nil {
		// 包上 `ErrNoPart`：界面上要说清「问不出这一集」，而不是把它底下那个
		// 网络错原样报出去（`bili.Friendly` 靠这个哨兵把两句话说开）。
		return it, fmt.Errorf("%w（%s）：%w", bili.ErrNoPart, it.Bvid, err)
	}
	if cid == "" {
		return it, fmt.Errorf("%w：%s 的分 P 列表里没有可用的分 P id", bili.ErrNoPart, it.Bvid)
	}
	it.Cid = cid
	return it, nil
}

// buildOnline 建一套「流引用 + 字节源 + 播放器」，**不往 App 上装**。
//
// 开新歌和「从播放位置重开」走的是同一条路，所以它们共用这一个函数——
// 两份实现迟早会漂移，而漂移的那一半正好是「换流后从原位置继续」这条规格。
//
// 音量和倍率是**建好之后现读**的，不跟着调用方一路传下来：建一条在线流要走网络
// （取流、等首字节），这中间用户完全可能按了音量或者换了倍速。建完才读，读到的
// 才是当下那个值——传下来的那个反而是几十秒前的。
func (a *App) buildOnline(ctx context.Context, s *Streaming, it Item, gen int) (
	*audio.StreamSource, *audio.Player, *bili.StreamRef, error,
) {
	ref, err := a.resolve(ctx, s, it, s.Quality)
	if err != nil {
		return nil, nil, nil, err
	}
	src, p, err := a.buildFromRef(ctx, s, it, gen, ref)
	if err != nil {
		return nil, nil, nil, err
	}
	return src, p, ref, nil
}

// buildFromRef 拿一条**已经取到的**流建播放链路，不往 App 上装。
//
// 为什么要从 buildOnline 里拆出来：换音质的时候要先看一眼「新取回来的还是
// 不是同一档」，是同一档就不该打断正在放的歌——那一下判断必须在建播放器
// 之前，而建播放链路本身要开网络连接（`audio.OpenStream` 会去 CDN 探容器）。
//
// 音量和倍率在这里**现读**（`buildOnline` 那段注释说了为什么）。三条建播放
// 链路的路——开新歌、换流重开、换音质——都走这里，所以三处一处都不会漏。
func (a *App) buildFromRef(ctx context.Context, s *Streaming, it Item, gen int,
	ref *bili.StreamRef,
) (*audio.StreamSource, *audio.Player, error) {
	src, err := audio.NewStreamSource(audio.StreamSourceOptions{
		URLs:      ref.URLs,
		ExpiresAt: ref.ExpiresAt,
		TempDir:   s.TempDir,
		Client:    s.Media,
		// 续传的偏移在源那边，它要接就从同一个字节接着下。
		// 这里重新取的是**整条**流——偏移只是告诉调用方「你从哪里断的」，
		// 而取流接口没有「从第几个字节开始」这个参数。
		Refetch:   func(ctx context.Context, _ int64) *audio.StreamReply { return a.refetch(ctx, s, it) },
		OnRefresh: func(r *audio.StreamReply) { a.noteQuality(gen, r.Quality) },
	})
	if err != nil {
		return nil, nil, err
	}
	// 🔴 **把源记在 App 上**：建播放器的这一段里源已经在下载了，而播放器还没
	// 建起来——「超时」和「退出」都要能把它掐掉（见 App.pendingSrc）。
	a.setPendingSource(src, gen)
	// 🔴 **建源的这一小段时间里代次也可能变了**（用户切歌、或者超时闹钟响了）。
	// 那就没人要这条流了——立刻关掉，别让它下到天亮。
	if a.genStale(gen) {
		a.clearPendingSource(src)
		src.Close()
		return nil, nil, errStaleGen
	}
	p, err := audio.OpenStream(src)
	if err != nil {
		a.clearPendingSource(src)
		src.Close()
		return nil, nil, err
	}
	// 音量和倍率**现读**（理由见上面那段注释）：建一条在线流要走网络，
	// 这中间用户完全可能按了音量或者换了倍速。建完才读，读到的才是当下那个值。
	//
	// ⚠️ **音量读的是增益**（静音的时候是 0），而音量那一格记的还是用户设的值。
	a.mu.Lock()
	vol, rate := a.gainLocked(), a.rate
	a.mu.Unlock()
	p.SetVolume(vol).SetRate(rate)
	return src, p, nil
}

// errStaleGen 是「这一趟取流已经作废了」（代次变了：切歌了 / 超时了 / 退出了）。
//
// 它只走**内部**：报给用户没有意义（用户要的是「现在放的是哪一首」或者
// 「为什么放不了」，而这一句两个都不是）。它进日志——「在途的播放被作废了」
// 那一句说的就是它。
var errStaleGen = errors.New("这一趟取流已经作废了")

// genStale 报「这一趟还在不在当前这一趟里」。
//
// 🔴 **代次变了就不要再往下走**：切歌、超时、退出都会加代次，而那一趟的
// 结果已经没人要了。不早停的话，它会一路走到「建播放器」——甚至真的把流
// 下下来（票 #282：那一段是白花的流量和时间）。
func (a *App) genStale(gen int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.onlineGen != gen || a.closed
}

// setPendingSource 记下「正在建的那条字节源」，并记下它属于哪一趟。
//
// 🔴 **上一条还在的话，先把它关掉**：它属于**上一趟**（代次已经变了的那一趟），
// 没人要它了——不关掉的话它会一直下到天亮（票 #282）。
func (a *App) setPendingSource(src *audio.StreamSource, gen int) {
	a.mu.Lock()
	old := a.pendingSrc
	if old != nil && a.pendingSrcGen != gen {
		a.pendingSrc = nil
	} else {
		old = nil
	}
	a.pendingSrc = src
	a.pendingSrcGen = gen
	a.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

// clearPendingSource 把在途的那一条从 App 上摘掉（它已经归播放器管了，或者
// 已经被关掉了）。
func (a *App) clearPendingSource(src *audio.StreamSource) {
	a.mu.Lock()
	if a.pendingSrc == src {
		a.pendingSrc = nil
	}
	a.mu.Unlock()
}

// abortPendingSource 关掉在途的那条字节源（如果还有的话）。
//
// **锁外面关**：字节源的 `Close` 要等它的下载 goroutine 退出来，而那个
// goroutine 可能正拿着 `a.mu`（换流那条路上它会回调 `noteQuality`）。锁里面
// 等就是死等。
func (a *App) abortPendingSource() {
	a.mu.Lock()
	src := a.pendingSrc
	a.pendingSrc = nil
	a.mu.Unlock()
	if src != nil {
		src.Close()
	}
}

// streamFor 去 B 站要这一项的流。**两条取流路径在这一处分派**。
//
//	mv     bili.PlayURL（dash）
//	audio  bili.AudioStreamURL（音乐库，sid）
//
// 为什么要分派而不是在两个调用点各判一次类型：换音质那条路（`resolve`）和
// 换流那条路（`refetch`）都要取流，分开判的话早晚有一处忘了改，而那种错在
// 界面上是「换流之后突然放不出来了」。
func (a *App) streamFor(ctx context.Context, s *Streaming, it Item, pref bili.AudioQuality) (*bili.StreamRef, error) {
	if it.Type == "audio" {
		return s.Client.AudioStreamURL(ctx, it.Sid, pref)
	}
	return s.Client.PlayURL(ctx, it.Bvid, it.Cid, pref)
}

// sameItem 报 ref 是不是 it 这一项的流。
//
// 比 Bvid+Cid 或者 sid：这一条是「取回来的这条是不是用户点的那个」的最后一道
// 闸，而音乐库那一路 Bvid / Cid 都是空的——不认 sid 的话它恒真。
func sameItem(ref *bili.StreamRef, it Item) bool {
	if ref == nil {
		return false
	}
	if it.Type == "audio" {
		return ref.Sid != "" && ref.Sid == it.Sid
	}
	return ref.Bvid == it.Bvid && ref.Cid == it.Cid
}

// resolve 拿到一条能播的流。
//
// 这就是**播放前预判**那一条路径：手上有一条这个条目的流（上次放过、
// 切走又切回来）而且**还没到过期线**，就直接用，省掉一次带签名的往返；
// 过期了或者压根没有，就去问 B 站要一条新的。
func (a *App) resolve(ctx context.Context, s *Streaming, it Item, pref bili.AudioQuality) (*bili.StreamRef, error) {
	key := streamKey(it)

	a.mu.Lock()
	cached := a.streamCache[key]
	a.mu.Unlock()

	if cached != nil && cached.Usable(time.Now()) {
		return cached, nil
	}
	ref, err := a.streamFor(ctx, s, it, pref)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	// 缓存只留手上这一条。留着几十条过期地址没有用——过期是常态，
	// 而每一条都在诊断里等着被脱敏。
	a.streamCache = map[string]*bili.StreamRef{key: ref}
	a.mu.Unlock()
	return ref, nil
}

// refetch 是**报错后换流重试**那一条路径的回调：源下不动了，用同一首
// 重新取一条流，源会从它给的那个字节偏移接着下。
//
// 一定要比一次「还是不是同一首」：取流接口在稿件被删、被替换成别的内容时
// 会**静默**给一条看着完全正常的流。不比的话，用户会在毫无提示的情况下
// 听到另一首歌，而界面上什么都看不出来——那比断在这儿糟得多。
func (a *App) refetch(ctx context.Context, s *Streaming, it Item) *audio.StreamReply {
	a.mu.Lock()
	prev := a.currentRef()
	a.mu.Unlock()

	ref, err := a.streamFor(ctx, s, it, s.Quality)
	if err != nil {
		return nil
	}
	if prev != nil && !prev.SameTrack(ref) {
		return nil
	}
	if !sameItem(ref, it) {
		return nil
	}

	a.mu.Lock()
	// 和 resolve 一样只留一条：换流之后手上这条就是唯一有用的那条。
	a.streamCache = map[string]*bili.StreamRef{streamKey(it): ref}
	a.mu.Unlock()

	// ⚠️ 这里**不填 Size**：取流接口的返回里没有长度，而 `StreamReply.Size`
	// 的作用是「长度对不上就别硬接」。它在源那边是靠**两个响应各自报出来的
	// Content-Length** 相比出来的（见 `stream_source_pump.go` 的 expectTotal），
	// 所以这一处填 0 不会让那道防线失效——填一个猜的数才会。
	return &audio.StreamReply{
		URLs:      ref.URLs,
		ExpiresAt: ref.ExpiresAt,
		Quality:   ref.Label,
	}
}

// currentRef 返回当前那条在线流的引用。必须在持锁时调。
func (a *App) currentRef() *bili.StreamRef {
	if a.online == nil {
		return nil
	}
	return a.online.ref
}

// noteQuality 是换流成功之后的回调：把播放条上的音质徽章更新掉。
//
// 换流拿回来的可能是**降级过**的那条（会员过期、B 站改了可用档位）。
// 用户得看见——无声地降级会让人以为自己还在听无损。
func (a *App) noteQuality(gen int, label string) {
	a.mu.Lock()
	if a.onlineGen != gen || a.online == nil || label == "" {
		a.mu.Unlock()
		return
	}
	changed := a.quality != label
	a.quality = label
	a.online.quality = label
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
}

// checkPlayback 每 50 ms 看一眼：这一路播放是不是出了错。
//
// **为什么不能靠 `Playing() == false` 判断**：暂停、停止、关闭、播完、
// 解码失败在那边**全都是 false**（`mygo/audio/audio.go` 的 `Playing`）。
// 不做这一步的话，网络一断、解码一失败、容器坏了，界面上就只是「歌忽然
// 停了」，没有任何提示——而用户完全不知道该重试还是该换网。
// `spec.md`「播放状态机」一节说的「解码失败不能被当成播放结束」是它。
func (a *App) checkPlayback() {
	a.mu.Lock()
	p := a.player
	tr := a.online
	a.mu.Unlock()
	if p == nil {
		return
	}
	err := p.Err()
	if err == nil {
		return
	}

	// ⚠️ **在线流要看字节源自己的错，不要看解码器的**。解码器看到的是**后果**：
	// 字节断了之后它多半先报「这一帧的 CRC 对不上」，而真正的原因是网络断了
	// 或者地址失效了。把后果说给用户听，用户会以为歌坏了、缓存坏了，
	// 于是去清缓存重下——而正确���反应是「换个网」。
	if tr != nil && tr.source != nil {
		if srcErr := tr.source.Err(); srcErr != nil && !errors.Is(srcErr, io.EOF) {
			err = srcErr
		}
	}

	// 同一条错误只报一次。解码器的 Err 记的是**第一条**错误、不自己清零，
	// 不比对的话每 50 ms 都会重新冒一次。
	a.mu.Lock()
	if a.reportedErr == err {
		a.mu.Unlock()
		return
	}
	a.reportedErr = err
	a.playing = false
	gen := a.onlineGen
	wake := a.wake
	a.mu.Unlock()

	// 歌停了（解码失败、网络断、换流换不动）。系统那边的播放状态也得跟着
	// 变成「不在放」，否则控制中心上还是「播放中」——用户按暂停，
	// 系统说它已经在暂停了，那一下就没反应。
	a.publishMedia()
	if wake != nil {
		wake()
	}

	switch classifyOnlineError(err) {
	case onlineErrIgnore:
		// 切歌/退出带出来的，不是错。状态已经被 publishMedia 报成「不在放」了。
		a.logf("biu: 播放停了（切歌/退出带出来的，不算错）：%v", err)
	case onlineErrReopen:
		// 换到了另一条流：字节偏移接不上，但**播放位置**还在原处。
		// 从那儿重开就是「换流后从原来的位置继续」。
		a.logf("biu: 播放中途换了流，准备从原位置重开：%v", err)
		a.refreshOnlineNow()
	default:
		// 🔴 **「能不能重试」跟着这句话一起落**：字节源拉不动多半是网络或
		// 地址的问题（可重试），而「这一条压根没有音频流」重试也没用。
		//
		// 🔴 **日志里写清是哪一首、走的哪一段、错在哪**（票 #282）：播放路径
		// 以前一个字都不写，于是「点了不播」「播到一半停了」这两类事后完全
		// 查不了——用户能描述的只有「它不响」。
		var title string
		if tr != nil {
			title = tr.item.Title
		}
		a.logf("biu: 播放失败：%q（代次 %d）：%v", title, gen, err)
		a.setErrText(bili.Friendly(err), bili.Recoverable(err))
	}
}

// failOnline 报一次在线播放的失败。
//
// 代次对不上就一声不吭地走人：那已经是上一首歌的事了，在播放条上冒一句
// 红字只会让用户以为现在这首选不动了。**但日志里要留一句**（票 #282）——
// 不留的话，事后看日志只知道「什么都没发生」，而用户看到的是「点了没反应」。
//
// 🔴 **「能不能重试」跟着这句话一起落**（`bili.Recoverable` 判）：
// 网络断了、地址过期了——重试一次就有可能好，界面上给一枚
// 「重试」键；而「B 站说这一条没有音频流」重试一万次也没用，界面上
// **不给**那一枚键，只把「为什么」和「还能怎么办」说准。
//
// ⚠️ **风控和限流也是不给那一枚键的**（票 #316 / #346）：那枚「重试」是**立刻**
// 再取一次流，而这两样要等的是时间——给下去就是让用户点一次撞一次。
//
// 🔴 **日志里写清是哪一条、走的哪一段、错在哪**（票 #282）：应用日志以前
// 一个字都不写播放路径，于是「点了不播」这件事事后完全查不了。
//
// 🔴 **它不管「正在取流」那一档**——那一档是**某一次播放**的，而这一条
// 也会被换音质 / 换流重开那些**别的**路调用（它们拿的是当前代次）：
// 「别的歌的失败把这一首的看门狗取消掉」那个洞就是这么来的（票 #282 的
// 评审）。收那一档是 `failPlay` 的事。
func (a *App) failOnline(gen int, it Item, branch string, err error) {
	a.mu.Lock()
	stale := a.onlineGen != gen
	closed := a.closed
	cur := a.onlineGen
	restore := a.restoreGen == gen
	a.mu.Unlock()
	if stale || closed {
		if closed {
			a.logf("biu: 在途的播放被作废了（应用退出了）：%q（%s，代次 %d）：%v",
				it.Title, branch, gen, err)
		} else {
			a.logf("biu: 在途的播放被作废了（代次变了）：%q（%s，来的代次 %d，现在的代次 %d）：%v",
				it.Title, branch, gen, cur, err)
		}
		return
	}
	// 🔴 **恢复那一趟：安静地跳过**。
	//
	// 这一趟是「把上次听到哪儿恢复出来」（见 playback_state.go），它放不起来
	// 的时候**不许**在播放条上冒一句红字——启动时用户什么都没做就被骂一句
	// 「打不开」，那是应用在说自己，不是在说给用户听。
	//
	// ⚠️ **它只罩住这一代次**：用户之后点的任何一首歌都是另一个代次，不会
	// 被这一句「安静」顺带吞掉。
	if restore {
		a.logf("biu: 恢复上次的那一首放不起来（安静跳过）：%q（%s，代次 %d）：%v",
			it.Title, branch, gen, err)
		return
	}
	a.logf("biu: 播放失败：%q（%s，代次 %d）：%v", it.Title, branch, gen, err)
	// ⚠️ **`setErrText` 自己会叫醒界面**（它把新的话写进去之后才醒），所以这里
	// 不要再叫一次——两次唤醒只是多一帧，而顺序写错会让界面先画出「转圈停了」
	// 再画出那句话，中间那一帧是「既没有转圈也没有话」。
	a.setErrText(bili.Friendly(err), bili.Recoverable(err))
}

// failPlay 报一次「**这一趟播放**」的失败，并把「正在取流」那一档收掉。
//
// 🔴 **它和 `failOnline` 的区别就是收不收「正在取流」**：那一档是**这一趟
// 播放**的（`playOnline` 挂上去的），而 `failOnline` 也会被换音质 / 换流
// 重开那些**别的**路调用——它们要是顺手把它收掉，正在加载的那首歌就
// **永远等不到超时**了：用户点的那一首既不转圈也不报错，闹钟还被人替它
// 取消了（票 #282 的评审写的那条探针）。
//
// ⚠️ **只在「这一趟还是当前这一趟」时才收**：已经被取代的那一趟（用户切歌了、
// 超时了）不能去动**新的**那一趟的看门狗。
func (a *App) failPlay(gen int, it Item, branch string, err error) {
	a.mu.Lock()
	if a.onlineGen == gen && !a.closed {
		a.endLoadingLocked()
	}
	a.mu.Unlock()
	a.failOnline(gen, it, branch, err)
}

// refreshOnlineNow 从当前的播放位置重开一首。
//
// 这是**第三条路径**在应用层的入口：音频层把「换了另一条流，接不上原来的
// 字节」这件事报上来（`audio.ErrStreamChanged`），能不能从**播放位置**继续
// 就得在这一层做——播放位置是时间，只有 App 知道上一次读到哪儿了。
//
// 一首歌只重开一次（`onlineTrack.reopening`）。第二次还不行就该如实报错：
// 用户听得到「这首歌放不了了」，比程序一直自己折腾强。
func (a *App) refreshOnlineNow() {
	a.mu.Lock()
	tr := a.online
	if tr == nil || tr.reopening {
		a.mu.Unlock()
		return
	}
	tr.reopening = true
	gen := a.onlineGen
	pos, want := a.position, a.wantPlay
	a.mu.Unlock()

	s := a.streamingSnapshot()
	if s == nil {
		a.setErr(bili.Friendly(errStreamingMissing))
		return
	}

	go func() {
		src, p, ref, err := a.buildOnline(context.Background(), s, tr.item, gen)
		if err != nil {
			a.failOnline(gen, tr.item, "换流重开", err)
			return
		}

		a.mu.Lock()
		if a.onlineGen != gen || a.closed || a.online == nil {
			cur := a.onlineGen
			a.mu.Unlock()
			p.Close()
			a.logf("biu: 换流重开被作废了（代次变了）：%q（来的代次 %d，现在的代次 %d）",
				tr.item.Title, gen, cur)
			return
		}
		old := a.player
		a.player = p
		a.duration = p.Duration()
		a.position = pos
		a.quality = ref.Label
		a.online = &onlineTrack{item: tr.item, ref: ref, source: src, quality: ref.Label,
			reopening: true}
		// 源归播放器管了（见 App.pendingSrc）。
		a.pendingSrc = nil
		a.reportedErr = nil
		a.clearErrLocked()
		wake := a.wake
		a.mu.Unlock()

		if old != nil {
			old.Close()
		}
		// 跳回原来的位置。这一步是这一整条路径的全部意义：
		// 没有它，用户会在歌播到一半的时候突然从头听到。
		if pos > 0 && pos < p.Duration() {
			p.Seek(pos)
		}
		if want {
			p.Play()
		}
		a.publishMedia()
		if wake != nil {
			wake()
		}
	}()
}

// onlineErrorAction 把一条在线流的错误分成「怎么办」。
//
// 分成三类是因为它们要走的路完全不同，而且**只有三类都处理对了，
// 「换流后从原进度继续」才成立**：错了的那一类会让歌停在半路。
type onlineErrorAction int

const (
	// onlineErrIgnore 什么都不用做：只是切歌/退出带出来的，不是错。
	onlineErrIgnore onlineErrorAction = iota
	// onlineErrReport 报给用户。歌停了，且没法自己救。
	onlineErrReport
	// onlineErrReopen 从当前的播放位置重开一首。
	onlineErrReopen
)

func classifyOnlineError(err error) onlineErrorAction {
	switch {
	case err == nil, errors.Is(err, audio.ErrSourceClosed):
		return onlineErrIgnore
	case errors.Is(err, audio.ErrStreamChanged):
		// 换了另一条流：字节偏移对不上，但**播放位置**还在。
		// 从那儿重开就是「从原来的位置继续」。
		return onlineErrReopen
	}
	// 其余（网络断、地址失效、换流换不动）都在 audio 层自己换过一轮了，
	// 换不动就是换不动，如实说。
	return onlineErrReport
}
