package app_test

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 随机模式下的**历史栈**（票 #322 的 B4）：「上一首」回到**刚才放过的那首**，
// 而不是按索引退一格。
//
// 为什么按索引退一格是错的：随机模式下用户听到的次序和队列的次序是两件事
// （队列是 [甲乙丙丁]，随机放的次序可能是 丙甲丁乙）。按索引退一格退回去的
// 是队列里的上一项，可能根本没放过——用户按「上一首」，听到的是一首他刚才
// 没听过的歌，而按钮上写着「上一首」。
//
// 抄的是 MP2 的 `m_random_list`（`Player.cpp:816-851`）：一列放过的歌，倒着
// 退回来用。

// TestRandomPreviousGoesBackToTheSongJustPlayed 盯最要紧的那一条：
// **随机模式下按「上一首」回到刚才那首**。
//
// 「刚才那首」= 正在放的这首**之前**放过的那首：放完甲放乙，乙在响的时候按
// 「上一首」，要回到的是甲（用户上一秒还在听的那首），不是乙自己。
//
// 抄的是 MP2 的 `m_random_list`：一列放过的歌，倒着退回来用。
func TestRandomPreviousGoesBackToTheSongJustPlayed(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0) // 现在在 a

	// 放两次：随机挑的那两首。
	q.Advance()
	mid, _ := q.Current()
	q.Advance()

	// 按「上一首」：回到刚才放过的那首（也就是 mid）。
	back, ok := q.MovePrev()
	if !ok {
		t.Fatal("按上一首没反应")
	}
	if back.Title != mid.Title {
		t.Errorf("上一首是 %q，want %q（刚才放过的那首）", back.Title, mid.Title)
	}
	now, _ := q.Current()
	if now.Title != back.Title {
		t.Errorf("当前项是 %q，want %q", now.Title, back.Title)
	}
}

// TestRandomPreviousWalksBackThroughTheHistory 盯历史栈是**一列**，不是一格：
// 连着按「上一首」要一路退回去。
func TestRandomPreviousWalksBackThroughTheHistory(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0)

	q.Advance()
	q.Advance()
	q.Advance()

	var got []string
	for range 3 {
		it, ok := q.MovePrev()
		if !ok {
			t.Fatalf("按了 %d 次上一首之后没反应了", len(got))
		}
		got = append(got, it.Title)
	}
	// 退回去的次序是倒着来的：三首都不许重复。
	seen := map[string]bool{}
	for _, title := range got {
		if seen[title] {
			t.Errorf("退回去的次序是 %q，里面 %q 出现了两次", got, title)
		}
		seen[title] = true
	}
	if len(seen) != 3 {
		t.Errorf("退回去 %d 次只退到了 %d 首（%q），want 三首", len(got), len(seen), got)
	}
}

// TestThePreviousButtonIsNotRandom 盯**反面教材**（报告里点名 Nuclear）：
// 随机模式下的「上一首」**不能是随机的**。
//
// Nuclear 的随机模式里按「上一首」也是随机挑一首——用户连按三次「上一首」，
// 听到的是三首**不一样的**歌，而按钮上写着「上一首」。
//
// 我们这里按下去听到的是**放过的次序倒着来**：放过的三首是 p1 p2 p3（p3 在响），
// 那连按三次「上一首」听到的就是 p2、p1、开场那首——**和放过的次序对得上**。
func TestThePreviousButtonIsNotRandom(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0) // 现在在 a（开场那首）

	var played []string
	for range 3 {
		q.Advance()
		cur, _ := q.Current()
		played = append(played, cur.Title)
	}

	// 退回去的次序：p2、p1、开场那首。
	want := []string{played[1], played[0], "a"}
	for i, w := range want {
		it, ok := q.MovePrev()
		if !ok {
			t.Fatalf("第 %d 次按上一首没反应", i+1)
		}
		if it.Title != w {
			t.Errorf("第 %d 次上一首是 %q，want %q（上一首不能是随机的）", i+1, it.Title, w)
		}
	}
}

// TestThePreviousButtonIsByIndexOutsideRandom 盯**别的档位不受影响**。
//
// 顺序 / 循环 / 单曲三档的「上一首」还是按索引退一格（两端绕回）——历史栈只
// 在随机模式下有读者。
func TestThePreviousButtonIsByIndexOutsideRandom(t *testing.T) {
	for _, mode := range []app.PlayMode{app.PlayModeSequence, app.PlayModeLoop, app.PlayModeSingle} {
		q := app.NewQueueWithRand(&steppingRand{})
		q.SetPlayMode(mode)
		q.Append(items("a.mp3", "b.mp3", "c.mp3")...)
		at(q, 1) // 现在在 b

		it, ok := q.MovePrev()
		if !ok {
			t.Fatalf("%s：按上一首没反应", mode)
		}
		if it.Title != "a" {
			t.Errorf("%s：上一首是 %q，want a（按索引退一格）", mode, it.Title)
		}
	}
}

// TestThePreviousButtonFallsBackWhenThereIsNoHistory 盯历史栈是空的时**退回按
// 索引退一格**，而不是「按了没反应」。
//
// 刚切到随机模式、还没放过任何一首的时候，历史栈就是空的。用户按「上一首」
// 什么都不发生的话，他只知道按钮坏了。
func TestThePreviousButtonFallsBackWhenThereIsNoHistory(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3")...)
	at(q, 1) // 现在在 b

	it, ok := q.MovePrev()
	if !ok {
		t.Fatal("历史栈是空的，按上一首却没反应")
	}
	if it.Title != "a" {
		t.Errorf("上一首是 %q，want a（退回按索引退一格）", it.Title)
	}
}

// TestSwitchingModeForgetsTheHistory 盯**换档就清掉**历史栈。
//
// 用户从随机切回循环、再切回随机，应该是一轮新的随机——而不是接着上一轮
// 继续退（那时候队列里的歌可能已经换了）。
func TestSwitchingModeForgetsTheHistory(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0)
	q.Advance()

	q.SetPlayMode(app.PlayModeLoop)
	q.SetPlayMode(app.PlayModeRandom)

	// 历史栈清空了：按「上一首」退回按索引退一格，而不是退回到随机放过的那首。
	it, ok := q.MovePrev()
	if !ok {
		t.Fatal("按上一首没反应")
	}
	cur, _ := q.Current()
	if it.Title != cur.Title {
		t.Errorf("上一首是 %q，而当前项是 %q——它们对不上", it.Title, cur.Title)
	}
}

// TestTheShuffleBagSurvivesAReorder 盯「**袋子认的是身份，不是下标**」。
//
// 重排之后袋子**不清掉**：它记的是判重标识（`Item.Identity`），而重排改的只是
// **顺序**——歌还是那几首，身份没变。
//
// 清掉的话这一轮会重新开始，用户会听到刚放过的歌又来一遍——而随机那一档最该
// 保证的就是「一列里一首不重复」。
func TestTheShuffleBagSurvivesAReorder(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0)

	// 发出这一列的第一首。
	first := q.Advance()
	if first.Kind != app.AdvancePlay {
		t.Fatalf("结论是 %v，want AdvancePlay", first.Kind)
	}

	// 重排：把 d 挪到 a 前面（歌还是那四首，只是顺序变了）。
	if !q.MoveSongBefore("local:d.mp3", "local:a.mp3") {
		t.Fatal("挪不动")
	}

	// 这一列剩下的三首还没轮到：重排之后也不能重复刚放过的那首。
	for range 3 {
		d := q.Advance()
		if d.Kind != app.AdvancePlay {
			t.Fatalf("结论是 %v，want AdvancePlay", d.Kind)
		}
		if d.Item.Title == first.Item.Title {
			t.Errorf("重排之后又放了一遍 %q——袋子被清掉、这一轮重新开始了", d.Item.Title)
		}
	}
}

// TestTheHistorySurvivesAReorder 盯「**历史栈认的是身份，不是下标**」。
//
// 重排之后按「上一首」还是退回到刚才放过的那首——历史栈记的是判重标识，而
// 重排改的只是顺序。
func TestTheHistorySurvivesAReorder(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0)

	// Advance 之前在放的那首——「上一首」要退回到它。
	before, _ := q.Current()
	q.Advance()

	if !q.MoveSongBefore("local:d.mp3", "local:a.mp3") {
		t.Fatal("挪不动")
	}

	back, ok := q.MovePrev()
	if !ok {
		t.Fatal("重排之后按上一首没反应")
	}
	if back.Title != before.Title {
		t.Errorf("上一首是 %q，want %q（重排不该把历史栈弄丢）", back.Title, before.Title)
	}
}

// ---- 袋子是池子：新加进来的歌这一轮就轮到它（票 #387）----
//
// 这一组盯的是**随机那一档的池子语义**（见 queue_shuffle.go 文件头）：随机不是
// 「排好一张表」，是「下一首从还没放过的里面随机挑」，而**新加进队列的歌立刻
// 就在池子里**（Spotify / Apple / QQ音乐 / 网易云都是这个做法）。

// TestRandomPlaysASongAddedMidRound 盯「**这一轮中途加进队列的歌，这一轮就轮到
// 它**」。
//
// 洗牌袋原来是**轮开始时的快照**——中途加进来的歌不在袋子里，要等下一轮重装
// 才轮到（而票面写的是「随机不重复、不遗漏」，那句话对中途加的歌不成立）。
//
// 判据不是「它排在第几个」：它插在池子里的位置是随机的（下一首就有可能抽到
// 它），所以钉的是「**这一轮里它被轮到，而且只轮到一次**」。
func TestRandomPlaysASongAddedMidRound(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0)

	// 发出这一轮的第一首（袋子里于是少了一首）。
	first := q.Advance()
	if first.Kind != app.AdvancePlay {
		t.Fatalf("结论是 %v，want AdvancePlay", first.Kind)
	}

	// 中途加一首歌。
	q.Append(items("e.mp3")...)

	// 这一轮剩下的四首：e 必须在里面，而且只出现一次。
	round := playTitles(t, q, 4)
	if got := countOf(round, "e"); got != 1 {
		t.Errorf("这一轮放了 %q，want 恰好一次（中途加进来的歌这一轮就该轮到它）", round)
	}
	if len(seenTitles(round)) != len(round) {
		t.Errorf("这一轮是 %q——里面有重复的歌（洗牌袋要保证一列里一首不重复）", round)
	}

	// 下一轮也一样：它**每一轮**都恰好轮到一次（不重复，也不遗漏）。
	next := playTitles(t, q, 5)
	if got := countOf(next, "e"); got != 1 {
		t.Errorf("下一轮放了 %q，want e 恰好一次", next)
	}
}

// TestRandomPlayNextIsTheNextOne 盯「**下一首播放压过随机**」。
//
// 随机档的「下一首」是洗牌袋说了算，而「下一首播放」是**用户显式的意图**——它
// 压过随机（Spotify 社区管这叫 "queue overrides"，见 queue_shuffle.go 文件头）。
//
// 改之前它在随机档是**完全失效**的：歌插进了队列，可下一首还是池子里随机抽的
// 另一首，用户按了跟没按一样（症状是「点了没反应」）。
func TestRandomPlayNextIsTheNextOne(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0)
	q.Advance() // 这一轮已经发出了一首

	q.InsertNext(app.LocalItem("e.mp3"))

	next, ok := q.MoveNext()
	if !ok {
		t.Fatal("按下一首没反应")
	}
	if next.Title != "e" {
		t.Errorf("下一首是 %q，want e（「下一首播放」压过随机）", next.Title)
	}
}

// TestRandomPlayNextIsTheNextOneOnAutoAdvance 同上一条，但走**自动切歌**那条路
// （一首歌放完了接着放哪首）。
//
// 两条路都得认「下一首播放」——它们读的是同一个袋子（见 `Queue.Advance` 和
// `Queue.MoveNext`）。
func TestRandomPlayNextIsTheNextOneOnAutoAdvance(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0)
	q.Advance()

	q.InsertNext(app.LocalItem("e.mp3"))

	d := q.Advance()
	if d.Kind != app.AdvancePlay {
		t.Fatalf("结论是 %v，want AdvancePlay", d.Kind)
	}
	if d.Item.Title != "e" {
		t.Errorf("放完这一首之后是 %q，want e（「下一首播放」压过随机）", d.Item.Title)
	}
}

// TestPlayNextOutsideRandomIsStillByIndex 盯「**顺序 / 循环 / 单曲三档一个字都
// 不变**」。
//
// 那一档的「下一首」是纯按索引算的：插在当前那一首后面的就是下一首；而「添加到
// 队列」接在队尾——它不改变下一首是谁。
//
// ⚠️ 这是**回归的护栏**：袋子是随机档的东西，而那一档的行为一个字都不许动。
func TestPlayNextOutsideRandomIsStillByIndex(t *testing.T) {
	for _, mode := range []app.PlayMode{app.PlayModeSequence, app.PlayModeLoop, app.PlayModeSingle} {
		q := app.NewQueueWithRand(&steppingRand{})
		q.SetPlayMode(mode)
		q.Append(items("a.mp3", "b.mp3", "c.mp3")...)
		at(q, 0)

		// 「下一首播放」：下一首就是它。
		q.InsertNext(app.LocalItem("x.mp3"))
		next, ok := q.MoveNext()
		if !ok {
			t.Fatalf("%s：按下一首没反应", mode)
		}
		if next.Title != "x" {
			t.Errorf("%s：下一首是 %q，want x", mode, next.Title)
		}

		// 「添加到队列」：接在队尾，下一首不变（那一档下一首是按索引算的）。
		q.Append(items("z.mp3")...)
		after, ok := q.MoveNext()
		if !ok {
			t.Fatalf("%s：加了一首之后按下一首没反应", mode)
		}
		if after.Title != "b" {
			t.Errorf("%s：加了一首之后下一首是 %q，want b（那一档下一首是按索引算的）", mode, after.Title)
		}
	}
}

// TestTheEnqueueNoticeIsOnlyForRandom 盯「**只有随机档才说那句话**」。
//
// 随机档的袋子是池子：加进来的歌这一轮就轮到它——而用户看不见池子里有什么，
// 所以得说一声（见 `PlayMode.EnqueueNotice`）。
//
// 其余三档是空串：那一档的下一首是按索引算的，加进来的歌排在哪就是哪，没有
// 什么要解释的。
func TestTheEnqueueNoticeIsOnlyForRandom(t *testing.T) {
	if n := app.PlayModeRandom.EnqueueNotice(); n == "" {
		t.Error("随机档加歌时没有话说——用户不知道这一轮就轮到它")
	}
	for _, m := range []app.PlayMode{app.PlayModeSequence, app.PlayModeLoop, app.PlayModeSingle} {
		if n := m.EnqueueNotice(); n != "" {
			t.Errorf("%s 档说了 %q，want 空串", m, n)
		}
	}
}

// playTitles 连着放 n 首，把放出来的歌名收成一列。
func playTitles(t *testing.T, q *app.Queue, n int) []string {
	t.Helper()
	out := make([]string, 0, n)
	for range n {
		d := q.Advance()
		if d.Kind != app.AdvancePlay {
			t.Fatalf("第 %d 首的结论是 %v，want AdvancePlay", len(out)+1, d.Kind)
		}
		out = append(out, d.Item.Title)
	}
	return out
}

// countOf 数一列里某首歌出现了几次。
func countOf(titles []string, want string) int {
	n := 0
	for _, t := range titles {
		if t == want {
			n++
		}
	}
	return n
}

// seenTitles 收一列里各不相同的那几个（重复的只留一份）。
func seenTitles(titles []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(titles))
	for _, t := range titles {
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
