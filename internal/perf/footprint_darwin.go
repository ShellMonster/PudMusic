//go:build darwin

package perf

import (
	"encoding/binary"
	"os"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// 进程级内存读数在 darwin 上走 Mach 的 `task_info(TASK_VM_INFO)`。
//
// 为什么不直接用 Go 的 heap 数：`runtime.MemStats` 只描述 Go 堆，而一个
// 界面进程的常驻内存还包含 Metal 纹理、AppKit 的对象、mmap 进来的文件和
// 解码器自己的缓冲。诊断快照要回答的是「这个进程现在占了多少物理内存」，
// 那个数只有系统知道——就是 `phys_footprint`（与活动监视器「内存」一列同源）。
//
// 这些字段在 `struct task_vm_info` 里的偏移由内核 ABI 固定：新系统只在尾部
// 追加字段，已有字段不搬家。所以这里按偏移取，而不是在 Go 里复刻整个结构体
// （那个结构体的长度随系统版本变，复刻反而会在新系统上读到错位的数据）。
const (
	taskVMInfoFlavor = 22 // TASK_VM_INFO

	taskVMInfoResidentOffset  = 16  // resident_size
	taskVMInfoFootprintOffset = 144 // phys_footprint

	// proc_pid_rusage 的 RUSAGE_INFO_V0：ri_resident_size / ri_phys_footprint
	// 同样在固定偏移上（后面的版本只追加字段）。
	rusageInfoV0Flavor    = 0
	rusageResidentOffset  = 64 // ri_resident_size
	rusageFootprintOffset = 72 // ri_phys_footprint

	rusageInfoBufferBytes = 512
	taskVMInfoBufferBytes = 1024
)

// footprintBindings 是懒绑定一次的 libSystem 符号。
type footprintBindings struct {
	// taskSelf 是当前任务的 task port。它是个函数（`task_self_trap`），不是
	// 导出的全局变量 `mach_task_self_`——读全局变量要先把符号地址转成指针，
	// 那一步过不了 vet 的 unsafeptr 检查（「possible misuse of unsafe.Pointer」），
	// 而这个函数给的是同一个值。
	taskSelf func() uint32
	taskInfo func(task uint32, flavor uint32, info unsafe.Pointer, count *uint32) int32
	// procPidRusage 是退路：`task_info` 失败时用它取 ri_phys_footprint。
	procPidRusage func(pid int32, flavor int32, info unsafe.Pointer) int32
}

var (
	footprintOnce sync.Once
	footprintLib  footprintBindings
)

// bindFootprint 绑定读取进程内存所需的 libSystem 符号。
//
// 两个来源都拿不到时保持零值：调用方据此报「未测量」，而不是报 0——0 会被
// 读成「这个进程没占内存」。
func bindFootprint() {
	if sym, err := purego.Dlsym(purego.RTLD_DEFAULT, "task_self_trap"); err == nil {
		var fn func() uint32
		purego.RegisterFunc(&fn, sym)
		footprintLib.taskSelf = fn
	}
	if sym, err := purego.Dlsym(purego.RTLD_DEFAULT, "task_info"); err == nil {
		var fn func(uint32, uint32, unsafe.Pointer, *uint32) int32
		purego.RegisterFunc(&fn, sym)
		footprintLib.taskInfo = fn
	}
	// proc_pid_rusage 在 libsystem_kernel 里，libSystem 重新导出了它。
	if sym, err := purego.Dlsym(purego.RTLD_DEFAULT, "proc_pid_rusage"); err == nil {
		var fn func(int32, int32, unsafe.Pointer) int32
		purego.RegisterFunc(&fn, sym)
		footprintLib.procPidRusage = fn
	}
}

// processFootprint 报这个进程的常驻字节与 phys_footprint。
//
// 先走 `task_info(TASK_VM_INFO)`（一次调用同时给两个数），失败再退到
// `proc_pid_rusage`。两条都失败时 ok=false。
func processFootprint() (resident, footprint int64, ok bool) {
	footprintOnce.Do(bindFootprint)
	if footprintLib.taskInfo != nil && footprintLib.taskSelf != nil {
		buf := make([]byte, taskVMInfoBufferBytes)
		count := uint32(len(buf) / 4) // integer_t 的个数，不是字节数
		if kr := footprintLib.taskInfo(footprintLib.taskSelf(), taskVMInfoFlavor, unsafe.Pointer(&buf[0]), &count); kr == 0 &&
			int64(count)*4 >= taskVMInfoFootprintOffset+8 {
			resident = int64(binary.LittleEndian.Uint64(buf[taskVMInfoResidentOffset:]))
			footprint = int64(binary.LittleEndian.Uint64(buf[taskVMInfoFootprintOffset:]))
			if resident > 0 && footprint > 0 {
				return resident, footprint, true
			}
		}
	}
	if footprintLib.procPidRusage != nil {
		buf := make([]byte, rusageInfoBufferBytes)
		if kr := footprintLib.procPidRusage(int32(os.Getpid()), rusageInfoV0Flavor, unsafe.Pointer(&buf[0])); kr == 0 {
			resident = int64(binary.LittleEndian.Uint64(buf[rusageResidentOffset:]))
			footprint = int64(binary.LittleEndian.Uint64(buf[rusageFootprintOffset:]))
			if resident > 0 && footprint > 0 {
				return resident, footprint, true
			}
		}
	}
	return 0, 0, false
}
