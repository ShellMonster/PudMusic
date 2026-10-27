// 票 #297 的判据：设置页两处分段控件**每一档都有 18px 图标**、「数据显示样式」
// 那一行**只剩一组选中信号**、那两处**走同一份实现**。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket297Shots
//
// 三条都对着**画面**断言（第三条对着**源码**——理由写在那一条的注释里）。
//
// ⚠️ **深浅两套都要过**：主色药丸上的字是 `AccentText`（浅色下是深色字压在
// 亮绿底上），而没选中的那一档图标是正文色——两套主题下图标所在的底色完全
// 不同，只验浅色会漏掉「图标在深色下糊进底色」这一种。
package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 一、每一档都有一枚 18px 图标
// ─────────────────────────────────────────────────────────────────────────────

// segmentOptionInk 量「标签左边那一段」里的图形（那一枚图标）。
//
// ⚠️ **取样的是标签左边那一段**：上游那两排是「图标 + 文字」，图标在文字左边
// （`system-settings.tsx:74-97` / `:124-146`），而那一段里除了图标只有按钮
// 自己的底色——所以量到的墨迹就是那枚图标。
//
// ⚠️ **取样的框是「图标那一格」**：横向是标签左边 24 到 6 像素（**图标那一格
// 正好 18 宽**），竖向是标签那一行上下各多 3 像素。取宽了会量到按钮的圆角
// （圆角那一段是按钮底色和页面底色揉出来的中间色，在实心主色底那一档会被
// 当成墨迹）。
//
// 返回的是：墨迹的包围盒、墨迹的**灰阶指纹**、墨迹的像素数。
func segmentOptionInk(tt *mygoui.Tester, label string) (mygoui.Rect, string, int) {
	text, ok := tt.Find(label)
	if !ok {
		return mygoui.Rect{}, "", 0
	}
	img := tt.Image()
	region := mygoui.Rect{
		X: text.X - 24,
		Y: text.Y - 3,
		W: 18,
		H: text.H + 6,
	}
	px := bandPixels(img, region)
	if len(px) == 0 {
		return mygoui.Rect{}, "", 0
	}
	bg := modeOf(px)

	var ink strings.Builder
	// ⚠️ **起点用取整后的那个**：取像素是从 `int(region.X)` 开始的，而包围盒要
	// 跟取像素用的是同一个起点——差那不到一个像素，量出来的缝就差一个像素。
	originX, originY := int(region.X), int(region.Y)
	minX, minY, maxX, maxY := int(region.W), int(region.H), -1, -1
	n := 0
	for dy := range int(region.H) {
		for dx := range int(region.W) {
			x := originX + dx
			y := originY + dy
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			p := img.RGBAAt(x, y)
			if !distinctPixel(p, bg) {
				continue
			}
			n++
			ink.WriteString(strconv.Itoa(glyphLuma(p) >> 4))
			ink.WriteByte('\n')
			minX, minY = min(minX, dx), min(minY, dy)
			maxX, maxY = max(maxX, dx), max(maxY, dy)
		}
	}
	if maxX < 0 {
		return mygoui.Rect{}, "", 0
	}
	return mygoui.Rect{
		X: float32(originX + minX),
		Y: float32(originY + minY),
		W: float32(maxX - minX + 1),
		H: float32(maxY - minY + 1),
	}, ink.String(), n
}

// segmentRows 是设置页上那两排**带图标的**分段控件。
//
// ⚠️ **「窗口关闭」那一排不在这里**：那两档没有配得上语义的图标（上游那两档
// 本来也没有图标），见 `pickCloseWindow` 的注释。
var segmentRows = []struct {
	group   string // 那一排的组名（`settingsSegment` 给的 `Label`）
	what    string // 说给人听
	options []string
}{
	{"显示样式档位", "「数据显示样式」", []string{"列表", "网格", "紧凑"}},
	{"主题模式", "「主题」", []string{"跟随系统", "浅色", "深色"}},
}

// TestEverySettingsSegmentOptionHasAnIcon 钉住「设置页两处分段控件每一档都有
// 18px 图标」。
//
// # 为什么图标要紧
//
// 上游那两排都是「图标 + 文字」（`system-settings.tsx:65-101` / `:105-153`），
// 而我们此前是**裸文字**。图标不是装饰：那一排是「三档里挑一档」，而
// 「列表 / 网格 / 紧凑」这三个字说的是**名字**，图标说的是**样子**——用户切档
// 的时候看的是画面变了没有。
//
// # 判据是**画面上那一段有图形**
//
// 量的是标签左边那一段（图标就在那儿）：没有墨迹就是没画图标，而「没画图标」
// 在画面上和「画了一枚看不见的图标」是一回事。
//
// ⚠️ **深浅两套都要过**：选中那一档的图标是 `AccentText`（压在实心主色底上），
// 没选中的那一档是正文色（压在页面底色上）——两套主题下底色完全不同。
func TestEverySettingsSegmentOptionHasAnIcon(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""), systemIsDark(dark))
			homeFrame(tt)

			for _, row := range segmentRows {
				// ⚠️ **先滚进可视区再量**：那一排在页面中间，不滚的话量到的是
				// 旧一帧的盒子（评审那一版就量错过，见 `settingRect`）。
				settingRect(t, tt, s, row.group)

				fingerprints := map[string]string{}
				for _, label := range row.options {
					box, fp, n := segmentOptionInk(tt, label)
					if n == 0 {
						t.Errorf("%s那一档 %q 的左边没有图形——图标没画出来",
							row.what, label)
						continue
					}
					// ⚠️ **图形装得进一个 18px 的方框**：那是上游那个尺寸
					//（`size={18}`）。装不下就是画大了。
					if box.W > 19 || box.H > 19 {
						t.Errorf("%s那一档 %q 的图形是 %.0f×%.0f，装不进 18×18——"+
							"图标比上游那一枚大", row.what, label, box.W, box.H)
					}
					// 三档各配一枚：配错了用户切档时看到的是同一张图。
					if prev, dup := fingerprints[fp]; dup {
						t.Errorf("%s那一档 %q 和 %q 画出来一模一样——"+
							"三档各配一枚，配错了用户切档时看不出换了档",
							row.what, label, prev)
					}
					fingerprints[fp] = label
				}
			}
		})
	}
}

// TestTheSettingsSegmentIconsSitRightNextToTheirLabel 钉住「图标就画在标签
// 左边，而且两排画在同一个位置」。
//
// # 为什么这一条不是「画个图标就完了」
//
// 「两处走同一份实现」（票面第四条）在画面上长什么样：**两排的图标和标签之间
// 的缝一样、图标离标签一样远**。差一两个像素用户看不出来，可差开了之后
// 「同一排里的三档」自己就对不齐了——而那一排是对齐着看的（三个格子并排）。
//
// ⚠️ **判据取的是墨迹的右沿**：图形本身离标签多远，取决于那一枚图形的右边
// 留白多少（太阳的 rays 几乎顶到边，显示器的那块屏留了一像素），所以这里断
// 的是**一个区间**而不是一个数。
func TestTheSettingsSegmentIconsSitRightNextToTheirLabel(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	homeFrame(tt)

	for _, row := range segmentRows {
		settingRect(t, tt, s, row.group)
		for _, label := range row.options {
			text, ok := tt.Find(label)
			if !ok {
				t.Fatalf("设置页上没有 %q，文字有 %q", label, tt.Texts())
			}
			box, _, n := segmentOptionInk(tt, label)
			if n == 0 {
				continue // 上一条已经报过了
			}
			gap := text.X - (box.X + box.W)
			if gap < 5 || gap > 10 {
				t.Errorf("%s那一档 %q 的图标离标签 %.1f 像素（want 5~10）——"+
					"图标和字之间那道缝在两排之间对不齐",
					row.what, label, gap)
			}
		}
	}
}

// TestTheSettingsSegmentLabelsAreNeverTruncated 钉住「分段控件的标签不会被裁」。
//
// # 哪一档最容易裁
//
// 「跟随系统」是那一排最宽的一档：18(图标) + 6(缝) + 四个字 + 20(内边距)。
// 在「窗口 900 + 侧栏拉到 480 + 字号 1.4 倍」那一档，控件列只有 **318.7**
// 宽——**三等分**的话每一格只有 (318.7 − 8) / 3 = **103.6**，而那一档要
// **116.2**，差 12.6 像素，于是那四个字被裁成「跟随…」。
//
// （`Grow(1)` 在框架里是**三等分**：`mygo/ui/layout.go` 的 `Grow` 把基准宽设成
// 0，于是每一格分到的是整排的等份，不是「内容 + 一份余量」。）
//
// # 判据是**同一个标签在两档配置下一样宽**
//
// 被裁的标签**宽度会变小**（裁成「跟随…」），而没裁的宽度就是那几个字本身
// 的长度——所以拿同一个标签在「放得下」的那一档比一比就知道它被裁没有。
//
// ⚠️ **两档的字号必须一样**（1.4 倍）：字跟着字号缩放，宽度自然不一样。
//
// ⚠️ **比的是宽度，不是「有没有省略号」**：省略号那三个点会落在字的后头，
// 像素上认得出来，可它认的是「这一段的墨迹」——而字的位置差半个像素，
// 那几个字本身的墨迹就会时而连成一段、时而断开（实测同一段字有时是 3 段、
// 有时是 4 段）。宽度是稳的。
func TestTheSettingsSegmentLabelsAreNeverTruncated(t *testing.T) {
	// 三排分段控件，每一档的标签。
	rows := []struct {
		group  string
		labels []string
	}{
		{"显示样式档位", []string{"列表", "网格", "紧凑"}},
		{"主题模式", []string{"跟随系统", "浅色", "深色"}},
		{"窗口关闭方式", []string{"隐藏到托盘", "直接退出"}},
	}

	// 放得下的一档：同样的 1.4 倍字号，侧栏没拉到最大（控件列 420 宽）。
	wideTT, wideS := settingsScene(t, app.DefaultSideMenuWidth, 1.4)
	// 放不下的一档：侧栏拉到 480（控件列 318.7 宽），字号同样是 1.4 倍。
	narrowTT, narrowS := settingsScene(t, app.MaxSideMenuWidth, 1.4)

	for _, row := range rows {
		for _, label := range row.labels {
			wide := settingsLabelWidth(t, wideTT, wideS, row.group, label)
			narrow := settingsLabelWidth(t, narrowTT, narrowS, row.group, label)
			if absF(narrow-wide) > 1 {
				t.Errorf("「%s」那一档的标签在最坏配置下是 %.1f 宽，而放得下的时候是 %.1f——"+
					"它被裁了（控件列只有 318.7 宽，三等分装不下「图标 + 四个字」）",
					label, narrow, wide)
			}
		}
	}
}

// settingsScene 造一个停在设置页上的外壳（某一档侧栏宽 + 某一档字号）。
func settingsScene(t *testing.T, side float64, scale float32) (*mygoui.Tester, *Shell) {
	t.Helper()
	tt, s, _, _ := shellOnSettings(t, sideSceneSettings(side), at(900, 800),
		withPrefs(mygoui.Preferences{TextScale: scale}))
	// 侧栏宽度有 200ms 的过渡（`saveNarrowSettingsShot` 的注释写着这一条）。
	for range 4 {
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
	}
	homeFrame(tt)
	return tt, s
}

// settingsLabelWidth 量设置页上某一档标签的宽（那一排先滚进可视区）。
//
// ⚠️ **自己滚，不复用 `settingRect`**：那一只往下滚 24 步（1440 像素）就
// 收手，而「1.4 倍字号 + 侧栏 480」那一档的页面**长得离谱**（说明列被挤到
// 一个字宽，每一行的标题都折成好几行）——「窗口关闭」那一排在它下面很远
// 的地方，滚 24 步到不了。
//
// ⚠️ **量宽度不需要它可见，但需要它被排过版**：滚动只改 y、不改宽，可滚过去
// 之后这一格就不在可视区了，而框架给滚出可视区的元素**不排版**（量出来是
// 0 宽）。所以每一档都要先滚到**它自己那一排**再量。
func settingsLabelWidth(t *testing.T, tt *mygoui.Tester, s *Shell, group, label string) float32 {
	t.Helper()
	for range 120 {
		if r, ok := tt.Find(label); ok && r.H > 0 && r.Y >= pageTop && r.Y+r.H <= pageBottom {
			return r.W
		}
		before := s.settings.scroll.Y
		s.settings.scroll.Y = before + 100
		tt.Frame()
		if s.settings.scroll.Y == before {
			break // 已经到底了，再滚也不动
		}
	}
	t.Fatalf("设置页上滚不到 %q 那一排里的 %q，文字有 %q", group, label, tt.Texts())
	return 0
}

// ─────────────────────────────────────────────────────────────────────────────
// 二、「数据显示样式」那一行只剩一组选中信号
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDisplayModeCellHasOnlyOneSelectedSignal 钉住「那一格里只有一个选中
// 信号」。
//
// # 「两种选中信号」是什么
//
// 那一格是「药丸（切档）+ 三张预览缩略图（示意）」。此前选中的那张预览**描
// 一圈主色边**，于是那一格里有两个绿：药丸说「现在是列表」，预览的绿框又说
// 一遍。用户会以为预览也是一个能点的开关（而它不是——预览不可点，见
// `displaymode.go` 的注释）。
//
// # 判据是**预览那一圈的描边**
//
// 三张预览的描边都必须是**中性色**（`t.Border`），没有一张是主色。
//
// ⚠️ **「预览留着」也要断**：三张缩略图都还在（那是我们的增强，上游没有），
// 而它们一个字都没有——所以认它们只能靠 `Label`。
func TestTheDisplayModeCellHasOnlyOneSelectedSignal(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""), systemIsDark(dark))
			homeFrame(tt)
			theme := BuildTheme(app.DefaultSettings(""), dark)

			// 那一格里的选中信号**只有药丸**——它是实心主色底。
			pill := settingRect(t, tt, s, "列表")
			got := modeOf(bandPixels(tt.Image(), buttonPadSpot(pill)))
			if !nearRGBA(got, rgbaOf(theme.Accent), 3) {
				t.Errorf("「列表」那一档的底色是 %v，want %v（实心主色底）——"+
					"那一格的选中信号是药丸", got, rgbaOf(theme.Accent))
			}

			// 三张预览都还在，而且描边都是中性色。
			for _, label := range []string{"预览：列表", "预览：网格", "预览：紧凑"} {
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("设置页上没有 %q 那张预览——预览是我们自己的增强，"+
						"不能被删掉（文字有 %q）", label, tt.Texts())
				}
				// ⚠️ `t.Border` 是半透明色，画面上量到的是它叠在卡片底色上的结果；
				// 底色取卡片左边外侧（卡片不另填底色，里面是示意线条，不能取）。
				base := tt.Image().RGBAAt(int(r.X)-4, int(r.Y+r.H/2))
				wantEdge := rgbaOf(theme.Border.Over(rgbaToColor(base)))
				edge := previewEdgePixel(tt.Image(), r, wantEdge)
				if nearRGBA(edge, rgbaOf(theme.Accent), 8) {
					t.Errorf("%s 那张预览描的是主色边（%v）——那一格里就有**两种**"+
						"选中信号了（药丸 + 预览的绿框），票 #297 裁掉的就是这个",
						label, edge)
				}
				if !nearRGBA(edge, wantEdge, 8) {
					t.Errorf("%s 那张预览的描边是 %v，want %v（中性色叠在底色上）",
						label, edge, wantEdge)
				}
			}
		})
	}
}

// previewEdgePixel 量一张预览缩略图**左边那一列描边**上的像素。
//
// ⚠️ **取左边那一列，不取上边**：那一列在圆角的直边上，是描边本身；上边
// 那一行落在**圆角**上（预览是圆角的），量到的是描边和底色揉出来的那几个
// 中间色。
//
// ⚠️ **卡片的 X 是小数**：描边落在哪一列取决于抗锯齿，所以在 X 左右各一列里取
// 最接近 want 的那个。
func previewEdgePixel(img *image.RGBA, r mygoui.Rect, want color.RGBA) color.RGBA {
	y := int(r.Y + r.H/2)
	best, bestD := color.RGBA{}, 1<<30
	for x := int(r.X) - 1; x <= int(r.X)+1; x++ {
		if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
			continue
		}
		if p := img.RGBAAt(x, y); maxDiff(p, want) < bestD {
			best, bestD = p, maxDiff(p, want)
		}
	}
	return best
}

// ─────────────────────────────────────────────────────────────────────────────
// 三、两处走同一份实现
// ─────────────────────────────────────────────────────────────────────────────

// TestBothSettingsSegmentsGoThroughOneDrawer 钉住「设置页那两处分段控件走
// 同一处实现」。
//
// # 为什么这一条断的是**源码**而不是画面
//
// 画面上两排长得一样，有两种可能：它们走的是同一段代码，或者碰巧画出来
// 一样。而「碰巧」那一种最坏——下一个人改了一处（比如给图标换个大小），
// 另一处不动，两排就分叉成两种样子，而那种分叉在界面上只是「这一枚比旁边
// 那枚淡一点」，**没人当它坏了**。
//
// # 判据是**调用关系**
//
//   - 画「图标 + 文字」那一档的函数（`segmentButton`）**只被一个地方调用**
//     （`settingsSegment`）——图标那一档只有一份画法；
//   - 设置页那三处分段控件**都走 `settingsSegment`** 这一个出口。
//
// ⚠️ **三处而不是两处**：「窗口关闭」那一排也在同一个出口上（票 #240 /
// 报告 F-17），它只是没有图标而已。
func TestBothSettingsSegmentsGoThroughOneDrawer(t *testing.T) {
	calls := segmentDrawerCallers(t)

	drawers := calls["segmentButton"]
	if len(drawers) != 1 || !drawers["settingsSegment"] {
		t.Errorf("画「图标 + 文字」那一档的 segmentButton 被 %v 调用着，"+
			"want 只有 settingsSegment 一处——图标那一档只能有一份画法",
			sortedKeys(drawers))
	}
	users := calls["settingsSegment"]
	for _, want := range []string{"pickTheme", "pickDisplayMode", "pickCloseWindow"} {
		if !users[want] {
			t.Errorf("settingsSegment 没有被 %s 调用——设置页的分段控件"+
				"必须走同一个出口（它被 %v 调用着）", want, sortedKeys(users))
		}
	}
}

// segmentDrawerCallers 扫这个包里的非测试文件，找出 `segmentButton` 和
// `settingsSegment` 分别被哪些函数调用。
func segmentDrawerCallers(t *testing.T) map[string]map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]bool{
		"segmentButton":   {},
		"settingsSegment": {},
	}
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败，这条就没法查了：%v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				// ⚠️ **两种写法都要认**：`settingsSegment` 是方法（`s.settingsSegment`），
				// 而 `segmentButton` 是普通函数（`segmentButton(...)`）——只认
				// `*ast.Ident` 的话，方法那一种整个漏掉，这条测试就恒绿。
				var called string
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					called = fun.Name
				case *ast.SelectorExpr:
					called = fun.Sel.Name
				}
				if _, watched := out[called]; watched {
					out[called][fn.Name.Name] = true
				}
				return true
			})
		}
	}
	return out
}

// sortedKeys 把一组名字排好序（报错时看着清楚）。
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 四、出图
// ─────────────────────────────────────────────────────────────────────────────

// TestRenderTicket297Shots 把这一票改过的设置页各存一张 PNG。
//
// ⚠️ **深浅两套**：这一票动的是「图标压在什么底色上」——浅色下药丸上的图标
// 是深色字压亮绿，深色下同一枚图标是别的对比度，只验浅色会漏掉一半。
//
// ⚠️ **最坏那两档也要出**：加了图标之后「跟随系统」那一档宽了 24 像素，而
// 控件列在「窗口 900 + 侧栏 480 + 字号 1.4 倍」那一档只有 320 宽——那一档
// 放不下的话，控件会被推出内容区（`settingsControlFitW` 的注释写着这一条）。
func TestRenderTicket297Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			for _, winW := range []int{900, 1200} {
				b := newTestShell(t, at(winW, 800), systemIsDark(dark),
					withSettings(app.DefaultSettings("")))
				tt := b.tt
				openSettings(t, tt)
				homeFrame(tt)

				save(t, dir, fmt.Sprintf("95-设置-整窗-%d-%s.png", winW, name), tt)
				head, ok := tt.Find("外观")
				if !ok {
					t.Fatalf("设置页上没有「外观」那个分组标题，文字有 %q", tt.Texts())
				}
				x0 := int(head.X) - 40
				saveRect(t, dir, fmt.Sprintf("96-设置-外观段-%d-%s.png", winW, name), tt,
					x0, int(head.Y)-12, winW-x0-8, 300)
				b.Close()
			}
			// 最坏的那几档。
			//
			// ⚠️ **「侧栏 480 + 字号 1.4 倍」那一档是真正最坏的那一档**：控件列
			// 只有 318.7 宽，而「跟随系统」那一档要 116.2（18 图标 + 6 缝 +
			// 72.2 字 + 20 内边距）——**三等分**的话每一格只有 103.6，那个字
			// 会被裁成「跟随…」。前两档各占一头（侧栏最宽、字号最大），合起来
			// 才是最坏的那一档，出图必须出它。
			for _, sc := range []struct {
				name  string
				side  float64
				scale float32
			}{
				{"侧栏480", app.MaxSideMenuWidth, 1},
				{"字号1.4倍", app.DefaultSideMenuWidth, 1.4},
				{"侧栏480+字号1.4倍", app.MaxSideMenuWidth, 1.4},
			} {
				saveNarrowSettingsShot297(t, dir, sc.name, sc.side, sc.scale, name)
			}
		})
	}
}

// saveNarrowSettingsShot297 出一张**最坏配置**的设置页。
//
// ⚠️ **侧栏宽度有 200ms 的过渡**：不推钟的话图上是动画中间那一档。
//
// ⚠️ **「侧栏 480 + 字号 1.4 倍」那一档还要多出一张**：那一档的说明列被挤到
// 一个字宽，每一行的标题都折成好几行，于是「主题」那一排在**第一屏下面**
// ——整窗那张图上看不见那一排（而那一排正是这一票的判据所在）。
func saveNarrowSettingsShot297(t *testing.T, dir, tag string, side float64, scale float32, name string) {
	t.Helper()
	st := app.DefaultSettings("")
	st.SideMenuWidth = side
	b := newTestShell(t, at(900, 800), systemIsDark(name == "深色"),
		withSettings(st),
		withPrefs(mygoui.Preferences{TextScale: scale}))
	tt := b.tt
	for range 4 {
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
	}
	openSettings(t, tt)
	homeFrame(tt)

	save(t, dir, "97-设置-最坏-"+tag+"-"+name+".png", tt)

	// 再出一张滚到「主题」那一排的：那一排才是这一票的判据所在。
	scrollSettingsTo(t, tt, b.s, "主题模式")
	save(t, dir, "98-设置-最坏主题段-"+tag+"-"+name+".png", tt)
	b.Close()
}

// scrollSettingsTo 把设置页滚到某一排可见为止（组名在可视区里）。
func scrollSettingsTo(t *testing.T, tt *mygoui.Tester, s *Shell, group string) {
	t.Helper()
	for range 120 {
		if r, ok := tt.Find(group); ok && r.H > 0 && r.Y >= pageTop && r.Y+r.H <= pageBottom {
			return
		}
		before := s.settings.scroll.Y
		s.settings.scroll.Y = before + 100
		tt.Frame()
		if s.settings.scroll.Y == before {
			break // 已经到底了，再滚也不动
		}
	}
	t.Fatalf("设置页上滚不到 %q 那一排，文字有 %q", group, tt.Texts())
}
