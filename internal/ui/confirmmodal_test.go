package ui

// 这一组盯的是「确认框是**应用自绘**的那一种」（票 #176 / G33）。
//
// 判据全是**画面上的东西**：图标在不在、图标是什么颜色、确认按钮是什么颜色、
// 取消按钮是不是 light、正文是不是 `text-sm`。断函数调用断不出这些——而
// 「系统原生那个」和「自绘那个」的区别**恰好全在画面上**（原生那个面板固定
// 300 宽、标题 14 粗体、**一个图标都没有**）。
//
// # 为什么逐像素
//
// 「图标是红的」这件事在画面上就是**一块红色的像素**。而断「调了某个函数」
// 的话，一个把图标画成别的东西的实现照样绿——而用户看到的是「一个灰的
// 三角形」（或者什么都没有）。

import (
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// 语义色的字面值（DESIGN.md §四 那一档）。
//
// ⚠️ **别用主题里的 `t.Danger`**：主题那一个可能被用户改过，而这里要钉的是
// **上游那一档**（票 #172 换上去的那个）。
const (
	confirmDangerHex  = "#f31260"
	confirmWarningHex = "#f5a524"
)

// TestTheConfirmDialogHasAToneColoredWarningIcon 钉住「标题左边有一枚按类型
// 变色的警告图标」。
//
// ⚠️ **这一条就是「不是系统原生那个」的判据**：MyGo 的 `ui.AlertDialog`
// （原生那一份）**一个图标都没有**——它的面板是「标题 + 正文 + 两颗按钮」，
// 而上游那个 `components/confirm-modal` 的标题行是
// `<RiErrorWarningLine color={colorMap[type]} />` + 标题。
//
// 判据是**那块区域里有几块接近目标色的像素**：图标是 SVG，笔画之间是空的，
// 所以不能指望整块都是色的——要找的是**笔画**。
func TestTheConfirmDialogHasAToneColoredWarningIcon(t *testing.T) {
	_, s, tt := collectionShell(t, "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	openBatchConfirm(t, tt, "批量删除")

	title, ok := tt.Find("确认删除选中的 2 首吗？")
	if !ok {
		t.Fatalf("确认框没画出来（找不到标题），文字有 %q", tt.Texts())
	}
	if n := countTonePixels(t, tt, title, hexColor(t, confirmDangerHex)); n < 3 {
		t.Errorf("标题左边只有 %d 块接近 %s 的像素，want ≥3——那枚警告图标不在（或者不是红的）",
			n, confirmDangerHex)
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
	t.Run("危险", func(t *testing.T) {
		_, s, tt := collectionShell(t, "12345",
			mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
		waitSongs(t, tt, s, 3)
		openBatchConfirm(t, tt, "批量删除")
		assertConfirmButton(t, tt, "确认删除", hexColor(t, confirmDangerHex))
	})

	t.Run("警告", func(t *testing.T) {
		_, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"))
		waitSongs(t, tt, s, 1)
		if err := chooseMore(t, tt, "清除失效内容"); err != nil {
			t.Fatal(err)
		}
		assertConfirmButton(t, tt, "确认清除", hexColor(t, confirmWarningHex))
	})

	t.Run("普通", func(t *testing.T) {
		_, s, tt := collectionShellAs(t, "1", "12345", mediaOf(1, "歌 一"))
		waitSongs(t, tt, s, 1)
		// 「从侧栏移除」是 primary 那一档：它一个字节都不往 B 站发
		// （理由见 `confirmToneOf`）。
		if err := chooseMore(t, tt, "从侧栏移除"); err != nil {
			t.Fatal(err)
		}
		// 主色那一档跟着用户挑的那一档走（默认设置是 Biu 的绿）。
		accent := BuildTheme(app.DefaultSettings(""), false).Accent
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
// ⚠️ **量的是高度，而且拿同一个框里的标题当尺子**：文字的行高**正好跟着字号
// 走**（实测 13px 的行高 15.310547、14px 的 16.488281、16px 的 18.84375——
// 三档两两成比例，粗体也不变）。所以「正文行高 ÷ 标题行高」就是两个字号之比。
//
// ⚠️ **不能拿宽度当判据**：中文标点在系统字体里是**半宽**的（实测「。」占
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
	title, ok := tt.Find("确认删除吗？")
	if !ok {
		t.Fatalf("弹窗上没有那个标题，文字有 %q", tt.Texts())
	}
	// 标题是 16（`confirmModal` 里写死的），正文要的是 14。
	want := title.H * 14 / 16
	if r.H < want-0.05 || r.H > want+0.05 {
		t.Errorf("正文的行高是 %v，want %v（标题 %v × 14/16）——它不是 text-sm",
			r.H, want, title.H)
	}
}

// TestTheCancelButtonIsLight 钉住「取消」是 light 的：透明底、没有描边。
//
// 上游是 `<Button variant="light">`。它和「确认」摆在一起时，用户一眼要能分出
// 哪个是主操作——而 light 那一档（不显眼）正是这个作用。
func TestTheCancelButtonIsLight(t *testing.T) {
	_, s, tt := collectionShell(t, "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	openBatchConfirm(t, tt, "批量删除")

	cancel, ok := tt.Find("取消")
	if !ok {
		t.Fatalf("弹窗上没有「取消」，文字有 %q", tt.Texts())
	}
	// 它坐在弹窗那块面板上，而面板的底色就是页面底色——所以「这一格和它
	// 后面那一块是同一个颜色」正是 light 的样子。
	assertFlat(t, tt.Image(), cancel, "取消")
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

// countTonePixels 数标题左边那一块里有几块接近 want 的像素。
func countTonePixels(t *testing.T, tt *ui.Tester, title ui.Rect, want color.RGBA) int {
	t.Helper()
	img := tt.Image()
	// 图标在标题左边：18 宽 + 4 的缝。
	x0 := max(int(title.X)-confirmIconSize-6, 0)
	x1 := max(int(title.X)-2, 1)
	y0 := max(int(title.Y)-6, 0)
	y1 := min(int(title.Y+title.H)+6, img.Rect.Max.Y)
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
