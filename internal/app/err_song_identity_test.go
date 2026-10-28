package app_test

// 出错时，播放条上要说得清**是哪一首**放不出来（票：左下角出错那三行）。
//
// # 为什么这一族存在
//
// 🔴 **出错那一栏显示的本来是「上一首」**：取流失败的时候，播放器压根没装上
// （`a.player` 还是 nil），而 `a.title` / `a.artist` 是**流装上之后**才写的
// （`online.go` 的 `installOnline`）。于是：
//
//	冷启动就失败		Title = ""（空）	Artist = ""
//	上一首在放，点第二首失败	Title = "第一首"（**还在放的那首**）
//
// 第二种是界面上最难看的一种：那一列上写的是「第一首 / 它的作者」，第三行
// 写的是「打不开：……」——**对不上号**。用户读到的是「这一首正在放，但它打不
// 开」，而实际上打不开的是他刚点的**另一首**。
//
// 这一族盯的是「失败的那一首的信息要送到界面上」。

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestTheFailedSongIsNamedOnThePlayBar 盯**出错时那一栏写的是失败的那一首**。
//
// ⚠️ **场景是「上一首还在放」**：那才是用户会撞上的那个（冷启动失败时那一列
// 本来就是空的，不会有「对不上号」的问题）。
func TestTheFailedSongIsNamedOnThePlayBar(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", time.Hour), int(110836), 110836))
	var calls int
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			f.servePlayURL(w, r.URL.Query())
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	a := newOnlineApp(t, f, t.TempDir())

	// 先放起来一首。
	if err := a.PlayFrom(onlineItem("BV1MbZnYoEk1", "29165486316", "第一首")); err != nil {
		t.Fatalf("放第一首：%v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st := a.Snapshot()
		if st.HasTrack && st.Title == "第一首" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 再点一首，那一首取流失败。
	if err := a.PlayFrom(onlineItem("BV1Second", "29165486317", "第二首")); err != nil {
		t.Fatalf("放第二首：%v", err)
	}
	for time.Now().Before(deadline) {
		if a.Snapshot().Err != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	st := a.Snapshot()
	if st.Err == "" {
		t.Fatal("取流失败了却什么都没说")
	}
	// 🔴 **失败的那一首要单独报出来**：它和「正在放的那一首」是两首。
	if st.FailedTitle != "第二首" {
		t.Errorf("失败的那一首 = %q，want \"第二首\"（出错那一栏要写的是它，"+
			"不是正在放的那一首——两首对不上号的话，用户读到的是"+
			"「这一首正在放，但它打不开」）", st.FailedTitle)
	}
	// 正在放的那一首**照旧**是它（它在响）。
	if st.Title != "第一首" {
		t.Errorf("正在放的那一首 = %q，want \"第一首\"", st.Title)
	}
}

// TestTheFailedSongHasNoNameWhenNothingFailed 盯**没出错时那一栏是空的**。
//
// ⚠️ **这一条是上面那一条的反面**：把「失败的那一首」无条件填成「正在放的
// 那一首」，那界面在**没出错**的时候也会显示一句多余的字。
func TestTheFailedSongHasNoNameWhenNothingFailed(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", time.Hour), int(110836), 110836))
	a := newOnlineApp(t, f, t.TempDir())
	if err := a.PlayFrom(onlineItem("BV1MbZnYoEk1", "29165486316", "第一首")); err != nil {
		t.Fatalf("放第一首：%v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st := a.Snapshot()
		if st.HasTrack && st.Title == "第一首" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := a.Snapshot()
	if st.FailedTitle != "" {
		t.Errorf("没出错却有「失败的那一首」= %q", st.FailedTitle)
	}
}

// TestTheFailedSongKeepsItsAuthor 盯**作者名也一起报出来**。
//
// ⚠️ **作者名是队列项里就有的**（`Item.Artist`）——不用再问一次接口。而它是
// 那一行第二行要显示的东西。
func TestTheFailedSongKeepsItsAuthor(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", time.Hour), int(110836), 110836))
	var calls int
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			f.servePlayURL(w, r.URL.Query())
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	a := newOnlineApp(t, f, t.TempDir())
	// 先放起来一首（它是「正在放的那一首」）。
	if err := a.PlayFrom(onlineItem("BV1MbZnYoEk1", "29165486316", "第一首")); err != nil {
		t.Fatalf("放第一首：%v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st := a.Snapshot()
		if st.HasTrack && st.Title == "第一首" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// 再点一首，那一首取流失败——失败的那一首**带作者名**。
	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1Second", Cid: "29165486317",
		Title: "第二首", Artist: "第二位作者", ArtistMid: 7,
	}); err != nil {
		t.Fatalf("放第二首：%v", err)
	}
	for time.Now().Before(deadline) {
		if a.Snapshot().Err != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := a.Snapshot()
	if st.Err == "" {
		t.Fatal("取流失败了却什么都没说")
	}
	// 作者名是**有**的（队列项里带的）时就要报出来。
	if st.FailedTitle != "第二首" || st.FailedArtist != "第二位作者" {
		t.Errorf("失败的那一首 = %q / %q，want \"第二首\" / \"第二位作者\"",
			st.FailedTitle, st.FailedArtist)
	}
	_ = strings.TrimSpace
}
