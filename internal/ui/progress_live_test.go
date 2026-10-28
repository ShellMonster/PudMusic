package ui

// 这一组钉住「播放条那根进度**自己会走**」。
//
// # 为什么它必须单独一条
//
// 为了省掉每秒 20 次的整树重建（见 `internal/app/tick_wake.go` 的
// `readoutMoved`），整棵树现在**只在读数（秒数）变的时候**才重建。那意味着
// 两次重建之间，那根线的位置**只能靠 `Draw` 回调里的 `live()` 现算**——
// 没有 `live` 的话它一秒跳一格，看起来就是「卡住的进度」。
//
// 而 `live` 生效还要两件事，任何一件漏了都是**静默**的：
//
//   - `Draw` 里真的去读它（而不是用建树时算好的 `frac`）；
//   - 读完之后 `p.AnimationFrame()` 要下一帧——不要的话线只画在**这一帧**
//     的位置上，之后没人再画它。
//
// 这两件在画面上都表现为「差不多但就是不走」，肉眼要盯很久才看得出，所以
// 得有一条会红的测试。
//
// # 判据为什么是「要了下一帧」
//
// 无头 Tester 的 `Frame()` 走 `settle`——它每一遍都**强制**请求一帧再
// `runFrame`，所以在这个装置里量「重建了几遍」量的是 `settle` 的行为，不是
// 产品的。产品那一半（`p.AnimationFrame()` 的帧**不重建**）由框架自己的
// `TestRepaintFrames` 钉着（那里 `builds` 不涨而 `draws` 涨）。
//
// 所以这一层要钉的是：**我们有没有把「我还要再画」这句话说出去**
// （`tt.FramePending()`）。说出去了 = 那条局部重绘的路就成立了；
// 没说 = 线永远停在建树那一刻的位置。

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestThePlayingBarKeepsAskingForRepaints 钉住「在放的时候那根线一直在走」。
func TestThePlayingBarKeepsAskingForRepaints(t *testing.T) {
	tt, s := metaBarTester(t, app.State{
		HasTrack: true, Title: "在放的歌", Duration: 3 * time.Minute,
		Position: 30 * time.Second, Playing: true,
	}, 1000)

	if s.builds.playbar == 0 {
		t.Fatal("播放条一帧都没建，这一条什么都验不到")
	}
	if !tt.FramePending() {
		t.Fatal("在放的播放条画完一帧之后没有要下一帧——" +
			"那根线只画在建树那一刻，两次整树重建之间它是**不动**的" +
			"（用户看到一秒跳一格）")
	}
}

// TestAPausedBarDoesNotAskForFrames 钉住「暂停时那根线不再要帧」。
//
// `live` 只在**在放**的时候给：暂停时位置不动，每帧重画同一个位置是纯浪费
// ——而它会一直要下一帧，窗口就停不下来。这一条保住的是「暂停之后窗口能
// 停下来」，它是「笔记本在包里慢慢变热」那一类问题的第一道闸。
func TestAPausedBarDoesNotAskForFrames(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "停着的歌", Duration: 3 * time.Minute,
		Position: 30 * time.Second, Playing: false,
	}, 1000)

	// 先画一帧把建树那一阵的东西收掉。
	tt.Frame()
	if tt.FramePending() {
		t.Error("暂停的播放条画完一帧之后还在要帧——位置不会动，" +
			"再画一百帧也是同一根线（窗口因此停不下来，白耗电）")
	}
}

// TestAnUnfocusedPlayingBarStopsAskingForFrames 钉住「在放 + 失焦时那根线不再
// 要下一帧」。
//
// # 这一条是评审挑出来的（PR #612）
//
// `live` 的第一版只判了「在放」，没有判「用户看不看得见」。而失焦（但没最小化）
// 这一档**框架自己不管**：macOS 的 `occlusionState` 那时还是 visible，框架
// `next()` 里那条 `occluded()` 拦不住。于是链子自己续下去——
//
//	这一帧画到进度条 → `AnimationFrame()` → 下一帧（只重绘）→ 再画到 → ...
//
// 一扇收在后台的窗口按显示器刷新率一直重绘。同仓的波形
// （`widgets/hover/hover.go`）和频谱（`fullplayer_spectrum.go`）都判了
// `visible`，唯独这一颗漏了。
//
// # 为什么它和「暂停时不要帧」是两条
//
// 那一条挡的是「位置不动还在画」，这一条挡的是「位置在动、但用户看不见」。
// 两件事,两个原因,两条测试——合成一条的话红了看不出是哪一档坏的。
func TestAnUnfocusedPlayingBarStopsAskingForFrames(t *testing.T) {
	tt, s := metaBarTester(t, app.State{
		HasTrack: true, Title: "在放的歌", Duration: 3 * time.Minute,
		Position: 30 * time.Second, Playing: true,
	}, 1000)

	// 起点：在放、看得见 → 该要帧（不然下面那半什么都验不到）。
	if !tt.FramePending() {
		t.Fatal("起点就不对：在放、看得见的播放条没有要下一帧")
	}

	// 失焦（用户切到别的应用）。
	s.noteWindow(func(p *windowPresence) { p.focused = false })
	// 这一帧是「最后那一下」：让正在飞的动画收掉。
	tt.Frame()
	if tt.FramePending() {
		t.Error("失焦之后播放条还在要下一帧——" +
			"失焦这一档框架自己不管（occlusionState 还是 visible），" +
			"链子会在绘制路径里自己续下去：一扇收在后台的窗口按刷新率一直重绘" +
			"（用户能感到的是风扇和电池）")
	}
}
