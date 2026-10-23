package ui

// 这一组盯的是「状态」那一列的两处颜色（票 #176 / G46）：
//
//	进度条   蓝（上游 `download-list/progress.tsx:49` 的 `bg-blue-500`），
//	         出错的那一条是红（同一个 `indicator` 的另一支）。
//	完成     绿勾 +「下载完成」。
//
// 判据是**画面上的像素**：进度条涂的是哪一块颜色、完成那一格左边有没有一枚
// 绿勾——这两件事在代码里是「调了哪个函数」，而在画面上就是颜色。

import (
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// dlBlue 是上游那个 `bg-blue-500`（Tailwind blue-500 = #3b82f6）。
const dlBlue = "#3b82f6"

// TestTheDownloadBarIsBlueAndNotTheAccent 钉住「进度条是蓝的」。
//
// ⚠️ **不是主色**：上游那一格是蓝的，用户把主色换成红的它也还是蓝的。
// 而这一条最容易滑回去的是「顺手用主题的 Accent」——那个也是绿的（默认设置
// 下 Biu 的绿），**画面上几乎看不出来**，只有把主色换成别的才现形。
//
// 所以这里两件事一起断：条上是**蓝**，而条上**不是主色**。
func TestTheDownloadBarIsBlueAndNotTheAccent(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusRunning, dlProgress, dlChunk))
	if !shows(tt, "下载中") {
		t.Fatalf("那一格没有状态文字，文字有 %q", tt.Texts())
	}

	if n := statusBarPixels(t, tt, "50%", hexColorOf(t, dlBlue)); n == 0 {
		t.Errorf("状态列里找不到一块 %s 的像素——进度条不是蓝的", dlBlue)
	}
	// 主色是默认设置下 Biu 的绿（#1ed760）。它和蓝差得很远（绿色通道 215 vs
	// 130），所以「条上有主色的像素」就说明条画成了主色。
	//
	// ⚠️ **只量状态列那一段**：整幅画面上还有别的主色（播放条上那条进度就是
	// 主色的），扫全图的话这一条永远是红的。
	if n := statusBarPixels(t, tt, "50%", hexColorOf(t, "#1ed760")); n > 0 {
		t.Errorf("状态列里有 %d 块主色的像素——进度条画成了主色，而上游那一格是蓝的", n)
	}
}

// TestTheFailedBarIsRed 钉住「出错的那一条是红的」。
//
// 上游同一个 `indicator` 的另一支：`data.status === "failed" ? "bg-danger"`。
func TestTheFailedBarIsRed(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusError, dlProgress, dlChunk))
	if !shows(tt, dlErrText) {
		t.Fatalf("那一格没有画出错误原因，文字有 %q", tt.Texts())
	}
	if n := statusBarPixels(t, tt, "50%", hexColorOf(t, "#f31260")); n == 0 {
		t.Errorf("状态列里找不到一块 %s 的像素——出错的那一条不是红的", "#f31260")
	}
}

// TestTheFinishedCellHasAGreenCheck 钉住「完成那一格有一枚绿勾」。
//
// 上游 `progress.tsx:17-25`：
//
//	<div className="text-success flex items-center justify-center space-x-1">
//	  <RiCheckboxCircleLine size={16} />
//	  <span>下载完成</span>
//	</div>
//
// ⚠️ **判据是「文字左边那一块里有成功色的像素」**：那两个字自己也是成功色的，
// 所以只看「有没有绿色」没用——要看**文字左边**那一块（图标在左，字在右）。
func TestTheFinishedCellHasAGreenCheck(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusDone, dlChunk, dlChunk))
	if !shows(tt, "下载完成") {
		t.Fatalf("那一格没有写出「下载完成」，文字有 %q", tt.Texts())
	}
	text, ok := tt.Find("下载完成")
	if !ok {
		t.Fatalf("找不到「下载完成」那几个字，文字有 %q", tt.Texts())
	}

	// 图标在字左边：16 宽 + 4 的缝。
	x0 := max(int(text.X)-dlDoneIconSize-6, 0)
	x1 := max(int(text.X)-2, 1)
	y0 := max(int(text.Y)-6, 0)
	y1 := int(text.Y + text.H + 6)
	n := countPixelsIn(t, tt, x0, y0, x1, y1, hexColorOf(t, "#17c964"))
	if n < 3 {
		t.Errorf("「下载完成」左边只有 %d 块成功色的像素，want ≥3——那枚绿勾不在", n)
	}
}

// statusBarPixels 数「状态」那一列的进度条上，有几块接近 want 的像素。
//
// ⚠️ **范围限在状态列里**（理由同 `progressWidth`）：整幅画面上别的地方也有
// 主色（播放条上那条进度就是主色的），扫全图的话「不是主色」这一条永远是红
// 的，而「是蓝的」会被别处的蓝骗过去。
//
// ⚠️ **条在哪儿，两支不一样**（票 #241 / 报告 F-19 之后）：
//
//   - **下载中**那一支照上游：条整宽占一行，状态文字和百分比在它**下面**一行
//     （`flex justify-between`）——所以条在百分比**上面**。
//   - **出错**那一支：条和百分比**并排**一行，错误原因独占下面一整行
//     （那一格只有 150 宽，错误原因和百分比挤在一行的话，13 个字只剩 6 个，
//     后面几个字被省略号吃掉——见 F-19）——所以条在百分比**左边**。
//
// 两条都扫，所以这个函数对两支都成立。
func statusBarPixels(t *testing.T, tt *ui.Tester, pct string, want color.RGBA) int {
	t.Helper()
	txt, ok := tt.Find(pct)
	if !ok {
		t.Fatalf("那一格没有写出 %q，文字有 %q", pct, tt.Texts())
	}
	// 百分比是那一格里定宽靠右的一段，它的右边缘就是状态列的右边缘；条是
	// 同一格里 FillWidth 的，所以右边缘也是那儿。
	right := int(txt.X + txt.W)
	left := right - dlStatusW

	img := tt.Image()
	n := 0
	count := func(x0, x1, y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := max(x0, 0); x < min(x1, img.Rect.Max.X); x++ {
				if maxDiff(img.RGBAAt(x, y), want) <= 40 {
					n++
				}
			}
		}
	}
	// ① 条在百分比上面（下载中那一支）。
	count(left, right, int(txt.Y)-24, int(txt.Y)-4)
	// ② 条在百分比左边、同一行（出错那一支）。**只到百分比左边**：错误原因在
	// 更下面一行，而它也是危险色——扫到它的话这一条就分不出「条是红的」和
	// 「字是红的」了。
	count(left, int(txt.X)-4, int(txt.Y), int(txt.Y+txt.H))
	return n
}

// countPixelsIn 数那一块矩形里有几块接近 want 的像素。
func countPixelsIn(t *testing.T, tt *ui.Tester, x0, y0, x1, y1 int, want color.RGBA) int {
	t.Helper()
	img := tt.Image()
	if y1 > img.Rect.Max.Y {
		y1 = img.Rect.Max.Y
	}
	n := 0
	for y := max(y0, 0); y < y1; y++ {
		for x := max(x0, 0); x < min(x1, img.Rect.Max.X); x++ {
			if maxDiff(img.RGBAAt(x, y), want) <= 40 {
				n++
			}
		}
	}
	return n
}

// hexColorOf 把 `#rrggbb` 翻成颜色。
func hexColorOf(t *testing.T, s string) color.RGBA {
	t.Helper()
	c := ui.Hex(s)
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}
