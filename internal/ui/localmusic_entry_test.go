package ui

// 这一组盯的是**本地音乐页那两个「添加目录」**：右上角那枚入口**看得见**吗，
// 空状态里**有一枚自己的按钮**吗（报告 C2）。
//
// 报告的原文：空状态正文写着「点右上角『添加目录』选一个文件夹」，而那个
// 「添加目录」在**右上角**、是**一小段灰色文字**、和正文隔着 375 像素——用户
// 按提示找过去，扫到右上角只看见一行小字，**很可能以为那只是说明文字**。

import (
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

// localAccent 是主色（`app.DefaultPrimaryColor`）：画面上那一块**实心**主色。
var localAccent = color.RGBA{R: 0x1e, G: 0xd7, B: 0x60, A: 255}

// findAccentBlock 找出画面上那一块实心主色，返回它的框。
func findAccentBlock(img *image.RGBA) (image.Rectangle, bool) {
	minX, minY, maxX, maxY := -1, -1, -1, -1
	for y := localScanY0; y < localScanY1 && y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y) != localAccent {
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
// 判据是**它左边那一格**：描边按钮在文字左边的内边距上铺的是**自己的底色**，
// 而一段纯文字那一段是**页面底色**。这一条断的就是「12px 纯文字」那个形态。
func TestTheAddDirectoryEntryIsNotAPlainPieceOfText(t *testing.T) {
	tt, _, _, _ := shellOnLocalMusic(t)

	r, ok := tt.Find("添加目录")
	if !ok {
		t.Fatalf("右上角没有「添加目录」那一枚，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	bg := img.RGBAAt(4, 4)
	// 按钮的内边距是 14（`flat.Button` 的 `t.Space(3.5)`），文字左边那一格
	// 就在按钮里面。
	x, y := int(r.X)-7, int(r.Y+r.H/2)
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		t.Fatalf("「添加目录」那个框不在画面上：%+v", r)
	}
	if img.RGBAAt(x, y) == bg {
		t.Errorf("「添加目录」左边那一格还是页面底色——它还是一段纯文字，" +
			"用户看不出那是个能点的入口")
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
