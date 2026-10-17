// 收藏：一首收进用户的收藏夹。
//
// # 三个接口
//
//	/x/web-interface/view?bvid=<bvid>              bvid → aid
//	/x/v3/fav/folder/created/list-all?up_mid=<mid>  「我创建的」收藏夹
//	/x/v3/fav/resource/ids?type=&rid=               这一首现在在哪些格子里（去重用）
//	/x/v3/fav/resource/deal                         往某几格放 / 从某几格拿（POST + CSRF）
//
// # 为什么 rid 是 aid 而我们手里是 bvid
//
// B 站的收藏接口一律收 **avid**（上游 `fav-folder-deal.ts` 的 `rid: 视频id`，它那边
// 传的就是 avid）。我们这一层存的是 bvid（`Item.Bvid`，取流、搜索、分享都用它），
// 所以收藏之前要换一次——**换的动作在服务层做完**：界面上不该知道「收藏要的是另一
// 个 id」，更不该自己拿 bvid 去拼。
//
// # 「放哪一格」这一层决定
//
// Biu 那边弹一个选择框让用户挑（`components/favorites-select-modal`）。我们这里没
// 那个框，所以规则写在这里：**取「我创建的」里的第一格**。B 站按创建顺序返回，而
// 默认收藏夹总是最早建的那一格，所以第一格就是它——这是本项目现在能给出的最像话
// 的选择；等收藏夹管理（BMG-32）把选择框接上，这里换成「界面传 folderID 进来」。
package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

const (
	// pathFavFolders 是「我创建的」收藏夹。B 站要 up_mid，问的是「这个 mid 建了哪些
	// 收藏夹」。
	pathFavFolders = "/x/v3/fav/folder/created/list-all"
	// pathFavMediaIDs 问「这一首现在在哪些格子里」。收藏之前问一遍，已经在里面了就
	// 不再写一次（B 站的 deal 重复放是幂等的，但「已经收藏过了」和「刚收藏好」对
	// 用户是两句话）。
	pathFavMediaIDs = "/x/v3/fav/resource/ids"
	// pathFavDeal 是写接口：往 add_media_ids 那些格子里放、从 del_media_ids 那些
	// 格子里拿。**它是 POST，要 CSRF**。
	pathFavDeal = "/x/v3/fav/resource/deal"
	// pathView 是稿件详情：bvid → aid。上游取分 P 列表用的是 `/x/player/pagelist`
	// （`video.go`），那一个不返回 aid，所以这里另问一次详情。
	pathView = "/x/web-interface/view"
)

// 收藏接口的 type 参数（B 站的约定，Biu 那边也是这两个数）。
const (
	// FavTypeVideo 是投稿（`item.type == "mv"`）。
	FavTypeVideo = 2
	// FavTypeAudio 是音乐库里的歌（收藏夹里的「音频」）。
	FavTypeAudio = 12
)

// 收藏这一摊自己的哨兵。**没登录那个不在这儿**：`ErrNotLoggedIn` 是包里共用的
// （`client.go`，-101），收藏是写操作但不需要另造一个同义的——`Friendly` 那边
// 已经有一张表把它们说成同一句话（「重新扫码登录一下」），两处各造一个只会让那句话
// 说两遍。
var (
	// ErrNoFavoriteFolder 是「这个账号一个收藏夹都没有」：没有格子可放。
	ErrNoFavoriteFolder = errors.New("bili: 这个账号还没有收藏夹")

	// ErrAlreadyFavorited 是「这一首已经在那一格里了」。单独一个哨兵是因为它和
	// 「网络断了」要分开说：用户看到「已经收藏过了」知道刚才那一下不是白按的，
	// 看到「连不上 B 站」则不知道该不该再试一次。
	ErrAlreadyFavorited = errors.New("bili: 这一首已经收藏过了")
)

// FavFolder 是「我创建的」收藏夹里的一格。
type FavFolder struct {
	ID    int64
	Title string
	// MediaCount 是里面已有多少条。**它是 B 站上次统计的数字**，不是实时的——
	// 这里只用来在报告里说个大概，判断「有没有这一首」不靠它（靠
	// `FavMediaFolders`）。
	MediaCount int
}

// FavTarget 是要收藏的那一项。
//
// Type 是「mv」或「audio」，和 `Item.Type` 一个说法；B 站那边收的是数字，那份
// 换算在服务层（见 favTypeOf）。
type FavTarget struct {
	Bvid string // mv
	Cid  string
	Sid  string // audio
	Type string // "mv" / "audio"

	// FolderID 是放进哪一格。**0 = 由这一层挑**（第一格，见文件头）。
	FolderID int64
}

// FavFolders 列「这个 mid 创建的收藏夹」，顺序照 B 站给的（创建顺序）。
//
// 一格都没有时是 `ErrNoFavoriteFolder`——那是「用户还没建过收藏夹」，不是错误。
func (c *Client) FavFolders(ctx context.Context, upMid int64) ([]FavFolder, error) {
	if upMid <= 0 {
		return nil, ErrNotLoggedIn
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + pathFavFolders,
		Query:  url.Values{"up_mid": {strconv.FormatInt(upMid, 10)}},
	})
	if err != nil {
		return nil, err
	}
	var d struct {
		List []struct {
			ID         int64  `json:"id"`
			Title      string `json:"title"`
			MediaCount int    `json:"media_count"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 收藏夹列表读不懂: %w", err)
	}
	out := make([]FavFolder, 0, len(d.List))
	for _, f := range d.List {
		out = append(out, FavFolder{ID: f.ID, Title: f.Title, MediaCount: f.MediaCount})
	}
	if len(out) == 0 {
		return nil, ErrNoFavoriteFolder
	}
	return out, nil
}

// ArchiveID 查一个稿件的 avid（收藏接口要的是它，见文件头）。
func (c *Client) ArchiveID(ctx context.Context, bvid string) (int64, error) {
	if bvid == "" {
		return 0, ErrNoVideo
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + pathView,
		Query:  url.Values{"bvid": {bvid}},
	})
	if err != nil {
		return 0, err
	}
	var d struct {
		Aid int64 `json:"aid"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return 0, fmt.Errorf("bili: 稿件详情读不懂: %w", err)
	}
	if d.Aid <= 0 {
		return 0, ErrNoVideo
	}
	return d.Aid, nil
}

// FavMediaFolders 问「这一首现在在哪些收藏夹里」，回收藏夹 id。
//
// 收藏之前用它判重复：B 站的 deal 重复放是幂等的（不会多出一条），但对用户来说
// 「已经收藏过了」和「刚收藏好」是两句话——前者说明刚才那一下没白按。
func (c *Client) FavMediaFolders(ctx context.Context, rid string, typ int) ([]int64, error) {
	if rid == "" {
		return nil, nil
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + pathFavMediaIDs,
		Query: url.Values{
			"type": {strconv.Itoa(typ)},
			"rid":  {rid},
		},
	})
	if err != nil {
		return nil, err
	}
	var d struct {
		Ids []int64 `json:"ids"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 收藏夹 id 列表读不懂: %w", err)
	}
	return d.Ids, nil
}

// FavoriteSong 把一首收进用户的收藏夹，返回收进了哪一格。
//
// 这一条是抽屉那一项「收藏」接的整个操作：换 aid → 挑格子 → 判重复 → 写。
// **界面那层只负责「点了 → 调它 → 把结果说给用户」**。
//
// FolderID 为 0 时挑第一格（见文件头）。已经在那儿了回 `ErrAlreadyFavorited`。
func (c *Client) FavoriteSong(ctx context.Context, s FavTarget) (FavFolder, error) {
	rid, typ, err := c.ridAndType(ctx, s)
	if err != nil {
		return FavFolder{}, err
	}

	// 挑格子：没指定就列出来取第一格。
	folder := FavFolder{}
	if s.FolderID != 0 {
		folder.ID = s.FolderID
	} else {
		nav, err := c.Nav(ctx)
		if err != nil {
			return FavFolder{}, err
		}
		if !nav.LoggedIn || nav.Mid == 0 {
			return FavFolder{}, ErrNotLoggedIn
		}
		folders, err := c.FavFolders(ctx, nav.Mid)
		if err != nil {
			return FavFolder{}, err
		}
		folder = folders[0]
	}

	// 判重复：已经在这一格里就别再写一次。
	ids, err := c.FavMediaFolders(ctx, rid, typ)
	if err != nil {
		return FavFolder{}, err
	}
	for _, id := range ids {
		if id == folder.ID {
			return folder, ErrAlreadyFavorited
		}
	}

	if err := c.dealFavMedia(ctx, rid, typ, folder.ID, 0); err != nil {
		return FavFolder{}, err
	}
	return folder, nil
}

// dealFavMedia 是一次写：往 addID 那一格放、从 delID 那一格拿。
//
// **只传 add_media_ids 的原因**：B 站的 deal 是「加和减一起」的，一次请求两件事。
// 少给一个参数比给错一个安全——`del_media_ids` 漏了只是不动，而给错了会把歌从
// 用户**已经收藏的地方**拿掉（那是要写用户真实收藏夹的操作，见 `FavTarget` 那段）。
func (c *Client) dealFavMedia(ctx context.Context, rid string, typ int, addID, delID int64) error {
	form := url.Values{
		"rid":         {rid},
		"type":        {strconv.Itoa(typ)},
		"platform":    {"web"},
		"ga":          {"1"},
		"gaia_source": {"web_normal"},
	}
	if addID != 0 {
		form.Set("add_media_ids", strconv.FormatInt(addID, 10))
	}
	if delID != 0 {
		form.Set("del_media_ids", strconv.FormatInt(delID, 10))
	}
	_, err := c.Do(ctx, Call{
		Method: "POST",
		URL:    c.hostAPI + pathFavDeal,
		Form:   form,
		CSRF:   true,
	})
	return err
}

// ridAndType 是这一项在收藏接口那边的身份：B 站要 **avid**，type 是 2 或 12。
//
// 这一步是一次网络往返（bvid → aid，见文件头），所以它**在这里**而不是界面上。
//
// audio 的那一路用 sid 当 rid（B 站的音乐库就是这么收的），而我们这一层还没有接
// 音乐库的接口，所以 `Type == "audio"` 这里会回一句人话，而不是发一次注定失败的
// 请求。
func (c *Client) ridAndType(ctx context.Context, s FavTarget) (rid string, typ int, err error) {
	switch s.Type {
	case "mv", "":
		if s.Bvid == "" {
			return "", 0, ErrNoVideo
		}
		aid, err := c.ArchiveID(ctx, s.Bvid)
		if err != nil {
			return "", 0, err
		}
		return strconv.FormatInt(aid, 10), FavTypeVideo, nil
	case "audio":
		if s.Sid == "" {
			return "", 0, ErrMusicLibraryUnsupported
		}
		return s.Sid, FavTypeAudio, nil
	}
	return "", 0, fmt.Errorf("bili: 认不出的条目类型 %q", s.Type)
}

// ErrMusicLibraryUnsupported 是「收藏夹里的音频那一路还没接上」。
//
// 单独一个哨兵：它和「这一首收藏不了」要分开说——这一条等音乐库接上就好了，
// 而别的收藏失败重试多少次都一样（见 `app.playableOnline` 那边同一个说法的哨兵）。
var ErrMusicLibraryUnsupported = errors.New("bili: 音乐库那一路还没接上")
