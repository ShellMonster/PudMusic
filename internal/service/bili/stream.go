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
	"encoding/json"
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
	Codecs string
	// Container 是我们据此选解码器的容器：`mp4` / `flac`。
	Container string

	// ExpiresAt 是地址的失效时刻，从地址里的 `deadline` 读出来。
	ExpiresAt time.Time

	// Size 是字节数。B 站给 Content-Length，给了就有。
	Size int64

	// Bvid / Cid 是这条流属于哪个稿件的哪一集。换流之后拿它们比对，
	// 对不上就是取错了歌（见 `SameTrack`）。
	Bvid string
	Cid  string
}

// SameTrack 报告 ref 说的是不是同一首（同一稿件、同一集）。
//
// 换流之后必须比一次：取流接口在稿件被删、被替换成别的内容时会
// 静默给一条**看着正常**的流，不比对的话用户会在毫无提示的情况下
// 听到另一首歌。
func (ref *StreamRef) SameTrack(other *StreamRef) bool {
	if ref == nil || other == nil {
		return false
	}
	return ref.Bvid == other.Bvid && ref.Cid == other.Cid
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
// pref 是用户选的音质档（`AudioAuto` 是默认）。bvid / cid 指向收藏夹里的
// 一个条目——**cid 必填**：B 站的多 P 投稿不给 cid 会取到第一集，
// 表现为「点第三集放出来的是第一集」，且不报错。
//
// 返回的 `StreamRef` 里没有任何凭据，**不要**把它写进日志：地址带
// `upsig` 签名，诊断文件里有它就等于把这条流公开了。
func (c *Client) PlayURL(ctx context.Context, bvid, cid string, pref AudioQuality) (*StreamRef, error) {
	if bvid == "" || cid == "" {
		// 不是 ErrNoStream：调用方给错了参数，换一条流也救不回来。
		return nil, fmt.Errorf("%w: 稿件或分 P 不完整（bvid=%q cid=%q）", ErrNoStream, bvid, cid)
	}

	q := url.Values{}
	q.Set("bvid", bvid)
	q.Set("cid", cid)
	q.Set("fnval", fmt.Sprint(dashAll))
	q.Set("fnver", "0")
	q.Set("fourk", "1")

	// ⚠️ 这个接口**不强制**要 WBI 签名：不带 w_rid 也能通（实测）。
	// 这里仍然签名，因为带签名的调用在风控下更稳，而签名本身由
	// `wbi.go` 那唯一一份实现负责，不存在「这一处忘了签」的可能。
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + playurlPath,
		Query:  q,
		WBI:    true,
	})
	if err != nil {
		return nil, err
	}

	var d playurlData
	if err := json.Unmarshal(raw, &d); err != nil {
		// 接口回了 200、code 也是 0，但 data 的形状不是我们认识的那个。
		// 这通常是 B 站改接口改出来的，不要把它当成网络错——重试没用。
		return nil, fmt.Errorf("bili: 取流接口的返回读不懂：%w", err)
	}
	return refFrom(&d, bvid, cid, pref)
}

// refFrom 把取流响应变成一个 StreamRef。
//
// 拆出来是因为「解析」这件事要能被 fixture 直接测：解析错了
// （比如把 flac 段漏掉）在真网上表现为「音质徽章一直显示 320K」，
// 那种问题不拆出来就只能靠人眼发现。
func refFrom(d *playurlData, bvid, cid string, pref AudioQuality) (*StreamRef, error) {
	if d == nil || d.Dash == nil {
		return nil, fmt.Errorf("%w: 这个稿件没有音频流", ErrNoStream)
	}

	s, q, ok := pickAudio(d.Dash, pref)
	if !ok || s.BaseURL == "" {
		// 区分两种「没有」：点名要无损却没有，和根本就没流。
		// 前者要给用户说「要大会员」，后者要说「这个稿件放不了」——
		// 合成一句话就变成了「没能取到可播的音频流」，用户不知道该干什么。
		if pref == AudioLossless {
			return nil, fmt.Errorf("%w: 这一首没有无损（要的是「%s」），换一档音质或用大会员账号", ErrNoStream, pref.Label())
		}
		return nil, fmt.Errorf("%w: 稿件 %s 的第 %s 集没有音频", ErrNoStream, bvid, cid)
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

// containerOf 判断这条流的容器，我们据此选解码器。
//
// 判据是**编码名**而不是 `mimeType`：B 站的音频 mime 是
// `audio/mp4`，无损那条也是 `audio/mp4`（装 FLAC 的还是 MP4 盒），
// 靠 mime 分不出 FLAC 和 AAC。而 `codecs` 里写的是 `flac` ——
// 这一位是从无争议的地方取的事实。
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
