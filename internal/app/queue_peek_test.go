package app

import "testing"

// 队列的「预告」那一支（`Queue.PeekNext`）。
//
// 预取要**提前**知道下一首是谁，才能去备地址；而它算出来的必须和真播那一
// 次算出来的**是同一首**。这一组盯的就是这一件事——它一旦漂了，预取备好的
// 地址就是**另一首**的，而症状是静默的：放出来的歌还是对的（对不上就重新
// 取），白费的是这一票要省的那几百毫秒。

// TestPeekNextAgreesWithAdvance 盯「预告」和「真抽」是同一件事。
//
// 四档各来一遍——随机那一档是最要紧的（它的「下一首」是从洗牌袋里抽的）。
func TestPeekNextAgreesWithAdvance(t *testing.T) {
	for _, mode := range PlayModes {
		q := NewQueueWithRand(&fixedRand{n: 0})
		q.SetPlayMode(mode)
		q.Append(
			LocalItem("a.mp3"),
			LocalItem("b.mp3"),
			LocalItem("c.mp3"),
		)
		q.Move(1) // cur = 0

		// 随机那一档的洗牌袋一开始是空的（换档时清掉，见 SetPlayMode）：
		// 先抽一次把袋子装上，后面的预告才答得出来。
		if mode == PlayModeRandom {
			q.Advance()
		}

		d, known := q.PeekNext()
		if !known {
			// 答不出来只该发生在「袋空」那一种（见 bagPeekLocked）。而袋里有货
			// 的时候答不出来，就是「预告」和「真抽」已经不是同一件事了。
			if mode == PlayModeRandom {
				t.Fatalf("%s：洗牌袋里有货，预告却答不出来", mode)
			}
			t.Fatalf("%s：预告答不出来", mode)
		}
		got := q.Advance()
		if got.Kind != d.Kind {
			t.Errorf("%s：预告说 %v，真抽说 %v", mode, d.Kind, got.Kind)
			continue
		}
		if got.Item.Identity() != d.Item.Identity() {
			t.Errorf("%s：预告说 %q，真抽说 %q", mode, d.Item.Title, got.Item.Title)
		}
	}
}

// TestPeekNextDoesNotTouchTheQueue 盯「预告」**不动**队列。
//
// 它是「只算不动」的那一支：指针不动、洗牌袋不抽、历史栈不压——
// 抽了的话，真播那一趟抽到的就不是这一首了，而这一票备的地址就备错了。
func TestPeekNextDoesNotTouchTheQueue(t *testing.T) {
	q := NewQueueWithRand(&fixedRand{n: 0})
	q.SetPlayMode(PlayModeRandom)
	q.Append(
		LocalItem("a.mp3"),
		LocalItem("b.mp3"),
		LocalItem("c.mp3"),
	)
	q.Move(1)
	q.Advance() // 装袋 + 抽走一首

	cur, bag, hist := q.CurrentIndex(), len(q.bag), len(q.history)
	for range 3 {
		if _, known := q.PeekNext(); !known {
			t.Fatal("洗牌袋里有货，预告却答不出来")
		}
	}
	if got := q.CurrentIndex(); got != cur {
		t.Errorf("预告把指针挪了：%d → %d", cur, got)
	}
	if got := len(q.bag); got != bag {
		t.Errorf("预告把洗牌袋抽走了：%d → %d", bag, got)
	}
	if got := len(q.history); got != hist {
		t.Errorf("预告动了历史栈：%d → %d", hist, got)
	}
}
