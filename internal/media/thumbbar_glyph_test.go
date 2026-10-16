package media

import (
	"image/color"
	"testing"
)

// allGlyphs 是四个字形，按 thumbbar_glyph 的顺序。
func allGlyphs() []thumbbarGlyph {
	return []thumbbarGlyph{glyphPlay, glyphPause, glyphPrev, glyphNext}
}

// TestEveryGlyphHasArt 断四个图标都画了。
//
// 字形是字符画，加一个图标忘了在 `art()` 里补一句，得到的是一张全空的白纸。
// 全空不报错：Windows 那边的位图照样建得出来，工具栏上就是四个看不见的格子。
func TestEveryGlyphHasArt(t *testing.T) {
	for _, g := range allGlyphs() {
		if g == glyphCount {
			t.Fatalf("allGlyphs 里混进了哨兵 glyphCount")
		}
		ink := 0
		for _, row := range g.art() {
			for _, on := range row {
				if on {
					ink++
				}
			}
		}
		if ink == 0 {
			t.Errorf("字形 %d 一个笔画都没有", g)
		}
	}
}

// TestTheGlyphTablesAreSquare 钉住「每行正好 16 个字符」。
//
// 这是字符画唯一的写法约束，而它**错了不会报错**：少写一个 '#' 只是图歪一位，
// 多写一个则被下面那个检查拦下来。手工画的四个图标里，Next 和 Prev 每一行
// 都踩在 16 这个数上，抄的时候很容易少一个点。
func TestTheGlyphTablesAreSquare(t *testing.T) {
	for _, g := range allGlyphs() {
		for y, row := range g.stringArt() {
			if len([]rune(row)) != glyphSize {
				t.Errorf("字形 %d 第 %d 行有 %d 个字符，want %d：%q",
					g, y, len([]rune(row)), glyphSize, row)
			}
		}
	}
}

// TestEveryGlyphKeepsItsMargin 断笔画不贴边。
//
// 贴边的笔画会被任务栏那条一像素分隔线切掉，而切掉之后**看不出坏了**——
// 「上一首」的两个三角看起来就只剩下一个，用户只会觉得这个图标有点怪。
func TestEveryGlyphKeepsItsMargin(t *testing.T) {
	for _, g := range allGlyphs() {
		art := g.art()
		for y := range glyphSize {
			for x := range glyphSize {
				if !art[y][x] {
					continue
				}
				if x < glyphMargin || y < glyphMargin ||
					x >= glyphSize-glyphMargin || y >= glyphSize-glyphMargin {
					t.Errorf("字形 %d 的笔画碰到了边：(%d,%d)", g, x, y)
				}
			}
		}
	}
}

// TestEveryGlyphIsSymmetricTopToBottom 断上下对称。
//
// 四个图标都该上下对称（三角居中、两条竖杠一样长）。不对称的一幅画出来是
// 「有点歪」，而没有人会去查是哪一行少画了一个点。
func TestEveryGlyphIsSymmetricTopToBottom(t *testing.T) {
	for _, g := range allGlyphs() {
		art := g.art()
		for y := range glyphSize {
			for x := range glyphSize {
				if art[y][x] != art[glyphSize-1-y][x] {
					t.Errorf("字形 %d 上下不对称：(%d,%d) 与 (%d,%d) 不同",
						g, x, y, x, glyphSize-1-y)
					break
				}
			}
		}
	}
}

// TestPauseIsLeftRightSymmetric 断「暂停」左右对称。
//
// 这一条只对暂停有意义：两条竖杠不一样宽的话，16 像素上会看成一粗一细两竖。
// 用户不会说「这个暂停图标歪了」，只会觉得别扭，所以得在这里钉死。
func TestPauseIsLeftRightSymmetric(t *testing.T) {
	art := glyphPause.art()
	for y := range glyphSize {
		for x := range glyphSize {
			if art[y][x] != art[y][glyphSize-1-x] {
				t.Errorf("暂停第 %d 行左右不对称：第 %d 个和第 %d 个不同",
					y, x, glyphSize-1-x)
			}
		}
	}
}

// TestPrevIsTheMirrorOfNext 断「上一首」就是「下一首」水平镜像。
//
// 这一对是**手工画的两幅**（见 thumbbar_glyph.go 里为什么不用代码算），
// 所以这条测试是有牙齿的：某一幅抄错一位，这条立刻红。
//
// 画错之后的症状是「上一首和下一首的图标长得几乎一样」，而那正好是大多数
// 播放器里这两个键的真实情况——所以这个错没人会发现。
func TestPrevIsTheMirrorOfNext(t *testing.T) {
	next, prev := glyphNext.art(), glyphPrev.art()
	for y := range glyphSize {
		for x := range glyphSize {
			if next[y][x] != prev[y][glyphSize-1-x] {
				t.Errorf("上一首不是下一首的镜像：(%d,%d) 与镜像后的 (%d,%d) 不同",
					x, y, glyphSize-1-x, y)
			}
		}
	}
}

// TestPlayAndPauseAreNotTheSameShape 断播放三角和暂停两竖不是同一幅。
//
// 两个图标一样时，播放中/暂停中的切换**看不出来**，于是「按暂停没反应」和
// 「按暂停变成了别的东西」分不开，日志里也只有一个名字。
func TestPlayAndPauseAreNotTheSameShape(t *testing.T) {
	if glyphPlay.art() == glyphPause.art() {
		t.Error("播放和暂停画成了同一幅")
	}
}

// TestPrevAndNextAreNotTheSameShape 是上一条的一对。
func TestPrevAndNextAreNotTheSameShape(t *testing.T) {
	if glyphPrev.art() == glyphNext.art() {
		t.Error("上一首和下一首画成了同一幅")
	}
}

// TestPlayPointsRight 断播放三角真的是「底边在左、尖在右」。
//
// 尖朝左的话它看起来像「上一首」，而中间那个按钮在放的时候画的是它——
// 于是「播放/暂停」那一格放着一个「上一首」。这个错**不会有人报出来**：按那个
// 按钮发出去的确实是 CommandPause，歌也确实停了，只有图标不对劲。
func TestPlayPointsRight(t *testing.T) {
	art := glyphPlay.art()
	top, bottom := glyphSize, -1
	for y := range glyphSize {
		for x := range glyphSize {
			if art[y][x] {
				top, bottom = min(top, y), max(bottom, y)
			}
		}
	}
	if bottom < 0 {
		t.Fatal("播放一个笔画都没有")
	}

	// 底边是左边那一竖：在**有笔画的那几行**里，最左边那一列不能断。
	base := -1
	for x := range glyphSize {
		full := true
		for y := top; y <= bottom; y++ {
			if !art[y][x] {
				full = false
				break
			}
		}
		if full {
			base = x
			break
		}
	}
	if base < 0 {
		t.Fatal("播放的左边没有一整列竖笔画，底边不在")
	}

	// 尖在右边且在竖直中线上：最右边那一列只在中间那两行有笔画。
	tip := -1
	for x := glyphSize - 1; x > base; x-- {
		rows := 0
		for y := range glyphSize {
			if art[y][x] {
				rows++
			}
		}
		if rows > 0 {
			if rows > 2 {
				t.Errorf("最右边那一列（x=%d）有 %d 行笔画，尖应该只在中间两行", x, rows)
			}
			tip = x
			break
		}
	}
	if tip < 0 {
		t.Fatal("播放的底边右边一个笔画都没有")
	}
	// 尖不能贴边：贴边的笔画会被任务栏那条一像素分隔线切掉。
	// （TestEveryGlyphKeepsItsMargin 已经全局断过一次，这里只把「尖」再说一遍：
	// 全局那条断的是所有笔画，尖是最靠右的那一个。）
	if tip > glyphSize-1-glyphMargin {
		t.Errorf("尖到了 x=%d，贴边了（右边只留 %d 像素）", tip, glyphSize-1-tip)
	}
}

// TestBitmapIsWhiteInkOnTransparent 断位图是「白笔画 + 全透明底」。
//
// 反过来（黑笔画 / 白底）在 Windows 上是一块白方块：任务栏深色主题下白方块
// 比看不见还糟，而位图本身是**完全合法**的——Windows 那边照收不误。
func TestBitmapIsWhiteInkOnTransparent(t *testing.T) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for _, g := range allGlyphs() {
		art := g.art()
		bmp := g.bitmap()
		if got := bmp.Bounds().Dx(); got != glyphSize {
			t.Errorf("字形 %d 的位图宽 %d，want %d", g, got, glyphSize)
		}
		for y := range glyphSize {
			for x := range glyphSize {
				got := bmp.RGBAAt(x, y)
				if !art[y][x] {
					if got != (color.RGBA{}) {
						t.Errorf("字形 %d 的 (%d,%d) 是空白处却是 %v", g, x, y, got)
					}
					continue
				}
				if got != white {
					t.Errorf("字形 %d 的笔画 (%d,%d) 是 %v，want 白色", g, x, y, got)
				}
			}
		}
	}
}

// TestScaledIsNearestNeighbour 断放大之后**不出现半透明的新像素**。
//
// 这条盯的是「最近邻而不是插值」：插值出来的边缘是灰的，16 像素的白三角
// 放大到 24 之后看着像描了一圈灰边。判据是：放大之后，原图里**每一个有笔画
// 的格子**都必须对应放大结果里的每一个像素都有笔画——反过来说，一个「从空白
// 长出来的笔画」就是插值插出来的。
func TestScaledIsNearestNeighbour(t *testing.T) {
	const size = 24
	for _, g := range allGlyphs() {
		src := g.art()
		dst := g.scaled(size)
		for y := range size {
			for x := range size {
				sy := y * glyphSize / size
				sx := x * glyphSize / size
				_, _, _, a := dst.At(x, y).RGBA()
				gotInk := a != 0
				wantInk := src[sy][sx]
				if gotInk != wantInk {
					t.Errorf("字形 %d 放大到 %d：(%d,%d) 取源 (%d,%d) 有笔画=%v，放大结果有笔画=%v",
						g, size, x, y, sx, sy, wantInk, gotInk)
				}
			}
		}
	}
}

// TestScaledIsSquareAtAnySize 断任意尺寸都是正方形，包括 0 和 1。
//
// 0 边长的位图在 Windows 那边建不出 DIB，症状是「工具栏上有几个空格子」——
// 而拿到 SM_CXSMICON 的那个调用在某些缩放下确实可能给 0（拿不到窗口时的
// 兜底路径）。所以这里把 0 明确夹到 1。
func TestScaledIsSquareAtAnySize(t *testing.T) {
	for _, size := range []int{-3, 0, 1, 7, 16, 23, 64} {
		bmp := glyphPlay.scaled(size)
		want := max(size, 1)
		if bmp.Bounds().Dx() != want || bmp.Bounds().Dy() != want {
			t.Errorf("scaled(%d) 是 %v，want %d×%d", size, bmp.Bounds(), want, want)
		}
	}
}
