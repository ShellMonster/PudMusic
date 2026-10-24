// 「我收藏的」——用户收藏了**别人**的哪些收藏夹 / 合集 / 系列。
//
// # 接口
//
//	GET /x/v3/fav/folder/collected/list?up_mid=<mid>&ps=<n>&pn=<n>&platform=web
//
// 官方社区文档：`docs/fav/info.md` 的「查询用户收藏的视频收藏夹」那一节
// （bilibili-API-collect）。上游 Biu 用的是同一个接口
// （`src/service/fav-folder-collected-list.ts`）。
//
// # ⚠️ `platform=web` 不能省
//
// 官方文档的参数表里写着「填写 web 返回值才会包含用户收藏的视频合集」。
// 省了它的话，收藏夹（11）还在，而合集（21）**一整个不返回**——
// 侧栏上就少了一整类东西，而界面上看不出任何异样。
//
// # ⚠️ 返回里的 `type` 是官方文档漏写的那一格
//
// 官方文档的 `list[]` 字段表里**没有** `type`，但线上响应里有（11 收藏夹 /
// 21 合集 / 31 系列）。上游 Biu 直接按它分支（`collection/index.tsx` 的
// `item.type === CollectionType.Favorite`），而它是一个已发布的客户端——
// 字段不存在的话那段代码跑不起来。
//
// 所以这一层**认它，但认不出来时按收藏夹处理**（11）：那是「最多打不开」，
// 而「整条失败」会让用户以为自己收藏的不是收藏夹。
//
// # ⚠️ 失效的那几条要滤掉
//
// `state != 0` 是「这个收藏夹已经失效了」（主人删了它，但收藏关系还留着）。
// 摆出来的话用户点进去看到的是一个打不开的页面。
package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// pathFavCollected 是「我收藏的收藏夹」那个列表接口。
const pathFavCollected = "/x/v3/fav/folder/collected/list"

// CollectedFolderPageSize 是拉这个列表时一页几条。
//
// ⚠️ **70 是 B 站那边的天花板**（官方文档写「定义域 1 - 大于70」），
// 取 50 是照上游 Biu（`src/store/favorite.ts` 的 `pageSize = 50`）。
const CollectedFolderPageSize = 50

// CollectedFolder 是「我收藏的」列表里的一条。
type CollectedFolder struct {
	// ID 是这个容器的完整 id（收藏夹的 mlid / 合集的 season_id）。
	ID    int64
	Title string
	// Mid 是这个容器**属于谁**——收藏夹主人的 mid。
	//
	// ⚠️ **它是主人的 mid，不是收藏的人的**：跳 `/collection/{id}?mid=` 要的
	// 就是它，而「我收藏的」那一组里的收藏夹全是别人的。
	Mid int64
	// Type 是哪一类容器：收藏夹 11 / 合集 21 / 系列 31。
	Type int
	// MediaCount 是里面有几个内容。
	MediaCount int
}

// CollectedFolders 拉「我收藏的收藏夹」那一页，page 从 1 起。
//
// 返回的第二项是「后面还有没有」。
//
// ⚠️ **翻页的循环不在这里**（理由同 `folder.go` 的包注释）：这一层只发一次
// 请求、只回一页。翻页的循环在 `app` 层（见 `fetchCollectedFolders`）。
func (c *Client) CollectedFolders(ctx context.Context, upMid int64, page int) ([]CollectedFolder, bool, error) {
	if upMid <= 0 {
		return nil, false, fmt.Errorf("bili: 收藏夹主人 id 不对（%d）", upMid)
	}
	if page < 1 {
		page = 1
	}

	q := url.Values{
		"up_mid": {strconv.FormatInt(upMid, 10)},
		"pn":     {strconv.Itoa(page)},
		"ps":     {strconv.Itoa(CollectedFolderPageSize)},
		// ⚠️ **不能省**：省了的话 B 站不返回「用户收藏的视频合集」，
		// 侧栏上就少一整类（见文件头）。
		"platform": {"web"},
	}

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + pathFavCollected, Query: q})
	if err != nil {
		return nil, false, err
	}
	var d struct {
		Count int `json:"count"`
		List  []struct {
			ID         int64  `json:"id"`
			Title      string `json:"title"`
			Mid        int64  `json:"mid"`
			Type       int    `json:"type"`
			State      int    `json:"state"`
			MediaCount int    `json:"media_count"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, false, fmt.Errorf("bili: 收藏的收藏夹列表读不懂：%w", err)
	}

	out := make([]CollectedFolder, 0, len(d.List))
	for _, f := range d.List {
		// ⚠️ **state != 0 的是失效的**：主人把收藏夹删了，而这一条还留着。
		// 摆出来的话用户点进去看到的是一个打不开的页面。
		if f.State != 0 {
			continue
		}
		out = append(out, CollectedFolder{
			ID: f.ID, Title: f.Title, Mid: f.Mid,
			Type: collectedType(f.Type), MediaCount: f.MediaCount,
		})
	}
	// 「后面还有没有」按 **count** 算，不按这一页的条数算：这一页里可能有
	// 被滤掉的失效条目，而 B 站的 count 是**没滤之前**的总数。
	return out, d.Count > CollectedFolderPageSize*page, nil
}

// collectedType 把返回里的 type 认成我们认识的那几个。
//
// ⚠️ **认不出来时按收藏夹（11）处理**（见文件头）：官方文档没写这一格，
// 所以线上万一不发它，我们不能把整组都当成「认不出来」。
func collectedType(t int) int {
	switch CollectionType(t) {
	case CollectionFavorite, CollectionAlbum, CollectionSeries:
		return t
	}
	return int(CollectionFavorite)
}
