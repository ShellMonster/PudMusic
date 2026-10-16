//go:build windows

// 内核那把独占锁的实现（Windows）：原子建一个锁文件。
//
// Windows 的标准库没有 flock，也没有 LockFileEx 能用（它在 `golang.org/x/sys`
// 那边，而这是个只为了单实例去加一个直接依赖不值得）。能用的是
// `os.OpenFile(path, O_CREATE|O_EXCL, …)`——在 Windows 上它落到
// `CREATE_NEW`，内核保证**只有一个进程建得出来**。这就是互斥本身。
//
// ## 比 unix 那份（flock）弱在哪
//
// flock 是内核在进程消失的那一刻就把锁还回去的，压根没有「上次留下的陈旧锁」
// 这种状态。这份要靠文件里记的 pid 判断主人还在不在：
//
//   - 主人正常退了 → 锁文件还在 → 下次启动靠 pid 判定把它清掉。
//   - 主人被强杀 → 同上。
//   - **pid 被别的进程复用了** → 会以为主人还活着，这一次拒绝启动。
//     极少见，而且下一次（那个进程没了）自己就好了。
//
// 想要和 flock 完全一样，得在 Windows 上接 `LockFileEx`（`golang.org/x/sys`）。
// 记在这里，别让它变成一个忘了的坑。
package platform

import (
	"errors"
	"os"
)

// lockExclusive 拿那把独占锁。拿到返回 true，拿不到（别人拿着）返回 false。
//
// unix 那份要留着文件句柄（flock 记在描述符上），这份**不要**：Windows 上的互斥
// 是文件「存在」这件事，句柄留着只会让文件删不掉。
func lockExclusive() bool {
	path := lockPath()
	for attempt := 0; attempt < 3; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
		if err == nil {
			_ = f.Close()
			// 只写 pid，不写 ready：ready 的意思是「socket 建好了」，那要等到
			// Lock 里调完框架那把锁之后才补上（见 markLockReady）。
			writeLockFile(false)
			return true
		}
		if !errors.Is(err, os.ErrExist) {
			// 建不出来（权限、AppData 拿不到…）。别把用户锁在外面：宁可有两个
			// 实例，也不要一个都起不来。框架那把锁还在，仍然挡得住绝大多数
			// 「不是同时启动」的重复启动。
			return true
		}
		if !lockHolderAlive() {
			// 上一次的主人已经不在了，清掉这份陈旧锁再来一轮。
			_ = os.Remove(path)
			continue
		}
		return false
	}
	return false
}

// lockHolderAlive 报锁文件里那个进程还在不在。
func lockHolderAlive() bool {
	pid, _ := readLockFile()
	if pid <= 0 {
		return false
	}
	// Windows 上 os.FindProcess 内部是 OpenProcess，进程没了它会报错。
	// （unix 上它永远成功，所以这份只在 Windows 上编译。）
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
