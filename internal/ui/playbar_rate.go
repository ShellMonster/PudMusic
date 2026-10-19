// 播放条上的倍率控件。
//
// 照 Biu 的 `src/components/music-rate`：一个写着「1x」的按钮，点开是一列
// 0.5~2.0 的档位，当前那一档上主色。档位表是 `app.RateSteps`，和 `App.SetRate`
// 认的是同一份（见 app.go 里那一行）。
//
// ⚠️ 为什么是**一列按钮**而不是一根滑块：滑块能停在 0.63 倍，而倍率是一个有档
// 的东西——用户下一次要回到「刚才那个」的时候，想得起的是「1.25 倍」不是
// 「往右挪一格」。Biu 那边也是这七行。
package ui

import (
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// rateBtnW 是倍率按钮的宽度，浮层里那七行也用同一个数。
//
// 64 是量出来的：「0.75x」五个字符，按钮自己还要留内边距（比 56 宽的那一头是
// 「加歌」，两个并排时窄的那个会先被压出省略号）。
//
// ⚠️ 浮层里那七行**不能比这个窄**。窄一点点，「0.75x」就变成「0.…」、
// 「1.25x」变成「1.…」——而那一列是用户挑倍速的全部依据，看不全就只能靠猜。
// 实测 56 就是这么被截的，而且弹出浮层之前没人看得出来：按钮上只有一档，
// 而无头 Tester 读的是控件的文本，被截的只是画出来的那几个字（见
// TestRateMenuLabelsAreNotTruncated）。
const rateBtnW = 64

// viewRate 是播放条右边的倍率按钮加上它弹出来的那一列档位。
//
// ⚠️ 读数是**每帧现读 `State.Rate`** 的，不是在按下那一刻记下来的。倍率是用户
// 的一个设置：换歌之后还在，而新歌的播放器是后开的（`app.App.SetRate` 把值记
// 在 App 上，新歌开好时套上去）。缓存住的话，切完歌按钮上会回到 1x，而声音
// 其实是 1.5 倍——界面上唯一能看见倍速的地方说错了话。
func (s *Shell) viewRate(c *ui.Context, st app.State) {
	// 扁平按钮：上游这一枚是无边框的 `IconButton`
	//（`components/music-rate/index.tsx:49`，`min-w-fit text-[16px]`），
	// 调研把它算进 F1 的「倍速键」（报告 §1.1 的 C3）。
	btn := flatButton(c, rateLabel(st.Rate)).Width(rateBtnW).Label("播放速率")
	if btn.Clicked() {
		s.rateOpen = !s.rateOpen
	}
	// Popover 自己处理「点外面关掉」和 Escape，所以这里只需要管开关本身。
	//
	// ⚠️ 浮层那一列外面包一层 Guard（铁律三）：它是这一票新增的 view，而浮层是
	// 在**播放条这一层的布局之外**单独建的，它一崩坏掉的是整块浮层（用户看到
	// 的是「点开倍率什么都没出来」），而报错的地方在播放条上——两者对不上，
	// 极难查。
	ui.Popover(c, btn, &s.rateOpen, func() {
		s.guardAt("倍速菜单", s.viewRateMenu)(c)
	})
}

// viewRateMenu 是浮层里那一列档位。
//
// 自己现读一次 Snapshot 而不是接着传 st：外面那一帧的 st 是**主帧**读的那一份，
// 而浮层什么时候开、关掉之前用户又按了什么，它自己不知道。宁可多读一次。
func (s *Shell) viewRateMenu(c *ui.Context) {
	st := s.app.Snapshot()

	ui.Column(c).Key("playbar:rate").Padding(4).Gap(2).Children(func() {
		for _, r := range app.RateSteps {
			b := flatButton(c, rateLabel(r)).Width(rateBtnW).Radius(4).
				Label("播放速率 " + rateLabel(r))
			// 选中那一档是主色的淡底 + 主色的字（`flatCurrent`），不是实心主色
			// ——和侧栏菜单那一处是同一条理由（报告 §1.1 的 F1）。
			if r == st.Rate {
				b = flatCurrent(c, b)
			}
			if b.Clicked() {
				// 选完就收起来：Biu 那边也是选完即关（`setRate` 之后浮层消失）。
				// 留着不关的话，它会一直压在进度条上，用户以为播放条坏了。
				s.rateOpen = false
				s.app.SetRate(r)
			}
		}
	})
}

// rateLabel 是按钮上写的那几个字：`1x` / `1.25x` / `0.5x`。
//
// 用 'g' 最短表示而不是写死小数位：Biu 那边是 JS 的 `${rate}x`，也就是
// `1` 而不是 `1.00`、`2` 而不是 `2.00`。两边按钮上的字要是一模一样，用户来回
// 切应用才不会觉得「这个怎么少了两位」。
func rateLabel(r float64) string {
	return strconv.FormatFloat(r, 'g', -1, 64) + "x"
}
