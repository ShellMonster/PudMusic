package ui

// 迷你播放器那个「歌词卷帘」的**两道轴**（票 #307）。
//
// # 参考的是哪一个
//
// 用户 2026-10-27 给的参考是 <https://qinggan.geekaso.com> 左侧那一栏
// 「西行纪 · 青甘大环线」的卷轴。它的骨架是三样：
//
//	#panelHead   天杆（顶杆，34px 贯通圆杆）
//	#panelReveal 纸身（clip-path 从下往上露出）
//	#panelTail   地轴（26px 贯通圆杆，跟着纸的下沿走）
//
// 而**地轴是跟着纸的下沿一起走的**：它 `transform: translateY(...)`，位置由
// 弹簧的 `p` 算出来——所以卷帘回弹的时候，**地轴跟着一起抖**。
//
// # 只借骨架，不借美术
//
// ⚠️ **不抄羊皮纸 + 木杆那一套**（票面原话）：我们自己的纸身还是那一块
// 半透明面板（`t.Surface` + 现有那圈影），轴也只是**一根圆角条 + 一道高光
// + 一点阴影**，颜色全部从**当前主题**推（`base` = 面板自己的底色，
// `t.Text` = 文字色）——所以深浅两套都跟着主题走。
//
// # 两道轴各自在哪儿
//
//   - **上轴（天杆）**：**迷你窗顶边**那一道，**一直都在**（收着的时候也在）。
//     票面原话：「上轴 = 迷你窗顶边那一道」。
//     ⇒ 参考页那个卷起来的状态**只露天杆、地轴藏着**（`#panelTail` 的
//     `visibility` 在 `pc <= 0.02` 时是 hidden）——所以收着的时候只留上轴，
//     跟它一样。
//   - **下轴（地轴）**：**卷帘的下沿**那一道，**跟着下沿一起走**，回弹时跟着
//     抖。⇒ 它是**根**的绝对定位孩子（不是卷帘那一段的）：根的高度就是窗的
//     高度，而窗高每帧跟着弹簧走——所以**轴永远贴着窗的下沿**，下沿到哪
//     轴到哪。
//
// ⚠️ **为什么下轴不能画在卷帘那一段里面**：窗高是**原生**那一边的事，而
// `SetSize` 改完要等下一帧才生效——于是**画这一段的时候，窗还是上一帧的
// 高度**。卷帘那一段的底边会比窗的下沿低（展开中）或高（收起中）**一帧的
// 差**（展开时每帧差 3~5 像素）——画在那一段里的话，**地轴有一截在窗外面**
// （被裁掉，看不见）。⇒ 画在**根**上，它才是贴着**窗**的下沿。

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/egoist/mygo/ui"
)

const (
	// miniCurtainTopAxisH 是**上轴**（天杆）那一道的高度。
	//
	// ⚠️ **6 是被封面那一列逼出来的**：迷你卡片的内边距是 6（`miniPad`），
	// 封面落在 (6,6)-(78,78)（`TestTheMiniCoverColumnIsBiuWidth` 钉着那个
	// 位置，而封面图从 y=7 起——格子外面还有 1 像素的边）。而天杆占的就是
	// 迷你窗顶边那一条。⇒ **再高 1 像素就压到封面的第一行**，那个测试立刻
	// 红（`miniCoverBox` 量的是封面**颜色**那一块的框）。
	//
	// ⚠️ **它只有 6 高，而下轴有 14**：那一扇窗只有 84 高，封面从 y=6 就开始
	// 了——天杆往下一寸就压住封面了。⇒ 上轴是**贴着顶边的一道细杆**，
	// 下轴是**卷帘下沿那一根**（那一头有地方给它）。
	miniCurtainTopAxisH = 6

	// miniCurtainAxisH 是**下轴**（地轴）那一道的高度。
	//
	// ⚠️ **14**：参考页那个地轴是 26（它那一栏 338 宽），按迷你窗的比例
	// （320 宽、84 高的卡片）落在票面给的「12~16」那一档，取中间这个
	// **14**。⇒ 圆角半径就是 7（14 的一半 = 胶囊形），而**根那面板展开时的
	// 下圆角也是 7**——两个圆弧正好对上（见 `miniplayer.go` 里那一段）。
	miniCurtainAxisH = 14

	// miniCurtainTopAxisKey 是上轴那一根的键（铁律四）。
	miniCurtainTopAxisKey = "mini:curtain:axis:top"

	// miniCurtainAxisKey 是下轴那一根的键（铁律四）。
	miniCurtainAxisKey = "mini:curtain:axis:bottom"
)

// miniCurtainAxisGradient 报轴那一道的渐变：上浅下深，像一根圆柱。
//
// base 是**面板自己的底色**（`t.Surface` 走过 `coverWash` 那一层），t.Text
// 是文字色。
//
// strong 是「这一道有多重」：上轴只有 6 高，贴着顶边，再重就成了一道黑边
// （见那个常量的注释）；下轴 14 高，是真的「一根杆」，重一点才看得出是杆。
//
// ⚠️ **为什么拿底色去混，而不是拿一个固定的深色**：深色主题下底色本身是
// 深的，混出来的轴是**亮的**——那一道轴跟着主题翻面，而不是在深色下变成
// 一根黑杆（黑杆在深色底上就看不见了）。
//
// ⚠️ **三档而不是两档**：中间那一档是那道**高光**的位置（圆柱受光的那一
// 面在上半截），两档的渐变看起来是「一块渐变的板」，三档才像一根杆。
func miniCurtainAxisGradient(t *ui.Theme, base ui.Color, strong float32) ui.LinearGradient {
	return ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
		{Color: base.Mix(t.Text, 0.22*strong), At: 0},
		{Color: base.Mix(t.Text, 0.38*strong), At: 0.38},
		{Color: base.Mix(t.Text, 0.54*strong), At: 1},
	}}
}

// miniCurtainAxisStrongTop / miniCurtainAxisStrongBottom 是两道轴各自的
// 「重多少」。
const (
	// miniCurtainAxisStrongTop 是上轴的重度（0~1）。
	//
	// ⚠️ **0.55**：上轴只有 6 高，贴着顶边——重一点它就是一道**黑边**，
	// 而迷你窗是浮在桌面上的卡片，一道黑边看着像画错了。
	miniCurtainAxisStrongTop = 0.55

	// miniCurtainAxisStrongBottom 是下轴的重度（0~1）。
	//
	// ⚠️ **1**：下轴是 14 高的一根**杆**，它得看得出是杆——而它压在纸上，
	// 是整扇窗上唯一一处「有厚度」的东西。
	miniCurtainAxisStrongBottom = 1
)

// miniCurtainAxisHighlight 报轴上沿那一道**高光**的颜色。
//
// ⚠️ **它比轴的上沿更淡**（往底色那边退一步）：轴的上沿是 30% 的文字色，
// 高光是 10%——于是那一根的最上面有一道**比别处亮**的细线（圆柱受光的那
// 一面）。
func miniCurtainAxisHighlight(t *ui.Theme, base ui.Color, strong float32) ui.Color {
	return base.Mix(t.Text, 0.05*strong)
}

// viewMiniCurtainAxisTop 画**上轴**（天杆）：迷你窗顶边那一道。
//
// ⚠️ **它一直都在**（收着的时候也在）：票面写的是「上轴 = 迷你窗顶边那一
// 道」，而参考页那个卷起来的状态**也是只露天杆**（地轴在 `pc <= 0.02` 时
// `visibility: hidden`）。
//
// ⚠️ **它不挡点击**：它没有 `Role` / `Clicked` 那一套，框架的命中链只收
// **interactive** 的元素——指针落在它上面，命中链往里找的是迷你卡片那一行
// 的 `DragWindow`（拖动区）。
func (s *Shell) viewMiniCurtainAxisTop(c *ui.Context, t *ui.Theme, base ui.Color) {
	ui.Box(c).Key(miniCurtainTopAxisKey).
		Absolute().Top(0).Left(0).Right(0).
		Height(miniCurtainTopAxisH).
		// ⚠️ **胶囊形**（半径 = 高的一半）：轴的两头是**圆头**，不是方角。
		Radius(miniCurtainTopAxisH / 2).
		LinearGradient(miniCurtainAxisGradient(t, base, miniCurtainAxisStrongTop)).
		DrawOver(func(p *ui.Painter, r ui.Rect) {
			// ⚠️ **高光那一道要避开两头那半圆**（各让 3 像素）：不然它会盖到
			// 圆头上，圆头那一圈就变成一个「缺口」。
			pad := float32(miniCurtainTopAxisH) / 2
			p.Fill(ui.Rect{
				X: r.X + pad, Y: r.Y + 1,
				W: r.W - 2*pad, H: 1,
			}, miniCurtainAxisHighlight(t, base, miniCurtainAxisStrongTop), 0.5)
		})
}

// viewMiniCurtainAxisBottom 画**下轴**（地轴）：贴着卷帘下沿那一道。
//
// ⚠️ **它是根的绝对定位孩子**（理由见文件头那一段）：根的高度就是窗的高度，
// 而窗高每帧跟着弹簧走——所以**轴永远贴在窗的下沿**，下沿到哪轴到哪，
// 回弹的时候它跟着一起抖。
//
// ⚠️ **它盖在纸身上面**（绝对定位的孩子最后画）：参考页那个地轴就是压在
// 纸的下边缘上的（`z-index: 1`，而纸身在它下面）。
func (s *Shell) viewMiniCurtainAxisBottom(c *ui.Context, t *ui.Theme, base ui.Color) {
	el := ui.Box(c).Key(miniCurtainAxisKey).
		Absolute().Bottom(0).Left(0).Right(0).
		Height(miniCurtainAxisH).
		// ⚠️ **胶囊形**（半径 = 高的一半 = 7）：轴的两头是**圆头**。
		//
		// ⚠️ **而根那面板展开时的下圆角也是 7**（`miniplayer.go` 里那一段）
		// ——两个圆弧正好对上，于是**窗的下沿就是这根杆的下沿**。
		Radius(miniCurtainAxisH / 2).
		LinearGradient(miniCurtainAxisGradient(t, base, miniCurtainAxisStrongBottom))
	// ⚠️ **影是杆自己的**（`shadow.CurtainAxis`）：它朝上落在纸上——参考页那个
	// 地轴就是 `0 -8px 10px -7px`（往上照），而我们**画不出窗外的影**（窗是
	// frameless 的，出了窗就没了）。
	//
	// ⚠️ **影是元素自己画的**（在它自己的底色**之前**画），所以它落在杆的
	// 上边——**不用**再单独建一个元素。
	el = shadow.CurtainAxis(el)
	el.DrawOver(func(p *ui.Painter, r ui.Rect) {
		pad := float32(miniCurtainAxisH) / 2
		p.Fill(ui.Rect{
			X: r.X + pad, Y: r.Y + 1,
			W: r.W - 2*pad, H: 1,
		}, miniCurtainAxisHighlight(t, base, miniCurtainAxisStrongBottom), 0.5)
	})
}
