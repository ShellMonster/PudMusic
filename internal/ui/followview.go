// 我的关注：左边分组栏、右边搜索 + 两列 UP 主卡片（「深夜唱片行」，原型 `PAGES.follow` /
// CSS `.split` `.group-col` `.ucard`，DESIGN.md §0′.T3）。取数、翻页、分组、搜索、取关的
// 机器都在 `follow.go`，这里只管长什么样。
package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
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

// followColumnsFor 是右边那一格有 w 宽时卡片排几列：原型固定两列，窄到两张卡放不下
// （每张至少 280）才退成一列。
func followColumnsFor(w float32) int {
	if w > 0 && w < 2*280+followCardGap {
		return 1
	}
	return 2
}

// viewFollowSplit 是页头下面那一整块：左分组栏，右工具条 + 网格。
func (s *Shell) viewFollowSplit(c *ui.Context, f *followPage) {
	ui.Row(c).Key("follow:split").Grow(1).Gap(space.Px40).AlignItems(ui.Start).
		Margin(space.Px24, space.None, space.None, space.None).Children(func() {
		s.viewFollowGroups(c, f)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).FillHeight().Children(func() {
			s.viewFollowToolbar(c, f)
			s.viewFollowGrid(c, f)
		})
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
	ui.Column(c).Key("follow:groups").Width(followGroupW).Shrink(0).Gap(space.Px2).Children(func() {
		ui.Text(c, "分组").FontSize(scale.Font(c, scale.Size12)).FontWeight(500).TextColor(pl.Text3).
			Padding(space.None, space.Px12, space.Px8, space.Px12)
		item := func(tab, label string, n int) {
			on := f.tab == tab
			b := ui.ButtonBase(c).Key("follow:group:"+tab).Label(label).FillWidth().
				Padding(space.Px8, space.Px12).Radius(tok.R8).AlignItems(ui.Center).Gap(space.Px8)
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
		ui.Box(c).FillWidth().Height(1).Background(pl.Line).Margin(space.Px8, space.Px12, space.Px8, space.Px12)
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
