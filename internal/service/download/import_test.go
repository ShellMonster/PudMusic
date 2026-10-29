package download

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 从别的来源并进队列文件。

// 只带新任务：文件本来不存在时并进去，出来的就是那些任务。
func TestMergingIntoAnAbsentQueueWritesJustThose(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	in := []*Task{newTask("BV1a|1", "甲"), newTask("BV1b|2", "乙")}

	if err := MergeIntoQueue(path, in); err != nil {
		t.Fatal(err)
	}
	got := readTasks(t, path)
	if len(got) != 2 {
		t.Fatalf("队列里有 %d 条，期望 2 条", len(got))
	}
	if got[0].ID != "BV1a|1" || got[1].ID != "BV1b|2" {
		t.Fatalf("顺序不对：%v", got)
	}
}

// 两边都有的同一首：以**现有队列**为准。
//
// 那是用户在这个应用里刚点的状态，而外来的是几个月前的记录——拿外来那份覆盖它
// 是倒退。
func TestTheQueueAlreadyHereWinsOverTheIncomingOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	mine := newTask("BV1a|1", "我点的")
	mine.Status = StatusRunning
	if err := MergeIntoQueue(path, []*Task{mine}); err != nil {
		t.Fatal(err)
	}

	their := newTask("BV1a|1", "Biu 里的")
	their.Status = StatusDone
	their.Title = "Biu 里的"
	if err := MergeIntoQueue(path, []*Task{their}); err != nil {
		t.Fatal(err)
	}

	got := readTasks(t, path)
	if len(got) != 1 {
		t.Fatalf("队列里有 %d 条，期望 1 条（同一首不该出现两次）", len(got))
	}
	if got[0].Title != "我点的" || got[0].Status != StatusRunning {
		t.Fatalf("现有的那条被外来记录顶掉了：%+v", got[0])
	}
}

// 现有队列排在前面，追加的在后面。
func TestTheExistingOrderComesFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	if err := MergeIntoQueue(path, []*Task{newTask("BV1a|1", "先"), newTask("BV1a|2", "后")}); err != nil {
		t.Fatal(err)
	}
	if err := MergeIntoQueue(path, []*Task{newTask("BV1b|3", "新来的")}); err != nil {
		t.Fatal(err)
	}
	got := readTasks(t, path)
	if len(got) != 3 || got[0].ID != "BV1a|1" || got[2].ID != "BV1b|3" {
		t.Fatalf("顺序不对：%v", got)
	}
}

// 认不出的任务（没有 ID、没有保存路径、分块下标越界）不进队列。
//
// 带进来的是记录页上一个按不动的死行——它既不能暂停也不能继续，而用户在界面上
// 看不出为什么。
func TestUnusableIncomingTasksAreLeftOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	in := []*Task{
		{ID: "", Dest: "/x/a.m4a"},
		{ID: "BV1a|1", Dest: ""},
		// 分块下标 3 越界（总长 25 MiB = 三块，下标 0/1/2）
		{ID: "BV1a|2", Dest: "/x/b.m4a", Identity: Identity{Length: 26214400}, Chunks: []int{3}},
		newTask("BV1a|3", "好的那一条"),
	}
	if err := MergeIntoQueue(path, in); err != nil {
		t.Fatal(err)
	}
	got := readTasks(t, path)
	if len(got) != 1 || got[0].ID != "BV1a|3" {
		t.Fatalf("并进去的是 %v，期望只有「好的那一条」", got)
	}
}

// 一份我们读不懂的队列：不碰，并报错。
//
// 静默清空一份下到一半的队列是这一类工具最不该做的事（`store.go` 的文件头写着
// 同一句话）。
func TestAQueueWeCannotReadIsNeverRewritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	if err := os.WriteFile(path, []byte(`{ 这不是 JSON`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := MergeIntoQueue(path, []*Task{newTask("BV1a|1", "甲")})
	if err == nil {
		t.Fatal("往一份读不懂的队列里并东西没有被拦下")
	}
	body, _ := os.ReadFile(path)
	if string(body) != `{ 这不是 JSON` {
		t.Fatalf("那份文件被改写了：%s", body)
	}
}

// 版本对不上的一份队列：同样不碰。
func TestAQueueFromANewerVersionIsNeverRewritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	body := `{"version":99,"tasks":[]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MergeIntoQueue(path, []*Task{newTask("BV1a|1", "甲")}); err == nil {
		t.Fatal("往一份 v99 的队列里并东西没有被拦下")
	}
	got, _ := os.ReadFile(path)
	if string(got) != body {
		t.Fatalf("那份文件被改写了：%s", got)
	}
}

// 空输入是一个 no-op：一个字都不写。
func TestMergingNothingWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	if err := MergeIntoQueue(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("空输入却建了文件：%v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────

func newTask(id, title string) *Task {
	return &Task{ID: id, Title: title, Dest: "/x/" + title + ".m4a", Status: StatusPaused}
}

func readTasks(t *testing.T, path string) []*Task {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f queueFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("读队列 %s：%v", path, err)
	}
	return f.Tasks
}
