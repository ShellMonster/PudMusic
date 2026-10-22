package list

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一组验的是**右键菜单**：菜单是系统的原生菜单（不是自绘浮层），项照 Biu 的
// 那一份，而且每一项都接着真的操作。
//
// 跑的是 MyGo 的 Tester：`tt.RightClick` 开菜单，`tt.Menu()` 列出菜单上有的项
// （"-" 是分隔），`tt.ChooseMenuItem` 点其中一项——和人在真窗口里做的是同一件事。

// recorded 是记动作的桩：菜单项接的是什么操作，全记在这儿。
type recorded struct {
	played  [][]app.Item // 每次「播放」收到的队列
	added   [][]app.Item // 每次「添加到队列」收到的
	nexted  []app.Item   // 每次「下一首播放」收到的
	opened  []app.Item   // 每次「在…打开」收到的
	notices []string     // 说过的话
}

func (r *recorded) actions() Actions {
	return Actions{
		Play:     func(items []app.Item) { r.played = append(r.played, items) },
		Add:      func(items []app.Item) { r.added = append(r.added, items) },
		PlayNext: func(item app.Item) { r.nexted = append(r.nexted, item) },
		Open:     func(item app.Item) { r.opened = append(r.opened, item) },
		Notify:   func(text string) { r.notices = append(r.notices, text) },
	}
}

// onlineSongs 造 n 行在线的歌（mv）。
func onlineSongs(n int) []Music {
	out := make([]Music, n)
	for i := range out {
		out[i] = Music{
			ID:    "bv" + string(rune('a'+i)),
			Title: "在线的歌 " + string(rune('A'+i)),
			Owner: "UP 主",
			Item:  app.Item{Source: "online", Type: "mv", Bvid: "BV" + string(rune('1'+i)), Title: "在线的歌"},
		}
	}
	return out
}

// TestRightClickOpensTheBiuMenu 是验收「右键菜单能点」的第一半：菜单开得出来，
// 而且开的就是 Biu 那一套。
//
// ⚠️ 判的是**菜单上真的有哪几项**，不是「代码里写了几个分支」——菜单是这一趟
// 全部建出来的，少建一项那一项就永远点不到（`mygo/ui/context-menu.md`）。
func TestRightClickOpensTheBiuMenu(t *testing.T) {
	_, tt := newList(t, Actions{}, onlineSongs(3))

	if err := tt.RightClick("在线的歌 B"); err != nil {
		t.Fatal(err)
	}
	got := tt.Menu()
	want := []string{"播放", "下一首播放", "添加到队列", "-", "在 B 站打开", "-", "收藏", "下载音频"}
	if !slices.Equal(got, want) {
		t.Errorf("菜单上有 %q，want %q", got, want)
	}
}

// TestChoosingPlayQueuesThisSongAndTheRestAfterIt 钉住两件事：
//
//   - 菜单挂在**行**上：按的是第二行，放出来的就得是第二首。菜单挂到整列上共用
//     一份的话，按第二行放出来的会是第一首——那比不弹还糟。
//   - 「播放」的语义是从这一首往后接（Biu 的 music-list-item 双击、从一页的第一首
//     开始听都是这个意思）。少一首就是「点了一首只放一首」，多一首就是「把整张
//     列表从头又放了一遍」。
func TestChoosingPlayQueuesThisSongAndTheRestAfterIt(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(5))

	if err := tt.RightClick("在线的歌 B"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Fatal(err)
	}

	if len(rec.played) != 1 {
		t.Fatalf("收到 %d 次播放，want 1 次", len(rec.played))
	}
	got := rec.played[0]
	if len(got) != 4 {
		t.Fatalf("从第二首开始，队列里进了 %d 首，want 4 首（它和它后面那三首）", len(got))
	}
	for i, want := range []string{"BV2", "BV3", "BV4", "BV5"} {
		if got[i].Bvid != want {
			t.Errorf("队列第 %d 首是 %q，want %q", i+1, got[i].Bvid, want)
		}
	}
}

// TestChoosingPlayNextHandsOverJustThatSong 钉住「下一首播放」的**那一句字**
// 说的是什么：插到正在放的那首后面。
//
// ⚠️ 它和「添加到队列」差在**插在哪**，而插在哪只有队列那边认得：这一层只
// 负责把**这一首**交过去，一个都不用带。带着整段过去的（也就是照着「播放」
// 抄一遍）的话，用户的队列会从当前这首起整个换一遍——症状是「按了下一首
// 播放，正在放的歌跳了」。
//
// 真的落位在 `internal/app` 的 `TestInsertNextLandsRightAfterTheCurrentOne`。
func TestChoosingPlayNextHandsOverJustThatSong(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(5))

	if err := tt.RightClick("在线的歌 C"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下一首播放"); err != nil {
		t.Fatal(err)
	}

	if len(rec.nexted) != 1 {
		t.Fatalf("收到 %d 次「下一首播放」，want 1 次", len(rec.nexted))
	}
	if got := rec.nexted[0].Bvid; got != "BV3" {
		t.Errorf("排进下一首的是 %q，want BV3", got)
	}
	// **不能顺手换成队列**：这一下只动一首，用户正在听的歌不该被换掉。
	if len(rec.played) != 0 {
		t.Errorf("「下一首播放」顺手换了一次队列：%v", rec.played)
	}
	if len(rec.added) != 0 {
		t.Errorf("「下一首播放」顺手接了一次队尾：%v", rec.added)
	}
}

// TestPlayNextSaysSoWhenNothingIsWired 钉住没接线时它**说一句**，不是按了没反应。
//
// 「按了没反应比还没接更难解释」——用户分不清是这一项坏了还是整个应用坏了。
func TestPlayNextSaysSoWhenNothingIsWired(t *testing.T) {
	rec := &recorded{}
	// 只留 Notify：`PlayNext` 缺席就是「那一层没接线」的样子。
	_, tt := newList(t, Actions{Notify: func(s string) { rec.notices = append(rec.notices, s) }}, onlineSongs(3))

	if err := tt.RightClick("在线的歌 B"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下一首播放"); err != nil {
		t.Fatal(err)
	}
	if len(rec.notices) == 0 {
		t.Errorf("没接线的「下一首播放」按下去一声不吭：%v", rec.notices)
	}
}

// TestChoosingAddAppendsJustThatSong 验「添加到队列」是加一首，不换队列。
func TestChoosingAddAppendsJustThatSong(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(5))

	if err := tt.RightClick("在线的歌 C"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加到队列"); err != nil {
		t.Fatal(err)
	}

	if len(rec.added) != 1 || len(rec.added[0]) != 1 {
		t.Fatalf("「添加到队列」收到 %v，want 只有一首", rec.added)
	}
	if got := rec.added[0][0].Bvid; got != "BV3" {
		t.Errorf("加进队列的是 %q，want BV3", got)
	}
	if len(rec.played) != 0 {
		t.Errorf("「添加到队列」顺手把队列换了（收到 %d 次播放）", len(rec.played))
	}
}

// TestTheOpenItemOpensThatSong 验「在 B 站打开」打开的是这一首，不是第一首。
func TestTheOpenItemOpensThatSong(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(3))

	if err := tt.RightClick("在线的歌 C"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("在 B 站打开"); err != nil {
		t.Fatal(err)
	}
	if len(rec.opened) != 1 || rec.opened[0].Bvid != "BV3" {
		t.Errorf("打开的是 %+v，want BV3 那一首", rec.opened)
	}
}

// TestALocalSongSaysFinderInsteadOfBili 验本地文件那一行写的是「在访达中显示」。
//
// 本地文件没有 bvid，按「在 B 站打开」只会打开一个打不开的网页。这不是小事：
// 用户按了、浏览器弹出来、404，界面上还多了一个窗口。
func TestALocalSongSaysFinderInsteadOfBili(t *testing.T) {
	_, tt := newList(t, Actions{}, []Music{
		{ID: "l", Title: "本地的一首", Item: app.LocalItem("/music/本地的一首.mp3")},
	})

	if err := tt.RightClick("本地的一首"); err != nil {
		t.Fatal(err)
	}
	got := tt.Menu()
	if !slices.Contains(got, "在访达中显示") {
		t.Errorf("菜单上有 %q，want 里面有「在访达中显示」", got)
	}
	if slices.Contains(got, "在 B 站打开") {
		t.Errorf("本地文件那一行写着「在 B 站打开」，菜单上有 %q", got)
	}
}

// TestTheUnwiredItemsSaySo 验还没接的操作会说话，不是按了没反应。
//
// 收藏和下载要 B 站接口，那一张票还没做。说清楚比按了没反应好——用户至少知道
// 不是界面卡了。
func TestTheUnwiredItemsSaySo(t *testing.T) {
	for _, label := range []string{"收藏", "下载音频"} {
		rec := &recorded{}
		_, tt := newList(t, rec.actions(), onlineSongs(3))

		if err := tt.RightClick("在线的歌 A"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(label); err != nil {
			t.Fatal(err)
		}
		if len(rec.notices) != 1 {
			t.Fatalf("按了「%s」，说了 %d 句话，want 1 句", label, len(rec.notices))
		}
		if !strings.Contains(rec.notices[0], label) {
			t.Errorf("按了「%s」，说的是 %q——话里得带上用户按的那个字", label, rec.notices[0])
		}
	}
}

// TestTheMenuActsOnTheSongThatWasClicked 验菜单做的是**用户按下去那一首**的
// 事，不是位置上那一首。
//
// 「菜单开着的时候又续拉了一页」是分页列表的常态（Biu 的 virtual-page-list 就是
// 这么长的）：菜单打开到被点之间，头上插了一行，原来那一首挪到了下标 3。按了
// 「播放」，放出来的必须是用户按的那首（BV3），不是新页顶上那一首，也不是原
// 先下标 2 上那首。
func TestTheMenuActsOnTheSongThatWasClicked(t *testing.T) {
	rec := &recorded{}
	songs := onlineSongs(3)
	l, tt := newList(t, rec.actions(), songs)

	// 点**第一首**：头上插一行之后它挪到了下标 1。
	if err := tt.RightClick("在线的歌 A"); err != nil {
		t.Fatal(err)
	}
	l.SetSongs(append([]Music{{ID: "bv-new", Title: "新来的一首"}}, songs...))
	frame(tt)

	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Fatal(err)
	}
	if len(rec.played) != 1 {
		t.Fatalf("收到 %d 次播放，want 1 次", len(rec.played))
	}
	// want 是 BV1/BV2/BV3 三首。拿老下标 0 的话，队首会是刚插进来的那一首
	//（它没有 bvid），这条断言就是靠这个把它抓出来的。
	if got := bvidOf(rec.played[0]); !slices.Equal(got, []string{"BV1", "BV2", "BV3"}) {
		t.Errorf("换过数据之后点「播放」，换进队列的是 %v，want [BV1 BV2 BV3]（按下去的那一首往后）", got)
	}
}

// bvidOf 拿一串项的 bvid，断言失败时说人话。
func bvidOf(items []app.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Bvid
	}
	return out
}

// TestTheMenuIsWiredToTheRealQueue 是验收「菜单项接上真实操作」：这里不接桩，
// 接的是真的 App，走的是右键菜单那条路。
//
// 判据是**队列里真的有那几首、播放条上真的换成了那一首**——不是「调了
// App.PlayFrom」。走一遍菜单才发现得了「菜单那一项忘了接」这种错。
func TestTheMenuIsWiredToTheRealQueue(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	paths := audioCopies(t, "第一首", "第二首", "第三首")
	songs := []Music{
		{ID: "a", Title: "第一首", Item: app.LocalItem(paths[0])},
		{ID: "b", Title: "第二首", Item: app.LocalItem(paths[1])},
		{ID: "c", Title: "第三首", Item: app.LocalItem(paths[2])},
	}

	var notices []string
	_, tt := newList(t, AppActions(a, func(s string) { notices = append(notices, s) }), songs)

	if err := tt.RightClick("第二首"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Fatal(err)
	}

	if len(notices) != 0 {
		t.Errorf("播放成功了却说了话：%q", notices)
	}
	items := a.Queue().Items()
	if len(items) != 2 {
		t.Fatalf("队列里有 %d 首，want 2（第二首和第三首）", len(items))
	}
	if items[0].Path != paths[1] || items[1].Path != paths[2] {
		t.Errorf("队列里是 %q 和 %q，want 第二首和第三首", items[0].Path, items[1].Path)
	}
	if got := a.Snapshot().Title; got != "第二首" {
		t.Errorf("播放条上是 %q，want 第二首", got)
	}

	// 「添加到队列」接的是 App.Enqueue：追加一首，不打断正在放的那首。
	if err := tt.RightClick("第一首"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加到队列"); err != nil {
		t.Fatal(err)
	}
	if got := len(a.Queue().Items()); got != 3 {
		t.Errorf("加了一首之后队列里有 %d 首，want 3", got)
	}
	if got := a.Snapshot().Title; got != "第二首" {
		t.Errorf("加完歌之后播放条换成了 %q，want 还是第二首（加歌不该抢播放）", got)
	}

}

// audioCopies 按给的名字复制测试音频，返回它们的路径。
//
// **文件名按歌名起**：app.LocalItem 的歌名取的是文件名（去扩展名），本地音乐
// 页就是这么来的。名字不同才看得出「换到下一首了」还是「根本没换」。
func audioCopies(t *testing.T, names ...string) []string {
	t.Helper()
	src := filepath.Join("..", "..", "..", "service", "audio", "testdata", "tone.mp3")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("找不到测试音频：%v", err)
	}
	dir := t.TempDir()
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = filepath.Join(dir, name+".mp3")
		if err := os.WriteFile(out[i], b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// TestDoubleClickingARowPlaysIt 验双击播放（Biu 的 onDoubleClick）。
//
// 单击是选中、双击才是播放：Biu 的 music-list-item 就是这么分的
// （`music-list-item/index.tsx` 的 onDoubleClick）。合成一处的话，选中一行的
// 动作就顺手把队列换了。
func TestDoubleClickingARowPlaysIt(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(4))

	r, ok := tt.Find("在线的歌 B")
	if !ok {
		t.Fatalf("画面上找不到那一行，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	frame(tt)

	if len(rec.played) != 1 {
		t.Fatalf("双击之后收到 %d 次播放，want 1 次（单击只该选中）", len(rec.played))
	}
	if got := rec.played[0][0].Bvid; got != "BV2" {
		t.Errorf("双击第二行放出来的是 %q，want BV2", got)
	}
}

// TestAnUnplayableSongHasNoPlayEntryBeforeYouPressIt 「点之前就不给播放入口」。
//
// 首页推荐、音乐榜、新歌速递、搜索结果**共用这一份菜单**，而它们里面确实有
// 放不了的条目（B 站给纯音乐条目不挂稿件，那种行画得出来而放不了）。
//
// 这一条钉的是**按不动**：菜单项还在（用户看得到这一份菜单），但它是灰的——
// 而「按了才说」是把一句应用内部的判断甩给用户，用户分不清「真没有音频流」
// 和「没取到」，而这两件事要走的路正好相反。
//
// 末尾还要有**一句说明**：按不动而不说为什么等于让用户猜，而用户猜不出来。
func TestAnUnplayableSongHasNoPlayEntryBeforeYouPressIt(t *testing.T) {
	songs := []Music{{
		ID:    "x",
		Title: "没挂稿件的那一条",
		Item:  app.Item{Source: "online", Type: "mv", Title: "没挂稿件的那一条"},
	}}
	_, tt := newList(t, Actions{}, songs)

	if err := tt.RightClick("没挂稿件的那一条"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("播放"); err == nil {
		t.Error("放不了的条目，「播放」竟然能按")
	}
	items := tt.Menu()
	last := items[len(items)-1]
	if !strings.Contains(last, "不是投稿") || !strings.Contains(last, "搜同名") {
		t.Errorf("菜单末尾那句话是 %q，没有说清「为什么」和「还能怎么办」", last)
	}
}

// TestALocalSongStillHasAPlayEntry 本地文件不许被误判成放不了。
//
// 🔴 **这个坑真踩过一次**：判据写成「没有 bvid 就没有身份」的话，本地文件
// 落进「不是投稿」那一支，于是本地音乐那一页的「播放」整片灰掉——
// 而本地文件走的是开文件那一路，它当然放得了。
func TestALocalSongStillHasAPlayEntry(t *testing.T) {
	paths := audioCopies(t, "第一首")
	rec := &recorded{}
	songs := []Music{{ID: "a", Title: "第一首", Item: app.LocalItem(paths[0])}}
	_, tt := newList(t, rec.actions(), songs)

	if err := tt.RightClick("第一首"); err != nil {
		t.Fatal(err)
	}
	// ⚠️ **菜单项要在点之前数**：点完菜单就关了，而 `tt.Menu()` 读的是
	// **当前开着的那一份**。
	items := tt.Menu()
	if len(items) != 8 {
		t.Errorf("菜单上有 %d 项：%q", len(items), items)
	}
	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Errorf("本地文件的「播放」按不动：%v", err)
	}
	if len(rec.played) != 1 {
		t.Errorf("本地文件的「播放」没接上（动作记了 %d 次）", len(rec.played))
	}
}
