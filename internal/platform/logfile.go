// 应用日志文件：让应用日志**事后查得到**。
//
// # 为什么需要它
//
// 应用以前只往 stderr 打日志（`log.Printf`），而从一个 `.app` 里启动的进程，
// stderr 落在**系统日志**里（要 `log show` 才看得见），用户和维护者都找不到。
// 于是「点了歌不播」这类问题**完全查不了**——应用自己什么都没留下，
// 唯一能问的只有用户，而用户能描述的只有「它不响」（票 #282）。
//
// 这一层做的事就一件：**把标准 logger 指到一个文件**。
//
// # 落在哪
//
// `mygo.App.Path(mygo.PathLogs)`——框架自己的约定（`mygo@v0.6.3/paths.go:96`）：
//
//	macOS	~/Library/Logs/<应用名>/
//	其它	<用户配置目录>/<应用名>/logs/
//
// ⚠️ **不去动 Biu 的那个目录**：`~/Library/Logs/Biu/` 是**上游那个 Electron
// 应用**的日志（`main.log` 里全是 "Checking for update"）。我们写自己的目录
// （`~/Library/Logs/PudMusic/`），不去覆盖别人的文件——票 #282 里提到的那个路径
// 是看错了地方。
//
// # 大小
//
// 超过 `maxLogBytes` 就把上一份挪成 `main.log.1`（**只留一份**），然后从头写；
// 启动时查一次，运行中每次写之前也查一次。不设上限的话，一个一直开着、
// 一直打日志的应用会把磁盘吃满——而应用日志
// 是「排障用」的，不是「永久档案」。
package platform

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"

	"github.com/egoist/mygo"
)

// maxLogBytes 是日志文件多大就换一份。
//
// 2 MB 对这个应用够用：播放路径一行也就百来个字，一天几百次播放也就几百 KB。
const maxLogBytes = 2 << 20

// OpenLogFile 把标准 logger 指到日志文件（默认目录是框架的 logs 目录）。
//
// ⚠️ **它要在第一次 `log.Printf` 之前调**，而且调失败**不挡启动**：日志没了
// 应用照样能用，而为了写日志就开不了机是本末倒置（调用方拿到的那个错要
// 报给用户，不是吞掉）。
func OpenLogFile() (string, error) {
	dir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil {
		return "", fmt.Errorf("拿不到日志目录：%w", err)
	}
	return OpenLogFileIn(dir)
}

// OpenCrashLog 在日志目录里接上 `crash.log`，见 `OpenCrashLogIn`。
func OpenCrashLog() (string, error) {
	dir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil {
		return "", fmt.Errorf("拿不到日志目录：%w", err)
	}
	return OpenCrashLogIn(dir)
}

// OpenLogFileIn 把标准 logger 指到 dir 里的 `main.log`。
//
// 那个文件由标准 logger 一直持有，写满 `maxLogBytes` 时自己换一份。
func OpenLogFileIn(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("建不了日志目录 %s：%w", dir, err)
	}
	path := filepath.Join(dir, "main.log")
	w, err := openRotatingFile(path)
	if err != nil {
		return "", fmt.Errorf("开不了日志文件 %s：%w", path, err)
	}
	log.SetOutput(w)
	log.SetFlags(log.LstdFlags)
	return path, nil
}

// OpenCrashLogIn 让运行时把致命错误（任何 goroutine 里没接住的 panic、
// `fatal error`）的堆栈追加到 dir 里的 `crash.log`。
//
// 从 Finder / 资源管理器启动时 stderr 没人看，不接这一处崩溃就什么都不留。
func OpenCrashLogIn(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("建不了日志目录 %s：%w", dir, err)
	}
	path := filepath.Join(dir, "crash.log")
	rotateIfOver(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("开不了崩溃日志 %s：%w", path, err)
	}
	// SetCrashOutput 自己 dup 一份描述符，这边的可以关。
	defer f.Close()
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		return "", fmt.Errorf("接不上崩溃日志 %s：%w", path, err)
	}
	return path, nil
}

// rotateIfOver 把超过 `maxLogBytes` 的旧文件挪成 `<path>.1`（只留一份）。
func rotateIfOver(path string) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogBytes {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
}

// rotatingFile 是一个数着字节的追加写文件：这一笔会写过 `maxLogBytes`
// 就先挪成 `<path>.1` 再从头写。各个 goroutine 的 log 调用并发进来，所以上锁。
type rotatingFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func openRotatingFile(path string) (*rotatingFile, error) {
	rotateIfOver(path)
	r := &rotatingFile{path: path}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, fi.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil && r.size > 0 && r.size+int64(len(p)) > maxLogBytes {
		// 先关再挪：Windows 上挪不动一个还开着的文件。
		r.f.Close()
		r.f = nil
		_ = os.Remove(r.path + ".1")
		_ = os.Rename(r.path, r.path+".1")
	}
	if r.f == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}
