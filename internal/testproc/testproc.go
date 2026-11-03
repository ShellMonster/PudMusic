// Package testproc 是「起一个子进程跑本测试自己」的那一份共用配方。
//
// # 为什么需要它
//
// 有几条用例要证明「设置在**另一个进程**里也读得到」——那就得起一个子进程，
// 而子进程跑的就是**本测试二进制自己**（`os.Args[0]` 加上 `-test.run=…`）。
//
// ⚠️ **而那一份的寿命不是我们的**：`os.Args[0]` 指的是 `go test` 的工作目录
// 里的那一份（`/tmp/go-buildNNN/bNNN/pkg.test`），**别的 `go` 进程会把那些
// 工作目录清掉**。本机实测（2026-10-24）：子进程起不来，红在
//
//	fork/exec /tmp/go-build…/app.test: no such file or directory
//
// 而那是一条**和改动毫无关系**的红——它红在别的包（`internal/app`）里，而
// 跑它的人什么都没改（这台机器上同时有好几个 agent 在跑同一份验收命令）。
//
// # 这一份配方做什么
//
// **进程一启动就把二进制**（硬链接一份；链接不了就复制一份）**到自己的临时
// 目录里**，于是子进程跑的是那份副本——**工作目录什么时候被清掉都不相干**。
// 那一刻二进制一定还在（进程正靠它跑着）。
//
// # 磁盘
//
// 优先**硬链接**：它和原文件是同一个 inode，**不额外占磁盘**。链接不了
// （跨设备、文件系统不支持）才退回复制。
package testproc

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var (
	once sync.Once
	// self 是那份副本的路径，err 是它的来由。
	self string
	err  error
)

// init 在**进程一启动**的时候把本测试二进制备一份。
//
// ⚠️ **必须是进程一启动**，有两个理由：
//
//   - 那刻 `go test` 的工作目录**一定还在**（而它什么时候被别的 go 进程
//     清掉是不由我们定的——见文件头）；
//   - 那刻 **`TMPDIR` 还是环境里那个**。有的用例会把 `TMPDIR` 改成自己的
//     临时目录（`internal/platform` 的锁那几处就改），而那份副本是要活过
//     整条用例的——放到用例自己的目录里，等用例收尾删目录时**副本跟着一起
//     没了**（实测 2026-10-24：子进程起不来，红在
//     `fork/exec /tmp/biulock…/biu-testproc-…/platform.test: no such file or directory`）。
func init() {
	prepare()
}

// prepare 备那份副本（只做一次）。
func prepare() {
	once.Do(func() { self, err = copySelf() })
}

// copySelf 备一份本测试二进制，返回副本的路径。
func copySelf() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("找不到本测试二进制：%w", err)
	}
	dir, err := os.MkdirTemp("", "biu-testproc-")
	if err != nil {
		return "", fmt.Errorf("建临时目录：%w", err)
	}
	dst := filepath.Join(dir, filepath.Base(exe))
	if err := os.Link(exe, dst); err == nil {
		return dst, nil
	}
	// 链接不了（跨设备、文件系统不支持）就复制一份。
	if err := copyFile(exe, dst); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dst, nil
}

// copyFile 把 src 复制到 dst。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开本测试二进制：%w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("建副本：%w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("复制本测试二进制：%w", err)
	}
	return nil
}

// Self 是本测试二进制那份副本的路径。
//
// 备不出来就返回那个错——**调用方自己决定怎么办**（有 `*testing.T` 的用
// `Path`，它会把这条用例判死；没有的就退回 `os.Args[0]`，也就是从前那份
// 做法）。
func Self() (string, error) {
	prepare()
	if err != nil {
		return "", err
	}
	return self, nil
}

// Path 是本测试二进制那份副本的路径——**起子进程用它**。
//
// 备不出来就当场把这条用例判死：那说明子进程这条路走不通，而让用例红在一个
// 「fork/exec …: no such file or directory」上更难查。
func Path(t *testing.T) string {
	t.Helper()
	p, err := Self()
	if err != nil {
		t.Fatalf("备不出本测试二进制的一份副本，子进程起不来：%v", err)
	}
	return p
}

// # 「慢测试声明」约定
//
// 这条约定解决的是：`-short` 不是「跳过」的同义词，它是个**信号**——
// 「我走的是默认/CI/评审路径，时间紧」。测试自己决定拿它怎么办。
//
// 三条用法，按代价从高到低：
//
//   - **整条跳过**：`testproc.Slow(t)`。留给那些「缩短了就不算数」的用例
//     （比如要量墙钟的，或素材短了判别力就塌的）。跳过前必须在注释里写清
//     「默认路径为什么可以跳」——不是「它慢」，而是「它验的那件事在默认
//     路径上由谁兜着」。
//   - **跑缩短版**：`if !testing.Short() { /* 完整素材 */ }`。素材长度不进
//     判据的用例用这个（判别是绝对量、相对量，或长度只是「盖住 N 跳」的
//     余量）。注释里要写清缩短后还验什么、丢的是什么余量。
//   - **不挂**：判据和素材长度解耦、或本来就快的，别挂。挂了 Short 的用例
//     在默认路径上等于不存在，宁可慢也别让一条该红的绿了。
//
// 挂 Short 的用例，注释里回答一个问题：**默认路径（CI、评审、`go test -short`）
// 跳/缩了它之后，它验的那件事还由谁兜着？** 答不上来的用例不许挂。
//
// Slow 把「这条是慢测试」标出来，`t.Helper` 记下调用点，`t.Logf` 把理由
// 写进 `-v` 输出——评审日志里能直接看到它为什么被跳过，不用翻源码。
//
// ⚠️ 它不是给「有点慢」用的。一条用例该不该挂 Short，看的是「默认路径跳了它
// 会不会漏验」，不是看绝对耗时。5 秒的用例如果验的是别处验不到的东西，不许挂；
// 0.5 秒的用例如果纯是凑数，也可以挂。
func Slow(t *testing.T, why string) {
	t.Helper()
	if testing.Short() {
		t.Skipf("慢测试：%s（-short 走默认路径，完整版留给本地 go test）", why)
	}
	t.Logf("慢测试：%s——默认路径跑的是完整版", why)
}

// Cleanup 把那份副本删掉。
//
// ⚠️ **测试进程退出前调一次**（放在 `TestMain` 的收尾里）：副本是一个链接，
// 留着它那份磁盘就一直占着（链接和原文件是同一个 inode，原文件没了它就
// 一直占着）。
func Cleanup() {
	if self == "" {
		return
	}
	os.RemoveAll(filepath.Dir(self))
}
