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
