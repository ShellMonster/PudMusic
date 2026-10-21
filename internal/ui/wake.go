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
//
// ⚠️ 规则三（票 #156）：**看不见的窗口不画**。`invalidate` 会问 `seen`，
// 用户看不到这一扇窗时只把这次唤醒丢掉，不请求帧——在放歌的时候 `App.tick`
// 每 50ms 叫醒每个窗口一次，那是风扇和电池的主要来源。叫醒本身仍然逐窗口
// 发生（铁律三不变），只是每一扇窗自己决定这一帧要不要画。
//
// 丢掉的那一帧不会丢内容：view 每帧都从 `App.Snapshot()` 读当前状态，窗口
// 重新看得见时 `noteWindow` 用 `invalidateNow` 补一帧，画出来就是此刻的样子。
type wakeHandle struct {
	mu   sync.Mutex
	svc  ui.Services
	held bool

	// seen 报「用户现在看得到这一扇窗」，nil = 没人管，永远算看得见。
	//
	// 由 `Shell` 接到它自己的 `windowPresence.seen`（见 presence.go）。做成
	// 函数而不是一个 bool：句柄在 `New` 里就能装好，而窗口的可见性是后来才
	// 建起来的；写成值的话得在每个窗口回调里同步两处，漏一处就是「窗口一直
	// 不刷新」而没有任何报错。
	seen func() bool
}

// set 在 view 每帧开头调用，登记本窗口这一帧的句柄。
func (h *wakeHandle) set(c *ui.Context) {
	h.mu.Lock()
	h.svc, h.held = c.Services(), true
	h.mu.Unlock()
}

// invalidate 从任何 goroutine 都能调，要一帧新画面。
//
// 窗口看不见时这一帧会被丢掉（见类型注释里的规则三）。窗口关了之后
// `Services` 内部是弱引用，窗口关了自动失效，这里不用管关窗的事。
func (h *wakeHandle) invalidate() { h.invalidateIf(true) }

// invalidateNow 是 `invalidate` 绕过「看不看得见」那条闸门的那一半。
//
// 只在一个场合用：窗口**刚从看不见变成看得见**（`Shell.noteWindow`）。那一刻
// 必须画一帧，否则界面停在前台之前的那一刻——而这一帧正是「下一帧就对齐」。
func (h *wakeHandle) invalidateNow() {
	h.invalidateIf(false)
}

// invalidateIf 是两者共用的那一半。askSeen 为 true 时先问可见性。
//
// 返回「有没有真的请求一帧」，测试里靠它分辨唤醒是被画掉了还是被丢掉了。
func (h *wakeHandle) invalidateIf(askSeen bool) bool {
	h.mu.Lock()
	svc, held, seen := h.svc, h.held, h.seen
	h.mu.Unlock()
	if !held {
		return false
	}
	if askSeen && seen != nil && !seen() {
		return false
	}
	svc.Invalidate()
	return true
}
