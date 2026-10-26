// 「下载视频」那一条在取流这一侧的解析。
//
// # 和 `stream_test.go` 同样的规矩
//
// fixture 里的字段名和取值照着真 B 站的返回抄（`dash.video[].id/bandwidth/
// codecs/mimeType/width/height/frameRate/baseUrl/backupUrl`），但**没有
// 一条断言是在证明「B 站真的这么返回」**——那要靠真机回归。这里证明的
// 是：形状对不对、挑得出挑不出、以及**挑错的时候有没有说人话**。
//
// 这一组走的是真的 HTTP 那条路（`playurlClient`），跟 `stream_test.go`
// 一样——WBI 签名和 `data` 那层外壳都被包在里面，绕开它们就等于在测
// 一条不存在的路。

package bili

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// videoItem 造一条 `dash.video[]` 项。
func dashVideoItem(id, bandwidth, w, h int, url string) map[string]any {
	return map[string]any{
		"id": id, "bandwidth": bandwidth, "codecs": "avc1.640028",
		"mimeType": "video/mp4", "width": w, "height": h,
		"frameRate": "30", "baseUrl": url,
	}
}

// dashWithVideo 造一份「最高 80 档 + 一条有损音频」的 dash 段。
func dashWithVideo(videos ...any) map[string]any {
	return map[string]any{
		"duration": 100,
		"video":    videos,
		"audio": []any{
			audioItem(30280, 110836, "mp4a.40.2", streamURL(time.Now().Add(time.Hour), "-30280.m4s")),
		},
	}
}

// dashClient 造一台假 B 站，回这份 dash。
func dashClient(t *testing.T, dash map[string]any) *Client {
	t.Helper()
	_, c := playurlClient(t, dash)
	return c
}

// 一条正常响应：挑出视频流，身份对得上，宽高记下来了。
func TestPlayDashPicksTheVideoAndKeepsItsIdentity(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	c := dashClient(t, dashWithVideo(
		dashVideoItem(80, 2_000_000, 1920, 1080, streamURL(exp, "-v80.m4s")),
		dashVideoItem(16, 300_000, 640, 360, streamURL(exp, "-v16.m4s")),
	))

	pair, err := c.PlayDash(context.Background(), testBvid, testCid, VideoQualityUnknown, AudioHigh)
	if err != nil {
		t.Fatalf("取流：%v", err)
	}
	if pair.Video == nil || pair.Audio == nil {
		t.Fatal("只拿到了一条流")
	}
	// 挑的是**最高**的那一档，而不是地址里出现得最早的那一条。
	if pair.Video.Quality != Quality(80) {
		t.Errorf("挑的是第 %d 档，要的是最高的 80", pair.Video.Quality)
	}
	if !strings.Contains(pair.Video.URLs[0], "-v80.m4s") {
		t.Errorf("拿到的地址是 %q，指的不是 80 档那一条", pair.Video.URLs[0])
	}
	// 身份要对：交织那一步会**逐条**查，而取错了的症状是「能播、
	// 播两分钟就没声」。
	if pair.Video.Bvid != testBvid || pair.Video.Cid != testCid {
		t.Errorf("视频流的身份是 %s/%s，要的是 %s/%s",
			pair.Video.Bvid, pair.Video.Cid, testBvid, testCid)
	}
	if pair.Audio.Bvid != testBvid || pair.Audio.Cid != testCid {
		t.Errorf("音频流的身份是 %s/%s，要的是 %s/%s",
			pair.Audio.Bvid, pair.Audio.Cid, testBvid, testCid)
	}
	// 宽高只是记录，但**记错**会让诊断文件说谎（「为什么这个 MV 这么大」
	// 的第一个答案就没了）。
	if pair.Video.Width != 1920 || pair.Video.Height != 1080 {
		t.Errorf("记下的分辨率是 %dx%d，要的是 1920x1080", pair.Video.Width, pair.Video.Height)
	}
	if pair.Video.Label != "1080P" {
		t.Errorf("画质徽章是 %q", pair.Video.Label)
	}
	// 视频流不是音频：容器名不能记成 `.m4a` 那一套（见 `containerOf`）。
	if pair.Video.Container != "mp4" {
		t.Errorf("视频流记下的容器名是 %q", pair.Video.Container)
	}
}

// 没有视频流要说人话。
//
// 断的是**那句话**：笼统的「没能取到流」用户不知道该干什么，而「这个
// 稿件没有视频流」直接告诉他这一条走不通。
func TestPlayDashSaysSoWhenThereIsNoVideo(t *testing.T) {
	c := dashClient(t, dashWithVideo())
	_, err := c.PlayDash(context.Background(), testBvid, testCid, VideoQualityUnknown, AudioHigh)
	if err == nil {
		t.Fatal("没有视频流也取成功了")
	}
	if !strings.Contains(err.Error(), "没有视频流") {
		t.Errorf("报错没说是没有视频流：%v", err)
	}
	if !strings.Contains(err.Error(), testBvid) {
		t.Errorf("报错没说是哪一稿：%v", err)
	}
}

// 有条目但没有地址的不算一条流。
//
// 这是 `playable` 那条注释里说的同一类形状：条目在、`baseUrl` 是空的。
// 挑中它的话后面会拿着一个空地址去请求，报「没有视频流」——而真正的
// 1080P 就在旁边摆着，用户看到的是「这一条走不通」。
func TestPlayDashSkipsAVideoEntryWithNoAddress(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	c := dashClient(t, dashWithVideo(
		dashVideoItem(120, 9_000_000, 3840, 2160, ""), // 条目在，地址是空的
		dashVideoItem(80, 2_000_000, 1920, 1080, streamURL(exp, "-v80.m4s")),
	))
	pair, err := c.PlayDash(context.Background(), testBvid, testCid, VideoQualityUnknown, AudioHigh)
	if err != nil {
		t.Fatalf("取流：%v", err)
	}
	if pair.Video.Quality != Quality(80) {
		t.Errorf("挑的是第 %d 档（大概挑中了那条没有地址的 4K），要的是 80", pair.Video.Quality)
	}
}

// 备用地址要跟上。
//
// B 站给一条流通常配 2~3 个 CDN 节点；主地址 403 时先试备用的，而
// 「备用的没被抄下来」表现为「视频下载总是 403」，而真因是少了一段。
func TestPlayDashKeepsTheBackupURLs(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	v := dashVideoItem(80, 2_000_000, 1920, 1080, streamURL(exp, "-v80.m4s"))
	v["backupUrl"] = []any{
		streamURL(exp, "-v80-b1.m4s"),
		streamURL(exp, "-v80-b2.m4s"),
	}
	c := dashClient(t, dashWithVideo(v))
	pair, err := c.PlayDash(context.Background(), testBvid, testCid, VideoQualityUnknown, AudioHigh)
	if err != nil {
		t.Fatalf("取流：%v", err)
	}
	if len(pair.Video.URLs) != 3 {
		t.Fatalf("视频流只有 %d 个地址，要的是 3（主 + 两个备用）", len(pair.Video.URLs))
	}
	if !strings.Contains(pair.Video.URLs[1], "-v80-b1.m4s") {
		t.Errorf("第二个地址是 %q，不是第一个备用", pair.Video.URLs[1])
	}
}

// 视频有、音频没有：也要说清楚是哪一条缺。
func TestPlayDashSaysSoWhenThereIsNoAudio(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	d := dashWithVideo(dashVideoItem(80, 2_000_000, 1920, 1080, streamURL(exp, "-v80.m4s")))
	d["audio"] = []any{}
	c := dashClient(t, d)
	_, err := c.PlayDash(context.Background(), testBvid, testCid, VideoQualityUnknown, AudioHigh)
	if err == nil {
		t.Fatal("没有音频流也取成功了")
	}
	// 报的是音频那句（`refFrom` 那边），而**不是**「没有视频流」——
	// 说反了用户会去检查稿件有没有视频，而问题在音频。
	if strings.Contains(err.Error(), "没有视频流") {
		t.Errorf("缺的是音频，报的却是视频：%v", err)
	}
}

// `qn` 该不该出现在请求里。
//
// 取流接口**不带** `qn` 时 B 站把所有档都返回来，由客户端挑；带了 `qn`
// 就是让服务端只给那一档。
//
// ⚠️ 两条都断。只断「默认不带」的话，点名那一档忘了传 `qn` 测试照样绿
// （请求照样发出去、照样下到东西，只是画质不是用户要的那一档）。
func TestPlayDashSendsTheQualityOnlyWhenAsked(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	dash := dashWithVideo(dashVideoItem(80, 2_000_000, 1920, 1080, streamURL(exp, "-v80.m4s")))
	f, c := playurlClient(t, dash)

	var mu sync.Mutex
	var qns []string
	f.on(playurlPath, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		qns = append(qns, r.URL.Query().Get("qn"))
		mu.Unlock()
		writeJSON(w, fakePlayurl(dash))
	})

	for _, tc := range []struct {
		name string
		qn   VideoQuality
		want string
	}{
		{"默认不带 qn", VideoQualityUnknown, ""},
		{"点名的档要出现在请求里", VideoQuality720, strconv.Itoa(int(VideoQuality720))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			qns = nil
			mu.Unlock()
			if _, err := c.PlayDash(context.Background(), testBvid, testCid, tc.qn, AudioHigh); err != nil {
				t.Fatalf("取流：%v", err)
			}
			mu.Lock()
			got := append([]string(nil), qns...)
			mu.Unlock()
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("取流请求里的 qn 记到的是 %v，要的是 [%q]", got, tc.want)
			}
		})
	}
}

// 画质徽章的文字要认得出来。
func TestVideoQualityLabels(t *testing.T) {
	for _, tc := range []struct {
		q    VideoQuality
		want string
	}{
		{VideoQuality360, "360P"},
		{VideoQuality480, "480P"},
		{VideoQuality720, "720P"},
		{VideoQuality1080, "1080P"},
		{VideoQuality1080P, "1080P+"},
		{VideoQuality4K, "4K"},
		{VideoQualityDolby, "杜比视界"},
		{VideoQualityUnknown, "未知画质"},
	} {
		if got := tc.q.Label(); got != tc.want {
			t.Errorf("%d 的徽章是 %q，要的是 %q", tc.q, got, tc.want)
		}
	}
}

// TestPlayDashRespectsTheQualityTheUserPicked 钉住「点名的那一档算数」。
//
// ⚠️ 这一条是被评审逮出来的（第一版 `pickVideo` 根本没有 `want` 这个参数，
// 拿的是响应里最高的那一档）。
//
// 它躲过了所有「能下下来」的验收，因为 `qn` **确实**被发到了请求上——
// 只是 `fnval=4048` + `fourk=1` 会让 B 站**把别的档一并返回**，而客户端
// 那个「不超过点名的那一档」的判断没有。
//
// 后果很具体：用户点了「下载 360P 省流量」，拿回来的是 4K（几百 MB），
// 而界面上从头到尾写着 360P。**要靠 `qn` 去相信服务端已经替我们限好了**，
// 那是把用户的选择交给接口的默认行为。
func TestPlayDashRespectsTheQualityTheUserPicked(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	// 响应里有 4K、1080P、720P、360P 四档（B 站带 `fourk=1` 时就是全给）。
	dash := dashWithVideo(
		dashVideoItem(120, 9_000_000, 3840, 2160, streamURL(exp, "-v120.m4s")),
		dashVideoItem(80, 2_000_000, 1920, 1080, streamURL(exp, "-v80.m4s")),
		dashVideoItem(64, 900_000, 1280, 720, streamURL(exp, "-v64.m4s")),
		dashVideoItem(16, 300_000, 640, 360, streamURL(exp, "-v16.m4s")),
	)
	c := dashClient(t, dash)

	for _, tc := range []struct {
		name string
		qn   VideoQuality
		want int
	}{
		{"点名 360P 就给 360P", VideoQuality360, 16},
		{"点名 720P 就给 720P", VideoQuality720, 64},
		{"点名 1080P 就给 1080P", VideoQuality1080, 80},
		{"没点名就给最好的（4K）", VideoQualityUnknown, 120},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair, err := c.PlayDash(context.Background(), testBvid, testCid, tc.qn, AudioHigh)
			if err != nil {
				t.Fatalf("取流：%v", err)
			}
			if got := int(pair.Video.Quality); got != tc.want {
				t.Errorf("挑的是第 %d 档，要的是 %d", got, tc.want)
			}
		})
	}
}

// TestPlayDashTakesTheLeastBadWhenNoneIsLowEnough 钉住「一档都不满足时往
// 哪边退」。
//
// 用户点名 360P，而这个稿件最低只给到 480P——`bestNotAbove` 一条都不剩。
// 这时候给**最低的那一档**（480P），不是最高的（1080P）。
//
// 为什么这条值得单列：`fnval=4048` + `fourk=1` 会让 B 站把整条阶梯都返回，
// 所以「一档都不满足」在真网上很常见（很多老稿件没有 360P 那一档）。
// 而**往高里退**是最坏的方向：用户明明白白选了「省流量」，结果拿回
// 一个几百 MB 的 1080P，而界面上从头到尾写着 360P。
//
// ⚠️ 素材要**三档以上**而且都高于点名的那一档：只有两档时「最低」和
// 「最高」是同一档，这个用例就白写了（第一版就是这么写的，跑了两遍
// 才发现它压根没走到那个分支）。
func TestPlayDashTakesTheLeastBadWhenNoneIsLowEnough(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	// 点名 360P；稿件只给 480P / 720P / 1080P，一档都不满足。
	c := dashClient(t, dashWithVideo(
		dashVideoItem(16, 300_000, 640, 360, ""), // 有一档没地址，挡掉
		dashVideoItem(80, 2_000_000, 1920, 1080, streamURL(exp, "-v80.m4s")),
		dashVideoItem(64, 900_000, 1280, 720, streamURL(exp, "-v64.m4s")),
		dashVideoItem(32, 500_000, 854, 480, streamURL(exp, "-v32.m4s")),
	))
	pair, err := c.PlayDash(context.Background(), testBvid, testCid, VideoQuality360, AudioHigh)
	if err != nil {
		t.Fatalf("取流：%v", err)
	}
	if got := int(pair.Video.Quality); got != 32 {
		t.Errorf("挑的是第 %d 档，要的是这几档里最低的 32（480P）"+
			"——往高里退的话用户明选了「省流量」却拿回一个几百 MB 的 1080P", got)
	}
}

func TestPlayVideoPrefersH264OverHigherHEVC(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	hevc := dashVideoItem(80, 2_000_000, 1920, 1080, streamURL(exp, "-hevc80.m4s"))
	hevc["codecs"] = "hev1.1.6.L120.90"
	avc := dashVideoItem(64, 1_000_000, 1280, 720, streamURL(exp, "-avc64.m4s"))
	c := dashClient(t, dashWithVideo(hevc, avc))
	ref, err := c.PlayVideo(context.Background(), testBvid, testCid, VideoQuality1080)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref.Codecs, "avc1") || ref.Height != 720 {
		t.Fatalf("got %s %dp, want the H.264 720p", ref.Codecs, ref.Height)
	}
}

func TestPlayVideoFallsBackWhenThereIsNoH264(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	hevc := dashVideoItem(64, 1_000_000, 1280, 720, streamURL(exp, "-hevc64.m4s"))
	hevc["codecs"] = "hev1.1.6.L120.90"
	c := dashClient(t, dashWithVideo(hevc))
	ref, err := c.PlayVideo(context.Background(), testBvid, testCid, VideoQuality720)
	if err != nil || !strings.HasPrefix(ref.Codecs, "hev1") {
		t.Fatalf("ref=%+v err=%v", ref, err)
	}
}

func TestPlayVideoListOffersEveryTierAndHonoursTheCap(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	c := dashClient(t, dashWithVideo(
		dashVideoItem(80, 2_000_000, 1920, 1080, streamURL(exp, "-80.m4s")),
		dashVideoItem(32, 500_000, 852, 480, streamURL(exp, "-32.m4s")),
		dashVideoItem(64, 1_000_000, 1280, 720, streamURL(exp, "-64.m4s")),
	))
	ref, list, err := c.PlayVideoList(context.Background(), testBvid, testCid, VideoQuality720)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Height != 720 {
		t.Errorf("上限 720P 却给了 %dp", ref.Height)
	}
	if len(list) != 3 || list[0] != VideoQuality480 || list[2] != VideoQuality1080 {
		t.Errorf("可选档 %v，应当是 480/720/1080 由低到高", list)
	}
}
