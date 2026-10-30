package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"image/color"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// mentionsString 报 e 里有没有写着一个值为 want 的字符串字面量。
func mentionsString(e ast.Expr, want string) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == strconv.Quote(want)
}

// 播放列表抽屉的验收：行高 64、按判重标识去重、「定位当前播放」平滑滚过去、
// 行菜单那五项。
//
// 窗口是 760×460（winW / winH），抽屉是 320 宽、到播放条上沿为止，所以：
// y < 372 是抽屉那一层（点它关抽屉），y >= 372 是播放条（照常用）。
//
// 「定位当前播放」那一条钉的是**平滑**：钟没走的时候那一行还看不见，走够了才
// 看见。用的是 Tester 那只站着不动的钟（`Advance` 才推进），所以「立刻就滚到位」
// 和「滚了半天还在原地」都断得出来——单看最终位置的话，一跳到位和平滑滚过去
// 长得一模一样。

// onlineSong 是造一个在线条目用。标题唯一，免得几行在画面上分不清。
func onlineSong(bvid, title string) app.Item {
	return app.Item{Source: "online", Type: "mv", Bvid: bvid, Cid: "1", Title: title}
}

// localSong 造一个**真的放得出来**的本地项：歌名就是文件名去掉后缀（所以起名
// 就是选断言里用的那个字）。
//
// 要真能放，是因为有一批用例断的就是「这一项真的跳过去放了」——用在线条目的话，
// 这个壳里没挂取流，`PlayAt` 会打不开然后把下标退回去，那条断言就成了永远绿。
func localSong(t *testing.T, dir, name string) app.Item {
	t.Helper()
	return app.LocalItem(writeCopy(t, dir, name))
}

// queueEmptyHint 是抽屉空着时那一句话（queue_drawer.go 的 queueEmpty.Hint）。
//
// 这里写死一份是故意的：那句话是**给人看的**，改文案就该顺手改这里，而不是
// 让测试跟着代码走、什么文案都判「在」。
const queueEmptyHint = "队列是空的"

// writeCopy 把 FLAC 素材复制成 dir/name 这个文件，返回完整路径。dir 不存在就建。
func writeCopy(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(flac(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// pixelAt 是画面上 (x, y) 那一点的颜色，用来断「正在放的那一行换了底色」。
func pixelAt(img *image.RGBA, x, y float32) color.RGBA {
	px := int(x)
	py := int(y)
	if px < 0 || py < 0 || px >= img.Rect.Dx() || py >= img.Rect.Dy() {
		return color.RGBA{}
	}
	return img.RGBAAt(px, py)
}

// putCurrent 把队列的当前项落在第 n 首，**不放歌**。
//
// 走 `Move` 是因为队列还没有别的公开办法只挪下标：还没开始（cur = -1）时
// `Move` 正向一律落在第 0 首，所以先挪一格再挪到位。
func putCurrent(t *testing.T, a *app.App, n int) {
	t.Helper()
	q := a.Queue()
	q.Move(1)
	q.Move(n)
	if got := q.CurrentIndex(); got != n {
		t.Fatalf("当前项落在第 %d 首，want %d", got, n)
	}
}

// countRows 数抽屉里画着几行。
//
// 数的是**每一行尾那颗操作键的 `Label`**（「「歌名」的操作」，票 #249 之后）。
// 它原来数的是屏幕上那个「⋯」字符——而那一颗后来换成了真图标（上游
// `RiMore2Line`），画面上不再有那个字符。数字形的话换图标之后会数成 0 行，而
// **「0 行」在这几条里全都不会自己报警**（判据都是 `want 2`，于是红的是它们，
// 可症状是「整张列表空了」而不是「那一颗换了画法」）。
//
// ⚠️ **数的是后缀，不是全等**：名字里带的是**歌名**，而歌名是每行不一样的，
// 所以只能按后缀数。真按全等数的话，得先把每一行的歌名都列出来——而歌名恰恰
// 是这一族断言里会改的东西。
func countRows(h *harness) int {
	n := 0
	for _, x := range h.tt.Texts() {
		if strings.HasSuffix(x, "」的操作") {
			n++
		}
	}
	return n
}

// openDrawer 点播放条上的那个键把抽屉拉出来，顺手确认它真的开了。
func openDrawer(t *testing.T, h *harness) {
	t.Helper()
	if err := h.tt.Click("播放列表"); err != nil {
		t.Fatalf("点不了播放列表：%v（画面上的字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()
	if !h.tt.HasText("播放列表") {
		t.Fatalf("点了播放列表，抽屉没开（画面上的字：%q）", h.tt.Texts())
	}
	// ⚠️ **要让抽屉滑到位**：Tester 的钟站着不动，不推它的话面板还在窗口外面
	//（它从右边滑进来）——于是「按名字找抽屉里的东西」量到的是空的右半边。
	settleDrawer(t, h.s, h.tt)
}

func TestQueueDrawerOpensFromThePlaybar(t *testing.T) {
	h := newHarness(t)

	if h.tt.HasText("暂无内容") {
		// 抽屉没开的时候画面上本来就有别的空状态（「先在顶栏「打开文件」放几首
		// 进来」那一句），所以不能只断这个字。
		if h.tt.HasText(queueEmptyHint) {
			t.Fatal("抽屉还没点就开着了")
		}
	}
	openDrawer(t, h)

	// 空队列：两个键不摆（Biu 的 `Boolean(pureList?.length) && …`），摆一句空状态。
	if h.tt.HasText("定位当前播放") {
		t.Error("队列是空的还摆着「定位当前播放」")
	}
	if !h.tt.HasText(queueEmptyHint) {
		t.Errorf("空的抽屉没有那句话，文字有 %q", h.tt.Texts())
	}

	// Esc 关掉。少这一条的表现是「按了 Esc 什么都没发生」。
	h.tt.Key(0, mygoui.KeyEscape)
	h.tt.Frame()
	// ⚠️ **要让退场走完**：抽屉是「动完再拆」的——那 200ms 里面板还在树上
	//（它滑到窗口外面去了，但还没拆），所以「抽屉没了」要等它滑完。
	settleDrawer(t, h.s, h.tt)
	if h.tt.HasText(queueEmptyHint) {
		t.Error("按了 Esc 抽屉还开着")
	}
}

// queueDrawerWithTwoRows 造一个开着、画着两行的抽屉：第一行是正在放的本地歌
// （标题「a」），第二行是一首在线的音频（标题「队列里的第二首」，副题
// 「B 站音乐」）。
//
// ⚠️ **第二行那首歌故意不播**：正在放的那一行不画悬停三角（它已经在放了），
// 而这一族要量的是**没在放的那一行**上的三角。也不用能让它播——它只是排在
// 队列第二个，`Enqueue` 放的是第一个。
//
// 副题用「B 站音乐」而不是「本地音乐」：后者在侧栏里也有一份（那一页的名字），
// `Tester.Find` 拿到的是第一个，会量到侧栏的菜单行。
func queueDrawerWithTwoRows(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	second := app.Item{
		Source: "online", Type: "audio", Sid: "999", Bvid: "BV1probe", Cid: "1",
		Title: "队列里的第二首",
	}
	enqueue(t, h, append(copiesOf(t, 1), second)...)
	openDrawer(t, h)
	return h
}

// TestTheQueueDrawerUsesTheUpstreamTypeScale 钉住队列抽屉那三档字号（票 #177）。
//
// 上游 `music-playlist-drawer/index.tsx:131-133` 与 `list-item.tsx:68-80`：
//
//	标题「播放列表」  18 / 400（`h3` = `text-lg`，Preflight 重置了字重）
//	条数「(2)」       14（`text-sm`）
//	行的歌名 / 副题   16 / 14（`text-base` / `text-sm`）
//
// 改之前是 15/700、12、15/12：标题小一号还重两档，副题比列表行的 UP 名还小一档。
//
// # 判据：能靠画面量的都走画面
//
// 条数和副题在画面上都只有一处，量的是文字盒高（字号 × `textBoxPerSize`，和
// `typography_test.go` 同一把尺子）。
//
// ⚠️ **两个标题量不了，只能钉代码**：
//
//   - 「播放列表」：`Tester.Find` 拿到的是播放条上那个开抽屉的图标键（Label 也
//     是「播放列表」，而且在树里排得更前），量到的是 32×32 的点击区。
//   - 行歌名：`ListState.Label` 也用的是这一行的歌名，而标签节点的框是**整行**
//     （实测 383×64），量到的是行高，不是字高。
//
// 所以这两条解析 `queue_drawer.go`：画标题那一句必须是 18 且**没有**
// `Bold()` / `FontWeight(...)`；画行歌名那一句必须是 16。字重本来也画不出盒子的
// 差别——这正是 `typography_test.go` 里设置页行标题那一条的做法。
func TestTheQueueDrawerUsesTheUpstreamTypeScale(t *testing.T) {
	h := queueDrawerWithTwoRows(t)

	for _, want := range []struct {
		text string
		size float64
		what string
	}{
		{text: "(2)", size: 14, what: "条数"},
		{text: "B 站音乐", size: 14, what: "行副题"},
	} {
		r, ok := h.tt.Find(want.text)
		if !ok {
			t.Fatalf("抽屉里找不到%s「%s」，文字有 %q", want.what, want.text, h.tt.Texts())
		}
		if got := float64(r.H) / textBoxPerSize; math.Abs(got-want.size) > 0.8 {
			t.Errorf("队列抽屉的%s「%s」折算出来是 %.1fpx，want %.0f"+
				"（上游 18/14/16/14 那一套刻度）",
				want.what, want.text, got, want.size)
		}
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "queue_drawer.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 queue_drawer.go 失败：%v", err)
	}
	header18, headerHeavy, row16 := false, "", false
	ast.Inspect(f, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		header, rowTitle := false, false
		has18, has16 := false, false
		heavy := ""
		ast.Inspect(stmt, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch callName(call) {
			case "Text":
				if len(call.Args) != 2 {
					break
				}
				if mentionsString(call.Args[1], "播放列表") {
					header = true
				}
				if sel, ok := call.Args[1].(*ast.SelectorExpr); ok && sel.Sel.Name == "Title" {
					rowTitle = true
				}
			case "FontSize":
				if len(call.Args) == 1 {
					// ⚠️ **认刻度表里的那个名**（`scale.Size18` /
					// `scale.Size16`），不认裸数字：字号的出处是
					// `widgets/scale` 那张表（票 #238）。
					has18 = has18 || mentionsIdent(call.Args[0], "Size18")
					has16 = has16 || mentionsIdent(call.Args[0], "Size16")
				}
			case "Bold":
				heavy = "Bold()"
			case "FontWeight":
				heavy = "FontWeight(...)"
			}
			return true
		})
		if header && has18 {
			header18 = true
			headerHeavy = heavy
		}
		if rowTitle && has16 {
			row16 = true
		}
		return true
	})
	if !header18 {
		t.Error("queue_drawer.go 里没有一句同时写着 `Text(c, \"播放列表\")` 和 `FontSize(…18…)`" +
			"——队列抽屉标题必须是 18/400")
	}
	if headerHeavy != "" {
		t.Errorf("队列抽屉标题用了 %s——上游的 h3 是 400，不是加粗", headerHeavy)
	}
	if !row16 {
		t.Error("queue_drawer.go 里没有一句同时写着 `Text(c, …Title…)` 和 `FontSize(…16…)`" +
			"——队列行的歌名必须是 16（DESIGN.md 的列表歌名那一档）")
	}
}

// TestTheHoveredQueueRowShowsAPlayIcon 钉住「行 hover 的播放三角是 20 的图标」
// （票 #177）。
//
// 上游 `list-item.tsx:48-52` 在封面上盖一层 `bg-[rgba(0,0,0,0.35)]`，中间是
// `<RiPlayFill size={20}/>`。我们原来画的是「▶」14 号字：字形跟着系统字体走，
// 同一个意思在两台机器上是两个不同的宽高。
//
// # 判据
//
//  1. 画面上**不再有「▶」这个字**——图标对 `Tester.Texts()` 是不可见的，所以
//     「`▶` 还在」就说明那还是一个字。
//  2. 蒙层真的盖上了（封面中心那一块明显变暗），而且中心 32×32 里出现了**近白
//     的像素**——那是压在蒙层上的白三角；蒙层下面的底色和封面的占位图标都到不了
//     这个亮度。这些像素还得落在封面中心附近，高宽都有几像素（20 的图标里三角
//     本身约 10×13）。
//
// 两步都要：只量「近白像素」的话，浅色主题下封面自己的浅色底就能凑出来，
// 删掉整个悬停层这条也会绿。
//
// 量的是没在放的那一行：正在放的那行不浮三角（见 `viewQueueCover`）。
func TestTheHoveredQueueRowShowsAPlayIcon(t *testing.T) {
	h := queueDrawerWithTwoRows(t)

	// ⚠️ 这个 `Find` 拿到的是**整行**（`ListState.Label` 也是这一行的歌名，
	// 而标签节点的框是行框：实测 383×64）。所以行内的位置要用行高和内边距算：
	// 行左边 + 8 是封面左沿，行中心就是封面中心（封面 48 竖着铺满 64-16）。
	row, ok := h.tt.Find("队列里的第二首")
	if !ok {
		t.Fatalf("抽屉里找不到第二行，文字有 %q", h.tt.Texts())
	}
	coverLeft := row.X + queuePadX
	cy := row.Y + row.H/2
	cx := coverLeft + queueCoverSize/2

	idle := snapshot(h.tt)
	h.tt.Move(cx, cy)
	h.tt.Frame()

	if h.tt.HasText("▶") {
		t.Errorf("悬停之后画面上还有「▶」这个字，文字有 %q——三角应该是一枚图标", h.tt.Texts())
	}

	hovered := snapshot(h.tt)
	// 封面中心这一块：**先确认蒙层真的盖上了**。不先确认的话，下面「近白像素」
	// 在浅色主题里拿封面自己的浅色底就能凑出来——删掉整个悬停层这条也会绿。
	// 蒙层是 35% 黑，浅色底下这一块至少要暗三成。
	const half = 16
	darkened := 0
	for y := int(cy) - half; y <= int(cy)+half; y++ {
		for x := int(cx) - half; x <= int(cx)+half; x++ {
			if glyphLuma(idle.RGBAAt(clampX(idle, x), clampY(idle, y)))-
				glyphLuma(hovered.RGBAAt(clampX(hovered, x), clampY(hovered, y))) > 30 {
				darkened++
			}
		}
	}
	if want := (2*half + 1) * (2*half + 1) / 2; darkened < want {
		t.Fatalf("悬停在封面 %.0f,%.0f 上之后只有 %d 个像素变暗（want ≥ %d）——"+
			"那层 35%% 黑的悬停蒙层没画出来，后面量到的「白三角」就不作数",
			cx, cy, darkened, want)
	}

	// 蒙层是 35% 黑：盖上去之后整张封面都暗下来，只有那枚白三角是亮的。
	// **只扫封面中心 32×32**：封面的圆角（Radius 6）在四个角上切掉了蒙层，
	// 露出底下那层浅色底——在浅色主题下那几个角也是「近白」，按整块扫会把
	// 它们算进三角的墨里。
	var minX, minY, maxX, maxY = 1 << 30, 1 << 30, -1, -1
	for y := int(cy) - half; y <= int(cy)+half; y++ {
		for x := int(cx) - half; x <= int(cx)+half; x++ {
			// 白三角是 92% 白压在蒙层上，两套主题下亮度都在 235 上下；
			// 蒙层下面的底色（浅色 166 / 深色 20 上下）和封面的占位图标
			// （`TextMuted`）都到不了 220。
			if glyphLuma(hovered.RGBAAt(clampX(hovered, x), clampY(hovered, y))) < 220 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < minX || maxY < minY {
		t.Fatalf("悬停在封面 %.0f,%.0f 上之后，封面中心没有一块白色的三角——播放图标没画出来",
			cx, cy)
	}
	gw, gh := maxX-minX+1, maxY-minY+1
	if gw < 5 || gh < 6 {
		t.Errorf("三角的墨只有 %d×%d，太小了——它该是一枚 20 的图标（三角本身约 10×13）", gw, gh)
	}
	// 三角的墨应该落在封面中心附近（play-fill 的三角在图标框里偏右一个像素多）。
	if dx := float32(minX+maxX)/2 - cx; math.Abs(float64(dx)) > 4 {
		t.Errorf("三角的墨中心横向偏了 %.1f 像素（封面中心 %.1f，墨中心 %.1f）",
			dx, cx, float32(minX+maxX)/2)
	}
	if dy := float32(minY+maxY)/2 - cy; math.Abs(float64(dy)) > 4 {
		t.Errorf("三角的墨中心纵向偏了 %.1f 像素（封面中心 %.1f，墨中心 %.1f）",
			dy, cy, float32(minY+maxY)/2)
	}
}

func TestQueueDrawerClosesOnAClickOutside(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 3)...)
	openDrawer(t, h)

	h.tt.ClickAt(40, 120) // 抽屉那一层上、面板左边
	h.tt.Frame()
	// ⚠️ **要让退场走完**：抽屉是「动完再拆」的（见上面那一条）。
	settleDrawer(t, h.s, h.tt)
	if h.tt.HasText("定位当前播放") {
		t.Error("点了抽屉外面，抽屉还开着")
	}
}

// TestQueueDrawerLeavesThePlaybarUsable 钉住抽屉那一层**不盖播放条**。
//
// 差一格的表现是「按了下一首，抽屉跟着关了」——用户明明只想换歌。
// Biu 那边是 `data-[placement=right]:mb-22`（上游的 88），我们的是 `playbarHeight`（76）。
func TestQueueDrawerLeavesThePlaybarUsable(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 2)...)
	openDrawer(t, h)

	if err := h.tt.Click("下一首"); err != nil {
		t.Fatalf("抽屉开着的时候点不了「下一首」：%v", err)
	}
	waitFor(t, "下一首", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().Title == "b"
	})
	if !h.tt.HasText("定位当前播放") {
		t.Error("点了一下播放条，抽屉跟着关了")
	}
}

// TestQueueDrawerTakesLocalSongsFromTheirPath 判重标识的三种拼法里，本地那一支
// 是拿路径当键的（`local:<完整路径>`）。两个不同的文件长成同一个歌名也不能并成
// 一行——那是两首不同的歌。
func TestQueueDrawerTakesLocalSongsFromTheirPath(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	twinA := writeCopy(t, dir, "同名.flac")
	twinB := writeCopy(t, dir+"-别的目录", "同名.flac")
	h.app.Queue().Append(app.LocalItem(twinA), app.LocalItem(twinB))
	h.tt.Frame()
	openDrawer(t, h)

	if got := countRows(h); got != 2 {
		t.Errorf("两个同名文件在抽屉里合成了一行（画着 %d 行），want 2（本地项的键是完整路径）", got)
	}
}

// TestQueueDrawerRowIsSixtyFourTall 钉住行高 64。
//
// 「定位当前播放」滚到的是 `targetIndex * 64`，行高不是 64 的话那个乘法算出来
// 的就不是这一行的顶边——而这里量到的**是行元素自己的框**（那一行还带着
// ListState.Label 给的无障碍名字，`Tester.Find` 按字找回来就是它）。
func TestQueueDrawerRowIsSixtyFourTall(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(onlineSong("BV1", "第一首"), onlineSong("BV2", "第二首"))
	h.tt.Frame()
	openDrawer(t, h)

	r, ok := h.tt.Find("第二首")
	if !ok {
		t.Fatalf("画面上找不到第二首，文字有 %q", h.tt.Texts())
	}
	if r.H != 64 {
		t.Errorf("一行高 %v，want 64", r.H)
	}
	if r.W != 319 {
		t.Errorf("一行宽 %v，want 319（抽屉 320 减去左边那一线）", r.W)
	}
}

func TestQueueRowClickPlaysThatSong(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 3)...)
	if got := h.app.Snapshot().Title; got != "a" {
		t.Fatalf("放的是 %q，want a", got)
	}
	openDrawer(t, h)

	if err := h.tt.Click("c"); err != nil {
		t.Fatalf("点不了第三行：%v（文字：%q）", err, h.tt.Texts())
	}
	waitFor(t, "跳到第三首", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().Title == "c"
	})
	if got := pathsOfItems(h.app.Queue().Items()); len(got) != 3 {
		t.Errorf("点一行把队列换掉了，剩 %d 首，want 还是 3 首", len(got))
	}
}

// TestQueueDrawerMarksTheSongBeingPlayed 钉「正在放的那一行」是按**判重标识**认
// 的，不是按队列下标。
//
// 摆的是 [甲, 甲]：正在放的是**第二份**甲（队列的当前项在下标 1），抽屉里去重之后只
// 画一行甲。按标识认 → 那行是绿的；按下标认 → 第 0 行和当前项不是同一行，画出来不是
// 绿的。这和 Biu 的 `isSame(playItem, item)` 是一回事。
//
// ⚠️ 必须**真的在放**：「正在放」的判据是「队列的当前项 **且** 确实有歌在放」
// （见 Shell.playingIdentity）——接歌失败停下来之后当前项还落在一首放不出来的歌上，
// 那时它不算正在放。
func TestQueueDrawerMarksTheSongBeingPlayed(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	a := localSong(t, dir, "甲.flac")
	enqueue(t, h, a)
	h.app.Enqueue(a, localSong(t, dir, "乙.flac")) // 队列 甲,甲,乙
	h.app.Queue().Move(2)                         // 当前项挪到乙（不是要它，是确认下标在动）
	h.app.Queue().Move(-1)                        // 回到第二份甲：正在放的是它
	if got := h.app.Queue().CurrentIndex(); got != 1 {
		t.Fatalf("测试自己没摆好：当前项是第 %d 首", got)
	}
	h.tt.Frame()
	openDrawer(t, h)

	if got := countRows(h); got != 2 {
		t.Fatalf("抽屉里画着 %d 行，want 2（甲 甲 乙 去重成 甲 乙）", got)
	}
	playing, ok1 := h.tt.Find("甲")
	other, ok2 := h.tt.Find("乙")
	if !ok1 || !ok2 {
		t.Fatalf("两行都得在画面上：%v %v，文字：%q", ok1, ok2, h.tt.Texts())
	}
	if playing.H == 0 || other.H == 0 {
		t.Fatalf("两行都没画出来，文字：%q", h.tt.Texts())
	}
	// 正在放的那一行是主色底，其余是面板自己的底色。两者的像素必须不同——一样的话
	// 这个状态就没画出来。
	img := h.tt.Image()
	pc := pixelAt(img, playing.X+playing.W/2, playing.Y+playing.H-4)
	oc := pixelAt(img, other.X+other.W/2, other.Y+other.H-4)
	if pc == oc {
		t.Errorf("正在放的那一行和别的行底色一样（%v），「正在放」没画出来", pc)
	}
}

// findRow 是「**抽屉里**那一行现在**真的在画面上**吗」。
//
// 两道过滤，缺一个都会放过「不是那一行」：
//
//  1. **大小不是 0**：MyGo 会给滚出视口、但列表还留着它那个位置的行发一个大小是 0
//     的节点，`Find` 照样报 ok=true。实测：目标偏移和行高对不上时，被定位的那一行就是
//     这个 0×0 的样子——只断 `ok` 的话「滚到了旁边那一首」是绿的。
//  2. **不在播放条那一带**：真放着的时候播放条上也是同一个歌名，而主布局先于浮层
//     排版，`Find` 先撞上的是播放条上那几行字（它比抽屉里那一行窄得多）。
//     抽屉是浮在播放条**上面**的（底边让开 76），所以 y >= 视口高 - 76 的不是抽屉。
func findRow(h *harness, title string) (mygoui.Rect, bool) {
	r, ok := h.tt.Find(title)
	if !ok || r.W <= 0 || r.H <= 0 || r.Y >= drawerBottom {
		return r, false
	}
	return r, true
}

// drawerBottom 是抽屉的底边：视口高减掉播放条那一条（`playbar.go` 的
// `playbarHeight`）。抽屉浮在播放条上面，y 到这儿为止。
//
// ⚠️ **跟着那个常量走，不写死 88**：2026-10-24 那次重设计把播放条从 88
// 改成 76，写死的那个数于是比真的底边**高了 12**——落在那一条带子上的行
// 会被当成「不在抽屉里」。
const drawerBottom = winH - playbarHeight

// TestLocateCurrentGlidesToTheCurrentSong 是验收里「定位当前播放能平滑滚过去」。
//
// 断的是三件事，一件一件都拿掉就会红：
//
//	点之前那一行在画面外 → 队列够长、定位真的会滚
//	点完钟没走还在画面外 → **不是一跳到位**（ListState.ScrollTo 就是一跳，
//	                  换成它这条立刻红）
//	Advance 够久之后在画面上 → 真的滚过去了
func TestLocateCurrentGlidesToTheCurrentSong(t *testing.T) {
	const target = 30
	h := newHarness(t)
	// 真的放得出来的 40 首（在线条目在这个壳里没挂取流，`PlayAt` 会打不开——那
	// 「定位」就变成「定位一首放不出来的歌」了）。
	//
	// ⚠️ **文件名和 Title 故意不一样**：播放条上的歌名是从**路径**推出来的（文件名
	// 去掉后缀），抽屉里那一行画的是 `Item.Title`。写成不一样的一份，`Find(歌名)`
	// 就只会撞上抽屉里那一行——主布局先于浮层排版，不这样它先找到的是播放条上那
	// 几个字（判据在 findRow 的注释里）。
	items := make([]app.Item, 40)
	for i := range items {
		p := writeCopy(t, t.TempDir(), fmt.Sprintf("%02d.flac", i))
		items[i] = app.Item{Source: "local", Path: p, Title: fmt.Sprintf("第%02d首", i)}
	}
	if err := h.app.Enqueue(items...); err != nil {
		t.Fatal(err)
	}
	// 真的跳到第 30 首去放它（不是只挪下标）：「定位」要滚过去的是**在放**的那一首。
	if err := h.app.PlayAt(target); err != nil {
		t.Fatal(err)
	}
	// 播放条上的歌名是从**路径**推出来的，所以这里是文件名的后缀（"30"），
	// 不是抽屉里那一行的「第30首」——上面刻意让两者不一样。
	waitFor(t, "第30首开始播", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().Title == "30"
	})
	h.tt.Frame()
	openDrawer(t, h)

	if _, visible := findRow(h, "第30首"); visible {
		t.Fatal("没定位那一行就在画面上了，测不出滚动")
	}
	if err := h.tt.Click("定位当前播放"); err != nil {
		t.Fatalf("点不了「定位当前播放」：%v", err)
	}
	h.tt.Frame()

	if _, visible := findRow(h, "第30首"); visible {
		t.Error("一点就跳到位了，那不是平滑滚过去（用了 ListState.ScrollTo？）")
	}
	// 推进 1 秒：滚动是 `ui.Smooth`（默认弹簧 500ms），1 秒足够它落定。
	// **别推进 2 秒**：Advance 期间每一帧都要把整个窗口重画一遍，带 `-race` 时
	// 一帧不便宜（实测这条用例 2 秒是 20.7s、1 秒是一半）。
	h.tt.Advance(time.Second)
	r, visible := findRow(h, "第30首")
	if !visible {
		t.Fatalf("Advance 之后还是滚不过去，文字：%q", h.tt.Texts())
	}
	// 滚到的是**第 30 行自己的顶边**，不是别的行的：40 行 × 64 = 2560，视口
	// 372-53 = 319，MaxY 是 2241，30×64 = 1920 没被钳。列表视口的顶在 y=53，
	// 所以这一行的顶边就该落在 53 上。
	//
	// 差得越远说明算的不是 `targetIndex*行高`：**行高和目标偏移用的是同一个数**
	// （见 queue_drawer.go 的 locateCurrent）。两个数分家的时候，滚过去的是
	// 另一首——这一行会变成那个 0×0 的节点，上面那句 `visible` 就把它挡下来了。
	if r.Y > 56 {
		t.Errorf("那一行停在 y=%v，want ≈53（滚到的是第 30 行的顶边）", r.Y)
	}
}

// TestLocateCurrentSaysSoWhenThereIsNothingToLocate 两种「滚不了」都要说话
// （Biu 的两个 addToast）：默默不动的话，用户分不清是按钮坏了还是自己记错了。
func TestLocateCurrentSaysSoWhenThereIsNothingToLocate(t *testing.T) {
	t.Run("没在放", func(t *testing.T) {
		h := newHarness(t)
		h.app.Queue().Append(onlineSong("BV1", "第一首")) // 放进去了，还没开始放
		h.tt.Frame()
		openDrawer(t, h)

		if err := h.tt.Click("定位当前播放"); err != nil {
			t.Fatal(err)
		}
		h.tt.Frame()
		if !h.tt.HasText("当前没有正在播放的歌曲") {
			t.Errorf("没在放的时候一句话也没有，文字：%q", h.tt.Texts())
		}
	})

	t.Run("这一首认不出来", func(t *testing.T) {
		h := newHarness(t)
		// 真的有歌在放（判据的第一半），队列的当前项挪到一个**什么来源都没有**的
		// 占位项上（Identity 是空串，定位无从谈起）。
		//
		// 这个组合界面上走不到（放不出来的项 `step` 会退回去），这里是为了钉住
		// 那句「认不出来」不是死代码：真走到了，说的必须是这一句而不是「没有正在
		// 播放的歌曲」——有歌在放着呢。
		dir := t.TempDir()
		enqueue(t, h, localSong(t, dir, "甲.flac"))
		h.app.Queue().Append(app.Item{Title: "还没接来源的一项"})
		h.app.Queue().Move(1)
		if got := h.app.Queue().CurrentIndex(); got != 1 {
			t.Fatalf("测试自己没摆好：当前项是第 %d 首", got)
		}
		h.tt.Frame()
		openDrawer(t, h)

		if err := h.tt.Click("定位当前播放"); err != nil {
			t.Fatal(err)
		}
		h.tt.Frame()
		if !h.tt.HasText("认不出来") {
			t.Errorf("认不出的那一首没有说话，文字：%q", h.tt.Texts())
		}
	})
}

// TestQueueRowMenuOffersFiveItemsForAnOnlineSong 五项是 Biu
// `music-playlist-drawer/menu.tsx` 的 `getMenus` 那一套，一项不多一项不少。
func TestQueueRowMenuOffersFiveItemsForAnOnlineSong(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(onlineSong("BV1", "第一首"))
	h.tt.Frame()
	openDrawer(t, h)

	if err := h.tt.RightClick("第一首"); err != nil {
		t.Fatalf("右键不了这一行：%v", err)
	}
	want := []string{"收藏", "下载音频", "下载视频", "在 B 站打开", "从列表删除"}
	got := h.tt.Menu()
	if len(got) != len(want) {
		t.Fatalf("菜单有 %d 项 %q，want %d 项 %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("菜单第 %d 项是 %q，want %q", i+1, got[i], want[i])
		}
	}
}

// TestQueueRowMenuOffersTheSameFiveItemsForALocalSong 本地项**也是五项**。
//
// 这一条盯的是「今天唯一把项放进队列的入口是 enqueuePicked（本地文件）」：照 Biu 的
// hidden 规则（收藏 / 下载 / 在 B 站打开对本地项都藏起来）的话，普通用户看到的行
// 菜单**永远只有「从列表删除」一项**，五项一条都见不到。第四项按来处换成「在访达中
// 显示」（判断和内容列表那一套菜单共用一份，见 list.OpenLabel）。
func TestQueueRowMenuOffersTheSameFiveItemsForALocalSong(t *testing.T) {
	h := newHarness(t)
	// 歌名起得特别一点：Tester 是按**精确的字**找元素的，歌名太普通会先撞上
	// 播放条上那一行同一个名字（那儿也写着这一首）。
	h.app.Queue().Append(app.LocalItem(writeCopy(t, t.TempDir(), "本地那一首.flac")))
	h.tt.Frame()
	openDrawer(t, h)

	if err := h.tt.RightClick("本地那一首"); err != nil {
		t.Fatalf("右键不了这一行：%v", err)
	}
	want := []string{"收藏", "下载音频", "下载视频", "在访达中显示", "从列表删除"}
	got := h.tt.Menu()
	if len(got) != len(want) {
		t.Fatalf("本地项的菜单有 %d 项 %q，want %d 项 %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项是 %q，want %q", i+1, got[i], want[i])
		}
	}
}

// TestQueueRowMenuItemsReallyDoTheirThing 钉验收那句话的后半段：**接上真实
// 操作**。这里断的是不需要 B 站和下载管理器就能断的两项（下载音频 / 从列表删除），
// 逐项走菜单（不是直接调 App 的方法）——走菜单才是用户按的那条路，菜单里少建一项、
// 或者那一项接错了函数，只有这样断得出来。
//
// 「收藏」和「下载视频」两条的端到端证据在 `queue_drawer_favorite_test.go`
// （`package ui`）：它们要真的 B 站客户端和真的下载管理器，那台假 B 站在那边。
//
// ⚠️ 「在访达中显示 / 在 B 站打开」不按：最后一步是 `mygo.Shell.ShowItemInFolder` /
// `OpenExternal`，它们要真的事件循环，在测试里会永远等下去（实测整个测试挂死）。
// 那一项验的是它在菜单里（上面两条）以及它的兄弟分支：没有来处时它回的那句提示。
func TestQueueRowMenuItemsReallyDoTheirThing(t *testing.T) {
	t.Run("下载音频", func(t *testing.T) {
		h := newHarness(t)
		mountDownloads(t, h.app)
		h.app.Queue().Append(onlineSong("BV1", "在线那一首"))
		openDrawer(t, h)
		chooseQueueItem(t, h, "在线那一首", "下载音频")

		if !h.tt.HasText("已加进下载队列") {
			t.Errorf("没有说加进下载队列，文字：%q", h.tt.Texts())
		}
		if n := len(h.app.DownloadSnapshots()); n != 1 {
			t.Errorf("下载队列里有 %d 个任务，want 1", n)
		}
		// 队列不等它下完（取流那一头本票没挂），但也别留一条 Queued 的任务给
		// Close 去等——见 mountDownloads 那段注释。
		h.app.ClearDownloads()
	})

	t.Run("从列表删除", func(t *testing.T) {
		h := newHarness(t)
		dir := t.TempDir()
		enqueue(t, h, localSong(t, dir, "甲.flac"))
		h.app.Enqueue(localSong(t, dir, "乙.flac"), localSong(t, dir, "丙.flac"))
		openDrawer(t, h)
		chooseQueueItem(t, h, "乙", "从列表删除")

		got := titlesOf(h.app.Queue().Items())
		if len(got) != 2 || got[0] != "甲" || got[1] != "丙" {
			t.Errorf("按了「从列表删除」队列是 %q，want 甲/丙", got)
		}
	})
}

// mountDownloads 把下载挂上（真实入口是 `download_mount.go`）。不挂的话
// `App.Download` 回「下载还没接上」，那验的是「没挂」不是「菜单接上了」。
//
// ⚠️ **挂完立刻排一条 cleanup 把队列清空**：cleanup 是后进先出的，所以它跑在
// `newHarness` 登记的 `app.Close` **之前**。不清的话，队列里躺着一条 Queued 的
// 任务，而 `app.Close` → `downloads.Close` 会等后台那条线退出——那条线此时正卡在
// 一个活锁里（`download.(*Manager).loop` 的内层循环不认 `ctx.Done()`，而
// `runOne` 见到 `m.closed` 会原样返回、不改任务状态，于是它一直转、一直返回同一
// 条任务），`Close` 就永远等不到它。窗口很窄但实测会中：满负载时这个包会卡满 10
// 分钟然后超时死掉。
//
// 上游那个活锁是 `internal/service/download` 的事（#71 那一票的地界），这里只是
// 别踩它。
func mountDownloads(t *testing.T, a *app.App) {
	t.Helper()
	dir := t.TempDir()
	if err := a.MountDownloads(app.Downloads{
		Media:       http.DefaultClient,
		QueueFile:   filepath.Join(dir, "queue.json"),
		FallbackDir: dir,
	}); err != nil {
		t.Fatalf("挂下载：%v", err)
	}
	t.Cleanup(a.ClearDownloads)
}

// chooseQueueItem 右键 title 那一行，再从菜单里选 label 那一项。
func chooseQueueItem(t *testing.T, h *harness, title, label string) {
	t.Helper()
	if err := h.tt.RightClick(title); err != nil {
		t.Fatalf("右键不了 %q 那一行：%v", title, err)
	}
	if err := h.tt.ChooseMenuItem(label); err != nil {
		t.Fatalf("选不了菜单里的 %q（菜单是 %q）：%v", label, h.tt.Menu(), err)
	}
	h.tt.Frame()
}

// TestDeletingADedupedRowTakesEveryCopyOut 队列是 **[甲, 甲, 乙]**，抽屉里去重
// 之后画两行（甲、乙）。
//
// 评审踩到的那一条：右键第一行甲、按「从列表删除」，只删一份的话那一行画完还在
// （顶上的计数也不变），而播放条上还是甲——用户按了等于没按。Biu 的 `play-list.ts`
// 里 `del` 用的就是 `isSame` 删全部同身份项。
func TestDeletingADedupedRowTakesEveryCopyOut(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	a := localSong(t, dir, "甲.flac")
	enqueue(t, h, a)
	h.app.Enqueue(a, localSong(t, dir, "乙.flac")) // 队列 甲,甲,乙
	openDrawer(t, h)

	if got := countRows(h); got != 2 {
		t.Fatalf("抽屉里画着 %d 行，want 2（去重之后）", got)
	}
	// 右键甲 那一行要用坐标：甲 正在放，同一个字在播放条上也有。
	h.tt.RightClickAt(500, 85)
	if got := h.tt.Menu(); len(got) != 5 {
		t.Fatalf("右键甲 那一行没有菜单，文字：%q", h.tt.Texts())
	}
	if err := h.tt.ChooseMenuItem("从列表删除"); err != nil {
		t.Fatalf("选不了「从列表删除」：%v", err)
	}
	h.tt.Frame()

	if got := titlesOf(h.app.Queue().Items()); len(got) != 1 || got[0] != "乙" {
		t.Fatalf("删完之后队列是 %q，want 只剩 乙", got)
	}
	if h.tt.HasText("甲") {
		t.Errorf("删掉了两首 甲，画面上还在，文字：%q", h.tt.Texts())
	}
	if got := countRows(h); got != 1 {
		t.Errorf("抽屉里画着 %d 行，want 1", got)
	}
	// 删掉的里面有正在放的那首 → 接着放乙。
	waitFor(t, "接着放乙", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().Title == "乙"
	})
}

// TestQueueRowClickUsesItsQueuePositionNotItsRowNumber 是上一条的另一半：点一行
// 「从这儿听」用的是**它在队列里的下标**。
//
// 把 `queueRowsFor` 里的 `at` 写成行号，原来那批用例**一条都不会红**——它们排的
// 队列里没有重复项，行号和队列下标本来就是同一个数。这一条排的队列里多出一格重复。
func TestQueueRowClickUsesItsQueuePositionNotItsRowNumber(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	a := localSong(t, dir, "甲.flac")
	enqueue(t, h, a)
	h.app.Enqueue(a, localSong(t, dir, "乙.flac")) // 队列 甲,甲,乙；乙是第 3 首、第 2 行
	openDrawer(t, h)

	if err := h.tt.Click("乙"); err != nil {
		t.Fatalf("点不了第二行：%v", err)
	}
	waitFor(t, "跳到乙", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().Title == "乙"
	})
	if got := h.app.Queue().CurrentIndex(); got != 2 {
		t.Errorf("点第二行之后当前项是第 %d 首，want 第 3 首（它在队列里的位置）", got)
	}
}

// TestQueueRowMenuSaysSoWhenThereIsNowhereToOpen 「在 B 站打开」接的是内容列表
// 那一套 Actions.Open（本地文件先判，其次才轮到 B 站地址）。这里断的是**它确实
// 接在那套上**：给一个没有 bvid 也没有 sid 的在线项，那一套会回一句「这一项还没有
// 可以打开的来处」。要是这一项自己写一份、或者干脆什么都不做，这句话不会出现。
//
// ⚠️ 不在这里真的按一个**有** bvid 的：那条路最后是 `mygo.Shell.OpenExternal`，
// 它要真的事件循环，测试里会永远等下去，还会真的把浏览器打开。
func TestQueueRowMenuSaysSoWhenThereIsNowhereToOpen(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(app.Item{Source: "online", Type: "mv", Title: "没有来处的一项"})
	h.tt.Frame()
	openDrawer(t, h)

	if err := h.tt.RightClick("没有来处的一项"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("在 B 站打开"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.tt.HasText("还没有可以打开的来处") {
		t.Errorf("没有一句话说清开不了，文字：%q", h.tt.Texts())
	}
}

func TestQueueDrawerClearEmptiesItAndStops(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 2)...)
	openDrawer(t, h)

	if err := h.tt.Click("清空"); err != nil {
		t.Fatalf("点不了「清空」：%v", err)
	}
	h.tt.Frame()

	// ⚠️ **要问一次**（票 #454）：这一下是「一键销毁整个队列 + 停掉播放」，而它
	// 原来连确认都没有。确认框长什么样、取消会怎么样，见 `ticket454_test.go` 的
	// `TestClearingTheQueueAsksFirst`——这一条只管「确定了之后确实清掉了」。
	if err := h.tt.Click("确认清空"); err != nil {
		t.Fatalf("点不了确认键：%v（文字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()

	if n := h.app.Queue().Len(); n != 0 {
		t.Errorf("清空之后队列还剩 %d 首", n)
	}
	if st := h.app.Snapshot(); st.HasTrack {
		t.Errorf("清空之后还在放：%+v", st)
	}
	if !h.tt.HasText(queueEmptyHint) {
		t.Errorf("清空之后抽屉里不是空状态，文字：%q", h.tt.Texts())
	}
}

// TestQueueDrawerListsEverySongOnce 钉「按判重标识去重」，也顺带是一道保险：
// 抽屉的行身份是判重标识（铁律四），而 MyGo 在 Tester 里**撞了 key 当场 panic**
// （`t.rt.strict = true`），所以去掉 queueRowsFor 里那一次去重，这里就红。
func TestQueueDrawerListsEverySongOnce(t *testing.T) {
	h := newHarness(t)
	dup := onlineSong("BV1", "第一首")
	// 五首里只有两个不同的判重标识，第一首还排了三遍。
	h.app.Queue().Append(dup, onlineSong("BV2", "第二首"), dup, dup, dup)
	h.tt.Frame()
	openDrawer(t, h)

	if got := countRows(h); got != 2 {
		t.Errorf("抽屉里画着 %d 行，want 2（五首里只有两个不同的判重标识）", got)
	}
	// 顶上那个计数也是去重之后的（Biu 的 `pureList?.length`）。
	if !h.tt.HasText("(2)") {
		t.Errorf("顶上写的不是去重之后的数量 (2)，文字有 %q", h.tt.Texts())
	}
}

// TestQueueDrawerShowsEachEpisodeOfOneVideo 钉住票 #683 的那条行为变化：
// **同一个 bvid 的两集在抽屉里是两行**。
//
// 判重标识带上 cid 之前，这里只画一行——于是用户看到队列里只有一首，而它其实
// 是两集；点「第 2 集」也找不到那一行（按 bvid 找的是第一集）。
//
// ⚠️ **这两项要真的带不同 cid**：`onlineSong` 那个 helper 每一条都填 `Cid: "1"`，
// 用它造出来的两项判重上仍然是同一首（同一集的两次拷贝），这一条就会红得莫名其
// 妙——而直觉上「同一个 bvid 就是同一个视频」看着又很像对。
func TestQueueDrawerShowsEachEpisodeOfOneVideo(t *testing.T) {
	h := newHarness(t)
	episode := func(cid, title string) app.Item {
		return app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: cid, Title: title}
	}
	h.app.Queue().Append(episode("111", "第一集"), episode("222", "第二集"))
	h.tt.Frame()
	openDrawer(t, h)

	if got := countRows(h); got != 2 {
		t.Errorf("抽屉里画着 %d 行，want 2（同一个投稿的两集各算一首）", got)
	}
	for _, want := range []string{"第一集", "第二集"} {
		if !h.tt.HasText(want) {
			t.Errorf("抽屉里没有 %q，文字有 %q", want, h.tt.Texts())
		}
	}
	if !h.tt.HasText("(2)") {
		t.Errorf("顶上写的不是 (2)，文字有 %q", h.tt.Texts())
	}
}

// pathsOfItems / titlesOf 是一列项的路径 / 歌名，好和期望逐个比。
func pathsOfItems(items []app.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Path
	}
	return out
}

func titlesOf(items []app.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}
