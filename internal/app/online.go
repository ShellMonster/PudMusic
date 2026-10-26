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
	"sync/atomic"
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
// 取流这一趟是**几段**串行的网络往返：
//
//	问哪一集 / 补元信息	最多 10 秒（`bili.Client` 自己的超时）
//	取流接口		最多 10 秒（同上）
//	CDN 的头几个字节 + 解码	**没有固定上限**（见下）
//
// 一次性的闹钟会把**前两段**的时间算进去：接口慢 10 秒（到顶）+ CDN 的头
// 11 秒 = 21 秒，本来放得出来，却在一个 20 秒的闹钟上先响了。所以每一段
// 结束都**重新起算**——判据是「**这一段**最多等 20 秒」。
//
// # 🔴 **「下音频」和「解码」那两段不是「一共等多久」，是「还有没有进展」**
// （票 #220）
//
// 前两段等的是**一次往返**——20 秒不回就是死了。而后两段等的是**一条持续的
// 下载**：一条慢而**一直在走**的流，20 秒之后照样在好好地下，而把「慢」当成
// 「死」的结果就是**把一首好歌掐掉**——用户看到的是「取流超时了」，而歌
// 明明下得好好的。那正是用户说的「取流非常慢」。
//
// 所以后两段的判据是「这一段还有没有进展」（见 `loadTimedOut`），而 20 秒
// 是「**多久没进展**算死」——不是「一共等多久」。
//
// ⚠️ **投稿那一路和音乐库那一路用的是同一套口径**（票 #220）：
//
//	问地址 / 问分集	同一个 `bili.Client`，同一个 10 秒请求超时
//	下音频 / 解码	同一个字节源，同一个 20 秒卡死判定
//
// 两路唯一的差别是**问的接口不一样**（投稿走 `/x/player/wbi/playurl`、
// 音乐库走 `/audio/music-service-c/url`），而音乐库那一路在退档时会**多问
// 一次**——所以它的「问地址」那段可能比投稿那一段多一个往返。
const onlinePlayTimeout = 20 * time.Second

// onlineLoadTotalTimeout 是「这一趟取流」**不管有没有进展**最多等多久。
//
// ⚠️ **它从「点下去」起算，不是从「当前这一段」起算**（`loadTripSince`）——
// 「这一趟最多等多久」这句话要真的管住**这一趟**（票 #220 的评审指出来的：
// 量的是「这一段」的话，最坏值是 120+20+20 ≈ 170 秒，而那句话说的是
// 「这一趟」）。
//
// ⚠️ **它管的是「永远转圈」那件事**：下载那一段的判据是「还有没有进展」
// （见 `loadTimedOut`），而一个每 19 秒吐一个字节的地址能让那个判据一直
// 为真——于是转圈就永远不停。这一栏是那道**总闸**。
//
// 取 120 秒：一首 10 MB 的歌在 100 KB/s 的网络上要 100 秒，而 120 秒之后
// 再等下去用户早就换歌了。
const onlineLoadTotalTimeout = 120 * time.Second

// 取流这一趟的**分段**。界面上「慢」的时候要说清「卡在哪一段」。
//
// ⚠️ **这些词是给人看的，所以要短**：播放条上那一行只有 140 像素宽
// （1200 宽的窗口，实测），12 号字放得下十来个字，而前面还顶着别的字。
//
// ⚠️ **它们是「这一段在等什么」，不是「这一段叫什么」**：「问地址」说的是
// 「在等 B 站给地址」，用户能对上自己遇到的事（网络慢、B 站慢）；
// 而「playurl」这种词他读不出来。
const (
	// loadStagePart 是「问哪一集」那一次往返（投稿那一路）。
	//
	// ⚠️ **它不是取流接口那一次**：投稿那一路要先问一次分 P（搜索结果
	// 全都没有 cid），音乐库那一路要先问一次元信息。两次都是网络往返，
	// 都可能慢。
	loadStagePart = "问分集"
	// loadStageInfo 是「补元信息」那一次往返（音乐库那一路）。
	//
	// ⚠️ **它和 `loadStagePart` 是两件事**：投稿那一路问的是「放哪一集」，
	// 音乐库那一路问的是「作者名 / 封面」——**问不出来也不挡播放**
	//（见 `withSongInfo`），而问分集问不出来是要报错的。
	loadStageInfo = "问资料"
	// loadStageURL 是「问 playurl 拿地址」那一次往返（票 #220 的第①段）。
	loadStageURL = "问地址"
	// loadStageBytes 是「从 CDN 取字节 + 解码第一帧」（票 #220 的第②③段）。
	//
	// ⚠️ **②和③合成一个词，是因为它们在慢的时候是同一件事**：③（解码第一帧）
	// 要等解码器把流开起来，而**MP4 的 `moov` 在文件末尾时，解码器要等整个
	// 文件下完**（实测见 `internal/service/audio/extaf` 那边的注释；本仓库
	// 自己的、按 B 站形状造的素材 `mfadec/testdata/aaclc-192k.m4a` 就是
	// `moov` 在末尾）。那时候界面上如果写「解码」，用户会以为**这台机器
	// 解得慢**——而真相是「还在下」。
	loadStageBytes = "下音频"
)

// loadStageAsk 是「开头那一次问」的阶段名，**按这一项的类型**。
//
// 投稿那一路问的是「放哪一集」（缺 cid 要补），音乐库那一路问的是
// 「作者名 / 封面」（`withSongInfo`）——**两件事不一样，说的词也不一样**。
func loadStageAsk(it Item) string {
	if it.Type == "audio" {
		return loadStageInfo
	}
	return loadStagePart
}

// loadRun 是「**开新歌**」那一趟的凭据：分段计时 + 「这一趟自己的那条源」。
//
// # 为什么它是一个东西
//
// 「这一趟」有两样东西要归它所有：
//
//	分段耗时	写进日志（事后查得到，票 #282）
//	那一条源	看门狗问「这一段还有没有进展」时要看的是**它**
//
// 而第二样**必须**是这一趟自己的：App 上那个共享的 `pendingSrc` 会被换音质
// / 换流重开那两条路顶掉（理由见 App 里 `loadSrc` 那段注释）——而看门狗问的
// 是「**这一趟**还在下吗」。
//
// ⚠️ **只有「开新歌」那一路有它**：换音质 / 换流重开传 nil——它们量的是
// 「换流花了多久」，不是「从点歌到出声花了多久」，而它们的源也不是用户
// 此刻在等的那一条。
type loadRun struct {
	// timing 是这一趟的分段耗时。
	timing loadTiming
}

// loadTiming 是「这一趟取流」的分段耗时（票 #220）。
//
// # 为什么要有它
//
// 「点一首歌到出声」是**三段串行的网络往返**，而它们的快慢完全不是一回事：
//
//	① 问 playurl 拿地址	一次带 WBI 签名的接口往返
//	② 下载首段字节		CDN 那边给头几个字节
//	③ 解码第一帧		这一台机器的解码器开这条流
//
// 混在一起只有一个「总的等了多久」，而那个数**说不出慢的是哪一段**——
// 而三段的 remedies 完全不同：① 是 B 站接口慢，② 是网络慢，③ 是这台机器
// 解得慢（换一档音质就有用）。
//
// ⚠️ **零值表示「没量到」**：某一段压根没发生（比如地址没取到，那一段的
// 耗时是 0 而不是「瞬间」）。
type loadTiming struct {
	// Part 是开头那一次「问哪一集 / 补元信息」。
	Part time.Duration
	// URL 是①：问 playurl 拿地址。
	URL time.Duration
	// Bytes 是②：从建源到头几个字节落到缓存里。
	//
	// ⚠️ **字节一个都没到时它是「等到失败为止的那段时间」**，而 Decode
	// 是 0——那时候根本没有「解码」这一段可量。
	Bytes time.Duration
	// Decode 是③：从头几个字节到解码器开好（能出声了）。
	Decode time.Duration
}

// Total 是这一趟一共等了多久。
func (t loadTiming) Total() time.Duration {
	return t.Part + t.URL + t.Bytes + t.Decode
}

// String 把这一趟的耗时排成一句能进日志的话。
//
// ⚠️ **它按「人读得懂的顺序」排**，不是按结构体的字段顺序——日志是给
// 人看的（票 #282 的「事后查得到」）。
func (t loadTiming) String() string {
	return fmt.Sprintf("① 问地址 %v ② 下音频 %v ③ 解码 %v（另：问分集 %v，合计 %v）",
		t.URL, t.Bytes, t.Decode, t.Part, t.Total())
}

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
	// Quality 是默认音质偏好。零值是 `bili.AudioHigh`（最高有损，见
	// `bili.AudioQuality` 的注释——零值必须是一档能立刻出声的有损）。
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
	// gen 是**装上这条流的那一趟**的代次（`App.onlineGen`）。
	//
	// 🔴 **它是「这一条流的状态归不归当前这一趟」的判据**（票 #378）：
	// 切歌之后 `a.online` 还是**旧那一首**（新播放器要等取流完了才装上，
	// 那之前旧的一直在放），而旧那条流**可能正在重连**——那时候界面上
	// 「正在重连」亮着的是**旧那一首**的状态，它把这一趟该说的
	// 「正在取流：新的一首」盖掉了。
	//
	// 有了这一栏：那一条源的状态只在 `gen` 还是当前代次的时候才算数
	// （见 `reconnect.go` 的 `reconnectingLocked`）。
	gen int
}

// streamKey 是取流缓存的键。
//
// 用 Biu 的判重标识（`source/type/bvid/cid/sid`）而不是队列下标：换了歌
// 回来时同一条流还能复用，省一次带签名的往返。
func streamKey(it Item) string {
	return it.Source + "|" + it.Type + "|" + it.Bvid + "|" + it.Cid + "|" + it.Sid
}

// MountStreaming 挂上在线播放。**在开窗口之前调**（它自己不读盘，但让
// 第一次点歌不要替「开窗」买单）。
func (a *App) MountStreaming(s Streaming) {
	a.mu.Lock()
	a.streaming = &s
	a.streamCache = map[string]*bili.StreamRef{}
	a.mu.Unlock()
	// 取流零件整个换掉了 → 备好的那一份地址也作废（它是在旧零件上备的）。
	a.invalidatePrefetch()
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
// 换好了，那正是这个控件存在的意义。设置页换档走的也是这里（见
// `UpdateSettings`），不然那一格就是只有重启才生效的摆设。
//
// 三件事一起做：
//
//	偏好落到设置里（下次启动还在）
//	缓存清掉（否则下一首还会拿旧地址）
//	正在放的那首重新取流（这是「立刻」两个字的意思）
//
// 真取流失败（网络断了、付费曲每一档都要会员）时**不打断正在放的歌**：
// 把原因报到播放条上，原来那条流继续放着。点名的档拿不到不算失败——
// 那种情况取流那层自己退到最高有损（见 `bili.pickAudio`），歌照常换过去。
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

	// 🔴 **备好的那一份也作废**：它是在**旧的那一档**上备的，而用户刚换了一档。
	// 留着它的话，下一首会按旧的那一档放——而界面上写的是用户选的那一档。
	a.invalidatePrefetch()

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
// 先判「其实没变」：拿回来的和手上这条是同一档（比如点「无损」而这一首没有，
// 退档退回来的还是 192K），那就什么都别做——为一个听不出区别的改动
// 打断一首歌，比点了没反应更糟。
//
// 换流之后的处理和 `refreshOnlineNow` 一样：跳回原来的**播放位置**，那一步
// 是「换流后从原进度继续」的全部意义。
func (a *App) restreamOnline(tr *onlineTrack, q bili.AudioQuality, pos time.Duration, want bool, gen int) {
	s := a.streamingSnapshot()
	if s == nil {
		a.failOnline(gen, tr.item, "换音质（取流零件没挂）", errStreamingMissing, "")
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
		src, p, err := a.buildFromRef(context.Background(), s, tr.item, gen, ref, nil)
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
			// 🔴 **代次记在这一条流上**（票 #378）：它是「这一条流的状态归不归
			// 当前这一趟」的判据（见 `reconnect.go`）。
			gen: gen,
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
	a.failOnline(gen, it, "换音质", err, "")
}

// AudioQuality 返回当前偏好。
func (a *App) AudioQuality() bili.AudioQuality {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streaming == nil {
		return bili.AudioHigh
	}
	return a.streaming.Quality
}

// PrefToBili 把设置里那一档翻译成取流用的那一档。
//
// 设置里存的是字符串（落盘给人看、跨版本稳），取流那边要的是 B 站的编号。
// 认不出来的值一律当「高品质」——那是一档**能立刻出声**的有损（见
// `bili.AudioQuality` 的注释），不要因为设置文件里多了一个字符就整条在线
// 播放放不出来。
func PrefToBili(q AudioQuality) bili.AudioQuality {
	switch q {
	case AudioQualityLossless:
		return bili.AudioLossless
	case AudioQualityDolby:
		return bili.AudioDolby
	case AudioQualityHigh:
		return bili.AudioHigh
	case AudioQualityMedium:
		return bili.AudioMedium
	case AudioQualityLow:
		return bili.AudioLow
	}
	return bili.AudioHigh
}

// BiliToPref 是 `PrefToBili` 的反方向：取流那边那一档在设置里写成什么。
// 界面拿它把「当前偏好」和菜单里的项对上。
func BiliToPref(q bili.AudioQuality) AudioQuality {
	switch q {
	case bili.AudioLossless:
		return AudioQualityLossless
	case bili.AudioDolby:
		return AudioQualityDolby
	case bili.AudioHigh:
		return AudioQualityHigh
	case bili.AudioMedium:
		return AudioQualityMedium
	case bili.AudioLow:
		return AudioQualityLow
	}
	return AudioQualityHigh
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
	// 🔴 **「正在重连」也收掉**（票 #315）：换歌 = 上一首的那场重连到此为止。
	// 不收的话它会一直亮着，而新的一首压根没有在重连。
	a.clearReconnectLocked()
	// 🔴 **记下用户刚才按的是哪一首**：重试要放的是它，而不是队列里当前的
	// 那一首——取流失败时队列常常已经滚回去了（见 `App.Retry`）。
	a.pending = it
	// 🔴 **「正在取流」从这一刻开始**（票 #282）：取流要跑一趟网络，而那一趟
	// 里界面上什么都不会变——不说这一句，用户看到的就是「点了没反应」。
	// 超时由 `loadTimedOut` 兜着，不让它变成另一种「什么都不说」。
	// 🔴 **整趟从这一刻开始算**（票 #220）：总闸量的是「这一趟」，而
	// `loadingSince` 是「当前这一段」（换段就重置）——两栏不是一回事。
	a.loadTripSince = time.Now()
	a.beginLoadingLocked(gen, it.Title, loadStageAsk(it))
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

// beginLoadingLocked 记下「正在取流」，并把两个闹钟挂上。**必须在持锁时调**。
//
// 闹钟带的是**这一次**的代次和歌名：超时那一刻要判「还在不在这一趟里」，
// 而歌名是要写进那句话和日志里的（用户要知道是哪一首超时了）。
//
// stage 是「这一段在等什么」——界面靠它说出「卡在哪一步」（票 #220）。
func (a *App) beginLoadingLocked(gen int, title, stage string) {
	a.loading = true
	a.loadingTitle = title
	a.loadingStage = stage
	a.loadingSince = time.Now()
	// 🔴 **进度归零**：新的一段从「还没下到东西」开始。
	a.loadFilled = -1
	// 挂新闹钟之前先停掉上一个：连点两首时，前一个的闹钟不能替后一个喊。
	a.stopLoadTimersLocked()
	a.loadStop = a.after(onlinePlayTimeout, func() { a.loadTimedOut(gen, title, stage) })
}

// stopLoadTimersLocked 把「正在取流」这一摊的闹钟停掉。
// **必须在持锁时调**。
func (a *App) stopLoadTimersLocked() {
	if a.loadStop != nil {
		a.loadStop()
		a.loadStop = nil
	}
}

// rearmLoading 换一段：重新起算「这一段最多等多久」。**必须在持锁时调**。
//
// 🔴 **它不碰「正在取流」那一档**，只重挂闹钟：那一档从头到尾都是「这一首
// 正在准备」，而闹钟量的是「**这一段**网络还没回」。
//
// ⚠️ **代次不对就不动**：那一趟已经被取代了（切歌 / 超时），它的闹钟早就
// 被收掉了，别去动**新的**那一趟的。
func (a *App) rearmLoading(gen int, title, stage string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.onlineGen != gen || a.closed || !a.loading {
		return
	}
	a.beginLoadingLocked(gen, title, stage)
}

// endLoadingLocked 把「正在取流」这一档收掉，并把闹钟停掉。
// **必须在持锁时调**。
func (a *App) endLoadingLocked() {
	a.loading = false
	a.loadingTitle = ""
	a.loadingStage = ""
	a.loadFilled = -1
	// 🔴 **「启动那一段」到此为止**（票 #478）：第一次取流有结果了
	// （成了或者败了），应用不再是在「启动」，而是在「用」。
	a.starting = false
	// 🔴 **整趟的起点也收掉**：这一趟到这儿就完了，下一趟会重新起算
	// （见 `playOnlineFrom` 里那一处）。
	a.loadTripSince = time.Time{}
	a.stopLoadTimersLocked()
}

// tripWaitLocked 报「这一趟取流从点下去到现在的多久」。**必须在持锁时调**。
//
// ⚠️ **它和 `loadingSince` 是两件事**：那一栏是「当前这一段」（换段就
// 重置），这一栏是「整趟」（不重置）——总闸量的是整趟（票 #220 的评审）。
func (a *App) tripWaitLocked() time.Duration {
	if a.loadTripSince.IsZero() {
		return 0
	}
	return time.Since(a.loadTripSince)
}

// humanBytes 把字节数说成一句人话。
//
// ⚠️ **它是给用户看的**（失败那句话的详情里），所以用 KB / MB——
// 「65536 字节」用户读不出那是多少。
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d 字节", n)
	}
}

// loadTimedOut 是「点了歌之后一直没开始」的闹钟响了。
//
// 🔴 **它按失败处理**：超了阈值就当这一首放不起来，给用户一句能看懂的话，
// 而不是让界面上那枚转圈一直转（票 #282）。
//
// **作废在途的那一次**：代次加一，那条还在跑的取流回来时对不上代次，自己走人
// ——它要是回来得比这个闹钟晚，装上来的话用户会看到「说了失败、结果又放起来了」。
//
// # 🔴 **下载那一段是「进度」判据，不是「一共等了多久」**（票 #220）
//
// 「下音频」「解码」这两段等的是**一条持续的下载**，不是「一次往返」。
// 一条慢而**一直在走**的流，20 秒之后照样在好好地下——而改之前那个
// 20 秒的闹钟会把它掐掉，界面上一句「取流超时了」，歌再也放不出来。
// 那正是用户说的「取流非常慢」：网络慢不是错，而**把慢当成错**是错。
//
// 所以这两段的判据是「**这一段还有没有进展**」：
//
//	有进展	还在下 → **重新起算**，让这一段接着等
//	没进展	卡死了 → 按失败处理（转圈停 + 一句能看懂的话）
//
// ⚠️ **「有进展」是拿字节源已经下到的字节数比的**（`Filled()`）：它是
// 「网络还在动」的唯一可靠证据——请求没报错不算（连上了却一个字节都不下，
// 那种连接在用户听来和没恢复一样）。
//
// ⚠️ **另有一道总闸**（`onlineLoadTotalTimeout`）：一个每 19 秒吐一个字节的
// 地址能让「有进展」一直为真，于是转圈就永远不停——总闸管的是那件事。
func (a *App) loadTimedOut(gen int, title, stage string) {
	a.mu.Lock()
	if a.onlineGen != gen || a.closed || !a.loading {
		// 这一趟已经不是当前那一趟了（用户又点了一首，或者它已经成了）。
		a.mu.Unlock()
		return
	}
	// ⚠️ **总闸量的是「整趟」**（`tripWaitLocked`），不是「当前这一段」
	// （`loadingSince` 换段就重置）——「这一趟最多等多久」这句话要真的
	// 管住这一趟（票 #220 的评审指出来的：注释说「这一趟」，而代码量的是
	// 「这一段」，于是最坏值是 120+20+20 ≈ 170 秒）。
	trip := a.tripWaitLocked()
	// 🔴 **下载那两段：先问「还有没有进展」**。
	if stage == loadStageBytes {
		if a.downloadProgressLocked() && trip < onlineLoadTotalTimeout {
			// 还在动：这一段慢，但不是死了。
			//
			// 🔴 **重新起算的是这一段**：进度那一栏也要留着（「上次量到多少
			// 字节」是下一次比较的基准），而「这一段从什么时候开始」要**接着
			// 算**——它量的是「用户为这一段等了多久」。
			a.rearmLoadingLocked(gen, title, stage)
			wake := a.wake
			a.mu.Unlock()
			if wake != nil {
				wake()
			}
			return
		}
	}
	a.onlineGen++
	// 🔴 **活下来的那一首还是当前这一首**（票 #378）：这一趟取流失败了，
	// 但**上一首还在放**（播放器压根没动过）。所以它那一栏的代次要跟着
	// 加——不加的话，它之后断流时的「正在重连」会被当成「别的歌的事」
	// 藏起来（见 `reconnect.go` 的 `reconnectingLocked`）。
	if a.online != nil {
		a.online.gen = a.onlineGen
	}
	stage = a.loadingStage
	// ⚠️ **失败那一刻已经下到多少字节**：判据是「这一段没有进展」，而
	// 「没进展」**不等于**「一个字节都没到」——下到一半卡住时字节明明已经
	// 到了，而那句话里写「一个字节都没到」是**错的**（票 #220 的评审）。
	got := a.loadFilled
	a.endLoadingLocked()
	restore := a.restoreGen == gen
	wake := a.wake
	a.mu.Unlock()

	// 🔴 **把在途的下载掐掉**：光在界面上说「超时」不够——那条字节源还在
	// 后台下载（而播放器还没建起来，`App.Close` 也管不到它）。不掐掉的话，
	// 这一次「已经判它失败」的取流会一直下到天亮。
	a.abortPendingSource()

	a.logf("biu: 取流超时：%q（代次 %d，%v 内这一段还没回，段=%s）",
		title, gen, onlinePlayTimeout, stage)
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
	//
	// 🔴 **下载那两段的话和接口那两段不一样**（票 #220）：「这一段一直没回」
	// 和「一个字节都不下来」是**两件不同的事**，而用户能做的事也不同
	// （前者是等，后者是换网 / 换歌）。给同一句「取流超时了」就是把两件事
	// 说成一件。
	short, detail := "取流超时了", fmt.Sprintf(
		"取流超时了（%v 内这一段网络没回）。可能是网络慢，也可能是这一首的流取不到——换一首放，或者点重试再试一次。",
		onlinePlayTimeout)
	if stage == loadStageBytes {
		if trip >= onlineLoadTotalTimeout {
			short = "取流太慢了"
			detail = fmt.Sprintf(
				"这一首取流太慢了（整趟已经等了 %v，再等下去也来不及）。可能是网络特别慢，也可能是这一首特别大——换一首放，或者点重试再试一次。",
				trip.Round(time.Second))
		} else {
			short = "下载超时了"
			// 🔴 **理由要按实际说**（票 #220 的评审）：判据是「这一段没有
			// 进展」，而「没进展」**不等于**「一个字节都没到」——下到一半
			// 卡住时字节明明已经到了。写「一个字节都没到」是在骗用户。
			if got > 0 {
				detail = fmt.Sprintf(
					"这条音频下到一半停住了（已经下了 %s，但 %v 内没有新的字节了）。多半是网络断了，或者这条地址失效了——换个网络，或者点重试再试一次。",
					humanBytes(got), onlinePlayTimeout)
			} else {
				detail = fmt.Sprintf(
					"这条音频一个字节都没下下来（%v 内什么都没到）。多半是网络断了，或者这条地址失效了——换个网络，或者点重试再试一次。",
					onlinePlayTimeout)
			}
		}
	}
	a.setErrTextDetail(short, detail, true)
	if wake != nil {
		wake()
	}
}

// setLoadSource 记下「这一趟取流」自己的那条字节源（看门狗的那一份）。
//
// ⚠️ **它和 `setPendingSource` 是两件事**：`pendingSrc` 是「当前在管的那条」
// （谁都在写），而这一栏是「用户此刻在等的那一条」（只有「开新歌」那一趟
// 写它）。理由见 App 里 `loadSrc` 那段注释。
//
// ⚠️ **同时把进度的基准归零**：这一段从「一个字节都还没到」开始，而「没下到」
// 和「下到了一点」在 `Filled()` 上分别是 0 和 n——基准必须是 0，不然「一个
// 字节都不来」会被判成「还在走」。
func (a *App) setLoadSource(src *audio.StreamSource, gen int) {
	a.mu.Lock()
	old := a.loadSrc
	if old != nil && old != src {
		a.loadSrc = nil
	} else {
		old = nil
	}
	a.loadSrc = src
	a.loadSrcGen = gen
	a.loadFilled = 0
	a.mu.Unlock()
	if old != nil {
		old.Close()
	}
}

// dropLoadSource 把「这一趟自己的那一条」摘掉（它已经归播放器管了，或者
// 已经被关掉了）。
func (a *App) dropLoadSource(src *audio.StreamSource) {
	a.mu.Lock()
	if a.loadSrc == src {
		a.loadSrc = nil
	}
	a.mu.Unlock()
}

// downloadProgressLocked 报「下载那一段还有没有进展」。**必须在持锁时调**。
//
// 判据是**字节源已经下到的字节数变没变**（`Filled()`）：那是「网络还在动」
// 的唯一可靠证据——请求没报错不算（连上了却一个字节都不下，那种连接在用户
// 听来和没恢复一样）。
//
// ⚠️ **基准是「这一段开始时已经下到多少」**（`setPendingSource` 记的 0），
// 不是「上一次量到多少」：一个字节都不下的地址从 0 到 0 是**没有进展**，
// 而拿「-1（还没量过）」当基准的话，0 ≠ -1 会被判成「有进展」——那正好把
// 最坏的那种情况（**一个字节都不来**）判成「还在走」。
//
// ⚠️ **代次要对**：上一趟留下的那条源（切歌了还没建好新的）说的不是这一趟
// 的进度。
func (a *App) downloadProgressLocked() bool {
	// 🔴 **读的是「这一趟自己的那一条」**（`loadSrc`），**不是共享的那一栏**
	// （`pendingSrc`）：换音质 / 换流重开那两条路会把 `pendingSrc` 换成它们
	// 自己的源，于是读它会读到**另一条**流的进度——于是一条正在好好下的流
	// 被判成「没进展」，然后被掐掉（票 #220 的评审复现过）。
	src := a.loadSrc
	if src == nil || a.loadSrcGen != a.onlineGen {
		// 还没有字节源（还在问地址那几段）：这一段压根不适用进度判据。
		return false
	}
	n := src.Filled()
	if a.loadFilled < 0 {
		// 还没量过：记下基准，**不算进展**。
		a.loadFilled = n
		return false
	}
	if n == a.loadFilled {
		return false
	}
	a.loadFilled = n
	return true
}

// rearmLoadingLocked 换一段（**必须在持锁时调**）。
//
// ⚠️ **它和 `rearmLoading` 的区别是「保留这一段的进度基准」**：
//
//	进度基准	下一次比较要用「上一次下到多少」，重置成 -1 的话，
//		一个字节都不下的地址也会被判成「有进展」
//	「从什么时候开始等」	它量的是「用户为这一段等了多久」，
//		中途重新起算不该把它归零
func (a *App) rearmLoadingLocked(gen int, title, stage string) {
	if a.onlineGen != gen || a.closed || !a.loading {
		return
	}
	filled, since := a.loadFilled, a.loadingSince
	a.beginLoadingLocked(gen, title, stage)
	a.loadFilled = filled
	a.loadingSince = since
}

// runOnline 是后台那条线：建一套播放链路，然后（代次还对得上的话）换上。
//
// at / paused 只有「恢复上次听到哪儿」那一趟会带（见 playback_state.go）：
// at 是要停到的位置，paused 是「开了之后别出声」。
func (a *App) runOnline(it Item, gen int, at time.Duration, paused bool) {
	// 🔴 **冷启动那一段的第一取流时刻**（票 #478）：用户「双击 → 出声」
	// 这条链上，「取流开始」是一个节点——而它在**恢复上次那一首**时
	// 是在**开窗口之前**就发出去的（`RestorePlayback` 排在 `openWindow`
	// 前面），所以用户看到窗口的时候那一趟已经在路上了。
	a.NoteFetchStart()

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
	//
	// 🔴 **分段计时从这一刻开始**（票 #220）：「点一首歌 → 出声」是几段串行
	// 的网络往返，而它们的快慢完全不是一回事——混在一起只有一个「等了多久」，
	// 那个数说不出慢的是哪一段。
	t0 := time.Now()
	// 🔴 **分段耗时写进日志**（票 #220）：这三段是「点一首歌 → 出声」的全部
	// 成本，而事后能查到的只有这一句——用户能描述的只有「它不响」。
	//
	// ⚠️ **失败的那几趟也要写**（用 `defer` 而不是写在成功那一支后面）：
	// **用户会来抱怨的正是失败的那些**（「点了歌一直没反应」）——只在成功
	// 时写，等于把最需要查的那一类漏掉（票 #220 的评审指出来的）。
	run := &loadRun{}
	defer func() {
		if run.timing.Total() == 0 {
			// 连第一段都没走完（取流零件没挂）：没什么可量的。
			return
		}
		a.logf("biu: 取流分段耗时：%q（代次 %d）%s", it.Title, gen, run.timing.String())
	}()
	it, err := a.withSource(context.Background(), s, it)
	if err != nil {
		a.failPlay(gen, it, "问哪一集/补元信息", err)
		return
	}
	run.timing.Part = time.Since(t0)
	// 🔴 **代次变了就别再往下走**：问分 P / 元信息是**一次网络往返**，这一趟
	// 等它的时候用户完全可能已经切了歌、或者超时闹钟已经响了。那就没人要这
	// 个结果了——继续往下走只会白花一次取流（票 #282）。
	if a.genStale(gen) {
		a.failPlay(gen, it, "问哪一集/补元信息", errStaleGen)
		return
	}
	// 🔴 **这一段过了，闹钟重新起算**：取流这一趟是几段串行的网络往返，
	// 判据是「**这一段**最多等 20 秒」，不是「从点下去起 20 秒」——
	// 理由见 `onlinePlayTimeout` 那段注释。
	a.rearmLoading(gen, it.Title, loadStageURL)

	// 🔴 **取流和开播放器是分开报的**：两者坏法不一样，用户要做的下一件事也
	// 不一样（取流失败是网络/地址，开不了播放器是这一条的格式/这台机器解不了）。
	// 合成一句「取流失败」的话，用户分不出该换网还是该换歌。
	//
	// 🔴 **① 问 playurl 拿地址**（票 #220 的第一段）：这一段是一次带 WBI
	// 签名的接口往返，而它是**两条取流路径（投稿 / 音乐库）唯一的共同段**。
	tURL := time.Now()
	ref, err := a.resolve(context.Background(), s, it, s.Quality)
	run.timing.URL = time.Since(tURL)
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
	//
	// ⚠️ **这一段的判据是「还有没有进展」**，不是「一共等了多久」——
	// 理由见 `loadTimedOut` 那段注释（票 #220）。
	a.rearmLoading(gen, it.Title, loadStageBytes)
	src, p, err := a.buildFromRef(context.Background(), s, it, gen, ref, run)
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
	a.online = &onlineTrack{item: it, ref: ref, source: src, quality: ref.Label, gen: gen}
	// 源归播放器管了（`audio.Player` 的 closers 会关它），App 这一栏清空。
	a.pendingSrc = nil
	// 🔴 **「这一趟自己的那一条」也清掉**（票 #220）：这一趟到这儿就完了，
	// 而它的源已经归播放器管了——留着它的话，**下一次**超时会把正在放着的
	// 那一条的源关掉。
	a.loadSrc = nil
	a.reportedErr = nil
	a.clearErrLocked()
	// 🔴 **这一首放起来了，跳过的账清零**（`autoSkipDead` 的保险丝）：连着失败
	// 的那串到此为止——「跳过了几首」只对「还没放成」的那一段有意义。
	a.skipStreak = 0
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
	// B 站自己带出来的那一份歌词：取流那一趟已经带回了它的地址，这里去下载、
	// 存进缓存、装到界面上（见 lyrics.go 的 `fetchBiliSubtitle`）。
	//
	// ⚠️ **整个过程在后台**，而且**它不拦播放**：没有字幕的那一首歌，界面照旧
	// 是「暂无歌词」加一个「找歌词」的入口。
	a.fetchBiliSubtitle(context.Background(), it, ref, s)
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
	// 🔴 **它背后那个投稿的身份也记下来**：音乐库的取流接口不带字幕，而这一条
	// 的词多半在那个投稿的字幕里（见 `lyrics.go` 的 `fetchBiliSubtitle`）。
	//
	// ⚠️ **记在 `SubtitleBvid`/`SubtitleCid` 上，不记在 `Bvid`/`Cid` 上**：
	// 后两栏是「这一条自己的身份」，歌词缓存键靠它们（`lyricKeyOf`）——
	// 音乐库条目的键是 `bili-audio-<sid>`，填错了键就变了。
	it.SubtitleBvid, it.SubtitleCid = info.Bvid, info.Cid
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
	src, p, err := a.buildFromRef(ctx, s, it, gen, ref, nil)
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
//
// run 是「**开新歌**」那一趟的凭据：nil = 不是那一趟（换音质 / 换流重开
// 那两条路）。它管两件事：分段耗时（写日志）和「这一趟自己的那条源」
// （看门狗看的就是它，理由见 `loadRun`）。
func (a *App) buildFromRef(ctx context.Context, s *Streaming, it Item, gen int,
	ref *bili.StreamRef, run *loadRun,
) (*audio.StreamSource, *audio.Player, error) {
	// 🔴 **`OnRetry` / `OnReconnected` 这两个回调只做一件事：叫醒界面**。
	//
	// ⚠️ **这里没有「重连令牌」**：旧源的回调可能迟到（它还在退避里等着的时候
	// 这一条已经换掉了），而**这两个回调不碰任何状态**——它们只调 `a.wakeUp()`，
	// 让界面重画一次。所以「回调来晚了」不会把状态改坏：那一档的状态**住在源
	// 那边**（`StreamSource.Reconnecting()`），应用只**问**当前在管的那一条流
	// （见 `reconnect.go` 的文件头）。
	//
	// 🔴 **`OnFirstBytes` 是「第②段」的计时点**（票 #220）：头几个字节落到
	// 缓存里的那一刻，「等字节」这一段就结束了，剩下的全是「等解码器」。
	//
	// ⚠️ **它跑在下载 goroutine 里，而读它的是这一段的主体**：所以那个时刻
	// 用**原子量**记（`atomic.Int64`），不是普通变量——普通变量在这里是
	// 一个实实在在的数据竞争（`-race` 一跑就红）。
	var firstAt atomic.Int64
	srcStart := time.Now()
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
		// 🔴 **「正在重连」从源那边报上来**（票 #315）：换地址 / 换流之前
		// 源要等一个退避（1 秒 / 2 秒 / 4 秒），那几秒里界面上不能一个字
		// 都没有——用户看到的是「歌停着、界面不动」。
		//
		// ⚠️ **这两个回调只负责「叫醒界面」**：那一档的状态**住在源那边**
		// （`StreamSource.Reconnecting()`），应用只**问**当前在管的那一条流。
		//
		// 🔴 **为什么不能反过来——让源把状态报上来、应用存一栏**：一条**建
		// 出来又被丢掉**（代次过期）的源会把那一栏抢走，于是正在放的那首
		// 断流时，界面上「正在重连」**一次都不亮**（票 #315 的评审复现过）。
		OnRetry:       func(attempt int, wait time.Duration) { a.wakeUp() },
		OnReconnected: func() { a.wakeUp() },
		// 🔴 **「头几个字节到了」**：第②段的计时点（票 #220）。
		//
		// ⚠️ **它在下载 goroutine 里调**，所以它只做一件事：记时刻。
		// **不许在里面拿 `a.mu`**——那把锁是界面每帧都要的。
		OnFirstBytes: func(int64) {
			firstAt.CompareAndSwap(0, time.Now().UnixNano())
		},
	})
	if err != nil {
		return nil, nil, err
	}
	// 🔴 **把源记在 App 上**：建播放器的这一段里源已经在下载了，而播放器还没
	// 建起来——「超时」和「退出」都要能把它掐掉（见 App.pendingSrc）。
	a.setPendingSource(src, gen)
	// 🔴 **「这一趟自己的那一条」**（票 #220）：看门狗问「这一段还有没有
	// 进展」时要看的是**它**，不是上面那个共享的 `pendingSrc`——
	// 换音质 / 换流重开那两条路会把 `pendingSrc` 顶掉（理由见 App 里
	// `loadSrc` 那段注释）。
	//
	// ⚠️ **只有「开新歌」那一趟才记**：那两条路建的源不是用户此刻在等的
	// 那一条，记上去只会让看门狗盯着别人的流。
	if run != nil {
		a.setLoadSource(src, gen)
	}
	// 🔴 **建源的这一小段时间里代次也可能变了**（用户切歌、或者超时闹钟响了）。
	// 那就没人要这条流了——立刻关掉，别让它下到天亮。
	if a.genStale(gen) {
		a.clearPendingSource(src)
		src.Close()
		return nil, nil, errStaleGen
	}
	// 🔴 **③ 解码第一帧**：`OpenStream` 要等解码器把这条流开起来，而
	// AudioToolbox 探容器要 8 KB 左右的字节——**moov 在文件尾的 MP4 还要
	// 把整个文件读完**（见 `internal/service/audio/extaf` 的注释）。
	//
	// ⚠️ **「开好」的时刻是 `OpenStream` 返回的那一刻**，不是它调用的前一刻：
	// 头几个字节可能正好落在那两句之间，而那时候「解码」这一段还没开始。
	// 拿调用前的时刻当终点会算出**负数**的③。
	p, err := audio.OpenStream(src)
	opened := time.Now()
	// 🔴 **分段耗时落地**（票 #220）：② 是「建源 → 头几个字节到」，
	// ③ 是「头几个字节到 → 解码器开好」。
	if run != nil {
		if ns := firstAt.Load(); ns > 0 {
			run.timing.Bytes = time.Unix(0, ns).Sub(srcStart)
			run.timing.Decode = opened.Sub(time.Unix(0, ns))
		} else {
			// 一个字节都没到：② 是「白等的那段时间」，而③压根没发生。
			run.timing.Bytes = opened.Sub(srcStart)
			run.timing.Decode = 0
		}
	}
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
	vol, rate, norm, eq := a.gainLocked(), a.rate, a.normalize, a.eqGains
	a.applyFadeInLocked(p)
	a.mu.Unlock()
	p.SetVolume(vol).SetRate(rate).SetNormalize(norm).SetEQ(eq)
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
//
// ⚠️ **它也摘「这一趟自己的那一条」**（`loadSrc`）：那一条和它是同一个源的
// 时候（正常路径）摘一次就够；而**换了音质 / 换流重开**那两条路建的是**另一条**
// 源，所以摘的时候要分别判（理由见 `loadSrc` 那段注释）。
func (a *App) clearPendingSource(src *audio.StreamSource) {
	a.mu.Lock()
	if a.pendingSrc == src {
		a.pendingSrc = nil
	}
	if a.loadSrc == src {
		a.loadSrc = nil
	}
	a.mu.Unlock()
}

// abortPendingSource 关掉在途的那条字节源（如果还有的话）。
//
// **锁外面关**：字节源的 `Close` 要等它的下载 goroutine 退出来，而那个
// goroutine 可能正拿着 `a.mu`（换流那条路上它会回调 `noteQuality`）。锁里面
// 等就是死等。
//
// 🔴 **它要关的不止 `pendingSrc` 那一栏**：还有「这一趟取流」自己的那一条
// （`loadSrc`，见那一段的注释）——换音质 / 换流重开那两条路会把 `pendingSrc`
// 换成**它们自己的**源，于是只关 `pendingSrc` 关不到这一趟真正在下的那一条
// （票 #220 的评审复现过：那一条会一直下到天亮，而它的临时文件也一起
// 留在盘上）。
func (a *App) abortPendingSource() {
	a.mu.Lock()
	var srcs []*audio.StreamSource
	if a.pendingSrc != nil {
		srcs = append(srcs, a.pendingSrc)
		a.pendingSrc = nil
	}
	if a.loadSrc != nil {
		srcs = append(srcs, a.loadSrc)
		a.loadSrc = nil
	}
	a.mu.Unlock()
	// ⚠️ **同一个源可能被列两次**（正常路径上 `pendingSrc` 和 `loadSrc`
	// 是同一条）——`StreamSource.Close` 可以重复调（它自己判 `closed`），
	// 所以重复关是安全的。
	for _, s := range srcs {
		s.Close()
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

// putStreamCache 把一条流放进「播放前预判」那个缓存。
//
// **预取备好的地址走这里**（见 prefetch.go）：真播那一趟的 `resolve` 会先问
// 这个缓存，命中了就**不再取流**——省下来的那几百毫秒就是这一票的收益。
// 自动切歌（`onTrackFinished`）和手动按下一首（`App.step`，票 #581）都吃
// 预取；两个入口在用之前都比对过「备的就是这一首」。
func (a *App) putStreamCache(it Item, ref *bili.StreamRef) {
	a.mu.Lock()
	a.streamCache = map[string]*bili.StreamRef{streamKey(it): ref}
	a.mu.Unlock()
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
	// 于是去清缓存重下——而正确的反应是「换个网」。
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
		a.refreshOnlineNow(err)
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
//
// stage 是「失败发生在哪一段」（票 #220）：空串 = 这一条不是「取流那一趟」
// （换音质 / 换流重开）。它进**那一句的完整说明**（悬停提示），不说清
// 「卡在哪一步」的话，用户面对的是一句「连不上 B 站」——而他连是**问地址**
// 问不动、还是**音频**下不来都分不出来。
func (a *App) failOnline(gen int, it Item, branch string, err error, stage string) {
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
	a.logf("biu: 播放失败：%q（%s，段=%s，代次 %d）：%v", it.Title, branch, stage, gen, err)
	// ⚠️ **`setErrText` 自己会叫醒界面**（它把新的话写进去之后才醒），所以这里
	// 不要再叫一次——两次唤醒只是多一帧，而顺序写错会让界面先画出「转圈停了」
	// 再画出那句话，中间那一帧是「既没有转圈也没有话」。
	//
	// 🔴 **短的那句上屏，长的那句进悬停提示**（票 #282 的评审）：播放条上
	// 那一行只有 140 像素宽，一句长话上屏只剩「打不开：取流…」这样的碎片。
	msg := bili.Friendly(err)
	detail := msg
	if stage != "" {
		detail = fmt.Sprintf("卡在「%s」这一步：%s", stage, msg)
	}
	a.setErrTextDetail(msg, detail, bili.Recoverable(err))
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
	// 🔴 **先抄下「卡在哪一段」再收档**：`endLoadingLocked` 会把那一栏清掉，
	// 而它是失败那句话里最有用的那一半（票 #220）。
	stage := a.loadingStage
	if a.onlineGen == gen && !a.closed {
		a.endLoadingLocked()
	}
	a.mu.Unlock()
	a.failOnline(gen, it, branch, err, stage)
	a.autoSkipDead(gen, it, err)
}

// autoSkipDead 是「这一首放不起来，自动换下一首」的那条路（起播失败专用）。
//
// 场景是连播一个收藏夹 / 合集：中间夹着失效的稿子（稿件删了、设了权限、
// 地区锁了），每一首都是取流那一步就死。以前的下场是红字一挂、队列停在
// 那首坏歌上——用户坐在那儿听不到歌，还得自己挨个点下一首。
//
// # 跳，还是不跳
//
// 🔴 **判据是 `bili.Recoverable`**，和「重试」按钮用的是同一张表：
//   - **不可重试**（稿件没了、要会员、没权限……重试一万次也是这个答案）
//     → 跳。留着红字等用户点「重试」是在让他做无用功。
//   - **可重试**（网络断了、地址过期、响应被截断）→ **不跳**。这类错每一首
//     都会撞上（网断了放谁都是死），跳过去就是把整张队列白烧一遍请求，
//     而用户真正要做的是修网络——红字和「重试」留在他眼皮底下。
//
// # 保险丝
//
// 🔴 **`a.skipStreak` 连着数失败的起播，整个队列数满一圈就停**：循环 / 随机
// 两档的 `Queue.Advance` 永远给得出「下一首」，不数的话一个全失效的收藏夹
// 会无限跳下去。放成一首就清零（两处成功起播的代码块里）。上限取
// **队列长度**：正常情况下一圈之内必然撞到活的；全死的话也只多绕一次就把
// 话说全（「连着 N 首都放不起来」）。
//
// # 不跳的三种
//
//   - **代次变了 / 应用退了**：用户已经切了别的歌，这一趟失败没人要它——
//     `failOnline` 把红字都省了，这里更不能去动队列。
//   - **「恢复上次那一首」那一趟**（`failOnline` 的 restore 分支）：同样是
//     静默的，而它的队列指针未必落在队列里，跳是没有方向的动作。
//   - **单曲循环 / 队列到底**（`Advance` 给不出「下一首」）：自动跳的目标
//     不存在，红字已经在屏上，照旧停下。
//
// ⚠️ **只罩起播失败**：`failPlay` 是唯一的调用点。放**到一半**断掉的
// （`refreshOnlineNow` 的「换流重开」救不回来那支）不走这里——那一首用户
// 已经听了一半，自动换歌等于替他按了下一首；红字和「重试」才是对的动作。
func (a *App) autoSkipDead(gen int, it Item, cause error) {
	if bili.Recoverable(cause) ||
		errors.Is(cause, errStaleGen) || errors.Is(cause, errStreamingMissing) {
		return
	}
	a.mu.Lock()
	if a.onlineGen != gen || a.closed || a.restoreGen == gen {
		a.mu.Unlock()
		return
	}
	a.skipStreak++
	streak := a.skipStreak
	limit := a.queue.Len()
	a.mu.Unlock()
	if streak >= limit && limit > 0 {
		a.giveUpSkipping(streak, it, cause)
		return
	}

	// 下一首是谁只有 `Queue.Advance` 说了算（播放模式在那里面）。
	d := a.queue.Advance()
	if d.Kind != AdvancePlay {
		// 单曲循环（重放的还是这首死的）或者队列到底了：红字已经在屏上。
		a.logf("biu: %q 放不起来（%v），没有下一首可跳（%v）", it.Title, cause, d.Kind)
		return
	}
	if d.Item.Path == "" && !d.Item.isOnline() {
		a.queue.UndoStep()
		return
	}
	a.logf("biu: %q 放不起来（%v），自动换下一首 %q（连着第 %d 首）",
		it.Title, cause, d.Item.Title, streak)
	// 念出去（无障碍）：歌没响就换了，读屏软件用户既没听见这一首失败、
	// 也看不见队列换了人——一句话把两件事都说清。
	a.announce(fmt.Sprintf("「%s」放不起来，自动换下一首", it.Title))
	if err := a.play(d.Item); err != nil {
		// 同步就接不上（没有源的那类）：退回指针，红字留给 failOnline 那一句。
		// 异步的死（取流失败）会再回到这一支，保险丝管着圈数。
		a.queue.UndoStep()
	}
}

// giveUpSkipping 是保险丝烧断的那一刻：整个队列挨个试过一圈都没放起来，
// 停下来，把话说明白。
//
// ⚠️ **状态的收法和 `onTrackFinished` 的 `AdvanceStop` 那一支同一个口径**
// （`stopped` 立起来、位置归零、老播放器停在开头；`wantPlay` 不动——
// 「用户想不想听」和「此刻有没有在响」是两栏），**但有一支例外**：起播失败
// 不打断正在放的歌（`playOnlineFrom` 不停旧播放器）——用户是一边听着上一首
// 一边点到这首坏歌的场合，上一首还在响就**让它接着响**，只把话说出去；
// 「停机」那一套只落在手上那首**已经不响**的播放器上（连播到坏歌、或者
// 第一首就死了的两种），nil 就没什么可停的。
func (a *App) giveUpSkipping(streak int, last Item, cause error) {
	a.mu.Lock()
	p := a.player
	sounding := p != nil && p.Playing()
	if !sounding {
		a.position, a.playing = 0, false
		a.stopped = true
	}
	wake := a.wake
	a.mu.Unlock()
	if !sounding {
		a.markPlaybackDirty()
		if p != nil {
			p.Pause()
			p.Seek(0)
		}
	}
	a.logf("biu: 连着 %d 首都放不起来，停下来了（最后一首 %q：%v）", streak, last.Title, cause)
	// 红字是 `failOnline` 写的（最后一首的那句错），它没人清，就停在屏上。
	if sounding {
		a.announce(fmt.Sprintf("「%s」放不起来；刚才那一首还在放，没有动它", last.Title))
	} else {
		a.announce(fmt.Sprintf("连着 %d 首都放不起来，先停下来了", streak))
	}
	if wake != nil {
		wake()
	}
}

// refreshOnlineNow 从当前的播放位置重开一首。
//
// 这是**第三条路径**在应用层的入口：音频层把「换了另一条流，接不上原来的
// 字节」这件事报上来（`audio.ErrStreamChanged`），能不能从**播放位置**继续
// 就得在这一层做——播放位置是时间，只有 App 知道上一次读到哪儿了。
//
// 一首歌只重开一次（`onlineTrack.reopening`）。第二次还不行就该如实报错：
// 用户听得到「这首歌放不了了」，比程序一直自己折腾强。
//
// 🔴 **「重开不了」必须报错，不许一声不吭**（票 #315 的评审复现过）：以前
// 这一支是直接 `return`——于是界面上「正在重连」那一档**永远亮着**、一句
// 红字也没有，而歌早就停了。用户看到的「正在重连」是**假的**：它压根没有
// 在重连，它是放弃了。
func (a *App) refreshOnlineNow(cause error) {
	a.mu.Lock()
	tr := a.online
	if tr == nil {
		a.mu.Unlock()
		return
	}
	if tr.reopening {
		// 已经重开过一次了：这一首救不回来了。老实报错。
		it := tr.item
		a.mu.Unlock()
		a.logf("biu: 换流重开被作废了（这一首已经重开过一次）：%q：%v", it.Title, cause)
		a.setErrTextDetail("这条流接不上了",
			"这条流换了两次，接不上刚才播放的位置了。换一首放，或者点「重试」再取一次流。",
			true)
		return
	}
	tr.reopening = true
	gen := a.onlineGen
	pos, want := a.position, a.wantPlay
	a.mu.Unlock()

	// 🔴 **「正在重连」从这一刻开始**（票 #315）：这一趟要走一次网络往返
	// （重新取流 + 开解码器），几百毫秒到几秒。界面上原来一个字都没有，
	// 用户看到的就是「歌停着、界面不动、没有字」——等多久都像应用死了。
	a.beginReconnect(gen)

	s := a.streamingSnapshot()
	if s == nil {
		a.setErr(bili.Friendly(errStreamingMissing))
		return
	}

	go func() {
		src, p, ref, err := a.buildOnline(context.Background(), s, tr.item, gen)
		if err != nil {
			a.failOnline(gen, tr.item, "换流重开", err, "")
			return
		}

		a.mu.Lock()
		if a.onlineGen != gen || a.closed || a.online == nil {
			cur := a.onlineGen
			a.mu.Unlock()
			p.Close()
			a.logf("biu: 换流重开被作废了（代次变了）：%q（来的代次 %d，现在的代次 %d）",
				tr.item.Title, gen, cur)
			a.endReconnect(gen)
			return
		}
		old := a.player
		a.player = p
		a.duration = p.Duration()
		a.position = pos
		a.quality = ref.Label
		a.online = &onlineTrack{item: tr.item, ref: ref, source: src, quality: ref.Label,
			reopening: true, gen: gen}
		// 源归播放器管了（见 App.pendingSrc）。
		a.pendingSrc = nil
		a.reportedErr = nil
		a.clearErrLocked()
		// 🔴 **「正在从播放位置重开」这一档到此为止**（新播放器装上了）。
		//
		// ⚠️ **只收应用层那一半**：源那一半（源正在等字节回来）归源自己管
		// （`Reconnecting()`）——它可能**正在退避里等着**：歌停着、位置不动、
		// 字节还没回来。这里收了它，退避的那几秒就又变成静默的了。
		a.reopenInFlight = false
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
