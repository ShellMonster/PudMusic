package app_test

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"unsafe"
)

// fdsOn 数这个进程里有几个文件描述符指着 path。只数这一个文件：/dev/fd 的
// 总数会被别的用例在后台收尾时临时开着的文件带偏。
func fdsOn(t *testing.T, path string) int {
	t.Helper()
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("读不到 /dev/fd，跳过句柄检查：%v", err)
	}
	n := 0
	var buf [1024]byte // MAXPATHLEN
	for _, e := range entries {
		fd, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETPATH, uintptr(unsafe.Pointer(&buf[0])))
		if errno != 0 {
			continue
		}
		got := string(buf[:clen(buf[:])])
		if got == want {
			n++
		}
	}
	return n
}

func clen(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return len(b)
}
