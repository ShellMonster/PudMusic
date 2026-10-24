// Package app 是业务状态层：全部状态挂在 App 上，界面层读它、用户操作写它。
//
// 它不认识界面（不 import mygo/ui），也不碰网络。打开文件、播放、跳位置这些
// 活儿都交给服务层；这一层只管「现在在放什么、放到哪儿了、要不要继续」。
package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/media"
	"github.com/ShellMonster/Biu_Go/internal/service/audio"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
)

// afterFunc 排一个「过 d 之后再跑 fn」的定时器，返回停掉它的函数。
//
// 包一层是为了让 `App.after` 的类型和 `time.AfterFunc` 的**返回值**对得上
// （后者返回一个 `*time.Timer`，不是函数）。
func afterFunc(d time.Duration, fn func()) func() {
	t := time.AfterFunc(d, fn)
	return func() { t.Stop() }
}

// tickInterval 是进度条时钟的间隔。每秒 20 次足够让进度条看起来是滑的，
// 又不至于把 CPU 烧在重绘上（Invalidate 会合并，一帧里叫多少次都只画一次）。
const tickInterval = 50 * time.Millisecond

// RateSteps 是倍率的档位（0.5 / 0.75 / 1 / 1.25 / 1.5 / 1.75 / 2）。
//
// 这是音频层那份的原样转发，界面层只认 app 这一份：界面画的每一行档位都必须
// 和 `SetRate` 认得的档位是同一份，两边各抄一份迟早会漂。
var RateSteps = audio.RateSteps

// State 是一帧里界面要读的全部状态。
//
// view 每帧开头调一次 Snapshot，拿到的是一份自洽的拷贝：它不会读到「歌名已经
// 换了但时长还是旧的」这种写了一半的状态。
type State struct {
	HasTrack bool
	Title    string
	Path     string

	// Source 是当前这一首的来源：`SourceLocal` / `SourceOnline`。
	//
	// 空串 = **没有歌**（和 `HasTrack` 一起是假）。
	//
	// ⚠️ **界面要靠它分两件事**（票 #222 / 报告 §3.12、§6.5）：
	//
	//	本地文件不摆「下载」那一枚（上游 `playbar/right/index.tsx:15` 的
	//	`source !== "local"`——本地文件在 B 站上压根没有 cid 可下）。
	//	歌名**点得动**与否也看它（上游 `playbar/left/index.tsx:31` 的
	//	`isClickable = playItem && playItem.source !== "local"`）：本地文件
	//	在 B 站上没有页面可跳。
	//
	// 这两处以前都判不出来——`State` 里没有来源这一栏（`Item.Source` 在队列
	// 那一层，不在快照里）。
	Source string
	// CoverURL 是当前这首的封面地址，交给界面的封面缓存去取。空串表示没有封面。
	//
	// ⚠️ 本地文件永远是空的，而且这是照抄 Biu 的行为，不是省事：Biu 上游也不读
	// 音频文件里内嵌的图（`electron/ipc/local-music.ts` 用 music-metadata 但只取
	// title 和 duration，result.push 里没有任何 picture 字段），所以本地音乐在
	// Biu 里也是占位。别拿文件路径去凑一个假地址——缓存拿到空串画的是空框，
	// 不是「加载失败」，这两个状态在 imgcache 的 drawCover 里是分开的两支。
	//
	// 字段本身先立好：接 B 站在线歌时那才有真封面，那时只要让队列项把它带下来，
	// 界面和缓存都不用改。
	CoverURL string
	Playing  bool
	// Stopped 是「**停住了**」：进度回到开头、不在放。
	//
	// ⚠️ **它和暂停是两件事**（票 #319）：暂停停在原地，而停住是**回到开头**——
	// 系统媒体会话那边也是两个不同的状态（`media.StateStopped`，见 media.go
	// 的 `mediaSnapshotLocked`）。界面上要靠它把两者分得开。
	//
	// 它由**两处**置上：`App.Stop`（用户按了停止）和顺序播放走到队尾
	// （`onTrackFinished` 的 `AdvanceStop`）——后者见 `QueueFinished`。
	Stopped bool
	// QueueFinished 是「**这一串放完了**」：顺序播放走到队尾，于是停在开头
	// 等用户（和 Biu 上游的 `audio.currentTime = 0; audio.pause()` 一样）。
	//
	// ⚠️ **它蕴含 `Stopped`**：那两条路最后都落在「停住了」这一档上，而用户
	// 要分的是**两件事**：队列里还有歌、而用户叫停的（「已停止」），和队列
	// 自己走完了（「这一串放完了」）。**只有后者才是「没有下一首了」**。
	//
	// 判据见 `Snapshot` 里那一行（`stopped` 和 `wantPlay` 一起读）。
	QueueFinished bool
	Position      time.Duration
	Duration      time.Duration
	Volume        float64
	// Muted 是「静音」这一档。
	//
	// ⚠️ **它和 Volume 是两件事**（票 #311 / 报告 §3.3 的 B2）：Volume 记的是
	// 「用户设的是多少」，Muted 记的是「现在按着静音没有」。
	//
	// 静音的时候 Volume **不变**——取消静音放回去的就是它。落盘的也是它：静音
	// 之后退出再启动，音量还是用户设的那一档，而不是 0（Clementine 的老毛病）。
	//
	// 界面读这一栏画那一枚图标（静音 / 取消静音），不靠「音量是不是 0」去猜
	// ——音量 0 和静音在**听感**上一样，在**意思**上不一样。
	Muted bool
	// Rate 是当前的播放倍率（0.5~2.0，1 是原速）。
	//
	// 它是**用户的一个设置**，不是某一首歌的属性：换歌之后还在，切歌那一瞬间
	// 也不该把它按成 1。所以它每帧都从 App 现读，界面上的按钮也一样。
	Rate float64
	// Err 是最近一次打开文件的失败原因，显示在播放条上，直到下一次打开成功。
	Err string
	// ErrDetail 是那一句错误的**完整说明**（界面上只显示 `Err`，完整的挂在
	// 悬停提示上）。
	//
	// 🔴 **为什么要有两栏**：播放条上那一行只有 140 像素宽（1200 宽的窗口，
	// 实测），12 号字放得下十来个字——一句 50 个字的话在屏幕上只剩
	// 「打不开：取流…」这样的碎片，那不是「一句能看懂的话」（票 #282 的
	// 评审）。所以**短的那句上屏，长的那句进悬停提示**。
	//
	// 空串 = 和 `Err` 一样（绝大多数错都是这样）。
	ErrDetail string

	// Loading 是「**正在取流**」：用户刚点了一首，在途的那一次播放还没装上
	// 播放器。
	//
	// ⚠️ **它和 `Playing` 是两件事**：`Playing` 说的是「播放器在不在响」，而
	// 这一栏说的是「**这一首还没放起来**」。在线流要先跑一趟网络（取流 + 开
	// 解码器），那一趟里播放器压根还不存在——不单独说这一栏，界面上就分不出
	// 「正在准备」和「压根没在放」，而这两种在用户眼里都是「点了没反应」。
	//
	// 🔴 **它必须有超时**：这一栏要是能一直亮着，它就成了另一种「什么都不说」
	// （票 #282）。超过 `onlinePlayTimeout` 就按失败处理，界面上换成一句人话。
	Loading bool
	// LoadingTitle 是正在取的那一首的名字。空串 = 没有在取流。
	//
	// ⚠️ **它不是 `Title`**：`Title` 是「正在放的那首」，而取流这一趟里正在放
	// 的还是上一首。用户要知道的是「我刚才点的那一首在准备」，不是上一首。
	LoadingTitle string
	// Reconnecting 是「**正在重连**」：断流之后应用在救这一首。
	//
	// 🔴 **它和 `Loading` 是两件事**，用户看到的差别很大：
	//
	//	Loading		这一首**还没放起来**（第一次点它）
	//	Reconnecting	这一首**响过一半**，流断了，正在换地址 / 换流重开
	//
	// 后者以前界面上一个字都没有——用户看到的是「歌停着、界面不动、没有
	// 字」，等多久都像应用死了（票 #315）。加上退避之后更糟：那 7 秒的
	// 等待原本是**完全静默**的。
	//
	// ⚠️ **它必须有上限**：退避最多等 7 秒（`audio` 的 `defaultBackoff` 按
	// 默认的三次重试算），加上换流那一趟网络往返，总共十几秒。到点了就该
	// 报失败，不能一直亮着——一直亮着就是另一种「什么都不说」。
	Reconnecting bool
	// ReconnectAttempt 是第几次重试（从 1 起）。0 = 不是「重试」那一档
	// （比如应用层自己从播放位置重开）。
	ReconnectAttempt int
	// CanRetry 报当前那一句错误**再试一次有没有用**。
	//
	// ⚠️ **它和 `Err` 是一对**：Err 说「出了什么事」，CanRetry 说「还能不能
	// 再试一下」。两件事必须分开——把「B 站说这一条没有音频流」（重试一万
	// 次也没用）和「网络断了」（点一下就好）说成同一句，用户就分不清该
	// 换网还是该换歌。
	//
	// 为真时界面在播放条上给一枚「重试」键；为假时**不给**。
	CanRetry bool

	// BiliURL 是当前这一首在 B 站上的地址。空串 = 没有（本地文件，或者
	// 这一条压根没有稿件身份）。
	//
	// 它的用处是给「还能怎么办」一个**动作**：B 站说这一条没有音频流的
	// 时候，用户能做的事就是「去 B 站里听它」——而那一枚键就是这句话。
	// 只给一句文字的话，播放条上那一行放不下（它只有一行，还带省略号）。
	BiliURL string

	// Artist 是当前这首的 UP 主名，播放条上那一行字点它进 UP 主空间。
	//
	// **本地文件永远是空的**：Biu 上游也不读音频内嵌的标签（见 media.go 的
	// setArtist），所以本地音乐在那边也是没有 UP 名的一行。空串时播放条
	// 压根不画那一行——摆一句空的地方不如不摆。
	Artist string

	// ArtistMid 是上面那个 UP 主的 mid，也就是 `/user/{id}` 里那个 id。
	//
	// ⚠️ **它可以是 0 而 Artist 不是空串**：有的来源只给名字不给编号（动态流
	// 那一批）。那种情况下 UP 名照样显示，只是**点不动**（见 `Item.ArtistMid`）。
	ArtistMid int64

	// Quality 是当前这条流的音质徽章（「Hi-Res 无损」/「192K」）。
	//
	// 空串 = 这一首是本地文件，没有音质这回事（本地文件是什么格式和
	// 「音质」不是一回事，不要拿文件格式凑一个）。
	//
	// **换流之后它会变**：B 站偶尔会给出比上次低的一档，用户得看见。
	Quality string

	// QualityPref 是用户**选的那一档**（`AudioQuality` 那组字符串）。
	//
	// 和 Quality 分开是因为它们是两件事：选的是「我要无损」，徽章说的是
	// 「B 站给了无损」。降级发生之后界面上要同时出现两者——混成一句就变成
	// 「用户要无损，系统给了无损」，看不出发生了降级。
	//
	// ⚠️ 它是**偏好**，本地文件播放时也照样有值：徽章只对在线流画，
	// 而换档这个动作不该要求当前正好在放在线流。
	QualityPref AudioQuality

	// PlayMode 是当前的播放模式（顺序 / 循环 / 随机 / 单曲）。
	//
	// 界面**只读它**、按一下调 `App.CyclePlayMode`，不许自己算下一首：
	// 「下一首是谁」只有 `Queue.Advance` 说得出来（见 queue.go 那一节）。
	PlayMode PlayMode

	// KeepPagesOrderInRandom 是「随机时保持分集顺序」。只在随机那一档下有意义，
	// 别的档位里它是什么都不改变。
	KeepPagesOrderInRandom bool
}

// App 是应用的全部状态。
type App struct {
	mu       sync.Mutex
	player   *audio.Player
	title    string
	path     string
	cover    string
	position time.Duration
	duration time.Duration
	volume   float64
	// muted 是「静音」这一档，和 volume 分开记（票 #311）。
	//
	// ⚠️ **它和 volume 必须是两件事**：静音的时候 volume 一动都不动——取消静音
	// 放回去的就是它，落盘的也是它。合成一档（静音 = 音量 0）的话，一旦开始
	// 落盘音量，就会踩 Clementine 那个坑：静音后退出再启动，音量变成 0。
	muted bool
	// rate 是播放倍率，和 volume 一样是**用户的选择**，换歌之后还在。
	// 它必须由这一层记着：播放器只管手上这一首（`audio.Player.SetRate`），
	// 而新歌是后开的播放器，得有人告诉它「用户要的是几倍」。
	rate float64
	// wantPlay 是用户的意图（按过播放），playing 是播放器此刻的实际状态。
	// 两者分开是因为一首歌播完之后 wantPlay 还是 true，而 playing 变回 false；
	// 进度条拖回中间时靠 wantPlay 才能接着播。
	//
	// ⚠️ **两个方向都要记**（票 #352）：按了暂停落 false，**按回来记回 true**。
	// 只落不记的话它会一直 false，而 `onTrackFinished` 拿它当「要不要往下走」
	// 的判据 → 队列卡住 → 而界面上一个字都没有。
	wantPlay bool
	playing  bool
	errText  string
	// errDetail 是那一句错误的完整说明（悬停提示里给的是它，见 State.ErrDetail）。
	errDetail string
	// errRetryable 说的是当前那一句错误**再试一次有没有用**。
	//
	// 界面上据此决定给不给「重试」那一枚键：给一个按了也没用的重试比不给
	// 更糟（用户按两次发现还是那样，就不再信这个应用了）。
	errRetryable bool
	// pending 是「上一次用户想放的那一项」。
	//
	// 它是重试那一枚键的**凭据**：重试要放的还是这一首，而不是队列里
	// 当前的那一首（取流失败时队列往往已经滚回去了）。
	pending Item

	queue *Queue // 播放队列，见 queue.go

	// playbackDirty 是「播放状态变了，该落盘了」。见 playback_state.go。
	playbackDirty bool
	// playbackSavedAt 是上一次落盘的时刻，playbackSavedPos 是那一次的位置。
	//
	// 两者是用来**限速**的：位置一直在变（在放的时候每一拍都在变），而不限速
	// 就是每 120ms 写一次整个设置文件。见 savePlaybackIfDue。
	playbackSavedAt  time.Time
	playbackSavedPos time.Duration
	// restoreGen 是「这一趟取流是为了恢复上次的位置」的标记，见
	// playback_state.go。
	//
	// ⚠️ **它认的是代次**，所以只对那一趟有效：用户之后点的任何一首歌都是
	// 另一个代次，不会被这一趟的「安静失败」罩住。
	restoreGen int

	// account 是扫码登录那摊事（状态、轮询、落盘），见 account.go。
	// 挂上去之前是 nil，界面要判空。
	account *Account

	// 在线播放这一摊，见 online.go。挂上去之前 streaming 是 nil。
	streaming *Streaming
	// 歌单内容页这一摊，见 collection.go。挂上去之前是 nil（界面判空）。
	collections *collections

	streamCache map[string]*bili.StreamRef
	// onlineGen 是**播放代次**。每开一次在线流就加一，异步回来的结果
	// 先对一遍，对不上就走人（快速切歌时旧曲的结果不能覆盖新曲）。
	onlineGen int
	// loading 是「正在取流」（见 State.Loading）。
	loading bool
	// loadingTitle 是正在取的那一首的名字（见 State.LoadingTitle）。
	loadingTitle string
	// reopenInFlight 是「**应用**正在从播放位置重开」那一半。
	//
	// ⚠️ **源那一半不住在这儿**（它住在 `audio.StreamSource` 那边，见
	// `reconnect.go`）：在这儿存一栏的话，一条**建出来又被丢掉**（代次过期）
	// 的源会把那一栏抢走——那时正在放的那首断流，界面上「正在重连」一次都
	// 不亮（票 #315 的评审复现过）。
	reopenInFlight bool
	// loadStop 是「取流超时」那个闹钟的停掉函数。nil = 没挂。
	//
	// ⚠️ **它和 `lyricStopTimer` 不是一个东西**：那一个管歌词偏移的防抖写盘，
	// 这一个管「这一首还没放起来」。两个都用 `a.after` 排定时器，但**停掉函数
	// 各存各的**——共用一个字段的话，先停掉另一个就把另一个的闹钟弄丢了
	// （歌词的防抖窗口会永远不落盘）。
	loadStop func()
	// pendingSrc 是「正在建的那条在线字节源」，pendingSrcGen 是它属于哪一趟。
	//
	// 🔴 **它是为了让「超时」和「退出」能真的把在途的下载掐掉**（票 #282）：
	// 字节源自己在后台下载，而那时**播放器还没建起来**——`App.Close` 关的是
	// 播放器，管不到它。没有这一栏的话，一次卡死的取流会在**应用退出之后还在
	// 下载**（连它的临时文件也一起留在盘上）。
	//
	// 装上播放器之后它就归播放器管了（`audio.Player` 的 closers），这一栏清空。
	pendingSrc *audio.StreamSource
	// pendingSrcGen 是 pendingSrc 属于哪一趟（见上）。
	pendingSrcGen int
	// online 是当前正在放的那条在线流，nil 表示放的是本地文件。
	online *onlineTrack
	// quality 是 State.Quality 的存放处，跟着换流更新。
	quality string
	// reportedErr 是已经往界面上报过的那一条。解码器的 Err 记的是**第一条**
	// 错误、不自己清零，所以不比对的话每 50 ms 会重复报一次。
	reportedErr error

	// 歌词这一摊，见 lyrics.go。挂上缓存之前 lyricCache 是 nil，那时就一直
	// 「没有歌词」。
	lyricCache *lyrics.Cache
	// lyricKey 是当前这首歌的缓存键。**换歌时才变**，界面靠它认出「换歌了」
	// （同一首换流不重置，见 setLyricTrack）。
	lyricKey string
	// lyricGen 是**歌词代次**。每换一次歌或换一次歌词源就加一，迟到的缓存读取
	// 回来发现代次变了直接走人（见 loadLyrics）。
	lyricGen int
	// lyricRows 是已经叠好译文的那一行行。
	lyricRows []lyrics.Row
	// lyricOffsetMS / lyricFontSize 是用户调过的值，切歌时同步重置成默认值。
	lyricOffsetMS int
	lyricFontSize float32
	// lyricEntry 是缓存里这一条原文+译文的**内容**（不含偏移和字号那两个
	// 用户值，那两个在 flushLyrics 里现填）。写盘和换源都靠它。
	lyricEntry lyrics.Entry
	// lyricStopTimer 是偏移/字号落盘那个防抖窗口的「停掉它」。每次重新排窗口都会
	// 先调上一次那个（理由见 armLyricsSaveLocked）。它是**函数**而不是
	// `*time.Timer`，是为了让测试能换成一个手动推进的假钟。
	lyricStopTimer func()

	// lyricSearch 是「歌词搜索」那一摊，lyricPreview 是「歌词预览」那一摊，
	// 见 lyrics_search.go。挂上歌词源之前 sources 是空的，界面上的搜索入口
	// 摆不出来。
	lyricSearch  lyricSearchState
	lyricPreview lyricPreviewState

	// suggest 是搜索浮层里「联想词」那一摊，见 searchsuggest.go。零值能用
	// （没问过就是「一个都没有」）。
	suggest suggestState

	// hookBetweenAdoptStateAndSave 是**测试专用**的替掉入口：采用歌词时，在「状态
	// 已经装上」和「往盘上写」之间调它。生产里永远是 nil。
	//
	// 为什么需要它：那个窗口只有几十纳秒，靠循环撞是撞不出来的（实测写过一次，跑
	// 一整天没碰上）。而它护着的是**持久数据**——撑开那一格往里塞一次换歌，就把
	// 「验键和取键不在同一个临界区」这件事变成一次必现的失败。
	//
	// 和 `Shell.pageBody` / `autostart` / `picker` 是同一个形状：一个只给测试用的
	// 接缝，挂在生产二进制里没有害（只是多一个永远为 nil 的字段）。
	hookBetweenAdoptStateAndSave func()

	// after 排一个「过 d 之后再跑 fn」的定时器，返回停掉它的函数。New 装成真的。
	//
	// 做成字段是为了让**等待**测得到：真等 500ms 的测试要么慢，要么靠
	// `time.Sleep`——而 `PLAN.md` 的验收项明写「用假钟，不靠 time.Sleep」。
	// 用得着的只有歌词那 500ms 的防抖窗口（见 lyrics.go）。
	after func(d time.Duration, fn func()) func()

	// playMu 护着 playLog。
	playMu sync.Mutex
	// playLog 是**播放路径**的日志出口（票 #282）。
	//
	// 生产上它是标准 logger（`log.Printf`），而 `main.go` 把标准 logger 指到
	// 日志文件（`platform.OpenLogFile`）——于是「点了歌不播」这类问题事后
	// 查得到。
	//
	// 🔴 **它为什么是 App 的字段而不是包级变量**：包级变量会让**两个并行的
	// 应用实例**互相看见对方的日志，而这一层的日志正是「哪一次播放出的事」的
	// 唯一凭据——串了的话，事后排查会拿着另一首歌的日志找原因。
	playLog func(format string, args ...any)

	// fullPlayerPrefs 是全屏播放器自己那一份偏好（见 fullplayer_prefs.go）。
	// 挂上之前是 nil，`FullPlayerPrefs` 判空之后给默认值。
	fullPlayerPrefs *fullPlayerPrefsStore
	// specBars 是频谱条那一行的高度（0~1），每一帧复用同一块。
	// 频谱是**每帧**要画的（条子每帧都在动），所以这里绝不能每帧新建。
	specBars []float32
	// parts 是当前这首的分集列表，partsBvid 是它属于哪一个稿件。见 fullplayer.go。
	partsMu     sync.Mutex
	parts       []bili.VideoPart
	partsBvid   string
	partsErr    error
	partsLoaded bool
	partsWanted bool
	// partsTriedAt 是上一次**发起**那一问的时刻，问失败之后拿它算退避
	// （`partsRetryAfter`）。成功之后用不上——`partsLoaded` 已经挡住了。
	partsTriedAt time.Time
	// partsFetch 是问分集的那一趟，nil = 走取流零件上的 `bili.Client.VideoPages`。
	//
	// **只由界面那一侧读写**：`ensureParts` 在那里把它取出来递给后台那个
	// goroutine，goroutine 不碰这个字段。生产上永远是 nil（所以不需要锁）；
	// 测试里替成一个假函数，才能量「失败之后到底有没有再问」。
	partsFetch func(context.Context, string) ([]bili.VideoPart, error)

	// like 是当前这一首的赞 / 藏状态，likeBvid 是它属于哪一个稿件。见 like.go。
	//
	// ⚠️ **它自己的锁，不在 `a.mu` 里**：写它的是网络往返回来的那个 goroutine，
	// 而 `a.mu` 是播放器那一摊的（`Snapshot` 每帧都要它）。把两者放一把锁里的话，
	// 一次网络回调要去抢每帧都在拿的那把锁。
	likeMu     sync.Mutex
	like       bili.ArchiveRelation
	likeBvid   string
	likeErr    error
	likeLoaded bool
	likeWanted bool
	// likeTriedAt 是上一次**发起**那一问的时刻，问失败之后拿它算退避
	// （`likeRetryAfter`）。成功之后用不上——`likeLoaded` 已经挡住了。
	likeTriedAt time.Time
	// likeFetch 是问赞藏的那一趟，nil = 走取流零件上的 `bili.Client.ArchiveRelation`。
	//
	// 和 `partsFetch` 同一条规矩：**只由界面那一侧读写**（`ensureLike` 在那里就
	// 把它取出来递给那个 goroutine，goroutine 不碰这个字段）。生产上永远是 nil。
	likeFetch func(context.Context, string) (bili.ArchiveRelation, error)

	// 本地音乐库，见 localmusic.go。挂上去之前是 nil，界面要判空。
	localMusic *localLibrary

	// 下载这一摊，见 downloads.go。挂上去之前是 nil，界面要判空。
	downloads *download.Manager
	// downloadQueueFile 是队列落盘的位置。退出时写、重开时读。
	downloadQueueFile string
	// downloadFallbackDir 是设置里没选过下载目录时的去处（默认是「音乐」目录下的
	// 应用子目录）。
	downloadFallbackDir string

	// updates 是自动更新那一摊，见 update.go。挂上去之前是 nil，界面要判空。
	//
	// ⚠️ 它和 downloads/account/localMusic 一样是**自己的锁**：后台那个下载
	// goroutine 一边下一边写，而界面每帧要读它，混进 a.mu 会把界面那一帧的
	// 等待时间变成「网络那一趟」。
	updates *updater

	// report 是「往 B 站写播放记录」这一摊，见 playreport.go。
	// 挂上在线播放（`MountStreaming`）之前是 nil。
	//
	// ⚠️ **唯一一处两把锁叠着拿的地方，是上报器拿着自己的锁去问
	// `App.account`**（`reportTick` / `stopTrack` 都是先放掉 `a.mu`、
	// 再去拿上报器的锁）。所以**永远不要在拿着 `a.mu` 的时候去拿
	// 上报器的锁**——反过来的顺序凑齐了就是一次死锁，而死锁在这个位置上
	// 表现为「整个应用冻住」（进度条的时钟每一拍都要过 `a.mu`）。
	report *playReporter

	// artist 是当前这首的艺人（在线条目的 UP 主名）。本地文件是空的，
	// 见 media.go 的 setArtist。它有两个用户：系统媒体会话，和界面上播放条
	// 那一行 UP 名（`State.Artist`）。
	artist string
	// artistMid 是那个 UP 主的 mid，播放条上点 UP 名进 `/user/{id}` 用它。
	//
	// ⚠️ 它和 artist **一起**换、一起清：只换名字不换编号的话，播放条上的
	// UP 名会指着**上一个 UP 主**的空间，而界面上看不出任何异样。
	artistMid int64
	// stopped 是「停住了」：和暂停分开，因为它让进度条回到开头，
	// 而系统那边也是两个不同的状态（见 media.go 的 Stop）。
	//
	// ⚠️ **它由两处置上，而那两处要分得开**（票 #319）：`App.Stop`（用户按了
	// 停止，顺手把 `wantPlay` 落成 false）和 `onTrackFinished` 的
	// `AdvanceStop`（顺序播放走到队尾，`wantPlay` 留着 true）。**用户要分的
	// 正是这两件事**：「我按了停止」和「这一串放完了」——只有后者才是「没有
	// 下一首了」。
	//
	// 所以读它的时候要**和 `wantPlay` 一起读**（见 `Snapshot` 里的
	// `QueueFinished`）。其余几处置 `stopped` 的地方都顺手把它收成 false，
	// 所以「停住了、而用户仍想放」这个组合只有 `AdvanceStop` 那一处凑得出来。
	stopped bool

	// mediaSink 是「该跟系统媒体会话说一声了」的那只手，由界面层装一次。
	// nil = 没装（这一层不参与，Publish 是空操作）。
	mediaSink func(media.NowPlaying)
	// mediaAt 是上一次跟系统说话的时刻。用来限住「多久校正一次进度」。
	mediaAt time.Time

	wake   func() // 界面层装：状态写完后叫它要一帧。装了就等于 `wakeAll`
	done   chan struct{}
	closed bool

	// wakes 是**逐窗口**的那一份唤醒句柄，见 AddWake。
	//
	// ⚠️ **它有自己的锁，不在 `a.mu` 里**：`wake` 在二十几处被调用，而其中有些
	// 是**拿着别的锁**取的（`updater.wake()` 就明确要求「拿着 u.mu 时可以调」）。
	// 共用 `a.mu` 就得逐处证明没有嵌套，漏一处是当场死锁——而死锁在这里的表现是
	// 「整个应用冻住」，和真因隔得十万八千里。分一把锁就永远凑不出那个环。
	wakeMu  sync.Mutex
	wakes   []wakeEntry
	wakeSeq uint64

	// announces 是**逐窗口**的那一份播报句柄，见 AddAnnounce。
	//
	// ⚠️ **它也有自己的锁，不在 `a.mu` 里**：理由同上——`Announce` 是从后台
	// 那条线（取流回来了、下载下完了）调进来的，而那条线上可能正拿着别的锁。
	announceMu  sync.Mutex
	announces   []announceEntry
	announceSeq uint64
}

// wakeEntry 是一个窗口的唤醒句柄，外加一个用来摘掉它的号。
//
// 号是必要的：函数值不能比（只能和 nil 比），所以「关窗时摘掉自己那一条」
// 只能靠一个能比较的东西认。
type wakeEntry struct {
	tok uint64
	fn  func()
}

// New 建一个空的应用状态，开后台时钟。
func New() *App {
	a := &App{volume: DefaultVolume, rate: audio.DefaultRate, queue: NewQueue(), done: make(chan struct{}),
		streamCache: map[string]*bili.StreamRef{}, after: afterFunc, playLog: defaultPlayLog}
	// 队列一变就标脏（落盘那一格，见 playback_state.go）。
	//
	// ⚠️ **排在起后台时钟之前**：那个钩子会被后台那一拍读到，而 goroutine 的
	// 创建就是一次同步——排在后面就是一次数据竞争。
	a.queue.onChange = a.markPlaybackDirty
	go a.tick()
	return a
}

// logf 往**播放路径**的日志里写一句。
//
// 🔴 **出口可以是 nil**：没装（或者被换成了 nil）时这句话就丢了——
// 日志不是功能，丢了不许把整个应用带下去。
func (a *App) logf(format string, args ...any) {
	a.playMu.Lock()
	f := a.playLog
	a.playMu.Unlock()
	if f != nil {
		f(format, args...)
	}
}

// SetWake 装上「状态变了，要一帧」的回调，由界面层装一次。
//
// 为什么需要它：MyGo 原生 UI 是即时模式的，后台 goroutine 改了状态之后，
// 界面不会自己重画——不叫醒它，界面就永远停在旧帧（实测：只投递不唤醒 = +0 帧）。
//
// ⚠️ 这个回调必须是合并安全的（ui.Services.Invalidate 就是），并且能跨 goroutine 调用。
//
// ⚠️ **它是「装一个」，不是「再加一个」**：登记第二个窗口用 `AddWake`。绝大多数
// 调用方（全部测试）一辈子只有一个窗口，两个函数在那儿是同一件事，所以没有把
// `SetWake` 删掉。
func (a *App) SetWake(fn func()) {
	a.wakeMu.Lock()
	a.wakes = []wakeEntry{{fn: fn}}
	a.wakeMu.Unlock()
	a.publishWake(1)
}

// AddWake **再**登记一个窗口的唤醒句柄，返回摘掉它的那一个（窗口真关掉时调）。
//
// # 为什么一个进程要登记好几个
//
// 迷你播放器是第二个窗口（`internal/ui/miniplayer.go`），两个窗口共用同一个 App。
// 而铁律三要的是**逐窗口**通知：只叫醒主窗口，迷你窗口停在旧内容（实测：只投递
// 不唤醒两个窗口各 +0 帧；只唤醒主窗口则迷你窗口停在旧内容）。所以「叫醒界面」
// 这一支必须是挨个叫醒的，而不是某一个窗口的那一支。
//
// 有两个入口通往后台：应用状态本身（这里）和本地音乐库（`AddLocalWake`）。两处
// 都是同一个道理，所以都收成「挨个叫醒」。
func (a *App) AddWake(fn func()) (remove func()) {
	a.wakeMu.Lock()
	a.wakeSeq++
	tok := a.wakeSeq
	a.wakes = append(a.wakes, wakeEntry{tok: tok, fn: fn})
	n := len(a.wakes)
	a.wakeMu.Unlock()
	a.publishWake(n)

	// 返回的这一支可以调任意次：摘掉之后再调是空操作（那是「关窗流程走了两遍」）。
	return func() {
		a.wakeMu.Lock()
		for i, w := range a.wakes {
			if w.tok != tok {
				continue
			}
			a.wakes = append(a.wakes[:i], a.wakes[i+1:]...)
			break
		}
		n := len(a.wakes)
		a.wakeMu.Unlock()
		a.publishWake(n)
	}
}

// wakeAll 挨个叫醒登记过的每一个窗口。
//
// 🔴 铁律三：后台改了状态必须唤醒**每一个**画着这份状态的窗口，否则那一个停在
// 旧帧。它在 `SetWake` 装过之后一直就是 `a.wake` 的值，所以那二十几处调用点
// 一个字都没改。
func (a *App) wakeAll() {
	a.wakeMu.Lock()
	all := slices.Clone(a.wakes)
	a.wakeMu.Unlock()
	for _, w := range all {
		if w.fn != nil {
			w.fn()
		}
	}
}

// publishWake 把「叫醒界面」这一支重新装上，并把同一支递给登录和下载那两摊。
//
// n 是登记完之后的条数，由调用方在**还拿着 wakeMu** 的时候数出来——在这里重数
// 一次就得再拿一次那把锁，而那把锁和 `a.mu` 是分开的（见 App 里的注释）。
func (a *App) publishWake(n int) {
	a.mu.Lock()
	if n == 0 {
		a.wake = nil
	} else {
		a.wake = a.wakeAll
	}
	acct := a.account
	dl := a.downloads
	a.mu.Unlock()
	// 挂登录早于装窗口（挂登录要读盘，装窗口要建窗口），所以挂登录时
	// 这里还是 nil。顺手补一次，否则后台改了登录状态没有一帧会被叫醒，
	// 界面会一直停在旧提示上。下载那一摊同理（`MountDownloads` 也读盘）。
	if acct != nil {
		acct.setWake(a.wakeAll)
	}
	if dl != nil {
		dl.SetWake(a.wakeAll)
	}
}

// Snapshot 返回当前状态的拷贝。
func (a *App) Snapshot() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	// ⚠️ 偏好直接从 `a.streaming` 读，**不能**调 `a.AudioQuality()`：那把锁
	// 已经在手里了，sync.Mutex 不可重入，那样写是当场死锁。
	pref := AudioQualityAuto
	if a.streaming != nil {
		pref = BiliToPref(a.streaming.Quality)
	}
	// 🔴 **「正在重连」那一档要去问源**（见 `reconnect.go`）：源那一半住在
	// 源那边，应用只问当前在管的那一两条流——所以一条被丢掉的源抢不走它。
	attempt, retrying := a.reconnectingLocked()
	return State{
		HasTrack: a.player != nil,
		Title:    a.title,
		Path:     a.path,
		Source:   a.sourceLocked(),
		CoverURL: a.cover,
		Playing:  a.playing,
		Stopped:  a.stopped,
		// ⚠️ **「这一串放完了」和「用户按了停止」是两件事**（票 #319）：前者是
		// 队列自己走完了（**没有下一首了**），后者是用户叫停的（队列里还有歌）。
		// 界面上要把它们说成两句不同的话，所以这一栏不能只是 `a.stopped`。
		//
		// 判据是**两栏一起读**：`stopped` 说「停住了」，`wantPlay` 说「用户还
		// 想放」。**只有 `onTrackFinished` 的 `AdvanceStop` 会留下「停住了、
		// 而用户仍想放」这个组合**——`App.Stop` 是把两栏一起落的，而其余几处
		// （重新播放、拖动进度条、清空当前曲目）会把 `stopped` 收掉。
		QueueFinished: a.stopped && a.wantPlay,
		Position:      a.position,
		Duration:      a.duration,
		Volume:        a.volume,
		Muted:         a.muted,
		Rate:          a.rate,
		Err:           a.errText,
		ErrDetail:     a.errDetail,
		Loading:       a.loading,
		LoadingTitle:  a.loadingTitle,
		// 🔴 **「正在重连」那一档要去问源**（见 `reconnect.go`）：源那一半住在
		// 源那边，应用只问当前在管的那一两条流——所以一条被丢掉的源抢不走它。
		Reconnecting:     a.reopenInFlight || retrying,
		ReconnectAttempt: attempt,
		CanRetry:         a.errRetryable,
		BiliURL:          a.biliURLLocked(),
		Quality:          a.quality,
		QualityPref:      pref,
		Artist:           a.artist,
		ArtistMid:        a.artistMid,

		// 播放模式在队列自己的锁里，不在 `a.mu` 里——它是队列的形状，不是
		// 播放器的状态。顺着一句一句取：`a.mu` 已经拿在手里，而 `a.queue`
		// 那把锁是**另一把**，两者没有嵌套，不会死锁。
		PlayMode:               a.queue.PlayMode(),
		KeepPagesOrderInRandom: a.queue.KeepPagesOrderInRandom(),
	}
}

// sourceLocked 报当前这一首的来源。**必须在持锁时调**。
//
// 🔴 **判据是 `a.path`**：本地文件那一路（`App.Open`）把路径写进去，在线那一路
// （`online.go` 的 `runOnline` 装上播放器那一段）把它清空——而**写标题和清路径
// 是同一段代码**，所以这一栏和 `Title` 永远是同一首的歌。
//
// ⚠️ **别改用 `a.online != nil` 判在线**：取流失败的时候 `a.online` 从没装上
// （那条流压根没建起来），而那一首**就是**在线的歌——判据换成它的话，用户听
// 不到声音的那几秒里播放条会以为自己在放本地文件，于是「下载」那一枚不见了。
//
// 空串 = 没有歌（`a.player == nil`）。
func (a *App) sourceLocked() string {
	if a.player == nil {
		return ""
	}
	if a.path != "" {
		return SourceLocal
	}
	return SourceOnline
}

// biliURLLocked 是当前这一首在 B 站上的地址。**必须在持锁时调**。
//
// 正在放的那一首（`a.online`）优先；还没有放上（取流失败了）就用**用户
// 刚才想放的那一首**（`a.pending`）。
//
// 本地文件在 B 站上没有对应的东西，而没有稿件身份的在线条目（没挂投稿的
// 那种）也没有——那两种都给空串。
func (a *App) biliURLLocked() string {
	if a.online != nil {
		return BiliLink(a.online.item)
	}
	// 🔴 **取流失败的时候 `a.online` 从没装上**（那条流压根没建起来），
	// 而用户想听的那一首在 `pending` 里。「在 B 站里听它」正是失败之后
	// 用户能做的事，地址得从那儿来——不然那枚键压根不会出现。
	return BiliLink(a.pending)
}

// setCover 记下当前这首的封面地址。
//
// 值不变就不叫醒界面：本地文件的封面是空串，每一次换歌、每一次入队都会走到
// 这里，为一个没变的值多要一帧是白要的。
func (a *App) setCover(url string) {
	a.mu.Lock()
	if a.cover == url {
		a.mu.Unlock()
		return
	}
	a.cover = url
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// Open 打开一个本地音频文件并开始播放，上一首连同它的文件句柄一起关掉。
func (a *App) Open(path string) error { return a.openLocal(path, 0, openPlay) }

// openMode 是「打开一个本地文件」时的那一档。
//
// 为什么是一档而不是两个布尔：那两个布尔（`paused` / `quiet`）是**一件事**的两
// 半——「恢复」这一档既要**不出声**，又要**失败时安静**。分成两个布尔的话迟早
// 会有一处只记了其中一半（而那一半正好是「启动时冒一句红字」）。
type openMode int

const (
	// openPlay 是正常打开：放起来，打不开就在播放条上说一句人话。
	openPlay openMode = iota
	// openRestore 是「恢复上次听到哪儿」：停在 at 那个位置、**不出声**，
	// 打不开就**安静跳过**（一句红字都不冒）。
	openRestore
)

// openLocal 是「打开一个本地音频文件」的那一条路。
//
// ⚠️ **为什么不能「先放再停」**：出声是 `p.Play()` 那一刻就发生的，而启动时
// 突然出声是最糟的一种（用户刚打开应用，房间里突然有声音）。所以恢复那一趟
// 从头到尾都不 `Play()`。
func (a *App) openLocal(path string, at time.Duration, mode openMode) error {
	paused := mode == openRestore
	p, err := audio.Open(path)
	if err != nil {
		if paused {
			// 🔴 **恢复那一趟：安静跳过**。
			//
			// 票 #347 的验收写的是「队列里的东西已经取不到了 / 路径没了 /
			// 条目对不上时，安静地跳过那一条」——而「文件在、但解不开」也是
			// 同一种「取不到」：用户换了个机器、文件被别的东西改了、编码是这台
			// 机器解不了的。
			//
			// ⚠️ **不许在播放条上冒一句红字**：启动时用户什么都没做就被骂一句
			// 「打不开」，那是应用在说自己，不是在说给用户听。在线那一首取不到
			// 流就是这么处理的（见 online.go 的 failOnline）。
			a.mu.Lock()
			a.pending = Item{}
			wake := a.wake
			a.mu.Unlock()
			a.logf("biu: 恢复上次那一首放不起来（安静跳过）：%q：%v", path, err)
			if wake != nil {
				wake()
			}
			return err
		}
		a.mu.Lock()
		a.errText = err.Error()
		// 悬停提示里给的是完整那一句（见 State.ErrDetail）。
		a.errDetail = err.Error()
		// 🔴 **本地文件打不开是不可重试的**：重试是「再取一次流」，而这一首
		// 连流都没有（它在你的磁盘上，坏了就是坏了）。留着上一句的
		// `errRetryable` 的话，界面上会给出一枚按了也没用的「重试」。
		a.errRetryable = false
		// ⚠️ **`pending` 也清掉**：用户现在面对的是**这个本地文件**，而
		// `pending` 里可能还留着上一首在线的歌——留着的话，播放条上那枚
		// 「在 B 站打开」会指向**另一首歌**（和这句错一点关系都没有）。
		a.pending = Item{}
		wake := a.wake
		a.mu.Unlock()
		// 念出去（无障碍）：本地文件打不开这件事，**读屏软件用户看不见**——
		// 播放条上那一行字他们看不到。
		//
		// ⚠️ **说人话，不念英文系统错**：读屏软件是**逐字念**的，而
		// `err.Error()` 是 "open /Users/x/a.m4a: no such file or directory"。
		a.announce("播放失败：" + announceErrText(err))
		if wake != nil {
			wake()
		}
		return err
	}

	a.mu.Lock()
	old := a.player
	// 倍率和音量一样是「用户选的那个值」，换歌之后照旧——见 App.rate 那段。
	//
	// ⚠️ **送进去的是增益，不是音量那一格**：静音的时候增益是 0，而音量那一格
	// 记的还是用户设的值（取消静音放回去的就是它）。
	p.SetVolume(a.gainLocked()).SetRate(a.rate)
	a.player = p
	a.online = nil
	// 🔴 **代次要加**：本地文件和在线流是**两条并行的路**，而在线那一路是后台
	// 建的（取流要几百毫秒的网络往返）。不加的话，用户点了本地文件之后，那条
	// 在途的在线流照样会装上来——把本地文件顶掉，界面上响的是用户没点的那首。
	// 那一头认的就是这个代次（`online.go` 的 `runOnline`）。
	a.onlineGen++
	// 「正在取流」这一档到此为止：本地文件是**同步**开的，开完就在放，没有
	// 「还在准备」这一段。
	a.endLoadingLocked()
	a.quality = ""
	a.reportedErr = nil
	a.title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	a.path = path
	// 手上只有一个本地路径，没有封面地址（见 State.CoverURL）。队列里的项自带
	// 封面的话，App.play 会在开成功之后补上。
	a.cover = ""
	// 本地文件没有艺人（Biu 上游也不读内嵌标签，见 media.go 的 setArtist）。
	// 队列里的项自带的话，App.play 会在开成功之后补上。
	a.artist = ""
	a.artistMid = 0
	a.duration = p.Duration()
	// 位置：正常打开是 0；恢复那一趟是 at。
	//
	// ⚠️ **夹进 [0, 时长]，但时长拿不到的时候不夹**：时长是 0 的时候夹下去就把
	// 位置清成 0 了——恢复出来的那一首会停在开头，而用户明明停在中间。
	// （时长拿不到不算致命，见 `internal/service/audio/extaf` 的 Len()。）
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
		// 的时候用户还没按过——这一格要和一个普通的暂停**一模一样**（拖了进度
		// 条不会顺手播起来，后台时钟也不会因为它每一拍都叫醒界面）。
		//
		// 用户按了播放之后 `Toggle` 会把它记回 true（见上面那段），于是这一首
		// 放完之后队列照样往下走。
		a.wantPlay, a.playing = false, false
	} else {
		a.wantPlay, a.playing = true, true
	}
	a.stopped = false
	a.clearErrLocked()
	// 🔴 **「正在重连」也收掉**（票 #315）：放本地文件 = 上一首在线歌的那场
	// 重连到此为止。不收的话那一行会一直亮着，而本地文件压根没有在重连。
	a.clearReconnectLocked()
	// ⚠️ **本地文件把 `pending` 让出来**：那一栏记的是「在线的那一首」，
	// 而本地文件在 B 站上没有对应的东西。留着上一首在线歌的话，播放条上
	// 那枚「在 B 站打开」会指向另一首歌。
	a.pending = Item{}
	title := a.title
	wake := a.wake
	a.mu.Unlock()

	// 换了一首 → 落盘那一格该跟着变（见 playback_state.go）。
	a.markPlaybackDirty()

	// 念出去（无障碍）：**换了一首**这件事读屏软件用户看不见——他们没有耳朵
	// 去等歌响起来，而界面上那行歌名在播放条上，不在焦点上。
	//
	// ⚠️ **本地文件不说「加载完成」**：它是同步开的，没有「还在准备」那一段
	// （见上面 `endLoadingLocked` 那句注释），而在线那一段是有的（见 online.go）。
	//
	// ⚠️ **歌名是解锁前拿的**：解锁之后再读 `a.title` 是竞态（别的 goroutine
	// 可能正在换歌）。
	//
	// ⚠️ **恢复那一趟说的是另一句话**：「正在放《X》」对一首**没有在放**的歌
	// 是一句谎话，而读屏软件会照着念给用户听。
	if paused {
		a.announce("已恢复上次的位置：《" + title + "》")
	} else {
		a.announce("正在放《" + title + "》")
	}

	// 在途的那条在线流也掐掉（代次加了，那一头会自己走人——但它自己走人要等
	// 网络那一头先回话，而这里可以立刻停）。
	//
	// ⚠️ **锁外面调**：它要拿 `a.mu`，而那把锁上面已经拿着。
	a.abortPendingSource()

	// 先把新歌换上，再关旧的：换歌期间 tick 拿到的一直是有效的播放器。
	if old != nil {
		old.Close()
	}
	if paused {
		// 停在那个位置，**不出声**。
		if pos > 0 {
			p.Seek(pos)
		}
		p.Pause()
	} else {
		p.Play()
	}
	a.publishMedia()
	// 歌词跟着换歌一起换：先重置成默认值，再从缓存恢复（见 lyrics.go）。
	a.setLyricTrack(LocalItem(path))
	if wake != nil {
		wake()
	}
	return nil
}

// Toggle 在播放和暂停之间切换。播放到头之后按它是重头播。
func (a *App) Toggle() {
	a.mu.Lock()
	p, wake := a.player, a.wake
	a.mu.Unlock()
	if p == nil {
		return
	}
	if p.Playing() {
		p.Pause()
	} else {
		p.Play()
	}

	a.mu.Lock()
	a.playing = p.Playing()
	if !a.playing {
		a.wantPlay = false
	} else {
		// 🔴 **恢复播放要把 wantPlay 记回 true**：它是「用户想放这一首」，而刚才
		// 那次暂停把它落成了 false。它要是留在 false，两处会跟着坏：
		//
		//   - 这一首放完之后**队列不会往下走**（`onTrackFinished` 认的就是
		//     wantPlay，用户按了播放却停在原地）。而那一句是**静默**的：队列
		//     不往下走，而界面上一个字都没有——既不是「已停止」，也不是
		//     「这一串放完了」（票 #352；回归测试在
		//     `internal/app/toggle_resume_test.go`）。
		//   - 拖了进度条之后不会接着播（「往回拖必须能接着播」是验收项 5，
		//     `internal/app/app.go` 的 ScrubEnd 那段注释写着这一句）。
		//
		// ⚠️ **别把它当成「播放器此刻在不在响」**：那一栏是 `playing`。这一栏
		// 答的是「用户想不想听」——两者分开正是为了分清「暂停」和「放完了」。
		a.wantPlay = true
	}
	// 按播放把一首停住的歌放起来时，「停过」这件事就过去了。
	if a.playing {
		a.stopped = false
	}
	a.mu.Unlock()
	// 用户动了一下 → 落盘那一格该跟着变（见 playback_state.go）。
	a.markPlaybackDirty()
	// 锁屏上的播放/暂停按钮是跟着状态走的，这一句在 App/media.go 的说明里。
	a.publishMedia()
	if wake != nil {
		wake()
	}
}

// SetVolume 设音量，1 是解码出来的原样，0 是没声。
//
// ⚠️ **设一个非零的音量等于取消静音**：用户按音量键、滚滚轮、拖滑块，说的都是
// 「我要出声」——静音着不动的话，按了音量加还是没声音，那一格就成了摆设
// （mpv 和 VLC 都是这样：动了音量就取消静音）。
//
// ⚠️ **设成 0 等于静音**：音量 0 那一档和静音在听感上是一件事（图标也是同一
// 张），而落盘的时候「用户自己拉到 0」和「按了静音」分不开——分开的那一格是
// `Muted`，而音量那一格记的是用户设的值。
func (a *App) SetVolume(v float64) {
	v = min(max(v, 0), 1)
	a.mu.Lock()
	if v <= 0 {
		a.volume = 0
		a.muted = true
	} else {
		a.volume = v
		a.muted = false
	}
	gain := a.gainLocked()
	p, wake := a.player, a.wake
	a.mu.Unlock()

	if p != nil {
		p.SetVolume(gain)
	}
	if wake != nil {
		wake()
	}
	a.persistVolume()
}

// ToggleMute 静音 / 取消静音。
//
// 静音**不动音量**：取消静音放回去的就是用户设的那一档（落盘的也是它）。
//
// ⚠️ **它切的是「现在有没有声」**：音量本身是 0（用户自己把滑块拉到最下）的时候
// 按它，是**取消静音**——那一档本来就是没声，按一下让它有声。
//
// ⚠️ **「压根没有音量可放回去」时给一半**（`DefaultUnmuteVolume`）：那多半是
// 用户自己把滑块拉到最下过的，或者是第一次运行——按了「取消静音」，听到的应该
// 是一半声音，不是 0。
func (a *App) ToggleMute() {
	a.mu.Lock()
	if a.muted || a.volume <= 0 {
		// 现在是没有声的：取消静音，把音量放回去。
		a.muted = false
		if a.volume <= 0 {
			a.volume = DefaultUnmuteVolume
		}
	} else {
		a.muted = true
	}
	gain := a.gainLocked()
	p, wake := a.player, a.wake
	a.mu.Unlock()

	if p != nil {
		p.SetVolume(gain)
	}
	if wake != nil {
		wake()
	}
	a.persistVolume()
}

// gainLocked 是**真正送进播放器的增益**。**必须在持锁时调**。
//
// 静音的时候是 0——而音量那一格（`a.volume`）一动不动：那是「用户设的是多少」，
// 和「现在有没有声」是两件事。
func (a *App) gainLocked() float64 {
	if a.muted {
		return 0
	}
	return a.volume
}

// persistVolume 把音量（和静音）写回设置。
//
// ⚠️ **锁外面调**：它要去 store 那边拿锁，还要叫醒界面（`UpdateSettings` 末尾
// 调 `a.invalidate`，而那一句要拿 `a.mu`）——拿着 `a.mu` 调它就是当场死锁。
func (a *App) persistVolume() {
	a.mu.Lock()
	vol, muted := a.volume, a.muted
	a.mu.Unlock()

	_, _ = a.UpdateSettings(func(s *Settings) {
		v := vol
		s.Volume = &v
		s.Muted = muted
	})
}

// SetRate 设播放倍率，夹在 [audio.MinRate, audio.MaxRate]（0.5~2.0）。1 是原速。
//
// 音高**不变**：时间轴是在音频层拉长/压短的（WSOLA，见
// `internal/service/audio/rate.go`），不是重采样。
//
// 这一层记着它，是因为它是**用户的一个选择**而不是某一首歌的属性：换歌、
// 换流重开之后都得照旧。没歌的时候也照收——用户先按了 2 倍速再点歌，
// 那首歌就应该从第一秒起就是 2 倍速。
func (a *App) SetRate(r float64) {
	r = audio.SnapRate(r)
	a.mu.Lock()
	a.rate = r
	p, wake := a.player, a.wake
	a.mu.Unlock()

	if p != nil {
		p.SetRate(r)
	}
	if wake != nil {
		wake()
	}
}

// ScrubStart 是进度条被按下的那一刻。
func (a *App) ScrubStart() {}

// ScrubEnd 是松手那一刻，跳到 fraction（0..1）对应的位置。
//
// 松手后会按用户意图决定要不要接着播：拖到末尾播放机会自然结束，往回拖必须
// 能接着播，否则会停在那儿不动。
func (a *App) ScrubEnd(fraction float64) {
	a.mu.Lock()
	p, dur := a.player, a.duration
	wantPlay := a.wantPlay
	a.mu.Unlock()
	if p == nil || dur <= 0 {
		return
	}
	t := min(time.Duration(fraction*float64(dur)), dur)
	p.Seek(t)
	if wantPlay {
		p.Play()
	}

	// 立刻把位置改过来：暂停状态下 tick 不跑，界面下一帧读的就是这里写的值。
	a.mu.Lock()
	a.position = t
	a.playing = p.Playing()
	// 用户动过时间线了，「停到开头」这件事就过去了。系统那边跟着回到
	// playing / paused——它看到的进度条此刻在 t 上，状态却说「停在开头」，
	// 那是两句互相打架的话。
	a.stopped = false
	wake := a.wake
	a.mu.Unlock()
	// 用户拖了进度条 → 落盘那一格该跟着变（见 playback_state.go）。
	//
	// ⚠️ **这一格算「用户动了一下」**：它走的是那条「尽快写、但至少隔半秒」的
	// 路（见 savePlaybackIfDue），所以拖完不用等 5 秒那个窗口。
	a.markPlaybackDirty()
	// 锁屏上那根进度条和界面上这根是同一根：不报的话它在原地漂着，
	// 直到下一次校正（mediaPublishEvery）才跟上。
	a.publishMedia()
	if wake != nil {
		wake()
	}
}

// ClearTrack 停掉播放并把当前曲目清空，**队列不动**。没有在放的时候什么都不做。
//
// 「清空播放列表」和「把正在放的那一首删掉」最后都落到这里。停了还留着状态是
// 不行的：那之后播放条上还是那首歌的名字，抽屉里已经没有它了——用户看到的界面
// 和它说的事实对不上，而且再按「播放」会试图重开一个用户已经删掉的文件。
//
// ⚠️ **和 `Stop` 不是一件事，别把两个混起来**（`media.go` 里的那个）：`Stop` 是
// 锁屏和控制中心上那个方块键——暂停并回到开头，这首歌还在、下一句还能接着听；
// 这里是「这一首到此为止，而且界面上不许再留着它」。
//
// ⚠️ **不停后台时钟**。clock 是 Close 的事（进程要退了才停）；这里停了还要能再
// 放下一首。
func (a *App) ClearTrack() {
	a.mu.Lock()
	p := a.player
	// 🔴 位置和时长要在**清零之前**抄下来：下面那句 `stopTrack` 要用它们
	// 去结掉播放记录那一轮，而清零之后再取就只剩 0 了——B 站那边这一条会
	// 变成「从头开始听就停了」，而用户明明听到了 3 分 20 秒。
	at := a.position
	a.player = nil
	a.playing = false
	// 「停到开头」这件事也就过去了：没有播放器就是「什么都没有」，不是「停在开头」
	// （mediaSnapshotLocked 那边 player 为 nil 时回的就是 StateNone）。
	a.stopped = false
	// ⚠️ **代次要加**：在途的在线流回来时只对一遍代次（`online.go` 的 runOnline
	// / failOnline / noteQuality 都对）。不加的话，用户把正在放的那首删掉之后
	// 那条流才建好——它照样会装上来，队列是空的、播放器却在放一首不在队列里的歌，
	// 正是这里要避免的那件事。
	a.onlineGen++
	a.online = nil
	// 「正在取流」这一档也收掉：清掉当前曲目 = 用户不要这一首了，那条在途的
	// 取流也作废了（代次已经加了，那一头会自己走人）。
	a.endLoadingLocked()
	a.quality = ""
	a.reportedErr = nil
	a.title, a.path, a.cover = "", "", ""
	a.artist = ""
	a.artistMid = 0
	a.position, a.duration = 0, 0
	a.wantPlay = false
	a.clearErrLocked()
	// 🔴 **「正在重连」也收掉**（票 #315）：清掉当前曲目 = 用户不要这一首了，
	// 那场重连到此为止。
	a.clearReconnectLocked()
	// 什么都没有了：「当前这一首」也就没有了（不然播放条上那枚
	// 「在 B 站打开」会指着上一首）。
	a.pending = Item{}
	wake := a.wake
	a.mu.Unlock()

	// 在途的那条在线流也掐掉（理由同 `Open` 里那一句：锁外面调）。
	a.abortPendingSource()

	// 「这一首到此为止」也是终止：播放记录那一轮到此结束（见 playreport.go
	// 的「一、什么时候发」）。它和 `Stop` 是同一件事的两个入口。
	a.stopTrack(at)

	// 先把状态摘干净，再关解码器：和 Open 换歌同一个道理，关的过程中 tick 拿到
	// 的不会再是这一个。
	if p != nil {
		p.Close()
	}
	// 锁屏上那一首已经没有了，跟系统说一声（media.go 的那只手；没装就是空操作）。
	a.publishMedia()
	if wake != nil {
		wake()
	}
}

// Close 停掉播放、关掉解码器和文件句柄，并停掉后台时钟。可以重复调用。
func (a *App) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	p := a.player
	a.player = nil
	a.playing = false
	// 「正在取流」这一档收掉，顺手把超时那个闹钟停掉——应用都要退了，
	// 让它再响一次没有意义（而且它会去写一个已经没人看的状态）。
	a.endLoadingLocked()
	dl, dlPath := a.downloads, a.downloadQueueFile
	lib := a.localMusic
	upd := a.updates
	rep := a.report
	a.report = nil
	at := a.position
	a.mu.Unlock()

	close(a.done)

	// 「上次听到哪儿」这一格：**退出前强制落一次**。
	//
	// ⚠️ 落盘是防抖合并写，最后那一下有可能还等在退避窗口里——用户听了一
	//会儿直接退出，那一格就丢了，而丢了的症状是「重启之后回到空队列」。
	//
	// 排在 `close(a.done)` 之后：后台时钟已经停了，不会再有别的 goroutine 来
	// 改这一格，落下去的就是**最后那个状态**。
	a.SavePlayback()

	// 播放记录先结掉这一轮，再关那条 goroutine。
	//
	// ⚠️ **顺序不能反**：退出这一刻用户可能刚听到一半，那条 PlayEnd 是这一轮
	// 最后一次机会，而 `close` 一执行它就不会再发了（历史记录里那一条就停在
	// 上一个采样点上）。
	//
	// ⚠️ 锁外面调：`close` 要等一条 goroutine，而那条 goroutine 每一拍都要拿
	// `a.mu` 问设置和登录态——锁里面等就是死锁。
	if rep != nil {
		rep.finishOnClose(at)
	}
	if p != nil {
		// Player.Close 会把字节源一起关掉（在线流那条路）。
		p.Close()
	}
	// 🔴 **在途的那条字节源也要关**：播放器还没建起来的时候它已经在下载了，
	// 而上面那句关的是**播放器**——关不到它。没有这一句的话，一次卡死的取流
	// 会在应用退出之后还在下载（临时文件也一起留在盘上）。
	a.abortPendingSource()
	a.mu.Lock()
	a.online = nil
	a.mu.Unlock()
	// 告诉系统「没有歌了」。不做这一步的话，退出去之后锁屏上还会留着
	// 一首已经不存在的歌，而控制中心的播放键按下去还能收到命令。
	a.publishMedia()
	if a.account != nil {
		a.account.Close()
	}
	// 歌词偏移/字号那一次防抖写可能还欠着，退出前落完。
	a.closeLyrics()
	// 下载要**停掉并落盘**。停掉是为了别让在飞的请求在应用退出时还在往盘上
	// 写；落盘是因为队列文件是「下到哪儿了」的唯一一份，丢了用户重启就得
	// 从头下。写失败不往上报错：退出路径上没有用户在看。
	if dl != nil {
		_ = dl.Close(dlPath)
	}
	// 本地音乐的文件监听也要关：它每个根一个 goroutine，不关的话退出之后
	// 还在那儿收着目录事件。
	if lib != nil {
		lib.close()
	}
	// 更新这一摊也要收尾，而且**排在最后**：它要取消在下的那一次下载并等它真的
	// 停了（SPEC 118「下载中退出应用，状态一致不损坏」）。等的时候不能有别的
	// goroutine 还在往界面上投递状态——界面已经拆了。
	if upd != nil {
		upd.close()
	}
}

// tick 是进度条的时钟：把播放器的当前位置读回来，界面上的进度条才会自己走。
//
// 空闲时（没有歌，或暂停）不唤醒，白刷的帧是白费的。
func (a *App) tick() {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-t.C:
			now := time.Now()
			a.mu.Lock()
			p := a.player
			live := a.wantPlay || a.playing
			wake := a.wake
			a.mu.Unlock()

			// ⚠️ `p.Position()` 必须在 `a.mu` **外面**调，理由和下面那句
			// `checkPlayback` 是同一条（它自己要拿 `a.mu`）：
			//
			// 它要过变速层那把锁，而那一把锁可能被一次**网络读**持着——在线流
			// 正在从 CDN 下这一段，CDN 卡一下锁就卡一下。而界面每一帧的第一件
			// 事就是 `Snapshot()`（要 `a.mu`），于是 CDN 卡 2 秒，整个窗口就跟着
			// 卡 2 秒；`StreamSource` 的卡死判定是 20 秒（`stream_source.go` 的
			// `defaultStallTimeout`），最坏就是**窗口卡 20 秒**。
			//
			// 变速层那一边已经改成读内层时不持锁了（`rate.go` 的 `readInner`），
			// 这一句是把最后那一层放大也去掉：即便它还要等，也等的是自己一个
			// goroutine 的事，不占着界面每帧都要的那把锁。
			var pos time.Duration
			var playing bool
			// finished 是「这一首真的放完了」（字节读到头、没有报错）——和
			// 「暂停」是两回事（暂停时字节没到头）。
			finished := false
			if p != nil && live {
				pos, playing = p.Position(), p.Playing()
				finished = !playing && p.Ended() && p.Err() == nil
			}

			a.mu.Lock()
			// ended 是「这一路从在放变成不在放」——绝大多数场合是歌放完了。
			//
			// ⚠️ 它要认准「是这一台播放器」：换歌时锁里就把 `a.player` 换成
			// 新的了，拿旧那台的「不在放」去判新歌放完了，是白报一次。
			ended := false
			// 位置和 playing 是**这一台播放器**的：这中间换歌了（换歌在锁里把
			// `a.player` 换成新的），就不能把旧那台的位置写到新歌头上。
			if p != nil && live && a.player == p {
				ended = a.playing && !playing
				if finished {
					// ⚠️ **放完了就把位置报成 0**：这一串放完了之后进度条该
					// 回到开头（见 AdvanceStop 那支），而播放器那边的 Seek(0)
					// 还没生效——这一拍读到的是**放完之前**的位置（实测是歌曲
					// 的全长），报出去就成了「进度条卡在末尾」。
					a.position = 0
				} else {
					a.position = pos
				}
				a.playing = playing
			}
			// 锁屏上的进度条是**系统自己外推**出来的（Apple 头文件原话），
			// 而外推的是它的时间：我们这边卡一下解码、换一次流、音频设备
			// 被抢占，它都不知道。所以每隔几秒把真位置报回去校正一次。
			//
			// ⚠️ 只在**在放**的时候校正：暂停时系统不外推，而位置一变
			// publishMedia 那一次就已经报过了。
			//
			// ⚠️ 而「歌放完了」只能靠 ended 那一支：结束那一刻恰好既不是
			// 「语义变化」（歌名时长什么都没变），校正也已经因为不再在放
			// 而停了。漏了它的症状是锁屏上永远停在「正在播放」——界面这边
			// 一切正常，只有控制中心和锁屏是错的。
			publish := a.mediaSink != nil && a.playing && now.Sub(a.mediaAt) >= mediaPublishEvery
			if publish {
				a.mediaAt = now
			}
			a.mu.Unlock()

			// 「上次听到哪儿」这一格：在放的时候位置一直在变，所以按节奏落一次
			// （见 savePlaybackIfDue）。**不在放的时候它落得更勤**——用户按了
			// 暂停或者拖了进度条，那一下就该马上落（见那份文件里的那张表）。
			a.savePlaybackIfDue(now)

			if publish || ended {
				a.publishMedia()
			}
			// 「往 B 站写播放记录」在这一拍上对一次账（见 playreport.go）。
			//
			// ⚠️ **`ended` 不是「放完了」**：解码失败和断流也是「从在放变成不
			// 在放」。拿它去发 `played_time = -1`（B 站靠它把一条标成「看完」）
			// 等于往用户的账号里写一句他没做过的事，而那一条会在他的历史记录
			// 页上一直摆着。真的放完了必须是「不在放了」**且**这一台没有报错。
			// 那是 `TestACorruptFileNeverLooksLikeItFinished` 那个道理的另外
			// 一半：那边量「位置到没到头」，这边量「有没有报错」，两半合起来
			// 才分得出「放完了」和「挂掉了」。
			//
			// 这里每一拍都调（哪怕此刻没有歌）：它只读内存、往队列里塞一条，
			// 没有在报的时候立刻回来，绝不碰网络。
			//
			// 多读一次 `p.Err()` 不花钱：解器把它存好了（`decoder.go` 那个
			// 错误记录器带自己的锁），底下两行的 `checkPlayback` 读的是同一个值。
			//
			// 「在不在放」它自己去 `a.playing` 读，不收这里那个局部变量——
			// 理由见 `reportTick` 那段。
			a.reportTick(ended && p != nil && p.Err() == nil)

			if live && wake != nil {
				wake()
			}
			// 「自然放完」才按播放模式往下走一档。**只判 ended 不够**：
			// 暂停、停止、关闭在 `Playing()` 眼里和放完了一样（见下面那句）。
			//
			// 补的两句各挡一样：
			//
			//   - `Ended()`：这一路的字节真的读到过头了。解码失败/断网时
			//     播放器也停了，但那是**因为坏了**，不是放完了。
			//   - `Err() == nil`：和 Ended 互补的那个（见 decoder.go）。这一句
			//     是显式地把两件事都摆出来，省得读代码的人去推「Ended 为真
			//     时 Err 是不是一定为 nil」。
			//
			// 用户按暂停/停止走不到这里：`Toggle` 的**暂停那一半**、`Pause`、
			// `Stop` 都把 wantPlay 落成 false，下一次 tick 的 `live` 已经是 false。
			//
			// ⚠️ **而 `Toggle` 的「按回来」那一半要把 wantPlay 记回 true**
			// （票 #352）：只落 false 不记回来的话，这一栏就一直 false，而
			// `onTrackFinished` 拿它当「要不要往下走」的判据 → 队列不往下走 →
			// 界面上什么话都没有。
			//
			// 放在锁外面调：那两样要拿解码器自己的锁，还可能去碰字节源。
			if ended && p != nil && p.Ended() && p.Err() == nil {
				a.onTrackFinished()
			}
			// ⚠️ 这一句**不能省**。`Playing() == false` 有五种情况：暂停、
			// 停止、关闭、播完、解码失败（`mygo/audio/audio.go`）。前四种
			// 什么都不用做，第五种必须变成播放条上的一句话——不然一次断网
			// 表现成「歌忽然放完了」，用户以为歌放完了。
			//
			// ⚠️ 这句在 onTrackFinished **之后**调：换流重开
			// （`refreshOnlineNow`）要从当前这首的位置接着放，而那一次
			// `Ended()` 是 false（换流走的是 seek，不是读到头），所以不会有
			// 冲突；但顺序摆在这里，「有错就换流接着放」才读在「放完了就下一首」
			// 后面——反过来会让人以为换流时被跳过一首。
			//
			// 放在锁外面调：它自己要拿 `a.mu`，而 `Player.Err()` 可能去碰
			// 字节源。
			a.checkPlayback()
		}
	}
}
