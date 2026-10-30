package list

import (
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
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

// TestTheSameRecordingInTwoRowsIsOneHighlight 钉住判据是**同一首**而不是同一份。
//
// 历史记录那一页同一首听过两次是**两行**（`Music.ID` 不同，那是铁律四要的），
// 而它们在 B 站那边是同一首**同一集**。认「同一首」时两行都亮——那是对的：用户
// 点的、听的都是同一首，而另一份只是它自己那份记录。
//
// 认「同一份」（比整个 `Item`）的话两行都不亮，于是用户在一份**正在放**的
// 记录上看到的是「哪首都没在放」。
//
// 🔴 **票 #683 之后「同一首」带上 cid 了**：同一个投稿的**两集**不再是同一首
// （那是两个独立的行、两个独立的队列项）。所以这一条改成造**同一个 cid** 的两行
// ——「同一集听过两次」，那才是这条判据真正要覆盖的形状；两集各亮一行由下面
// 那一条 `TestTwoEpisodesOfOneVideoAreNotTheSameSong` 钉。
func TestTheSameRecordingInTwoRowsIsOneHighlight(t *testing.T) {
	rec := &recorded{}
	a := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "同一集"}
	songs := []Music{
		{ID: "history:BV1@1", Title: "看过的第一次", Item: a},
		{ID: "history:BV1@2", Title: "看过的第二次", Item: a},
	}
	_, tt := newList(t, playingActions(rec, a.Identity()), songs)

	if !markedOn(t, tt, "看过的第一次") {
		t.Errorf("正在放的那份没有被标出来：%q", tt.Texts())
	}
	if !markedOn(t, tt, "看过的第二次") {
		t.Errorf("同一集的两条记录只亮了一行（那是同一首，只是 Music.ID 不同）：%q", tt.Texts())
	}
}

// TestTwoEpisodesOfOneVideoAreNotTheSameSong 钉住票 #683 的行为变化。
//
// 历史记录里同一个投稿的第一集和第二集是**两行**，而只有正在放的那一行该亮。
// 判重不认 cid 的那些年，这里两行都亮——用户看到「两集都在放」，而真正出声的
// 只有一集；同一个洞还让「点第二集」变成了「什么都不做」。
func TestTwoEpisodesOfOneVideoAreNotTheSameSong(t *testing.T) {
	rec := &recorded{}
	first := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "第一集"}
	second := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "2", Title: "第二集"}
	songs := []Music{
		{ID: "history:BV1@1", Title: "看过的第一集", Item: first},
		{ID: "history:BV1@2", Title: "看过的第二集", Item: second},
	}
	_, tt := newList(t, playingActions(rec, first.Identity()), songs)

	if !markedOn(t, tt, "看过的第一集") {
		t.Errorf("正在放的那一集没有被标出来：%q", tt.Texts())
	}
	if markedOn(t, tt, "看过的第二集") {
		t.Errorf("同一投稿的另一集也被标成「正在播放」了（只有一集在放）：%q", tt.Texts())
	}
	if n := countMarks(tt); n != 1 {
		t.Errorf("画面上有 %d 处「正在播放」，want 1 处：%q", n, tt.Texts())
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

// TestTheRowThatIsReallyPlayingIsMarked 端到端：**真的在放**的时候，那一行
// 真的有那个标记。
//
// ⚠️ 上面那几条走的是**假接线**（`playingActions` 自己说「哪首在放」），而这一条
// 走**真的接线**（`AppActions` → `App.IsPlaying`）和**真的播放器**——它量的是
// 「应用真的在放一首歌时，界面上真的有那个标记」，也就是票 #279 的另一半：
// 修好之后「没在放就不标」，而「在放**仍然**要标」。
//
// ⚠️ 它走系统音频输出：没有声卡时 MyGo 静音播放（照常走时间轴），而断言的是
// 「那一行有没有被标」，不依赖「听得见」。
func TestTheRowThatIsReallyPlayingIsMarked(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	path := realFLAC(t)
	if err := a.Open(path); err != nil {
		t.Fatalf("放不了那个文件：%v", err)
	}

	songs := []Music{{
		ID: "local:1", Title: "本地那一首",
		Item: app.LocalItem(path),
	}}
	_, tt := newList(t, AppActions(a, nil), songs)

	if !markedOn(t, tt, "本地那一首") {
		t.Errorf("真的在放的那一行没有被标成「正在播放」：%q", tt.Texts())
	}
	if n := countMarks(tt); n != 1 {
		t.Errorf("画面上有 %d 处「正在播放」，want 1 处：%q", n, tt.Texts())
	}
}
