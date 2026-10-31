// 评论抽屉：评论内容从**独立路由页**改成**右侧抽屉**（票 #706）。
//
// # 为什么从独立页改成抽屉
//
// 原来评论是 `/comments` 那一条独立路由（票 #573）。用户的要求是「评论从独立
// 页改成右侧抽屉」——理由很直接：**独立页把整个窗口顶掉**，用户看完评论要再
// 点一次「返回」才回得去播放条；而评论是「当前这一首的附属信息」，它是**叠在
// 底下那一页上面**的东西，不该把底下那一页挤走。
//
// # 骨架照队列抽屉（`queue_drawer.go`）
//
// 同样是：浮在主布局**外面**（`ui.Overlay` + `Absolute`）、同样从右边滑进来、
// 同样动的是**布局位置**（面板的右边距）而不是 transform、同样的「开的时候先
// 建面板再动，关的时候动完再拆」。
//
// ⚠️ **面板宽度比队列抽屉宽**（400 对 320）：这一份面板里装的是**会折行的
// 正文**——一段吐槽五六行，而队列那一列是「封面 + 歌名 + 来源」的定宽三段。
// 320 里一段正文折成五六行，每行二十来个字，读起来是「被剪碎的句子」；400 里
// 一行三十几个字，那才是一句话。⚠️ **而它仍然是窄的**：400 宽在 900 的最小
// 窗口上占 44%，底下那一页还剩得下一半（而评论那一页原来**整个窗口**都是它）。
//
// # 底下那一页点不动（模态）
//
// 开着的时候蒙版是 `Modal()`：焦点关进抽屉、底下那些快捷键让路、底下那一页
// 点不动。Biu 的透明蒙版也是这样，它只是看不见，不是空的。
//
// ⚠️ **关抽屉用蒙版的 `Clicked()`，不要用 `panel.PressedOutside()`**
// （理由见 `queue_drawer.go` 那一段）：后者问的是「指针按在了不是这个元素的
// 地方」，而播放条上按一下也算「按在外面」——于是按播放会顺手把抽屉关了。
//
// # 满屏播放器那一枚不带数字
//
// 播放条那一枚**继续显示评论数**（票 #682 的两级逻辑照旧）；满屏那一枚**不带**
// ——满屏底栏是**居中的 flex**，任何一枚变宽播放键就离开竖直中轴了（理由见
// `comments_entry.go`）。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/egoist/mygo/ui"
)

// commentsDrawerWidth 是评论面板有多宽：**400**。
//
// 🔴 **它比队列抽屉宽 80**（那个是 320）：这一份面板里装的是**会折行的正文**。
// 队列那一列是「封面 48 + 歌名 + 来源」的定宽三段，320 绰绰有余；而一段评论
// 正文在 320 的内容宽（320 − 头像 32 − 缝 12 ≈ 276）里折成五六行，每行二十
// 来个字——读起来是「被剪碎的句子」。400 里一行三十几个字，那才是一句话。
//
// ⚠️ **它仍然是窄的**：400 在 900 的最小窗口上占 44%，底下那一页还剩一半
// （而改之前评论是**整个窗口**都是它——底下那一页一格都没剩）。
const commentsDrawerWidth = 400

// commentsSlideKey 是这一段动画在面板那个元素上的键。
//
// ⚠️ **键必须是常量字符串**：框架把动画的值记在元素的 state 里，而键是 `any`，
// 非常量字符串每帧装箱一次就是每帧一个分配（理由同 `queueSlideKey`）。
//
// ⚠️ **它和队列那一段不是同一个键**：两个面板是两个元素，各自动各自的——
// 而同一个键会让两个面板抢同一条动画通道（框架的动画记在元素的 state 上，
// 而两处建的是**不同的**元素，所以其实不会撞——但键撞了会让读代码的人以为
// 那一段是共享的，而它们各自有各自的曲线和时长）。
const commentsSlideKey = "comments-drawer:slide"

// viewCommentsDrawer 是评论抽屉。没开就一个字都不建。
//
// 🔴 铁律二：**这一层不调 SetTheme**——它在 `viewMain` 的同一个 build pass 里建
// （`viewMain` 第一行就 `applyFrameTheme` 过），读到的 `c.Theme()` 已经是用户选
// 的那一档。跟 `viewQueueDrawer` 一样。
//
// ⚠️ **外面那层 `Guard` 是 `viewMain` 加的**（铁律三）：抽屉一崩，崩掉的是**整个
// 窗口**。
func (s *Shell) viewCommentsDrawer(c *ui.Context) {
	// 阶段先推，**建不建后说**：退场的那一段面板还在树上，所以「关掉」不是「不建」。
	justIn := s.syncCommentsDrawerPhase()
	if s.commentsPhase == drawerGone {
		return
	}
	t := c.Theme()
	ui.Overlay(c, func() {
		// ⚠️ **底边留出底下那一条的高度**（理由同 `queue_drawer.go`）：这一层
		// 是吃点击的，不留出播放条那一截，播放条就**整个被它盖住**——抽屉
		// 开着时按播放、上一首、下一首都按不动。
		//
		// 满屏开着的时候底下那一条是满屏的底栏（108 高），不是播放条——让出的
		// 高度要跟着它。
		bottomInset := float32(playbarHeight)
		if s.full.open {
			bottomInset = fullControlsHeight
		}
		back := ui.Row(c).Key("comments-backdrop").
			Absolute().Left(0).Top(0).Right(0).Bottom(bottomInset)
		if s.commentsPhase == drawerOut {
			// ⚠️ **退场那 200ms 里，这片蒙版不吃点击**（理由同 `queue_drawer.go`）：
			// 它是**透明**的，用户看不见它，而它盖住整页——看不见的东西吃点击，
			// 就是「点不动」。
			back = back.PassThrough()
		} else {
			// ⚠️ **开着的时候它是模态**：Modal 把焦点关进抽屉、外面那些快捷键
			// 让路，而底下那一页在它开着的时候点不动。
			back = back.Modal()
		}
		back.Children(func() {
			// ⚠️ **面板底色取 `cardBackground`**（理由同 `queue_drawer.go`）：设计
			// 规范把卡片、弹层的底统一到这一个出口；深色下「靠底色差分层」不够，
			// 靠的是**左边那道影 + 左缘那条线**。
			//
			// ⚠️ **面板是绝对定位的**：进出场要动的是**布局位置**，而 flex 那一套
			// 摆不出「从右边滑进来」——绝对定位按 **inset** 摆，那才是「从窗口
			// 右边挪进来」能用的那一套（理由同 `queue_drawer.go`）。
			panel := shadow.HeroMedium(ui.Column(c).Key("comments-drawer").
				Absolute().Top(0).Bottom(0).Width(commentsDrawerWidth).
				Background(s.cardBackground(t)).BorderStartWidth(1).BorderColor(t.Border), t)
			// ⚠️ **面板在退场时照旧接点击**：它还在屏幕上（滑出去那一段看得见），
			// 而「看不见的东西吃点击」才是坏的（那是底下那片蒙版）。
			//
			// ⚠️ **别用 `Disabled` 关它**：框架把禁用画成**半透明**，那会让面板
			// 一边滑一边淡——变成「淡出」而不是「滑出」了。
			panel.Role(ui.RoleDialog).Label("评论").
				Children(func() {
					s.viewCommentsDrawerBody(c, t)
				})

			// ---- 进出场 ----
			//
			// 动的是面板的**右边距**：0 = 贴住窗口右沿（到位了），
			// -commentsDrawerWidth = 整个面板在窗口外面（还没进来）。
			//
			// ⚠️ **动的是布局位置，不是 transform**（DESIGN.md §五：「动画不要用
			// transform 动大子树……要动就动**布局位置**」）。软件渲染回退时
			// transform 会让整棵子树逐像素重绘。
			//
			// ⚠️ **第一次建要 Jump 一下**：框架的规矩是「一个值从它的第一个目标
			// 开始」，所以不先 Jump 的话面板会**直接出现在**右沿上，没有滑入。
			if justIn {
				panel.Jump(commentsSlideKey, -commentsDrawerWidth)
			}
			// 目标：开着就贴住右沿（0），关着就整个在窗口外面。
			target := float32(0)
			if s.commentsPhase == drawerOut {
				target = -commentsDrawerWidth
			}
			slide := panel.AnimateTo(commentsSlideKey, target, queueSlideAnim)
			panel = panel.Right(slide)

			// 到位了 / 滑到头了：换一段。
			//
			// ⚠️ **用 Settled 不用 Arrived**（理由同 `queue_drawer.go`）：退场这一段
			// 要的是「滑到头了就把面板拆掉」。
			if panel.Settled(commentsSlideKey) {
				switch s.commentsPhase {
				case drawerIn:
					s.commentsPhase = drawerShown
				case drawerOut:
					s.commentsPhase = drawerGone
				}
			}
		})

		// 关抽屉的两条路：点底下那一层、按 Esc。少一条的表现是「我明明点了外面，
		// 它还在」，另一条少的是「按了 Esc 什么都没发生」。
		//
		// ⚠️ 用 `back.Clicked()`，**不要用 `panel.PressedOutside()`**（理由见
		// 文件头）。
		//
		// ⚠️ **这里是「=false」不是「= !commentsOpen」**：关的动作是幂等的——
		// 退场那 200ms 里用户再点一次外面，不该把抽屉又打开。
		if back.Clicked() || back.OverlayShortcut(0, ui.KeyEscape) {
			s.commentsOpen = false
		}
	})
}

// syncCommentsDrawerPhase 推一遍评论抽屉的阶段机，返回这一帧是不是「刚打开」。
//
// ⚠️ **它和队列抽屉那一支是同一套形状，但各推各的**（两处各有一份状态）：
// 一个抽屉开着的时候另一个可以也开着——而它们的阶段是**各自的**。
//
// ⚠️ **「关」是幂等的**：`commentsOpen = false` 连按几次都是「关」，而
// `commentsOpen = !commentsOpen` 那样写在关的动作上就是「开」——退场那 200ms
// 里用户再点一次外面，抽屉会弹回来。
func (s *Shell) syncCommentsDrawerPhase() bool {
	switch s.commentsPhase {
	case drawerGone:
		if !s.commentsOpen {
			return false
		}
		// 刚开：面板从窗口外面滑进来。
		s.commentsPhase = drawerIn
		return true
	case drawerIn, drawerShown:
		if !s.commentsOpen {
			s.commentsPhase = drawerOut
		}
	case drawerOut:
		if s.commentsOpen {
			s.commentsPhase = drawerIn
		}
	}
	return false
}

// viewCommentsDrawerBody 是抽屉里面那一块：页头 + 评论列表。
//
// ⚠️ **它和原来那一页的画法是同一份**（`viewCommentsContent`）：评论从独立页
// 搬进抽屉之后，**功能一个字不许少**（排序两档、刷新、只看提到歌名的开关、
// 楼中楼、高亮、翻页、分页哨兵）。所以这里**复用**那一页的画法，而不是抄一份
// ——抄一份的话两边迟早分叉（而分叉出来的那种差异极难查）。
func (s *Shell) viewCommentsDrawerBody(c *ui.Context, t *ui.Theme) {
	s.viewCommentsContent(c, t)
}
