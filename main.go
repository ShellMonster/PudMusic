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

	// 「稍后再看」和「历史记录」这两页要的那几个接口，**跟着同一个客户端**。
	// 这里没有额外零件要装（它们不碰 CDN、不写盘），所以直接挂即可——全项目
	// 一个 B 站客户端这件事在这里露出来：换个地方建第二个，两页就会走第二份
	// 请求头和第二份 cookie jar。
	a.MountLibrary(cli)

	// 本地音乐也挂在开窗口之前：它不读盘，真正要等的是「退出时把那几摊
	// 文件监听关掉」，而那要走 App.Close（退出流程里跑的）。
	ui.MountLocalMusic(a)

	// 下载也挂在开窗口之前：它要读盘（恢复上次的队列）。取流零件
	// （`MountStreaming` 那个）可以晚一点挂，缺了也只是点了下载时给一句
	// 人话，而不会开窗就报错。
	if err := ui.MountDownloads(a); err != nil {
		log.Printf("biu: 挂下载失败，这次的下载功能用不了：%v", err)
	}

	// 系统媒体会话（浮窗 / 锁屏 / 任务栏缩略图按钮）也必须在开窗口之前挂：
	// macOS 那一半建的时候要注册 Objective-C 的类，而注册必须在还没有 ObjC
	// 回调在跑的时候做。Windows 那一半反过来要等窗口（它的会话是按窗口算的），
	// 所以这里拿到的是会话本身，绑窗口那一下交给 ui.Run。
	sess := ui.MountMedia(a)

	// 歌词缓存同样在开窗口之前挂好：建目录是阻塞 IO，放到第一次点歌时才发现，
	// 那一次点歌就要替「建目录」买单。
	ui.MountLyrics(a)

	if err := ui.Run(a, sess); err != nil {
		log.Fatal(err)
	}
}
