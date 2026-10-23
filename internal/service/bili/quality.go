// 音质：把 B 站那些七零八碎的 id 变成界面能说的话。
//
// # 为什么单独一个文件
//
// 取流、音质、错误分类是三件事，但它们共享同一份「B 站的返回长什么样」的
// 知识。拆开的关键不是行数，是**改一处不需要回头改另一处**：音质 id 的
// 含义变了，只动这个文件。
//
// # 两套编号，别混
//
//	Quality      B 站接口自己给的 id（30280 这种）。从接口读到的东西用它。
//	AudioQuality 用户选的偏好（无损 / 高 / 标准）。界面上给的是它。
//
// 混成一个的后果是：界面上会出现「30280」这种数字，而且「用户选了无损」
// 和「B 站给了一条 30280」不再是两件事——降级发生时就没法说清降到了什么。
//
// # 档位从哪来：id 表是**快照**，不是永恒真理
//
// B 站的音质 id 会改。所以这里**不把 id 表当真理**，而是分清两件事：
//
//	**类别**（无损 / 杜比）由**字段**决定：`dash.flac` 就是 FLAC，
//	`dash.dolby` 就是杜比全景声，`dash.audio[]` 才是按 id 分档的有损流。
//	**档位名**（64K / 128K / 192K / 杜比全景声 / Hi-Res 无损）才查 id 表。
//
// 这条分界是本文件最重要的一道防线：查 id 表错了，最坏是**徽章写错**；
// 而连「这条流是不是无损」都靠 id 猜的话，B 站一改 id，无损流就会挂上
// 「杜比 Hi-Res」这种完全不相干的标签——票 #255 的症状就是这个。
//
// # id 表的出处
//
// **公开资料**：社区恢复的接口文档 `docs/video/videostream_url.md`
// （「视频伴音音质代码」一节。原仓 `SocialSisterYi/bilibili-API-collect` 已于
// 2026-01 因律师函关停，此处为镜像 `afiuh/bilibili-API-collect`）。
// 调研报告 `Biu_MyGo/150` §3.1 转引了那张表。
//
// **只读实测**（2026-10-23，票 #255）：只读打 `x/player/playurl`，把 `dash`
// 三个容器里的 id / codecs / bandwidth 记下来（音乐区排行 ~50 个稿件，外加
// 几个带杜比的），结果：
//
//	容器               id     codecs              bandwidth 实测
//	dash.audio[]       30216  mp4a.40.2 / 40.5    33K ~ 66K
//	dash.audio[]       30232  mp4a.40.2            37K ~ 129K
//	dash.audio[]       30280  mp4a.40.2            75K ~ 231K
//	dash.dolby.audio[] 30250  ec-3                768K ~ 1024K
//	dash.flac.audio    30251  fLaC                1609K ~ 1821K
//
// ⚠️ **30257 / 30259 / 30260 不在这张表里**，实测也没在 `dash` 里见过它们。
// 它们出自一份互相抄来抄去的二手映射表，而那份表自己内部就矛盾（同一个
// 30260，一处写 320K、一处写 128K）。留着它们当「已知档位」的话，界面上就
// 会冒出「杜比 Hi-Res」（票 #255 的症状）。认不出来的 id 一律落到 `Label`
// 的兜底：按码率说话。
//
// ⚠️ **这张表会过期**：B 站加档、改档的时候它就旧了。旧了不要紧——认不出来
// 的 id 有兜底，不会报错、不会挂错标签。要紧的是**别把某一时点的表当永恒
// 真理硬编码进去**（那正是本文件以前干的事）。

package bili

import (
	"fmt"
	"sort"
	"strings"
)

// Quality 是 B 站音频流的一档音质，就是接口里的 `dash.audio[].id`。
//
// 只列取流接口真正会返回的。碰到没列出来的（以后 B 站加档）不算错误：
// `Label` 会退回按码率说话，见下面的 Label。
type Quality int

const (
	// QualityUnknown 是「没认出来」。它不是错误，是「B 站加了新档」的意思。
	QualityUnknown Quality = 0

	// QualityLow64 是 64 Kbps。B 站给未登录用户的默认档。
	QualityLow64 Quality = 30216
	// QualityMid128 是 128 Kbps。
	//
	// ⚠️ B 站标称这一档是 **132K**，我们显示「128K」——那是 AAC-LC 这一档的
	// 业界通用叫法（B 站自己的播放器上写「标准 132K」）。指的是同一档。
	QualityMid128 Quality = 30232
	// QualityHigh192 是 192 Kbps。投稿侧能拿到的**最高的有损档**。
	//
	// ⚠️ **投稿侧没有 320K**：320K 是**音乐库**的 `type=2`（`MusicTier320`），
	// 那是第三套编号（见 `MusicTier`）。这里以前写「320K」，是把两套编号
	// 混成一套了（票 #255）。
	QualityHigh192 Quality = 30280

	// QualityDolby 是杜比全景声（`dash.dolby`，codecs 是 `ec-3`）。
	QualityDolby Quality = 30250
	// QualityHiResLossless 是 Hi-Res 无损（`dash.flac`，codecs 是 `fLaC`）。
	//
	// ⚠️ 这个 id 以前叫「杜比 Hi-Res」（`QualityDolbyHiRes`）——**那是错的**：
	// 30251 是**投稿侧的无损流**，和杜比没有关系。挂错了标签、无损判定也跟着
	// 判错（票 #255）。
	QualityHiResLossless Quality = 30251
)

// AudioQuality 是**用户**选的那一档偏好。
//
// 界面上给的是这几档，不是 B 站的 id：用户要选的是「无损」还是「省流量」，
// 「30251」对谁都没有意义。
type AudioQuality int

const (
	// AudioAuto 是默认档：有无损给无损，没有就给能拿到的最好的。
	//
	// Biu 上游的 auto 行为就是这个（`common/utils/audio.ts` 里
	// `quality === "auto" || quality === "lossless"` 都先找 flac）。
	// 两者唯一的区别是：auto 找不到无损不会报错，lossless 会。
	AudioAuto AudioQuality = iota
	// AudioLossless 只要无损。拿不到就报错，不要偷偷降到有损——
	// 用户为无损付了钱，静默降级比报错更糟。
	AudioLossless
	// AudioHigh 只要 192K（投稿侧最高的有损档）。
	AudioHigh
	// AudioMedium 只要 128K。
	AudioMedium
	// AudioLow 只要 64K。
	AudioLow
)

// Label 是这一档偏好在设置页里显示的字。
//
// ⚠️ 不是给 `Quality.Label` 用的：那个说的是「B 站给了什么」，
// 这个说的是「用户要什么」。降级发生之后界面上要同时出现两句时，
// 混起来会看成「用户要无损，系统给了无损」而看不出降级。
func (q AudioQuality) Label() string {
	switch q {
	case AudioLossless:
		return "无损"
	case AudioHigh:
		return "高音质"
	case AudioMedium:
		return "标准"
	case AudioLow:
		return "省流量"
	}
	return "自动"
}

// qualityRank 是「从低到高」的排序键。B 站的 id 本身不是可比的
// （30251 是 Hi-Res 无损、30280 是 192K，数字大小和音质无关），所以要另排
// 一张表。
//
// ⚠️ 表里没有的 id 排在**所有已知档之下**（rank 0），不是中间：
// 认不出来的档位我们不知道它有多好，宁可当成最低的那档让别的档胜出，
// 也不要凭 id 的大小猜。
var qualityRank = map[Quality]int{
	QualityLow64:         1,
	QualityMid128:        2,
	QualityHigh192:       3,
	QualityDolby:         4,
	QualityHiResLossless: 5,
}

// Label 是播放条上那个徽章的字。
//
// bitrate 是这条流的码率（B 站给的 `bandwidth`，单位 bit/s），
// **只在认不出档位时才用**。
//
// ⚠️ 认得出的档位必须用**档位自己的名字**，不能拿码率去算：实测 B 站给的
// `bandwidth` 是**这一次编码的实际码率**，和档位标称的码率对不上——30232
// （标称 132K）实测落在 37K~129K，30280（标称 192K）实测落在 75K~231K，
// 同一个档位在不同稿件上能差近一倍。按码率算出来的数字说的不是「这一档」
// （`electron/ipc/download/utils.ts` 里 Biu 自己也是按 id 查表出名字的，
// 没用过 bandwidth）。
func (q Quality) Label(bitrate int) string {
	switch q {
	case QualityHiResLossless:
		return "Hi-Res 无损"
	case QualityDolby:
		return "杜比全景声"
	case QualityHigh192:
		return "192K"
	case QualityMid128:
		return "128K"
	case QualityLow64:
		return "64K"
	}
	// 认不出来的档位（B 站加新档时）：码率说话，总比「未知音质」有用。
	if bitrate > 0 {
		return fmt.Sprintf("%dK", (bitrate+500)/1000)
	}
	return "未知音质"
}

// ChipLabel 是这一档音质在**播放条上**那枚 chip 的字。
//
// full 是 `Label` 给的那一串（「Hi-Res 无损」/「杜比全景声」/「192K」）。
//
// # 为什么要收短
//
// 上游播放条上那枚 chip 是**短的**（「无损」/「杜比」，`playbar/left/index.tsx:50-56`
// 的 `text-[10px]`），而我们那一行字说的是「**B 站给了什么**」——全称在 11px 下
// 是 52~60 宽（实测「无损 FLAC」52.7、「杜比全景声」54.7、「Hi-Res 无损」60.6）。
//
// 播放条上歌名那一行只有 **76** 宽（左边那一列的 244 减去封面 56、两道 8 的缝
// 和三枚键 96），全称和歌名**挤在一行放不下**：框架在装不下时按**基宽的比例**
// 分摊那份 shortfall，于是**徽章自己被摊扁**——实测「杜比全景声」只剩 13.8
// （一个字），而它同时是「换音质」的入口，**点击区只剩 14 像素**。
//
// ⚠️ **全称不能丢**：它在悬停提示里（`internal/ui` 的 `viewQualityBadge`：
// 「音质：杜比全景声（点一下换）」），那一行才是完整的说法。短的那一枚只是
// **一眼认得出是哪一类**（无损 / 杜比 / 多少 K）——和上游那一枚 chip 一个道理。
//
// 「未知音质」也收成「未知」：它同样是四个字（44 宽），而那一行放不下。
//
// 认不出的档位原样返回：它本来就短（`Label` 给的是「128K」或「111K」那样
// 的码率）。
func ChipLabel(full string) string {
	switch {
	case strings.Contains(full, "无损"):
		// 「Hi-Res 无损」（投稿那套）和「无损 FLAC」（音乐库那套）都是「无损」。
		// 用 Contains 而不是 HasPrefix：投稿那套的全称**以「Hi-Res」开头**，
		// 按前缀收会收不到。
		return "无损"
	case strings.HasPrefix(full, "杜比"):
		return "杜比"
	case strings.HasPrefix(full, "未知"):
		return "未知"
	}
	return full
}

// IsLossless 报告这一档是不是无损 FLAC。
//
// 判据是**档位**不是编码名：投稿侧的无损只有 Hi-Res 一档（`dash.flac`）。
func (q Quality) IsLossless() bool {
	return q == QualityHiResLossless
}

// MusicTier 是**B 站音乐库**的音质档，和 `Quality` 是**第三套**编号。
//
// 这一套是 `/audio/music-service-c/url` 的 `quality` 请求参数，返回的
// `data.type` 用的也是它。它和投稿那套 dash id（30216 / 30280 那种）没有任何
// 关系：投稿侧的 30251 是 Hi-Res 无损，音乐库这里的 `3` 是 FLAC——**两个 3
// 说的不是同一件事**（投稿侧的 30250 才是杜比全景声）。所以不并进
// `Quality`——这正是本文件顶上「两套编号，别混」那条规矩，多一套也一样。
//
// Biu 上游同样分开处理：`getDashUrl` 用 dash 的 id，`getAudioUrl` 只判
// `data.type === 3` 是不是无损（`common/utils/audio.ts`）。
type MusicTier int

const (
	// MusicTierPreview 是**试听片段**：会员曲在没有权限时给的就是它。
	//
	// 🔴 它**不是错误也不是空**，是一条能下、能播的正常字节流，只有开头几十秒。
	// 所以任何不认它的地方，症状都是「歌播到一半静默停住」而界面上一个错都
	// 没有。见 `audio.go` 的文件头。
	MusicTierPreview MusicTier = -1

	// MusicTier128 是 128 Kbps。
	MusicTier128 MusicTier = 0
	// MusicTier192 是 192 Kbps。投稿侧也有这一档——那是 `QualityHigh192`
	// （30280，B 站标称 192K）。
	MusicTier192 MusicTier = 1
	// MusicTier320 是 320 Kbps。**只有音乐库有这一档**：投稿侧最高的有损
	// 是 192K（30280），没有 320K（票 #255 把这个搞错过一次）。
	MusicTier320 MusicTier = 2
	// MusicTierFLAC 是无损 FLAC，要会员。
	MusicTierFLAC MusicTier = 3
)

// Label 是这一档在播放条上写的字。
//
// 认不出来的（将来 B 站加新编号）给空串，**不拿 `qualities[].bps` 之类去补**：
// 播放条的徽章只有一个来源（`StreamRef.Label`），两处各说一半会让同一个意思
// 在徽章上飘。宁可空着——空徽章是不说话，错的徽章是在撒谎。
func (t MusicTier) Label() string {
	switch t {
	case MusicTierFLAC:
		return "无损 FLAC"
	case MusicTier320:
		return "320K"
	case MusicTier192:
		return "192K"
	case MusicTier128:
		return "128K"
	}
	return ""
}

// MusicTierFor 把用户选的那一档偏好翻译成**要向 B 站要**的那一档。
//
// 退的规矩和 `pickAudio` 一样（点名的档没有就退到不高于它的那一档里最好的），
// 只是**这里退不了**——这是一个请求参数，退不动。所以退不动的那一档由
// `audio.go` 在**拿到 `qualities` 之后**再问一次（见 `ErrPreviewOnly`）。
func MusicTierFor(pref AudioQuality) MusicTier {
	switch pref {
	case AudioLossless:
		return MusicTierFLAC
	case AudioHigh:
		return MusicTier320
	case AudioMedium, AudioLow:
		// 音乐库最低的一档就是 128K（它没有 64K），所以「标准」和「省流量」
		// 在这一路上落在同一档。**不给「省流量」发更高的那一档**：用户为了
		// 少流量点的它，拿到 192K 就是多给了流量——这正是 `pickAudio` 末尾
		// 那段注释说的反例。
		return MusicTier128
	}
	// 自动档要 **320K，不冒然要无损**。
	//
	// ⚠️ 这个 320K 是**音乐库**的 320K（`type=2`）。投稿侧没有 320K——
	// 那边最高的有损是 192K（`QualityHigh192`）。两套编号，别混。
	//
	// B 站对「没权限的那一档」不报错，回的是试听片段（`MusicTierPreview`），
	// 而这一层拿不到「这个号有没有会员」——所以自动档不去赌那一档，否则
	// 每一个没会员的号每放一首都要多问一次。会员想听无损请显式选「无损」：
	// 那一档拿到了就是 FLAC，拿不到会如实报错而不是给一段试听。
	//
	// 上游 Biu 是同一个取舍，只是它那边知道有没有会员
	// （`common/utils/audio.ts` 的 `vipStatus ? 3 : 2`）。
	return MusicTier320
}

// audioStream 是 `dash.audio[]` / `dash.flac.audio` / `dash.dolby.audio[]`
// 里的一项。三种容器形状的字段名一样，只是位置不同。
type audioStream struct {
	ID        int      `json:"id"`
	Bandwidth int      `json:"bandwidth"`
	Codecs    string   `json:"codecs"`
	MimeType  string   `json:"mimeType"`
	BaseURL   string   `json:"baseUrl"`
	BackupURL []string `json:"backupUrl"`
}

// dashInfo 是取流返回里 `data.dash` 的那一段。
//
// 只声明我们真的要用到的字段。B 站这个对象有几十个字段，剩下的不看。
//
// `Video` 以前是整段不看的（那时候这个应用只放音频），视频下载那一票
// 把它接上了——而**接口一直都在返回它**（取流带的 `fnval=4048` 就是
// 「要全部 DASH 流」，见 `stream.go` 的 `dashAll`）。见 `dash_video.go`。
type dashInfo struct {
	Duration float64       `json:"duration"`
	Video    []dashVideo   `json:"video"`
	Audio    []audioStream `json:"audio"`
	Flac     *struct {
		Display bool         `json:"display"`
		Audio   *audioStream `json:"audio"`
	} `json:"flac"`
	Dolby *struct {
		Type  int           `json:"type"`
		Audio []audioStream `json:"audio"`
	} `json:"dolby"`
}

// pickAudio 按偏好挑一条音频流，连同它的音质档位。
//
// 顺序照 Biu 上游（`common/utils/audio.ts` 的 `getDashUrl`）：
// **先**找 flac，**再**找 dolby，**最后**才在 `dash.audio` 里按档位挑。
// 不是因为无损更重要，而是 B 站的 `dash.audio` 里**根本不含 FLAC**——
// 无损只在 `dash.flac` 那个独立字段里。先翻 dash.audio 会永远拿不到无损。
//
// 返回的档位按「**字段定类别、id 定档位名**」来（见 `quality.go` 的文件头）。
func pickAudio(d *dashInfo, pref AudioQuality) (audioStream, Quality, bool) {
	if d == nil {
		return audioStream{}, QualityUnknown, false
	}

	var dolby []audioStream
	if d.Dolby != nil {
		dolby = playable(d.Dolby.Audio)
	}
	hasDolby := len(dolby) > 0
	hasFlac := d.Flac != nil && d.Flac.Audio != nil && d.Flac.Audio.BaseURL != ""

	// 只要无损/自动：先看 flac，再看 dolby。
	//
	// ⚠️ 这两条**不盲信流里的 id**：`dash.flac` 就是 FLAC、`dash.dolby` 就是杜比
	// 全景声——**字段名本身才是权威的**，id 是 B 站的内部编号，而且**改过**：
	// FLAC 那一档在老资料里是 30257，2026-10-23 实测是 30251。按 id 猜的话，
	// B 站一改，无损流就会挂上「杜比 Hi-Res」这种完全不相干的标签（票 #255）。
	//
	// 有损那几档（`dash.audio[]`）反过来**只能**靠 id：同一个字段里躺着好几档，
	// 字段名分不出来。
	if (pref == AudioAuto || pref == AudioLossless) && hasFlac {
		s := *d.Flac.Audio
		return s, QualityHiResLossless, true
	}
	if pref == AudioAuto && hasDolby {
		s := bestOf(dolby)
		return s, QualityDolby, true
	}

	list := sortStreams(playable(d.Audio))
	switch pref {
	case AudioLossless:
		// 到这里还没有无损，而无损是被点名要的：不给。
		return audioStream{}, QualityUnknown, false
	case AudioHigh:
		if s, ok := pickExact(list, QualityHigh192); ok {
			return s, QualityHigh192, true
		}
	case AudioMedium:
		if s, ok := pickExact(list, QualityMid128); ok {
			return s, QualityMid128, true
		}
	case AudioLow:
		if s, ok := pickExact(list, QualityLow64); ok {
			return s, QualityLow64, true
		}
	}

	// 点名的档位没有：退到**不高于**它的那一档里最好的（Biu 上游对 high 的
	// 处理是直接取 sortedList[0]，那对「省流量」是反的——用户为了少流量点了
	// 64K，拿到 192K，流量翻三倍）。真的一个都没有（比如 B 站只给了 192K），
	// 退到最低的那一档：宁可少要一档，也不要多给——用户可以再点一次提高。
	//
	// 自动档不走这条路：它的意思就是「能给的多好就多好」，直接拿最好的那条。
	if len(list) == 0 {
		return audioStream{}, QualityUnknown, false
	}
	if pref == AudioAuto {
		s := list[0]
		return s, Quality(s.ID), true
	}
	s := closestTo(list, wantedTier(pref))
	return s, Quality(s.ID), true
}

// wantedTier 是这一档偏好点名要的那一档。取不到时按它比高低。
func wantedTier(pref AudioQuality) Quality {
	switch pref {
	case AudioHigh:
		return QualityHigh192
	case AudioMedium:
		return QualityMid128
	case AudioLow:
		return QualityLow64
	}
	return QualityHigh192
}

// closestTo 在 list（已按从好到坏排好）里挑最接近 want 的那一档：
// 先要不超过 want 的最好的，没有就挑最低的。
//
// 认不出来的档位（`qualityRank` 里没有的）排在 0，也就是比所有已知档都低——
// 我们不知道它有多好，宁可当成最低。
func closestTo(list []audioStream, want Quality) audioStream {
	worst := list[len(list)-1]
	for _, s := range list {
		if qualityRank[Quality(s.ID)] <= qualityRank[want] {
			return s
		}
	}
	return worst
}

// playable 剔掉**有条目但没有地址**的流。
//
// ⚠️ 这里挡的不是「那一段是空的」。真机实测过没有大会员时 B 站回的是
// `dash.dolby = {"type":0,"audio":[]}`、`dash.flac = null`——那种由
// `len(dolby.Audio) > 0` 那一句就挡住了，`playable` 对它不起任何作用。
//
// 它挡的是**另一种形状**：条目在，`baseUrl` 是空的。自动档会把它当成「有杜比」
// 挑中它，然后由 `refFrom` 报一句「这个稿件没有音频流」——**而 `dash.audio`
// 里好好的 192K 就在旁边摆着**。用户看到的是「这一首放不了」，而真因是我们
// 挑错了列表里的一条。
//
// 一条没有地址的流不是流，这一条不需要理由。
func playable(in []audioStream) []audioStream {
	out := make([]audioStream, 0, len(in))
	for _, s := range in {
		if s.BaseURL != "" {
			out = append(out, s)
		}
	}
	return out
}

// sortStreams 把一批流按「码率高的优先，码率一样时音质 id 高的优先」排。
//
// 码率在前面：Biu 上游也是这么排的（`sortAudio` 的第一个比较条件），
// 而且它比 id 可靠——id 的高低在有损档里跟码率一致，而 `bandwidth` 是**这一
// 次编码的实际码率**，同一个档位在不同稿件上能差近一倍（见 `Label` 那一段）。
//
// ⚠️ 它只用在**一个字段里躺着好几档**的那两个容器上（`dash.audio[]` 和
// `dash.dolby.audio[]`）：那种地方字段名分不出档位，只能靠码率和 id 排。
// `dash.flac` 不走这里——它只有一条，而那一档由**字段**决定（见 `pickAudio`）。
func sortStreams(in []audioStream) []audioStream {
	out := make([]audioStream, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Bandwidth != out[j].Bandwidth {
			return out[i].Bandwidth > out[j].Bandwidth
		}
		return qualityRank[Quality(out[i].ID)] > qualityRank[Quality(out[j].ID)]
	})
	return out
}

// bestOf 取一批流里最好的那条。
func bestOf(in []audioStream) audioStream {
	return sortStreams(in)[0]
}

func pickExact(list []audioStream, q Quality) (audioStream, bool) {
	for _, s := range list {
		if Quality(s.ID) == q {
			return s, true
		}
	}
	return audioStream{}, false
}
