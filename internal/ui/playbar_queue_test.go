package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 上一首/下一首这条验收项：按钮在播放条上、加了三首歌能来回切。
//
// 加歌这一步走的是 app.Enqueue 而不是文件框——文件框要系统面板，Tester 里
// 开不了。导航栏的「打开文件」和播放条的「加歌」调的是同一个
// pickAndEnqueue，底下接的就是这里这条 app.Enqueue，所以那条链路同样成立。

// copiesOf 把 FLAC 素材复制成 n 个不同路径的歌名，这样「换到下一首了」
// 是能认出来的，而不是靠时间猜。
func copiesOf(t *testing.T, n int) []app.Item {
	t.Helper()
	b, err := os.ReadFile(flac(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := make([]app.Item, n)
	for i := range n {
		name := string(rune('a'+i)) + ".flac"
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
		out[i] = app.LocalItem(filepath.Join(dir, name))
	}
	return out
}

func enqueue(t *testing.T, h *harness, items ...app.Item) {
	t.Helper()
	if err := h.app.Enqueue(items...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "队列里的歌开始播", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().HasTrack
	})
}

func TestStepButtonsWalkTheQueue(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 3)...)

	if got := h.app.Snapshot().Title; got != "a" {
		t.Fatalf("加完歌放的是 %q，want a", got)
	}
	for _, want := range []string{"b", "c", "a"} {
		if err := h.tt.Click("下一首"); err != nil {
			t.Fatalf("点不了「下一首」：%v", err)
		}
		waitFor(t, "下一首", func() bool {
			h.tt.Frame()
			return h.app.Snapshot().Title == want
		})
		if got := h.app.Snapshot().Title; got != want {
			t.Fatalf("下一首之后放的是 %q，want %q", got, want)
		}
	}
	for _, want := range []string{"c", "b", "a"} {
		if err := h.tt.Click("上一首"); err != nil {
			t.Fatalf("点不了「上一首」：%v", err)
		}
		waitFor(t, "上一首", func() bool {
			h.tt.Frame()
			return h.app.Snapshot().Title == want
		})
	}
}

// TestStepButtonsDoNothingWithoutAQueue 盯的是「没进队列就不许跳」：
// 不走 Enqueue 放的那一首不在队列里，按钮必须是灰的，点了也不该换歌。
func TestStepButtonsDoNothingWithoutAQueue(t *testing.T) {
	h := newHarness(t)
	h.open(t)
	before := h.app.Snapshot()

	// 灰掉的按钮连元素都不该有，Tester 会直接说「画面上没有这个东西」。
	if err := h.tt.Click("下一首"); err != nil {
		t.Logf("按钮是灰的，点不到：%v", err)
		return
	}
	// 万一能点到，也不该换歌。
	for range 20 {
		h.tt.Frame()
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.app.Snapshot(); got.Path != before.Path || got.Title != before.Title {
		t.Errorf("没进队列的时候点「下一首」把歌换了：%q → %q", before.Title, got.Title)
	}
}

// TestStepButtonsLightUpOnceThereIsAQueue 是上一条的另一半：进了队列就该能按。
func TestStepButtonsLightUpOnceThereIsAQueue(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 2)...)

	for _, name := range []string{"上一首", "下一首"} {
		r, ok := h.tt.Find(name)
		if !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", name, h.tt.Texts())
		}
		if r.W == 0 || r.H == 0 {
			t.Errorf("%q 没有尺寸 %v，按钮没画出来", name, r)
		}
	}
}
