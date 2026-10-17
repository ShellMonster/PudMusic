// 动态卡片与收藏夹卡片：UP 空间和动态流两页共用。
//
// **两页画的是同一样东西**（Biu 上游也是同一个组件：动态流的
// `dynamic-feed/item.tsx` 和 UP 空间的 `user-profile/dynamic-list/item.tsx`
// 结构一样）。写一份在这里，两边就不会画出两种样子——
// 「在动态流里点得开、在空间里点不开」是最难查的那一类不一致。
package ui

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// dynamicCoverW / dynamicCoverH 是投稿那一块的封面尺寸。
//
// Biu 那边是 `h-32 w-48`（`dynamic-feed/item.tsx:118`），比例 1.5:1。
const (
	dynamicCoverW = 132
	dynamicCoverH = 88
)

// dynamicPicSide 是图文里每张图的边长。
const dynamicPicSide = 92

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
			s.addToast(likeFailText(next.liked, err))
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
func (s *Shell) viewCollectionRow(c *ui.Context, col bili.UpCollection) {
	t := c.Theme()
	box := ui.Row(c).Grow(1).Gap(10).Padding(8).AlignItems(ui.Center).
		Key("coll:" + strconv.FormatInt(col.ID, 10))
	if box.Clicked() {
		s.navigate(collectionPath(col.ID))
	}
	box.Children(func() {
		s.spaceThumb(c, col.Cover, 48, 64)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, col.Title).FontSize(14).MaxLines(2).Ellipsis("…")
			ui.Text(c, fmt.Sprintf("%d 条", col.Count)).FontSize(12).TextColor(t.TextMuted)
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
func (s *Shell) viewDynamicCard(c *ui.Context, d bili.Dynamic) {
	t := c.Theme()
	ui.Column(c).Key("dyn:" + d.ID).Grow(1).Padding(10).Gap(6).Children(func() {
		// ① 头一行：头像 + 昵称（可点）+ 时间。
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			s.spaceAvatar(c, d.Author.Face, 32)
			name := d.Author.Name
			if name == "" {
				name = "这个 UP 主"
			}
			// 昵称是**链接**不是装饰：它是「从这条动态进他空间」这条路。
			if ui.Button(c, name).FontSize(14).Clicked() && d.Author.Mid > 0 {
				s.navigate(upPathOf(d.Author.Mid))
			}
			if d.Time != "" {
				ui.Text(c, d.Time).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
			}
		})

		// ② 正文。
		if d.Text != "" {
			ui.Text(c, d.Text).FontSize(13).LineHeight(1.5).MaxLines(8)
		}

		// ③ 内容：三种类型。
		switch {
		case d.Kind == bili.DynamicVideo && d.Video != nil:
			s.viewDynamicVideo(c, d.Video)
		case len(d.Pics) > 0:
			s.viewDynamicPics(c, d.Pics)
		}

		// ④ 那一排动作：赞 +（投稿型的）下载音频 / 下载视频。
		//
		// ⚠️ **「下一首播放」不在这四项里**：队列现在只有「追加到队尾」和
		// 「从这一首往后换队列」两种（`app.Enqueue` / `app.PlayFrom`），没有
		// 「插到当前这一首后面」。那一项是 A9 那张票的事；在它落地之前摆一个
		// 按下去只是加到队尾的按钮，用户会以为它排在了正在放的那首后面——
		// 而它排在队尾。这比不摆更坏（见 `later.go` 里「清除已看完」为什么
		// 故意不摆，同一个理由）。
		s.viewDynamicActions(c, d)
	})
}

// viewDynamicActions 是卡片最下面那一排按钮。
//
// 项照 Biu 的 `src/pages/dynamic-feed/item.tsx:112`（点赞）、`:134`/`:135`
// （下载音频 / 下载视频）：
//
//   - **赞**对三种卡片都有——B 站给纯文字和图文也评赞。
//   - **下载**只有投稿型有：B 站音乐库里的音频条目没有取流通道（Biu 那边的
//     下载按钮也只挂在视频上），摆出来按不动。
func (s *Shell) viewDynamicActions(c *ui.Context, d bili.Dynamic) {
	t := c.Theme()
	ui.Row(c).Key("dyn-acts:" + d.ID).Gap(6).Children(func() {
		liked, count := s.dynLikes.get(d.ID, d.Liked, d.LikeCount)
		label := "赞"
		if count > 0 {
			label = "赞 " + strconv.Itoa(count)
		}
		btn := ui.Button(c, label).FontSize(12)
		if liked {
			// 点亮的那一下是**看得见的**（「我赞过了」不是数字变大）。
			btn = btn.TextColor(t.Accent)
		} else {
			btn = btn.TextColor(t.TextMuted)
		}
		if btn.Clicked() {
			s.toggleDynamicLike(d)
		}

		if d.Kind != bili.DynamicVideo || d.Video == nil {
			return
		}
		// ⚠️ 下载走 `downloadLater`（异步补 cid）：动态接口给的稿件**不带
		// cid**（`major.archive` 里没有这个字段），而下载按分 P 存文件。
		if ui.Button(c, "下载音频").FontSize(12).TextColor(t.TextMuted).Clicked() {
			s.downloadLater(dynVideoItem(d.Video), false, "下载音频")
		}
		if ui.Button(c, "下载视频").FontSize(12).TextColor(t.TextMuted).Clicked() {
			s.downloadLater(dynVideoItem(d.Video), true, "下载视频")
		}
	})
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
	ui.Column(c).Key("dyn-pics").Gap(6).Children(func() {
		for i := 0; i < len(pics); i += dynamicPicsPerRow {
			ui.Row(c).Key(fmt.Sprintf("dyn-pics-row:%d", i/dynamicPicsPerRow)).Gap(6).Children(func() {
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
func (s *Shell) viewDynamicVideo(c *ui.Context, v *bili.DynamicArchive) {
	t := c.Theme()
	box := ui.Row(c).Grow(1).Gap(8).Padding(6).Radius(6).
		Background(t.Surface).AlignItems(ui.Center).Key("dynv:" + v.Bvid)
	if box.Clicked() {
		list.AppActions(s.app, s.addToast).Play([]app.Item{dynVideoItem(v)})
	}
	box.Children(func() {
		s.spaceThumb(c, v.Cover, dynamicCoverW, dynamicCoverH)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, v.Title).FontSize(14).MaxLines(2).Ellipsis("…")
			if v.Desc != "" {
				ui.Text(c, v.Desc).FontSize(12).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
			}
			if meta := dynVideoMeta(v); meta != "" {
				ui.Text(c, meta).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
			}
		})
	})
}

// dynVideoMeta 是投稿那一块底下那一行小字（播放数 + 时长）。
func dynVideoMeta(v *bili.DynamicArchive) string {
	meta := ""
	if v.Play != "" {
		meta = v.Play + "观看"
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
func dynVideoItem(v *bili.DynamicArchive) app.Item {
	return app.Item{Source: "online", Type: "mv", Bvid: v.Bvid, Title: v.Title, Cover: v.Cover}
}

// spaceVideoItem 把「投稿」Tab 的一条变成队列里的一项。
//
// ⚠️ **cid 必须带上**：B 站的取流接口不给 cid 会取到**第一集**，多 P 投稿
// 的第三集放出来是第一集，不报错（见 `bili.UpVideo.Cid`）。
func spaceVideoItem(v bili.UpVideo) app.Item {
	return app.Item{Source: "online", Type: "mv", Bvid: v.Bvid, Cid: v.Cid, Title: v.Title, Cover: v.Cover}
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
