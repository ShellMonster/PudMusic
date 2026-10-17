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

// MigrateFile：v0 → v1 的队列文件能被升到当前版本并写回。
func TestMigratingAQueueFileLiftsItToTheCurrentVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	// 没有 `version` 键的那一份（v0）。
	body := `{"tasks":[{"id":"BV1a|1","title":"甲","dest":"/x/a.m4a","status":"done"}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateFile(path); err != nil {
		t.Fatal(err)
	}
	var f queueFile
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Version != storeVersion {
		t.Fatalf("版本停在 %d，不是 %d", f.Version, storeVersion)
	}
	if len(f.Tasks) != 1 || f.Tasks[0].Title != "甲" {
		t.Fatalf("迁完之后任务没了：%+v", f.Tasks)
	}
}

// 已经是当前版本：一个字都不动。
//
// 每次启动都重写一遍的话，mtime 全在变，而有些备份工具按 mtime 判断「改过了」。
func TestMigratingAnUpToDateQueueWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	body := `{"version":1,"tasks":[]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateFile(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("已经是最新的一份却被重写了：%v → %v", before.ModTime(), after.ModTime())
	}
}

// 一份未来的队列：不碰，并报错。
func TestMigratingAQueueFromANewerVersionTouchesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	body := `{"version":99,"tasks":[]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateFile(path); err == nil {
		t.Fatal("一份 v99 的队列没有被拦下")
	}
	got, _ := os.ReadFile(path)
	if string(got) != body {
		t.Fatalf("那份文件被改写了：%s", got)
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

// 并进一份 v0 的队列时，那一份**先走过迁移链**，和 `Restore` / `MigrateFile` 同路。
//
// 这一条钉的是「三条路走同一条链」。今天 `queueMigrations[0]` 是空函数，所以不跑也
// 看不出差别——那正是 `migrate.go` 注释里写的那个失败模式反过来：**一份 v0 的
// 内容被盖上 v1 的版本号，然后永远不再被迁**。等有人往那张表里加了真的 v0 迁移，
// 这一条就是唯一绕过它的路。
//
// 判据不是「跑没跑链」（那得往包里塞一条假迁移才看得见），而是**写出去的那一份
// 自称的版本**：走了链的话它停在 `storeVersion`，不走的话这里会停在一个更低的
// 编号上——而那份文件从此对后面所有人都是「已经是最新版了」。
func TestMergingIntoAnOlderQueueDoesNotSkipItsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), QueueFileName)
	// 一份 v0：没有 version 键。
	if err := os.WriteFile(path, []byte(`{"tasks":[{"id":"BV1|1","title":"旧的","dest":"/x/a.m4a","status":"paused"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MergeIntoQueue(path, []*Task{newTask("BV2|2", "新的")}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f queueFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Version != storeVersion {
		t.Errorf("写出去的那一份自称第 %d 版，want %d（读出来的那份没走迁移链）", f.Version, storeVersion)
	}
	if len(f.Tasks) != 2 {
		t.Errorf("队列里有 %d 条，want 2（旧的一条 + 并进来的）", len(f.Tasks))
	}
}
