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
	"math"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
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
	chip, ok := tt.Find(suggestAnimTerm)
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

// TestTheSuggestPanelFadesInAndRises 断①：面板进场有 200ms 的过渡（淡入 +
// 上移 10 DIP），曲线是统一曲线。
//
// 判据是**画面上的两样**：面板的位置（那一条历史的字在哪儿）和面板的不透明度
// （那一格里离底色最远的那个像素）。
func TestTheSuggestPanelFadesInAndRises(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := suggestAnimShell(t, dark)
			openSuggest(t, tt)

			bg := rgb(BuildTheme(s.app.SettingsSnapshot(), dark).Background)
			// 第 0 毫秒：面板在下面 10 DIP，而且**一个字都没画**。
			y0, mark0 := suggestPanelMark(tt, bg)

			// 25 毫秒（1/8 的那一段）：曲线在这一刻走到 57.3%。
			tt.Advance(25 * time.Millisecond)
			tt.Frame()
			y1, mark1 := suggestPanelMark(tt, bg)

			// 100 毫秒（半程）：曲线走到 95.4%。
			tt.Advance(75 * time.Millisecond)
			tt.Frame()
			y2, mark2 := suggestPanelMark(tt, bg)

			// 200 毫秒：走完了。
			tt.Advance(100 * time.Millisecond)
			tt.Frame()
			y3, mark3 := suggestPanelMark(tt, bg)

			// ① 幅度：整段是 10 DIP（票面要的是 8~12 那一档）。
			if d := y0 - y3; d < 8 || d > 12 {
				t.Errorf("面板整段升了 %.2f DIP，want 10（票面要的是 8~12 那一档）", d)
			}

			// ② 曲线和时长：半程时位置已经走完 95.4%。
			//
			// ⚠️ **这一条同时钉住了时长**：100 毫秒时它**还没走完**（离终点还有
			// 0.46 DIP），而 200 毫秒时正好走完——时长就是 200ms，不是 100ms。
			wantY := y3 + float32(suggestEnterRise)*(1-float32(suggestCurveAtHalf))
			if math.Abs(float64(y2-wantY)) > 0.05 {
				t.Errorf("100ms 时面板在 y=%.4f，want %.4f（那条曲线在半程处是 95.4%%）.\n"+
					"差 %.4f 个像素：多半是曲线用错了（框架默认的 EaseOut 在半程处是 87.5%%）",
					y2, wantY, math.Abs(float64(y2-wantY)))
			}

			// ③ 曲线：1/8 的那一段也照着走（这一处把曲线和时长都钉死了——
			// 半程那一处曲线再像，1/8 那一处就露馅了）。
			wantY1 := y3 + float32(suggestEnterRise)*(1-float32(suggestCurveAtEighth))
			if math.Abs(float64(y1-wantY1)) > 0.05 {
				t.Errorf("25ms 时面板在 y=%.4f，want %.4f（那条曲线在 1/8 处是 57.3%%）",
					y1, wantY1)
			}

			// ④ 淡入：起手那一帧面板上一个字都没画。
			//
			// ⚠️ **和落位那一帧比**：面板的底和页面的底是同一个色，所以「画了
			// 没有」在画面上就是「那一格里有没有离底更远的东西」。
			if mark0 > mark3/4 {
				t.Errorf("第一帧面板上已经有东西了（离底 %d，落位那一帧是 %d）——它没有淡入",
					mark0, mark3)
			}

			// ⑤ 淡入走的是同一条曲线：1/8 那一处和半程那一处都照着走。
			//
			// ⚠️ **1/8 那一处是最能挑出错曲线的一处**：统一曲线在 1/8 处已经
			// 走到 57.3%，而框架默认的 EaseOut 在那儿只有 23.4%——差着一倍。
			wantMark1 := float64(mark0) + (float64(mark3)-float64(mark0))*suggestCurveAtEighth
			if math.Abs(float64(mark1)-wantMark1) > 12 {
				t.Errorf("25ms 时面板的墨离底 %d，want %.0f（那条曲线在 1/8 处是 57.3%%）",
					mark1, wantMark1)
			}
			wantMark := float64(mark0) + (float64(mark3)-float64(mark0))*suggestCurveAtHalf
			if math.Abs(float64(mark2)-wantMark) > 12 {
				t.Errorf("100ms 时面板的墨离底 %d，want %.0f（半程处该是 95.4%%）.\n"+
					"差 %.0f：多半是淡入没跟着那条曲线走",
					mark2, wantMark, math.Abs(float64(mark2)-wantMark))
			}

			// ⑥ 走完之后不再动（位置和墨都不再变）。
			tt.Advance(100 * time.Millisecond)
			tt.Frame()
			y4, mark4 := suggestPanelMark(tt, bg)
			if y4 != y3 || mark4 != mark3 {
				t.Errorf("进场走完之后面板还在动（y %.4f→%.4f、墨 %d→%d）", y3, y4, mark3, mark4)
			}
		})
	}
}

// TestTheSuggestPanelVanishesAtOnce 断②：**退场是瞬消**。
//
// 框架的 `PopoverBase` 在 `open == false` 时**根本不 build 那个 panel** → 面板
// 直接消失（要做退场就得自己接管开关状态，代价大于收益——理由见
// `navbar_search.go` 的文件头）。
//
// 判据是**画面上那一格没有了**：不是「淡到看不见」，是**不在树上**。
func TestTheSuggestPanelVanishesAtOnce(t *testing.T) {
	s, tt := suggestAnimShell(t, false)
	openSuggest(t, tt)
	settleSuggestEnter(t, tt)
	if _, ok := tt.Find(suggestAnimTerm); !ok {
		t.Fatalf("浮层上没有那条历史，文字有 %q", tt.Texts())
	}

	// 关掉。
	s.searchSuggest = false
	tt.Frame()

	if _, ok := tt.Find(suggestAnimTerm); ok {
		t.Error("关掉之后浮层上还有那条历史——它是淡出去的（这一票只做进场，退场是瞬消）")
	}
}

// TestTheSuggestPanelDoesNotReplayWhenTheTermsArrive 断③：面板开着、内容变了
// **不重播**那一段进场。
//
// 场景就是真的那一个：用户敲了字 → 面板开着（历史那一档）→ 500 毫秒后联想词
// 回来 → **面板的内容变了**。那时候它**不能**再淡一遍、也不能再升一遍——用户
// 正盯着它看词。
//
// 判据是**画面**：内容变了之后，那一条历史的位置和墨都**一个字都没变**。
func TestTheSuggestPanelDoesNotReplayWhenTheTermsArrive(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦")
	s, tt := newSuggestShell(t, f)
	s.hist.add(suggestAnimTerm)
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	settleSuggestEnter(t, tt)

	bg := rgb(BuildTheme(s.app.SettingsSnapshot(), false).Background)
	y0, mark0 := suggestPanelMark(tt, bg)

	// 联想词回来：面板的内容变了。
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词出来了", func() bool { return tt.HasText("周杰伦") })

	y1, mark1 := suggestPanelMark(tt, bg)
	if y1 != y0 {
		t.Errorf("内容变了之后面板动了一下（y %.4f → %.4f）——进场的动画重播了", y0, y1)
	}
	if mark1 != mark0 {
		t.Errorf("内容变了之后面板淡了一下（墨 %d → %d）——进场的动画重播了", mark0, mark1)
	}
}

// TestTheSuggestPanelHonoursReduceMotion 断④：系统开着「减少动态效果」时
// **直接到终点**。
//
// 判据是**开面板的第一帧**：那一帧就该是落位的样子（满亮、在该在的地方），
// 不推钟也一样。
//
// ⚠️ **这一条走的是框架那一条**（`AnimateTo` 读到 `Preferences.ReduceMotion`
// 就落到目标），所以它同时钉住「这一层没有绕开框架自己写一段动画」。
func TestTheSuggestPanelHonoursReduceMotion(t *testing.T) {
	bg := rgb(BuildTheme(app.DefaultSettings(""), false).Background)

	// 正常那一套的落位样子（拿来当「直接到终点」该有的样子）。
	_, ttNormal := suggestAnimShell(t, false)
	openSuggest(t, ttNormal)
	settleSuggestEnter(t, ttNormal)
	wantY, wantMark := suggestPanelMark(ttNormal, bg)

	_, tt := suggestAnimShell(t, false, withPrefs(mygoui.Preferences{ReduceMotion: true}))
	openSuggest(t, tt)
	gotY, gotMark := suggestPanelMark(tt, bg)

	if math.Abs(float64(gotY-wantY)) > 0.05 {
		t.Errorf("减少动态效果时面板在 y=%.4f，want %.4f——它应该直接到位", gotY, wantY)
	}
	if math.Abs(float64(gotMark-wantMark)) > 2 {
		t.Errorf("减少动态效果时面板的墨离底 %d，want %d——它应该直接到位", gotMark, wantMark)
	}
}
