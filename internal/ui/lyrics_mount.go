// 把歌词缓存挂到 App 上。
//
// 跟 `MountStreaming` 是同一个形状的活儿：那一件决定「歌词从哪儿来」（#21 的
// 活儿，这一票没有），这一件决定「缓存落在盘上的哪儿」。
//
// 和 `MountSettings` 一样**必须在开窗口之前调**：建目录是阻塞 IO，而歌词缓存的
// 位置要用 `mygo.App.Path(mygo.PathUserData)`——「应用数据目录在哪」是框架的
// 事，别自己拼 `~/Library/Application Support/…`（理由见 settings.go 的文件头）。
package ui

import (
	"log"
	"path/filepath"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/egoist/mygo"
)

// lyricsDirName 是歌词缓存在应用数据目录下的子目录名。
const lyricsDirName = "lyrics"

// MountLyrics 把歌词缓存挂到 a 上。**必须在开窗口之前调**。
//
// 拿不到应用数据目录不算错误——记一条日志，接着挂一个不落盘的缓存：歌词照样
// 看得见，只是调好的偏移和字号关掉应用就没了。为一件可有可无的东西挡住开窗口
// 不值当。
func MountLyrics(a *app.App) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，歌词缓存这次关掉就没了：%v", err)
		a.MountLyrics(lyrics.NewCache(""))
		return
	}
	a.MountLyrics(lyrics.NewCache(filepath.Join(dir, lyricsDirName)))
}
