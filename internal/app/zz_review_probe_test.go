package app_test

// 评审用的探针（临时文件，评审完删掉）。
//
// 盯三件这一票自己那组用例没有直接量的事：
//  1. 提前量真的挂在「还剩多久」上（不是一开播就打出去）。
//  2. 音乐库那一路（Type == "audio"）也预取得到。
//  3. 队列改了之后，**陈旧的那份地址不会被播出去**。

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// ProbeLeadTime：提前量必须挂在「还剩多久」上。
func TestProbeLeadTime(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(25.0)
	playURLBySong(f, 2*time.Hour)

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")

	// 开了播之后 4 秒：这一首还剩 21 秒 > 15 秒 → **不该**已经备好了。
	time.Sleep(4 * time.Second)
	if n := f.playCalls.Load(); n != 1 {
		t.Fatalf("一开播就备了（取流接口被打了 %d 次，want 1）——提前量没生效", n)
	}
	st := a.Snapshot()
	t.Logf("4 秒时：position=%v duration=%v 剩=%v", st.Position, st.Duration, st.Duration-st.Position)

	// 盯着取流接口，看它第一次被第二次打是在什么位置。
	var at atomic.Value
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if f.playCalls.Load() >= 2 {
			s := a.Snapshot()
			at.Store(s.Duration - s.Position)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if at.Load() == nil {
		t.Fatalf("20 秒内压根没有备地址（取流接口被打了 %d 次）", f.playCalls.Load())
	}
	remaining := at.Load().(time.Duration)
	t.Logf("备地址那一刻：还剩 %v", remaining)
	if remaining > 16*time.Second {
		t.Errorf("备得太早了：那一刻还剩 %v（提前量是 15 秒）", remaining)
	}
	if remaining < 5*time.Second {
		t.Errorf("备得太晚了：那一刻还剩 %v（提前量是 15 秒）", remaining)
	}
}

// ProbeMusicLibraryPrefetch：音乐库那一路（Type == "audio"）也预取得到。
func TestProbeMusicLibraryPrefetch(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)
	f.musicTier = 2

	// 取流接口回**要的那一首**的 sid（夹具默认永远回 1293494，那样两首会认成同一首）。
	f.on("/audio/music-service-c/url", func(w http.ResponseWriter, r *http.Request) {
		f.songCalls.Add(1)
		sid := r.URL.Query().Get("songid")
		writeStreamJSON(w, map[string]any{"code": 0, "msg": "success", "ttl": 1,
			"data": map[string]any{
				"sid":     mustAtoi(t, sid),
				"type":    2,
				"timeout": 10800,
				"cdns":    []string{cdnURL(f.URL, "song-"+sid, 2*time.Hour)},
				"qualities": []any{
					map[string]any{"type": 3, "require": 1},
					map[string]any{"type": 2, "require": 0},
					map[string]any{"type": 1, "require": 0},
					map[string]any{"type": 0, "require": 0},
				},
			}})
	})
	f.on("/audio/music-service-c/web/song/info", func(w http.ResponseWriter, r *http.Request) {
		f.songInfoCalls.Add(1)
		sid := r.URL.Query().Get("sid")
		writeStreamJSON(w, map[string]any{"code": 0, "msg": "success", "ttl": 1,
			"data": map[string]any{
				"id": mustAtoi(t, sid), "uid": 900, "uname": "某某音乐台",
				"author": "夜灯", "title": "歌" + sid, "cover": "", "duration": 1,
				"bvid": "", "cid": 0, "collectIds": []int{},
			}})
	})

	a := newOnlineApp(t, f, t.TempDir())
	song := func(sid, title string) app.Item {
		return app.Item{Source: "online", Type: "audio", Sid: sid, Title: title}
	}
	if err := a.Enqueue(song("111", "第一首"), song("222", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")

	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.songCalls.Load() >= 2
	})
	if st := a.Snapshot(); st.Title != "第一首" {
		t.Fatalf("备地址的时候当前这一首已经换了：%q", st.Title)
	}
	// 真播第二首时不再取流。
	playing(t, a, "第二首")
	if n := f.songCalls.Load(); n != 2 {
		t.Errorf("音乐库那一路真播时又取了一次流（取流接口一共被打了 %d 次，want 2）", n)
	}
}

// ProbeStaleAddressIsNeverPlayed：队列改了之后，陈旧的那份地址**不会被播出去**。
func TestProbeStaleAddressIsNeverPlayed(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(3.0)
	playURLBySong(f, 2*time.Hour)
	cdnBV2 := watchCDN(f, "/cdn/BV2-2")
	cdnBV3 := watchCDN(f, "/cdn/BV3-3")

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")
	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.playCalls.Load() >= 2
	})

	// 队列变了：下一首从「第二首」变成「第三首」。
	if err := a.EnqueueNext(onlineItem("BV3", "3", "第三首")); err != nil {
		t.Fatal(err)
	}

	// 这一首放完 → 自动切歌 → 放的一定是**第三首**。
	playing(t, a, "第三首")
	if n := cdnBV2.Load(); n != 0 {
		t.Errorf("陈旧的那份地址被播出去了（第二首的 CDN 被打了 %d 次，want 0）", n)
	}
	if n := cdnBV3.Load(); n == 0 {
		t.Errorf("第三首压根没被播（CDN 一次都没被碰）")
	}
}

func mustAtoi(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("解析 %q 失败：%v", s, err)
	}
	return n
}

var _ = bili.QualityHigh192
