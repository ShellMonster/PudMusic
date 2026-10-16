package app_test

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// 这一组盯的是**接线**，不是下载引擎本身（分块、206 检查、续传、净化
// 都在 `service/download` 的包内测试里跑，那里能精确造出每一种形态）。
//
// 这里要验的是三件只有 app 层知道的事：
//
//	1. 取流走的是**同一个** B 站客户端，音质偏好是用户选的那一档
//	2. 下载目录来自设置，不是某一层的常量
//	3. 拉字节用的 http.Client 和在线播放是**同一个**（凭据边界）

// newDownloadApp 建一个挂了在线播放**和**下载的 App。
//
// ⚠️ Media 用 fixture 的 client，不走 `CredentialTransport`：凭据边界有
// 它自己的一条用例（`TestTheMediaClientGivesTheCDNNoCookie`），这里要验
// 的是取流 → 队列 → 落盘这条线。
func newDownloadApp(t *testing.T, f *streamFixture, dlDir, queueFile string) *app.App {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)

	// 设置得挂上：下载目录是从设置里读的（`Settings.DownloadPath`）。
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatalf("挂设置：%v", err)
	}
	t.Cleanup(func() { _ = a.CloseSettings() })

	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	// ⚠️ 音质偏好要照 `ui.MountStreaming` 那样**从设置里读**（#67 加的）。
	// 少了这一行，下载拿到的是零值 `AudioAuto`——于是「用户选了省流量、
	// 下回来的却是 320K」这条在测试里看不出来。
	a.MountStreaming(app.Streaming{
		Client:  cli,
		Media:   f.Client(),
		Quality: app.PrefToBili(a.SettingsSnapshot().AudioQuality),
	})

	if err := a.MountDownloads(app.Downloads{
		Media: f.Client(), QueueFile: queueFile,
		// 设置里没选过时退回这个（真机上就是系统下载目录）。
		FallbackDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("挂下载：%v", err)
	}
	if dlDir != "" {
		if _, err := a.UpdateSettings(func(s *app.Settings) { s.DownloadPath = dlDir }); err != nil {
			t.Fatalf("设置下载目录：%v", err)
		}
	}
	return a
}

// serveRanged 把 fixture 的 CDN 换成一台认 Range 的（`streamFixture` 自带
// 那个对 `bytes=0-…` 回 200，而下载引擎要求带 Range 的一律 206）。
func serveRanged(f *streamFixture, data []byte) *atomic.Int32 {
	var hits atomic.Int32
	f.on("/cdn/audio.m4s", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		start, end := int64(0), int64(len(data)-1)
		if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
			a, b, _ := strings.Cut(strings.TrimPrefix(h, "bytes="), "-")
			start, _ = strconv.ParseInt(a, 10, 64)
			if b != "" {
				end, _ = strconv.ParseInt(b, 10, 64)
			}
		}
		if start > int64(len(data)) {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end > int64(len(data))-1 {
			end = int64(len(data)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	})
	return &hits
}

// mustGet 读一项的快照。`app_test` 是外部包，只能走公开方法——
// 正好说明记录页（#31）也只能走这一条路。
func mustGet(a *app.App, id string) (download.Snapshot, bool) { return a.DownloadSnapshot(id) }

// 🔴 一条完整的线：点下载 → 取流 → 分块 → 落盘 → 文件名是净化过的。
func TestADownloadGoesFromAFavoriteItemToAFileOnDisk(t *testing.T) {
	f := newStreamFixture(t)
	data := []byte(strings.Repeat("biu-download-", 400)) // 5 KB
	hits := serveRanged(f, data)
	f.onPlayURL(dashFor(cdnURL(f.URL, "audio.m4s", 2*time.Hour), int(bili.QualityHigh320), 110836))

	dir := t.TempDir()
	a := newDownloadApp(t, f, dir, filepath.Join(t.TempDir(), "queue.json"))

	// 歌名带着 B 站搜索结果里那种脏东西：标签 + 实体 + 冒号。
	const title = `<em class="keyword">残酷</em>物语: 陈绮贞`
	snap, err := a.Download(onlineItem("BV1xx411c7mD", "c123", title))
	if err != nil {
		t.Fatalf("下载：%v", err)
	}

	waitFor(t, 20*time.Second, "下完", func() bool {
		got, ok := mustGet(a, snap.ID)
		return ok && got.Status == download.StatusDone
	})

	got, _ := mustGet(a, snap.ID)
	if got.Done != int64(len(data)) {
		t.Fatalf("下了 %d 字节，源有 %d", got.Done, len(data))
	}
	// 音质徽章从 B 站给的那一档来（app 层问的是 `bili.Client`）。
	if got.Label != bili.QualityHigh320.Label(110836) {
		t.Fatalf("音质是 %q", got.Label)
	}
	// 净化过的名字落在下载目录里。
	// 扩展名按**地址上真的后缀**落盘，不是入队时那个临时猜的 `.m4a`。
	want := filepath.Join(dir, "残酷物语_ 陈绮贞.m4s")
	if got.Dest != want {
		t.Fatalf("落到了 %q，期望 %q", got.Dest, want)
	}
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("读落盘的文件：%v", err)
	}
	if string(b) != string(data) {
		t.Fatalf("落盘的内容和源不一样")
	}
	if hits.Load() == 0 {
		t.Fatalf("一次 CDN 都没打")
	}
}

// 🔴 下载用的是**用户在设置里选的那一档**，不是自动档。
//
// 主干把 #67（在线音质覆盖）合进来之后这一条才成立：`MountStreaming` 现在把
// 设置里的偏好喂给 `Streaming.Quality`，而下载的取流走的是**同一个**
// `Streaming`（`resolveDownload` → `streamingSnapshot`）。所以这条是白捡的
// ——也正因为白捡，不钉住就会悄悄退回自动档：用户为了省流量选了 64K，下回来
// 的是 320K，而界面上一点提示都没有（那不是「错」，是白花三倍流量下载）。
//
// ⚠️ 取流接口**不带 `qn`**——它把所有档都返回来，由 `bili.pickAudio` 按偏好
// 在客户端挑。所以这条造的是「同一个 dash 里给两档、两个不同的地址」，断的是
// 下载最后落在**哪一个**上。
func TestDownloadUsesTheQualityTheUserPicked(t *testing.T) {
	f := newStreamFixture(t)
	const bigN, smallN = 4096, 512

	serveFixed(f, "/cdn/high.m4s", bytes.Repeat([]byte("B"), bigN))
	serveFixed(f, "/cdn/low.m4s", bytes.Repeat([]byte("s"), smallN))
	f.onPlayURL(map[string]any{
		"duration": 1,
		"audio": []any{
			map[string]any{"id": int(bili.QualityHigh320), "bandwidth": 110836,
				"codecs": "mp4a.40.2", "mimeType": "audio/mp4",
				"baseUrl": cdnURL(f.URL, "high.m4s", 2*time.Hour)},
			map[string]any{"id": int(bili.QualityLow64), "bandwidth": 50000,
				"codecs": "mp4a.40.2", "mimeType": "audio/mp4",
				"baseUrl": cdnURL(f.URL, "low.m4s", 2*time.Hour)},
		},
	})

	a := newDownloadApp(t, f, t.TempDir(), filepath.Join(t.TempDir(), "queue.json"))

	// 用户选了「省流量」。
	if _, err := a.UpdateSettings(func(s *app.Settings) {
		s.AudioQuality = app.AudioQualityLow
	}); err != nil {
		t.Fatal(err)
	}
	a.SetAudioQuality(app.PrefToBili(app.AudioQualityLow))

	snap, err := a.Download(onlineItem("BV1", "c1", "省流量的歌"))
	if err != nil {
		t.Fatal(err)
	}
	got := waitDone(t, a, snap.ID)
	if got.Total != smallN {
		t.Fatalf("下回来的是 %d 字节，期望省流量档的 %d —— 取的不是用户选的那一档", got.Total, smallN)
	}
	if got.Label != bili.QualityLow64.Label(50000) {
		t.Fatalf("音质徽章是 %q，期望 64K", got.Label)
	}
}

// waitDone 等一项下完（失败就直接红）。
func waitDone(t *testing.T, a *app.App, id string) download.Snapshot {
	t.Helper()
	var last download.Snapshot
	waitFor(t, 20*time.Second, "下完", func() bool {
		last, _ = a.DownloadSnapshot(id)
		return last.Status == download.StatusDone || last.Status == download.StatusError
	})
	if last.Status != download.StatusDone {
		t.Fatalf("没下完：%s（%s）", last.Status, last.Err)
	}
	return last
}

// serveFixed 在某个 CDN 路径上架一份固定的字节，支持 Range。
func serveFixed(f *streamFixture, path string, data []byte) {
	f.on(path, func(w http.ResponseWriter, r *http.Request) {
		start, end := 0, len(data)-1
		if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
			a, b, _ := strings.Cut(strings.TrimPrefix(h, "bytes="), "-")
			start, _ = strconv.Atoi(a)
			if b != "" {
				end, _ = strconv.Atoi(b)
			}
		}
		if start > len(data)-1 || start > end {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end > len(data)-1 {
			end = len(data) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	})
}

// 下载目录**来自设置**：用户换了目录之后点下载，下的是新目录。
func TestTheDownloadDirectoryComesFromTheSettings(t *testing.T) {
	f := newStreamFixture(t)
	serveRanged(f, []byte("12345678"))
	f.onPlayURL(dashFor(cdnURL(f.URL, "audio.m4s", 2*time.Hour), int(bili.QualityHigh320), 110836))

	first, second := t.TempDir(), t.TempDir()
	a := newDownloadApp(t, f, first, filepath.Join(t.TempDir(), "queue.json"))

	s1, err := a.Download(onlineItem("BV1", "c1", "第一首"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "第一首下完", func() bool {
		g, ok := mustGet(a, s1.ID)
		return ok && g.Status == download.StatusDone
	})

	// 用户在设置页换了下载目录。
	if _, err := a.UpdateSettings(func(s *app.Settings) { s.DownloadPath = second }); err != nil {
		t.Fatal(err)
	}

	s2, err := a.Download(onlineItem("BV2", "c2", "第二首"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "第二首下完", func() bool {
		g, ok := mustGet(a, s2.ID)
		return ok && g.Status == download.StatusDone
	})
	g2, _ := mustGet(a, s2.ID)
	if filepath.Dir(g2.Dest) != second {
		t.Fatalf("第二首下到了 %q，新目录是 %q", g2.Dest, second)
	}
	g1, _ := mustGet(a, s1.ID)
	if filepath.Dir(g1.Dest) != first {
		t.Fatalf("第一首被挪到了 %q", g1.Dest)
	}
}

// 🔴 退出时保存队列、重开后恢复。
//
// 这是「下到一半退出，重开能继续」在 app 层的那一半：退出那一刻的东西
// 要落到磁盘上，重开时要读回来。
func TestTheQueueIsSavedOnQuitAndRestoredOnStart(t *testing.T) {
	f := newStreamFixture(t)
	data := []byte(strings.Repeat("x", 3<<20)) // 3 MB
	serveRanged(f, data)
	f.onPlayURL(dashFor(cdnURL(f.URL, "audio.m4s", 2*time.Hour), int(bili.QualityHigh320), 110836))

	dir, queueFile := t.TempDir(), filepath.Join(t.TempDir(), "queue.json")

	// 第一段：下到一半就退出。
	a1 := newDownloadApp(t, f, dir, queueFile)
	snap, err := a1.Download(onlineItem("BV1", "c1", "半首"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "开始下", func() bool {
		return len(a1.DownloadSnapshots()) > 0
	})
	if err := a1.SaveDownloads(); err != nil {
		t.Fatalf("保存队列：%v", err)
	}
	if _, err := os.Stat(queueFile); err != nil {
		t.Fatalf("队列文件没落盘：%v", err)
	}
	before, _ := a1.DownloadSnapshot(snap.ID)
	a1.Close()

	// 第二段：重开。队列读回来了。
	a2 := newDownloadApp(t, f, dir, queueFile)
	got := a2.DownloadSnapshots()
	if len(got) != 1 || got[0].ID != snap.ID {
		t.Fatalf("重开后队列是 %+v", got)
	}
	if got[0].Title != "半首" || got[0].Dest != before.Dest {
		t.Fatalf("恢复出来的是 %+v，退出前是 %+v", got[0], before)
	}

	// 状态按「退出那一刻它在干什么」还原：下完了还是下完，出错了还是
	// 出错，只有**在飞的**那些变成暂停（重开那一刻那个 HTTP 请求早就不
	// 存在了，记成 running 会让界面上永远转圈）。
	switch before.Status {
	case download.StatusRunning, download.StatusQueued:
		if got[0].Status != download.StatusPaused {
			t.Fatalf("在飞的恢复成 %q，要的是暂停", got[0].Status)
		}
	default:
		if got[0].Status != before.Status {
			t.Fatalf("恢复出来是 %q，退出前是 %q —— 下完了就该还是下完",
				got[0].Status, before.Status)
		}
	}
}

// 取流失败要变成记录页上那一行的一句人话，不是「下到一半停住」。
func TestAFailedResolveBecomesAReadableError(t *testing.T) {
	f := newStreamFixture(t)
	serveRanged(f, []byte("12345678"))
	// 取流接口回一个 B 站的错误码。这一条走的是**同步就能看出来**的
	// 失败：任务会先排上队，再在后台 goroutine 里失败。
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": -404, "message": "啥都取不到", "ttl": 1})
	})

	a := newDownloadApp(t, f, t.TempDir(), filepath.Join(t.TempDir(), "queue.json"))
	snap, err := a.Download(onlineItem("BV1", "c1", "歌"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "任务出错", func() bool {
		g, ok := mustGet(a, snap.ID)
		return ok && g.Status == download.StatusError
	})
	got, _ := mustGet(a, snap.ID)
	if got.Err == "" {
		t.Fatalf("出错了却没留下一句话")
	}
	// 「加载中」不能是终态：那一行永远转下去，用户看不出发生了什么。
	if got.Status == download.StatusQueued || got.Status == download.StatusRunning {
		t.Fatalf("失败之后还停在 %q", got.Status)
	}
}

// 没挂下载 / 条目不完整时给的是人话，不是 panic 也不是空指针。
func TestDownloadRefusesWhatItCannotDoAndSaysWhy(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	if a.DownloadMounted() {
		t.Fatalf("没挂却说挂了")
	}
	if _, err := a.Download(onlineItem("BV1", "c1", "歌")); err == nil {
		t.Fatalf("没挂下载却收了请求")
	}
	if got := a.DownloadSnapshots(); got != nil {
		t.Fatalf("没挂却有快照：%+v", got)
	}
	if err := a.PauseDownload("nope"); err == nil {
		t.Fatalf("没挂却让暂停了")
	}
	// 没挂时保存队列不报错（退出路径上没有用户在看）。
	if err := a.SaveDownloads(); err != nil {
		t.Fatalf("没挂时保存队列报错：%v", err)
	}

	// 挂了，但条目本身不完整。
	f := newStreamFixture(t)
	a2 := newDownloadApp(t, f, t.TempDir(), "")
	for _, it := range []app.Item{
		{Source: "online", Type: "audio", Sid: "s1"},         // 音乐库那条路还没接
		{Source: "online", Type: "mv", Bvid: "BV1"},          // 缺 cid
		{Source: "local", Type: "local", Path: "/tmp/x.m4a"}, // 本地文件
	} {
		if _, err := a2.Download(it); err == nil {
			t.Fatalf("%+v 被收下了", it)
		} else if !strings.Contains(err.Error(), "还不能放") && !strings.Contains(err.Error(), "还没接上") {
			t.Fatalf("%+v 的错不像人话：%v", it, err)
		}
	}
	// 本地文件不进下载队列。
	if got := a2.DownloadSnapshots(); len(got) != 0 {
		t.Fatalf("队列里有 %d 项", len(got))
	}
}

// 暂停 / 继续 / 删除 / 清空四个动作在 App 上都通。
func TestTheFourQueueActionsAreWired(t *testing.T) {
	f := newStreamFixture(t)
	data := []byte(strings.Repeat("y", 8<<20)) // 8 MB
	serveRanged(f, data)
	f.onPlayURL(dashFor(cdnURL(f.URL, "audio.m4s", 2*time.Hour), int(bili.QualityHigh320), 110836))

	dir := t.TempDir()
	a := newDownloadApp(t, f, dir, filepath.Join(t.TempDir(), "queue.json"))

	snap, err := a.Download(onlineItem("BV1", "c1", "歌"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PauseDownload(snap.ID); err != nil {
		t.Fatalf("暂停：%v", err)
	}
	if err := a.ResumeDownload(snap.ID); err != nil {
		t.Fatalf("继续：%v", err)
	}
	waitFor(t, 20*time.Second, "下完", func() bool {
		g, ok := mustGet(a, snap.ID)
		return ok && g.Status == download.StatusDone
	})
	got, _ := mustGet(a, snap.ID)
	if _, err := os.Stat(got.Dest); err != nil {
		t.Fatalf("下完的歌不在：%v", err)
	}

	if err := a.RemoveDownload(snap.ID); err != nil {
		t.Fatalf("删除：%v", err)
	}
	if _, ok := mustGet(a, snap.ID); ok {
		t.Fatalf("删了还能问出来")
	}
	// 删的是队列项，不是硬盘上那首歌。
	if _, err := os.Stat(got.Dest); err != nil {
		t.Fatalf("删队列项把那首歌也删了：%v", err)
	}

	s2, err := a.Download(onlineItem("BV2", "c2", "另一首"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "第二首下完", func() bool {
		g, ok := mustGet(a, s2.ID)
		return ok && g.Status == download.StatusDone
	})
	a.ClearDownloads()
	if got := a.DownloadSnapshots(); len(got) != 0 {
		t.Fatalf("清空之后队列里还有 %d 项", len(got))
	}
}
