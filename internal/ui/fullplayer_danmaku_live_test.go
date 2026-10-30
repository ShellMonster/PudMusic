// 满屏弹幕的**局部动画链路**（票 #669）。
//
// # 这一组钉的是哪件事
//
// 为了不让整棵界面按 20Hz 重建，播放中的整窗唤醒被降到约 1Hz
// （`internal/app/tick_wake.go` 的 `readoutMoved`）。而弹幕的位移目标原来只
// 在建树那一帧算一次，于是弹幕「动一下、停住、下一秒再动」。
//
// ⇒ 位移现在由**绘制帧**按 `p.Now()` 现算（`Painter.AnimationFrame` 只重绘、
// 不重建视图），弹幕这一层于是有了自己的时钟：整窗 1Hz、弹幕 60fps，两条链
// 互不相干。
//
// # 判据为什么是「推钟 + 像素 + builds 计数」
//
//   - **连续推进**：Tester 的钟站着不动，只有 `Advance` 才走；量画面上的墨迹
//     才看得到「它真的往前走了」。
//   - **不重建**：`s.builds.full` 是 `viewFullPlayer` 建树的计数——推钟之后
//     它不变，才说明那段位移走的是只重绘的帧。
//   - **暂停 / 恢复 / 换歌 / 回退**：像素 + 模型层（`danmakuLayer` 的 slots /
//     lanes）两头都断。
package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// danmakuPlayingEnv 是「满屏开着、正在放、位置随测试摆」的外壳。
//
// ⚠️ **频谱要关掉**：满屏播放器默认开着频谱，而它在播放中也会
// `p.AnimationFrame()`（`fullplayer_spectrum.go`）。开着的话这几条断「弹幕要不
// 要帧」就分不清是谁要的——量到的可能是频谱而不是弹幕。
func danmakuPlayingEnv(t *testing.T, items []app.DanmakuItem, opts ...shellOpt) *danmakuEnv {
	t.Helper()
	env := danmakuShell(t, items, opts...)
	prefs := env.s.app.FullPlayerPrefs()
	prefs.ShowSpectrum = false
	env.s.app.SetFullPlayerPrefs(prefs)
	env.st.Playing = true
	return env
}

// redSeen / greenSeen 报画面上（封面那一块、任意一条轨道）有没有那种墨迹。
func redSeen(tt *mygoui.Tester, area mygoui.Rect) bool {
	for lane := range danmakuTracks {
		if l, _ := redSpan(tt, area, lane); l >= 0 {
			return true
		}
	}
	return false
}

func greenSeen(tt *mygoui.Tester, area mygoui.Rect) bool {
	for lane := range danmakuTracks {
		if l, _ := greenSpan(tt, area, lane); l >= 0 {
			return true
		}
	}
	return false
}

// TestTheDanmakuKeepsMovingWithoutRebuildingTheView 钉住「播放时弹幕连续推进，
// 而且不靠整窗重建」。
//
// # 判据
//
// 建好一帧之后**不再动 `st.Position`**，只把测试时钟往前推 300ms：
//
//   - 墨迹得往左走 18 像素（60 像素/秒 × 0.3 秒）；
//   - `builds.full` 一次都不许涨（涨了就是整窗重建在推它，不是绘制帧）。
func TestTheDanmakuKeepsMovingWithoutRebuildingTheView(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()
	before, _ := redSpan(env.tt, danmakuArea(), 0)
	if before < 0 {
		t.Fatalf("画面上没有弹幕，文字有 %q", env.tt.Texts())
	}
	if n := env.s.full.danmaku.requests; n == 0 {
		t.Fatal("播放中的弹幕画完一帧之后没有要下一帧——" +
			"位移只能等下一次整窗重建（约 1Hz），用户看到的是「动一下停一下」")
	}

	builds := env.s.builds.full
	pos := env.st.Position
	env.tt.Advance(300 * time.Millisecond)
	after, _ := redSpan(env.tt, danmakuArea(), 0)
	if after < 0 {
		t.Fatalf("推了 300 毫秒之后画面上没有弹幕了")
	}
	// 60 像素/秒 × 0.3 秒 = 18 像素；像素取整再留一点余量。
	if got := before - after; got < 14 || got > 22 {
		t.Errorf("推了 300 毫秒，弹幕左边沿从 x=%d 到了 x=%d（走了 %d，"+
			"want 18 上下）——它没有跟着绘制帧连续推进", before, after, got)
	}
	if env.s.builds.full != builds {
		t.Errorf("推这 300 毫秒时满屏整窗重建了 %d 遍（原来 %d）——"+
			"弹幕的位移不该靠整窗重建来推", env.s.builds.full, builds)
	}
	if env.st.Position != pos {
		t.Errorf("这一条测试自己动了位置（%v → %v），判据就不成立了",
			pos, env.st.Position)
	}
}

// TestTheDanmakuMovesOneStepPerFrame 钉住「每一帧都往前走一点，而不是一跳一大截」。
//
// # 为什么这条和上一条都要
//
// 上一条只证明「300ms 里走了 18 像素」——那在「一帧跳 18 像素」的实现里也成立。
// 用户能感到的卡顿正是**每帧跳一大截**，所以这一条按 16ms 一帧推，量每一步。
//
// ⚠️ **不用固定帧数当判据**：循环的是**时间**，每一步的期望值由那一步的时长
// 算出来（60 像素/秒 × 16ms ≈ 1 像素）。
func TestTheDanmakuMovesOneStepPerFrame(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()
	prev, _ := redSpan(env.tt, danmakuArea(), 0)
	if prev < 0 {
		t.Fatalf("画面上没有弹幕")
	}

	builds := env.s.builds.full
	const step = 16 * time.Millisecond
	// 60 像素/秒 × 16ms = 0.96 像素。累计 5 步约 4.8 像素。
	for i := 0; i < 5; i++ {
		env.tt.Advance(step)
		cur, _ := redSpan(env.tt, danmakuArea(), 0)
		if cur < 0 {
			t.Fatalf("第 %d 步之后画面上没有弹幕了", i+1)
		}
		want := int(float64(step.Milliseconds()) * danmakuSpeed / 1000 * float64(i+1))
		if got := prev - cur; got < want-2 || got > want+2 {
			t.Errorf("第 %d 步走了 %d 像素（want %d 上下）——"+
				"弹幕该按绘制帧一像素一像素地走，不是憋一会儿跳一下", i+1, got, want)
		}
	}
	if env.s.builds.full != builds {
		t.Errorf("这 5 帧里满屏整窗重建了 %d 遍（原来 %d）——"+
			"连续位移不该靠整窗重建", env.s.builds.full, builds)
	}
}

// TestAPausedDanmakuStopsAndKeepsItsPlace 钉住「暂停时停在原地，而且不再要帧」。
func TestAPausedDanmakuStopsAndKeepsItsPlace(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()
	before, _ := redSpan(env.tt, danmakuArea(), 0)
	if before < 0 {
		t.Fatalf("画面上没有弹幕，文字有 %q", env.tt.Texts())
	}

	requests := env.s.full.danmaku.requests
	env.st.Playing = false
	env.tt.Frame()
	paused, _ := redSpan(env.tt, danmakuArea(), 0)
	if paused < 0 {
		t.Fatalf("暂停那一帧画面上没有弹幕了")
	}
	if d := paused - before; d < -2 || d > 2 {
		t.Errorf("暂停的那一帧弹幕跳了 %d 像素（x=%d → x=%d）——"+
			"位置没变，它不该动", d, before, paused)
	}
	if n := env.s.full.danmaku.requests; n != requests {
		t.Errorf("暂停之后弹幕还要了 %d 次下一帧（暂停前累计 %d）——"+
			"位置不会动，一直重画是白烧 CPU", n, requests)
	}

	// 推钟：暂停时没有位置变化，也没有绘制帧 ⇒ 弹幕不动。
	env.tt.Advance(500 * time.Millisecond)
	env.tt.Frame()
	still, _ := redSpan(env.tt, danmakuArea(), 0)
	if d := still - paused; d < -1 || d > 1 {
		t.Errorf("暂停期间弹幕还在走（x=%d → x=%d，差 %d）——"+
			"暂停时它该停在原地", paused, still, d)
	}
}

// TestTheDanmakuGoesOnFromWhereItPaused 钉住「恢复之后从原地继续」。
func TestTheDanmakuGoesOnFromWhereItPaused(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()

	env.st.Playing = false
	env.tt.Frame()
	paused, _ := redSpan(env.tt, danmakuArea(), 0)
	if paused < 0 {
		t.Fatalf("暂停那一帧画面上没有弹幕了")
	}

	requests := env.s.full.danmaku.requests
	env.st.Playing = true
	env.tt.Frame()
	resumed, _ := redSpan(env.tt, danmakuArea(), 0)
	if d := resumed - paused; d < -2 || d > 2 {
		t.Errorf("恢复的那一帧弹幕跳了 %d 像素（x=%d → x=%d）——"+
			"恢复该从原地接着走", d, paused, resumed)
	}
	if env.s.full.danmaku.requests == requests {
		t.Fatal("恢复播放之后弹幕没有要下一帧")
	}

	env.tt.Advance(300 * time.Millisecond)
	after, _ := redSpan(env.tt, danmakuArea(), 0)
	if got := resumed - after; got < 14 || got > 22 {
		t.Errorf("恢复后推了 300 毫秒，弹幕走了 %d 像素（want 18 上下）——"+
			"它没有接着往前走", got)
	}
}

// TestChangingTheSongResetsTheDanmaku 钉住「换歌之后上一首的弹幕、轨道和时钟
// 都不残留」。
func TestChangingTheSongResetsTheDanmaku(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("old", 41500, "上一首"),
	}, systemIsDark(true))
	env.tt.Frame()
	if !redSeen(env.tt, danmakuArea()) {
		t.Fatalf("换歌前画面上就没有弹幕，文字有 %q", env.tt.Texts())
	}

	// 下一首：不同的 rowid、不同的颜色、新的位置。
	env.s.app.SetDanmakuForTest([]app.DanmakuItem{
		danmakuItemColor("new", 9500, "下一首", danmakuGreen),
	})
	env.st.Position = 10 * time.Second
	env.tt.Frame()

	if redSeen(env.tt, danmakuArea()) {
		t.Error("换歌之后上一首的弹幕还在画面上——轨道和动画都该整份复位")
	}
	if !greenSeen(env.tt, danmakuArea()) {
		t.Errorf("换歌之后新的弹幕没有画出来，文字有 %q", env.tt.Texts())
	}

	d := &env.s.full.danmaku
	if len(d.slots) != 1 {
		t.Errorf("换歌之后还记着 %d 条弹幕的轨道（want 1 条新的）：%v",
			len(d.slots), d.slots)
	}
	if slot, ok := d.slots["new"]; !ok || slot.lane != 0 {
		t.Errorf("新的那条没有落在第 0 条轨道上（slot=%+v）——"+
			"换歌之后轨道该从空的那一份重新分配", slot)
	}
	if d.live != 1 {
		t.Errorf("换歌之后在屏的条数是 %d，want 1", d.live)
	}
	if d.lastBaseMS != 10000 {
		t.Errorf("换歌之后记的位置是 %d，want 10000", d.lastBaseMS)
	}
}

// TestSeekingBackwardsClearsTheStaleLanes 钉住「往回拖之后轨道复位、旧的
// 弹幕不残留」。
//
// # 为什么判据是「落回第 0 条轨道」
//
// 轨道上记的是「上一条入场时刻」。回退之后那些时刻还在**将来**，于是新位置
// 上的弹幕会被挤到后面的轨道上（甚至一条都放不进去）——屏幕上要么是空的，
// 要么是从中间的轨道冒出来。复位之后第一条新弹幕必然落在第 0 条轨道。
func TestSeekingBackwardsClearsTheStaleLanes(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("old", 59000, "回退前那一条"),
		danmakuItemColor("new", 19500, "回退后那一条", danmakuGreen),
	}, systemIsDark(true))
	env.st.Position = 60 * time.Second
	env.tt.Frame()
	if !redSeen(env.tt, danmakuArea()) {
		t.Fatalf("回退前画面上没有那一条红弹幕，文字有 %q", env.tt.Texts())
	}
	if greenSeen(env.tt, danmakuArea()) {
		t.Fatal("回退前第 19.5 秒那一条就在画面上——测试场景本身不对")
	}

	// 用户把进度条拖回第 20 秒。
	env.st.Position = 20 * time.Second
	env.tt.Frame()

	if redSeen(env.tt, danmakuArea()) {
		t.Error("往回拖之后将来那一条还留在画面上")
	}
	if !greenSeen(env.tt, danmakuArea()) {
		t.Errorf("往回拖之后新位置上的弹幕没有画出来，文字有 %q", env.tt.Texts())
	}
	d := &env.s.full.danmaku
	if slot, ok := d.slots["new"]; !ok || slot.lane != 0 {
		t.Errorf("往回拖之后新位置上的弹幕落在轨道 %+v 上——"+
			"回退该把轨道整份复位，第一条从第 0 条轨道重新开始", slot)
	}
	if d.lanes[0].timeMS != 19500 {
		t.Errorf("第 0 条轨道记的入场时刻是 %d，want 19500——"+
			"回退之后旧轨道状态还留着", d.lanes[0].timeMS)
	}
}

// TestLosingTheTrackClearsTheDanmaku 钉住「没有歌的那一帧弹幕那一摊也收掉」。
//
// # 它钉的是哪一个坏
//
// 满屏开着、换歌的那一瞬 `HasTrack` 是假（在线流要几百毫秒才装上），而
// `viewFullPlayer` 那一支**直接 return**——`viewFullDanmaku` 压根跑不到。
// 不在那儿收的话，换歌回来那一帧会把上一首的锚点和轨道荡回来。
func TestLosingTheTrackClearsTheDanmaku(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()
	if !env.s.full.danmaku.ready {
		t.Fatal("有歌的时候弹幕那一层没建起来")
	}

	env.st.HasTrack = false
	env.tt.Frame()

	d := &env.s.full.danmaku
	if d.ready || d.itemsByTime != nil || len(d.slots) != 0 || d.live != 0 {
		t.Errorf("没有歌之后弹幕状态还在：ready=%v items=%d slots=%d live=%d",
			d.ready, len(d.itemsByTime), len(d.slots), d.live)
	}
	if !d.baseAt.IsZero() {
		t.Errorf("没有歌之后锚点还留着（%v）", d.baseAt)
	}
}

// TestTurningTheDanmakuOffStopsTheFrames 钉住「设置里关掉之后一条都不画，
// 而且不再要绘制帧」。
func TestTurningTheDanmakuOffStopsTheFrames(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()
	if !redSeen(env.tt, danmakuArea()) {
		t.Fatalf("关掉之前画面上就没有弹幕，文字有 %q", env.tt.Texts())
	}

	requests, paints := env.s.full.danmaku.requests, env.s.full.danmaku.paints
	prefs := env.s.app.FullPlayerPrefs()
	prefs.ShowDanmaku = false
	env.s.app.SetFullPlayerPrefs(prefs)
	env.tt.Frame()

	if redSeen(env.tt, danmakuArea()) {
		t.Error("关掉弹幕之后画面上还有弹幕")
	}
	if n := env.s.full.danmaku.requests; n != requests {
		t.Errorf("关掉弹幕之后还要了 %d 次下一帧（关之前累计 %d）——"+
			"没有东西在动了", n, requests)
	}
	if n := env.s.full.danmaku.paints; n != paints {
		t.Errorf("关掉弹幕之后那一层还画了 %d 帧（关之前 %d）——它应该整个不建",
			n, paints)
	}
	d := &env.s.full.danmaku
	if d.itemsByTime != nil || d.live != 0 || !d.baseAt.IsZero() {
		t.Errorf("关掉弹幕之后状态没有收干净：items=%d live=%d baseAt=%v",
			len(d.itemsByTime), d.live, d.baseAt)
	}

	env.tt.Advance(300 * time.Millisecond)
	env.tt.Frame()
	if redSeen(env.tt, danmakuArea()) {
		t.Error("关掉弹幕之后推钟又画出了弹幕")
	}
}

// TestClosingTheFullPlayerStopsTheDanmakuFrames 钉住「退出满屏之后局部刷新
// 请求和状态都停掉」。
func TestClosingTheFullPlayerStopsTheDanmakuFrames(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true))
	env.tt.Frame()
	requests := env.s.full.danmaku.requests
	if requests == 0 {
		t.Fatal("播放中的弹幕一开始就没有要帧，这一条什么都验不到")
	}

	env.s.closeFullPlayer()
	env.tt.Frame()

	if redSeen(env.tt, danmakuArea()) {
		t.Error("退出满屏之后画面上还有弹幕")
	}
	// ⚠️ **关满屏是 `s.full = fullPlayer{}`**：那一份状态整个换掉，所以计数
	// 也归零。这里断的是「新那一份一次都没要过帧」。
	if n := env.s.full.danmaku.requests; n != 0 {
		t.Errorf("退出满屏之后还要了 %d 次下一帧——那一层已经不存在了", n)
	}
	if d := &env.s.full.danmaku; d.itemsByTime != nil || d.live != 0 || d.slots != nil {
		t.Errorf("退出满屏之后弹幕状态没有复位：items=%d live=%d slots=%v",
			len(d.itemsByTime), d.live, d.slots)
	}
}

// TestTheDanmakuKeepsItsLaneWhenTheViewRebuilds 钉住「重建之后同一条弹幕还在
// 同一条轨道上」。
//
// # 它顶替的是原来那条「key 用 rowid」的测试
//
// 原来每条弹幕是一个 `ui.Text` 元素，位移走 `AnimatePose`，而 pose 的状态按
// **元素的 key** 记——key 用下标的话，换一批弹幕会继承上一批的位移。
//
// 现在弹幕不再逐条建元素（它们是一层自绘的墨迹），那条状态挪到了
// `danmakuLayer.slots` / `lanes`，键仍然是**弹幕自己的 rowid**。这一条断的
// 就是它：整窗重建（同一份弹幕、位置往前走了 1 秒）之后，两条弹幕的轨道
// 一条都不许变。
func TestTheDanmakuKeepsItsLaneWhenTheViewRebuilds(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("a", 41000, "第一条"),
		danmakuItem("b", 41800, "第二条"),
	}, systemIsDark(true))
	env.st.Position = 42 * time.Second
	env.tt.Frame()

	d := &env.s.full.danmaku
	before := map[string]int{}
	for id, slot := range d.slots {
		before[id] = slot.lane
	}
	if len(before) != 2 {
		t.Fatalf("这一帧记下的弹幕是 %v，want 两条", before)
	}
	if before["a"] == before["b"] {
		t.Fatalf("两条弹幕挤在同一条轨道上（%v）——测试场景不对", before)
	}

	// 位置往前走 1 秒：整窗重建一份，track 分配必须照旧。
	env.st.Position += time.Second
	env.tt.Frame()
	for id, lane := range before {
		slot, ok := d.slots[id]
		if !ok {
			t.Errorf("重建之后 %q 从 slots 里不见了（%v）", id, d.slots)
			continue
		}
		if slot.lane != lane {
			t.Errorf("重建之后 %q 从第 %d 条轨道换到了第 %d 条——"+
				"轨道是入场那一刻定的，不该在重建时重算", id, lane, slot.lane)
		}
	}
}

// TestReduceMotionDoesNotAskForContinuousFrames 钉住「减少动态效果时不要
// 逐帧重绘」。
//
// 弹幕本来就是运动本身，那个偏好下不值得按显示器刷新率一直重画；位置仍然
// 跟着位置走，只是更新频率回到整窗那一档。
func TestReduceMotionDoesNotAskForContinuousFrames(t *testing.T) {
	env := danmakuPlayingEnv(t, []app.DanmakuItem{
		danmakuItem("d1", 41500, "第一条弹幕"),
	}, systemIsDark(true), reduceMotion())
	env.tt.Frame()
	before, _ := redSpan(env.tt, danmakuArea(), 0)
	if before < 0 {
		t.Fatalf("画面上没有弹幕，文字有 %q", env.tt.Texts())
	}
	if n := env.s.full.danmaku.requests; n != 0 {
		t.Errorf("「减少动态效果」开着的时候弹幕还要了 %d 次下一帧", n)
	}

	env.st.Position += 500 * time.Millisecond
	env.tt.Frame()
	after, _ := redSpan(env.tt, danmakuArea(), 0)
	if got := before - after; got < 26 || got > 34 {
		t.Errorf("位置往前 500 毫秒之后弹幕走了 %d 像素（want 30）——"+
			"偏好的只是刷新频率，位置还是跟着位置走", got)
	}
}
