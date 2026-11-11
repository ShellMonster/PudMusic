package bili

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 这一组测的是「B 站那一份字幕的地址」是怎么从取流响应里读出来的。
//
// # 素材是照着 B 站真的给的样子造的
//
// B 站在 `/x/player/wbi/playurl` 的返回里带着 `data.subtitle`：
//
//	"subtitle": {"allow_submit": false, "lan": "zh-CN",
//	             "subtitles": [{"id": 1, "lan": "zh-CN",
//	                            "subtitle_url": "//aisubtitle.hdslb.com/…json",
//	                            "subtitle_url_v2": "//aisubtitle.hdslb.com/…json?auth_key=…"}]}
//
// ⚠️ **地址是协议相对的**（前面没有 `https:`）——那是 B 站给的样子，而原样拿去
// 发请求的话，`http.NewRequest` 会把整串当成一个**路径**。

// playurlWithSubtitle 造一份带字幕的取流响应。
func playurlWithSubtitle(dash map[string]any, subtitle any) string {
	data := map[string]any{
		"from": "local", "result": "suee", "quality": 32, "format": "flv480",
		"timelength": 99056, "dash": dash,
	}
	if subtitle != nil {
		data["subtitle"] = subtitle
	}
	b, err := json.Marshal(map[string]any{"code": 0, "message": "0", "ttl": 1, "data": data})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// subtitleClient 建一台会回带字幕的取流响应的假 B 站。
func subtitleClient(t *testing.T, body string) *Client {
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
		writeJSON(w, body)
	})
	return f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})
}

// TestPlayURLHandsBackTheSubtitleItWasGiven 盯「取流那一趟带回来的字幕没有再被丢掉」。
//
// ⚠️ **这就是这一票的根因**：B 站在取流的返回里就带着字幕的地址，而 `playurlData`
// 以前只声明了三个字段，于是那一段**静悄悄地**被丢了——歌词只能靠用户在
// 「找歌词」里搜。
//
// 🔴 **两个地址都要留着**：B 站给同一份字幕两个（不带鉴权参数的那个、和带
// `auth_key` 的那个），而**不带鉴权的那个可能要登录态**——拿它去请求会 403，
// 而登录态在字幕那条路上根本到不了（cookie 的域是 `.bilibili.com`，字幕文件在
// `.hdslb.com`）。所以**只给第一个**的话，「取到了地址但下载 403」就变成
// 「这一整条自动歌词静悄悄地不出现」。
//
// 上游那个 `||` 兜底只管「地址为空」那一种（`get-lyrics.ts`）：
// `subtitles[0].subtitle_url || subtitles[0].subtitle_url_v2`——两个都有的时候
// 它只给第一个。**所以兜底必须落在下载那一层**（见
// `lyrics.FetchBiliSubtitle`）。
func TestPlayURLHandsBackTheSubtitleItWasGiven(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	c := subtitleClient(t, playurlWithSubtitle(map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-192.m4s")),
		},
	}, map[string]any{
		"allow_submit": false,
		"subtitles": []any{map[string]any{
			"id": 12345, "lan": "zh-CN", "lan_doc": "中文（中国）",
			"subtitle_url":    "//aisubtitle.hdslb.com/bfs/ai_subtitle/prod/one.json",
			"subtitle_url_v2": "//aisubtitle.hdslb.com/bfs/ai_subtitle/prod/one.json?auth_key=abc",
		}},
	}))

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioHigh)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/one.json",
		"https://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/one.json?auth_key=abc",
	}
	if len(ref.SubtitleURLs) != len(want) {
		t.Fatalf("字幕地址有 %d 个，want %d 个：%q", len(ref.SubtitleURLs), len(want), ref.SubtitleURLs)
	}
	for i, w := range want {
		if ref.SubtitleURLs[i] != w {
			t.Errorf("第 %d 个字幕地址 = %q，want %q", i, ref.SubtitleURLs[i], w)
		}
	}
}

// TestTheSecondSubtitleAddressIsKeptToo 盯「只有 v2 那一个地址」那一支。
//
// ⚠️ **它不是「v1 兜底」**（上游的 `||` 是「v1 空了就用 v2」），而是**两个都在
// 列表里、按该试的顺序排**——下载失败时要改试下一个（见
// `lyrics.FetchBiliSubtitle`）。
func TestTheSecondSubtitleAddressIsKeptToo(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	c := subtitleClient(t, playurlWithSubtitle(map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-192.m4s")),
		},
	}, map[string]any{
		"subtitles": []any{map[string]any{
			"subtitle_url":    "",
			"subtitle_url_v2": "//aisubtitle.hdslb.com/bfs/ai_subtitle/prod/two.json?auth_key=abc",
		}},
	}))

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioHigh)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/two.json?auth_key=abc"}
	if len(ref.SubtitleURLs) != len(want) || ref.SubtitleURLs[0] != want[0] {
		t.Errorf("字幕地址 = %q，want %q", ref.SubtitleURLs, want)
	}
}

// TestNoSubtitleMeansNoAddress 盯「B 站没给字幕」那一支。
//
// ⚠️ **空列表不是错误**：很多歌压根没有字幕，而那是**正常**的一种——界面上照旧
// 是「暂无歌词」加一个「找歌词」的入口。
func TestNoSubtitleMeansNoAddress(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	for _, c := range []struct {
		what string
		body string
	}{
		{"整个字段都没有", playurlWithSubtitle(map[string]any{
			"duration": 100,
			"audio": []any{
				audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-192.m4s")),
			},
		}, nil)},
		{"给的是空数组", playurlWithSubtitle(map[string]any{
			"duration": 100,
			"audio": []any{
				audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-192.m4s")),
			},
		}, map[string]any{"subtitles": []any{}})},
		{"给的是空地址", playurlWithSubtitle(map[string]any{
			"duration": 100,
			"audio": []any{
				audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-192.m4s")),
			},
		}, map[string]any{"subtitles": []any{map[string]any{"subtitle_url": ""}}})},
	} {
		cli := subtitleClient(t, c.body)
		ref, err := cli.PlayURL(context.Background(), testBvid, testCid, AudioHigh)
		if err != nil {
			t.Fatal(err)
		}
		if len(ref.SubtitleURLs) != 0 {
			t.Errorf("%s：字幕地址 = %q，want 空", c.what, ref.SubtitleURLs)
		}
	}
}

// TestAnAddressThatIsNotAnAddressIsIgnored 盯「不像地址的那一串」。
//
// ⚠️ **它发出去只会得到一个看不懂的错**，而「这一条没有字幕」是这一层能说清的
// 那一句。
func TestAnAddressThatIsNotAnAddressIsIgnored(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	c := subtitleClient(t, playurlWithSubtitle(map[string]any{
		"duration": 100,
		"audio": []any{
			audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-192.m4s")),
		},
	}, map[string]any{
		"subtitles": []any{map[string]any{"subtitle_url": "/bfs/ai_subtitle/prod/one.json"}},
	}))

	ref, err := c.PlayURL(context.Background(), testBvid, testCid, AudioHigh)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.SubtitleURLs) != 0 {
		t.Errorf("字幕地址 = %q，want 空", ref.SubtitleURLs)
	}
}

// TestSubtitleURLsAsksThePlayerInfoEndpoint 盯字幕地址是问播放器信息接口
// `/x/player/wbi/v2` 取的（playurl 响应里没有字幕，以前读那里永远是空的）。
//
// ⚠️ 音乐库的取流走的是另一个接口（`/audio/music-service-c/url`），那个返回里
// **没有**字幕——所以要问它背后那个投稿一次（同一个取流接口，只看 `subtitle`
// 那一段）。
func TestSubtitleURLsAsksThePlayerInfoEndpoint(t *testing.T) {
	exp := time.Unix(1702204169+7200, 0)
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	f.on("/x/player/wbi/v2", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, playurlWithSubtitle(map[string]any{
			"duration": 100,
			"audio": []any{
				audioItem(30280, 110836, "mp4a.40.2", streamURL(exp, "-192.m4s")),
			},
		}, map[string]any{
			"subtitles": []any{map[string]any{
				"subtitle_url": "//aisubtitle.hdslb.com/bfs/ai_subtitle/prod/three.json",
			}},
		}))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	urls, err := c.SubtitleURLs(context.Background(), testBvid, testCid)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/three.json"}
	if len(urls) != len(want) || urls[0] != want[0] {
		t.Errorf("字幕地址 = %q，want %q", urls, want)
	}

	// 身份不全的时候**不发请求**：问不出这一集是哪一集，重试一万次还是这一句。
	f.mu.Lock()
	before := len(f.seen)
	f.mu.Unlock()
	if urls, err := c.SubtitleURLs(context.Background(), "", testCid); err != nil || len(urls) != 0 {
		t.Errorf("没有 bvid 时不该问：%q %v", urls, err)
	}
	f.mu.Lock()
	after := len(f.seen)
	f.mu.Unlock()
	if after != before {
		t.Errorf("没有 bvid 时还是发了 %d 个请求", after-before)
	}
}

// 下面这几条盯的是「字幕地址问不到时不要静悄悄地当成没有字幕」。

// playerInfoFixture 建一台答播放器信息接口的假 B 站，并注册好 WBI key 的 nav。
func playerInfoFixture(t *testing.T) *fakeBili {
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
	return f
}

// playerInfoBody 造一份播放器信息接口的返回：身份字段 + 可选字幕。
func playerInfoBody(bvid string, cid any, subtitle any) string {
	data := map[string]any{"aid": 123456, "bvid": bvid, "cid": cid}
	if subtitle != nil {
		data["subtitle"] = subtitle
	}
	b, err := json.Marshal(map[string]any{"code": 0, "message": "0", "ttl": 1, "data": data})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// oneSubtitle 造一段只带一个地址的字幕。
func oneSubtitle(url string) map[string]any {
	return map[string]any{"subtitles": []any{map[string]any{"subtitle_url": url}}}
}

// TestSubtitleURLsRejectsAResponseForAnotherVideo 盯「返回的 bvid / cid 对不上
// 就不要那份字幕」。
//
// ⚠️ 这是这一层的身份核对：B 站把请求导到另一条视频上时，`subtitle` 里是
// **别人的**词。原样交出去的话，用户看到一段对不上的字，而日志里一句错都没有。
func TestSubtitleURLsRejectsAResponseForAnotherVideo(t *testing.T) {
	for _, tc := range []struct {
		what string
		bvid string
		cid  any
	}{
		{"bvid 对不上", "BV1xx411c7m0B", testCid},
		{"cid 对不上", testBvid, 999},
	} {
		t.Run(tc.what, func(t *testing.T) {
			f := playerInfoFixture(t)
			f.on("/x/player/wbi/v2", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, playerInfoBody(tc.bvid, tc.cid, oneSubtitle("//aisubtitle.hdslb.com/bfs/ai_subtitle/prod/wrong.json")))
			})
			c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

			urls, err := c.SubtitleURLs(context.Background(), testBvid, testCid)
			if err == nil {
				t.Fatalf("身份对不上还是交了字幕：%q", urls)
			}
			if !strings.Contains(err.Error(), testBvid) {
				t.Errorf("错误里没有说问的是哪一条：%v", err)
			}
			// 核对失败是「这个响应用不了」，不是「这个地址不通」——不往下一趟试。
			if n := countPath(f, "/x/player/wbi/v2"); n != 1 {
				t.Errorf("播放器信息问了 %d 次，want 1", n)
			}
			if n := countPath(f, "/x/player/v2"); n != 0 {
				t.Errorf("身份对不上还是问了旧接口 %d 次", n)
			}
		})
	}
}

// TestSubtitleURLsAcceptsTheMatchingVideo 盯「bvid / cid 都对得上就正常交字幕」。
func TestSubtitleURLsAcceptsTheMatchingVideo(t *testing.T) {
	f := playerInfoFixture(t)
	f.on("/x/player/wbi/v2", func(w http.ResponseWriter, r *http.Request) {
		// cid 用数字给：B 站两种形态都可能，核对不能只认字符串。
		writeJSON(w, playerInfoBody(testBvid, 29165486316, oneSubtitle("//aisubtitle.hdslb.com/bfs/ai_subtitle/prod/ok.json")))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	urls, err := c.SubtitleURLs(context.Background(), testBvid, testCid)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/ok.json"
	if len(urls) != 1 || urls[0] != want {
		t.Errorf("字幕地址 = %q，want %q", urls, want)
	}
}

// TestSubtitleURLsFallsBackWhenTheSignedAttemptFails 盯「签名那一趟报错时退到
// 不签名的那一趟」。
//
// ⚠️ **只有请求报错才退**：签名成功但没有字幕是「这一条没有字幕」，换一个
// 地址问也问不出别的来（见下一条）。
func TestSubtitleURLsFallsBackWhenTheSignedAttemptFails(t *testing.T) {
	f := playerInfoFixture(t)
	f.on("/x/player/wbi/v2", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("w_rid") != "" {
			writeJSON(w, jsonData(t, -400, "请求错误", nil))
			return
		}
		writeJSON(w, playerInfoBody(testBvid, testCid, oneSubtitle("//aisubtitle.hdslb.com/bfs/ai_subtitle/prod/unsigned.json")))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	urls, err := c.SubtitleURLs(context.Background(), testBvid, testCid)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/unsigned.json"
	if len(urls) != 1 || urls[0] != want {
		t.Errorf("字幕地址 = %q，want %q", urls, want)
	}
	if n := countPath(f, "/x/player/wbi/v2"); n != 2 {
		t.Errorf("播放器信息问了 %d 次，want 2（签名一次、不签名一次）", n)
	}
}

// TestSubtitleURLsFallsBackToTheLegacyEndpoint 盯「前两趟都报错时退到旧接口」。
func TestSubtitleURLsFallsBackToTheLegacyEndpoint(t *testing.T) {
	f := playerInfoFixture(t)
	f.on("/x/player/wbi/v2", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, -404, "啥都木有", nil))
	})
	f.on("/x/player/v2", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, playerInfoBody(testBvid, testCid, oneSubtitle("//aisubtitle.hdslb.com/bfs/ai_subtitle/prod/legacy.json")))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	urls, err := c.SubtitleURLs(context.Background(), testBvid, testCid)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/legacy.json"
	if len(urls) != 1 || urls[0] != want {
		t.Errorf("字幕地址 = %q，want %q", urls, want)
	}
	if n := countPath(f, "/x/player/wbi/v2"); n != 2 {
		t.Errorf("新接口问了 %d 次，want 2", n)
	}
	if n := countPath(f, "/x/player/v2"); n != 1 {
		t.Errorf("旧接口问了 %d 次，want 1", n)
	}
}

// TestSubtitleURLsStopsWhenTheSignedResponseHasNoSubtitle 盯「签名那一趟成功但
// 没有字幕时不再往下试」。
//
// ⚠️ 空列表 = 这一条没有字幕，是正常的一种；换成不签名的地址问一次，得到的
// 还是同一份「没有」。
func TestSubtitleURLsStopsWhenTheSignedResponseHasNoSubtitle(t *testing.T) {
	f := playerInfoFixture(t)
	f.on("/x/player/wbi/v2", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, playerInfoBody(testBvid, testCid, nil))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	urls, err := c.SubtitleURLs(context.Background(), testBvid, testCid)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 0 {
		t.Errorf("字幕地址 = %q，want 空", urls)
	}
	if n := countPath(f, "/x/player/wbi/v2"); n != 1 {
		t.Errorf("播放器信息问了 %d 次，want 1（没有字幕不算失败）", n)
	}
	if n := countPath(f, "/x/player/v2"); n != 0 {
		t.Errorf("没有字幕还是问了旧接口 %d 次", n)
	}
}
