// 收藏夹 / 合集 / 系列页的头部和操作条（「深夜唱片行」，原型 `PAGES.collection` 和
// CSS `.col-head` / `.col-actions`，DESIGN.md §0′.T3）。
//
//	头部     通栏色块：封面取色按 26% 混进 bg；232 封面（`--shadow-cover`）、
//	         Eyebrow（类型 · 私密）、64 号宋体标题、元信息一行、简介
//	操作条   播放全部 / 随机播放 / 收藏 / 下载（菜单）/ 多选 / 更多（菜单），右边排序 + 搜索
//
// 这个文件只管**长什么样**；拉什么、怎么排、多选勾哪些仍在 `collectionpage.go` 和
// `collectionbatch.go`。
package ui

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/covertint"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/skeleton"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// 头部尺寸（原型 `.col-head`）。
const (
	// collectionCoverSide 是头部封面的边长（`.cover.lg` 232）。
	collectionCoverSide float32 = 232
	// collectionHeadPad 是头部的上下内边距，也是封面和文字的缝（40）。
	collectionHeadPad float32 = 40
	// collectionOpsGap 是操作条上控件的缝（12）。
	collectionOpsGap float32 = 12
)

// tintBackground 是「封面取色按 mix 混进 bg」的那块纯色（原型
// `color-mix(in srgb, var(--tint) N%, var(--bg))`）。
//
// 原型的 tint 是专辑平均色；这里的封面取的是 `covertint.Hue`（色相 + 饱和度，亮度由主题
// 定，见 `Hue.Color`），所以混出来的色相对、明暗稳。还没取到、封面没有颜色或是空地址
// 时就是 bg 本色（没有色块，和原型里 `tint` 缺省灰的效果接近）。
func (s *Shell) tintBackground(c *ui.Context, cover string, mix float32) ui.Color {
	t := c.Theme()
	bg := tok.Of(t).Bg
	if cover == "" {
		return bg
	}
	hue, sat := s.tint.sync(cover, s.wake.invalidate)
	if sat <= 0 {
		return bg
	}
	col, ok := parseHexColor(covertint.Hue{H: hue, S: sat}.Color(t.Dark))
	if !ok {
		return bg
	}
	return bg.Mix(col, mix)
}

// collCoverDraw 把一个封面地址包成 `pud.Cover`（有地址的三态走 imgcache）。
func (s *Shell) collCoverDraw(url string) pud.Cover {
	return func(c *ui.Context, w, h float32) {
		imgcache.ViewRadius(c, s.images, url, w, h, 0)
	}
}

// upFaceDraw 把一个头像地址包成 `pud.Cover`；地址为空返回 nil（pud.Avatar 画首字）。
func (s *Shell) upFaceDraw(url string) pud.Cover {
	if url == "" {
		return nil
	}
	return func(c *ui.Context, w, h float32) {
		imgcache.States(c, s.pageImages(), url, w, h, w/2, imgcache.AvatarShape())
	}
}

// viewCollectionHeader 是头部（封面 + 文字列）；通栏色块见 `paintCollectionBand`。
func (s *Shell) viewCollectionHeader(c *ui.Context, p *collectionPage) {
	t := c.Theme()
	pl := tok.Of(t)
	info := s.app.Collection(p.ref)
	cover := s.collectionHeaderCover(p.ref, info)
	bg := s.tintBackground(c, cover, tok.TintCollection)

	// ⚠️ **色块不画在这一行上**：这一行在列表的第 0 行里，被列表的边界裁在页边距里面；
	// 通栏的底色由页根的 `Draw` 在列表**外面**铺（`paintCollectionBand`），这里只记
	// 颜色和高度（上一帧量到的）；这一行自己也刷同一色，盖住列表里那层页面底色。
	p.bandBG = bg
	row := ui.Row(c).Key("collection-header").FillWidth().AlignItems(ui.End).Gap(space.Px40).
		Padding(collectionHeadPad, space.None, collectionHeadPad, space.None).Wrap().Background(bg)
	defer func() {
		if h := row.Bounds().H; h > 0 {
			p.bandH = h
		}
	}()
	row.Children(func() {
		// ① 封面。元信息没回来时画骨架（空地址在 imgcache 里是「本来就没有」，含义相反）。
		box := ui.Box(c).Size(collectionCoverSide, collectionCoverSide).Radius(tok.R8).Clip().
			Background(pl.Surface2).Shrink(0)
		box = shadow.Cover(box, t)
		box.Children(func() {
			switch {
			case !info.Loaded:
				imgcache.Loading(c, collectionCoverSide, collectionCoverSide, imgcache.Shape{})
			case cover == "" && info.Default:
				// 默认收藏夹没有封面时是主色底 + 心（原型「我喜欢」）。
				ui.Box(c).Size(collectionCoverSide, collectionCoverSide).Background(pl.Accent).
					AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					ui.Icon(c, pud.Icon("heart")).FontSize(72).TextColor(pl.AccentInk)
				})
			default:
				imgcache.ViewRadius(c, s.images, cover, collectionCoverSide, collectionCoverSide, 0)
			}
		})

		// ② 文字列：Eyebrow / 标题 / 元信息 / 简介。整列可选中复制（票 #685）。
		ui.Column(c).Grow(1).Shrink(1).MinWidth(240).AlignItems(ui.Start).Selectable().Children(func() {
			typo.Eyebrow(c, collectionEyebrow(p.ref.Type, info))
			typo.Display(ui.Text(c, collectionTitleText(p.ref.Type, info)), scale.Font(c, collectionTitleSize(c))).
				LineHeight(1.1).TextColor(pl.Text).MaxLines(2).Ellipsis("…").
				Margin(space.Px12, space.None, space.Px12, space.None)

			s.viewCollectionMeta(c, p, info)

			if info.Intro != "" {
				ui.Text(c, info.Intro).FontSize(scale.Font(c, scale.Size14)).TextColor(pl.Text2).
					MaxLines(2).Ellipsis("…").MaxWidth(560).
					Margin(space.Px12, space.None, space.None, space.None)
			}
		})
	})
}

// collectionTitleSize 是头部大标题的字号：原型 `font-size: clamp(36px, 4.4vw, 64px)`，
// vw 是窗口宽（不是内容区宽）。
func collectionTitleSize(c *ui.Context) float32 {
	w, _ := c.Size()
	return min(max(36, 0.044*w), 64)
}

// collectionEyebrow 是标题上面那行小字：「收藏夹 · 私密」。
func collectionEyebrow(typ bili.CollectionType, info app.CollectionInfo) string {
	k := typ.Label()
	if typ == bili.CollectionFavorite && info.Loaded && info.Private {
		k += " · 私密"
	}
	return k
}

// viewCollectionMeta 是元信息一行（原型 `.col-meta`）：作者、N 首、总时长、播放量、
// 被收藏数、仅自己可见；13 号次要色，缝 8/16，放不下就换行。
//
// **数据取法**（原型里全是假数）：
//   - 作者、首数：容器元信息（`folder/info`、合集 / 系列接口）。
//   - 播放量、被收藏数：`folder/info` 的 `cnt_info`；合集 / 系列接口不给，不画那一格（不画 0）。
//   - 总时长：已加载的条目时长之和——**只在条目都加载完（数量等于首数）时才画**，
//     否则那是个偏小的假数。
func (s *Shell) viewCollectionMeta(c *ui.Context, p *collectionPage, info app.CollectionInfo) {
	pl := tok.Of(c.Theme())
	fs := scale.Font(c, scale.Size13)
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(space.Px8).Selectable().Children(func() {
		// 作者：元信息没回来时一道骨架，数据到了不跳高。
		if !info.Loaded {
			skeleton.Block(c, "collection-owner:skel", 96, 14, 4)
			return
		}
		if info.OwnerName != "" {
			list.OwnerName(c, info.OwnerName, info.OwnerName, list.OwnerTipText(info.OwnerName),
				info.OwnerMid, fs, 1.5, pl.Text2, s.ownerActions())
		}
		item := func(txt string) {
			ui.Text(c, txt).FontSize(fs).TextColor(pl.Text2).SingleLine().Margin(space.None, space.None, space.None, space.Px8)
		}
		mono := func(txt string) {
			typo.Num(ui.Text(c, txt)).FontSize(fs).TextColor(pl.Text2).SingleLine().
				Margin(space.None, space.None, space.None, space.Px8)
		}
		mono(strconv.Itoa(info.MediaCount) + " 首")
		if d, ok := s.collectionTotalDuration(p, info); ok {
			item(collectionDurationText(d))
		}
		if n := list.PlayCountText(info.PlayCount); n != "" {
			mono(n + " 播放")
		}
		if n := list.PlayCountText(info.FavCount); n != "" {
			mono(n + " 人收藏")
		}
		if p.ref.Type == bili.CollectionFavorite && info.Private {
			ui.Row(c).Gap(space.Px4).AlignItems(ui.Center).Margin(space.None, space.None, space.None, space.Px8).Children(func() {
				ui.Icon(c, pud.Icon("lock")).FontSize(14).TextColor(pl.Text2)
				ui.Text(c, "仅自己可见").FontSize(fs).TextColor(pl.Text2).SingleLine()
			})
		}
	})
}

// collectionTotalDuration 是已加载条目的时长之和；条目没加载全时 ok 为假。
func (s *Shell) collectionTotalDuration(p *collectionPage, info app.CollectionInfo) (time.Duration, bool) {
	if info.MediaCount <= 0 {
		return 0, false
	}
	l := p.list.List()
	if l.Len() < info.MediaCount {
		return 0, false
	}
	var sum time.Duration
	for i := range l.Len() {
		if m, ok := l.At(i); ok {
			sum += m.Duration
		}
	}
	return sum, sum > 0
}

// collectionDurationText 是总时长的口语写法：「41 分钟」「2 小时 5 分钟」。
func collectionDurationText(d time.Duration) string {
	mins := int(d.Round(time.Minute) / time.Minute)
	if mins < 1 {
		mins = 1
	}
	if mins < 60 {
		return strconv.Itoa(mins) + " 分钟"
	}
	h, m := mins/60, mins%60
	if m == 0 {
		return strconv.Itoa(h) + " 小时"
	}
	return strconv.Itoa(h) + " 小时 " + strconv.Itoa(m) + " 分钟"
}

// ── 操作条 ─────────────────────────────────────────────────────────────────

// collectionMine 报这个容器是不是自己建的（自己的收藏夹不画「收藏」键）。
func (s *Shell) collectionMine(info app.CollectionInfo) bool {
	self := s.app.SelfMid()
	return info.Loaded && self > 0 && info.OwnerMid == self
}

// viewCollectionOps 是头部下面那一排（原型 `.col-actions`：上 24 下 8，缝 12，放不下换行）：
// 播放全部 / 随机播放 / 收藏 / 下载 / 多选 / 更多，右边排序和列表内搜索。
func (s *Shell) viewCollectionOps(c *ui.Context, p *collectionPage) {
	pl := tok.Of(c.Theme())
	info := s.app.Collection(p.ref)
	empty := info.Loaded && info.MediaCount == 0

	ui.Row(c).Key("collection-ops").FillWidth().Wrap().AlignItems(ui.Center).Gap(collectionOpsGap).
		Padding(space.Px24, space.None, space.Px8, space.None).Children(func() {
		if pud.Button(c, "col:play", pud.Primary, pud.Std, "播放全部", "play").Disabled(empty).Clicked() {
			s.playWholeCollection(p)
		}
		if pud.Button(c, "col:shuffle", pud.Ghost, pud.Std, "随机播放", "shuffle").Disabled(empty).Clicked() {
			s.shuffleWholeCollection(p)
		}
		s.viewCollectionStar(c, p, info)

		// 下载（菜单）。
		s.withTip(c, pud.RoundButton(c, "col:dl", pud.Ghost, pud.Std, "download", "下载全部"), "下载全部").
			Menu(func(m *ui.Menu) {
				for _, it := range collectionMoreBaseItems {
					if m.Item(it.label).Chosen() {
						s.collectionMoreAction(p, it)
						return
					}
				}
			})

		// 多选。
		multi := pud.RoundButton(c, "col:multi", pud.Ghost, pud.Std, "select", "多选")
		if p.multi {
			multi = multi.Background(pl.Press).TextColor(pl.AccentText)
		}
		if s.withTip(c, multi, "多选").Clicked() {
			s.toggleCollectionMulti(p)
		}

		s.viewCollectionMore(c, p)

		// 右边：排序 + 搜索（原型 `.list-tools`，margin-left:auto）。
		ui.Row(c).Grow(1).Justify(ui.End).AlignItems(ui.Center).Gap(space.Px12).Children(func() {
			if collectionSortSupported(p.ref.Type) {
				s.viewCollectionSort(c, p)
			}
			s.viewCollectionSearch(c, p)
		})
	})
	_ = motion.Fast
}

// shuffleWholeCollection 随机播放：把已加载的条目洗牌后换成队列并开播。
func (s *Shell) shuffleWholeCollection(p *collectionPage) {
	items, skipped := app.QueueItemsOfMediasCounting(s.allMedias(p))
	if len(items) == 0 {
		s.addToast("这里还没有可以播的歌", notice.Warning)
		return
	}
	rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	if err := s.app.PlayFrom(items...); err != nil {
		s.addToast("放不了："+err.Error(), notice.Danger)
		return
	}
	s.addToast(collectionBatchToast("已随机播放 "+strconv.Itoa(len(items))+" 首", skipped), notice.Success)
}

// viewCollectionStar 是「收藏 / 已收藏」幽灵键（原型 `data-act="subscribe"`）。
//
// 不画：系列（B 站没有收藏系列这个动作）、自己建的收藏夹（原型 `c.mine`）。
func (s *Shell) viewCollectionStar(c *ui.Context, p *collectionPage, info app.CollectionInfo) {
	if p.ref.Type == bili.CollectionSeries || s.collectionMine(info) {
		return
	}
	label, icon := "收藏", "star"
	tip := "收藏这个收藏夹"
	if p.ref.Type != bili.CollectionFavorite {
		tip = "收藏这个" + p.ref.Type.Label()
	}
	if info.Faved {
		label, icon, tip = "已收藏", "check", "取消收藏"
	}
	if s.withTip(c, pud.Button(c, "col:fav", pud.Ghost, pud.Std, label, icon), tip).Clicked() {
		s.setCollectionFav(p, !info.Faved)
	}
}

// collectionMoreItems 是 ⋯ 菜单：原型的「编辑收藏夹 / 复制链接 / 全部加入队列 / 删除收藏夹」，
// 加上本仓库原有的「清除失效内容 / 从侧栏移除」（服务层能力保留）。
func (s *Shell) collectionMoreItems(p *collectionPage) []collectionMenuItem {
	info := s.app.Collection(p.ref)
	items := []collectionMenuItem{}
	mineFolder := p.ref.Type == bili.CollectionFavorite && s.collectionMine(info) && !info.Default
	if mineFolder {
		items = append(items, collectionMenuItem{key: "edit-folder", label: "编辑收藏夹"})
	}
	items = append(items,
		collectionMenuItem{key: "copy-link", label: "复制链接"},
		collectionMenuItem{key: "enqueue", label: "全部加入队列"})
	if info.Loaded && p.ref.Type == bili.CollectionFavorite && s.app.CanDeleteCollection(p.ref) {
		items = append(items, collectionMoreOwnerItems...)
	}
	return append(items, collectionMoreForgetItem)
}

// viewCollectionMore 是 ⋯ 圆键和它的菜单。
func (s *Shell) viewCollectionMore(c *ui.Context, p *collectionPage) {
	items := s.collectionMoreItems(p)
	s.withTip(c, pud.RoundButton(c, "col:more", pud.Ghost, pud.Std, "more", "更多"), "更多").
		Menu(func(m *ui.Menu) {
			for _, it := range items {
				if m.Item(it.label).Chosen() {
					s.collectionMoreAction(p, it)
					return
				}
			}
		})
}

// collectionLink 是这个容器在 B 站网页上的地址。
func collectionLink(ref app.CollectionRef) string {
	id := strconv.FormatInt(ref.ID, 10)
	switch ref.Type {
	case bili.CollectionFavorite:
		return "https://www.bilibili.com/medialist/detail/ml" + id
	default:
		return "https://www.bilibili.com/list/" + id
	}
}

// viewCollectionSort 是排序下拉（原型 `.select`：高 36、描边 line-strong、surface-2 底）。
// 排序是接口排的，换档后整个换列表（见 `setCollectionSort`）。
func (s *Shell) viewCollectionSort(c *ui.Context, p *collectionPage) {
	t := c.Theme()
	pl := tok.Of(t)
	sel := ui.SelectBase[bili.CollectionSort](c, &p.sort)

	trigger := sel.Trigger.Height(tok.SelectH).Radius(tok.R8).
		Padding(space.None, space.Px10, space.None, space.Px12).Gap(space.Px8).AlignItems(ui.Center).
		Background(pl.Surface2).MinWidth(scale.Font(c, collectionSortMinW)).Shrink(0)
	border := pl.LineStrong
	if trigger.Hovered() {
		border = pl.Text3
	}
	trigger = trigger.Border(1, border).Transition(motion.Fade).Label("排序：" + p.sort.Label())
	trigger.Children(func() {
		ui.Text(c, p.sort.Label()).FontSize(scale.Font(c, scale.Size13)).TextColor(pl.Text).SingleLine()
		ui.Icon(c, pud.Icon("down")).FontSize(14).TextColor(pl.Text3)
	})

	sel.Popup(func(panel ui.Element) {
		popupPanel(t, panel.Padding(space.Px8))
		for _, sort := range bili.CollectionSorts {
			item := sel.Item(sort).Padding(space.Px8, space.Px10).Radius(tok.R8).Gap(space.Px16).AlignItems(ui.Center)
			if item.Highlighted() {
				item = item.Background(pl.Hover)
			}
			checked := sort == p.sort
			item.Children(func() {
				lbl := ui.Text(c, sort.Label()).FontSize(scale.Font(c, scale.Size13)).TextColor(pl.Text).SingleLine().Grow(1)
				if checked {
					lbl.TextColor(pl.AccentText).FontWeight(600)
					ui.Icon(c, pud.Icon("check")).FontSize(14).TextColor(pl.AccentText)
				}
			})
		}
	})

	// 判据是「下拉那一档」和「列表是按哪一档建的」不一样，不是 `Changed()`
	// （重画 / 切回来时那一帧已经过去，列表会永远停在旧排序）。
	if p.sort != p.listSort {
		s.setCollectionSort(p)
	}
}

// collectionSearchLabelText 是搜索框的占位字（原型「搜索标题/UP主名称」）。
const collectionSearchLabelText = "搜索标题/UP主名称"

// viewCollectionSearch 是列表内搜索（原型 `.mini-search`：高 32 胶囊、surface-2 底）。
//
// 筛是接口筛的，回车立刻搜，防抖 500ms（`collectionSearchChanged`）。
// 合集 / 系列接口没有 keyword：框画出来但禁用，说明走无障碍描述。
func (s *Shell) viewCollectionSearch(c *ui.Context, p *collectionPage) {
	supported := collectionSearchSupported(p.ref.Type)
	if !supported {
		p.draft = p.keyword
		p.pendingSince = time.Time{}
		p.submitted = false
	}
	in := pud.MiniSearch(c, "col:filter", &p.draft, collectionSearchLabelText, 140)
	if !supported {
		in.Disabled(true).Description(collectionSearchUnsupportedTip)
		return
	}
	p.submitted = in.Submitted()
	if s.collectionSearchChanged(p) {
		s.reloadCollection(p)
	}
}

// collectionBulkText 把批量条上的「已选 N 首」拼出来（原型 `.batch`）。
func collectionBulkText(n int) string { return "已选 " + strconv.Itoa(n) + " 首" }

var _ = strings.TrimSpace

// paintCollectionBand 在页根上铺头部那块通栏底色（原型 `.col-head` 的 `margin: 0 -40px`）：
// 从内容区左缘到右缘、从页顶往下 bandH 高，跟着列表滚走（`ScrollOffset` 是精确像素），
// 并裁在页根矩形里，不会盖到顶栏上。
func (s *Shell) paintCollectionBand(pp *ui.Painter, r ui.Rect, p *collectionPage) {
	if p.bandH <= 0 || p.bandBG.A == 0 {
		return
	}
	top := r.Y
	if p.leadSet {
		top -= p.list.ScrollOffset()
	}
	bottom := top + p.bandH
	if top < r.Y {
		top = r.Y
	}
	if bottom > r.Y+r.H {
		bottom = r.Y + r.H
	}
	if bottom <= top {
		return
	}
	pp.Fill(ui.Rect{X: r.X, Y: top, W: r.W, H: bottom - top}, p.bandBG, 0)
}
