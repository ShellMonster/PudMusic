// 听歌统计在界面层的挂载。
//
// # 为什么单独一个文件
//
// 挂载这一摊有三件事：拿目录、挂到 App 上、退出前收尾。而它们和「设置」
// 那一摊的规矩**不一样**——设置挂不上要报错（用户改了设置存不住必须知道），
// 统计挂不上只是「这一份从空开始」，不该弹任何提示。

package ui

import (
	"log"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo"
)

// MountListeningStats 把听歌统计挂到盘上。
//
// ⚠️ **要在开窗口之前调**：读盘是阻塞 IO，而这一份要在**第一帧就有数**
// ——用户点开统计页不能看到「加载中」。
//
// ⚠️ **拿不到应用数据目录不算错误**：记一条日志，接着用空的那一份。统计是
// 「可有可无」的东西，为它挡住开窗口不值当（和 `mountSearchHistory` 一个
// 规矩）。
func MountListeningStats(a *app.App) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("pudmusic: 拿不到应用数据目录，这次的听歌统计关掉就没了：%v", err)
		return
	}
	if err := a.MountListeningStats(app.StatsPath(dir)); err != nil {
		log.Printf("pudmusic: 挂听歌统计失败：%v", err)
	}
}
