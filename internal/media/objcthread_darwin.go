//go:build darwin

package media

// **整个进程里碰 Objective-C 的只有一条 OS 线程**，别的线程一个消息都不发。
//
// # 这不是修复，是加固
//
// 真正让这一层从「每次必崩」变成干净的是**不用 Objective-C 的自动释放池**
// （见 `objc_darwin.go`「Foundation 类型」顶上那段）。把 ObjC 活钉在一条线程上
// 是**顺手做的第二道防线**，不是治根的那一下——实测只钉线程、池子照用，一样崩。
//
// 那为什么还要它：
//
// - Objective-C 有一堆**每线程**的状态。Go 的 goroutine 随时会换 OS 线程，
//   「上一个线程留下的东西」和「这一个线程干的活」共用一套本该分开的账。
//   池子只是其中最容易要命的一个，别的还有。
// - 这一层要造 `NSImage`。AppKit 的对象本来就不该在随手一个线程上造。
// - 系统的媒体会话本来就有一条自己的线程，这是那个形状。
//
// # 🔴 这里不能重入
//
// `onObjCThread` 是**排队 + 同步等**。在那条线程上再排一个活儿，就是等一个正
// 在等它自己的东西——死等，界面上表现为「按了一下播放，整个应用没反应」。
//
// 所以这一层用**函数名**把这件事挡住，不用运行时的标志位（标志位要多线程读写，
// 会自己长出竞态来——`-race` 一跑就红）：
//
//	apply            —— 给外面调用的，排队
//	applyOnObjCThread —— **只能**在那条线程上跑
//
// 排进去的活儿里要接着干，就调那个 `…OnObjCThread` 的，别绕回 `apply`。
// 这是这一层唯一的规矩，改之前先看一眼这里。

import (
	"runtime"
	"sync"
)

var (
	objcOnce sync.Once
	objcJobs chan func()
)

// onObjCThread 把 fn 放到那条钉住的 OS 线程上跑，**等它跑完再回来**。
//
// 同步等是有意的：这一层所有 ObjC 动作都很短（拼一个字典、发两条消息），
// 等一下比引入异步状态机便宜得多。调用方是 App 的后台时钟和远程命令的工作
// 协程，两边都等得起。
//
// ⚠️ **不能从那条线程上重入**（见文件顶上）。
func onObjCThread(fn func()) {
	objcStart()
	done := make(chan struct{})
	objcJobs <- func() {
		defer close(done)
		fn()
	}
	<-done
}

// objcStart 拉起那条线程（幂等）。
func objcStart() {
	objcOnce.Do(func() {
		// 队列给 64：一次 `onObjCThread` 最多同时排一个（调用方是同步等的），
		// 真正会排满的是「App 状态一变就推」和「封面下好了补推」撞在一起。
		objcJobs = make(chan func(), 64)
		go func() {
			// ⚠️ 这一句是这一层存在的全部理由：不锁，goroutine 会换线程，
			// 那这一层等于白做。
			runtime.LockOSThread()
			for job := range objcJobs {
				job()
			}
		}()
	})
}
