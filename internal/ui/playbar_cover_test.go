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

// Biu 的那几个数，**在这里写死字面量**，不去用 playbar.go 里的同名常量。
//
// 为什么不用那边的常量：布局和期望都用它的话，常量一改，两边一起跟着变，
// 测试就发现不了了——把 playbarHeight 从 88 改成 122，期望也跟着变成 122，
// 照样全绿。而这几条正是本任务要钉死的东西（「总高 88px，封面上下各 16」），
// 它们的真值只来自 Biu 的源码，不来自我们的常量。
const (
	biuPlaybarHeight = 88 // layout/index.tsx:48 的 h-[88px]
	biuPlaybarPadX   = 16 // playbar/index.tsx:23-31 的 px-4
	biuCoverSize     = 56 // playbar/left/index.tsx:29-45 的 width={56} height={56}
	biuCoverGap      = 8  // playbar/left/index.tsx 的 space-x-2
)

// 测试窗口的高度：几个都要用到 88 这条，窗口高写成一个常量，位置才能一次算清。
const coverWinH = 460

// TestPlayBarUsesBiuNumbers 是那几个数字本身的一条：playbar.go 里改成了别的数，
// 这里就挂。下面的测试是从画面上量像素，那是另一层保险。
func TestPlayBarUsesBiuNumbers(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want float32
		fromBiuAt string
	}{
		{"播放条总高", playbarHeight, biuPlaybarHeight, "layout/index.tsx:48 的 h-[88px]"},
		{"左右内边距", playbarPadX, biuPlaybarPadX, "playbar/index.tsx 的 px-4"},
		{"封面边长", coverSize, biuCoverSize, "playbar/left 的 width={56} height={56}"},
		{"封面与歌名的间距", coverGap, biuCoverGap, "playbar/left 的 space-x-2"},
	} {
		if c.got != c.want {
			t.Errorf("playbar.go 里 %s 是 %v，Biu 的是 %v（%s）", c.name, c.got, c.want, c.fromBiuAt)
		}
	}
}

// ---- 四个状态 ----

// TestPlayBarCoverWithNoURLIsABlankBox：没有地址 → 画一个空框。
//
// ⚠️ 这不是「加载失败」。imgcache 的 drawCover 里这是分开的两支：空地址压根
// 没有东西可显示，失败是有个地址但取不回来。这里两样都断：文字上不出现占位也
// 不出现失败，并且给歌名留出的位置仍然是「16 + 56 + 8」——空框也占地方。
func TestPlayBarCoverWithNoURLIsABlankBox(t *testing.T) {
	_, tt := coverBarTester(t, app.State{HasTrack: true, Title: "本地的一首歌"})

	if tt.HasText("载入中") {
		t.Errorf("没有地址不该画占位，文字有 %q", tt.Texts())
	}
	if tt.HasText("封面加载失败") {
		t.Errorf("没有地址被画成了加载失败，文字有 %q", tt.Texts())
	}

	title, ok := tt.Find("本地的一首歌")
	if !ok {
		t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
	}
	if want := float32(biuPlaybarPadX + biuCoverSize + biuCoverGap); title.X != want {
		t.Errorf("歌名从 x=%v 开始，应该是 %v（Biu 的 %d 左边距 + %d 封面 + %d 间距）",
			title.X, want, biuPlaybarPadX, biuCoverSize, biuCoverGap)
	}
}

// TestPlayBarCoverWhileLoadingShowsThePlaceholder 是载入中：占位。
func TestPlayBarCoverWhileLoadingShowsThePlaceholder(t *testing.T) {
	g := newGatedCover(t)
	_, tt := coverBarTester(t, app.State{HasTrack: true, Title: "在线的一首歌", CoverURL: g.url()})

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

// TestPlayBarCoverShowsTheImageWhenItIsReady 是就绪：图，并且量它的位置。
//
// 尺寸和位置照 Biu 抄（playbar.go 顶上那几个常量）：播放条 88 高、左右 16、
// 封面 56×56 在这一行里垂直居中（上下各 16）、到歌名 8。这里从像素上量，
// 不是问控件要尺寸。
func TestPlayBarCoverShowsTheImageWhenItIsReady(t *testing.T) {
	g := newGatedCover(t)
	_, tt := coverBarTester(t, app.State{HasTrack: true, Title: "在线的一首歌", CoverURL: g.url()})

	g.open() // 放行，图马上就能到
	waitCover(t, 5*time.Second, "画面上换成图", func() bool {
		tt.Frame()
		return !tt.HasText("载入中")
	})
	if tt.HasText("封面加载失败") {
		t.Fatalf("图下完了却画了失败态，文字有 %q", tt.Texts())
	}

	// 只画播放条时它贴在画面顶上：封面该落在 y=16..72，上下各 16。
	assertCoverGeometry(t, tt, 0)

	title, ok := tt.Find("在线的一首歌")
	if !ok {
		t.Fatalf("画面上找不到歌名，文字有 %q", tt.Texts())
	}
	box := paintedBox(t, tt, coverColor)
	if gap := title.X - float32(box.Max.X); gap != biuCoverGap {
		t.Errorf("封面到歌名之间是 %v，Biu 的 space-x-2 是 %d", gap, biuCoverGap)
	}
}

// TestPlayBarCoverFailedShowsTheFailedState 是失败：有地址但取不回来。
func TestPlayBarCoverFailedShowsTheFailedState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "没了", http.StatusNotFound)
	}))
	defer srv.Close()

	_, tt := coverBarTester(t, app.State{HasTrack: true, Title: "在线的一首歌", CoverURL: srv.URL + "/404.png"})

	waitCover(t, 5*time.Second, "画面上画出失败态", func() bool {
		tt.Frame()
		return tt.HasText("封面加载失败")
	})
	if tt.HasText("载入中") {
		t.Errorf("已经失败了还在占位，文字有 %q", tt.Texts())
	}
}

// ---- 唤醒 ----

// TestPlayBarCoverWakeRedrawsTheWindow 钉住 imgcache/view.go 顶上那条约定：
// 图下完之后必须叫醒窗口，否则界面永远停在占位（实测 0 帧刷新）。
//
// 这一条只走生产接线，测试不去替它装任何唤醒、也不去替它数通知：
//
//	Shell.New 里 s.images.AddWake(s.wake.invalidate)
//	App.Enqueue 一项带 Cover 的歌 → State.CoverURL → Shell.View → imgcache.View
//	Cache.finish → notify → wakeHandle.invalidate → Services().Invalidate
//	  → 窗口被请求一帧 → settle 多画一帧 → 占位自己换成图
//
// ⚠️ MyGo 的 Tester 是**拉**式的：测试不调 Frame()，它一帧都不画。所以「有人叫醒
// 了窗口」和「没人叫醒」的唯一区别，就是 settle 自己多跑的那一帧——而那一帧只有
// requested 被置上才会跑（headless.go 的 settle 在 runFrame 之后才看这个标志），
// requested 又只有 Services().Invalidate() 会置。**把生产那一路换成
// `AddWake(func() {})`，settle 就只画一帧，占位留在屏幕上，这里立刻挂。**
//
// 为了让唤醒落在 settle 的循环**里面**（落在循环之后就看不见了），测试在第一帧
// 画完之后就地等图进缓存，再给 notify 一点时间——这段等待发生在一帧之内。
func TestPlayBarCoverWakeRedrawsTheWindow(t *testing.T) {
	g := newGatedCover(t)

	a := app.New()
	t.Cleanup(a.Close)
	s := New(a) // ← 生产接线就在这里
	t.Cleanup(s.images.Close)

	url := g.url()
	// 走真实的那条路：一个能放的文件 + 一个有地址的封面。
	//
	// ⚠️ 这里摆的是**本地**项。测试原来写的是 `Source: "online"`，那时候
	// `play` 还不分来源，在线两项走的就是「开本地文件」那条路，所以那个标签
	// 只是个装饰。现在在线两项真的走取流（`app.playOnline`），一个没有
	// bvid/cid 的在线项会被当场拒掉——那是对的。
	//
	// 要测的仍然是「队列项把封面带下来」这一半，`play` 里 `setCover` 那一步
	// 对本地和在线是同一条。取流那一半在 `internal/app/online_test.go` 里测。
	if err := a.Enqueue(app.Item{
		Source: "local",
		Type:   "local",
		Path:   flacPath(t),
		Title:  "带封面的歌",
		Cover:  url,
	}); err != nil {
		t.Fatal(err)
	}
	// 暂停：播放器那个 50ms 的时钟也会叫醒窗口（真实窗口里 Attach 装了 app 的
	// 唤醒回调），让它醒着的话「是谁叫醒的」就说不清了。
	a.Toggle()

	frames, cached := 0, false
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		frames++
		s.View(c)
		if cached {
			return // 只在第一帧里喂一次
		}
		cached = true

		g.open() // 放行：图开始下
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if s.images.Get(url).Ready() {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		// finish 里是先落终态、再 notify：看到图进了缓存，不代表 notify 已经到
		// 窗口了。留一段余量让这一路 wake 走完。
		time.Sleep(400 * time.Millisecond)
	}, 760, coverWinH)

	if !s.images.Get(url).Ready() {
		t.Fatal("图一直没进缓存，这条测的就不是唤醒了")
	}
	if frames < 2 {
		t.Errorf("只画了 %d 帧：缓存好了却没人请求窗口重绘，文字有 %q", frames, tt.Texts())
	}
	if tt.HasText("载入中") {
		t.Errorf("没人叫醒窗口，画面停在占位，文字有 %q", tt.Texts())
	}
	// 整窗里播放条是最后一个孩子、整条贴底，所以条顶在 coverWinH-88；封面该落在
	// 那 88 的正中间（上下各 16）。条一高封面就往上跑，这里立刻挂。
	assertCoverGeometry(t, tt, coverWinH-biuPlaybarHeight)
}

// TestPlayBarCoverGoesThroughTheRealShell 走完整 Shell.View 放本地文件：
// 今天本地文件没有封面，播放条上应该是空框，不是占位也不是失败，更不该为了
// 「有东西显示」去发一个请求。
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
	tt := mygoui.NewTester(s.View, 760, coverWinH)
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

// coverBarTester 起一个整屏的窗口，状态由测试给。
//
// 只用来摆「有地址 / 没地址」那两种状态——封面地址在生产里是队列项带下来的
// （本地文件是空的，见 app.State.CoverURL），要把有地址的那两种状态摆出来就
// 得自己递状态进去。走的是 viewPlayBar，和 viewMain 调的是同一个函数。
func coverBarTester(t *testing.T, st app.State) (*Shell, *mygoui.Tester) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)

	tt := mygoui.NewTester(func(c *mygoui.Context) {
		s.wake.set(c) // 和 Shell.View 每帧开头做的事一样，唤醒要用它
		s.viewPlayBar(c, st)
	}, 760, coverWinH)
	return s, tt
}

// assertCoverGeometry 量封面在整张画面上的位置。
//
// barTop 是播放条顶边在画面上的 y。播放条是 88 高的一行、封面 56 在行内垂直居中，
// 所以封面该落在 [barTop+16, barTop+72]：上下各 16 是怎么来的——(88-56)/2。
//
// 这几条一起断的是「总高 88 + 上下各 16」。播放条一高封面就跟着往上跑，底边那条
// 先挂；上下留白不对，中间那条挂。只画播放条时它贴在画面顶上，barTop 就是 0；
// 走整窗时它是最后一个孩子、整条贴底，barTop 就是 winH-88。
func assertCoverGeometry(t *testing.T, tt *mygoui.Tester, barTop int) {
	t.Helper()
	box := paintedBox(t, tt, coverColor)
	// (88-56)/2 = 16：上下各留 16 就是这么来的。
	mid := (biuPlaybarHeight - biuCoverSize) / 2

	if box.Dx() != biuCoverSize || box.Dy() != biuCoverSize {
		t.Errorf("封面是 %dx%d，Biu 是 %dx%d，画在 %v", box.Dx(), box.Dy(), biuCoverSize, biuCoverSize, box)
	}
	if want := barTop + mid; box.Min.Y != want {
		t.Errorf("封面的上边在 y=%d，按 %d 高的播放条该在 y=%d（上下留白 (88-56)/2 = 16）",
			box.Min.Y, biuPlaybarHeight, want)
	}
	if want := barTop + biuPlaybarHeight - mid; box.Max.Y != want {
		t.Errorf("封面的下边在 y=%d，离播放条底边 16 该是 y=%d", box.Max.Y, want)
	}
	if box.Min.X != biuPlaybarPadX {
		t.Errorf("封面的左边在 x=%d，Biu 的播放条内边距是 %d", box.Min.X, biuPlaybarPadX)
	}
}

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
