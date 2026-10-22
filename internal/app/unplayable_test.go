package app_test

// 「这一首放不了」的两类：**真没有音频流**和**取流失败**。
//
// # 为什么这两类必须分开
//
// 用户分不清它们的时候，做的动作正好相反：
//
//	真没有音频流	重试一万次也没用；该做的是换一首、去 B 站里听
//	取流失败	再试一次就有可能好；该做的是等一下、换个网
//
// 而它们在界面上原本长得一模一样（都是「打不开：……」），于是用户按着
// 「怎么又坏了」的方向一直查，方向从头就是错的。
//
// 这一组盯三件事：
//
//  1. **真没有音频流的那一类**：话说准「B 站说这一条没有音频流」，
//     **而且不给重试**（给了也是按了没用的）。
//  2. **取流失败的那一类**：话说清是网络，**而且给重试**，
//     按下去真的能放出来。
//  3. **点之前就不给播放入口**：判据在这里，界面照它画（见
//     `internal/ui/widgets/list/menu_test.go`）。
//
// ⚠️ **全部走内存里的假 B 站**（`streamFixture`），一条真接口都不打。

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// TestAManifestWithoutAnAudioStreamIsNotOfferedARetry 真没有音频流的那一类。
//
// 这一条对着真机实测过：首页推荐里的「戴上耳机，释放多巴胺」（UP「挽风丶Sama」，
// 音乐区 tid=130、单 P、三十多分钟），B 站的取流接口对它回的是
// **「这个稿件没有音频流」**——它压根不是投稿没有稿件的那种（那一种在同步那一步
// 就被拒了），而是**取流的时候 B 站说的**。
//
// 🔴 **它不许被说成网络问题**：说成网络问题的话，用户会去查网、换网、等它好，
// 而这一条等一万年还是没有音频流。
//
// 🔴 **它不许给「重试」**：给一个按了也没用的重试比不给更糟——用户按两次
// 发现还是那样，就不再信这个应用了。
func TestAManifestWithoutAnAudioStreamIsNotOfferedARetry(t *testing.T) {
	f := newStreamFixture(t)
	// dash 段里没有音频——B 站对这一类稿件回的就是这个样子。
	f.onPlayURL(map[string]any{"duration": 1})
	a := newOnlineApp(t, f, t.TempDir())

	if err := a.PlayFrom(onlineItem("BV1B6orBxEFE", "37875483591", "戴上耳机，释放多巴胺")); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	msg := waitForErr(t, a)

	if !strings.Contains(msg, "音频流") {
		t.Errorf("播放条上那句话是 %q，没有说清是「这一条没有音频流」", msg)
	}
	if strings.Contains(msg, "网络") || strings.Contains(msg, "连不上") {
		t.Errorf("播放条上那句话是 %q，把「真的没有音频流」说成了「没取到」", msg)
	}
	if a.Snapshot().CanRetry {
		t.Error("给了一枚按了也没用的「重试」")
	}
}

// TestANetworkFailureIsOfferedARetryThatActuallyWorks 取流失败的那一类。
//
// 这一条钉的是**两半**：说清是网络（可恢复的那一类），**而且**给的那枚
// 「重试」按下去真的能把这一首放出来。
//
// 🔴 **「给了一枚重试」不够**：重试要放的是**用户刚才按的那一首**，而不是
// 队列里当前的那一首（取流失败时队列常常已经滚回去了）。所以这里断言的是
// 「播放条上的歌名变成了刚才那首」。
func TestANetworkFailureIsOfferedARetryThatActuallyWorks(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	var calls atomic.Int32
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// 第一次：服务端炸了（这就是「没取到」那一类）。
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		f.servePlayURL(w, r.URL.Query())
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", time.Hour), int(bili.QualityHigh320), 110836))
	a := newOnlineApp(t, f, t.TempDir())

	if err := a.PlayFrom(onlineItem("BV1MbZnYoEk1", "29165486316", "这一首")); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	msg := waitForErr(t, a)
	if !strings.Contains(msg, "网络") && !strings.Contains(msg, "连不上") {
		t.Errorf("播放条上那句话是 %q，没有说清是「没取到」", msg)
	}
	if !a.Snapshot().CanRetry {
		t.Fatalf("没有给「重试」，而这句话是 %q（它明明是可以再试一次的）", msg)
	}

	// 按「重试」：这一次服务端好了，这一首该放出来。
	if err := a.Retry(); err != nil {
		t.Fatalf("重试失败：%v", err)
	}
	playing(t, a, "这一首")
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("放出来了，播放条上还挂着那句话：%q", st.Err)
	}
}

// TestTheTwoKindsDoNotSayTheSameThing 两类不许说成同一句。
//
// 这一条是整个票的**主线**：两类混成一句的话，用户就分不清「真没有」和
// 「没取到」——而他做的动作正好相反。
//
// 这里比的是**两个出口的文案**：
//
//	点之前就能判出来的那一类（没挂稿件的推荐条目）
//	按下去之后才知道的那一类（B 站说这个稿件没有音频流）
//
// 两句必须**互不包含**：一句里出现另一句的关键字，就说明它们还是混着的。
func TestTheTwoKindsDoNotSayTheSameThing(t *testing.T) {
	before := app.UnplayableReason(app.Item{Source: "online", Type: "mv", Title: "没挂稿件的那一条"})
	if !strings.Contains(before, "不是投稿") {
		t.Fatalf("点之前那一类的文案是 %q，没有说清「为什么」", before)
	}
	if strings.Contains(before, "音频流") {
		t.Errorf("点之前那一类的文案是 %q，和「B 站说没有音频流」混上了", before)
	}

	f := newStreamFixture(t)
	f.onPlayURL(map[string]any{"duration": 1})
	a := newOnlineApp(t, f, t.TempDir())
	if err := a.PlayFrom(onlineItem("BV1B6orBxEFE", "37875483591", "这一条")); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	after := waitForErr(t, a)

	if strings.Contains(after, "不是投稿") {
		t.Errorf("按下去之后那一类的文案是 %q，和「没挂稿件」混上了", after)
	}
	if before == after {
		t.Errorf("两类说的是同一句：%q", before)
	}
}

// TestEveryUnplayableSentenceAnswersWhyAndWhatToDo 每句都要答两问。
//
// 「为什么」和「还能怎么办」缺一个，用户就只能在界面上干瞪眼——
// 而这一条最容易被写成半句（「不能放」「出错了」）。
func TestEveryUnplayableSentenceAnswersWhyAndWhatToDo(t *testing.T) {
	for _, c := range []struct {
		what string
		it   app.Item
		why  string
		next string
	}{
		{"没挂稿件的推荐条目", app.Item{Source: "online", Type: "mv"}, "不是投稿", "搜同名"},
		{"缺音频 id 的音乐库条目", app.Item{Source: "online", Type: "audio"}, "音乐库", "重新收藏"},
	} {
		msg := app.UnplayableReason(c.it)
		if !strings.Contains(msg, c.why) {
			t.Errorf("%s：%q 里没有「为什么」", c.what, msg)
		}
		if !strings.Contains(msg, c.next) {
			t.Errorf("%s：%q 里没有「还能怎么办」", c.what, msg)
		}
		// 菜单里那一句话（短的）也得两半都有。
		note := app.UnplayableMenuNote(c.it)
		if note == "" {
			t.Errorf("%s：菜单里一句说明都没有", c.what)
			continue
		}
		if !strings.Contains(note, c.why) && !strings.Contains(note, c.next) {
			t.Errorf("%s：菜单里那句话 %q 两半都没有", c.what, note)
		}
	}
}

// TestNoPlayEntryBeforeYouPressIt 「点之前就不给播放入口」。
//
// 这一条在界面层是「菜单里的『播放』那一项是灰的」，而判据在这里：
// `app.CanPlay` 说是放不了的，界面就不给那个入口。
//
// ⚠️ **本地文件一律放得了**：它走开文件那一路，和取流无关——
// 判据写成「没有 bvid 就没有身份」的话，本地音乐那一页的「播放」会整片
// 灰掉（那个 bug 真发生过一次：`widgets/list` 的菜单把本地文件判成放不了）。
func TestNoPlayEntryBeforeYouPressIt(t *testing.T) {
	for _, c := range []struct {
		what    string
		it      app.Item
		canPlay bool
		why     string
	}{
		{"本地文件", app.LocalItem("/tmp/一首歌.m4a"), true, ""},
		{"投稿", app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1"}, true, ""},
		{"音乐库条目", app.Item{Source: "online", Type: "audio", Sid: "1293494"}, true, ""},
		{"没挂稿件的推荐条目", app.Item{Source: "online", Type: "mv"}, false, "不是投稿"},
		{"缺音频 id 的音乐库条目", app.Item{Source: "online", Type: "audio"}, false, "音乐库"},
	} {
		if got := app.CanPlay(c.it); got != c.canPlay {
			t.Errorf("%s：CanPlay = %v，want %v", c.what, got, c.canPlay)
		}
		note := app.UnplayableMenuNote(c.it)
		if c.canPlay {
			if note != "" {
				t.Errorf("%s：放得了却有一句说明：%q", c.what, note)
			}
			continue
		}
		if note == "" {
			t.Errorf("%s：放不了却一句说明都没有", c.what)
			continue
		}
		if !strings.Contains(note, c.why) {
			t.Errorf("%s：说明 %q 里没有「为什么」", c.what, note)
		}
	}
}

// TestAMusicLibraryEntryStillHasNoDownloadPath 下不了的那一类。
//
// 音乐库条目**放得了**（sid 那一路真能出声），但**下不了**（下载是按稿件 +
// 分 P 取流的，而它只有一个 auid）。两件事分开说——把它们说成一件事的话，
// 用户会以为「放不了」于是根本不听，而它明明能放。
func TestAMusicLibraryEntryStillHasNoDownloadPath(t *testing.T) {
	it := app.Item{Source: "online", Type: "audio", Sid: "1293494"}
	if !app.CanPlay(it) {
		t.Error("音乐库条目被判成放不了，而它真放得出来（sid 那一路）")
	}
	if app.CanDownload(it) {
		t.Error("音乐库条目被判成下得了，而下载那一路按稿件存文件，它只有 auid")
	}
	reason := app.UndownloadableReason(it)
	if !strings.Contains(reason, "音乐库") || !strings.Contains(reason, "B 站") {
		t.Errorf("下不了的理由是 %q，没有说清「为什么」和「还能怎么办」", reason)
	}
}

// waitForErr 等播放条上出现那句话。
//
// 用状态去等，而不是等固定的时间：这一句什么时候落下来取决于后台那一趟
// 取流什么时候回来（网络往返，200~400ms），等固定毫秒的话慢的机器上会飘。
func waitForErr(t *testing.T, a *app.App) string {
	t.Helper()
	var msg string
	waitFor(t, 30*time.Second, "播放条上出现那句话", func() bool {
		msg = a.Snapshot().Err
		return msg != ""
	})
	return msg
}
