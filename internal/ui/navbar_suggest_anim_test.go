// 联想面板的进场（票 #259）。
//
// # 这一票在做什么
//
// 顶栏那个联想面板**第一次出现**的时候有一小段过渡：**淡入 + 从下面升上来
// 10 DIP**，**200ms** + `motion.Unified`（和队列抽屉、侧栏折叠同一档）。
//
// 理由和出处见 `navbar_search.go` 的文件头《面板的进场》那一段（DESIGN.md §五
// 《不做》那一条的例外）。
//
// # 判据是「画面」
//
// AGENTS.md §2：查布局问题先对着画面量。面板的位置在画面上能量（那一条历史的
// 字在哪儿），不透明度能量（那一格里离底色最远的那个像素）。
//
// ⚠️ **Tester 的钟站着不动**：不推它的话画面停在进场的第一帧，而那一帧**面板
// 一个字都没画**（不透明度是 0）。所以这一族每一条都要自己推钟。
package ui

import (
	"image/color"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// suggestAnimTerm 是这一族量面板时用的那一条历史（面板上唯一一处稳当的墨）。
const suggestAnimTerm = "林俊杰"

// suggestAnimShell 造一个「顶栏搜索框 + 一条搜索历史」的外壳。
//
// ⚠️ **要有那条历史**：联想面板两档都空着的时候**根本不摆面板**（见
// `viewSuggest`），于是没有面板可进场。
func suggestAnimShell(t *testing.T, dark bool, opts ...shellOpt) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, append([]shellOpt{systemIsDark(dark)}, opts...)...)
	b.tt.SetFocused(true)
	b.s.hist.add(suggestAnimTerm)
	b.tt.Frame()
	return b.s, b.tt
}

// openSuggest 点一下搜索框，把浮层打开。
//
// ⚠️ **点完只画一帧**：面板从这一帧开始进场（进场的那一段走的是**框架的钟**，
// 见 `viewSuggest`），而 Tester 的钟站着不动——于是这一帧就是进场的第 0 毫秒。
func openSuggest(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
}

// settleSuggestEnter 把面板的进场推完（票 #259）。
//
// ⚠️ **要推的是框架的钟**（`tt.Advance`），不是那只假表（`advance(s, …)` 推的
// 是界面层那只）：进场那一段走的是 `AnimateTo`，它读的是框架的钟。
//
// ⚠️ **凡是要量面板上像素的用例都得先调它**：进场的第一帧面板是**全透明**的
// （不透明度 0），那时候画面上只有底下的东西。
func settleSuggestEnter(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	tt.Advance(suggestEnterDuration + 50*time.Millisecond)
	tt.Frame()
}

// suggestPanelMark 量浮层里那一格：它在哪儿（y），以及**面板画了多少**（墨）。
//
// 墨的判据是**离底色最远的那个像素**（`darkestIn`）：字的笔画中间是它自己的
// 颜色，边上那一圈抗锯齿是它和底的混合——离底最远的那个才是它本身。
//
// ⚠️ **底色是面板那一块的颜色**（`t.Background`）：面板的底和页面的底**是同一
// 个色**（上游的 `bg-background`），所以「面板画出来了」在画面上就是「那一格里
// 出现了离底更远的东西」——面板没画的时候，那一格是**底下**的东西（首页那几个
// Tab），离底色只有十几。
func suggestPanelMark(tt *mygoui.Tester, bg color.RGBA) (y float32, mark int) {
	return suggestMarkOf(tt, bg, suggestAnimTerm)
}

// suggestMarkOf 量浮层里**那一个字**所在的一格：它在哪儿（y），以及面板画了
// 多少（墨）。
//
// ⚠️ **量的是「字」而不是「面板」**：面板的底和页面的底是同一个色，面板画没画
// 在画面上就是「那一格里有没有离底更远的东西」——而字是那一格里最靠得住的一处。
func suggestMarkOf(tt *mygoui.Tester, bg color.RGBA, what string) (y float32, mark int) {
	chip, ok := tt.Find(what)
	if !ok {
		return 0, -1
	}
	ink, _ := darkestIn(tt, int(chip.X), int(chip.Y), int(chip.X+chip.W), int(chip.Y+chip.H), bg)
	return chip.Y, maxDiff(ink, bg)
}

// 那条曲线在两处的值（**自己算的，不是抄来的**）：
//
//	Unified(0.125) = 0.573130		（进场走了 1/8 的时间）
//	Unified(0.5)   = 0.954461		（进场走了 1/2 的时间）
//
// ⚠️ **别拿 `motion.Unified` 现算**：那样断的是「代码调的是同一个函数」，而
// 不是「同一种过渡走的是同一条曲线」——曲线换成别的（比如框架默认的 EaseOut，
// 它在 0.5 处是 0.875），这里会红。
const (
	suggestCurveAtEighth float64 = 0.573130
	suggestCurveAtHalf   float64 = 0.954461
)
