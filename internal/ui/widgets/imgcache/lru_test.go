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
