package download

import "testing"

// SnapshotList 在队列 revision 不变时复用只读切片，状态变化后换成新的一份。
func TestSnapshotListReusesRowsUntilRevisionChanges(t *testing.T) {
	const id = "BV1|c1"
	m := &Manager{
		tasks: map[string]*Task{
			id: {ID: id, Title: "歌", Status: StatusQueued},
		},
		order:    []string{id},
		revision: 1,
	}

	first := m.SnapshotList()
	second := m.SnapshotList()
	if first.Revision != second.Revision {
		t.Fatalf("未变化的 revision 变了：%d → %d", first.Revision, second.Revision)
	}
	if len(first.Rows) != 1 || &first.Rows[0] != &second.Rows[0] {
		t.Fatal("未变化的队列没有复用快照切片")
	}

	m.mu.Lock()
	m.tasks[id].Status = StatusPaused
	m.touchLocked()
	m.mu.Unlock()

	third := m.SnapshotList()
	if third.Revision == second.Revision {
		t.Fatalf("状态变化后 revision 仍是 %d", third.Revision)
	}
	if &third.Rows[0] == &second.Rows[0] {
		t.Fatal("状态变化后仍复用了旧快照切片")
	}
	if got := third.Rows[0].Status; got != StatusPaused {
		t.Fatalf("新快照没有反映状态变化：%q", got)
	}
}
