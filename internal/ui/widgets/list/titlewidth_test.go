package list

// 这一组钉住「超长歌名不占满整行中段」（票 #244 / UX 报告 §2 的 E1）。
//
// # 为什么歌名要有一个上限
//
// 歌名那一列是 `Grow(1)`——它吃掉了整行剩下的宽，而**字没有上限**的话，一个
// 630 像素的歌名会一路长到离右边那几列只差 31 像素（实测：歌名 x=337..969，
// 播放量 x=1000）。于是省略号要等歌名长到 700 多像素才出现，而「一行一首歌」
// 那个紧凑的信息单元被拉散了一整屏。
//
// # 判据是**画面上的墨迹**
//
// ⚠️ **不能用控件的盒子量**：歌名那个文本元素的盒子是**整行**的（`Find` 认的是
// 行那一块的标签），量不到字本身有多宽。
//
// ⚠️ **量的是墨迹的最右沿**，不是「有没有省略号」：省略号是最后一个字形，
// 抗锯齿的边缘落在哪一格说不准；而墨迹的右沿是稳定的（字画到哪儿就到哪儿）。

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// titleInkLimit 是扫描的上界：右边那几列（时长 / 行尾那枚「⋯」）之前。
//
// ⚠️ **必须限住**：行尾那枚「⋯」也是深色的墨迹，扫过去量到的就是它了。
func titleInkLimit(rowW float32) float32 {
	return rowW - cardPadX - OperationColWidth - rowGap - durationWidth - rowGap
}

// TestALongTitleDoesNotFillTheWholeRow 钉住「超长歌名不占满整行中段」。
//
// # 三样都要成立
//
//   - **列表要够宽**：窄窗口下歌名那一列本来就窄于上限，那时候截断是**列**的
//     功劳（而那一档本来就该截断），量不出这一条在管什么；
//   - **墨迹的右沿离歌名列的左沿不超过上限**；
//   - **而且它离那一列的右沿还远着**——否则说明是列把它截断的（那是窄窗口的
//     那一档，不是这一条要的）。
func TestALongTitleDoesNotFillTheWholeRow(t *testing.T) {
	const rowW = 1200
	long := strings.Repeat("很长很长的歌名", 40)

	l := New(newCache(t), Actions{})
	l.SetSongs([]Music{song("a", long, "")})
	tt := ui.NewTester(l.View, rowW, 200)
	tt.Frame()

	// 歌名列的左沿：内边距 + 序号列 + 缝 + 封面 + 缝。
	left := float32(cardPadX + indexWidth + rowGap + coverSize + rowGap)
	// 那一列的右沿（= 右边那几列之前）。
	limit := titleInkLimit(rowW)
	if limit-left <= listTitleMaxW {
		t.Fatalf("这一档的歌名列只有 %.0f 宽（上限 %.0f）——放不下一个长歌名，"+
			"量不出这一条在管什么", limit-left, float32(listTitleMaxW))
	}

	right := titleInkRight(tt, left, limit)
	got := right - left
	if got > listTitleMaxW {
		t.Errorf("超长歌名的墨迹有 %.0f 宽（上限 %.0f）——它一路顶到了右边那几列跟前，"+
			"整行中段被它占满了", got, float32(listTitleMaxW))
	}
	if right >= limit {
		t.Errorf("超长歌名的墨迹一直画到 x=%.0f（那一列到 %.0f）——它是**列**把它"+
			"截断的，不是上限在管这件事", right, limit)
	}
}

// titleInkRight 量歌名那一行里**墨迹的最右沿**。
//
// ⚠️ **量的是那一行字的那一条带**（行内边距往上开始，字高那么高）：行里还有
// 别的墨迹（行尾那枚「⋯」在垂直方向上居中，落在这一条带之外）。
//
// ⚠️ **判据是「明显比底色深」**：歌名是正文色（浅色主题下接近黑），底色是页面
// 底色（接近白）。阈值 200 把抗锯齿的边缘算在「不是墨迹」那一侧。
func titleInkRight(tt *ui.Tester, left, limit float32) float32 {
	img := tt.Image()
	y0 := cardPadY
	y1 := cardPadY + 22 // 一行 16 号字连它的行高
	right := left
	for x := int(left); float32(x) < limit; x++ {
		for y := int(y0); y < int(y1); y++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 < 200 && g>>8 < 200 && b>>8 < 200 {
				right = float32(x)
				break
			}
		}
	}
	return right
}

// TestAShortTitleIsNotAffected 钉住「短歌名一个字都不多让」。
//
// ⚠️ **上限不许反过来咬短歌名**：短歌名照旧占它自己的宽（那一列仍然吃满剩下
// 的宽，字在那一格里左对齐）。
func TestAShortTitleIsNotAffected(t *testing.T) {
	const rowW = 1200
	l := New(newCache(t), Actions{})
	l.SetSongs([]Music{song("a", "短歌名", "")})
	tt := ui.NewTester(l.View, rowW, 200)
	tt.Frame()

	left := float32(cardPadX + indexWidth + rowGap + coverSize + rowGap)
	right := titleInkRight(tt, left, titleInkLimit(rowW))
	if got := right - left; got > 120 {
		t.Errorf("短歌名的墨迹有 %.0f 宽——上限（%.0f）不该套到短歌名上",
			got, float32(listTitleMaxW))
	}
}
