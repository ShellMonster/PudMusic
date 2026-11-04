// 全屏播放器（「深夜唱片行」§0′.T4，原型 `openFull` / `syncFull`）。
//
// # 结构（原型 `.full`：三行网格 auto / 1fr / auto）
//
//	顶栏   收起键 · `NOW PLAYING · i / n` · 评论 · 队列         （fullplayer_stage.go）
//	中间   左栏：封面 / 视频小窗 · 歌名 · 歌手 · 播放量 · 赞 / 喜欢   （fullplayer_stage.go）
//	       右栏：歌词来源 + 原文 / 翻译 chip · 歌词 · 旋钮           （fullplayer_lyrics.go）
//	       影院（cinema）：视频居中 + 一行卡拉 OK 歌词，盖住两栏
//	底栏   频谱 · 章节名 · 进度 · 控制区                        （fullplayer_bottom.go）
//
// # 固定暗色
//
// 深浅主题下它都是同一套暗色（`tok.Full`）：底是 `#0B0B0C` 和封面取色按 30% 混出来的色，
// 上面叠一层 50% 的模糊封面。里面的 `pud` 组件靠 `pud.SetFull(true)` 取暗色那一档令牌
// （原型 `.full` 把 `--text` / `--surface` / `--line` 等整套变量改写了）。
//
// # 位置自己算
//
// 两栏、封面、歌词、旋钮都是按窗口尺寸**算出矩形**再 `Absolute` 摆（`fullGeometry`），
// 而不是靠 flex：弹幕那一层是另一个浮层，要知道封面此刻在窗口里的哪一块
// （`fullPlayer.coverRect`）。
//
// # 与旧版（Biu 版）的差别
//
// 旧版的「指针三秒不动就藏界面 / 点空白处切换显隐」、右边的分集把手和面板、点封面开
// 全屏视频都是原型没有的入口，已删除（分集的数据层和播放条上的分集抽屉不动）。
//
// 🔴 铁律二：这里**不调 SetTheme**。它在 `viewMain` 的同一个 build pass 里建
// （`viewMain` 第一行就 `applyFrameTheme` 过）。
//
// ⚠️ 外面那层 `Guard` 是 `viewMain` 加的（铁律三）：这一层一崩，崩掉的是整个窗口。
package ui

import (
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/rowkey"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// 全屏播放器的尺寸（原型 `.full-*` CSS，646–711 行 + 855–894 行）。
const (
	// fullTopH 是顶栏高：上下各 16 + 36 的图标键。
	fullTopH = 68
	// fullTopPadX 是顶栏左右内边距（原型 `.full-top { padding: 16px 20px }`）。
	fullTopPadX = 20
	// fullBottomPadTop / fullBottomPadBottom 是底栏上下内边距（`padding: 8px 80px 28px`）。
	fullBottomPadTop    = 8
	fullBottomPadBottom = 28
	// fullBottomGap 是底栏里各行之间的缝（`gap: 12px`）。
	fullBottomGap = 12
	// fullSpectrumH 是频谱高（`.spectrum { height: 40px }`）；隐藏频谱时这一行照样占着
	// （`visibility: hidden`）。
	fullSpectrumH = 40
	// fullChapH 是章节名那一行的最小高（`min-height: 17px`）。
	fullChapH = 17
	// fullSeekH 是进度那一行的高（`.range { height: 16px }`）。
	fullSeekH = 16
	// fullPlayHit 是 64 的大播放键，fullPlayGlyph 是里面图形的边长（26）。
	fullPlayHit   = 64
	fullPlayGlyph = 26
	// fullBottomH 是整条底栏的高：8 + 40 + 12 + 17 + 12 + 16 + 12 + 64 + 28。
	fullBottomH = fullBottomPadTop + fullSpectrumH + fullBottomGap + fullChapH + fullBottomGap +
		fullSeekH + fullBottomGap + fullPlayHit + fullBottomPadBottom

	// fullColGap 是两栏之间的缝（`.full-main { gap: 64px }`）。
	fullColGap = 64
	// fullCoverMax 是封面最大边（`.full-left { max-width: 440px }`）。
	fullCoverMax = 440
	// fullLeftGap 是封面和歌名那一块之间的缝（`.full-left { gap: 24px }`）。
	fullLeftGap = 24
	// fullInfoH 是歌名那一块的高：歌名 43 + 歌手 24 + 缝 8 + 播放量 17。
	fullInfoH = 92
	// fullLyricsMaxH 是歌词区最高（`.lyrics { max-height: 520px }`）。
	fullLyricsMaxH = 520
	// fullLyHeadH 是歌词来源那一行的高（chip 26）；fullKnobH 是旋钮行的高（32）。
	fullLyHeadH = 26
	fullKnobH   = 32
	// fullRightGap 是右栏三行之间的缝（`.full-right { gap: 8px }`）。
	fullRightGap = 8
)

// fullPadX 是中间和底栏左右的留白：窗口 > 1180 是 80，≤ 1180 是 40，≤ 760 是 20。
func fullPadX(winW float32) float32 {
	switch {
	case winW <= 760:
		return 20
	case winW <= 1180:
		return 40
	}
	return 80
}

// fullNarrowBelow 是窗口宽 ≤ 它时右栏（歌词）整个不画，封面居中（原型 `max-width: 1024px`）。
const fullNarrowBelow = 1024

// fullGeo 是这一帧各块在窗口里的矩形。
type fullGeo struct {
	winW, winH float32
	padX       float32
	narrow     bool
	// mainTop / mainBottom 是中间那一块的上下沿（顶栏下沿、底栏上沿）。
	mainTop, mainBottom float32
	// left 是左栏：封面（cover）+ 下面那一块歌名（info）。
	leftX, leftW float32
	cover        ui.Rect
	info         ui.Rect
	// right 是右栏：歌词来源行、歌词、旋钮行。
	rightX, rightW float32
	lyHead         ui.Rect
	lyrics         ui.Rect
	knobs          ui.Rect
	// cinema 是影院模式下视频那一块和卡拉 OK 那一行。
	cinema, karaoke ui.Rect
}

// fullGeometry 算这一帧的几何。showCover 说封面开着没有，frameH 是左栏那一块的高
// （封面是方的；视频小窗是 16:9）。
func (s *Shell) fullGeometry(c *ui.Context, showCover, video bool) fullGeo {
	winW, winH := c.Size()
	g := fullGeo{winW: winW, winH: winH, padX: fullPadX(winW), narrow: winW <= fullNarrowBelow,
		mainTop: fullTopH, mainBottom: winH - fullBottomH}
	avail := winW - 2*g.padX
	colW := (avail - fullColGap) / 2
	if g.narrow {
		g.leftW = min(360, avail)
		g.leftX = (winW - g.leftW) / 2
	} else {
		g.leftW = min(fullCoverMax, colW)
		g.leftX = g.padX + colW - g.leftW
		g.rightX, g.rightW = g.padX+colW+fullColGap, colW
	}

	// 左栏：封面边长不超过窗口高给的余量（原型在 800 高时刚好 440）。
	side := min(g.leftW, max(240, winH-360))
	coverH := side
	if video {
		coverH = side * 9 / 16
	}
	hc := float32(fullInfoH)
	if showCover {
		hc += coverH + fullLeftGap
	}
	top := max(g.mainTop+36, g.mainTop+(g.mainBottom-g.mainTop-hc)/2)
	g.cover = ui.Rect{X: g.leftX + (g.leftW-side)/2, Y: top, W: side, H: coverH}
	if !showCover {
		g.cover = ui.Rect{}
	}
	infoY := top
	if showCover {
		infoY = top + coverH + fullLeftGap
	}
	g.info = ui.Rect{X: g.leftX, Y: infoY, W: g.leftW, H: fullInfoH}

	// 右栏：从 84 起，歌词最高 520，旋钮行不超过进度条上沿。
	if !g.narrow {
		seekTop := winH - fullBottomPadBottom - fullPlayHit - fullBottomGap - fullSeekH
		maxBottom := seekTop - 2
		colH := float32(fullLyHeadH + fullRightGap + fullLyricsMaxH + fullRightGap + fullKnobH)
		lyH := float32(fullLyricsMaxH)
		if avail := maxBottom - (g.mainTop + 16) - (colH - lyH); avail < lyH {
			lyH = max(avail, 160)
			colH = fullLyHeadH + fullRightGap + lyH + fullRightGap + fullKnobH
		}
		top := max(g.mainTop+16, (g.mainTop+16+g.mainBottom-16-colH)/2)
		g.lyHead = ui.Rect{X: g.rightX, Y: top, W: g.rightW, H: fullLyHeadH}
		g.lyrics = ui.Rect{X: g.rightX, Y: top + fullLyHeadH + fullRightGap, W: g.rightW, H: lyH}
		g.knobs = ui.Rect{X: g.rightX, Y: g.lyrics.Y + lyH + fullRightGap, W: g.rightW, H: fullKnobH}
	}

	// 影院：`.cinema { padding: 0 40px; rows 1fr auto; gap 16 }`，视频 16:9 填满高。
	karH := float32(40)
	vh := max(g.mainBottom-g.mainTop-16-karH, 120)
	vw := vh * 16 / 9
	if maxW := winW - 80; vw > maxW {
		vw, vh = maxW, maxW*9/16
	}
	g.cinema = ui.Rect{X: (winW - vw) / 2, Y: g.mainTop, W: vw, H: vh}
	g.karaoke = ui.Rect{X: 40, Y: g.mainTop + vh + 16, W: winW - 80, H: karH}
	return g
}

// fullEnter 是满屏进场（原型 `full-in`：从下 24 淡入，360ms）。
//
// ⚠️ **只有进场，没有退场**：`motion.FullIn` 带着 Exit，而满屏是整层浮层一起拆掉的——退场时框架
// 会把整棵子树留成「影子」继续画，影子里的滑块（`selfSliderLive` 的画法回调）会去问一个早已
// 过期的元素，当场 panic。原型的 `full-out` 因此做不了；关掉就是直接没了。
var fullEnter = ui.ElementTransition{
	Duration: motion.Slow, Ease: motion.EaseOut,
	Enter: &ui.Motion{Y: 24, Opacity: 0},
}

// fullBgBase 是背景的底：`color-mix(in srgb, tint 30%, #0b0b0c)`。取不到封面色时按原型的
// 兜底 `#222`。关掉虚化背景并挑了颜色时就是那个颜色（原型 `!f.blur && f.bg`）。
func (s *Shell) fullBgBase(url string) ui.Color {
	p := s.pagePrefs()
	if !p.ShowBlurredBackground && p.BackgroundColor != "" {
		if col, ok := parseHexColor(p.BackgroundColor); ok {
			return col
		}
	}
	if s.glowWake == nil {
		s.glowWake = s.wake.invalidate
	}
	tint := ui.Hex("#222222")
	if col, ok := s.tint.glow(url, s.glowWake); ok {
		tint = col
	}
	return tok.Full.Base.Mix(tint, tok.Full.TintMix)
}

// fullBlurOpacity 是模糊封面那一层的不透明度（原型 `.full-blur { opacity: .5 }`）。
const fullBlurOpacity = 0.5

// coverMatch 是「封面在两个地方是同一个东西」的身份。
//
// 🔴 **用自己定义的类型**（框架文档明说：「Use a type of your own for the
// identity, so that it is no other's」）。值是当前这一首的判重标识——换一首歌就是
// 另一个身份，于是转场不会把 A 的封面拉到 B 的位置上。
type coverMatch struct{ id string }

// coverTransition 是封面那一次转场。`Snappy` 是框架自带的弹簧。
func coverTransition(id string) ui.ElementTransition {
	return ui.ElementTransition{Animation: &ui.Snappy, Match: coverMatch{id: id}}
}

// fullPlayer 是全屏播放器自己那一摊。**只由 view 在主线程上读写**（铁律一）。
type fullPlayer struct {
	// open 是它开着没有。
	open bool
	// theater 是影院模式（原型 `S.video === 2`）：视频居中、歌词变成一行卡拉 OK。
	theater bool

	// pop* 是底栏上五个弹层开着没有；同一时刻只开一个（`openFullPop`）。
	popQuality, popRate, popSleep, popEQ, popPrefs bool
	// popWas 记着「按下那一帧弹层是不是开着」：弹层是「按在外面就关」的，按在它自己的
	// 按钮上也算外面，不记的话松手那一下 `Clicked` 会把刚关掉的又开开。
	popWas [5]bool
	// popHeld 是五个按钮各自「按着没有」，用来认出「刚按下的那一帧」。
	popHeld [5]bool

	// lyTrans 是「歌词翻译」chip 按下了没有（原型 `S.lyTrans`，每次打开满屏重新是原文）。
	lyTrans bool

	// partsOpen 只剩 `notfound.go` 在复位它：分集面板已随旧版一起删除，数据层
	// （`app.Parts`）和播放条上的分集抽屉不动。
	partsOpen bool
	// partsRows / partsList / partsRowKeys 同上（分集抽屉 `partdrawer.go` 共用 `partRow`）。
	partsRows    []partRow
	partsList    ui.ListState
	partsRowKeys rowkey.Memo[string, any]

	// bg 是背景那一层的画布：封面糊过一次画进去，稳态下每一帧只是一张图
	// （见 viewFullBackdropCanvas 为什么是画布而不是一层材质）。
	bg *ui.Canvas
	// bgW/bgH 是这张画布的尺寸（窗口改了尺寸要重建）。
	bgW, bgH int
	// bgURL/bgBitmap/bgSigma 是这张画布画的是哪一张封面以及实际使用的模糊半径。
	bgURL    string
	bgBitmap *ui.Bitmap
	bgSigma  float32
	// bgDirty 是「画布要重画」那一下（画它要一个 Painter，只有画的时候有）。
	bgDirty bool
	// bgDraws 是这张画布被重画了几次。**给测试看的**。
	bgDraws int

	// coverRect 是**满屏那张封面（或视频小窗、影院视频）此刻在窗口里的那块**。
	//
	// ⚠️ **弹幕要画在它上面**（见 `danmaku.go`），而弹幕层是**另一个浮层**
	// ——浮层拿不到主树里那个元素的位置，所以这一块由 `viewFullPlayer` 算好写下来。
	// 零值 = 没有封面可放（封面关着）：弹幕那一层就不建。
	coverRect ui.Rect

	// danmaku 是弹幕那一摊跨帧的状态（见 `danmaku.go`）。
	danmaku danmakuLayer

	// txt 是每帧都要的几行字（序号、倍速、偏移、播放模式）按输入记好的那一份：
	// 每帧现拼就是每帧多几次分配（满屏的每帧分配预算盯着，见 framebudget_test.go）。
	txt fullTexts

	// eqSel 是均衡器弹层里的预设下拉展开了没有。
	eqSel bool
}

// partRow 是分集列表里的一行。
//
// 这一份是**播放条上的分集抽屉**（`partdrawer.go`）用的行模型。「正在放的那一集」只有
// 一处认——认两处的话，迟早有一处认错，而那正是用户在找的那一句。
type partRow struct {
	key string
	cid string
	// page 是第几集。播放条那个抽屉画它（见 partdrawer.go）。
	page   int
	title  string
	active bool
}

// viewFullPlayer 画全屏播放器。没开就一个字都不建。
//
// ⚠️ **状态是外面递进来的**（和 `viewPlayBar` 同一个样子）：`viewMain` 那一帧
// 已经取过一份快照了（`st`），这里再取一份就是**两份快照、两个时刻**。
func (s *Shell) viewFullPlayer(c *ui.Context, st app.State) {
	if !s.full.open {
		return
	}
	// 记一笔「满屏建了一遍」——给「一帧建了几遍」那条测试数（见 builds.go）。
	s.builds.full++
	// ⚠️ **每一帧先清掉封面那一块**：弹幕是画在它**上面**的，不清的话画在上一帧的位置。
	s.full.coverRect = ui.Rect{}
	if !st.HasTrack {
		// ⚠️ **没有歌的时候弹幕那一摊也要收掉**，留着锚点和轨道的话换歌回来那一帧会把
		// 上一首的荡回来。
		s.full.danmaku.reset()
		// 🔴 **不能在这里关掉自己**：换歌的那一瞬在线流是后台取流，这几百毫秒里
		// `HasTrack` 恒为 false——关掉的话每一次换歌都会把满屏踢回普通窗口。
		s.viewFullEmpty(c)
		return
	}

	phase := s.app.VideoPhase()
	if phase == app.VideoOff {
		s.full.theater = false
		s.vv.reset()
		if s.videoMadeFull {
			s.setScreenFull(false)
		}
	}
	videoOn := phase == app.VideoLoading || phase == app.VideoOn
	prefs := s.pagePrefs()
	g := s.fullGeometry(c, prefs.ShowCover || videoOn, videoOn && !s.full.theater)

	pud.SetFull(true)
	ui.Overlay(c, func() {
		// 背景先摆：它是整窗的一层，后面那些控件都压在它上面。
		s.viewFullBackdrop(c, st.CoverURL)

		// 铺满整窗的那一层：吃掉底下那一层的点击。
		root := ui.Box(c).Key("full-root").
			Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Modal().Role(ui.RoleDialog).Label("满屏播放器").Transition(fullEnter)
		root.Children(func() {
			s.viewFullTop(c, st, g)
			cinema := s.full.theater && videoOn
			if cinema {
				s.viewFullCinema(c, st, g, phase)
			} else {
				s.viewFullLeft(c, st, g, phase, prefs)
			}
			s.viewFullBottom(c, st, g)
			// 🔴 右栏排在底栏**之后**（后建的在上）：旋钮行和歌词下沿落在底栏的地盘里
			// （1200×800 时旋钮行 y=646，底栏从 591 起，频谱 / 章节名 / 进度那几行是透明的
			// 盒子），排在前面的话旋钮点不到。
			if !cinema && !g.narrow && prefs.ShowLyrics {
				s.viewFullRight(c, st, g)
			}
		})
		if root.OverlayShortcut(0, ui.KeyEscape) {
			switch {
			case s.fullScreen && s.videoMadeFull:
				s.setScreenFull(false)
			case s.full.theater:
				s.full.theater = false
			default:
				s.closeFullPlayer()
			}
		}
	})
	pud.SetFull(false)

	// 弹幕：压在封面（或视频）上。🔴 它必须走 `ui.Overlay`，而且建在满屏那一层**之后**
	// （后建的浮层在上）。
	s.viewFullDanmaku(c, st)

	// 抽屉画在满屏**上面**（原型：满屏开着时 scrim / drawer 的 z-index 抬到 55 / 56，
	// 并且 bottom 归 0——播放条被满屏盖着）。`viewMain` 在满屏开着的时候跳过它们。
	s.viewQueueDrawer(c)
	s.guardAt("评论抽屉", s.viewCommentsDrawer)(c)
}

// fullNothing 是「满屏开着、此刻没有歌」那一句。
var fullNothing = emptyState{
	Title: "暂无播放内容",
	Hint:  "按 Esc 退回去，在主窗口里放几首歌进来",
	Icon:  iconEmptyMusic,
}

// viewFullEmpty 是「满屏开着、此刻没有歌」的那一屏：背景 + 收起键 + 一句话。
// 换歌的那几百毫秒正好会走到这里，所以收起键和抽屉都留着。
func (s *Shell) viewFullEmpty(c *ui.Context) {
	pud.SetFull(true)
	ui.Overlay(c, func() {
		s.viewFullBackdrop(c, "")
		root := ui.Box(c).Key("full-root").
			Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Modal().Role(ui.RoleDialog).Label("满屏播放器")
		root.Children(func() {
			s.viewFullTop(c, app.State{}, fullGeo{})
			ui.Column(c).Key("full-empty").
				Absolute().Left(0).Top(0).Right(0).Bottom(0).
				Center().Gap(6).Children(func() {
				ui.Text(c, fullNothing.Title).FontSize(scale.Font(c, scale.Size16)).TextColor(tok.Full.Text)
			})
		})
		if root.OverlayShortcut(0, ui.KeyEscape) {
			s.closeFullPlayer()
		}
	})
	pud.SetFull(false)
	// 换歌的那几百毫秒里 `HasTrack` 是 false，抽屉得跟着留着。
	s.viewQueueDrawer(c)
	s.guardAt("评论抽屉", s.viewCommentsDrawer)(c)
}

// closeFullPlayer 关掉全屏播放器，顺手把那一摊复位。
func (s *Shell) closeFullPlayer() {
	s.full = fullPlayer{}
	// 没人画频谱了：告诉 App 把 FFT 停掉（它自己不知道满屏关没关）。
	s.app.SetSpectrumWanted(false)
}

// openFullPlayer 开全屏播放器。
//
// 必须有一首歌在放：播放条上那一张封面没有的时候就没有东西可点，而这儿也还有
// 别处会走这条路（快捷键将来就有），所以这里再判一次。
func (s *Shell) openFullPlayer() {
	if !s.pageInput().Playback.HasTrack {
		s.addToast("还没有在放什么，先进一首歌", notice.Warning)
		return
	}
	if s.full.open {
		return
	}
	s.full.open = true
	// ⚠️ **播放条上那个分集抽屉也得收掉**：满屏开着的时候播放条根本不画，于是那个键不在、
	// 浮层也不建，而 `partOpen` 还留着 true，退回普通窗口时这一列会自己弹出来。
	s.partOpen = false
}

// ---- 背景 ----

// viewFullBackdrop 是满屏背景：底色（封面取色混 `#0B0B0C`）+ 一层 50% 的模糊封面。
//
// 封面取不到（本地音乐没有封面地址，见 `app.State.CoverURL`）时只有底色。关掉虚化
// 背景只剩底色（原型 `.full.no-blur .full-blur { display: none }`）。
func (s *Shell) viewFullBackdrop(c *ui.Context, url string) {
	bg := s.fullBgBase(url)
	ui.Box(c).Key("full-backdrop").
		Absolute().Left(0).Top(0).Right(0).Bottom(0).
		Background(bg).
		Children(func() {
			if s.pagePrefs().ShowBlurredBackground {
				s.viewFullBackdropCanvas(c, url)
			}
		})
}

// clarityBlurSigma 是某一档在**画布上**的模糊半径（画布像素）。
func clarityBlurSigma(clarity app.BackdropClarity, canvasW, winW float32) float32 {
	blur := float32(clarity.BlurDIP())
	if winW <= 0 {
		return blur
	}
	return blur * canvasW / winW
}

// fullBackdropBlur 是背景那一层糊掉多少（DIP，标准差）。
const fullBackdropBlur = 32

// fullBackdropSigma 是画布上的模糊半径（画布像素）。
func fullBackdropSigma(canvasW, winW float32) float32 {
	if winW <= 0 {
		return fullBackdropBlur
	}
	return fullBackdropBlur * canvasW / winW
}

// fullBackdropMaterial 是背景那一层糊的那一份材质：框架自带的 `glass.Blur`，不是自绘的
// 等效效果。sigma 是**画布上的**半径。
func fullBackdropMaterial(sigma float32) ui.Material { return glass.Blur{Radius: sigma} }

// backdropChanged 报「背景那张画布要不要重画」：换了封面、换了位图、窗口改了尺寸或者
// 实际 blur sigma 变了才重画。五个判据分开比（拼字符串每帧一次分配）。
func (f *fullPlayer) backdropChanged(url string, bm *ui.Bitmap, w, h int, sigma float32) bool {
	return f.bgURL != url || f.bgBitmap != bm || f.bgW != w || f.bgH != h || f.bgSigma != sigma
}

// viewFullBackdropCanvas 是背景那一层：封面糊过一次画进一张画布，之后每一帧只是把那张
// 画布放大铺上去（毛玻璃每帧重渲在软件渲染上要多花 80~140 毫秒）。
func (s *Shell) viewFullBackdropCanvas(c *ui.Context, url string) {
	winW, winH := c.Size()
	clarity := s.app.SettingsSnapshot().BackdropClarity
	div := float32(clarity.CanvasDivisor())
	cw := max(int(winW/div), 1)
	ch := max(int(winH/div), 1)
	// 背景要的源图是「画布尺寸 × 屏幕倍率」，不是整窗那么大（票 #664 / #666）。
	st := s.pageImages().GetSizedStoredAt(url, float32(cw), float32(ch), max(cw, ch))
	if !st.Ready() {
		// 封面还没来：底下是底色（外头那一层），画布先不动。
		return
	}
	if s.full.backdropChanged(url, st.Bitmap, cw, ch, clarityBlurSigma(clarity, float32(cw), winW)) {
		if s.full.bg == nil || s.full.bgW != cw || s.full.bgH != ch {
			s.full.bg = ui.NewCanvas(cw, ch)
			s.full.bgW, s.full.bgH = cw, ch
		}
		s.full.bgURL, s.full.bgBitmap = url, st.Bitmap
		s.full.bgSigma = clarityBlurSigma(clarity, float32(cw), winW)
		s.full.bgDirty = true
	}
	if s.full.bg == nil {
		return
	}
	// 画布的内容要一个 Painter，只有画的时候有：放在 Draw 回调里。
	ui.Box(c).Key("full-backdrop-canvas").
		Absolute().Left(0).Top(0).Right(0).Bottom(0).
		Draw(func(p *ui.Painter, r ui.Rect) {
			if !s.full.bgDirty {
				return
			}
			s.full.bgDirty = false
			s.full.bgDraws++
			box := ui.Rect{W: float32(cw), H: float32(ch)}
			s.full.bg.Draw(p, func(cp *ui.Painter) {
				cp.Image(st.Bitmap, box, ui.Cover)
				fullBackdropMaterial(clarityBlurSigma(clarity, float32(cw), winW)).
					PaintMaterial(cp, box, [4]float32{})
			})
		})
	// 把它放大铺满整窗，不透明度 50%。
	ui.Image(c, s.full.bg.Bitmap()).Fill().Fit(ui.FillBox).Opacity(fullBlurOpacity)
}

// ---- 分集的行模型（播放条上的分集抽屉共用）----

// partRowsFor 是分集列表这一帧要画的那几行。
//
// ⚠️ 铁律四：行的 key 是**这一集的 cid**，不是行号（见 queue_drawer.go 的
// queueRowKey）。
func partRowsFor(parts []bili.VideoPart, cur app.Item) []partRow {
	rows := make([]partRow, 0, len(parts))
	for _, p := range parts {
		rows = append(rows, partRow{
			key:    "part:" + p.Cid,
			cid:    p.Cid,
			page:   p.Page,
			title:  partTitle(p),
			active: p.Cid == cur.Cid,
		})
	}
	return rows
}

// partTitle 是一行显示的字。分 P 的标题是空的时说「第 N 集」。
func partTitle(p bili.VideoPart) string {
	if p.Part != "" {
		return p.Part
	}
	return "第 " + strconv.Itoa(p.Page) + " 集"
}

// fullMono 是满屏里那种「等宽小字」：Mono、给定字号 / 字重 / 字距、颜色。
func fullMono(c *ui.Context, text string, size float32, weight int, tracking float32, col ui.Color) ui.Element {
	return typo.Mono(ui.Text(c, text)).FontSize(size).FontWeight(weight).LetterSpacing(tracking).
		TextColor(col).SingleLine()
}

// fullBarAnim 是「淡入淡出 + 位移」那一档动画：「减少动态效果」时用一次到位。
//
// 全屏播放器自己不再用它（旧版藏 / 显界面的动画已删），首页的「今日一张」轮播
// （`homepage_overview.go`）还在用，所以留在这里。
func fullBarAnim(c *ui.Context) ui.Animation {
	if c.Preferences().ReduceMotion {
		return motion.Snap
	}
	return ui.Smooth
}

// fullTexts 记着几行「由几个数拼出来」的字，输入不变就原样返回。
type fullTexts struct {
	nowIdx, nowN int
	now          string
	rate         float64
	rateStr      string
	off          int
	offStr       string
	mode         app.PlayMode
	modeStr      string
}

// nowPlaying 是顶栏正中那一行 `NOW PLAYING · i / n`。
func (t *fullTexts) nowPlaying(idx, n int) string {
	if t.now == "" || t.nowIdx != idx || t.nowN != n {
		t.nowIdx, t.nowN = idx, n
		t.now = "NOW PLAYING"
		if n > 0 && idx >= 0 {
			t.now += " · " + strconv.Itoa(idx+1) + " / " + strconv.Itoa(n)
		}
	}
	return t.now
}

// rateText 是倍速键上的字（`1×`）。
func (t *fullTexts) rateText(r float64) string {
	if t.rateStr == "" || t.rate != r {
		t.rate, t.rateStr = r, fullRateText(r)
	}
	return t.rateStr
}

// offsetText 是歌词偏移的读数。
func (t *fullTexts) offsetText(ms int) string {
	if t.offStr == "" || t.off != ms {
		t.off, t.offStr = ms, fullOffsetText(ms)
	}
	return t.offStr
}

// modeLabel 是播放模式键的无障碍名字。
func (t *fullTexts) modeLabel(m app.PlayMode) string {
	if t.modeStr == "" || t.mode != m {
		t.mode, t.modeStr = m, "切换播放模式："+m.Desc()
	}
	return t.modeStr
}
