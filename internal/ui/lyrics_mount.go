// 歌词这一摊的挂载：缓存 + 两个搜索源。
//
// # 必须在开窗口之前调
//
// `lyrics.NewCache` 要 `MkdirAll`——那是阻塞 IO，放在开窗口那条路上就是每启动
// 一次窗口白等一次（见 shell_mount.go 里 `mountSearchHistory` / `mountShortcuts` 的同一条
// 理由）。它也不发任何请求，所以「早晚」无所谓，排在开窗口之前只是因为「所有零件
// 都齐了」这件事在那儿看得见一点。
//
// # 拿不到目录不算错误
//
// 和搜索历史、快捷键同一套规矩：记一条日志，接着用一份**不落盘**的缓存。
// `lyrics.NewCache("")` 什么都不做，于是这一次听的歌词照样看得见（如果调用方
// 给得出歌词文本），只是关掉之后不记得；而为了一个可有可无的目录挡住开窗口
// 不值当。
//
// # 缓存的键和内容
//
// 一首歌一个文件、文件名是键的哈希（`local-/Users/…/歌.mp3` 当文件名会造出一层
// 一层的目录），JSON 的键名照抄 Biu（`lyrics` / `tLyrics` / `offset` /
// `fontSize`）——都是 `service/lyrics/cache.go` 的事，这里只给一个目录。
//
// # 两个搜索源：凭据边界和 B 站接口共用一个
//
// 跟 `MountLogin` 是同一件事的两半：那一件决定「B 站 API 怎么调」，这一件决定
// 「歌词源怎么调」。两者共用**同一个**凭据边界（`app.CredentialTransport`），
// 因为两个歌词源本来就在那张白名单的 referer 档里（`app.DefaultInjectPolicy`）——
// **B 站的 Cookie 一个都不给它们**，规则写在代码里而不是只在心里。
//
// ⚠️ **共用 `newCredentialTransport()` 那一个网络层**（理由和 `MountStreaming` 一样：
// 同一个 jar、同一份请求头、同一张白名单）。另建一个 `http.Client` 意味着凭据这件
// 事有了第二个可能不一样的来源——凭据上「有两个来源」等于「有一个在偷偷生效」。
package ui

import (
	"log"
	"path/filepath"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/egoist/mygo"
)

// lyricsDirName 是歌词缓存放在应用数据目录底下的那一个文件夹。
const lyricsDirName = "lyrics"

// MountLyrics 把歌词缓存和那两个搜索源挂上。
//
// ⚠️ **两个源是不挂 jar 的**：`Client(nil)` 里的 nil 是 **cookie jar**，不是
// 「用默认的」。歌词源跟 B 站完全无关，拿到 SESSDATA 没有任何理由
// （`app.DefaultInjectPolicy` 把 `music.163.com` / `lrclib.net` 明确列在 referer 档，
// `RedirectClass` 也会把 Cookie / Authorization 全删掉——但那已经是「发了出去再删」，
// 不如一开始就一个都不给）。
//
// ⚠️ **拿不到应用数据目录的时候，两个源照挂**：缓存不落盘只是关掉之后不记得，而
// 那两个源压根不要目录。所以这里**不能 early return**——返回了就等于把「去找一份
// 歌词」这件事整个关掉了，而界面上那个空状态恰恰在劝用户去找。
func MountLyrics(a *app.App) {
	a.MountLyrics(lyrics.NewCache(lyricsCacheDir(appDataDir())))
	a.MountLyricSources(
		lyrics.NewNetease(newCredentialTransport().Client(nil)),
		lyrics.NewLRCLib(newCredentialTransport().Client(nil)),
	)
}

// appDataDir 是应用数据目录，拿不到时记一条日志并返回空串。
//
// 拿不到不算错误：和搜索历史、快捷键同一套规矩（见文件头「拿不到目录不算错误」）。
func appDataDir() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		// ⚠️ **这句话是给「所有挂在用户数据目录底下的东西」说的**（歌词缓存、
		// 评论缓存……），不是只给歌词：写死「歌词」的话，评论缓存拿不到目录时
		// 用户看到的是一句跟评论无关的话。
		log.Printf("biu: 拿不到应用数据目录，这次的歌词和评论关掉就没了：%v", err)
		return ""
	}
	return dir
}

// lyricsCacheDir 是歌词缓存落在哪儿。**空目录进、空目录出**。
//
// 🔴 **不能写成 `filepath.Join(dir, lyricsDirName)` 了事**：空目录时 `Join` 返回的是
// **相对路径** `"lyrics"`（不是空串！），而 `lyrics.NewCache` 只把**空串**当成
// 「不落盘」。于是「拿不到数据目录」这一种情况下，缓存会在**进程的当前工作目录**
// 里建出 `lyrics/` 并真的写盘——应用数据没拿到，却往用户碰巧启动它的那个目录里
// 写了一堆缓存文件。
//
// 所以这里显式判空：空就是空，`NewCache("")` 只读不写（见 `lyrics.Cache.Path`）。
func lyricsCacheDir(userDataDir string) string {
	if userDataDir == "" {
		return ""
	}
	return filepath.Join(userDataDir, lyricsDirName)
}
