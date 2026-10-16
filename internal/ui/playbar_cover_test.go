// 播放条上那张封面的四个状态，各摆一次，外加唤醒那一条链路。
//
// 断言的是**画面上画出来的东西**——哪一块方框、多大、摆在哪儿、上面是什么字。
// SPEC 的 Testing Decisions 说得很清楚：UI 测试断言渲染结果，不断言「调了哪个
// 函数」。所以这里量的是像素和位置，不是往 imgcache 里塞东西然后问它返回什么。
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// coverColor 是测出来的封面用的颜色：画面上别处不会出现，一扫就能找到它画在哪。
var coverColor = color.RGBA{R: 255, G: 0, B: 128, A: 255}

const (
	// 播放条那一行控件加起来就要 600 出头，窗口得比它宽，不然布局会缩。
	coverWinW = 760
	coverWinH = 240
)

// coverTester 起一个只画播放条的窗口，状态由测试给。
//
// 为什么不是 s.View：那条路的状态是 app.Snapshot() 给的，而封面地址今天只能
// 由队列项带下来（本地文件是空的，见 app.State.CoverURL）。要摆出「有地址」
// 这两种状态，就得自己把状态递进去。viewPlayBar 是 viewMain 直接调的那一个
// 函数，走它并不绕开生产代码。
func coverTester(t *testing.T, st app.State) (*Shell, *mygoui.Tester) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)

	tt := mygoui.NewTester(func(c *mygoui.Context) {
		s.wake.set(c) // 和 Shell.View 每帧开头做的事一样，唤醒要用它
		s.viewPlayBar(c, st)
	}, coverWinW, coverWinH)
	return s, tt
}

// ---- 四个状态 ----

// TestPlayBarCoverWithNoURLIsABlankBoxIsCoverStateEmpty：没有地址 → 画一个空框。
//
// ⚠️ 这不是「加载失败」。imgcache 的 drawCover 里这是分开的两支：空地址压根
// 没有东西可显示，失败是有个地址但取不回来。这里两样都断：文字上不出现占位也
// 不出现失败，网络上一个请求都不该发。
func TestPlayBarCoverWithNoURLIsABlankBox(t *testing.T) {
	// 状态里带上一个地址、又指望它别被用上，是自相矛盾的。所以这里就是不给地址。
	_, tt := coverTester(t, app.State{HasTrack: true, Title: "本地的一首歌", CoverURL: ""})

	if tt.HasText("载入中") {
		t.Errorf("没有地址不该画占位，文字有 %q", tt.Texts())
	}
	if tt.HasText("封面加载失败") {
		t.Errorf("没有地址被画成了加载失败，文字有 %q", tt.Texts())
	}

	// 空框也占地方：歌名照样从封面右边 8px 开始（16 + 56 + 8）。
	coverBox, ok := tt.Find("本地的一首歌")
	if !ok {
		t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
	}
	if coverBox.X != 16+56+8 {
		t.Errorf("歌名从 x=%v 开始，应该是 16+56+8=80", coverBox.X)
	}
}

// TestPlayBarCoverWhileLoadingShowsThePlaceholder 是载入中：占位。
func TestPlayBarCoverWhileLoadingShowsThePlaceholder(t *testing.T) {
	g := newGatedCover(t)
	_, tt := coverTester(t, app.State{HasTrack: true, Title: "在线的一首歌", CoverURL: g.url()})

	// 服务端卡着不回，第一帧一定是占位。
	if !tt.HasText("载入中") {
		t.Fatalf("图没来之前应该画占位，文字有 %q", tt.Texts())
	}
	if tt.HasText("封面加载失败") {
		t.Fatalf("还没下完就画了失败态，文字有 %q", tt.Texts())
	}

	waitCover(t, 5*time.Second, "封面请求发出去了", func() bool { return g.hits.Load() > 0 })

	// 一直卡着就一直停在占位——这一条同时钉住「占位不是失败」。
	tt.Frame()
	if !tt.HasText("载入中") {
		t.Errorf("图还没来，占位没了，文字有 %q", tt.Texts())
	}
}

// TestPlayBarCoverShowsTheImageWhenItIsReady 是就绪：图。
//
// 尺寸和位置照 Biu 抄（playbar.go 顶上那三个常量）：56×56、离左边 16、
// 歌名在它右边 8。这里从像素上量，不用问控件要尺寸。
func TestPlayBarCoverShowsTheImageWhenItIsReady(t *testing.T) {
	g := newGatedCover(t)
	_, tt := coverTester(t, app.State{HasTrack: true, Title: "在线的一首歌", CoverURL: g.url()})

	g.open() // 放行，图马上就能到
	waitCover(t, 5*time.Second, "画面上换成图", func() bool {
		tt.Frame()
		return !tt.HasText("载入中")
	})
	if tt.HasText("封面加载失败") {
		t.Fatalf("图下完了却画了失败态，文字有 %q", tt.Texts())
	}

	box := paintedBox(t, tt, coverColor)
	if box.Dx() != 56 || box.Dy() != 56 {
		t.Errorf("封面是 %dx%d，Biu 是 56x56，画在 %v", box.Dx(), box.Dy(), box)
	}
	if box.Min.X != 16 {
		t.Errorf("封面左边在 x=%d，Biu 的播放条内边距是 16", box.Min.X)
	}

	title, ok := tt.Find("在线的一首歌")
	if !ok {
		t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
	}
	if gap := title.X - float32(box.Max.X); gap != 8 {
		t.Errorf("封面到歌名之间是 %v，Biu 的 space-x-2 是 8", gap)
	}
}

// TestPlayBarCoverFailedShowsTheFailedState 是失败：有地址但取不回来。
func TestPlayBarCoverFailedShowsTheFailedState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "没了", http.StatusNotFound)
	}))
	defer srv.Close()

	_, tt := coverTester(t, app.State{HasTrack: true, Title: "在线的一首歌", CoverURL: srv.URL + "/404.png"})

	waitCover(t, 5*time.Second, "画面上画出失败态", func() bool {
		tt.Frame()
		return tt.HasText("封面加载失败")
	})
	if tt.HasText("载入中") {
		t.Errorf("已经失败了还在占位，文字有 %q", tt.Texts())
	}
}

// ---- 唤醒 ----

// TestPlayBarCoverWakeReachesTheWindow 钉住 imgcache/view.go 顶上那条约定：
// 调用方必须先 AddWake，否则图下完了界面永远停在占位（实测 0 帧刷新）。
//
// 关键在这一段：**不调 Frame()**，只等唤醒次数自己涨上来。少注册一路唤醒的
// 话，wakes 会一直是 0，测试到这里就失败——而四个状态那条测试照样全绿，
// 因为它们是自己一帧一帧催出来的。这就是「看着对但永远不刷新」混过测试的样子。
func TestPlayBarCoverWakeReachesTheWindow(t *testing.T) {
	g := newGatedCover(t)

	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)

	var wakes atomic.Int32
	// Attach 装的就是这一路（shell.go 的 s.images.AddWake(s.wake.invalidate)），
	// 照着它装一遍：缓存好了 → 叫醒这个窗口。窗口句柄要真窗口才拿得到，
	// 无头 Tester 里只能装这一路。
	s.images.AddWake(func() {
		wakes.Add(1)
		s.wake.invalidate()
	})

	url := g.url()
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		s.wake.set(c) // 没有这一步，下面的 invalidate 什么也不会做
		s.viewPlayBar(c, app.State{HasTrack: true, Title: "在线的一首歌", CoverURL: url})
	}, coverWinW, coverWinH)

	if !tt.HasText("载入中") {
		t.Fatalf("第一帧应该是占位，文字有 %q", tt.Texts())
	}
	waitCover(t, 5*time.Second, "封面请求发出去了", func() bool { return g.hits.Load() > 0 })

	g.open() // 图可以回来了，但从这里到 wakes 涨起来之间不许碰 Frame()

	waitCover(t, 5*time.Second, "图下完之后窗口收到唤醒", func() bool { return wakes.Load() > 0 })

	tt.Frame()
	if tt.HasText("载入中") {
		t.Errorf("唤醒之后画面还停在占位，文字有 %q", tt.Texts())
	}
	if box := paintedBox(t, tt, coverColor); box.Dx() != 56 {
		t.Errorf("唤醒之后画出来的封面是 %v，Biu 是 56x56", box)
	}
}

// TestPlayBarCoverGoesThroughTheRealShell 是接线的那一条：整个窗口跑起来，
// 放的是本地文件——今天本地文件没有封面，播放条上应该是空框，不是占位也不是
// 失败，更不该为了「有东西显示」去发一个请求。
func TestPlayBarCoverGoesThroughTheRealShell(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(coverPNG(t))
	}))
	defer srv.Close()

	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)

	if err := a.Open(flacPath(t)); err != nil {
		t.Fatal(err)
	}
	tt := mygoui.NewTester(s.View, 760, 460)
	tt.Frame()

	if got := a.Snapshot().CoverURL; got != "" {
		t.Errorf("本地文件的封面地址是 %q，应该是空的", got)
	}
	if tt.HasText("载入中") || tt.HasText("封面加载失败") {
		t.Errorf("没有封面地址却画出了状态，文字有 %q", tt.Texts())
	}
	if !tt.HasText("stereo16") {
		t.Errorf("播放条上找不到歌名，文字有 %q", tt.Texts())
	}
	if hits.Load() != 0 {
		t.Errorf("没有地址却发了 %d 个请求", hits.Load())
	}
}

// ---- 小工具 ----

// flacPath 是现成的测试音频，放一首歌进去才有一条「在放什么」。
func flacPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	return p
}

// gatedCover 是可以卡住的封面服务端：收到 open 之前不回话。
type gatedCover struct {
	srv     *httptest.Server
	release chan struct{}
	once    sync.Once
	hits    atomic.Int32
}

func newGatedCover(t *testing.T) *gatedCover {
	t.Helper()
	g := &gatedCover{release: make(chan struct{})}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		g.hits.Add(1)
		<-g.release
		w.Write(coverPNG(t))
	}))
	// 收尾一定先放行再关：关一个处理函数还没返回的服务端会一直等。
	t.Cleanup(func() {
		g.open()
		g.srv.Close()
	})
	return g
}

func (g *gatedCover) url() string { return g.srv.URL + "/cover.png" }

func (g *gatedCover) open() { g.once.Do(func() { close(g.release) }) }

// coverPNG 是一张纯色的方图。纯色才能扫出它被画在了哪儿。
func coverPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, coverColor)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// paintedBox 是画面上被 want 这个颜色涂到的最小外框。
func paintedBox(t *testing.T, tt *mygoui.Tester, want color.RGBA) image.Rectangle {
	t.Helper()
	img := tt.Image()
	var box image.Rectangle
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			if !sameColor(img.RGBAAt(x, y), want) {
				continue
			}
			box = box.Union(image.Rect(x, y, x+1, y+1))
		}
	}
	if box.Empty() {
		t.Fatalf("画面上没有涂过 %v", want)
	}
	return box
}

// sameColor 留一点余量：光栅化会有舍入。
func sameColor(got, want color.RGBA) bool {
	const slack = 8
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(got.R, want.R) <= slack && d(got.G, want.G) <= slack && d(got.B, want.B) <= slack
}

// waitCover 轮询到 cond 成立。异步的东西（下载、解码）只能这样等。
func waitCover(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等了 %v，%s 还没发生", d, what)
}
