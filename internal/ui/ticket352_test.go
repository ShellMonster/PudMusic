package ui

// 「队列走完时抽屉里那句话对不上」（票 #352）。
//
// # 这一票在界面上的那一半
//
// 队列自己走完的时候，**播放条上写「这一串放完了」**，而**队列抽屉里当前那一行
// 的副题写「已停止」**——同一屏上对同一件事说了两句不同的话。而「用户按了
// 停止」和「队列自己走完了」在播放器那一边落在**同一档**上（进度回到开头、
// 不在放），所以分不出这两件事的地方，说的话就该是两句。
//
// # 判据都在**画面**上
//
// 抽屉那一行平常的副题是它的**来源**（「本地音乐」/「B 站音乐」…），停住的
// 时候才换成「此刻怎么了」那句话。而「同一屏上不矛盾」这件事只能从画面上量：
// 两处都画出来，看画面上有没有同时出现两句不同的话。
//
// # 接线那一半在别处
//
// 「按过一次暂停之后队列卡住」是 `internal/app` 那一层的事（`App.Toggle` 恢复
// 播放时没把 `wantPlay` 记回 true），那边的测试在
// `internal/app/toggle_resume_test.go`。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheQueueDrawerSaysTheQueueIsDone 断**队列走完时抽屉里那一行的副题是
// 「这一串放完了」**。
//
// ⚠️ **判据是「画面上有这句话」**：那一行的副题平常是它的来源，而「停住了」
// 的时候要换成状态那句话——换对换错都在同一行上，量别的量不到。
func TestTheQueueDrawerSaysTheQueueIsDone(t *testing.T) {
	tt, s := queueDrawerTesterWith(t, false, shortItems(t, 1)...)

	// 等这一首**真的放完**（不是手摆一个状态）：「这一串放完了」这一档只有真的
	// 走到队尾才会出现。
	waitFrames(t, tt, "队列走完", func() bool {
		return s.app.Snapshot().QueueFinished
	})
	tt.Frame()

	if !tt.HasText("这一串放完了") {
		t.Errorf("队列放完了，抽屉里那一行却没有说「这一串放完了」，文字有 %q", tt.Texts())
	}
	if tt.HasText("已停止") {
		t.Errorf("队列放完时抽屉里那一行说的是「已停止」——那是「用户按了停止」"+
			"那一档的话，而这里没有下一首了，文字有 %q", tt.Texts())
	}
}

// TestTheQueueDrawerStillSaysStoppedWhenTheUserStops 断**用户按了停止**那一档
// 说的还是「已停止」。
//
// ⚠️ **它是上一条的那一半**：把两种停住一律说成「这一串放完了」也照样绿——
// 而那是在骗用户（队列里明明还有十几首）。
//
// 🔴 **队列里要有多首歌**（票 #352 的评审）：这一档和「这一串放完了」的区别
// **就是**「队列里还有歌」——摆一首的话，队列一放完就是「没有下一首了」，两件事
// 在画面上分不开，而这一条验的就不是它了。所以这里摆三首。
//
// ⚠️ **而且要摆长素材**（`copiesOf`，4 秒那一份）：这一档的前提是「按停止的
// 时候歌还在放」——用 `shortItems`（0.25 秒）的话，放完就变成另一档了，
// 这一条就变成在赌时序。
func TestTheQueueDrawerStillSaysStoppedWhenTheUserStops(t *testing.T) {
	tt, s := queueDrawerTesterWith(t, false, copiesOf(t, 3)...)

	// ⚠️ **队列里确实还有歌**：这一档的前提。不钉住它的话，将来有人把这条
	// 用例改成「只有一首歌」，它照样绿——而那时候它验的就不是这一档了。
	if got := s.app.Queue().Len(); got < 2 {
		t.Fatalf("队列里只剩 %d 首了——这一档说的是「用户按了停止，队列里还有歌」", got)
	}

	s.app.Stop()
	tt.Frame()

	if !tt.HasText("已停止") {
		t.Errorf("用户按了停止，抽屉里那一行却没有说「已停止」，文字有 %q", tt.Texts())
	}
	if tt.HasText("这一串放完了") {
		t.Errorf("用户按了停止，抽屉里那一行却说是「这一串放完了」——"+
			"队列里还有歌，文字有 %q", tt.Texts())
	}
}

// TestThePlayBarAndTheDrawerDoNotContradictEachOther 断**同一屏上对同一件事
// 不说两句不同的话**。
//
// 这一条把播放条和抽屉**画在同一屏**上（前面的几条各自只画一处）：两处说的
// 要是同一句——队列走完时两处都是「这一串放完了」，而画面上**不该**还有
// 「已停止」。
//
// ⚠️ **为什么非得画在同一屏上**：分开画时，「播放条说 A、抽屉说 B」两条测试
// 各自都能绿——而用户看到的是**一屏两句话**。
func TestThePlayBarAndTheDrawerDoNotContradictEachOther(t *testing.T) {
	h := newHarness(t)
	h.app.SetPlayMode(app.PlayModeSequence)
	if err := h.app.Enqueue(shortItems(t, 1)...); err != nil {
		t.Fatal(err)
	}
	// 抽屉要开着——不然画面上只有播放条那一处，抽屉里那句话压根不在画面上。
	h.s.drawerOpen = true

	waitFrames(t, h.tt, "队列走完", func() bool {
		return h.app.Snapshot().QueueFinished
	})
	settleDrawer(t, h.s, h.tt)

	if !h.tt.HasText("这一串放完了") {
		t.Errorf("队列放完了，画面上却没有「这一串放完了」，文字有 %q", h.tt.Texts())
	}
	if h.tt.HasText("已停止") {
		t.Errorf("同一屏上「已停止」和「这一串放完了」同时出现——那是两句不同的话，"+
			"而这里只有一件事（队列走完了），文字有 %q", h.tt.Texts())
	}
}

// shortItems 造 n 份各 0.25 秒的 WAV 素材。
//
// ⚠️ **为什么 UI 这一层要自己造短素材**：「队列走完」这一档得**真的放完**一首，
// 而 `flac(t)` 那份素材有四秒——四秒的等待在一条界面测试里太贵了。
//
// ⚠️ **和 `internal/app` 那边的那份是同一段代码**：两个包的测试互不共享辅助
// 代码，抄一份比拉一条跨包的依赖干净。
func shortItems(t *testing.T, n int) []app.Item {
	t.Helper()
	b := shortenWAV(t, readWAV(t), 0.25)
	dir := t.TempDir()
	out := make([]app.Item, n)
	for i := range n {
		p := filepath.Join(dir, string(rune('a'+i))+".wav")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		out[i] = app.LocalItem(p)
	}
	return out
}

// readWAV 读那份 WAV 素材。
func readWAV(t *testing.T) []byte {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.wav")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("找不到 WAV 测试素材：%v", err)
	}
	return b
}

// shortenWAV 把一份 WAV 砍到只剩前 seconds 秒（只改长度字段，样本不动）。
func shortenWAV(t *testing.T, wav []byte, seconds float64) []byte {
	t.Helper()
	if len(wav) <= 44 {
		t.Fatalf("这份 WAV 只有 %d 字节，没有样本可砍", len(wav))
	}
	byteRate := int(wav[28]) | int(wav[29])<<8 | int(wav[30])<<16 | int(wav[31])<<24
	if byteRate <= 0 {
		t.Fatalf("这份 WAV 的字节率是 %d，读不懂", byteRate)
	}
	n := byteRate * int(seconds*1000) / 1000
	n = min(n, len(wav)-44)

	out := append([]byte(nil), wav[:44+n]...)
	put32 := func(off, v int) {
		out[off] = byte(v)
		out[off+1] = byte(v >> 8)
		out[off+2] = byte(v >> 16)
		out[off+3] = byte(v >> 24)
	}
	put32(40, n)         // data 段长度
	put32(4, len(out)-8) // RIFF 总长（从第 8 字节算到文件尾）
	return out
}

// queueDrawerTesterWith 只画抽屉那一块（不画整窗），摆指定的歌并把抽屉拉开。
//
// ⚠️ **顺序模式是这里的前提**：默认档是「循环」，而循环放完一首会接着放下一首
// ——「队列走完」这一档在默认档下压根不存在（而这一票要验的正是它）。
func queueDrawerTesterWith(t *testing.T, dark bool, items ...app.Item) (*mygoui.Tester, *Shell) {
	t.Helper()
	b := newTestShell(t, at(winW, winH), systemIsDark(dark),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.applyFrameTheme(c)
				s.viewQueueDrawer(c)
			}
		}))
	tt := b.tt
	b.a.SetPlayMode(app.PlayModeSequence)
	if err := b.a.Enqueue(items...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "队列里的歌开始播", func() bool {
		tt.Frame()
		return b.a.Snapshot().HasTrack
	})
	b.s.drawerOpen = true
	tt.Frame()
	settleDrawer(t, b.s, tt)
	return tt, b.s
}
