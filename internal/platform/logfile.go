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
// （`~/Library/Logs/BiuGo/`），不去覆盖别人的文件——票 #282 里提到的那个路径
// 是看错了地方。
//
// # 大小
//
// 超过 `maxLogBytes` 就把上一份挪成 `main.log.1`（**只留一份**），然后从头写。
// 不设上限的话，一个一直开着、一直打日志的应用会把磁盘吃满——而应用日志
// 是「排障用」的，不是「永久档案」。
package platform

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

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

// OpenLogFileIn 把标准 logger 指到 dir 里的 `main.log`。
//
// 传回的文件**不要关**：它是标准 logger 的出口，关掉的话后面所有日志都进
// 不了文件（而 `log` 不会因此报错——它只是写不进去）。
func OpenLogFileIn(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("建不了日志目录 %s：%w", dir, err)
	}
	path := filepath.Join(dir, "main.log")
	// 上一份太大就挪开：留着它，用户翻日志时先看到的是昨天那次崩溃。
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogBytes {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("开不了日志文件 %s：%w", path, err)
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags)
	return path, nil
}
