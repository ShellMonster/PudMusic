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
	// s 是那副外壳。**它是为了推钟用的**：抽屉的进出场走的是时间，而
	// Tester 的钟站着不动——不推它的话抽屉永远停在「刚打开」那一帧，
	// 而那一帧面板还在窗口外面（见 `settleDrawer`）。
	s *Shell
}

// 走基座（`newTestShell`），所以收尾是无条件挂上的。`New(a)` 会起
// `searchHistory.loop` 和 `shortcutKeys.loop` 两个后台写循环，首帧还会经
// `feed.arm` 装一个每五分钟一跳的定时器——只 `t.Cleanup(a.Close)` 收不掉它们
// （`App.Close` 不管 Shell 的资源），几百条测试就是几百个挂到测试二进制结束的
// goroutine。
func newHarness(t *testing.T) *harness {
	t.Helper()
	b := newTestShell(t, at(winW, winH))
	return &harness{app: b.a, tt: b.tt, s: b.s}
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
	if !h.tt.HasText("布丁音乐") {
		t.Errorf("标题不在，文字有 %q", h.tt.Texts())
	}
	if !h.tt.HasText("还没有打开文件") {
		t.Errorf("没歌时的提示不在，文字有 %q", h.tt.Texts())
	}
	// ⚠️ **「音量」那一枚现在叫「静音」**：音量收成了一枚图标，而那枚图标的
	// 无障碍名字说的是**按下去会发生什么**（静音 / 取消静音），和上游
	// `music-volume` 的 `aria-label={isMuted ? "取消静音" : "静音"}` 一样。
	// 真的那根音量滑块在浮层里，浮层关着的时候不在画面上。
	for _, want := range []string{"播放", "静音", "播放进度"} {
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

// TestVolumeSliderDrivesTheApp 盯浮层里那根竖滑块真的在调音量。
//
// ⚠️ **它现在要先打开浮层**：音量收成了一枚图标，那根滑块在悬停浮出来的浮层里
// （上游 `music-volume` 那个做法）。悬停 → 浮层出来 → 滑块在画面上 → 按它。
func TestVolumeSliderDrivesTheApp(t *testing.T) {
	h := newHarness(t)
	h.open(t)

	hoverVolumeKey(t, h.tt)

	bar, ok := h.tt.Find("音量")
	if !ok {
		t.Fatalf("浮层里找不到那根音量滑块，文字有 %q", h.tt.Texts())
	}
	// 竖滑块：按在**底下**就是 0（框架的竖直映射是 `1-(y-cy)/ch`，见
	// `mygo/ui/value_input.go:207`）。
	h.tt.Press(bar.X+bar.W/2, bar.Y+bar.H-2)
	h.tt.Release(bar.X+bar.W/2, bar.Y+bar.H-2)
	h.tt.Frame()

	if got := h.app.Snapshot().Volume; got > 0.3 {
		t.Errorf("把音量条拉到最下，音量却是 %v", got)
	}
}

// TestThePlayBarRightRowFollowsBiuOrder 钉住右边那一组的次序。
//
// 票 #127 的验收原话：「右侧控件顺序照 Biu：**模式 / 下载 / 队列 / 音量 / 倍速**」。
// 抄的是 `biu/src/layout/playbar/right/index.tsx:14-18` 那一行的顺序。
//
// ⚠️ **断的是横向位置，不是「这几个键都在」**：五个键都在而次序不对，这条会全绿
// ——而「次序」恰恰是这一票要的东西（用户按的是肌肉记忆，不是读出来的清单）。
// 所以逐对比的是「右边那个的左边沿 > 左边那个的右边沿」。
//
// ⚠️ **「加歌」不算在内**：它是本应用多出来的一枚（上游把选本地音乐放在侧栏菜单
// 里），摆在最后一格。把它也算进次序的话，这条就在给一个上游没有的东西定规矩。
func TestThePlayBarRightRowFollowsBiuOrder(t *testing.T) {
	// 有歌时才全：Biu 那一行是 `{Boolean(playId) && … && <MusicDownloadButton />}`
	// （`playbar/right/index.tsx:15`），没歌的时候不摆下载。
	want := []string{"播放模式", "下载", "播放列表", "播放速率"}

	tt, _ := metaBarTester(t, fullBarState(), 1200)

	// 音量那一枚**自己找**：它的无障碍名字跟着静音状态变（见 volumeKeyOnTheBar）。
	vol, ok := volumeKeyOnTheBar(tt)
	if !ok {
		t.Fatalf("播放条右边那一组里没有音量那一枚，文字有 %q", tt.Texts())
	}

	// 次序：模式 → 下载 → 队列 → 音量 → 倍速。
	ordered := []mygoui.Rect{}
	names := []string{}
	for _, label := range want[:3] {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("播放条右边那一组里没有 %q，文字有 %q", label, tt.Texts())
		}
		if r.X < 0 || r.X+r.W > 1200 {
			t.Errorf("%q 在 %v，有一半在窗口外面", label, r)
		}
		ordered = append(ordered, r)
		names = append(names, label)
	}
	ordered = append(ordered, vol)
	names = append(names, "音量")
	rate, ok := tt.Find("播放速率")
	if !ok {
		t.Fatalf("播放条右边那一组里没有 %q，文字有 %q", "播放速率", tt.Texts())
	}
	ordered = append(ordered, rate)
	names = append(names, "播放速率")

	for i := 1; i < len(ordered); i++ {
		if ordered[i].X <= ordered[i-1].X {
			t.Errorf("%q（x=%.1f）排在 %q（x=%.1f）**前面**了——"+
				"次序要和 Biu 一致：%v", names[i], ordered[i].X, names[i-1],
				ordered[i-1].X, names)
		}
	}

	// 加歌在最后：它在整组里最靠右。
	enq, ok := tt.Find("加歌")
	if !ok {
		t.Fatalf("播放条上没有「加歌」，文字有 %q", tt.Texts())
	}
	if enq.X <= rate.X {
		t.Errorf("「加歌」（x=%.1f）不在最后——它在「播放速率」（x=%.1f）前面", enq.X, rate.X)
	}
}

// volumeKeyOnTheBar 找播放条右边那一组里的音量键。
//
// ⚠️ **它的无障碍名字跟着状态变**：没静音的时候是「静音」（按下去会静音），
// 静音了是「取消静音」——和上游 `music-volume` 的
// `aria-label={isMuted ? "取消静音" : "静音"}` 一个做法。所以两个都要试。
func volumeKeyOnTheBar(tt *mygoui.Tester) (mygoui.Rect, bool) {
	for _, label := range []string{"静音", "取消静音"} {
		if r, ok := tt.Find(label); ok {
			return r, true
		}
	}
	return mygoui.Rect{}, false
}

// TestTheDownloadKeyIsOnlyThereWhenThereIsATrack 钉住没歌时不摆下载键。
//
// 照 Biu：`{Boolean(playId) && getPlayItem()?.source !== "local" && <MusicDownloadButton />}`
// （`playbar/right/index.tsx:15`）。本地文件压根没有 cid 可下，给一个按下去只会
// 报错的键，不如不摆。
func TestTheDownloadKeyIsOnlyThereWhenThereIsATrack(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{Rate: 1, Volume: 0.8}, 1200)
	if tt.HasText("下载") {
		t.Errorf("没有歌的时候播放条上画出了「下载」，文字有 %q", tt.Texts())
	}
	// 有歌的时候它在——不然上面那一条次序测试会量出一个空的洞。
	withTrack, _ := metaBarTester(t, app.State{HasTrack: true, Title: "一首歌", Rate: 1, Volume: 0.8}, 1200)
	if !withTrack.HasText("下载") {
		t.Errorf("有歌的时候播放条上没有「下载」，文字有 %q", withTrack.Texts())
	}
}

// TestThePlaylistKeySitsOnThePlayBarNotTheTopBar 钉住队列开关只在播放条上。
//
// 它当初在顶栏是因为右边那一组摆不下；票 #127 右边腾得开了，于是搬回去（照
// `playbar/right/index.tsx:16`）。**两处都留着就是同一个动作两个入口**，而它们
// 会不一致——顶栏那枚在满屏播放器盖住播放条时还点得到，而那一枚的 Tooltip 说
// 的也是「打开播放列表」。
//
// 两半都要断：顶栏上**没有**（`navBarTester` 只画顶栏那一块，所以那是全部文字，
// 不是「在整窗里找」）＋ 播放条上按它真的能开抽屉。
func TestThePlaylistKeySitsOnThePlayBarNotTheTopBar(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			if navBarTester(t, dark).HasText("播放列表") {
				t.Error("顶栏上还有「播放列表」——它搬到播放条右边那一组了，" +
					"两处都留着就是同一个动作两个入口")
			}
			h := newHarness(t)
			if !h.tt.HasText("播放列表") {
				t.Fatalf("播放条上没有「播放列表」，文字有 %q", h.tt.Texts())
			}
			openDrawer(t, h) // 按一下，抽屉要真的开
		})
	}
}
