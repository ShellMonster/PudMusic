package ui

import (
	"image"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// 这一组钉住「**路由换页没有过渡**」（票 #469）。
//
// # 为什么要有这一组
//
// `DESIGN.md` §五《不做》第一条写着「页面切换动画（上游没有）」，§九也把它列进
// 「已经拍板、不要重新提的」。而框架的 `Router.Transition` **零值是 `TransitionAuto`**
// ——不设它就是开着一条 250ms 滑入 / 150ms 淡入。2026-10-25 实测证实那条兜底
// **一直在跑**，而**没有任何测试**报过警：现象只是「换页滑了一下」，不报错、
// 不崩溃，评审也看不出来。
//
// 所以这里钉的不是「动画长什么样」，是「**换页是硬切**」——那条兜底一旦被打开，
// 这一组立刻红。
//
// # 判据为什么是「换页那一帧 == 走完之后那一帧」
//
// 转场（框架的 `Router.build`，`mygo@v0.6.3/ui/router.go:544-569`）会让**离场页
// 和新页同时画在画面上**：离场页往左挪、变暗，新页从右边滑进来盖住它。
// 所以「换页那一帧」和「动画走完之后那一帧」**长得不一样**——差的是整整一张页面。
//
// 而硬切时，框架只建新页（`v.leaving` 是 nil），所以这两帧**逐像素相同**。
//
// 这是一个**像素计数**判据：不看毫秒、不数帧，所以**不受机器负载影响**
// （AGENTS.md §2 那条禁令针对的是时间类判据，这条不是）。
//
// ⚠️ **为什么不用「旧页的字还在不在」**：离场页在框架里带 `flagInert`
// （`mygo@v0.6.3/ui/router.go:679`），而绘制遍历会**跳过**带这个标志的节点
// （`runtime.go:1032`），所以离场页上的字**根本不进** `tt.Texts()`——
// 那个判据量到的是「树里有没有那个节点」，不是「画面上有没有那行字」，
// 两页叠着的时候它照样说「旧页不在了」。
//
// 另有一道**对照**：同一个测量在「开着动画」的壳上必须量到**差**——
// 没有这一道，这一条有可能因为「探针根本没生效」而恒绿。

// pageDiff 数两幅图不一样的像素数。
func pageDiff(a, b *image.RGBA) int {
	n := 0
	for y := 0; y < a.Rect.Dy() && y < b.Rect.Dy(); y++ {
		for x := 0; x < a.Rect.Dx() && x < b.Rect.Dx(); x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// pageCopy 复制一幅图（`tt.Image()` 给的是内部那张，下一帧就被覆盖了）。
func pageCopy(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, src.Rect.Dx(), src.Rect.Dy()))
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			dst.Set(x, y, src.RGBAAt(x, y))
		}
	}
	return dst
}

// TestTheRouterHasNoPageTransition 钉住「换页是硬切：换页那一帧和走完之后那一帧
// 逐像素相同」。
//
// ⚠️ **判据是画面**（像素差），不是 `s.router.Transition` 那个字段：读字段只能
// 证明「我们设了那个值」，而**框架哪天改了默认行为**的话，字段照样是
// `TransitionNone`，画面上却多出一条动画。
func TestTheRouterHasNoPageTransition(t *testing.T) {
	s, tt := newShell(t)

	tt.Frame()
	s.navigate(settingsPath)
	tt.Frame()
	// 换页那一帧：硬切时它已经是终态了。
	afterSwitch := pageCopy(tt.Image())

	// 推钟走完一整段（硬切时这段什么也不发生；开着动画时它把过渡走完）。
	for range 30 {
		tt.Advance(16 * 1000000)
		tt.Frame()
	}
	settled := pageCopy(tt.Image())

	if got := pageDiff(afterSwitch, settled); got != 0 {
		t.Errorf("换页那一帧和走完之后那一帧差 %d 像素——**换页有过渡**。"+
			"规范说页面切换动画不做（DESIGN.md §五《不做》），"+
			"框架的默认值却是 TransitionAuto（进深 250ms 滑入 / 平级 150ms 淡入），"+
			"所以必须显式设 TransitionNone（internal/ui/router.go 的 newRouter）", got)
	}
}

// TestThePageTransitionProbeWouldNoticeItIfItCameBack 是上面那条的**对照**。
//
// ⚠️ **这一条不能删**：没有它，上面那条有可能因为「探针根本没生效」而恒绿——
// 也就是说，框架哪天把那条兜底打开了，我们**什么都不会知道**。
// 这里的做法是**故意把动画打开**（`withPageAnimation`），然后要求探针**必须**
// 量到差：量不到，就是探针坏了。
func TestThePageTransitionProbeWouldNoticeItIfItCameBack(t *testing.T) {
	s, tt := newShell(t, withPageAnimation())

	tt.Frame()
	s.navigate(settingsPath)
	tt.Frame()
	afterSwitch := pageCopy(tt.Image())

	for range 30 {
		tt.Advance(16 * 1000000)
		tt.Frame()
	}
	settled := pageCopy(tt.Image())

	if got := pageDiff(afterSwitch, settled); got == 0 {
		t.Fatalf("动画开着，探针却量不到差别——**探针坏了**，" +
			"上面那条 TestTheRouterHasNoPageTransition 就是恒绿的，它挡不住任何东西")
	}
}

// TestTheRouterTransitionIsSetExplicitly 钉住「那个值是**显式**设的」。
//
// ⚠️ **这一条和上面那两条不是一回事**：上面那两条钉的是**画面**（用户看见的），
// 这一条钉的是**代码**（我们没靠框架的默认值）。
//
// 少了这一条，上面那两条绿着也可能是因为**框架自己**把 `TransitionNone` 也做成了
// 带过渡的样子——那时画面上多一条动画，而我们代码里一个字都没改。
func TestTheRouterTransitionIsSetExplicitly(t *testing.T) {
	s, _ := newShell(t)
	if got := s.router.Transition; got != mygoui.TransitionNone {
		t.Errorf("router.Transition = %v，want TransitionNone(%v)——"+
			"**不靠框架的默认值**：它的零值是 TransitionAuto（一条 250ms 滑入 / "+
			"150ms 淡入），规范说页面切换动画不做（DESIGN.md §五《不做》），"+
			"所以这个值必须显式写出来", got, mygoui.TransitionNone)
	}
}

// TestTheMiniWindowAlsoHasNoPageTransition 钉住「迷你播放器那一扇窗也一样」。
//
// ⚠️ **迷你窗口有它自己的 router**（`NewMini` 走的是 `newShellWithImages`，
// 同一个 `newRouter`），而它画的是**另一个窗口的同一段内容**——
// 换句话说，两扇窗是两个 router，而**每一个**都得显式关掉。
// 只关主窗口那一扇的话，迷你播放器上换页照样滑。
func TestTheMiniWindowAlsoHasNoPageTransition(t *testing.T) {
	main := newTestShell(t).s
	mini := NewMini(main.app, main, func() {})
	t.Cleanup(mini.closePerWindow)
	if got := mini.router.Transition; got != mygoui.TransitionNone {
		t.Errorf("迷你播放器那一扇的 router.Transition = %v，want TransitionNone(%v)",
			got, mygoui.TransitionNone)
	}
}
