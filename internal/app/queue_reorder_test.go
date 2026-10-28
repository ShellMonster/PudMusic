package app_test

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 队列的 **item 级重排**（票 #322 的 B5）。
//
// 这一组盯三件事：
//
//   - 挪的**顺序**对不对；
//   - **正在放的那首不错位**（Nuclear 的 currentIndex 三分支修正）；
//   - **挪的是那首歌的全部份数**（抽屉里同一首只画一行）。

// titles 是队列里此刻的歌名，按顺序。
func titles(q *app.Queue) []string {
	items := q.Items()
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

// sameOrder 断两列歌名是不是一模一样。
func sameOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestTheQueueCanBeReordered 盯最基本的那一条：**挪得动，顺序对**。
//
// 挪的是判重标识，所以调用方给的是「哪一首歌」和「挪到哪一首歌前面」，不是
// 下标——抽屉里同一首只画一行，下标对不上（见 queueRowsFor）。
func TestTheQueueCanBeReordered(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 1) // 现在在 b

	// 把 d 挪到 b 前面。
	if !q.MoveSongBefore("local:d.mp3", "local:b.mp3") {
		t.Fatal("挪不动：MoveSongBefore 说它没挪")
	}
	want := []string{"a", "d", "b", "c"}
	if got := titles(q); !sameOrder(got, want) {
		t.Errorf("挪完是 %q，want %q", got, want)
	}
}

// TestTheReorderMovesEveryCopyOfTheSong 盯「挪的是那首歌的全部份数」。
//
// 同一首在队列里排了两遍（用户从两个页面各加了一次），而抽屉里它**只画一行**
// （去重）。用户拖那一行，挪的是**那首歌**——只挪一份的话抽屉上那一行会留在
// 原地（去重留下的还是第一份），用户看到的是「拖了没反应」。
//
// 这一条钉的是「两份都到队尾去」：只挪一份的话队尾只有一份，另一份还在老地方。
func TestTheReorderMovesEveryCopyOfTheSong(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a.mp3", "b.mp3", "a.mp3", "c.mp3", "d.mp3")...)

	if !q.MoveSongBefore("local:a.mp3", "") {
		t.Fatal("挪不动：MoveSongBefore 说它没挪")
	}
	want := []string{"b", "c", "d", "a", "a"}
	if got := titles(q); !sameOrder(got, want) {
		t.Errorf("挪完是 %q，want %q（那一份也该跟着走）", got, want)
	}
}

// TestTheReorderKeepsThePlayingSongInPlace 盯**正在放的那首不错位**
// （Nuclear 的 currentIndex 三分支修正）。
//
// 三种情形各钉一次：
//
//	挪的就是它         → 指针落到它的新位置。
//	挪的是它前面那首   → 指针往前挪一格。
//	挪的是它后面那首   → 指针不动。
//
// 错了的症状是**静默**的：播放条上写着 A，抽屉里高亮的是 B，而响着的还是 A。
func TestTheReorderKeepsThePlayingSongInPlace(t *testing.T) {
	// 挪的就是正在放的那首。
	q := app.NewQueue()
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 2) // 现在在 c
	if !q.MoveSongBefore("local:c.mp3", "local:a.mp3") {
		t.Fatal("挪不动")
	}
	if got := titles(q); !sameOrder(got, []string{"c", "a", "b", "d"}) {
		t.Errorf("挪完是 %q，want [c a b d]", got)
	}
	cur, ok := q.Current()
	if !ok || cur.Title != "c" {
		t.Errorf("正在放的是 %q，want c（挪的就是它，指针要跟着它走）", cur.Title)
	}

	// 挪的是它**前面**那首：指针往前挪一格。
	q = app.NewQueue()
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 2) // 现在在 c
	if !q.MoveSongBefore("local:a.mp3", "local:d.mp3") {
		t.Fatal("挪不动")
	}
	if got := titles(q); !sameOrder(got, []string{"b", "c", "a", "d"}) {
		t.Errorf("挪完是 %q，want [b c a d]", got)
	}
	cur, ok = q.Current()
	if !ok || cur.Title != "c" {
		t.Errorf("正在放的是 %q，want c（别的歌挪走了，指针要跟着挪）", cur.Title)
	}

	// 挪的是它**后面**那首：指针不动。
	q = app.NewQueue()
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 1) // 现在在 b
	if !q.MoveSongBefore("local:d.mp3", "local:a.mp3") {
		t.Fatal("挪不动")
	}
	if got := titles(q); !sameOrder(got, []string{"d", "a", "b", "c"}) {
		t.Errorf("挪完是 %q，want [d a b c]", got)
	}
	cur, ok = q.Current()
	if !ok || cur.Title != "b" {
		t.Errorf("正在放的是 %q，want b（指针不动）", cur.Title)
	}
}

// TestTheReorderToTheEnd 盯「挪到队尾」那一条。
//
// before 给空串就是队尾（界面上的拖法是「拖到最后一行的下面」）。
func TestTheReorderToTheEnd(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a.mp3", "b.mp3", "c.mp3")...)
	at(q, 2) // 现在在 c

	if !q.MoveSongBefore("local:a.mp3", "") {
		t.Fatal("挪不动")
	}
	if got := titles(q); !sameOrder(got, []string{"b", "c", "a"}) {
		t.Errorf("挪完是 %q，want [b c a]", got)
	}
	cur, ok := q.Current()
	if !ok || cur.Title != "c" {
		t.Errorf("正在放的是 %q，want c", cur.Title)
	}
}

// TestTheReorderRefusesWhatItCannotDo 盯「挪不动的时候就不动」。
//
// 四种情形：认不出是哪一首、挪到自己前面、队列里没有那一首、目标不在队列里。
//
// 「挪动失败还把队列改了」比「挪不动」糟得多：用户看到的是顺序乱了一截，而
// 界面上没有任何一句话说「这一下没生效」。
func TestTheReorderRefusesWhatItCannotDo(t *testing.T) {
	q := app.NewQueue()
	q.Append(items("a.mp3", "b.mp3", "c.mp3")...)
	before := titles(q)

	if q.MoveSongBefore("", "local:a.mp3") {
		t.Error("认不出是哪一首的时候挪动了")
	}
	if q.MoveSongBefore("local:a.mp3", "local:a.mp3") {
		t.Error("挪到自己前面的时候挪动了")
	}
	if q.MoveSongBefore("local:zzz.mp3", "local:a.mp3") {
		t.Error("队列里没有的那一首挪动了")
	}
	if q.MoveSongBefore("local:a.mp3", "local:zzz.mp3") {
		t.Error("目标不在队列里的时候挪动了")
	}
	if got := titles(q); !sameOrder(got, before) {
		t.Errorf("挪不动之后队列变成了 %q，want %q（一个字节都不许动）", got, before)
	}
}
