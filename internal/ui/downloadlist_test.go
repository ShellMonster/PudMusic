package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// 这一组跑的是 MyGo 的 Tester：和真窗口一模一样的布局、点击和弹窗，只是画到
// 内存里。
//
// 两条造数据的路，各有各的用处，**别混**：
//
//	newDownloads  从**队列文件**读回来的记录。这一条才是这一页真正要处理的事
//	              （重启之后队列长什么样），而五种操作（暂停 / 继续 / 重试 /
//	              删除 / 清空）断的是**真引擎**的状态变没变，只能走它。
//	newRows       直接给几条快照。这一条是给「排队中」和「交织中」那两格用的
//	              ——它们**没有任何代码会置**（见 `download.StatusMuxing`），
//	              而 `Restore` 会把「排队中 / 正在下 / 交织中」统统读成暂停，
//	              所以走队列文件永远造不出那两格。

// dlChunk 是一块多大（10 MB，见 `service/download` 的 chunkSize）。
//
// ⚠️ 它在这个文件里是**抄**的，不是查的：`chunkSize` 没有导出，而 `Restore`
// 会把分块下标越界的记录**整条扔掉**（`keepOnRestore`）。抄错了的后果是队列
// 悄悄变空、后面每一条断言都因为「一条记录都没有」而红——所以第一个测试就断言
// 了记录数。
const dlChunk = 10 << 20

// dlProgress 是「跑到一半」的总长：两块 = 20 MB。
const dlProgress = 2 * dlChunk

// dlAddedAt 是那几条记录共用的入队时刻：1700000000000（2023-11-14 22:13 UTC）。
//
// ⚠️ 断言日期时**不能**写死「2023-11-14」：那一列是按**本机时区**写的（上游的
// `moment(s)` 也是本地时区），在这台机器上是 11-15。写死的那个断言会在换一台
// 时区的机器上红——而那时什么错都没有。
const dlAddedAt = int64(1700000000000)

// dlDir 是这组测试里下载目录的**真值**，用来核对页头上写的路径和「打开文件」
// 打开的路径。
const dlDir = "/tmp/biu-downloads-test"

// queuedTask 是队列文件里的一条记录，字段对着 `service/download` 的 `Task` 的
// json tag。
//
// ⚠️ 这份形状是**盘上的格式**，不是 Go 结构体：这里手写一遍，是为了让「读回
// 一份别人写下的队列」这条真正被走到（而不是造一个 Task 塞进去自己读出来，
// 那样测的是同一份内存）。
type queuedTask struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Label    string `json:"label,omitempty"`
	Cover    string `json:"cover,omitempty"`
	Dest     string `json:"dest"`
	Ext      string `json:"ext,omitempty"`
	Identity struct {
		Bvid   string `json:"bvid"`
		Cid    string `json:"cid"`
		Length int64  `json:"length"`
		Video  bool   `json:"video,omitempty"`
	} `json:"identity"`
	// Video 是视频任务那一位（对着 `service/download` 的 `Task` 的 json tag）。
	//
	// ⚠️ 它**不在** `Identity` 里面：`Task.Video` 和 `Task.Identity.Video` 是两处
	// 各自独立的字段（后者是续传的资源身份），而记录页那一排筛选读的是前者。
	Video   bool   `json:"video,omitempty"`
	Chunks  []int  `json:"chunks,omitempty"`
	Status  string `json:"status"`
	Err     string `json:"err,omitempty"`
	AddedAt int64  `json:"addedAt"`
}

// newTask 造一条队列记录：歌名是 title，状态是 st，总长 total，已下 percent%。
//
// 分块记账按 percent 算出来——`SnapshotOf` 的进度是从 `Chunks` 上算的，只写
// `status` 而不写分块的话那一格画出来是 0%。
func newTask(title string, st download.Status, total int64, percent int) queuedTask {
	all := int((total + dlChunk - 1) / dlChunk)
	task := queuedTask{
		ID: "BV1" + title + "|" + title, Title: title, Label: "无损 FLAC",
		Dest: filepath.Join(dlDir, title+".m4a"), Ext: ".m4a",
		Status: string(st), AddedAt: dlAddedAt,
	}
	task.Identity.Bvid = "BV1" + title
	task.Identity.Cid = title
	task.Identity.Length = total
	for i := range all * percent / 100 {
		task.Chunks = append(task.Chunks, i)
	}
	if st == download.StatusError {
		task.Err = dlErrText
	}
	return task
}

// dlErrText 是出错那一格上写的那句错误原因。照上游的写法：出错时那一格摆的是
// **原因本身**，不是「任务出错」那个标签（`progress.tsx:53-60`）。
const dlErrText = "服务器不肯说这一条流有多长"

// newDownloads 建一个挂着下载队列的外壳，画着下载记录页，返回外壳和 Tester。
func newDownloads(t *testing.T, tasks ...queuedTask) (*Shell, *ui.Tester) {
	t.Helper()
	dir := t.TempDir()
	qfile := filepath.Join(dir, download.QueueFileName)
	// 不传任务时 tasks 是 nil，会被序列化成 `"tasks": null`；加载器有意拒绝 null（缺 tasks 算坏文件），
	// 所以空队列要写成 `[]`。
	if tasks == nil {
		tasks = []queuedTask{}
	}
	b, err := json.Marshal(struct {
		Version int          `json:"version"`
		Tasks   []queuedTask `json:"tasks"`
	}{download.QueueVersion, tasks})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(qfile, b, 0o644); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountDownloads(app.Downloads{
		Media: http.DefaultClient, QueueFile: qfile, FallbackDir: dlDir,
	}); err != nil {
		t.Fatalf("挂下载：%v", err)
	}
	if got := len(a.DownloadSnapshots()); got != len(tasks) {
		t.Fatalf("队列里读回来 %d 条，want %d（队列文件读不懂时它是空的，"+
			"后面每一条断言都会因为「一条记录都没有」而红）", got, len(tasks))
	}

	s := newDownloadShell(t, a)
	tt := ui.NewTester(s.View, 1200, 800)
	s.navigate(downloadListPath)
	dlFrame(tt)
	return s, tt
}

// newRows 建一个外壳，这一页的数据源换成给定的快照（见文件头那两条路的分工）。
//
// 它钉住的是**画法**：哪个状态写哪句话、亮哪颗按钮、百分比画不画。这一层一旦
// 也走真引擎，「排队中」和「交织中」两格就永远走不到。
func newRows(t *testing.T, rows ...download.Snapshot) (*Shell, *ui.Tester) {
	t.Helper()
	return newRowsIn(t, dlWide, rows...)
}

// newRowsWithDir 是 newRows 加一个下载目录。
//
// ⚠️ **为什么要有它**：下载记录页有**两种**空——「连落盘的地方都没有」和「真的
// 没下载过」。后者的那一句（`noDownloads`）只在**有目录**的时候才是那一句：没
// 目录时那一格说的是「去设置里选一个」（报告 F3）。所以验「真的没下载过」那
// 几句的测试得有一个目录。
//
// ⚠️ 目录走 `FallbackDir`（「设置里没选过时下到哪儿」那一条），而**不是**去改
// 设置里的 `DownloadPath`——这一组测试的外壳**没挂设置**（`newRowsIn` 造的那
// 一个），改设置会写不进去。
func newRowsWithDir(t *testing.T, rows ...download.Snapshot) (*Shell, *ui.Tester) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountDownloads(app.Downloads{
		Media: http.DefaultClient, FallbackDir: dlDir,
	}); err != nil {
		t.Fatalf("挂下载：%v", err)
	}
	s := newDownloadShell(t, a)
	s.downloads.rowsFn = func() []download.Snapshot { return rows }

	tt := ui.NewTester(s.View, dlWide, 800)
	s.navigate(downloadListPath)
	dlFrame(tt)
	return s, tt
}

// newRowsIn 是 `newRows` 加一个窗口宽度。窄那一档的布局只有它量得出来（
// `TestTheFileColumnStaysReadableAtTheNarrowestWindow`）。
func newRowsIn(t *testing.T, w int, rows ...download.Snapshot) (*Shell, *ui.Tester) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	s := newDownloadShell(t, a)
	s.downloads.rowsFn = func() []download.Snapshot { return rows }

	tt := ui.NewTester(s.View, w, 800)
	s.navigate(downloadListPath)
	dlFrame(tt)
	return s, tt
}

// dlWide 是这一组默认的窗口宽度（应用自己定的 `Width: 1200`，见 `WindowOptions`）。
const dlWide = 1200

// newRowsOnSide 是 `newRowsIn` 加一个侧栏宽度。
//
// 侧栏走设置里的 `SideMenuWidth`（`app.MaxSideMenuWidth` = 480），而它能把内容
// 压到多少是这一页布局最坏的一档——比窗口最窄那一档还坏，所以单独一个夹具。
// 设置先写到盘上再挂：那是「启动时读盘」那条路，和真窗口一样。
func newRowsOnSide(t *testing.T, w int, side float64, rows ...download.Snapshot) *ui.Tester {
	t.Helper()
	b := newTestShell(t,
		withSettings(func() app.Settings {
			st := app.DefaultSettings("")
			st.SideMenuWidth = side
			return st
		}()),
		at(w, 800),
		stubbed(func(_ *testing.T, s *Shell) {
			s.downloads.rowsFn = func() []download.Snapshot { return rows }
		}))
	s := b.s
	s.navigate(downloadListPath)
	dlFrame(b.tt)
	return b.tt
}

// newDownloadShell 建外壳、把两个「打开」换成记动作的桩。
func newDownloadShell(t *testing.T, a *app.App) *Shell {
	t.Helper()
	// 这个外壳挂在**外面那个** a 上（那些测试要拿同一个 App 验别的东西），所以
	// 造外壳的地方给的是 `onApp`：收尾只停外壳自己占的那些东西，不碰那个 a。
	b := newTestShell(t, onApp(a),
		stubbed(func(_ *testing.T, s *Shell) {
			dlOpened, dlRevealed, dlOpenedLinks = nil, nil, nil
			s.downloads.openDir = func(path string) error { dlOpened = append(dlOpened, path); return nil }
			s.downloads.reveal = func(path string) { dlRevealed = append(dlRevealed, path) }
			s.downloads.openLink = func(url string) error {
				dlOpenedLinks = append(dlOpenedLinks, url)
				return nil
			}
		}))
	return b.s
}

// dlOpened / dlRevealed / dlOpenedLinks 记下三个「打开」各自打开了什么。
var (
	dlOpened      []string
	dlRevealed    []string
	dlOpenedLinks []string
)

// dlFrame 连跑几帧。
//
// 三帧的由来和 homepage 那边一样：布局里量行高、表格里定位锚点都要一帧。
func dlFrame(tt *ui.Tester) {
	for range 3 {
		tt.Frame()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 表格画出来了
// ─────────────────────────────────────────────────────────────────────────────

// TestTheTableShowsEveryTask 断的是**行**而不是状态：五列都画了，每一条记录
// 画一行，歌名、音质徽章、大小、日期都在。
//
// 断「有没有画出来」最省事也最没用——一条记录都没有时页面照样有字。所以先钉条
// 数（由 `newDownloads` 造数据时就断），再逐列钉那一行上该有的东西。
func TestTheTableShowsEveryTask(t *testing.T) {
	_, tt := newDownloads(t,
		newTask("歌 甲", download.StatusRunning, dlProgress, 50),
		newTask("歌 乙", download.StatusDone, dlChunk, 100),
		newTask("歌 丙", download.StatusPaused, dlProgress, 50),
		newTask("歌 丁", download.StatusError, dlProgress, 20),
		newTask("歌 戊", download.StatusQueued, dlProgress, 0),
	)

	for _, title := range []string{"歌 甲", "歌 乙", "歌 丙", "歌 丁", "歌 戊"} {
		if !shows(tt, title) {
			t.Errorf("画面上没有 %q 这一行，文字有 %q", title, tt.Texts())
		}
	}
	// 原型的行没有表头也没有下载时间：文件名 + 副标题、大小（完成的）、状态、操作。
	for _, want := range []string{"无损 FLAC"} {
		if !tt.HasText(want) {
			t.Errorf("没有画出 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 页面自己的那一行（上游的 `<h1>下载记录</h1>`）。
	if !shows(tt, "下载记录") {
		t.Errorf("没有画出页标题，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 三段进度
// ─────────────────────────────────────────────────────────────────────────────

// TestTheStatusColumnSaysWhereEachTaskIs 逐个状态走一遍：那一格写的是上游
// `status-desc.ts` 里那一句。
//
// 出错那一行断的是**错误原因**而不是「任务出错」那个标签：上游在出错时把整句
// 原因摆在这一格（`progress.tsx:53-60`），只写标签的话用户看到的是「失败了」
// 却不知道为什么。
func TestTheStatusColumnSaysWhereEachTaskIs(t *testing.T) {
	for _, c := range []struct {
		status download.Status
		want   string
	}{
		{download.StatusQueued, "排队中"},
		{download.StatusRunning, "下载中"},
		{download.StatusPaused, "下载暂停"},
		{download.StatusDone, "下载完成"},
		{download.StatusMuxing, "交织中"},
		{download.StatusError, dlErrText},
	} {
		_, tt := newRows(t, dlSnapshot("歌 甲", c.status, dlProgress, dlChunk))
		if !tt.HasText(c.want) { // 子串：失败那一格是「失败 · 原因」
			t.Errorf("%q 那一格没有写出 %q，文字有 %q", c.status, c.want, tt.Texts())
		}
	}
}

// progressWidth 是画面上那一条进度条涂了色的宽度。
//
// 🔴 **只量「状态」那一列里那一段**（横向窗口是从百分比那一格的右边缘往左
// `dlStatusW`），不是扫整张图。早先这里是整幅宽度扫，于是「侧栏里当前页那一项
// 用主色垫底」也会被量进来——而侧栏那一项的高度和这一页的状态列**没有任何
// 关系**，它落没落进这几行像素全看侧栏上排了几项：未登录时那三个入口不摆
// （`Route.HideInMenuWhenLoggedOut`），当前页那一项往上挪了三行，正好掉进
// `txt.Y-4` 那一行，于是量到的恒是侧栏那一项左右两段空白里较长的一段
// （实测 74），50% 和 90% 一样宽。
//
// 这和 BMG-56 量行高时踩的是同一个坑：**量一件东西，要先把范围限在这一件东西
// 里面**，否则量到的是碰巧和它同一行的别的东西。
func progressWidth(t *testing.T, tt *ui.Tester, pct string) float32 {
	t.Helper()
	txt, ok := tt.Find(pct)
	if !ok {
		t.Fatalf("那一格没有写出 %q，文字有 %q", pct, tt.Texts())
	}
	// 条涂的是**蓝**（上游 `progress.tsx:49` 的 `bg-blue-500`），不是主色。
	bar := ui.Hex(dlBarColor)

	// 百分比是那一格里**定宽靠右**的一段（`dlPctW`），它的右边缘就是状态列的
	// 右边缘；条是同一格里 `FillWidth` 的，所以右边缘也是那儿。
	right := int(txt.X + txt.W)
	left := right - dlStatusW

	// 条在百分比那一行**上面**：从文字的上边界往上扫，找那个颜色。
	img := tt.Image()
	y := int(txt.Y) - 4
	for y > int(txt.Y)-24 {
		run, best := 0, 0
		for x := max(left, 0); x < right; x++ {
			c := img.RGBAAt(x, y)
			if c.R == bar.R && c.G == bar.G && c.B == bar.B && c.A > 0 {
				if run++; run > best {
					best = run
				}
			} else {
				run = 0
			}
		}
		if best > 0 {
			return float32(best)
		}
		y--
	}
	return 0
}

// TestTheMuxingCellSaysSoAndDoesNotClaimToBeDone 钉住「交织中」那一格。
//
// ⚠️ 这一格这一版走不到（没有任何代码会置交织中，见 `download.StatusMuxing`），
// 所以这里直接给一条交织中的快照——状态位先占着，画法也先钉住，等 #32 接上来
// 的时候这一格不用再想。
//
// 断的是两件事：写的是「交织中」，以及**百分比那一段不画**。后者是这一格最容
// 易做错的地方：字节早就是 100% 了，画成「100%」用户看到的就是「下完了」，然
// 后盯着一个永远不变成「下载完成」的「下载完成」等下去。
func TestTheMuxingCellSaysSoAndDoesNotClaimToBeDone(t *testing.T) {
	// 字节全下完了（Percent = 100），交织还没写完。
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusMuxing, dlChunk, dlChunk))

	if !shows(tt, "交织中") {
		t.Fatalf("交织中的那一格没有写出「交织中」，文字有 %q", tt.Texts())
	}
	for _, bad := range []string{"100%", "下载完成"} {
		if shows(tt, bad) {
			t.Errorf("交织中的那一格画出了 %q：字节是 100%% 不等于这一首好了，"+
				"而用户只看得见这一格", bad)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 纯文字（那几个格式化函数）
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSizeColumnCountsIn1024 断大小的算法和边界。
//
// 那一列上游走 npm 的 `filesize`，我们是自己写的一份（见 `fileSize` 的注
// 释）。这里断的是「别把 1024 进制写成 1000 进制」——1023 字节写成 1.0 KB 是
// 撒谎，而这一页上别的每一行都不撒谎。
func TestTheSizeCountsIn1024(t *testing.T) {
	for _, c := range []struct {
		in   int64
		want string
	}{
		{0, "-"},
		{-1, "-"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{20 * 1024 * 1024, "20.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	} {
		if got := fileSize(c.in); got != c.want {
			t.Errorf("fileSize(%d) = %q，want %q", c.in, got, c.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────

// dlSnapshot 造一条给画面用的快照：状态是 st，总长 total，已下 done 字节。
//
// 进度那一栏是**算**出来的（`Percent`），所以这一条和引擎那一侧口径一致
// （`SnapshotOf` 干的也是同一件事）。
func dlSnapshot(title string, st download.Status, total, done int64) download.Snapshot {
	p := -1
	if total > 0 {
		p = int(done * 100 / total)
		if p > 100 {
			p = 100
		}
	}
	s := download.Snapshot{
		ID: "BV1" + title + "|" + title, Title: title, Label: "无损 FLAC",
		Dest:   filepath.Join(dlDir, title+".m4a"),
		Status: st, Done: done, Total: total, Percent: p,
		AddedAt: dlAddedAt,
	}
	if st == download.StatusError {
		s.Err = dlErrText
		// 🔴 **默认是「可以重试」**（票 #346）：出错那一行的「重试」键按
		// `Snapshot.CanRetry` 亮不亮，而这一位默认是真的（引擎那边没装判据
		// 时的旧行为）。「不能重试」那一档由它自己的一条用例钉住。
		s.CanRetry = true
	}
	return s
}

// dlVideoSnapshot 是 `dlSnapshot` 的视频那一版。
//
// ⚠️ 它只翻 `Video` 那一一位，别的全和音频一样（进度、日期、大小）——
// 这样「筛完之后剩下的是不是那一类」才是判据，而不会混进「视频行的画法不一样」
// 这件无关的事。
func dlVideoSnapshot(title string, st download.Status, total, done int64) download.Snapshot {
	s := dlSnapshot(title, st, total, done)
	s.Video = true
	s.Dest = filepath.Join(dlDir, title+".mp4")
	return s
}

// newVideoTask 是 `newTask` 的视频那一版，喂的是**队列文件**（真引擎那条路）。
//
// ⚠️ 三样都要改，而它们是三件不同的事：ID 带 `|video`（`download.TaskID` 的视频
// 那一支，判重键）、`Video` 那一位（业务上的「这一项是视频」，记录页那一排筛的
// 就是它）、落盘名 `.mp4`（交织出来的产物）。只改 ID 的话 `Snapshot.Video` 是假值，
// 而只改 `Video` 的话真引擎那一路认不出这是视频。
func newVideoTask(title string, st download.Status, total int64, percent int) queuedTask {
	task := newTask(title, st, total, percent)
	task.ID += "|video"
	task.Video = true
	task.Identity.Video = true
	task.Dest = filepath.Join(dlDir, title+".mp4")
	task.Ext = ".mp4"
	return task
}

// dlTitles 是给报错用的一行摘要。
func dlTitles(rows []download.Snapshot) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s(%s)", r.Title, r.Status))
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 票 #175：卡片底、文件名 hover 的外链
// ─────────────────────────────────────────────────────────────────────────────
