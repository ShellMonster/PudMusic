package app

import (
	"testing"
)

// 队列这一层的**内部**测试（能碰那些没导出的方法）。
//
// 队列的导出方法只有那么几个，而「历史栈」那几样（洗牌袋、历史栈、退回去）
// 全是队列自己管的事——它们不用导出，所以只能在这儿量。

// fixedRand 是「每次都给出同一个数」的随机源。
//
// ⚠️ **和 playmode_test.go 里的那个不是同一个**：那一份在外部测试包
// （`app_test`）里，这里是内部测试包（`app`）——同一个包名只能有一份定义。
type fixedRand struct{ n int }

func (r *fixedRand) Intn(n int) int { return r.n % n }

// TestTheSameSongInTwoPlacesIsNotHistory 盯「**同一首的另一份不算放过一首歌**」。
//
// 队列里同一首排了两遍（用户从两个页面各加了一次），而抽屉里它**只画一行**
// （见 queueRowsFor 的去重）——画的是第一份，正在放的可能是第二份。
//
// 用户点那一行的时候，指针挪到**另一份**上，歌还是那一首。这时候不能把它压进
// 历史栈：压了的话「上一首」退回来的是同一首，用户会以为那个按钮坏了。
func TestTheSameSongInTwoPlacesIsNotHistory(t *testing.T) {
	q := NewQueueWithRand(&fixedRand{n: 0})
	q.SetPlayMode(PlayModeRandom)
	q.Append(
		LocalItem("a.mp3"),
		LocalItem("b.mp3"),
		LocalItem("a.mp3"), // 同一首的第二份
	)
	q.Move(1) // cur = 0
	q.Move(1) // cur = 1（b）

	// 跳到**另一首**（a 的第一份）：这一次算「放过 b」。
	if _, ok := q.selectAt(0); !ok {
		t.Fatal("挪不过去")
	}
	if len(q.history) != 1 {
		t.Fatalf("跳到另一首之后历史栈有 %d 项，want 1", len(q.history))
	}

	// 跳到**同一首的另一份**（a 的第二份）：这一次不算。
	if _, ok := q.selectAt(2); !ok {
		t.Fatal("挪不过去")
	}
	if len(q.history) != 1 {
		t.Errorf("跳到同一首的另一份之后历史栈有 %d 项，want 1"+
			"（同一首的另一份不算「放过一首歌」）", len(q.history))
	}
}

// TestReplacingTheEpisodeKeepsTheShuffleBagInStep 钉住「换集之后洗牌袋还认得那一格」。
//
// 袋子记的是**判重标识**。换集是就地改队列项的 cid（`replaceAt`），那一项的身份
// 从 `mv:BV1|111` 变成 `mv:BV1|555`——袋子不改的话，里面那一格还是个已经不存在的
// 身份：`indexOfIdentityLocked` 返回 -1，`bagNextLocked` 把它当「已经被删掉」跳过，
// 于是那一首这一轮**一轮都抽不到**（用户听到的是「随机播了一轮，有一首没放」）。
func TestReplacingTheEpisodeKeepsTheShuffleBagInStep(t *testing.T) {
	q := NewQueueWithRand(&fixedRand{n: 0})
	q.SetPlayMode(PlayModeRandom)
	episode := func(cid, title string) Item {
		return Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: cid, Title: title}
	}
	q.Append(
		episode("111", "第一集"),
		episode("222", "第二集"),
		Item{Source: "online", Type: "mv", Bvid: "BV2", Cid: "333", Title: "另一个投稿"},
	)
	q.Move(1) // cur = 0

	// 把袋子装起来（真播时是袋空那一拍重装的，这里直接调那一个）。
	q.mu.Lock()
	q.bagResetLocked()
	q.mu.Unlock()
	oldID, newID := episode("111", "").Identity(), episode("555", "").Identity()
	if !q.bagHasLocked(oldID) {
		t.Fatalf("装好的袋子里没有 %q：%q", oldID, q.bag)
	}

	// 换集：第 0 项从第 111 集换成第 555 集。
	if !q.replaceAt(0, episode("555", "第五集")) {
		t.Fatal("换集失败")
	}
	if !q.bagHasLocked(newID) {
		t.Fatalf("洗牌袋里没有换集之后的身份 %q（袋里是 %q）", newID, q.bag)
	}
	if q.bagHasLocked(oldID) {
		t.Errorf("洗牌袋里还留着旧身份 %q（袋里是 %q）", oldID, q.bag)
	}

	// 这一轮连着抽，三首都要抽到（旧身份那一格要是作废了，第 555 集永远轮不到）。
	seen := map[string]bool{}
	for range 3 {
		d := q.Advance()
		if d.Kind != AdvancePlay {
			t.Fatalf("抽到的结论是 %v，want AdvancePlay", d.Kind)
		}
		seen[d.Item.Identity()] = true
	}
	for _, want := range []string{newID, episode("222", "").Identity(), "mv:BV2|333"} {
		if !seen[want] {
			t.Errorf("这一轮没抽到 %q（抽到的是 %v）——换集之后袋子里那一格作废了", want, seen)
		}
	}
}
