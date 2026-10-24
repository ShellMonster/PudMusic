package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
)

// 这一组测的是「拿到取流响应之后」的那半：挑哪一条流、有效期怎么算、
// B 站的错误码怎么变成人话。
//
// # 真网的形状是哪来的
//
// fixture 里的字段名和取值是**照着真 B 站的返回抄的**（`/x/player/wbi/playurl`
// 的 `dash.audio[].id/bandwidth/codecs/baseUrl/backupUrl`、
// `dash.flac.audio`、`dash.dolby.audio[]`，以及地址上的 `deadline`）。
// 抄的是 2026-10-16 未登录打真接口拿到的形状，见 `TestPlayURLAgainstTheRealBili`
// 里列的字段清单。
//
// 但**没有任何一条断言是在证明「B 站真的这么返回」**——那要靠真机回归。
// 这里证明的是：形状对不对、我们在各种失败形态下行为对不对。

const (
	testBvid = "BV1MbZnYoEk1"
	testCid  = "29165486316"
)

// fakePlayurl 造一份取流响应。
//
// dash 是「除了 dash 之外都跟真 B 站一样」的那层外壳。
func fakePlayurl(dash map[string]any) string {
	return `{"code":0,"message":"0","ttl":1,"data":{"from":"local","result":"suee",
	"quality":32,"format":"flv480","timelength":99056,
	"accept_quality":[120,112,80,64,32,16],"dash":` + mustJSON(dash) + `}}`
}

// audioItem 造一条 dash.audio 项。
func audioItem(id, bandwidth int, codecs, url string, backups ...string) map[string]any {
	m := map[string]any{
		"id": id, "bandwidth": bandwidth, "codecs": codecs,
		"mimeType": "audio/mp4", "baseUrl": url,
	}
	if len(backups) > 0 {
		m["backupUrl"] = backups
	}
	return m
}

// streamURL 造一个带 deadline 的 B 站 CDN 地址。
func streamURL(deadline time.Time, name string) string {
	return "https://upos-sz-mirrorhw.bilivideo.com/upgcxcode/16/63/" +
		testCid + "/29165486316-1-100047" + name +
		"?deadline=" + strconv.FormatInt(deadline.Unix(), 10) +
		"&upsig=2d39dc&uparams=e,mid,trid,deadline&oi=1699201781"
}

// playurlClient 建一台会回 dash 的假 B 站，顺手把 WBI 的 key 也备好。
func playurlClient(t *testing.T, dash map[string]any) (*fakeBili, *Client) {
	t.Helper()
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	f.on(playurlPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fakePlayurl(dash))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})
	return f, c
}

// —— 挑流 ——

// TestPlayURLTakesTheBestAvailable 盯「默认档给最好的那条」。
//
// 真 B 站给的是 30216 / 30232 / 30280 三条（2026-10-23 只读实测：码率
// ~44K / ~103K / ~204K，30216 的 codecs 是 `mp4a.40.5`）。默认（Auto）应该挑
// 30280。
func TestPlayURLTakesTheBestAvailable(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30216, 51651, "mp4a.40.5", streamURL(exp, "-30216.m4s")),
			audioItem(30232, 110836, "mp4a.40.2", streamURL(exp, "-30232.m4s")),
			audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-30280.m4s")),
		},
	})

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioAuto)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Quality != QualityHigh192 {
		t.Errorf("音质 = %d，want 30280", ref.Quality)
	}
	if ref.Label != "192K" {
		t.Errorf("徽章 = %q，want 192K", ref.Label)
	}
	if ref.Container != "mp4" {
		t.Errorf("容器 = %q，want mp4", ref.Container)
	}
}

// TestPlayURLPrefersFLAC 盯「无损优先」。
//
// B 站的 `dash.audio` 里**根本不含 FLAC**：无损只在 `dash.flac.audio` 那个
// 独立字段里。所以这一条既是「优先无损」的规则，也是「不查 flac 就永远拿不到
// 无损」的提醒——`fnval` 给 16 而不是 4048 的话，这里连字段都没有。
//
// ⚠️ 夹具里的 id 和 codecs 是**实测的真值**（2026-10-23 只读实测）：
// id=**30251**、codecs=**"fLaC"**、mimeType=`audio/mp4`。以前这里写的是
// 30257 和 `"flac"`——那个 id 在公开资料和实测里都找不到（票 #255）。
//
// ⚠️ **容器是 MP4 盒**（不是裸 FLAC）：见 `testdata/lossless/` 与
// `lossless_container_test.go`（issue #252）。
func TestPlayURLPrefersFLAC(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30280, 203786, "mp4a.40.2", streamURL(exp, "-192.m4s")),
		},
		"flac": map[string]any{
			"display": true,
			"audio":   audioItem(30251, 1_609_860, "fLaC", streamURL(exp, "-flac.m4s")),
		},
	})

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !ref.Quality.IsLossless() {
		t.Errorf("音质 = %d，want 无损", ref.Quality)
	}
	if ref.Quality != QualityHiResLossless {
		t.Errorf("音质 = %d，want %d（Hi-Res 无损）", ref.Quality, QualityHiResLossless)
	}
	if ref.Label != "Hi-Res 无损" {
		t.Errorf("徽章 = %q，want Hi-Res 无损", ref.Label)
	}
	// ⚠️ **容器是 MP4，不是裸 FLAC**：实测 B 站的无损流头几个字节是 `ftyp`
	// 盒（2026-10-23，issue #252）。以前这里写的是 "flac"，那是照着
	// 「B 站给的是裸 FLAC」那个（没验过的）说法写的。
	if ref.Container != "mp4" {
		t.Errorf("容器 = %q，want mp4（实测：B 站的无损流是 MP4 盒里的 FLAC）", ref.Container)
	}
}

// TestPlayURLTrustsTheFlacFieldNotTheIdInsideIt 盯「**字段比 id 权威**」。
//
// `dash.flac` 这个**字段名**就说明了「这是 FLAC 无损流」；字段**里面**的 id
// 是 B 站的内部编号，而且**改过**（老资料里是 30257，2026-10-23 实测是
// 30251）。
//
// 靠 id 猜的话，B 站一改 id，无损流就会挂上不相干的标签——票 #255 的症状就是
// 30251 被当成了「杜比 Hi-Res」，`IsLossless()` 也跟着判成不是无损。
//
// 所以这里故意造一个**表里没有的 id**：档位必须仍然是「Hi-Res 无损」。
func TestPlayURLTrustsTheFlacFieldNotTheIdInsideIt(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30280, 203786, "mp4a.40.2", streamURL(exp, "-192.m4s")),
		},
		"flac": map[string]any{
			"display": true,
			"audio":   audioItem(99999, 1_609_860, "fLaC", streamURL(exp, "-flac.m4s")),
		},
	})

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioAuto)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Quality != QualityHiResLossless {
		t.Errorf("音质 = %d，want %d（字段说了算，不看里面的 id）", ref.Quality, QualityHiResLossless)
	}
	if !ref.Quality.IsLossless() {
		t.Errorf("IsLossless() = false，want true")
	}
	if ref.Label != "Hi-Res 无损" {
		t.Errorf("徽章 = %q，want Hi-Res 无损", ref.Label)
	}
}

// TestPlayURLLosslessRefusesToFallBack 盯「要无损就必须要无损」。
//
// 静默降级比报错更糟：用户为无损付了钱，界面上还写着「Hi-Res 无损」，
// 而实际放的是 192K。Biu 上游这里是不管的（auto 与 lossless 都往下退）。
func TestPlayURLLosslessRefusesToFallBack(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-320.m4s")),
		},
	})

	_, err := c.PlayURL(context.Background(), testBvid, testCid, AudioLossless)
	if !errors.Is(err, ErrNoStream) {
		t.Fatalf("err = %v，want ErrNoStream", err)
	}
	if !strings.Contains(err.Error(), "无损") {
		t.Errorf("错里没有说「是因为没有无损」：%v", err)
	}
}

// TestPlayURLPicksTheNamedTier 盯用户点名的档位。
func TestPlayURLPicksTheNamedTier(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30216, 51651, "mp4a.40.5", streamURL(exp, "-64k.m4s")),
			audioItem(30232, 110836, "mp4a.40.2", streamURL(exp, "-128k.m4s")),
			audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-320k.m4s")),
		},
	})

	for _, tc := range []struct {
		pref AudioQuality
		want Quality
	}{
		{AudioHigh, QualityHigh192},
		{AudioMedium, QualityMid128},
		{AudioLow, QualityLow64},
	} {
		ref, err := c.PlayURL(context.Background(), testBvid, testCid, tc.pref)
		if err != nil {
			t.Fatalf("%v: %v", tc.pref, err)
		}
		if ref.Quality != tc.want {
			t.Errorf("%v 拿到的是 %d，want %d", tc.pref, ref.Quality, tc.want)
		}
	}
}

// TestPlayURLFallsBackWhenTheNamedTierIsMissing 盯点名那一档没有时的退路。
//
// 用户选 192K 但账号只能给 128K —— 退到 128K 是对的，界面上的徽章会跟着
// 变成「128K」，用户看得见。**报错反而更糟**（歌都放不了）。
func TestPlayURLFallsBackWhenTheNamedTierIsMissing(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30216, 51651, "mp4a.40.5", streamURL(exp, "-64k.m4s")),
			audioItem(30232, 110836, "mp4a.40.2", streamURL(exp, "-128k.m4s")),
		},
	})

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioHigh)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Quality != QualityMid128 {
		t.Errorf("音质 = %d，want 30232（退到能给的那条）", ref.Quality)
	}
}

// TestPlayURLNeverHandsBackABetterTierThanAsked 盯「要省流量别给高音质」。
//
// 上一条断的是「要高的没有 → 退到低的」，那是对的。这条断的是反过来的
// 那一半，同样重要：用户为了少流量点了 64K，B 站没给 64K，而旧写法会直接
// 取最好的那一条——也就是 192K。流量翻三倍，而界面上只多一个「192K」徽章，
// 用户多半不会注意到自己多花了钱。
//
// 退路是「不高于点名那一档里最好的」；一条都不低于点名的那档时才退到最低的
// 那条（宁可少要一档，也不要多给——用户可以再点一次提高）。
func TestPlayURLNeverHandsBackABetterTierThanAsked(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	// ⚠️ **不放 64K**：放了它，「要省流量」就会在点名命中那一档上短路，
	// 根本走不到退路，断的就不是这条要断的东西了。
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30232, 128000, "mp4a.40.2", streamURL(exp, "-128k.m4s")),
			audioItem(30280, 320000, "mp4a.40.2", streamURL(exp, "-320k.m4s")),
		},
	})

	for _, tc := range []struct {
		name string
		pref AudioQuality
		want Quality
	}{
		{"要省流量", AudioLow, QualityMid128},
		{"要标准", AudioMedium, QualityMid128},
		{"要高音质", AudioHigh, QualityHigh192},
	} {
		ref, err := c.PlayURL(context.Background(), testBvid, testCid, tc.pref)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if ref.Quality != tc.want {
			t.Errorf("%s（%v）拿到的是 %d，want %d", tc.name, tc.pref, ref.Quality, tc.want)
		}
	}
}

// TestPlayURLIgnoresStreamsWithoutAnAddress 盯「没有地址的流不算流」。
//
// 造的是**条目在、地址空**那一种：`dash.dolby` 里摆着一条杜比，`baseUrl` 是
// 空串。挑流的时候不把它剔掉，自动档就把它当成「有杜比」挑中，然后报一句
// 「这个稿件没有音频流」——**而 `dash.audio` 里好好的 192K 就在旁边摆着**。
//
// ⚠️ 别和「那一段是空的」搞混：真机实测过没有大会员时 B 站回的是
// `dash.dolby = {"type":0,"audio":[]}`、`dash.flac = null`，那种形状由
// `len(dolby.Audio) > 0` 那一句就挡住了，跟这里要断的不是一回事
// （用那个形状造这条用例的话，改动前后都是绿的）。
func TestPlayURLIgnoresStreamsWithoutAnAddress(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30280, 320000, "mp4a.40.2", streamURL(exp, "-320k.m4s")),
		},
		"dolby": map[string]any{"type": 2, "audio": []any{
			map[string]any{"id": 30250, "bandwidth": 448000, "codecs": "ec-3", "baseUrl": ""},
		}},
	})

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioAuto)
	if err != nil {
		t.Fatalf("自动档被一条没有地址的流带偏了：%v", err)
	}
	if ref.Quality != QualityHigh192 {
		t.Errorf("自动档拿到的是 %d，want 30280", ref.Quality)
	}
}

// TestPlayURLGivesTheOnlyTierThereIs 盯「全都比点名的那档高」。
//
// B 站只给了 192K，而用户点了「省流量」。这时没法不越级——那就给唯一的那条，
// 让徽章说出真相（用户看得见自己没省到流量，可以再决定）。
func TestPlayURLGivesTheOnlyTierThereIs(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30280, 320000, "mp4a.40.2", streamURL(exp, "-320k.m4s")),
		},
	})

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioLow)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Quality != QualityHigh192 {
		t.Errorf("音质 = %d，want 30280（只有这一档）", ref.Quality)
	}
	if ref.Label != "192K" {
		t.Errorf("徽章 = %q，want 192K（徽章要说实话，用户才知道没省到流量）", ref.Label)
	}
}

// —— 地址与有效期 ——

// TestPlayURLReadsTheDeadline 盯有效期是从地址上读出来的。
//
// B 站的 `deadline` 是一个 Unix 秒的查询参数。**读不到就当立刻过期**：
// 猜成「永不过期」的话换流那三条路径一次都不会跑，而真机上那种情况
// 表现是「播到一半静默停住」，比提前换流难查一百倍。
func TestPlayURLReadsTheDeadline(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio":    []any{audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, ".m4s"))},
	})

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !ref.ExpiresAt.Equal(exp) {
		t.Errorf("失效时刻 = %v，want %v", ref.ExpiresAt, exp)
	}
	if !ref.Usable(time.Unix(1702204169, 0)) {
		t.Error("刚拿到的流被判成不能用了")
	}
	if ref.Usable(exp) {
		t.Error("已经过期的流被判成还能用")
	}
}

// TestUsableLeavesAMarginBeforeTheDeadline 盯那条提前量。
//
// B 站的 deadline 是给 CDN 做粗粒度回收用的，最后一两分钟里仍然可能 403。
// 宁可早换一次（一次带签名的请求），也不要让用户在歌中间听到卡顿。
func TestUsableLeavesAMarginBeforeTheDeadline(t *testing.T) {
	now := time.Unix(1702204169, 0)
	ref := &StreamRef{URLs: []string{"https://x/y"}, ExpiresAt: now.Add(expiryMargin + 30*time.Second)}

	if !ref.Usable(now) {
		t.Error("离过期还有半分钟就被判死，太激进")
	}
	if ref.Usable(now.Add(expiryMargin + time.Minute)) {
		t.Error("离过期只剩一分钟了还判活，太松")
	}
}

func TestUsableRejectsEmptyAndDateless(t *testing.T) {
	var nilRef *StreamRef
	if nilRef.Usable(time.Now()) {
		t.Error("nil 也报能用")
	}
	if (&StreamRef{}).Usable(time.Now()) {
		t.Error("没有地址的也报能用")
	}
	// 没有 deadline 的地址一律当成已经过期。
	if (&StreamRef{URLs: []string{"https://x/y"}}).Usable(time.Now()) {
		t.Error("读不出失效时刻的地址被当成永不过期")
	}
}

// TestPlayURLKeepsBackupURLs 盯备用地址要留着。
//
// B 站给一条流通常配 2~3 个 CDN 节点。主地址 403 时换一条备用只要一次普通
// 请求，换流要调一次带 WBI 签名的接口——后者贵得多（`audio` 层那条
// 「先换备用地址再换流」的顺序靠这个）。
//
// Biu 上游只取 `baseUrl`，`backupUrl` 拿到了不用。
func TestPlayURLKeepsBackupURLs(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	_, c := playurlClient(t, map[string]any{
		"duration": 100,
		"audio": []any{audioItem(30280, 110836, "mp4a.40.2",
			streamURL(exp, "-main.m4s"), streamURL(exp, "-b1.m4s"), streamURL(exp, "-b2.m4s"))},
	})

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioAuto)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.URLs) != 3 {
		t.Fatalf("留了 %d 个地址，want 3", len(ref.URLs))
	}
	if !strings.Contains(ref.URLs[0], "-main") {
		t.Errorf("主地址不在第一个：%s", ref.URLs[0])
	}
}

// —— 身份 ——

// TestSameTrackStopsTheWrongSong 盯「换流换回来的还是不是这一首」。
//
// 取流接口在稿件被删、被替换成别的内容时会**静默**给一条看着完全正常的流。
// 不比一次的话，用户会在毫无提示的情况下听到另一首歌——而界面上什么都看不出来。
func TestSameTrackStopsTheWrongSong(t *testing.T) {
	a := &StreamRef{Bvid: "BV1", Cid: "1"}
	b := &StreamRef{Bvid: "BV1", Cid: "1"}
	c := &StreamRef{Bvid: "BV1", Cid: "2"} // 多 P 视频里换了一集
	d := &StreamRef{Bvid: "BV2", Cid: "1"}

	if !a.SameTrack(b) {
		t.Error("同一个 bvid/cid 被判成不同一首")
	}
	if a.SameTrack(c) {
		t.Error("换了分 P 还说是同一首")
	}
	if a.SameTrack(d) {
		t.Error("换了稿件还说是同一首")
	}
	if a.SameTrack(nil) {
		t.Error("和 nil 比说是同一首")
	}
}

// TestPlayURLNeedsBothBvidAndCid 盯 bvid 和 cid 都必填。
//
// 给 cid 时 B 站取到的是**哪一集**；不给 cid 它多半按第一集给，不报错——
// 表现是「点第三集放出来的是第一集」，而且没有任何提示。上游也把 cid 写成必填
// （`player-playurl.ts:9`）。
//
// 搜出来的歌只有 bvid，那不是这一层的事：`app.runOnline` 先问一次 `VideoPages`
// （`video.go`）把 cid 补上，问不出来就报错。**这一层不替它猜**——本地 fixture
// 放不放假参数，拿「反正会按第一集给」当依据是不成立的。
func TestPlayURLNeedsBothBvidAndCid(t *testing.T) {
	f, c := playurlClient(t, map[string]any{"duration": 1})
	for _, tc := range []struct{ bvid, cid string }{
		{"", testCid}, {testBvid, ""}, {"", ""},
	} {
		if _, err := c.PlayURL(context.Background(), tc.bvid, tc.cid, AudioAuto); !errors.Is(err, ErrNoStream) {
			t.Errorf("bvid=%q cid=%q：err = %v，want ErrNoStream", tc.bvid, tc.cid, err)
		}
	}
	if len(f.requests()) != 0 {
		t.Error("参数不全还发了请求")
	}
}

// —— 没有流可放 ——

func TestPlayURLSaysWhyThereIsNoStream(t *testing.T) {
	_, c := playurlClient(t, map[string]any{"duration": 100})
	_, err := c.PlayURL(context.Background(), testBvid, testCid, AudioAuto)
	if !errors.Is(err, ErrNoStream) {
		t.Fatalf("err = %v，want ErrNoStream", err)
	}
	// 「没有流」和「取不到」要说不同的话：前者让用户去检查收藏夹里那一首，
	// 后者让用户检查网络/权限。
	if got := Friendly(err); !strings.Contains(got, "音频流") {
		t.Errorf("人话 = %q，没有说清是「这一条没有音频流」", got)
	}
	// 🔴 **更不许被说成「没取到」**（票 #209）：说成网络问题的话，用户会去
	// 查网、换网、等它好，而这一条等一万年还是没有音频流——两件事做的
	// 动作正好相反。
	if got := Friendly(err); strings.Contains(got, "网络") || strings.Contains(got, "连不上") {
		t.Errorf("人话 = %q，把「真的没有音频流」说成了「没取到」", got)
	}
}

// —— B 站错误码 → 人话 ——

// TestEveryKnownCodeHasAHumanSentence 盯这张表是**全的**。
//
// 表里有的码，每一个都必须有一句能显示的话。少一条，那个码就会以
// 「B 站返回了一个看不懂的错误（-12345）」的形式出现在界面上。
func TestEveryKnownCodeHasAHumanSentence(t *testing.T) {
	for code := range codeMessages {
		err := &APIError{Code: code, Message: "", Endpoint: "/x/player/wbi/playurl"}
		got := Friendly(err)
		if got == "" {
			t.Errorf("码 %d 没有对应的话", code)
		}
		if strings.Contains(got, "%d") || strings.Contains(got, "看不懂") {
			t.Errorf("码 %d 落到了兜底上：%q", code, got)
		}
	}
}

// TestFriendlyNeverReturnsEmpty 盯「认不出来也要给一句人话」。
//
// 空串在播放条上表现为「打不开：」——那比任何一句兜底都更让人困惑。
func TestFriendlyNeverReturnsEmpty(t *testing.T) {
	for _, err := range []error{
		errors.New("something went sideways"),
		&APIError{Code: -99999, Message: "内部错误", Endpoint: "/x"},
		&APIError{Code: -404, Message: "", Endpoint: "/x"},
		&APIError{Code: -404, Message: "no such video", Endpoint: "/x"},
	} {
		if Friendly(err) == "" {
			t.Errorf("%v 变成了空串", err)
		}
	}
	if Friendly(nil) != "" {
		t.Error("nil 也给了话")
	}
}

// TestFriendlyPrefersOurOwnClassification 盯分类优先于原话。
//
// `-101` 的原始 message 是「账号未登录」。我们要说的是「登录已经过期了，
// 重新扫码登录一下」——带上下一步动作。前者只陈述事实。
func TestFriendlyPrefersOurOwnClassification(t *testing.T) {
	err := &APIError{Code: -101, Message: "账号未登录", Endpoint: "/x/web-interface/nav"}
	got := Friendly(err)
	if got == "账号未登录" {
		t.Errorf("用的是 B 站的原话，不是我们分类后的：%q", got)
	}
	if !strings.Contains(got, "登录") {
		t.Errorf("话里没有说下一步做什么：%q", got)
	}
}

// TestFriendlyKeepsBStationMessagesWhenTheyAreChinese 盯不认识的码仍能说人话。
//
// B 站自己的 message 多数是中文的（"稿件不可见"），比显示一个裸数字强。
func TestFriendlyKeepsBStationMessagesWhenTheyAreChinese(t *testing.T) {
	err := &APIError{Code: -62002, Message: "稿件不可见", Endpoint: "/x"}
	if got := Friendly(err); got != "稿件不可见" {
		t.Errorf("人话 = %q，want 沿用 B 站的中文说明", got)
	}
	// 表里有的码走我们的说法。
	if got := Friendly(&APIError{Code: -101, Message: "账号未登录"}); got == "账号未登录" {
		t.Error("表里有的码反而没走我们的说法")
	}
}

// TestFriendlyHidesCancellation 盯「用户自己切歌」不该报红字。
func TestFriendlyHidesCancellation(t *testing.T) {
	if got := Friendly(errors.New("audio: 字节源已关闭: context canceled")); got != "" {
		t.Errorf("取消被说成了 %q", got)
	}
	if got := Friendly(errors.New("client: 请求超时")); got == "" {
		t.Error("超时没有话")
	}
}

// TestFriendlySaysTheFormatCannotBeDecoded 盯「解不了」说人话（票 #282）。
//
// 底下是「OSStatus -11800」「不像 FLAC」这种行话——用户读不出来，也照着
// 做不了任何事。界面上要说的是「解不了」，并且给出**还能怎么办**。
//
// ⚠️ **也不许给「重试」**：再取一次流拿回来的还是同一条，还是解不了。
func TestFriendlySaysTheFormatCannotBeDecoded(t *testing.T) {
	err := fmt.Errorf("%w: extaf: AudioFileOpenWithCallbacks: OSStatus -11800",
		audio.ErrUnsupportedFormat)
	got := Friendly(err)
	if !strings.Contains(got, "解不了") {
		t.Errorf("人话 = %q，没有说清是「解不了」", got)
	}
	if strings.Contains(got, "OSStatus") || strings.Contains(got, "-11800") {
		t.Errorf("人话 = %q，把行话原样给用户了", got)
	}
	if Recoverable(err) {
		t.Error("解不了的流给了「可重试」——重试一万次还是这一句")
	}
}

// TestFriendlyDoesNotLeakSignedURLs 盯别把带签名的地址打出去。
//
// `APIError.Error()` 里带了 Endpoint（已经脱敏过），但 `Message` 是 B 站给的，
// 有时含 URL 片段。清洗那一层负责把明显不是人话的东西退掉。
func TestFriendlyDoesNotLeakSignedURLs(t *testing.T) {
	err := &APIError{
		Code: -12345,
		// 一句纯英文的机器消息 —— 里面带了一个签名地址。
		Message:  "failed at https://upos-sz.bilivideo.com/x.m4s?upsig=deadbeef",
		Endpoint: "/x/player/wbi/playurl",
	}
	got := Friendly(err)
	if strings.Contains(got, "upsig") {
		t.Errorf("人话里带出了签名地址：%q", got)
	}
}

// mustJSON 是把任意东西变成 JSON，测试里造 fixture 用的。
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestWBIKeysComeBackEvenWhenNotLoggedIn 盯「未登录也能签上名」。
//
// B 站的 nav 在没登录时回的是 `{"code":-101,"message":"账号未登录",
// "data":{"wbi_img":{...}}}`——**WBI key 就在错误里**。当成「取不到 key」的话，
// 未登录用户一个签名接口都调不了，而 `signWBI` 顶上那句「拿不到 key 就
// 原样放行」也永远没机会执行。
//
// （这一条是接在线取流时撞出来的真问题：真机上一调 PlayURL 就死在 nav 的
// -101 上，取流根本走不到。）
func TestWBIKeysComeBackEvenWhenNotLoggedIn(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		// code 是 -101，但 data 里带着 key。
		writeJSON(w, jsonData(t, -101, "账号未登录", map[string]any{
			"isLogin": false,
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	f.on(playurlPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("w_rid") == "" {
			t.Error("取流请求没有带 WBI 签名")
		}
		writeJSON(w, fakePlayurl(map[string]any{"duration": 1}))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	if _, err := c.PlayURL(context.Background(), testBvid, testCid, AudioAuto); !errors.Is(err, ErrNoStream) {
		// 这里要的不是「取到了流」（dash 是空的），而是「走到了取流接口」。
		t.Fatalf("err = %v，want ErrNoStream（说明请求发出去了）", err)
	}
	for _, rec := range f.requests() {
		if rec.Path == playurlPath && rec.Query.Get("w_rid") == "" {
			t.Fatal("nav 回 -101 之后，取流请求没有签名——未登录用户连接口都调不了")
		}
	}
}

// TestAPIErrorKeepsItsData 盯业务错误里那一段 data 没被丢掉。
//
// 「错误就是没有数据」是错的：B 站会在 code != 0 的时候照样带 data
// （未登录时的 wbi_img 就是这么来的）。
func TestAPIErrorKeepsItsData(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, -101, "账号未登录", map[string]any{
			"isLogin": false,
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	c := f.client(t, Options{})

	_, err := c.Do(context.Background(), Call{Method: "GET", URL: f.URL + pathNav})
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v，want *APIError", err)
	}
	if len(api.Data) == 0 {
		t.Fatal("业务错误把 data 丢了")
	}
	if !strings.Contains(string(api.Data), "wbi_img") {
		t.Errorf("data 里没有 wbi_img：%s", api.Data)
	}
}

// TestAgainstTheRealBiliStreamingAPIIsNotCovered 不是测试，是一份**欠账清单**。
//
// 上面每一条 fixture 都证明不了「B 站真的这么返回」。真机上验过的是
// 2026-10-16 未登录状态下打真接口的那一次，加上 2026-10-23 用大会员账号
// 验的无损流形状（issue #252，素材见 `testdata/lossless/`）——
// 记在这里免得下次重新踩。
func TestAgainstTheRealBiliStreamingAPIIsNotCovered(t *testing.T) {
	t.Log("以下只有 fixture 覆盖，必须真机回归：")
	t.Log("  1. dash.audio[].id / bandwidth / codecs / mimeType / baseUrl / backupUrl 的真值")
	t.Log("     —— 2026-10-23 实测（登录账号）：30216(~44K, mp4a.40.5) / 30232(~103K) / 30280(~204K)")
	t.Log("  2. fnval=4048 时 dash.flac 才会出现（fnval=16 时整个字段都没有）")
	t.Log("  3. 地址上的 deadline 是 Unix 秒；实测有效期 2 小时")
	t.Log("  4. dash.flac.audio 的 id / 容器 —— **验过了**（2026-10-23，issue #252：id 30251、codecs \"fLaC\"、容器是 MP4 盒，素材见 testdata/lossless/）")
	t.Log("  5. dash.dolby.audio[0] —— 实测 id 30250、codecs \"ec-3\"、容器是 MP4 盒（issue #252）")
	t.Log("     30251 那一档**也验过了**（issue #255）：它是 `dash.flac` 里的 Hi-Res 无损（codecs \"fLaC\"）")
	t.Log("     ——「杜比 Hi-Res = 30251」是二手映射表的说法，公开资料那一节写的是「30251 | Hi-Res无损」")
	t.Log("  6. playurl 接口带不带 WBI 签名都能通（实测不带也通，本实现仍然签）")
	t.Log("  7. 多个 P 时不给 cid 会取到第一集，且 code 仍是 0")
	t.Log("  8. 取到的地址从 CDN 拉要带 Referer（不给直接 403）")
}

// TestTheExpiryRuleIsOneRule 盯**两条取流路径用同一套有效期口径**（票 #220）。
//
// 投稿那一路（`/x/player/wbi/playurl`）和音乐库那一路（`sid`）问的是两个
// 接口，而它们的地址**从哪来**不一样：
//
//	投稿	地址上的 `deadline` 查询参数
//	音乐库	接口给的 `timeout`（有效秒数，一般三小时）
//
// ⚠️ **但判据必须是同一条**：地址上的 `deadline` 优先（最准），没有就用
// 接口给的秒数，都没有就**当成立刻失效**。而「当成立刻失效」这个方向是
// **保守的**——提前换流，而不是拿一条死地址放到歌播到一半。
//
// 这一条钉住那个**公共函数**：两路各写一份的话，迟早有一处漂移，而漂移的
// 那一处症状是「有时候放得出来、有时候放不出来」。
func TestTheExpiryRuleIsOneRule(t *testing.T) {
	base := time.Unix(1702204169, 0)
	// 地址上那个 deadline 是 1702208369 = base + 4200 秒。
	withDeadline := base.Add(4200 * time.Second)

	t.Run("地址上带 deadline 就用它", func(t *testing.T) {
		got := expiryOf([]string{"https://cdn/x/a.m4s?deadline=1702208369"}, 10800, base)
		if !got.Equal(withDeadline) {
			t.Errorf("失效时刻 = %v，want %v", got, withDeadline)
		}
	})

	t.Run("只有接口给的秒数就用它", func(t *testing.T) {
		got := expiryOf([]string{"https://cdn/x/a.m4a?upsig=1"}, 10800, base)
		if !got.Equal(base.Add(10800 * time.Second)) {
			t.Errorf("失效时刻 = %v，want %v", got, base.Add(10800*time.Second))
		}
	})

	t.Run("两个都没有就是立刻失效", func(t *testing.T) {
		got := expiryOf([]string{"https://cdn/x/a.m4a?upsig=1"}, 0, base)
		if !got.IsZero() {
			t.Errorf("失效时刻 = %v，want 零值（立刻失效）", got)
		}
	})

	t.Run("地址上没有 deadline 的那种也是立刻失效", func(t *testing.T) {
		got := expiryOf([]string{"https://cdn/x/a.m4a"}, 0, base)
		if !got.IsZero() {
			t.Errorf("失效时刻 = %v，want 零值（立刻失效）", got)
		}
	})
}
