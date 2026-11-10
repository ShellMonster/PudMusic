// 布丁音乐是用 MyGo 框架重写的 Biu 音乐播放器。
//
// 界面是 MyGo 自己画的原生 UI（即时模式，没有 webview），音频在 Go 里解码。
// 当前阶段：`internal/ui/router.go` 登记的 14 条路由里**已经有 13 条接上了真数据**
// （首页推荐与新歌速递、稍后再看、历史记录、我的关注、收藏夹/合集/系列、UP 空间、
// 动态流、搜索、下载与下载记录、本地音乐、设置、扫码登录、听歌统计），
// 加上 404
// 那一页；剩下 `/empty`（空状态落地页，按设计该空）走的是这一页自己的空状态。
// 全局 Toast 与网络封面缓存都在，本地音频能出声。
//
// ⚠️ 那个 14 是**算出来的**，不是人记得的：`internal/ui/realdata_test.go` 的
// `TestTheCountsInTheProseMatchTheCountsInTheCode` 拿 `builtPage` 数一遍，再回头
// 核这三处散文（这里、README、router.go 的 `navigate`）——所以加一页却忘了改这三句
// 会在 `go test` 里当场红，而不是等下一个读它的人发现它在说假话。
//
// ⚠️ **票 #706 把这个数从 14 减到了 13，路由从 15 减到了 14**：评论从**独立
// 路由页**改成了首页那一档旁边的**右侧抽屉**——而一个「不是页」的东西不进
// `routes` 那张表。
//
// ⚠️ 「哪一页接上了」不是靠人记得的：`internal/ui/realdata_test.go` 逐条钉着，
// 而它自己也被 `internal/evidence` 那张台账列成了一条证据。
//
// 窗口怎么开、标题栏什么样、1200×800 这些，都在界面层的 ui.Run 里 ——
// 那才是窗口骨架该待的地方。设置挂在哪儿、退出时怎么存，在 ui.MountSettings 里。
//
// ⚠️ **窗口有两扇**，别把「单实例」读成「一个窗口」：除了主窗口还有一扇 320×100
// 的迷你播放器（`internal/ui/miniplayer.go`），两个窗口互斥、共用同一个 `app.App`。
// 所以「后台改了状态要挨着唤醒界面」那条在应用层是**挨着每一扇窗**唤醒
// （`app.AddWake`），不是叫醒一个。
package main

import (
	"log"
	"os"
	"runtime/debug"

	"github.com/ShellMonster/PudMusic/internal/bootstrap"
	"github.com/ShellMonster/PudMusic/internal/platform"
)

func main() {
	tuneMemory()
	// 日志先接上：接不上不挡启动，后续日志退回 stderr。
	if _, err := platform.OpenLogFile(); err != nil {
		log.Printf("pudmusic: 接不上日志文件，这一次的日志只在 stderr 上：%v", err)
	}
	if _, err := platform.OpenCrashLog(); err != nil {
		log.Printf("pudmusic: 接不上崩溃日志，崩溃堆栈只在 stderr 上：%v", err)
	}

	if _, err := bootstrap.Assemble(bootstrap.DefaultDependencies()); err != nil {
		log.Fatal(err)
	}
}

// tuneMemory 收紧 Go 运行时的内存策略：播放器常驻后台，堆里大头是按显示
// 尺寸缓存的封面位图（`internal/ui/widgets/imgcache`，默认预算 32 MiB）加
// 解码缓冲。默认 GOGC=100 会让垃圾堆到和存活数据一样多才回收，GOGC 降到 50
// 让峰值堆更低；软上限 256MB 让接近上限时回收更积极（它是软的，超了也不会
// 崩，只是回收更勤）。用户自己设了环境变量就尊重用户的。
//
// ⚠️ **这里不再每分钟 `debug.FreeOSMemory()`**：它只把 Go 堆的空闲页还给
// 系统（实测 footprint 只降 2~3MB），而进程的大头是 Metal 渲染器的 GPU
// 工作集（`owned unmapped (graphics)`，见 `internal/ui/notch.go` 的
// `notchPollAgain`），它碰不到；反过来，每分钟一次强制 GC 在播放器这种
// 长驻进程里只会制造周期性的停顿。要压内存，该减少的是重绘和常驻位图，
// 不是更勤地 GC。
func tuneMemory() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(256 << 20)
	}
}
