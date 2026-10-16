//go:build windows

// 内核那把独占锁的实现：LockFileEx(2)——unix 上 flock(2) 的对应物。
//
// 标准库的 `syscall` 里没有 LockFileEx，常见的写法是引 `golang.org/x/sys/windows`。
// 这里没有引：那是为了一把锁函数加一个直接依赖，而这两个 API 从 Windows 2000 起
// 就是稳定 ABI，用 stdlib 的 `NewLazyDLL` 直调更省事（`single_instance.go` 里
// 那个 `lockFile` 也还是包级的、和 unix 那份共用）。
//
// ## 为什么不用「O_EXCL 建锁文件 + 记 pid」
//
// 试过，是错的，而且错得不隐蔽：
//
//  1. 进程 A：`O_CREATE|O_EXCL` 建出一个**空**锁文件，立刻关掉句柄；
//  2. **在 A 把 pid 写进去之前**，进程 B 撞进来 → 拿到 EEXIST；
//  3. B 读锁文件，读到 `pid<=0`（`readLockFile` 对「读不出来」一律当成主人没了）；
//  4. B 于是把 **A 的锁文件删了**，自己再建一个 → 两个都成了 first。
//
// 窗口就在第 1 步和「写 pid」那一次 `os.WriteFile` 之间——两次系统调用，不可能合
// 成一趟。后果不只是「两个实例」，还和 unix 那条路同一个：谁活下来谁的 socket 就可能
// 被另一个进程的 `os.Remove` 毁掉，之后 deep link 再也递不进来。
//
// 别的补法也都不划算：把「建 + 写」做成原子的（Windows 上做不到），或者删之前反复
// 确认主人还在（确认和删除之间还是有窗口）。LockFileEx 压根没有这些状态——
//
//   - 锁记在**打开的文件句柄**上，不在「文件存不存在」上；
//   - 进程一消失（正常退出、被强杀、崩溃）内核就放回去，没有陈旧锁文件要判主人死没死；
//   - 抢不到立刻返回 false（`LOCKFILE_FAIL_IMMEDIATELY`），不等。
package platform

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// kernel32 里那两个函数。
var (
	modkernel32    = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx = modkernel32.NewProc("LockFileEx")
)

// LockFileEx 的标志位与错误码。
const (
	lockfileFailImmediately uint32 = 0x00000001 // 抢不到立刻返回，别等
	lockfileExclusiveLock   uint32 = 0x00000002

	// ERROR_LOCK_VIOLATION：锁被别的进程拿着。
	errorLockViolation syscall.Errno = 33
)

// lockExclusive 拿那把独占锁。拿到返回 true，拿不到（别人拿着）返回 false。
//
// **文件句柄必须留着**（记在包级的 lockFile 里）：锁记在句柄上，关掉句柄等于放掉锁。
func lockExclusive() bool {
	f, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		// 建不出来（权限、AppData 拿不到…）。别把用户锁在外面：宁可有两个实例，
		// 也不要一个都起不来。框架那把锁还在，仍然挡得住绝大多数「不是同时启动」
		// 的重复启动。
		return true
	}

	// 偏移全是 0，等于锁「从第 0 个字节起的一段」；锁几个字节无所谓，重点是
	// 独占 + 立刻返回。
	var ol syscall.Overlapped
	r, _, callErr := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0,    // 保留参数，必须是 0
		1, 0, // 锁的字节数：低 32 位 1，高 32 位 0
		uintptr(unsafe.Pointer(&ol)),
	)

	switch {
	case r != 0:
		lockFile = f
		// 拿到锁就先抹掉上一次留下的 ready（见 clearLockReady 的注释：留着它
		// 会让后来者以为第一个实例已经就绪，白白去抢它的 socket）。
		// 这一步必须在框架那把锁**之前**：这中间没有别人能拿到内核锁。
		clearLockReady()
		return true
	case errors.Is(callErr, errorLockViolation):
		_ = f.Close()
		return false
	default:
		// 锁不了（网络驱动器之类）。同上，宁可放行。
		_ = f.Close()
		return true
	}
}
