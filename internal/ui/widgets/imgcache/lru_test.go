package imgcache

import "testing"

// TestTouchReplacesAForgottenEntry 验同一个 URL 换过一条 entry 之后，
// LRU 记的是新的那一条。
//
// 边角是这么来的：失败表到上限会把最老的那条从 entries 里删掉，而界面
// 可能正好在这一刻拿着旧指针调 Get（它先 Load 到了旧条目，删除发生在
// 之后）。那一下 touch 会把旧条目重新塞回 URL 表；如果 touch 只认 URL、
// 不认条目，之后新条目的字节数就永远不在账上——used 涨了却挤不掉。
func TestTouchReplacesAForgottenEntry(t *testing.T) {
	l := newLRU(1000)
	old := &entry{url: "https://example.invalid/a.png"}
	l.touch(old)
	l.resize(old, 600)
	l.drop(old)
	if l.used != 0 {
		t.Fatalf("drop 之后 used=%d，want 0", l.used)
	}

	// 界面手里那个旧指针又碰了一下。
	l.touch(old)
	// 下一次 Get 拿到的是新条目。
	fresh := &entry{url: old.url}
	l.touch(fresh)

	if el, ok := l.byURL[old.url]; !ok || el.Value.(*entry) != fresh {
		t.Fatal("URL 表里收的仍然是旧条目")
	}
	if n := l.order.Len(); n != 1 {
		t.Errorf("LRU 里有 %d 条，want 1", n)
	}
	l.resize(fresh, 400)
	if l.used != 400 {
		t.Errorf("新条目记账之后 used=%d，want 400", l.used)
	}
}

// TestAnEntryEvictedBeforeItIsDecodedIsNotAccounted 钉住「挤掉的那一条
// 解码回来之后不许再记一笔账」。
//
// 这是「封面在有图 ↔ 占位之间反复切换」的**一半根因**（issue #208）：一条从
// 「有人要它」到「解完码」之间被挤掉的条目，解码回来之后还会被记一笔账，
// 而**那一笔再也挤不掉**（表里已经没有它了，没有一个「挤掉它」的动作能把它
// 从 used 里减掉）。
//
// 后果不是「内存多占一点」：挤的循环最后会把整张表清空还挤不下（表空了
// used 还是正的），之后**每来一张图就把表里剩下的全挤掉**——一张封面刚解出来
// 就被挤掉、退回占位、重新下一次（实测 16 张封面下了 7000 多次）。
func TestAnEntryEvictedBeforeItIsDecodedIsNotAccounted(t *testing.T) {
	l := newLRU(1000)
	e := &entry{url: "https://example.invalid/a.png"}
	l.touch(e)

	// 它在解码完成之前被挤掉了（界面上它退回占位，重新开始下一次）。
	l.drop(e)

	// 解码这时候才回来。
	l.resize(e, 500)

	if l.used != 0 {
		t.Errorf("一条已经不在表里的条目被记了 %d 字节（want 0）——\n"+
			"挤的循环再也挤不掉它，于是挤的循环最后把整张表清空还挤不下，"+
			"之后每来一张图就把表里剩下的全挤掉（issue #208）", l.used)
	}
	if l.order.Len() != 0 {
		t.Errorf("LRU 里有 %d 条，want 0", l.order.Len())
	}
}

// TestTheEvictionSkipsEntriesThatHaveNoBytesYet 钉住「还在下的那一张不参与
// 淘汰」。
//
// 0 字节的条目（还在下，或者下失败了）**不占预算**：把它挤掉对预算一分钱都
// 不省，只是把正在下的一次扔了——那一次请求被取消，下一帧有人再要它，它又
// 从零开始，而「从零开始」在界面上就是「退回占位」。
func TestTheEvictionSkipsEntriesThatHaveNoBytesYet(t *testing.T) {
	l := newLRU(1000)

	// 「还在下的」那一张：先碰一下（界面上第一帧就要它），它排在最前面。
	loading := &entry{url: "https://example.invalid/loading.png"}
	l.touch(loading)

	// 后来的那一张下完了，把预算占满。
	loaded := &entry{url: "https://example.invalid/loaded.png"}
	l.touch(loaded)
	l.resize(loaded, 1000)
	if l.used != 1000 {
		t.Fatalf("used=%d，want 1000", l.used)
	}

	// 再来一张：预算装不下了，要挤掉一个。
	third := &entry{url: "https://example.invalid/third.png"}
	l.touch(third)
	l.resize(third, 1000)

	// 挤掉的是**有字节的那一张**，不是还在下的那一张。
	if _, ok := l.byURL[loading.url]; !ok {
		t.Errorf("还在下的那一张被挤掉了——它不占预算，挤掉它只是把" +
			"正在下的一次扔了（下一帧它又从零开始，界面上就是退回占位）")
	}
	if l.used > l.max {
		t.Errorf("挤完之后 used=%d，上限 %d", l.used, l.max)
	}
}
