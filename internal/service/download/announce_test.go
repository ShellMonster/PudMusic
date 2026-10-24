package download

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// 这一组盯的是「一项下载有结果了」那个回调（票 #321，无障碍）。
//
// 🔴 **「下载完成」这件事界面上几乎没有声音**：下载记录页上那一列变成
// 「下载完成」加一枚绿勾，而用户多半在别的页——**他既看不见那个勾，也听不见
// 任何动静**。所以这一件事得被**念出来**，而念出来之前得先有人报告它。
//
// ⚠️ **只有终态才报**：暂停 / 取消不是「下完了」，报出去是撒谎（用户还要接着下）。

// onDoneCollector 收那个回调报过来的快照。
type onDoneCollector struct {
	ch chan Snapshot
}

func newOnDoneCollector() *onDoneCollector {
	return &onDoneCollector{ch: make(chan Snapshot, 8)}
}

// sink 是装给 Manager 的那一句。
func (c *onDoneCollector) sink(s Snapshot) {
	select {
	case c.ch <- s:
	default:
	}
}

// wait 等下一次报告，最多等 d。
func (c *onDoneCollector) wait(t *testing.T, d time.Duration, what string) Snapshot {
	t.Helper()
	select {
	case s := <-c.ch:
		return s
	case <-time.After(d):
		t.Fatalf("等了 %v，%s 还没发生", d, what)
		return Snapshot{}
	}
}

// nothing 断「回调没有报」。
func (c *onDoneCollector) nothing(t *testing.T, d time.Duration, what string) {
	t.Helper()
	select {
	case s := <-c.ch:
		t.Fatalf("%s：回调却报了一个 %+v", what, s)
	case <-time.After(d):
	}
}

// newManagerWithOnDone 起一个队列，带上那个回调。
func newManagerWithOnDone(t *testing.T, c *onDoneCollector, cdnURL string, dir string) *Manager {
	t.Helper()
	m, err := New(Options{
		Client: http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) {
			return &Source{
				Bvid: bvid, Cid: cid, Quality: 30280,
				Label: "320K", URLs: []string{cdnURL},
			}, nil
		},
		OnDone: c.sink,
	}, "")
	if err != nil {
		t.Fatalf("建队列：%v", err)
	}
	t.Cleanup(func() { closeManager(t, m, dir) })
	return m
}

// TestTheCallbackIsToldWhenAnItemIsDone 钉住「下完了要报一声」。
func TestTheCallbackIsToldWhenAnItemIsDone(t *testing.T) {
	cdn := newCDN(t, 1<<20)
	dir := t.TempDir()
	c := newOnDoneCollector()
	m := newManagerWithOnDone(t, c, cdn.url(), dir)

	snap, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "测试的歌", Dir: dir, Ext: ".m4a"})
	if err != nil {
		t.Fatalf("入队：%v", err)
	}

	got := c.wait(t, 20*time.Second, "回调报「下完了」")
	if got.Status != StatusDone {
		t.Fatalf("回调报的状态是 %q，want %q", got.Status, StatusDone)
	}
	if got.ID != snap.ID {
		t.Fatalf("回调报的是 %q，want %q", got.ID, snap.ID)
	}
	if got.Title != "测试的歌" {
		t.Fatalf("回调报的歌名是 %q，want 「测试的歌」", got.Title)
	}
}

// TestTheCallbackIsToldWhenAnItemFails 钉住「下坏了也要报一声」。
func TestTheCallbackIsToldWhenAnItemFails(t *testing.T) {
	fake := newCDN(t, 1<<20)
	fake.set(func(c *cdn) { c.broken = true })
	dir := t.TempDir()
	c := newOnDoneCollector()
	m := newManagerWithOnDone(t, c, fake.url(), dir)

	if _, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "测试的歌", Dir: dir, Ext: ".m4a"}); err != nil {
		t.Fatalf("入队：%v", err)
	}

	got := c.wait(t, 20*time.Second, "回调报「下坏了」")
	if got.Status != StatusError {
		t.Fatalf("回调报的状态是 %q，want %q", got.Status, StatusError)
	}
	if got.Err == "" {
		t.Fatal("回调报失败却没带错的那一句")
	}
}

// TestTheCallbackIsToldOnlyOnceForAnItem 钉住「一项只报一声」。
//
// 🔴 **重复报是最吵的一种吵**：读屏软件把同一句话念两遍，用户听到的是「下载完成」
// 说了两声——而他只下了一首。
//
// ⚠️ 这不是假想的：下载队列的后台线是**定时**跑的（每 50ms 看一次），而
// 「解码器的错记的是第一条错、不自己清零」那一类的错是会一直冒出来的。
func TestTheCallbackIsToldOnlyOnceForAnItem(t *testing.T) {
	cdn := newCDN(t, 1<<20)
	dir := t.TempDir()
	c := newOnDoneCollector()
	m := newManagerWithOnDone(t, c, cdn.url(), dir)

	if _, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "测试的歌", Dir: dir, Ext: ".m4a"}); err != nil {
		t.Fatalf("入队：%v", err)
	}

	got := c.wait(t, 20*time.Second, "回调报「下完了」")
	if got.Status != StatusDone {
		t.Fatalf("回调报的状态是 %q，want %q", got.Status, StatusDone)
	}
	c.nothing(t, 300*time.Millisecond, "同一项报了第二声")
}
