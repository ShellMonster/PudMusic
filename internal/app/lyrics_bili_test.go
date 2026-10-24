package app_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
)

// 这一组测的是「B 站那一份歌词是自己来的」这条线：
// 取流那一趟带回字幕的地址 → 后台下载 → 解析 → 存进缓存 → 装到界面上。
//
// # 为什么它归在这一层测
//
// 这一层是**唯一**知道「现在放的是哪一首」的地方，而歌词缓存的键是按那首歌
// 算的（`lyricKeyOf`）。往下一层（服务层）测就造不出「这一首的键」。

// subtitleFixtureBody 是 B 站给的那份字幕（形状照真的那份造：
// `from` 是秒、带小数；`content` 里可能带 `♪♫`）。
const subtitleFixtureBody = `{"body":[
  {"from":12.345,"to":15.0,"content":"♪ 自动来的第一句 ♫"},
  {"from":1.0,"to":3.0,"content":"   "},
  {"from":3.0,"to":5.0,"content":"第二句"}
]}`

// TestTheBiliSubtitleComesOnItsOwn 是这一票的主线用例。
//
// 完整的一条链：取流（响应里带着字幕的地址）→ 后台下载 → 解析 → 存进缓存 →
// 界面上有词。
//
// ⚠️ **歌词是异步到的**：取流那一趟带回来的只是**地址**，文件要另外下。所以这里
// 等的是「歌词出现了」，不是「取流成功了」。
func TestTheBiliSubtitleComesOnItsOwn(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.subtitleBody = subtitleFixtureBody
	f.subtitleURL = f.URL + "/subtitle.json"
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), 30280, 110836))

	cacheDir := t.TempDir()
	a := newOnlineApp(t, f, t.TempDir())
	a.MountLyrics(lyrics.NewCache(cacheDir))

	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")

	waitFor(t, 15*time.Second, "歌词自己出来了", func() bool {
		return a.Lyrics().HasLyrics
	})

	snap := a.Lyrics()
	if len(snap.Rows) != 2 {
		t.Fatalf("歌词有 %d 行，want 2：%+v", len(snap.Rows), snap.Rows)
	}
	// 时间 = `round(from * 1000)` 毫秒（上游 `get-lyrics.ts`）。
	if snap.Rows[0].At != 3*time.Second {
		t.Errorf("第一句在 %v，want 3s", snap.Rows[0].At)
	}
	// 文字去掉首尾的 `♪♫` 再 trim。
	if snap.Rows[0].Text != "第二句" {
		t.Errorf("第一句是 %q，want 「第二句」", snap.Rows[0].Text)
	}
	if snap.Rows[1].At != 12345*time.Millisecond {
		t.Errorf("第二句在 %v，want 12.345s", snap.Rows[1].At)
	}
	if snap.Rows[1].Text != "自动来的第一句" {
		t.Errorf("第二句是 %q，want 「自动来的第一句」", snap.Rows[1].Text)
	}
	// 来路要记下来：界面上那句话靠它说清「这份词是自动来的还是搜来的」。
	if snap.Origin != lyrics.OriginBili {
		t.Errorf("来路 = %q，want %q", snap.Origin, lyrics.OriginBili)
	}

	// 存进缓存了（键是 bvid+cid，和别的歌词缓存一致）。
	//
	// ⚠️ **要等它落盘**：词是**先**装到界面上的，落盘在**之后**（`installBiliLyrics`
	// 先放锁再写盘）。所以「界面上有词」比「盘上有」早那么一瞬——直接读盘会读到
	// 一个空文件，而那不是「没存」。
	key := "bili-BV1xx-123"
	cache := lyrics.NewCache(cacheDir)
	var entry lyrics.Entry
	waitFor(t, 15*time.Second, "歌词落进缓存了", func() bool {
		e, ok := cache.Load(key)
		if ok && strings.Contains(e.Lyrics, "自动来的第一句") {
			entry = e
			return true
		}
		return false
	})
	if entry.Origin != lyrics.OriginBili {
		t.Errorf("缓存里的来路 = %q，want %q", entry.Origin, lyrics.OriginBili)
	}
}

// TestTheBiliSubtitleSurvivesARestart 盯「关掉应用再开还在」。
//
// 第二次启动**不再去下载**那一份：缓存里已经有了，而 B 站那份「自动」的也不该
// 把用户自己找来的那一份顶掉。
func TestTheBiliSubtitleSurvivesARestart(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.subtitleBody = subtitleFixtureBody
	f.subtitleURL = f.URL + "/subtitle.json"
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), 30280, 110836))

	cacheDir := t.TempDir()
	first := newOnlineApp(t, f, t.TempDir())
	first.MountLyrics(lyrics.NewCache(cacheDir))
	if err := first.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, first, "在线的一首")
	waitFor(t, 15*time.Second, "歌词自己出来了", func() bool {
		return first.Lyrics().HasLyrics
	})
	first.Close()

	// 第二次启动：同一个缓存目录。
	second := newOnlineApp(t, f, t.TempDir())
	second.MountLyrics(lyrics.NewCache(cacheDir))
	if err := second.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, second, "在线的一首")

	waitFor(t, 15*time.Second, "歌词从缓存里出来了", func() bool {
		return second.Lyrics().HasLyrics
	})
	snap := second.Lyrics()
	if len(snap.Rows) != 2 || snap.Rows[0].Text != "第二句" {
		t.Errorf("重启之后的歌词是 %+v", snap.Rows)
	}
	if snap.Origin != lyrics.OriginBili {
		t.Errorf("重启之后的来路 = %q，want %q", snap.Origin, lyrics.OriginBili)
	}
}

// TestAMusicLibraryEntryAlsoGetsItsLyrics 盯「音乐库那一路」也有词。
//
// ⚠️ 音乐库的取流走的是**另一个接口**（`/audio/music-service-c/url`），那个返回里
// **没有**字幕——所以要问它背后那个投稿一次（同一个取流接口，只看 `subtitle`
// 那一段）。上游 Biu 也是这么做的：`getLyricsByBili` 打的是**投稿**那个取流
// 接口，而音乐库条目的 bvid/cid 来自 `song/info`。
//
// ⚠️ **那一趟是额外的往返**，所以判据里要留着它：它是「这一首的词在那个投稿的
// 字幕里」这一条路唯一要花的东西。
func TestAMusicLibraryEntryAlsoGetsItsLyrics(t *testing.T) {
	f := newMusicFixture(t)
	f.subtitleBody = subtitleFixtureBody
	f.subtitleURL = f.URL + "/subtitle.json"
	// 元信息给出一个关联的投稿。
	f.on("/audio/music-service-c/web/song/info", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "msg": "success", "ttl": 1,
			"data": map[string]any{
				"id": 1293494, "uid": 900, "uname": "某某音乐台",
				"author": "夜灯", "title": "月光", "cover": "",
				"duration": 214, "bvid": "BVsong", "cid": 77, "collectIds": []int{},
			}})
	})

	cacheDir := t.TempDir()
	a := newOnlineApp(t, f, t.TempDir())
	a.MountLyrics(lyrics.NewCache(cacheDir))
	if err := a.Enqueue(musicItem("1293494", "音乐库的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "音乐库的一首")

	waitFor(t, 15*time.Second, "歌词自己出来了", func() bool {
		return a.Lyrics().HasLyrics
	})
	if got := a.Lyrics().Origin; got != lyrics.OriginBili {
		t.Errorf("来路 = %q，want %q", got, lyrics.OriginBili)
	}

	// 缓存键是**音乐库那一条**的（sid），不是那个投稿的。
	cache := lyrics.NewCache(cacheDir)
	waitFor(t, 15*time.Second, "歌词落进缓存了", func() bool {
		e, ok := cache.Load("bili-audio-1293494")
		return ok && strings.Contains(e.Lyrics, "自动来的第一句")
	})
}

// TestTheBiliSubtitleFallsBackToTheSecondAddress 盯「第一个地址 403 时改试第二个」。
//
// 🔴 **这一条是这一整条自动歌词的生死线**：B 站给同一份字幕两个地址，而**不带
// 鉴权参数的那个可能要登录态**——拿它去请求会 403。而登录态在字幕那条路上根本
// 到不了（cookie 的域是 `.bilibili.com`，字幕文件在 `.hdslb.com`；上游那个
// Electron 客户端也一样，浏览器的 cookie 同样按域匹配）。
//
// 所以**唯一能授权这一趟的就是地址自己带的那段 `auth_key`**——也就是第二个地址。
// 只试第一个的话，「取到了地址但下载 403」就变成「这一整条自动歌词静悄悄地不
// 出现」（界面上照旧是「暂无歌词」，日志里只有一行）。
//
// ⚠️ **夹具必须真的会 403**：仓库里别的夹具永远回 200，于是这一档一次都没被
// 跑到过。
func TestTheBiliSubtitleFallsBackToTheSecondAddress(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.subtitleBody = subtitleFixtureBody
	// 第一个地址：**403**（模拟「它要登录态」）。第二个：给的是正文。
	f.subtitleURL = f.URL + "/subtitle-need-login.json"
	f.subtitleURLV2 = f.URL + "/subtitle.json"
	f.on("/subtitle-need-login.json", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "没有登录态", http.StatusForbidden)
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), 30280, 110836))

	a := newOnlineApp(t, f, t.TempDir())
	a.MountLyrics(lyrics.NewCache(t.TempDir()))
	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")

	waitFor(t, 15*time.Second, "歌词从第二个地址来了", func() bool {
		return a.Lyrics().HasLyrics
	})
	snap := a.Lyrics()
	if len(snap.Rows) != 2 || snap.Rows[0].Text != "第二句" {
		t.Errorf("歌词是 %+v", snap.Rows)
	}
	if snap.Origin != lyrics.OriginBili {
		t.Errorf("来路 = %q，want %q", snap.Origin, lyrics.OriginBili)
	}
}

// TestNoSubtitleMeansTheSearchStaysAvailable 盯「B 站没给字幕」那一支。
//
// ⚠️ **那不是错误**：很多歌压根没有字幕，而用户还有另一条路——「找歌词」那个
// 弹窗（两个搜索源）。歌词区照旧是「暂无歌词」加那个入口。
func TestNoSubtitleMeansTheSearchStaysAvailable(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	// ⚠️ **不设置 subtitleURL**：那就是「B 站没给字幕」这一支。
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), 30280, 110836))

	a := newOnlineApp(t, f, t.TempDir())
	a.MountLyrics(lyrics.NewCache(t.TempDir()))
	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")

	// 等一小会儿，确认「它没有自己冒出来」。
	time.Sleep(300 * time.Millisecond)
	if a.Lyrics().HasLyrics {
		t.Errorf("B 站没给字幕，却有词了：%+v", a.Lyrics().Rows)
	}
	// 界面上那一句话还在（空的、什么都没有）。
	if a.Lyrics().Origin != "" {
		t.Errorf("没有歌词时的来路 = %q，want 空", a.Lyrics().Origin)
	}
}

// TestTheSubtitleIsFetchedInTheBackground 盯「下载与写盘都不在界面上」。
//
// ⚠️ **取流那一趟带回来的只是地址**，文件要另外下——而那一趟是**网络往返**。
// 它要是发生在界面线程上，表现是「点了歌，窗口冻一下」。
//
// 这一条让 fixture **把那份字幕扣住**，然后看应用**照常 answering**：歌词快照
// 拿得到、播放照旧。等放行之后歌词才到。
func TestTheSubtitleIsFetchedInTheBackground(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.subtitleBody = subtitleFixtureBody
	f.subtitleURL = f.URL + "/subtitle.json"
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), 30280, 110836))

	gate := make(chan struct{})
	f.on("/subtitle.json", func(w http.ResponseWriter, r *http.Request) {
		<-gate
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.subtitleBody))
	})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	a := newOnlineApp(t, f, t.TempDir())
	a.MountLyrics(lyrics.NewCache(t.TempDir()))
	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")

	// 那份字幕还扣着：歌照放，界面也照常 answering。
	waitFor(t, 15*time.Second, "进度条往前走", func() bool {
		return a.Snapshot().Position > 0
	})
	if a.Lyrics().HasLyrics {
		t.Fatal("那份字幕还扣着，歌词却已经出来了")
	}
	// 界面线程问的那一句（每一帧都问）要立刻答。
	_ = a.Lyrics()

	close(gate)
	waitFor(t, 15*time.Second, "歌词自己出来了", func() bool {
		return a.Lyrics().HasLyrics
	})
}
