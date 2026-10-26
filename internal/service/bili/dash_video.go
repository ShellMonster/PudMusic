// 视频下载要的那一对流（`dash.video` + `dash.audio`）。
//
// # 我们一直在拿视频，只是没看
//
// 取流请求**早就**带 `fnval=4048`（`stream.go` 的 `dashAll`）了——那是
// 「要全部 DASH 流」的位，B 站的响应里 `dash.video[]` **一直都在**。
// 缺的是 `dashInfo` 里那个字段和挑它的逻辑（见 `quality.go` 里
// `dashInfo` 那段注释：`video` 整段一条都不看，因为那时这个应用只放
// 音频）。
//
// 所以这一票**没有新增接口调用**：`PlayDash` 和 `PlayURL` 走的是同一个
// 请求，多出来的是 `qn`（视频画质靠它选，音频那一条它不关心）。
//
// # 视频那一路为什么必须是两条流
//
// B 站的 dash 是**音视频分开的两条流**（两个 `.m4s`），而用户要的是一个
// 能播的文件。`mux` 那一层把它们交织成一个 `.mp4`——不转码、不改编码
// 参数（见 `internal/service/mux`）。

package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// dashVideo 是 `dash.video[]` 里的一项。
//
// 字段名跟 `audioStream` 一样（同一个对象形状），多两个**视频才有**的：
// 宽高和帧率。它们只作记录——「下载视频」这一路不转码，所以宽高不参与
// 任何计算，但记录下来之后诊断文件里能一眼看出这一条是 1080P 还是
// 720P，而那正是「为什么这个 MV 这么大」的第一答案。
type dashVideo struct {
	ID        int      `json:"id"`
	Bandwidth int      `json:"bandwidth"`
	Codecs    string   `json:"codecs"`
	MimeType  string   `json:"mimeType"`
	BaseURL   string   `json:"baseUrl"`
	BackupURL []string `json:"backupUrl"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	FrameRate string   `json:"frameRate"`
}

// VideoQuality 是 B 站的视频画质档。
//
// 跟 `Quality`（音质）**不是同一套编号**：那些是 30216/30280 这种，
// 这些是 16/32/64 这种——它们既是 `dash.video[].id`，也是请求里的 `qn`。
type VideoQuality int

const (
	VideoQualityUnknown VideoQuality = 0
	VideoQuality360     VideoQuality = 16
	VideoQuality480     VideoQuality = 32
	VideoQuality720     VideoQuality = 64
	VideoQuality1080    VideoQuality = 80
	VideoQuality1080P   VideoQuality = 112 // 1080P 高码率
	VideoQuality4K      VideoQuality = 120
	VideoQualityDolby   VideoQuality = 126
)

// videoQualityRank 从低到高。
//
// ⚠️ 和音频那一套不同，视频这一套**不能只按 id 比高低**——B 站的
// `dash.video[]` 里 120（4K）和 126（杜比）都**不是**「更清晰的 1080P」：
// 杜比那一条是 HDR，4K 那一档动辄 10 分钟几十兆。这个表只用来
// 「不超过点名的那一档里挑最好的」，所以排在最上面那两档要特殊对待
// （见 `pickVideo`）。
var videoQualityRank = map[VideoQuality]int{
	VideoQuality360:   1,
	VideoQuality480:   2,
	VideoQuality720:   3,
	VideoQuality1080:  4,
	VideoQuality1080P: 5,
	VideoQuality4K:    6,
	VideoQualityDolby: 7,
}

// Label 是给人看的画质徽章文字（`1080P`、`720P60`…）。
func (q VideoQuality) Label() string {
	switch q {
	case VideoQuality360:
		return "360P"
	case VideoQuality480:
		return "480P"
	case VideoQuality720:
		return "720P"
	case VideoQuality1080:
		return "1080P"
	case VideoQuality1080P:
		return "1080P+"
	case VideoQuality4K:
		return "4K"
	case VideoQualityDolby:
		return "杜比视界"
	}
	return "未知画质"
}

// DashPair 是一次视频下载要的那两条流。
//
// ⚠️ **两条都是必须的**：少一条就交织不出能播的文件，而缺的那一条在
// 取流那一步就要说清楚（「这个稿件没有视频」而不是交织的时候才炸）。
type DashPair struct {
	Video *StreamRef
	Audio *StreamRef
}

// PlayDash 问 B 站要「视频 + 音频」这一对流。
//
// qn 是要的视频画质档，`VideoQualityUnknown`（0）表示**不传** —— 让 B 站
// 按它自己的默认给。pref 是音频那一侧的偏好，和取音频时是同一套。
//
// ⚠️ 每次开始或续传都要**重新问一遍**（地址带 `deadline`，实测两小时就
// 失效，而一个几百 MB 的 MV 很可能下超过两小时）。
func (c *Client) PlayDash(ctx context.Context, bvid, cid string, qn VideoQuality, pref AudioQuality) (*DashPair, error) {
	if bvid == "" || cid == "" {
		return nil, fmt.Errorf("%w: 稿件或分 P 不完整（bvid=%q cid=%q）", ErrNoStream, bvid, cid)
	}
	d, err := c.fetchPlayurl(ctx, bvid, cid, qn)
	if err != nil {
		return nil, err
	}
	return pairFrom(d, bvid, cid, qn, pref)
}

// pairFrom 把取流响应变成「视频 + 音频」这一对。
//
// qn 是调用方点名要的那一档；`VideoQualityUnknown`（0）表示「不点名，
// 能多好就多好」。
//
// 拆出来是为了「解析」这件事能被 fixture 直接测：解析错了在真网上
// 表现为「下载视频」永远报「这个稿件没有视频」，而真实原因是字段名
// 写错了或者档位挑不出来。
func pairFrom(d *playurlData, bvid, cid string, qn VideoQuality, pref AudioQuality) (*DashPair, error) {
	if d == nil || d.Dash == nil {
		return nil, fmt.Errorf("%w: 这个稿件没有 dash 流", ErrNoStream)
	}
	v, ok := pickVideo(d.Dash, qn)
	if !ok {
		return nil, fmt.Errorf("%w: 稿件 %s 的第 %s 集没有视频流", ErrNoStream, bvid, cid)
	}
	a, err := refFrom(d, bvid, cid, pref)
	if err != nil {
		return nil, err
	}

	video := videoRef(v, bvid, cid)
	return &DashPair{Video: video, Audio: a}, nil
}

// urlsOf 拼出「主地址在前、备用地址在后」那一串。
func urlsOf(base string, backup []string) []string {
	out := make([]string, 0, 1+len(backup))
	if base != "" {
		out = append(out, base)
	}
	return append(out, backup...)
}

// videoContainer 是这条视频流的容器名，**只作记录**。
//
// dash 的视频流永远是 `video/mp4`（也就是 fMP4 分片流），而落盘的不是
// 它——是交织出来的那个 `.mp4`。这里记一份是为了诊断文件里能一眼看出
// 「源是什么」，而它**不参与**任何选择。
func videoContainer(v dashVideo) string {
	if v.MimeType != "" {
		if i := strings.Index(v.MimeType, "/"); i >= 0 {
			return v.MimeType[i+1:]
		}
	}
	return "mp4"
}

// pickVideo 从 `dash.video[]` 里挑一条。
//
// 判据是**不超过点名的那一档里挑最好的**，退回时也只往低里退：
// 用户点了「下载视频」，拿到 4K 的话一部 MV 几百兆起步，而那是
// 下载一份音乐该有的体积。
//
// 4K 和杜比**排在点名的那一档之上就不给**：它们不是「更清晰的同一支
// 片子」，而是另一个量级的东西（4K 的码率是 1080P 的三四倍，杜比那一条
// HDR 的播放器支持还是个问题）。要它们得显式点名（见 `videoQualityRank`）。
func pickVideo(d *dashInfo, want VideoQuality) (dashVideo, bool) {
	if d == nil {
		return dashVideo{}, false
	}
	var list []dashVideo
	for _, v := range d.Video {
		// 有条目但没有地址的不是一个流：挑中它会让后面 `fetch` 拿着
		// 一个空地址去请求，报「这个稿件没有视频流」，而真实的 720P
		// 就在旁边摆着（和 `playable` 那条注释是一回事）。
		if v.BaseURL != "" {
			list = append(list, v)
		}
	}
	if len(list) == 0 {
		return dashVideo{}, false
	}

	// 点名了一档：**不超过它的里面挑最好的**。
	//
	// ⚠️ 少了这个判断，用户点了「下载 360P 省流量」而拿回来的是 4K——
	// 而 `qn=16` 只是让 B 站**也**把别的档一并返回（`fnval=4048` + `fourk=1`），
	// 不代表别的档就没回来。整个 4K 下下来几百 MB，而界面上从头到尾写着
	// 用户点的那一档。**要靠 `qn` 去相信服务端已经替我们限好了**，那是把
	// 用户的选择交给接口的默认行为。
	if want != VideoQualityUnknown {
		if best, ok := bestNotAbove(list, want); ok {
			return best, true
		}
		// 一档都不超过点名的那一档（B 站没给）：只能拿最低的那一档，
		// 而不是最高的那一档。宁可少一档，也不要多给——和音频那条
		// `closestTo` 同一个道理（见 `quality.go`）。
		worst := list[0]
		for _, v := range list[1:] {
			if videoQualityRank[VideoQuality(v.ID)] < videoQualityRank[VideoQuality(worst.ID)] {
				worst = v
			}
		}
		return worst, true
	}

	// 没点名：能多好就多好。
	best := list[0]
	for _, v := range list[1:] {
		if videoQualityRank[VideoQuality(v.ID)] > videoQualityRank[VideoQuality(best.ID)] {
			best = v
		}
	}
	return best, true
}

// bestNotAbove 在 list 里挑不超过 want 的最好的那一档。
func bestNotAbove(list []dashVideo, want VideoQuality) (dashVideo, bool) {
	limit := videoQualityRank[want]
	best, found := dashVideo{}, false
	for _, v := range list {
		if r := videoQualityRank[VideoQuality(v.ID)]; r <= limit && (!found || r > videoQualityRank[VideoQuality(best.ID)]) {
			best, found = v, true
		}
	}
	return best, found
}

// fetchPlayurl 发那一次取流请求。
//
// `qn` 是视频画质档；0 表示**不传**这个参数——那是音频那条路的原样
// 行为（取流不带 `qn`，B 站把所有档都返回来，由客户端挑）。
func (c *Client) fetchPlayurl(ctx context.Context, bvid, cid string, qn VideoQuality) (*playurlData, error) {
	return c.fetchPlayurlGaia(ctx, bvid, cid, qn, false)
}

// fetchPlayurlGaia 同 fetchPlayurl。B 站要求验证（只回 v_voucher）时，带着
// gaia_source=view-card 再问一次；还是不给就报风控。
func (c *Client) fetchPlayurlGaia(ctx context.Context, bvid, cid string, qn VideoQuality, gaia bool) (*playurlData, error) {
	q := url.Values{}
	q.Set("bvid", bvid)
	q.Set("cid", cid)
	q.Set("fnval", fmt.Sprint(dashAll))
	q.Set("fnver", "0")
	q.Set("fourk", "1")
	if qn != VideoQualityUnknown {
		q.Set("qn", fmt.Sprint(int(qn)))
	}
	if gaia {
		q.Set("gaia_source", "view-card")
	}

	// ⚠️ 这个接口**不强制**要 WBI 签名：不带 w_rid 也能通（实测）。
	// 这里仍然签名，因为带签名的调用在风控下更稳，而签名本身由
	// `wbi.go` 那唯一一份实现负责，不存在「这一处忘了签」的可能。
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + playurlPath,
		Query:  q,
		WBI:    true,
		Buvid:  true,
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
	if d.VVoucher != "" && d.Dash == nil {
		if gaia {
			return nil, fmt.Errorf("%w: 取流被要求验证（v_voucher）", ErrRiskControl)
		}
		return c.fetchPlayurlGaia(ctx, bvid, cid, qn, true)
	}
	return &d, nil
}

// videoRef 把 `dash.video[]` 的一项变成 StreamRef。
func videoRef(v dashVideo, bvid, cid string) *StreamRef {
	return &StreamRef{
		URLs:      urlsOf(v.BaseURL, v.BackupURL),
		Quality:   Quality(v.ID),
		Label:     VideoQuality(v.ID).Label(),
		Bitrate:   v.Bandwidth,
		Codecs:    v.Codecs,
		Container: videoContainer(v),
		ExpiresAt: deadlineOf(v.BaseURL),
		Bvid:      bvid,
		Cid:       cid,
		Width:     v.Width,
		Height:    v.Height,
	}
}

// PlayVideo 只要视频那一条流，给边放边看用：优先 H.264（系统解码器
// 都认），没有 H.264 才退到别的编码；画质不超过 qn。
func (c *Client) PlayVideo(ctx context.Context, bvid, cid string, qn VideoQuality) (*StreamRef, error) {
	ref, _, err := c.PlayVideoList(ctx, bvid, cid, qn)
	return ref, err
}

// PlayVideoList 同 PlayVideo，另外给出这一集**能选的画质**（从低到高）。
// 没登录时 B 站只给低档，登录后才有 1080P 以上，列表如实反映这一点。
func (c *Client) PlayVideoList(ctx context.Context, bvid, cid string, qn VideoQuality) (*StreamRef, []VideoQuality, error) {
	if bvid == "" || cid == "" {
		return nil, nil, fmt.Errorf("%w: 稿件或分 P 不完整（bvid=%q cid=%q）", ErrNoStream, bvid, cid)
	}
	// 不带 qn：B 站把这一集所有的档都返回来，才列得出能选的画质；上限在本地挑。
	d, err := c.fetchPlayurl(ctx, bvid, cid, VideoQualityUnknown)
	if err != nil {
		return nil, nil, err
	}
	return videoOnly(d, bvid, cid, qn)
}

func videoOnly(d *playurlData, bvid, cid string, qn VideoQuality) (*StreamRef, []VideoQuality, error) {
	if d == nil || d.Dash == nil {
		return nil, nil, fmt.Errorf("%w: 这个稿件没有 dash 流", ErrNoStream)
	}
	dash := *d.Dash
	var avc []dashVideo
	for _, v := range dash.Video {
		if strings.HasPrefix(v.Codecs, "avc1") {
			avc = append(avc, v)
		}
	}
	if len(avc) > 0 {
		dash.Video = avc
	}
	v, ok := pickVideo(&dash, qn)
	if !ok {
		return nil, nil, fmt.Errorf("%w: 稿件 %s 的第 %s 集没有视频流", ErrNoStream, bvid, cid)
	}
	seen := map[VideoQuality]bool{}
	var list []VideoQuality
	for _, x := range dash.Video {
		if q := VideoQuality(x.ID); x.BaseURL != "" && !seen[q] && videoQualityRank[q] > 0 {
			seen[q] = true
			list = append(list, q)
		}
	}
	sort.Slice(list, func(i, j int) bool { return videoQualityRank[list[i]] < videoQualityRank[list[j]] })
	return videoRef(v, bvid, cid), list, nil
}
