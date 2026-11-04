// UP 主空间头部（「深夜唱片行」，原型 `PAGES.user` / CSS `.up-band` `.up-head`，
// DESIGN.md §0′.T3）：
//
//	头图带   168 高，取色按 34% 混进 bg，加唱片纹 `grooves`（同心圆，白 7%）
//	头部     112 头像（4 像素 bg 色环，压住头图带 48）、名字（宋体 32）+ LV 角标、签名、
//	         粉丝 / 关注 / 获赞统计、关注 / 播放投稿 / 在 B 站打开
//
// 取色用头像（`covertint`），UP 主自己设的头图横幅按原型不再显示。
package ui

import (
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// 头部尺寸（原型 CSS）。
const (
	// upBandH 是头图带高（`.up-band` 168）。
	upBandH float32 = 168
	// upFaceS 是头像边长（`avatar(…,112)`），外面再套 4 像素 bg 色环。
	upFaceS float32 = 112
	// upFaceRing 是头像外那圈色环的宽（`.up-head .avatar` 的 4px border）。
	upFaceRing float32 = 4
	// upHeadLift 是头部往上压住头图带多少（`margin-top:-48px`）。
	upHeadLift float32 = 48
)

// upPadFor 是这一帧 UP 空间页左右的页边距（40 / 32 / 16）。
func (s *Shell) upPadFor(c *ui.Context) float32 {
	return space.PageHorizontal(s.spaceContentWidth(c))
}

// paintGrooves 画唱片纹：圆心在 (85% 宽, 140% 高)，半径 40 起每 14 一圈到 900，
// 1 像素白线 14% 不透明、整层再 50%（原型 `.up-band svg { opacity: .5 }`），裁在 r 里。
func paintGrooves(pp *ui.Painter, r ui.Rect) {
	pp.Clip(r, 0, func() { grooveRings(pp, r) })
}

func grooveRings(pp *ui.Painter, r ui.Rect) {
	cx, cy := r.X+r.W*0.85, r.Y+r.H*1.4
	col := ui.RGBA(255, 255, 255, 0.07)
	for rad := float32(40); rad < 900; rad += 14 {
		// 圆环完全落在可见区域外的跳过（整圈在带子下面或左右之外时不用画）。
		if cy-rad > r.Y+r.H || cx+rad < r.X || cx-rad > r.X+r.W {
			continue
		}
		if cy+rad < r.Y {
			continue
		}
		pp.Stroke(ui.Rect{X: cx - rad, Y: cy - rad, W: 2 * rad, H: 2 * rad}, col, rad, 1)
	}
}

// viewSpaceHeader 画头部。加载中 / 失败 / 拉黑三种状态沿用原来的分流，只换了样子。
func (s *Shell) viewSpaceHeader(c *ui.Context, sp *upSpace, h app.SpaceHeader) {
	pl := tok.Of(c.Theme())
	key := "space-header:" + strconv.FormatInt(sp.mid, 10)
	pad := s.upPadFor(c)

	switch {
	case h.Loading && !h.Loaded:
		ui.Row(c).Key(key).Height(upBandH + upHeadLift + 80).FillWidth().Center().Children(func() {
			ui.Spinner(c).Label("正在打开这个 UP 主的空间")
		})
		return

	case h.Err != "" && !h.Loaded:
		ui.Column(c).Key(key).Height(upBandH + upHeadLift + 80).FillWidth().Center().Gap(space.Px10).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, "打不开这个 UP 主的空间").FontSize(scale.Font(c, scale.Size16)).TextColor(pl.Text)
			ui.Text(c, h.Err).FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text2).MaxLines(3)
			// 失败之后只有人能让它再来一次（每帧都跑，没有这道门就是死循环）。
			if pud.Button(c, "space:retry", pud.Ghost, pud.Sm, "再试一次", "refresh").Clicked() {
				sp.header.Load()
			}
		})
		return
	}

	tint := s.tintBackground(c, h.User.Face, tok.TintUpBand)
	ui.Column(c).Key(key).FillWidth().Children(func() {
		// ① 头图带。
		ui.Box(c).Key("space-band").FillWidth().Height(upBandH).Background(tint).Clip().
			Draw(func(pp *ui.Painter, r ui.Rect) { paintGrooves(pp, r) })

		// ② 头部：往上压 48，放不下就换行（原型 `flex-wrap: wrap`，缝 24，底对齐）。
		ui.Row(c).Key("space-head-info").FillWidth().AlignItems(ui.End).Gap(space.Px24).Wrap().
			Padding(space.None, pad, space.None, pad).Margin(-upHeadLift, space.None, space.None, space.None).
			Children(func() {
				ring := upFaceS + 2*upFaceRing
				ui.Box(c).Size(ring, ring).Radius(ring / 2).Background(pl.Bg).AlignItems(ui.Center).Justify(ui.Center).
					Shrink(0).Children(func() {
					pud.Avatar(c, h.User.Name, upFaceS, tintAvatarColor(pl, tint), s.upFaceDraw(h.User.Face))
				})

				// 名字 + LV，签名。
				ui.Column(c).Grow(1).Shrink(1).MinWidth(240).Padding(space.None, space.None, space.Px8, space.None).
					Children(func() {
						ui.Row(c).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
							typo.Display(ui.Text(c, spaceName(h.User)), scale.Font(c, scale.Size32)).
								TextColor(pl.Text).MaxLines(1).Ellipsis("…").Shrink(1)
							if h.User.Level > 0 {
								pud.Badge(c, "LV"+strconv.Itoa(h.User.Level), false)
							}
						})
						if h.Relation.Blocked() {
							ui.Text(c, "你把他拉黑了，他的内容在这儿看不到").FontSize(scale.Font(c, scale.Size14)).
								TextColor(pl.Err).Margin(space.Px4, space.None, space.None, space.None)
						} else if h.User.Sign != "" {
							ui.Text(c, h.User.Sign).FontSize(scale.Font(c, scale.Size14)).TextColor(pl.Text2).
								MaxLines(2).Ellipsis("…").Margin(space.Px4, space.None, space.None, space.None)
						}
					})

				s.viewSpaceStats(c, sp, h)
				s.viewSpaceActions(c, sp, h)
			})
	})
}

// tintAvatarColor 是没头像时头像底色：取色块的同色相、深一点（原型 `avatar()` 用 UP 的 tint）。
func tintAvatarColor(pl tok.Palette, tint ui.Color) ui.Color {
	return pl.Bg.Mix(tint, 1).Mix(ui.RGB(40, 44, 60), 0.35)
}

// viewSpaceStats 是「粉丝 / 关注 / 获赞」三栏（原型 `.stats`：缝 32；数字等宽 20 / 600，
// 名字 12 号 text-3）。
//
// 取法：
//   - **整块只在登录后画**：`/x/relation/stat` 要 Cookie，没登录时是 0——一个干干净净的
//     「粉丝 0」是假数。
//   - **没查到就不画**，不画 0（见 `StatOK` / `StatFailed`）。
//   - **关注只在看自己时画**：别人的关注数 B 站不公开。
//   - **获赞** 来自单独的 `x/space/upstat`，取不到（0）就不画这一格。
func (s *Shell) viewSpaceStats(c *ui.Context, sp *upSpace, h app.SpaceHeader) {
	account := s.pageAccount()
	if account.SelfMid == 0 {
		return
	}
	pl := tok.Of(c.Theme())
	isSelf := account.SelfMid == sp.mid
	switch {
	case h.StatOK():
		ui.Row(c).Key("space-stats").Gap(space.Px32).Padding(space.None, space.None, space.Px12, space.None).
			Children(func() {
				spaceStat(c, "粉丝", list.PlayCountText(max(h.Stat.Follower, 1)), h.Stat.Follower)
				if isSelf {
					spaceStat(c, "关注", strconv.FormatInt(h.Stat.Following, 10), 1)
				}
				if h.Stat.Likes > 0 {
					spaceStat(c, "获赞", list.PlayCountText(h.Stat.Likes), h.Stat.Likes)
				}
			})
	case h.StatFailed():
		ui.Row(c).Gap(space.Px8).AlignItems(ui.Center).Padding(space.None, space.None, space.Px12, space.None).
			Children(func() {
				ui.Text(c, "粉丝数没读到").FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3)
				if pud.Button(c, "space:stat-retry", pud.Quiet, pud.Sm, "重试", "").Clicked() {
					sp.header.RetryRelation()
					sp.header.Load()
				}
			})
	}
}

// spaceStat 是一栏统计：上面数字、下面名字。n 为 0 时数字写「0」。
func spaceStat(c *ui.Context, label, value string, n int64) {
	pl := tok.Of(c.Theme())
	if n <= 0 {
		value = "0"
	}
	ui.Column(c).Key("space-stat:" + label).AlignItems(ui.Start).Children(func() {
		typo.Mono(ui.Text(c, value)).FontSize(scale.Font(c, scale.Size20)).FontWeight(600).
			TextColor(pl.Text).SingleLine()
		ui.Text(c, label).FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3).SingleLine()
	})
}

// viewSpaceActions 是「关注 / 播放投稿 / 在 B 站打开」一排（原型：缝 8，下 12）。
//
// 关注键只在「登录了、且看的不是自己」时画；没读到关注状态就说没读到——画一个能按的
// 「关注」比不画更糟（按下去是一次取关）。
func (s *Shell) viewSpaceActions(c *ui.Context, sp *upSpace, h app.SpaceHeader) {
	account := s.pageAccount()
	isSelf := account.SelfMid == sp.mid
	pl := tok.Of(c.Theme())

	ui.Row(c).Key("space-actions").Gap(space.Px8).AlignItems(ui.Center).Shrink(0).
		Padding(space.None, space.None, space.Px12, space.None).Children(func() {
		if !isSelf {
			switch h.Follow(account.SelfMid, account.SelfMid != 0) {
			case app.FollowDone:
				// 关注是写接口：开后台，`UpSpace.Follow` 自己会叫醒界面。
				if pud.Button(c, "space-follow", pud.Ghost, pud.Std, "已关注", "check").Clicked() {
					sp.follow(false)
				}
			case app.FollowAskable:
				if pud.Button(c, "space-follow", pud.Primary, pud.Std, "关注", "plus").Clicked() {
					sp.follow(true)
				}
			case app.FollowFailed:
				ui.Text(c, "关注状态没读到").FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3)
				if pud.Button(c, "space:rel-retry", pud.Quiet, pud.Sm, "重试", "").Clicked() {
					sp.header.RetryRelation()
					sp.header.Load()
				}
			}
		}
		if pud.Button(c, "space:play", pud.Ghost, pud.Std, "播放投稿", "play").Clicked() {
			s.playAllSongs(sp.videos.Items())
		}
		if s.withTip(c, pud.RoundButton(c, "space:open", pud.Ghost, pud.Std, "open", "在 B 站打开"), "在 B 站打开").Clicked() {
			c.OpenURL(searchUserSpaceURL(strconv.FormatInt(sp.mid, 10)))
		}
	})
}
