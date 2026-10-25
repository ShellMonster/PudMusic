// 满屏播放器右下角那三枚歌词旋钮（票 #166，报告 S-19 + S-20 + S-22）。
//
// 这一族断的是**三件事**：
//
//	那三枚旋钮   满屏这一屏真的有，而且真的能用（字号 / 偏移 / 找歌词）
//	位置         它们挂在**歌词区**的右下角（右沿 1128、底沿 680 —— 上游那两个数）
//	歌词靠左     每一句的左沿咬在内容盒的左沿上（648），不是逐行居中
//
// 出图在 `fullplayer_lyrics_test.go` 里（深浅两套）。
package ui

import (
	"image"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheFullPlayerHasTheThreeLyricKnobs 断满屏那一屏**真的有**那三枚旋钮。
//
// ⚠️ **这一条是这一票的核心**（报告 S-19，P0）：上游满屏播放器的歌词视图右下角
// 竖排三枚圆形旋钮（`components/lyrics/index.tsx:380-398`，满屏那一处是
// `full-screen-player/index.tsx:361` 的 `<Lyrics showControls>`），而我们满屏这一
// 屏一枚都没有——那三枚只画在主窗口「正在播放」那一页。
//
// 判据是「它们在画面上」：三枚（字号 / 偏移 / 找歌词）都在。
func TestTheFullPlayerHasTheThreeLyricKnobs(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	for _, want := range []string{lyricFontKnobName, lyricOffsetKnobName, lyricSearchKnobLabel} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("满屏那一屏没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

// TestTheFullPlayerKnobsSitAtTheBottomRightOfTheLyrics 断那三枚**挂在歌词区的
// 右下角**，位置和上游那两个数一样。
//
// ⚠️ **这两个数是上游的**（报告 S-19 实测，上游图 `15-满屏播放器-浅色.png`）：
//
//	右沿 1128   = 歌词内容盒的右沿（1152）− `right-6` 的 24
//	底沿 680    = 歌词内容盒的底沿（704）− `bottom-6` 的 24
//
// ⚠️ **不能拿「离窗口右边 24」当判据**：窗口右边是 1200，那是**歌词区**的边，
// 不是**歌词内容盒**的边——差那一份 48 的左右留白（上游 `px-12`）。
//
// ⚠️ **写死在这儿**：布局和期望都用生产代码那个常量的话，常量一改两边一起变，
// 测试就发现不了了。
func TestTheFullPlayerKnobsSitAtTheBottomRightOfTheLyrics(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	col, ok := tt.Find(lyricKnobColumnName)
	if !ok {
		t.Fatalf("画面上找不到那一列旋钮，文字有 %q", tt.Texts())
	}
	const wantRight, wantBottom = 1128, 680
	if d := absF(col.X + col.W - wantRight); d > 1 {
		t.Errorf("那一列旋钮的右沿在 %.1f，want %d（上游：内容盒右沿 1152 − 24）",
			col.X+col.W, wantRight)
	}
	if d := absF(col.Y + col.H - wantBottom); d > 1 {
		t.Errorf("那一列旋钮的底沿在 %.1f，want %d（上游：内容盒底沿 704 − 24）",
			col.Y+col.H, wantBottom)
	}
	// 竖着那一串：三枚各 32 高、中间 12 的缝（上游的 `space-y-3`），于是顶沿
	// 560、中心间距 44——和上游那三枚圆钮一样。
	const wantTop, wantStep = 560, 44
	if d := absF(col.Y - wantTop); d > 1 {
		t.Errorf("那一列旋钮的顶沿在 %.1f，want %d（上游：三枚 ⌀32 + 两道 12 的缝）",
			col.Y, wantTop)
	}
	font, ok := tt.Find(lyricFontKnobName)
	if !ok {
		t.Fatalf("画面上找不到字号旋钮，文字有 %q", tt.Texts())
	}
	offset, ok := tt.Find(lyricOffsetKnobName)
	if !ok {
		t.Fatalf("画面上找不到偏移旋钮，文字有 %q", tt.Texts())
	}
	if d := absF(offset.Y - font.Y - wantStep); d > 1 {
		t.Errorf("字号旋钮在 y=%.1f，偏移旋钮在 y=%.1f，中心间距 %.1f，want %d"+
			"（上游 ⌀32 + `space-y-3` 的 12）",
			font.Y, offset.Y, offset.Y-font.Y, wantStep)
	}
	// 每一枚都是 32×32 的一个**正方**（上游 ⌀32 的圆钮：圆角半径 = 直径的一半，
	// 所以画出来是一个正圆，票 #194）。
	for _, name := range []string{lyricFontKnobName, lyricOffsetKnobName, lyricSearchKnobLabel} {
		r, ok := tt.Find(name)
		if !ok {
			t.Errorf("画面上找不到 %q，文字有 %q", name, tt.Texts())
			continue
		}
		if r.W != 32 || r.H != 32 {
			t.Errorf("%q 是 %.0f×%.0f，want 32×32（上游 ⌀32 的圆钮）", name, r.W, r.H)
		}
	}
}

// TestTheFullPlayerKnobsActuallyWork 断那三枚**真的能用**。
//
// ⚠️ **摆一枚按下去什么都不发生的键比不摆更坏**（见 `fullplayer_settings.go` 顶上）：
// 字号那枚要能改字号、偏移那枚要能改偏移、找歌词那枚要能开搜索弹窗。
//
// ⚠️ **满屏那一列用的是满屏自己的浮层状态**（`s.fullLyric.fontOpen` /
// `offsetOpen`），不是主窗口那一列的——所以这一条也顺带钉住了「两处各记各的」。
func TestTheFullPlayerKnobsActuallyWork(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	// ① 字号那枚：点开浮层，拖到顶 → 字号变成上限。
	if err := tt.Click(lyricFontKnobName); err != nil {
		t.Fatalf("点不开字号旋钮：%v（文字有 %q）", err, tt.Texts())
	}
	settleFull(t, tt)
	if _, ok := tt.Find(lyricKnobFull.font); !ok {
		t.Fatalf("点开字号旋钮之后那条滑块没出来，文字有 %q", tt.Texts())
	}
	dragFullLyricSlider(t, tt, lyricKnobFull.font, 1)
	if got := s.app.Lyrics().FontSize; got != app.MaxLyricsFontSize {
		t.Errorf("把字号那条滑块拖到顶，字号是 %v，want %v", got, float32(app.MaxLyricsFontSize))
	}

	// ② 偏移那枚：点开浮层，拖到底 → 偏移变成下限。
	if err := tt.Click(lyricOffsetKnobName); err != nil {
		t.Fatalf("点不开偏移旋钮：%v（文字有 %q）", err, tt.Texts())
	}
	settleFull(t, tt)
	if _, ok := tt.Find(lyricKnobFull.offset); !ok {
		t.Fatalf("点开偏移旋钮之后那条滑块没出来，文字有 %q", tt.Texts())
	}
	dragFullLyricSlider(t, tt, lyricKnobFull.offset, 0)
	if got := s.app.Lyrics().OffsetMS; got != app.MinLyricsOffsetMS {
		t.Errorf("把偏移那条滑块拖到底，偏移是 %d，want %d", got, app.MinLyricsOffsetMS)
	}

	// ③ 找歌词那枚：开的是歌词搜索弹窗。
	if err := tt.Click(lyricSearchKnobLabel); err != nil {
		t.Fatalf("点不开「%s」：%v（文字有 %q）", lyricSearchKnobLabel, err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText(lyricSearchHeading) {
		t.Errorf("按了「%s」，搜索弹窗没开，文字有 %q", lyricSearchKnobLabel, tt.Texts())
	}
}

// TestTheFullPlayerLyricsAreLeftAligned 断满屏的歌词**靠左一列**，不是逐行居中。
//
// ⚠️ **这一条是报告 S-20（P1）**：上游开着封面的时候歌词是左对齐的
// （`centered={!showCover}`，`full-screen-player/index.tsx:361`），所有行的左沿
// 都咬在 x=648（= 右半栏 600 加 `px-12` 的 48）。我们原来是**逐行居中**（中心
// ~851）。
//
// 判据是**像素**：歌词那一列的字是白 92%（235），而它压着的底是糊过的封面
// （最亮的一处 ~145），所以「亮过 180 的那一列」就是字。量的是那些字的最左沿。
//
// ⚠️ **不能拿文本框的左沿当判据**：文本框铺满整列（504 宽），左对齐和居中
// 两种画法下**框**的左沿都是 648——差的是**字**落在框里的哪儿。
func TestTheFullPlayerLyricsAreLeftAligned(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	content, ok := tt.Find("满屏歌词")
	if !ok {
		t.Fatalf("画面上找不到歌词那一列，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	for _, name := range []string{"第一句", "第二句", "第三句", "第四句"} {
		line, ok := tt.Find(name)
		if !ok {
			t.Fatalf("找不到 %q，文字有 %q", name, tt.Texts())
		}
		// ⚠️ **只量左半边**：右下角那三枚旋钮浮在歌词区里（上游也是浮在那儿），
		// 最下面那一句的框和旋钮那一列**重叠**——而旋钮上那枚图标是全亮的（235），
		// 比歌词那几句（60% 的不透明度，最亮 146）亮得多，整框一起量的话门槛会被
		// 旋钮抬上去，量到的就成了旋钮上的图形。
		//
		// 左半边没有旋钮（它们在 x=1096 那一带），而「字该落在哪儿」这件事在
		// 左半边就说得清：左对齐的话字从内容盒的左沿开始，居中的话左半边一个字
		// 都没有。
		half := mygoui.Rect{X: line.X, Y: line.Y, W: line.W / 2, H: line.H}
		left, ok := leftmostGlyph(img, half)
		if !ok {
			t.Errorf("%q 那一行的左半边一个字都没有——它是**居中**的"+
				"（上游是左对齐，字咬在内容盒的左沿上）", name)
			continue
		}
		// 内容盒的左沿（上游是 648）。字形自己还有半个像素的左边距，放四像素。
		if d := absF(float32(left) - content.X); d > 20 { // 竖线那一列占 16
			t.Errorf("%q 的字从 x=%d 开始，内容盒的左沿是 %.1f——"+
				"它是**居中**的（上游是左对齐，字咬在内容盒的左沿上；"+
				"居中的话那几个字会在 x=870 一带）",
				name, left, content.X)
		}
	}
}

// TestTheWheelStillScrollsTheLyricsOverTheKnobs 断**指针落在旋钮列上时滚轮照样
// 滚歌词**。
//
// ⚠️ **这一条是评审抓出来的（minor）**：那一列旋钮浮在歌词区里，而它原来是
// **滚动容器的兄弟**——框架认「滚轮滚哪一层」是顺着**命中链**（指针底下的元素
// 再一层往上）找第一个能滚的，链上没有滚动容器，滚轮就什么也不做。于是右下角
// 一条 66×120 的带子（旋钮列，连两枚之间的缝）**滚不动歌词**——那一块在这一票
// 之前是能滚的（那时候满屏一枚旋钮也没有）。
//
// ⚠️ **上游那一层是 `pointer-events-none`**（只有三枚圆钮 `pointer-events-auto`），
// 缝是透着的，滚轮照旧往上冒。我们这边对应的做法是**把旋钮列画进滚动容器里面**
// （`lyricStyle.overlay`）：那样命中链上是「旋钮 → 滚动容器」，滚轮就滚得动。
//
// 判据是**滚动位置真的动了**（`s.fullLyric.scroll.Y`），不是「看起来动了」。
func TestTheWheelStillScrollsTheLyricsOverTheKnobs(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC12, "")
	openFull(t, tt)
	settleFull(t, tt)

	col, ok := tt.Find(lyricKnobColumnName)
	if !ok {
		t.Fatalf("画面上找不到那一列旋钮，文字有 %q", tt.Texts())
	}
	// 三个采样点：两枚之间的缝（第一枚与第二枚之间）、第二枚与第三枚之间、
	// 以及一枚旋钮自己身上（上游那一枚是 `pointer-events-auto`，滚轮照样往上冒）。
	x := col.X + col.W/2
	for _, y := range []float32{
		col.Y + 32 + 6, // 第一枚与第二枚之间
		col.Y + 32 + 12 + 32 + 6,
		col.Y + 16, // 一枚旋钮自己身上
	} {
		// ⚠️ **先往下滚一段**：那样上面一定有地方（滚轮往上滚才动得了），
		// 而判据是「位置变了」——已经在最下头的时候，往下滚是不动的。
		tt.Scroll(x, y, 0, 400)
		settleFull(t, tt)
		before := s.fullLyric.scroll.Y
		tt.Scroll(x, y, 0, -200)
		settleFull(t, tt)
		if s.fullLyric.scroll.Y == before {
			t.Errorf("指针落在旋钮列上（x=%.1f, y=%.1f）滚了一下，歌词没动"+
				"（滚动位置还是 %.1f）——那一块滚不动了", x, y, before)
		}
	}
}

// TestTheFullPlayerSplitsTheWindowInHalf 断满屏**两栏 50/50，封面靠右沿**。
//
// ⚠️ **这一条是报告 S-22（P2）**：上游那两栏是硬 50/50（封面栏 0..600、歌词栏
// 600..1200），封面那一栏 `justify-end`，所以封面贴着右沿——实测 x=168..551。
// 我们原来是 42/58 且封面居中（60..443），同一张封面横向差 108 像素。
//
// ⚠️ **这些数写死在这儿**（理由同上一条）。
func TestTheFullPlayerSplitsTheWindowInHalf(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("满屏上找不到封面，文字有 %q", tt.Texts())
	}
	const (
		wantCoverLeft  = 192 // 封面栏 600 − 留白 48 − 封面 360
		wantCoverRight = 552
	)
	if d := absF(cover.X - wantCoverLeft); d > 1 {
		t.Errorf("封面的左沿在 %.1f，want %d（封面栏 600 − 留白 48 − 封面 360）",
			cover.X, wantCoverLeft)
	}
	if d := absF(cover.X + cover.W - wantCoverRight); d > 1 {
		t.Errorf("封面的右沿在 %.1f，want %d（上游：封面栏 600 − 留白 48）",
			cover.X+cover.W, wantCoverRight)
	}

	// 歌词那一栏从 600 开始（上游两个 `w-full` 的兄弟各占一半）。
	content, ok := tt.Find("满屏歌词")
	if !ok {
		t.Fatalf("画面上找不到歌词那一列，文字有 %q", tt.Texts())
	}
	if d := absF(content.X - 648); d > 1 {
		t.Errorf("歌词内容盒的左沿在 %.1f，want 648（上游：歌词栏 600 + `px-12` 的 48）",
			content.X)
	}
}

// ---- 小帮手 ----

// dragFullLyricSlider 把满屏那一条滑块拖到 0（底）或 1（顶）。
//
// f 是「从底边往上」的比例：0 是底（最小值）、1 是顶（最大值）。
//
// ⚠️ **上下各留半个钮**（`.PaddingY(thumb/2)`），所以内容框比整块矮一个钮——
// 算式和 `lyrics_test.go` 里那一份一样（同一个控件）。
func dragFullLyricSlider(t *testing.T, tt *mygoui.Tester, key string, f float32) {
	t.Helper()
	r, ok := tt.Find(key)
	if !ok {
		t.Fatalf("那条滑块 %q 不在画面上", key)
	}
	y := r.Y + r.H - float32(lyricSliderThumb)/2 - (r.H-float32(lyricSliderThumb))*f
	dragLyricSliderAt(t, tt, r, y)
}

// leftmostGlyph 是 box 里那些字的最左沿（第一个算是「字」的像素的 x）。
//
// ⚠️ **门槛是相对这一行自己的**：满屏的字有两种亮度——正在唱的那一句是不透明的
// （白 92%，实测最亮 236），别的句是 60% 的不透明度（实测最亮 146）。拿一个写死
// 的门槛去量，两种句量不到同一个数上（146 那一种用 180 的门槛量出来是「没有
// 字」）。所以门槛取「这一行最亮的那一个减去 60」，再和 40 取大——
// 40 是抗锯齿那一圈（实测最靠左的那个字像素是 48~63）。
func leftmostGlyph(img *image.RGBA, box mygoui.Rect) (int, bool) {
	peak := maxBright(img, box)
	if peak == 0 {
		return 0, false
	}
	floor := max(40, peak-60)
	for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
		for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
			p := img.RGBAAt(x, y)
			if int(p.R) >= floor && int(p.G) >= floor && int(p.B) >= floor {
				return x, true
			}
		}
	}
	return 0, false
}
