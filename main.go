// Biu MyGo 是用 MyGo 框架重写的 Biu 音乐播放器。
//
// 界面是 MyGo 自己画的原生 UI（即时模式，没有 webview），音频在 Go 里解码。
// 当前阶段：14 条路由各有各的页面（空状态 + 404 兜底）+ 全局 Toast + 网络封面
// 缓存，本地音频能出声。
//
// 窗口怎么开、标题栏什么样、1200×800 这些，都在界面层的 ui.Run 里 ——
// 那才是窗口骨架该待的地方。设置挂在哪儿、退出时怎么存，在 ui.MountSettings 里。
package main

import (
	"log"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui"
)

func main() {
	a := app.New()
	if err := ui.MountSettings(a); err != nil {
		log.Printf("biu: 拿不到应用数据目录，这次的设置存不住：%v", err)
	}
	// 登录也在开窗口之前挂好：读钥匙串是阻塞 IO，
	// 放到界面线程上读的表现是开窗时卡一下。
	cli := ui.MountLogin(a)

	// 在线取流跟着登录一起挂：它要用的**就是同一个** B 站客户端和同一张
	// 凭据白名单。这里只是把零件递过去，不发请求，所以早晚无所谓——但放在
	// 开窗口之前，「第一次点歌」就不会替「开窗」买单。
	ui.MountStreaming(a, cli)

	// 系统媒体会话（控制中心 / 锁屏）也必须在开窗口之前：它建的时候要注册
	// Objective-C 的类，而注册必须在还没有 ObjC 回调在跑的时候做。
	ui.MountMedia(a)

	if err := ui.Run(a); err != nil {
		log.Fatal(err)
	}
}
