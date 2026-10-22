package ui

import (
	"slices"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// Toast 是 Biu 全局浮层里最常用的一条。参数逐条抄自 Biu 的
// `src/app.tsx:166-172` 的 `<ToastProvider>`：
//
//	placement="bottom-right"   → 右下角
//	toastOffset={90}           → 抬过底部 90px（播放条）
//	maxVisibleToasts={3}       → 同时最多 3 条
//	toastProps={{ timeout: 2000, color: "primary" }} → 2 秒、主色卡片
//
// 这里自己管，不用 MyGo 内置的那一套：`AddToast` / `CloseToast` 挂在未导出的
// `*ui.context` 上，从我们这个包里根本调不到（`ui.Context` 上没有转发）。
// 而且「避开播放条、切路由不留上一页的提示」本来也得自己排版、自己管状态。
const (
	// toastLimit 是同时在屏幕上最多几条。多出来的把最旧的挤掉。
	toastLimit = 3
	// playbarClearance 是提示要抬过多高的那条底边。Biu 的 `toastOffset` 是 90。
	playbarClearance = 90
)

// toastLifetime 是一条提示待多久。Biu 的 timeout 是 2000ms。
//
// 它是 var 不是 const：测试要把它改短，不然每条都干等 2 秒。
var toastLifetime = 2 * time.Second

// toast 是一条提示。id 用来在到点时认出「是自己那一条」——文字可能重复。
type toast struct {
	id    uint64
	text  string
	until time.Time
}

// addToast 弹一条提示。多到 3 条时把最旧的挤掉。
func (s *Shell) addToast(text string) {
	s.toastMu.Lock()
	s.toastSeq++
	t := toast{id: s.toastSeq, text: text, until: time.Now().Add(toastLifetime)}
	s.toasts = append(s.toasts, t)
	if len(s.toasts) > toastLimit {
		s.toasts = s.toasts[1:]
	}
	s.toastMu.Unlock()

	go s.expire(t)
}

// expire 等到点就把这一条收走，然后叫醒界面。
//
// ⚠️ 收走之后**必须**调 invalidate。少了这一行，这条提示会一直挂在屏幕上，
// 直到别的原因碰巧重画了界面为止——「只投递不唤醒 = 0 帧刷新」实测踩过。
func (s *Shell) expire(t toast) {
	if d := time.Until(t.until); d > 0 {
		time.Sleep(d)
	}
	s.dropToast(t.id)
	s.wake.invalidate()
}

// dropToast 收走 id 那一条。
func (s *Shell) dropToast(id uint64) {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	for i := range s.toasts {
		if s.toasts[i].id == id {
			s.toasts = slices.Delete(s.toasts, i, i+1)
			return
		}
	}
}

// clearToasts 清空全部提示。切路由时调：上一页的提示不该留到下一页。
func (s *Shell) clearToasts() {
	s.toastMu.Lock()
	s.toasts = nil
	s.toastMu.Unlock()
}

// visibleToasts 是这一帧该画的几条，顺手把到点的收走。
func (s *Shell) visibleToasts() []toast {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	now := time.Now()
	kept := s.toasts[:0]
	for _, t := range s.toasts {
		if t.until.After(now) {
			kept = append(kept, t)
		}
	}
	s.toasts = kept
	// 复制一份再给出去：s.toasts 的底层数组下一帧会被 reuse。
	return slices.Clone(kept)
}

// viewToasts 把提示浮在右下角、播放条上面。
//
// 用 Overlay 铺一层铺满窗口的空容器，而不是在主布局里塞一段留白——提示不该
// 把底下的内容顶开。
func (s *Shell) viewToasts(c *ui.Context) {
	list := s.visibleToasts()
	if len(list) == 0 {
		return
	}
	t := c.Theme()

	ui.Overlay(c, func() {
		// Key 要写在构造之前：同一帧里这个浮层只建一次，key 用来在下一帧
		// 认出是它。
		ui.Column(c).Key("toasts").
			Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.End).AlignItems(ui.End).Gap(8).
			// 底边留出播放条的高度，再加一圈边距。Padding 的四个参数是
			// 上、右、下、左，别把 88 写成了左边距。
			Padding(16, 16, playbarClearance+16, 16).
			// 空的地方让鼠标穿过去，不然会挡住底下侧栏和播放条的点击。
			PassThrough().
			Children(func() {
				for _, item := range list {
					s.viewToast(c, item, t)
				}
			})
	})
}

// viewToast 一条提示的样子。
//
// ⚠️ 卡片底色用 Accent / AccentText（Biu 的 `color: "primary"` 就是主色卡片 +
// 主色上的文字），**不要用 Theme.Inverse**：主题没设它的时候它是零值，
// 等于 Background —— 白底白卡片，元素排版排得好好的，屏幕上什么也看不见
// （头less Tester 只查元素框，查不到这个坑）。
//
// 🔴 **卡片本身也 `PassThrough`**——和外面那个铺满窗口的容器一样（见 viewToasts）。
// 一条提示**一个控件都没有**：它不可点、不可关、也不带任何动作。挡在它底下的东西
// 却是实打实的可点控件，而提示在右下角、抬过播放条之后正好压住页面右下角那一块
// （设置页「快捷键」那一页最后那行的 × 就在那儿）。
//
// 症状是「刚改完一个设置，右下角弹出一句提示，两秒之内那个 × 点不动」——而提示
// 两秒后自己没了，于是这件事看起来像是那个 × 偶尔失灵，最容易被当成界面坏了。
//
// 框架那一边也是同一个做法：它自己的 tooltip 也是 `Absolute().PassThrough()`
// （`mygo/ui/tooltip.go:80`）——同样的话，同样没有控件可点。
func (s *Shell) viewToast(c *ui.Context, t toast, theme *ui.Theme) {
	ui.Column(c).Width(320).Radius(10).PassThrough().
		Background(theme.Accent).Padding(12, 16).
		Children(func() {
			ui.Text(c, t.text).FontSize(scale.Font(c, 13)).TextColor(theme.AccentText).PassThrough()
		})
}
