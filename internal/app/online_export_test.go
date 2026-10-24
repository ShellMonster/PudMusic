package app

import (
	"fmt"
	"sync"
	"time"
)

// 这一份是给**外部测试包**（`package app_test`）用的几个接缝。
//
// 为什么要走接缝而不是把测试写进这个包：在线播放的夹具（假 B 站：一台
// `httptest` 服务器同时扮演取流接口和 CDN）在 `online_test.go` 里，而那是
// **外部**测试包。测试接缝只能从里面导出给外面用（Go 的惯例：
// `export_test.go` 里的导出只在测试构建里存在）。

// LoadSlowAfter 是「这一段慢到该告诉用户了」那个门槛（给测试用）。
//
// ⚠️ **给测试看的是它「小于超时那个」**：慢那一档必须在超时之前就说话，
// 否则用户白等 20 秒才等到一句「超时」。
const LoadSlowAfter = loadSlowAfter

// LoadPlayTimeout 是「这一段超时了判失败」那个门槛（给测试用）。
const LoadPlayTimeout = onlinePlayTimeout

// SetAfterForTest 换掉「排定时器」那一个入口（给测试用）。
//
// 生产上它是 `time.AfterFunc`；换成假的之后，「取流超时」那个闹钟由测试
// 自己点火——不用真的等 15 秒（`onlinePlayTimeout`）。
//
// ⚠️ **换掉之后 `a.after` 就是全局的了**：所以用它的那条测试不能和别的测试
// 并行跑（同一个 App 实例上的）。
func (a *App) SetAfterForTest(f func(time.Duration, func()) func()) { a.after = f }

// LoadedBytesForTest 报「这一趟取流的字节源已经下到多少字节」（给测试用）。
//
// ⚠️ **它报的是字节源自己那份数**（`Filled()`），不是缓存文件的大小：失败
// 那句话里写的就是这个数（见 `online.go` 的「已经下了 %s」），而文件可能
// 比它大——传输那一头写得快，读它的那一头慢（满载的时候差得出来）。
//
// ⚠️ **读 `loadSrc` 要持锁**（看门狗也是这么读的，见 `online.go` 的
// `downloadProgressLocked`）。
func (a *App) LoadedBytesForTest() int64 {
	a.mu.Lock()
	src := a.loadSrc
	a.mu.Unlock()
	if src == nil {
		return 0
	}
	return src.Filled()
}

// CapturePlayLog 把**播放路径**的日志收进一个切片，返回还原函数。
//
// 为什么需要它：播放路径的日志是「点了歌不播」这类问题**唯一**能查的东西
// （票 #282）——所以「代次变了要留一句」「失败要写清是哪一首」这些事都得有
// 一条测试钉着，而钉它们就得能读到写出去的那些字。
//
// ⚠️ **它是这一个 App 的**：日志出口挂在 App 上（见 App.playLog），所以两个
// 并行的测试不会互相看见对方的日志——而这一层的日志正是「哪一次播放出的事」
// 的唯一凭据。
func CapturePlayLog(a *App) (*[]string, func()) {
	var mu sync.Mutex
	var lines []string
	a.playMu.Lock()
	old := a.playLog
	a.playLog = func(format string, args ...any) {
		mu.Lock()
		lines = append(lines, fmt.Sprintf(format, args...))
		mu.Unlock()
	}
	a.playMu.Unlock()
	return &lines, func() {
		a.playMu.Lock()
		a.playLog = old
		a.playMu.Unlock()
	}
}
