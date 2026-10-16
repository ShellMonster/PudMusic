//go:build darwin

package localmusic

import (
	"io/fs"
	"syscall"
	"time"
)

// Created 报文件的创建时间。
//
// macOS 的 `stat` 结构里有专门的 `Birthtimespec`（HFS+/APFS 从 10.12 起才有，
// 更早的卷上它是零值），这就是 Biu 那边 `stat.birthtimeMs` 拿到的东西。
//
// 给不出的时候退回修改时间：Biu 是 `stat.birthtimeMs || stat.mtimeMs`
// （`local-music.ts:67`），那一栏至少得有个数。
func Created(fi fs.FileInfo) time.Time {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		if bt := st.Birthtimespec; bt.Sec != 0 {
			return time.Unix(bt.Sec, bt.Nsec)
		}
	}
	return fi.ModTime()
}
