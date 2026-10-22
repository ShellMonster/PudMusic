// 歌词区（内部/lyrics.go、internal/ui/lyrics_knobs.go）的验收。
//
// 这些测试跑在 MyGo 的 headless Tester 上：真布局、真绘制、真读像素，和真窗口
// 走的是同一套 layout 与 paint（`mygo/ui/headless.go` 的文件头就写着这件事）。
//
// # 为什么每一帧都要 `tt.Advance`
//
// **Tester 的钟是站住的**，只有 `Advance` 走它（`headless.go` 的文件头）。而滚到
// 中心那一段是 `ScrollState.ScrollTo` 带的一支弹簧——弹簧按帧与帧之间的**时间**
// 推进（`mygo/ui/motion.go` 的 `step`：dt = 0 就一步都不走）。只跑 `tt.Frame()`
// 的话那支弹簧一步都不动，歌词一帧都不滚，而断言「它滚到中间了」会永远红。
//
// 这一层每一处量距离的地方都写死了数（没有 `want := 高度 + 常量` 那种拿生产
// 代码的常量核对生产代码画出来的东西的写法）——常量一改断言跟着改，那条断言就
// 永远绿。
package ui

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 歌词素材
// ─────────────────────────────────────────────────────────────────────────────

// lyricsOfN 是一份 n 句的假 LRC：第 i 句在 i × lyricStepMS。
//
// ⚠️ 步长取 200ms、句数最多 20 句，是为了让整首歌塞进测试素材那 4 秒里——
// 测试要把播放位置定到某一句上（`SeekTo`），而那个位置得真在歌的时长之内。
// 一个在歌的范围之外的偏移，`SeekTo` 会走到 `ScrubEnd(1)` 那条「放完这一首」的
// 路上去，位置归零，测的就不是想测的那一句了。
func lyricsOfN(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "[%02d:%05.2f]第%s句\n", 0, float64(i)*lyricStepMS/1000, lyricCN(i))
	}
	return b.String()
}

// lyricCN 是第 i 句里的那个数。
//
// ⚠️ 必须是**逐句都不同**的：`Tester.Find` 按整句文字找元素，撞了名它返回的
// 是第一个，而那一条断言量到的是另一句的位置——「第十九句」和「第八句」在用
// 「零一二三四五六七八九十」循环时是同一句话，于是最后一句「滚不到中间」而
// 断言量的是中间那句在哪儿，绿得像模像样。
func lyricCN(i int) string { return strconv.Itoa(i) }

// lyricsWrappingN 是同一首歌的另一个版本：句数、时刻都一样，**句子长得多**。
//
// 20px 的中文字约 20px 宽，所以歌词区 896 宽时一行放得下 44 个字，644 宽时只
// 放得下 32 个——60 个字在两种宽度下分别是两行和三行。于是**同一份歌词、同一个
// 窗口，改一次窗口宽窄就会改变每一行的高度**，而句号、句数、字号一个都没变。
func lyricsWrappingN(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "[%02d:%05.2f]第%d句%s\n",
			0, float64(i)*lyricStepMS/1000, i, lyricLongTail)
	}
	return b.String()
}

// lyricLongTail 是折行那一份素材每句后面接的那一长串。
//
// ⚠️ 要**真的折行，而且要折得够多**：歌词区 896 宽、内容框 872，而 20px 的中文
// 一行放得下 43 个字；缩到 900 宽的窗口时内容框只剩 644，一行放得下 32 个。
// 这条 121 个字，于是 872 宽时折成三行、644 宽时折成四行。
//
// 写成三十几个字的话它一行就放得下：整份素材和短句那一份量起来**一模一样**，
// 于是「换一份歌词」和「改窗口宽窄」这两条断言全都成了空转——它们量不到任何
// 高度的变化，而高度不变就没有东西会偏出去。（这一条就是这么被自己抓到的：
// 头一版 38 个字，四条断言全绿，而它们什么也没量到。）
const lyricLongTail = "这一句歌词写得很长很长长到一定要折行才放得下否则就量不到窗口宽窄对它的高度" +
	"所以换一份歌词源的时候它的高度就变了于是正在被唱的那一句会被顶到下面去而句子还是那一句" +
	"这里再补一句把它凑到一百二十一个字上下好让折行这件事在两个宽度下真的发生"

// lyricStepMS 是假 LRC 里两句相隔多少毫秒。
const lyricStepMS = 200

// lyricsOfNWithTranslation 是同一首歌的**译文**那份 LRC：同一时刻的那一句后面
// 跟一句「译 i」。
//
// ⚠️ 译文的时间戳必须和原文**一模一样**——叠不上的那些行会被丢掉（`lyrics.Row`
// 的注释，上游是拿时间戳做键去查的）。差一毫秒译文就是空的，而界面上看不出
// 「叠不上」，只看出「这歌没有翻译」。
func lyricsOfNWithTranslation(n int) string {
	var b strings.Builder
	for i := range n {
		at := fmt.Sprintf("[%02d:%05.2f]", 0, float64(i)*lyricStepMS/1000)
		fmt.Fprintf(&b, "%s译 %d\n", at, i)
	}
	return b.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// 宿主
// ─────────────────────────────────────────────────────────────────────────────

// lyricShell 造一个停在迷你播放器上、正放着一首歌的外壳 + Tester。
//
// ⚠️ **播完立刻暂停**：测试素材那 4 秒是真在走的，而后面的断言全靠
// `SeekTo` 把位置定死。暂停之后 `App.tick` 那个 50ms 的钟不再改位置
// （`internal/app/app.go` 的 `live := a.wantPlay || a.playing`），位置就定住了。
func lyricShell(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	s, tt := newShell(t)
	s.app.SetWake(s.wake.invalidate)
	openATrack(t, s)
	s.app.Pause()
	s.navigate(miniPlayerPath)
	tt.Frame()
	return s, tt
}

// putLyrics 给当前这一首装上歌词（`App.PutLyrics` 是给「找到了歌词」那条路用的
// 那个入口，见 internal/app/lyrics.go）。
func putLyrics(s *Shell, lrc, translated string) {
	s.app.PutLyrics(lrc, translated)
}

// seekTo 把播放位置定到第 i 句上，然后让画面安定下来。
func seekTo(t *testing.T, s *Shell, tt *mygoui.Tester, i int) {
	t.Helper()
	s.app.SeekTo(time.Duration(i) * lyricStepMS * time.Millisecond)
	settleLyrics(t, s, tt)
}

// settleLyrics 跑几帧、推几段时间，直到「滚到中间」那支弹簧**真的停手**。
//
// ⚠️ **不能只跑固定几帧。** 每一帧这一层都会把目标重新对一次（见
// `lyricsView.centre`），而目标本身是拿**上一帧**量到的框算的——于是弹簧到位之后
// 还要再花两三帧才真的落定，固定四帧跑完它常常还在飞。弹簧还在飞的时候
// `Scrolling()` 是 true，于是「依据变了没有」这件事被盖住了：换一份歌词、拖一下
// 字号都会顺带再对一次目标，而那条断言量到的是那一下，不是触发它的那个依据。
//
// 所以这里推到 `Scrolling()` 变 false 为止（最多 20 帧，2 秒）。
func settleLyrics(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	// ⚠️ **要连续三帧都停着才走**。只等「这一帧停着」不够：某一帧如果刚好重新对
	// 了一个目标，而那个目标就是当前位置，弹簧当场就收手、`Scrolling()` 变 false，
	// 于是在**下一帧按新几何重新对一次**之前就被当成了「落定了」——而那一帧正是
	// 内容刚变、框还量着旧的。
	//
	// 换个说法：这一层是对着**上一帧**的框算目标的，所以任何一次改动都要再花一
	// 两帧才对得准。停三帧就是给那两三帧留的。
	calm := 0
	for range 40 {
		tt.Advance(100 * time.Millisecond)
		tt.Frame()
		if s.lyric.scroll.Scrolling() {
			calm = 0
			continue
		}
		if calm++; calm >= 3 {
			return
		}
	}
	t.Fatalf("40 帧（4 秒）之后画面还没落定")
}

// boxOf 是歌词容器那个框（界面上量到的）。它是「上一帧」那份，见 lyrics.go。
func boxOf(s *Shell) mygoui.Rect { return s.lyric.box }

// lyricRowAt 找出第 i 句在画面上的框（短句那一份素材）。
func lyricRowAt(t *testing.T, tt *mygoui.Tester, i int) mygoui.Rect {
	t.Helper()
	return lyricRowAtText(t, tt, lyricLineText(i))
}

// lyricRowAtText 按**整句**文字找它在画面上的框。
//
// ⚠️ 必须是整句：`Tester.Find` 比的是相等而不是包含（`mygo/ui/headless.go`），
// 而折行那一份素材的每句后面还带着一长串，只找「第8句」找不到。
func lyricRowAtText(t *testing.T, tt *mygoui.Tester, text string) mygoui.Rect {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("「%s」不在画面上，文字有 %q", text, tt.Texts())
	}
	return r
}

// lyricWrappingText 是折行那一份素材里第 i 句的整句。
func lyricWrappingText(i int) string {
	return fmt.Sprintf("第%d句%s", i, lyricLongTail)
}

// lyricLineText 是第 i 句在歌词里的那句话。
func lyricLineText(i int) string { return "第" + lyricCN(i) + "句" }

// lyricInk 报第 y 那**一行**横线上有多少墨：取这一行里离底色最远的那一个像素，
// 换算成「比底色暗了几成」。
//
// # 为什么是「最远的一个」而不是「平均」
//
// 一行中文的字有横有竖，行与行之间还有那 8px 的缝。取平均的话缝所在的那几行
// 把整行拉低，量到的是「这一带恰好没有字」而不是「这一带被盖住了多少」——
// 两种量法在渐隐对的时候都会绿，在渐隐错的时候都红不了。
//
// 取最远的那一个像素量到的是字的最浓处（笔画正中），而渐隐是**均匀**盖在上面的，
// 所以盖多少就还剩几成。
//
// # 为什么不是「数一数有多少个像素不是底色」
//
// 那是个开关，不是刻度：一个被盖到只剩 15% 的字，它和底色差着二十几个数，还是
// 「不是底色」；而被完全盖住的那一份才是 0。于是量出来的是「有没有字」，而
// 「渐隐是不是一段渐变」问的是「盖了多少」——两种不同的东西，用同一种量法量，
// 量到的是前者。
//
// # 为什么只量每句**左边**
//
// `x1-x0` 取 400 是因为右下角浮着那两个旋钮（internal/ui/lyrics_knobs.go），
// 量整行宽度会把它们算进来——实测下沿量到的比没被盖住时还多。
func lyricInk(img *image.RGBA, bg [3]int, x0, x1, y int) float64 {
	if y < 0 || y >= img.Bounds().Dy() {
		return 0
	}
	worst := 0.0
	for x := x0; x < x1; x++ {
		if x < 0 || x >= img.Bounds().Dx() {
			continue
		}
		p := img.RGBAAt(x, y)
		if d := bgLuma(bg) - luma(p.R, p.G, p.B); d > worst {
			worst = d
		}
	}
	if bgLuma(bg) <= 0 {
		return 0
	}
	return worst / bgLuma(bg)
}

// luma 是一个像素的亮度（0…255）。
func luma(r, g, b uint8) float64 {
	return 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
}

// bgLuma 是底色的亮度。
func bgLuma(bg [3]int) float64 { return luma(uint8(bg[0]), uint8(bg[1]), uint8(bg[2])) }

// lyricRowNearest 找出**整句都在画面上**、中心离相对位置 f 最近的那一句。
//
// ⚠️ 为什么要「找一句」而不是直接量某一行：渐隐那两条带子各有 24% 高，而一句
// 歌词占 51px（歌词区 588 高的 8.7%），所以任意一个相对位置上总有一句落进去。
// 直接量某一行的話，量到的可能是句与句之间那条缝——那一行本来就没有字，量到
// 0，而那与渐隐对不对无关。
func lyricRowNearest(t *testing.T, tt *mygoui.Tester, box mygoui.Rect, f float32) (mygoui.Rect, float32) {
	t.Helper()
	want := box.Y + box.H*f
	best, bestD, bestF := mygoui.Rect{}, float32(-1), float32(0)
	for i := range 20 {
		r, ok := tt.Find(lyricLineText(i))
		// H 是 0 = 这一句被容器裁掉了（画在可视区外），量不到墨。
		if !ok || r.H <= 0 || r.Y < box.Y || r.Y+r.H > box.Y+box.H {
			continue
		}
		if d := absf(r.Y + r.H/2 - want); bestD < 0 || d < bestD {
			best, bestD = r, d
			bestF = (r.Y + r.H/2 - box.Y) / box.H
		}
	}
	if bestD < 0 {
		t.Skipf("没有一句整句都在画面上（f=%.2f）", f)
	}
	return best, bestF
}

// lyricInkNear 量「相对位置 f 附近那一句的墨」，并且**核对自己落在哪儿**。
//
// 返回取样点实际落在的相对位置：渐隐那两条带子各有 24% 高，而一句歌词只占
// 8.7%（51px / 588px），所以「离 f 最近的那一句」可能落在带子外面——那时候量
// 到的就不是渐隐，而是带子外面那一句自己的墨，一条断言会**绿得像模一样**。
// 所以这里把落点报出来，由调用方核对它在自己的带子里。
func lyricInkNear(t *testing.T, tt *mygoui.Tester, img *image.RGBA, bg [3]int,
	box mygoui.Rect, f, lo, hi float32,
) (float64, float32) {
	t.Helper()
	r, got := lyricRowNearest(t, tt, box, f)
	x0 := int(box.X) + 4
	ink := lyricInk(img, bg, x0, x0+400, int(r.Y+r.H/2))
	t.Logf("  取样想要 %.3f，落在 %.3f（该在 %.3f…%.3f）：墨 %.3f", f, got, lo, hi, ink)
	if got < lo || got > hi {
		t.Errorf("取样点落在 %.3f，不在它该在的 %.3f…%.3f 里 —— 这一条量不到渐隐（"+
			"要么带子宽度改了，要么歌词区高度变了）", got, lo, hi)
	}
	return ink, got
}

// absf 是 float32 的绝对值（内建 `math.Abs` 只吃 float64）。
func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// bgOf 是歌词区那一块的底色：那块里出现得最多的那个颜色。
//
// ⚠️ **不能**拿窗口某个角上的像素当底色：渐隐遮罩盖的正是边角，拿那里的像素
// 当底色等于「假设遮罩是对的」再去验它。
func bgOf(img *image.RGBA, box mygoui.Rect) [3]int {
	count := map[[3]int]int{}
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		for x := int(box.X); x < int(box.X+box.W); x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			p := img.RGBAAt(x, y)
			count[[3]int{int(p.R), int(p.G), int(p.B)}]++
		}
	}
	var best [3]int
	bestN := -1
	for c, n := range count {
		if n > bestN {
			best, bestN = c, n
		}
	}
	return best
}

// ─────────────────────────────────────────────────────────────────────────────
// 逐行画出来
// ─────────────────────────────────────────────────────────────────────────────

// TestEveryLyricLineIsOnScreen 一句一句都在。
//
// 抄上游 `index.tsx:371` 那个 `lyrics.map(...)`：歌词有多少句就画多少行，没有
// 分页、没有虚拟滚动——一首歌的歌词最多几百行，MyGo 的布局扛得住，而虚拟化
// 会在「正在唱的那一句刚好在可视区外面」时算错。
func TestEveryLyricLineIsOnScreen(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	for i := range 20 {
		if !tt.HasText(lyricLineText(i)) {
			t.Fatalf("第 %d 句没画出来，文字有 %q", i, tt.Texts())
		}
	}
	// 反过来：素材里没有的那些字一个字都不许出现。
	if tt.HasText("第二十一句") {
		t.Errorf("素材里没有的句子画出来了，文字有 %q", tt.Texts())
	}
}

// TestTheSungLineIsHalfAgainAsBig 正在唱的那一句放大到 1.5 倍。
//
// 抄上游 `index.tsx:335` 的 `fontSize: isActive ? fontSize * 1.5 : fontSize`。
// 量的是**画出来的那一行文字有多高**：行高是字号的 1.375 倍（`leading-snug`），
// 所以两行的高度之比就是字号的比。
func TestTheSungLineIsHalfAgainAsBig(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	active := lyricRowAt(t, tt, 8)
	quiet := lyricRowAt(t, tt, 9)
	if active.H <= quiet.H {
		t.Fatalf("正在唱的那一句没有变大：它 %v 高，旁边那句 %v 高", active.H, quiet.H)
	}
	// 1.5 倍不是「差不多 1.5 倍」。行高 = 字号 × 1.375，两行同一条基线，
	// 所以比值就是 1.5。
	//
	// ⚠️ **期望值写死成 1.5，不写 `lyricsActiveScale`**：拿生产代码的常量当尺子
	// 量生产代码画出来的东西，常量一改两边一起动，这条断言照样绿——那正是本文件
	// 顶上明令禁止的写法。「放大倍数这个数本身对不对」由
	// TestTheUpstreamNumbersAreTheOnesWeUse 管，那一条的期望值同样写死。
	gotRatio := float64(active.H) / float64(quiet.H)
	if math.Abs(gotRatio-1.5) > 0.02 {
		t.Errorf("放大后和旁边的比是 %.3f，want 1.5（上游 index.tsx:335 的 fontSize * 1.5）", gotRatio)
	}
}

// TestTheSungLineIsBlackerThanTheOthers 正在唱的那一句不透明，其余是 60%。
//
// 抄上游 `index.tsx:333` 的 `isActive ? "opacity-100" : "opacity-60"`。
//
// ⚠️ 这条**量像素**：只看元素在不在、字高对不对，量不到「淡」——而「淡」是
// 这一条验收项的另一半（`opacity-60` 摆在那儿就够验收了的话，那一半就没人管）。
func TestTheSungLineIsBlackerThanTheOthers(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	box := boxOf(s)
	img := tt.Image()
	bg := bgOf(img, box)
	x0 := int(box.X) + 4
	x1 := x0 + 400
	inkOf := func(r mygoui.Rect) float64 {
		return lyricInk(img, bg, x0, x1, int(r.Y+r.H/2))
	}

	// 量的是**没有被遮罩盖到**的两句：上沿那条带子在 24% 就结束了，中间和紧挨着
	// 中间的那两句都落在没盖的地方，量到的才是它们自己的墨。
	activeRow, _ := lyricRowNearest(t, tt, box, 0.5)
	quietRow, _ := lyricRowNearest(t, tt, box, 0.62)
	active := inkOf(activeRow)
	quiet := inkOf(quietRow)
	t.Logf("墨 正在唱=%.3f 旁边=%.3f", active, quiet)
	if quiet <= 0 {
		t.Fatalf("旁边那一句一个字都没量到")
	}
	// `opacity-100` 对 `opacity-60`：不透明的那一句比旁边那句至少深三成。
	if active < 1.3*quiet {
		t.Errorf("正在唱的那一句 %.3f，旁边那句 %.3f —— 「放大到 1.5 倍且不透明」没做全",
			active, quiet)
	}
}

// TestTheSungLineScrollsToTheMiddle 正在唱的那一句落在容器中间。
//
// 抄上游 `index.tsx:306-316` 那个 `useEffect`：当前这一句 `el.offsetTop -
// wrapper.clientHeight / 2 + el.clientHeight / 2`，滚过去。
func TestTheSungLineScrollsToTheMiddle(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	for _, i := range []int{0, 8, 19} {
		seekTo(t, s, tt, i)
		box := boxOf(s)
		row := lyricRowAt(t, tt, i)
		want := box.Y + box.H/2
		got := row.Y + row.H/2
		if math.Abs(float64(got-want)) > 1 {
			t.Errorf("第 %d 句的中心在 %.2f，容器中心在 %.2f，差 %.2f",
				i, got, want, math.Abs(float64(got-want)))
		}
	}
}

// TestTheSungLineStaysCentredAfterTheFontKnob 拖完字号滑块，它还在中间。
//
// 🔴 这是真踩过的坑：滚动指令**只在「句号变了」或「上一次那一下还在飞」时才发**
// （`lyricsView.centre`）。而拖字号旋钮这两样都不成立——句号没变，也不产生任何
// 动画——可是内容高度与那份居中留白（`box.H/2 − 该句高/2`）已经整个变了。症状是
// 正在唱的那一句被甩出歌词区：48px 时偏 400 多像素、整句在可视区之外，要等换到
// 下一句才回来，而旋钮上明明白白写着 48px。
//
// 上游不会这样，是**因为 `centerPadding` 进了那个滚动 effect 的依赖表**
// （`index.tsx:316` 的 `[activeIndex, centerPadding]`），它一变就自动补上那一下。
// 这一层没有 useEffect，得自己把「依据」凑齐（见 `lyricAim`）。
//
// ⚠️ 这一条走**真界面那条路**：点开浮层、拖滑块——绕开界面直接
// `SetLyricsFontSize` 也测得到（下面那条），但走界面才能顺带验到「点得开浮层、
// 拖得动滑块、旋钮上的数跟着变」。
func TestTheSungLineStaysCentredAfterTheFontKnob(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	openLyricPopover(t, s, tt, lyricFontKnobName)
	for _, px := range []float32{app.MaxLyricsFontSize, app.MinLyricsFontSize,
		app.DefaultLyricsFontSize, 40} {
		dragLyricSliderTo(t, tt, lyricFontSliderKey, px)
		settleLyrics(t, s, tt)

		if got := s.app.Lyrics().FontSize; got != px {
			t.Fatalf("拖到 %v：字号是 %v —— 滑块那一段没验到", px, got)
		}
		box, row := boxOf(s), lyricRowAt(t, tt, 8)
		off := math.Abs(float64(row.Y + row.H/2 - (box.Y + box.H/2)))
		if off > 1 {
			t.Errorf("字号 %v：第 8 句的中心偏了 %.1fpx（离容器中心 %.1f）——"+
				"整句还看得见吗：%v", px, off, row.Y+row.H/2,
				row.Y >= box.Y && row.Y+row.H <= box.Y+box.H)
		}
	}
	// 旋钮上写的那个数要跟着一起变——不然这一条就只是在验「旋钮没断电」。
	if !tt.HasText(lyricFontLabel(40)) {
		t.Errorf("旋钮上没有 %q，文字有 %q", lyricFontLabel(40), tt.Texts())
	}
}

// TestTheSungLineStaysCentredAtEveryFontSizeEveryFontIsNotJustTheOneTheUser
// Asked 遍一遍 12~48：任何一个字号都不能把正在唱的那一句甩出可视区。
//
// 上面那条量的是「中间」这一处；这一条量的是**整句看得见**——48px 时整句偏出
// 可视区，是先前那条断言量不到的（它量的是中心点离容器中心多远，而整句在屏幕
// 之外时中心点也在屏幕之外，两头一起偏、差值反而看着不大）。
func TestTheSungLineStaysCentredAtEveryFontSize(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)
	openLyricPopover(t, s, tt, lyricFontKnobName)

	for px := float32(app.MinLyricsFontSize); px <= app.MaxLyricsFontSize; px++ {
		dragLyricSliderTo(t, tt, lyricFontSliderKey, px)
		settleLyrics(t, s, tt)

		box, row := boxOf(s), lyricRowAt(t, tt, 8)
		if off := math.Abs(float64(row.Y + row.H/2 - (box.Y + box.H/2))); off > 1 {
			t.Errorf("字号 %v：第 8 句的中心偏了 %.1fpx（离容器中心 %.1f）", px, off, row.Y+row.H/2)
		}
		// ⚠️ 「整句看得见」是**另一条**：中心点离容器中心多远，和整句在不在屏幕
		// 里，是两件事——整句甩到屏幕外时它的中心点也在屏幕外，两头一起偏，看
		// 「偏了多少」是量不出来的（实测：偏 276px 时中心点还在区内，而那一票的
		// 现象是整句不见）。
		if row.Y < box.Y || row.Y+row.H > box.Y+box.H {
			t.Errorf("字号 %v：第 8 句在 %.2f…%.2f，歌词区是 %.2f…%.2f —— 整句不在可视区里",
				px, row.Y, row.Y+row.H, box.Y, box.Y+box.H)
		}
	}
}

// TestTheSungLineStaysCentredWhenTheWindowResizes 改窗口大小之后它还在中间。
//
// 和拖字号是同一类：容器变了，句号没变，也不产生动画。上游靠 `resize` 监听
// 重新量那份居中留白（`index.tsx:300-304`），而留白一变，滚动那个 effect 就跟着
// 重跑（它的依赖表里有 `centerPadding`）。
func TestTheSungLineStaysCentredWhenTheWindowResizes(t *testing.T) {
	s, tt := lyricShell(t)
	// ⚠️ **必须用会折行的长句**。三个字一句的歌词在 644 宽和 896 宽下都是一行，
	// 改窗口宽窄不改变任何一行的高度，于是这一条量的是「容器高度变了」——而
	// 那一件是**自己会对齐的**（那份居中留白是按容器高算的，容器矮一半、留白
	// 也矮一半，两者一起动，句子的位置对着容器中心不动）。真正会偏的是**宽度**
	// 变了、于是句子折行数变了的那一种，也就是长句。
	putLyrics(s, lyricsWrappingN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	for _, size := range [][2]int{{900, 800}, {1200, 800}, {960, 560}} {
		tt.SetSize(size[0], size[1])
		settleLyrics(t, s, tt)
		box, row := boxOf(s), lyricRowAtText(t, tt, lyricWrappingText(8))
		if off := math.Abs(float64(row.Y + row.H/2 - (box.Y + box.H/2))); off > 1 {
			t.Errorf("窗口 %dx%d（歌词区 %.2f 宽 %.1f 高）：第 8 句的中心偏了 %.1fpx",
				size[0], size[1], box.W, box.H, off)
		}
	}
}

// TestTheSungLineStaysCentredAfterSwappingTheLyrics 换一份歌词源之后它还在中间。
//
// #21 的「找到了歌词」走的就是 `App.PutLyrics`：同一首歌，句数可以一样、句子可以
// 长短不同，而**句子长短就是那一行的高度**。于是正在唱的那一句被顶到下面去了，
// 而句号、句数、字号、容器一个都没变——上游那份依赖表
// （`[activeIndex, centerPadding]`）没有这一项，它那边同样会偏出去。
func TestTheSungLineStaysCentredAfterSwappingTheLyrics(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)
	putLyrics(s, lyricsWrappingN(20), "")
	settleLyrics(t, s, tt)

	box, row := boxOf(s), lyricRowAtText(t, tt, lyricWrappingText(8))
	if off := math.Abs(float64(row.Y + row.H/2 - (box.Y + box.H/2))); off > 1 {
		t.Errorf("换了一份更长的歌词之后，第 8 句的中心偏了 %.1fpx", off)
	}

	// 句数也换（#21 找到的另一份是另一个版本的歌词）。
	putLyrics(s, lyricsOfN(40), "")
	settleLyrics(t, s, tt)
	box, row = boxOf(s), lyricRowAt(t, tt, 8)
	if off := math.Abs(float64(row.Y + row.H/2 - (box.Y + box.H/2))); off > 1 {
		t.Errorf("换成四十句的那一份之后，第 8 句的中心偏了 %.1fpx", off)
	}
}

// TestTheFirstLineAlsoReachesTheMiddle 第一句也能滚到中间。
//
// 这是上下那两份居中留白存在的**唯一**理由（上游 `index.tsx:369-372` 的
// `paddingTop/paddingBottom: centerPadding`）。没有留白的话第一句永远在顶上，
// 而用户看着歌还没唱到第一句词就已经看到它了。
func TestTheFirstLineAlsoReachesTheMiddle(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 0)

	box := boxOf(s)
	row := lyricRowAt(t, tt, 0)
	if math.Abs(float64(row.Y+row.H/2-(box.Y+box.H/2))) > 1 {
		t.Errorf("第一句的中心在 %.2f，容器中心在 %.2f", row.Y+row.H/2, box.Y+box.H/2)
	}
	if s.lyric.scroll.Y != 0 {
		t.Errorf("第一句时滚动量是 %.2f，want 0（上面那份留白就够把它放到中间）", s.lyric.scroll.Y)
	}
}

// TestTheLyricsAreaNeverGrowsTallerThanItsPage 歌词区不会越长越高。
//
// 🔴 这是**一个真的踩过的坑**：歌词区原本是 `Box` 里套一个 `Row`，而 `Row` 的
// `Grow(1)` 走的是横那条轴，它的高度就变成「由内容撑着」；内容的高度里有那两份
// 居中留白，留白又是按容器高度算的，于是 高 → 留白 → 内容更高 → 高更大。
// 一帧大一截，最后歌词区比窗口还高、顶上半截在屏幕外，而界面上看不出异样：
// 放大、渐隐、译文全对，只有「滚不到中间」那一样看不出来。
func TestTheLyricsAreaNeverGrowsTallerThanItsPage(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	box := boxOf(s)
	if box.H <= 0 {
		t.Fatalf("歌词区没有高度")
	}
	if box.Y < 0 || box.Y+box.H > 800 {
		t.Errorf("歌词区是 %.2f…%.2f，跑出 800 高的窗口上边去了", box.Y, box.Y+box.H)
	}
	// 三句不同的位置各量一次：留白是跟着「正在唱的那一句」变的，只量一次量不到。
	for _, i := range []int{0, 8, 19} {
		seekTo(t, s, tt, i)
		if b := boxOf(s); b.H != box.H {
			t.Errorf("第 %d 句时歌词区高 %.2f，起始那一句时是 %.2f", i, b.H, box.H)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 译文
// ─────────────────────────────────────────────────────────────────────────────

// TestTheTranslationSitsUnderItsLine 译文叠在原词下面。
//
// 抄上游 `index.tsx:343-345`：译文是原词那个 `div` 里的第二个 `div`，排版
// 就在下面，中间是 `mt-1`（0.25rem = 4px）。
func TestTheTranslationSitsUnderItsLine(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), lyricsOfNWithTranslation(20))
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	original := lyricRowAt(t, tt, 8)
	translated, ok := tt.Find("译 8")
	if !ok {
		t.Fatalf("译文没画出来，文字有 %q", tt.Texts())
	}
	if translated.Y <= original.Y+original.H {
		t.Errorf("译文没有在原词下面：原词 %.2f…%.2f，译文 %.2f",
			original.Y, original.Y+original.H, translated.Y)
	}
}

// TestTheTranslationKeepsItsOwnSize 译文不跟着原词那个字号走。
//
// 抄上游 `index.tsx:344` 的 `text-sm`：那是**绝对值**（0.875rem = 14px），不是
// `字号 × 某个系数`。跟着走的话，20px 的歌词下面是一句 30px 的翻译，而翻译比
// 歌词还大——那一眼看过去是「主次反了」。
func TestTheTranslationKeepsItsOwnSize(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), lyricsOfNWithTranslation(20))
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	quietOriginal := lyricRowAt(t, tt, 9) // 20px
	quietTranslated, ok := tt.Find("译 9")
	if !ok {
		t.Fatalf("第 9 句的译文没画出来，文字有 %q", tt.Texts())
	}
	// 20px × 1.375（leading-snug）= 27.5；译文 20px 固定行高（text-sm 自带的
	// line-height: 1.25rem）。所以译文比原文矮一点点，而不是一样高。
	if quietTranslated.H >= quietOriginal.H {
		t.Errorf("译文 %v 高，和 20px 的原文 %v 一样高或更高（它该是 14px）",
			quietTranslated.H, quietOriginal.H)
	}
	// 正在被唱的那一句是 30px，它的译文仍然是 14px。
	activeOriginal := lyricRowAt(t, tt, 8)
	activeTranslated, ok := tt.Find("译 8")
	if !ok {
		t.Fatalf("第 8 句的译文没画出来，文字有 %q", tt.Texts())
	}
	if math.Abs(float64(activeTranslated.H-quietTranslated.H)) > 0.5 {
		t.Errorf("原词从 20px 放大到 30px，译文却从 %v 变成了 %v（它该是死的 14px）",
			quietTranslated.H, activeTranslated.H)
	}
	_ = activeOriginal
}

// ─────────────────────────────────────────────────────────────────────────────
// 上下渐隐遮罩
// ─────────────────────────────────────────────────────────────────────────────

// TestTheTopAndTheBottomBothFadeOut 上沿和下沿都把歌词淡进底色里。
//
// 抄上游 `index.tsx:357-360` 那条 `maskImage` 的八个色标。
//
// 🔴 钉的是**盖住多少**，不是「有渐变就行」。上游那条 mask 的 alpha 0 是「藏起来」、
// 1 是「透出来」，而这一层画的是一块盖板，盖住的程度 = 1 − alpha。写反了的现象
// 是：紧贴窗口上下边缘的歌词满色被硬切、条子内侧反而淡到看不见，而两条带子各自
// 在中间鼓一个包——那一眼看过去是「渐隐做出来了」，只是反了。
//
// 每条带子上量两个点：贴边那个（该几乎全被盖住）和带子内侧（该透出大半），
// 再加中间那个当尺子。四种改法各自会红：① 方向倒过来 ② 整条拿掉 ③ 纯色盖板
// ④ 只画上沿。
func TestTheTopAndTheBottomBothFadeOut(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	box := boxOf(s)
	img := tt.Image()
	bg := bgOf(img, box)
	x0 := int(box.X) + 4
	x1 := x0 + 400

	_ = x1
	// 位置照上游那八个色标换算过来：上沿占 0%…24%、下沿占 76%…100%。
	// 带子里 20% 的位置上盖住 0.29（还剩 71%），贴边那个位置盖住 0.87（还剩 13%）。
	const inBand = 0.20
	const edge = 0.05
	top, bot := float32(lyricsFadeTop), float32(1-lyricsFadeBottom)

	topEdge, _ := lyricInkNear(t, tt, img, bg, box, edge, 0, 0.5*top)
	topIn, _ := lyricInkNear(t, tt, img, bg, box, inBand, 0.5*top, top)
	mid, _ := lyricInkNear(t, tt, img, bg, box, 0.5, top, bot)
	botIn, _ := lyricInkNear(t, tt, img, bg, box, 1-inBand, bot, 1-0.5*(1-bot))
	botEdge, _ := lyricInkNear(t, tt, img, bg, box, 1-edge, 1-0.5*(1-bot), 1)
	t.Logf("墨 上沿边=%.3f 上沿内=%.3f 中=%.3f 下沿内=%.3f 下沿边=%.3f",
		topEdge, topIn, mid, botIn, botEdge)
	if mid <= 0 {
		t.Fatalf("中间那一句一个字都没量到（%.3f），后面几条都没得比", mid)
	}

	// ① 方向倒过来：贴边满色、带子内侧全淡，这两条直接反着红。
	if topEdge > 0.35*mid {
		t.Errorf("上沿贴边还剩 %.3f 的墨，中间有 %.3f —— 渐隐方向反了", topEdge, mid)
	}
	if botEdge > 0.35*mid {
		t.Errorf("下沿贴边还剩 %.3f 的墨，中间有 %.3f —— 渐隐方向反了", botEdge, mid)
	}
	// ③ 纯色盖板 / 只有一档：带子内侧应该已经透出大半，而不是盖得比贴边还多。
	if topIn < 0.45*mid {
		t.Errorf("上沿带子内侧只剩 %.3f，中间有 %.3f —— 那是一块盖板，不是一段渐变", topIn, mid)
	}
	if botIn < 0.45*mid {
		t.Errorf("下沿带子内侧只剩 %.3f，中间有 %.3f —— 那是一块盖板，不是一段渐变", botIn, mid)
	}
	// 渐变是越往里越透：贴边必须比内侧淡。
	if topEdge >= topIn {
		t.Errorf("上沿贴边 %.3f 不比内侧 %.3f 淡 —— 中间鼓了一个包", topEdge, topIn)
	}
	if botEdge >= botIn {
		t.Errorf("下沿贴边 %.3f 不比内侧 %.3f 淡 —— 中间鼓了一个包", botEdge, botIn)
	}
	// ④ 只画上沿：下沿内侧等于中间。
	if botIn > 0.9*mid {
		t.Errorf("下沿内侧 %.3f 和中间 %.3f 一样浓 —— 下沿那条根本没画", botIn, mid)
	}
	// 上下两段是对称的（Biu 那条 mask 上下一样），差太多说明有一段写歪了。
	if d := math.Abs(topIn - botIn); d > 0.2 {
		t.Errorf("上下两段内侧差了 %.3f（%.3f / %.3f），上游那条 mask 上下是对称的",
			d, topIn, botIn)
	}
}

// TestTheFadeBandsEndWhereUpstreamSays 遮罩带子在哪儿结束，照上游那八个色标。
//
// 上游：`transparent 0% … black 24%` 是上沿（`black` = alpha 1 = 完全透出来，
// 也就是盖板一点都不盖），`black 76% … transparent 100%` 是下沿。
//
// 这一条钉的是**带子有多宽**：24% 那儿往里就该一点不盖了，76% 那儿往外才刚开始
// 盖。宽了是「中段整片歌词都灰着」，窄了是「边上一圈字被硬切」。
func TestTheFadeBandsEndWhereUpstreamSays(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	box := boxOf(s)
	img := tt.Image()
	bg := bgOf(img, box)
	x0 := int(box.X) + 4
	x1 := x0 + 400
	_ = x1
	top, bot := float32(lyricsFadeTop), float32(1-lyricsFadeBottom)
	// 20% / 32% / 68% / 80% 四个取样点，各自核对自己落在哪一段里。
	in, _ := lyricInkNear(t, tt, img, bg, box, 0.20, 0.5*top, top)
	pastTop, _ := lyricInkNear(t, tt, img, bg, box, 0.32, top, 0.5*(top+bot))
	pastBot, _ := lyricInkNear(t, tt, img, bg, box, 0.68, 0.5*(top+bot), bot)
	inBot, _ := lyricInkNear(t, tt, img, bg, box, 0.80, bot, 1-0.5*(1-bot))

	// 上沿：20% 处还盖着（盖 0.29，还剩 71%），32% 处一点不盖（100%）。
	if in < 0.75*pastTop {
		t.Errorf("24%% 那条带子之内（20%% 处）只剩 %.3f，带子之外（32%% 处）有 %.3f —— 带子太宽了",
			in, pastTop)
	}
	// 下沿：68% 处一点不盖（100%），80% 处还盖着（还剩 71%）。
	if inBot < 0.75*pastBot {
		t.Errorf("76%% 那条带子之内（80%% 处）只剩 %.3f，带子之外（68%% 处）有 %.3f —— 带子太宽了",
			inBot, pastBot)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 切歌
// ─────────────────────────────────────────────────────────────────────────────

// TestSwitchingSongsLeavesNothingOfTheLastOne 切歌不残留上一首的歌词。
//
// 票面点名的那一条。歌词文本每一帧都是从快照读的（所以按道理残留不了），而
// **滚动到了哪儿**是要自己记着的——那正是唯一会残留的地方：滚到 800 的位置再换歌，
// 新歌就从中间开始显示，而那看着像是「这首歌缺了头几句」。
func TestSwitchingSongsLeavesNothingOfTheLastOne(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)
	if s.lyric.scroll.Y <= 0 {
		t.Fatalf("没滚下去过（Y=%.2f），后面那一步就验不到残留", s.lyric.scroll.Y)
	}

	openAnotherTrack(t, s)
	settleLyrics(t, s, tt)

	for i := range 20 {
		if tt.HasText(lyricLineText(i)) {
			t.Fatalf("换歌之后上一首的第 %d 句还在画面上，文字有 %q", i, tt.Texts())
		}
	}
	if s.lyric.scroll.Y != 0 {
		t.Errorf("换歌之后还停在 %.2f，want 0", s.lyric.scroll.Y)
	}
	// 新歌的歌词一上来，从头开始。
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 0)
	if s.lyric.scroll.Y != 0 {
		t.Errorf("新歌第一句时滚动量是 %.2f，want 0", s.lyric.scroll.Y)
	}
}

// TestTheSongBeforeTheOneYouGoBackTo 没有反过来也没有。
//
// 换歌时是**先重置成默认值、再从缓存恢复**（internal/app/lyrics.go 的
// `setLyricTrack`；顺序反了的症状见那个文件头）。这一条从界面上钉住「回来的时候
// 那一首还在自己那个位置上」——因为界面上看得到的就是那两个旋钮上写的数。
//
// ⚠️ 这一条要挂一个**真的**缓存（`lyrics.NewCache`），不然调过的偏移和字号压根
// 落不下去（`App.flushLyrics` 拿不到缓存就什么都不写），回来的时候自然也没有
// 「恢复」可言——而那一条会绿得像模像样。
func TestTheSongBeforeTheOneYouGoBackTo(t *testing.T) {
	s, tt := lyricShell(t)
	s.app.MountLyrics(lyrics.NewCache(writableTempDir(t, "lyrics")))
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	s.app.SetLyricsOffset(app.LyricsOffsetStepMS * 4) // +200ms
	s.app.SetLyricsFontSize(33)
	settleLyrics(t, s, tt)
	// ⚠️ 再装一次歌词，把那两个值**立刻**落盘（`PutLyrics` 是同步写的）。
	// 不这么做就得等 500ms 的防抖窗口真到期，而那是真钟——这一包测试里不靠
	// `time.Sleep` 等时间（理由见 internal/app/lyrics_test.go 那一份）。
	putLyrics(s, lyricsOfN(20), "")

	first := s.app.Lyrics().Key
	if !tt.HasText(lyricOffsetLabel(app.LyricsOffsetStepMS * 4)) {
		t.Errorf("第一首的偏移旋钮上不是 %q，文字有 %q",
			lyricOffsetLabel(app.LyricsOffsetStepMS*4), tt.Texts())
	}

	// 换到第二首：没有缓存，于是两个旋钮都回到默认值。
	openAnotherTrack(t, s)
	settleLyrics(t, s, tt)
	if got := s.app.Lyrics().OffsetMS; got != 0 {
		t.Errorf("换歌之后偏移是 %d，want 0（没有缓存就该是默认值）", got)
	}
	if got := s.app.Lyrics().FontSize; got != app.DefaultLyricsFontSize {
		t.Errorf("换歌之后字号是 %v，want %v", got, app.DefaultLyricsFontSize)
	}

	// 再换回第一首：它自己那份还在（后台读盘是异步的，等它回来）。
	openATrack(t, s)
	s.app.Pause()
	deadline := time.Now().Add(2 * time.Second)
	for {
		tt.Frame()
		if s.app.Lyrics().FontSize == 33 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了两秒，读回来的字号是 %v，want 33（缓存没恢复）",
				s.app.Lyrics().FontSize)
		}
		time.Sleep(time.Millisecond)
	}
	if got := s.app.Lyrics().Key; got != first {
		t.Fatalf("换回来之后缓存键是 %q，want %q", got, first)
	}
	if got := s.app.Lyrics().OffsetMS; got != app.LyricsOffsetStepMS*4 {
		t.Errorf("换回来之后偏移是 %d，want %d", got, app.LyricsOffsetStepMS*4)
	}
}

// openAnotherTrack 换到**另一首**歌（不同的缓存键）。
//
// ⚠️ 必须是另一个文件：同一首歌再开一次，缓存键不变，而那一票（`setLyricTrack`）
// 对同一首是直接返回的——「同一首重报不重置」是它刻意的行为（换流对用户来说是
// 「歌没断过」）。拿同一个文件去试，测的是那一条，不是切歌。
func openAnotherTrack(t *testing.T, s *Shell) {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "mono16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到第二个 FLAC 测试素材：%v", err)
	}
	if err := s.app.Open(p); err != nil {
		t.Fatal(err)
	}
	s.app.Pause()
}

// ─────────────────────────────────────────────────────────────────────────────
// 偏移与字号两个旋钮
// ─────────────────────────────────────────────────────────────────────────────

// TestTheTwoKnobsAreThereOnlyWithLyrics 两个旋钮在，有歌词的时候。
func TestTheTwoKnobsAreThereOnlyWithLyrics(t *testing.T) {
	s, tt := lyricShell(t)

	// 没有歌词：一个旋钮都不摆（它们无处可附，见 lyrics.go 里 viewLyrics 上面的
	// 那一条），而且照实说「暂无歌词」。
	if _, ok := tt.Find(lyricFontKnobName); ok {
		t.Errorf("没有歌词却画出了字号旋钮，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find(lyricOffsetKnobName); ok {
		t.Errorf("没有歌词却画出了偏移旋钮，文字有 %q", tt.Texts())
	}
	if !tt.HasText(noLyrics.Title) {
		t.Errorf("没有歌词的时候没说 %q，文字有 %q", noLyrics.Title, tt.Texts())
	}

	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	if _, ok := tt.Find(lyricFontKnobName); !ok {
		t.Errorf("有歌词却没有字号旋钮，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find(lyricOffsetKnobName); !ok {
		t.Errorf("有歌词却没有偏移旋钮，文字有 %q", tt.Texts())
	}
}

// TestTheKnobShowsTheCurrentValue 旋钮上写的就是当前那个值。
func TestTheKnobShowsTheCurrentValue(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	if !tt.HasText(lyricFontLabel(app.DefaultLyricsFontSize)) {
		t.Errorf("字号旋钮上没有 %q，文字有 %q",
			lyricFontLabel(app.DefaultLyricsFontSize), tt.Texts())
	}
	if !tt.HasText(lyricOffsetLabel(0)) {
		t.Errorf("偏移旋钮上没有 %q，文字有 %q", lyricOffsetLabel(0), tt.Texts())
	}
}

// TestTheFontKnobGoesFromTwelveToFortyEight 字号滑块走的是 12~48。
//
// 抄上游 `font-size-control.tsx:16` 的 `min = 12, max = 48`。范围**不在界面上
// 定死**：滑块的两个端点是从底下那一层取的（`app.MinLyricsFontSize` /
// `app.MaxLyricsFontSize`），真正写进去的是 `app.SetLyricsFontSize`，由它自己夹。
func TestTheFontKnobGoesFromTwelveToFortyEight(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	openLyricPopover(t, s, tt, lyricFontKnobName)

	dragLyricSlider(t, tt, lyricFontSliderKey, 0)
	if got := s.app.Lyrics().FontSize; got != app.MinLyricsFontSize {
		t.Errorf("拖到底：字号 %v，want %v", got, float32(app.MinLyricsFontSize))
	}
	dragLyricSlider(t, tt, lyricFontSliderKey, 1)
	if got := s.app.Lyrics().FontSize; got != app.MaxLyricsFontSize {
		t.Errorf("拖到顶：字号 %v，want %v", got, float32(app.MaxLyricsFontSize))
	}
}

// TestTheOffsetKnobGoesFromMinusFiveThousandToFiveThousand 偏移滑块走的是 ±5000、
// 步长 50。
//
// 抄上游 `offset-control.tsx:18` 的 `min = -5000, max = 5000` 和它上面那行的
// `step = 50`。三个数**全部**从底下那一层取，界面上一个都不自己写。
func TestTheOffsetKnobGoesFromMinusFiveThousandToFiveThousand(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	openLyricPopover(t, s, tt, lyricOffsetKnobName)

	dragLyricSlider(t, tt, lyricOffsetSliderKey, 0)
	if got := s.app.Lyrics().OffsetMS; got != app.MinLyricsOffsetMS {
		t.Errorf("拖到底：偏移 %d，want %d", got, app.MinLyricsOffsetMS)
	}
	dragLyricSlider(t, tt, lyricOffsetSliderKey, 1)
	if got := s.app.Lyrics().OffsetMS; got != app.MaxLyricsOffsetMS {
		t.Errorf("拖到顶：偏移 %d，want %d", got, app.MaxLyricsOffsetMS)
	}

	// 步长：拖到中间，值必须落在 50 的倍数上。
	// ⚠️ 不能拿「除以 50 是整数」当断言的全部——那是说 0 也算，而 0 恰好是
	// 拖到正中间时的值。所以先离开正中间一点，再量。
	dragLyricSlider(t, tt, lyricOffsetSliderKey, 0.4)
	got := s.app.Lyrics().OffsetMS
	if got%app.LyricsOffsetStepMS != 0 {
		t.Errorf("拖到 40%%：偏移 %d，不是 %d 的倍数", got, app.LyricsOffsetStepMS)
	}
	if got != -1000 {
		t.Errorf("拖到 40%%：偏移 %d，want -1000（[-5000, 5000] 的 40%% 是 -1000）", got)
	}
}

// TestTheOffsetMovesWhichLineIsSung 偏移真的挪得动「哪一句正在唱」。
//
// ⚠️ 这一条是偏移唯一能被用户看见的地方：旋钮上的数改了而歌词不动，用户会以为
// 那个旋钮是坏的。正数 = 歌词整体往后挪，于是同一个播放位置落在**更早**的一句上。
func TestTheOffsetMovesWhichLineIsSung(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	big := lyricRowAt(t, tt, 8)
	if math.Abs(float64(big.H)-(lyricsActiveScale*app.DefaultLyricsFontSize*lyricsLineHeight)) > 1 {
		t.Fatalf("第 8 句还没有被放大（高 %v），后面那一步就验不到", big.H)
	}

	// 往后挪 400ms = 两句：同一个位置落回第 6 句。
	s.app.SetLyricsOffset(-2 * lyricStepMS)
	settleLyrics(t, s, tt)

	box := boxOf(s)
	row := lyricRowAt(t, tt, 6)
	if math.Abs(float64(row.Y+row.H/2-(box.Y+box.H/2))) > 1 {
		t.Errorf("偏移 -400ms 之后，第 6 句的中心在 %.2f，容器中心在 %.2f",
			row.Y+row.H/2, box.Y+box.H/2)
	}
	if h := lyricRowAt(t, tt, 6).H; h <= lyricRowAt(t, tt, 7).H {
		t.Errorf("偏移之后被放大的不是第 6 句（它 %v 高，第 7 句 %v 高）",
			h, lyricRowAt(t, tt, 7).H)
	}
}

// openLyricPopover 点开那一个浮层并等它画出来。
func openLyricPopover(t *testing.T, s *Shell, tt *mygoui.Tester, knob string) {
	t.Helper()
	if err := tt.Click(knob); err != nil {
		t.Fatalf("点不开 %q：%v（文字有 %q）", knob, err, tt.Texts())
	}
	settleLyrics(t, s, tt)
}

// dragSliderTo 把那条滑块拖到 0（底）或 1（顶）或者中间某个比例。
func dragLyricSlider(t *testing.T, tt *mygoui.Tester, slider string, f float32) {
	t.Helper()
	r, ok := tt.Find(slider)
	if !ok {
		t.Fatalf("那条滑块 %q 不在画面上", slider)
	}
	// f 是「从底边往上」的比例：0 是底（最小值）、1 是顶（最大值）。
	// ⚠️ 上下各留半个钮（`.PaddingY(thumb/2)`），所以内容框比整块矮一个钮。
	y := r.Y + r.H - float32(lyricSliderThumb)/2 - (r.H-float32(lyricSliderThumb))*f
	dragLyricSliderAt(t, tt, r, y)
}

// dragLyricSliderTo 把那条滑块拖到**某一个具体的值**上。
//
// ⚠️ 不能拿比例代替：字号 12~48 与偏移 ±5000 是两套范围，同一个比例落到两边是
// 两个完全不同的数，而「字号 12 到 48 走一遍」这条要的就是那几个确切的数。
func dragLyricSliderTo(t *testing.T, tt *mygoui.Tester, slider string, want float32) {
	t.Helper()
	r, ok := tt.Find(slider)
	if !ok {
		t.Fatalf("那条滑块 %q 不在画面上", slider)
	}
	var lo, hi float32
	switch slider {
	case lyricFontSliderKey:
		lo, hi = app.MinLyricsFontSize, app.MaxLyricsFontSize
	case lyricOffsetSliderKey:
		lo, hi = float32(app.MinLyricsOffsetMS), float32(app.MaxLyricsOffsetMS)
	default:
		t.Fatalf("不认识的那条滑块 %q", slider)
	}
	at := (want - lo) / (hi - lo)
	y := r.Y + r.H - float32(lyricSliderThumb)/2 - (r.H-float32(lyricSliderThumb))*at
	dragLyricSliderAt(t, tt, r, y)
}

// dragLyricSliderAt 在滑块里按下、松手，y 是按住的那个高度。
//
// ⚠️ 按下和松手之间要 `Advance`：MyGo 的动画状态挂在控件的节点上，而无头 Tester
// 的钟**只有 `Advance(d)` 才走**（同 settings_tabs_test.go 里那一份的注释）。
func dragLyricSliderAt(t *testing.T, tt *mygoui.Tester, r mygoui.Rect, y float32) {
	t.Helper()
	x := r.X + r.W/2
	tt.Press(x, y)
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
	tt.Release(x, y)
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
}

// ─────────────────────────────────────────────────────────────────────────────
// 抄来的每一个数
// ─────────────────────────────────────────────────────────────────────────────

// TestTheUpstreamNumbersAreTheOnesWeUse 每一个抄来的数都对着上游那一行。
//
// ⚠️ 这些断言里的数**一律写死**，不写成 `want := lyricsLineGap + 2*lyricsLinePadY`
// 那种拿生产代码的常量核对生产代码算出来的东西的写法——那种写法常量一改它跟着改，
// 永远绿。这一条要绿，得有人去改上面那些常量。
func TestTheUpstreamNumbersAreTheOnesWeUse(t *testing.T) {
	for _, c := range []struct {
		what      string
		got, want float64
	}{
		// index.tsx:335 `fontSize * 1.5`
		{"放大倍数 lyricsActiveScale", lyricsActiveScale, 1.5},
		// index.tsx:333 `opacity-60` / `opacity-100`
		{"没在唱的不透明度 lyricsIdleOpacity", lyricsIdleOpacity, 0.6},
		{"在唱的不透明度 lyricsActiveOpacity", lyricsActiveOpacity, 1},
		// index.tsx:331 `py-2`（0.5rem）、index.tsx:365 `space-y-2`（0.5rem）
		{"每句上下的留白 lyricsLinePadY", lyricsLinePadY, 8},
		{"句与句之间的缝 lyricsLineGap", lyricsLineGap, 8},
		// index.tsx:331 `leading-snug`
		{"行高倍数 lyricsLineHeight", lyricsLineHeight, 1.375},
		// index.tsx:355 `max-w-4xl`（56rem）
		{"歌词区最宽 lyricsMaxWidth", lyricsMaxWidth, 896},
		// index.tsx:344 `mt-1`（0.25rem）、`text-sm`（0.875rem / 1.25rem）、
		// `text-white/80`
		{"译文离原词 lyricsTranslationGap", lyricsTranslationGap, 4},
		{"译文字号 lyricsTranslationSize", lyricsTranslationSize, 14},
		{"译文行高 lyricsTranslationLineHeight", lyricsTranslationLineHeight, 20},
		{"译文不透明度 lyricsTranslationOpacity", lyricsTranslationOpacity, 0.8},
		// index.tsx:358 那条 maskImage 的八个色标：上沿到 24%、下沿从 76% 起
		{"上沿带子宽 lyricsFadeTop", lyricsFadeTop, 0.24},
		{"下沿带子宽 lyricsFadeBottom", lyricsFadeBottom, 0.24},
		// offset-control.tsx / font-size-control.tsx
		{"字号步长 lyricFontStep", lyricFontStep, 1},
		{"偏移旋钮离边 lyricKnobInset", lyricKnobInset, 24},
		{"字号滑块高 lyricFontSliderH", lyricFontSliderH, 112},
		{"偏移滑块高 lyricOffsetSliderH", lyricOffsetSliderH, 128},
	} {
		if c.got != c.want {
			t.Errorf("%s 是 %v，want %v", c.what, c.got, c.want)
		}
	}
}

// TestTheRangesComeFromTheStateLayer 范围和步长一个都不在界面上写死。
//
// 票面点名的那一条：「offset / 字号的可调夹界照服务与状态层暴露的来，界面不要
// 自己算」。这里钉的是**界面上没有第二份数**：滑块的两个端点直接取底下那一层的
// 常量，而写进去的是 `app.SetLyricsXxx`——由它自己夹界。
//
// ⚠️ 所以这一条还钉住了「界面夹得比底下松/紧」这一类错：拖到底必须是底下那个
// 端点，一个不多一个不少。
func TestTheRangesComeFromTheStateLayer(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	openLyricPopover(t, s, tt, lyricFontKnobName)
	dragLyricSlider(t, tt, lyricFontSliderKey, 0)
	if got, want := s.app.Lyrics().FontSize, float32(app.MinLyricsFontSize); got != want {
		t.Errorf("界面滑到底给的是 %v，底下那一层的下端点是 %v", got, want)
	}
	// 越界的那一下：绕开界面直接调底下那一层，它自己夹住。
	s.app.SetLyricsFontSize(app.MaxLyricsFontSize + 100)
	if got, want := s.app.Lyrics().FontSize, float32(app.MaxLyricsFontSize); got != want {
		t.Errorf("给了 %v，夹回来的是 %v，want %v", app.MaxLyricsFontSize+100, got, want)
	}
	s.app.SetLyricsOffset(app.MaxLyricsOffsetMS + 1)
	if got := s.app.Lyrics().OffsetMS; got != app.MaxLyricsOffsetMS {
		t.Errorf("给了 %d，夹回来的是 %d，want %d", app.MaxLyricsOffsetMS+1, got, app.MaxLyricsOffsetMS)
	}
	s.app.SetLyricsOffset(app.MinLyricsOffsetMS - 1)
	if got := s.app.Lyrics().OffsetMS; got != app.MinLyricsOffsetMS {
		t.Errorf("给了 %d，夹回来的是 %d，want %d", app.MinLyricsOffsetMS-1, got, app.MinLyricsOffsetMS)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 用户自己翻
// ─────────────────────────────────────────────────────────────────────────────

// TestHandScrollingIsNotFought 用户拿滚轮翻了歌词，下一帧不会被拽回中间。
//
// 症状是「歌词翻不动」：手指还在往上推，内容一帧一帧自己往回跑。抄上游的
// `useEffect(..., [activeIndex, centerPadding])`——它**只在换句时**对一次目标，
// 不是每帧都对着中间。框架那边一收到滚轮就把那支弹簧收走了
// （`mygo/ui/input.go:490`），所以这里不用自己再记一份「用户是不是翻过了」。
func TestHandScrollingIsNotFought(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	seekTo(t, s, tt, 8)

	box := boxOf(s)
	tt.Scroll(box.X+box.W/2, box.Y+box.H/2, 0, -300)
	settleLyrics(t, s, tt)

	mine := s.lyric.scroll.Y
	t.Logf("用户翻到 %.2f", mine)
	if mine <= 0 {
		t.Fatalf("滚轮没翻动（Y=%.2f），后面那一步就验不到", mine)
	}
	// 停在用户翻到的地方，不动。
	for range 4 {
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
		got := s.lyric.scroll.Y
		if got != mine {
			t.Errorf("用户翻到 %.2f 之后它自己走到了 %.2f —— 歌词被拽回去了", mine, got)
		}
		mine = got
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 帧循环
// ─────────────────────────────────────────────────────────────────────────────

// TestTheLyricsStopAskingForFrames 歌词区不会一直要帧。
//
// 两件事：量不到的时候**要**一帧（头一帧必然量不到，而那一帧不来就永远量不到），
// 以及要得**有上限**——不加上限的话，只要歌词区这一帧压根没画出来，它就会一帧
// 一帧要下去，而表现是「已经切走的那一页还在让窗口满速重画」。
func TestTheLyricsStopAskingForFrames(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	// 没有歌词的那一支：那里压根不画歌词区，不许要帧。
	putLyrics(s, "", "")
	settleLyrics(t, s, tt)
	if tt.FramePending() {
		t.Errorf("没有歌词的时候还在要帧，pend=%d", s.lyric.pend)
	}

	// 离页走一趟再回来，确认「不在这一页上」也不会一直要帧。
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	s.navigate("/")
	settleLyrics(t, s, tt)
	if tt.FramePending() {
		t.Errorf("不在歌词页上的时候还在要帧，pend=%d", s.lyric.pend)
	}
}

// TestTheMeasureBudgetIsBounded 那个预算真的只有几帧。
//
// `lyricsView.centre` 在量不到容器和正在唱那一句的框时会要一帧再来量。**这条把
// 「量不到」那个条件直接造出来**——用一份零值的 `lyricsView`（框永远是零），
// 于是它每一帧都在要帧，而 Tester 的 `settle()` 最多跑 20 帧就自己停
// （`mygo/ui/headless.go:246`）。
//
// ⚠️ 为什么要单独造这个条件：从界面上走**逼不进**那个分支——歌词区一画出来，
// 容器和那一句的框第二帧就量到了；而没有歌词那一支压根不调 `centre`。不单独
// 造它，这条断言就只是一句「界面上没观察到问题」，而不是「那个上限在」。
func TestTheMeasureBudgetIsBounded(t *testing.T) {
	var lv lyricsView // 框永远是零 = 永远量不到
	frames := 0
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		frames++
		lv.centre(c, 0, 0, app.DefaultLyricsFontSize, false)
	}, 100, 100)

	// 一共 lyricMeasureFrames+1 帧：pend 从 0 数起，头一帧（pend=0）本身就在预算
	// 里，于是 `pend = 0,1,2,3` 跑满四帧，第 5 帧不再要。
	if want := lyricMeasureFrames + 1; frames > want {
		t.Errorf("量不到的时候要了 %d 帧，want ≤ %d —— 没有上限的话它会一直要下去",
			frames, want)
	}
	if tt.FramePending() {
		t.Errorf("要满了预算之后还在要帧（pend=%d）", lv.pend)
	}
	// 量得到的时候反过来：一帧都不该多要。
	lv.box = mygoui.Rect{Y: 0, W: 400, H: 600}
	lv.line = mygoui.Rect{Y: 300, W: 400, H: 40}
	before := frames
	tt.Frame()
	if got := frames - before; got != 1 {
		t.Errorf("量得到之后跑了一帧要了 %d 次，want 1", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 接上
// ─────────────────────────────────────────────────────────────────────────────

// TestMountLyricsIsWiredBeforeTheWindow 歌词缓存挂在开窗口之前。
//
// `lyrics.NewCache` 要 `MkdirAll`，那是阻塞 IO；放到开窗口那条路上就是每启动
// 一次窗口白等一次。这一条钉住的是 `main.go` 里那一行还在——它不在编译能管到的
// 地方（`ui.Run` 之前那一段是手写的），而少了它歌词区会一直说「暂无歌词」，
// 界面上看不出任何异样。
func TestMountLyricsIsWiredBeforeTheWindow(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	at := strings.Index(text, "ui.MountLyrics(a)")
	if at < 0 {
		t.Fatalf("main.go 里没有 ui.MountLyrics(a) 这一行")
	}
	run := strings.Index(text, "ui.Run(a, sess)")
	if run < 0 {
		t.Fatal("main.go 里没有 ui.Run(a, sess) 那一行")
	}
	if at > run {
		t.Errorf("ui.MountLyrics(a) 排在 ui.Run 之后（第 %d 字节 vs 第 %d 字节）——"+
			"建目录是阻塞 IO，得在开窗口之前", at, run)
	}
}
