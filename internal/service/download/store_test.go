package download

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 这一组盯的是「退出时保存队列，重开后恢复」——
// `spec.md` 的第 76 条用户故事（断点续传）落在这一层。

func TestTheQueueRoundTripsThroughAFile(t *testing.T) {
	const size = 3 * chunkSize
	h := newHarness(t, size, 30280)
	id := TaskID(bvid, cid)
	dest := DestFor(h.dir, "半首", ".m4a", id)

	// 造一份「下完两块就退出了」的队列：分块记账 + 半成品。
	writePart(t, h.cdn, dest+".part", 0, size)
	h.saveQueue(t, map[string]*Task{
		id: {
			ID: id, Title: "半首", Dest: dest,
			Identity: Identity{Bvid: bvid, Cid: cid, Quality: 30280, Length: size},
			Chunks:   []int{0, 1},
			Status:   StatusRunning,
			AddedAt:  1700000000000,
		},
	})

	h2 := h.restart()
	got := h2.m.Snapshots()
	if len(got) != 1 {
		t.Fatalf("恢复出 %d 项", len(got))
	}
	g := got[0]
	if g.ID != id || g.Title != "半首" {
		t.Fatalf("恢复出来的是 %+v", g)
	}
	if g.Dest != dest {
		t.Fatalf("落盘路径变了：%q → %q", dest, g.Dest)
	}
	if g.Total != size {
		t.Fatalf("总长是 %d，期望 %d", g.Total, size)
	}
	// 两块 = 2 * 10 MB。这是「续传从哪儿接着」的依据，差一个字节
	// 就是从文件中间接下去。
	if want := int64(2 * chunkSize); g.Done != want {
		t.Fatalf("进度是 %d 字节，期望 %d", g.Done, want)
	}
	if g.AddedAt != 1700000000000 {
		t.Fatalf("入队时刻没恢复：%d", g.AddedAt)
	}
	if g.Percent != 66 {
		t.Fatalf("百分比是 %d，20000/30000 应该是 66", g.Percent)
	}
}

// 🔴 退出时正在下的，存下来必须是**暂停**。
//
// 存成 running 没有意义：重启之后那个在飞的 HTTP 请求早就不存在了，
// 而界面上会永远转圈——用户看到的是「下了一天还是 0%」。
func TestInFlightTasksAreSavedAsPaused(t *testing.T) {
	const size = 6 * chunkSize
	h := newHarness(t, size, 30280)
	// 每发只给 1 MB 就掐断：任务会失败，但仍然是「在队列里、有进度」。
	h.cdn.set(func(c *cdn) { c.cutAt = 1 << 20 })
	id := TaskID(bvid, cid)
	if _, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "歌", Dir: h.dir}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, h.m, id, StatusError)

	if err := h.m.Save(h.queueFile); err != nil {
		t.Fatal(err)
	}
	raw := readQueueFile(t, h.queueFile)
	for _, task := range raw.Tasks {
		if task.Status == StatusRunning || task.Status == StatusQueued {
			t.Fatalf("存下来的是 %q，恢复之后那个请求已经不在了", task.Status)
		}
	}
}

// 恢复出来的**不自动开下**。
//
// 一开窗就发几十个请求，表现为「一启动就卡」和一堆红字——而应用启动时
// 网络可能还没起来、机器可能根本没有网。自动续是界面层的决定。
//
// ⚠️ 断的是「一个请求都没发出去」，不是「某一项的状态是什么」——
// 「已完成」的那些重开之后本来就还是已完成（那是另一个用例断的），
// 拿它当这一条的样本会把两件事搅在一起。
func TestRestoredTasksDoNotStartOnTheirOwn(t *testing.T) {
	const size = 3 * chunkSize
	h := newHarness(t, size, 30280)
	id := TaskID(bvid, cid)
	dest := DestFor(h.dir, "半首", ".m4a", id)

	// 造一个「下了一半、退出了」的世界。
	writePart(t, h.cdn, partPath(dest), 0, size)
	h.saveQueue(t, map[string]*Task{
		id: {
			ID: id, Title: "半首", Dest: dest,
			Identity: Identity{Bvid: bvid, Cid: cid, Quality: 30280, Length: size},
			Chunks:   []int{0}, Status: StatusRunning, AddedAt: 1,
		},
	})

	h2 := h.restart()
	time.Sleep(300 * time.Millisecond) // 给后台那条线一点时间「要是它会开下」

	if got := h2.cdn.requests(); len(got) != 0 {
		t.Fatalf("恢复之后自己开下了，发了 %d 发请求：%v", len(got), got)
	}
	got, ok := h2.m.Get(id)
	if !ok {
		t.Fatal("重开后队列里没有这一项")
	}
	if got.Status != StatusPaused {
		t.Fatalf("恢复出来是 %q", got.Status)
	}
	if got.Done != chunkSize {
		t.Fatalf("恢复出来的进度是 %d 字节", got.Done)
	}
}

// 第一次运行时那个文件本来就不存在——那不是错误。
func TestAFreshInstallHasNoQueueFileAndThatIsFine(t *testing.T) {
	m, err := New(Options{
		Client:  http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) { return nil, ErrNoSource },
	}, filepath.Join(t.TempDir(), QueueFileName))
	if err != nil {
		t.Fatalf("第一次运行不该报错：%v", err)
	}
	t.Cleanup(func() { _ = m.Close("") })
	if got := m.Snapshots(); len(got) != 0 {
		t.Fatalf("队列里有 %d 项", len(got))
	}
}

// 版本不对要说清楚，而不是硬读出一个「下到一半」的假象。
func TestAQueueFromAnotherVersionIsRefusedLoudly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, QueueFileName)

	// 一份记账方式不一样的队列：分块下标指向的还是 10 MB 一块，
	// 而新版本换了分块大小的话，那些下标全指错了位置。硬读出来的
	// 结果是「续传」接着续，而续出来的是两段不同的字节。
	if err := os.WriteFile(path, mustJSON(t, queueFile{
		Version: storeVersion + 1,
		Tasks: []*Task{{
			ID: "x|y|1", Title: "歌", Dest: filepath.Join(dir, "歌.m4a"),
			Identity: Identity{Bvid: "x", Cid: "y", Quality: 1, Length: 100 << 20},
			Chunks:   []int{0, 1, 2}, Status: StatusPaused,
		}},
	}), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := New(Options{
		Client:  http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) { return nil, ErrNoSource },
	}, path)
	if err == nil {
		t.Fatal("版本不对却读成功了")
	}
	if !strings.Contains(err.Error(), "认") {
		t.Fatalf("错不像人话：%v", err)
	}
}

// 读不懂的队列也要说，而不是当空的。
func TestACorruptQueueFileIsReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, QueueFileName)
	if err := os.WriteFile(path, []byte("{这不是 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{
		Client:  http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) { return nil, ErrNoSource },
	}, path)
	if err == nil {
		t.Fatal("读不懂的队列却读成功了")
	}
}

// 记账坏了的记录要丢掉，而不是当成能续。
//
// 「分块下标越界」是一份自己跟自己矛盾的记录：总长说 10 MB（只有一块），
// 账上却有第 5 块。留着它的话「已完成」会被算成真，用户点开一个缺了一大段
// 的文件，而下载记录说它下完了。
func TestARecordWithImpossibleChunkBookkeepingIsDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, QueueFileName)
	if err := os.WriteFile(path, mustJSON(t, queueFile{
		Version: storeVersion,
		Tasks: []*Task{
			{
				// 总长 10 MB = 一块，账上却有第 5 块：不可能。
				ID: "bad", Dest: filepath.Join(dir, "坏.m4a"),
				Identity: Identity{Bvid: "b", Cid: "c", Quality: 1, Length: chunkSize},
				Chunks:   []int{0, 5}, Status: StatusPaused,
			},
			{
				// 认不出来的身份（没 ID、没落盘路径）也丢掉。
				Title: "没身份", Status: StatusPaused,
			},
			{
				// 这一条是好的，要留下。
				ID: "good", Title: "好", Dest: filepath.Join(dir, "好.m4a"),
				Identity: Identity{Bvid: "b", Cid: "c", Quality: 1, Length: chunkSize},
				Chunks:   []int{0}, Status: StatusPaused,
			},
		},
	}), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := New(Options{
		Client:  http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) { return nil, ErrNoSource },
	}, path)
	if err != nil {
		t.Fatalf("读队列：%v", err)
	}
	t.Cleanup(func() { _ = m.Close("") })

	got := m.Snapshots()
	if len(got) != 1 || got[0].ID != "good" {
		t.Fatalf("恢复出来的是 %+v", got)
	}
}

// 原子写：写完不留临时文件。
func TestSavingLeavesNoTemporaryFiles(t *testing.T) {
	h := newHarness(t, 1024, 30280)
	if _, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "歌", Dir: h.dir}); err != nil {
		t.Fatal(err)
	}
	if err := h.m.Save(h.queueFile); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(h.queueFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".download-") {
			t.Fatalf("留下了临时文件 %s", e.Name())
		}
	}
	if _, err := os.Stat(h.queueFile); err != nil {
		t.Fatalf("队列文件不在：%v", err)
	}
}

// 关掉之后再存是安全的（退出路径上会被调两次：OnWillQuit 和 App.Close）。
func TestClosingTwiceIsSafe(t *testing.T) {
	h := newHarness(t, 1024, 30280)
	if err := h.m.Close(h.queueFile); err != nil {
		t.Fatalf("第一次关：%v", err)
	}
	if err := h.m.Close(h.queueFile); err != nil {
		t.Fatalf("第二次关：%v", err)
	}
}

func readQueueFile(t *testing.T, path string) queueFile {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f queueFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestAMuxingTaskNeverSurvivesARestartAsMuxing 钉住「交织中」不会带着「交织中」
// 重开。
//
// 交织是视频那一路（PLAN 8.9 / #32），这一版还没有交织器，所以**没有任何
// 正常路径会置它**——这一条是把那一项直接按成交织中，然后走一遍真实的
// 存盘 → 读回。留这个状态位是为了记录页能画它，而它一旦**跨过一次重启**还叫
// 「交织中」，表现是：界面上永远有一条进度条在走、永远走不完、也永远等不到
// 「下载完成」——用户能做的只有退出应用。
//
// 断的是**存下去的那一份**（不是「有没有报错」）：磁盘上写着的必须是暂停。
func TestAMuxingTaskNeverSurvivesARestartAsMuxing(t *testing.T) {
	const size = chunkSize
	h := newHarness(t, size, 30280)
	h.queueFile = filepath.Join(h.dir, QueueFileName)
	id := TaskID(bvid, cid)
	dest := DestFor(h.dir, "交织中的歌", ".mp4", id)

	// 字节全下完了（分块齐了），交织还没写完 —— 这正是交织中的样子。
	h.m.mu.Lock()
	h.m.tasks[id] = &Task{
		ID: id, Title: "交织中的歌", Dest: dest, Ext: ".mp4",
		Identity: Identity{Bvid: bvid, Cid: cid, Quality: 30280, Length: size},
		Chunks:   []int{0},
		Status:   StatusMuxing,
		AddedAt:  1700000000000,
	}
	h.m.order = append(h.m.order, id)
	h.m.mu.Unlock()

	// ① 存盘那一层：写下去的就该是暂停。
	if err := h.m.Save(h.queueFile); err != nil {
		t.Fatalf("存队列：%v", err)
	}
	var onDisk queueFile
	b, err := os.ReadFile(h.queueFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &onDisk); err != nil {
		t.Fatal(err)
	}
	if len(onDisk.Tasks) != 1 {
		t.Fatalf("队列文件里有 %d 项，want 1", len(onDisk.Tasks))
	}
	if got := onDisk.Tasks[0].Status; got != StatusPaused {
		t.Fatalf("交织中的那一项存成了 %q，want %q：重开之后界面上会永远转圈", got, StatusPaused)
	}

	// ② 读回那一层：万一有一份别处写的队列文件带着交织中，它也该被读成暂停。
	raw := string(b)
	raw = strings.Replace(raw, string(StatusPaused), string(StatusMuxing), 1)
	if err := os.WriteFile(h.queueFile, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	h2 := h.restart()
	got := h2.m.Snapshots()
	if len(got) != 1 {
		t.Fatalf("恢复出 %d 项", len(got))
	}
	if got[0].Status != StatusPaused {
		t.Fatalf("带着交织中的队列文件重开，恢复出来是 %q，want %q", got[0].Status, StatusPaused)
	}
	// 而交织过的分块记账要留着：接着下去不能把那几兆重下一遍。
	if got[0].Done != size {
		t.Fatalf("恢复出来只记着下了 %d 字节，want %d：交织过的字节还在盘上，账上不能没有",
			got[0].Done, size)
	}
}

// TestTheReservedMuxingStatusIsInFlight 钉住交织中算「在飞」的那一半语义：
// 它不能暂停、也不能继续。
//
// 这两条不是此刻的偏好，是 #32 接上来之前就得定下来的：交织器拿着的那几个
// 字节停不下来（Biu 那边有 `mergePaused`，那要交织器自己支持存点），而
// 「继续」对它没有可继续的东西——字节全下完了。真出错了那一路会落在
// `StatusError` 上，由重试从头走。
func TestTheReservedMuxingStatusIsInFlight(t *testing.T) {
	if StatusMuxing.CanPause() {
		t.Error("交织中能暂停：这一版停不下来交织，摆一颗按了会停不下来的按钮是骗用户")
	}
	if StatusMuxing.CanResume() {
		t.Error("交织中能继续：字节已经全下完了，没有可继续的东西")
	}
	if !StatusMuxing.Running() {
		t.Error("交织中不算在飞：退出时它会被存成「交织中」，重开之后永远转圈（见上面那一条）")
	}
	// 下完了 / 出错了 / 暂停了都不在飞。
	for _, s := range []Status{StatusDone, StatusError, StatusPaused} {
		if s.Running() {
			t.Errorf("%q 被算成在飞", s)
		}
	}
}
