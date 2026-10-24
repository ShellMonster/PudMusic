// 动态卡片与收藏夹卡片：UP 空间和动态流两页共用。
//
// **两页画的是同一样东西**（Biu 上游也是同一个组件：动态流的
// `dynamic-feed/item.tsx` 和 UP 空间的 `user-profile/dynamic-list/item.tsx`
// 结构一样）。写一份在这里，两边就不会画出两种样子——
// 「在动态流里点得开、在空间里点不开」是最难查的那一类不一致。
package ui

import (
	"fmt"
	tclean "github.com/ShellMonster/Biu_Go/internal/ui/widgets/title"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/leading"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// dynamicCoverW / dynamicCoverH 是投稿那一块的封面尺寸：纯音频产品，封面是**方的**
// （设计规范 v0.1 §7.17，取代上游 Biu 的 192×128 横图缩略图）。
const (
	dynamicCoverW = 96
	dynamicCoverH = 96
)

// dynamicPlayBtn 是投稿卡右边那枚播放键的直径；它是正圆，所以圆角是直径的一半，不跟主题圆角走。
const dynamicPlayBtn = 36

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
// 没动过的那些不在这儿：读的是 B 站拉回来的那份（`bili.Dynamic.Liked`）。
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
func (s *Shell) toggleDynamicLike(d bili.Dynamic) {
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
func (s *Shell) viewCollectionRow(c *ui.Context, col bili.UpCollection, selected, ring bool) {
	t := c.Theme()
	box := ui.Row(c).Grow(1).Gap(space.Px12).Padding(space.Px12).AlignItems(ui.Center).
		Margin(space.None, space.None, space.Px8, space.None).
		Background(s.cardBackground(t)).Radius(s.cardRadius(t)).Border(1, t.Border).
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
// 内容那一块按类型分三种（见 `bili.DynamicKind`）。
//
// 🔴 **作者名可点进他的空间**（上游 `dynamic-feed/item.tsx:88-90` 跳
// `/user/{mid}`）。这是「能点进对应 UP 空间」那条验收在卡片上的落点。
func (s *Shell) viewDynamicCard(c *ui.Context, d bili.Dynamic, selected, ring bool) {
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
		// ① 头一行：头像 + 昵称（可点）+ 时间。
		ui.Row(c).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
			s.spaceAvatar(c, d.Author.Face, 40, false)
			name := d.Author.Name
			if name == "" {
				name = "这个 UP 主"
			}
			// 昵称是**链接**不是装饰：它是「从这条动态进他空间」这条路。
			// 扁平配方（上游那一格是 `User` 的 `hover:underline`，没有按钮壳）。
			if flatButton(c, name).FontSize(scale.Font(c, scale.Size14)).Clicked() && d.Author.Mid > 0 {
				s.navigate(upPathOf(d.Author.Mid))
			}
			if d.Time != "" {
				when := d.Time
				if d.Kind == bili.DynamicVideo && d.Video != nil {
					when += " · 发布了音乐"
				}
				ui.Text(c, when).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)
			}
		})

		// ② 正文。
		if d.Text != "" {
			ui.Text(c, d.Text).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Copy).MaxLines(8)
		}

		// ③ 内容：三种类型。
		switch {
		case d.Kind == bili.DynamicVideo && d.Video != nil:
			s.viewDynamicVideo(c, d.Video, d.Author)
		case len(d.Pics) > 0:
			s.viewDynamicPics(c, d.Pics)
		}

		// ④ 那一排动作：赞 +（投稿型的）下一首播放 / 下载音频 / 下载视频。
		s.viewDynamicActions(c, d)
	})
}

// viewDynamicActions 是卡片最下面那一排按钮。
//
// 项照 Biu 的 `src/pages/dynamic-feed/item.tsx:112`（点赞）、`:118`（下一首播放）、
// `:134`/`:135`（下载音频 / 下载视频）：
//
//   - **赞**对三种卡片都有——B 站给纯文字和图文也评赞。
//   - 其余三项只有投稿型有：纯文字和图文里没有可放的东西（B 站音乐库里的音频
//     条目还没有取流通道），摆出来按不动。
func (s *Shell) viewDynamicActions(c *ui.Context, d bili.Dynamic) {
	t := c.Theme()
	ui.Row(c).Key("dyn-acts:" + d.ID).Gap(space.Px6).Children(func() {
		liked, count := s.dynLikes.get(d.ID, d.Liked, d.LikeCount)
		label := "赞"
		if count > 0 {
			label = "赞 " + strconv.Itoa(count)
		}
		// 上游那一枚是 `variant="light"`（`dynamic-feed/item.tsx:112`）：图标 +
		// 数字，无边框无底。我们这一枚还带着数字（「赞 12」），所以保留文字键的
		// 形状，只换掉那层壳——图标化会丢掉「赞过几次」这个用户要看的数。
		btn := flatButton(c, label).FontSize(scale.Font(c, scale.Size12))
		if liked {
			// 点亮的那一下是**看得见的**（「我赞过了」不是数字变大）。
			// 主色当**字**：浅色下换成与底色 ≥4.5:1 的变体（理由见
			// `widgets/ink`）。
			btn = btn.TextColor(ink.AccentOn(t))
		} else {
			btn = btn.TextColor(t.TextMuted)
		}
		if btn.Clicked() {
			s.toggleDynamicLike(d)
		}

		if d.Kind != bili.DynamicVideo || d.Video == nil {
			return
		}
		if flatButton(c, "下一首播放").FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).Clicked() {
			s.playNextFromDynamic(d.Video, d.Author)
		}
		// ⚠️ 下载走 `downloadLater`（异步补 cid）：动态接口给的稿件**不带
		// cid**（`major.archive` 里没有这个字段），而下载按分 P 存文件。
		if flatButton(c, "下载音频").FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).Clicked() {
			s.downloadLater(dynVideoItem(d.Video, d.Author), false, "下载音频")
		}
	})
}

// playNextFromDynamic 是动态卡片上的「下一首播放」。
//
// ⚠️ **不许换成 `Enqueue`**：那个接在**队尾**，而这一项的字面意思是「下一首就
// 放它」。用户按这一下是因为他听见歌不好，想把某支片子插到眼前这一首后面，
// 而它在队尾的话他得等到整张列表放完——症状是「按了，什么也没发生，直到一小时后」。
func (s *Shell) playNextFromDynamic(v *bili.DynamicArchive, author bili.DynamicAuthor) {
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
// **整块可点，点它从这一首往后播**——和列表里双击一首是一个意思。
// 真放不了的时候说一句话（`list.Actions.Play` 里的 Toast），不装死。
func (s *Shell) viewDynamicVideo(c *ui.Context, v *bili.DynamicArchive, author bili.DynamicAuthor) {
	t := c.Theme()
	box := ui.Row(c).Grow(1).Gap(space.Px12).Padding(space.Px8).Radius(t.Radius).
		Background(t.Surface).AlignItems(ui.Center).Key("dynv:" + v.Bvid)
	if box.Clicked() {
		list.AppActions(s.app, s.addToast).Play([]app.Item{dynVideoItem(v, author)})
	}
	box.Children(func() {
		s.spaceThumb(c, v.Cover, dynamicCoverW, dynamicCoverH)
		ui.Column(c).Grow(1).Gap(space.Px4).Children(func() {
			ui.Text(c, tclean.Clean(v.Title).Title).FontSize(scale.Font(c, scale.Size14)).MaxLines(2).Ellipsis("…")
			if v.Desc != "" {
				ui.Text(c, v.Desc).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
			}
			if meta := dynVideoMeta(v); meta != "" {
				ui.Text(c, meta).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)
			}
		})
		// 右边一枚实心强调色圆的播放键（整块也可点，这一枚是看得见的入口）。
		ui.Box(c).Size(dynamicPlayBtn, dynamicPlayBtn).Radius(dynamicPlayBtn / 2).Background(t.Accent).Center().Children(func() {
			ui.Icon(c, icon(iconPlayFill)).FontSize(18).TextColor(t.AccentText)
		})
	})
}

// dynVideoMeta 是投稿那一块底下那一行小字（播放数 + 时长）。
func dynVideoMeta(v *bili.DynamicArchive) string {
	meta := ""
	if v.Play != "" {
		meta = v.Play + "收听"
	}
	if v.Duration != "" {
		if meta != "" {
			meta += " · "
		}
		meta += v.Duration
	}
	return meta
}

// dynVideoItem 把一份动态里的投稿变成队列里的一项。
//
// ⚠️ **author 要单独传进来**：`bili.DynamicArchive` 里只有稿件，而发这条动态
// 的人挂在 `bili.Dynamic.Author` 上（`DynamicAuthor` 名和编号都有）。少传
// 的话从动态流放的歌在播放条上**没有 UP 名**——那一行整个不画，而界面上看
// 不出任何异样（见 `app.Item.Artist`）。
func dynVideoItem(v *bili.DynamicArchive, author bili.DynamicAuthor) app.Item {
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
func spaceVideoItem(v bili.UpVideo, ownerMid int64) app.Item {
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
