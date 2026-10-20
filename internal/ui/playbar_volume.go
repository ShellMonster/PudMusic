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
// ## 「静音」是**音量 0**，不是播放器的一个开关
//
// 播放器那边压根没有静音这一档（`audio.Player.SetVolume` 的 0 就是静音），
// 应用层也只有音量（`App.SetVolume`）。所以「静音」在这里是**把音量设成 0**
// 加一个「记着静音之前是多少」的头（`volBeforeMute`）。
//
// 这么做有一个好处：静音之后**别处看到的也是 0**——迷你播放器上那根音量滑块
// 立刻变成 0，锁屏上的媒体会话也是 0。而如果静音只是界面上的一个标记，
// 那些地方会继续显示原来的音量。
package ui

import (
	"fmt"
	"math"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
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
	// `trackWrapper`）。我们矮一点：浮层是从播放条往上弹的，160 高在 560 高的
	// 最小窗口里会顶进内容区。
	volSliderW = 20
	volSliderH = 120

	// volCloseGrace 是「指针离开了图标和浮层」之后还留着浮层多久。
	//
	// ⚠️ **这一段宽限是必需的**：浮层浮在图标**上方**（底下是播放条，没有位置），
	// 而框架的浮层和锚点之间留着一道 4 像素的边距——指针从图标移到浮层上，
	// 中间有那一段谁都不在。没有宽限的话浮层会在指针走过那一段的时候收掉，
	// 用户于是永远够不着那根滑块。
	volCloseGrace = 400 * time.Millisecond

	// volHoverDelay 是「指针停在音量图标上」到浮层出来之间的那一下延迟。
	//
	// 300 毫秒照上游（`music-volume` 那个 Tooltip 的 `delay={300}`）。
	//
	// ⚠️ **没有这一下的坏处**：指针**扫过**那一枚图标的时候浮层就弹出来，
	// 而它弹在内容区里（播放条的上方）——用户从左边划到倍速键，中间扫过音量
	// 那一枚，于是内容区里闪一下一个竖滑块。
	volHoverDelay = 300 * time.Millisecond

	// volDefaultUnmute 是「压根没静音过（音量本来就是 0）」时取消静音放回去的值。
	//
	// 0.5 是一半——用户按了「取消静音」，听到的应该是一半声音，不是 0。
	volDefaultUnmute = 0.5
)

// volSliderLook 是浮层里那根竖滑块的样子。
//
// 轨道 4 像素、旋钮 14 见方，旋钮是主色（浮层的底是 `t.Background`，白旋钮在
// 浅色主题下和底色一样）。
var volSliderLook = sliderLook{
	vertical:     true,
	trackW:       4,
	knobW:        14,
	knobH:        14,
	knobWhenIdle: true,
	face: func(t *ui.Theme) ui.Color {
		return t.Accent
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
	muted := st.Volume <= 0

	// 画的是哪一档看音量本身（Biu 的 music-volume 三档同一个做法）。
	//
	// ⚠️ **图标只能从 `volumeIcon` 出去**：哪一档配哪一张由那个函数独家说了算
	// （`icons_test.go` 的 `TestTheStateIconsOnlyReachTheScreenThroughTheirFunction`
	// 盯着这一条），而它对 0 已经返回静音那张了。
	name := volumeIcon(st.Volume)
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
	btn := iconButton(c, name, label, iconMd).
		Size(iconHitMd, iconHitMd).Padding(0, 5).
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
	s.volIconHovered = hoverTarget > 0
	if btn.Animate(volHoverKey, hoverTarget, volHoverDelay) >= 1 {
		s.volOpen = true
	}

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

	col := ui.Column(c).Key("playbar:volume").Padding(4).Gap(2).AlignItems(ui.Center)
	col.Children(func() {
		sl := volumeSlider(c, &vol, 0, 1).Label("音量")
		if sl.Changed() {
			s.setVolume(vol)
		}
		ui.Text(c, volPercent(vol)).FontSize(11).TextColor(t.TextMuted)
	})

	// 收不收浮层：指针在图标上、或者在浮层里，就留着；两边都不在，隔过一段
	// 宽限之后收掉。
	//
	// ⚠️ **判的是这一列而不是浮层本身**：框架的 `ui.Popover` 不把浮层那一块交给
	// 调用方（`PopoverBase` 才给），而这一列是**浮层里所有东西的爹**——指针在
	// 滑块上、在百分比上，它都在这一列的命中链里。
	onPanel := col.Hovered()
	switch {
	case s.volIconHovered || onPanel:
		s.volIdleSince = time.Time{}
	default:
		if s.volIdleSince.IsZero() {
			s.volIdleSince = s.clock.Now()
		} else if s.clock.Now().Sub(s.volIdleSince) > volCloseGrace {
			s.volOpen = false
			s.volIdleSince = time.Time{}
		}
	}
}

// setVolume 设音量，并且记住「不是静音的那个音量」。
//
// ⚠️ **记的是「最后一个不是 0 的音量」**：取消静音要放回去的就是它。音量是 0
// 的时候不记——否则「静音之前是多少」会被静音之后的 0 顶掉。
func (s *Shell) setVolume(v float64) {
	v = max(0, min(1, v))
	if v > 0 {
		s.volBeforeMute = v
	}
	s.app.SetVolume(v)
}

// toggleMute 静音 / 取消静音。
//
// 静音 = 音量 0（见文件头）。取消静音 = 放回静音之前那个音量。
func (s *Shell) toggleMute() {
	cur := s.app.Snapshot().Volume
	if cur > 0 {
		s.volBeforeMute = cur
		s.setVolume(0)
		return
	}
	back := s.volBeforeMute
	if back <= 0 {
		back = volDefaultUnmute
	}
	s.setVolume(back)
}

// volPercent 是浮层底下那个百分比。
//
// 上游是 `{Math.round(volume * 100)}%`（`music-volume/index.tsx` 的
// `endContent`），所以四舍五入而不是截断——0.5 显示成「50%」而不是「49%」。
func volPercent(v float64) string {
	return fmt.Sprintf("%d%%", int(math.Round(v*100)))
}
