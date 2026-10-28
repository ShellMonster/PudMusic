// 播放条上的音量：一枚图标 + 悬停浮出来的那根竖滑块。
//
// 照上游 `src/components/music-volume/index.tsx`：
//
//   - 图标**是能按的**：按一下静音、再按一下取消静音（上游 `onToggleMute`）。
//   - 指针停在它上面时浮出一根**竖**滑块，底下是百分比（上游那个 Tooltip 里就是
//     `orientation="vertical"` 的 Slider 加一个 `{Math.round(volume * 100)}%`）。
//   - 在它上面滚滚轮，音量 ±0.05（上游 `onWheel` 的 `delta`），而且滚轮会把浮层
//     唤出来（上游 `setIsTooltipOpen(true)`）。
//
// ## 为什么把常驻的那根横滑块收掉了
//
// 原来播放条右边常驻一根 **120 像素**的横滑块（`viewPlayBarRight`）。它抢的是
// 进度条该有的空间：那一档右边那一组是 362 宽，而窗口下限是 900。收成图标之后
// 右边那组窄了 141（362 → 221），中间那一列（进度条）拿得回来的宽度更多。
//
// ## 「静音」是**它自己的一档**，不是「音量 0」
//
// 播放器那边只有增益（`audio.Player.SetVolume`），应用层多了一格静音
// （`App.ToggleMute`）：静音的时候**音量那一格一动不动**——取消静音放回去的
// 就是它，落盘的也是它。
//
// ⚠️ **合成一档（静音 = 音量 0）会踩一个落盘的坑**（票 #311）：音量一旦开始
// 落盘，「静音后退出再启动，音量变成 0」就成了必然（Clementine 的老毛病）。
//
// 画面上分两处：那一枚**图标**跟着静音那一格走（静音的时候画的是「取消静音」
// 那张，和上游 `aria-label={isMuted ? "取消静音" : "静音"}` 一个做法）；而**滑块
// 照旧显示用户设的那一档**——静音的时候它停在 20%，取消静音放回去的就是 20%。
package ui

import (
	"fmt"
	"math"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// volWheelStep 是滚轮转一格音量走多少。
	//
	// 0.05 照上游（`music-volume/index.tsx` 的
	// `const delta = event.deltaY > 0 ? -0.05 : 0.05`），和快捷键那一步
	// （`shortcutVolumeStep`）是同一个数——两条路走一样的步子，用户不用记两个。
	volWheelStep = 0.05

	// volSliderW / volSliderH 是浮层里那根竖滑块的宽和高。
	//
	// 上游是 `h-40 w-[32px]`（160 高、32 宽，`music-volume` 的
	// `trackWrapper`）——票 #220 / 报告 §3.9 把它对齐了。
	//
	// ⚠️ **160 高在 560 高的最小窗口里装得下**：浮层是**往上**弹的，160 高加上
	// 播放条那 76 是 236，而窗口最少 560（`WindowOptions.MinHeight`）——
	// 剩 324 给内容区，绰绰有余。改之前那句「160 高会顶进内容区」的理由是按
	// 「浮层往下弹」算的（它不往下弹）。
	volSliderW = 32
	volSliderH = 160

	// volCloseGrace 是「指针离开了图标和浮层」之后还留着浮层多久。
	//
	// ⚠️ **这一段宽限是必需的**：浮层浮在图标**上方**（底下是播放条，没有位置），
	// 而框架的浮层和锚点之间留着一道 4 像素的边距——指针从图标移到浮层上，
	// 中间有那一段谁都不在。没有宽限的话浮层会在指针走过那一段的时候收掉，
	// 用户于是永远够不着那根滑块。
	volCloseGrace = 400 * time.Millisecond

	// volHoverDelay 是「指针停在音量图标上」到浮层出来之间的那一下延迟。
	//
	// 300 毫秒照上游（`music-volume` 那个 Tooltip 的 `delay={300}`）；这个数和
	// `tooltip.go` 的 `tipDelayVolume` 是同一个——浮层虽然是自己画的，但它在
	// 上游就是那个 Tooltip，时延不能和别处的提示分家。
	//
	// ⚠️ **没有这一下的坏处**：指针**扫过**那一枚图标的时候浮层就弹出来，
	// 而它弹在内容区里（播放条的上方）——用户从左边划到倍速键，中间扫过音量
	// 那一枚，于是内容区里闪一下一个竖滑块。
	volHoverDelay = tipDelayVolume

	// volPanelW 是音量浮层里那一列的宽度。
	//
	// ⚠️ **定宽是必须的**：列宽若由最宽的那一行（百分比）撑着，而滑块是**居中**的，
	// 数字从「0%」变成「100%」时列宽变了，**滑块就在指针底下横向挪**（评审指出来的）。
	//
	// 40 = 滑块那 32 + 左右各 4 的内边距——**滑块要放得下**（票 #220 把它从 20
	// 加宽到 32，而列的内容宽是「列宽 − 内边距」，列不改的话滑块会探出列外）。
	//
	// ⚠️ **这个 40 也是浮层里那个百分比那一格的宽**（见 `viewVolumePanel`）：
	// 「100%」在 12 号字下的排版宽是 **31.8**，格子窄一点就折行（变成「100」换行
	// 「%」），而 40 宽的格子有 **8.2 像素**余量。
	volPanelW = 40
)

// volSliderLook 是浮层里那根竖滑块的样子。
//
// 轨道 4 像素、旋钮 **16** 见方（上游 `size="sm"` 的那一档，票 #220 / 报告 §3.9
// 把 14 改成 16），旋钮是主色（浮层的底是 `t.Background`，白旋钮在
// 浅色主题下和底色一样）。
var volSliderLook = sliderLook{
	vertical:     true,
	trackW:       4,
	knobW:        16,
	knobH:        16,
	knobWhenIdle: true,
	face: func(t *ui.Theme) ui.Color {
		return t.Accent
	},
	// ⚠️ **这一根一直是主色，不拆两档**：上游音量那根是 `music-volume` 里另一个
	// 组件（`orientation="vertical"` 的 HeroUI Slider），填过的那一段一直是一个
	// 颜色；而它的旋钮**一直画着**（`knobWhenIdle: true`）——「旋钮出不出来」
	// 这一档在它身上根本不存在。拆两档是播放条那根的事（票 #218）。
	fill: func(t *ui.Theme) (ui.Color, ui.Color, ui.Color) {
		return t.Accent, t.Accent, t.Accent
	},
}

// volHoverKey 是「指针在音量图标上」那个动画的键。
//
// ⚠️ **它得是一个稳定的键**（不是每次新建的字符串）：框架的 `Animate` 靠它认出
// 「同一个动画的下一帧」，键变了就是另一个动画，于是每次都是「第一次」——
// 那个值直接是目标值（见上面那段注释）。
var volHoverKey = struct{ hover bool }{}

// volumeSlider 是音量浮层里那根竖滑块。
//
// ⚠️ **它必须是 `SliderBase`**：框架的 `ui.Slider` 摆不了竖的（`Vertical()` 只认
// `SliderBase`，`mygo/ui/base.go:145`），而上游那根就是竖的
// （`music-volume` 的 `orientation="vertical"`）。
func volumeSlider(c *ui.Context, value *float64, lo, hi float64) ui.Element {
	return selfSlider(c, value, lo, hi, volSliderLook).
		Width(volSliderW).Height(volSliderH)
}

// viewVolumeButton 是播放条上那一枚音量图标，和它悬停时浮出来的那根竖滑块。
//
// 见文件头：图标能按（静音 / 取消静音）、悬停浮出竖滑块、滚轮 ±0.05。
func (s *Shell) viewVolumeButton(c *ui.Context, st app.State) {
	t := c.Theme()
	// 静音是它自己的一格（`st.Muted`），而「音量是 0」在听感上也是没声——
	// 那一枚键说的是「按下去会怎样」，所以两样都得算进去。
	muted := st.Muted || st.Volume <= 0

	// 画的是哪一档看音量本身（Biu 的 music-volume 三档同一个做法），
	// 静音是它自己的一格（`st.Muted`）。
	//
	// ⚠️ **图标只能从 `volumeIcon` 出去**：哪一档配哪一张由那个函数独家说了算
	// （`icons_test.go` 的 `TestTheStateIconsOnlyReachTheScreenThroughTheirFunction`
	// 盯着这一条）。
	name := volumeIcon(st.Volume, st.Muted)
	label := "静音"
	if muted {
		label = "取消静音"
	}

	// ⚠️ **这一枚刻意不加 `Tooltip`**：框架的提示浮在元素**上方**，而音量浮层
	// （竖滑块）也浮在上方——两者叠在一起，提示被浮层盖掉一半（实测：浮层底下
	// 露着一个黑边的小角，写着「点」）。
	//
	// 上游也没有：它那一枚的 `aria-label` 就是「静音 / 取消静音」，而悬停出来
	// 的那个 Tooltip **是竖滑块本身**。所以「按一下会静音」这件事靠的是
	// **图标本身**（静音了它换成静音那张）加上这一枚的无障碍名字。
	btn := iconKey(c, name, label, iconMd, false).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7).
		TextColor(t.TextMuted)

	// 悬停就浮出那根竖滑块——**但要停一会儿**（上游那个 Tooltip 的 `delay={300}`）。
	//
	// ⚠️ **这一下延迟是用框架自己的动画做的**，不是另起一套状态机：
	// `Animate` 的文档写着「keeps frames coming while it moves」，所以指针停在
	// 图标上的时候，那 300 毫秒是**框架自己一帧一帧要走完**的——不用谁去叫醒界面。
	//
	// ⚠️ **动画的键必须每一帧都问**（哪怕这一帧不在图标上）：框架的 `Animate` 在
	// **第一次**请求某个键时直接返回目标值（`mygo/ui/motion.go:316` 的
	// `if made { return target }`），而不问的那一帧就是「第一次」——于是第一次悬停
	// 立刻就是 1，延迟等于没有。
	//
	// ⚠️ **滚轮不等这 300 毫秒**：用户是在调音量，他得立刻看见调到了多少
	// （上游的 `onWheel` 也是立刻 `setIsTooltipOpen(true)`）。
	hoverTarget := float32(0)
	if btn.Hovered() {
		hoverTarget = 1
	}
	if btn.Animate(volHoverKey, hoverTarget, volHoverDelay) >= 1 {
		s.volOpen = true
	}

	// 浮层**自己**收掉：指针既不在图标上、也不在浮层里，隔过一段宽限之后。
	//
	// 🔴 **这一段必须排一个「到点叫醒界面」的钟**（评审指出来的）：原来它只读
	// `s.clock.Now()`，于是「指针离开了」这件事**只在那一帧**被算一次——而
	// **不播放的时候没有任何东西在要帧**，浮层于是永远留在画面上，遮住内容区
	//（症状：把鼠标移开，浮层还在那儿）。
	//
	// ⚠️ **浮层那一条的悬停是上一帧的**：浮层在**覆盖层**里建，而覆盖层是主树
	// **之后**才建的——这一帧里它还没建出来，问不到它的悬停。
	s.syncVolIdle(btn.Hovered(), s.volPanelHovered)

	// 滚轮：±0.05（上游 `onWheel`）。
	//
	// ⚠️ **它吃掉了这个滚轮事件**（返回 true）：不吃掉的话，滚轮会继续往下传给
	// 页面上的滚动容器——在播放条上滚滚轮居然把整页滚了，那是另一个 bug。
	btn.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll {
			return false
		}
		delta := volWheelStep
		if ev.DY > 0 {
			delta = -volWheelStep
		}
		s.setVolume(s.app.Snapshot().Volume + delta)
		// 滚轮也把浮层唤出来（上游 `setIsTooltipOpen(true)`）——用户是在调音量，
		// 他得看见调到了多少。
		s.volOpen = true
		return true
	})

	if btn.Clicked() {
		s.toggleMute()
	}

	// 浮层本体。
	ui.Popover(c, btn, &s.volOpen, func() {
		s.guardAt("音量浮层", func(c *ui.Context) { s.viewVolumePanel(c, st) })(c)
	})
}

// viewVolumePanel 是音量浮层里的东西：一根竖滑块，底下是百分比。
//
// ⚠️ **百分比读的是滑块当前那个值**，不是浮层打开那一刻的：拖滑块的时候它得跟着
// 变（上游那个 `endContent` 就是这么写的）。滑块的值绑定写的就是这个 `vol`
// （框架的 `sliderInput` 在输入那一趟就把它改了），所以这里读它就是当下的值。
func (s *Shell) viewVolumePanel(c *ui.Context, st app.State) {
	t := c.Theme()
	vol := st.Volume

	// 🔴 **这一列定宽**（`volPanelW`）：不定宽的话，列宽由**最宽的那一行**撑着
	// （百分比那行），而滑块是**居中**的——于是数字从「0%」变成「100%」时列宽变了
	// 4.7 像素，**滑块就在指针底下横向挪**（评审指出来的）。
	//
	// 定宽之后列宽恒定，居中的滑块也就恒定 ✓。上游那边也是这么做的：那个百分比
	// 是个 `w-8 text-center`（32 像素的定宽格子，字在格子里居中，
	// `music-volume` 的 `endContent`）。
	col := ui.Column(c).Key("playbar:volume").Padding(space.Px4).Gap(space.Px2).AlignItems(ui.Center).
		Width(volPanelW)
	col.Children(func() {
		sl := volumeSlider(c, &vol, 0, 1).Label("音量")
		if sl.Changed() {
			s.setVolume(vol)
		}
		// 百分比也定宽：不然字数一变位置就变。
		//
		// 🔴 **字在这一格里必须居中**（票 #485）：之前字是**靠左**的，而这一格
		// （`Width(volPanelW)` = 40）比列的内容区（32）还宽 8，在内容区里居中时
		// 左右各探出 4——数字的墨迹中心于是比滑块中心**偏左 7.3 像素**（实测）。
		// 现在字居中，于是墨迹中心 = 这一格的中心 = 滑块那一格的中心 ✓。
		//
		// ⚠️ **这一格要比滑块宽（40 对 32），是刻意的**：上游那边是 `w-8`（32）
		// 的定宽格子 + `text-center`（`music-volume/index.tsx`），而「100%」在
		// 12 号字下的排版宽是 **31.8**——32 宽的格子只剩 **0.2 像素**余量，系统
		// 字号放大到 **1.05 倍**它就折成两行（「100」换行「%」），而改动前要到
		// 1.40 倍才折（票 #485 的评审抓出来的）。40 宽的格子有 8.2 像素余量。
		//
		// ⚠️ **而且不许折行**（`NoWrap`）：Windows 读注册表 TextScaleFactor、
		// Linux 读 GNOME 的 text-scaling-factor，放大倍数没有上限——1.75 倍时
		// 「100%」要 56 宽，格子放不下，于是让它**溢出**（浮层面板的 padding 还
		// 有 8 像素）而不是折成两行：折行会让浮层变高、把那个百分比挤到离滑块
		// 更远的地方去。
		ui.Text(c, volPercent(vol)).FontSize(scale.Font(c, scale.VolPercentFont)).
			TextColor(t.TextMuted).Width(volPanelW).TextAlign(ui.Center).NoWrap()
	})

	// 记下「指针在浮层里」，给下一帧的收放判断用（见 `syncVolIdle`）。
	//
	// ⚠️ **判的是这一列而不是浮层本身**：框架的 `ui.Popover` 不把浮层那一块交给
	// 调用方（`PopoverBase` 才给），而这一列是**浮层里所有东西的爹**——指针在
	// 滑块上、在百分比上，它都在这一列的命中链里。
	s.volPanelHovered = col.Hovered()
}

// syncVolIdle 收放那个浮层：指针在图标上、或者在浮层里，就留着；两边都不在，
// 隔过一段宽限之后收掉。
//
// 🔴 **它排了一个「到点叫醒界面」的钟**（`armVolIdle`）：没有它，「指针离开了」
// 这件事**只在那一帧**被算一次——而**不播放的时候没有任何东西在要帧**，浮层于是
// 永远留在画面上，遮住内容区（症状：把鼠标移开，浮层还在那儿）。
//
// ⚠️ **收掉的判断放在这里（主树里）而不是浮层里**：浮层在**覆盖层**里建，而覆盖层
// 是主树**之后**才建的——在浮层里把 `volOpen` 设成 false，这一帧它照样画着，
// 要**下一帧**才消失。
//
// ⚠️ **浮层那一条的悬停是上一帧的**（`onPanel` 是上一帧记下的）：浮层还没建出来
// 的那一帧问不到它的悬停。
func (s *Shell) syncVolIdle(onIcon, onPanel bool) {
	switch {
	case onIcon || onPanel:
		// 指针还在上面：清掉「离开的时刻」，那个钟也收掉。
		s.volIdleSince = time.Time{}
		s.disarmVolIdle()
	case s.volOpen:
		// 指针不在了：记下这一刻，并排一个到点叫醒界面的钟。
		if s.volIdleSince.IsZero() {
			s.volIdleSince = s.clock.Now()
			s.armVolIdle()
		} else if s.clock.Now().Sub(s.volIdleSince) >= volCloseGrace {
			s.volOpen = false
			s.volIdleSince = time.Time{}
			s.disarmVolIdle()
		}
	default:
		// 浮层已经没了（点外面关掉的）：那个钟也收掉，别一直醒着。
		s.disarmVolIdle()
	}
}

// armVolIdle 排一个「到点叫醒界面」的钟。
//
// ⚠️ **回调里只调 `wake.invalidate`**：它跑在后台 goroutine（真时钟）或测试
// goroutine（假时钟）上，而界面状态是主线程单点写入的（铁律一）——到没到点由
// `syncVolIdle` 现算。
//
// 周期就是那一段宽限：第一次醒的时候刚好到期，那一帧就把浮层收掉。
func (s *Shell) armVolIdle() {
	if s.volIdleStop != nil {
		return
	}
	s.volIdleStop = s.clock.EveryFunc(volCloseGrace, s.wake.invalidate)
}

// disarmVolIdle 停掉那个钟。
func (s *Shell) disarmVolIdle() {
	if s.volIdleStop != nil {
		s.volIdleStop()
		s.volIdleStop = nil
	}
}

// setVolume 设音量。
//
// ⚠️ **「静音之前是多少」不用这一层记**：那是应用层的事（`App.SetVolume` 记的是
// 用户设的那一档，静音那一档是它自己的一格）。这一层以前有个 `volBeforeMute`
// ——它跟着窗口走，进程一退就没了，而音量是要落盘的（票 #311）。
func (s *Shell) setVolume(v float64) {
	s.app.SetVolume(v)
}

// toggleMute 静音 / 取消静音。
//
// 静音 = 按下静音键（音量那一格不动）；取消静音 = 放回用户设的那一档。
func (s *Shell) toggleMute() {
	s.app.ToggleMute()
}

// volPercent 是浮层底下那个百分比。
//
// 上游是 `{Math.round(volume * 100)}%`（`music-volume/index.tsx` 的
// `endContent`），所以四舍五入而不是截断——0.5 显示成「50%」而不是「49%」。
func volPercent(v float64) string {
	return fmt.Sprintf("%d%%", int(math.Round(v*100)))
}
