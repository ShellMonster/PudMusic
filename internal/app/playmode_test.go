// 播放模式的四条断言：**四档各说清「一首歌放完之后下一首是哪首」**。
//
// 全部量在 `Queue` 这一层，而不是 App：这一层是纯数据（不碰播放器、不等 IO），
// 所以「顺序放完停在队尾」「单曲放完还是它」这些能一次一次地钉死，而不用等一首
// 真放完四秒钟。
//
// 判据就是任务面那句话本身——**放完下一首是哪首**。所以每个用例都跑一整串
// Advance（连着放完好几首），看每一首放完之后落到谁身上；只钉「放一次」的话，
// 「第二首放完之后呢」就没人管了，而顺序/循环两档的差别正好在那儿。
package app_test

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// fixedRand 是「每次都给出同一个数」的随机源。
//
// 它是「注入随机源」这半边本身：同一个源必然挑出同一串歌。生产那一路用的是按
// 时间播种的 `rand.Rand`，所以**换一台机器、隔一天再跑，随机那一档挑出来的歌
// 也不一样**——而测试这一路永远一样。
type fixedRand struct{ n int }

func (r *fixedRand) Intn(n int) int { return r.n % n }

// steppingRand 是「一次给一个、下一次给下一个」的随机源，第 k 次给 k%n。
//
// 它把抽签的那一步变成**可以逐个断言**的：给了确定的源，「这一档下一首是 b」
// 就不再是「大概是 b」，而是一个确定的事实。
type steppingRand struct{ calls int }

func (r *steppingRand) Intn(n int) int {
	v := r.calls % n
	r.calls++
	return v
}

// 走 n 步，每一步记下「这一首放完之后落到谁」。
//
// 第二个返回值是每一步的结论（停下 / 重放 / 放下一首），断「是不是停」的时候
// 用得上。
func playOut(q *app.Queue, n int) (titles []string, kinds []app.AdvanceKind) {
	for range n {
		d := q.Advance()
		kinds = append(kinds, d.Kind)
		switch d.Kind {
		case app.AdvanceStop:
			// 停：把这一轮记下来，免得后面几次的「还在原地」被当成「停了」。
			titles = append(titles, "<停>")
		case app.AdvanceReplay:
			it, _ := q.Current()
			titles = append(titles, it.Title)
		case app.AdvancePlay:
			titles = append(titles, d.Item.Title)
		}
	}
	return titles, kinds
}

// at 把队列开到第 i 首（0 起）。
//
// 绕两步而不是一步：刚 Append 完 cur 还是 -1，而 Move 对 -1 的语义是「正向落
// 在第一首」（见 Queue.Move），所以先落到 0 再往后挪 i 格。
func at(q *app.Queue, i int) {
	q.Move(1)
	q.Move(i)
}

// ---- 顺序播放 ----

// TestSequenceWalksTheListAndStopsAtTheEnd 盯顺序播放那一档。
//
// 两件事，缺一件都算坏：
//
//   - 从头往后一格一格走（这是「顺序」）；
//   - **走到最后一首就停，不绕回第一首**——这一句才是它和「循环」的分界，
//     而它最容易写成循环：绕回去的话两档就成了同一档，而用户点「顺序播放」
//     通常正是为了听完这一串就停。
func TestSequenceWalksTheListAndStopsAtTheEnd(t *testing.T) {
	q := app.NewQueue()
	q.SetPlayMode(app.PlayModeSequence)
	q.Append(app.LocalItem("a.mp3"), app.LocalItem("b.mp3"), app.LocalItem("c.mp3"))
	at(q, 0) // 从 a 开始

	got, kinds := playOut(q, 4)
	want := []string{"b", "c", "<停>", "<停>"}
	if len(got) != len(want) {
		t.Fatalf("走了 %d 步，want %d 步：%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序播放走了 %d 步 = %q，want %q", len(got), got, want)
		}
	}
	// 停下来时当前项还指着最后一首，不是回到第一首。
	if last := kinds[len(kinds)-1]; last != app.AdvanceStop {
		t.Errorf("最后一首放完之后的结论是 %v，want AdvanceStop", last)
	}
	cur, _ := q.Current()
	if cur.Title != "c" {
		t.Errorf("停下来的时候当前项是 %q，want c（停不等于回到第一首）", cur.Title)
	}
}

// ---- 循环播放（列表循环） ----

// TestLoopWrapsFromTheLastBackToTheFirst 盯循环播放那一档。
//
// 这是**列表循环**：一格一格往后，**走到队尾绕回第一首**，一直不停。
//
// ⚠️ 别和单曲循环混起来：那是「单曲播放」那一档，放完还是同一首。两条分开是
// Biu 的做法（`audio.tsx` 里 Loop 和 Single 是两个枚举值），也是用户按那个
// 按钮时脑子里的两件事——「别停」和「别跳」。
func TestLoopWrapsFromTheLastBackToTheFirst(t *testing.T) {
	q := app.NewQueue()
	q.SetPlayMode(app.PlayModeLoop)
	q.Append(app.LocalItem("a.mp3"), app.LocalItem("b.mp3"), app.LocalItem("c.mp3"))
	at(q, 0)

	got, _ := playOut(q, 6)
	want := []string{"b", "c", "a", "b", "c", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("循环播放走了 6 步 = %q，want %q", got, want)
		}
	}
}

// TestLoopWithOneSongReplaysIt 盯队列里只有一首歌时的循环。
//
// 绕回的还是它，那就不该走「换下一首」这条路：换了等于**重新打开一遍那个文件**
// （多一次解码、多一次句柄来回），而用户要的是同一首从头再来。
func TestLoopWithOneSongReplaysIt(t *testing.T) {
	q := app.NewQueue()
	q.SetPlayMode(app.PlayModeLoop)
	q.Append(app.LocalItem("only.mp3"))
	at(q, 0)

	d := q.Advance()
	if d.Kind != app.AdvanceReplay {
		t.Fatalf("只有一首歌时循环的结论是 %v，want AdvanceReplay", d.Kind)
	}
	if d.Item.Title != "only" {
		t.Errorf("重放的是 %q，want only", d.Item.Title)
	}
}

// ---- 随机播放 ----

// TestRandomReallyShuffles 盯随机那一档**真的打乱**。
//
// 三件事，缺一件都算坏：
//
//   - 抽出来的是**另一首**，不是当前这一首。图省事写成「往后随机若干首」
//     （`cur+1+rand(n-1)` 再对 n 取模）的话它**抽得到当前这一首**——连着放两遍
//     同一首，用户第一反应是「卡住了」，而随机这一档最该保证的就是这件事不会发生。
//   - **每一首都一样可能被抽中**：在除掉当前首剩下的 n-1 首里抽，而不是在
//     整列里抽。后者会让当前这一首比别的歌多一次机会，于是有一首会被反复选中。
//   - **抽得到第一首**（下面那条固定的抽取序列里它出现了两次）。
//
// 这一条用的是注入的固定源，所以那一串「下一首是谁」是**确定的**：它不只是
// 「跑到过所有歌」，而是逐个钉住了从当前下标映射到被抽中下标的那个算法。生产
// 那一路按时间播种，所以用户每次听到的乱序都不一样，而这一条永远绿。
func TestRandomReallyShuffles(t *testing.T) {
	q := app.NewQueueWithRand(&steppingRand{})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("a.mp3", "b.mp3", "c.mp3", "d.mp3")...)
	at(q, 0)

	got, _ := playOut(q, 8)
	want := []string{"b", "c", "d", "a", "c", "d", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("抽了 %d 次 = %q，want %d 次", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("抽了 %d 次 = %q，want %q", len(got), got, want)
		}
	}

	// 顺带把「连着放两遍同一首」和「抽得到第一首」各钉一遍：上面那条序列已经
	// 含着这两件事，但各报一次错的话，坏了的时候能一眼看出坏的是哪一件。
	seen := map[string]bool{}
	prev := "a"
	for i, title := range got {
		if title == prev {
			t.Errorf("第 %d 次抽到了 %q，而当前那首就是它——随机不能连着放两遍同一首", i, title)
		}
		seen[title] = true
		prev = title
	}
	if !seen["a"] {
		t.Errorf("第一首一次都没被抽中（抽到过 %v）", seen)
	}
	if len(seen) != 4 {
		t.Errorf("只抽到过 %d 首（%v），want 四首都抽到过", len(seen), seen)
	}
}

// TestRandomWithOneSongReplaysIt 盯随机 + 只有一首歌：没有第二首可抽，重放。
func TestRandomWithOneSongReplaysIt(t *testing.T) {
	q := app.NewQueueWithRand(&fixedRand{n: 0})
	q.SetPlayMode(app.PlayModeRandom)
	q.Append(items("only.mp3")...)
	at(q, 0)

	if d := q.Advance(); d.Kind != app.AdvanceReplay || d.Item.Title != "only" {
		t.Fatalf("只有一首歌时随机的结论是 %v/%q，want AdvanceReplay/only", d.Kind, d.Item.Title)
	}
}

// ---- 单曲播放 ----

// TestSingleReplaysTheSameSongAndNeverMoves 盯单曲播放那一档。
//
// 走五步，每一步都还得是**当前这一首**，而且一次都不许挪位置（`CurrentIndex`
// 一路不变）。挪了位置的话抽屉的高亮会跟着跑到另一首上，而响着的还是原来
// 那首——界面上两处各说各话。
func TestSingleReplaysTheSameSongAndNeverMoves(t *testing.T) {
	q := app.NewQueue()
	q.SetPlayMode(app.PlayModeSingle)
	q.Append(app.LocalItem("a.mp3"), app.LocalItem("b.mp3"), app.LocalItem("c.mp3"))
	at(q, 1) // 从 b 开始：不是第一首，才看得出它没有往前挪

	start := q.CurrentIndex()
	for k := range 5 {
		d := q.Advance()
		if d.Kind != app.AdvanceReplay {
			t.Fatalf("第 %d 步的结论是 %v，want AdvanceReplay", k, d.Kind)
		}
		if d.Item.Title != "b" {
			t.Fatalf("第 %d 步重放的是 %q，want b（单曲播放一步都不挪）", k, d.Item.Title)
		}
		if now := q.CurrentIndex(); now != start {
			t.Fatalf("第 %d 步当前项从 %d 挪到了 %d——单曲播放不许挪位置", k, start, now)
		}
	}
}

// ---- 随机时保持分集顺序 ----

// page 造一个分集视频里的一项（第几集 / 共几集）。
func page(bvid string, idx, total int) app.Item {
	return app.Item{Source: "online", Type: "mv", Bvid: bvid, Cid: bvid + "-p" + string(rune('0'+idx)),
		Title: bvid + " 第" + string(rune('0'+idx)) + "集", PageIndex: idx, TotalPage: total}
}

// TestRandomKeepsThePageOrderWhenAskedTo 盯「随机时保持分集顺序」那个开关。
//
// 开着的意思（抄 Biu 的 `next()` 随机分支开头那几句）：当前这首是分集视频的
// 某一集、而且**不是最后一集**时，接着放**同一个视频的下一集**，不洗牌。
// 用户在听第三集，下一首跳到别的视频去，他一眼就看出来了。
//
// 三种情形各钉一次：开着 → 跟着分集走；关掉 → 立刻回到真的随机；开着但已经
// 是最后一集 → 也回到真的随机（后面没有下一集可跟）。
func TestRandomKeepsThePageOrderWhenAskedTo(t *testing.T) {
	build := func() *app.Queue {
		q := app.NewQueueWithRand(&fixedRand{n: 0})
		q.SetPlayMode(app.PlayModeRandom)
		q.SetKeepPagesOrderInRandom(true)
		q.Append(
			page("BV1x", 1, 3), // 别的视频的第一集（随机时可能被抽到）
			page("BV2y", 1, 3),
			page("BV2y", 2, 3),
			page("BV2y", 3, 3),
			page("BV3z", 1, 2),
		)
		at(q, 1) // 现在在 BV2y 第一集
		return q
	}

	q := build()
	if d := q.Advance(); d.Item.Bvid != "BV2y" || d.Item.PageIndex != 2 {
		t.Errorf("开着的时候第一集放完落到 %s 第%d集，want BV2y 第2集", d.Item.Bvid, d.Item.PageIndex)
	}
	if d := q.Advance(); d.Item.Bvid != "BV2y" || d.Item.PageIndex != 3 {
		t.Errorf("第二集放完落到 %s 第%d集，want BV2y 第3集", d.Item.Bvid, d.Item.PageIndex)
	}
	// 已经是最后一集了：后面没有可跟的，回到真的随机。
	if d := q.Advance(); d.Kind != app.AdvancePlay || d.Item.PageIndex == 4 {
		t.Errorf("最后一集放完落到 %s 第%d集，want 真随机（后面没有下一集可跟）", d.Item.Bvid, d.Item.PageIndex)
	}

	// 关掉：立刻回到真的随机（跟着分集走的那两步不再发生）。
	q = build()
	q.SetKeepPagesOrderInRandom(false)
	if d := q.Advance(); d.Item.Bvid == "BV2y" && d.Item.PageIndex == 2 {
		t.Error("关掉之后还是跟着分集走了——这个开关没生效")
	}
}

// ---- 档位本身 ----

// TestTheFourModesAreInBiuOrder 盯四档的档位和文案。
//
// 档序决定「播放条上按几下能转回同一档」，文案决定用户读不读得懂——两样都
// 照 Biu 的 `common/constants/audio.tsx:27-48`（PlayMode 枚举 + getPlayModeList）。
func TestTheFourModesAreInBiuOrder(t *testing.T) {
	wantMode := []app.PlayMode{app.PlayModeSequence, app.PlayModeLoop, app.PlayModeRandom, app.PlayModeSingle}
	wantDesc := []string{"顺序播放", "循环播放", "随机播放", "单曲播放"}

	if len(app.PlayModes) != len(wantMode) {
		t.Fatalf("有 %d 档，want 4 档", len(app.PlayModes))
	}
	for i, m := range wantMode {
		if app.PlayModes[i] != m {
			t.Errorf("第 %d 档是 %q，want %q", i, app.PlayModes[i], m)
		}
		if got := m.Desc(); got != wantDesc[i] {
			t.Errorf("%q 的文案是 %q，want %q", m, got, wantDesc[i])
		}
		if m.Label() == "" || m.Label() == m.Desc() {
			t.Errorf("%q 按钮上的短标签是 %q：要么是空的，要么和完整那句一样（那样就没省下宽度）",
				m, m.Label())
		}
	}

	// 按一下走一格，第四档之后回到第一档。
	m := app.PlayModeSequence
	for i := range 3 {
		next := m.Next()
		if next != wantMode[i+1] {
			t.Fatalf("从 %q 按一下是 %q，want %q", m, next, wantMode[i+1])
		}
		m = next
	}
	if last := app.PlayModeSingle.Next(); last != app.PlayModeSequence {
		t.Errorf("从最后一档按一下是 %q，want 回第一档", last)
	}
}

// TestAnUnknownModeFallsBackInsteadOfSayingNothing 盯认不出的档位。
//
// 两个方向都要：界面上不能是一个**没有字的按钮**（空按钮不报错、不消失，只是
// 「按下去不知道换成了什么」），行为上也不能掉进 `Advance` 的 default 那一支
// （那是循环）——那会出现「按钮上是空的，行为是循环」。
func TestAnUnknownModeFallsBackInsteadOfSayingNothing(t *testing.T) {
	if got := app.PlayMode("").Label(); got != app.PlayModeLoop.Label() {
		t.Errorf("空档位的短标签是 %q，want %q", got, app.PlayModeLoop.Label())
	}
	if got := app.PlayMode("nonsense").Desc(); got != app.PlayModeLoop.Desc() {
		t.Errorf("认不出的档位的文案是 %q，want %q", got, app.PlayModeLoop.Desc())
	}

	q := app.NewQueue()
	q.SetPlayMode("nonsense")
	if got := q.PlayMode(); got != app.DefaultPlayMode {
		t.Errorf("设了认不出的档位之后读到的是 %q，want %q", got, app.DefaultPlayMode)
	}
}

// TestSwitchingTheModeTakesEffectAtOnce 盯「换档不用重开歌」。
//
// 硬要求那一条。做法是**在同一个队列实例上**连着换档，每换一次立刻问下一首
// 是谁——中间不重新建队列、不重新灌歌、不碰播放器。
func TestSwitchingTheModeTakesEffectAtOnce(t *testing.T) {
	q := app.NewQueue()
	q.Append(app.LocalItem("a.mp3"), app.LocalItem("b.mp3"), app.LocalItem("c.mp3"))
	at(q, 0)

	// 顺序：c 放完就停。
	q.SetPlayMode(app.PlayModeSequence)
	q.Advance() // → b
	q.Advance() // → c
	if d := q.Advance(); d.Kind != app.AdvanceStop {
		t.Fatalf("顺序模式下 c 放完的结论是 %v，want AdvanceStop", d.Kind)
	}
	// **同一个队列**，切到循环，再问一次同样的问题：这一次要绕回 a。
	q.SetPlayMode(app.PlayModeLoop)
	q.SetPlayMode(app.PlayModeSequence)
	q.SetPlayMode(app.PlayModeLoop)
	if d := q.Advance(); d.Kind != app.AdvancePlay || d.Item.Title != "a" {
		t.Fatalf("切到循环之后 c 放完落到 %q（%v），want a", d.Item.Title, d.Kind)
	}
	// 再切到单曲：同一个位置，同一个问题，答案变成「还是它」。
	q.SetPlayMode(app.PlayModeSingle)
	if d := q.Advance(); d.Kind != app.AdvanceReplay || d.Item.Title != "a" {
		t.Fatalf("切到单曲之后放完的结论是 %v/%q，want AdvanceReplay/a", d.Kind, d.Item.Title)
	}
}

// TestAdvanceOnAnEmptyOrUnstartedQueueStops 盯两个边界。
//
// 空队列、以及「还没开始放」（cur 是 -1）都没有「下一首」这件事可决定：两种都
// 是停下来，而不是去取第 0 项或者第 -1 项——后者会 panic，而前者会让界面在
// 什么都没有的时候以为「有一首，只是放完了」。
func TestAdvanceOnAnEmptyOrUnstartedQueueStops(t *testing.T) {
	if d := app.NewQueue().Advance(); d.Kind != app.AdvanceStop {
		t.Errorf("空队列的结论是 %v，want AdvanceStop", d.Kind)
	}

	for _, m := range app.PlayModes {
		q := app.NewQueue()
		q.SetPlayMode(m)
		q.Append(app.LocalItem("a.mp3"))
		// Append 不动 cur，所以现在还是「还没开始」。
		if d := q.Advance(); d.Kind != app.AdvanceStop {
			t.Errorf("%s 档下「还没开始」时的结论是 %v，want AdvanceStop", m, d.Kind)
		}
	}
}
