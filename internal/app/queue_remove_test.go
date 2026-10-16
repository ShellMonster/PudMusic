package app_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 抽屉要动队列的那些动作：从队列里删掉一首、清空、点一行跳过去。
//
// 队列数据本身（判重标识、RemoveAt 之后 cur 指哪）在这两层里都有断言：抽屉那边
// 断的是「界面上看得见的那几行」，而界面看不见的 `cur` 走偏了，表现是播放条和
// 抽屉说的不是同一首歌——那正是这里要挡住的东西。

// TestIdentityFollowsBuisKeys 钉住判重标识的三种拼法，照 Biu
// `src/components/music-playlist-drawer/index.tsx` 那个 uniqBy 的键。
//
// 前缀不是多余的：本地项的键是完整路径，在线的键是 bvid / sid，三者不加分就有
// 可能撞上（撞上的表现是抽屉里两首不同的歌并成一行，看起来像少了一首）。
func TestIdentityFollowsBuisKeys(t *testing.T) {
	cases := []struct {
		name string
		item app.Item
		want string
	}{
		{"本地", app.LocalItem("/music/a.flac"), "local:/music/a.flac"},
		{"MV", app.Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD"}, "mv:BV1xx411c7mD"},
		{"音乐", app.Item{Source: "online", Type: "audio", Sid: "1293494"}, "audio:1293494"},
		// 只填了 Type 的本地项：LocalItem 两个都填，手写的常常只填 Type，
		// 少判一个它就掉到下面两支里去拿一个空标识了。
		{"只写了 Type 的本地项", app.Item{Type: "local", Path: "/music/a.flac"}, "local:/music/a.flac"},
		{"本地但没路径", app.Item{Source: "local"}, ""},
		{"MV 但没 bvid", app.Item{Source: "online", Type: "mv"}, ""},
		{"音乐但没 sid", app.Item{Source: "online", Type: "audio"}, ""},
		{"什么都没有", app.Item{}, ""},
	}
	for _, c := range cases {
		if got := c.item.Identity(); got != c.want {
			t.Errorf("%s：Identity() = %q，want %q", c.name, got, c.want)
		}
	}
}

// TestIdentityTellsSameSongApartFromDifferentOnes 是 Identity 的真正判据：
// 同一首的两个写法和两首不同的歌，两边都要分清。
func TestIdentityTellsSameSongApartFromDifferentOnes(t *testing.T) {
	same1 := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "第一集"}
	same2 := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "2", Title: "第二集"}
	if same1.Identity() != same2.Identity() {
		t.Errorf("同一个投稿的两分集身份不同：%q / %q", same1.Identity(), same2.Identity())
	}

	// 同一个文件路径摆两次是同一首；两个路径不是。
	a := app.LocalItem("/music/a.flac")
	if a.Identity() != app.LocalItem("/music/a.flac").Identity() {
		t.Error("同一个本地文件摆两次身份不同")
	}
	if a.Identity() == app.LocalItem("/music/b.flac").Identity() {
		t.Error("两个不同的本地文件身份撞上了")
	}

	// 空标识的两项身份相同，但**不该**被当成同一首——调用方要自己分开它们
	// （见下面 TestQueueDrawerKeepsUnidentifiableRows 的对应说明）。这里只钉住
	// 空串这个事实：Identity 不会凭空给它们编一个键。
	if (app.Item{}).Identity() != "" {
		t.Error("认不出的项给了它一个键")
	}
}

// TestQueueRemoveKeepsPointingAtTheSameSong 断的是 Queue.RemoveAt 之后 cur
// 指的还是**同一首正在放的歌**。
//
// 这一层错了、界面上的表现是：播放条停在 b，抽屉里高亮的却是 c（或者反过来），
// 而两个按钮按下去跳的都不是用户看到的那首。
func TestQueueRemoveKeepsPointingAtTheSameSong(t *testing.T) {
	cases := []struct {
		name      string
		cur       int // 先挪到第几首
		remove    int // 删掉第几首
		wantCur   string
		wantItems []string
	}{
		{"删掉前面一首，后面一首顶上来", 1, 0, "b", []string{"b", "c"}},
		{"删掉当前的那首，接着放下一首", 1, 1, "c", []string{"a", "c"}},
		{"删掉后面一首，当前项不动", 0, 2, "a", []string{"a", "b"}},
		{"删掉末尾的当前项，绕回第一首", 2, 2, "a", []string{"a", "b"}},
	}
	for _, c := range cases {
		q := app.NewQueue()
		q.Append(items("a", "b", "c")...)
		for q.CurrentIndex() != c.cur {
			q.Move(1)
		}
		before, _ := q.Current()

		removed, ok := q.RemoveAt(c.remove)
		if !ok {
			t.Fatalf("%s：RemoveAt(%d) 没删掉", c.name, c.remove)
		}
		if removed.Path == "" {
			t.Fatalf("%s：删掉的是空项", c.name)
		}
		if got, _ := q.Current(); got.Path != c.wantCur {
			t.Errorf("%s：当前项变成了 %q，want %q", c.name, got.Path, c.wantCur)
		}
		got := pathsOf(q.Items())
		if len(got) != len(c.wantItems) {
			t.Fatalf("%s：剩下 %v，want %v", c.name, got, c.wantItems)
		}
		for i := range got {
			if got[i] != c.wantItems[i] {
				t.Errorf("%s：剩下 %v，want %v", c.name, got, c.wantItems)
			}
		}
		// 除了「删掉的就是它自己」那一种，删别的都不该换歌。
		if c.remove != c.cur && got[0] != before.Path && c.cur != 0 {
			t.Errorf("%s：删一首别的歌把当前项也换了", c.name)
		}
	}
}

func TestQueueRemoveTheOnlyOneLeavesNoCurrent(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a")...)
	q.Move(1)

	if _, ok := q.RemoveAt(0); !ok {
		t.Fatal("唯一那一首没删掉")
	}
	if q.Len() != 0 {
		t.Fatalf("删完还剩 %d 首", q.Len())
	}
	if q.CurrentIndex() != -1 {
		t.Errorf("空队列的当前项是 %d，want -1", q.CurrentIndex())
	}
	if _, ok := q.Current(); ok {
		t.Error("空队列里 Current() 还有值")
	}
}

func TestQueueRemoveOutOfRangeChangesNothing(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a", "b")...)
	q.Move(1)

	for _, i := range []int{-1, 2, 99} {
		if _, ok := q.RemoveAt(i); ok {
			t.Errorf("RemoveAt(%d) 报告删成功了", i)
		}
	}
	if q.Len() != 2 {
		t.Errorf("越界删除之后队列剩 %d 首，want 2", q.Len())
	}
	if got, _ := q.Current(); got.Path != "a" {
		t.Errorf("越界删除把当前项改成了 %q", got.Path)
	}
}

// TestRemoveAtKeepsPlayingWhenItIsNotTheCurrentOne 删别的歌不该打断正在放的。
func TestRemoveAtKeepsPlayingWhenItIsNotTheCurrentOne(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)
	a.Enqueue(items(paths...)...)

	if err := a.RemoveAt(2); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, "队列变成两首", func() bool { return a.Queue().Len() == 2 })
	if got := a.Snapshot().Title; got != "a" {
		t.Errorf("删了后面一首，正在放的变成了 %q，want 还是 a", got)
	}
	if !a.Snapshot().HasTrack {
		t.Error("删一首不在放的歌把播放停了")
	}
}

// TestRemoveAtMovesToTheNextSongWhenItIsTheCurrentOne 删掉正在放的那首时
// 接着放下一首（Biu 的 del 也是这个意思），末尾则绕回第一首。
func TestRemoveAtMovesToTheNextSongWhenItIsTheCurrentOne(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)
	a.Enqueue(items(paths...)...)

	if err := a.RemoveAt(0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "接着放第二首", func() bool { return a.Snapshot().Title == "b" })
	if cur, ok := a.Queue().Current(); !ok || cur.Path != paths[1] {
		t.Errorf("队列的当前项是 %q，want %q（播放条和队列对不上）", cur.Path, paths[1])
	}
	if got := pathsOf(a.Queue().Items()); len(got) != 2 || got[0] != paths[1] {
		t.Errorf("队列剩下 %v，want 以 b 开头", got)
	}
}

// TestRemoveAtTheLastSongStops 是「删一首就没歌了」的那一支：必须停下来，
// 而且要把当前曲目清干净。留着歌继续响的话，播放条上还写着那首歌的名字，
// 队列里已经空了——界面说的和它做的不一致。
func TestRemoveAtTheLastSongStops(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 1)
	a.Enqueue(items(paths...)...)
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().HasTrack })

	if err := a.RemoveAt(0); err != nil {
		t.Fatal(err)
	}
	st := a.Snapshot()
	if st.HasTrack || st.Title != "" || st.Duration != 0 || st.Position != 0 {
		t.Errorf("删掉最后一首之后还在放：%+v", st)
	}
	if a.Queue().Len() != 0 {
		t.Errorf("队列还剩 %d 首", a.Queue().Len())
	}
}

// TestClearQueueEmptiesAndStops 同上，但走抽屉顶上那个「清空」。
func TestClearQueueEmptiesAndStops(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)
	a.Enqueue(items(paths...)...)
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().HasTrack })

	a.ClearQueue()

	if a.Queue().Len() != 0 {
		t.Errorf("清空之后队列还剩 %d 首", a.Queue().Len())
	}
	if st := a.Snapshot(); st.HasTrack || st.Title != "" {
		t.Errorf("清空之后还在放：%+v", st)
	}
}

// TestStopOnAnIdleAppDoesNothing 是「没在放的时候点清空」：不许凭空报错，
// 也不许凭空改状态。
func TestStopOnAnIdleAppDoesNothing(t *testing.T) {
	a := newApp(t)
	a.ClearQueue()
	if st := a.Snapshot(); st.HasTrack || st.Err != "" {
		t.Errorf("什么都没放的时候清空改了状态：%+v", st)
	}
}

// TestPlayAtJumpsWithoutSwappingTheQueue 是抽屉里点一行的那条路：跳过去，
// 但**不换队列**（换了的话用户队列里后面那些歌就没了）。
func TestPlayAtJumpsWithoutSwappingTheQueue(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)
	a.Enqueue(items(paths...)...)

	if err := a.PlayAt(2); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "跳到第三首", func() bool { return a.Snapshot().Title == "c" })

	got := pathsOf(a.Queue().Items())
	if len(got) != 3 {
		t.Fatalf("跳过去之后队列只剩 %d 首，want 还是 3 首", len(got))
	}
	for i := range got {
		if got[i] != paths[i] {
			t.Fatalf("跳过去之后队列变成了 %v，want %v", got, paths)
		}
	}
	if a.Queue().CurrentIndex() != 2 {
		t.Errorf("队列的当前项是 %d，want 2", a.Queue().CurrentIndex())
	}
}

// TestPlayAtOnTheSongAlreadyPlayingDoesNothing 照 Biu 的 playListItem：已经在放
// 这一首就什么都不做。重头播是播放条上「播放」那个键的活。
func TestPlayAtOnTheSongAlreadyPlayingDoesNothing(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 2)
	a.Enqueue(items(paths...)...)
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().HasTrack })

	if err := a.PlayAt(0); err != nil {
		t.Fatalf("点正在放的那一首：%v", err)
	}
	if got := a.Snapshot().Title; got != "a" {
		t.Errorf("点正在放的那一首之后放的是 %q，want 还是 a", got)
	}
}

// TestPlayAtKeepsTheQueueWhenTheSongWillNotPlay 放不进去就退回原来那一首：
// 用户点的是这一首，不能因为它打不开就把界面挪到别的歌上。
func TestPlayAtKeepsTheQueueWhenTheSongWillNotPlay(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)
	a.Enqueue(items(paths...)...)
	a.Queue().Append(app.LocalItem(filepath.Join(t.TempDir(), "没有这个文件.flac")))

	if err := a.PlayAt(3); err == nil {
		t.Fatal("点一首打不开的歌居然没报错")
	}
	if got := a.Snapshot().Title; got != "a" {
		t.Errorf("点打不开的那首之后放的是 %q，want 还是 a", got)
	}
	if a.Queue().CurrentIndex() != 0 {
		t.Errorf("队列的当前项是 %d，want 退回 0", a.Queue().CurrentIndex())
	}
	if a.Queue().Len() != 4 {
		t.Errorf("点一首打不开的歌把队列弄短了：剩 %d 首", a.Queue().Len())
	}
}

// TestPlayAtOutOfRangeSaysSo 越界要有一句话说，不是静默什么都不做。
func TestPlayAtOutOfRangeSaysSo(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 1)
	a.Enqueue(items(paths...)...)

	for _, i := range []int{-1, 1, 99} {
		if err := a.PlayAt(i); err == nil {
			t.Errorf("PlayAt(%d) 没报错", i)
		}
	}
	if got := a.Snapshot().Title; got != "a" {
		t.Errorf("越界之后放的是 %q，want 还是 a", got)
	}
}

// pathsOf 是一列项的路径，好和期望的字符串逐个比。
func pathsOf(items []app.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Path
	}
	return out
}
