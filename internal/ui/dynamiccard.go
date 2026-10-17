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
