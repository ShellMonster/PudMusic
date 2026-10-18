// Biu MyGo 是用 MyGo 框架重写的 Biu 音乐播放器。
//
// 界面是 MyGo 自己画的原生 UI（即时模式，没有 webview），音频在 Go 里解码。
// 当前阶段：`internal/ui/router.go` 登记的 14 条路由里**已经有 13 条接上了真数据**
// （首页推荐与新歌速递、稍后再看、历史记录、我的关注、收藏夹/合集/系列、UP 空间、
// 动态流、搜索、下载与下载记录、本地音乐、设置、扫码登录、迷你播放器），加上 404
// 那一页；剩下 `/empty`（空状态落地页，按设计该空）走的是这一页自己的空状态。
// 全局 Toast 与网络封面缓存都在，本地音频能出声。
//
// ⚠️ 那个 13 是**算出来的**，不是人记得的：`internal/ui/realdata_test.go` 的
// `TestTheCountsInTheProseMatchTheCountsInTheCode` 拿 `builtPage` 数一遍，再回头
// 核这三处散文（这里、README、router.go 的 `navigate`）——所以加一页却忘了改这三句
// 会在 `go test` 里当场红，而不是等下一个读它的人发现它在说假话。
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

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui"
)

func main() {
	a := app.New()
	// 数据升级与「从 Biu 首次导入」排在**所有挂载之前**。顺序反了的话，备份里
	// 存到的是「已经被挂载动过的」内容，回滚回不去；而且只读判断也会晚一步，
	// 一份来自更新版本的文件会先被挂载按旧语义改写一遍。
	//
	// 失败不挡启动：用户照常用这个应用，数据存不住的事界面上另有提示。
	if err := ui.MountMigration(); err != nil {
		log.Printf("biu: 升级数据失败，原文件都留着了：%v", err)
	}
	if err := ui.MountSettings(a); err != nil {
		log.Printf("biu: 拿不到应用数据目录，这次的设置存不住：%v", err)
	}
	// 全屏播放器自己那一份偏好（频谱开关、频谱颜色）。同样是读盘，所以同样排在
	// 开窗口之前；拿不到目录就用默认值继续，那一份的默认值就是「频谱关着」。
	ui.MountFullPlayerPrefs(a)
	// 登录也在开窗口之前挂好：读钥匙串是阻塞 IO，
	// 放到界面线程上读的表现是开窗时卡一下。
	cli := ui.MountLogin(a)
	// 在线取流跟着登录一起挂：它要用的**就是同一个** B 站客户端和同一张
	// 凭据白名单。这里只是把零件递过去，不发请求，所以早晚无所谓——但放在
	// 开窗口之前，「第一次点歌」就不会替「开窗」买单。
	ui.MountStreaming(a, cli)

	// 歌单内容页（收藏夹 / 合集 / 系列）用的是**同一个**客户端和同一张
	// 凭据白名单。它不发请求，所以挂在开窗口之前只是「零件都齐了」这件事
	// 看得见一点，不是怕它抢时间。
	ui.MountCollections(a, cli)

	// 下面这三处（在线取流、歌单内容页、UP 空间与动态流、稍后再看与历史记录）
	// 拿到的**是同一个客户端**，也就是 `MountLogin` 返回的那一个 —— `bili`
	// 全项目一个（见 `client.go` 的文件头）。它们都只是把那个客户端递给 app，
	// 不发请求、不建第二个。
	//
	// ⚠️ 谁在这里 `bili.NewClient(...)` 建第二个，登录态、请求头、WBI 密钥
	// 就都不是同一份了，而症状是「登录成功了，一打开空间就 -101」——
	// 那句话读起来像是「你没权限看这个人」，跟「客户端不是同一个」没有任何
	// 关系。

	// UP 空间与动态流：头图、关注、四个 Tab、动态流。
	a.MountExplore(cli)

	// 「稍后再看」和「历史记录」这两页要的那几个接口。它们不碰 CDN、不写盘，
	// 所以这里没有额外零件要装，直接挂即可。
	a.MountLibrary(cli)

	// 首页（推荐音乐）：三个 Tab 的推荐单 + 新歌速递。同一个客户端，同一张
	// 凭据白名单——首页不登录也能拉到公开推荐（登录之后是更合口味的那一批，
	// 见 `internal/app/recommend.go`）。
	a.MountRecommend(cli)

	// 本地音乐也挂在开窗口之前：它不读盘，真正要等的是「退出时把那几摊
	// 文件监听关掉」，而那要走 App.Close（退出流程里跑的）。
	ui.MountLocalMusic(a)

	// 歌词同理：缓存那半边要建目录（阻塞 IO），两个搜索源那半边不发请求。
	// 排在开窗口之前只是「所有零件都齐了」这件事在开窗口之前看得见一点。
	ui.MountLyrics(a)

	// 自动更新也挂在开窗口之前：`mygo.Updater.Enabled()` 要在应用旁边建一个
	// 临时目录试写，那是阻塞 IO，而它的结果（能不能更新）要在界面第一帧上就
	// 是对的。真正开始查是 `ui.Run` 的 WhenReady 里（见那里）。
	if err := ui.MountUpdates(a); err != nil {
		log.Printf("biu: 挂自动更新失败：%v", err)
	}

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

	if err := ui.Run(a, sess); err != nil {
		log.Fatal(err)
	}
}
