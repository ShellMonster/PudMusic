package app_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 这一组盯的是「加载完成」那一句的**整条链**（票 #321，无障碍）。
//
// 在线流要跑一趟网络（取流 + 开解码器），那一趟里界面上一句话都没有——而读屏
// 软件用户既看不见播放条上转的那枚圈，也听不见「歌放起来了」。
//
// ⚠️ **这里走的是真的那一条链**（队列 → 取流 → CDN → 字节源 → flacdec →
// 播放器），不是拿状态拼一句出来：这一票要验的是「真的发生了就真的念了」。

// saidCollector 收念出去的那些话（后台那条线在念，测试线程在读）。
type saidCollector struct {
	mu    sync.Mutex
	texts []string
}

func (c *saidCollector) sink(text string) {
	c.mu.Lock()
	c.texts = append(c.texts, text)
	c.mu.Unlock()
}

// has 报有没有哪一句等于 want。
func (c *saidCollector) has(want string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.texts {
		if s == want {
			return true
		}
	}
	return false
}

// all 拿一份拷贝。
func (c *saidCollector) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.texts))
	copy(out, c.texts)
	return out
}

// TestALoadedTrackIsReadOut 钉住「加载完成」。
//
// 🔴 **这一句是「刚刚发生了什么」**：这一首备好了、开始放了。它**不是**「这一枚
// 是什么」——后者是 `.Label(...)` 的事，两者不许混。
func TestALoadedTrackIsReadOut(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	said := &saidCollector{}
	a.AddAnnounce(said.sink)

	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")

	waitFor(t, 15*time.Second, "念出「加载完成」", func() bool {
		return said.has("加载完成，正在放《在线的一首》")
	})
}

// TestALocalTrackIsReadOut 钉住本地文件那一路也念一句。
//
// ⚠️ **本地文件不说「加载完成」**：它是同步开的，没有「还在准备」那一段，
// 而在线那一段是有的。它说的是「正在放《…》」——**换了一首**这件事读屏软件
// 用户看不见（他们没有耳朵去等歌响起来）。
func TestALocalTrackIsReadOut(t *testing.T) {
	a := newApp(t)
	said := &saidCollector{}
	a.AddAnnounce(said.sink)

	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 5*time.Second, "念出「正在放」", func() bool {
		for _, s := range said.all() {
			if strings.HasPrefix(s, "正在放《") {
				return true
			}
		}
		return false
	})
}
