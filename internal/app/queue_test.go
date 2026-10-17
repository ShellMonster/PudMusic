package app_test

import (
	"os"
	"path/filepath"
	"slices"
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

// TestPlayFromSwapsTheQueueAndStartsAtTheFirst 是列表里「播放」那一项接的那条路
// （widgets/list 的 Actions.Play）：换掉整个队列，从第一首开始播。
//
// 跟 Enqueue 的差要钉住：Enqueue 是往队尾追加（点「加歌」），PlayFrom 是换掉
// 整个队列（点「播放」）。分不清的话，用户点了一首歌之后队列里多出来的还是
// 原来那一整张列表。
func TestPlayFromSwapsTheQueueAndStartsAtTheFirst(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)

	a.Enqueue(items(paths...)...)
	if err := a.PlayFrom(items(paths[1], paths[2])...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().Title == "b" })

	got := a.Queue().Items()
	if len(got) != 2 {
		t.Fatalf("队列里有 %d 首，want 2（a 被换掉了）", len(got))
	}
	if got[0].Path != paths[1] || got[1].Path != paths[2] {
		t.Errorf("队列里是 %q / %q，want b / c", got[0].Path, got[1].Path)
	}
}

// TestPlayFromWithNothingToPlayLeavesTheQueueAlone 验给空是「什么都不做」，
// 不是「清空队列」。
//
// 清空是 Queue.Reset() 的事，两者混在一起的表现最难查：调用方给不出东西时，
// 用户的队列悄悄没了，正在放的那首还在响，播放条和队列对不上，而调用方拿到的是
// nil（成功）。
func TestPlayFromWithNothingToPlayLeavesTheQueueAlone(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)
	a.Enqueue(items(paths...)...)
	a.Next()
	waitFor(t, 2*time.Second, "换到第二首", func() bool { return a.Snapshot().Title == "b" })

	if err := a.PlayFrom(); err != nil {
		t.Fatalf("PlayFrom()：%v", err)
	}

	got := a.Queue().Items()
	if len(got) != 3 {
		t.Fatalf("给空之后队列里有 %d 首，want 还是原来那 3 首", len(got))
	}
	if title := a.Snapshot().Title; title != "b" {
		t.Errorf("给空之后播放条变成了 %q，want 还是 b", title)
	}
}

// TestPlayFromKeepsTheQueueWhenTheFirstSongWillNotPlay 验打不开的歌不该把用户
// 原来的队列弄丢。
//
// 表现上就是「按了一首放不了的歌，队列和播放条还停在原来那一首」：队列被换掉
// 了、播放条却还是旧的，两者对不上（和 step 里同一个道理）。
func TestPlayFromKeepsTheQueueWhenTheFirstSongWillNotPlay(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 2)
	a.Enqueue(items(paths...)...)

	missing := app.Item{Source: "local", Path: filepath.Join(t.TempDir(), "没有这个文件.flac")}
	if err := a.PlayFrom(missing); err == nil {
		t.Fatal("放一首打不开的歌居然没报错")
	}

	got := a.Queue().Items()
	if len(got) != 2 || got[0].Path != paths[0] || got[1].Path != paths[1] {
		t.Fatalf("打不开之后队列变成了 %+v，want 原来那两首", got)
	}
	// 当前项也得换回去：播放条停在 a，队列的当前项就该是 a。
	if cur, ok := a.Queue().Current(); !ok || cur.Path != paths[0] {
		t.Errorf("当前项是 %q，want %q（播放条和队列对不上）", cur.Path, paths[0])
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

// ── 下一首播放 ───────────────────────────────────────────────────────────────

// TestInsertNextLandsRightAfterTheCurrentOne 插在**正在放的那首后面**。
//
// 这是「下一首播放」的字面意思。三条一起钉：
//
//  1. 位置对（下一首就是它，不是队尾）；
//  2. **正在放的那首不动**——而它是靠身份认的，不是靠下标（改 cur 的话用户
//     听着听着歌会自己跳一首）；
//  3. 队列里其余的顺序原样往后挪（不是换掉后面那一首）。
func TestInsertNextLandsRightAfterTheCurrentOne(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a", "b", "c")...)
	if _, ok := q.Move(1); !ok { // 落在 a
		t.Fatal("没落到第一首")
	}

	q.InsertNext(app.LocalItem("x"))
	want := []string{"a", "x", "b", "c"}
	if got := pathsOf(q.Items()); !slices.Equal(got, want) {
		t.Fatalf("队列是 %v，want %v", got, want)
	}
	// 正在放的仍然是 a。
	if cur, ok := q.Current(); !ok || cur.Path != "a" {
		t.Errorf("当前项是 %v，want a（插在它后面不该动它）", cur)
	}
	// 下一首就是刚插的那首。
	if next, ok := q.Move(1); !ok || next.Path != "x" {
		t.Errorf("Move(+1) = %v，want x", next)
	}
}

// TestInsertNextWhenNothingHasPlayedYet 还没开始放的时候插到最前面。
//
// 队列里的东西都还没轮到，而用户按的是「下一首播放」——队尾那个位置要等整张
// 列表放完才轮得到（症状是「按了，一小时之后才响」）。
func TestInsertNextWhenNothingHasPlayedYet(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a", "b")...)
	q.InsertNext(app.LocalItem("x"))

	if got, ok := q.Move(1); !ok || got.Path != "x" {
		t.Errorf("第一个放出来的是 %v，want x", got)
	}
}

// TestInsertNextIntoAnEmptyQueue 空的队列里它就是自己。
func TestInsertNextIntoAnEmptyQueue(t *testing.T) {
	q := app.NewQueue()
	q.InsertNext(app.LocalItem("x"))
	if q.Len() != 1 {
		t.Fatalf("队列里有 %d 首，want 1", q.Len())
	}
	if cur, ok := q.Current(); !ok || cur.Path != "x" {
		t.Errorf("当前项是 %v，want x", cur)
	}
}
