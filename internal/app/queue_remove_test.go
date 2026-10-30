package app_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
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
		// ⚠️ **没有 cid 的 MV 只有 `mv:<bvid>`**（票 #683）：搜索结果只给 bvid，
		// 那时还不知道是哪一集，按投稿判。有 cid 的才带上 `|<cid>`（见下一行）。
		{"MV", app.Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD"}, "mv:BV1xx411c7mD"},
		{"带 cid 的 MV", app.Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD", Cid: "777"}, "mv:BV1xx411c7mD|777"},
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
//
// 🔴 **同一个投稿的两分集是两首**（票 #683）：用户报的「一个视频下多个子集，
// 点里面的歌单播放不了」根子就在这一句上——判重不认 cid 的话，抽屉只画一行、
// 点第二集放的是第一集、`PlayAt` 连播放器都不换。完整的判据（含「一个有一个
// 没有为什么判不同」）在 `multipart_identity_test.go`。
func TestIdentityTellsSameSongApartFromDifferentOnes(t *testing.T) {
	one := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "第一集"}
	two := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "2", Title: "第二集"}
	if one.Identity() == two.Identity() {
		t.Errorf("同一个投稿的两分集身份相同：%q（两集被当成同一首）", one.Identity())
	}
	// 同一集的两次拷贝（同一个 bvid + 同一个 cid）仍然是同一首——标题不参与身份。
	sameCopy := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "另一个标题"}
	if one.Identity() != sameCopy.Identity() {
		t.Errorf("同一集的两次拷贝身份不同：%q / %q", one.Identity(), sameCopy.Identity())
	}
	if !app.SameTrack(one, sameCopy) {
		t.Error("同一集的两次拷贝没有被判成同一首")
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

// TestRemoveAtStopsWhenTheNextSongWillNotPlay 是删掉正在放的那首、而下一首
// 打不开的那一支。
//
// 这里只有一个「不留在原地继续响」的选择：那首已经从队列里删掉了，让它继续响就是
// 队列和播放器说的不是同一首歌——播放条上还是它的名字，抽屉却把下一首高亮成
// 正在放，正是 `App.ClearTrack` 要避免的那种不一致。
//
// 断的是**停下来**（HasTrack 变 false、状态清干净）加上**话别说反**：返回的
// 错误说的是「已经删掉了，但接不上下一首」，不是「删不掉」——删是真的删掉了。
func TestRemoveAtStopsWhenTheNextSongWillNotPlay(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 2)
	a.Enqueue(items(paths[0])...)
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().HasTrack })

	// 队列 = [正在放的那首, 一个打不开的文件]
	a.Queue().Append(app.Item{Source: "local", Path: filepath.Join(t.TempDir(), "缺了这一首.flac")})

	err := a.RemoveAt(0)
	if err == nil {
		t.Fatal("下一首打不开却一声不吭")
	}
	if !strings.Contains(err.Error(), "已经删掉了") {
		t.Errorf("话没说清删是删掉了：%v", err)
	}
	if strings.Contains(err.Error(), "删不掉") {
		t.Errorf("说的是「删不掉」，可它确实删掉了：%v", err)
	}

	// 队列里确实没有了。
	got := pathsOf(a.Queue().Items())
	if len(got) != 1 || !strings.HasSuffix(got[0], "缺了这一首.flac") {
		t.Fatalf("删完之后队列是 %v，want 只剩那个打不开的文件", got)
	}
	// **停下来了**，而且状态清干净：没有一首正在放的歌，也没有半句红字。
	st := a.Snapshot()
	if st.HasTrack || st.Title != "" || st.Duration != 0 || st.Position != 0 {
		t.Errorf("下一首打不开之后还在放：%+v", st)
	}
	if st.Err != "" {
		t.Errorf("停下来之后播放条上还挂着 %q", st.Err)
	}
	// 抽屉里把下一首高亮成正在放的那一处，也跟着没有当前项了。
	if a.Queue().CurrentIndex() != 0 {
		t.Errorf("当前项是 %d，want 0（那一项还在队列里）", a.Queue().CurrentIndex())
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

// TestClearQueueOnAnIdleAppDoesNothing 是「没在放的时候点清空」：不许凭空报错，
// 也不许凭空改状态。
func TestClearQueueOnAnIdleAppDoesNothing(t *testing.T) {
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

// TestRemoveSongFromListRemovesEveryCopy 钉「从列表删除」删的是**全部**同身份项。
//
// 抽屉那一行是按判重标识画的：队列里 [甲,甲,乙] 只画两行（甲、乙）。只删一份的
// 话，用户按了「从列表删除」之后那一行还在（顶上的计数也不变），看起来就是按了
// 没反应。Biu 的 `play-list.ts` 里 `del` 用 `isSame` 删的就是全部。
func TestRemoveSongFromListRemovesEveryCopy(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 2)
	dup := app.LocalItem(paths[0])
	a.Enqueue(app.LocalItem(paths[0]))
	a.Enqueue(dup, app.LocalItem(paths[1])) // 队列：甲,甲,乙
	if n := a.Queue().Len(); n != 3 {
		t.Fatalf("测试自己没摆好：队列 %d 首", n)
	}

	n, err := a.RemoveSongFromList(dup)
	if err != nil {
		t.Fatalf("删不掉：%v", err)
	}
	if n != 2 {
		t.Errorf("删掉了 %d 份，want 2", n)
	}
	got := pathsOf(a.Queue().Items())
	if len(got) != 1 || got[0] != paths[1] {
		t.Fatalf("删完之后队列是 %v，want 只剩 %s", got, paths[1])
	}
}

// TestRemoveSongFromListTakesTheCurrentOneWithIt 删掉的里面有正在放的那首时，
// 接着放下一首——和删一份那条同一个道理。
func TestRemoveSongFromListTakesTheCurrentOneWithIt(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 2)
	a.Enqueue(app.LocalItem(paths[0]))
	dup := app.LocalItem(paths[0])
	a.Enqueue(dup, app.LocalItem(paths[1])) // 队列：甲,甲,乙，正在放甲

	if _, err := a.RemoveSongFromList(dup); err != nil {
		t.Fatalf("删不掉：%v", err)
	}
	waitFor(t, 2*time.Second, "接着放乙", func() bool { return a.Snapshot().Title == "b" })
	if got := a.Queue().CurrentIndex(); got != 0 {
		t.Errorf("当前项是第 %d 首，want 第 1 首（就是乙）", got)
	}
}

// TestRemoveSongFromListRefusesUnidentifiableItems 认不出是哪一项的，**这个口
// 一概不删**：两首都认不出来的歌在标识上长得一样，按一次删掉两首不同的歌比按了
// 没反应糟得多。界面上那条行自己知道位置，走 `RemoveAt`（按位置删一份）。
func TestRemoveSongFromListRefusesUnidentifiableItems(t *testing.T) {
	a := newApp(t)
	a.Queue().Append(app.Item{Title: "甲"}, app.Item{Title: "乙"})

	if _, err := a.RemoveSongFromList(app.Item{Title: "甲"}); err == nil {
		t.Error("拿一个认不出来的项走「删全部同身份」，居然报告成功了")
	}
	if n := a.Queue().Len(); n != 2 {
		t.Errorf("队列剩下 %d 首（把两首都删了？）", n)
	}
}

// TestPlayAtRetriesTheCurrentSongWhenNothingIsPlaying 是「接歌失败 → 停下来」
// 之后的重试：队列的当前项还指着那一首，但**没有歌在放**，用户再点它就得真的
// 把它放出来（文件补回来了、或者网好了）。
//
// 判据原来只是「当前项 == 这一项就什么都不做」，于是接歌失败之后那一行永远按
// 不动——用户按的确实是「播放」，而界面上一个字错误都没有。
func TestPlayAtRetriesTheCurrentSongWhenNothingIsPlaying(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 1)
	a.Enqueue(items(paths[0])...)
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().HasTrack })

	// 队列里加一首打不开的文件，删掉正在放的 → 接不上，停下来。
	missing := filepath.Join(t.TempDir(), "缺了这一首.flac")
	a.Queue().Append(app.Item{Source: "local", Path: missing})
	if err := a.RemoveAt(0); err == nil {
		t.Fatal("删掉正在放的那首、下一首打不开，却一声不吭")
	}
	st := a.Snapshot()
	if st.HasTrack {
		t.Fatalf("接不上之后还在放：%+v", st)
	}
	if a.Queue().CurrentIndex() != 0 {
		t.Fatalf("测试自己没摆好：当前项是第 %d 首", a.Queue().CurrentIndex())
	}

	// 把那个文件补回来（复制一份真的 FLAC 过去），再点当前行 = 重试。
	b, err := os.ReadFile(flac(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(missing, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.PlayAt(0); err != nil {
		t.Fatalf("重试：%v", err)
	}
	waitFor(t, 2*time.Second, "重试之后放出来了", func() bool { return a.Snapshot().HasTrack })
	if got := a.Snapshot().Title; got != "缺了这一首" {
		t.Errorf("重试之后放的是 %q，want 缺了这一首", got)
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

// TestPlayAtStopsTheOldSongBeforeOpeningAnOnlineOne 钉「换成在线的一首时，旧的
// 那首不许继续响」。
//
// 现象：在放本地 A，抽屉里点在线 B（B 要在后台取流）。抽屉已经按新的当前项把高亮
// 挪到 B 上，而**真正出声的还是 A**——播放条上写着 A，用户以为在放 B。换来的是
// 「界面说的和它做的不一致」，而且 B 要是压根建不起来（网络断了、风控了），这个
// 状态就一直停在那儿。
//
// 修法：换成在线的一首之前先 `ClearTrack`（在线流是后台建的，建好之前界面上没有
// 新歌）。本地项不用这样——`Open` 是同步的。
func TestPlayAtStopsTheOldSongBeforeOpeningAnOnlineOne(t *testing.T) {
	f := newStreamFixture(t)
	dir := t.TempDir()
	a := newOnlineApp(t, f, dir)
	if err := a.Enqueue(app.LocalItem(flac(t))); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "本地那一首开始播", func() bool { return a.Snapshot().HasTrack })
	localTitle := a.Snapshot().Title

	// 取流一律回 503：在线那一首建不起来（断网 / 风控），于是它连播放条都上不去。
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "取流失败", http.StatusServiceUnavailable)
	})
	a.Queue().Append(onlineItem("BV1", "c1", "在线那一首"))
	if err := a.PlayAt(1); err != nil {
		t.Fatalf("点在线那一首：%v", err)
	}
	waitFor(t, 30*time.Second, "在线那条报出失败", func() bool { return a.Snapshot().Err != "" })

	st := a.Snapshot()
	if st.HasTrack {
		t.Errorf("换成在线的一首之后本地那首还在放（%q）——播放条和抽屉说的不是同一首", st.Title)
	}
	if st.Title == localTitle {
		t.Errorf("播放条上还是本地那首 %q", st.Title)
	}
	if cur, ok := a.Queue().Current(); !ok || cur.Title != "在线那一首" {
		t.Errorf("队列的当前项是 %+v，want 那一首在线的", cur)
	}
}

// TestPlayAtOnTheSameSongSomewhereElseDoesNotRestartIt 钉「点正在放的那一行不会
// 从头重播」。
//
// 队列 `[甲, 甲]`、抽屉去重只画一行（第一份），而**正在放的是第二份**。用户点那一
// 行，指的就是他正在听的这首——重播一遍是没问过他的（那是播放条上「播放」那个键的
// 活，Biu 的 `playListItem` 也是 `if (playId === id) return`）。
//
// 判据是**位置**：重播的话 Position 会从 2s 跳回 0。
func TestPlayAtOnTheSameSongSomewhereElseDoesNotRestartIt(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 1)
	dup := app.LocalItem(paths[0])
	a.Enqueue(app.LocalItem(paths[0]))
	a.Enqueue(dup)    // 队列 甲,甲
	a.Queue().Move(1) // 正在放的是**第二份**甲
	waitFor(t, 10*time.Second, "开始出声", func() bool { return a.Snapshot().HasTrack })

	// 拖到 2s 附近，好在后面量「有没有从头来」。
	a.ScrubEnd(0.5)
	waitFor(t, 5*time.Second, "位置往前走", func() bool { return a.Snapshot().Position > 0 })
	before := a.Snapshot().Position
	if before <= 0 {
		t.Fatalf("还没开始走（%v），这条用例量不出重播", before)
	}

	// 点抽屉里那一行（它在队列里的下标是 0，而正在放的是第 1 首）。
	if err := a.PlayAt(0); err != nil {
		t.Fatal(err)
	}
	// 队列的当前项挪到那一行（去重之后它就是「这一首」在队列里的代表），
	// 但**不重播**。
	if got := a.Queue().CurrentIndex(); got != 0 {
		t.Errorf("当前项是第 %d 首，want 第 1 首（点的那一行）", got)
	}
	if got := a.Snapshot().Position; got < before {
		t.Errorf("位置从 %v 退回 %v —— 从头重播了", before, got)
	}
}
