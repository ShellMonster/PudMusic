//go:build windows

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

// freeDiskBytes 报**临时目录所在的卷**上还剩多少字节是当前用户能用的
// （`GetDiskFreeSpaceExW` 的 lpFreeBytesAvailableToCaller）。
//
// 量的是临时目录而不是别的盘，因为 `t.TempDir()` 建的就是它旁边的目录——
// 别的盘空着不算数。
//
// ⚠️ 用的是 "AvailableToCaller" 而不是 "TotalFree"：后者把分配额度
// （配额）也算在里面，而普通用户写不进那部分。
func freeDiskBytes() (uint64, error) {
	dir, err := syscall.UTF16PtrFromString(os.TempDir())
	if err != nil {
		return 0, err
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")
	var avail, total, totalFree uint64
	r, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(dir)),
		uintptr(unsafe.Pointer(&avail)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, callErr
	}
	return avail, nil
}
