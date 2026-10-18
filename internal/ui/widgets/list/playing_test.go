package list

import (
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// 列表上的「正在播放」和 hover 出来的播放按钮（对照审计的 A24）。
//
// 这一组最要紧的是**判据**：正在放的那一行是靠 `app.Item.Identity`（判重标识）
// 认出来的，不是靠行的序号、也不是靠 `Music.ID`。理由在 `Actions.IsPlaying`
// 和 `App.IsPlaying` 各自的注释里——队列会重排，而 `Music.ID` 是行的身份。
//
// 那两样认错各自长什么样、而**屏幕上完全看不出来**：
//
//   - 按序号认 → 高亮跳到**另一首**上（队列里当前项的下标和列表里的下标是两套）
//   - 按 Music.ID 认 → 历史记录那一页把**同一首歌的两次观看**当成两首，
//     而它们在 B 站那边是同一首

// playingActions 是一份「认得出正在放的那首」的接线，判据和 `AppActions`
// 接的 `App.IsPlaying` 同一个：按判重标识比。
//
// playing 收的是**判重标识**，不是下标——这样测试里写下来的意图和实现里的
// 判据是同一件事：摆 `[]string{"mv:BV3"}` 就是「正在放的是 BV3 那一首」。
func playingActions(rec *recorded, playing ...string) Actions {
	set := make(map[string]bool, len(playing))
	for _, id := range playing {
		set[id] = true
	}
	acts := rec.actions()
	acts.IsPlaying = func(item app.Item) bool { return set[item.Identity()] }
	return acts
}

// markedOn 报 title 那一行有没有被标成「正在播放」。
//
// 认的是**无障碍标签**（`歌名 正在播放`），不是画出来的那五个字：`Tester.Find`
// 只给回第一个匹配，而同一首歌排了两行时两个标记的字一模一样——按字找的话
// 第二行永远量不到，而那一行恰恰是最容易认错的那一行。
func markedOn(t *testing.T, tt *ui.Tester, title string) bool {
	t.Helper()
	if _, ok := tt.Find(title); !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	_, ok := tt.Find(title + " " + PlayingMark)
	return ok
}

// countMarks 数一帧里有几处「正在播放」。
//
// ⚠️ 走 `Texts()` 而不是 `HasText`：后者是**子串**匹配，而「正在播放」里就含着
// 「播放」——用它判 hover 按钮的话，按钮一次都不出现也会为真（这一条已经真的
// 咬了一次：`TestThePlayingRowDoesNotOfferAPlayButton` 第一版就是这么绿的）。
func countMarks(tt *ui.Tester) int {
	n := 0
	for _, s := range tt.Texts() {
		if s == PlayingMark || strings.HasSuffix(s, " "+PlayingMark) {
			n++
		}
	}
	return n
}

// shows 报这一帧上有没有一个**文字正好是** s 的控件。
//
// 为什么要自己写而不用 `HasText`：它是 `strings.Contains`（`mygo/ui/headless.go`），
// 「播放」和「正在播放」在它眼里是同一个。
func shows(tt *ui.Tester, s string) bool {
	for _, got := range tt.Texts() {
		if got == s {
			return true
		}
	}
	return false
}

// TestTheRowThatIsPlayingIsTheOnlyOneMarked 钉住「一眼能看出来」的那一半：
// 整个列表里**有且只有一行**带标记。
func TestTheRowThatIsPlayingIsTheOnlyOneMarked(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, playingActions(rec, "mv:BV3"), onlineSongs(5))

	for i, title := range []string{"在线的歌 A", "在线的歌 B", "在线的歌 C", "在线的歌 D", "在线的歌 E"} {
		want := i == 2
		if got := markedOn(t, tt, title); got != want {
			t.Errorf("「%s」被标成正在放 = %v，want %v", title, got, want)
		}
	}
	if n := countMarks(tt); n != 1 {
		t.Errorf("画面上有 %d 处「正在播放」，want 1 处", n)
	}
}

// TestThePlayingMarkIsFoundByTheSongNotByItsPlace 是这一组最要紧的一条。
//
// 它照着**按下标认**会出的那个错摆：队列是 `[Z, A]`、正在放的是 **A**（当前项
// 的下标是 1），而列表里 A 在**第 0 行**。按下标认的话高亮会落到第 1 行去——
// 界面上那是另一首歌，而**界面上没有任何异样**。
func TestThePlayingMarkIsFoundByTheSongNotByItsPlace(t *testing.T) {
	rec := &recorded{}
	songs := onlineSongs(3)
	// 队列里正在放的是第 2 首，列表里它在第 1 行。
	acts := playingActions(rec, songs[1].Item.Identity())
	_, tt := newList(t, acts, songs)

	if !markedOn(t, tt, songs[1].Title) {
		t.Errorf("正在放的那首（第 %d 行，队列里是第 2 首）没有被标出来，文字有 %q",
			2, tt.Texts())
	}
	for _, m := range []Music{songs[0], songs[2]} {
		if markedOn(t, tt, m.Title) {
			t.Errorf("「%s」不是正在放的那首，却被标出来了（按队列的下标认的话就会这样）", m.Title)
		}
	}
}

// TestThePlayingMarkFollowsTheSongWhenTheQueueMoves 钉住换歌之后标记跟着换。
//
// 队列重排是常事（「下一首播放」插在当前后面、拖拽排序、随机模式），而**列表
// 不动**。缓存住判据的话换歌之后高亮还钉在上一首——那一首已经不在放了。
func TestThePlayingMarkFollowsTheSongWhenTheQueueMoves(t *testing.T) {
	rec := &recorded{}
	songs := onlineSongs(4)
	var playing = songs[0].Item.Identity()
	acts := playingActions(rec)
	acts.IsPlaying = func(item app.Item) bool { return item.Identity() == playing }

	_, tt := newList(t, acts, songs)
	if !markedOn(t, tt, songs[0].Title) {
		t.Fatalf("一开始就没有标出正在放的那首：%q", tt.Texts())
	}

	playing = songs[2].Item.Identity()
	frame(tt)
	if markedOn(t, tt, songs[0].Title) {
		t.Errorf("换歌之后旧的那首还标着「正在播放」：%q", tt.Texts())
	}
	if !markedOn(t, tt, songs[2].Title) {
		t.Errorf("换歌之后新的那首没有被标出来：%q", tt.Texts())
	}
}

// TestTheSameSongInTwoRowsIsOneHighlight 钉住判据是**同一首**而不是同一份。
//
// 历史记录那一页同一首歌看过两次是**两行**（`Music.ID` 不同，那是铁律四要的），
// 而它们在 B 站那边是同一首。认「同一首」时两行都亮——那是对的：用户点的、
// 听的都是同一首歌，而另一份只是它自己那份记录。
//
// 认「同一份」（比整个 `Item`）的话两行都不亮，于是用户在一份**正在放**的
// 记录上看到的是「哪首都没在放」。
func TestTheSameSongInTwoRowsIsOneHighlight(t *testing.T) {
	rec := &recorded{}
	a := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "同一首"}
	b := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "2", Title: "同一首的另一集"}
	songs := []Music{
		{ID: "history:BV1@1", Title: "看过的第一次", Item: a},
		{ID: "history:BV1@2", Title: "看过的第二次", Item: b},
	}
	_, tt := newList(t, playingActions(rec, a.Identity()), songs)

	if !markedOn(t, tt, "看过的第一次") {
		t.Errorf("正在放的那份没有被标出来：%q", tt.Texts())
	}
	if !markedOn(t, tt, "看过的第二次") {
		t.Errorf("同一首歌的另一份没有被标出来（那是同一首，cid 不一样而已）：%q", tt.Texts())
	}
}

// TestNoPlayingMarkWhenNothingIsWired 是「没接」的样子，不是一片高亮。
func TestNoPlayingMarkWhenNothingIsWired(t *testing.T) {
	_, tt := newList(t, (&recorded{}).actions(), onlineSongs(3))
	if n := countMarks(tt); n != 0 {
		t.Errorf("没装判据却标了 %d 行「正在播放」：%q", n, tt.Texts())
	}
}

// TestTheGridMarksThePlayingCellToo 盯网格和列表说的是同一件事。
//
// 同一份数据两种摆法，判据一样的话两边才会一致。用户从首页的网格里放了某一首、
// 再切到列表页，高亮得跟着走。
func TestTheGridMarksThePlayingCellToo(t *testing.T) {
	rec := &recorded{}
	songs := onlineSongs(4)
	_, tt := newGrid(t, playingActions(rec, songs[2].Item.Identity()), songs, 1100, gridH)

	if n := countMarks(tt); n != 1 {
		t.Errorf("网格上有 %d 处「正在播放」，want 1 处：%q", n, tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hover 出来的播放按钮
// ─────────────────────────────────────────────────────────────────────────────

// hoverOn 把指针移到 title 那一行的中间并重画几帧。
//
// ⚠️ **要连着几帧**：hover 是**上一帧**的布局决定的（`rt.hover`），所以移过去
// 之后第一帧往往还拿着旧答案。
func hoverOn(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	for range 3 {
		tt.Frame()
	}
}

// hoverOff 把指针挪到画面外（列表上方那块空白），然后重画几帧。
func hoverOff(t *testing.T, tt *ui.Tester) {
	t.Helper()
	tt.Move(testerW+100, -100)
	for range 3 {
		tt.Frame()
	}
}

// TestHoveringARowOffersAPlayButton 钉住「指针移上去出一个能按的播放按钮」。
func TestHoveringARowOffersAPlayButton(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(5))

	if shows(tt, "播放") {
		t.Fatalf("指针还没移上去就有「播放」了：%q", tt.Texts())
	}
	hoverOn(t, tt, "在线的歌 C")
	if !shows(tt, "播放") {
		t.Errorf("指针移上去之后没有出现播放按钮：%q", tt.Texts())
	}

	hoverOff(t, tt)
	if shows(tt, "播放") {
		t.Errorf("指针移走之后按钮还留着：%q", tt.Texts())
	}
}

// TestTheHoverButtonPlaysThatSongAndTheRestAfterIt 钉住按钮的**语义**和双击
// 一样：从这一首往后接。
//
// 不一样的话用户第一次按 hover 出来的按钮就会拿到一个自己没点过的动作
// （比如只加一首、或者只跳到这一首）。
func TestTheHoverButtonPlaysThatSongAndTheRestAfterIt(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(5))

	hoverOn(t, tt, "在线的歌 B")
	if err := tt.Click("播放"); err != nil {
		t.Fatalf("点不了 hover 出来的播放按钮：%v（画面：%q）", err, tt.Texts())
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

// ownersAre 是 n 行歌，**每行的 UP 名都不一样**（"UP A" 到 "UP E"）。
//
// 为什么非要唯一：这一组要量的是**行里某一块东西的横坐标**，而量它就得先找到
// 它——`Tester.Find` 认的是控件的文本，两行画着同一个字的时候它只给回第一块。
//
// ⚠️ **量的是 UP 名那一块，不是时长**：时长是这一行最后一块、且靠右对齐，它
// 的位置由行的右边界决定。序号那一列变宽时被推走的是**标题列的左边**，
// 也就是 UP 名那一块——时长一动不动（实测：按钮加宽 30，歌名和封面右移了，
// 时长的 x 一个像素没变）。
func ownersAre(n int) []Music {
	songs := onlineSongs(n)
	for i := range songs {
		songs[i].Owner = "UP " + string(rune('A'+i))
	}
	return songs
}

// TestTheHoverButtonStaysInTheIndexColumn 盯按钮出现时那一行**不横着跳一下**。
//
// 序号那一列是定宽 `indexWidth` 的（Biu 的 `min-w-8 text-center`），按钮用它
// 自己的宽度就会把封面、连同后面每一块一起往右推——而指针正在往那一行上移，
// 行在指针底下滑一下是很难受的。
//
// ⚠️ **量的是行里的 UP 名那一块，不是歌名**：歌名要量它就得用
// `Tester.Find(歌名)`，而它命中的是 `ui.List` 挂在**整行**上的无障碍盒子
// （见 `List.View` 里那句 `l.state.Label`），那个盒子的 X **永远是 0**——
// 第一版就是这么写的，于是把按钮从 `indexWidth` 改成 70（封面整体右移 32px）
// 它照样一路绿。
func TestTheHoverButtonStaysInTheIndexColumn(t *testing.T) {
	_, tt := newList(t, (&recorded{}).actions(), ownersAre(5))

	// 第三行的 UP 名。它在序号和封面右边，而左边就是序号那一列。
	before, ok := tt.Find("UP C")
	if !ok {
		t.Fatalf("找不到第三行的 UP 名，文字有 %q", tt.Texts())
	}
	if before.X <= indexWidth+coverSize {
		t.Fatalf("第三行的 UP 名只在 x=%.1f，连序号（%d）加封面（%d）都没让开——"+
			"量错了东西，这条用例什么也证明不了", before.X, indexWidth, coverSize)
	}

	hoverOn(t, tt, "在线的歌 C")

	after, ok := tt.Find("UP C")
	if !ok {
		t.Fatalf("hover 之后找不到第三行的 UP 名，文字有 %q", tt.Texts())
	}
	if after.X != before.X {
		t.Errorf("hover 之后第三行横着挪了 %.1f（%.1f → %.1f）——"+
			"按钮比序号那一列宽，它右边的每一块都被推走了，而指针就在那一行上",
			after.X-before.X, before.X, after.X)
	}
}

// TestTheHoverButtonIsNotTruncated 量 hover 出来的按钮上那两个字**画全了没有**。
//
// ⚠️ `HasText("播放")` / `Find("播放")` 读的是控件的**文本**，被截的只是画出来的
// 那几个字——所以这一条断言第一版一直是绿的，而屏幕上写着的是一个 10px 宽的
// 「播」字。断法是量宽度：和同一串字在一个**不设限**的同款按钮里的自然宽度比。
//
// 这一条教训在 `internal/ui/playbar_rate_test.go` 的 TestRateMenuLabelsAreNotTruncated
// 那里已经吃过一次，这里是同一个坑的第二个出口。
func TestTheHoverButtonIsNotTruncated(t *testing.T) {
	natural := hoverButtonNaturalWidth(t)

	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(5))
	hoverOn(t, tt, "在线的歌 C")

	got, ok := tt.Find("播放")
	if !ok {
		t.Fatalf("hover 之后画面上没有那个按钮：%q", tt.Texts())
	}
	if got.W+0.5 < natural {
		t.Errorf("那个按钮上的字只画出了 %.1f 宽，它本来要 %.1f——被截了"+
			"（一个只写着「播」的按钮，用户按之前得先猜那是什么）", got.W, natural)
	}
}

// hoverButtonNaturalWidth 是「播放」两个字在一个不设限的同款按钮里占多宽。
func hoverButtonNaturalWidth(t *testing.T) float32 {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Width(400).Children(func() { ui.Button(c, "播放").Padding(1, 4) })
	}, 400, 100)
	tt.Frame()
	r, ok := tt.Find("播放")
	if !ok {
		t.Fatal("连不设限的按钮都量不出「播放」的宽度")
	}
	return r.W
}

// TestThePlayingRowDoesNotOfferAPlayButton 盯正在放的那一行不摆这个按钮。
//
// 按下去是同一首从头重播（`App.PlayAt` 开头就 return 了）：一个「按了等于
// 没换歌」的按钮比序号更没用。
func TestThePlayingRowDoesNotOfferAPlayButton(t *testing.T) {
	rec := &recorded{}
	songs := onlineSongs(4)
	_, tt := newList(t, playingActions(rec, songs[1].Item.Identity()), songs)

	hoverOn(t, tt, songs[1].Title)
	if shows(tt, "播放") {
		t.Errorf("正在放的那一行也摆出了播放按钮：%q", tt.Texts())
	}
	// 别的行照摆。
	hoverOn(t, tt, songs[2].Title)
	if !shows(tt, "播放") {
		t.Errorf("别的行没有摆出播放按钮：%q", tt.Texts())
	}
}
