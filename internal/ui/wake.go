package ui

import (
	"sync"

	"github.com/egoist/mygo/ui"
)

// wakeHandle 记着一个窗口的唤醒句柄，用来从后台 goroutine 叫醒它。
//
// ⚠️ 规则一：后台投递完状态**必须**调用 Invalidate。不唤醒的话，界面永远停在
// 旧帧（实测：不唤醒 = 新状态到了但 0 帧刷新）。
//
// ⚠️ 规则二：改状态用 Window.Update(func)，只刷画面用 Invalidate()。
// Window.Update(nil) 会 SIGSEGV —— content.go:174 没有 nil 检查，直接 fn()。
type wakeHandle struct {
	mu   sync.Mutex
	svc  ui.Services
	held bool
}

// set 在 view 每帧开头调用，登记本窗口这一帧的句柄。
func (h *wakeHandle) set(c *ui.Context) {
	h.mu.Lock()
	h.svc, h.held = c.Services(), true
	h.mu.Unlock()
}

// invalidate 从任何 goroutine 都能调，要一帧新画面。
//
// Services 内部是窗口的弱引用，窗口关了自动失效，这里不用管关窗的事。
func (h *wakeHandle) invalidate() {
	h.mu.Lock()
	svc, held := h.svc, h.held
	h.mu.Unlock()
	if held {
		svc.Invalidate()
	}
}
