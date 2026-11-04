package ui

import (
	"log"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo"
)

// MountRecommend 把首页要的接口挂到 App 上，并挂上热榜名次的本地记录（算榜单的升降）。
//
// ⚠️ **要在开窗口之前调**。拿不到应用数据目录不算错误：榜单只是没有升降，照常显示。
func MountRecommend(a *app.App, cli *bili.Client) {
	a.MountRecommend(cli)
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("pudmusic: 拿不到应用数据目录，榜单的升降这次记不住：%v", err)
		return
	}
	a.RecommendOf().SetChartHistoryPath(app.ChartHistoryPath(dir))
}
