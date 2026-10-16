// UP 空间那几张 Tab 的取数：后台拉、投信箱、界面线程收。
//
// # 三条「panic 不能杀进程」里，这一层占两条
//
//   - **界面线程不碰网络**：三个网络往返跑在后台 goroutine 里；
//   - **后台那一条自己 recover**：界面那一层的 recover（行级的、页级的、
//     页级的 `Shell.page`）**一个都接不到它**——Go 里没人在跑那个
//     goroutine。而这里正是「解析接口返回」的地方，B 站改一个字段就在
//     这一行炸。崩了当成一个错投进信箱，走正常那条路。
//
// # 投递完必须叫醒界面
//
// 只投递不唤醒 = 界面永远停在旧帧（实测 0 帧刷新）。
package ui

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// ─────────────────────────────────────────────────────────────────────────────
// 取数
// ─────────────────────────────────────────────────────────────────────────────

// fetchVideos 是「投稿」Tab 的拉页器。**在后台 goroutine 里跑**
// （`list.PagedList.load`），所以里面不许碰界面状态。
func (s *Shell) fetchVideos(ctx context.Context, mid int64, page int) (list.Page, error) {
	pg, err := s.app.UpVideos(ctx, bili.UpVideoQuery{
		Mid: mid, Page: page, Size: bili.UpVideoDefaultSize,
	})
	if err != nil {
		return list.Page{}, err
	}
	songs := make([]list.Music, 0, len(pg.Videos))
	for _, v := range pg.Videos {
		songs = append(songs, list.Music{
			// 🔴 铁律四：行的身份带上 mid。两个 UP 各自的投稿 Tab 是两个
			// PagedList（各有一份行），bvid 全局唯一本来够用——带上 mid 是
			// 为了万�� B 站哪天给了重号时两页不会撞。
			ID:       "up:" + strconv.FormatInt(mid, 10) + ":" + v.Bvid,
			Title:    v.Title,
			Cover:    v.Cover,
			Duration: parseDurationText(v.Duration),
			Item:     spaceVideoItem(v),
		})
	}
	return list.Page{Songs: songs, HasMore: pg.HasMore}, nil
}

// fetchUp 是「动态 / 收藏夹 / 合集」这三张 Tab 的拉取。**在后台 goroutine
// 里跑**。
//
// ⚠️ **自己 recover**：界面那一层的 recover（行级的、页级的）一个都接不到
// 它——Go 里没人在跑那个 goroutine，而这一段正是「解析接口返回」的地方，
// B 站改一个字段就是在这一行炸。崩了当成一个错投进信箱，走正常那条路。
func (s *Shell) fetchUp(ctx context.Context, sp *upSpace, tab int, gen uint64) {
	var r upResult
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("biu: 拉 UP 空间的「%s」崩了：%v\n%s", upTabNames[tab], rec, debug.Stack())
			r.err = fmt.Errorf("拉 %s 的代码崩了：%v", upTabNames[tab], rec)
		}
		// 🔴 铁律三：投递完**必须**唤醒。只投递不唤醒 = 界面停在旧帧
		// （实测 0 帧刷新）。
		sp.boxMu.Lock()
		sp.box = append(sp.box, r)
		sp.boxMu.Unlock()
		s.wake.invalidate()
	}()

	r = upResult{gen: gen, tab: tab}
	p := sp.pagerOf(tab)

	switch tab {
	case upTabDynamic:
		pg, err := s.app.UpDynamics(ctx, sp.mid, p.cursor)
		r.err, r.dyns, r.hasMore, r.dropped = err, pg.Items, pg.HasMore, pg.Dropped
		r.cursor = pg.Offset
	case upTabCollection:
		// 收藏夹那个接口**一次给全，没有翻页参数**（见 bili.SpaceFavFolders），
		// 所以它只有第一页：拉过一次就到底。
		cols, err := s.app.UpCollections(ctx, sp.mid)
		r.err, r.cols, r.hasMore = err, cols, false
	case upTabSeries:
		pg, err := s.app.UpSeries(ctx, sp.mid, p.page+1, upSeriesPageSize)
		r.err, r.cols, r.hasMore = err, pg.Collections, pg.HasMore
	}
}

// adoptUp 收走后台投回来的结果。**只能由界面线程调**（view 开头那一次）。
func (s *Shell) adoptUp(sp *upSpace) {
	sp.boxMu.Lock()
	box := sp.box
	sp.box = nil
	sp.boxMu.Unlock()
	if len(box) == 0 {
		return
	}

	for _, r := range box {
		p := sp.pagerOf(r.tab)
		// ⚠️ **`settle` 必须先调**：代次对不上也是「这一拉结束了」，
		// 不解开闸门的话那一格会永远停在「正在加载」。
		if !p.settle(r.gen) {
			continue // 换 Tab 之前的那个请求。它说什么都不作数。
		}
		if r.err != nil {
			p.err = r.err
			log.Printf("biu: UP 空间的「%s」拉失败了：%v", upTabNames[r.tab], r.err)
			continue
		}
		// 🔴 铁律四：追加是**尾部**（重查才是头部），尾部追加不动已有行的
		// 位置，所以「正在看的那一条」翻完页还是它。
		sp.dynItems = append(sp.dynItems, r.dyns...)
		sp.cols[r.tab] = append(sp.cols[r.tab], r.cols...)
		sp.dropped[r.tab] = r.dropped
		p.adopt(r.cursor, upRowCount(sp, r.tab), r.hasMore)
	}
}
