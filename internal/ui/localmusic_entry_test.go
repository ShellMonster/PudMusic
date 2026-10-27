package ui

// 这一组盯的是**本地音乐页那两个「添加目录」**：右上角那枚入口**看得见**吗，
// 空状态里**有一枚自己的按钮**吗（报告 C2）。
//
// 报告的原文：空状态正文写着「点右上角『添加目录』选一个文件夹」，而那个
// 「添加目录」在**右上角**、是**一小段灰色文字**、和正文隔着 375 像素——用户
// 按提示找过去，扫到右上角只看见一行小字，**很可能以为那只是说明文字**。

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"image"
	"image/color"
	"testing"
)

// 扫主色块的那一段 y。
//
// ⚠️ **只扫中间那一段**：顶栏右上角那枚绿方块、播放条上那枚播放键也是实心
// 主色，而它们一个在最上、一个在最下。
const (
	localScanY0 = 200
	localScanY1 = 650
)

// localAccent 是当前主题的主色：画面上那一块**实心**主色。
//
// ⚠️ **取主题里的，不写死色值**：默认主题换过配色，写死的字面色一换就找不到那一块。
func localAccent() color.RGBA {
	return rgbaOf(BuildTheme(app.DefaultSettings(""), false).Accent)
}

// findAccentBlock 找出画面上那一块实心主色，返回它的框。
func findAccentBlock(img *image.RGBA) (image.Rectangle, bool) {
	accent := localAccent()
	minX, minY, maxX, maxY := -1, -1, -1, -1
	for y := localScanY0; y < localScanY1 && y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y) != accent {
				continue
			}
			if minX < 0 {
				minX, minY = x, y
			}
			maxX, maxY = max(x, maxX), max(y, maxY)
		}
	}
	if minX < 0 {
		return image.Rectangle{}, false
	}
	return image.Rectangle{Min: image.Pt(minX, minY), Max: image.Pt(maxX+1, maxY+1)}, true
}

// TestTheAddDirectoryEntryIsNotAPlainPieceOfText 断右上角那个「添加目录」
// **看得见它是能点的**。
//
// 判据是**它左边那一段有图形**：上游那一枚是「图标 + 文字」的 flat 键
// （`local-music/index.tsx:189`），而图标一直没画——于是在画面上它就是**一行
// 小字**，而空状态那句话让用户去点它：用户按提示找过去，扫到右上角只看见
// 一行说明文字，**很可能以为那只是说明文字**（报告 C2）。
//
// ⚠️ **断的是「有图形」而不是「有底色」**：全应用的次级按钮都是扁平的（无描边、
// 无常驻底，见 flatbutton.go 的文件头），`flatspread_test.go` 逐页钉着这一条。
// 「看得见」靠的是那枚图标和空状态里那枚实心主色按钮，不是给它裹一圈边。
func TestTheAddDirectoryEntryIsNotAPlainPieceOfText(t *testing.T) {
	tt, _, _, _ := shellOnLocalMusic(t)

	r, ok := tt.Find("添加目录")
	if !ok {
		t.Fatalf("右上角没有「添加目录」那一枚，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	bg := img.RGBAAt(4, 4)
	// 文字左边那一段：图标就在那儿（14 的图标 + 6 的缝）。
	x0, y0 := int(r.X)-24, int(r.Y)-2
	strokes := 0
	for y := y0; y < y0+int(r.H)+4 && y < img.Bounds().Dy(); y++ {
		for x := x0; x < x0+18 && x < img.Bounds().Dx(); x++ {
			if x < 0 || y < 0 {
				continue
			}
			if maxDiff(img.RGBAAt(x, y), bg) > probeTolerance {
				strokes++
			}
		}
	}
	if strokes < 5 {
		t.Errorf("「添加目录」左边那一段没有图形（只有 %d 个非底色像素）——它还是"+
			"一段纯文字，用户看不出那是个能点的入口", strokes)
	}
}

// TestTheEmptyStateHasItsOwnAddDirectoryButton 断空状态里**有一枚自己的
// 按钮**。
//
// 空状态那句话让用户去点右上角那个入口，而那个入口离它三百多像素——所以
// **同一个词、同一个动作**要在这句话下面再摆一枚（报告 C2 的建议动作）。
//
// ⚠️ **判据是画面上那一块实心主色**：那是那枚按钮（它是这一页此刻唯一能做的
// 事，所以它是主按钮）。找它是为了两件事：它在（=用户看得见），点它有用。
func TestTheEmptyStateHasItsOwnAddDirectoryButton(t *testing.T) {
	tt, _, _, pick := shellOnLocalMusic(t)

	block, ok := findAccentBlock(tt.Image())
	if !ok {
		t.Fatalf("空状态那一格上没有实心主色的按钮，文字有 %q", tt.Texts())
	}
	// 它在那一格的中间（不是页头那一枚）——页头那一枚是扁平的，不是实心块。
	if block.Min.Y < localScanY0 {
		t.Fatalf("那块主色不在内容区中间：%+v", block)
	}
	// 正文不再让人「去右上角找」——那里离得太远，指过去等于没指。
	if tt.HasText("点右上角") {
		t.Errorf("空状态还在让用户去右上角找那一枚，文字有 %q", tt.Texts())
	}

	// 点它：系统框得被叫起来（桩记着调了几次）。
	pick.set(t.TempDir())
	tt.ClickAt(float32(block.Min.X+block.Dx()/2), float32(block.Min.Y+block.Dy()/2))
	waitFor(t, "系统框被叫起来", func() bool { return pick.calls > 0 })
}
