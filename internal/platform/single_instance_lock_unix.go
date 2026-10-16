//go:build !windows

// 内核那把独占锁的实现：flock(2)。
//
// 为什么是它而不是「O_EXCL 建一个锁文件」：flock 是内核在**进程消失的那一刻**
// 就把锁还回去的，锁住的东西跟着进程的生命周期走。被 SIGKILL 杀了、被强制退出
// 了、崩了——都没有「上次留下的陈旧锁文件要判断主人死没死」这种状态要处理。
// O_EXCL 那套要写 pid、要判断 pid 还在不在（还会碰上 pid 复用），每多一行就是
// 多一个能出错的分支。
//
// macOS 和 Linux 的标准库里都有 `syscall.Flock`，不用引新依赖。
package platform

import (
	"errors"
	"os"
	"syscall"
)

// lockExclusive 拿那把独占锁。拿到返回 true，拿不到（别人拿着）返回 false。
//
// **文件句柄必须留着**：flock 记在打开的那个文件描述符上，进程退出时内核把它
// 放掉，但 `f.Close()` 也会。所以这里不能关——存进包级的 lockFile，让它活到
// 进程结束。（Windows 那份不设它：那边的互斥是「文件存在」这件事。）
func lockExclusive() bool {
	f, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		// 建不出来（家目录只读、AppData 拿不到…）。这时候**别把用户锁在外面**：
		// 宁可有第二个实例，也不要一个都起不来。框架那把锁还在，仍然能挡住
		// 绝大多数「不是同时启动」的重复启动。
		return true
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		lockFile = f
		// 拿到锁就先抹掉上一次留下的 ready（见 clearLockReady 的注释：留着它
		// 会让后来者以为第一个实例已经就绪，白白去抢它的 socket）。
		// 这一步必须在框架那把锁**之前**：这中间没有别人能拿到内核锁。
		clearLockReady()
		return true
	case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EAGAIN):
		// 有人在用。Linux 上 flock 返回 EWOULDBLOCK，Darwin 上是 EAGAIN，
		// 两个值在 Linux 上是同一个，所以要都查。
		_ = f.Close()
		return false
	default:
		// flock 本身不认（网络文件系统之类）。同上，宁可放行。
		_ = f.Close()
		return true
	}
}
