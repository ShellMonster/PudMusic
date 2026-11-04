// 动态卡片与收藏夹卡片：UP 空间和动态流两页共用。
//
// **两页画的是同一样东西**（Biu 上游也是同一个组件：动态流的
// `dynamic-feed/item.tsx` 和 UP 空间的 `user-profile/dynamic-list/item.tsx`
// 结构一样）。写一份在这里，两边就不会画出两种样子——
// 「在动态流里点得开、在空间里点不开」是最难查的那一类不一致。
package ui

import (
	"fmt"
	tclean "github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// dynamicCoverW / dynamicCoverH 是投稿那一块的封面尺寸：**16:9 的横图**
// （上游 `w-48 h-32` = 192×128，`user-profile/dynamic-list/item.tsx:180`）。
//
// ⚠️ **不是方的**：改之前是 96 见方（纯音频产品的旧设计，票 #268 改成对齐
// 参考图——B 站 UP 主页那条动态里的稿件卡是横着的）。
const (
	dynamicCoverW = 192
	dynamicCoverH = 128
)

// dynamicAvatarS 是动态条目上那个头像的边长。
//
// 上游 HeroUI 的 `User` 组件默认 `size="md"`——头像那一档是 **40**
// （`md` 是 `w-10 h-10`）。
const dynamicAvatarS = 40

// dynamicPicSide 是图文里每张图的边长。
const dynamicPicSide = 92

// dynamicCardPadY 是动态条目上下的内边距：上游的 `pt-4`（CardHeader）和
// `pb-4`（卡片自己），两个都是 16。
//
// ⚠️ **左右是 0**（上游那一档没有横向内边距）：那一段由页面那一层给——
// 动态流那一页是 16+16 = 32，UP 空间那一页是 `p-4` 的 16。
const dynamicCardPadY = space.Px16

// maxDynamicPics 是图文最多画几张图。
//
// B 站一条图文最多九张。**超出的丢掉而不是缩小**：缩小到看不见的那几张
// 只是多占一行布局，而这一行的行高是量出来的（变高行），布局一变
// 滚动就会跳。
const maxDynamicPics = 9

// dynLikes 是「这一次会话里用户动过的那几条赞」。
//
// 🔴 **它存在的理由是「点了之后要马上变」**：那一按是**乐观**的（先点亮再发
// 请求），而界面上如果还是原来那一帧，用户会以为没点上，于是再点一下——
// 那一下把刚才那一下撤掉了。他什么都没做，赞数却动了两次。
//
// 🔴 **带锁**：写它的是界面线程（按下那一下）和后台 goroutine（B 站说「赞不了」
// 时要撤回去）。不加锁 `-race` 会红，而红的那次症状是「赞的按钮偶尔自己弹回去」
// ——因为撤回去那一条正好盖掉了用户紧接着按的第二下。
//
// 没动过的那些不在这儿：读的是 B 站拉回来的那份（`app.Dynamic.Liked`）。
type dynLikes struct {
	mu sync.Mutex
	m  map[string]dynLike
}

// dynLike 是一条动态此刻的赞状态。
type dynLike struct {
	liked bool
	count int
}

// get 读这一条此刻的赞状态。没有动过就给拉回来的那一份（wasLiked / wasCount）。
func (l *dynLikes) get(id string, wasLiked bool, wasCount int) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.m[id]
	if !ok {
		return wasLiked, wasCount
	}
	return v.liked, v.count
}

// flip 是「按了一下赞」：翻过来，赞数跟着 +1 / −1，返回翻之后的那一份。
func (l *dynLikes) flip(id string, wasLiked bool, wasCount int) dynLike {
	l.mu.Lock()
	defer l.mu.Unlock()
	prev, ok := l.m[id]
	if !ok {
		// 没动过的那一条从 B 站拉回来的那份起算。
		prev = dynLike{liked: wasLiked, count: wasCount}
	}
	next := dynLike{liked: !prev.liked}
	if next.liked {
		next.count = prev.count + 1
	} else {
		// ⚠️ **不许减到负数**：未登录时 B 站那个字段整个没有（赞数是 0），
		// 减下去界面上会出现「赞 -1」。
		next.count = max(prev.count-1, 0)
	}
	if l.m == nil {
		l.m = map[string]dynLike{}
	}
	l.m[id] = next
	return next
}

// revert 把这一条从表里拿掉，界面于是回到**B 站拉回来的那一份**。
//
// 拿掉而不是写回旧值：B 站那边根本没有变（请求失败了），所以「拉回来的那份」
// 就是事实本身。写回一个副本的话，用户下一次翻页重新拉到同一条时会读到那个
// 副本，而两份不一样——界面上会显示一个他从来没设过的赞数。
func (l *dynLikes) revert(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, id)
}

// toggleDynamicLike 是「按了赞」那一下。
//
// **乐观**：先翻过来（界面立刻变），再发请求；B 站说不行就撤回去并说一句为什么。
// 不这么做的话，从按下到界面变化之间隔着一次网络往返（几百毫秒），而用户在这
// 段时间里会再按一下——于是他点了三次，最后的状态和他以为的相反。
func (s *Shell) toggleDynamicLike(d app.Dynamic) {
	account := s.app.AccountSnapshot()
	next := s.dynLikes.flip(d.ID, d.Liked, d.LikeCount)
	s.wake.invalidate()

	s.app.LikeDynamicForAccount(account, d.ID, next.liked, func(err error) {
		if err != nil {
			if err == app.ErrStaleGeneration || s.app.AccountSnapshot() != account {
				return
			}
			s.dynLikes.revert(d.ID)
			s.addToast(likeFailText(next.liked, err), notice.Danger)
		}
		s.wake.invalidate()
	})
}

// likeFailText 是赞 / 取消赞失败时说的那句话。
func likeFailText(like bool, err error) string {
	if like {
		return "赞不了：" + err.Error()
	}
	return "取消不了赞：" + err.Error()
}

// viewCollectionRow 画一张收藏夹 / 合集卡片。**整行可点**，跳到那个的内容页。
//
// ⚠️ **封面在指针移上去的时候放大 5%**：上游 UP 空间那两张 Tab（收藏夹 / 合集）
// 摆的是一格一格的封面卡（`user-profile/grid-card/index.tsx:26` 的
// `group-hover:scale-105` + `transition-transform duration-300`）。放大的是
// **框里那张图**，框不动——框要是自己长大，它就会压到旁边的标题上。
func (s *Shell) viewCollectionRow(c *ui.Context, col app.SpaceCollection, selected, ring bool) {
	t := c.Theme()
	box := ui.Row(c).Grow(1).Gap(space.Px12).Padding(space.Px12).AlignItems(ui.Center).
		Margin(space.None, space.None, space.Px8, space.None).
		Background(s.cardBackground(t)).Radius(s.cardRadius(t)).Border(1, t.Border).
		// 🔴 **字色钉回正文色，是无条件的**（理由见 viewDynamicCard 根上那一段）：
		// 框架在指针点一下时就把行标成选中并带上一层白字，标题没写死颜色，
		// 不钉的话点一下就变白字。
		TextColor(t.Text).
		Key("coll:" + strconv.FormatInt(col.ID, 10))
	// ⚠️ hover 要在 Children **之前**问（理由同 `widgets/list` 的 grid.go）：
	// 它报的是这一帧指针在不在这一行上，而封面是在 Children 里画的。
	on := box.Hovered()
	// 选中那一层：盖掉框架那块实心主色底，键盘走到它上面时给它一圈焦点环
	//（理由见 `widgets/list/selection.go`）。
	//
	// ⚠️ **必须盖**：框架给选中的行铺一整块实心主色底（`mygo/ui/table.go` 的
	// `node.Selected`），而票 #197 拍板去掉它——那一层在库里改不动。
	box = list.SelectedRow(box, t, selected, on, ring)
	if box.Clicked() {
		s.navigate(collectionPath(col.ID))
	}
	box.Children(func() {
		s.spaceThumbZoom(c, col.Cover, 72, 72,
			"coll-zoom:"+strconv.FormatInt(col.ID, 10), on)
		ui.Column(c).Grow(1).Gap(space.Px2).Children(func() {
			ui.Text(c, col.Title).FontSize(scale.Font(c, scale.Size16)).MaxLines(2).Ellipsis("…")
			ui.Text(c, fmt.Sprintf("%d 首", col.Count)).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
		})
	})
}

// viewDynamicCard 画一条动态卡片。
//
// 三种类型画在同一张卡上：**头像 + 昵称 + 时间 → 正文 → 内容**。
// 内容那一块按类型分三种（见 `app.DynamicKind`）。
//
// 🔴 **作者名可点进他的空间**（上游 `dynamic-feed/item.tsx:88-90` 跳
// `/user/{mid}`）。这是「能点进对应 UP 空间」那条验收在卡片上的落点。
func (s *Shell) viewDynamicCard(c *ui.Context, d app.Dynamic, selected, ring bool) {
	t := c.Theme()
	pl := tok.Of(t)
	// 原型 `.post`：44 头像 | 内容，缝 16，上下内边距 24，底下一条 line；`.feed` 最宽 760。
	//
	// ⚠️ **底色刷成页面底色、字色钉回正文色**：框架给「指针压着 / 点过的那一行」铺主色底和
	// 白字（`mygo/ui/list.go` 的 styleFn / `Selected`），底盖掉、字钉回，卡片自己的悬停反馈
	// （名字、赞、播放键各自有）不受影响。
	card := ui.Row(c).Key("dyn:"+d.ID).Grow(1).MaxWidth(feedMaxW).AlignItems(ui.Start).Gap(space.Px16).
		Padding(space.Px24, space.None, space.Px24, space.None).
		Background(pl.Bg).TextColor(pl.Text).
		BorderWidth(0, 0, 1, 0).BorderColor(pl.Line)
	// 选中那一层：盖掉框架那块实心主色底，键盘走到它上面时给它一圈焦点环
	//（理由见 `widgets/list/selection.go`）。`hovered` 传 false：悬停底已经被盖掉了。
	card = list.SelectedRow(card, t, selected, false, ring)
	card.Children(func() {
		pud.Avatar(c, d.Author.Name, 44, ui.Color{}, s.upFaceDraw(d.Author.Face))
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			// ① 头一行：名字（链接，600）+ 「时间 · 发布了音乐」（12 号 text-3）。
			ui.Row(c).Wrap().AlignItems(ui.Baseline).Gap(space.Px12).Children(func() {
				// ⚠️ **名字是「名字型链接」不是按钮壳**（票 #417），没有编号时点不动。
				name := d.Author.Name
				if name == "" {
					name = "这个 UP 主"
				}
				if flatLink(c, name).FontSize(scale.Font(c, scale.Size14)).FontWeight(600).Clicked() && d.Author.Mid > 0 {
					s.navigate(upPathOf(d.Author.Mid))
				}
				if d.Time != "" {
					when := d.Time
					if d.Kind == app.DynamicVideo && d.Video != nil {
						when += " · 发布了音乐"
					}
					ui.Text(c, when).FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3).MaxLines(1)
				}
			})

			// ② 正文（14 号，行高 1.7，上 8 下 16）。
			//
			// ⚠️ **投稿型动态的正文与稿件卡里的简介是同一句话时，正文不画**（见 `dynBodyRepeatsCard`）。
			hasBody := d.Text != "" && !dynBodyRepeatsCard(d)
			if hasBody {
				ui.Text(c, d.Text).FontSize(scale.Font(c, scale.Size14)).LineHeight(1.7).MaxLines(8).
					Margin(space.Px8, space.None, space.Px16, space.None)
			}

			// ③ 内容：投稿的嵌入卡 / 图文的图。
			gap := float32(space.Px8)
			if hasBody {
				gap = space.None
			}
			ui.Column(c).Margin(gap, space.None, space.None, space.None).Children(func() {
				switch {
				case d.Kind == app.DynamicVideo && d.Video != nil:
					s.viewDynamicVideo(c, d.Video, d.Author)
				case len(d.Pics) > 0:
					s.viewDynamicPics(c, d.Pics)
				}
			})

			// ④ 脚：赞、⋯（原型 `.post-foot`：缝 24，上 12，text-3 13 号）。
			s.viewDynamicActions(c, d)
		})
	})
}

// feedMaxW 是动态流的最宽（原型 `.feed { max-width: 760px }`）。
const feedMaxW = 760

// dynBodyRepeatsCard 报「这一条的正文就是稿件卡里那一句简介」。
//
// ⚠️ **它成立是因为数据那一层的一个约定**：投稿型动态在正文空的时候**用简介
// 顶上**（`bili` 那层，票 #344 的判据要靠它分「什么也没有」和「投稿没有稿件
// 号」）。于是那一句简介在**正文那一行**和**稿件卡里**各出现一次——同一句话
// 说两遍。
//
// 参考图（B 站 UP 主页）上那一句简介是**画在稿件卡里**的，正文那一行是空的
// （上游的 `textContent` 是 `desc?.text || opus?.summary?.text`，投稿动态的
// `desc.text` 通常是空的）。
func dynBodyRepeatsCard(d app.Dynamic) bool {
	if d.Kind != app.DynamicVideo || d.Video == nil || d.Text == "" {
		return false
	}
	// ⚠️ **两边都要去空白才能比**：`bili` 那一层给 `Video.Desc` 的是**原样**
	// （`a.Desc.String()`），而 `Dynamic.Text` 是**去空白**的
	// （`strings.TrimSpace(...)`）——拿原样去比，简介后面多一个空格或换行
	// 时判据就是假的，同一句话照样画两遍（这一处要消掉的正是那个重复）。
	return d.Text == strings.TrimSpace(d.Video.Desc)
}

// viewDynamicActions 是条目**右上角**那一组：赞 + 数，和「⋯」。
//
// 上游是 `CardHeader` 右边那一组（`user-profile/dynamic-list/item.tsx:135-160`）：
// 一枚 `variant="light"` 的赞（18 的拇指图标 + 数字）和一枚「⋯」。
//
// ⚠️ **赞是自己看得见的一枚按钮**，不收进「⋯」菜单（票 #268）：B 站给纯文字
// 和图文也评赞，而那一按是**乐观**的（先点亮再发请求）——收进菜单里就多了一层。
//
// ⚠️ **「下一首播放」「下载音频」收进「⋯」菜单**（上游的 `MoreMenu` 就是这么
// 放的：`dynamic-feed/item.tsx:145-150` 的 `onAddToNext` / `onDownloadAudio`）。
// 它们原来在卡片最下面那一排（正文下面），而参考图上那一排没有它们。
func (s *Shell) viewDynamicActions(c *ui.Context, d app.Dynamic) {
	pl := tok.Of(c.Theme())
	ui.Row(c).Key("dyn-acts:"+d.ID).Gap(space.Px24).AlignItems(ui.Center).
		Margin(space.Px12, space.None, space.None, space.None).Children(func() {
		liked, count := s.dynLikes.get(d.ID, d.Liked, d.LikeCount)
		// 赞：拇指图标 16 + 等宽数字；赞过是实心拇指 + accent-text，没赞是空心 + text-3，
		// 悬停换正文色。点亮是**乐观**的（先点亮再发请求）。
		thumb := "thumb"
		col := pl.Text3
		if liked {
			thumb, col = "thumb-on", pl.AccentText
		}
		btn := ui.ButtonBase(c).Key("dyn-like:" + d.ID).Label("点赞 " + strconv.Itoa(count)).
			Gap(space.Px6).AlignItems(ui.Center)
		if btn.Hovered() && !liked {
			col = pl.Text
		}
		btn.Children(func() {
			ui.Icon(c, pud.Icon(thumb)).FontSize(16).TextColor(col)
			if count > 0 {
				typo.Mono(ui.Text(c, strconv.Itoa(count))).FontSize(scale.Font(c, scale.Size13)).TextColor(col).SingleLine()
			}
		})
		if btn.Clicked() {
			s.toggleDynamicLike(d)
		}
		// 「⋯」：下一首播放 / 下载音频 / 在 B 站打开（只有投稿型动态有）。
		s.viewDynamicMore(c, d)
	})
}

// dynMoreLabel 是「⋯」那一枚的无障碍名字（也是无头 Tester 认得它的凭据）。
const dynMoreLabel = "这条动态的更多操作"

// viewDynamicMore 是「⋯」那一枚：点开一个菜单。
//
// ⚠️ **走 `ui.PopoverBase`**（和头像下拉同一份）：框架那一份自带「点外面关 /
// 按 Esc 关」，自己拼的话得自己实现那两件事——漏了就是面板关不掉。
//
// ⚠️ **开着的是哪一条记在 `Shell.dynMenuFor`**（不是「第几行」）：动态是往
// 尾部追加的，而刷新时新动态会插到**前面**——记行号的话，用户盯着的那一格
// 菜单会突然跑到别的条目上去。
//
// # ⚠️ 菜单**只能用鼠标打开**
//
// 条目在**列表的一行里**，而框架的列表把回车留给「选中那一行」
// （`mygo/ui/list.go:1190` 的 `o.Shortcut(0, KeyEnter)`）——那一行里的按钮
// **按回车没有反应**（实测：焦点停在「⋯」上，回车之后菜单没开；「赞」那一枚
// 同样按回车没反应）。作者名是唯一的例外：它是 `RoleLink`，走的是另一条路
// （`flat.Link` 给它 `Role(ui.RoleLink)`）。
//
// ⚠️ **这不是这一票引入的**：改之前这一行里的「赞」「下一首播放」「下载音频」
// 同样按回车没反应（基线实测：赞数不变、队列没变）。收进菜单只是把「看得见
// 但按不动」变成「看不见也按不动」——**功能上没有进一步变差**。
func (s *Shell) viewDynamicMore(c *ui.Context, d app.Dynamic) {
	btn := flatIconButton(c, iconMore, dynMoreLabel, 16)
	if btn.Clicked() {
		if s.dynMenuOpen && s.dynMenuFor == d.ID {
			s.dynMenuOpen = false
		} else {
			s.dynMenuOpen = true
			s.dynMenuFor = d.ID
		}
	}
	if !s.dynMenuOpen || s.dynMenuFor != d.ID {
		return
	}
	t := c.Theme()
	ui.PopoverBase(c, btn, &s.dynMenuOpen, func(panel ui.Element) {
		popupPanelCard(s, t, panel.
			AttachTo(btn, ui.AnchorBottomEnd, ui.AnchorTopEnd).
			Margin(t.Space(1), space.None, space.None, space.None).
			Width(dynMenuW).
			Padding(space.Px4).
			Label("这条动态的更多操作")).
			Children(func() {
				s.viewDynamicMenuItems(c, d)
			})
	})
}

// dynMenuW 是「⋯」菜单多宽。
const dynMenuW = 168

// viewDynamicMenuItems 是「⋯」菜单里的那几项。
//
// ⚠️ **纯文字和图文那两条只有一项**：B 站音乐库里的音频条目还没有取流通道，
// 「下一首播放」「下载音频」摆出来按不动（理由同 `viewDynamicActions` 原来
// 那一段注释）。
func (s *Shell) viewDynamicMenuItems(c *ui.Context, d app.Dynamic) {
	t := c.Theme()
	items := []dynMenuItem{}
	if d.Kind == app.DynamicVideo && d.Video != nil {
		items = append(items,
			dynMenuItem{label: "下一首播放", icon: iconEnqueue, run: func() {
				s.playNextFromDynamic(d.Video, d.Author)
			}},
			dynMenuItem{label: "下载音频", icon: iconDownload, run: func() {
				s.downloadLater(dynVideoItem(d.Video, d.Author), false, "下载音频")
			}},
			dynMenuItem{label: "在 B 站打开", icon: iconExternal, run: func() {
				s.openBiliVideo(d.Video.Bvid)
			}},
		)
	}
	if len(items) == 0 {
		return
	}
	ui.Column(c).Key("dyn-menu:" + d.ID).FillWidth().Children(func() {
		for _, item := range items {
			row := ui.ButtonBase(c).Key("dyn-menu-item:"+item.label).
				FillWidth().Height(dynMenuRowH).
				Padding(space.None, space.Px12).Gap(space.Px8).
				Justify(ui.Start).Radius(t.Radius)
			row.Children(func() {
				ui.Icon(c, icon(item.icon)).FontSize(dynMenuIconSize).TextColor(t.TextMuted)
				ui.Text(c, item.label).FontSize(scale.Font(c, scale.Size14)).Grow(1)
			})
			if row.Clicked() {
				s.dynMenuOpen = false
				item.run()
			}
		}
	})
}

// dynMenuRowH 是「⋯」菜单里每一项多高（和头像下拉那六项同一档）。
const dynMenuRowH = 32

// dynMenuIconSize 是菜单项左边那一枚图标多大。
const dynMenuIconSize = 16

// dynMenuItem 是「⋯」菜单里的一项。
type dynMenuItem struct {
	label string
	icon  string
	run   func()
}

// openBiliVideo 在浏览器里打开 B 站上那一条稿件。
//
// ⚠️ **打不开要说一句**：静默失败的话，用户按了那一项什么也没发生，而界面上
// 看不出任何异样（菜单还收起来了，像是按对了）。
func (s *Shell) openBiliVideo(bvid string) {
	if bvid == "" {
		s.addToast("这一条没有 B 站地址", notice.Warning)
		return
	}
	if err := systemOpenLink("https://www.bilibili.com/video/" + bvid); err != nil {
		s.addToast("打不开那个链接："+err.Error(), notice.Danger)
	}
}

// playNextFromDynamic 是动态卡片上的「下一首播放」。
//
// ⚠️ **不许换成 `Enqueue`**：那个接在**队尾**，而这一项的字面意思是「下一首就
// 放它」。用户按这一下是因为他听见歌不好，想把某支片子插到眼前这一首后面，
// 而它在队尾的话他得等到整张列表放完——症状是「按了，什么也没发生，直到一小时后」。
func (s *Shell) playNextFromDynamic(v *app.DynamicVideoItem, author app.DynamicAuthor) {
	if err := s.app.EnqueueNext(dynVideoItem(v, author)); err != nil {
		s.addToast("放不了这一首："+err.Error(), notice.Danger)
		return
	}
	// 这一句在两种情况下都是真的：队列里已经有歌时它排在下一位，而队列原本是
	// 空的时候它就是接下来要放的那首（`EnqueueNext` 那时会直接起播）。
	s.addToast("已经加进队列，下一首就放它", notice.Success)
}

// dynamicPicsPerRow 是图文的图一行摆几张。Biu 是 `w-1/3`（一行三张，
// `item.tsx:141` 的图片区块）。
const dynamicPicsPerRow = 3

// viewDynamicPics 画图文的图：**一行三张，满了换下一行**。
//
// ⚠️ 换成多行不是审美，是它原来那一行放不下：九张 × 92 点 + 八道 6 点的缝
// = 876 点，而窗口缩到 `MinWidth` 900 时列表只有 660 点宽——一个不会换行的
// `Row` 把最后几张切在窗口边界外，用户既看不见也点不着（界面不滚动，
// 一张图的方框贴在边上露半截）。
func (s *Shell) viewDynamicPics(c *ui.Context, pics []string) {
	if len(pics) > maxDynamicPics {
		pics = pics[:maxDynamicPics]
	}
	ui.Column(c).Key("dyn-pics").Gap(space.Px6).Children(func() {
		for i := 0; i < len(pics); i += dynamicPicsPerRow {
			ui.Row(c).Key(fmt.Sprintf("dyn-pics-row:%d", i/dynamicPicsPerRow)).Gap(space.Px6).Children(func() {
				for _, p := range pics[i:min(i+dynamicPicsPerRow, len(pics))] {
					s.spaceThumb(c, p, dynamicPicSide, dynamicPicSide)
				}
			})
		}
	})
}

// viewDynamicVideo 画一条投稿型动态里那份稿件。
//
// 排布照上游（`user-profile/dynamic-list/item.tsx:173-215`）：
//
//	<div className="group border-default-200 bg-default-50 hover:bg-default-100
//	              relative mt-2 cursor-pointer overflow-hidden rounded-xl border">
//	  <div className="flex flex-row">
//	    <div className="relative h-32 w-48 shrink-0">  ← 封面 192×128（16:9）
//	      …右下角一个时长角标…
//	    </div>
//	    <div className="flex min-w-0 grow flex-col justify-between p-3">
//	      <div className="space-y-1">
//	        <h3 className="line-clamp-2 text-sm font-medium">标题</h3>
//	        <div className="text-default-500 line-clamp-1 text-xs">描述</div>
//	      </div>
//	      <span className="text-default-400 mt-1 text-xs">播放量</span>
//	    </div>
//	  </div>
//	</div>
//
// ⚠️ **封面是 16:9 的横图**（上游 `h-32 w-48`），不是我们原来那个 96 见方的
// 方块（那是纯音频产品的旧设计，票 #268 改成对齐参考图）。
//
// ⚠️ **行数照参考图**（B 站 UP 主页那张图）：标题 **1 行**、描述 **2 行**。
// 上游那一版是标题 2 行、描述 1 行。
//
// 整块可点：放这一首并立即播，保留原队列（票 #337 定的方向；票 #608 照上游
// `play()` 补上「已经在队列里就定位过去、不重复入队」那一支）。
// 起播失败由 list.Actions.PlayNow 的 Toast 说明。
func (s *Shell) viewDynamicVideo(c *ui.Context, v *app.DynamicVideoItem, author app.DynamicAuthor) {
	pl := tok.Of(c.Theme())
	// 原型 `.embed`：72 封面 | 标题 / 作者 / 读数 | 播放键；内边距 12、缝 16、圆角 12、
	// surface 底、line 描边。整块可点：放这一首并立即播，保留原队列（票 #337 / #608）。
	box := ui.Row(c).Key("dynv:"+v.Bvid).FillWidth().AlignItems(ui.Center).Gap(space.Px16).
		Padding(space.Px12).Radius(tok.R12).Background(pl.Surface).Border(1, pl.Line)
	if box.Clicked() {
		s.listActions().PlayNow(dynVideoItem(v, author))
	}
	box.Children(func() {
		ui.Box(c).Size(72, 72).Radius(tok.R8).Clip().Background(pl.Surface2).Shrink(0).Children(func() {
			s.spaceThumb(c, v.Cover, 72, 72)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, tclean.Clean(v.Title).Title).FontSize(scale.Font(c, scale.Size16)).FontWeight(700).
				TextColor(pl.Text).SingleLine().Ellipsis("…")
			if author.Name != "" {
				ui.Text(c, author.Name).FontSize(scale.Font(c, scale.Size13)).TextColor(pl.Text2).
					SingleLine().Ellipsis("…")
			}
			meta := ""
			if v.Play != "" {
				meta = v.Play + " 播放"
			}
			if v.Duration != "" {
				if meta != "" {
					meta += "  "
				}
				meta += v.Duration
			}
			if meta != "" {
				typo.Mono(ui.Text(c, meta)).FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3).SingleLine()
			}
		})
		if pud.RoundButton(c, "dynv-play:"+v.Bvid, pud.Primary, pud.Std, "play", "播放 "+v.Title).Clicked() {
			s.listActions().PlayNow(dynVideoItem(v, author))
		}
	})
}

// dynVideoMeta 是投稿那一块底下那一行小字（播放数）。
//
// ⚠️ **时长不在这一行**：它现在是封面右下角那个角标（上游那样），两处都画
// 是同一句话说两遍。
func dynVideoMeta(v *app.DynamicVideoItem) string {
	if v.Play == "" {
		return ""
	}
	return v.Play + "收听"
}

// dynVideoItem 把一份动态里的投稿变成队列里的一项。
//
// ⚠️ **author 要单独传进来**：`app.DynamicVideoItem` 里只有稿件，而发这条动态
// 的人挂在 `app.Dynamic.Author` 上（`DynamicAuthor` 名和编号都有）。少传
// 的话从动态流放的歌在播放条上**没有 UP 名**——那一行整个不画，而界面上看
// 不出任何异样（见 `app.Item.Artist`）。
func dynVideoItem(v *app.DynamicVideoItem, author app.DynamicAuthor) app.Item {
	return app.Item{
		Source: "online", Type: "mv", Bvid: v.Bvid, Title: v.Title, Cover: v.Cover,
		Artist:    author.Name,
		ArtistMid: author.Mid,
	}
}

// spaceVideoItem 把「投稿」Tab 的一条变成队列里的一项。
//
// ⚠️ **cid 必须带上**：B 站的取流接口不给 cid 会取到**第一集**，多 P 投稿
// 的第三集放出来是第一集，不报错（见 `bili.UpVideo.Cid`）。
//
// ownerMid 是这一页所属的那个 UP（`fetchVideos` 手上就有）：B 站的投稿接口
// 逐条给了名字（`UpVideo.Author`），可没给编号，而播放条上那一句 UP 名要靠
// 编号拼 `/user/{id}`。
func spaceVideoItem(v app.SpaceVideo, ownerMid int64) app.Item {
	return app.Item{
		Source: "online", Type: "mv", Bvid: v.Bvid, Cid: v.Cid,
		Title: v.Title, Cover: v.Cover,
		Artist:    v.Author,
		ArtistMid: ownerMid,
	}
}

// parseDurationText 把 B 站写的时长（`3:20` / `1:02:03`）读成 time.Duration。
//
// ⚠️ **读不出来给 0，不给一个猜的**：时长在零值时那一列**不画**
// （见 `list.formatDuration`），而一个猜出来的时长会让用户以为这首歌
// 只有三分钟。
func parseDurationText(s string) time.Duration {
	fields := strings.Split(s, ":")
	if len(fields) < 2 || len(fields) > 3 {
		return 0
	}
	total := 0
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return time.Duration(total) * time.Second
}
