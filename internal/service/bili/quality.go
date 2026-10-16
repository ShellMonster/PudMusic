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

package bili

import (
	"fmt"
	"sort"
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
	QualityMid128 Quality = 30232
	// QualityHigh320 是 320 Kbps。普通大会员能拿到的最高有损。
	QualityHigh320 Quality = 30280

	// QualityDolby 是杜比全景声（`dash.dolby`）。
	QualityDolby Quality = 30250
	// QualityDolbyHiRes 是杜比全景声的高解析版（`dash.dolby`）。
	QualityDolbyHiRes Quality = 30251

	// QualityFLAC 是 FLAC 无损。`dash.flac` 下的 id 有好几个，
	// 区别是采样率和声道数，不是「是不是 FLAC」——所以它们都归到无损，
	// 具体是哪一档由码率说出来。
	QualityFLAC Quality = 30257
	// QualityFLACHR 是 FLAC 无损（高解析）。
	QualityFLACHR Quality = 30260
	// QualityFLACSurround 是 FLAC 无损（环绕声）。
	QualityFLACSurround Quality = 30259
)

// AudioQuality 是**用户**选的那一档偏好。
//
// 界面上给的是这几档，不是 B 站的 id：用户要选的是「无损」还是「省流量」，
// 「30259」对谁都没有意义。
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
	// AudioHigh 只要 320K。
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
// （30257 是无损、30280 是 320K，数字大小和音质无关），所以要另排一张表。
//
// ⚠️ 表里没有的 id 排在**所有已知档之下**（rank 0），不是中间：
// 认不出来的档位我们不知道它有多好，宁可当成最低的那档让别的档胜出，
// 也不要凭 id 的大小猜。
var qualityRank = map[Quality]int{
	QualityLow64:        1,
	QualityMid128:       2,
	QualityHigh320:      3,
	QualityDolby:        4,
	QualityDolbyHiRes:   5,
	QualityFLAC:         6,
	QualityFLACHR:       7,
	QualityFLACSurround: 8,
}

// Label 是播放条上那个徽章的字。
//
// bitrate 是这条流的码率（B 站给的 `bandwidth`，单位 bit/s），
// **只在认不出档位时才用**。
//
// ⚠️ 认得出的档位必须用**档位自己的名字**，不能拿码率去算：实测 B 站给
// 30280（320K）时回的 `bandwidth` 是 110836，算出来是「111K」——那个字段
// 是这次编码的实际码率，和档位标称的码率对不上（`electron/ipc/download/utils.ts`
// 里 Biu 自己也是按 id 查表出名字的，没用过 bandwidth）。
func (q Quality) Label(bitrate int) string {
	switch q {
	case QualityFLAC, QualityFLACHR, QualityFLACSurround:
		return "无损 FLAC"
	case QualityDolby:
		return "杜比全景声"
	case QualityDolbyHiRes:
		return "杜比 Hi-Res"
	case QualityHigh320:
		return "320K"
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

// IsLossless 报告这一档是不是无损 FLAC。
func (q Quality) IsLossless() bool {
	return q == QualityFLAC || q == QualityFLACHR || q == QualityFLACSurround
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
// 只声明我们真的要用到的字段。B 站这个对象有几十个字段，
// `video` 整段我们一条都不看（这个应用只放音频）。
type dashInfo struct {
	Duration float64       `json:"duration"`
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
func pickAudio(d *dashInfo, pref AudioQuality) (audioStream, Quality, bool) {
	if d == nil {
		return audioStream{}, QualityUnknown, false
	}

	dolby := d.Dolby
	hasDolby := dolby != nil && len(dolby.Audio) > 0
	hasFlac := d.Flac != nil && d.Flac.Audio != nil && d.Flac.Audio.BaseURL != ""

	// 只要无损/自动：先看 flac，再看 dolby。
	if (pref == AudioAuto || pref == AudioLossless) && hasFlac {
		s := *d.Flac.Audio
		return s, Quality(s.ID), true
	}
	if pref == AudioAuto && hasDolby {
		s := bestOf(dolby.Audio)
		return s, Quality(s.ID), true
	}

	list := sortStreams(d.Audio)
	switch pref {
	case AudioLossless:
		// 到这里还没有无损，而无损是被点名要的：不给。
		return audioStream{}, QualityUnknown, false
	case AudioHigh:
		if s, ok := pickExact(list, QualityHigh320); ok {
			return s, QualityHigh320, true
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

	// 点名的档位没有，退到最好的那一条（Biu 上游的 selectAudioByQuality
	// 对 high 的处理：取 sortedList[0]）。
	if len(list) == 0 {
		return audioStream{}, QualityUnknown, false
	}
	s := list[0]
	return s, Quality(s.ID), true
}

// sortStreams 把一批流按「码率高的优先，码率一样时音质 id 高的优先」排。
//
// 码率在前面：Biu 上游也是这么排的（`sortAudio` 的第一个比较条件），
// 而且它比 id 可靠——id 的高低在有损档里跟码率一致，在无损档里却是反的
// （30257 是最低的无损、30251 是杜比高解析）。
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

// qualityOf 把接口里的 id 变成 Quality，顺手兜住两件事：
// B 站给的无损流 id 有好几个（归到 QualityFLAC），以及将来出现的新 id
// （归到 QualityUnknown，Label 会改用码率）。
func qualityOf(id, bitrate int) Quality {
	switch q := Quality(id); {
	case q.IsLossless():
		return q
	case q == QualityDolby, q == QualityDolbyHiRes,
		q == QualityLow64, q == QualityMid128, q == QualityHigh320:
		return q
	}
	// 没见过的 id：如果是 FLAC 编码，它就是无损。判据用 codecs 而不是
	// 「名字里有没有 flac」——B 站的 codecs 写的是 `flac` / `mp4a.40.2`
	// 这类标准串，`.audio` 段是「容器里的音频轨」而不是文件名。
	return QualityUnknown
}
