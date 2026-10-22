//go:build windows && (amd64 || arm64)

package mfadec

// 系统 DLL 的加载。
//
// **一律从系统目录按全路径加载**，不靠搜索路径。理由和 `internal/media`
// 那一层一样：搜索路径里第一个是**可执行文件所在的目录**，而那个目录用户
// 是能写的。从那里先找到一份同名 DLL 就等于把「加载系统组件」这一步交出去了。
//
// mfplat.dll 和 ole32.dll 都是 Windows 自带的组件：不静态链接进我们的
// 二进制，不进 go list -deps，也没有许可证要随包发出去——理由写在
// `internal/licensing` 的清单头里。

import (
	"fmt"
	"syscall"
	"unsafe"
)

// kernel32 是 KnownDLL，不走下面的全路径那一套（它是拿到系统目录的那把梯子）。
var kernel32 = syscall.NewLazyDLL("kernel32.dll")

// systemDirectory 问系统自己 Windows 装在哪儿。
func systemDirectory() string {
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, _ := kernel32.NewProc("GetSystemDirectoryW").Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) > len(buf) {
		return `C:\Windows\System32`
	}
	return syscall.UTF16ToString(buf[:n])
}

var sysDir = systemDirectory()

// systemDLL 按全路径打开一个系统 DLL。
func systemDLL(name string) *syscall.LazyDLL {
	return syscall.NewLazyDLL(sysDir + `\` + name)
}

// kernel32DLL 是 kernel32 的全路径句柄。
func kernel32DLL() *syscall.LazyDLL { return systemDLL("kernel32.dll") }

// missingProcs 数的是「要用的系统函数里，有几个在这台机器上取不到」。
//
// 取不到符号这件事本身不报错——`LazyProc.Call` 在符号没找到时 **panic**，
// 而 panic 的位置是「解码器刚建起来的那一刻」，在 COM 那条路上 panic 会直接
// 带走进程。所以这里先探一遍，探不到就带着一句人话报错回去。
func missingProcs() []string {
	all := map[string]*syscall.LazyProc{
		"MFStartup":            procMFStartup,
		"MFCreateMediaType":    procMFCreateMediaType,
		"MFCreateSample":       procMFCreateSample,
		"MFCreateMemoryBuffer": procMFCreateMemoryBuffer,
		"CoInitializeEx":       procCoInitializeEx,
		"CoUninitialize":       procCoUninitialize,
		"CoCreateInstance":     procCoCreateInstance,
		"GetCurrentThreadId":   procGetCurrentThreadID,
	}
	var missing []string
	for name, p := range all {
		if p.Find() != nil {
			missing = append(missing, name)
		}
	}
	return missing
}

// bindingsError 把「缺哪个符号」说成人话。
func bindingsError() error {
	missing := missingProcs()
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("mfadec: 这个系统上缺这些系统函数：%v（Media Foundation 装了吗？）", missing)
}
