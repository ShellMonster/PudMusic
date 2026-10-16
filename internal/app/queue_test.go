package app_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 队列的判据分两层：Queue 本身是纯数据（不碰播放器），App 上的那几个方法要走
// 真的播放器。素材还是那个 FLAC——它够短，几首之间来回切也快。

func items(paths ...string) []app.Item {
	out := make([]app.Item, len(paths))
	for i, p := range paths {
		out[i] = app.LocalItem(p)
	}
	return out
}

// copies 把素材复制成 n 个不同路径，这样队列里几首的歌名各不相同，
// 断言「换到下一首了」才不是靠猜。
func copies(t *testing.T, n int) []string {
	t.Helper()
	src := flac(t)
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := make([]string, n)
	for i := range n {
		out[i] = filepath.Join(dir, string(rune('a'+i))+".flac")
		if err := os.WriteFile(out[i], b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestQueueMovesBothWaysAndWrapsAround(t *testing.T) {
	q := app.NewQueue()
	if _, ok := q.Move(1); ok {
		t.Fatal("空队列里挪了一格")
	}

	q.Append(items("a", "b", "c")...)
	if got := q.Len(); got != 3 {
		t.Fatalf("Len() = %d，want 3", got)
	}
	if _, ok := q.Current(); ok {
		t.Error("还没开始的时候 Current() 就有值了")
	}

	// 还没开始：正向落在第一首，反向落在最后一首。
	if got, _ := q.Move(1); got.Path != "a" {
		t.Errorf("第一次 Move(+1) = %q，want a", got.Path)
	}
	if got, _ := q.Move(-1); got.Path != "c" {
		t.Errorf("Move(-1) = %q，want c（绕回到最后一首）", got.Path)
	}
	if got, _ := q.Move(1); got.Path != "a" {
		t.Errorf("Move(+1) = %q，want a（绕回到第一首）", got.Path)
	}
	if got, _ := q.Move(1); got.Path != "b" {
		t.Errorf("Move(+1) = %q，want b", got.Path)
	}
	if cur, ok := q.Current(); !ok || cur.Path != "b" {
		t.Errorf("Current() = (%q, %v)，want (b, true)", cur.Path, ok)
	}
}

func TestQueueAppendDoesNotMoveTheCurrentItem(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a", "b")...)
	q.Move(1) // 现在在 a
	q.Append(items("c")...)

	if q.Len() != 3 {
		t.Fatalf("Len() = %d，want 3", q.Len())
	}
	if cur, _ := q.Current(); cur.Path != "a" {
		t.Errorf("追加之后当前项变成了 %q，追加不该动当前项", cur.Path)
	}
	if got, _ := q.Move(1); got.Path != "b" {
		t.Errorf("追加之后下一首是 %q，want b", got.Path)
	}
}

func TestQueueItemsIsACopy(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a", "b")...)
	snapshot := q.Items()
	snapshot[0].Path = "改过了"
	if cur, _ := q.Current(); cur.Path == "改过了" {
		t.Error("改 Items() 返回的那份，把队列里的东西也改了")
	}
}

func TestEnqueueFirstStartsPlayingAndRestJustWaits(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)

	if err := a.Enqueue(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().HasTrack })
	if got := a.Snapshot().Title; got != "a" {
		t.Fatalf("放的是 %q，want a", got)
	}

	// 再加两首只是排着，当前项不动，也不该出声。
	a.Enqueue(items(paths[1], paths[2])...)
	if got := a.Snapshot().Title; got != "a" {
		t.Errorf("加歌之后当前项变成了 %q，want a", got)
	}
	if got := a.Queue().Len(); got != 3 {
		t.Errorf("队列长度 = %d，want 3", got)
	}
}

func TestNextAndPrevWalkTheQueue(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)
	a.Enqueue(items(paths...)...)

	for i, want := range []string{"b", "c", "a", "b"} {
		a.Next()
		waitFor(t, 2*time.Second, "下一首", func() bool { return a.Snapshot().Title == want })
		if got := a.Snapshot().Title; got != want {
			t.Fatalf("第 %d 次下一首放的是 %q，want %q", i+1, got, want)
		}
	}
	for i, want := range []string{"a", "c", "b"} {
		a.Prev()
		waitFor(t, 2*time.Second, "上一首", func() bool { return a.Snapshot().Title == want })
		if got := a.Snapshot().Title; got != want {
			t.Fatalf("第 %d 次上一首放的是 %q，want %q", i+1, got, want)
		}
	}
}

// TestSteppingRollsBackWhenTheTrackCannotOpen 钉住「放不进去就别乱换」：
// 队列的当前项必须和正在播的那首对得上，否则连按两次下一首就会跳过一整首。
func TestSteppingRollsBackWhenTheTrackCannotOpen(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 2)
	a.Enqueue(items(paths[0], "根本没有这个文件.flac")...)

	a.Next()
	if got := a.Snapshot().Title; got == "" {
		t.Fatal("打不开的那一首把界面清空了")
	}
	if cur, ok := a.Queue().Current(); !ok || cur.Path == "根本没有这个文件.flac" {
		t.Errorf("放不进去之后当前项停在了 %q，应该退回来", cur.Path)
	}
	if a.Snapshot().Err == "" {
		t.Error("播放条上应该有一条说明为什么放不了")
	}
}

// TestSteppingAcrossEveryTrackIsStable 是个空转的压力测试：来回切几轮，
// 队列的下标不能和正在播的那首走散。
func TestSteppingAcrossEveryTrackIsStable(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)
	a.Enqueue(items(paths...)...)

	for range 12 {
		a.Next()
		a.Prev()
		a.Next()
	}
	cur, ok := a.Queue().Current()
	if !ok {
		t.Fatal("走了一轮之后队列里没有当前项了")
	}
	// 下一首往前挪一格应该正好等于正在播的那首。
	next, _ := a.Queue().Move(0)
	if cur.Path != next.Path {
		t.Errorf("队列当前项是 %q，和 Move(0) 给的 %q 不是同一首", cur.Path, next.Path)
	}
}

func TestStepButtonsAreOffUntilThereIsAQueue(t *testing.T) {
	a := newApp(t)
	if a.CanNext() || a.CanPrev() {
		t.Error("什么都没有的时候上一首/下一首是亮的")
	}

	a.Enqueue(items(copies(t, 1)...)...)
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().HasTrack })
	if !a.CanNext() || !a.CanPrev() {
		t.Error("放上歌之后上一首/下一首还是灰的")
	}

	// 直接调 Open 不进队列（界面上两个入口现在都走 Enqueue，只有 app.Open
	// 这一层 API 才是裸的），按钮应该灭回去。
	a2 := newApp(t)
	if err := a2.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	if a2.CanNext() || a2.CanPrev() {
		t.Error("没进队列的歌也让上一首/下一首亮了")
	}
}

func TestSteppingAnEmptyQueueDoesNothing(t *testing.T) {
	a := newApp(t)
	a.Next()
	a.Prev()
	if st := a.Snapshot(); st.HasTrack || st.Err != "" {
		t.Errorf("空队列上按上一首/下一首改了状态：%+v", st)
	}
}
