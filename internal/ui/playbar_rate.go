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
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// rateMenuW 是**浮层本身**的宽：**60**。
	//
	// 上游 `src/components/music-rate/index.tsx:26-46`：那一列是
	// `py-3 px-2 w-[60px] min-w-[60px]`（票 #220 / 报告 §3.10）。
	rateMenuW = 60

	// rateBtnW 是**浮层里那七行**的宽度：**44**。
	//
	// 44 = 60 − 左右各 8 的内边距（上游那一列的 `px-2`）——浮层定宽 60，内容
	// 就是 44，而那七行按钮各占满内容宽。
	//
	// ⚠️ **浮层里那七行不能比这个窄**。窄一点点，「0.75x」就变成「0.…」、
	// 「1.25x」变成「1.…」——而那一列是用户挑倍速的全部依据，看不全就只能靠猜。
	// 实测 56 就是这么被截的，而且弹出浮层之前没人看得出来：按钮上只有一档，
	// 而无头 Tester 读的是控件的文本，被截的只是画出来的那几个字（见
	// TestRateMenuLabelsAreNotTruncated）。
	//
	// ⚠️ **44 装得下「0.75x」，但前提是那七行的按钮左右没有内边距**：上游那一列
	// 的按钮是 `isIconOnly`（HeroUI 给它的就是 `px-0`），字在 **12**
	//（`size="sm"` 的 `text-tiny`）下约 **30** 宽，44 绰绰有余。我们那枚扁平按钮
	// 左右各留 **14**（合 28），44 减去 28 只剩 16——**照原来的内边距改成 44，
	// 「0.75x」会被截成「0.…」**。
	//
	// ⚠️ **播放条上那一枚按钮不用这个数**（它贴字宽，见 `viewRate`）：那一枚摆的是
	// 当前这一档的字（「1x」两个字符），给它留 44 就是空着二十几像素。
	rateBtnW = 44

	// rateMenuFont 是浮层里那七行的字号：**12**。
	//
	// 上游那一列的按钮是 `size="sm"`，HeroUI 那个档的字是 `text-tiny`（12）。
	// 字大一号，44 宽就装不下「0.75x」了（见上面那段）。
	rateMenuFont = 12
)

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
	//
	// 🔴 **宽度贴着那几个字**，不写死：上游是 `min-w-fit`（就那么宽），
	// 而我们原来恒定 64——「1x」两个字摆在一个 64 宽的按钮里，两边空着二十几
	// 像素，而右边那一组（模式/下载/队列/音量/倍速/加歌）每一枚都紧挨着，
	// 只有它是鼓出来的。
	//
	// ⚠️ **`Shrink(0)` 是必需的**：框架的 flex 默认 `shrink = 1`
	//（`mygo/ui/context.go:88`），窗口一窄这一枚会被压扁，而它压扁的方式是
	// **把字截掉**——「1.25x」变成「1.…」。而这一枚上的字是**当前是多少倍**，
	// 看不全的那一半等于没说。
	//
	// 🔴 **字是 16**（票 #173 / G26）：上游那一枚是 `text-[16px]`
	//（`components/music-rate/index.tsx:49`，我们那句注释里本来就写着它）。
	// 我们原来**没写字号**，于是它跟着主题的默认值走——macOS 上 13、别的平台
	// 14，**同一枚按钮在两个平台上是两个字号**。写死 16 之后两边一样，也和上游
	// 一样。
	//
	// ⚠️ **浮层里那七行不跟着长到 16**：那一列的宽度是 44（票 #220 对齐上游之后
	// 的数，DESIGN.md 的 `倍速：浮层宽 60 / 那七行 44`），而那一列的字是 **12**
	//（上游 `size="sm"` 的 `text-tiny`）——「0.75x」在 12 下要 33 宽，44 装得下；
	// 而这一枚要是也用 16，「0.75x」就要 39 宽，加上按钮自己的内边距就装不下了。
	// 截掉的那一半恰恰是用户挑倍速的全部依据（见 `TestRateMenuLabelsAreNotTruncated`）。
	btn := flatButton(c, rateLabel(st.Rate)).FontSize(scale.Font(c, 16)).
		Shrink(0).Label("播放速率")
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
	t := c.Theme()

	// ⚠️ **浮层定宽 60、内边距上下 12 左右 8**（上游那一列的
	// `py-3 px-2 w-[60px] min-w-[60px]`，票 #220 / 报告 §3.10）：内容宽 44，
	// 那七行按钮各占满它。
	//
	// ⚠️ **那个 `Label` 是给无障碍和测试用的**：这一列是「选倍速」这个浮层，
	// 读屏软件念出来的时候该有一个名字；而测试要量它有多宽（60），Tester 找
	// 元素只认「字」和「名字」（`mygo/ui/headless.go` 的 `Find`）。
	ui.Column(c).Key("playbar:rate").Label("倍速菜单").
		Padding(space.Px12, space.Px8).Gap(space.Px2).Width(rateMenuW).Children(func() {
		for _, r := range app.RateSteps {
			// ⚠️ **左右内边距清零**：上游那一列的按钮是 `isIconOnly`（HeroUI 给
			// 它的就是 `px-0`），而我们那枚扁平按钮左右各留 14——44 减去 28 只剩
			// 16，「0.75x」会被截成「0.…」（见 `rateBtnW` 那段）。
			//
			// ⚠️ **字是 12**（上游 `size="sm"` 的 `text-tiny`）：字大一号，44 宽
			// 就装不下了。
			b := flatButton(c, rateLabel(r)).Width(rateBtnW).Radius(4).
				Padding(t.Space(1.5), space.None).FontSize(scale.Font(c, rateMenuFont)).
				Label("播放速率 " + rateLabel(r))
			// 选中那一档是**一块实心主色**（上游 `color="primary" variant="solid"`），
			// 字取 `t.AccentText`（主色底上该有的那一档黑白）。
			//
			// ⚠️ **和 `flatCurrent` 不是同一件事**：那一处是「中性灰底 + 主色的字」
			// （侧栏菜单项），而上游这一列的当前档是**实心的**——灰底绿字在浅色下
			// 读不出来（`flatCurrent` 那段写着为什么），实心的就没有那个问题。
			if r == st.Rate {
				b = flatSolid(c, b)
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
