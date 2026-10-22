// 「这一首放不了」的分类：说清**是哪一种**、**为什么**、**还能怎么办**。
//
// # 为什么要有这一层
//
// 「这一首还不能放」这句话把**两件正好相反的事**混成了一句：
//
//	这一条压根没有可播的音频流	合集 / 剧集 / 音乐库条目 / 没挂稿件的推荐
//	这一条有音频流，只是没取到	网络抖了、风控拦了、地址过期了
//
// 用户分不清这两件，是因为界面上它们长得一模一样（都是「打不开：……」）。
// 而它们要走的路**正好相反**：第一件重试一万次也没用，第二件点一下重试就好。
// 把它们混成一句的直接后果是：网络断了一下，用户以为自己点的这一首坏了，
// 于是去 B 站删收藏、清缓存、换设备——而真正要做的是等两秒钟再点一次。
//
// # 判据和「真能放」必须一致
//
// 这一层说的「放得了」和 `app.streamFor` 那两条取流路径必须对上：说放得了的
// 条目，按下去必须真能出声。说放不了的，**界面上就不该给播放入口**——
// 「点了才说」和「压根不给点」在用户那边是两件事，前者像是应用出错了。
// `bili.Media.Playable` 当初就是抄对了判据却没接上 type=12 那一路，
// 于是「可放」在界面上成了一句谎言（见 `internal/ui/rowplayable_test.go`）。
//
// # 「还能怎么办」不是客套
//
// 每一句都要能回答用户下一步做什么。做不到的就别写「还能怎么办」，
// 只把「为什么」说准——**宁可短，不可骗**。
package app

import (
	"errors"
)

// UnplayableKind 是「这一首放不了」的种类。
//
// 分成几种是因为它们**要说的那句话不一样**：合集/剧集说一句，和
// 「B 站说这个稿件没有音频流」说的是两件事，用户照着做的下一步也不同。
type UnplayableKind int

const (
	// UnplayableNoArchive 是「这一条不是投稿，B 站没给它挂能播的稿件」。
	//
	// 首页的推荐单和音乐榜上有些条目是**纯音乐条目**（歌单 / OST），
	// B 站没给它们挂投稿——那种行画得出来而放不了（见 `recommend.go`）。
	UnplayableNoArchive UnplayableKind = iota

	// UnplayableNoSid 是「这是 B 站音乐库里的条目，但它没带音频 id」。
	//
	// 音乐库那一路取流认的是 sid（auid），没有它就无从问起。
	UnplayableNoSid

	// UnplayableNoAudioFlow 是「这一条是合集/剧集那一类，没有单独的音频流」。
	//
	// 它是**当场判得出**的（`bili.Media` 的 type=21/24 → `Item.Type`
	// 是 `"collection"`），所以界面上**点之前就不给播放入口**。
	//
	// ⚠️ **它和「没挂稿件的推荐条目」必须说成两句话**：那一种是「B 站没给
	// 它挂可播的稿件」（去搜同名），这一种是「这一条本来就是合集，里面才
	// 有单集」（打开里面的单集）。用户照着做的下一步不一样。
	//
	// ⚠️ **另一个同名的东西在 `bili` 层**：`bili.ErrNoAudioFlow` 是「B 站
	// 在取流的时候说这一条没有音频流」（实测：音乐区 tid=130 那种把歌单/
	// 活动整个塞进一个投稿的，单 P、三十多分钟）——那一类**点之前判不出来**
	// （判它要一次带签名的网络往返，界面线程上一次都不许有），只能在
	// 按下去之后说准。**两个不是同一件事。**
	UnplayableNoAudioFlow

	// UnplayableNoDownload 是「这一类还没有下载通道」。
	//
	// 音乐库条目只有一个 auid（sid），而下载是**按稿件 + 分 P**取流的，
	// 所以这一类放得了、下不了。放得了不等于下得了——两件事分开说。
	UnplayableNoDownload
)

// Unplayable 是「这一首放不了」这件事，带上为什么和还能怎么办。
//
// 它是一个**带类型的错误**（`errors.As` 找得到），因为调用方要靠类型
// 决定「给不给播放入口」和「给不给重试」，而不是靠匹配句子。
type Unplayable struct {
	// Kind 是种类。界面据此说清「是哪一种」。
	Kind UnplayableKind
	// Why 是**为什么**放不了。空的时候用这一类默认的那句。
	Why string
	// Next 是**还能怎么办**。空的时候用这一类默认的那句。
	Next string
}

// Error 把两半拼成一句给用户看的话。
//
// 拼的顺序是「为什么」在前、「怎么办」在后：用户先要知道出了什么事，
// 才知道该做什么。反过来（先说「请重试」）像是把责任推给用户。
func (e *Unplayable) Error() string {
	why, next := e.Why, e.Next
	if why == "" {
		why = defaultWhy(e.Kind)
	}
	if next == "" {
		next = defaultNext(e.Kind)
	}
	return why + "。" + next
}

// defaultWhy 是这一类默认的「为什么」。
func defaultWhy(k UnplayableKind) string {
	switch k {
	case UnplayableNoArchive:
		return "这一条不是投稿，B 站没有给它挂能播的稿件"
	case UnplayableNoSid:
		return "这一条是 B 站音乐库里的条目，但没有带音频 id"
	case UnplayableNoAudioFlow:
		return "这一条是合集/剧集那一类，B 站没有给它们单独的音频流"
	case UnplayableNoDownload:
		return "这一条是 B 站音乐库里的条目，还没有下载通道"
	}
	return "这一首放不了"
}

// defaultNext 是这一类默认的「还能怎么办」。
//
// ⚠️ 每一句都得是**用户真能做的事**。写不出用户能做的事时，宁可短一句
// 「换一个吧」，也不要写「请稍后再试」——那把一件永远不会成功的事说成
// 网络问题，用户会去查网、换网、等它好，而真正要做的跟网络一点关系都没有
// （这个坑 `collection_download.go` 那条 `ErrNoAudioDownload` 已经踩过一次）。
func defaultNext(k UnplayableKind) string {
	switch k {
	case UnplayableNoArchive:
		return "在 B 站搜同名的歌，或者换成带视频的那一版再放"
	case UnplayableNoSid:
		return "重新收藏一次，或者在 B 站搜同名的歌"
	case UnplayableNoAudioFlow:
		return "打开里面的单集就能放"
	case UnplayableNoDownload:
		return "先在应用里放它，或者去 B 站下载"
	}
	return "换一首放"
}

// unplayableOf 造一个「这一首放不了」的错误。
func unplayableOf(kind UnplayableKind) *Unplayable {
	return &Unplayable{Kind: kind}
}

// errNoAudioFlow 是「合集和剧集那一类没有单独的音频流」。
//
// 🔴 **它是一个 `Unplayable`，而且必须是同一个指针**：判重靠的是
// `errors.Is` 的相等比较，而每次现造一个的话 `errors.Is` 就不成立了。
//
// 为什么单列一个哨兵：这一句在两个出口上（批量下载、单首下载），而两处
// 必须说**同一句话**——用户在同一个界面上对照着看，发现「同一个东西两句话」
// 比两句话都说错更难解释。
//
// ⚠️ **它和 `bili.ErrNoAudioFlow` 是两回事**：那一个是 B 站说的（「这个稿件
// 没有音频流」），这一个是我们判的（「合集和剧集没有单独的音频流」）。
// 名字像是因为说的是同一件事，**别把它们当成同一个**。
var errNoAudioFlow = &Unplayable{Kind: UnplayableNoAudioFlow}

// ErrNoAudioDownload 是「B 站音乐库里的条目（type=12）还没有下载通道」。
//
// 🔴 **它也是一个 `Unplayable`**（kind = `UnplayableNoDownload`）：界面和
// 测试既能 `errors.Is` 认这一类，又能从它身上拿到「为什么 + 还能怎么办」
// 那两半（`AsUnplayable`）。
//
// 同样必须是**同一个指针**（理由同 `ErrNoAudioFlow`）。
var ErrNoAudioDownload = &Unplayable{Kind: UnplayableNoDownload}

// AsUnplayable 从一个错里找出「这一首放不了」那一个。找不到给 nil。
//
// 找的是**类型**不是句子：话会改，而「这一类放不了」这件事不会。
func AsUnplayable(err error) *Unplayable {
	var u *Unplayable
	if errors.As(err, &u) {
		return u
	}
	return nil
}

// errNoSource 是「队列里这一项既没有文件也不是 B 站的条目」。
//
// 它是一个**内部状态不对**的情形（来源没接上），不是「这一条没有音频流」：
// 说成「这一首还不能放：还没有接来源」的话，用户既不知道出了什么事，
// 也不知道自己该干什么。而它其实是**能自己查出来**的一句话——这一项
// 两头都没有。
var errNoSource = errors.New("这一首还没有来源（既不是本地文件，也不是 B 站的条目）。换一首放")

// unplayableReasonOf 报这一项「放不了」的那一个原因。放得了的话给 nil。
//
// ⚠️ **本地文件一律放得了**：它走的是开文件那一路，和取流无关。
// 而 `playableOnline` 只管在线那两类——本地文件落进去会落到「不是投稿」
// 那一支，于是本地文件被说成放不了（症状：本地音乐那一页的「播放」是灰的）。
func unplayableReasonOf(it Item) *Unplayable {
	if it.IsLocal() {
		return nil
	}
	return AsUnplayable(playableOnline(it))
}

// CanPlay 报这一项现在放得了放不了。
//
// **界面上「给不给播放入口」问的就是它**（见 `internal/ui/widgets/list/menu.go`）。
//
// ⚠️ 判据必须和 `app.streamFor` 那两条取流路径一致：说放得了而实际放不了，
// 「可放」就成了界面上的谎言（`bili.Media.Playable` 当初就是那个情况）。
func CanPlay(it Item) bool {
	return unplayableReasonOf(it) == nil
}

// UnplayableReason 报这一项放不了的原因，**带为什么和还能怎么办**。
//
// 放得了的话给空串。界面拿它做两件事：
//
//	菜单里的「播放」那一项该不该给（`CanPlay`）
//	灰掉之后给用户看的说明（这一句）
//
// ⚠️ 这一句是给**还没按下去**的用户看的，所以它得能独立成立——
// 「这一条放不了」后面必须跟着「为什么」和「还能怎么办」，
// 而不是一个只有错误码的句子。
func UnplayableReason(it Item) string {
	u := unplayableReasonOf(it)
	if u == nil {
		return ""
	}
	return u.Error()
}

// UnplayableMenuNote 报这一项放不了时，摆在菜单末尾的那一句说明。
//
// ⚠️ **它比 `UnplayableReason` 短**：系统菜单里一行太长的字会把整个菜单
// 撑宽（macOS 的菜单项不换行），一屏放不下就得出屏。
//
// 但它**仍然两半都有**（「为什么」和「还能怎么办」）——按不动而不说为什么
// 等于让用户猜，而用户猜不出来（他不会知道「没挂稿件」是什么意思）。
func UnplayableMenuNote(it Item) string {
	u := unplayableReasonOf(it)
	if u == nil {
		return ""
	}
	return menuNote(u.Kind)
}

// menuNote 是这一类在**菜单里**的那一句。
//
// 🔴 **它和 `defaultWhy` / `defaultNext` 是同一件事的两种写法**，
// 所以放在一个文件里、挨着改：话漂了的话，用户在菜单里看到的和按下去
// 之后看到的会不一样。
func menuNote(k UnplayableKind) string {
	switch k {
	case UnplayableNoArchive:
		return "这一条不是投稿，放不了。搜同名的歌再放"
	case UnplayableNoSid:
		return "这一条是音乐库条目，缺音频 id。重新收藏一次"
	case UnplayableNoAudioFlow:
		return "这一条是合集/剧集，没有音频流。打开里面的单集就能放"
	case UnplayableNoDownload:
		return "音乐库条目还没下载通道。去 B 站下载"
	}
	return "这一首放不了。换一首放"
}

// UndownloadableMenuNote 是「下不了」时摆在菜单末尾的那一句。
func UndownloadableMenuNote(it Item) string {
	if CanDownload(it) {
		return ""
	}
	return menuNote(UnplayableNoDownload)
}

// CanDownload 报这一项现在下得了下不了。
//
// ⚠️ **本地文件算「下得了」**：那一条在界面上的动作是「在访达里显示」，
// 不是真的下载（见 `widgets/list` 的 `AppActions.Download`）。而它按下去
// 会说清「这一首已经在你磁盘上了」，那句话本来就答了「为什么」。
//
// 在线条目只有**有 bvid** 的才下得了：下载是按稿件 + 分 P 取流的，而音乐库
// 条目只有一个 auid（sid）——它放得了，下不了。
func CanDownload(it Item) bool {
	if it.IsLocal() {
		return true
	}
	return it.Bvid != ""
}

// UndownloadableReason 报这一项下不了的原因，**带为什么和还能怎么办**。
//
// 放得了的话给空串。
func UndownloadableReason(it Item) string {
	if CanDownload(it) {
		return ""
	}
	return unplayableOf(UnplayableNoDownload).Error()
}

// BiliLink 是这一项在 B 站上的地址。什么都没有就返回空串——
// 调用方拿空串不去开浏览器（开一个空白页比什么都不做更让人以为界面卡了）。
//
// 🔴 **只有这一处拼**：播放列表抽屉的行菜单、内容列表那一套菜单、播放条上
// 「在 B 站打开」那一枚键都要它，而拼错一处就是「打开了一个打不开的页面」。
func BiliLink(it Item) string {
	switch {
	case it.Bvid != "":
		return "https://www.bilibili.com/video/" + it.Bvid
	case it.Sid != "":
		return "https://www.bilibili.com/audio/au" + it.Sid
	}
	return ""
}
