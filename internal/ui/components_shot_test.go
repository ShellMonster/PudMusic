// 「深夜唱片行」公共组件陈列（T0 验收，DESIGN.md §0′）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderComponentShowcase
//
// 把 `widgets/pud` 里每一个组件在深浅两套主题下各摆一遍，拿来和原型 `ref/` 里
// 对应的元素逐项对照（布局 / 间距 / 层级 / 密度 / 对齐）。封面一律是合成的色块，
// 不用任何真实专辑图。
package ui

import (
	"image/color"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	mygoui "github.com/egoist/mygo/ui"
)

// synthCover 是合成封面：一块纯色，上面一道浅色对角块，足够看出「完整显示、不裁切」。
func synthCover(base color.RGBA) pud.Cover {
	return func(c *mygoui.Context, w, h, radius float32) {
		mygoui.Box(c).Size(w, h).Radius(radius).Clip().Background(mygoui.RGB(base.R, base.G, base.B)).Draw(
			func(p *mygoui.Painter, r mygoui.Rect) {
				p.Fill(mygoui.Rect{X: r.X + r.W*0.2, Y: r.Y + r.H*0.2, W: r.W * 0.6, H: r.H * 0.6},
					mygoui.RGBA(255, 255, 255, 0.18), 6)
			})
	}
}

// showcaseSection 画一节的小标题（只给出图用，不是组件）。
func showcaseSection(c *mygoui.Context, name string) {
	p := tok.Of(c.Theme())
	mygoui.Text(c, name).FontSize(12).TextColor(p.Text3).Margin(space.Px32, 0, space.Px12, 0)
}

// showcaseView 是陈列页：左列按钮 / Chip / 角标 / 开关 / Tab / 头部，右列卡片，下面
// 曲目行 / 空状态 / 弹层 / Toast / 对话框。
func showcaseView(s *Shell, on *bool, tab *int, search *string) func(*mygoui.Context) {
	return func(c *mygoui.Context) {
		s.applyFrameTheme(c)
		p := tok.Of(c.Theme())
		covers := []color.RGBA{{129, 59, 43, 255}, {75, 85, 71, 255}, {126, 155, 170, 255}, {64, 63, 50, 255}}
		mygoui.Column(c).FillWidth().FillHeight().Background(p.Bg).Children(func() {
			mygoui.Scroll(c).Key("showcase").Grow(1).Children(func() {
				mygoui.Column(c).Width(1100).Padding(space.Px8, 40, 64, 40).Children(func() {
					pud.PageHead(c, "Library", "本地音乐", "把电脑里的歌拖进来，或者选一个文件夹，它们会一直在这里。", 1100, func() {
						pud.Button(c, "imp", pud.Ghost, pud.Std, "导入", "import")
						pud.Button(c, "play-all", pud.Primary, pud.Std, "播放全部", "play")
					})

					showcaseSection(c, "BUTTONS  primary · ghost · quiet · danger · sm · round")
					mygoui.Row(c).Gap(12).AlignItems(mygoui.Center).Children(func() {
						pud.Button(c, "b1", pud.Primary, pud.Std, "播放", "play")
						pud.Button(c, "b2", pud.Ghost, pud.Std, "稍后再看", "later")
						pud.Button(c, "b3", pud.Quiet, pud.Std, "清空", "trash")
						pud.Button(c, "b4", pud.Danger, pud.Std, "删除", "")
						pud.Button(c, "b5", pud.Primary, pud.Sm, "关注", "")
						pud.Button(c, "b6", pud.Ghost, pud.Sm, "已关注", "check")
						pud.RoundButton(c, "b7", pud.Primary, pud.Std, "play", "播放")
						pud.RoundButton(c, "b8", pud.Ghost, pud.Sm, "plus", "新建")
						pud.Button(c, "b9", pud.Primary, pud.Std, "禁用", "").Disabled(true)
					})

					showcaseSection(c, "ICON-BTN  36 · row 32 · .on · .cnt  /  CHIP  /  BADGE  /  KBD  /  SWITCH")
					mygoui.Row(c).Gap(12).AlignItems(mygoui.Center).Children(func() {
						pud.IconBtn(c, "i1", "search", "搜索", pud.IconOpts{})
						pud.IconBtn(c, "i2", "queue", "队列", pud.IconOpts{Count: "12"})
						pud.IconBtn(c, "i3", "heart-on", "喜欢", pud.IconOpts{On: true})
						pud.IconBtn(c, "i4", "more", "更多", pud.IconOpts{Row: true})
						pud.Chip(c, "c1", "全部", "", true)
						pud.Chip(c, "c2", "最近 7 天", "calendar", false)
						pud.Chip(c, "c3", "音乐区", "", false)
						pud.Badge(c, "HI-RES", true)
						pud.Badge(c, "SQ", false)
						pud.Kbd(c, "⌘K")
						pud.Switch(c, "sw1", on, "开关")
						off := false
						pud.Switch(c, "sw2", &off, "关")
					})

					showcaseSection(c, "TABS  underline")
					pud.Tabs(c, "tabs", tab, []pud.Tab{{Label: "作品", Count: "128"}, {Label: "动态", Count: "24"}, {Label: "合集"}})

					showcaseSection(c, "SEC-HEAD  +  CARD GRID (6 cols)")
					pud.SecHead(c, "more", "每日推荐", "根据你最近的收听，每天 6 首", "更多")
					cw := pud.CellWidth(1020, 6, 24)
					mygoui.Row(c).Gap(24).Children(func() {
						for i, t := range []string{"叶惠美", "范特西", "寓言", "U87", "自传", "小宇宙"} {
							cov := synthCover(covers[i%len(covers)])
							if i == 5 {
								pud.ColCard(c, "card5", "我喜欢的音乐", "收藏夹 · 128 首", cw, nil)
								continue
							}
							pud.CoverCard(c, "card"+t, t, "周杰伦", cw, cov, i == 1)
						}
					})

					showcaseSection(c, "TRACK LIST  head · normal · playing · hover-less · liked · HI-RES · SQ")
					pud.TrackHead(c, 1020)
					tracks := []pud.Track{
						{Index: "1", Title: "晴天", Artist: "周杰伦", Album: "叶惠美", Owner: "杰伦官方", Quality: "HI-RES", Plays: "1.2亿", Danmaku: "34万", Duration: "4:29", Cover: synthCover(covers[0]), CanLike: true, Liked: true},
						{Index: "2", Title: "夜航星", Artist: "不才", Album: "夜航星", Owner: "夜航电台", Quality: "SQ", Plays: "823万", Danmaku: "5.1万", Duration: "3:47", Cover: synthCover(covers[1]), Playing: true, CanLike: true},
						{Index: "3", Title: "本地文件.mp3", Artist: "未知艺术家", Album: "本地文件", Duration: "2:58", Cover: synthCover(covers[2])},
					}
					for i, t := range tracks {
						pud.TrackRow(c, "tr"+t.Index, t, 1020)
						_ = i
					}

					showcaseSection(c, "EMPTY  +  LOGIN PROMPT")
					pud.Empty(c, pud.ArtClock, "还没有稍后再看", "在视频卡片上点「稍后再看」，之后可以在这里集中看。", func() {
						pud.Button(c, "go", pud.Primary, pud.Std, "去首页逛逛", "")
					})
					pud.LoginPrompt(c, "lp", "我的关注")

					showcaseSection(c, "SKELETON  /  MINI-SEARCH  /  SELECT  /  POP  /  TOAST")
					pud.SkeletonRow(c, "sk1", 1020)
					mygoui.Row(c).Gap(24).AlignItems(mygoui.Start).Children(func() {
						pud.MiniSearch(c, "ms", search, "在列表里找", 0)
						pud.SelectBox(c, "sel", "最新发布", 160)
						pop := pud.PopPanel(c, "pop")
						pop.Children(func() {
							pud.PopHeading(c, "音质")
							pud.PopOpt(c, "o1", "Hi-Res", "", false)
							pud.PopOpt(c, "o2", "无损", "FLAC", true)
							pud.PopOpt(c, "o3", "高品质", "320K", false)
						})
						mygoui.Column(c).Gap(8).Children(func() {
							pud.Toast(c, "t1", "已加入队列", "check", "")
							pud.Toast(c, "t2", "已移除「晴天」", "", "撤销")
						})
					})

					showcaseSection(c, "DIALOG")
					d := pud.DialogPanel(c, "dlg")
					d.Children(func() {
						pud.DialogBody(c, "删除这个收藏夹？", "删除之后收藏夹里的 128 首歌不会被删除，只是从这个收藏夹里移走。")
						pud.DialogFoot(c, func() {
							pud.Button(c, "d1", pud.Ghost, pud.Std, "取消", "")
							pud.Button(c, "d2", pud.Danger, pud.Std, "删除", "")
						})
					})

					showcaseSection(c, "TYPE  eyebrow · hero 64 · display 48 / 32 · tnum")
					typo.Eyebrow(c, "Album of the day")
					typo.Display(mygoui.Text(c, "叶惠美"), 64)
					typo.Display(mygoui.Text(c, "晚上好，阿布"), 48)
					typo.Num(mygoui.Text(c, "0:57  4:29  1:32:08  1.2万  3,456")).FontSize(14).TextColor(p.Text2)
				})
			})
		})
	}
}

// TestRenderComponentShowcase 把组件陈列存成 PNG（深浅各一张）。
func TestRenderComponentShowcase(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			on, tab, search := true, 0, ""
			b := newTestShell(t, at(1180, 2900), systemIsDark(dark),
				withView(func(s *Shell) func(*mygoui.Context) { return showcaseView(s, &on, &tab, &search) }))
			b.tt.Frame()
			for range 30 {
				if !b.tt.FramePending() {
					break
				}
				b.tt.Advance(16e6)
			}
			save(t, dir, "components-"+name+".png", b.tt)
		})
	}
}

// TestRenderHomeWithTheNewTheme 出一张首页现状图（T0 验收）：确认「深夜唱片行」主题已经
// 生效。布局还是旧的（由 T1 / T2 重做），这张图只看颜色、标题字体和图标。
func TestRenderHomeWithTheNewTheme(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			_, tt, _ := shellShotShell(t, dark)
			save(t, dir, "home-now-"+name+".png", tt)
		})
	}
}
