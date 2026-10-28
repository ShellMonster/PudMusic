package app

import (
	"context"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// folderCoverCache 记着「封面失效的收藏夹，改用哪首歌的封面」。
// 每个收藏夹只拉一次第一页；拉完（不管有没有找到）都记下，不重复请求。
type folderCoverCache struct {
	mu   sync.Mutex
	done map[int64]string
	busy map[int64]bool
}

// FolderFallbackCover 返回收藏夹里第一个有效且有封面的内容的封面；
// 还没拉完或没有就返回空串。第一次调用会在后台拉第一页，拉完叫醒界面。
func (a *App) FolderFallbackCover(f Favorite) string {
	fc := &a.folderCovers
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if v, ok := fc.done[f.ID]; ok {
		return v
	}
	if fc.busy[f.ID] || a.collectionsSnapshot() == nil {
		return ""
	}
	if fc.busy == nil {
		fc.busy, fc.done = map[int64]bool{}, map[int64]string{}
	}
	fc.busy[f.ID] = true
	go func() {
		medias, _, err := a.LoadCollectionPage(context.Background(), folderRef(f), bili.SortByFavTime, "", 1)
		cover := ""
		for _, m := range medias {
			if m.Playable() && m.Attr == 0 && m.Cover != "" {
				cover = m.Cover
				break
			}
		}
		fc.mu.Lock()
		delete(fc.busy, f.ID)
		if err == nil {
			fc.done[f.ID] = cover
		}
		fc.mu.Unlock()
		a.wakeUI()
	}()
	return ""
}
