package ui

import (
	"fmt"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"

	mygoui "github.com/egoist/mygo/ui"
)

// 播放条上的倍率控件。
//
// # 为什么这些断言非写不可
//
// 倍速是这一票唯一的「新控件」，而它出问题时**屏幕上什么异样都没有**：进度条照
// 走、声音照出，只有音高悄悄变了。所以这一组用例盯的是三件人眼能看见的事：
//
//	按钮上写的是不是当前倍率
//	点开之后七档是不是都在
//	按下去之后状态和字有没有一起变
//
// 音高对不对在 `internal/service/audio/rate_test.go` 里量（那儿拿频谱说话）；
// 换歌之后倍率还在不在在 `internal/app/rate_test.go` 里量（那儿才开得了播放器）。

// rateBarTester 画播放条，State 每帧从 App 现读——和真窗口里一模一样。
//
// 现读是必须的：这一组要验的正是「按下去 → App 改了 → 界面下一帧读到」。
// 传一份钉死的 State 进来，等于把要验的那半段先接上了假线。
func rateBarTester(t *testing.T, width int) (*mygoui.Tester, *app.App, *Shell) {
	t.Helper()
	a := app.New()
	b := newTestShell(t, onApp(a), at(width, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, a.Snapshot())

		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, a, b.s
}

// TestRateButtonShowsTheCurrentRate 盯按钮上的字就是当前倍率。
func TestRateButtonShowsTheCurrentRate(t *testing.T) {
	tt, a, _ := rateBarTester(t, 760)
	if !tt.HasText("1x") {
		t.Fatalf("默认是原速，按钮上却不是「1x」：%q", tt.Texts())
	}

	for _, r := range []float64{0.5, 0.75, 1.25, 1.5, 1.75, 2} {
		a.SetRate(r)
		tt.Frame()
		if want := rateLabel(r); !tt.HasText(want) {
			t.Errorf("倍率是 %v，按钮上却是 %q：%q", r, want, tt.Texts())
		}
	}
}

// TestRateIsReadEveryFrame 盯读数是**每帧现读**的，不是按下时记下来的。
//
// 这一条钉的是「换歌之后不会回到 1x」：倍率是用户的一个设置（记在 App 上），
// 新歌开好时套上去（见 `app.App.Open`）。界面上要是把按下那一刻的值缓存住，
// 换歌之后按钮回到 1x 而声音是 1.5 倍——界面上唯一能看见倍速的地方说错了话。
//
// 做法和 `TestQualityBadgeFollowsTheRefresh` 一样：直接喂 State，只改闭包里
// 那个变量，别的一律不动——这一条验的正是「喂进来的东西一变，画面就跟着变」。
func TestRateIsReadEveryFrame(t *testing.T) {
	st := app.State{HasTrack: true, Title: "一首歌", Rate: 1}
	b := newTestShell(t, at(760, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, st) // ← 闭包读的是外面那个变量

		}
	}))
	tt := b.tt

	tt.Frame()
	if !tt.HasText("1x") {
		t.Fatalf("一开始就没有「1x」：%q", tt.Texts())
	}

	st.Rate = 1.75
	tt.Frame()
	if !tt.HasText("1.75x") {
		t.Fatalf("倍率没跟上：%q", tt.Texts())
	}

	// 换歌那一帧：歌名、时长全变了，倍率不变。
	st.Title, st.Duration = "换了一首", 3*60*1000*1000*1000
	tt.Frame()
	if !tt.HasText("1.75x") {
		t.Errorf("换歌之后倍率按钮回到别的值了：%q", tt.Texts())
	}

	st.Rate = 1
	tt.Frame()
	if !tt.HasText("1x") {
		t.Errorf("切回原速之后按钮上不是「1x」：%q", tt.Texts())
	}
}

// TestRateMenuListsEveryStep 盯点开之后七档都在。
//
// 少一档就是「用户想用 1.5 倍，翻遍了浮层没有」；多一档就是界面上出现了算法
// 不认的值，按下去被吸到别的档位上，而按钮写的又是吸过去的那个——用户完全
// 不知道自己刚才按的到底是什么。
func TestRateMenuListsEveryStep(t *testing.T) {
	tt, _, _ := rateBarTester(t, 760)

	// 没点开时，浮层里的档位不该出现在画面上（按钮上的那一档当然在）。
	before := map[string]bool{}
	for _, r := range app.RateSteps {
		before[rateLabel(r)] = tt.HasText(rateLabel(r))
	}
	if before[rateLabel(1.5)] || before[rateLabel(2)] {
		t.Errorf("没点开就能在画面上看到浮层里的档位：%q", tt.Texts())
	}

	if err := tt.Click("播放速率"); err != nil {
		t.Fatalf("点不开倍率按钮：%v（画面：%q）", err, tt.Texts())
	}
	for _, r := range app.RateSteps {
		if !tt.HasText(rateLabel(r)) {
			t.Errorf("浮层里没有 %q：%q", rateLabel(r), tt.Texts())
		}
	}
}

// TestClickingAStepSetsTheRate 盯按档位之后状态真的改了、字也真的变了。
func TestClickingAStepSetsTheRate(t *testing.T) {
	tt, a, s := rateBarTester(t, 760)

	for _, r := range []float64{1.5, 0.5, 2, 1} {
		if err := tt.Click("播放速率"); err != nil {
			t.Fatalf("点不开倍率按钮：%v", err)
		}
		if err := tt.Click(rateLabel(r)); err != nil {
			t.Fatalf("点不了 %q：%v（画面：%q）", rateLabel(r), err, tt.Texts())
		}
		// 按钮上的字是 State 里的值，而 State 由 App 现读——所以这一句
		// 断的是「界面点了 → App 改了 → 界面又读到了」。
		if got := a.Snapshot().Rate; got != r {
			t.Fatalf("按了 %q，App 里的倍率是 %v", rateLabel(r), got)
		}
		tt.Frame()
		if want := rateLabel(r); !tt.HasText(want) {
			t.Errorf("按了 %q，按钮上却是 %q：%q", rateLabel(r), want, tt.Texts())
		}
		// 选完即收：Biu 那边也是这样，见 viewRateMenu 的注释。
		if s.rateOpen {
			t.Errorf("按完 %q 浮层还开着", rateLabel(r))
		}
	}
}

// TestTheRateButtonDoesNotSquashTheTitle 盯多出来的 64px 没有把歌名挤没。
//
// 播放条是固定 76 高的一行，右边多一格就往左挤。挤掉的正是歌名——而歌名挤没了
// 之后**界面看不出坏了**：字还在那儿，只是一个字一行、或者被裁掉半个。
func TestTheRateButtonDoesNotSquashTheTitle(t *testing.T) {
	for _, width := range []int{760, 900, 1000, 1200, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			// 这里要的是一根很长的歌名，喂 State 比真开一首文件省事得多
			// （真开的话标题是文件名，短得挤不出问题）。
			st := app.State{HasTrack: true, Title: "一首歌的名字很长很长很长很长", Rate: 1}
			b := newTestShell(t, at(width, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
				return func(c *mygoui.Context) {
					s.wake.set(c)
					s.viewPlayBar(c, st)

				}
			}))
			tt := b.tt
			tt.Frame()

			title, ok := tt.Find("一首歌的名字很长很长很长很长")
			if !ok {
				t.Fatalf("宽度 %d：歌名不在画面上：%q", width, tt.Texts())
			}
			if title.W < 40 || title.W < title.H {
				t.Errorf("宽度 %d：歌名只剩 %.1f×%.1f，已经被挤成竖条了",
					width, title.W, title.H)
			}
			if title.H > playbarHeight || title.Y+title.H > playbarHeight {
				t.Errorf("宽度 %d：歌名（y=%.1f 高 %.1f）被 %d 高的播放条裁了",
					width, title.Y, title.H, playbarHeight)
			}
			if !tt.HasText("1x") {
				t.Errorf("宽度 %d：倍率按钮不见了：%q", width, tt.Texts())
			}
		})
	}
}

// TestRateMenuLabelsAreNotTruncated 盯浮层里那七行**看全了**。
//
// 这条是被一张截图逮到的：浮层里的按钮宽 56 时，「0.75x」和「1.75x」被截成
// 「0.…」「1.…」——而界面上所有已有的断言**都是绿的**。因为无头 Tester 读的是
// 控件的**文本**（`HasText("0.75x")` 照样成立），被截的只是画出来的那几个字。
// 换句话说：界面上唯一能看见倍速的地方少了一大半信息，却没有任何一条测试会红。
//
// 断法是量**画出来**的宽度：每一行的字占多宽，和同一串字在一根不设限的行里的
// 自然宽度比。少了就是被截了。
//
// ⚠️ 喂的倍率是 1.1（不在七档里），好让锚点上那个按钮写着「1.1x」，和浮层里的
// 七行**一个都不撞名**。撞了名的话 `Find` 拿到的是先画的那个（锚点），量到的
// 是锚点的宽度——永远是 64，永远不红。
func TestRateMenuLabelsAreNotTruncated(t *testing.T) {
	labels := make([]string, 0, len(app.RateSteps))
	for _, r := range app.RateSteps {
		labels = append(labels, rateLabel(r))
	}
	// ⚠️ **自然宽度要用浮层里那个字号量**（上游 `size="sm"` 的 `text-tiny` = 12，
	// 票 #220）：浮层里那七行的字是 12，而 `textWidths` 默认量的是主题的默认字号
	// （13）——拿 13 的自然宽度去比 12 的画面，**每一个都会「被截了」**，而屏幕上
	// 一个字都没少。
	//
	// ⚠️ **写的是字面的 12，不是 `rateMenuFont`**：照抄被测对象的话，改字号的人
	// 顺手把这一行也改了，这条就永远不会红（票 #217 的评审查过一条同款的）。
	natural := textWidths(t, labels, 12)

	b := newTestShell(t, at(760, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewRate(c, app.State{HasTrack: true, Title: "一首歌", Rate: 1.1})

		}
	}))
	b.s.rateOpen = true
	tt := b.tt
	tt.Frame()

	for _, label := range labels {
		rect, ok := tt.Find(label)
		if !ok {
			t.Fatalf("浮层里没有 %q：%q", label, tt.Texts())
		}
		if rect.W+0.5 < natural[label] {
			t.Errorf("%q 只画出了 %.1f 宽，它本来要 %.1f——被截了"+
				"（用户在这一列里挑倍速，缺字就只能靠猜）", label, rect.W, natural[label])
		}
	}
}

// textWidths 量几串字各占多宽，也就是它们的**自然**宽度：在一根够宽、不会
// 截断的行里各画一次，量画出来的那一个。
//
// ⚠️ **字号必须和被量的那几行一样**：自然宽度是「这几个字在那个字号下有多宽」，
// 拿另一个字号的宽度去比，短的会被说成「被截了」、长的会被说成「还有富余」。
func textWidths(t *testing.T, ss []string, font float32) map[string]float32 {
	t.Helper()
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Width(1200).Children(func() {
			for _, s := range ss {
				mygoui.Text(c, s).FontSize(font)
			}
		})
	}, 1200, 200)
	tt.Frame()

	out := make(map[string]float32, len(ss))
	for _, s := range ss {
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("连不设限的一行都量不出 %q 的宽度", s)
		}
		out[s] = r.W
	}
	return out
}

// TestRateLabelMatchesBiu 盯按钮上的字和 Biu 那边一模一样。
//
// Biu 那边是 JS 的 `${rate}x`（`biu/src/components/music-rate/index.tsx:50`），
// 也就是 `1` 而不是 `1.00`、`2` 而不是 `2.00`。两边写的字不一样，用户来回切
// 应用会觉得「这个怎么少了两位」——而它确实少了两位，少错了。
func TestRateLabelMatchesBiu(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want string
	}{
		{0.5, "0.5x"}, {0.75, "0.75x"}, {1, "1x"},
		{1.25, "1.25x"}, {1.5, "1.5x"}, {1.75, "1.75x"}, {2, "2x"},
	} {
		if got := rateLabel(c.in); got != c.want {
			t.Errorf("rateLabel(%v) = %q，要的是 %q", c.in, got, c.want)
		}
	}
}
