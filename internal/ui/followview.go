// 我的关注：左边分组栏、右边搜索 + 两列 UP 主卡片（「深夜唱片行」，原型 `PAGES.follow` /
// CSS `.split` `.group-col` `.ucard`，DESIGN.md §0′.T3）。取数、翻页、分组、搜索、取关的
// 机器都在 `follow.go`，这里只管长什么样。
package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/skeleton"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// 原型 `.split { grid-template-columns: 200px 1fr; gap: 40px; margin-top: 24px }`。
const (
	followGroupW  float32 = 200
	followCardGap float32 = 16
)

// followColumnsFor 是卡片排几列：原型 `--od-cols:2` 是行内样式，任何窗口宽都是两列。
func followColumnsFor(w float32) int { return 2 }

// viewFollowSplit 是页头下面那一整块：左分组栏，右工具条 + 网格。
func (s *Shell) viewFollowSplit(c *ui.Context, f *followPage) {
	right := func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).FillHeight().Children(func() {
			s.viewFollowToolbar(c, f)
			s.viewFollowGrid(c, f)
		})
	}
	// 窗口 ≤ 1024：`.split` 变单列（缝 24），分组栏变成一排横着的按钮、没有「分组」小标题和分隔线。
	if windowW(c) <= 1024 {
		ui.Column(c).Key("follow:split").Grow(1).Gap(space.Px24).
			Margin(space.Px24, space.None, space.None, space.None).Children(func() {
			s.viewFollowGroups(c, f)
			right()
		})
		return
	}
	ui.Row(c).Key("follow:split").Grow(1).Gap(space.Px40).AlignItems(ui.Start).
		Margin(space.Px24, space.None, space.None, space.None).Children(func() {
		s.viewFollowGroups(c, f)
		right()
	})
}

// viewFollowGroups 是左边那一栏（原型 `.group-col`：宽 200，项距 2；「分组」小标题 12 号
// text-3，项 8/12 内边距、圆角 8、14 号次要色，计数等宽 12 号 text-3；当前项 surface-2 底
// 正文色 600；「全部关注」和分组之间一道 1 像素分隔线）。
//
// 分组还在拉 / 拉失败 / 还没有分组，三句话一句都不能少：B 站返回空数组和「我们没拉到」
// 是两件事。
func (s *Shell) viewFollowGroups(c *ui.Context, f *followPage) {
	pl := tok.Of(c.Theme())
	narrow := windowW(c) <= 1024
	var box ui.Element
	if narrow {
		box = ui.Row(c).Key("follow:groups").FillWidth().Wrap().Gap(space.Px8).AlignItems(ui.Center)
	} else {
		box = ui.Column(c).Key("follow:groups").Width(followGroupW).Shrink(0).Gap(space.Px2)
	}
	box.Children(func() {
		if !narrow {
			ui.Text(c, "分组").FontSize(scale.Font(c, scale.Size12)).FontWeight(500).TextColor(pl.Text3).
				Padding(space.None, space.Px12, space.Px8, space.Px12)
		}
		item := func(tab, label string, n int) {
			on := f.tab == tab
			b := ui.ButtonBase(c).Key("follow:group:"+tab).Label(label).
				Padding(space.Px8, space.Px12).Radius(tok.R8).AlignItems(ui.Center).Gap(space.Px8)
			if !narrow {
				b = b.FillWidth()
			}
			fg := pl.Text2
			switch {
			case on:
				b, fg = b.Background(pl.Surface2), pl.Text
			case b.Hovered():
				b, fg = b.Background(pl.Hover), pl.Text
			}
			b = b.Transition(motion.Fade)
			b.Children(func() {
				t := ui.Text(c, label).FontSize(scale.Font(c, scale.Size14)).TextColor(fg).SingleLine().Ellipsis("…").Grow(1)
				if on {
					t.FontWeight(600)
				}
				if n > 0 {
					typo.Mono(ui.Text(c, fmt.Sprint(n))).FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3).SingleLine()
				}
			})
			if b.Clicked() && !on {
				f.tab = tab
				s.applyFollowTab(f)
			}
		}
		note := func(txt string) {
			ui.Text(c, txt).FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3).
				Padding(space.Px4, space.Px12).MaxLines(2).Ellipsis("…")
		}
		item(followTabAll, "全部关注", f.total)
		switch {
		case !f.tagStarted && f.tagErr != nil:
			note("分组没拉出来：" + f.tagErr.Error())
			if pud.Button(c, "follow:tag-retry", pud.Quiet, pud.Sm, "重试拉分组", "refresh").Clicked() {
				f.tagErr, f.tagLoading = nil, true
				go s.fetchFollowTags(context.Background(), f)
			}
			return
		case !f.tagStarted:
			note("正在加载分组…")
			return
		}
		if !narrow {
			ui.Box(c).FillWidth().Height(1).Background(pl.Line).Margin(space.Px8, space.Px12, space.Px8, space.Px12)
		}
		for _, tg := range f.tags {
			name := tg.Name
			if name == "" {
				name = "这一组"
			}
			item(followTagTab(tg.ID), name, tg.Count)
		}
		if len(f.tags) == 0 {
			// 一个分组都没有也要说一句，不然用户以为整栏没拉出来。
			note("还没有分组")
		}
	})
}

// viewFollowToolbar 是右边顶上的搜索（原型 `liveInput('follow-q')`：32 高胶囊、180 宽输入）。
//
// 只在「全部关注」画：B 站的搜索接口搜的是关注的全集，搜不出某一组里的谁。回车才搜、
// 不防抖（上游就是这样）。排序下拉没有做：关注接口里没有粉丝数 / 获赞数可排。
func (s *Shell) viewFollowToolbar(c *ui.Context, f *followPage) {
	if f.tab != followTabAll {
		return
	}
	pl := tok.Of(c.Theme())
	ui.Row(c).Key("follow-search").FillWidth().Gap(space.Px12).AlignItems(ui.Center).
		Margin(space.None, space.None, space.Px16, space.None).Children(func() {
		in := pud.MiniSearch(c, "follow:q", &f.draft, followSearchLabel, 180)
		if in.Submitted() {
			s.searchFollow(f, strings.TrimSpace(f.draft))
		}
		if f.applied != "" {
			ui.Text(c, "正在搜「"+f.applied+"」").FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3).
				MaxLines(1).Ellipsis("…")
			if pud.Button(c, "follow:q-clear", pud.Quiet, pud.Sm, "清空搜索", "").Clicked() {
				f.draft = ""
				s.searchFollow(f, "")
			}
		}
	})
}

// viewFollowCard 画一张 UP 主卡（原型 `.ucard`，`pud.UCard`）。整张卡点一下进他的空间；
// 右边的「已关注」是幽灵键，点了取消关注（后台跑，下一帧开头收结果，见 `unfollowFromFollowCard`）。
//
// 原型卡上有 LV 角标和「粉丝 · 获赞」一行，关注接口不给这两样：LV 不画，那一行写认证说明
// （`official_verify.desc`，有就写）。头像底色用 mid 派生的一个稳定色。
func (s *Shell) viewFollowCard(c *ui.Context, u app.FollowUser, w float32) {
	ev := pud.UCard(c, followKeyOf(u.Mid), pud.UpCard{
		Name: u.Name, Sign: u.Sign, Stats: u.Official,
		Tint: faceTint(u.Mid), Face: s.upFaceDraw(u.Face), Following: true,
	}, w)
	switch {
	case ev.Follow:
		s.unfollowFromFollowCard(u)
	case ev.Open && u.Mid > 0:
		s.navigate(upPathOf(u.Mid))
	}
}

// faceTint 是没有头像图时的头像底色：按 mid 在几个压低了的色相里挑一个（稳定，同一个人
// 永远同一个色）。
func faceTint(mid int64) ui.Color {
	palette := [...]ui.Color{
		ui.RGB(61, 74, 110), ui.RGB(110, 70, 61), ui.RGB(61, 105, 84),
		ui.RGB(98, 70, 110), ui.RGB(110, 96, 61), ui.RGB(61, 98, 110),
	}
	if mid < 0 {
		mid = -mid
	}
	return palette[mid%int64(len(palette))]
}

// followCardLaneWidth 是右边那一格（工具条 + 卡网格）实际有多宽：页面体
// （`pageBodyW`）减掉左右页边距，再减掉左分组栏和两栏之间那道缝。窄窗口
// （≤1024）分栏收成单列，右格就是「页面体 − 左右页边距」。
//
// ⚠️ **它是算出来的，不是量的**：骨架画出来的时候网格容器还没被 `Bounds()`
// 量到（要下一帧），等它量到骨架已经不在了——所以和 `pageBodyW` 一样按
// 「内容区 − 固定的那几项」算，首帧就对。量出来的 `f.width`（上一帧的网格
// 容器）优先级仍高于它：窗口拖宽拖到一半时它是真值。
func (s *Shell) followCardLaneWidth(c *ui.Context) float32 {
	winW := windowW(c)
	pad := space.PageHorizontalForWindow(winW)
	lane := s.pageBodyW(c) - 2*pad
	if winW > 1024 {
		lane -= followGroupW + space.Px40
	}
	return lane
}

// ─────────────────────────────────────────────────────────────────────────────
// 骨架屏：首屏还在拉的时候，先把「两列关注卡」摆成灰块
// ─────────────────────────────────────────────────────────────────────────────

// followSkeletonLabel 是骨架占位件在无障碍树 / Tester 里的名字。
//
// ⚠️ **Label 而不是 Key**：Tester 的 `Find` 认的是「画着的字或挂着的名字」，
// 不认 key；挂 Label 让「这一格里有什么」可以在测试里按名字量位置（头像在左、
// 按钮在右、两列缝 16——见 `follow_skeleton_test.go`）。名字带序号，于是**同
// 一格的两个状态（骨架/真卡）不会撞上**：数据回来时 Label 整个换掉。
//
// part 为空是整张卡，非空是卡上的某个件（「头像」/「按钮」）。
func followSkeletonLabel(idx, part string) string {
	if part == "" {
		return "关注占位:" + idx
	}
	return "关注占位:" + idx + ":" + part
}

// followSkeletonCard 画一张关注卡的骨架（`.ucard` 的影）：56 圆头像 + 名字条
// + 两行小字条 + 右侧一枚按钮大小的药丸占位。布局尺寸全部照 `pud.UCard`：
// 16 内边距、16 缝、56 头像、`tok.BtnSmH` 高的按钮、1px `p.Line` 的边框
// （真卡 `ButtonBase` 上那 1px 会让头像左缘比骨架外框的内边距再进 1——没有它
// 数据回来那一帧所有件都横着挪 1 像素）。
func followSkeletonCard(c *ui.Context, idx string, w float32) {
	p := tok.Of(c.Theme())
	key := "follow:skel:" + idx
	ui.Row(c).Key(key).Label(followSkeletonLabel(idx, "")).Width(w).Shrink(1).MinWidth(0).
		Padding(space.Px16).Gap(space.Px16).AlignItems(ui.Center).Radius(tok.R12).
		Border(1, p.Line).
		Children(func() {
			skeleton.Block(c, key+":av", 56, 56, 28).Label(followSkeletonLabel(idx, "头像"))
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(space.Px6).Children(func() {
				// ⚠️ **字条宽按真卡的份数给，不按卡宽比例**：真卡上「名字 16 /
				// 签名 13 / 认证 12」是三行不同长的字，骨架用三根不同长的条
				// 模拟；拿卡宽乘比例的话窄卡里字条会比真字短一截、宽卡里长一截。
				// `.Shrink(1)` 允许窄卡把它们压短——真卡的字也是 `Shrink(1)`
				// 让位给按钮。
				skeleton.Block(c, key+":n", 140, 14, tok.R4).Shrink(1)
				skeleton.Block(c, key+":s", 200, 12, tok.R4).Shrink(1)
				skeleton.Block(c, key+":o", 120, 12, tok.R4).Shrink(1)
			})
			// 按钮占位取「已关注」那一枚的 Sm 档（32 高），宽按那一枚的实测
			// （三个 13px 字 + 左右 14 内边距 ≈ 66）。Shrink(0) 不许它被压：
			// 窄卡里中间那一列已经 `Shrink(1)` 会让路，按钮永远贴在右缘
			//（真卡上它也是 `Shrink(0)`，同一档）。
			skeleton.Block(c, key+":btn", 66, tok.BtnSmH, tok.Pill).Shrink(0).Label(followSkeletonLabel(idx, "按钮"))
		})
}

// viewFollowSkeleton 是「右边那一格还在拉第一页」时画的：和真网格同一个形状
// 的两列骨架（用户 2026-11-03 拍板「一行 5~6 个、差不多 4 行」——但列数断点
// 归 `followColumnsFor` 管，骨架照它的列数摆，行数摆满一首屏）。
//
// ⚠️ **不用 `viewLoading`（八行歌曲占位）**：那一格的形状和真卡差太远——
// 回来那一帧行高、列宽一起跳，加上关注卡自己的入场过渡就是「关注图标叠加 /
// 加载卡顿」那一票的样子。骨架和真卡对齐之后，数据回来只是「灰块换真人」，
// 版面一步不挪。
//
// ⚠️ **Label 挂在外层那一格上**（「正在加载关注的人…」）：读屏软件和 Tester
// 靠它认出「这一格在拉」，和 `drawSkeletonRows` 那一档同一句话。
func (s *Shell) viewFollowSkeleton(c *ui.Context, f *followPage) {
	w := f.width
	if w <= 0 {
		// 头一帧还没量出网格容器，按「内容区 − 页边距 − 分组栏 − 缝」算
		//（`followCardLaneWidth`）——**不能拿内容区宽直接当容器**：它比右格
		// 多出页边距 + 分组栏 + 缝共三百多像素，两列算出来的 cell 就虚胖，
		// 第二列整张越出卡道（按钮占位也跟着冲出卡片右缘）。
		w = s.followCardLaneWidth(c)
	}
	cols := followColumnsFor(w)
	// ⚠️ **占位卡宽按「右边那一格的宽」算，不许拿内容区宽凑**：上一帧量到
	// 的 `f.width` 是真值；估算值不许大过它（拖宽窗口的半路，`f.width` 还
	// 停在旧值，算出来的 lane 会比它宽——那一帧宁可窄，不溢出）。
	contW := s.followCardLaneWidth(c)
	if w > 0 && w < contW {
		contW = w
	}
	cell := pud.CellWidth(max(contW, 0), cols, followCardGap)
	ui.Column(c).Key("follow:skeleton").Label("正在加载关注的人…").FillWidth().
		Gap(followRowGap).Children(func() {
		for i := range 4 {
			row := ui.Row(c).Key("follow:skel:row:" + strconv.Itoa(i)).FillWidth().Gap(space.Px16)
			row = list.LayoutRow(row, c.Theme())
			row.Children(func() {
				for k := range cols {
					followSkeletonCard(c, strconv.Itoa(i*cols+k), cell)
				}
			})
		}
	})
}
