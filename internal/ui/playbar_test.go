package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 这些测试用 MyGo 的 Tester 跑：view 不需要真窗口就能跑，能点、能拖、还能
// 看画出来的文字。阶段 1 的验收项 1（出窗口和播放条）、3（进度条能拖）、
// 4（播放/暂停）都在这里验。

const (
	winW = 760
	winH = 460
)

func flac(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	return p
}

// harness 是一套跑起来的界面：无窗口，但控件和真的一样能点能拖。
type harness struct {
	app *app.App
	tt  *mygoui.Tester
}

// 走基座（`newTestShell`），所以收尾是无条件挂上的。`New(a)` 会起
// `searchHistory.loop` 和 `shortcutKeys.loop` 两个后台写循环，首帧还会经
// `feed.arm` 装一个每五分钟一跳的定时器——只 `t.Cleanup(a.Close)` 收不掉它们
// （`App.Close` 不管 Shell 的资源），几百条测试就是几百个挂到测试二进制结束的
// goroutine。
func newHarness(t *testing.T) *harness {
	t.Helper()
	b := newTestShell(t, at(winW, winH))
	return &harness{app: b.a, tt: b.tt}
}

func (h *harness) open(t *testing.T) {
	t.Helper()
	if err := h.app.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
}

// dragTo 把进度条从一端拖到另一端（0..1 的位置），按下、移动、松手一步做完。
func dragTo(t *testing.T, tt *mygoui.Tester, to float64) {
	t.Helper()
	bar, ok := tt.Find("播放进度")
	if !ok {
		t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
	}
	// ⚠️ **量不出大小就直说**（`settings_page_test.go` 的 `clickSetting` 是同一个形状）。
	// 少这一句的话，按在一个还没排好版的框上什么也不会发生，而症状报在两三步之后
	// 「位置没变」——离真因隔了三层。
	if bar.W <= 0 || bar.H <= 0 {
		t.Fatalf("进度条没量出大小（%v），按不中：文字有 %q", bar, tt.Texts())
	}
	x := bar.X + bar.W*float32(to)
	y := bar.Y + bar.H/2
	tt.Press(bar.X+bar.W/2, y)
	for i := 1; i <= 4; i++ {
		tt.Move(bar.X+(bar.X+bar.W*float32(to)-bar.X)*float32(i)/4, y)
	}
	tt.Release(x, y)
	tt.Frame()
}

// TestWindowShowsPlayBar 是验收项 1：起得来，画面上有播放条。
func TestWindowShowsPlayBar(t *testing.T) {
	h := newHarness(t)
	if !h.tt.HasText("BiuGo") {
		t.Errorf("标题不在，文字有 %q", h.tt.Texts())
	}
	if !h.tt.HasText("还没有打开文件") {
		t.Errorf("没歌时的提示不在，文字有 %q", h.tt.Texts())
	}
	for _, want := range []string{"播放", "音量", "播放进度"} {
		if _, ok := h.tt.Find(want); !ok {
			t.Errorf("画面上找不到 %q，文字有 %q", want, h.tt.Texts())
		}
	}
}

func TestOpeningShowsTheTrack(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	if !h.tt.HasText("stereo16") {
		t.Errorf("歌名不在，文字有 %q", h.tt.Texts())
	}
	if h.tt.HasText("还没有打开文件") {
		t.Error("开了文件之后还在显示「还没有打开文件」")
	}
}

// TestPlayPauseButton 是验收项 4。
func TestPlayPauseButton(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	if !h.tt.HasText("暂停") {
		t.Fatalf("正在播时按钮应该是「暂停」，文字有 %q", h.tt.Texts())
	}
	if err := h.tt.Click("暂停"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.tt.HasText("播放") {
		t.Errorf("点了之后按钮没变成「播放」，文字有 %q", h.tt.Texts())
	}
	if h.app.Snapshot().Playing {
		t.Error("点了暂停，播放器还在播")
	}

	if err := h.tt.Click("播放"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.app.Snapshot().Playing {
		t.Error("再点一次没接着播")
	}
}

// TestProgressBarDrags 是验收项 3：拖了位置会跳。
func TestProgressBarDrags(t *testing.T) {
	h := newHarness(t)
	h.open(t)
	dur := h.app.Snapshot().Duration
	if dur <= 0 {
		t.Fatal("没有时长，没法验拖动")
	}
	h.app.Toggle() // 暂停，让位置稳定下来，断言才不含糊

	dragTo(t, h.tt, 0.5)

	got := h.app.Snapshot().Position
	if got < dur/4 || got > 3*dur/4 {
		t.Errorf("拖到中间之后位置是 %v，时长 %v，应该在中间附近", got, dur)
	}
	if h.app.Snapshot().Playing {
		t.Error("暂停状态下拖进度条，顺手把它播起来了")
	}
}

// TestDragToEndDoesNotCrash 是验收项 5：拖到文件末尾不崩、不假死。
//
// 这条专门盯 t-0017 验过的边界：seek 到 Len() 是合法的位置，但容器层
// 「seek 到自己的末尾」会拒绝，必须由解码器自己兜住。
//
// ⚠️ **不断言「拖完之后位置还在末尾」**：一首歌放完之后播放模式立刻接管
// （这里是队列空 → 停在开头，见 `App.onTrackFinished`），而接管发生在 50ms 一
// 次的那个 tick 上。所以拖完之后读到的是「末尾」还是「开头」要**看那一帧和
// 那个 tick 谁先**，断它等于断一次抛硬币——它翻掉时和功能坏掉时长得一模一样。
//
// 假死那一半没有因此变松：下面断的是**终态**（停下来、往回拖能接着播、界面还
// 点得动）。而真正要防的「seek 到末尾把解码器搞崩」也不用那个瞬时值来防——
// 那会是解码 goroutine 里的 panic，它带走的是整个测试进程，这条用例自己就没了。
func TestDragToEndDoesNotCrash(t *testing.T) {
	h := newHarness(t)
	h.open(t)
	// ⚠️ **先把前提摆回去：拖的那一刻这一首得真的在放。**
	//
	// 素材只有 4 秒，而 `dragTo` 要画 7 帧（每帧都是**整窗**：顶栏 + 侧栏 + 页面 +
	// 播放条 + 抽屉）。满载的机器上这 7 帧本身就能把一首 4 秒的歌放完——放完之后
	// `playing` 变 false，于是下面「拖完就停」证明不了任何事：**歌自己放完也是停**。
	//
	// 差的就是「这 7 帧要花多久」：实测整包跑 10 次挂 3 次，单独跑 0 次（单独跑没有
	// 整包那些堆和 GC 压力）。所以这里显式检查并复位，而不是指望它还在放。
	if !h.app.Snapshot().Playing {
		h.app.SeekTo(0)
		h.app.Play()
		waitFor(t, "这一首回到开头重新放上了", func() bool {
			return h.app.Snapshot().Playing
		})
	}

	dragTo(t, h.tt, 1) // 一路拖到最右端

	// ⚠️ **不要断「位置到了末尾」——量出来那不是一个状态。**
	//
	// `ScrubEnd` 确实**同步**把位置写成末尾（松手那一帧读就是 4s），但**播放器不接**
	// 「seek 到自己末尾」那一下——就是函数头上说的 t-0017 那个边界。50ms 之后后台那个
	// 时钟（`App.tick`）用播放器的**真实**位置覆盖回来，读到的是 0s，从此不动。
	//
	// 同一轮量到的对照，四行连起来才是结论：
	//
	//	A 拖完立刻         位置=4s    playing=true    ← 拖动确实生效，ScrubEnd 写进去了
	//	B 50ms 之后        位置=0s    playing=false   ← 时钟覆盖回播放器的真实位置
	//	C 手动 seek 到 2s  位置=2.14s playing=true    ← seek 本身正常
	//	D 手动 seek 到末尾  位置=0s    playing=false   ← 只有「跳到末尾」这一下不接
	//
	// 也就是说「位置 == 时长」这个状态**这个播放器从来不到**。写它进去只能得到一个
	// 靠运气的东西：读到 4s 还是 0s，取决于读取落在那 50ms 窗口的哪一边（两边都实测
	// 出现过：31.995464ms 和 4s）。
	//
	// 要断的是下一句：「拖到末尾之后播放器停了」。前提上面刚摆好（这一首在放），所以
	// 它停下来就只可能是这次拖动把 seek 送到了末尾——**这才是验收项 5 要的东西**。
	//
	// 队列是空的 → 没有下一首可放 → 停下来。不是卡住。
	waitFor(t, "拖到末尾后播放器停下来", func() bool {
		h.tt.Frame()
		return !h.app.Snapshot().Playing
	})

	// 再往回拖，画面还得能接着画。
	dragTo(t, h.tt, 0.2)
	if !h.app.Snapshot().Playing {
		t.Error("往回拖之后没有接着播")
	}
	// 界面还活着：再点一次暂停，不该挂。
	if err := h.tt.Click("暂停"); err != nil {
		t.Fatalf("拖到末尾之后界面点不动了：%v", err)
	}
}

// TestRenderPlayBarShots 把播放条那几个状态存成 PNG（01–05），留给人眼对照。
// 和 `router_test.go` 的 TestRenderShots（10–18，整窗走一遍）是同一套图的两半。
// 它跑的是 MyGo 的 Tester，
// 走的是和真窗口一模一样的 layout + paint，只是画到内存里而不是屏幕上。
//
//	go test ./internal/ui/ -run TestRenderPlayBarShots
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarShots
func TestRenderPlayBarShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	h := newHarness(t)
	save(t, dir, "01-empty.png", h.tt)

	h.open(t)
	save(t, dir, "02-playing.png", h.tt)

	h.app.Toggle() // 暂停
	save(t, dir, "03-paused.png", h.tt)

	dragTo(t, h.tt, 0.55)
	save(t, dir, "04-after-drag.png", h.tt)

	// 打不开的文件：界面上要有话说。
	h.app.Open(filepath.Join(t.TempDir(), "nope.flac"))
	h.tt.Frame()
	save(t, dir, "05-error.png", h.tt)
}

func TestVolumeSliderDrivesTheApp(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	bar, ok := h.tt.Find("音量")
	if !ok {
		t.Fatalf("画面上找不到音量条，文字有 %q", h.tt.Texts())
	}
	h.tt.Press(bar.X+bar.W*0.1, bar.Y+bar.H/2)
	h.tt.Release(bar.X+bar.W*0.1, bar.Y+bar.H/2)
	h.tt.Frame()

	if got := h.app.Snapshot().Volume; got > 0.3 {
		t.Errorf("把音量条拉到最左，音量却是 %v", got)
	}
}
