package imgcache

// 落盘那一半的测试：上限、淘汰、启动清扫、目录边界、半截文件、关掉落盘。
//
// 全部在 `t.TempDir()` 里跑，不碰用户真正的缓存目录，也不连真图床
// （用到网络的那几条走 `httptest`）。

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// diskTestBody 是测试用的 10 字节缓存内容。直接调 diskCache 的那几条要的只是
// 「有多大」，不需要是能解码的图。
var diskTestBody = []byte("0123456789")

func diskPath(dir, name string) string { return filepath.Join(dir, name) }

// TestDiskCacheEvictsTheLeastRecentlyUsed 验磁盘缓存按字节上限挤，而且挤的是
// **最久未用**的那一份，不是最早写进去的那一份。
func TestDiskCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	d := newDiskCache(dir, 25)
	t.Cleanup(d.close)
	<-d.ready

	a, b, c := diskNameFor("a"), diskNameFor("b"), diskNameFor("c")
	d.writeSync(a, diskTestBody)
	d.writeSync(b, diskTestBody)

	// 用一下 a：之后 b 才是「最久未用」。如果淘汰按写入顺序，被挤掉的会是 a。
	if _, ok := d.open(a); !ok {
		t.Fatal("刚写进去的 a 读不出来")
	}
	d.writeSync(c, diskTestBody)

	if _, err := os.Stat(diskPath(dir, b)); !os.IsNotExist(err) {
		t.Errorf("最久未用的 b 还在盘上（stat 返回 %v），应该被挤掉", err)
	}
	for _, name := range []string{a, c} {
		if _, err := os.Stat(diskPath(dir, name)); err != nil {
			t.Errorf("最近用过的 %s 被误删了：%v", name, err)
		}
	}
	d.mu.Lock()
	used := d.used
	d.mu.Unlock()
	if used > 25 {
		t.Errorf("挤过之后账面还是 %d 字节，上限是 25", used)
	}
}

// TestStartupSweepTrimsWhatTheLastRunLeft 验启动时清一次超限目录：上次异常退出
// 留下的超额文件按修改时间从老到新删掉，写到一半的临时文件直接删掉，
// **而目录里不是缓存的东西一个都不碰**。
func TestStartupSweepTrimsWhatTheLastRunLeft(t *testing.T) {
	dir := t.TempDir()
	oldest, middle, newest := diskNameFor("old"), diskNameFor("mid"), diskNameFor("new")
	now := time.Now()
	for name, age := range map[string]time.Duration{
		oldest: 3 * time.Hour,
		middle: 2 * time.Hour,
		newest: time.Hour,
	} {
		path := diskPath(dir, name)
		if err := os.WriteFile(path, diskTestBody, 0o644); err != nil {
			t.Fatal(err)
		}
		when := now.Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}

	// 上次写到一半被杀：最终文件名下什么都没有，只剩这个临时文件。
	crashed := diskNameFor("crashed") + diskTempMark + "123456"
	if err := os.WriteFile(diskPath(dir, crashed), []byte("半截"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 用户/别的程序放进来的东西：不是我们命名的，不删、也不计入字节账。
	foreign := diskPath(dir, "notes.txt")
	if err := os.WriteFile(foreign, []byte("别删我"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := diskPath(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	d := newDiskCache(dir, 25)
	t.Cleanup(d.close)
	<-d.ready

	if _, err := os.Stat(diskPath(dir, oldest)); !os.IsNotExist(err) {
		t.Errorf("最老的那份没有被清掉（stat 返回 %v）", err)
	}
	for _, name := range []string{middle, newest} {
		if _, err := os.Stat(diskPath(dir, name)); err != nil {
			t.Errorf("后写进去的 %s 被误删了：%v", name, err)
		}
	}
	if _, err := os.Stat(diskPath(dir, crashed)); !os.IsNotExist(err) {
		t.Errorf("上次写到一半的临时文件没有清掉（stat 返回 %v）", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("目录里不是缓存的文件被删了：%v", err)
	}
	if _, err := os.Stat(sub); err != nil {
		t.Errorf("目录里的子目录被删了：%v", err)
	}
	d.mu.Lock()
	used := d.used
	d.mu.Unlock()
	if used > 25 {
		t.Errorf("清扫之后账面还是 %d 字节，上限是 25", used)
	}
}

// TestDeletionNeverLeavesTheCacheDir 是「删除只会发生在缓存目录内」那一条的
// 证据：带 `../` 的名字写不进去、也删不掉目录外面的文件；目录里别人的文件
// 在淘汰时也不会被顺手删掉。
func TestDeletionNeverLeavesTheCacheDir(t *testing.T) {
	root := t.TempDir()
	dir := diskPath(root, "cache")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	precious := diskPath(root, "precious.txt")
	if err := os.WriteFile(precious, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := diskPath(dir, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("not ours"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := newDiskCache(dir, 10)
	t.Cleanup(d.close)
	<-d.ready

	// 想逃出目录：写和删都必须被 `filePath` 拒掉。
	d.writeSync("../precious.txt", []byte("overwritten"))
	d.remove("../precious.txt")
	if got, err := os.ReadFile(precious); err != nil || !bytes.Equal(got, []byte("keep")) {
		t.Fatalf("缓存目录外面的文件被动了：内容 %q，err %v", got, err)
	}

	// 连写三张，上限只放得下一张：挤掉的是我们自己的，不是 foreign.txt。
	for i := range 3 {
		d.writeSync(diskNameFor(string(rune('a'+i))), diskTestBody)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("淘汰把目录里别人的文件删了：%v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	ours := 0
	for _, ent := range entries {
		if isDiskName(ent.Name()) {
			ours++
		}
	}
	if ours > 1 {
		t.Errorf("上限 10 字节、每张 10 字节，盘上却留了 %d 张", ours)
	}
}

// TestAHalfWrittenFileIsNeverDecoded 验：最终文件名下如果躺着半截字节
// （外部写坏、旧版本留下的），解码失败之后会把那一份删掉、回去下真的，
// 而不是把坏图一直画下去。
func TestAHalfWrittenFileIsNeverDecoded(t *testing.T) {
	body := pngBytes(t, 16, 16)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	url := srv.URL + "/broken-entry.png"
	name := diskNameFor(url)
	// 模拟「半截字节直接落在了最终文件名上」。
	if err := os.WriteFile(diskPath(dir, name), body[:len(body)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(Options{Dir: dir})
	t.Cleanup(c.Close)

	waitState(t, c, url, 5*time.Second, loaded)
	if n := hits.Load(); n != 1 {
		t.Errorf("盘上那份坏了，应该只回去下一次真的，实际下了 %d 次", n)
	}

	// 关掉会把排着的写盘落完；之后盘上那份必须是完整、能解码的图。
	c.Close()
	got, err := os.ReadFile(diskPath(dir, name))
	if err != nil {
		t.Fatalf("坏缓存删掉之后没有写回好的那份：%v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("写回盘上的不是图床给的字节（%d 字节 vs %d 字节）", len(got), len(body))
	}
}

// TestACrashedWriteIsJustAMiss 验写到一半留下的临时文件按「没有」处理：
// 它不挡命中，下一次下载照常进行，清扫顺手把它删掉。
func TestACrashedWriteIsJustAMiss(t *testing.T) {
	body := pngBytes(t, 16, 16)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	url := srv.URL + "/crashed.png"
	tmp := diskPath(dir, diskNameFor(url)+diskTempMark+"987654")
	if err := os.WriteFile(tmp, body[:len(body)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(Options{Dir: dir})
	t.Cleanup(c.Close)

	waitState(t, c, url, 5*time.Second, loaded)
	if n := hits.Load(); n != 1 {
		t.Errorf("临时文件不该被当成命中：实际下了 %d 次", n)
	}
	waitFor(t, 2*time.Second, "启动清扫删掉了半截临时文件", func() bool {
		_, err := os.Stat(tmp)
		return os.IsNotExist(err)
	})
}

// TestEmptyDirMeansNoDisk 验空选项就是现在的零值语义：不落盘，新建实例不会
// 复用上一次的字节。
func TestEmptyDirMeansNoDisk(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(pngBytes(t, 16, 16))
	}))
	defer srv.Close()

	url := srv.URL + "/memory-only.png"
	first := New(Options{})
	if first.disk != nil {
		t.Fatal("空 Dir 却建了磁盘缓存")
	}
	waitState(t, first, url, 5*time.Second, loaded)
	first.Close()

	second := New(Options{})
	t.Cleanup(second.Close)
	waitState(t, second, url, 5*time.Second, loaded)
	if n := hits.Load(); n != 2 {
		t.Errorf("没配目录的两个实例一共该下 2 次，实际 %d 次", n)
	}
}

// TestRestartServesFromDiskWithNoRequest 验重启之后不发请求就能拿到图。
//
// 第一实例关掉之后把 HTTP 服务器也关掉：第二实例要是去网络找，连接会被拒，
// 不可能变成「有图」。
func TestRestartServesFromDiskWithNoRequest(t *testing.T) {
	var hits atomic.Int32
	body := pngBytes(t, 16, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))

	dir := t.TempDir()
	url := srv.URL + "/restart.png"

	first := New(Options{Dir: dir})
	waitState(t, first, url, 5*time.Second, loaded)
	first.Close()
	srv.Close()

	second := New(Options{Dir: dir})
	t.Cleanup(second.Close)
	s := waitState(t, second, url, 5*time.Second, loaded)
	if w, h := s.Bitmap.Size(); w != 16 || h != 16 {
		t.Errorf("从盘上解出来的是 %dx%d，want 16x16", w, h)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("第二实例又打了图床：一共 %d 次请求", n)
	}
}

// TestDiskReadsDoNotBlockTheCaller 验读盘在后台：把磁盘读卡住，`Get`（渲染帧
// 每张图都调它）必须照样立刻返回占位态。
func TestDiskReadsDoNotBlockTheCaller(t *testing.T) {
	dir := t.TempDir()
	url := "https://example.invalid/on-disk.webp"
	body := pngBytes(t, 16, 16)
	if err := os.WriteFile(diskPath(dir, diskNameFor(url)), body, 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(Options{Dir: dir})
	t.Cleanup(c.Close)

	gate := make(chan struct{})
	c.disk.readFile = func(path string) ([]byte, error) {
		<-gate
		return os.ReadFile(path)
	}

	done := make(chan State, 1)
	go func() { done <- c.Get(url) }()
	select {
	case s := <-done:
		if s.Ready() || !s.Loading {
			t.Fatalf("第一帧拿到 %+v，want 占位态", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Get 在等磁盘读：渲染帧里做了磁盘 IO")
	}

	close(gate)
	waitState(t, c, url, 5*time.Second, loaded)
}

// TestDiskWritesDoNotBlockTheCaller 验写盘在自己的 worker 里：把写盘卡住，
// 图照样从「占位」变成「有图」，`Get` 不会被写盘拖住。
func TestDiskWritesDoNotBlockTheCaller(t *testing.T) {
	body := pngBytes(t, 16, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	url := srv.URL + "/slow-disk.png"
	c := New(Options{Dir: dir})
	t.Cleanup(c.Close)

	gate := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	c.disk.writeFile = func(path string, data []byte) error {
		once.Do(func() { close(entered) })
		<-gate
		return writeDiskFile(dir, path, data)
	}

	c.Get(url)
	waitFor(t, 5*time.Second, "写盘 worker 卡在磁盘上", func() bool {
		select {
		case <-entered:
			return true
		default:
			return false
		}
	})

	// 写盘还卡着，图上必须已经能出来了（解码和解码结果是两回事）。
	waitState(t, c, url, 5*time.Second, loaded)
	close(gate)

	// 收尾会把排着的那一笔落完。
	c.Close()
	if got, err := os.ReadFile(diskPath(dir, diskNameFor(url))); err != nil || !bytes.Equal(got, body) {
		t.Errorf("收尾之后盘上没有完整的那一份：%d 字节，err %v", len(got), err)
	}
}
