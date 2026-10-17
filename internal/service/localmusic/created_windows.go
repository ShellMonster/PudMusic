//go:build windows

package localmusic

import (
	"io/fs"
	"syscall"
	"time"
)

// Created 报文件的创建时间。
//
// Windows 上是 NTFS 的「创建时间」（`CreationTime`，100 纳秒 tick 从 1601 年
// 起算，`syscall.NsecToTimeStamp` 的逆运算就是它）。见 `created_darwin.go`
// 那份的说明。
//
// NTFS 之外的卷（FAT32/exFAT 上的 U 盘、老移动硬盘）可能给不出，这时退回
// 修改时间——比给一个 1601 年的日期像样。
func Created(fi fs.FileInfo) time.Time {
	if st, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		if t := st.CreationTime; t.Nanoseconds() != 0 {
			return time.Unix(0, t.Nanoseconds())
		}
	}
	return fi.ModTime()
}
