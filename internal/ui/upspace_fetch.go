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

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
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
			// 为了万一 B 站哪天给了重号时两页不会撞。
			ID:       "up:" + strconv.FormatInt(mid, 10) + ":" + v.Bvid,
			Title:    v.Title,
			Cover:    v.Cover,
			Duration: parseDurationText(v.Duration),
			// 播放量：列表那一列和网格卡片封面底部那一条渐变上画的都是它
			//（`Music.View`）。
			View: v.Play,
			// 投稿时间：这一页的接口把它放在 `created` 里（见 `UpVideo.Created`）。
			PubTime: v.Created,
			Item:    spaceVideoItem(v, mid),
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
// cursor 和 page 是**调用方（界面线程）**在开这一拉那一刻读出来的
// 闸门状态，不是这里自己读的——闸门是界面线程的状态，后台碰它就是数据竞争。
func (s *Shell) fetchUp(ctx context.Context, sp *upSpace, tab int, cursor string, page int) {
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

	r = upResult{tab: tab}

	switch tab {
	case upTabDynamic:
		pg, err := s.app.UpDynamics(ctx, sp.mid, cursor)
		r.err, r.dyns, r.hasMore, r.drops = err, pg.Items, pg.HasMore, pg.Drops
		r.cursor, r.sent = pg.Offset, pg.Raw
		r.dropWhy = pg.DropWhy
	case upTabCollection:
		// 收藏夹那个接口是**分页**的（pn/ps，默认 20；见 bili.SpaceFavFolders）。
		pg, err := s.app.UpCollections(ctx, sp.mid, page+1)
		r.err, r.cols, r.hasMore, r.sent = err, pg.Folders, pg.HasMore, len(pg.Folders)
	case upTabSeries:
		pg, err := s.app.UpSeries(ctx, sp.mid, page+1, upSeriesPageSize)
		r.err, r.cols, r.hasMore, r.sent = err, pg.Collections, pg.HasMore, len(pg.Collections)
	}

	// 🔴 **每一趟都留一句计数**：这一页 B 站发了几条、留下几条、丢掉几条、
	// 还有没有下一页。
	//
	// 「一直刷」这个症状要靠它才看得见：界面上那个转着的圈**分不出**是
	// 「同一页被反复要了」还是「真的在翻页」（票 #300）。而丢掉的理由更要
	// 留着——「整页一条都不剩」在界面上完全看不出来（只有一个空列表）。
	//
	// ⚠️ **丢掉的那几条要按类别分开写**（票 #344）：「形态不认识」和「读不懂」
	// 是两回事，合在一起就看不出是 B 站换了形态还是我们读不懂。
	if r.err == nil {
		log.Printf("biu: 拉 UP 空间的「%s」：B 站发了 %d 条，留下 %d 条，丢掉 %d 条（%s），还有下一页=%v",
			upTabNames[tab], r.sent, len(r.dyns)+len(r.cols), len(r.drops),
			dropCountText(dropCountsOf(r.drops)), r.hasMore)
		for _, why := range r.dropWhy {
			log.Printf("biu: 拉 UP 空间的「%s」丢掉一条：%s", upTabNames[tab], why)
		}
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
		// ⚠️ **`settle` 必须先调**：失败、超时、返回不对、拉取的代码崩了，
		// 全走这一条。漏了它那一格会永远停在「正在加载」。
		p.settle()
		if r.err != nil {
			p.err = r.err
			log.Printf("biu: UP 空间的「%s」拉失败了：%v", upTabNames[r.tab], r.err)
			continue
		}
		// 🔴 铁律四：追加是**尾部**（重查才是头部），尾部追加不动已有行的
		// 位置，所以「正在看的那一条」翻完页还是它。
		//
		// ⚠️ **重了的在这里丢掉**：B 站翻页会重叠（同一页的东西挪了名次就会在
		// 两页里各出现一次）。两个一模一样的行身份会让那几行共用一份状态
		// ——Tester 里当场 panic，真窗口里选中一个另一个也亮。
		if sp.seen == nil {
			sp.seen = map[string]bool{}
		}
		for _, d := range r.dyns {
			if sp.seen["dyn:"+d.ID] {
				continue
			}
			sp.seen["dyn:"+d.ID] = true
			sp.dynItems = append(sp.dynItems, d)
		}
		for _, col := range r.cols {
			k := fmt.Sprintf("coll:%d:%d", col.Type, col.ID)
			if sp.seen[k] {
				continue
			}
			sp.seen[k] = true
			sp.cols[r.tab] = append(sp.cols[r.tab], col)
		}
		// ⚠️ **丢掉的是累计的，不是这一页的**：翻了三页各丢 5 条就是 15 条，
		// 界面上那句话说的是「一共有多少条没显示」——说「刚才那 5 条」会让
		// 用户以为只有 5 条。
		//
		// ⚠️ **而且是按「不同的动态」累计的**（票 #344）：B 站翻页会重叠，
		// 同一批画不出来的动态在两页里各出现一次——按出现次数记的话，那个
		// 数会**偏大**（翻了三页就可能是同一个数的三倍）。
		sp.drops[r.tab].add(r.drops)
		p.adopt(r.cursor, r.sent, r.hasMore)
	}
}
