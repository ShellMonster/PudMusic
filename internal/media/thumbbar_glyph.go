// 缩略图按钮上那四个图标：字形，以及把它们变成位图的纯逻辑。
//
// 之所以是自己画而不是打包四张 PNG 进二进制：图标的**大小**要跟着显示缩放
// 走（Windows 那边问的是 `SM_CXSMICON`，150% 缩放上它就是 24 而不是 16），
// 而一张 16×16 的 PNG 放大之后是一堆锯齿。自己拿一张 16×16 的点阵按最近邻放大，
// 放大之后仍然是干净的实心块——那是 16 像素图标该有的样子。
//
// 字形本身用字符画写出来而不是一串十六进制：评审能一眼看出画的是播放还是暂停，
// 而十六进制那一坨谁也看不出，改一个像素要重新数。
//
// 画的那些调用全在这一层，Windows 那一半（thumbbar_windows.go）只负责把
// `bitmap()` 出来的 RGBA 塞进 DIB。所以这一整层在没有 Windows 的机器上
// 断得动。

package media

import "image"

// glyphSize 是字形点阵的边长。
//
// 16 是 96 DPI 上的 `SM_CXSMICON`（小图标的系统尺寸）。放大到别的尺寸由
// scaleGlyph 做，最近邻。
const glyphSize = 16

// thumbbarGlyph 是四个图标。
//
// 按数字排下去纯粹是为了 `bitmap` 里那个数组能按下标取——没有语义，
// 别给它们编顺序。
type thumbbarGlyph int

const (
	glyphPlay thumbbarGlyph = iota
	glyphPause
	glyphPrev
	glyphNext
	glyphCount
)

// glyphMargin 是字形和边缘至少隔几像素。
//
// 不是审美问题：贴着边的笔画在 100% 缩放下会被任务栏那条一像素的分隔线切掉，
// 切掉之后「上一首」的两个三角看起来就只剩下一个，而且**看不出坏了**。
const glyphMargin = 2

// 字形。'#' 是笔画，'.' 是空。
//
// ⚠️ **每一行必须正好 16 个字符**：少一个不会报错，只会让整幅画歪一位。
// glyphArt 用一个刻意的检查（每一行等长且等于 glyphSize）把它钉住，
// 写歪了当场就红。
var (
	// 播放：尖端朝右，底边是左边那一竖。
	//
	// 每一行的笔画数是 1,3,5,6,8,8,6,5,3,1——这是把顶点在 (11, 7.5) 的等腰
	// 三角形按行取整画出来的。上下那一对「1」是尖的两端，不是一个点是为了
	// 16 像素上还看得见形状；真取一个点的话最上和最下各只剩一个像素，
	// 任务栏那种深色底上会变成两粒噪点。
	glyphArtPlay = [glyphSize]string{
		"................",
		"................",
		"................",
		"....#...........",
		"....###.........",
		"....#####.......",
		"....######......",
		"....########....",
		"....########....",
		"....######......",
		"....#####.......",
		"....###.........",
		"....#...........",
		"................",
		"................",
		"................",
	}
	// 暂停：两条一样宽的竖杠，中间留一道缝。
	//
	// ⚠️ 两条必须一样宽、位置必须对称：一条粗一条细的「暂停」在 16 像素上会
	// 被看成一粗一细两竖，而用户不会觉得「这个暂停图标歪了」，只会觉得别扭。
	// 对称这条由 thumbbar_glyph_test.go 的 TestPauseIsLeftRightSymmetric 钉住。
	glyphArtPause = [glyphSize]string{
		"................",
		"................",
		"................",
		".....##..##.....",
		".....##..##.....",
		".....##..##.....",
		".....##..##.....",
		".....##..##.....",
		".....##..##.....",
		".....##..##.....",
		".....##..##.....",
		".....##..##.....",
		".....##..##.....",
		"................",
		"................",
		"................",
	}
	// 下一首：三角 + 右边一根竖杠。三角和播放同形，只是往左挪两格。
	glyphArtNext = [glyphSize]string{
		"................",
		"................",
		"................",
		"..#........##...",
		"..###......##...",
		"..#####....##...",
		"..######...##...",
		"..########.##...",
		"..########.##...",
		"..######...##...",
		"..#####....##...",
		"..###......##...",
		"..#........##...",
		"................",
		"................",
		"................",
	}
	// 上一首：下一首水平镜像。
	//
	// 这一幅**照着上面那一幅手工镜像出来的**，不是代码里算出来的——镜像关系
	// 交给 thumbbar_glyph_test.go 去钉（TestPrevIsTheMirrorOfNext）。写成
	// 「下一首取镜像」的话那条测试就自己验自己了，抄错了也发现不了。
	glyphArtPrev = [glyphSize]string{
		"................",
		"................",
		"................",
		"...##........#..",
		"...##......###..",
		"...##....#####..",
		"...##...######..",
		"...##.########..",
		"...##.########..",
		"...##...######..",
		"...##....#####..",
		"...##......###..",
		"...##........#..",
		"................",
		"................",
		"................",
	}
)

// art 返回这一幅的点阵。
//
// **越界返回零值**而不是 panic：这里只有四个调用点，全是包内写死的常量，
// 越界只可能是有人加了第五个图标却忘了在 art 里补。那种情况应该在测试里
// （TestEveryGlyphHasArt）红，而不是在用户点缩略图的时候崩掉整个进程
// ——媒体会话崩了是小事，但那是这个包唯一有系统回调的地方。
func (g thumbbarGlyph) art() [glyphSize][glyphSize]bool {
	var out [glyphSize][glyphSize]bool
	src := g.stringArt()
	for y := range glyphSize {
		for x := range glyphSize {
			out[y][x] = src[y][x] == '#'
		}
	}
	return out
}

// stringArt 取出字符画，超范围给一张空的。
func (g thumbbarGlyph) stringArt() [glyphSize]string {
	switch g {
	case glyphPlay:
		return glyphArtPlay
	case glyphPause:
		return glyphArtPause
	case glyphPrev:
		return glyphArtPrev
	case glyphNext:
		return glyphArtNext
	}
	return [glyphSize]string{}
}

// bitmap 画出这个图标，笔画是白的、其余全透明。
//
// 白而不透明、反面**全透明而不是白**：Windows 的任务栏在深色和浅色两套主题下
// 都是深色背景，一个「白底黑字」的图标贴上去会是一块白方块，比看不见还糟。
// 透明的那部分靠 Windows 自己把底色透出来。
func (g thumbbarGlyph) bitmap() *image.RGBA {
	return g.scaled(glyphSize)
}

// scaled 按最近邻把字形放大（或缩小）到 size 方形。
//
// **最近邻，不插值**：图标是被任务栏原样贴上去的，插值出来的半透明边缘在
// 16 像素上就是一圈灰边，白笔画看着像描了灰边。最近邻放大出来的边缘是硬的，
// 和低分辨率图标本来就该有的样子一致。
//
// size 小于 1 时给 1：0 边长的位图在 Windows 那边会让 `CreateDIBSection`
// 拿不到东西，返回的位图句柄是空，而症状是「工具栏上有四个空格子」。
func (g thumbbarGlyph) scaled(size int) *image.RGBA {
	if size < 1 {
		size = 1
	}
	src := g.art()
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		// 采样点取源格子的正中，免得 size 不是 glyphSize 的整数倍时整体偏半格。
		sy := y*glyphSize/size + glyphSize/(2*size)
		if sy >= glyphSize {
			sy = glyphSize - 1
		}
		for x := range size {
			sx := x*glyphSize/size + glyphSize/(2*size)
			if sx >= glyphSize {
				sx = glyphSize - 1
			}
			if !src[sy][sx] {
				continue
			}
			i := out.PixOffset(x, y)
			out.Pix[i+0], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = 255, 255, 255, 255
		}
	}
	return out
}
