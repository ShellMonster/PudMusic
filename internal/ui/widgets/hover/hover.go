// 封面上的 hover 反馈：列表行和网格卡片那两套，外加「正在放」那个跳动波形。
//
// Biu 上游这三块是 `components/music-list-item/index.tsx:100-107`（列表行的压暗 +
// 播放三角 + 序号位置的跳动波形）、`components/music-card/index.tsx:39-47, :50-55`
// （网格卡片右下角的播放钮 + 封面底部的渐变条）。本项目两边都缺：列表行只有一个
// 「序号换成一个写着『播放』的按钮」，网格卡片一点 hover 反馈都没有，序号位置摆的是
// 一句静态的「正在播放」。
//
// # 尺寸都是抄上游的，一个没改
//
//	列表行：48×48 封面上盖 35% 黑 + 一个 20px 的白色三角，指针停在三角上时放大 110%
//	网格卡：右下角浮出 40px 主色圆播放钮，封面底部一条 black/90 渐变（左播放量右时长）
//	淡入淡出 200ms（上游两处都是 `transition-opacity duration-200`）
//
// # ⚠️ 反馈是**每一帧都建**的，指针不在也建
//
// 这是这一包唯一一处要动脑的地方。淡入要跨帧：三角画出来那一帧不透明，它得从
// **上一帧**的值接着往下走。而 MyGo 的动画值记在**节点**上（`ui.AnimateTo` 的
// `st.motions`），节点的身份跨帧不变才有那份状态。于是这一块不能写成
//
//	if hover { 建三角 }
//
// 那样节点每帧都是新的，`Animate` 走的是 `channel(..., made=true)` 那一支、直接返回
// 目标值（`mygo/ui/motion.go` 的 AnimateTo），于是**淡入变成「啪」地一下出现**——不
// 报错、不闪红，只是没动效。
//
// 现在的写法是每帧都建、完全透明时用 `Invisible()` 收掉，理由逐条写在 `show` 那里。
package hover

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// frameVisible 是「画这一帧的那个窗口，用户现在看得到吗」。
//
// 为什么是包级而不是参数：`Wave` 在控件层，拿不到 `Shell`；而它每帧都要问
// 同一个问题。MyGo 的 view 全部跑在主线程上，同一时刻只画一个窗口，所以
// `Shell.View` 每帧开头设一次就够，画完不用恢复——下一个窗口画自己那一帧时会
// 重新设置（`internal/ui/shell.go` 的 `View`）。
//
// 零值必须是「看得见」：这个包自己的测试直接调 `Wave`，没有 Shell 来设置；
// 设成 false 的话那些测试里的波形会永远不动，而它们测的正是「动」。
//
// ⚠️ **这个开关只管「要不要下一帧」，不管「这一帧画不画」**：看不见的时候
// 能走到这里的那一帧照样画完整（也许是某条后台路径送进来的），只是画完不再
// 要下一帧。真正让帧停下来的那一半在 `wakeHandle.invalidate`（wake.go）。
var frameVisible = func() *atomic.Bool {
	v := &atomic.Bool{}
	v.Store(true)
	return v
}()

// SetFrameVisible 告诉这个包「这一帧所在的窗口看不看得见」。只由
// `internal/ui.Shell.View` 每帧调一次。
func SetFrameVisible(visible bool) { frameVisible.Store(visible) }

const (
	// Fade 是淡入淡出的时长。Biu 那两处都是 `duration-200`。
	Fade = 200 * time.Millisecond

	// dimAlpha 是列表行封面上那层压暗。**35% 黑**，逐字抄上游
	// （`music-list-item/index.tsx:100-107` 的 `bg-black/35`）。
	dimAlpha = 0.35

	// triSize 是列表行封面上那个播放三角的边长。上游是 20。
	triSize = 20

	// triScale 是指针停在三角上时它放大的倍数。**110%**，抄上游那一处。
	triScale = 1.1

	// btn 是网格卡片右下角那个圆播放钮的直径。上游是 40。
	btn = 40

	// btnInset 是那个播放钮离封面边留多少。
	btnInset = 8

	// barH 是封面底部那一条渐变多高。
	barH = 22

	// barTextInset 是渐变上那几个字离右边多远。**播放钮的宽再加一个 btnInset**
	// （40 + 8）：按钮从右边往里让了 btnInset，所以「播放钮占的那一条 + 再空一个
	// btnInset」正好是字该让出的宽——只让 `btn` 的话字的右边缘和按钮的左边缘**贴
	// 着**，而那一列 11 号的字读起来是「顶着」按钮的。
	barTextInset = btn + 2*btnInset

	// triTint 是播放三角和封面底部那几个字的白。**带一点透明**：纯白压在一张浅色
	// 封面上会看不见，而封面是什么颜色我们不知道。
	triTint = 0.92
)

// PlayFrom 是「按下去从这一首往后听」这句话。
//
// ⚠️ 「按下去」和**双击那一行**是同一件事（`playFrom`），所以提示是这一句而不是
// 「播放」：说「播放」的话用户第一次按下去会发现整张歌单都换了队列——那正是它该做
// 的事，只是那句话没有提前说。
const PlayFrom = "从这一首往后听"

// PlayNow 是「按下去只放这一首」这句话。
//
// ⚠️ 和 PlayFrom 分开：首页「新歌速递」那张卡片按下去换掉整个队列、只放这一首
// （上游 `new-music-top.tsx` 的 `handlePlay`），说「从这一首往后听」会提前许诺一
// 件那一下不做的事。
const PlayNow = "播放"

// PlayingMark 是「这一行正在放」那句话，无障碍标签和无头 Tester 都靠它认这一行。
//
// ⚠️ **它的唯一出处在这里**（`list.PlayingMark` 和 `internal/ui` 那个都指回来）。
// 同一个应用里两套列表写着两种说法，用户会以为是两个意思——那是 BMG-56 已经付过账
// 的一个结论。
const PlayingMark = "正在播放"

// FromLabel 是列表行封面上那个播放三角的标签。**带上歌名**：读屏软件读的是标签而不
// 是画出来的那几个字，光说「从这一首往后听」的话用户听到的是一句没头没尾的话，得自
// 己猜是哪一首。顺带它让这一行在 Tester 里可认（`Tester.Find` 认标签）。
func FromLabel(title string) string { return title + " " + PlayFrom }

// PlayLabel 是「只放这一首」那枚播放钮的标签，理由同 FromLabel。
func PlayLabel(title string) string { return title + " " + PlayNow }

// PlayingLabel 是那个跳动波形的标签，理由同 FromLabel。
func PlayingLabel(title string) string { return title + " " + PlayingMark }

// ─────────────────────────────────────────────────────────────────────────────
// 淡入
// ─────────────────────────────────────────────────────────────────────────────

// fade 问 e 在 Fade 这段时间里走到 on 那个值没有，返回此刻的值（0~1）。
//
// ⚠️ **动画只装在 dim 这一个元素上**，别的层用返回值——两份各跑一份动画的话它们会
// 差一帧，而那种差在画面上是「压暗已经上来了、三角还差一丝」。
//
// ⚠️ MyGo 的 `Animate` 在系统开着「减少动态效果」时直接给目标值（`motion.go` 里
// `rt.preferences().ReduceMotion`），那是系统设置而不是我们这一层的判断，别在这里
// 自己绕过去。
func fade(e ui.Element, key string, on bool) float32 {
	if on {
		return e.Animate(key, 1, Fade)
	}
	return e.Animate(key, 0, Fade)
}

// show 收掉一层：完全透明时把这一层**藏掉**。
//
// ## 为什么是 `Invisible()` 而不是「不建」
//
// 见文件头：不建就没有动画状态，淡入变成「啪」地一下。`Invisible()` 的原话是
// 「keeps their room in the layout but draws nothing and **takes neither the
// pointer nor the focus**, and **has no text to find**」（`mygo/ui/api_gen.go`），三件
// 事正好都是这里要的：
//
//   - 房间还在 → 行高不变，三角淡进来的那一帧封面不会跳一下；
//   - 不吃指针 → 指针不在的时候那个播放三角点不到（不然它会盖掉封面）；
//   - **找不到它的字** → 无头 Tester 里 `Find`/`HasText` 找不到它，于是「指针没移
//     上去时没有播放三角」这句话仍然是一句能断言的话。
//
// ⚠️ 判据写 `a <= 0` 而不是 `!on`：`on` 是指针在不在，`a` 是**画出来有多浓**。淡出
// 的那 200ms 里 `on` 已经是 false 了，而那一层还得再画 200ms 才收掉。
func show(e ui.Element, a float32) ui.Element {
	if a <= 0 {
		return e.Invisible()
	}
	return e.Opacity(a)
}

// drawTri 画一个播放三角，r 是它要待的那个盒子，c 是颜色。
//
// ⚠️ **左边多留 15%、上下各留 12%**：一个正三角形放在正方形的正中间，视觉上是偏左的
// （三角形的重心不在几何中心）。不挪的话用户会觉得那个三角没对准封面的中心。
func drawTri(p *ui.Painter, r ui.Rect, c ui.Color) {
	var path ui.Path
	path.MoveTo(r.X+r.W*0.15, r.Y+r.H*0.12).
		LineTo(r.X+r.W*0.15, r.Y+r.H*0.88).
		LineTo(r.X+r.W*0.88, r.Y+r.H*0.5).
		Close()
	p.FillPath(&path, c)
}

// ─────────────────────────────────────────────────────────────────────────────
// 列表行
// ─────────────────────────────────────────────────────────────────────────────

// RowCover 是「列表行的封面 + 它上面那一圈 hover 反馈」。
//
// 代替直接调 `imgcache.View`：反馈是**贴在封面上**的，它得知道封面有多大、又摆在
// 哪儿。调用方手里只有「一个 48×48 的封面」，所以连盒子一起给了。
//
// on 是**指针在这一行上**。play 是按下去做的事，为 nil 时那一块不可按。
//
// ⚠️ 这里的封面是方的（列表 48、紧凑 32），所以只收一个 size；网格卡片那边的封面
// 是 140 高跟着格子宽走，那一边收两个（见 CardCover）。
func RowCover(c *ui.Context, cache *imgcache.Cache, url string, size float32,
	on bool, title string, play func()) {
	t := c.Theme()
	key := FromLabel(title)

	// ⚠️ **外面这一层盒子是必需的**：压暗和那个三角用 `Attach` 贴在**父盒子的内边距
	// 框**上（`mygo/ui/api_gen.go` 的 Attach），而调用方给的父是那一行的 `ui.Row`
	//——贴上去就成了「浮在这一行中间」，横竖都不对。裹一层和封面一样大的盒子，贴的
	// 是它。
	//
	// `Clip()` 不能省：封面是圆角的，压暗那一层不裁就会在四个角上盖出方的黑边。
	ui.Box(c).Size(size, size).Radius(t.Radius).Clip().Children(func() {
		imgcache.View(c, cache, url, size, size)

		// ⚠️ **压暗和三角是这一层的两个兄弟，不是一个套一个**：`Attach` 贴在**直接
		// 那个父**的盒子上，而套一层时内层贴到的是**外层自己那个盒子**——而外层刚
		// 被 `Attach` 拿掉了布局，量不出盒子来。实测那样画出来的三角浮在封面的左上
		// 角外面（而不是正中），而屏幕上只是「三角歪了」，一眼看不出是哪儿错了。
		//
		// 顺序不能换：后画的盖在先画的上面（绘制和命中都走树序，`mygo/ui/runtime.go`
		// 的 `rt.hits`），三角要压在压暗之上。
		//
		// ⚠️ 这两层都**不占布局**（`Attach` 把它们从父的布局里拿出来了），所以指针
		// 移上去的时候那一行不会横向跳一下。
		dim := ui.Box(c).Key(key+":dim").Size(size, size).Radius(t.Radius).
			Background(ui.RGBA(0, 0, 0, dimAlpha)).
			Attach(ui.AnchorCenter, ui.AnchorCenter)
		// **动画只装在压暗这一层上**，三角用同一个返回值（理由见 fade）。
		a := fade(dim, key+":fade", on)
		show(dim, a)

		tri := ui.Box(c).Key(key+":tri").Size(triSize, triSize).
			Label(key).Tooltip(PlayFrom).
			Attach(ui.AnchorCenter, ui.AnchorCenter).
			Draw(func(p *ui.Painter, r ui.Rect) {
				drawTri(p, r, ui.RGBA(255, 255, 255, triTint))
			})
		show(tri, a)

		// ⚠️ **`Hovered` / `Clicked` 要在它建完之后立刻问**（同 `list/card.go` 里那一
		// 处）：它们报的是这一帧指针在这一格上发生了什么，而那一格得先被画出来才问
		// 得到。
		if play != nil && tri.Clicked() {
			play()
		}
		if tri.Hovered() {
			// 上游是 `hover:scale-110`。`Scale` 的原点默认是盒子中心
			//（`mygo/ui/transform.go` 的 transformAt），所以是往外涨而不是往一边偏。
			_ = tri.Scale(triScale, triScale)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 网格卡片
// ─────────────────────────────────────────────────────────────────────────────

// CardCover 是「网格卡片的封面 + 它上面那一圈 hover 反馈」。
//
// playCount 是封面底部那一行左边的播放量、clock 是右边的时长。**任一个是空串就不画
// 那半边**（本地音乐没有播放量，见 `list/grid.go` 里那一段注释）——画一列空白比不画
// 更难看，而用户看见半行字会以为这一格「数据不全」。
//
// ⚠️ 网格那一档**不给卡片加底板**：上游的 `music-card/index.tsx:31` 上没有任何背景
// 类，两边都是「封面 + 两行字浮在页面上」，那不是差距（票面「不要做」里点名了）。
func CardCover(c *ui.Context, cache *imgcache.Cache, url string, w, h float32,
	on bool, playCount, clock, title string, play func()) {
	cardCover(c, cache, url, w, h, on, playCount, clock, FromLabel(title), PlayFrom, play)
}

// CardCoverOne 是同一圈反馈，但按下去**只放这一首**（`PlayNow`）。
//
// 首页「新歌速递」那一块用它：那张卡片单击就换掉整个队列只放这一首（上游
// `new-music-top.tsx` 的 `handlePlay`），和网格卡片「从这一首往后接」不是一件事。
// 两处的视觉是同一套——同一个应用里同一个手势不该长出两个样子。
func CardCoverOne(c *ui.Context, cache *imgcache.Cache, url string, w, h float32,
	on bool, title string, play func()) {
	cardCover(c, cache, url, w, h, on, "", "", PlayLabel(title), PlayNow, play)
}

// cardCover 是 CardCover / CardCoverOne 共用的那一层：封面 + 上面那一圈反馈。
func cardCover(c *ui.Context, cache *imgcache.Cache, url string, w, h float32,
	on bool, playCount, clock, label, tip string, play func()) {
	t := c.Theme()

	ui.Box(c).Size(w, h).Radius(t.Radius).Clip().Children(func() {
		imgcache.View(c, cache, url, w, h)
		cardOverlay(c, w, h, on, playCount, clock, label, tip, play)
	})
}

// cardOverlay 画网格卡片封面**上面**那一圈。
func cardOverlay(c *ui.Context, w, h float32, on bool, playCount, clock, label, tip string, play func()) {
	t := c.Theme()
	key := label

	bar := ui.Box(c).Key(key+":bar").Size(w, barH).
		// 上游是 `bg-gradient-to-t from-black/90`：`from-` 那一头在下边，也就是封面的
		// 底边，往上淡出。`Gradient` 的角度是「从竖直方向顺时针起算，180 是从上到
		// 下」，所以不透明那一头要放在第二个参数上。
		Gradient(ui.RGBA(0, 0, 0, 0), ui.RGBA(0, 0, 0, 0.9), 180).
		Attach(ui.AnchorBottomLeft, ui.AnchorBottomLeft)
	a := fade(bar, key+":fade", on)

	show(bar, a).Children(func() {
		// ⚠️ **`Padding` 的四个值是 上 / 右 / 下 / 左**（CSS 那一套）：这一层**右边**
		// 留 56（`barTextInset`）——那个播放钮就浮在右下角（按钮宽 40 + 离右边 8），
		// 字让得不够就有一半压在按钮底下，用户读到的是「播放量 1.2 万」和一个「时长
		// 没画完的 3:0」；**左边**留 8，播放量贴着封面左边正是上游的样子。
		//
		// 左右写反的话时长压在播放钮底下，而那种错在画面上只是「时长看着偏了一点」。
		ui.Row(c).FillWidth().FillHeight().
			Padding(0, barTextInset, 0, btnInset).Children(func() {
			// ⚠️ **时长那一块是 Grow + 右对齐，播放量不是**：Row 默认把子元素**从左边一
			// 个挨一个**摆，而时长是这一行里最后一块，它要贴着那个播放钮的左边。给它
			// `Grow(1)` + 右对齐，两种情形都对：左边有播放量时它被推到右边，只有时长
			// 时它自己占满整行、仍然贴着右边。（第一版把它放在左边，于是没有播放量的
			// 那一半——本地音乐和首页——时长浮在封面左下角，看着像另一个控件。）
			if playCount != "" {
				ui.Text(c, playCount).FontSize(scale.Font(c, 11)).
					TextColor(ui.RGBA(255, 255, 255, triTint)).MaxLines(1)
			}
			if clock != "" {
				ui.Text(c, clock).Grow(1).TextAlign(ui.End).FontSize(scale.Font(c, 11)).
					TextColor(ui.RGBA(255, 255, 255, triTint)).MaxLines(1)
			}
		})
	})

	// 右下角那个主色圆播放钮。上游 `music-card/index.tsx:39-47`。
	circle := ui.Box(c).Key(key+":btn").Size(btn, btn).Radius(btn/2).
		Background(t.Accent).
		Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Right(btnInset).Bottom(btnInset)

	show(circle, a).Children(func() {
		e := ui.Box(c).Size(btn, btn).Label(key).Tooltip(tip).Center().
			Draw(func(p *ui.Painter, r ui.Rect) {
				// 三角缩到圆的六成：整个圆那么大的三角会顶出圆外。
				drawTri(p, shrink(r, 0.4), t.AccentText)
			})
		if play != nil && e.Clicked() {
			play()
		}
	})
}

// shrink 把 r 往里收掉 f（0~1），按比例。
func shrink(r ui.Rect, f float32) ui.Rect {
	dx, dy := r.W*f/2, r.H*f/2
	return ui.Rect{X: r.X + dx, Y: r.Y + dy, W: r.W - 2*dx, H: r.H - 2*dy}
}

// ─────────────────────────────────────────────────────────────────────────────
// 正在放
// ─────────────────────────────────────────────────────────────────────────────

const (
	// waveBars 是那个波形有几根竖条。上游那一段是四根。
	waveBars = 4

	// waveBarW 是每根竖条多宽，waveGap 是两根之间空多少。4×2 + 3×2 = 14 宽，摆在序号
	// 那一列（40）里正好居中。
	waveBarW = 2
	waveGap  = 2

	// waveMin / waveMax 是竖条最短和最长时多高，waveH 是这一块盒子多高
	// （12 号字的行高，和原来那一句「正在播放」占的一样高）。
	waveMin = 3
	waveMax = 11
	waveH   = 14

	// wavePeriod 是跳一轮要多久。上游那一段是 800ms 一轮，四根条子各错开四分之一
	// 轮，于是四根永远不同相——同一轮里四根一起落下去的话看着像闪了一下。
	wavePeriod = 800 * time.Millisecond
)

// Wave 是「这一行正在放」那个标记：序号位置上的一段跳动波形。
//
// ## 为什么把它做成会动的
//
// 早先那一版是一句 11px 的「正在播放」，理由写在 `list.PlayingMark` 顶上（「跳要一帧一
// 变，而快速切歌时帧率本来就紧」）。**这一版换掉了那个理由**：界面骨架已经和上游对
// 上了，而这一句字和上游的波形是两回事——用户扫一眼列表时，眼睛先被动的那个抓住。
//
// ⚠️ **窗口看得见的时候它每一帧都要一帧**（`AnimationFrame`）。这一帧的成本是重画一
// 次画面（`mygo/ui/runtime.go` 的 `next()`：`animating` → `requestFrame`），而屏幕上
// 同时最多只有一行在放。窗口看不见时不要（票 #156，见 `frameVisible`）——那是
// 「后台还在每 50ms 叫醒一次」时风扇和电池的来源。
//
// ## color 是**调用方给的**
//
// 波形摆的那一格在选中时是**主色底**（`ui.List` 自己刷的），而主色底上画一条主色波形等
// 于没画。所以这一格颜色是行自己判的（和它的歌名同一套判据），这里不猜。
//
// ⚠️ **标签必须带歌名**（`PlayingLabel`）：理由同文件头那一段——读屏软件读的是标签。
func Wave(c *ui.Context, key string, width float32, color ui.Color, title string) {
	wave := ui.Box(c).Key(key).Size(width, waveH).Label(PlayingLabel(title))
	phase := wavePhase(c, wave, key)
	wave.Draw(func(p *ui.Painter, r ui.Rect) {
		// 竖条整体居中：4×2 + 3×2 = 14 宽，在 40 宽的序号那一列里两边各空 13。
		x0 := r.X + (r.W-float32(waveBars*waveBarW+(waveBars-1)*waveGap))/2
		for i := range waveBars {
			// 每根条子错开 1/4 轮，四根于是永远不同相。
			h := waveHeight(phase + float32(i)/waveBars)
			p.Fill(ui.Rect{
				X: x0 + float32(i)*(waveBarW+waveGap),
				Y: r.Y + (r.H-h)/2,
				W: waveBarW, H: h,
			}, color, waveBarW/2)
		}
	})
}

// wavePhase 是此刻的相位（0~1），并且在**钟走了、窗口也看得见的时候**要下一帧。
//
// ⚠️ **看不见时 `seen` 照样更新，只是不要帧**：恢复可见之后那一次比较仍然
// 「钟走了」，于是会重新要帧，波形也从当前时刻接着跳。要是不更新 `seen`，
// 恢复后那一次也一样会要帧——两者在行为上一样，但更新让「这个字段是上一次
// 看表的时间」这条意思保持诚实，而不是变成「上一次要帧的时间」。
//
// ## 为什么不直接用 `ui.Loop`
//
// `Loop` 每一帧都调 `AnimationFrame()`（`mygo/ui/widgets.go`），而 Tester 的
// `settle()` 只要画面要了帧就一直画到 20 帧为止（`mygo/ui/headless.go`）——于是**只要
// 屏幕上有一行在放，每一次 `tt.Frame()` 都贵二十倍**。实测那一条把 `internal/ui` 整包从
// 27 秒拉到 57 秒，而每一张票的评审都要付这 30 秒。
//
// ## 判据是「钟有没有走」，不是「相位有没有变」
//
// 两者在真窗口里是一回事，而**在无头 Tester 里差很远**：它的钟站着不动
// （`headless.go`：「Its clock stands still unless the test moves it with Advance」）。
//
//   - 真窗口：`runFrame` 每一帧都取一次墙钟（`runtime.go` 的 `now := rt.now()`，然后
//     `rt.c.reset(now, …)`），一帧一个时刻，两帧不会相同 → 每帧都要一帧（窗口看得见
//     时）→ 波形照跳。
//   - Tester：`settle()` 里那 20 帧**共用一个时刻** → 只有第一帧要 → `settle()` 画两
//     帧就收工。测试要看哪一刻的样子自己 `Advance` 多少（见 `hover_test.go` 的
//     `advance`：`Advance` 之后还要补一帧 `Frame()`——`Advance` 只画它那一刻之前到点的
//     那些帧，刻意的，免得在同一个时刻上白画）。
//
// ⚠️ **判据不是「相位有没有变」**：那样它在 Tester 里会**彻底不动**——因为没有下一帧
// 就没人问「相位变没变」，而没有下一帧就没人看（死锁）。问钟不会：那一次 `Advance` 推了
// 钟，就有一帧跑起来，帧里钟和上一帧不一样，于是它又能要下一帧。
func wavePhase(c *ui.Context, wave ui.Element, key string) float32 {
	now := c.Now()
	start := ui.Local(wave, key+":start", func() time.Time { return now })
	// seen 是上一次要帧时那一刻的时刻。零值 = 从来没要过，于是第一帧一定会要。
	seen := ui.Local(wave, key+":seen", func() time.Time { return time.Time{} })
	if !now.Equal(*seen) {
		*seen = now
		// 看不见的窗口不要再要帧（票 #156）：它每帧都请求下一帧，而
		// `App.tick` 又在放歌时每 50ms 叫醒每个窗口一次——两条链合起来，
		// 一扇收在后台的窗口会一直按帧重画，用户能感到的是风扇和电池。
		if frameVisible.Load() {
			c.AnimationFrame()
		}
	}
	return float32(now.Sub(*start)%wavePeriod) / float32(wavePeriod)
}

// waveHeight 是某一根竖条此刻多高。
func waveHeight(phase float32) float32 {
	// 一个正弦：phase 走 0~1 是一轮，条子从最短走到最长再走回来。
	s := math.Sin(2 * math.Pi * float64(phase))
	return waveMin + (waveMax-waveMin)*float32((s+1)/2)
}
