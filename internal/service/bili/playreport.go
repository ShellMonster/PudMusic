// 播放记录上报：把「现在在放哪一首、放到哪儿了」写进 B 站的服务器。
//
// # 这个文件为什么必须有
//
// 「历史记录」那一页读的是 B 站**服务器**上的历史（`history.go` 的
// `/x/web-interface/history/search`）。我们一次都没往里写过，于是只用我们这个
// 应用、从不开 B 站网页端的用户，那一页永远是空的（审计 A7）。
//
// # 这是**一次会话**，不是一个接口
//
//	/x/click-interface/click/web/h5   「开始看这一首」——每一轮播放发一次
//	/x/click-interface/web/heartbeat   「听到这儿了」——30 秒一次，外加暂停/继续/结束各一次
//
// 两条都是 POST、urlencoded 表单、**带 WBI 签名**、**带 csrf** 的写操作。
// 两条都不返回我们要的东西（一个 code 加一个 trackId），所以都走 `Do`，
// data 段直接丢掉。
//
// ⚠️ 它们是**登录之后才有意义**的写操作：没登录时 B 站照样回 0，但什么也
// 不会记下来。所以「有没有登录」是上一层的事（`app/playreport.go`）。
//
// # aid 要另外问一次
//
// 两个请求体里都有 `aid`，Biu 直接发（他们队列项里带着这个字段）。我们手里
// 只有 `bvid`——本项目的 `app.Item` 只留 `bvid`/`cid`——而 bvid → aid 得抄
// B 站那套 58 进制表（`history.go` 的 `kidOf` 为什么不抄，写在同一段里）。
// 这一层把那一次换算交给**已经有的** `ArchiveID`；问不到就是这一首不记，
// 而别的什么都不受影响。
package bili

import (
	"context"
	"net/url"
	"strconv"
)

// 两条上报接口的路径。抄 Biu 上游那两个 service 文件
// （`src/service/click-interface-click-web-h5.ts`、`click-interface-web-heartbeat.ts`）。
const (
	playReportOpenPath = "/x/click-interface/click/web/h5"
	playReportBeatPath = "/x/click-interface/web/heartbeat"
)

const (
	// playTypeArchive 是「投稿视频」。两个接口都要它。
	playTypeArchive = 3
	// playSubTypeNormal 是「普通投稿」（不是番剧、不是课程）。
	playSubTypeNormal = 0
	// playDT 是两个接口里那个写死的 `dt` / `w_dt`，B 站按它认「网页端播放器」。
	playDT = 2
	// playOuter 写死 0（Biu 也是写死的 0）。
	playOuter = 0
	// playPlatform 是「从网页端发出来的」。
	playPlatform = "web"
	// playWebLocation 是 B 站的网页位置编码，默认值就是「视频详情页播放器」。
	playWebLocation = 1315873
)

// PlayAction 是心跳里的 `play_type`：这一下是「在干哪件事」。
//
// 取值抄 Biu（`play-report.ts` 的 `reportHeartbeat` 注释）和接口文档。
// **只有 PlayPlaying 会按节奏发**，其余四个都是「发生了什么就立刻发一次」——
// 界面上的 pause / resume / stop 就是要等这一发才落到 B 站。
type PlayAction int

const (
	// PlayPlaying 是「在放，心跳」。
	PlayPlaying PlayAction = 0
	// PlayStart 是「开始播放这一首」。
	PlayStart PlayAction = 1
	// PlayPause 是「暂停」。
	PlayPause PlayAction = 2
	// PlayResume 是「继续播放」。
	PlayResume PlayAction = 3
	// PlayEnd 是「结束播放」（切歌、停止、播完都走它）。
	PlayEnd PlayAction = 4
)

// String 是给日志看的名字。
func (a PlayAction) String() string {
	switch a {
	case PlayPlaying:
		return "播放中"
	case PlayStart:
		return "开始播放"
	case PlayPause:
		return "暂停"
	case PlayResume:
		return "继续播放"
	case PlayEnd:
		return "结束播放"
	}
	return "未知动作"
}

// PlayFnval 是我们上报的流类型。
//
// 🔴 **它必须和我们取流时发的那一个一样**（`stream.go` 的 `dashAll` = 4048）：
// 心跳报的是「这一轮我放的是哪一档流」，而我们放的和要的是同一个答案。
// 写成别的数，B 站那边看到的就是「说是无损、实际发的是 192K」。
const PlayFnval = dashAll

// PlayFinished 是「这一首播完了」的进度值。
//
// 接口文档写的是「视频播放进度（秒），完成为 -1」（上游
// `click-interface-web-heartbeat.ts` 的字段注释）。**只有真正放完了才发它**：
// 发早了，B 站会把历史记录里那一条标成「已看完」，而用户其实只听了一半。
const PlayFinished int64 = -1

// PlayReportOpen 是「开始看这一首」那一次请求的参数。
type PlayReportOpen struct {
	// Mid 是当前用户的 mid（就是 DedeUserID）。没登录时是 0，而那一栏
	// B 站收 0 也不拦（它是「不必要」参数）。
	Mid int64
	// Aid 是稿件 avid。**必填**（Biu 的那个接口把它列成非可选）。
	Aid int64
	// Bvid 是稿件号，用来拼 referer_url 和后面那一次心跳。
	Bvid string
	// Cid 是分 P id。正值为 0 时那一栏不发。
	Cid int64
	// Part 是第几集，从 1 起。0 表示「不知道」——那一栏不发，
	// 而 B 站要的是它来做分 P 归属，写一个假的 1 会把记录挂到第一集上。
	Part int
	// Session 是这一轮播放的会话 id（一串不带分隔符的小写十六进制）。
	Session string
	// StartTS 是开始播放的 Unix 秒。
	StartTS int64
}

// PlayReportOpen 发「开始看这一首」。
//
// ⚠️ 这是一次**写**：它没有 CSRF 会被 B 站拒成 -111，而拒了**不报错在界面上**
// ——表现是「历史记录那一页还是空的」，正好是我们要修的那个症状。
func (c *Client) PlayReportOpen(ctx context.Context, p PlayReportOpen) error {
	if p.Bvid == "" || p.Aid <= 0 {
		return ErrNoVideo
	}

	form := url.Values{}
	form.Set("aid", strconv.FormatInt(p.Aid, 10))
	form.Set("bvid", p.Bvid)
	form.Set("type", strconv.Itoa(playTypeArchive))
	form.Set("sub_type", strconv.Itoa(playSubTypeNormal))
	form.Set("session", p.Session)
	form.Set("ftime", strconv.FormatInt(p.StartTS, 10))
	form.Set("stime", strconv.FormatInt(p.StartTS, 10))
	form.Set("referer_url", "https://www.bilibili.com/video/"+p.Bvid)
	form.Set("outer", strconv.Itoa(playOuter))
	form.Set("platform", playPlatform)
	setInt64(form, "mid", p.Mid)
	setInt64(form, "cid", p.Cid)

	q := url.Values{}
	q.Set("w_aid", strconv.FormatInt(p.Aid, 10))
	q.Set("w_type", strconv.Itoa(playTypeArchive))
	q.Set("w_ftime", strconv.FormatInt(p.StartTS, 10))
	q.Set("w_stime", strconv.FormatInt(p.StartTS, 10))
	q.Set("web_location", strconv.Itoa(playWebLocation))
	if p.Part > 0 {
		form.Set("part", strconv.Itoa(p.Part))
		q.Set("w_part", strconv.Itoa(p.Part))
	}

	_, err := c.Do(ctx, Call{
		Method: "POST",
		URL:    c.hostAPI + playReportOpenPath,
		Query:  q,
		Form:   form,
		CSRF:   true,
		WBI:    true,
	})
	return err
}

// PlayReportBeat 是一次心跳的参数。
//
// `Played` / `Realtime` / `RealPlayed` / `MaxPlayed` 都是**秒**。
// `MaxPlayed` 是这一轮里到过的最远处（拖回去重听不会把它拽回来），
// B 站拿它算「看了多久」。
type PlayReportBeat struct {
	Mid int64
	Aid int64
	// Bvid / Cid 认是哪一稿的哪一集。
	Bvid string
	Cid  int64
	// Played 是这一下的进度。**放完了发 PlayFinished（-1）**。
	Played int64
	// Realtime / RealPlayed 是这一轮真实听过多少秒（页面会话口径）。
	Realtime   int64
	RealPlayed int64
	// VideoDuration 是总时长秒。
	VideoDuration int64
	// MaxPlayed 是这一轮到过的最远处。
	MaxPlayed int64
	// StartTS 是这一轮开始的 Unix 秒。
	StartTS int64
	Session string
	Action  PlayAction
}

// PlayReportBeat 发一次心跳。
func (c *Client) PlayReportBeat(ctx context.Context, b PlayReportBeat) error {
	if b.Bvid == "" || b.Session == "" {
		return ErrNoVideo
	}

	form := url.Values{}
	form.Set("bvid", b.Bvid)
	form.Set("type", strconv.Itoa(playTypeArchive))
	form.Set("sub_type", strconv.Itoa(playSubTypeNormal))
	form.Set("dt", strconv.Itoa(playDT))
	form.Set("outer", strconv.Itoa(playOuter))
	form.Set("session", b.Session)
	form.Set("start_ts", strconv.FormatInt(b.StartTS, 10))
	form.Set("played_time", strconv.FormatInt(b.Played, 10))
	form.Set("realtime", strconv.FormatInt(b.Realtime, 10))
	form.Set("real_played_time", strconv.FormatInt(b.RealPlayed, 10))
	form.Set("last_play_progress_time", strconv.FormatInt(b.MaxPlayed, 10))
	form.Set("max_play_progress_time", strconv.FormatInt(b.MaxPlayed, 10))
	form.Set("play_type", strconv.Itoa(int(b.Action)))
	// 🔴 这三个是一组，缺一不可：`fnval` 说「我放的是哪一类流」，`fnver`
	// 是它的版本（网页端一直是 0），`fourk` 是「允许 4K」那一位。
	// 我们的 `PlayFnval` 里已经含了 4K 那一档，所以 `fourk=1` 和它自洽。
	form.Set("fnval", strconv.Itoa(PlayFnval))
	form.Set("fnver", "0")
	form.Set("fourk", "1")
	setInt64(form, "aid", b.Aid)
	setInt64(form, "cid", b.Cid)
	setInt64(form, "mid", b.Mid)
	setInt64(form, "video_duration", b.VideoDuration)

	q := url.Values{}
	q.Set("w_aid", strconv.FormatInt(b.Aid, 10))
	q.Set("w_start_ts", strconv.FormatInt(b.StartTS, 10))
	q.Set("w_dt", strconv.Itoa(playDT))
	q.Set("w_realtime", strconv.FormatInt(b.Realtime, 10))
	q.Set("w_playedtime", strconv.FormatInt(b.Played, 10))
	q.Set("w_real_played_time", strconv.FormatInt(b.RealPlayed, 10))
	q.Set("w_last_play_progress_time", strconv.FormatInt(b.MaxPlayed, 10))
	q.Set("w_video_duration", strconv.FormatInt(b.VideoDuration, 10))
	q.Set("web_location", strconv.Itoa(playWebLocation))
	setInt64(q, "w_mid", b.Mid)

	_, err := c.Do(ctx, Call{
		Method: "POST",
		URL:    c.hostAPI + playReportBeatPath,
		Query:  q,
		Form:   form,
		CSRF:   true,
		WBI:    true,
	})
	return err
}

// setInt64 写一个可选的数字参数。
//
// **零值不写**，而不是写一个 `0` 出去：Biu 上游在发之前把
// `undefined / null / ""` 全剔掉了（`omitBy`），所以「没有这一栏」才是
// 上游真正发出去的形状。发 `0` 和不发是两回事——`mid=0` 在 B 站那里是
// 「匿名」，而这一栏缺席它自己知道怎么处理。
func setInt64(v url.Values, key string, n int64) {
	if n <= 0 {
		return
	}
	v.Set(key, strconv.FormatInt(n, 10))
}
