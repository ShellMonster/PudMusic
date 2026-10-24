// 「图标键按下时缩到 96%」那一档在**播放键**和**歌词区那三枚旋钮**上的测试。
//
// # 为什么这两处要单独断
//
// 按压缩放那一档的配方住在 `internal/ui/widgets/flat`（`press.go`），而
// `flat` 那一族（`IconButton` / `IconButtonAccent` / `IconCellButton` /
// `IconLabelButton`）自己就带上了 ✓。但**播放/暂停那一枚**和**歌词区右下角
// 那三枚**不是 `flat` 按钮——它们直接是 `ui.ButtonBase` + `ui.Icon`（各自的
// 形状自己定：播放键是 48×48 的实心圆、旋钮是 ⌀32 的圆钮）——所以那两处
// **要自己接上**。
//
// ⚠️ **而文档把它们当图标键列着**（DESIGN.md §六「图标键」：播放键 48×48、
// 歌词区那三枚 16）——文档说的比代码做的多，就是这一族存在的理由。
//
// # 判据是画面上的像素，不是元素框
//
// ⚠️ **`Scale` 不改布局**：元素框量不出缩放（理由见 `widgets/flat/press.go`）。
// 而 `Tester.Find` 给的是**变换后**的盒子——但那只是**按钮**的盒子（缩的是
// **图形**，按钮盒子不动 ✗）。所以这里量**画面上的像素**。
package ui

import (
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// glyphArea 是画面上**图形那一片**的面积（像素个数）。
//
// ⚠️ **量的是「接近那个颜色的像素」**：图形是抗锯齿的，边缘那些像素是图形色和
// 底色的混合——所以这里按「够接近」算（差 ≤ 24）。
//
// ⚠️ **按下去之后底色会变**（播放键的圆底换成 `AccentPressed`）——所以两次量
// 的是**同一个颜色**（图形色 ✓），底色变了也不影响 ✓（只是边缘那几个混合像素
// 会跟着变，所以判据留了余量 ✗）。
func glyphArea(t *testing.T, tt *mygoui.Tester, r mygoui.Rect, c [3]int) int {
	t.Helper()
	img := tt.Image()
	n := 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := img.RGBAAt(x, y)
			if absInt(int(p.R)-c[0]) <= 24 && absInt(int(p.G)-c[1]) <= 24 && absInt(int(p.B)-c[2]) <= 24 {
				n++
			}
		}
	}
	return n
}

// TestThePlayKeyShrinksWhenPressed 钉住「播放/暂停那一枚按下时缩到 96%」。
//
// ⚠️ **量的是面积，不是边长**：缩放是**线性**的，边长少 4%、面积少 8%
// （0.96² = 0.92）——像素数是整数，边长那一两像素的差量不出来 ✗。
//
// ⚠️ **判据留了余量**：按住的时候圆底会换成 `AccentPressed`（更深的绿），而
// 图形的边缘像素是「图形色和底色的混合」——底色一变，那几个混合像素也跟着变
// （实测：不带缩放时那一档也会少 6 个像素 ≈ 1.7%）。所以判据是「少了 ≥ 5%」
// （实测：带缩放时少 7.2%）——两个数之间差得开 ✓。
//
// ⚠️ **要推钟**：缩放是 120ms 的一段，而 Tester 的钟站着不动——不推的话量到的
// 是「还没开始缩」的那一帧。
func TestThePlayKeyShrinksWhenPressed(t *testing.T) {
	st := barState()
	st.Playing = true
	tt, s := playbarTester(t, st, 1200, 0, false)
	// 指针挪到播放条之外：悬停会改圆底的颜色。
	tt.Move(600, 300)
	tt.Frame()

	play, ok := tt.Find("暂停")
	if !ok {
		t.Fatalf("画面上找不到「暂停」，文字有 %q", tt.Texts())
	}
	th := BuildTheme(s.app.SettingsSnapshot(), false)
	on := rgb(th.AccentText)
	before := glyphArea(t, tt, play, [3]int{int(on.R), int(on.G), int(on.B)})
	if before == 0 {
		t.Fatalf("播放键的圆里没有画出图形（找不到 AccentText 色 %v）", on)
	}

	// 按住那一枚（**不松手**——`Pressed()` 为真）。
	tt.Press(play.X+play.W/2, play.Y+play.H/2)
	tt.Advance(200 * time.Millisecond)
	tt.Frame()

	after := glyphArea(t, tt, play, [3]int{int(on.R), int(on.G), int(on.B)})
	if float64(after) > float64(before)*0.95 {
		t.Errorf("按住之后图形那一片是 %d 像素，按住之前是 %d——它该缩到 96%%（面积少 8%%）",
			after, before)
	}
}

// TestTheLyricKnobShrinksWhenPressed 钉住「歌词区右下角那三枚旋钮按下时缩到 96%」。
//
// ⚠️ **比的是「悬停着的」和「悬停着又按着的」两帧**：旋钮的**底盘**在悬停时会变
// （20% → 30%），所以要先把指针停在钮上再比——那样底盘是同一种颜色 ✓，两帧差的
// 就**只有图形**（它缩了 4%）。
//
// ⚠️ **不比像素数，比「有几处不一样」**：这一枚的图形是**盖在封面上的**
// （旋钮的底盘是前景色的 20%，封面透上来 ✗），所以「图形那一片有多少像素」
// 量不准（封面上亮的地方也在里面）。而**两帧之间变了哪几个像素**是干净的
// ——按住与不按住的唯一区别就是图形缩了 4% ✓。
//
// ⚠️ **要推钟**：同上一条——缩放是 120ms 的一段。
func TestTheLyricKnobShrinksWhenPressed(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	knob, ok := tt.Find(lyricFontKnobName)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", lyricFontKnobName, tt.Texts())
	}

	// 指针落在那一枚上（悬停：底盘变深一点），但**不按**。
	tt.Move(knob.X+knob.W/2, knob.Y+knob.H/2)
	tt.Frame()
	before := tt.Image()

	// 按住：图形缩到 96%。
	tt.Press(knob.X+knob.W/2, knob.Y+knob.H/2)
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	after := tt.Image()

	changed := 0
	for y := int(knob.Y); y < int(knob.Y+knob.H); y++ {
		for x := int(knob.X); x < int(knob.X+knob.W); x++ {
			if before.RGBAAt(x, y) != after.RGBAAt(x, y) {
				changed++
			}
		}
	}
	if changed == 0 {
		t.Errorf("按住 %q 之后画面上一个像素都没变——它该缩到 96%%（图形那一圈要变）", lyricFontKnobName)
	}
}
