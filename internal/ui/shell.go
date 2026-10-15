// Package ui 是界面层：整个项目里只有它 import MyGo 的 ui 包。
//
// 本文件里的 `ui.Xxx` 一律指 MyGo 的 ui 包，不是本包——包内引用自己不需要写包名。
package ui

import (
	"log"
	"runtime/debug"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Shell 是一个窗口的全部东西：状态、画法、唤醒句柄。
type Shell struct {
	app *app.App
	win *mygo.Window

	// draw 是外面包了一层 recover 的 viewMain。
	draw func(*ui.Context)

	// wake 是这个窗口的唤醒句柄，由 view 每帧重新登记。
	wake wakeHandle

	// scrub 是进度条的值（0..1），held 是指针有没有按在它上面。
	// 两个都只由 view 读写——view 永远跑在主线程上。
	scrub float64
	held  bool

	// crash 是上一帧 panic 到的东西，非 nil 时这一帧画错误页。
	crash any
}

// New 建一个窗口外壳，绑在状态 a 上。
func New(a *app.App) *Shell {
	s := &Shell{app: a}
	s.draw = Guard(s.viewMain, s.onPanic)
	return s
}

// Attach 把外壳接到窗口上：装唤醒回调，窗口关掉时收尾。
func (s *Shell) Attach(w *mygo.Window) {
	s.win = w
	s.app.SetWake(s.wake.invalidate)
	w.OnClosed(func() { s.app.Close() })
}

// Guard 给一个 view 包一层页面级的 panic recover。
//
// 这是整个界面层最重要的一条约定：MyGo 的 Dispatch（app.go:576）和 drain
// 都没有 recover，view 里 panic 会穿透 Cocoa 事件循环**直接杀死进程**，连
// 「这一页坏了」的机会都没有。Biu 原来靠 React 的 ErrorBoundary 兜住，
// 这里用 recover 兜住同一个位置的效果。
//
// onPanic 拿到崩掉的东西，界面用它换一张错误页。崩了以后这一帧已经建了一半，
// MyGo 的 build pass 会在下一次 pass 从头重建，所以 onPanic 里只要记下状态
// 并要一帧，画面下一帧就是完整的。
func Guard(draw func(*ui.Context), onPanic func(any)) func(*ui.Context) {
	return func(c *ui.Context) {
		defer func() {
			if r := recover(); r != nil {
				onPanic(r)
			}
		}()
		draw(c)
	}
}

// View 画一帧，MyGo 在需要新画面的时候调它。
func (s *Shell) View(c *ui.Context) {
	// 每帧重新登记：句柄内部是窗口的弱引用，窗口关了自动失效，关掉重开也不用管。
	s.wake.set(c)

	if s.crash != nil {
		s.viewCrash(c)
		return
	}
	s.draw(c)
}

// onPanic 记下崩掉的东西并要一帧，下一帧画错误页。
func (s *Shell) onPanic(r any) {
	s.crash = r
	log.Printf("biu: 界面崩溃，已切到错误页\n%v\n%s", r, debug.Stack())
	s.wake.invalidate()
}

// viewMain 是正常的那一帧：上面一片空，中间是播放条。
func (s *Shell) viewMain(c *ui.Context) {
	t := c.Theme()
	st := s.app.Snapshot()

	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Gap(10).Padding(16).Children(func() {
			if ui.Button(c, "打开文件").Clicked() {
				s.pickFile()
			}
			ui.Text(c, "Biu MyGo").FontSize(16).Bold().TextColor(t.TextMuted)
		})
		if st.Path != "" {
			ui.Text(c, st.Path).FontSize(11).TextColor(t.TextMuted).Padding(0, 0, 16, 0)
		}
		ui.Column(c).Grow(1)
		s.viewPlayBar(c, st)
	})
}

// viewCrash 是崩溃后那一帧。Biu 原来的 ErrorBoundary 也是这么做的：程序活着，
// 出事的那块换一张说明页。
func (s *Shell) viewCrash(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(10).Padding(24).Children(func() {
		ui.Text(c, "界面出错了，播放器已经停在这一页").FontSize(18).Bold()
		ui.Textf(c, "%v", s.crash).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, "细节已经写进日志。点下面的按钮再试一次。").FontSize(12).TextColor(t.TextMuted)
		if ui.PrimaryButton(c, "重新载入").Clicked() {
			s.crash = nil
		}
	})
}

// pickFile 弹系统文件框。
//
// 对话框在另一个 goroutine 里开：MyGo 把弹框请求 post 到主线程再等系统回调，
// 在主线程里等就是死等，事件循环会卡死。
func (s *Shell) pickFile() {
	win := s.win
	go func() {
		paths, err := platform.PickAudioFile(win)
		if err != nil || len(paths) == 0 {
			return
		}
		s.app.Open(paths[0])
	}()
}
