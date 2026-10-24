package app_test

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/testproc"
)

// TestMain 是这一包的门：跑完把「子进程跑本测试自己」的那份副本收掉。
//
// ⚠️ **不收的话那份副本会一直占着磁盘**：它和 `go test` 工作目录里那一份是
// 同一个 inode（硬链接），工作目录被删之后副本还占着那份空间——而那是整个
// 测试二进制那么大。
func TestMain(m *testing.M) {
	code := m.Run()
	testproc.Cleanup()
	os.Exit(code)
}
