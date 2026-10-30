package app

import (
	"context"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// folderCoverCache 记着「封面失效的收藏夹，改用哪首歌的封面」。
// 每个收藏夹只拉一次第一页；拉完（不管有没有找到）都记下，不重复请求。
//
// ⚠️ **键是 `CollectionRef.Key()`，不是 id**：同一个 id 在收藏夹（11）/
// 合集（21）/ 系列（31）下是三个不同的容器。只按 id 记的话，用户先打开收藏夹、
// 再打开 id 相同的合集，第二边拿到的会是**第一边那个封面**——症状是「合集头图
// 显示的是收藏夹里那首歌的封面」，而离真因隔着一整套缓存。
type folderCoverCache struct {
	mu   sync.Mutex
	done map[string]string
	busy map[string]bool
}

// FolderFallbackCover 返回这个容器里第一个有效且有封面的内容的封面；
// 还没拉完或没有就返回空串。第一次调用会在后台拉第一页，拉完叫醒界面。
//
// # 为什么要它
//
// 接口给的那个封面是**容器的第一首**（B 站那边就是这么算的）。第一首失效之后
// 那个地址往往还在、图已经 404 了——界面上那一格于是永远是一张坏图。兜底改成
// 「夹里第一首还在、还有封面的」那张，用户的观感是「封面对了」，而不是
// 「这个收藏夹没有封面」。
//
// # ref 而不是 Favorite
//
// 页面头图区手上只有 `CollectionRef`（`p.ref`），侧栏手上是 `Favorite`——
// 两边都要走同一套换算（`CollectionRefOf`），否则「类型认不出时按收藏夹处理」
// 那条兜底会在其中一边漏掉。
//
// ⚠️ **失败时不写 `done`**：拉第一页那一趟失败了，下次调用还会再试——网络抖动
// 是暂时的，永久记死会让「网络早就好了，封面却一直出不来」。而**拉成功但没有
// 可用封面**（空结果）要记下，不然每一帧都会重拉。
func (a *App) FolderFallbackCover(ref CollectionRef) string {
	if ref.ID <= 0 {
		return ""
	}
	fc := &a.folderCovers
	key := ref.Key()
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if v, ok := fc.done[key]; ok {
		return v
	}
	if fc.busy[key] || a.collectionsSnapshot() == nil {
		return ""
	}
	if fc.busy == nil {
		fc.busy, fc.done = map[string]bool{}, map[string]string{}
	}
	fc.busy[key] = true
	go func() {
		medias, _, err := a.LoadCollectionPage(context.Background(), ref, bili.SortByFavTime, "", 1)
		cover := ""
		for _, m := range medias {
			// ⚠️ **三个条件缺一不可**：`Playable` 是有没有音频流（合集 / 电影
			// 没有），`Attr == 0` 是还在不在（失效条目常常还挂着一个 404 的
			// 封面地址），`Cover != ""` 是它有没有封面可给。
			if m.Playable() && m.Attr == 0 && m.Cover != "" {
				cover = m.Cover
				break
			}
		}
		fc.mu.Lock()
		delete(fc.busy, key)
		if err == nil {
			fc.done[key] = cover
		}
		fc.mu.Unlock()
		a.wakeUI()
	}()
	return ""
}
