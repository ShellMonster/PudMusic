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
