// 收藏夹的写操作：新建、修改、封面上传、拉全量、往里放东西。
//
// # 和 collection.go 的分工
//
// `collection.go` 那一组是**读容器**（它是什么、里面有什么、收藏没收藏它）。
// 这个文件是**改容器**（建一个、改一个、换个封面、把内容放进去拿出来）。
// 分开是因为读的那一组全程可以重试，而写的这一组每调一次都是不可逆的——
// 把它们放在一起，复用的代码路径会顺手带上重试，而重试一个「新建收藏夹」
// 的后果是界面上多出两个同名收藏夹。
//
// # 接口清单（全部照 Biu 上游的 service/ 目录）
//
//	POST /x/v3/fav/folder/add            新建收藏夹
//	POST /x/v3/fav/folder/edit           修改收藏夹
//	GET  /x/v3/fav/folder/created/list   拉某个人创建的全部收藏夹（分页）
//	POST /x/vu/web/cover/up              上传一张封面，返回它的地址
//	POST /x/v3/fav/resource/deal         往收藏夹里放 / 拿出内容
//
// # 这一层仍然只回答问题，不做事
//
// 没有一个方法是后台的，没有一个会去唤醒界面，翻页的循环也不在这里
// （理由同 collection.go 的包注释）。要「拉全量」的话自己按
// `CreatedFolderPageSize` 一页一页翻——见 `app.LoadCreatedFolders`。
package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// 这几个接口的路径。
const (
	pathFolderAdd     = "/x/v3/fav/folder/add"
	pathFolderEdit    = "/x/v3/fav/folder/edit"
	pathFolderCreated = "/x/v3/fav/folder/created/list"
	pathCoverUpload   = "/x/vu/web/cover/up"
	pathResourceDeal  = "/x/v3/fav/resource/deal"
)

// CreatedFolderPageSize 是拉收藏夹列表时一页几条。
//
// ⚠️ **100 是这个接口的上限，别调大**。和内容列表那个「20 会被默默截断」
// （见 `pageSizeLimit`）不一样：B 站对 `ps` 超过 50 的值是**回 -400**，不是
// 默默截断。所以 100 看着比内容列表那个大一半，实际已经贴着它的天花板了。
//
// 取 100 而不是 50 是照 Biu（`components/favorites-select-modal` 的 `ps: 100`）。
// 一个人的收藏夹很少超过几十个（上百已经是极端情况），一页通常就拉完了，
// 翻页只是兜底。
const CreatedFolderPageSize = 100

// FolderDraft 是一个收藏夹改完之后应该长什么样。
//
// ⚠️ **Privacy 是 B 站那边的原值，不是「是不是公开」**：0 公开、1 私密
// （Biu 的 `privacy: values.isPublic ? 0 : 1`，`components/favorites-edit-modal/index.tsx:107`）。
// 在这一层保留成原值是刻意的：把它翻成布尔会在调用方和这一层各写一遍映射，
// 而两遍迟早会不一致——一边写 1 私密，另一边把 `attr` 的第 0 位当成了枚举。
type FolderDraft struct {
	Title string
	// Intro 是简介。空串表示「不填」——Biu 那边空着就不发这个字段
	// （`omitBy(data, v => v === undefined || v === "" || v === null)`），
	// 这里照做：发空串和不发是同一个意思，但 B 站存的是「上次那个」还是
	// 「空」两种可能。
	Intro string
	// Cover 是封面地址（已经上传过的那种 `https://…`）。空串表示不换封面。
	Cover string
	// Privacy 见类型上的注释。**它跟着 Attr 的第 0 位**，不是跟着 Attr 整个。
	Privacy int
}

// FolderInfo 是 add / edit 两个接口回来的 `data` 段。
//
// ⚠️ 只取界面上立刻要用得到的那几样（id / title / cover / mid / type）。
// Biu 把这一份当 `FavFolderInfoData` 用，字段有二十来个，而新建成功之后
// 界面要做的只有两件事：把新收藏夹塞进侧栏、把弹窗关掉。
type FolderInfo struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Cover string `json:"cover"`
	Mid   int64  `json:"mid"`
	Type  int    `json:"type"`
	Attr  int    `json:"attr"`
}

// CreatedFolder 是「我创建的收藏夹」列表里的一条。
type CreatedFolder struct {
	ID    int64
	Title string
	// Attr 的第 0 位是公开性（见 `CollectionInfo` 的注释），第 1 位是
	// 「是不是默认收藏夹」。
	Private bool
	Default bool
	// MediaCount 是里面有几个内容。
	MediaCount int
	// Faved 是**这条内容**在不在里面。选择弹窗就是拿它填初始勾选的。
	Faved bool
}

// createdListData 是 created/list 的 `data` 段。
type createdListData struct {
	Count   int  `json:"count"`
	HasMore bool `json:"has_more"`
	List    []struct {
		ID         int64  `json:"id"`
		Title      string `json:"title"`
		Attr       int    `json:"attr"`
		MediaCount int    `json:"media_count"`
		FavState   int    `json:"fav_state"`
	} `json:"list"`
}

// CreatedFolders 拉某人创建的那一页收藏夹，page 从 1 起。
//
// rid 是「问的是这条内容在不在里面」，0 表示不问（选择弹窗关掉初始勾选时
// 就是这么问的）。upMid 是谁的收藏夹，一般是当前登录的人。
//
// 返回的第二项是「后面还有没有」。**翻页的循环不在这里**：见包注释。
func (c *Client) CreatedFolders(ctx context.Context, upMid, rid int64, page int) ([]CreatedFolder, bool, error) {
	if upMid <= 0 {
		return nil, false, fmt.Errorf("bili: 收藏夹主人 id 不对（%d）", upMid)
	}
	if page < 1 {
		page = 1
	}

	q := url.Values{
		"up_mid": {strconv.FormatInt(upMid, 10)},
		"pn":     {strconv.Itoa(page)},
		"ps":     {strconv.Itoa(CreatedFolderPageSize)},
	}
	// rid 传 0 和不传不一样：B 站认 0 是「都返回」而不带这个参数时它按自己
	// 的记忆补一个值，而那个值会把一部分收藏夹悄悄藏起来。
	q.Set("rid", strconv.FormatInt(rid, 10))

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + pathFolderCreated, Query: q})
	if err != nil {
		return nil, false, err
	}
	var d createdListData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, false, fmt.Errorf("bili: 收藏夹列表的返回读不懂：%w", err)
	}

	out := make([]CreatedFolder, 0, len(d.List))
	for _, f := range d.List {
		out = append(out, CreatedFolder{
			ID: f.ID, Title: f.Title,
			Private: f.Attr&1 == 1, Default: (f.Attr>>1)&1 == 0,
			MediaCount: f.MediaCount, Faved: f.FavState == 1,
		})
	}
	return out, d.HasMore, nil
}

// CreateFolder 新建一个收藏夹，返回它的新 id 和那几个字段。
func (c *Client) CreateFolder(ctx context.Context, d FolderDraft) (*FolderInfo, error) {
	if strings.TrimSpace(d.Title) == "" {
		return nil, fmt.Errorf("bili: 收藏夹还没起名字")
	}
	return c.writeFolder(ctx, pathFolderAdd, url.Values{
		"title":   {strings.TrimSpace(d.Title)},
		"intro":   {strings.TrimSpace(d.Intro)},
		"privacy": {strconv.Itoa(d.Privacy)},
		"cover":   {d.Cover},
	})
}

// EditFolder 改一个已有的收藏夹。id 是收藏夹的完整 id（mlid）。
func (c *Client) EditFolder(ctx context.Context, id int64, d FolderDraft) (*FolderInfo, error) {
	if id <= 0 {
		return nil, fmt.Errorf("bili: 收藏夹 id 不对（%d）", id)
	}
	if strings.TrimSpace(d.Title) == "" {
		return nil, fmt.Errorf("bili: 收藏夹还没起名字")
	}
	return c.writeFolder(ctx, pathFolderEdit, url.Values{
		"media_id": {strconv.FormatInt(id, 10)},
		"title":    {strings.TrimSpace(d.Title)},
		"intro":    {strings.TrimSpace(d.Intro)},
		"privacy":  {strconv.Itoa(d.Privacy)},
		"cover":    {d.Cover},
	})
}

func (c *Client) writeFolder(ctx context.Context, path string, form url.Values) (*FolderInfo, error) {
	raw, err := c.Do(ctx, c.writeCall(c.hostAPI+path, dropEmpty(form)))
	if err != nil {
		return nil, fmt.Errorf("bili: %s: %w", path, err)
	}
	var d FolderInfo
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: %s 的返回读不懂：%w", path, err)
	}
	return &d, nil
}

// UploadCover 上传一张封面，返回 B 站给它分配的地址。
//
// cover 是一张图直接 base64 之后的数据地址（`data:image/jpeg;base64,…`），
// **不是文件路径、也不是 raw base64**——B 站要从前面那个前缀里认格式。
// 裁成什么、编成什么格式是调用方的事，见 `ui` 里的封面裁剪弹窗。
//
// ⚠️ 这一条打的是 `member.bilibili.com`（Biu 的 `memberRequest`，别的接口
// 都是 `apiRequest`）。打到 `api` 上去会得到 -404。
func (c *Client) UploadCover(ctx context.Context, cover string) (string, error) {
	if !strings.HasPrefix(cover, "data:image/") {
		return "", fmt.Errorf("bili: 封面要的是 data:image/… 开头的数据地址")
	}
	raw, err := c.Do(ctx, c.writeCall(c.hostMember+pathCoverUpload, url.Values{"cover": {cover}}))
	if err != nil {
		return "", fmt.Errorf("bili: 上传封面: %w", err)
	}
	var d struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return "", fmt.Errorf("bili: 上传封面的返回读不懂：%w", err)
	}
	if d.URL == "" {
		return "", fmt.Errorf("bili: 上传封面没给回地址")
	}
	return d.URL, nil
}

// DealFolderMedias 往收藏夹里放几个内容、从里面拿到几个。
//
// ⚠️ **这是「改收藏夹里的内容」那一个接口，本项目只有这一份。** BMG-25（播放
// 列表抽屉）那张票要实现「把这一首收藏起来」这个动作，它要的**就是这一个调用**
// （Biu 的抽屉也是调 `postFavFolderDeal`，`music-playlist-drawer` 的
// `onOpenFavSelectModal` 之后走的就是它）。**别再写一份**——那个接口的四个
// 平台字段（`platform` / `ga` / `gaia_source`）少给不报错也不生效，抄漏了
// 表现是「收藏了但条目数不涨」，而那要翻很久才知道是哪个字段。
//
// ⚠️ **两个都空是 no-op**：Biu 的前端会发一个两个字段都空的请求过去
// （`handleConfirm` 的 join 结果都是空串），而 B 站对那种请求回 -400。
//
// ⚠️ type 只认 2（视频）和 12（音频），和内容列表那边同一套
// （见 `Media` 那两个常量）。合集不适用这个接口。
func (c *Client) DealFolderMedias(ctx context.Context, rid int64, contentType int, add, del []int64) error {
	if rid <= 0 {
		return fmt.Errorf("bili: 内容 id 不对（%d）", rid)
	}
	if len(add) == 0 && len(del) == 0 {
		return nil
	}
	_, err := c.Do(ctx, c.writeCall(c.hostAPI+pathResourceDeal, url.Values{
		"rid":           {strconv.FormatInt(rid, 10)},
		"type":          {strconv.Itoa(contentType)},
		"add_media_ids": {joinIDs(add)},
		"del_media_ids": {joinIDs(del)},
		// 这四个是 B 站网页端自己带的。Biu 逐字抄（`service/fav-folder-deal.ts`）。
		// 少给不报错也不生效——B 站按 web 端的统计口径记账，而这里是 app，
		// 于是「加了但计数没涨」，表现是收藏夹里东西在、条目数永远停在 0。
		"platform":    {platformWeb},
		"ga":          {"1"},
		"gaia_source": {"web_normal"},
	}))
	if err != nil {
		return fmt.Errorf("bili: 改收藏夹 %d 的内容: %w", rid, err)
	}
	return nil
}

// joinIDs 把一堆 id 拼成 B 站要的那个逗号串。
func joinIDs(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

// dropEmpty 去掉空值的字段。
//
// ⚠️ 别把它当「顺手清理」：`cover` 为空串在 Biu 那边表示**不换封面**
// （`omitBy` 那一处），所以它被去掉之后 B 站保留原来的封面——这是我们要的。
// 而 `privacy` 是 "0" 或 "1"，那个字符串非空，删不掉。
func dropEmpty(form url.Values) url.Values {
	out := url.Values{}
	for k, vs := range form {
		for _, v := range vs {
			if v != "" {
				out.Set(k, v)
			}
		}
	}
	return out
}
