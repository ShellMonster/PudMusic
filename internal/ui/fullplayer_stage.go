// 全屏播放器的顶栏、左栏（封面 / 视频小窗 / 歌名 / 赞和喜欢）和影院模式。
//
// 数值出处：原型 `index.css` 的 `.full-top`（680–682）、`.full-left`（684–690）、
// `.vbadge`（870）、`.vframe`（872–874）、`.cinema`（888–892）。
package ui

import (
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	tclean "github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// fullCoverPausedScale 是暂停时封面缩到的比例（原型 `.full.paused .full-cover { scale(.94) }`），
// 过渡 600ms ease-out。
const fullCoverPausedScale = 0.94

var fullCoverAnim = ui.Animation{Duration: 600 * time.Millisecond, Ease: motion.EaseOut}

// viewFullTop 是顶栏：收起键、居中的 `NOW PLAYING · i / n`、评论、队列。
//
// 🔴 **左边必须让开红黄绿**：隐藏标题栏的窗口里系统控件叠在内容**上面**，而满屏把普通顶栏
// 整条盖住了——所以左边留白是 `TitleBar().Left + 20`。同一条顶栏还要能拖窗口。
func (s *Shell) viewFullTop(c *ui.Context, st app.State, g fullGeo) {
	tb := c.TitleBar()
	p := tok.OfFull(c.Theme())
	q := s.app.Queue()
	idx, n := q.CurrentIndex(), q.Len()
	label := s.full.txt.nowPlaying(idx, n)
	ui.Row(c).Key("full-top").
		Absolute().Left(0).Right(0).Top(0).Height(max(tb.Height, fullTopH)).
		Padding(space.Px16, fullTopPadX, space.Px16, tb.Left+fullTopPadX).Gap(space.Px8).AlignItems(ui.Center).DragWindow().
		Children(func() {
			if pud.IconBtn(c, "full:close", "down", "收起满屏播放器", pud.IconOpts{Color: p.Text, HoverColor: p.Text}).Clicked() {
				s.closeFullPlayer()
			}
			// 标签在剩下的那一段里居中（原型 `.src { margin: 0 auto }`）。
			ui.Row(c).Grow(1).Justify(ui.Center).Children(func() {
				fullMono(c, label, scale.Size12, 400, 12*0.2, p.Text3)
			})
			if st.HasTrack && s.app.CommentsWorthAsking() {
				count := s.app.CommentCount()
				b := pud.IconBtn(c, "full:comments", "comment", commentsKeyLabel(count),
					pud.IconOpts{Color: p.Text, HoverColor: p.Text, Count: playbarCountText(count)})
				if s.withTip(c, b, commentsKeyTip(count)).Clicked() {
					s.commentsOpen = true
				}
			}
			if s.withTip(c, pud.IconBtn(c, "full:queue", "queue", "打开播放列表",
				pud.IconOpts{Color: p.Text, HoverColor: p.Text}), drawerTip(s.drawerOpen)).Clicked() {
				s.drawerOpen = !s.drawerOpen
			}
		})
}

// viewFullLeft 是左栏：封面（或视频小窗）+ 下面的歌名那一块。
func (s *Shell) viewFullLeft(c *ui.Context, st app.State, g fullGeo, phase app.VideoPhase, prefs app.FullPlayerPrefs) {
	videoOn := phase == app.VideoLoading || phase == app.VideoOn
	if g.cover.W > 0 {
		s.full.coverRect = g.cover
		s.viewFullCover(c, st, g.cover, phase)
	}
	s.viewFullInfo(c, st, g, videoOn)
}

// viewFullCover 是满屏那张封面——**和播放条上那一张是同一个元素**（共享元素转场）。
//
// 圆角 8、`0 32 80 .6` 的影、surface 6% 白的占位底；暂停时缩到 0.94。右键 / 菜单键开封面菜单
// （复制歌曲信息 / 复制封面 / 一键三连 / 在 B 站打开）。有视频的投稿右下角叠「看视频」。
func (s *Shell) viewFullCover(c *ui.Context, st app.State, r ui.Rect, phase app.VideoPhase) {
	videoOn := phase == app.VideoLoading || phase == app.VideoOn
	scaleTo := float32(1)
	if !st.Playing && !videoOn {
		scaleTo = fullCoverPausedScale
	}
	box := shadow.FullCover(ui.Box(c).Key("full-cover").
		Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Radius(tok.R8).
		Background(ui.RGBA(255, 255, 255, 0.06))).
		// 🔴 **可聚焦**：没有它，键盘（菜单键 / Shift+F10）够不着这一份菜单。
		// ⚠️ 名字不能叫「当前封面」：里面那张图已经有一个了。
		Focusable().Label(copyCoverActionsLabel).
		ContextMenu(func(m *ui.Menu) { s.viewCopyMenu(st, m) }).
		AnimatePose(ui.Pose{Scale: scaleTo}, fullCoverAnim)
	box.Children(func() {
		if videoOn {
			s.viewVideoWindow(c, phase, r.W, r.H, false)
			return
		}
		// 封面还没来 / 没有封面：原型的占位是外面那层 `rgba(255,255,255,.06)`，中间一枚 text-3 的图片图标
		// （`imgcache` 自己的占位按**当前主题**上色，浅色主题下会在这块固定暗底上亮成一块白）。
		if st.CoverURL == "" || !s.pageImages().GetSized(st.CoverURL, r.W, r.H).Ready() {
			ui.Box(c).Key("full-cover-empty").Size(r.W, r.H).Center().Label("当前封面").Children(func() {
				ui.Icon(c, pud.Icon("image")).FontSize(r.W * 0.18).TextColor(tok.Full.Text3)
			})
			if s.app.VideoAvailable() {
				s.viewVideoBadge(c)
			}
			return
		}
		id := coverIdentity(s, st)
		cover := imgcache.ViewFlat(c, s.pageImages(), st.CoverURL, r.W, r.H, tok.R8).
			Label("当前封面").Key(s.fullCoverKeyFor(st.CoverURL))
		cover.Transition(s.fullCoverMatchFor(id))
		if s.app.VideoAvailable() {
			s.viewVideoBadge(c)
		}
	})
}

// fullCoverKeyFor 是满屏封面格子的键（带封面地址）。拼出来的字符串缓存着，地址不变就不再拼。
func (s *Shell) fullCoverKeyFor(url string) any {
	if s.fullCoverKey == nil || s.fullCoverKeyURL != url {
		s.fullCoverKeyURL, s.fullCoverKey = url, "full-cover-art:"+url
	}
	return s.fullCoverKey
}

// fullCoverGhost 是封面进场的起点和退场的终点：全透明。
var fullCoverGhost = ui.Motion{Opacity: 0}

// fullCoverMatchFor 是「共享元素 + 淡入淡出」的转场。按身份缓存，身份不变就原样返回。
func (s *Shell) fullCoverMatchFor(id string) ui.ElementTransition {
	if s.fullCoverTrID != id || s.fullCoverTr.Match == nil {
		s.fullCoverTrID, s.fullCoverTr = id, coverTransition(id)
		s.fullCoverTr.Enter, s.fullCoverTr.Exit = &fullCoverGhost, &fullCoverGhost
	}
	return s.fullCoverTr
}

// coverIdentity 是共享元素这一次的身份：当前这一首的判重标识。
//
// 认不出是哪一首时退回按播放状态拼一个。**不能留空**：空串能和任何一首的空串对上，
// 转场就变成了「从随便哪张封面飞过来」。
func coverIdentity(s *Shell, st app.State) string {
	if cur, ok := s.pageInput().Current, s.pageInput().HasCurrent; ok {
		if id := cur.Identity(); id != "" {
			return id
		}
	}
	return "state:" + st.Path
}

// fullTitleSize 是歌名字号：`clamp(28px, 3vw, 40px)`。
func fullTitleSize(winW float32) float32 { return min(max(winW*0.03, 28), 40) }

// viewFullInfo 是封面下面那一块：宋体歌名、「歌手 · UP」、播放量和弹幕数，右边是赞和喜欢。
func (s *Shell) viewFullInfo(c *ui.Context, st app.State, g fullGeo, videoOn bool) {
	p := tok.OfFull(c.Theme())
	likeable := s.app.Likeable()
	btns := float32(0)
	if likeable {
		btns += 2*tok.IconHit + 2*space.Px8
	}
	if videoOn {
		btns += tok.IconHit + space.Px8
	}
	textW := max(g.info.W-btns, 80)
	size := fullTitleSize(g.winW)
	ui.Row(c).Key("full-info").Absolute().Left(g.info.X).Top(g.info.Y).Width(g.info.W).
		Gap(space.Px8).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(textW).Shrink(0).Children(func() {
			typo.Display(ui.Text(c, tclean.Clean(st.Title).Title), scale.Font(c, size)).
				FixedLineHeight(scale.Font(c, size) * 1.2).TextColor(p.Text).SingleLine().Ellipsis("…")
			s.viewFullArtist(c, st, p)
			s.viewFullStats(c, p)
		})
		ui.Box(c).Grow(1)
		if likeable {
			s.viewFullThumb(c)
			s.viewFullHeart(c)
		}
		if videoOn {
			if pud.IconBtn(c, "full:video-off", "x", "关闭视频", pud.IconOpts{Color: p.Text}).Clicked() {
				s.app.StopVideo()
			}
		}
	})
}

// viewFullArtist 是歌手那一行（`.full-artist`：次要色，UP 名可点）。
//
// ⚠️ 原型是「歌手 · 专辑 · UP」；B 站的曲目没有专辑，歌手就是 UP，所以只有一段。
func (s *Shell) viewFullArtist(c *ui.Context, st app.State, p tok.Palette) {
	if st.Artist == "" {
		return
	}
	row := ui.Row(c).Key("full-artist").FillWidth().Height(24).AlignItems(ui.Center)
	row.Children(func() {
		if st.ArtistMid <= 0 {
			ui.Text(c, st.Artist).FontSize(scale.Font(c, scale.Size14)).TextColor(p.Text2).SingleLine().Ellipsis("…")
			return
		}
		b := ui.ButtonBase(c).Key("full:artist").Label("打开 UP 主空间 " + st.Artist).Cursor(ui.CursorPointer)
		col := p.Text2
		if b.Hovered() {
			col = p.Text
		}
		b.Children(func() {
			ui.Text(c, st.Artist).FontSize(scale.Font(c, scale.Size14)).TextColor(col).SingleLine().Ellipsis("…")
		})
		if b.Clicked() {
			s.closeFullPlayer()
			s.openPage(upPathPrefix + strconv.FormatInt(st.ArtistMid, 10))
		}
	})
}

// viewFullStats 是播放量 / 弹幕数那一行（原型 `.embed-stats`：12 号等宽 text-3，12 的小图标，
// 缝 12）。
//
// ⚠️ **B 站这一趟拿不到播放量**（`ArchiveCounts` 只有评论 / 收藏 / 赞），所以只有弹幕数；
// 弹幕还没拉回来或者这一首没有弹幕时整行不画。
func (s *Shell) viewFullStats(c *ui.Context, p tok.Palette) {
	n := len(s.app.Danmaku().Items)
	if n == 0 {
		return
	}
	ui.Row(c).Key("full-stats").Margin(space.Px8, space.None, space.None, space.None).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(space.Px4).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, pud.Icon("danmaku")).FontSize(12).TextColor(p.Text3)
			fullMono(c, playCountText(int64(n)), scale.Size12, 400, 0, p.Text3)
		})
	})
}

// viewFullThumb 是点赞键（原型 `.icon-btn[data-act="thumb"]`）：右上角带赞数，**长按三连**。
//
// 状态机和进度环是播放条上封面用的那一份（`s.like`，满屏开着时播放条不建，互斥）。
func (s *Shell) viewFullThumb(c *ui.Context) {
	p := tok.OfFull(c.Theme())
	rel := s.app.LikeState()
	count := s.app.ArchiveCounts().Like
	label := "赞"
	if rel.Liked {
		label = "已赞"
	}
	ic := "thumb"
	if rel.Liked {
		ic = "thumb-on"
	}
	var btn ui.Element
	ui.Box(c).Key("full:thumb-wrap").Size(tok.IconHit, tok.IconHit).Shrink(0).Children(func() {
		btn = pud.IconBtn(c, "full:thumb", ic, countKeyLabel(label, count)+"（长按三连）",
			pud.IconOpts{On: rel.Liked, Count: playbarCountText(count), Color: p.Text2})
		s.viewLikeRing(&s.like, c, btn.Pressed(), tok.R8)
	})
	btn = s.withTip(c, btn, countKeyTip("赞", count, likeStarTooltip(rel.Liked, s.app.LikeErr() != nil && !s.app.LikeAsked())))
	if btn.Clicked() {
		if s.like.swallowClick() {
			return
		}
		s.app.SetLike(!rel.Liked, func(err error) {
			if err != nil {
				s.addToast("赞不了："+err.Error(), notice.Danger)
			}
		})
	}
}

// viewFullHeart 是「喜欢」键（原型 `[data-act="like"]`，心形）：对应收藏——点开选收藏夹。
func (s *Shell) viewFullHeart(c *ui.Context) {
	p := tok.OfFull(c.Theme())
	faved := s.app.LikeState().Favorited
	ic := "heart"
	if faved {
		ic = "heart-on"
	}
	b := pud.IconBtn(c, "full:heart", ic, "收藏", pud.IconOpts{On: faved, Color: p.Text2})
	if s.withTip(c, b, favTooltip(faved)).Clicked() {
		it, ok := s.app.PlayingItem()
		if !ok {
			s.addToast("这一首还没有取回来，收藏不了", notice.Warning)
			return
		}
		s.openFolderSelect(it)
	}
}

// viewFullCinema 是影院模式（原型 `.full.cinema`）：视频居中、高度撑满中间那一块，
// 下面一行卡拉 OK 歌词（宋体 28 / 700 白色）。点视频回小窗。
func (s *Shell) viewFullCinema(c *ui.Context, st app.State, g fullGeo, phase app.VideoPhase) {
	p := tok.OfFull(c.Theme())
	s.full.coverRect = g.cinema
	ui.Box(c).Key("full-cinema").Absolute().Left(g.cinema.X).Top(g.cinema.Y).Size(g.cinema.W, g.cinema.H).
		Radius(tok.R8).Background(ui.RGB(0, 0, 0)).Clip().Children(func() {
		s.viewVideoWindow(c, phase, g.cinema.W, g.cinema.H, true)
	})
	snap := s.app.Lyrics()
	line := ""
	if i := lyrics.ActiveAt(snap.Rows, lyricNow(st.Position, snap.OffsetMS)); i >= 0 {
		line = snap.Rows[i].Text
	}
	ui.Box(c).Key("full-karaoke").Absolute().Left(g.karaoke.X).Top(g.karaoke.Y).Size(g.karaoke.W, g.karaoke.H).
		Center().Children(func() {
		typo.Display(ui.Text(c, line), scale.Font(c, 28)).FixedLineHeight(scale.Font(c, 28) * 1.4).
			TextColor(tok.Full.LyricCurrent).SingleLine().Ellipsis("…")
	})
	_ = p
}
