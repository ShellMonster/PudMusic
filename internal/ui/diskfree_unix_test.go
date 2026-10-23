//go:build unix

package ui

import (
	"os"
	"syscall"
)

// freeDiskBytes 报**临时目录所在的文件系统**上还剩多少字节是当前用户能用的
// （`df` 里的 "Avail" 那一列）。
//
// 量的是临时目录而不是别的盘，因为 `t.TempDir()` 建的就是它旁边的目录——
// 别的盘空着不算数。
//
// ⚠️ `Bavail` 是「非特权用户也能用」的那部分，比 `Bfree` 小（`Bfree` 里有
// root 预留的那一块）。用它才对：普通用户写的文件占不到预留区。
func freeDiskBytes() (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(os.TempDir(), &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
