//go:build !darwin && !windows

package localmusic

import (
	"io/fs"
	"time"
)

// Created 报文件的创建时间。
//
// 首发平台只有 macOS 和 Windows，那两边都有真正的创建时间（见
// `created_darwin.go` / `created_windows.go`）。别的系统上退回修改时间：
// Linux 的 stat 结构里那个叫 `Ctim` 的是「状态最后变更时间」，语义不是一回事，
// 拿来当创建时间显示是在说假话，不如老实给修改时间。
func Created(fi fs.FileInfo) time.Time { return fi.ModTime() }
