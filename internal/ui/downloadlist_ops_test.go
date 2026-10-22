package ui

// 这一组盯的是五种操作**按下去之后**发生的事：暂停 / 继续 / 重试 / 删除 /
// 清空，加删除与清空前那两道确认弹窗、「打开文件所在文件夹」，以及这一页自己
// 的那几条约定（行身份、崩了一格只废一格、页面 view 装主题）。
//
// 造数据的夹具（`newDownloads` / `newRows` / `dlFrame` / `dlSongName` …）和
// 那几条快照都在 downloadlist_test.go 顶上那一段。

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 五种操作
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFiveOperationsShowWhereBiuShowsThem 逐个状态走一遍，钉住每一颗按钮
// **该不该亮**。
//
// 条件逐条抄上游 `download-list/actions.tsx` 的那份 `actions`（`show` 那一
// 栏）。亮错的代价不是难看：摆一颗按了没反应的按钮，用户分不清「这一首不能暂
// 停」和「这个应用坏了」；少摆一颗则是一个做得到的动作没有入口。
//
// ⚠️ 这一条一行数据，因此每个状态里每个按钮名都**唯一**——`Tester.Click` 命中
// 的是第一个同名元素（同名的两颗会被它当成一颗）。
func TestTheFiveOperationsShowWhereBiuShowsThem(t *testing.T) {
	for _, c := range []struct {
		status download.Status
		want   []string
		absent []string
	}{
		{download.StatusRunning, []string{opPause, opDelete}, []string{opResume, opRetry, opOpen}},
		{download.StatusPaused, []string{opResume, opDelete}, []string{opPause, opRetry, opOpen}},
		{download.StatusError, []string{opRetry, opDelete}, []string{opPause, opResume, opOpen}},
		{download.StatusDone, []string{opOpen, opDelete}, []string{opPause, opResume, opRetry}},
		{download.StatusQueued, []string{opDelete}, []string{opPause, opResume, opRetry, opOpen}},
		{download.StatusMuxing, []string{opDelete}, []string{opPause, opResume, opRetry, opOpen}},
	} {
		_, tt := newRows(t, dlSnapshot("歌 甲", c.status, dlProgress, dlChunk))
		for _, want := range c.want {
			if !shows(tt, want) {
				t.Errorf("%q 那一行没有 %q 这颗按钮（上游在这一格摆着它），文字有 %q",
					c.status, want, tt.Texts())
			}
		}
		for _, absent := range c.absent {
			if shows(tt, absent) {
				t.Errorf("%q 那一行摆着 %q 这颗按钮：这一格上游是 show:false，"+
					"摆出来是一个按了没反应的东西", c.status, absent)
			}
		}
		// 清空是五种操作里唯一一个不属于某一行的，它在页头。
		if !shows(tt, "清空记录") {
			t.Errorf("%q 那一行看不到页头上的「清空记录」，文字有 %q", c.status, tt.Texts())
		}
	}
}

// TestClearingIsGreyWhenThereIsNothingToClear 空的时候「清空记录」是灰的：
// 一条记录都没有时按它什么都不会发生，而一颗亮着的按钮按下去没反应是最难解释
// 的那种。
func TestClearingIsGreyWhenThereIsNothingToClear(t *testing.T) {
	s, tt := newDownloads(t)

	if !shows(tt, "清空记录") {
		t.Fatalf("空着的时候页头上没有「清空记录」，文字有 %q", tt.Texts())
	}
	if err := tt.Click("清空记录"); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)
	if s.downloads.ask != nil {
		t.Error("一条记录都没有，按「清空记录」却弹出了确认框")
	}
	if len(s.app.DownloadSnapshots()) != 0 {
		t.Errorf("一条记录都没有，清空之后队列里却有 %d 条", len(s.app.DownloadSnapshots()))
	}
}

// TestPauseAndResumeActuallyMoveTheTask 断的是**状态真的动了**，不是按钮亮着。
//
// 判据用应用层的快照而不是画面上的字：按钮按下之后这一帧可能还没重画，
// 而「队列里那一项现在是暂停」这件事立刻就是真的。
//
// ⚠️ 暂停要有一个真的**在飞**的任务，而「在飞」只有入队才会产生（从队列文件读
// 回来的都已经被 `Restore` 改成了暂停，那正是它该做的事）。而入队之后那一项会
// 立刻去取流、几毫秒就失败——所以 `dlBlockingStream` 挂一个一直不回的取流服
// 务器，让它在测试收尾之前一直停在「正在下」。
func TestPauseAndResumeActuallyMoveTheTask(t *testing.T) {
	s, tt := newDownloads(t)
	dlBlockingStream(t, s.app)
	id := dlEnqueue(t, s)
	dlFrame(tt)

	if got, _ := s.app.DownloadSnapshot(id); got.Status != download.StatusRunning {
		t.Fatalf("造出来的那一项是 %q，want %q：这一条断的是「按暂停之后状态真的变了」",
			got.Status, download.StatusRunning)
	}
	if err := tt.Click(opPause); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)
	if got, _ := s.app.DownloadSnapshot(id); got.Status != download.StatusPaused {
		t.Fatalf("按了「%s」之后那一项是 %q，want %q", opPause, got.Status, download.StatusPaused)
	}

	if err := tt.Click(opResume); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)
	if got, _ := s.app.DownloadSnapshot(id); got.Status == download.StatusPaused {
		t.Fatalf("按了「%s」之后那一项还停着", opResume)
	}
	// 画面上那颗「继续」必须没了。
	//
	// ⚠️ 别断言「此刻亮着的是暂停」：接着下之后那一项先进「等待中」，后台那条线
	// 才把它捡起来变成「下载中」，这两帧之间有多久是调度说了算的。断的是
	// 「用户按的那一下生效了」——一颗摆设的「继续」会让它一直停着。
	if tt.HasText(opResume) {
		t.Errorf("按了「%s」之后那一格上还有「%s」，文字有 %q", opResume, opResume, tt.Texts())
	}
}

// TestRetryOnAFailedTaskQueuesItAgain 断「重试」真的把出错的那一项重新排上
// 队，而不只是把状态字段改了一下。
//
// 出错那一项是本组里唯一一条 `error` 的记录。上游的「重试」和「继续」是**两
// 个按钮、一个动作**（`actions.tsx` 里两个都调 `resumeMediaDownloadTask`），
// 这一边也是（`Manager.Resume`）——所以这里断的是同一个动作在两种状态下都能
// 用，而且错误原因被清掉了。
func TestRetryOnAFailedTaskQueuesItAgain(t *testing.T) {
	task := newTask("歌 甲", download.StatusError, dlProgress, 20)
	s, tt := newDownloads(t, task)

	if got := s.app.DownloadSnapshots(); len(got) != 1 || got[0].Status != download.StatusError {
		t.Fatalf("造出来的不是一条出错的记录：%+v", got)
	}
	if err := tt.Click(opRetry); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)
	got, _ := s.app.DownloadSnapshot(task.ID)

	// ⚠️ **断错误原因变了，不是「状态不再是 error」**：这一组没挂在线播放，重试
	// 会立刻又失败（取流报「在线播放还没接上」），于是在任何一帧上看状态都得
	// 看运气。错误原因换了一个才是稳的那一头：队列文件里播下的那句是
	// dlErrText，重试之后要么是空的（正在排队的路上），要么是新那一句——两种
	// 都说明引擎真的被叫醒了。而只有 `Resume` 根本没被调到时，它才会原封不动
	// 留在那儿。
	if got.Err == dlErrText {
		t.Fatalf("按了「%s」之后那一条还是原来那个错误（%q）：引擎根本没有被叫醒", opRetry, got.Err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 删除走确认弹窗
// ─────────────────────────────────────────────────────────────────────────────

// TestDeletingAnUnfinishedTaskAsksFirst 是这一票的验收项：删除走确认弹窗。
//
// 断的是**两半**：按下去之后记录还在（弹窗没点头之前不许动数据），而且弹窗
// 上写的是上游那两句——「确认删除吗？」和「当前任务未下载完成，确认删除后将
// 无法恢复」。只弹一个「确定吗？」的话用户不知道自己在删什么。
func TestDeletingAnUnfinishedTaskAsksFirst(t *testing.T) {
	s, tt := newDownloads(t, newTask("歌 甲", download.StatusPaused, dlProgress, 50))

	if err := tt.Click(opDelete); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)

	if !shows(tt, "确认删除吗？") {
		t.Fatalf("按了「%s」没有弹出确认框，文字有 %q", opDelete, tt.Texts())
	}
	if !tt.HasText("当前任务未下载完成，确认删除后将无法恢复") {
		t.Errorf("确认框上没有上游那句交代，文字有 %q", tt.Texts())
	}
	if len(s.app.DownloadSnapshots()) != 1 {
		t.Fatalf("还没点头，记录就已经被删了：队列里剩 %d 条", len(s.app.DownloadSnapshots()))
	}
}

// TestCancellingTheConfirmKeepsTheTask 断「取消」真的什么都不做。
func TestCancellingTheConfirmKeepsTheTask(t *testing.T) {
	s, tt := newDownloads(t, newTask("歌 甲", download.StatusPaused, dlProgress, 50))

	if err := tt.Click(opDelete); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)

	if shows(tt, "确认删除吗？") {
		t.Errorf("点了取消，弹窗还在，文字有 %q", tt.Texts())
	}
	if got := s.app.DownloadSnapshots(); len(got) != 1 {
		t.Fatalf("点了取消，记录却被删了：队列里剩 %d 条", len(got))
	}
}

// TestConfirmingTheDeleteTakesTheTaskAway 断「删除」那头真的删，而且删的是
// **被问的那一条**。
//
// 用两条记录不是为了凑数：`Tester.Click` 命中的是第一个同名元素，两行都摆着
// 「删除」时按第一下问的是第一行、确认之后走的也得是第一行。少了「第二行还在」
// 这半句，一个把 `d.ask` 记成 id 却按当前行删的实现照样绿。
func TestConfirmingTheDeleteTakesTheTaskAway(t *testing.T) {
	s, tt := newDownloads(t,
		newTask("歌 甲", download.StatusPaused, dlProgress, 50),
		newTask("歌 乙", download.StatusDone, dlChunk, 100),
	)

	if err := tt.Click(opDelete); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)
	dlClickConfirm(t, tt)

	if got := s.app.DownloadSnapshots(); len(got) != 1 || got[0].Title != "歌 乙" {
		t.Errorf("确认之后队列里剩的是 %v，want 只有「歌 乙」", dlTitles(got))
	}
	if shows(tt, "确认删除吗？") {
		t.Errorf("确认之后弹窗还开着，文字有 %q", tt.Texts())
	}
}

// TestDeletingAFinishedTaskStillAsks 删一条**已经下完**的也要问。
//
// 第一版照上游（`actions.tsx:137-146`）只在没下完的时候问，而这一票的验收项写
// 的是「删除走确认弹窗」——评审把这一格挑出来是对的：这一页上「删除」不是只有
// 一处的动作，用户在弹窗里看的是这一行删掉的是什么，而删除是不可撤销的。
//
// 这一条同时钉住**正文按状态分**：已经下完的那一条，删掉的只是记录，那首歌在
// 下载目录里留着（`Manager.Remove` 只删 `.part`）。正文照抄上游那句「未下载
// 完成…无法恢复」就是在骗用户，而它恰恰是最容易照抄的一句。
func TestDeletingAFinishedTaskStillAsks(t *testing.T) {
	s, tt := newDownloads(t,
		newTask("歌 甲", download.StatusDone, dlChunk, 100),
		newTask("歌 乙", download.StatusPaused, dlProgress, 50),
	)

	if err := tt.Click(opDelete); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)

	if !shows(tt, "确认删除吗？") {
		t.Fatalf("删一条已经下完的记录没有弹确认框，文字有 %q", tt.Texts())
	}
	if !tt.HasText("下载目录里的歌留着") {
		t.Errorf("正文没有说清歌还在，文字有 %q", tt.Texts())
	}
	if tt.HasText("确认删除后将无法恢复") {
		t.Error("一条已经下完的记录，正文却说「确认删除后将无法恢复」：那首歌还在盘上")
	}
	if got := s.app.DownloadSnapshots(); len(got) != 2 {
		t.Fatalf("还没点头，记录就已经被删了：队列里剩 %d 条", len(got))
	}

	dlClickConfirm(t, tt)
	if got := s.app.DownloadSnapshots(); len(got) != 1 || got[0].Title != "歌 乙" {
		t.Errorf("确认之后队列里剩的是 %v，want 只有「歌 乙」", dlTitles(got))
	}
}

// dlClickConfirm 点弹窗上那颗确认按钮。
//
// ⚠️ **按无障碍名字点，不按可见的字点**：弹窗上那颗的**可见字**和行里那颗是
// **同一个字**（上游 `confirmText: "删除"`，我们照抄），而 `Tester.Click` 命中
// 的是**第一个**同名元素——那是行里的那颗。
//
// 所以弹窗上那颗的无障碍名字是「确认删除」（`confirmButton` 里的 `Label`）：
// 那一层不是给测试凑的，是给读屏软件凑的——同一个画面里两颗都叫「删除」，
// 而这一颗是不可逆的那一下（理由见 `confirmmodal.go` 的 `confirmButton`）。
func dlClickConfirm(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("确认删除"); err != nil {
		t.Fatalf("弹窗上没有那颗确认键：%v，文字有 %q", err, tt.Texts())
	}
}

// TestClearingAsksFirstAndThenEmptiesTheQueue 清空也要问，而且问的时候要说清
// **会连着删掉什么**。
//
// 这一边的清空比上游狠：它会停掉所有正在下的、把每一首的半成品删掉（
// `Manager.Clear`）。所以正文里得有「还没下完的那几条也会被删掉」这一句。
func TestClearingAsksFirstAndThenEmptiesTheQueue(t *testing.T) {
	s, tt := newDownloads(t,
		newTask("歌 甲", download.StatusDone, dlChunk, 100),
		newTask("歌 乙", download.StatusPaused, dlProgress, 50),
	)

	if err := tt.Click("清空记录"); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)
	if !shows(tt, "确认清空吗？") {
		t.Fatalf("按了「清空记录」没有弹出确认框，文字有 %q", tt.Texts())
	}
	if !tt.HasText("其中 1 条还没下完") {
		t.Errorf("确认框没有说清半成品也会被删掉，文字有 %q", tt.Texts())
	}
	if got := s.app.DownloadSnapshots(); len(got) != 2 {
		t.Fatalf("还没点头，队列已经被清空了：剩 %d 条", len(got))
	}

	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)
	if got := s.app.DownloadSnapshots(); len(got) != 0 {
		t.Fatalf("确认之后队列里还剩 %d 条", len(got))
	}
	if !shows(tt, lookup(downloadListPath).Empty.Title) {
		t.Errorf("清空之后没有回到空状态，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 打开文件所在文件夹
// ─────────────────────────────────────────────────────────────────────────────

// TestOpeningTheDownloadFolderOpensWhereSongsLand 断页头上那个按钮打开的是
// **歌真正落下来的那个目录**。
//
// 断「目录」而不只是「调了那个函数」：设置里的下载路径为空时，落盘退回的是
// 系统下载目录（`app.Downloads.FallbackDir`），而页头上写的路径要是照设置的空
// 值画出来，用户看到的是「还没有下载目录」、按下去打开的却是别处——界面上没
// 有一个字是假的这件事，比按钮本身更要紧。
func TestOpeningTheDownloadFolderOpensWhereSongsLand(t *testing.T) {
	_, tt := newDownloads(t, newTask("歌 甲", download.StatusDone, dlChunk, 100))

	if !tt.HasText(dlDir) {
		t.Errorf("页头上没有写出下载目录 %q，文字有 %q", dlDir, tt.Texts())
	}
	if err := tt.Click("打开文件夹"); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)

	if len(dlOpened) != 1 || dlOpened[0] != dlDir {
		t.Errorf("按「打开文件夹」打开的是 %v，want [%s]", dlOpened, dlDir)
	}
}

// TestTheOpenButtonRevealsThatSongsOwnFile 断行内那个「打开」指的是**这一首
// 自己的文件**，不是那个目录、也不是别的一首。
//
// 断的是 `Dest` 用对了没有：点 A 行的「打开」去显示 B 行的文件，是 `Dest` 取
// 错了行的症状；而它得是 `Dest` 而不是 `Dest + ".part"`——那个半成品不是那首歌。
func TestTheOpenButtonRevealsThatSongsOwnFile(t *testing.T) {
	// 两行都下完了、都摆着「打开」。`Tester.Click` 命中的是第一行那颗。
	_, tt := newDownloads(t,
		newTask("歌 甲", download.StatusDone, dlChunk, 100),
		newTask("歌 乙", download.StatusDone, dlChunk, 100),
	)

	if err := tt.Click(opOpen); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)
	if len(dlRevealed) != 1 {
		t.Fatalf("按了「%s」打开了 %d 个文件：%v", opOpen, len(dlRevealed), dlRevealed)
	}
	want := filepath.Join(dlDir, "歌 甲.m4a")
	if dlRevealed[0] != want {
		t.Errorf("打开的是 %q，want %q", dlRevealed[0], want)
	}
	if strings.HasSuffix(dlRevealed[0], ".part") {
		t.Errorf("打开的是半成品 %q：那不是那首歌", dlRevealed[0])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 🔴 铁律四：行身份
// ─────────────────────────────────────────────────────────────────────────────

// TestDeletingARowAboveKeepsTheTopRowWhereItIs 是铁律四在这一页上的落点。
//
// 断的是**滚动锚点**：用户滚到第 20 首附近、在上面删掉一条，屏幕上**还是那几
// 首歌**。不装 `ListState.Key` 的话锚点是一个下标，删掉上面一行之后它就指着隔
// 壁那一首了——用户什么都没做，看着的东西自己换了。
func TestDeletingARowAboveKeepsTheTopRowWhereItIs(t *testing.T) {
	tasks := make([]queuedTask, 0, dlSongCount)
	for i := 1; i <= dlSongCount; i++ {
		tasks = append(tasks, newTask(dlSongName(i), download.StatusPaused, dlProgress, 50))
	}
	s, tt := newDownloads(t, tasks...)

	// 滚到下面去。
	tt.Scroll(600, 400, 0, 1200)
	dlFrame(tt)
	before := dlVisibleSongs(t, tt)
	if len(before) == 0 {
		t.Fatalf("滚过之后一首歌都没画出来，文字有 %q", tt.Texts())
	}
	top := before[0]

	// 删掉**上面**的一条（队列的第 1 条）。
	if err := s.app.RemoveDownload(tasks[0].ID); err != nil {
		t.Fatalf("删上面那一条：%v", err)
	}
	dlFrame(tt)

	after := dlVisibleSongs(t, tt)
	if len(after) == 0 {
		t.Fatalf("删完之后一首歌都没画出来，文字有 %q", tt.Texts())
	}
	if after[0] != top {
		t.Errorf("删掉上面一行之后，画面上顶上那首从 %q 变成了 %q：行身份跟着序号走了",
			top, after[0])
	}
}

// dlSongCount 是铁律四那一条用的歌数：够多才滚得动。
const dlSongCount = 40

// dlSongName 是第 i 首（1 起）的歌名。带着零补的序号是为了让每一行都认得出
// 自己，也方便按名字找画面上画着的是哪几行。
func dlSongName(i int) string { return fmt.Sprintf("测试的歌 %02d", i) }

// dlVisibleSongs 是画面上（视口之内）画着的那几首歌，从上往下排。
//
// ⚠️ **只算盒子非空的**：虚拟表格在视口上下各多建两行（mygo 的 overscan），
// 那些建在视口之外的行盒子是空的（宽 0），但仍然有 y——把它们算进来，按 y 找
// 「最上面那一首」会挑到视口上面那一行去（实测会挑到真正顶上那首的前两首）。
func dlVisibleSongs(t *testing.T, tt *ui.Tester) []string {
	t.Helper()
	type at struct {
		name string
		y    float32
	}
	var found []at
	for i := 1; i <= dlSongCount; i++ {
		r, ok := tt.Find(dlSongName(i))
		if !ok || r.W == 0 || r.H == 0 {
			continue
		}
		found = append(found, at{dlSongName(i), r.Y})
	}
	// 按 y 排（插入排序：几十行，够用，而且不想为这一处引 sort）。
	for i := 1; i < len(found); i++ {
		for j := i; j > 0 && found[j].y < found[j-1].y; j-- {
			found[j], found[j-1] = found[j-1], found[j]
		}
	}
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.name)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 接线与空状态
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDownloadRouteShowsItsOwnTable 验**接线**：从别的页面点侧栏上的
// 「下载记录」，画的是这一页自己的表格，不是 router.go 兜底的那张空状态页。
//
// ⚠️ 这一条不能省：这一页整个接在 `builtPage` 上，接错了的话画的是
// `pageContent` 的兜底分支——那一支照样画得出一屏东西，上面几条却全在往
// `s.downloads` 的字段上读，读的是没被画过的那一份，全绿。
func TestTheDownloadRouteShowsItsOwnTable(t *testing.T) {
	s, tt := newDownloads(t, newTask("歌 甲", download.StatusRunning, dlProgress, 50))

	// 从别的页面点侧栏上的「下载记录」过去——走的是用户真正走的那条路。
	s.navigate(homePath)
	dlFrame(tt)
	if err := tt.Click(lookup(downloadListPath).Title); err != nil {
		t.Fatalf("点不到侧栏上的「%s」：%v，文字有 %q",
			lookup(downloadListPath).Title, err, tt.Texts())
	}
	dlFrame(tt)

	if got := s.router.Path(); got != downloadListPath {
		t.Fatalf("点侧栏之后停在 %q，want %q", got, downloadListPath)
	}
	if !shows(tt, "歌 甲") {
		t.Errorf("这一页没有画出表格里的那一行，文字有 %q", tt.Texts())
	}
	if shows(tt, lookup(downloadListPath).Empty.Hint) {
		t.Errorf("有记录的时候却画了兜底的空状态页，文字有 %q", tt.Texts())
	}
}

// TestTheEmptyListStillSaysWhyItIsEmpty 钉住接了内容之后空状态**没被弄丢**。
//
// 这一页空着的时候画的是 `routes` 表里 `/download-list` 那一条（`noDownloads`），
// 两句话都在：标题说「这里为什么空」，提示说「怎么才能不空」。
func TestTheEmptyListStillSaysWhyItIsEmpty(t *testing.T) {
	want := lookup(downloadListPath).Empty
	_, tt := newDownloads(t)

	if !shows(tt, want.Title) {
		t.Errorf("空着的时候没有画出 %q，文字有 %q", want.Title, tt.Texts())
	}
	if !shows(tt, want.Hint) {
		t.Errorf("空着的时候没有画出「怎么才能不空」%q，文字有 %q", want.Hint, tt.Texts())
	}
	// 页头还在：不然用户连这一页是哪儿都认不出来。
	if !shows(tt, "下载记录") {
		t.Errorf("空着的时候连页标题都没了，文字有 %q", tt.Texts())
	}
	// ⚠️ 空着的时候**不该**有那五个列名：一张空表格画出来是五个列名加一片白，
	// 比画一句「这里为什么空」更像坏了。
	if shows(tt, "下载时间") {
		t.Errorf("一条记录都没有，却画出了表头那一列，文字有 %q", tt.Texts())
	}
}

// TestOneBrokenCellDoesNotBlankOutThePage 断「一格画崩了只废掉那一格」。
//
// 页级那道 Guard（`Shell.page`）兜的是整页：一格画崩了，用户该看到的是那一格
// 写「画不出来」，而不是整个下载记录页变成一张错误页。
func TestOneBrokenCellDoesNotBlankOutThePage(t *testing.T) {
	restore := logOutput(&bytes.Buffer{})
	t.Cleanup(restore)

	withCover := newTask("歌 甲", download.StatusRunning, dlProgress, 50)
	withCover.Cover = "http://example.invalid/a.jpg"
	s, tt := newDownloads(t, withCover,
		newTask("歌 乙", download.StatusDone, dlChunk, 100))

	// 把封面缓存摘掉。`imgcache.View` 拿到非空的封面地址就会去缓存上取状态，
	// 而那上面没有缓存（widgets/list 的 `New` 顶上那一段记着这一条）——于是
	// **有封面的那一格**崩，别的格子照画。
	//
	// ⚠️ 摘掉之后**要还回去**：收尾走的是 `Shell.Close`，它在**调用那一刻**才读
	// `s.images` 这个字段（不像早先的 `t.Cleanup(s.images.Close)`——方法值在登记
	// 那一刻就把接收者绑死了，换字段连累不到它）。所以这一句得排在基座挂的那次
	// 收尾**前面**跑：`t.Cleanup` 是后挂的先跑。
	keepImages := s.images
	t.Cleanup(func() { s.images = keepImages })
	s.images = nil
	dlFrame(tt)

	if s.crash != nil {
		t.Fatalf("一格画崩了，整页被换成了错误页：%v", s.crash)
	}
	if !tt.HasText("画不出来") {
		t.Errorf("画崩的那一格没有换成占位，文字有 %q", tt.Texts())
	}
	// 别的行照画：这就是「只废掉那一格」。
	if !shows(tt, "歌 乙") {
		t.Errorf("画崩了一格之后整张表空了，文字有 %q", tt.Texts())
	}
	if !shows(tt, "下载记录") {
		t.Errorf("画崩了一格之后连页头都没了，文字有 %q", tt.Texts())
	}
}

// TestThePageAppliesTheThemeItself 验 🔴 铁律二在**这一页的 view 上**也成立：
// 故意让系统外观和设置要的相反，只画这一页，画面上跟系统那份一样就说明这一页
// 忘了在自己开头装主题（MyGo 每帧开头会把主题重置成跟随系统那份）。
func TestThePageAppliesTheThemeItself(t *testing.T) {
	for _, c := range []struct {
		mode     app.ThemeMode
		sysDark  bool
		wantDark bool
	}{
		{app.ThemeModeLight, true, false},
		{app.ThemeModeDark, false, true},
	} {
		s := shellOn(t, c.mode)
		// 外层**没有** viewMain 替它装主题（那边每帧第一件事就是装）：这一页
		// 忘了在自己开头装的话，读到的就是 MyGo 每帧重置好的系统那份。
		var got bool
		tt := ui.NewTester(func(c *ui.Context) {
			s.viewDownloadList(c, nil)
			got = c.Theme().Dark
		}, 1200, 800)
		tt.SetDark(c.sysDark)
		tt.Frame()

		if got != c.wantDark {
			t.Errorf("设置 %v + 系统 %v：画面用的是深色 %v，want %v",
				c.mode, c.sysDark, got, c.wantDark)
		}
	}
}

// dlBlockingStream 挂上一个「取流一直挂着」的在线播放件。
//
// 用途只有一个：造一条**真的在下**的任务。下载引擎一入队就去取流，而取流失败
// 是一瞬间的事——那一刻这一项已经从 running 变成 error 了，于是「暂停」那颗按
// 钮根本不存在（它只在 running 的时候亮，见 `TestTheFiveOperationsShow…`）。
// 让取流一直不返回，那一项就停在 running 上，直到测试收尾。
//
// ⚠️ 收尾的顺序靠 `t.Cleanup` 的后进先出：先注册的 `srv.Close` 后跑，所以是
// 先放行（`close(release)`）、再关服务器、最后才 `a.Close()` 收下载队列。不这
// 么排的话 `httptest.Server.Close` 会一直等那个挂着的请求。
func dlBlockingStream(t *testing.T, a *app.App) {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		http.Error(w, "测试收尾了", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: http.DefaultClient})
}

// dlEnqueue 往队列里加一首，返回它的 id。
//
// 这一条是整个组里**唯一**用入队而不是队列文件或快照的：暂停和继续只对**正
// 在下的**那一项成立，而「正在下」只有入队才会产生（从文件读回来的都已经被
// `Restore` 改成了暂停）。取流会失败（没挂在线播放），于是它几毫秒之后就是出错
// 的——那也够用：出错那一项同样能按「重试」，而重试会把它重新排上队。
func dlEnqueue(t *testing.T, s *Shell) string {
	t.Helper()
	snap, err := s.app.Download(app.Item{Type: "mv", Bvid: "BV1dl", Cid: "c1", Title: "歌 甲"})
	if err != nil {
		t.Fatalf("入队：%v", err)
	}
	return snap.ID
}
