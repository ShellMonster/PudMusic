// 满屏播放器那一档：封面在转（票 #468）。
//
// # 这一族断的是四件事
//
//	转起来了        画面上那一块封面真的在转（不是「代码里有个角度」）
//	走的是 pose 那一档 转的时候**不重建界面**（那是这一票最要紧的一处成本）
//	停得干净        暂停之后它不再转（不在看不见的地方空转）
//	听系统的偏好    「减少动态效果」开着的时候它不转
//
// # 为什么这一族喂的是**手摆的状态**
//
// 「在不在放」是应用层那一栏 `Playing`——它由后台那个 **tick**（每 50ms 一次）
// 从解码器那里读回来，于是「在不在放」这件事**取决于机器有多快**。
// 而这一族要断的恰恰是「封面转不转」——它要是也取决于机器快慢，这条测试就是
// 掷骰子（票 #468 落地的时候，这一族的第一版就是这么飘的）。
//
// 所以这里喂一份**手摆的** `app.State`（和 `framebudget_test.go` 的
// `budgetState` 同一个路子，见它那儿的理由）：转不转由测试自己说了算。
package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// spinTester 造一个「只画满屏播放器」的外壳，喂一份手摆的状态。
//
// ⚠️ **状态是手摆的**（理由见文件头）：`Playing` 由测试自己说。
//
// ⚠️ **它只画满屏那一块**（`withView`）：整窗的分配里混着侧栏、顶栏和别的页面，
// 断「转的时候有没有重建」的时候只看满屏这一块 attributable。
func spinTester(t *testing.T, st app.State, opts ...shellOpt) (*Shell, *mygoui.Tester) {
	t.Helper()
	opts = append(opts, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			s.viewFullPlayer(c, st)
		}
	}))
	s, tt := newShell(t, opts...)
	s.full.open = true
	return s, tt
}

// spinPlayingState 是「正在放」的满屏那一副状态（`budgetState` 那一副的翻版）。
func spinPlayingState() app.State {
	st := budgetState()
	st.Playing = true
	return st
}

// armTheSpin 走到「封面已经在转」的那一刻。
//
// ⚠️ **要两帧**：框架把一条 pose 通道建出来的时候是**建在目标上、歇着的**
// （`mygo/ui/pose.go`：「Not posed in the frame before: it shows there at
// once」）——所以第一帧那一转是**没动**的，第二帧才被重新瞄了一个目标、
// 真的动起来。真应用里第二帧是 50ms 之后（应用层每 50ms 叫醒一次窗口）。
func armTheSpin(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	tt.Frame()
	tt.Frame()
}

// TestTheFullCoverTurns 断「封面真的在转」。
//
// # 判据是**看得见的那个框**在长大
//
// 一个正方形转起来之后，它**可见的外接框**是 320×(cosθ+sinθ)——θ=0 时是 320，
// θ=45° 时是 320×1.414 ≈ 452。框架给无障碍标签的那个框就是**外接框**
// （`mygo/ui/runtime.go` 的 `visualRect`），所以它变大了就是转起来了。
//
// ⚠️ **别用「中心有没有动」**：中心是**转轴**，它一动不动（那正好是另一条
// 该断的事——见下面那条）。
func TestTheFullCoverTurns(t *testing.T) {
	st := spinPlayingState()
	_, tt := spinTester(t, st)
	armTheSpin(t, tt)

	before, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("画面上找不到满屏那张封面，文字有 %q", tt.Texts())
	}
	if before.W != 320 {
		t.Fatalf("满屏那张封面是 %v 宽，want 320（设计规范 v0.1 §7.5）", before.W)
	}

	// 33⅓ rpm = 200 度/秒 → 225 毫秒是 45 度。
	tt.Advance(225 * time.Millisecond)
	// ⚠️ **要再画一帧（重建）**：那 225 毫秒里画的是「只重绘」的帧，而框架
	// 给无障碍标签的那个框是**重建时**量的（`mygo/ui/runtime.go` 的
	// commitElement）——只重绘的那些帧里它还是上一次的值。
	tt.Frame()

	after, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("画面上找不到满屏那张封面，文字有 %q", tt.Texts())
	}
	if after.W <= before.W {
		t.Errorf("推进 225 毫秒之后封面那个框还是 %v 宽（转起来之前也是 %v）——它没有在转",
			after.W, before.W)
	}
	// 转 45 度的话，外接框该是 320×(cos45+sin45) ≈ 452。
	if maxW := float32(320 * 1.4143); after.W > maxW+1 {
		t.Errorf("封面那个框宽到了 %v，比「转 45 度」该有的 %v 还大——它转得不对劲",
			after.W, maxW)
	}
	// 中心是转轴：它一动不动。
	if d := abs32(after.X + after.W/2 - (before.X + before.W/2)); d > 1 {
		t.Errorf("封面的中心挪了 %v 像素——转是**绕着中心**转的，中心不该动", d)
	}
}

// TestTheFullCoverTurnsWithoutRebuildingTheWindow 是这一票最要紧的一处成本：
// **转的时候不重建界面**。
//
// # 判据是「建了几遍」，不是「花了几毫秒」
//
// 框架有两档动效：`AnimateTo` 那一档每一帧都要**重建 view + 布局**，
// `AnimatePose` 那一档**只重绘帧**（`mygo/ui/pose.go:32`）。常驻的旋转必须走
// 后者——走前者的话，满屏开着的时候每秒要重建 60 遍整棵满屏播放器
// （票 #392 治的正是这个毛病）。
//
// 而这一条量的是**次数**（不受机器快慢影响，AGENTS.md §2）：转一秒，
// 满屏那一块建了几遍。走 pose 那一档的话是 **0 遍**——那几帧都是「只重绘」。
func TestTheFullCoverTurnsWithoutRebuildingTheWindow(t *testing.T) {
	st := spinPlayingState()
	s, tt := spinTester(t, st)
	armTheSpin(t, tt)

	s.builds = buildTally{}
	tt.Advance(time.Second)
	if got := s.builds.full; got != 0 {
		t.Errorf("封面转的那一秒里，满屏播放器重建了 %d 遍，want 0。\n"+
			"旋转走的是 `AnimatePose` 那一档（只重绘帧，不重建 view）——"+
			"重建 %d 遍说明它走的是 `AnimateTo` 那一档：每一帧都要重建整棵"+
			"满屏播放器（票 #392 治的正是这个）。", got, got)
	}
}

// TestTheFullCoverStopsTurningWhenPaused 断「停得干净」。
//
// ⚠️ **停是「停在它此刻转到的地方」**，不是回到 0 度（那是一下跳，封面「啪」
// 地转正）。所以判据是：**暂停之后再推进半秒，封面那个框一点没变**。
//
// ⚠️ **别用「有没有还在要帧」当判据**：那一帧之后框架确实不再要帧了（通道歇了），
// 但「不再要帧」这件事和「封面没有再转」不是一回事——前者是**后果**，后者是
// **画面上的事实**。这里断的是后者。
func TestTheFullCoverStopsTurningWhenPaused(t *testing.T) {
	st := spinPlayingState()
	_, tt := spinTester(t, st)
	armTheSpin(t, tt)

	// 先转一会儿（不转的话，下面那条「停住了」就是白停）。
	tt.Advance(500 * time.Millisecond)
	moving, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("画面上找不到满屏那张封面，文字有 %q", tt.Texts())
	}

	// 暂停。
	st.Playing = false
	tt.Frame()

	stopped, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("画面上找不到满屏那张封面，文字有 %q", tt.Texts())
	}
	tt.Advance(500 * time.Millisecond)
	after, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("画面上找不到满屏那张封面，文字有 %q", tt.Texts())
	}
	if after.W != stopped.W || after.X != stopped.X {
		t.Errorf("暂停之后封面那个框从 %v,%v 变成了 %v,%v —— 它还在转",
			stopped.X, stopped.W, after.X, after.W)
	}
	if stopped.W == moving.W {
		t.Errorf("暂停之前封面那个框一直是 %v 宽——它压根没转过，"+
			"这一条断的「停」就没有意义", moving.W)
	}
}

// TestTheFullCoverDoesNotTurnWithReduceMotion 断「听系统的偏好」。
//
// 系统开着「减少动态效果」时**不许转**（DESIGN.md §五《减少动态效果》）。
//
// ⚠️ **框架只管它自己算的那一半**：ReduceMotion 会让 pose 的值**直接到终点**
// ——那是「停在某一个角度」，不是「不转」。所以这一档得自己判，而判据是
// **画面上的那个框**：不转的时候它就是封面本身那么大（320）。
func TestTheFullCoverDoesNotTurnWithReduceMotion(t *testing.T) {
	st := spinPlayingState()
	_, tt := spinTester(t, st, reduceMotion())
	armTheSpin(t, tt)

	tt.Advance(500 * time.Millisecond)

	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("画面上找不到满屏那张封面，文字有 %q", tt.Texts())
	}
	if cover.W != 320 {
		t.Errorf("开着「减少动态效果」的时候封面那个框是 %v 宽，want 320"+
			"（不转的时候它就是封面本身那么大）", cover.W)
	}
}

// TestRenderFullCoverSpinShots 存图：满屏播放器转起来的封面。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullCoverSpinShots
//
// 存四张：**深浅各一套**（系统外观）× **两套主题方案**（设置里的「暖纸 / 暮光」），
// 每一张里封面都转到**同一个角度**（45 度），这样两套之间能直接比。
//
// # 为什么这一族出图用的是**隔离**的那一份
//
// 整窗那一份的歌是**真的在放**的，而那个 FLAC 素材只有几秒长——放完了
// `Playing` 就变假，封面于是**停了**（这正是它该做的：歌放完了就不转了）。
// 于是整窗那一份出的图上，封面转到哪儿是**看机器快慢**的（后台那个 tick 每
// 50ms 叫醒一次窗口，每叫醒一次角度就往前跳 10 度）。
//
// 隔离的这一份喂的是**手摆的状态**（`Playing: true` 一直是真的），于是角度是
// **推出来的**：推 225 毫秒 = 45 度（200 度/秒），每一张图都是同一个角度。
func TestRenderFullCoverSpinShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, tc := range []struct {
		name   string
		dark   bool
		preset app.ThemePreset
	}{
		{"浅色-暖纸", false, app.ThemePresetWarmPaper},
		{"深色-暖纸", true, app.ThemePresetWarmPaper},
		{"深色-暮光", true, app.ThemePresetDuskAmber},
		{"浅色-柔雾", false, app.ThemePresetSoftPeach},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 一张真封面（左右两半不一样，转起来看得出转的是哪一块）。
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Write(splitCoverPNG(t, 1200, 1200))
			}))
			t.Cleanup(srv.Close)

			settings := app.DefaultSettings("")
			settings.ThemePreset = tc.preset
			st := spinPlayingState()
			st.CoverURL = srv.URL + "/cover.png"
			s, tt := spinTester(t, st, withSettings(settings))
			s.app.PutLyrics(budgetLRC, "")
			tt.SetDark(tc.dark)

			// 等封面真的到了（图片是后台拉的，Tester 的钟不管它）。
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				if s.images.Get(st.CoverURL).Ready() {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !s.images.Get(st.CoverURL).Ready() {
				t.Fatal("封面没到")
			}

			// 转 45 度（33⅓ rpm = 200 度/秒 → 225 毫秒）。
			armTheSpin(t, tt)
			tt.Advance(225 * time.Millisecond)
			tt.Frame()

			save(t, dir, "468-满屏播放器-封面在转-"+tc.name+".png", tt)
		})
	}
}

// TestTheCoverSpinDoesNotAddPerFrameAllocations 断「转起来不花钱」。
//
// # 判据是**每一帧的分配次数**，不是毫秒
//
// 分配次数是进程级的全局计数（`runtime.MemStats.Mallocs`），而它是「这一帧建了
// 多少个对象」——**不受机器快慢影响**（AGENTS.md §2：不要用固定毫秒当判据）。
//
// # 量的是**每一帧重建**的分配次数
//
// 转起来的时候那一帧是「只重绘」的（不重建 view），而 `tt.Frame()` 会一直画到
// 界面不再要帧（最多二十帧）——所以转的时候一次 `tt.Frame()` 是**二十次重建**。
// 分配次数除以**真正建了几遍**（`s.builds.full` 那个计数钩子），两边就是同一个
// 单位了。
//
// ⚠️ **两边要能比**：不转的那一份只是 `Playing: false`（界面一模一样，只是不转），
// 所以差出来的那个数就是「转这一下多花了几次分配」。
func TestTheCoverSpinDoesNotAddPerFrameAllocals(t *testing.T) {
	off := measureSpinAllocs(t, false)
	on := measureSpinAllocs(t, true)
	t.Logf("不转：每帧重建 %.1f 次分配；转：每帧重建 %.1f 次分配", off, on)
	if on > off+1 {
		t.Errorf("转起来之后每一帧重建多分配了 %.1f 次（不转 %.1f，转 %.1f）。\n"+
			"转动的那个 pose 只该是几次数值的写入（`AnimatePose` 不建任何东西）——"+
			"多出来的是「每一帧重建一整棵满屏播放器」那笔（票 #392 治的正是它）。",
			on-off, off, on)
	}
}

// measureSpinAllocs 量「转 / 不转」两种情况下**每一帧重建**的分配次数。
func measureSpinAllocs(t *testing.T, playing bool) float64 {
	t.Helper()
	st := spinPlayingState()
	st.Playing = playing
	s, tt := spinTester(t, st)
	armTheSpin(t, tt)

	// 热身：头几帧有一次性开销（字形进图集、元素池涨到这一屏的规模、
	// 那条 pose 通道第一次被建出来）。
	for range 8 {
		tt.Frame()
		runtime.Gosched()
	}

	s.builds = buildTally{}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	const frames = 20
	for range frames {
		tt.Frame()
		runtime.Gosched()
	}
	runtime.ReadMemStats(&after)

	rebuilds := s.builds.full
	if rebuilds == 0 {
		t.Fatal("一帧都没重建——量不了")
	}
	return float64(after.Mallocs-before.Mallocs) / float64(rebuilds)
}

// TestTheFullCoverKeepsTurningBetweenRebuilds 断「转是**连续的**，不是重建时
// 才跳一下」。
//
// # 为什么要有这一条
//
// 「转起来」这件事有两种做法，**画面上看着差不多，成本差得远**：
//
//	重建的时候把角度改一下   每一帧都要重建整棵满屏播放器（`AnimateTo` 那一档）
//	一条通道一直往前走      中间的帧只是**重画**（`AnimatePose` 那一档）
//
// 而它们在**画面上**的区别只有一处：**重建之间**（也就是「只重绘」的那些帧里）
// 封面还在不在动。所以判据是：**推一段时间、一次都不重建，画面上的像素变了
// 没有**。
//
// ⚠️ **别拿「框变大了没有」当判据**：那个框（无障碍标签的框）是**重建时**量的，
// 只重绘的帧里它还是上一次的值（`mygo/ui/runtime.go` 的 commitElement）。
// 所以这里比的是**画面本身**。
func TestTheFullCoverKeepsTurningBetweenRebuilds(t *testing.T) {
	st := spinPlayingState()
	_, tt := spinTester(t, st)
	armTheSpin(t, tt)

	before := snapshot(tt)
	// 推进 100 毫秒：这中间**没有一次重建**（那几帧是「只重绘」的）。
	tt.Advance(100 * time.Millisecond)
	after := snapshot(tt)

	if changed := changedPixels(before, after); len(changed) == 0 {
		t.Error("推进 100 毫秒（中间一次都没重建），画面一个像素都没变——" +
			"封面只在重建的时候才动一下，那不是「转」")
	}
}

// TestTheFullCoverStopsAskingForFramesWhenTheWindowIsNotSeen 断「看不见就停」。
//
// # 为什么这一条要紧
//
// 满屏的封面在转的时候，要帧走的是**框架那一层**（pose 的通道 →
// `rt.repainting` → `requestFrame`），**不经过本应用那道「看不见就不画」的闸**
// （`wake.go` 的 `invalidateIf` 只挡应用自己发的唤醒）。所以这一档要是没人管，
// 窗口失焦之后仍然每秒画 60 帧（票 #156 治的正是那个风扇和电池的问题）。
//
// # 判据是「还要不要下一帧」
//
// 「看不见」在应用层是**失焦**那一档（框架管不了：窗口还在屏幕上，只是没有
// 焦点），于是它得由这一层自己挡（`presence.go`）。挡住的标志是**不再要帧**。
func TestTheFullCoverStopsAskingForFramesWhenTheWindowIsNotSeen(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	// 推够钟，让**共享元素转场**先走完——它自己也要帧，不走完的话下面那条
	// 「还在要帧」量的是转场而不是封面那一转。
	tt.Advance(800 * time.Millisecond)
	if !tt.FramePending() {
		t.Fatal("满屏开着、歌在放，界面却不再要帧了——封面那一转压根没转起来，" +
			"下面那条就量不到东西")
	}

	// 窗口看不见了（失焦：框架管不了的那一档）。
	s.noteWindow(func(p *windowPresence) { p.focused = false })
	tt.Advance(200 * time.Millisecond)
	if tt.FramePending() {
		t.Error("窗口看不见了，界面还在要帧——封面那一转要帧走的是框架那一层，" +
			"不经过本应用那道闸，得由这一档自己判（见 fullplayer_spin.go）")
	}
}
