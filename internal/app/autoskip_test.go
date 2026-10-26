package app_test

// 起播失败自动跳过（收藏夹里夹着失效稿子那一类）。
//
// # 三条各钉一件事
//
//  1. **夹在中间的失效稿子会被跳过去**：队列 [死的, 活的]，结局是活的这首在放，
//     播放条上不挂红字（下一首起播把它清了），读屏软件听得到「为什么换了歌」。
//  2. **整个队列全死的话只绕一圈就停**：循环 / 随机档的 `Advance` 永远给得出
//     「下一首」，不数着的话全失效的收藏夹会无限跳下去。判据是取流次数刚好
//     = 队列长度（每首恰好试一次），并且 `stopped` 立起来、红字留在屏上。
//  3. **可重试的错不跳**：网络那一类每一首都会撞上，跳过去就是把整张队列白烧
//     一遍请求——红字和「重试」留在用户眼皮底下才是对的。
//
// ⚠️ **全部走内存里的假 B 站**（`streamFixture`），一条真接口都不打。
import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// announceSink 收 app 念出去的那些话，给断言用。
type announceSink struct {
	mu    sync.Mutex
	texts []string
}

func (s *announceSink) add(text string) {
	s.mu.Lock()
	s.texts = append(s.texts, text)
	s.mu.Unlock()
}

func (s *announceSink) has(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.texts {
		if strings.Contains(t, sub) {
			return true
		}
	}
	return false
}

// noAudioDash 是「这个稿件没有音频流」那一种死：dash 里没有音频轨，
// `bili.Recoverable` 判它**不可重试**，正是自动跳过的那一类。
func noAudioDash() map[string]any {
	return map[string]any{"duration": 1}
}

func TestADeadSongInTheQueueIsSkippedAutomatically(t *testing.T) {
	f := newStreamFixture(t)
	// playurl 的回答按**问的顺序**消费：第一首回「没有音频流」（失效那一类），
	// 第二首回真的可播的 dash。
	f.onPlayURL(noAudioDash())
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", time.Hour), int(bili.QualityHigh192), 110836))
	f.media = readFlac(t)

	sink := &announceSink{}
	a := newOnlineApp(t, f, t.TempDir())
	remove := a.AddAnnounce(sink.add)
	defer remove()

	dead := onlineItem("BV1dead", "11", "失效的那首")
	live := onlineItem("BV1live", "22", "活着的那首")
	if err := a.PlayFrom(dead, live); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	playing(t, a, "活着的那首")

	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("跳到活的那首之后播放条上还挂着红字：%q", st.Err)
	}
	if !sink.has("失效的那首") || !sink.has("自动换下一首") {
		t.Errorf("读屏软件没有听到「为什么换了歌」，播报有 %v", sink.texts)
	}
}

func TestTheWholeQueueBeingDeadStopsAfterOneRound(t *testing.T) {
	f := newStreamFixture(t)
	var calls sync.Mutex
	count := 0
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		calls.Lock()
		count++
		calls.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":{"dash":{}}}`))
	})

	sink := &announceSink{}
	a := newOnlineApp(t, f, t.TempDir())
	remove := a.AddAnnounce(sink.add)
	defer remove()

	if err := a.PlayFrom(
		onlineItem("BV1d1", "11", "死一"),
		onlineItem("BV1d2", "22", "死二"),
	); err != nil {
		t.Fatalf("放不进去：%v", err)
	}

	// 两首各试一次就该停：第三趟取流不会发生（那是在白烧请求）。
	waitFor(t, 30*time.Second, "整圈试完之后停下来", func() bool {
		return a.Snapshot().Stopped
	})
	calls.Lock()
	got := count
	calls.Unlock()
	if got != 2 {
		t.Errorf("playurl 被问了 %d 次，want 2（每首恰好一次，不绕第二圈）", got)
	}
	if st := a.Snapshot(); st.Err == "" {
		t.Error("停下来了播放条上却没有红字——用户不知道为什么停")
	}
	if !sink.has("连着 2 首") {
		t.Errorf("读屏软件没有听到「为什么停」，播报有 %v", sink.texts)
	}
}

func TestARetriableFailureDoesNotSkipToTheNextSong(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	var mu sync.Mutex
	first := true
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		isFirst := first
		first = false
		mu.Unlock()
		if isFirst {
			// 服务端炸了：这一类是「网络/没取到」，可重试——不跳。
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		f.servePlayURL(w, r.URL.Query())
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.PlayFrom(
		onlineItem("BV1net", "11", "网络不好那首"),
		onlineItem("BV1ok", "22", "本该被跳到的那首"),
	); err != nil {
		t.Fatalf("放不进去：%v", err)
	}
	waitForErr(t, a)

	// 可重试的错不跳：队列停在原来那首上，重试给了、按下去能放出来。
	if st := a.Snapshot(); !st.CanRetry {
		t.Error("网络类的失败没有给「重试」")
	}
	if err := a.Retry(); err != nil {
		t.Fatalf("重试失败：%v", err)
	}
	playing(t, a, "网络不好那首")
	if st := a.Snapshot(); st.Title == "本该被跳到的那首" {
		t.Error("可重试的错被自动跳到了下一首——网络断了的话这是在白烧整张队列")
	}
}
