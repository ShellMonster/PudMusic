package ui

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一份跑的是 MyGo 的 Tester：歌词是即时模式的，断言只能看**画出来的**东西——哪一
// 行大了、哪一行在中间、译文在不在原词下面、渐隐盖没盖住。只断 App 上的状态那些全是
// 「零件对、装上去不一定对」。

// lyricLines 是测试用的歌词一共有几句。比一屏放得下的多，好让「滚到中间」有事可做，
// 也好让最后一句能被滚到**下沿遮罩**那个高度上（居中留白之外，每一句能到的最低
// 位置都被它自己的位置限死）。
const lyricLines = 17

// lineLines 是歌词的最后一句。
const lineLines = lyricLines

// line 是第 n 句的文字（n 从 1 起）。
func line(n int) string { return fmt.Sprintf("第 %d 句", n) }

// lineAt 是唱到 fraction 处正在被唱的那一句。
//
// ⚠️ 别在断言里写死「第 9 句」：歌词是按 `lyricLines` 平分时间生成的，两边对不上
// 的话测的会是一句**根本没有歌词在唱**的行，而那一行的高度是 0，拿它做除法直接
// 变成 +Inf（实测过：断言报的是「比例不对」，跟真正的原因隔了十万八千里）。
func lineAt(fraction float64) string { return line(lineIndexAt(fraction)) }

// lineIndexAt 是唱到 fraction 处那一句的序号（从 1 起）。
func lineIndexAt(fraction float64) int {
	return int(fraction*float64(lyricLines-1)) + 1
}

// lyricsHarness 是一套跑起来、正放着歌、歌词已经装上的迷你播放器。
type lyricsHarness struct {
	s  *Shell
	tt *mygoui.Tester
}

func newLyricsHarness(t *testing.T) *lyricsHarness {
	t.Helper()
	s, tt := newShell(t)

	path := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	if err := s.app.Open(path); err != nil {
		t.Fatal(err)
	}
	// 暂停，让播放位置钉死在一个数上。这一份测试的每一条断言都是关于
	// 「哪一个位置上的哪一行」的，位置一动结论就跟着动。
	s.app.Toggle()
	s.navigate(miniPlayerPath)
	tt.Frame()
	return &lyricsHarness{s: s, tt: tt}
}

// seek 给歌装上歌词并跳到 fraction。
//
// 时间标签是**照着歌的时长平分**算出来的（`时长 * i / (句数-1)`），不是写死：素材
// 文件多长、拖到几分之几各不相同，写死的话素材一换测试就断，而断的方式是「等了两秒
// 没等到那一行」——看不出是自己写错了还是素材变了。
func (h *lyricsHarness) seek(t *testing.T, fraction float64, withTranslation bool) {
	t.Helper()
	dur := h.s.app.Snapshot().Duration
	if dur <= 0 {
		t.Fatal("没有时长，没法排歌词的时间标签")
	}
	h.s.app.ScrubEnd(fraction)

	var lrc, tLrc strings.Builder
	for i := range lyricLines {
		at := time.Duration(float64(dur) * float64(i) / (lyricLines - 1))
		fmt.Fprintf(&lrc, "[%s]第 %d 句\n", lrcTag(at), i+1)
		if withTranslation {
			fmt.Fprintf(&tLrc, "[%s]第 %d 句的译文\n", lrcTag(at), i+1)
		}
	}
	h.s.app.PutLyrics(lrc.String(), tLrc.String())
	h.tt.Frame()
}

// untilLaidOut 等到 text 那一行的盒子**稳住**（连续两帧一样高）。
//
// 改字号、改偏移之后行高和滚动目标都会变，要一两帧才落定。判据是「盒子的高度不再
// 变」，**不是**「盒子非零」——半行被裁掉时盒子也是非零的，只是很小，等到那一步就收
// 手会量到一个正在移动的值（实测：字号只调三下时量到 5.3，而调八下量到 40）。
//
// 只等这一行稳住，不等整个滚动收敛——后者在 `-race` 下是几十帧。
func (h *lyricsHarness) untilLaidOut(t *testing.T, text string) {
	t.Helper()
	last := float32(-1)
	for range 12 {
		h.tt.Advance(100 * time.Millisecond)
		h.tt.Frame()
		r, ok := h.tt.Find(text)
		if !ok || r.H <= 0 {
			last = -1
			continue
		}
		if r.H == last {
			return
		}
		last = r.H
	}
	t.Fatalf("等了 12 帧，%q 的盒子还没稳住（最后是 %v）", text, last)
}

// seekSettled 是 seek 再加「等滚动走到位」。
//
// ⚠️ **只有量「滚到中间」的那几条才需要它**。其余的断言量的是行高、墨色和两个盒子的
// 相对位置——那些跟滚动到哪儿没关系，而这一等在 `-race` 下是几十帧（每帧要重画整个
// 界面），乘以十几条用例就是好几分钟。
func (h *lyricsHarness) seekSettled(t *testing.T, fraction float64, withTranslation bool) {
	t.Helper()
	h.seek(t, fraction, withTranslation)
	h.settleLyrics()
}

// settleLyrics 推钟直到歌词区不再动为止。
//
// 这一块是靠 `c.After` 自己一帧一帧要下去的（见 followActiveLyric），而「滚到正在唱
// 的那一行」要跨好几跳：量出来 → 算出来 → 滚过去 → 居中留白长出来 → 再滚。一次推进
// 不一定够，不够的时候失败的样子是「歌词停在半路」而不是「没滚」，看着像别的 bug
// （实测：整包一起跑会红，单跑不会）。
//
// ⚠️ **连续两轮没动就停**，不跑满预算。这一条是为了 `-race` 下的耗时：`-race` 里一帧
// 要重画整个界面（顶栏、侧栏、十几行歌词、播放条），一个弹簧 500ms 就是三十帧。跑满
// 8×500ms 等于白烧两百多帧 CPU，而收敛真正需要的只有几十帧——`-race` 下整包多花四分钟。
// 两轮而不是一轮：滚动量「停一下」再「又动一下」是常态（居中留白长出来那一拍就是这样），
// 一轮就收手会在半路上停住。
func (h *lyricsHarness) settleLyrics() {
	quiet := 0
	for range 16 {
		before := h.s.lyricScroll.Y
		h.tt.Advance(200 * time.Millisecond)
		h.tt.Frame()
		switch {
		case h.s.lyricScroll.Scrolling(), h.s.lyricScroll.Y != before:
			quiet = 0
		default:
			if quiet++; quiet >= 2 {
				return
			}
		}
	}
}

// lrcTag 把一个时间写成 LRC 的时间标签。
//
// ⚠️ **必须走它**，不能自己拼 `%02d:%02d`：LRC 的秒只认 1~2 位（上游正则
// `index.tsx:29`），写成 `[00:250.00]` 的那十几行会**整行被丢掉**——解析不出来的东西
// 在界面上看不出任何异样，歌词区少一大截而日志里一个字都没有。
func lrcTag(d time.Duration) string {
	return fmt.Sprintf("%02d:%02d.%03d",
		int(d.Minutes()), int(d.Seconds())%60, int(d/time.Millisecond)%1000)
}

// rectOf 是画面上某个文字的盒子，找不到就当场失败。
func rectOf(t *testing.T, h *lyricsHarness, text string) mygoui.Rect {
	t.Helper()
	r, ok := h.tt.Find(text)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", text, h.tt.Texts())
	}
	return r
}

// boxMid 是歌词区正中间那一行的 y。
func (h *lyricsHarness) boxMid() float32 { return h.s.lyricBoxY + h.s.lyricBoxH/2 }

// TestTheLineBeingSungIsBigger 盯住「逐行高亮，字号放大 1.5 倍」（上游 index.tsx:335
// 的 `fontSize: isActive ? fontSize * 1.5 : fontSize`）。
//
// 断的是**两个盒子的比例**，不是「某一行存在」：倍数不对的时候那行照样画在屏幕上、
// 一个字不少，只有量出来才知道。取个宽区间是因为字形本身也占着高度。
func TestTheLineBeingSungIsBigger(t *testing.T) {
	h := newLyricsHarness(t)
	h.seek(t, 0.5, false)

	singing, idle := lineAt(0.5), line(lineIndexAt(0.5)+1)
	sH, iH := rectOf(t, h, singing).H, rectOf(t, h, idle).H
	if sH <= iH {
		t.Fatalf("正在唱的（%s）高 %v，没在唱的（%s）高 %v——没放大", singing, sH, idle, iH)
	}
	if got := sH / iH; got < 1.4 || got > 1.6 {
		t.Errorf("行高之比是 %v，want 1.5", got)
	}
}

// TestTheIdleLinesAreDimmed 钉住其余各行那 0.6 的不透明度（上游 index.tsx:333 的
// `opacity-60`）。
//
// 量的是「字最深的那一个像素离背景有多远」——那一个像素就是那行字的实际颜色（字有
// 描边，中文 20px 的笔画里总有几个像素是满色的），于是这条断言量的正是
// `正文色 × 0.6 压在背景上` 有多深。
//
// ⚠️ 这里写死的是一个**量出来的**区间（浅色主题、正文色 #18181b），不是拿
// `lyricsIdleOpacity` 这个常量算出来的：拿常量核对常量，常量一改它跟着改，永远绿。
func TestTheIdleLinesAreDimmed(t *testing.T) {
	h := newLyricsHarness(t)
	// 这一条要**滚动落到位**：墨色是量出来的，而量到之前它头顶/脚底那两条渐隐还压在
	// 那一行上（实测没落位时第 10 句只有 0.16 的墨——量到的全是渐隐，不是那 0.6）。
	h.seekSettled(t, 0.5, false)

	// 紧挨着正在唱那一行的上下两句：它们在渐隐条之外（渐隐只占上下各 24%），
	// 量到的就是纯粹的 0.6。
	sung := lineIndexAt(0.5)
	for _, n := range []int{sung - 1, sung + 1} {
		ink := inkOf(h.tt.Image(), rectOf(t, h, line(n)))
		if ink < 0.45 || ink > 0.65 {
			t.Errorf("%s 的墨是 %.2f，want 约 0.54（正文色 0.6 不透明度压在背景上）", line(n), ink)
		}
	}
	// 正在唱的那一句是满色的，比旁边那句明显深。
	if got, idle := inkOf(h.tt.Image(), rectOf(t, h, line(sung))), 0.54; got < 0.8 {
		t.Errorf("正在唱的%s的墨只有 %.2f（旁边那句约 %.2f）——没有满色", line(sung), got, idle)
	}
}

// TestTheLinesAreSpacedEightApart 钉住 `space-y-2` 和 `py-2`（上游 index.tsx:331-336）。
//
// 量的是「上一句字盒的底边到下一句字盒的顶边」，那一条缝里有三样东西：上一行的下留白
// + 行间距 + 下一行的上留白 = 8 + 8 + 8 = 24。
//
// ⚠️ 24 **写死**，不写成 `lyricsLineGap + 2*lyricsLinePadY`：那样就是拿生产代码的
// 常量核对生产代码画出来的东西，常量一改它跟着改，永远绿。
func TestTheLinesAreSpacedEightApart(t *testing.T) {
	h := newLyricsHarness(t)
	h.seek(t, 0.5, false)

	const want = 24 // py-2 的 8 + space-y-2 的 8 + py-2 的 8
	a, b := rectOf(t, h, line(3)), rectOf(t, h, line(4))
	if gap := b.Y - (a.Y + a.H); absf(gap-want) > 1 {
		t.Errorf("两行之间的缝是 %v，want %v（`py-2` + `space-y-2` + `py-2`）", gap, want)
	}
}

// TestTheLineBeingSungFollowsTheSong 是「高亮跟着唱的位置走」。只钉住一个时刻的高亮
// 不够：行高比钉住的是静态那一帧，而**会不会换行**才是唱 K 时一直盯着看的那件事。
func TestTheLineBeingSungFollowsTheSong(t *testing.T) {
	h := newLyricsHarness(t)
	h.seek(t, 0.5, false)

	// 拖回开头：第一句该被唱到了，原来那一句该缩回去。
	h.s.app.ScrubEnd(0)
	h.tt.Frame() // 只看行高，不用等滚动走到位

	first, wasSung := rectOf(t, h, lineAt(0)).H, rectOf(t, h, lineAt(0.5)).H
	if first <= wasSung {
		t.Errorf("拖回开头之后%s高 %v，%s高 %v——高亮没跟着歌走",
			lineAt(0), first, lineAt(0.5), wasSung)
	}
}

// TestTheTranslationSitsUnderItsLine 盯住「翻译行叠在原词下方」（上游 index.tsx:343-346：
// 译文是原词那个 div 的**下一个兄弟**，在它下面）。
//
// 断的是两个盒子的**相对位置**，不是「译文出现了」：叠在同一行上（有人会图省事画成
// 一句「原文 / 译文」）那样也画得出译文。
func TestTheTranslationSitsUnderItsLine(t *testing.T) {
	h := newLyricsHarness(t)
	h.seek(t, 0.5, true)

	original := rectOf(t, h, lineAt(0.5))
	translated := rectOf(t, h, lineAt(0.5)+"的译文")

	if translated.Y <= original.Y {
		t.Errorf("译文在上方（%.1f），原词在下方（%.1f）——译文要叠在原词下面", translated.Y, original.Y)
	}
	// 挨着：中间的缝就是那个 `mt-1`（4px）。容差 1px：字盒底边本来就比笔画底边
	// 低一点，所以量出来正好是 4。放宽到 3 倍的话，写成 8（`space-y-2` 那个数）
	// 也过得去——而那正是「行间距」和「译文间距」被抄串了的样子。
	if gap := translated.Y - (original.Y + original.H); absf(gap-lyricsTranslationGap) > 1 {
		t.Errorf("译文离原词 %v，want %v（`mt-1`）", gap, lyricsTranslationGap)
	}
	// 同一侧。译文不是从左边缘另起一段排的（那是 `ml-` 的做法，上游没有）。
	if x := translated.X - original.X; x < -1 || x > original.W {
		t.Errorf("译文横向偏了 %v，原词宽 %v", x, original.W)
	}
}

// TestTheSungLineScrollsToTheMiddle 盯住「平滑滚动到中间」。断的是**位置**不是「有没有
// 滚动」：歌词只要有滚动条就说明它在滚，可滚到哪儿全凭这一条。
func TestTheSungLineScrollsToTheMiddle(t *testing.T) {
	h := newLyricsHarness(t)
	h.seekSettled(t, 0.5, false)

	if h.s.lyricBoxH <= 0 {
		t.Fatal("歌词区没有高度，量不了中间")
	}
	if got := h.s.lyricScroll.Y; got <= 0 {
		t.Errorf("滚动量是 %v，一点都没往下滚——%s 在中间偏下，本该往下滚", got, lineAt(0.5))
	}

	// 容差用容器高的 2%：滚动是个弹簧，落定的那一帧差零点几是常事，
	// 而差一像素在 588 高的容器上是 0.17%——卡太紧就成了量浮点数。
	for _, line := range []string{"第 9 句"} {
		r := rectOf(t, h, line)
		if d := r.Y + r.H/2 - h.boxMid(); absf(d) > h.s.lyricBoxH*0.02 {
			t.Errorf("%s 的中心离中间 %v（容器的 %.1f%%），没滚到中间去",
				line, d, 100*absf(d)/h.s.lyricBoxH)
		}
	}
}

// TestTheFirstLineCanAlsoReachTheMiddle 盯住居中留白。没有上下留白的话内容比容器短、
// 滚动量被夹在 0，于是第一句永远贴着顶——「唱到第一句时它没居中」看着就是歌词坏了：
// 那一行和别的行长得一模一样，只是它没跟着走。
func TestTheFirstLineCanAlsoReachTheMiddle(t *testing.T) {
	h := newLyricsHarness(t)
	h.seekSettled(t, 0, false)

	if h.s.lyricScroll.MaxY <= 0 {
		t.Fatalf("内容没有比容器高（MaxY=%v），第一句滚不到中间", h.s.lyricScroll.MaxY)
	}
	r := rectOf(t, h, lineAt(0))
	if d := r.Y + r.H/2 - h.boxMid(); absf(d) > h.s.lyricBoxH*0.02 {
		t.Errorf("%s 的中心离中间 %v（容器的 %.1f%%）——第一句滚不到中间",
			lineAt(0), d, 100*absf(d)/h.s.lyricBoxH)
	}
}

// TestTheLastLineCanAlsoReachTheMiddle 是同一条的另一半：第一句能到中间，
// 最后一句也得能——留白是上下各给一半的，只验上面那一头的话，下面给 0 也过得去。
func TestTheLastLineCanAlsoReachTheMiddle(t *testing.T) {
	h := newLyricsHarness(t)
	// 时长打满，最后一句正在被唱。
	h.seekSettled(t, 1, false)

	r := rectOf(t, h, line(lyricLines))
	if d := r.Y + r.H/2 - h.boxMid(); absf(d) > h.s.lyricBoxH*0.02 {
		t.Errorf("%s 的中心离中间 %v（容器的 %.1f%%）——最后一句滚不到中间",
			line(lyricLines), d, 100*absf(d)/h.s.lyricBoxH)
	}
}

// TestTheFadeFadesWhatScrollsUnderIt 盯住上下那两条渐隐遮罩
// （上游 index.tsx:357-360 的 maskImage）。
//
// ## 它盯的到底是什么
//
// 上游那条 maskImage 的 alpha **0 是「藏起来」、1 是「原样透出来」**，所以它的意思是
// **紧贴窗口上下边缘的歌词被藏起来，中间那一段原样显示**。本文件画的是一块**盖板**
// （背景色的渐变条压在歌词上面），于是「盖住的程度」= 1 - mask 的 alpha：
//
//	上沿（占歌词区 0%~24%）：盖住 1 → 0.85 → 0.5 → 0
//	下沿（占歌词区 76%~100%）：盖住 0 → 0.5 → 0.85 → 1
//
// **写反过一次**：把 mask 的 alpha 当成了盖板的 alpha，于是**紧贴窗口边缘的歌词满色
// 被硬切、条子内侧反而淡到看不见**（现象是「76% 处消失、99% 处又恢复满色」）。当时
// 这条测试还只量了上半区、又把倒过来的方向写成了期望值，所以那个倒置它不但抓不到、
// 还判成对的。现在上下两条都量，方向也摆正了。
//
// ## 七个采样点，三种改法各有断言红
//
//	边（0.01 / 0.99）几乎全被盖住   → 拿掉渐隐、或把盖板铺满，两者都红
//	中（0.12 / 0.88）半蒙           → 换成一块纯色盖板的话这里是 0，红
//	内（0.23 / 0.77）不盖           → 方向写反的话这里变成「几乎全被盖住」，红
//
// 上下用**不同的行**：上沿那三个采样点用正在被唱的那一句（它能滚到最上面），下沿那三个
// 用最后一句（它能滚到最下面）——居中留白之外，每一句能到的最高/最低位置都被它自己
// 的位置限死，拿一句量两端是量不到的。
func TestTheFadeFadesWhatScrollsUnderIt(t *testing.T) {
	h := newLyricsHarness(t)
	h.seekSettled(t, 0.5, false)
	if h.s.lyricBoxH <= 0 {
		t.Fatal("歌词区没有高度")
	}

	// full 是「一点没被盖住」时一行歌词的墨色，拿紧挨着正在唱那一行的下一句量
	// （它停在歌词区的中间，上下两条遮罩都盖不到它）。
	full := inkOf(h.tt.Image(), rectOf(t, h, line(lineIndexAt(0.5)+1)))
	if full < 0.3 {
		t.Fatalf("中间那一行几乎没有墨（%.2f），这条测试量不出渐隐", full)
	}

	ink := map[string]float64{}
	for _, c := range []struct {
		label string
		line  string
		frac  float32
	}{
		{"边·上", lineAt(0.5), 0.01},     // 几乎全被盖住
		{"中·上", lineAt(0.5), 0.12},     // 半蒙
		{"内·上", lineAt(0.5), 0.23},     // 不盖
		{"内·下", line(lineLines), 0.77}, // 不盖
		{"中·下", line(lineLines), 0.88}, // 半蒙
		{"边·下", line(lineLines), 0.99}, // 几乎全被盖住
	} {
		ink[c.label] = h.measureAt(t, c.line, c.frac)
	}

	// 边：紧贴窗口边缘的那一头，歌词要**看不见**。
	if v := ink["边·上"]; v > full*0.15 {
		t.Errorf("上沿最外侧还有 %.2f 的墨（没被盖住时是 %.2f）——紧贴窗口边缘的歌词应该藏起来", v, full)
	}
	if v := ink["边·下"]; v > full*0.15 {
		t.Errorf("下沿最外侧还有 %.2f 的墨（没被盖住时是 %.2f）——紧贴窗口边缘的歌词应该藏起来", v, full)
	}
	// 内：遮罩靠里那一头，歌词要**原样透出来**。
	if v := ink["内·上"]; v < full*0.85 {
		t.Errorf("上沿最内侧只剩 %.2f 的墨（没被盖住时是 %.2f）——遮罩盖过头了", v, full)
	}
	if v := ink["内·下"]; v < full*0.85 {
		t.Errorf("下沿最内侧只剩 %.2f 的墨（没被盖住时是 %.2f）——遮罩盖过头了", v, full)
	}
	// 中：两头之间是**渐变**的。一块纯色盖板在这里会是 0，而「拿掉渐隐」这里是满的——
	// 这两条各自抓一种改法。
	if v := ink["中·上"]; v <= ink["边·上"]*2 || v >= full*0.8 {
		t.Errorf("上沿正中的墨是 %.2f，要落在边（%.2f）和没被盖住（%.2f）之间才对",
			v, ink["边·上"], full)
	}
	if v := ink["中·下"]; v <= ink["边·下"]*2 || v >= full*0.8 {
		t.Errorf("下沿正中的墨是 %.2f，要落在边（%.2f）和没被盖住（%.2f）之间才对",
			v, ink["边·下"], full)
	}
}

// measureAt 把 text 那一行挪到歌词区 frac 那个高度上，量它的墨色。
func (h *lyricsHarness) measureAt(t *testing.T, text string, frac float32) float64 {
	t.Helper()
	r, ok := h.tt.Find(text)
	if !ok || r.H <= 0 {
		t.Fatalf("画面上找不到 %q（或它还没有盒子），文字有 %q", text, h.tt.Texts())
	}
	// 往下滚 d，正文就往上走 d，所以「把它挪到 want 那个高度」要滚 `现在 - 想要`。
	h.s.lyricScroll.Y += r.Y + r.H/2 - (h.s.lyricBoxY + frac*h.s.lyricBoxH)
	h.tt.Frame()

	r, ok = h.tt.Find(text)
	if !ok || r.H <= 0 {
		t.Fatalf("挪到 %.0f%% 之后 %q 仍然没有盒子", frac*100, text)
	}
	// 量这一句**正中间**那一小条：它落在同一个渐变位置里，整条的墨色才是同一个数。
	// 量整句的话上下半截分处遮罩内外，量出来的是没被盖住的那一半。
	//
	// ⚠️ 只量**左边这一段**：右下角浮着偏移和字号两个控件，量到整行的宽度就会把
	// 控件的边框和字一起算进去，而它们是实心的——下沿那几个采样点正好落在它们上面，
	// 量出来的「墨」比没被盖住时还多（实测 0.89，而没被盖住是 0.54）。
	band := mygoui.Rect{X: r.X, Y: r.Y + r.H/2 - 5, W: min(r.W, 400), H: 10}
	return inkOf(h.tt.Image(), band)
}

// TestTheFadeDoesNotSwallowTheWheel 盯住上下那两条盖板**不吃滚轮**。
//
// 即时模式的命中链按画出来的顺序排，而那两条盖板画在滚动区**之后**，于是它们排在
// 命中链前面：歌词区上下各 24%（合计约一半面积）拿滚轮翻不动，只有中间那条有反应。
// 自动跟随不受影响（那是程序自己调 ScrollTo），受影响的是**用户手动翻歌词找词**——
// 而那正是渐隐要服务的场景。
func TestTheFadeDoesNotSwallowTheWheel(t *testing.T) {
	h := newLyricsHarness(t)
	h.seekSettled(t, 0.5, false)
	if h.s.lyricBoxH <= 0 {
		t.Fatal("歌词区没有高度")
	}
	mid := h.s.lyricBoxX + h.s.lyricBoxW/2

	// 上沿那条盖板里面、里面那条（本来就该动）、下沿那条盖板里面。
	for _, frac := range []float32{0.12, 0.5, 0.88} {
		before := h.s.lyricScroll.Y
		h.tt.Scroll(mid, h.s.lyricBoxY+frac*h.s.lyricBoxH, 0, -120)
		h.tt.Frame()
		if got := h.s.lyricScroll.Y; got == before {
			t.Errorf("在歌词区 %.0f%% 高度上滚滚轮没动（滚动量还是 %v）——"+
				"上面那两条渐隐盖板把滚轮吃了", frac*100, got)
		}
	}
}

// TestTheRetryBudgetForAnUnreachableTarget 盯住「目标够不着时别把帧率一直拖在 60 上」
// 那道保险（followActiveLyric 里的 `lyricStuck`）。
//
// 怎么把它逼进那个分支：用**三句**歌词（内容比容器矮得多，`MaxY` 是 0），再把
// `lyricBoxH` / `lyricLineH` 清成 0——居中留白于是永远是 0，三句挤在顶上，正在被
// 唱的那一句落在容器中心**上方**，要往下滚才回得来，而 `MaxY` 是 0 滚不下去。于是
// `target = y + off` 恒为负，「够不着」每一帧都成立。
//
// 判据是「要了几帧」：够不着的时候**每一帧都该要下一帧**（留白和内容随时可能长出来），
// 但**最多要 `lyricsStuckTries` 帧**，然后停下来。保险失效的症状是「永远停在 1」——
// 每帧先被清零再 +1，`c.After` 一直发，而画面上什么都看不出来。
func TestTheRetryBudgetForAnUnreachableTarget(t *testing.T) {
	h := newLyricsHarness(t)
	h.s.app.PutLyrics("[00:00.00]一\n[00:01.00]二\n[00:02.00]三\n", "")
	h.tt.Frame()

	// 居中留白永远算成 0（`lyricPadForced` 就是干这个的），三句于是挤在顶上，
	// 正在被唱的那一句落在容器中心上方，要往下滚才回得来，而 `MaxY` 是 0 滚不下去。
	// `lyricPadH` 也摆成 0，让「留白变了」这个重置条件从第二帧起就是假的——它要真的
	// 在「够不着」的状态里把预算用完，而不是每帧被清零。
	zero := float32(0)
	h.s.lyricPadForced, h.s.lyricPadH = &zero, 0

	for range lyricsStuckTries + 2 {
		h.tt.Frame()
	}
	// 预算要**用满**：用满 = 「这一支真的在数」。保险失效时它每帧被清零再 +1，
	// 永远停在 1，而 1 < 上限，所以只断「不超过上限」是抓不住它的。
	if got := h.s.lyricStuck; got != lyricsStuckTries {
		t.Errorf("目标够不着 %d 帧之后预算停在 %d，want %d——每帧先清零再 +1 的话它会"+
			"永远停在 1，`c.After` 一直发，帧率一直掉在 60 上而画面上什么都看不出来",
			lyricsStuckTries, got, lyricsStuckTries)
	}
	// 预算用完之后就不再要帧了：再画一帧，计数不会涨。
	before := h.s.lyricStuck
	h.tt.Frame()
	if got := h.s.lyricStuck; got != before {
		t.Errorf("预算用完之后计数还在涨（%d → %d）——它每帧都在要下一帧", before, got)
	}
}

// inkOf 是这一块里「墨最深」的那个像素离背景有多远，0 是完全空白，1 是纯黑字。
//
// 用「最深」而不是「平均」：字是描出来的，一个字框里大部分像素是空的，平均值会被行高
// 稀释——放大 1.5 倍之后同一句话的平均墨色也会变大，混进要量的东西里。
func inkOf(img *image.RGBA, r mygoui.Rect) float64 {
	bg := img.RGBAAt(4, 4)
	x0, y0 := int(r.X), int(r.Y)
	x1, y1 := int(r.X+r.W), int(r.Y+r.H)
	if x0 < 0 || y0 < 0 || x1 > img.Bounds().Dx() || y1 > img.Bounds().Dy() {
		return 0
	}
	worst := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			d := absInt(int(c.R)-int(bg.R)) + absInt(int(c.G)-int(bg.G)) + absInt(int(c.B)-int(bg.B))
			worst = max(worst, d)
		}
	}
	// 3 个通道各满 255，所以纯黑字在纯白底上是 765。
	return float64(worst) / 765
}

func absInt(a int) int { return max(a, -a) }

// TestNoLyricsSaysSo 盯住空状态。「没有歌词」是一个**有前提**的状态：得先有歌在放，
// 才谈得上「这首歌没有歌词」；而这一票的歌词只从缓存来（源是 #21），缓存里没有就是
// 这一支。
func TestNoLyricsSaysSo(t *testing.T) {
	h := newLyricsHarness(t)

	if !h.tt.HasText(noLyrics.Title) {
		t.Errorf("没歌词时应该说「%s」，文字有 %q", noLyrics.Title, h.tt.Texts())
	}
	// 控件不画：没有歌词的时候那两个控件按下去什么也不会发生，摆着两个不能按的
	// 空壳比不摆更让人以为是自己点错了地方。
	if _, ok := h.tt.Find("歌词偏移"); ok {
		t.Error("没有歌词却把「歌词偏移」摆出来了")
	}
	if _, ok := h.tt.Find("歌词字号"); ok {
		t.Error("没有歌词却把「歌词字号」摆出来了")
	}
}

// TestNoSongIsNotNoLyrics 是那条前提的反面：没打开任何文件时迷你播放器上**不能**出现
// 「暂无歌词」——那句话和屏幕上的事实对不上。
func TestNoSongIsNotNoLyrics(t *testing.T) {
	s, tt := newShell(t)
	s.navigate(miniPlayerPath)
	tt.Frame()

	if tt.HasText(noLyrics.Title) {
		t.Errorf("还没打开任何文件就说「暂无歌词」，文字有 %q", tt.Texts())
	}
	if !tt.HasText(nothingPlaying.Title) {
		t.Errorf("没歌时该说「%s」，文字有 %q", nothingPlaying.Title, tt.Texts())
	}
}

// TestTheTwoKnobsAreThereWithTheUpstreamLabels 盯住两个控件和它们旁边那两句字
// （`offset-control.tsx:68` 的 `+N ms` 和 `font-size-control.tsx:66` 的 `Npx`）。
func TestTheTwoKnobsAreThereWithTheUpstreamLabels(t *testing.T) {
	h := newLyricsHarness(t)
	h.seek(t, 0.5, false)

	for _, want := range []string{"歌词偏移", "歌词字号"} {
		if _, ok := h.tt.Find(want); !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", want, h.tt.Texts())
		}
	}
	if !h.tt.HasText("+0 ms") {
		t.Errorf("偏移那一格没有按上游写成「+0 ms」，文字有 %q", h.tt.Texts())
	}
	if !h.tt.HasText(fmt.Sprintf("%dpx", int(app.DefaultLyricsFontSize))) {
		t.Errorf("字号那一格没写出默认字号，文字有 %q", h.tt.Texts())
	}
}

// TestTheOffsetKnobDrivesTheSungLine 盯住偏移那个控件**接到了 App 上**。点的是数字框
// 旁边的「+」——`ui.NumberInput` 自带那个加号按钮（`ui/number.go:66`），一次加一个
// step。断的是 App 上的偏移真的变了：能看见两个数字框不等于点得动它们。
func TestTheOffsetKnobDrivesTheSungLine(t *testing.T) {
	h := newLyricsHarness(t)
	h.seek(t, 0.5, false)

	// 画面上第一个 Increase 是**偏移**那行的（偏移行画在字号行上面）。
	if err := h.tt.Click("Increase"); err != nil {
		t.Fatalf("点不到偏移那个加号：%v", err)
	}
	h.tt.Frame()

	if got := h.s.app.Lyrics().OffsetMS; got != app.LyricsOffsetStepMS {
		t.Errorf("点一下「+」之后偏移是 %d，want %d（步长抄自上游 offset-control.tsx:20）",
			got, app.LyricsOffsetStepMS)
	}
	if !h.tt.HasText(fmt.Sprintf("+%d ms", app.LyricsOffsetStepMS)) {
		t.Errorf("旁边的字没跟着变，文字有 %q", h.tt.Texts())
	}

	// 一步就钉住了步长（50 是死的，`offset-control.tsx:20`）。⚠️ **不要多按**：
	// `Tester.Click` 每点一下都跑一次 settle，而 `-race` 下一帧要重画整个界面；
	// 一次和五次钉住的是同一件事，代价差五倍。
	if err := h.tt.Click("Increase"); err != nil {
		t.Fatal(err)
	}
	if got := h.s.app.Lyrics().OffsetMS; got != 2*app.LyricsOffsetStepMS {
		t.Errorf("一共点两下之后偏移是 %d，want %d（步长抄自上游 offset-control.tsx:20）",
			got, 2*app.LyricsOffsetStepMS)
	}
}

// TestTheFontSizeKnobResizesTheLines 盯住字号那个控件**接到了 view 上**。断的是**画出
// 来的字变大了**，不是 App 上的数变了：数变了而画法没跟着变的话，用户拖滑块什么也看
// 不出来——那正是「控件摆在那儿但没接线」的样子。
func TestTheFontSizeKnobResizesTheLines(t *testing.T) {
	h := newLyricsHarness(t)
	h.seek(t, 0.5, false)
	before := rectOf(t, h, lineAt(0.5)).H
	// ⚠️ 先确认量到的**不是 0**。`seek` 不等滚动落位，那一句有可能还没被画出来
	// （盒子是 0），而「0 <= 0」是假——后面那条「有没有变大」就永远绿了（实测：
	// 把字号从 App 接到 view 那一行断掉，这条照样绿）。
	if before <= 0 {
		t.Fatalf("改之前 %s 就没有盒子（高 %v），后面没法比", lineAt(0.5), before)
	}

	// ⚠️ **点那个控件，不直接调 App**。直接调 App 的话，把 view 里那一行
	// `s.app.SetLyricsFontSize(...)` 删掉这条照样绿——「控件摆在那儿、点了没反应」
	// 就是这么漏出去的（实测：删掉之后这条还是绿的）。
	//
	// 点的是数字框本身再按 ↑（`ui/number.go:56` 的 `in.Shortcut(0, KeyUp)` 走的是
	// 焦点在那个输入框上时的按键）。不用点旁边的「+」按钮：两个 NumberInput 都有，
	// 按钮的无障碍名字都叫 `Increase`，而 `Tester.Find` 只给第一个。
	box, ok := h.tt.Find("歌词字号")
	if !ok {
		t.Fatalf("画面上找不到「歌词字号」那个控件，文字有 %q", h.tt.Texts())
	}
	// ⚠️ **只按三下，不要按更多**：`Tester.Key` 每按一下都跑一次 settle，而 `-race` 下
	// 一帧要重画整个界面。三下和二十下钉住的是同一件事（`step = 1`），代价差七倍。
	h.tt.ClickAt(box.X+12, box.Y+box.H/2)
	for range 3 {
		h.tt.Key(0, mygoui.KeyUp)
	}
	h.tt.Frame()

	// 三下 ↑ 正好 +3：顺带钉住了 `font-size-control.tsx:18` 那个 `step = 1`
	//（步长改成 5 的话三下是 +15，这条就红）。
	if got := h.s.app.Lyrics().FontSize; got != 20+3 {
		t.Fatalf("按了三下 ↑ 之后字号是 %v，want 23（控件没接到 App 上，或步长不是 1？）", got)
	}
	// 字号一变，行高和滚动目标都跟着变，要一两帧才落定——落定的标志就是这一行有盒子。
	h.untilLaidOut(t, lineAt(0.5))

	if got := rectOf(t, h, lineAt(0.5)).H; got <= before {
		t.Errorf("字号从 20 调到 23 之后%s还是高 %v（调之前 %v）——控件没接到 view 上",
			lineAt(0.5), got, before)
	}
}

// TestTheOffsetMovesTheSungLine 盯住偏移本身：它是**加在播放位置上**的（上游
// index.tsx:46 的 `currentTime * 1000 + offset`）。断的是「调了偏移，高亮换到了别的
// 那一句」——不是「某个数变了」，后者在控件没接到界面上的时候也是绿的。
func TestTheOffsetMovesTheSungLine(t *testing.T) {
	h := newLyricsHarness(t)
	h.seek(t, 0.5, false)

	// 每句隔时长/(句数-1)，往前挪一句的偏移就该换一句。
	// ⚠️ 偏移的单位是**毫秒**（上游 `formatLabel` 写的是 ms），拿 duration 的
	// 纳秒去填会被 ±5000 那道夹子夹成 -5000ms——于是「往前挪一句」变成「往前挪
	// 十句」，第一句都滚出去了，断言看着像偏移没生效，其实是自己算错了单位。
	h.s.app.SetLyricsOffset(-int(h.s.app.Snapshot().Duration/time.Millisecond) / (lyricLines - 1))
	// 高亮换到了上一句，滚动也跟着往回走，等那一行有盒子。
	h.untilLaidOut(t, line(lineIndexAt(0.5)-1))

	back, was := line(lineIndexAt(0.5)-1), lineAt(0.5)
	if rectOf(t, h, back).H <= rectOf(t, h, was).H {
		t.Errorf("往前挪一句的偏移之后%s高 %v，%s高 %v——高亮没跟着偏移走",
			back, rectOf(t, h, back).H, was, rectOf(t, h, was).H)
	}
}

// TestTheCacheIsWhatFeedsTheView 把整条链串一遍：缓存里有一首歌的歌词 → 打开那一首
// → 迷你播放器上画出来。
//
// 前面的用例都是直接 `PutLyrics` 塞进去的，验的是「有了歌词画得对不对」。这一条验的是
// **歌词真的到得了 view**：中间隔着一次后台读盘和一次唤醒（铁律三）。少了唤醒，表现
// 是歌词永远停在「暂无歌词」而没有任何报错。
func TestTheCacheIsWhatFeedsTheView(t *testing.T) {
	// ⚠️ 缓存要在**打开那首歌之前**挂上（`MountLyrics` 会补读当前这一首，挂晚了就
	// 不会回头再读一次），而歌词要在**打开之后**才有地方可附。所以这一条不走
	// `newLyricsHarness`：那一个已经把「打开 + 暂停 + 切页」做掉了。
	//
	// ⚠️ `t.TempDir()` 必须在 `t.Cleanup(a.Close)` **之前**调：cleanup 是后进先出，
	// 反过来的话临时目录先被删掉，`a.Close()` 里那次「退出前把欠着的偏移落完」就往
	// 一个已经不存在的目录里写，日志里全是它。
	dir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	a.MountLyrics(lyrics.NewCache(filepath.Join(dir, "lyrics")))

	path := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	if err := a.Open(path); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyrics(sampleLRC(), "")

	s := New(a)
	t.Cleanup(s.images.Close)
	// 和 newShell 一样把那几个后台 goroutine 收掉（见 router_test.go 里那段）。
	t.Cleanup(func() { _ = s.hist.Close() })
	t.Cleanup(func() { _ = s.keys.Close() })
	t.Cleanup(s.feed.disarm)
	s.router.Transition = mygoui.TransitionNone
	tt := mygoui.NewTester(s.View, 1200, 800)

	s.navigate(miniPlayerPath)
	tt.Frame()
	// 歌词是后台读缓存再唤醒界面进来的（铁律三），所以要等它到。
	deadline := time.Now().Add(3 * time.Second)
	for !tt.HasText(line(1)) && time.Now().Before(deadline) {
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if !tt.HasText(line(1)) {
		t.Fatalf("缓存里有歌词，等了 3 秒它也没到界面上，文字有 %q", tt.Texts())
	}
	if tt.HasText(noLyrics.Title) {
		t.Errorf("缓存里有歌词，界面上还写着「暂无歌词」，文字有 %q", tt.Texts())
	}
}

// sampleLRC 是一段与歌无关的固定歌词，给「缓存 → view」那条链用。
//
// ⚠️ 时间标签必须走 `lrcTag`（见那一条），不能自己拼。
func sampleLRC() string {
	var b strings.Builder
	for i := range lyricLines {
		fmt.Fprintf(&b, "[%s]第 %d 句\n", lrcTag(time.Duration(i)*10*time.Second), i+1)
	}
	return b.String()
}
