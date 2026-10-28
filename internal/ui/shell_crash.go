// 这一块是**崩溃兜底与错误页**：view 里 panic 了之后，程序怎么活着，以及活着的
// 那一帧画什么。
//
//   - `Guard` —— 页面级的 panic recover（实现本身在 `internal/ui/guard`，控件层
//     也要给自己的 view 包一层，放这里的话它就得反过来 import 界面层，那是个环）。
//   - `crashInfo` —— 一次崩溃的**完整**现场（崩在界面的哪一块 + 当时在看哪一页 +
//     panic 到的东西）。
//   - `viewCrash` —— 崩溃后那一帧画的那张错误页。
//   - `errorArt` / `errorArtHeight` —— 错误页中间那张插画和它的高度。
//
// ⚠️ `artFile`（插画那一小份 embed）也在这里：它只被错误页用着，跟着走。
package ui

import (
	"embed"

	"github.com/ShellMonster/PudMusic/internal/ui/guard"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// artFile 是插画那一小份 embed（错误页那张 `error.svg`）。
//
// ⚠️ **它和图标那一份分开**：图标全是单色的遮罩、走 `ui.Icon`，插画有自己
// 的灰度层次、走 `ui.Image`。合成一份 embed 的话，`iconset` 就得替插画也
// 实现一套「解析成 *ui.SVG 再交给调用方」的东西，而它对图标那套是对的、对
// 插画是多余的。
//
//go:embed art
var artFile embed.FS

// Guard 给一个 view 包一层页面级的 panic recover。
//
// 这是整个界面层最重要的一条约定：MyGo 的 Dispatch（app.go:576）和 drain
// 都没有 recover，view 里 panic 会穿透 Cocoa 事件循环**直接杀死进程**，连
// 「这一页坏了」的机会都没有。Biu 原来靠 React 的 ErrorBoundary 兜住，
// 这里用 recover 兜住同一个位置的效果。
//
// onPanic 拿到崩掉的东西，界面用它换一张错误页。崩了以后这一帧已经建了一半，
// MyGo 的 build pass 会在下一次 pass 从头重建，所以 onPanic 里只要记下状态
// 并要一帧，画面下一帧就是完整的。
//
// 实现本身在 `internal/ui/guard`：控件层（widgets/list）也要给自己的 view 包
// 一层，放这里的话它就得反过来 import 界面层，那是个环。
//
// ⚠️ **界面上的每一处都该走 `guardAt` 而不是它**，因为只有前者记得「崩在界面的
// 哪一块」，而那是错误页上「出错在哪一步」那一句的全部来源（见 `crashInfo`）。
// 这一个留给**拿不到 Shell 的调用方**——控件层，以及测试里单独验 Guard 本身。
func Guard(draw func(*ui.Context), onPanic func(any)) func(*ui.Context) {
	return guard.Wrap(draw, onPanic)
}

// crashInfo 是一次崩溃的**完整**现场。
//
// # 为什么要三个字段，而不只是崩掉的那个值
//
// 改之前错误页上只有一句 `%v`（panic 的值）。而 panic 的值大多长这样：
// `runtime error: index out of range [5] with length 3`——这句话说明了**是什么**，
// 一点也没说**在哪儿**。用户能做的只有把这句发给开发者，而开发者拿着它既复现
// 不出来，也定位不到（同一个越界能发生在七个不同的地方）。
//
// 两条「在哪儿」各有各的来源，缺一条就退化成原来那句：
//
//   - where 是**界面的哪一块**：由包住那一块的 `guardAt` 写上去。崩在侧栏和崩在
//     队列抽屉，用户看到的是完全不同的两种现象（一个是一大片空白，一个是抽屉
//     关不上）。
//   - path/title 是**当时在看哪一页**：从 `s.router` 上读，不用谁记得报上来。
type crashInfo struct {
	// where 是崩在界面的哪一块，由 `guardAt` 的调用点给。
	where string
	// path 是崩的时候正在看的那条路径（`s.router.Location()`）。
	path string
	// title 是那一页的名字（`routes` 表里的 `Title`）。
	title string
	// got 是 panic 到的东西。
	got any
}

// viewCrash 是崩溃后那一帧。Biu 原来的 ErrorBoundary 也是这么做的：程序活着，
// 出事的那块换一张说明页。
//
// # 这一页要说的三件事，少一件都不算「能看的错误页」
//
//  1. **出在哪儿**：哪一块（侧栏 / 队列抽屉 / 页面「下载记录」）和当时在看哪一页。
//     这一句是用户拿去跟开发者说的第一句，也是这一票要的（issue 126：「出错时
//     能看到出错在哪一步」）。它下面那一行原始错误是第二句。
//  2. **插画**：上游那一页中间摆一张 480 宽的 `error.svg`
//     （`components/error-fallback/index.tsx:11-27`），而我们原来只有两行字。
//     「一片白」和「一张说明页」在用户那边差得远——他说不清出事了，而这里
//     一眼就看得出来。
//  3. **走得出去**：重新载入（回同一页）、回到首页、以及**反馈**——上游那枚
//     描边按钮把用户带到 GitHub issues。没有反馈的话用户只能重启然后在心里
//     骂一句。
//
// ⚠️ **不装主题的深色下会亮成一张白纸**（和错误页同一段：它是崩溃后那一帧，
// 而 `View` 在崩着的时候不会先走 `viewMain`，那个开头装主题的一步没跑过）。
func (s *Shell) viewCrash(c *ui.Context) {
	s.applyFrameTheme(c)

	t := c.Theme()
	cr := s.crash
	ui.Column(c).Fill().Center().Gap(space.Px10).Padding(space.Px24).Children(func() {
		// 上游那张 480 宽的插画。它是 `currentColor` 的（见 art/error.svg
		// 顶上那一段），所以深浅两套主题里它各自是「在这套底色上看得清」的
		// 那个色——不给它定死颜色是既定行为，和图标那一批同一条理由。
		if v := errorArt; v != nil {
			ui.Image(c, v).Height(errorArtHeight)
		}
		// ⚠️ **字重不写**（400）：标题那一档是 400（DESIGN.md §二）。
		ui.Text(c, "界面出错了，播放器已经停在这一页").FontSize(scale.Font(c, scale.Size18))
		// 「出错在哪一步」这一块：三行，各自说清一件事。
		ui.Column(c).Key("crash-where").Gap(space.Px2).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "出错的地方："+cr.where).FontSize(scale.Font(c, scale.Size13)).TextColor(t.Text)
			// 路径和页面名分开两行：用户在地址里看到的是路径，而跟开发者
			// 说的时候说的是页面名（「下载记录那一页」）。只给一个的话，另一
			// 边的人听不懂。
			// ⚠️ **路径空着的时候整句不画**：路由器上还没有路径（测试里
			// 手摆的崩溃现场就是这样），而「当时在看的路径：」这一句后面
			// 空着，比不说更让人以为程序自己也不知道自己在哪儿。
			switch {
			case cr.path == "":
			case cr.title == "" || cr.title == notFound.Title:
				// 404 那一页没有「页面名」可言（`notFound` 的 Title 是兜底
				// 的），只说路径。
				ui.Text(c, "当时在看的路径："+cr.path).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			default:
				ui.Text(c, "当时在看的页面："+cr.title+"（"+cr.path+"）").
					FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			}
		})

		ui.Textf(c, "%v", cr.got).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(3)
		ui.Text(c, "细节已经写进日志。点下面的按钮再试一次。").FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
		ui.Row(c).Gap(space.Px10).Justify(ui.Center).Children(func() {
			// 反馈是**描边**的那一枚、主操作是实心的那一枚：抄上游的
			// `components/error-fallback/index.tsx:11-27`（「反馈」描边去
			// GitHub issues、「回到首页」主色）。
			if ui.Button(c, "反馈").Clicked() {
				s.openFeedback(c)
			}
			if ui.PrimaryButton(c, "回到首页").Clicked() {
				s.navigate("/")
			}
			// 崩的是某一页，不一定是首页。得给一条走得出去的路，
			// 不然错误页里连侧栏都点不到，只能干看着。
			if ui.Button(c, "重新载入这一页").Clicked() {
				s.crash = nil
			}
		})
	})
}

// errorArt 是错误页中间那张插画。
//
// 它和图标那一批**不是一回事**：图标走 `ui.Icon`（画成当前文字色的遮罩），
// 而这张是一张有自己灰度层次的插画，所以走 `ui.Image` 画它自己的颜色。
// 它照样**不写死颜色**——`art/error.svg` 用的是 `currentColor` 加几档透明度，
// 于是它跟着主题走（`drawSVG` 的 current 参数就是祖先的文字色）。
var errorArt = func() *ui.SVG {
	b, err := artFile.ReadFile("art/error.svg")
	if err != nil {
		// 编译进去的文件读不到 = 有人在重构里挪了目录，而那要在第一次启动
		// 就炸出来，而不是画出一张空白的错误页。
		panic("ui: 错误页那张插画读不到（art/error.svg）：" + err.Error())
	}
	return ui.MustParseSVG(b)
}()

// errorArtHeight 是那张插画在错误页上的高度。
//
// ⚠️ **按高度不按宽度**：上游那张是 480 宽，而窗口宽度是变的（900 ~ 1440），
// 按宽度摆的话窄窗口上那张插画会把三行字挤到屏幕外面去。按高度摆它在任何
// 窗口宽度上都占同样的一行，而它的宽高比（480:220）自己会把宽度收在 187 左右
// ——那正是「一张插画」该有的尺寸。
const errorArtHeight = 96
