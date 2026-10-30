package ui

// 票 #673 的界面那一半：**滚过一百多首之后，最早那几张封面的像素被放掉，
// 滚回去还能正确画出来**。
//
// # 为什么这一层也要一条
//
// 缓存自己那一层（`widgets/imgcache/release_test.go`）已经钉住了「连续 N 帧没
// 被要就放像素、再要时重新解码」。这一条验的是**真的在滚的界面**：
//
//   - `BeginFrame` 是 `Shell.viewMain` 每帧调的（没有它，释放这一档压根不跑）；
//   - 每一格封面每一帧都走 `imgcache.View` → `GetSized`（画面上那几张因此
//     永远 `onScreen`，不会被放掉）；
//   - 滚过去之后那些封面**不再被要**，于是它们才轮到释放。
//
// 少任何一环，缓存那一层的测试都是绿的，而真机上一张都放不掉。
//
// # 判据
//
//	滚过 200 首之后   内存账远低于 200 张全留着（释放真的发生了）
//	滚回顶上          那一格画出来的是**封面**（不是骨架屏），而且内存账又涨回去
//	                   （重新解码真的发生了）
//
// ⚠️ **不用「刚 Get 一定 Loading」当判据**：重新解码跑在后台 worker 上，一次
// `Get` 之内可能已经解完——那样的断言会在快机器上偶发（缓存那一层用的是
// `entry.released` 这个确定性的标记，这里没法用：`entry` 不在这个包里）。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shimmer"
)

// flatPNG 编一张纯色 PNG。纯色是为了「滚回来那一格画的是不是这张图」能用**一个
// 像素**判：首页那一屏的布局量出来之前，渐变封面取样点容易落在字上。
func flatPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestScrollingPastManyCoversReleasesTheOldOnes 是这一票的界面那一半。
//
// 200 首、每首一张 200×200 的纯色封面（解码后约 213KB），全留着约 42.7MB。
// 缓存的预算给到 64MiB，所以**账目回落只可能来自释放**，不是 LRU 挤的。
func TestScrollingPastManyCoversReleasesTheOldOnes(t *testing.T) {
	const (
		songs = 200
		side  = 200
	)
	first := color.RGBA{R: 220, G: 60, B: 60, A: 0xff}
	bodies := make([][]byte, songs)
	for i := range bodies {
		// 第一张是「一眼认得出来」的红，其余各不相同。
		c := color.RGBA{R: uint8(30 + i), G: uint8(200 - i/2), B: uint8(i * 3), A: 0xff}
		if i == 0 {
			c = first
		}
		bodies[i] = flatPNG(t, side, side, c)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, err := strconv.Atoi(r.URL.Path[1:])
		if err != nil || i < 0 || i >= songs {
			http.NotFound(w, r)
			return
		}
		w.Write(bodies[i])
	}))
	defer srv.Close()

	// 每一首都带自己的封面地址（不同 URL = 不同的缓存条目）。
	ms := homeSongsOf("长滚", songs)
	for i := range ms {
		ms[i].Cover = srv.URL + "/" + strconv.Itoa(i)
	}
	b := newTestShell(t)
	s, tt := b.s, b.tt
	s.images.SetMaxBytes(64 << 20) // 200 张全留着也装得下：回落只能来自释放
	s.home.tab = 0
	s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: ms}})
	homeFrame(tt)
	settleHome(tt)

	// 等这一屏的封面都到齐（到齐之前退回占位是正常的：那一张还没下完）。
	//
	// ⚠️ **每帧都要推钟**（`tt.Advance`）：`BeginFrame` 是按**时刻**去重的
	// （同一帧的几遍 build 只算一帧），而 Tester 的钟**站着不动**——只调
	// `tt.Frame()` 的话帧号永远停在 1，「连续 N 帧没人要」这个判据压根不会前进
	// （实测：不推钟时这张测试滚到底也一张都放不掉）。生产上钟一直在走。
	waitVisibleCovers := func() {
		deadline := time.Now().Add(20 * time.Second)
		for {
			tt.Advance(16 * time.Millisecond)
			tt.Frame()
			// ⚠️ 只问**画面上真的画着的那几行**：列表是虚拟的，没画到的不会被要。
			pending := 0
			for i := 1; i <= songs; i++ {
				r, ok := tt.Find(songName("长滚", i))
				if !ok || r.W == 0 || r.H == 0 {
					continue
				}
				if !s.images.Get(ms[i-1].Cover).Ready() {
					pending++
				}
			}
			if pending == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("等了 20s，画面上还有 %d 格封面没出来（文字有 %q）", pending, tt.Texts())
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	waitVisibleCovers()

	if !s.images.Get(ms[0].Cover).Ready() {
		t.Fatalf("第一首的封面没出来——这一条量不下去")
	}
	loadedAtStart := s.images.UsedBytes()
	if loadedAtStart == 0 {
		t.Fatal("一屏封面都到齐了，内存账却是 0")
	}

	// 滚到列表尽头。每一次滚动之后等这一屏的封面都到齐——不然量到的是
	// 「还没下完」而不是「已经放掉」。
	const step = 1200
	var peak int64
	for range 40 {
		tt.Scroll(homeW/2, homeH/2, 0, step)
		homeFrame(tt)
		waitVisibleCovers()
		if u := s.images.UsedBytes(); u > peak {
			peak = u
		}
		if _, ok := tt.Find(songName("长滚", songs)); ok {
			break
		}
	}
	if _, ok := tt.Find(songName("长滚", songs)); !ok {
		t.Fatalf("一直滚也没到最后一首（文字有 %q）", tt.Texts())
	}

	perCover := int64(side*side*4) * 4 / 3
	t.Logf("滚到第 %d 首：账目峰值 %.1fMB、当前 %.1fMB（%d 张全留着是 %.1fMB）",
		songs, float64(peak)/(1<<20), float64(s.images.UsedBytes())/(1<<20),
		songs, float64(perCover*songs)/(1<<20))

	// 滚到尽头之后**再走一段帧**：释放的判据是「连续 N 帧没人要」
	// （`DefaultReleaseAfterFrames` = 120），而滚完那一刻前面的封面才刚闲置
	// 几十帧。真机上用户滚到底之后停在那儿看，这几秒就是这段时间。
	//
	// ⚠️ **画帧必须推钟**（理由见 `waitVisibleCovers`）。
	for range 200 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}
	if got := s.images.UsedBytes(); got > 16<<20 {
		t.Errorf("滚到尽头、又等了 200 帧之后还持有 %.1fMB（%d 张全留着是 %.1fMB）——"+
			"滚过去的封面没有被放掉", float64(got)/(1<<20), songs, float64(perCover*songs)/(1<<20))
	}
	if s.images.Get(ms[0].Cover).Ready() {
		t.Error("第一首已经滚出视口一百多首了，它的像素还挂着")
	}

	// 滚回顶上：那一格要重新画出封面（不是骨架屏），账也要涨回去。
	releasesBefore, redecodesBefore := s.images.ReleaseStats()
	tt.Scroll(homeW/2, homeH/2, 0, -100000)
	homeFrame(tt)
	waitVisibleCovers()
	if !s.images.Get(ms[0].Cover).Ready() {
		t.Fatal("滚回顶上之后第一首的封面没有恢复")
	}
	releases, redecodes := s.images.ReleaseStats()
	t.Logf("放掉 %d 次像素，其中重新解码 %d 次（滚到尽头之前：%d / %d）",
		releases, redecodes, releasesBefore, redecodesBefore)
	if redecodes <= redecodesBefore {
		t.Errorf("滚回来之后重新解码计数没有涨（%d → %d）——"+
			"那一格不是重新解出来的", redecodesBefore, redecodes)
	}
	if got := s.images.UsedBytes(); got == 0 {
		t.Errorf("滚回顶上之后内存账还是 0 字节——没有重新解码")
	}

	// 逐像素：那一格画的是封面，不是骨架屏。
	//
	// ⚠️ **判据从主题现算**（同 `coverPixelShowsTheImage`）：上一版抄过一个
	// 常数，配方一改它就静默为绿。
	//
	// ⚠️ **在整行里扫，不硬编码列偏移**：列表行的左端依次是勾选框（没装多选时
	// 不画）、序号、封面，而每一列的宽是布局量出来的——写死一个偏移的话，将来
	// 改一次内边距它就静默地量到页面底色上。这一条找的是「这一行里有几个像素
	// 和封面纯色对得上」，有就是画出来了。
	rect, ok := tt.Find(songName("长滚", 1))
	if !ok || rect.W <= 0 {
		t.Fatalf("滚回顶上之后找不到第一首（文字有 %q）", tt.Texts())
	}
	theme := BuildTheme(s.app.SettingsSnapshot(), false)
	img := tt.Image()
	cy := int(rect.Y) + int(rect.H/2)
	x0 := max(0, int(rect.X))
	x1 := min(img.Bounds().Dx()-1, int(rect.X+rect.W))
	if cy < 0 || cy >= img.Bounds().Dy() || x1 <= x0 {
		t.Fatalf("取样行 %d 不在画面上（行盒子是 %+v）", cy, rect)
	}
	skeleton := rgbaOf(theme.Text.Alpha(shimmer.BaseAlpha))
	covers, skeletons := 0, 0
	for x := x0; x <= x1; x++ {
		got := img.RGBAAt(x, cy)
		if near(got, skeleton) {
			skeletons++
		}
		// `colorDistance` 是 `toast_test.go` 那个（逐通道差之和）。
		if colorDistance(got, first) < 30 {
			covers++
		}
	}
	t.Logf("滚回来那一行（y=%d）里有 %d 个像素是封面纯色 %v，%d 个是骨架屏",
		cy, covers, first, skeletons)
	if covers == 0 {
		t.Errorf("滚回来那一行里一个封面色 %v 的像素都没有——"+
			"那一格没有重新解出来（画面上是骨架屏 %v，共 %d 个像素）", first, skeleton, skeletons)
	}
}
