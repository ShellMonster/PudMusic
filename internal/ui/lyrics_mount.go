// 歌词缓存的挂载：按应用数据目录建出缓存目录，递给应用状态。
//
// # 必须在开窗口之前调
//
// `lyrics.NewCache` 要 `MkdirAll`——那是阻塞 IO，放在开窗口那条路上就是每启动
// 一次窗口白等一次（见 shell.go 里 `mountSearchHistory` / `mountShortcuts` 的同一条
// 理由）。它也不发任何请求，所以「早晚」无所谓，排在哪儿都行，排在开窗口之前
// 只是因为「所有零件都齐了」这件事在那儿看得见一点。
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

// MountLyrics 把歌词缓存挂上。
func MountLyrics(a *app.App) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，这次的歌词关掉就没了：%v", err)
		return
	}
	a.MountLyrics(lyrics.NewCache(filepath.Join(dir, lyricsDirName)))
}
