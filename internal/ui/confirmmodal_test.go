package ui

// 这一组盯的是「确认框是**应用自绘**的那一种」（票 #176 / G33）。
//
// 判据全是**画面上的东西**：图标在不在、图标是什么颜色、确认按钮是什么颜色、
// 取消按钮是不是带壳、正文是不是 `text-sm`。断函数调用断不出这些——而
// 「系统原生那个」和「自绘那个」的区别**恰好全在画面上**（原生那个面板固定
// 300 宽、标题 14 粗体、**一个图标都没有**）。
//
// # 为什么逐像素
//
// 「图标是红的」这件事在画面上就是**一块红色的像素**。而断「调了某个函数」
// 的话，一个把图标画成别的东西的实现照样绿——而用户看到的是「一个灰的
// 三角形」（或者什么都没有）。

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"image/color"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// TestTheConfirmDialogHasANameInTheAccessibilityTree 钉住「那个框在无障碍树里
// **有名字**」。
//
// 🔴 **这一条是换掉系统原生那一份时丢掉的**：原生那个 `ui.AlertDialog` 建面板
// 时是 `Role(RoleAlertDialog)` + `panel.Label(title)` +
// `panel.description = message`（mygo v0.6.3 的 `ui/feedback.go:186-188`），
// 所以读屏软件报得出「删除确认吗？」和它下面那一句正文。而 `ui.Modal`
// （`coreDialogBase`）只给 `Role(RoleDialog)`——**既没有名字也没有描述**，
// 于是读屏软件只报「一个对话框」。
//
// 判据：**那个标题在 `Texts()` 里出现两次**——一次是面板的无障碍名字，一次是
// 那个标题字本身（原生那一份也是这样：它也是 `Label` + 一个同名文字）。
func TestTheConfirmDialogHasANameInTheAccessibilityTree(t *testing.T) {
	_, s, tt := collectionShellOpts(t, []shellOpt{at(1200, 1200)}, "1", "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	openBatchConfirm(t, tt, "批量删除")

	const title = "确认删除选中的 2 首吗？"
	n := 0
	for _, txt := range tt.Texts() {
		if txt == title {
			n++
		}
	}
	if n < 2 {
		t.Errorf("那个标题在无障碍树里只出现 %d 次，want ≥2（面板的名字 + 标题字）——"+
			"它没有名字，读屏软件报不出这个框是什么", n)
	}
}

// TestTheConfirmButtonTakesTheToneColor 钉住「确认键的颜色跟着类型」。
//
// 上游是 `<Button color={type}>`（`components/confirm-modal/index.tsx:79`），
// 所以三档分别是：危险红、警告橙、普通主色。
//
// ⚠️ **取的是按钮左边那一小块，不是中心**：按钮上写着字，而中心正落在字上
// （主色按钮上的字是白的）——量中心量到的是**字**，不是底。
func TestTheConfirmButtonTakesTheToneColor(t *testing.T) {
	// 原型的确认键一律是 `.btn-danger`（`var(--err)` 底 + 白字，index.html 288 行）：危险和警告两档
	// 都是 err 底；普通（非破坏性）那一档是主色键。
	th := BuildTheme(app.DefaultSettings(""), false)
	pal := tok.Of(th)
	danger := color.RGBA{R: pal.Err.R, G: pal.Err.G, B: pal.Err.B, A: pal.Err.A}
	warning := danger

	t.Run("危险", func(t *testing.T) {
		_, s, tt := collectionShellOpts(t, []shellOpt{at(1200, 1200)}, "1", "12345",
			mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
		waitSongs(t, tt, s, 3)
		openBatchConfirm(t, tt, "批量删除")
		assertConfirmButton(t, tt, "确认删除", danger)
	})

	t.Run("警告", func(t *testing.T) {
		_, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"))
		waitSongs(t, tt, s, 1)
		if err := chooseMore(t, tt, "清除失效内容"); err != nil {
			t.Fatal(err)
		}
		assertConfirmButton(t, tt, "确认清除", warning)
	})

	t.Run("普通", func(t *testing.T) {
		_, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"))
		waitSongs(t, tt, s, 1)
		// 「从侧栏移除」是 primary 那一档：它一个字节都不往 B 站发
		// （理由见 `confirmToneOf`）。
		if err := chooseMore(t, tt, "从侧栏移除"); err != nil {
			t.Fatal(err)
		}
		// 主色那一档跟着当前方案走（默认是暖纸的主色）。
		accent := pal.Accent
		assertConfirmButton(t, tt, "确认移除", color.RGBA{
			R: accent.R, G: accent.G, B: accent.B, A: accent.A,
		})
	})
}

// TestTheConfirmBodyIsTextSmall 钉住正文是 `text-sm`（14）。
//
// 上游那个弹窗的正文是 `<div className="text-sm text-zinc-500">`（14px），
// 而框架的默认字号是 13——**差一档**，而这一档正是「正文读起来是不是和标题
// 一个层次」的事。
//
// ⚠️ **量的是高度，而且拿同一个框里那颗「取消」的字当尺子**：文字的行高**正好
// 跟着字号走**（实测 13px 的行高 15.310547、14px 的 16.488281、16px 的
// 18.84375——三档两两成比例，粗体也不变）。而那颗「取消」是**框架的默认字号
// （13）**，正文要的是 14。
//
// ⚠️ **不能拿标题当尺子**：面板自己带着标题（`Label`，见
// `TestTheConfirmDialogHasANameInTheAccessibilityTree`），而面板是标题那行字的
// **父**——父先被遍历，所以 `Find(标题)` 给的是**面板**那个框（高 126），不是
// 标题字的高（19）。拿它当尺子，这一次会量出「正文的行高是标题的 87%」而
// 判据写的是 14/16 ——差得远。
//
// ⚠️ **也不能拿宽度当判据**：中文标点在系统字体里是**半宽**的（实测「。」占
// 半个字身），而哪几个标点是半宽的跟字体走——按「字数 × 字号」算出来的期望值
// 和实测差 7 个像素（实测 285.09、期望 294）。
func TestTheConfirmBodyIsTextSmall(t *testing.T) {
	const body = "当前任务未下载完成，确认删除后将无法恢复。"
	_, tt := newDownloads(t, newTask("歌 甲", download.StatusPaused, dlProgress, 50))
	if err := tt.Click(opDelete); err != nil {
		t.Fatal(err)
	}
	dlFrame(tt)

	r, ok := tt.Find(body)
	if !ok {
		t.Fatalf("弹窗上没有那句正文，文字有 %q", tt.Texts())
	}
	cancel, ok := tt.Find("取消")
	if !ok {
		t.Fatalf("弹窗上没有那颗「取消」，文字有 %q", tt.Texts())
	}
	// 「取消」是 pud.Button 的 14 号字（原型 `.btn` 14，行高取字体自己的，量出来 `cancel.H`）；正文也是 14，
	// 但它继承 body 的行高 1.55（原型 `.dlg-body p` 不写行高，`body { line-height: 1.55 }`），所以框高是
	// 14 × 1.55 = 21.7。字号是 14 的判据于是是「框高 = 14 × leading.Body」，而不是和按钮的框一样高。
	want := float32(14 * leading.Body)
	if r.H < want-0.05 || r.H > want+0.05 {
		t.Errorf("正文的行高是 %v，want %v（14 号字 × 正文行高 %v；「取消」框高 %v）——它不是 14 号的正文",
			r.H, want, leading.Body, cancel.H)
	}
}

// TestTheCancelButtonKeepsItsShell 钉住「取消」是**带壳**的（常驻底 + 描边）。
//
// 🔴 **这一条在 #176 之后反过来过一次**：#176 把两处系统原生 `ui.AlertDialog`
// 换成自绘模态的时候，把取消键从 `coreButton`（带壳）换成了 `flatButton`
// （扁的），而那违反了 #141「铺开扁平按钮」定下的契约——那一票把页面上**一批**
// 药丸拍平了，**明确留下**对话框这一批（主次分明的地方）。
// `flatspread_test.go` 的 `TestTheDialogButtonsStayOutlined` 一直在钉那一条
// （它从 #176 起就红，票 #228 修的就是它）。
//
// 上游是 HeroUI 的 `variant="light"`（透明底、无描边），而这一处按**仓库的**
// 契约走：取消键和确认键的层级靠的是「壳」，不是颜色——确认键那一档是危险红 /
// 警告橙 / 普通主色，取消键**不在那三档里**（票 #228 明确点名「别把它也涂成
// 主色」）。
//
// ⚠️ **和上游不一样是故意的**：上游要的是「不显眼」，我们要的是「看得出是个键，
// 而且和旁边那颗实心主键分得开」。
func TestTheCancelButtonKeepsItsShell(t *testing.T) {
	_, s, tt := collectionShellOpts(t, []shellOpt{at(1200, 1200)}, "1", "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	openBatchConfirm(t, tt, "批量删除")

	cancel, ok := tt.Find("取消")
	if !ok {
		t.Fatalf("弹窗上没有「取消」，文字有 %q", tt.Texts())
	}
	// 它坐在弹窗那块面板上，而面板的底色就是页面底色——所以「字上面那几行
	// 和外面是同一个颜色」正是「它被拍平了」的样子。
	assertTextKeyHasAShell(t, tt.Image(), cancel, "弹窗里的「取消」")
}

// openBatchConfirm 是「进多选 → 勾两首 → 点某个键 → 确认框弹出来」那一下。
func openBatchConfirm(t *testing.T, tt *ui.Tester, button string) {
	t.Helper()
	enterMulti(t, tt)
	for _, title := range []string{"歌 一", "歌 二"} {
		if err := tt.Click("选择 " + title); err != nil {
			t.Fatal(err)
		}
		settleFrame(tt)
	}
	if err := tt.Click(button); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
}

// assertConfirmButton 断那颗确认键的底色是 want。
func assertConfirmButton(t *testing.T, tt *ui.Tester, label string, want color.RGBA) {
	t.Helper()
	btn, ok := tt.Find(label)
	if !ok {
		t.Fatalf("弹窗上没有那颗确认键（%q），文字有 %q", label, tt.Texts())
	}
	// 取**左边那一小块**：按钮的左右各留 14 的内边距，而中心是字。
	x := int(btn.X) + 4
	y := int(btn.Y + btn.H/2)
	got := probeColor(tt, x, y)
	if maxDiff(got, want) > probeTolerance {
		t.Errorf("确认键（%q）上那块是 %v，want %v", label, got, want)
	}
}

// countTonePixels 数那一块矩形里有几块接近 want 的像素。
func countTonePixels(t *testing.T, tt *ui.Tester, box ui.Rect, want color.RGBA) int {
	t.Helper()
	img := tt.Image()
	x0 := max(int(box.X)-2, 0)
	y0 := max(int(box.Y)-2, 0)
	x1 := min(int(box.X+box.W)+2, img.Rect.Max.X)
	y1 := min(int(box.Y+box.H)+2, img.Rect.Max.Y)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if maxDiff(img.RGBAAt(x, y), want) <= 40 {
				n++
			}
		}
	}
	return n
}

// hexColor 把 `#rrggbb` 翻成颜色。
func hexColor(t *testing.T, s string) color.RGBA {
	t.Helper()
	c := ui.Hex(s)
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}
