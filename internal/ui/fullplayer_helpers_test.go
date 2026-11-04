// 全屏播放器这一组测试共用的帮手（原来散在已经按旧设计删掉的几个测试文件里，别的测试
// 文件——动画、剪贴板、EQ、分集抽屉、迷你窗——也在用它们）。
//
// 走 MyGo 的 Tester：和真窗口一模一样的 layout 与 paint，只是画到内存里。
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// 几个写死的字面量，不去用 fullplayer.go 里的同名常量：常量一改两边一起跟着变，测试就发现不了了。
const (
	biuRowCoverSize = 52 // 播放条封面 52×52
	// biuHideSeconds 只剩一些动画测试在拿它当「推多久的钟」用；满屏的「三秒藏界面」已随旧设计删掉。
	biuHideSeconds = 3
)

// fullWin 是测试窗口的尺寸（newShell 用的也是这个）。
const (
	fullWinW = 1200
	fullWinH = 800
)

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// openFull 点开全屏（点播放条上那张封面）。
func openFull(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	row, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("找不到封面，文字有 %q", tt.Texts())
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if !tt.HasText("收起满屏播放器") {
		t.Fatalf("全屏没开，文字有 %q", tt.Texts())
	}
}

// whitePixels 是画面上「接近白」的像素个数。
//
// 为什么是数白像素而不是找某一个元素的框：界面藏起来之后那些元素**还在**，
// 只是画不出来（`Opacity(0)`）。所以「元素还在」证明不了任何事，「一个亮像素都
// 没有」才算。
//
// 为什么白色只可能来自界面：这一组测试一律把主题设成深色（见 fullTester 的调用
// 处），于是主题自己的 Surface / Text 都是深的，满屏背景是 `Theme().Background`
// 再压一层 55% 的黑，封面占位是 `t.Surface`。画面上唯一亮的东西就是界面自己
// 画的字和控件。
func whitePixels(tt *mygoui.Tester) int {
	img := tt.Image()
	n := 0
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R > 190 && p.G > 190 && p.B > 190 {
				n++
			}
		}
	}
	return n
}

// fullTester 建一个开着歌的界面，**假时钟**已经换上。
//
// 为什么换假时钟：那三秒必须是假时钟测的（`PLAN.md` 的验收项），而真等三秒一条
// 测试就是三秒。
func fullTester(t *testing.T) (*Shell, *mygoui.Tester, *fakeClock) {
	t.Helper()
	s, tt := newShell(t)
	// 🔴 在建 Tester **之前**换上：Tester 建好就跑了一帧，那一帧已经按真时钟把
	// 「最后一次指针移动」记成墙钟了，换晚了三秒就成了三秒加一大截。
	fc := newFakeClock(time.Unix(1_700_000_000, 0))
	s.clock = fc

	// 🔴 一律深色主题：`whitePixels` 数的是「画面上接近白的像素」，而浅色主题下
	// 满屏背景本身就是白的，藏没藏界面数出来都一样（实测藏起来还有 11 万个）。
	// 深色下画面上唯一亮的东西就是界面自己画的字和控件——见 whitePixels。
	tt.SetDark(true)

	if err := s.app.Enqueue(app.LocalItem(flacPath(t))); err != nil {
		t.Fatalf("把歌放进队列：%v", err)
	}
	// 等播放器把歌打开（异步，见 playbar_test.go 的 waitFor）。本地的 Open 是
	// 同步的，一帧就够。
	tt.Frame()
	if !s.app.Snapshot().HasTrack {
		t.Fatalf("歌没开起来：%q", s.app.Snapshot().Err)
	}
	return s, tt, fc
}

// advanceFull 把两个钟都往前推。
//
// ⚠️ **两个都要推**：`s.clock` 是界面层那个（判三秒），Tester 自己还有一个
// （跑动画）。只推一个的话，要么三秒到不了、要么动画停在半路——而动画停在半路
// 的时候画面上仍然有字，看着像「没藏起来」。
func advanceFull(s *Shell, tt *mygoui.Tester, fc *fakeClock, d time.Duration) {
	fc.Advance(d)
	tt.Advance(d)
}

// settleFull 推钟直到框架不再要帧（动画走完了）。
//
// 用的是仓库自己的写法（见 settings_test.go 的 settleSettingsPage）：
// **`tt.Advance(d)` 配 `tt.FramePending()`**，不数帧。Tester 的钟站着不动，
// 数帧那种等法会静默失效（CLAUDE.md §2）。
//
// ⚠️ **满屏的封面在转的时候，这一条等不到**（票 #468）：那一转是**常驻**的，
// 框架于是**一直要帧**——而本应用的「看不见就不画」那道闸只挡应用自己发的
// 唤醒，挡不住框架发的（`fullplayer_spin.go` 那一段有细说）。
//
// 所以这里**推满 1.28 秒就收工**（满屏那个共享元素转场是条弹簧，约 500ms；
// 1.28s 是它原来的上限），而**不再把「还在要帧」当错**。
//
// ⚠️ **「动画停不下来」这件事并没有因此没人管**：共享元素转场停不下来的话，
// 量它落位的那些用例（`TestTheFullPlayerSplitsTheWindowInHalf` 等）会量到
// 错的位置——它们照样红。
func settleFull(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	const step = 16 * time.Millisecond
	for range 80 {
		tt.Advance(step)
		tt.Frame()
		if !tt.FramePending() {
			return
		}
	}
	t.Log("推了 1.28s 的钟，界面还在要帧——多半是满屏的封面在转（常驻动画）")
}

// splitCoverPNG 编一张左右对半（黑 | 白）的 PNG，当封面用。
//
// 为什么是**硬边**：毛玻璃糊的就是边，而硬边糊开之后那一段是渐变的——「有没有
// 糊」于是有一个能量的判据（相邻两像素的差）。
func splitCoverPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				img.Set(x, y, color.RGBA{A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// whitePNG 编一张纯白的 PNG，当封面用（底色最亮的那一档）。
func whitePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// multiPages 是一份**三 P** 的回包。
//
// 之所以要造一份：三 P 才有「选」的意思，Biu 的入口条件是
// `playItem.hasMultiPart`（`index.tsx:404`），而默认那份 fixture 只回一集。
func multiPages() []any {
	return []any{
		map[string]any{"cid": 777, "page": 1, "part": "第一集", "duration": 260},
		map[string]any{"cid": 778, "page": 2, "part": "第二集", "duration": 300},
		map[string]any{"cid": 779, "page": 3, "part": "", "duration": 280},
	}
}

// multiParts 是 multiPages 的强类型版本（`App.Parts` 给回来的就是它）。
func multiParts() []bili.VideoPart {
	return []bili.VideoPart{
		{Cid: "777", Page: 1, Part: "第一集"},
		{Cid: "778", Page: 2, Part: "第二集"},
		{Cid: "779", Page: 3, Part: ""},
	}
}

// onlineTester 建一个真的在放**在线**歌的界面（挂着一台假 B 站）。
//
// 为什么不是本地文件：分集是 B 站的概念，`App.Parts` 判的是「当前项是 mv」。
//
// blockPages 是「让分集问不到」（顶掉 pagelist）。⚠️ 队列里那一项**自带 cid**，
// 于是放歌那一步的 `withCid` 压根不问分 P（见 online.go），被顶掉的只有
// `App.Parts` 那一问——正好造出「歌放得起来、可是分集问不到」这个状态。
//
// opts 往后传给基座（`partdrawer_test.go` 用它换窗口尺寸）。前面三个参数不动，
// 是为了让满屏那几条用例一行都不用改。
func onlineTester(t *testing.T, pages []any, blockPages bool, opts ...shellOpt) (*Shell, *mygoui.Tester) {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.setVideoPages(pages)
	if blockPages {
		serve := f.Server.Config.Handler
		f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/x/player/pagelist" {
				http.Error(w, "风控", http.StatusForbidden)
				return
			}
			serve.ServeHTTP(w, r)
		})
	}

	s, tt := newShell(t, opts...)
	tt.SetDark(true)
	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})

	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVmultiPart",
		// ⚠️ 标题给的是一条**长**的：播放条那一行的宽度账全靠它才量得出来
		// （见 `partdrawer_test.go` 的 `TestThePartsKeyDoesNotBreakThePlayBar`）。
		// 满屏那几条不关心标题，所以换成长的那条对它们没有影响。
		Cid: "777", Title: "一首歌的名字很长很长很长很长",
		Artist: "一个非常非常非常非常非常非常长的 UP 主名字", ArtistMid: 12345,
	}); err != nil {
		t.Fatalf("把在线的那首放进队列：%v", err)
	}
	waitFrames(t, tt, "在线那首开始播", func() bool { return s.app.Snapshot().HasTrack })
	return s, tt
}

// farthestFrom 是 box 里离 bg 最远的那一个像素——**图形本身**。
//
// ⚠️ **不能改成「找最白的那个」**：主色当字色那一档在浅色下是**比底色还暗**的
// 深绿，那一格里面最白的像素反而是底色。
func farthestFrom(img *image.RGBA, box mygoui.Rect, bg color.RGBA) color.RGBA {
	best, bestD := bg, 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if d := maxDiff(p, bg); d > bestD {
				best, bestD = p, d
			}
		}
	}
	return best
}

// snapshot 是这一帧画面的一份拷贝。
//
// `tt.Image()` 给的是宿主内部那张图自己，直接留到下一帧会被覆盖。
func snapshot(tt *mygoui.Tester) *image.RGBA {
	src := tt.Image()
	cp := image.NewRGBA(src.Rect)
	copy(cp.Pix, src.Pix)
	return cp
}

// changedPixels 是两帧之间颜色变了的那些像素。
func changedPixels(a, b *image.RGBA) []image.Point {
	var out []image.Point
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
			if color.RGBAModel.Convert(a.RGBAAt(x, y)) != color.RGBAModel.Convert(b.RGBAAt(x, y)) {
				out = append(out, image.Pt(x, y))
			}
		}
	}
	return out
}

// fullTestLRC 是这一组测试用的那份歌词：四句，各带一个时间戳。
//
// ⚠️ **句数要够**：一屏放得下十来句才看得出「滚到中间」那件事，而四句是
// 「一屏放得下」的最小值——再多几句，这一组测试就慢起来了。
const fullTestLRC = "[00:00.00]第一句\n[00:01.00]第二句\n[00:02.00]第三句\n[00:03.00]第四句\n"

// fullLyricsTester 造一个开着歌、**暂停着**、挂着假歌词源的外壳 + Tester。
//
// ⚠️ **假源是必须的**：满屏那个「找歌词」的键会真的去搜（那是它存在的意义），
// 而不挂源的话那一次搜索会打到真网上去——验收一律走 fixture（AGENTS §6）。
//
// ⚠️ **暂停是必须的**：歌在放的时候播放位置一直在走，于是「正在唱的是第几句」
// 每隔一秒换一句，而换一句就要重新对一次滚动的目标——界面于是永远在要帧
// （`settleFull` 等不到它停）。歌词那一列的滚动要停下来，位置就得停住。
func fullLyricsTester(t *testing.T) (*Shell, *mygoui.Tester, *fakeClock) {
	t.Helper()
	s, tt, fc := fullTester(t)
	s.app.Pause()
	s.app.SetWake(s.wake.invalidate)
	s.app.MountLyricSources(lrclibSource(cand("1", "第一句")))
	return s, tt, fc
}

// fullNoLyricsPrefs 把「显示歌词」关掉——满屏那一屏于是不画歌词。
//
// ⚠️ **这是给「量界面」的那些测试用的**：歌词是**内容**，界面藏起来之后它还在
// （见 `TestTheLyricsStayWhenTheInterfaceHides`），而那些测试量的是「界面上还
// 有没有亮像素」——歌词的字也在那儿，会把量出来的东西搅浑。
func fullNoLyricsPrefs(t *testing.T, s *Shell) {
	t.Helper()
	p := app.DefaultFullPlayerPrefs()
	p.ShowLyrics = false
	s.app.SetFullPlayerPrefs(p)
}

// knobHalves 数开关那一个框的左右两半里各有多少个亮像素（旋钮是白的）。
//
// # 🔴 **一像素的抗锯齿要算在它该在的那半边**
//
// 开关那个框的 X **可能是小数**：它锚在满屏顶栏那枚齿轮上（`viewFullTopBar`
// 的 `ui.Popover`），而齿轮的 X 是「左边距 + 关闭键 + 缝 + **歌名的宽度** + 缝」
// ——歌名的宽度是字体量出来的小数（实测 75.45732），于是齿轮落在 x=147.45732、
// 开关落在 x=420.45734。
//
// 切口中线于是是 438.45734，而旋钮最左边那一列（x=438）**画在右半边**（旋钮的
// 左沿正好压在中线上）——按「x < 中线就算左半边」判，那一列被算进了左半边，于是
// 「旋钮整个在右半边」这一条判据在**画面是对的**的情况下红。
//
// 所以判据是「**这一像素大部分落在哪半边**」：只有它整个（`x+1`）还在中线左边
// 才算左半边。刀口那一列算右边——它挨着旋钮，画的是旋钮，不是左半边。
//
// ⚠️ 这**不是**把判据放宽：关着的状态仍然要求左半边**一个**亮像素都没有（旋钮
// 整个在左边），开着的那一帧仍然要求左半边一个都没有。改的只是「刀口上那一列
// 归谁」。
func knobHalves(img *image.RGBA, box mygoui.Rect) (left, right int) {
	mid := box.X + box.W/2
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if p.R < 190 || p.G < 190 || p.B < 190 {
				continue
			}
			// 这一像素整个还在中线左边 → 左半边；否则（右半边，含刀口那一列）。
			if float32(x)+1 <= mid {
				left++
			} else {
				right++
			}
		}
	}
	return left, right
}
