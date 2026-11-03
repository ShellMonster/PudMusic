//go:build unix

package audio

// 这一组盯「**等文件头**那一下是阻塞的，不是空转」（票 #478 的评审）。
//
// # 这一条钉的是什么
//
// 盒子头不一定在前 512 字节里——一个大的 `free`/`sidx` 盒子排在
// `mdat`/`moov` 前面时，`mdat` 的头在好几兆之外。而那时候源要**等**
// 顺序下载追上那个偏移。
//
// ⚠️ **写成「空转重试」的话，那个 goroutine 会把一个 CPU 核占满**，
// 直到下载追上——而那段时间里源唯一在做的事就是「等字节」
// （2026-10-25 的评审实测：墙钟 6.148 秒、进程 CPU 5.848 秒，≈0.95 个核）。
//
// # 判据是**这个进程用的 CPU 秒**
//
// 墙钟是「等了多久」（那本来就该等），CPU 才是「有没有在空转」。
//
// ⚠️ **为什么单独一个文件、还带 `unix` 标签**：CPU 时间要用
// `getrusage` 量，而它只在 Unix 上有（Windows 上 `syscall.Getrusage`
// 不存在，而本仓库不新增依赖）。功能的那一半由
// `stream_source_tail_test.go` 里的
// `TestTheTailIsFetchedEvenWhenTheBoxHeaderIsFarAway` 钉着——**它到处都跑**。

import (
	"net/http/httptest"
	"syscall"
	"testing"
	"time"
)

// cpuSeconds 报这个进程一共用了多少 CPU 秒（用户态 + 内核态）。
//
// ⚠️ **它是「这个进程」的**，不是整台机器的——所以别的 agent 在编译、
// 别的测试在跑，都不影响它。这正是这一条要的那种判据：**空转会烧掉本
// 进程的一个核，而「阻塞等」一点都不烧**。
//
// ⚠️ **不要用 `runtime/metrics` 的 `/cpu/classes/total:cpu-seconds`**：
// 那个指标在 macOS 上**不更新**（实测：睡 1.5 秒它涨 0.000 秒，空转
// 1.5 秒它也涨 0.000 秒——它要等运行时下一次刷新才更新，而那一刻
// 读到的差值是「从上次刷新到现在」，不是「从我这一读到那一读」）。
func cpuSeconds() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6 +
		float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
}

// TestTheTailWaitBlocksInsteadOfSpinning 盯「等文件头」那一下是**阻塞**
// 而不是**空转**。
func TestTheTailWaitBlocksInsteadOfSpinning(t *testing.T) {
	// 一份「`free` 盒子很大、`mdat` 的头在很后面」的素材。
	rest := concat(
		box("ftyp", make([]byte, 20)),
		box("free", make([]byte, 512<<10)),
		box("mdat", make([]byte, 64<<10)),
		box("moov", make([]byte, 100)),
	)
	// 顺序下载跑 200 KB/s → 追上 `mdat` 的头要 2.6 秒。
	srv := httptest.NewServer(slowRangeCDN(rest, 200_000))
	defer srv.Close()

	src, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{srv.URL},
		Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	// 等它开始走盒子（文件头到了就够了）。
	deadline := time.Now().Add(20 * time.Second)
	for src.Filled() < mp4HeadMin {
		if time.Now().After(deadline) {
			t.Fatal("文件头一直没下到")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 给它一点时间去发现「盒子头还在很后面」。
	time.Sleep(200 * time.Millisecond)

	// 🔴 **量这一段**：这 1.5 秒里源唯一在做的事就是「等字节」。
	//
	// ⚠️ **判据是「CPU 占比」，不是总 CPU 秒**。总 CPU 秒在慢机器上会
	// 整个抬高（CI 上阻塞态量到过 0.33s、压线 0.3s 阈值；2026-11-03
	// 的 CI 抖动正是这一条红的）；**占比**不一样——它是「这 1.5 秒里
	// 本进程吃了多少核」，慢机器把分子分母一起抬，比值不变：
	//
	//	阻塞等（修好之后）	≈0.006（0.007s / 1.5s，≈0）
	//	空转（改坏之后）	≈0.43（0.64s / 1.5s，接近半个核）
	//	CI 抖动基线		≈0.22（0.33s / 1.5s——比阻塞高，但仍是「大部
	//				分时间在等」的形状，不是「一直在烧」的形状）
	//
	// 阈值 0.35 离「空转」的 0.43 差 0.08、离「CI 抖动」的 0.22 差
	// 0.13——慢机器把整个进程 CPU 抬高时，空转仍是「一直在烧」（接近 1），
	// 阻塞+抖动仍是「大部分时间在等」（到不了 0.35）。
	const maxCPUFraction = 0.35
	cpu0 := cpuSeconds()
	const wait = 1500 * time.Millisecond
	time.Sleep(wait)
	used := cpuSeconds() - cpu0
	frac := used / wait.Seconds()
	t.Logf("等文件头的那 1.5 秒用了 %.3f 秒 CPU（占比 %.2f）", used, frac)

	if frac > maxCPUFraction {
		t.Errorf("等文件头的那 1.5 秒占了 %.0f%% 的 CPU——那是**空转**，"+
			"不是阻塞等（空转会占满一个核）", frac*100)
	}
}
