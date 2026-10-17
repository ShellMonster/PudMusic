// B 站音乐库（sid）这一组的测试。
//
// ⚠️ 和 collection_test.go 顶上那句话一样：这里**没有一个断言在证明「B 站真的
// 这么返回」**。它证明的是协议形状（我们发的参数对不对、我们读的字段对不对）
// 以及**失败形态下**的行为对不对。真接口的兼容性只能靠真机回归。
//
// # 🔴 fixture 的外壳用的是 `msg` 不是 `message`
//
// 音乐库这两个接口（`audio/music-service-c/url` 和 `/web/song/info`）回的是
// `msg`，而收藏夹那些回的是 `message`。照着 `jsonData` 造的话
// `envelope.Message` 是空串——而真接口 code≠0 时中文原因就在 `msg` 里，
// 造错形状的后果是「错的那一句永远测不到」。

package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSid = "1293494"

// musicEnvelope 造音乐库那几个接口的外壳：`msg` 而不是 `message`。
//
// ⚠️ **`data` 那个键永远在**（没有值时是 `null`）：真接口在「歌没了」的场合
// 回的是 `{"code":0,"msg":"success","data":null}`，省掉那个键的话
// `Client.Do` 给回来的 RawMessage 是空的，症状是「返回读不懂」而不是
// 「这一首不在了」。
func musicEnvelope(t *testing.T, code int, msg string, data any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"code": code, "msg": msg, "ttl": 1, "data": data,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// songURLDataFor 造一份取流响应。
//
// tier 是 B 站**实际给回来的**那一档（请求的是 `want` 时这里应该相等；
// 不相等就是「没权限，退给了试听片段」那种形状）。
func songURLDataFor(tier int, cdns ...string) map[string]any {
	return map[string]any{
		"sid":     1293494,
		"type":    tier,
		"info":    "",
		"timeout": 10800,
		"size":    12345678,
		"cdns":    cdns,
		"title":   "月光",
		"cover":   "https://i0.hdslb.com/cover.jpg",
		"qualities": []any{
			musicQualityItem(3, 1), musicQualityItem(2, 0), musicQualityItem(1, 0), musicQualityItem(0, 0),
		},
	}
}

// songURL 造一个音乐库 CDN 地址。
func songURL(base, name string, deadline time.Time) string {
	return base + "/cdn/" + name + "?deadline=" +
		strconv.FormatInt(deadline.Unix(), 10) + "&upsig=deadbeef"
}

// ─────────────────────────────────────────────────────────────────────────────
// 元信息
// ─────────────────────────────────────────────────────────────────────────────

// TestSongInfoReadsTheFieldsThePlaybarNeeds 盯元信息那几个字段。
//
// 作者名和 UP 主名**分开测**：B 站的 `author`（这首歌的作者）和 `uname`
// （投稿它的 UP 主）多半不是同一个人，抄成同一个字段的表现是界面上挂着一个
// 陌生人的名字。
func TestSongInfoReadsTheFieldsThePlaybarNeeds(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathSongInfo, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("sid"); got != testSid {
			t.Errorf("sid = %q，want %s", got, testSid)
		}
		writeJSON(w, musicEnvelope(t, 0, "success", map[string]any{
			"id": 1293494, "uid": 900, "uname": "某某音乐台",
			"author": "夜灯", "title": "月光", "cover": "https://i0.hdslb.com/c.jpg",
			"duration": 214, "aid": 0, "bvid": "", "cid": 0,
			"collectIds": []int{11, 22},
		}))
	})

	got, err := f.client(t, Options{}).SongInfo(context.Background(), testSid)
	if err != nil {
		t.Fatal(err)
	}
	want := SongInfo{
		Sid: testSid, Title: "月光", Cover: "https://i0.hdslb.com/c.jpg",
		Author: "夜灯", Uname: "某某音乐台", Duration: 214 * time.Second,
		CollectFolders: []int64{11, 22},
	}
	if got.Sid != want.Sid || got.Title != want.Title || got.Cover != want.Cover ||
		got.Author != want.Author || got.Uname != want.Uname || got.Duration != want.Duration {
		t.Errorf("SongInfo =\n %+v\nwant\n %+v", *got, want)
	}
	if fmt.Sprint(got.CollectFolders) != fmt.Sprint(want.CollectFolders) {
		t.Errorf("CollectFolders = %v，want %v", got.CollectFolders, want.CollectFolders)
	}
}

// TestSongInfoReadsSecondsNotMilliseconds 单盯时长。
//
// B 站的 duration 是**秒**。当毫秒读的话一首三分半的歌是 214 毫秒，
// 而界面上没有任何一处会露出错——歌放完了进度条才走了 0.2 秒。
func TestSongInfoReadsSecondsNotMilliseconds(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathSongInfo, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, musicEnvelope(t, 0, "success", map[string]any{
			"id": 1293494, "duration": 214,
		}))
	})
	got, err := f.client(t, Options{}).SongInfo(context.Background(), testSid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Duration != 214*time.Second {
		t.Errorf("时长 = %v，want 3m34s", got.Duration)
	}
}

// TestSongInfoSaysWhyWhenTheSongIsGone 下架的条目要说得清。
//
// code 7201006 是「这首音乐不存在或者已下架」。`Friendly` 认得它，所以这里只
// 盯「错被原样传上去」——包装时把 `msg` 丢了的话，界面上落到的就是
// 「B 站返回了一个看不懂的错误（7201006）」。
func TestSongInfoSaysWhyWhenTheSongIsGone(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathSongInfo, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, musicEnvelope(t, 7201006, "该音频不存在或已被下架", nil))
	})
	_, err := f.client(t, Options{}).SongInfo(context.Background(), testSid)
	var api *APIError
	if !errors.As(err, &api) || api.Code != 7201006 {
		t.Fatalf("err = %v，want 一个 code=7201006 的 APIError", err)
	}
	if api.Message == "" {
		t.Error("err.Message 是空的：B 站在 msg 里写了原因，没被读进来")
	}
	if got := Friendly(err); got != "这首音乐不存在或者已下架" {
		t.Errorf("给用户的话 = %q，want「这首音乐不存在或者已下架」", got)
	}
}

// TestSongInfoRejectsAnEmptyDataSegment code 是 0 但 data 是 null。
//
// 那种回答能反序列化成一份「标题空着、时长 0 秒」的元信息，而把它填进播放条
// 就是一行空白——真因（这一首已经不在了）一个字都不说。
func TestSongInfoRejectsAnEmptyDataSegment(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathSongInfo, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, musicEnvelope(t, 0, "success", nil))
	})
	_, err := f.client(t, Options{}).SongInfo(context.Background(), testSid)
	if !errors.Is(err, ErrNoStream) {
		t.Fatalf("err = %v，want ErrNoStream", err)
	}
}

// TestSongInfoRejectsABadID 参数给错的时候要当场说，别发请求。
func TestSongInfoRejectsABadID(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	for _, sid := range []string{"", "0", "-3", "abc"} {
		if _, err := c.SongInfo(context.Background(), sid); err == nil {
			t.Errorf("sid=%q 也放过去了", sid)
		}
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发出去 %d 个请求，want 0（参数不对不该打接口）", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 取流
// ─────────────────────────────────────────────────────────────────────────────

// songURLClient 建一台会回音乐库取流的假 B 站。
func songURLClient(t *testing.T, data map[string]any) (*fakeBili, *Client) {
	t.Helper()
	f := newFakeBili(t)
	f.on(pathSongURL, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, musicEnvelope(t, 0, "success", data))
	})
	return f, f.client(t, Options{})
}

// songQuality 造 `qualities[]` 里的一项。
//
// require 1 = 要会员。**只有无损那一档是 1**（文档：「FLAC 需要会员资格」），
// 这正是自动档要 320K 而不是冒然要无损的原因。
func musicQualityItem(typ, require int) map[string]any {
	return map[string]any{
		"type": typ, "size": 12345678, "bps": "", "tag": "",
		"require": require, "requiredesc": "",
	}
}

// TestMusicStreamURLTakesTheDefaultTier 盯默认（自动）档要的是哪一档。
//
// 自动档要 **320K 而不是 FLAC**：B 站对没权限的那一档不报错，回的是试听片段，
// 而这一层拿不到「这个号有没有会员」——自动档去赌那一档的话，每一个没会员的
// 号每放一首都要多问一次。
func TestMusicStreamURLTakesTheDefaultTier(t *testing.T) {
	exp := time.Now().Add(3 * time.Hour)
	f, c := songURLClient(t, songURLDataFor(2, songURL("https://cdn.example", "a.m4a", exp)))

	ref, err := c.AudioStreamURL(context.Background(), testSid, AudioAuto)
	if err != nil {
		t.Fatal(err)
	}
	if got := lastQuery(t, f.requests()).Get("quality"); got != "2" {
		t.Errorf("quality = %q，want 2（自动档要 320K）", got)
	}
	if ref.Label != "320K" {
		t.Errorf("徽章 = %q，want 320K", ref.Label)
	}
	if ref.Sid != testSid {
		t.Errorf("Sid = %q，want %s（音乐库那条路没有 bvid，身份只在 sid 上）", ref.Sid, testSid)
	}
	if ref.Quality != QualityUnknown {
		t.Errorf("Quality = %v，want 0（音乐库的档位在 MusicTier 上，别混进 dash 那套）", ref.Quality)
	}
	if ref.Container != "m4a" {
		t.Errorf("容器 = %q，want m4a", ref.Container)
	}
}

// TestMusicStreamURLSendsTheMandatoryParameters 盯那几个必填参数。
//
// `privilege` 文档写着「必须为 2」，少了它 B 站会当成另一个接口。
func TestMusicStreamURLSendsTheMandatoryParameters(t *testing.T) {
	f, c := songURLClient(t, songURLDataFor(2, songURL("https://cdn.example", "a.m4a", time.Now())))
	if _, err := c.AudioStreamURL(context.Background(), testSid, AudioHigh); err != nil {
		t.Fatal(err)
	}
	q := lastQuery(t, f.requests())
	for k, want := range map[string]string{
		"songid":    testSid,
		"privilege": "2",
		"platform":  "web",
		"mid":       "1",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q，want %q", k, got, want)
		}
	}
	if q.Get("mid") == "0" {
		t.Error("mid 发了 0：文档说可为任意值，但 0 会撞上别的校验")
	}
}

// TestMusicStreamURLPutsTheBackupAfterTheMainAddress 盯 CDN 顺序。
//
// 主地址 403 时先试备用的，所以主的一定在前面（`StreamRef.URLs` 的约定）。
func TestMusicStreamURLPutsTheBackupAfterTheMainAddress(t *testing.T) {
	_, c := songURLClient(t, songURLDataFor(2,
		songURL("https://a.example", "main.m4a", time.Now()),
		songURL("https://b.example", "backup.m4a", time.Now())))
	ref, err := c.AudioStreamURL(context.Background(), testSid, AudioAuto)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.URLs) != 2 || !strings.Contains(ref.URLs[0], "main.m4a") {
		t.Errorf("地址顺序 = %v，want 主地址在前", ref.URLs)
	}
}

// TestLosslessIsAskedForAndRefusedLoudly 点名无损就点名要 FLAC。
func TestLosslessIsAskedForAndRefusedLoudly(t *testing.T) {
	f, c := songURLClient(t, songURLDataFor(3, songURL("https://cdn.example", "a.flac", time.Now())))
	ref, err := c.AudioStreamURL(context.Background(), testSid, AudioLossless)
	if err != nil {
		t.Fatal(err)
	}
	if q := lastQuery(t, f.requests()); q.Get("quality") != "3" {
		t.Errorf("quality = %q，want 3", q.Get("quality"))
	}
	if ref.MusicTier != MusicTierFLAC || ref.Label != "无损 FLAC" {
		t.Errorf("档位 = %v 徽章 = %q，want FLAC / 无损 FLAC", ref.MusicTier, ref.Label)
	}
	if ref.Container != "flac" {
		t.Errorf("容器 = %q，want flac", ref.Container)
	}
}

// ── 试听片段：这一路最大的一个坑 ────────────────────────────────────────────

// TestAMusicPreviewIsNeverHandedBackAsAStream 是这一票最要紧的一条。
//
// B 站对「没权限的那一档」不报错，回的是一条 `type = -1` 的**试听片段**：
// 地址能下、字节能播、播放器能出声，然后在用户以为歌还没放完的地方停住。
// 放它过去，「放不了」就换成了一个更难解释的样子——界面上一个错都没有。
func TestAMusicPreviewIsNeverHandedBackAsAStream(t *testing.T) {
	// 付费曲：qualities 里每一档都要会员，所以没有能退的档。
	f := newFakeBili(t)
	f.on(pathSongURL, func(w http.ResponseWriter, r *http.Request) {
		d := songURLDataFor(-1, songURL("https://cdn.example", "preview.m4a", time.Now()))
		d["size"], d["qualities"] = 0, paidSongQualities()
		writeJSON(w, musicEnvelope(t, 0, "success", d))
	})

	ref, err := f.client(t, Options{}).AudioStreamURL(context.Background(), testSid, AudioAuto)
	if err == nil {
		t.Fatalf("把试听片段当成流发出去了：%+v", ref)
	}
	if !errors.Is(err, ErrPreviewOnly) {
		t.Errorf("err = %v，want ErrPreviewOnly", err)
	}
	if got := Friendly(err); !strings.Contains(got, "会员") {
		t.Errorf("给用户的话 = %q，要说清差在哪（会员）", got)
	}
}

// paidSongQualities 是**会员曲**的 qualities：320K 和无损都要会员，
// 192K / 128K 不要。
//
// 退档退到哪一档是从这张表里算出来的，所以这张表就是这两条用例里「退到哪」的
// 唯一依据——别拿普通歌的那张表来（那样退档会退到 320K，而 320K 正是被拒的那一档）。
func paidSongQualities() []any {
	return []any{musicQualityItem(3, 1), musicQualityItem(2, 1),
		musicQualityItem(1, 0), musicQualityItem(0, 0)}
}

// TestAPaidSongFallsBackToAFreeTier 会员曲但有不用会员的档时要退。
//
// 退到**不高于用户要的那一档**里最好的那一档（`pickAudio` 末尾那条规矩），
// 退不动才报「要会员」。
func TestAPaidSongFallsBackToAFreeTier(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathSongURL, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("quality") == "2" {
			// 要 320K（会员曲），拿回来的是试听片段。
			d := songURLDataFor(-1, songURL("https://cdn.example", "preview.m4a", time.Now()))
			d["size"], d["qualities"] = 0, paidSongQualities()
			writeJSON(w, musicEnvelope(t, 0, "success", d))
			return
		}
		if q.Get("quality") != "1" {
			t.Errorf("退档退到了 %q，want 1（qualities 里不高于 320K 又不用会员的最好那一档）",
				q.Get("quality"))
		}
		d := songURLDataFor(1, songURL("https://cdn.example", "full.m4a", time.Now()))
		d["qualities"] = paidSongQualities()
		writeJSON(w, musicEnvelope(t, 0, "success", d))
	})

	ref, err := f.client(t, Options{}).AudioStreamURL(context.Background(), testSid, AudioHigh)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Label != "192K" {
		t.Errorf("徽章 = %q，want 192K（退到不用会员的那一档，不是 320K）", ref.Label)
	}
	if n := len(f.requests()); n != 2 {
		t.Errorf("发了 %d 次请求，want 2（一次要、一次退档）", n)
	}
}

// TestAskingForLosslessNeverSilentlyDowngrades 点名无损时**不许**退档。
//
// 用户为无损付了钱，静默给他一段 192K 比报错糟得多。而且只发**一次**请求：
// 再问一次就是拿「用户点了无损」当由头多要一次带签名的往返。
func TestAskingForLosslessNeverSilentlyDowngrades(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathSongURL, func(w http.ResponseWriter, r *http.Request) {
		d := songURLDataFor(-1, songURL("https://cdn.example", "preview.m4a", time.Now()))
		d["size"] = 0
		writeJSON(w, musicEnvelope(t, 0, "success", d))
	})

	_, err := f.client(t, Options{}).AudioStreamURL(context.Background(), testSid, AudioLossless)
	if !errors.Is(err, ErrNoStream) {
		t.Fatalf("err = %v，want ErrNoStream（没有无损就报错，不要偷偷降级）", err)
	}
	if !strings.Contains(err.Error(), "无损") {
		t.Errorf("err = %v，那句话里要出现「无损」，用户才知道自己点的什么", err)
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("发了 %d 次请求，want 1（点名的档没有就是没有，不退档）", n)
	}
}

// TestAFailedFallbackIsNotBlamedOnMembership 退档那一趟网络错了要说它。
//
// 拿「要开会员」去盖住一次网络错，会让用户去办一件用不着的会员。
func TestAFailedFallbackIsNotBlamedOnMembership(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathSongURL, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("quality") == "2" {
			d := songURLDataFor(-1, songURL("https://cdn.example", "preview.m4a", time.Now()))
			d["qualities"] = paidSongQualities()
			writeJSON(w, musicEnvelope(t, 0, "success", d))
			return
		}
		http.Error(w, "挂了", http.StatusBadGateway)
	})
	_, err := f.client(t, Options{}).AudioStreamURL(context.Background(), testSid, AudioHigh)
	if errors.Is(err, ErrPreviewOnly) {
		t.Fatalf("退档的网络错被说成了「要会员」：%v", err)
	}
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("err = %v，want ErrNetwork", err)
	}
}

// TestMusicStreamURLExpiresFromTheTimeout 盯有效期。
//
// 音乐库这一路给的是 `timeout`（有效秒数），不是地址上的 deadline 查询参数。
// 两个都认，优先 deadline（更准）；都读不到就当**立刻失效**——
// 误判成「永不过期」会让换流一次都不跑，症状是歌播到一半静默停住。
func TestMusicStreamURLExpiresFromTheTimeout(t *testing.T) {
	t.Run("地址上带 deadline 就用它", func(t *testing.T) {
		deadline := time.Now().Add(90 * time.Minute)
		_, c := songURLClient(t, songURLDataFor(2, songURL("https://cdn.example", "a.m4a", deadline)))
		ref, err := c.AudioStreamURL(context.Background(), testSid, AudioAuto)
		if err != nil {
			t.Fatal(err)
		}
		if !ref.Usable(time.Now()) {
			t.Errorf("有效期 = %v，一小时五十分钟内的地址被判成过期了", ref.ExpiresAt)
		}
	})
	t.Run("只有 timeout 就加上去", func(t *testing.T) {
		_, c := songURLClient(t, songURLDataFor(2, "https://cdn.example/a.m4a?upsig=x"))
		ref, err := c.AudioStreamURL(context.Background(), testSid, AudioAuto)
		if err != nil {
			t.Fatal(err)
		}
		if !ref.Usable(time.Now()) {
			t.Errorf("有效期 = %v，timeout=10800 却判成过期", ref.ExpiresAt)
		}
	})
	t.Run("两个都没有就是立刻失效", func(t *testing.T) {
		d := songURLDataFor(2, "https://cdn.example/a.m4a?upsig=x")
		d["timeout"] = 0
		_, c := songURLClient(t, d)
		ref, err := c.AudioStreamURL(context.Background(), testSid, AudioAuto)
		if err != nil {
			t.Fatal(err)
		}
		if ref.Usable(time.Now()) {
			t.Error("既没有 deadline 也没有 timeout，却判成还能用：换流会一次都不跑")
		}
	})
}

// TestMusicStreamURLWithNoAddressIsNotAStream 空 cdns。
//
// B 站在稿件被删时会回 code 0 + 空 cdns（同一形态见 `quality.go` 的 `playable`）。
func TestMusicStreamURLWithNoAddressIsNotAStream(t *testing.T) {
	_, c := songURLClient(t, songURLDataFor(2))
	_, err := c.AudioStreamURL(context.Background(), testSid, AudioAuto)
	if !errors.Is(err, ErrNoStream) {
		t.Fatalf("err = %v，want ErrNoStream", err)
	}
}

// TestMusicStreamURLRejectsABadID 参数给错的时候要当场说，别发请求。
func TestMusicStreamURLRejectsABadID(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	for _, sid := range []string{"", "0", "-1", "  ", "abc"} {
		if _, err := c.AudioStreamURL(context.Background(), sid, AudioAuto); err == nil {
			t.Errorf("sid=%q 也放过去了", sid)
		}
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发出去 %d 个请求，want 0", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 收藏态
// ─────────────────────────────────────────────────────────────────────────────

// TestCollResourceCheckReadsTheNumber 收藏态的 data 是一个 0/1 的数。
func TestCollResourceCheckReadsTheNumber(t *testing.T) {
	for _, c := range []struct {
		data string
		want bool
	}{
		{"1", true}, {"0", false}, {"2", true},
	} {
		f := newFakeBili(t)
		f.on(pathCollResourceCheck, func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("rid") != testSid || q.Get("type") != "12" {
				t.Errorf("发了 rid=%q type=%q，want rid=%s type=12", q.Get("rid"), q.Get("type"), testSid)
			}
			writeJSON(w, musicEnvelope(t, 0, "success", json.RawMessage(c.data)))
		})
		got, err := f.client(t, Options{}).CollResourceCheck(context.Background(), 1293494, MediaAudio)
		if err != nil {
			t.Fatalf("data=%s: %v", c.data, err)
		}
		if got != c.want {
			t.Errorf("data=%s → %v，want %v", c.data, got, c.want)
		}
	}
}

// TestCollResourceCheckAlsoReadsABoolean 布尔形状也认。
//
// 上游那一句注释写的是「data **通常**为 0/1」——「通常」两个字意味着另一种
// 形状是可能的，而把它当读不懂处理的话，一条收藏了的歌会静悄悄地显示成没收藏。
func TestCollResourceCheckAlsoReadsABoolean(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathCollResourceCheck, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, musicEnvelope(t, 0, "success", true))
	})
	got, err := f.client(t, Options{}).CollResourceCheck(context.Background(), 1293494, MediaAudio)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("data=true 却读成没收藏")
	}
}

// TestCollResourceCheckSaysWhyWhenTheShapeIsNew 认不出来的形状要说，不猜。
func TestCollResourceCheckSaysWhyWhenTheShapeIsNew(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathCollResourceCheck, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, musicEnvelope(t, 0, "success", map[string]any{"fav": 1}))
	})
	_, err := f.client(t, Options{}).CollResourceCheck(context.Background(), 1293494, MediaAudio)
	if err == nil {
		t.Fatal("data 是个对象，却当 0/1 答了")
	}
	if !strings.Contains(err.Error(), "认不出来") {
		t.Errorf("err = %v，那句话要说清是形状认不出来", err)
	}
}

// TestCollResourceCheckRejectsABadID 参数给错的时候要当场说，别发请求。
func TestCollResourceCheckRejectsABadID(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	if _, err := c.CollResourceCheck(context.Background(), 0, MediaAudio); err == nil {
		t.Error("rid=0 也放过去了")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发出去 %d 个请求，want 0", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 身份比对
// ─────────────────────────────────────────────────────────────────────────────

// TestSameTrackTellsMusicLibraryEntriesApart 换流那道防线在音乐库上要成立。
//
// 音乐库那一路 Bvid / Cid 都是空的，所以只按 Bvid+Cid 比的话**这一路的任何
// 两条流都会被认成同一首**——而这道闸正是拦「静默换成另一首歌」的。
func TestSameTrackTellsMusicLibraryEntriesApart(t *testing.T) {
	a := &StreamRef{Sid: "1293494"}
	b := &StreamRef{Sid: "1293495"}
	mv := &StreamRef{Bvid: "BV1xx", Cid: "1"}

	if !a.SameTrack(&StreamRef{Sid: "1293494"}) {
		t.Error("同一个 sid 被判成不是同一首")
	}
	if a.SameTrack(b) {
		t.Error("两个不同的 sid 被判成同一首")
	}
	if a.SameTrack(mv) || mv.SameTrack(a) {
		t.Error("音乐库的一条流和一条投稿的流被判成同一首")
	}
	// 老的那条不能被改坏：投稿之间照样只比 bvid + cid。
	x := &StreamRef{Bvid: "BV1xx", Cid: "1"}
	if !mv.SameTrack(x) || mv.SameTrack(&StreamRef{Bvid: "BV1xx", Cid: "2"}) {
		t.Error("投稿那一路的 SameTrack 变了")
	}
}

// TestSameTierComparesBothNumberings 换音质判「同一档」要认两套编号。
//
// 音乐库那一路的档位在 MusicTier 上（0..3），投稿那一路在 Quality 上
// （30280 那种）。只比其中一套的话，音乐库的换音质会永远判成「不是同一档」，
// 于是每一次点音质都把歌从中间截断。
func TestSameTierComparesBothNumberings(t *testing.T) {
	music := &StreamRef{Sid: "1", MusicTier: MusicTier320}
	if !music.SameTier(&StreamRef{Sid: "1", MusicTier: MusicTier320}) {
		t.Error("同一个音乐库档位被判成不是同一档")
	}
	if music.SameTier(&StreamRef{Sid: "1", MusicTier: MusicTierFLAC}) {
		t.Error("320K 和 FLAC 被判成同一档")
	}
	mv := &StreamRef{Bvid: "BV1xx", Cid: "1", Quality: QualityHigh320}
	if !mv.SameTier(&StreamRef{Bvid: "BV1yy", Cid: "2", Quality: QualityHigh320}) {
		t.Error("投稿那一路的 SameTier 变了")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 档位编号
// ─────────────────────────────────────────────────────────────────────────────

// TestMusicTierLabels 徽章文字。
//
// 认不出来的编号给**空串**：徽章只有一个来源，两处各说一半会让同一个意思
// 在界面上飘。
func TestMusicTierLabels(t *testing.T) {
	for tier, want := range map[MusicTier]string{
		MusicTierFLAC: "无损 FLAC",
		MusicTier320:  "320K",
		MusicTier192:  "192K",
		MusicTier128:  "128K",
		MusicTier(9):  "",
		MusicTier(-1): "",
	} {
		if got := tier.Label(); got != want {
			t.Errorf("MusicTier(%d).Label() = %q，want %q", int(tier), got, want)
		}
	}
}

// TestStandardAndLowBothLandOnTheCheapestTier 省流量不许拿到更高的那一档。
//
// 用户为了少流量点的它，拿到 192K 就是多给了流量——那种错在界面上完全看不出来。
func TestStandardAndLowBothLandOnTheCheapestTier(t *testing.T) {
	if got := MusicTierFor(AudioLow); got != MusicTier128 {
		t.Errorf("省流量要的是 %v，want 128K（音乐库最低的一档）", got)
	}
	if got := MusicTierFor(AudioMedium); got != MusicTier128 {
		t.Errorf("标准要的是 %v，want 128K", got)
	}
	if got := MusicTierFor(AudioHigh); got != MusicTier320 {
		t.Errorf("高音质要的是 %v，want 320K", got)
	}
	if got := MusicTierFor(AudioLossless); got != MusicTierFLAC {
		t.Errorf("无损要的是 %v，want FLAC", got)
	}
	// 自动档**不冒然要无损**：拿不到那一档时 B 站回的是试听片段，而这一层
	// 不知道这个号有没有会员。
	if got := MusicTierFor(AudioAuto); got != MusicTier320 {
		t.Errorf("自动档要的是 %v，want 320K", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 杂项
// ─────────────────────────────────────────────────────────────────────────────

// lastQuery 拿最后一个请求的查询串。
//
// 盯参数的地方统一走它：这一组的 bug 全是「发出去的参数不对」，
// 而那在返回里一个字都看不出来。
func lastQuery(t *testing.T, seen []recordedRequest) url.Values {
	t.Helper()
	if len(seen) == 0 {
		t.Fatal("一个请求都没有发出去")
	}
	return seen[len(seen)-1].Query
}
