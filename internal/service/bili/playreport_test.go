// 播放记录上报那两个接口的测试。
//
// ⚠️ 和这一包里其它 fixture 测试一样：这里没有一个断言在证明「B 站真的
// 这么返回」。它证明的是**我们发出去的形状**——打哪条路径、发的是不是表单、
// 那些必填参数有没有带、以及 `played_time=-1` 只在「播完了」那一下出现。
//
// 真接口的兼容性只能靠真机回归，而且**这一票的回归要登录态**——它往用户
// 真实的 B 站账号里写历史记录（见任务报告里「需要真网验」那一节）。

package bili

import (
	"context"
	"net/http"
	"testing"
)

// serveReport 让假 B 站答得上来上报。
func serveReport(t *testing.T, f *fakeBili, paths ...string) {
	t.Helper()
	for _, p := range paths {
		f.on(p, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, jsonData(t, 0, "0", map[string]any{"trackId": 1}))
		})
	}
	// WBI 签名要先打 nav 拿两把密钥，拿不到的话签名那一步直接就报错，
	// 连真正的请求都发不出去（`signWBI` 的注释）。
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"wbi_img": map[string]string{
			"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
			"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
		}}))
	})
}

// reportOf 取录下来的那一条上报请求。
func reportOf(t *testing.T, f *fakeBili, path string) recordedRequest {
	t.Helper()
	for _, r := range f.requests() {
		if r.Path == path {
			return r
		}
	}
	t.Fatalf("没有请求打过来：%s", path)
	return recordedRequest{}
}

// TestPlayReportOpenSendsEveryParam 「开始看这一首」那一条的参数集。
//
// 🔴 **少一个参数不会报错**：B 站照收，只是这一轮不进历史记录——而症状
// 是「历史记录那一页还是空的」，正好是这一票要修的那个症状，所以逐个钉住。
func TestPlayReportOpenSendsEveryParam(t *testing.T) {
	f := newFakeBili(t)
	serveReport(t, f, playReportOpenPath)

	err := f.client(t, Options{CSRF: func() string { return "jct" }}).
		PlayReportOpen(context.Background(), PlayReportOpen{
			Mid: 2333, Aid: 114514, Bvid: "BV1xx411c7mD", Cid: 98765,
			Part: 2, Session: "0123456789abcdef0123456789abcdef", StartTS: 1728000000,
		})
	if err != nil {
		t.Fatal(err)
	}

	req := reportOf(t, f, playReportOpenPath)
	want := map[string]string{
		"aid":         "114514",
		"bvid":        "BV1xx411c7mD",
		"cid":         "98765",
		"mid":         "2333",
		"part":        "2",
		"type":        "3", // 3 = 投稿视频
		"sub_type":    "0", // 0 = 普通投稿
		"session":     "0123456789abcdef0123456789abcdef",
		"ftime":       "1728000000",
		"stime":       "1728000000",
		"referer_url": "https://www.bilibili.com/video/BV1xx411c7mD",
		"outer":       "0",
		"platform":    "web",
	}
	for k, v := range want {
		if got := req.Form.Get(k); got != v {
			t.Errorf("表单里的 %s = %q，want %q", k, got, v)
		}
	}
	if got := req.Query.Get("web_location"); got != "1315873" {
		t.Errorf("web_location = %q，want 1315873（B 站的默认位置编码）", got)
	}
	if got := req.Query.Get("w_aid"); got != "114514" {
		t.Errorf("w_aid = %q，want 114514", got)
	}
	// 这是一次**写**：没 csrf 会被拒成 -111，而拒了不报在界面上。
	if req.Query.Get("csrf") != "jct" || req.Header.Get(CSRFHeaderName) != "jct" {
		t.Errorf("csrf 没带上：query=%q header=%q",
			req.Query.Get("csrf"), req.Header.Get(CSRFHeaderName))
	}
}

// TestPlayReportBeatSendsEveryParam 心跳那一条的参数集。
//
// `fnval` / `fnver` / `fourk` 是这一组里最要紧的三个：`fnval` 说「这一轮
// 我放的是哪一类流」，写错了 B 站记下来的是一条和实际不符的播放记录
// （说是无损、实际发的是 192K）。它们必须和我们取流时发的那个一样。
func TestPlayReportBeatSendsEveryParam(t *testing.T) {
	f := newFakeBili(t)
	serveReport(t, f, playReportBeatPath)

	err := f.client(t, Options{CSRF: func() string { return "jct" }}).
		PlayReportBeat(context.Background(), PlayReportBeat{
			Mid: 2333, Aid: 114514, Bvid: "BV1xx411c7mD", Cid: 98765,
			Played: 200, Realtime: 200, RealPlayed: 200,
			VideoDuration: 900, MaxPlayed: 240,
			StartTS: 1728000000, Session: "0123456789abcdef0123456789abcdef",
			Action: PlayPlaying,
		})
	if err != nil {
		t.Fatal(err)
	}

	req := reportOf(t, f, playReportBeatPath)
	want := map[string]string{
		"aid":                     "114514",
		"bvid":                    "BV1xx411c7mD",
		"cid":                     "98765",
		"mid":                     "2333",
		"played_time":             "200",
		"realtime":                "200",
		"real_played_time":        "200",
		"video_duration":          "900",
		"last_play_progress_time": "240",
		"max_play_progress_time":  "240",
		"start_ts":                "1728000000",
		"session":                 "0123456789abcdef0123456789abcdef",
		"play_type":               "0", // 0 = 播放中
		"dt":                      "2",
		"type":                    "3",
		"fnval":                   "4048",
		"fnver":                   "0",
		"fourk":                   "1",
	}
	for k, v := range want {
		if got := req.Form.Get(k); got != v {
			t.Errorf("表单里的 %s = %q，want %q", k, got, v)
		}
	}
	if got := req.Query.Get("w_playedtime"); got != "200" {
		t.Errorf("w_playedtime = %q，want 200", got)
	}
	if got := req.Query.Get("web_location"); got != "1315873" {
		t.Errorf("web_location = %q，want 1315873", got)
	}
	if req.Query.Get("csrf") != "jct" || req.Header.Get(CSRFHeaderName) != "jct" {
		t.Errorf("csrf 没带上：query=%q header=%q",
			req.Query.Get("csrf"), req.Header.Get(CSRFHeaderName))
	}
}

// TestTheFnvalIsTheOneWeAskStreamsFor 钉住「上报的 fnval = 取流的 fnval」。
//
// 两个数是同一个领域里的同一个答案。抄成两个字面量的话，先改的那个迟早
// 和另一个对不上，而对不上的后果在界面上一个字都看不出来。
func TestTheFnvalIsTheOneWeAskStreamsFor(t *testing.T) {
	if PlayFnval != dashAll {
		t.Errorf("上报的 fnval 是 %d，取流用的是 %d——这两个必须是同一个", PlayFnval, dashAll)
	}
}

// TestFinishedSendsMinusOne 「播完了」那一下发 -1。
//
// B 站靠 `played_time = -1` 把这一条标成「看完」，而历史记录页把这一条
// 摊给用户看。发别的数（0 尤其）就等于把没看完的东西记成看完了。
func TestFinishedSendsMinusOne(t *testing.T) {
	f := newFakeBili(t)
	serveReport(t, f, playReportBeatPath)

	err := f.client(t, Options{CSRF: func() string { return "jct" }}).
		PlayReportBeat(context.Background(), PlayReportBeat{
			Aid: 1, Bvid: "BV1", Played: PlayFinished,
			Session: "s", Action: PlayEnd,
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := reportOf(t, f, playReportBeatPath).Form.Get("played_time"); got != "-1" {
		t.Errorf("播完那一下的 played_time = %q，want -1", got)
	}
}

// TestEveryPlayTypeHasItsOwnNumber 把五个动作钉在各自的号上。
//
// 写错了不报错——B 站照收，只是这一轮在它那边的形状不对，而我们要量的
// 恰恰是「暂停被记成了继续」。
func TestEveryPlayTypeHasItsOwnNumber(t *testing.T) {
	want := map[PlayAction]string{
		PlayPlaying: "0",
		PlayStart:   "1",
		PlayPause:   "2",
		PlayResume:  "3",
		PlayEnd:     "4",
	}
	for act, num := range want {
		f := newFakeBili(t)
		serveReport(t, f, playReportBeatPath)
		err := f.client(t, Options{CSRF: func() string { return "jct" }}).
			PlayReportBeat(context.Background(), PlayReportBeat{
				Aid: 1, Bvid: "BV1", Session: "s", Action: act,
			})
		if err != nil {
			t.Fatal(err)
		}
		if got := reportOf(t, f, playReportBeatPath).Form.Get("play_type"); got != num {
			t.Errorf("%s 发出去的 play_type 是 %q，want %q", act, got, num)
		}
		f.Close()
	}
}

// TestReportingNeedsAVideo 缺稿件号就不该发请求。
//
// 发出去 B 站会回 0，而什么也不会记下来——一个「回成功但没做事」的请求，
// 比不发更坏：它让日志看着一切正常。
func TestReportingNeedsAVideo(t *testing.T) {
	f := newFakeBili(t)
	serveReport(t, f, playReportOpenPath, playReportBeatPath)
	c := f.client(t, Options{CSRF: func() string { return "jct" }})

	if err := c.PlayReportOpen(context.Background(), PlayReportOpen{Bvid: "", Aid: 1}); err == nil {
		t.Error("空 bvid 也当成功")
	}
	if err := c.PlayReportOpen(context.Background(), PlayReportOpen{Bvid: "BV1", Aid: 0}); err == nil {
		t.Error("没有 aid 也当成功")
	}
	if err := c.PlayReportBeat(context.Background(), PlayReportBeat{Bvid: "BV1", Session: ""}); err == nil {
		t.Error("空会话 id 也当成功")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("缺参数还是发了 %d 次请求", n)
	}
}
