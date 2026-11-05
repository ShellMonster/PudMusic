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

// upPadFor 是这一帧 UP 空间页左右的页边距（40 / 32 / 16）。
func (s *Shell) upPadFor(c *ui.Context) float32 {
	return space.PageHorizontalForWindow(windowW(c))
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
		// ⚠️ **占位高度要和加载完之后那一版一样**（`upBannerH` + 头像探出
		// 头图带下沿那一截 `upAvatarS - 48`）：占位高了，数据落地那一瞬
		// 整个页面往上抽一段——Tab 条和内容看着像自己跳了一下。
		ui.Row(c).Key(key).Height(upBannerH + upAvatarS - 48).FillWidth().Center().Children(func() {
			ui.Spinner(c).Label("正在打开这个 UP 主的空间")
		})
		return

	case h.Err != "" && !h.Loaded:
		// ⚠️ **高度和正常头部同一份**（理由同上一支）：失败页比正常页高的话，
		// 「再试一次」按下去、加载中的占位和最后的数据页是三个不同的高度。
		ui.Column(c).Key(key).Height(upBannerH + upAvatarS - 48).FillWidth().Center().Gap(space.Px10).
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
		// UP 主自己设了头图就画它（原型的假数据没有头图，是取色带；真实数据比假数据多这一样，
		// 用户看得到的内容不丢）；没设的走原型的取色带。唱片纹两种情况都叠在上面。
		band := ui.Box(c).Key("space-band").FillWidth().Height(upBannerH).Background(tint).Clip()
		if h.User.Banner != "" {
			band = band.Label(spaceBannerLabel)
		}
		band.Draw(func(pp *ui.Painter, r ui.Rect) { paintGrooves(pp, r) }).Children(func() {
			if h.User.Banner != "" {
				s.spaceFill(c, h.User.Banner, s.spaceContentWidth(c), upBannerH)
			}
		})

		// ② 头部：往上压 48，放不下就换行（原型 `flex-wrap: wrap`，缝 24，底对齐）。
		ui.Row(c).Key("space-head-info").FillWidth().AlignItems(ui.End).Gap(space.Px24).Wrap().
			Padding(space.None, pad, space.None, pad).Margin(-48, space.None, space.None, space.None).
			Children(func() {
				outer := float32(upAvatarS)
				inner := outer - 2*upAvatarRing
				// 无障碍名字沿用头像地址（`spaceAvatar` 的老口径，无头测试靠它量头像的盒子）。
				ui.Box(c).Size(outer, outer).Radius(outer / 2).Background(pl.Bg).AlignItems(ui.Center).Justify(ui.Center).
					Shrink(0).Label(h.User.Face).Children(func() {
					pud.Avatar(c, h.User.Name, inner, faceTint(sp.mid), s.upFaceDraw(h.User.Face))
				})

				// 名字 + LV，签名。
				// ⚠️ **上内边距 16 把文字顶推过头图带的下沿**（原型 `.up-head` 里
				// 这一列和头像底对齐时，32 的名字本来就探回带里约 6px——对齐
				// 之后用户读到的是「文字骑在带子上」。多往下推 16，名字的
				// 整个字形落在带子下面，只有头像压线）。
				ui.Column(c).Grow(1).Shrink(1).MinWidth(240).Padding(space.Px16, space.None, space.Px8, space.None).
					Children(func() {
						ui.Row(c).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
							typo.Display(ui.Text(c, spaceName(h.User)), scale.Font(c, scale.Size32)).
								TextColor(pl.Text).MaxLines(1).Ellipsis("…").Shrink(1)
							if h.User.Level > 0 {
								pud.Badge(c, "LV"+strconv.Itoa(h.User.Level), false)
							}
						})
						// 认证说明（`official.title`）：原型没有，真实数据有，小字 text-3 放在签名下面。
						defer func() {
							if h.User.Official != "" && !h.Relation.Blocked() {
								ui.Text(c, h.User.Official).FontSize(scale.Font(c, scale.Size12)).TextColor(pl.Text3).
									SingleLine().Label(h.User.Official).Margin(space.Px4, space.None, space.None, space.None)
							}
						}()
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
		// 拉黑了不画关注键：按下去发出去的是取关（旧的拉黑分支只留头像和名字，同一个理由）。
		if !isSelf && !h.Relation.Blocked() {
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
