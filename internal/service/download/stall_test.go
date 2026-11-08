package download

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// stallCDN 是一台「回了响应头、给了一点字节，然后既不说话也不断开」的 CDN。
//
// trickle > 0 时它不卡死：每隔 trickle 给一个字节，直到给完。
type stallCDN struct {
	size    int64
	head    int64 // 卡住之前先给多少字节
	trickle time.Duration

	srv *httptest.Server

	mu    sync.Mutex
	paths []string // 每一发分块请求打到的路径
}

func newStallCDN(t *testing.T, size, head int64, trickle time.Duration) *stallCDN {
	t.Helper()
	c := &stallCDN{size: size, head: head, trickle: trickle}
	c.srv = httptest.NewServer(c)
	t.Cleanup(c.srv.Close)
	return c
}

func (c *stallCDN) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Range") == "bytes=0-0" {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", c.size))
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{payloadByte(0, 0)})
		return
	}
	start, end, ok := parseRange(r.Header.Get("Range"), c.size)
	if !ok {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	c.mu.Lock()
	c.paths = append(c.paths, r.URL.Path)
	c.mu.Unlock()

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, c.size))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	flush := w.(http.Flusher).Flush

	i := start
	for ; i < start+c.head && i <= end; i++ {
		_, _ = w.Write([]byte{payloadByte(0, i)})
	}
	flush()
	if c.trickle <= 0 {
		<-r.Context().Done()
		return
	}
	for ; i <= end; i++ {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(c.trickle):
		}
		_, _ = w.Write([]byte{payloadByte(0, i)})
		flush()
	}
}

func (c *stallCDN) chunkPaths() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.paths...)
}

func newStallManager(t *testing.T, urls []string, idle time.Duration) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := New(Options{
		Client: http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) {
			return &Source{Bvid: bvid, Cid: cid, Quality: 1, Label: "320K", URLs: urls}, nil
		},
	}, "")
	if err != nil {
		t.Fatalf("建队列：%v", err)
	}
	m.idleTimeout = idle
	t.Cleanup(func() { closeManager(t, m, dir) })
	return m, dir
}

// 正文读到一半就不再来字节的连接会被掐掉：先换备用地址，备用的也卡就落成可重试的失败，
// 而不是让这一项（和它后面整条队列）永远停在「下载中」。
func TestAConnectionThatGoesSilentFailsOverAndThenFails(t *testing.T) {
	c := newStallCDN(t, 4096, 512, 0)
	m, dir := newStallManager(t, []string{c.srv.URL + "/main.m4a", c.srv.URL + "/backup.m4a"}, 50*time.Millisecond)

	snap, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "卡住的歌", Dir: dir, Ext: ".m4a"})
	if err != nil {
		t.Fatalf("入队：%v", err)
	}
	got := waitStatus(t, m, snap.ID, StatusError)

	if !strings.Contains(got.Err, ErrStalled.Error()) {
		t.Fatalf("错误应该说连接没有数据了，实际是 %q", got.Err)
	}
	if !got.CanRetry {
		t.Fatal("卡死是网络问题，应该给「重试」")
	}
	if paths := c.chunkPaths(); len(paths) != 2 || paths[0] != "/main.m4a" || paths[1] != "/backup.m4a" {
		t.Fatalf("主地址卡死之后应该换备用地址再试一次，实际请求的是 %v", paths)
	}
}

// 慢但一直有字节进来的连接不会被掐：空闲计时在每次读到字节时重新算，
// 总时长远超空闲上限也能下完。
func TestASlowButSteadyConnectionIsNotCutOff(t *testing.T) {
	const size, head = 64, 44
	c := newStallCDN(t, size, head, 50*time.Millisecond)
	m, dir := newStallManager(t, []string{c.srv.URL + "/main.m4a"}, 500*time.Millisecond)

	snap, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "慢慢来的歌", Dir: dir, Ext: ".m4a"})
	if err != nil {
		t.Fatalf("入队：%v", err)
	}
	got := waitStatus(t, m, snap.ID, StatusDone)
	assertFile(t, got.Dest, 0, size)
	if paths := c.chunkPaths(); len(paths) != 1 {
		t.Fatalf("一直有字节进来就不该重连，实际请求了 %d 次：%v", len(paths), paths)
	}
}
