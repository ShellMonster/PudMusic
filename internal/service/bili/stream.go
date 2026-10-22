// 取流：问 B 站要一条能播的音频流地址。
//
// # 有效期是常态，不是异常
//
// B 站给的地址带一个 `deadline` 查询参数（Unix 秒），到点就失效。
// 实测一条 5 分钟的地址有效期是**两小时**，但用户在两小时里完全可能
// 拖进度条拖到没下过的位置、也可能中途切走再切回来——所以「地址过期」
// 必须当成一条**正常路径**来写，不是异常分支。三条路径见 `spec.md`
// 「网络」一节，实现在 `internal/app/online.go` 与
// `internal/service/audio/stream_source.go`。
//
// 这一层只管**拿到地址**，不管下载、不管解码、不管播放。

package bili

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 取流相关的哨兵错误。都拿 errors.Is 判。
var (
	// ErrNoStream 是「接口成功了，但没有可播的音频」。
	//
	// 不是网络错也不是 bug：B 站对「没有大会员的用户要无损」、
	// 「稿件被删了」「分 P 不对」都回 code 0 + 一段没有流的 dash。
	// 这一条把「查得到但没得放」和「查都查不到」分开。
	ErrNoStream = errors.New("bili: 没能取到可播的音频流")

	// ErrNoAudioFlow 是「B 站说这一条**没有单独的音频流**」。
	//
	// 它和 `ErrNoStream` 分开，是因为**要走的路正好相反**：
	//
	//	ErrNoStream		「查到了这一条，但没给可播的」——多半是没大会员、
	//	                已下架那一类，换一档音质或者换个账号也许还有戏
	//	ErrNoAudioFlow	这一条**压根就没有音频流**：合集、剧集、还有把歌单
	//	                整个塞进一个投稿的那种（实测：音乐区 tid=130、
	//	                单 P、三十多分钟，取流接口回的就是这一句）
	//
	// 后者重试一万次还是这一句。给用户的话必须说清是后者——说成「没获取到
	// 音频」的话，用户会以为是网络抖了，去查网、换网、等它好，而它永远
	// 不会好（这一条见 `internal/app/unplayable.go`）。
	//
	// ⚠️ **它包着 `ErrNoStream`**：`errors.Is(err, ErrNoStream)` 仍然成立
	// （那一类仍然是「查到了但没得放」），而 `errors.Is(err, ErrNoAudioFlow)`
	// 只在这一种更细的情况上成立。两层判据不是重复，是「粗的一层 + 细的一层」。
	ErrNoAudioFlow = fmt.Errorf("%w: 这一条没有单独的音频流", ErrNoStream)

	// ErrStreamExpired 是「手上这条流已经过期了」。
	//
	// 它不是从接口返回的，是从地址的 deadline 算出来的——
	// 所以它出现的地方一定有一个「现在可以换一条流」的场合。
	ErrStreamExpired = errors.New("bili: 播放地址已过期")

	// ErrNoLoginForOnline 是「要在线流但没登录」。
	ErrNoLoginForOnline = errors.New("bili: 在线播放需要先登录")
)

// StreamRef 是一条取到的音频流的引用。
//
// 它是**地址 + 身份 + 有效期**三件事打包：只有地址的话，
// 「这条地址还是不是这一首歌的」这件事就没人回答了——而换流时
// 恰恰要问这个问题。
type StreamRef struct {
	// URLs 是同一段字节的多个地址：主地址在前，备用地址在后。
	//
	// B 站给一条流通常配 2~3 个 CDN 节点。主地址 403 时先试备用的，
	// 全试过还不行才叫 Refetch 换一条新流——换流要走一次带签名的接口，
	// 比在已有地址上换一个便宜得多。
	URLs []string

	// Quality 是这一档音质，Label 是它对应的徽章文字。
	Quality Quality
	Label   string
	// Bitrate 是码率（bit/s）。认不出的音质靠它说话。
	Bitrate int

	// Codecs 是 B 站的编码名（`mp4a.40.2` / `flac` / `mp4a.40.5`）。
	//
	// ⚠️ 它和 Container 都只是**给人看的记录**，播放那一侧不用它们选解码器：
	// 解码器是按字节流的头几个字节嗅探出来的（`audio.containers.go` 注册的
	// `????ftyp` / `fLaC`，`audio.OpenStream` 先试 MyGo 的格式表再试 flacdec）。
	// 换句话说，即使这两个字段填错了，歌照样放得出来——所以别拿它们做判断。
	Codecs    string
	Container string

	// Width / Height 是视频那条流的分辨率（音频流恒为 0）。
	//
	// **只作记录**：视频下载这一路不转码，所以宽高不参与任何计算。
	// 记下来是因为诊断文件里能一眼看出「这一条是 1080P 还是 720P」，
	// 而那正是「为什么这个 MV 这么大」的第一个答案。
	Width  int
	Height int

	// ExpiresAt 是地址的失效时刻。
	//
	// 两条取流路径的来源不一样：投稿那路从地址上的 `deadline` 读
	// （`deadlineOf`），音乐库那路用接口给的 `timeout` 秒数（`songExpiry`）。
	ExpiresAt time.Time

	// Bvid / Cid 是这条流属于哪个稿件的哪一集。换流之后拿它们比对，
	// 对不上就是取错了歌（见 `SameTrack`）。
	Bvid string
	Cid  string

	// Sid 是这条流属于音乐库里的哪一条（auid）。
	//
	// ⚠️ **音乐库那一路 Bvid / Cid 都是空的**，只有一个 sid。所以只按
	// Bvid+Cid 判身份的话，音乐库的任何两条流都会被认成同一首——而换流的
	// 那道防线（`SameTrack`）正是拿它拦「静默换成另一首歌」的。
	Sid string

	// MusicTier 是**音乐库那一路**的档位。投稿那一路是空的（它的档位在
	// Quality 里）。
	//
	// 两套编号不合并，理由见 `quality.go` 的文件头。判断「换回来的还是不是
	// 同一档」用 `SameTier`，它两边都认。
	MusicTier MusicTier
}

// SameTrack 报告 ref 说的是不是同一首。
//
// 换流之后必须比一次：取流接口在稿件被删、被替换成别的内容时会
// 静默给一条**看着正常**的流，不比对的话用户会在毫无提示的情况下
// 听到另一首歌。
//
// ⚠️ **两边都有 sid 就只比 sid**：音乐库那一路 Bvid / Cid 都是空的，只按
// Bvid+Cid 比的话这一路的任何两条流都会被认成同一首，那道防线在音乐库上
// 整个失效。
func (ref *StreamRef) SameTrack(other *StreamRef) bool {
	if ref == nil || other == nil {
		return false
	}
	if ref.Sid != "" || other.Sid != "" {
		return ref.Sid != "" && ref.Sid == other.Sid
	}
	return ref.Bvid == other.Bvid && ref.Cid == other.Cid
}

// SameTier 报告 ref 说的是不是同一档音质。
//
// 换音质时判的就是这个：用户点的档拿回来的还是同一档，就不该打断正在放的
// 歌——为一个听不出区别的改动把歌从中间截断，比点了没反应更糟。
//
// 徽章文字比不了（那是给人看的，会改写），所以比的是**档位本身**。
func (ref *StreamRef) SameTier(other *StreamRef) bool {
	if ref == nil || other == nil {
		return false
	}
	// 音乐库那一路只有 MusicTier；投稿那一路只有 Quality。哪一路有值就比哪一路。
	if ref.MusicTier != 0 || other.MusicTier != 0 {
		return ref.MusicTier == other.MusicTier
	}
	return ref.Quality == other.Quality
}

// Usable 报告这个引用现在还能不能拿去播。
//
// ⚠️ **不是「deadline 还没到」**。B 站的 deadline 是个粗粒度的
// 开关：地址在 deadline 之后确实会 403，但在 deadline 之前的最后
// 一两分钟也已经开始不稳。所以要留一段提前量（`expiryMargin`），
// 宁可早换一次，也不要让用户在歌中间听到卡顿。
func (ref *StreamRef) Usable(now time.Time) bool {
	if ref == nil || len(ref.URLs) == 0 {
		return false
	}
	return now.Add(expiryMargin).Before(ref.ExpiresAt)
}

// expiryMargin 是「提前多久认为过期」。
//
// B 站的 deadline 是给 CDN 侧做粗粒度回收用的，客户端拿着它在最后一两分钟
// 里仍然可能拿到 403。取 2 分钟：早换一次的代价是一次带签名的取流请求，
// 迟换的代价是用户听到断流。
const expiryMargin = 2 * time.Minute

// playurlPath 是取流接口。
//
// 带 `wbi` 的那个版本是现在的正式接口。Biu 上游用的也是它
// （`src/service/player-playurl.ts`）。
const playurlPath = "/x/player/wbi/playurl"

// dashAll 是「要全部 DASH 流」的 fnval。
//
// 4048 = 16(DASH) + 4048 里另外几项资源的联合位。**必须是 4048 而不是
// 单独的 16**：只给 16 时 B 站不会返回 `dash.flac` 字段，取不到无损。
// Biu 上游用的也是 4048（`common/constants/video.ts` 的
// `VideoFnval.AllDash`）。
const dashAll = 4048

// playurlData 是 `data` 段。字段只留我们要的。
type playurlData struct {
	Timelength int       `json:"timelength"` // 毫秒
	Quality    int       `json:"quality"`
	Dash       *dashInfo `json:"dash"`
}

// PlayURL 问 B 站要一条音频流。
//
// pref 是用户选的音质档（`AudioAuto` 是默认）。bvid / cid 指向一个条目。
//
// ⚠️ **这一条只管投稿（mv）**。音乐库条目（sid）走的是另一个接口、另一套档位
// 编号，见 `audio.go` 的 `AudioStreamURL`。
//
// 🔴 **cid 必填**（上游 `player-playurl.ts:9` 的 `cid: number | string`，那行没有
// `?`）。不给 cid，B 站多半按第一集给——多 P 投稿放出来的和用户点的不是一回事，
// 而本地 fixture 放不放假参数，所以「它会不会老实按第一集给」没人验过：既然上游的
// 合同写着必填，这里就不去赌它。
//
// B 站的搜索结果只给 bvid（见 search.go 的文件头），所以**调用方要先补上 cid**：
// `app.runOnline` 问一次 `VideoPages`（见 video.go）再调这里。补不上就报错，
// 不硬着头皮发一个没有 cid 的请求。
//
// 返回的 `StreamRef` 里没有任何凭据，**不要**把它写进日志：地址带
// `upsig` 签名，诊断文件里有它就等于把这条流公开了。
func (c *Client) PlayURL(ctx context.Context, bvid, cid string, pref AudioQuality) (*StreamRef, error) {
	if bvid == "" || cid == "" {
		// 不是网络错也不是重试能救的：调用方没给全。重试一万次还是这一句。
		return nil, fmt.Errorf("%w: 稿件或分 P 不完整（bvid=%q cid=%q）", ErrNoStream, bvid, cid)
	}
	// 不带 `qn`：取流接口把所有档都返回来，由 `pickAudio` 按偏好在客户端挑
	//（B 站那边也是这么用的）。视频那一路才需要点明一档（见 `fetchPlayurl`）。
	d, err := c.fetchPlayurl(ctx, bvid, cid, VideoQualityUnknown)
	if err != nil {
		return nil, err
	}
	return refFrom(d, bvid, cid, pref)
}

// refFrom 把取流响应变成一个 StreamRef。
//
// 拆出来是因为「解析」这件事要能被 fixture 直接测：解析错了
// （比如把 flac 段漏掉）在真网上表现为「音质徽章一直显示 320K」，
// 那种问题不拆出来就只能靠人眼发现。
func refFrom(d *playurlData, bvid, cid string, pref AudioQuality) (*StreamRef, error) {
	if d == nil || d.Dash == nil {
		// 🔴 **用 `ErrNoAudioFlow` 而不是 `ErrNoStream`**：这一支是「B 站说
		// 这一条压根没有音频流」（合集 / 剧集 / 歌单塞一个投稿的那种），
		// 重试一万次还是这一句。混进 `ErrNoStream` 里的话，界面上给用户的
		// 那句就会变成「没能取到可播的音频流」——用户会以为是网络抖了。
		return nil, fmt.Errorf("%w：这个稿件没有音频流", ErrNoAudioFlow)
	}

	s, q, ok := pickAudio(d.Dash, pref)
	if !ok || s.BaseURL == "" {
		// 区分两种「没有」：点名要无损却没有，和根本就没流。
		// 前者要给用户说「要大会员」，后者要说「这个稿件放不了」——
		// 合成一句话就变成了「没能取到可播的音频流」，用户不知道该干什么。
		if pref == AudioLossless {
			return nil, fmt.Errorf("%w: 这一首没有无损（要的是「%s」），换一档音质或用大会员账号", ErrNoStream, pref.Label())
		}
		return nil, fmt.Errorf("%w：稿件 %s 的第 %s 集没有音频", ErrNoAudioFlow, bvid, cid)
	}

	urls := make([]string, 0, 1+len(s.BackupURL))
	urls = append(urls, s.BaseURL)
	urls = append(urls, s.BackupURL...)

	ref := &StreamRef{
		URLs:      urls,
		Quality:   q,
		Bitrate:   s.Bandwidth,
		Codecs:    s.Codecs,
		Container: containerOf(s, q),
		ExpiresAt: deadlineOf(urls[0]),
		Bvid:      bvid,
		Cid:       cid,
	}
	ref.Label = ref.Quality.Label(ref.Bitrate)
	return ref, nil
}

// containerOf 记下这条流的容器名，**只作记录**。
//
// 判据是**编码名**而不是 `mimeType`：B 站的音频 mime 是 `audio/mp4`，
// 无损那条也是 `audio/mp4`（装 FLAC 的还是 MP4 盒），靠 mime 分不出
// FLAC 和 AAC。而 `codecs` 里写的是 `flac`。
//
// ⚠️ 播放那一侧**不用**这个结果去选解码器——见 `StreamRef.Container` 的
// 注释：解码器是按头几个字节嗅探的。这里记一份是为了诊断和日志里能一眼
// 看出「这会儿在放的是 mp4 还是 flac」。
func containerOf(s audioStream, q Quality) string {
	if q.IsLossless() || strings.Contains(s.Codecs, "flac") {
		// `dash.flac.audio` 里可能是裸 FLAC 也可能是 MP4 里的 FLAC，
		// 两种 `flacdec.Open` 都认（见 flacdec.go 的分支），所以这里
		// 统一说 flac，让 flacdec 自己判。
		return "flac"
	}
	return "mp4"
}

// deadlineOf 从地址里读 B 站的失效时刻。
//
// 读不到就当成**立刻失效**（返回零值时间），不当成「永不过期」：
// 猜错了两种后果里，误判成永不过期会让换流路径一次都不跑，
// 而真机上那种情况表现为「播到一半静默停住」，比提前换流难查得多。
func deadlineOf(raw string) time.Time {
	u, err := url.Parse(raw)
	if err != nil {
		return time.Time{}
	}
	ds := u.Query().Get("deadline")
	if ds == "" {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(ds, 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
