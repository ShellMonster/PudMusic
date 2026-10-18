package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
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
	} `json:"identity"`
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
			dlOpened, dlRevealed = nil, nil
			s.downloads.openDir = func(path string) error { dlOpened = append(dlOpened, path); return nil }
			s.downloads.reveal = func(path string) { dlRevealed = append(dlRevealed, path) }
		}))
	return b.s
}

// dlOpened / dlRevealed 记下两个「打开」各自打开了什么。
var (
	dlOpened   []string
	dlRevealed []string
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
	// 列名。表头是 ui.Table 自己画的（上游是 HeroUI 的 TableHeader）。
	for _, col := range []string{"文件", "状态", "大小", "下载时间", "操作"} {
		if !shows(tt, col) {
			t.Errorf("没有 %q 那一列的表头，文字有 %q", col, tt.Texts())
		}
	}
	for _, want := range []string{"无损 FLAC", "20.0 MB", time.UnixMilli(dlAddedAt).Format("2006-01-02")} {
		if !tt.HasText(want) {
			t.Errorf("没有画出 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 页面自己的那一行（上游的 `<h1>下载记录</h1>`）。
	if !shows(tt, "下载记录") {
		t.Errorf("没有画出页标题，文字有 %q", tt.Texts())
	}
}

// TestTheFileColumnStaysReadableAtTheNarrowestWindow 钉住**最窄那一档窗口**下
// 歌名还读得出来。
//
// 900 是这个应用自己定的 `WindowOptions.MinWidth`（shell.go），而这一页最难看
// 的尺寸就是它：减去侧栏和页面内边距，表格只剩约 660。MyGo 的 `ui.Table` 把没给
// `Width` 的列当「吃剩下的」，四个定宽吃掉多少，文件那一列就只剩多少——第一版
// 那四个数合计 560，于是文件列只剩 100，封面占掉 48 之后**歌名一行放得下两个
// 字**，剩下的全是「…」。而那一版的注释还说这一档「摆得下封面 + 歌名」，那是
// 错的。
//
// 两种内容宽度各断一遍，因为压着歌名的两样东西各管一半：
//
//	侧栏默认（200）  定宽合计 452 → 文件列剩 208，装得下，所以**不许滚动**
//	                （一个「反正列都有 MinWidth、能滚就行」的实现也绿，而用户在
//	                 最小尺寸下得先滚才够得着删除）
//	侧栏拖到最宽    内容只剩约 400，四个定宽装不下 → 表格横向滚动，文件列由
//	（480）          `MinWidth` 兜在 200 上（MyGo 的 `clampWidth` 只认
//	                `TableColumn.MinWidth`，`mygo/ui/table.go`）
//
// 量法：表头那个框是**字**的框，而两个表头之间隔着的正是文件列的整宽（两个表头
// 格子各自左右各留 10 的内边距，一来一回抵掉）；表格的左边界是「文件」那个框
// 往左 10（单元格里边距），右边界是窗口减去页面的内边距 20。
func TestTheFileColumnStaysReadableAtTheNarrowestWindow(t *testing.T) {
	// dlNarrow 抄的是 `WindowOptions.MinWidth`（internal/ui/shell.go）。抄而不
	// 查是因为那边是个结构体字面量、没有导出的名字可引；而这一条的全部意义就是
	// 「应用自己定的那一档」。
	const dlNarrow = 900
	row := dlSnapshot("歌名很长很长很长的一首歌", download.StatusRunning, dlProgress, dlChunk)

	for _, c := range []struct {
		what string
		side float64
		fits bool // 这一档五列**装得下**（装不下就横向滚动）
	}{
		{"侧栏默认", app.DefaultSideMenuWidth, true},
		{"侧栏拖到最宽", app.MaxSideMenuWidth, false},
	} {
		tt := newRowsOnSide(t, dlNarrow, c.side, row)

		file, okFile := tt.Find("文件")
		status, okStatus := tt.Find("状态")
		if !okFile || !okStatus {
			t.Errorf("%s：表头没画出来，文字有 %q", c.what, tt.Texts())
			continue
		}
		if got := status.X - file.X; got < dlFileMin {
			t.Errorf("%s：窗口 %d 时「文件」列只有 %.0f 宽，want ≥ %d：封面和内边距"+
				"占掉 76，剩下的歌名只够 %d 个汉字", c.what, dlNarrow, got, dlFileMin,
				int((got-dlCover-8-20)/14))
		}

		// 表格实际有多宽（内容那一块的宽度）。
		//
		// ⚠️ **别改成量「操作」那一列的表头框跑没跑到窗口外面**：跑到外面时框架
		// 会把它裁成宽 0（量过一次，`x=889 / w=0`），于是 `x+w > 窗口` 永远不
		// 成立——一个「五列在最小尺寸下要滚动才够得着删除」的实现照样绿。
		// 量表格的边界是量**布局**，量表头是量**裁剪之后**。
		const cellPad = 10 // MyGo 单元格里边距 t.Space(2.5)
		const pagePad = 20 // 这一页的内边距（viewDownloadList 的 Padding(20)）
		content := float32(dlNarrow) - pagePad - (file.X - cellPad)
		need := dlFileMin + dlStatusW + dlSizeW + dlTimeW + dlOpsW
		if c.fits && float32(need) > content {
			t.Errorf("%s：窗口 %d 时表格只有 %.0f 宽，五列最少要 %d：这一档要横向"+
				"滚动才够得着删除（歌名不挤了，可删除按钮挤没了）", c.what, dlNarrow, content, need)
		}
		if !c.fits && float32(need) <= content {
			t.Logf("%s：窗口 %d 时五列还装得下（%.0f ≥ %d），横向滚动那一条走不到——"+
				"这一档只是侧栏太宽时的一个兜底", c.what, dlNarrow, content, need)
		}
	}
}

// TestTheTableShowsASongWithNoLengthAsADash 断「还没探到长度」那一格。
//
// 上游是 `item.totalBytes ? filesize(item.totalBytes) : "-"`（index.tsx:190）。
// 画一个 `0 B` 或者干脆空着的话，用户会以为这首歌是空的——而它只是还没开始下。
func TestTheTableShowsASongWithNoLengthAsADash(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusQueued, 0, 0))
	if !shows(tt, "-") {
		t.Errorf("还没探到长度的那一行没有画出「-」，文字有 %q", tt.Texts())
	}
	if tt.HasText("0 B") {
		t.Errorf("还没探到长度却画成了「0 B」，文字有 %q", tt.Texts())
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
		{download.StatusQueued, "等待中"},
		{download.StatusRunning, "下载中"},
		{download.StatusPaused, "下载暂停"},
		{download.StatusDone, "下载完成"},
		{download.StatusMuxing, "交织中"},
		{download.StatusError, dlErrText},
	} {
		_, tt := newRows(t, dlSnapshot("歌 甲", c.status, dlProgress, dlChunk))
		if !shows(tt, c.want) {
			t.Errorf("%q 那一格没有写出 %q，文字有 %q", c.status, c.want, tt.Texts())
		}
	}
}

// TestTheStatusColumnShowsTheThreeParts 断的是「三段」：进度条 + 状态文字 +
// 百分比，一个都不少，而且百分比跟**已下的字节**对得上。
//
// 为什么盯着百分比：这一列是用户判断「还差多少」的唯一依据，而 `Percent` 是
// 「没探到长度时 -1」的那一个数。把 -1 当 0 画出去的话，排着队的那一行会写着
// 「等待中 0%」，用户以为卡住了。
func TestTheStatusColumnShowsTheThreeParts(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusRunning, dlProgress, dlChunk))

	// ② 状态文字。
	if !shows(tt, "下载中") {
		t.Errorf("没有状态文字，文字有 %q", tt.Texts())
	}
	// ③ 百分比：20 MB 的歌下了一半 = 50%。
	if !shows(tt, "50%") {
		t.Errorf("没有写出 50%%，文字有 %q", tt.Texts())
	}
	// ① 进度条。断的是「条真的跟着百分比走」而不是「条存在」：一个恒定满格或者
	// 恒定空的条也算画出来了，而用户看它就是错的。
	w50 := progressWidth(t, tt, "50%")
	if w50 <= 0 {
		t.Fatalf("那一格没有画出进度条")
	}
	_, tt90 := newRows(t, dlSnapshot("歌 乙", download.StatusRunning, dlProgress, dlProgress*9/10))
	if w90 := progressWidth(t, tt90, "90%"); w90 <= w50 {
		t.Errorf("90%% 的条（%v）和 50%% 的条（%v）一样宽：进度条没有跟着值走", w90, w50)
	}
}

// progressWidth 是画面上那一条进度条涂了主色的宽度。
//
// 🔴 **只量「状态」那一列里那一段**（横向窗口是从百分比那一格的右边缘往左
// `dlStatusW`），不是扫整张图。早先这里是整幅宽度扫，于是「侧栏里当前页那一项
// 用主色垫底」也会被量进来——而侧栏那一项的高度和这一页的状态列**没有任何
// 关系**，它落没落进这几行像素全看侧栏上排了几项：未登录时那三个入口不摆
// （`Route.NeedsLogin`），当前页那一项往上挪了三行，正好掉进
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
	// 主色从这一帧真正用的那份主题里取，别写死：默认设置是「跟随系统」，
	// 而 Tester 默认不深色，所以浅色那份的 Accent 正是条上涂的那个颜色。
	accent := BuildTheme(app.DefaultSettings(""), false).Accent

	// 百分比是那一格里**定宽靠右**的一段（`dlPctW`），它的右边缘就是状态列的
	// 右边缘；条是同一格里 `FillWidth` 的，所以右边缘也是那儿。
	right := int(txt.X + txt.W)
	left := right - dlStatusW

	// 条在百分比那一行**上面**：从文字的上边界往上扫，找主色。
	img := tt.Image()
	y := int(txt.Y) - 4
	for y > int(txt.Y)-24 {
		run, best := 0, 0
		for x := max(left, 0); x < right; x++ {
			c := img.RGBAAt(x, y)
			if c.R == accent.R && c.G == accent.G && c.B == accent.B && c.A > 0 {
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

// TestTheProgressColumnDoesNotClaimAFractionItDoesNotHave 断「没探到长度」的那
// 一格：**不画百分比**。
//
// `Percent` 是 -1 的时候画一个「-1%」是笑话，画 0% 是撒谎——那一刻根本不知道
// 这一首会有多大。
func TestTheProgressColumnDoesNotClaimAFractionItDoesNotHave(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusRunning, 0, 0))
	if !shows(tt, "下载中") {
		t.Fatalf("没有状态文字，文字有 %q", tt.Texts())
	}
	for _, bad := range []string{"-1%", "0%"} {
		if shows(tt, bad) {
			t.Errorf("还没探到长度却写出了 %q：那一刻不知道会有多大，写 0%% 是在撒谎", bad)
		}
	}
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

// TestTheAddedOnColumnIsLocalTime 断日期那一列按**本机时区**写。
//
// 上游是 `moment(s).format("YYYY-MM-DD")`，而 moment 不给时区就用本地（
// biu/src/common/utils/time.ts:20）。用 UTC 写的话这台机器上会是 11-15、比用户
// 自己点下载的那一天晚一天——而「我今天下的东西」正是用户看这一列的理由。
func TestTheAddedOnColumnIsLocalTime(t *testing.T) {
	want := time.UnixMilli(dlAddedAt).In(time.Local).Format("2006-01-02")
	if got := addedOn(dlAddedAt); got != want {
		t.Errorf("addedOn(%d) = %q，want %q（本机时区）", dlAddedAt, got, want)
	}
	if got := addedOn(0); got != "-" {
		t.Errorf("没有入队时刻时写的是 %q，want -", got)
	}
}

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
	}
	return s
}

// dlTitles 是给报错用的一行摘要。
func dlTitles(rows []download.Snapshot) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s(%s)", r.Title, r.Status))
	}
	return out
}
