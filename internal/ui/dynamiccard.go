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
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrim"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
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
	next := s.dynLikes.flip(d.ID, d.Liked, d.LikeCount)
	s.wake.invalidate()

	s.app.LikeDynamic(d.ID, next.liked, func(err error) {
		if err != nil {
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
	// ⚠️ **通栏**：左右 0、只有一条下边框、透明底、直角。
	//
	// 上游 `border-default-100 mb-2 w-full rounded-none border-b bg-transparent
	// pb-4`（`dynamic-feed/item.tsx:80`）+ CardHeader 的 `pt-4`：
	//   - **左右 0**：那 16（动态流那一页是 16+16 = 32，UP 空间是 `p-4` 的 16）
	//     由页面那一层给——卡片自己再缩 16 的话，同一份内容上游左右各多出一段，
	//     而上游那边是「图左右略宽」的（用户 2026-10-25 说的）；
	//   - 上下各 16（`pt-4` / `pb-4`）；
	//   - **只有一条下边框**（`border-b`）、**透明底**（`bg-transparent`）、
	//     **直角**（`rounded-none`）；
	//   - 距下一条 8（`mb-2`）——**那一段由列表的行间距给**（`upspace_tabs.go`
	//     和 `dynamicfeed.go` 里的 `GapY`）。不给的话卡片自己的下外边距会把
	//     **框架那一层行悬停底**露在卡片底下（就是那条棕带，见下面那段）。
	//
	// # 底下那条棕带是怎么来的，怎么没的
	//
	// 框架给「指针压着的那一行」铺一层 `t.SurfaceHover`（`mygo/ui/list.go` 的
	// styleFn：`w.bg = t.SurfaceHover`）。那一层铺的是**整行**的矩形，而**行比
	// 卡片高**（差着卡片自己的下外边距）——于是卡片**底下**那一条露出了框架
	// 那一层。暖纸主题下 `t.SurfaceHover` 是**淡棕**（`surface2` 往字色推 4%），
	// 用户看到的就是「莫名其妙的一层棕色阴影」（2026-10-25）。
	//
	// ⚠️ **盖掉的是那一层，不是把主题里那一档调淡**：调淡 `t.SurfaceHover` 会
	// 波及全应用（关注页的网格、收藏夹页、下载记录页…）。
	//
	// ⚠️ **盖的办法是「卡片自己刷页面底色」**：卡片现在左右 0、上下 16，正好
	// 填满整行，于是它刷的底色把框架那一层整个盖住——**悬停反馈本身还在**
	// （作者名、赞、下一首播放、下载音频那几个键各自有反馈）。
	card := ui.Column(c).Key("dyn:"+d.ID).Grow(1).
		Padding(dynamicCardPadY, space.None, dynamicCardPadY, space.None).
		// ⚠️ **底色刷成页面底色，是为了盖掉框架那一层悬停底**（见下面那段）。
		Background(t.Background).
		// 🔴 **字色钉回正文色，是无条件的**：框架的列表在**指针点一下**时就把
		// 那一行标记成选中（`mygo/ui/list.go:708` 的 `w.Selected(on)`），那一笔
		// 带着一块主色底和一层**白字**。底被上面那句盖掉了，白字盖不住——卡片里
		// 没写死颜色的字（标题、UP 昵称、行尾那枚「⋯」）会继承它，压在浅底上
		// 就是「点过的那张卡变成白字，看不清」（2026-10-26 用户实测）。在根上
		// 钉一层正文色，继承到的是它；写死了颜色的字（「赞 N」、时间、简介）
		// 不受影响。
		TextColor(t.Text).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).
		Gap(space.Px8)
	// 选中那一层：盖掉框架那块实心主色底，键盘走到它上面时给它一圈焦点环
	//（理由见 `widgets/list/selection.go`）。
	//
	// ⚠️ **`hovered` 传 false**：框架那一层悬停底**已经被卡片自己刷的页面底色
	// 盖掉了**（下面那段），所以「盖上去的颜色」恒是页面底色——传 true 的话，
	// 指针停在一张**被选中**的动态上时，整条卡片会变成淡棕。
	card = list.SelectedRow(card, t, selected, false, ring)
	card.Children(func() {
		// ① 头一行：**左边头像 + 昵称 / 「时间 · 动作」，右边「赞 + 数」与「⋯」**。
		//
		// 排布照上游（`user-profile/dynamic-list/item.tsx:113-160` 的
		// `CardHeader className="flex items-start justify-between px-0 py-2"`）：
		// 左边是 `User`（头像 + 名字 + 下面一行的「时间 · 动作」），右边是
		// 赞和「⋯」那一组。
		ui.Row(c).FillWidth().AlignItems(ui.Start).Children(func() {
			// 左组：头像 + 名字那一列。
			ui.Row(c).Grow(1).Shrink(1).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
				s.spaceAvatar(c, d.Author.Face, dynamicAvatarS, false)
				ui.Column(c).Gap(space.Px2).Children(func() {
					// 昵称是**链接**不是装饰：它是「从这条动态进他空间」这条路。
					//
					// ⚠️ **它是「名字型链接」，不是一枚按钮**（票 #417）：上游那一格是
					// `User` 的 `hover:underline`——**没有按钮壳、没有底色、没有胶囊**。
					// 改之前这里画的是扁平按钮（左右各留一段内边距 + 圆角），于是那个
					// 名字被一个胶囊包着，而指针落上去时胶囊才显出来——像一块底牌。
					//
					// ⚠️ **没有编号时点不动**（`d.Author.Mid > 0`）：那一句仍然显示——
					// 「把名字藏起来是另一种撒谎」（理由同 `widgets/list` 的 `OwnerName`）。
					name := d.Author.Name
					if name == "" {
						name = "这个 UP 主"
					}
					if flatLink(c, name).FontSize(scale.Font(c, scale.Size14)).Clicked() && d.Author.Mid > 0 {
						s.navigate(upPathOf(d.Author.Mid))
					}
					// 「时间 · 动作」那一行（上游 `User` 的 `description`）。
					if d.Time != "" {
						when := d.Time
						if d.Kind == app.DynamicVideo && d.Video != nil {
							when += " · 发布了音乐"
						}
						ui.Text(c, when).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)
					}
				})
			})
			// 右组：赞 + 数，还有「⋯」。
			s.viewDynamicActions(c, d)
		})

		// ② 正文。
		//
		// ⚠️ **投稿型动态的正文与稿件卡里的简介是同一句话时，正文不画**：我们
		// 这一版的投稿动态在正文空的时候**用简介顶上**（`bili` 那层，票 #344 的
		// 判据要靠它），而参考图上那一句简介是**画在稿件卡里**的——两处都画
		// 就是同一句话说两遍（改之前那一张卡也是两处都画，而画出来是「上面
		// 一行字、卡里又一行一样的字」）。
		if d.Text != "" && !dynBodyRepeatsCard(d) {
			ui.Text(c, d.Text).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Copy).MaxLines(8)
		}

		// ③ 内容：三种类型。
		switch {
		case d.Kind == app.DynamicVideo && d.Video != nil:
			s.viewDynamicVideo(c, d.Video, d.Author)
		case len(d.Pics) > 0:
			s.viewDynamicPics(c, d.Pics)
		}
	})
}

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
	t := c.Theme()
	ui.Row(c).Key("dyn-acts:" + d.ID).Gap(space.Px4).AlignItems(ui.Center).Children(func() {
		liked, count := s.dynLikes.get(d.ID, d.Liked, d.LikeCount)
		label := "赞"
		if count > 0 {
			label = "赞 " + strconv.Itoa(count)
		}
		// 上游那一枚是 `variant="light"`（`dynamic-feed/item.tsx:108-116`）：
		// **拇指图标（18，赞过实心/没赞空心）+ 数字**，无边框无底。以前我们只画
		// 「赞 N」两个字的文字键——用户 2026-10-26 点名要回上游那个大拇指。
		// 壳走 `flat.ButtonContent`（和 Button 同一份壳），内容自己拼。
		//
		// 点亮的那一下是**看得见的**（「我赞过了」不是数字变大）：实心拇指 +
		// 主色当字（浅色下取压深的那一档，理由见 `widgets/ink`）；没赞是空心
		// 拇指 + 次要色。
		col := t.TextMuted
		thumb := likeIcon(liked)
		if liked {
			col = ink.AccentOn(t)
		}
		btn := flat.ButtonContent(c, func(c *ui.Context) {
			ui.Icon(c, icon(thumb)).FontSize(18).TextColor(col)
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size12)).TextColor(col).SingleLine()
		})
		if btn.Clicked() {
			s.toggleDynamicLike(d)
		}
		// 「⋯」。
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
	t := c.Theme()
	box := ui.Row(c).Grow(1).AlignItems(ui.Center).Padding(space.Px8).
		Radius(s.cardRadius(t)).Background(s.cardBackground(t)).Border(1, t.Border).
		Clip().Key("dynv:" + v.Bvid)
	if box.Clicked() {
		s.listActions().PlayNow(dynVideoItem(v, author))
	}
	box.Children(func() {
		// ① 封面（16:9）+ 悬停罩上的播放键 + 右下角时长角标。
		//
		// ⚠️ **播放入口是封面上的悬停罩，不是卡右上一枚圆键**（上游
		// `dynamic-feed/item.tsx:166-171`：封面 `absolute inset-0` 一层黑罩，
		// 中间一枚 32 的白色播放三角，`opacity-0 group-hover:opacity-100`——
		// 指针停在**卡上**（`group` 在外层那一格）罩才升起来）。我们原来在卡
		// 最右边画了一枚实心主色圆键（#357 引入的），用户 2026-10-26 点名
		// 「要像上游那样，不是在最右侧」。整块可点不变，这一枚罩只是**看得见的
		// 入口**（和原来那枚圆键同一句话）。
		//
		// ⚠️ **罩的黑白走令牌**：上游是 `rgba(0,0,0,0.4)`，我们取共用的
		// `scrim.Overlay`（35%，图上薄黑那一档，行封面悬停压暗同款）——界面层
		// 不许写裸的遮罩 alpha（`widgets/scrim` 的 `TestNoScrimLiteralIsLeft`）。
		// 白三角和时长角标一样是纯白：它压在黑罩加照片上，主题色读不出来。
		ui.Box(c).Size(dynamicCoverW, dynamicCoverH).Radius(t.Radius).Clip().Children(func() {
			s.spaceThumb(c, v.Cover, dynamicCoverW, dynamicCoverH)
			if box.Hovered() {
				ui.Box(c).Absolute().Size(dynamicCoverW, dynamicCoverH).
					Background(ui.RGBA(0, 0, 0, scrim.Overlay)).Center().Children(func() {
					ui.Icon(c, icon(iconPlayFill)).FontSize(32).TextColor(ui.Color{R: 255, G: 255, B: 255, A: 255})
				})
			}
			// ⚠️ **时长角标是黑底白字**（上游 `bg-black/70 px-1 text-xs
			// text-white`），不是主题色——它压在封面那张照片上，主题色在深色
			// 封面上读不出来。
			//
			// ⚠️ **那 70% 的黑走 `scrim.Badge`**（令牌表里那一档）：界面层不许
			// 出现裸的遮罩 alpha（`widgets/scrim` 的 `TestNoScrimLiteralIsLeft`
			// 盯着这件事）。
			//
			// ⚠️ **画在悬停罩的上面**（上游 z-20 > z-10）：罩升起来也不能把
			// 时长盖掉。
			if v.Duration != "" {
				ui.Box(c).Absolute().Right(space.Px4).Bottom(space.Px4).
					Padding(space.None, space.Px4).Radius(space.Px4).
					Background(ui.RGBA(0, 0, 0, scrim.Badge)).Children(func() {
					ui.Text(c, v.Duration).FontSize(scale.Font(c, scale.Size12)).TextColor(ui.Color{R: 255, G: 255, B: 255, A: 255})
				})
			}
		})
		// ② 右边那一列：标题 + 描述 + 播放量。
		ui.Column(c).Grow(1).Shrink(1).Padding(space.Px12).Gap(space.Px4).Children(func() {
			ui.Text(c, tclean.Clean(v.Title).Title).FontSize(scale.Font(c, scale.Size14)).
				FontWeight(weightMedium).MaxLines(1).Ellipsis("…")
			if v.Desc != "" {
				ui.Text(c, v.Desc).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).
					MaxLines(2).Ellipsis("…")
			}
			if meta := dynVideoMeta(v); meta != "" {
				ui.Text(c, meta).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)
			}
		})
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
