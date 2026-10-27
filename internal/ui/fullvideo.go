// 满屏播放器里的视频：封面位置上的小窗，和点开之后铺满整窗的「影院」。
//
// # 入口
//
// 只有 B 站投稿（`app.VideoAvailable`）才在封面右下角叠一枚播放角标；点它开视频，
// 封面换成 16:9 的小窗（第一帧到之前还是封面）。点小窗铺满整窗，歌词挪到画面
// 底下，一句一句、边唱边扫亮（卡拉 OK 的做法）；再点一下回小窗。
//
// # 画面怎么到屏幕上
//
// 视频的帧在 `Draw` 里取：`App.VideoFrame(pos)` 按**音频的实时进度**挑该显示的
// 那一帧，再 `Bitmap.Update` 上传。放在 Draw 而不是 view 里，是因为播放中只靠
// `AnimationFrame` 重画、根本不重建 view——放在 view 里画面会一顿一顿。
package ui

import (
	"image"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/ShellMonster/Biu_Go/internal/service/video"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrim"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	fullVideoKey       = "full:video"
	fullVideoBadgeKey  = "full:video:badge"
	fullVideoCloseKey  = "full:video:close"
	fullTheaterKey     = "full:theater"
	fullVideoInKey     = "full:video:in-window"
	fullVideoScreenKey = "full:video:screen"

	fullVideoBadgeSize = 40
	fullVideoEdge      = 10
	fullVideoRadius    = 8
	// 影院里字幕离底边的距离：控制条露着时让开它，藏起来时贴近底边。
	fullKaraokeLift       = 24
	fullKaraokeLiftHidden = 56
	// 画面停下后再要多久的帧（等拖动进度之后解码器追上来）。
	videoIdleFrames = time.Second

	fullVideoQualityKey = "full:video:quality"
	fullVideoQItemKey   = "full:video:quality:item:"
)

// videoView 是画面这一边的状态，**只在界面线程读写**。
type videoView struct {
	bm    *ui.Bitmap
	w, h  int
	frame time.Time
	// qOpen 是画质菜单开着没有。
	qOpen bool
}

func (v *videoView) upload(f video.Frame, now time.Time) {
	img := &image.RGBA{Pix: f.Pix, Stride: f.W * 4, Rect: image.Rect(0, 0, f.W, f.H)}
	if v.bm == nil || v.w != f.W || v.h != f.H {
		v.bm, v.w, v.h = ui.NewBitmap(img), f.W, f.H
	} else {
		v.bm.Update(img)
	}
	v.frame = now
}

func (v *videoView) reset() { v.bm, v.w, v.h, v.frame, v.qOpen = nil, 0, 0, time.Time{}, false }

// drawVideo 取帧、上传、画。radius 为 0 就是直角。
func (s *Shell) drawVideo(p *ui.Painter, r ui.Rect, radius float32) {
	v := &s.vv
	st := s.app.Snapshot()
	if f, ok := s.app.VideoFrame(st.Position); ok {
		v.upload(f, p.Now())
	}
	p.Fill(r, ui.RGBA(0, 0, 0, 1), radius)
	if v.bm != nil {
		p.Clip(r, radius, func() { p.Image(v.bm, r, ui.Contain) })
	}
	if st.Playing || p.Now().Sub(v.frame) < videoIdleFrames || v.bm == nil {
		p.AnimationFrame()
	}
}

// viewVideoBadge 是封面右下角的播放角标。
func (s *Shell) viewVideoBadge(c *ui.Context) {
	b := ui.Box(c).Key(fullVideoBadgeKey).Role(ui.RoleButton).Label("看视频").
		Absolute().Right(fullVideoEdge).Bottom(fullVideoEdge).
		Size(fullVideoBadgeSize, fullVideoBadgeSize).
		Radius(fullVideoBadgeSize / 2).Background(ui.RGBA(0, 0, 0, scrim.Media)).
		Center().Cursor(ui.CursorPointer)
	b.Children(func() {
		ui.Icon(c, icon(transportIcon(false))).FontSize(24).TextColor(ui.RGBA(255, 255, 255, 0.95))
	})
	if b.Clicked() {
		s.app.StartVideo()
	}
}

// viewVideoWindow 是封面位置上的视频小窗，w×h 是它的框。
func (s *Shell) viewVideoWindow(c *ui.Context, phase app.VideoPhase, w, h float32) {
	box := ui.Box(c).Key(fullVideoKey).Role(ui.RoleButton).Label("视频，点击全屏").
		Absolute().Left(0).Top(0).Size(w, h).Cursor(ui.CursorPointer)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		s.drawVideo(p, r, fullVideoRadius)
	})
	box.Children(func() {
		if phase == app.VideoLoading || s.vv.bm == nil {
			ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).Center().Children(func() {
				ui.Spinner(c).Label("正在加载视频")
			})
		}
	})
	if box.Clicked() {
		s.full.theater = true
	}
	s.viewVideoClose(c)
	s.viewVideoModeKeys(c, false, fullVideoEdge, fullVideoEdge, 0)
}

// viewVideoModeKeys 是「两种全屏」那一对键，**图标不同、名字不同**：
//
//	窗口内全屏   斜向双箭头（expand）：视频撑满这扇窗口，窗口大小不变
//	屏幕全屏     四角括号（fullscreen）：整个窗口进系统全屏
//
// on 是已经在影院里（那时两枚都变成「退出」）。right/bottom 是它离父盒右、下沿的距离。
func (s *Shell) viewVideoModeKeys(c *ui.Context, on bool, right, bottom, top float32) {
	row := ui.Row(c).Key("full:video:modes").Absolute().Right(right).Gap(space.Px6)
	if top > 0 {
		row = row.Top(top)
	} else {
		row = row.Bottom(bottom)
	}
	row.Children(func() {
		s.viewVideoQuality(c, right, bottom, top)
		inLabel, scrLabel := "窗口内全屏", "屏幕全屏"
		if on {
			inLabel = "退出窗口内全屏"
		}
		if s.fullScreen {
			scrLabel = "退出屏幕全屏"
		}
		if s.videoModeKey(c, fullVideoInKey, iconExpand, inLabel).Clicked() {
			s.full.theater = !on
			if on {
				s.setScreenFull(false)
			}
		}
		if s.videoModeKey(c, fullVideoScreenKey, iconFullscreenLine, scrLabel).Clicked() {
			if s.fullScreen {
				s.setScreenFull(false)
			} else {
				s.full.theater = true
				s.setScreenFull(true)
			}
		}
	})
}

func (s *Shell) videoModeKey(c *ui.Context, key, name, label string) ui.Element {
	b := ui.Box(c).Key(key).Role(ui.RoleButton).Label(label).Size(28, 28).
		Radius(14).Background(ui.RGBA(0, 0, 0, scrim.Media)).Center().Cursor(ui.CursorPointer)
	b.Children(func() {
		ui.Icon(c, icon(name)).FontSize(16).TextColor(ui.RGBA(255, 255, 255, 0.95))
	})
	return b
}

// setScreenFull 让窗口进出系统全屏，并记下是不是**我们**让它进的：
// 视频关掉的时候只有我们进的才替用户退回来。
func (s *Shell) setScreenFull(v bool) {
	if s.win == nil || s.fullScreen == v {
		return
	}
	s.videoMadeFull = v
	s.win.SetFullScreen(v)
}

func (s *Shell) viewVideoClose(c *ui.Context) {
	b := ui.Box(c).Key(fullVideoCloseKey).Role(ui.RoleButton).Label("关闭视频").
		Absolute().Right(fullVideoEdge).Top(fullVideoEdge).Size(28, 28).
		Radius(14).Background(ui.RGBA(0, 0, 0, scrim.Media)).Center().Cursor(ui.CursorPointer)
	b.Children(func() {
		ui.Icon(c, icon(iconClose)).FontSize(16).TextColor(ui.RGBA(255, 255, 255, 0.95))
	})
	if b.Clicked() {
		s.app.StopVideo()
	}
}

// viewFullTheater 是铺满整窗的视频，字幕在底下。点一下回小窗。
func (s *Shell) viewFullTheater(c *ui.Context, hidden bool) {
	snap := s.app.Lyrics()
	winW, winH := c.Size()
	lift := float32(fullKaraokeLift + fullControlsHeight)
	if hidden {
		lift = fullKaraokeLiftHidden
	}
	box := ui.Box(c).Key(fullTheaterKey).Role(ui.RoleButton).Label("视频，点击退出全屏").
		Absolute().Left(0).Top(0).Right(0).Bottom(0)
	if hidden {
		box = box.Cursor(ui.CursorNone)
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		s.drawVideo(p, r, 0)
		band := ui.Rect{X: r.X, Y: r.Y + r.H - 320, W: r.W, H: 320}
		p.FillGradient(band, ui.LinearGradient{From: ui.RGBA(0, 0, 0, 0), To: ui.RGBA(0, 0, 0, 0.6), Angle: 180}, 0)
		paintKaraoke(p, ui.Rect{X: 0, Y: winH - lift - 110, W: winW, H: 110}, snap.Rows, snap.OffsetMS,
			s.app.Snapshot().Position)
	})
	if box.Clicked() {
		s.full.theater = false
		s.setScreenFull(false)
	}
	s.viewVideoModeKeys(c, true, 24, 0, 72)
}

// paintKaraoke 画两句：正在唱的那句边唱边从左往右扫亮，下一句淡着。
func paintKaraoke(p *ui.Painter, r ui.Rect, rows []lyrics.Row, offsetMS int, pos time.Duration) {
	now := lyricNow(pos, offsetMS)
	i := lyrics.ActiveAt(rows, now)
	if i < 0 {
		return
	}
	size := min(max(r.W/38, 22), 40)
	cur := rows[i].Text
	w, _ := p.MeasureText(0, ui.Span{Text: cur, Size: size})
	if maxW := r.W - 96; w > maxW {
		size *= maxW / w
		w = maxW
	}
	x, y := r.X+(r.W-w)/2, r.Y+r.H-size*2.6

	frac := karaokeFrac(rows, i, now)
	shadow, base, lit := ui.RGBA(0, 0, 0, 0.6), ui.RGBA(255, 255, 255, 0.55), ui.RGBA(255, 255, 255, 1)
	p.Text(x+1, y+2, cur, size, shadow)
	p.Text(x, y, cur, size, base)
	p.Clip(ui.Rect{X: x, Y: y - 4, W: w * frac, H: size * 1.6}, 0, func() { p.Text(x, y, cur, size, lit) })

	if i+1 < len(rows) {
		ns := size * 0.62
		nt := rows[i+1].Text
		nw, _ := p.MeasureText(0, ui.Span{Text: nt, Size: ns})
		nw = min(nw, r.W-96)
		p.Text(r.X+(r.W-nw)/2+1, y+size*1.35+1, nt, ns, shadow)
		p.Text(r.X+(r.W-nw)/2, y+size*1.35, nt, ns, ui.RGBA(255, 255, 255, 0.4))
	}
}

// karaokeFrac 是第 i 句已经唱到了几成（0..1）。
//
// 按「这一句到下一句」的时长走，走到 85% 就扫满：留一小段空当作换气，
// 字不会比歌慢半拍。最后一句没有下一句，按 5 秒算。
func karaokeFrac(rows []lyrics.Row, i int, now time.Duration) float32 {
	if i < 0 || i >= len(rows) || now < rows[i].At {
		return 0
	}
	end := rows[i].At + 5*time.Second
	if i+1 < len(rows) {
		end = rows[i+1].At
	}
	d := end - rows[i].At
	if d <= 0 {
		return 1
	}
	return min(float32(now-rows[i].At)/float32(d)/0.85, 1)
}

// viewVideoQuality 是画质键：一枚写着当前档（如 720P）的小药丸，点开是一列可选的档。
// 这一集只有一档（没登录时常见）就不画——只有一个选项的菜单是噪音。
func (s *Shell) viewVideoQuality(c *ui.Context, right, bottom, top float32) {
	list, cur := s.app.VideoQualities()
	if len(list) < 2 {
		s.vv.qOpen = false
		return
	}
	white := ui.RGBA(255, 255, 255, 0.95)
	pill := ui.Box(c).Key(fullVideoQualityKey).Role(ui.RoleButton).Label("清晰度").
		Height(28).PaddingX(space.Px10).Radius(14).Background(ui.RGBA(0, 0, 0, scrim.Media)).
		Center().Cursor(ui.CursorPointer)
	pill.Children(func() {
		ui.Text(c, cur.Label()).FontSize(13).TextColor(white)
	})
	if pill.Clicked() {
		s.vv.qOpen = !s.vv.qOpen
	}
	if !s.vv.qOpen {
		return
	}
	menu := ui.Column(c).Key("full:video:quality:menu").Absolute().Right(0).Gap(space.Px2).Padding(space.Px6).
		Radius(10).Background(ui.RGBA(0, 0, 0, scrim.Badge))
	if top > 0 {
		menu = menu.Top(36)
	} else {
		menu = menu.Bottom(36)
	}
	menu.Children(func() {
		for i := len(list) - 1; i >= 0; i-- {
			q := list[i]
			col := ui.RGBA(255, 255, 255, 0.75)
			if q == cur {
				col = ui.Color{R: 0xE8, G: 0x8A, B: 0x5E, A: 255}
			}
			item := ui.Box(c).Key(fullVideoQItemKey + q.Label()).Role(ui.RoleButton).Label(q.Label()).
				Height(30).PaddingX(space.Px12).Radius(6).Center().Cursor(ui.CursorPointer)
			item.Children(func() { ui.Text(c, q.Label()).FontSize(13).TextColor(col) })
			if item.Clicked() {
				s.vv.qOpen = false
				s.app.SetVideoQuality(q)
			}
		}
	})
}
