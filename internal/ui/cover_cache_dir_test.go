package ui

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo"
)

// TestCoverCacheDirIsUnderTheAppsCachePath 钉「封面缓存目录是专用的」：它在
// `mygo.PathCache`（系统给这个应用的缓存目录）底下，不在用户数据目录里。
//
// 这一条不是纯洁癖：缓存目录会被整个当成「这些都可以删」来管——超上限的文件
// 会被删掉。指到用户数据目录里的话，设置、历史、下载记录和封面缓存住同一片
// 地方，哪天上限一算，删的就不只是封面了。指到用户文件目录（文稿、下载）里
// 更糟：那里面一个字节都不该由我们删。
func TestCoverCacheDirIsUnderTheAppsCachePath(t *testing.T) {
	cacheRoot := t.TempDir()
	dataRoot := t.TempDir()
	mygo.App.SetPath(mygo.PathCache, cacheRoot)
	mygo.App.SetPath(mygo.PathUserData, dataRoot)
	t.Cleanup(func() {
		mygo.App.SetPath(mygo.PathCache, "")
		mygo.App.SetPath(mygo.PathUserData, "")
	})

	dir := coverCacheDir()
	if dir == "" {
		t.Fatal("拿得到缓存目录的时候不该返回空串")
	}
	if parent := filepath.Dir(dir); parent != cacheRoot {
		t.Errorf("封面缓存目录 %q 不在应用缓存路径 %q 底下", dir, cacheRoot)
	}
	rel, err := filepath.Rel(dataRoot, dir)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Errorf("封面缓存目录 %q 落在了用户数据目录 %q 里", dir, dataRoot)
	}
}

// TestTheMiniWindowSharesTheMainWindowsCoverCache 钉「一个磁盘缓存目录只有一份
// 记账人」：迷你窗口用的是主窗口那份封面缓存；关掉迷你**不能**把它收掉。
//
// 两个实例同时写一个目录的话，各算各的字节数，上限就成了「各自以为的上限」；
// 而迷你关掉时要是把主窗口那份关了，主窗口后面所有新封面会永远停在占位
// ——界面上没有任何报错。
func TestTheMiniWindowSharesTheMainWindowsCoverCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("不是图也没关系：这一条只看到没看到「已关闭」"))
	}))
	defer srv.Close()

	a := app.New()
	t.Cleanup(a.Close)
	main := newTestShell(t, onApp(a)).s
	mini := main.newMiniShell()
	if mini.images != main.images {
		t.Fatalf("迷你窗口用的是自己那份封面缓存（%p），不是主窗口那份（%p）；"+
			"两份同时写磁盘缓存目录，字节上限会各算各的", mini.images, main.images)
	}
	mini.releaseMini()

	// 迷你收尾之后再要一张：主窗口那份必须还活着（关了的话 Get 给 ErrClosed）。
	url := srv.URL + "/after-mini.png"
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := main.images.Get(url)
		if s.Err == imgcache.ErrClosed {
			t.Fatal("关掉迷你窗口把主窗口那份封面缓存也关了：主窗口的新封面会永远停在占位")
		}
		if !s.Loading {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatal("迷你收尾之后主窗口的封面缓存不再干活")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestTheMiniWindowKeepsItsCoversAfterTheMainWindowIsClosed 钉两扇窗的寿命
// 解耦：迷你借的是主窗口那份封面缓存，而**主窗口先被关掉**时迷你那份必须还能
// 用。
//
// 没有引用计数的话，主窗口那个外壳一收尾就把这份缓存关了，迷你之后每要一张
// 图都是 `ErrClosed` 永久失败态——界面上只是「图不出来」，没有一句报错。
func TestTheMiniWindowKeepsItsCoversAfterTheMainWindowIsClosed(t *testing.T) {
	body := pngBytesForUI(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	a := app.New()
	t.Cleanup(a.Close)
	main := newTestShell(t, onApp(a)).s
	mini := main.newMiniShell()
	t.Cleanup(mini.releaseMini)

	main.Close() // 主窗口那个外壳收尾：生产里窗口真被销毁时走的就是它

	url := srv.URL + "/after-main-closed.png"
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := mini.images.Get(url)
		if s.Err == imgcache.ErrClosed {
			t.Fatal("主窗口收尾把迷你那份封面缓存一起关了：迷你之后每张图都是「已关闭」")
		}
		if s.Ready() {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("主窗口收尾之后迷你收不到图：%v", s.Err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// pngBytesForUI 编一张能解码的小图（界面层这边没有 imgcache 的那个 helper）。
func pngBytesForUI(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
