// UP 空间的头图横幅：**它真的画出来了**（票 #630）。
//
// # 这一票修的是一条老 bug
//
// 横幅那一格传给图片缓存的**宽度是 0**（通栏，量不出宽度），而画图那一层用的
// 就是这个宽度——于是那一格的宽度真的是 0 像素，图从来没画出来过（票 #65 起）。
// 票 #621 又给它加了一层浪费：宽度 0 掉进「量不出尺寸 ⇒ 给最小档 64×64」的
// 分支，于是多要了一张 64×64 的正方缩略图——横幅的两端直接丢掉。
//
// # 这一组量三样东西
//
//  1. **画面上的像素**：横幅那一带**有图**（数像素，不是断言元素宽度）。这是
//     唯一能抓住「画不出来」的判据——帧预算抓不到它（画不出来是「少分配」）。
//  2. **请求地址**：横幅要的是**它真正显示的那一档**（内容区宽 × 横幅高），
//     不是 64×64。
//  3. **铺满**：横幅那一格**占满整个内容区宽度**。
//
// # 全是假 B 站 + 假图床
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。假图床给一张
// **纯色**的图——判据是「横幅那一带有那个颜色」，而那个颜色在界面的别处
// 根本不出现（见 `bannerRed`）。

package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
)

// bannerRed 是假图床给**横幅那张**的颜色。
//
// ⚠️ **这个颜色在界面的别处不出现**：主题色是绿的，页面底色是米色——所以「数
// 这个颜色的像素」只会数到横幅本身。
var bannerRed = color.RGBA{R: 220, G: 30, B: 30, A: 255}

// bannerBlue 是假图床给**别的图**（头像那些）的颜色。
//
// ⚠️ **为什么头像要换成另一种颜色**：头像和横幅**挨着**（头像压住横幅下沿），
// 而头像也是一张网络图——假图床给什么它就画什么。于是「数红色的像素」会把
// 头像那一块圆也数进去，**横幅画没画出来就分不出来了**。
//
// ⚠️ **这一条是踩出来的**：第一版假图床给所有图同一个红色，那条「横幅画出来」
// 的测试在**把宽度改回 0 之后照样是绿的**——它数到的是头像，不是横幅。
var bannerBlue = color.RGBA{R: 30, G: 30, B: 220, A: 255}

// bannerPicServer 是那台**假图床**：它给出去的是一张纯色的图（横幅红色、别的
// 蓝色），并且把每一个请求的地址记下来。
type bannerPicServer struct {
	mu    sync.Mutex
	paths []string
	// hold 是「横幅那一趟**按住不放**」的闸门。关着的时候横幅立刻就回来。
	//
	// ⚠️ **为什么需要它**：「正在加载」那一档只存在**图还在路上**的那几帧，
	// 而本地那台假图床是**瞬间**就回的——于是「加载中」那一档在画面上一帧都
	// 看不见，测它就成了碰运气。按住不放，那一档就稳定地停在那儿。
	hold    bool
	release chan struct{}
	// failBanner 让横幅那一趟**回 404**（永久失败：4xx 不重试），于是画面上
	// 停的是失败那一档。
	failBanner bool
	// bannerImg 是横幅那一趟的**图**。nil = 给那张纯红的。
	//
	// ⚠️ **出图那一组要给它一条渐变带**（见 `bannerShotPNG`）：纯色的一块
	// 看不出「这是横幅」——看不出比例，也看不出两端被裁掉了没有。
	bannerImg []byte
}

func (p *bannerPicServer) hit(path string) {
	p.mu.Lock()
	p.paths = append(p.paths, path)
	p.mu.Unlock()
}

// holdBanner 让横幅那一趟**按住不放**，直到 `releaseBanner` 被调。
func (p *bannerPicServer) holdBanner() {
	p.mu.Lock()
	p.hold = true
	p.release = make(chan struct{})
	p.mu.Unlock()
}

// releaseBanner 放开按住的那一趟。
func (p *bannerPicServer) releaseBanner() {
	p.mu.Lock()
	ch := p.release
	p.hold = false
	p.release = nil
	p.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// seen 记下它收到过的那些路径。
func (p *bannerPicServer) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

// bannerPicTransport 是 imgcache 那一趟的网络层：把请求交给假图床。
type bannerPicTransport struct {
	base http.RoundTripper
	pics *bannerPicServer
}

// shotBannerPics 是出图那一组用的那台假图床（横幅那张是**渐变带**）。
var shotBannerPics *bannerPicServer

func (t *bannerPicTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.pics.hit(r.URL.Path)
	t.pics.mu.Lock()
	hold, ch, fail := t.pics.hold, t.pics.release, t.pics.failBanner
	t.pics.mu.Unlock()
	isBanner := strings.Contains(r.URL.Path, "banner")
	if hold && isBanner && ch != nil {
		<-ch
	}
	if fail && isBanner {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       nopCloser{bytes.NewBuffer(nil)},
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
		}, nil
	}
	t.pics.mu.Lock()
	img := t.pics.bannerImg
	t.pics.mu.Unlock()
	if isBanner && img != nil {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       nopCloser{bytes.NewBuffer(img)},
			Header:     http.Header{"Content-Type": []string{"image/png"}},
		}, nil
	}
	return bannerResponse(r.URL.Path), nil
}

// bannerResponse 造一张纯色图的回应：**横幅红色、别的蓝色**。
func bannerResponse(path string) *http.Response {
	c := bannerBlue
	if strings.Contains(path, "banner") {
		c = bannerRed
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       nopCloser{&buf},
		Header:     http.Header{"Content-Type": []string{"image/png"}},
	}
}

// nopCloser 是一条最小的 ReadCloser。
type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

// installBannerImages 把 `s.images` 换成一台**给纯红图**的假图床。
//
// ⚠️ **为什么必须换**：imgcache 用的是它**自己**的 http.Client（不是取色那个
// `tintClient`），而横幅地址是 `i0.hdslb.com`——不换的话这一组会**真的去
// B 站下载**（实测：图床回 404，而那个 404 来自真的 B 站）。
//
// ⚠️ 收尾顺序：还回去的那一句得排在基座挂的那次收尾**前面**跑
// （`Shell.Close` 在**调用那一刻**才读 `s.images` 这个字段）。
func installBannerImages(t *testing.T, s *Shell, pics *bannerPicServer) {
	t.Helper()
	tr := &bannerPicTransport{base: http.DefaultTransport, pics: pics}
	cache := imgcache.New(imgcache.Options{Client: &http.Client{Transport: tr}})
	keep := s.images
	keepRemove := s.imagesWakeRemove
	t.Cleanup(func() {
		cache.Close()
		s.images = keep
		s.imagesWakeRemove = keepRemove
	})
	s.images = cache
	s.imagesWakeRemove = cache.AddWake(s.wake.invalidate)
}

// bannerSpaceShell 造一个「打开一个 UP 主空间」的壳，那台假 B 站给的横幅
// 地址指向假图床。
func bannerSpaceShell(t *testing.T, pics *bannerPicServer, bannerPath string) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newUIExplore(t)
	// 横幅那张图自己给（不走假 B 站那一台——它只管接口）。
	b.on("/bfs/archive/banner.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bannerPNG(64, 64, bannerRed))
	})
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": "有头图的 UP", "sign": "这是一个签名", "level": 4,
			"face":         "https://f/face-42.jpg",
			"top_photo_v2": map[string]string{"l_200h_img": bannerPath},
			"official":     map[string]any{"role": 0},
		})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"follower": 123456, "following": 12})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"relation":    map[string]any{"attribute": 0},
			"be_relation": map[string]any{"attribute": 0},
		})
	})
	b.on("/x/relation/modify", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, nil)
	})
	s, tt, _ := newSpaceShell(t, b, true, "77")
	installBannerImages(t, s, pics)
	openSpace(t, s, tt, 42)
	return s, tt
}

// bannerPNG 造一张 w×h 的纯色 PNG。
func bannerPNG(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// bannerBox 量出横幅那一格在画面上的盒子。
//
// ⚠️ **靠它的无障碍名字找**：无头 Tester 只认 label / text，不认图片——
// 没有它就量不到横幅那一格的盒子（`spaceThumb` 的注释记着同一件事）。
// 那个名字是 `spaceBannerLabel`（生产代码里那一处）。
func bannerBox(t *testing.T, tt *mygoui.Tester) mygoui.Rect {
	t.Helper()
	r, ok := tt.Find(spaceBannerLabel)
	if !ok {
		t.Fatalf("画面上找不到横幅那一格，文字有 %q", tt.Texts())
	}
	return r
}

// bannerRedPixels 数横幅那一带里有多少个**那个红色**的像素。
//
// # 为什么数像素而不是问元素
//
// 这一票要证明的是「图**画出来了**」，而元素树上看得见图片元素不等于画面上
// 有它——那一格宽度是 0 的时候，图片元素照样在树上，画面上一个像素也没有。
// 所以只能看像素（仓库里多处用同一个办法，见 `comments_test.go` 的
// `magentaPixels`）。
//
// # ⚠️ **只数横幅自己那一条，不数头像那一块**
//
// 头像压住横幅的**下沿**（`upAvatarTop` = 88），而头像是**另一张网络图**。
// 于是「横幅那一格」这个盒子在竖直方向上和头像**重叠**——数整格的话，头像
// 那几行也会被算进来。
//
// ⚠️ **这不是理论上的顾虑**：第一版假图床给**所有**图同一个红色，那条测试
// 在**把宽度改回 0 之后照样是绿的**——它数到的是头像（一个圆，3048 个像素），
// 不是横幅。横幅那一带（y 从上沿到头像上沿）一个红色像素都没有。
//
// 所以这里只数**横幅自己占的那一条**：从横幅的上沿到**头像的上沿**（减去
// 一点余量），横向是横幅的整条宽。
func bannerRedPixels(t *testing.T, tt *mygoui.Tester, box mygoui.Rect) int {
	t.Helper()
	img := tt.Image()
	// 头像上沿 = 头像那一块的 y。头像那一块是**圆的**，它从 `upAvatarTop`
	// （= 横幅高 − 44 = 88）开始——所以横幅自己独占的是上面那 88 像素。
	//
	// ⚠️ **用「横幅高 − 头像压住的那一段」算**，而不是写死 88：那两个数在
	// `upspace.go` 的常量表里（`upBannerH` / `upAvatarTop`），改了要跟着变。
	own := box.Y + upBannerH - (upBannerH - upAvatarTop)
	// 留 4 像素余量：头像那一圈底色环（`upAvatarRing`）压在上面。
	own -= upAvatarRing + 1
	if own <= box.Y {
		own = box.Y + 1
	}
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(own), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if absInt(int(p.R)-int(bannerRed.R)) <= 24 &&
				absInt(int(p.G)-int(bannerRed.G)) <= 24 &&
				absInt(int(p.B)-int(bannerRed.B)) <= 24 {
				n++
			}
		}
	}
	return n
}

// TestTheSpaceBannerIsActuallyDrawn 钉住这一票的**核心**：打开一个 UP 主空间，
// 头图横幅**真的画出来了**。
//
// # 判据是画面上的像素
//
// 横幅那一带应该有**成片**那个红色的像素（假图床给的是一张纯红图）。
// 判据用的是「成片」而不是「>0」：一个像素可能来自别处的巧合，成片才是
// 「图铺满了那一格」。
//
// ⚠️ **这一条是唯一能抓住「画不出来」的判据**：横幅那一格宽度是 0 的时候，
// 元素树上照样有图片元素、请求照样发出去了、帧预算甚至更省——只有画面上的
// 像素会说话。
func TestTheSpaceBannerIsActuallyDrawn(t *testing.T) {
	pics := &bannerPicServer{}
	_, tt := bannerSpaceShell(t, pics, "https://i0.hdslb.com/bfs/archive/banner.png")

	// 等横幅那一格**画出来**：那一格里的像素变成那个红色。
	waitFrames(t, tt, "横幅画出来", func() bool {
		return bannerRedPixels(t, tt, bannerBox(t, tt)) > 100
	})
}

// TestTheSpaceBannerFillsTheWholeWidth 钉住横幅**铺满整个内容区宽度**。
//
// # 判据是「那一格的宽」和「内容区的宽」
//
// ⚠️ **内容区宽不是窗口宽**：侧栏占掉一块（默认 220），所以 1200 的窗口上
// 内容区是 980。横幅是**内容区里的通栏**，所以它该铺满的是内容区，不是整窗。
//
// 这一条量的是**元素盒子的宽度**（不是像素）——它断的是「那一格只占了半行」
// 那种摆布问题，而「有没有图」由上一条断。
func TestTheSpaceBannerFillsTheWholeWidth(t *testing.T) {
	pics := &bannerPicServer{}
	_, tt := bannerSpaceShell(t, pics, "https://i0.hdslb.com/bfs/archive/banner.png")

	box := bannerBox(t, tt)
	if box.W <= 0 {
		t.Fatalf("横幅那一格的宽是 %.1f——它压根没占地方", box.W)
	}
	// 顶栏那一列的左沿就是内容区的左沿（侧栏右边）。而横幅那一格的右沿
	// 应当和内容区的右沿齐——用「窗口右边减去右边距」来比。
	//
	// ⚠️ **为什么拿窗口宽减而不是拿某个具体数字**：内容区的右沿跟着侧栏宽
	// 和窗口宽走，写死一个数会在别的窗口宽度下假红。
	winW := float32(tt.Image().Rect.Max.X)
	gap := winW - (box.X + box.W)
	if gap > 1 {
		t.Errorf("横幅那一格的右沿离窗口右边还有 %.1f 像素——它没铺满内容区", gap)
	}
}

// TestTheSpaceBannerAsksForItsOwnSize 钉住横幅**要的是它自己那一档**，不是
// 64×64。
//
// # 判据是「请求了什么」
//
// 横幅要显示的是**内容区宽 × 横幅高**（默认设置下 980×132 DIP，Retina 上
// 1960×264 像素，长边吸档后落在 768 那一档）。
//
// ⚠️ **不能断言一个写死的地址**：内容区宽跟着窗口宽和侧栏宽走。断的是
// 「**它要的那一档和它显示的尺寸对得上**」，以及「**它不是 64×64 那一档**」。
func TestTheSpaceBannerAsksForItsOwnSize(t *testing.T) {
	pics := &bannerPicServer{}
	_, tt := bannerSpaceShell(t, pics, "https://i0.hdslb.com/bfs/archive/banner.png")

	// 等横幅那一格画出来（那时候它已经要过图了）。
	waitFrames(t, tt, "横幅画出来", func() bool {
		return bannerRedPixels(t, tt, bannerBox(t, tt)) > 100
	})

	paths := pics.seen()
	if len(paths) == 0 {
		t.Fatal("一次请求都没发出去")
	}
	for _, p := range paths {
		if strings.Contains(p, "@64w_64h") {
			t.Fatalf("横幅要的是 %q——那是「量不出尺寸 ⇒ 给最小档」那一档，"+
				"一张通栏横幅被缩成正方形缩略图，两端直接丢掉", p)
		}
	}
	// 它要的那一档要带着 `@<w>w_<h>h_1c`（中心裁剪：横幅是**显示**，和封面、
	// 头像同一族）。
	var sized string
	for _, p := range paths {
		if strings.Contains(p, "@") {
			sized = p
			break
		}
	}
	if sized == "" {
		t.Fatalf("横幅要的图不带尺寸参数（请求过 %v）——"+
			"它要的是原图，而不是「按显示尺寸要图」那一档", paths)
	}
	if !strings.Contains(sized, "_1c") {
		t.Errorf("横幅要的图 %q 不带中心裁剪——横幅是**显示**，"+
			"要的是中心裁剪那一档（和上游 `background-size: cover` 一致）", sized)
	}
}

// TestTheSpaceBannerDoesNotAskForANewImageOnEveryPixel 钉住「拖动窗口时横幅
// 不会每拖一个像素就换一张图」。
//
// # 判据是「产生了几档不同的地址」
//
// 内容区宽是连续的，而地址必须**吸档**——于是拖动 64 个像素只该产生很少的
// 几档（现有那套档位：64/96/128/…/768）。
//
// ⚠️ **这一条断的是「吸档被摘掉」那种错**：地址跟着宽度连续变的话，拖一次
// 窗口就下一批图、在盘上攒一批几乎一样的文件。
func TestTheSpaceBannerDoesNotAskForANewImageOnEveryPixel(t *testing.T) {
	pics := &bannerPicServer{}
	_, tt := bannerSpaceShell(t, pics, "https://i0.hdslb.com/bfs/archive/banner.png")
	waitFrames(t, tt, "横幅画出来", func() bool {
		return bannerRedPixels(t, tt, bannerBox(t, tt)) > 100
	})

	before := len(pics.seen())
	// 拖 64 个像素（每 4 个像素一帧，够密了）。
	for i := range 16 {
		tt.SetSize(1200+i*4, 800)
		tt.Frame()
	}
	tt.SetSize(1200, 800)
	tt.Frame()

	distinct := map[string]bool{}
	for _, p := range pics.seen()[before:] {
		distinct[p] = true
	}
	if len(distinct) > 5 {
		t.Errorf("拖动窗口 64 个像素，横幅要了 %d 个不同的地址（%v）——"+
			"它没有吸档：拖一次窗口就下一批图", len(distinct), distinct)
	}
}

// TestTheSpaceBannerFollowsTheWindowWidth 钉住「窗口拖宽之后横幅跟着变清晰」。
//
// # 判据是「要的那一档跟着内容区宽走」
//
// 窗口拉宽 400 像素之后，横幅要的那一档应该**跟着变**——具体是**比例**变宽：
// 横幅要的是「内容区宽 × 横幅高」那个比例的图，而内容区变宽 → 那个比例变宽。
//
// # ⚠️ **为什么断的是「比例」而不是「长边的数」
//
// `coverPixelSize` 的长边**有全局上限 768**（`maxStoredSide`，内存账的那条
// 线，票 #621 就有）。而横幅在 1200 窗口上要的是 980×132 DIP —— Retina 上是
// 1960 像素，**早就超过 768 了**。于是长边那一头是**钉住的**（768），跟着
// 窗口宽走的是**短边**：980×132 → 768×128，1380×132 → 768×96。
//
// 所以「那一档跟着窗口宽走」在画面上的可观察形态是**比例变宽**，不是「长边的
// 数字变大」。
//
// ⚠️ **这条上限是「按显示尺寸要图」那个机制自己的**（`maxStoredSide`），而
// 票 #627 的 Out of Scope 明写「按显示尺寸要图这个机制本身的改造」不归这一票
// ——所以这里不碰它，只断「横幅要的那一档确实跟着宽走」。
//
// ⚠️ **这一条单列**：前一条断的是「别换太频繁」，这一条断的是「**要跟着
// 走**」——只盯着「别换太频繁」的话，一个「永远要最小档」的实现也能绿。
func TestTheSpaceBannerFollowsTheWindowWidth(t *testing.T) {
	pics := &bannerPicServer{}
	_, tt := bannerSpaceShell(t, pics, "https://i0.hdslb.com/bfs/archive/banner.png")
	waitFrames(t, tt, "横幅画出来", func() bool {
		return bannerRedPixels(t, tt, bannerBox(t, tt)) > 100
	})
	small := bannerRequestRatio(t, pics.seen())

	// 窗口拉宽 400。
	tt.SetSize(1600, 800)
	tt.Frame()
	waitFrames(t, tt, "横幅按新的宽要图", func() bool {
		return bannerRequestRatio(t, pics.seen()) > small
	})
	big := bannerRequestRatio(t, pics.seen())
	if big <= small {
		t.Errorf("窗口从 1200 拉到 1600，横幅要的那一档的比例还是 %.2f（没变宽）——"+
			"它没有跟着内容区的宽走", big)
	}
}

// bannerRequestRatio 报「要过的那些地址里，**最宽的那一档**的宽高比」。
//
// 报 0 是「一次带尺寸的请求都没有」。
//
// ⚠️ **取最宽的那一档而不是最后一档**：拖动过程中会有好几档，比例最宽的那一档
// 才是「窗口最宽时要的那一档」。
func bannerRequestRatio(t *testing.T, paths []string) float64 {
	t.Helper()
	best := 0.0
	for _, p := range paths {
		w, h := bannerSpecWH(p)
		if w <= 0 || h <= 0 {
			continue
		}
		if r := float64(w) / float64(h); r > best {
			best = r
		}
	}
	return best
}

// bannerSpecWH 从 `…@<w>w_<h>h_1c.webp` 那一段里读出宽和高。
//
// 读不出来就是 (0, 0)。
func bannerSpecWH(path string) (int, int) {
	i := strings.Index(path, "@")
	if i < 0 {
		return 0, 0
	}
	spec := path[i+1:]
	j := strings.Index(spec, "w_")
	if j <= 0 {
		return 0, 0
	}
	k := strings.Index(spec[j+2:], "h")
	if k <= 0 {
		return 0, 0
	}
	w, ok1 := atoi(spec[:j])
	h, ok2 := atoi(spec[j+2 : j+2+k])
	if !ok1 || !ok2 {
		return 0, 0
	}
	return w, h
}

// atoi 读一串数字。读不出来是 false。
func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		n = n*10 + int(ch-'0')
	}
	return n, true
}

// ─────────────────────────────────────────────────────────────────────────────
// 横幅的那三档（加载 / 就绪 / 失败）与「压根没有头图」那一档
// ─────────────────────────────────────────────────────────────────────────────

// bannerShellWith 是 `bannerSpaceShell` 加一个「横幅那一趟要多久才回来」。
//
// delay 是横幅那张图**多久之后**才回来（0 = 立刻）。等它的那一段时间里画面上
// 该是「正在加载」那一档。
func bannerShellWith(t *testing.T, pics *bannerPicServer, delay time.Duration) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newUIExplore(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": "有头图的 UP", "sign": "这是一个签名", "level": 4,
			"face":         "https://f/face-42.jpg",
			"top_photo_v2": map[string]string{"l_200h_img": "https://i0.hdslb.com/bfs/archive/banner.png"},
			"official":     map[string]any{"role": 0},
		})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"follower": 123456, "following": 12})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"relation":    map[string]any{"attribute": 0},
			"be_relation": map[string]any{"attribute": 0},
		})
	})
	b.on("/x/relation/modify", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, nil)
	})
	s, tt, _ := newSpaceShell(t, b, true, "77")
	installBannerImages(t, s, pics)
	openSpace(t, s, tt, 42)
	return s, tt
}

// TestTheSpaceBannerShowsSomethingWhileItLoads 钉住「头图还没下下来时看到
// 「正在加载」那一档（带扫光的灰），不是空白」。
//
// # 判据是「那一带不是画布底色」
//
// 横幅那一格在加载中是**一块浅灰**（`shimmer` 的扫光底），不是页面的米色底。
// 所以判据是「横幅自己那一条里，颜色**和页面底色不一样**的像素有一片」。
//
// ⚠️ **为什么用「不是底色」而不是「是那个灰」**：扫光会动，那块灰的**亮度**
// 每一帧都不一样——写死一个数就变成「等某一帧恰好亮到那个值」。而「和页面
// 底色不一样」是稳定的判据。
//
// ⚠️ **那一趟要按住不放**（`holdBanner`）：本地那台假图床是**瞬间**就回的，
// 于是「加载中」那一档在画面上一帧都看不见，测它就成了碰运气。按住不放，
// 那一档就稳定地停在那儿。
func TestTheSpaceBannerShowsSomethingWhileItLoads(t *testing.T) {
	pics := &bannerPicServer{}
	pics.holdBanner()
	_, tt := bannerSpaceShell(t, pics, "https://i0.hdslb.com/bfs/archive/banner.png")
	// ⚠️ **收尾的顺序要紧**：cleanups 是**后注册的先跑**，而
	// `installBannerImages` 的收尾会 `cache.Close()`——那一句**等在途的下载
	// 结束**。于是「放开按住的那一趟」必须排在它**前面**（也就是**后**注册）。
	// 顺序反了就是死锁：Close 在等一个永远不回来的下载。
	t.Cleanup(pics.releaseBanner)

	// 等那一趟真的发出去了（不然这一帧还压根没开始下）。
	waitFrames(t, tt, "横幅开始下", func() bool {
		for _, p := range pics.seen() {
			if strings.Contains(p, "banner") {
				return true
			}
		}
		return false
	})

	box := bannerBox(t, tt)
	// 还没回来：那一条里**一个红色像素都没有**。
	if n := bannerRedPixels(t, tt, box); n > 0 {
		t.Fatalf("横幅还没下下来，那一带却有 %d 个红色的像素——它已经画出来了", n)
	}
	// 而它**不是空白**：那一带和页面底色不一样（有一块灰在那儿）。
	if n := bannerDistinctPixels(t, tt, box, bannerPageBackdrop(t, tt)); n < 100 {
		t.Errorf("横幅还在加载，可那一带只有 %d 个像素和页面底色不一样——"+
			"那一块是空白的（应该是一块带扫光的灰）", n)
	}
}

// TestTheSpaceBannerShowsAFailureState 钉住「头图**拉不到**时看到失败态
// （描边框 + 图片图标）」。
//
// # 判据是「那一带不是空白，而且也没有那个红色」
//
// 横幅拉不到的时候画的是**失败那一档**：一块描了边框的底 + 中间一枚图片图标。
// 所以：那一带**不是空白**（有东西画着），也**不是图**（没有那个红色）。
//
// ⚠️ **失败怎么造**：假图床回 **404**。那是**永久失败**（`isPermanent`：4xx
// 不重试），于是画面上就停在这一档，不会反复闪。
func TestTheSpaceBannerShowsAFailureState(t *testing.T) {
	pics := &bannerPicServer{}
	// ⚠️ **失败要在那一趟出发之前就定下来**：横幅在第一帧就开始下那一趟。
	pics.failBanner = true
	_, tt := bannerSpaceShell(t, pics, "https://i0.hdslb.com/bfs/archive/banner.png")

	// 等失败落到画面上：那一带既没有那个红色，也不是空白。
	waitFrames(t, tt, "横幅落到失败态", func() bool {
		box := bannerBox(t, tt)
		return bannerRedPixels(t, tt, box) == 0 &&
			bannerDistinctPixels(t, tt, box, bannerPageBackdrop(t, tt)) > 100
	})
}

// bannerPageBackdrop 报「页面底色」是哪个颜色。
//
// 判据是「页面背景上那一块」的颜色——取窗口右下角那一小块（那儿是页面底色，
// 没有任何控件）。
func bannerPageBackdrop(t *testing.T, tt *mygoui.Tester) color.RGBA {
	t.Helper()
	img := tt.Image()
	x, y := img.Rect.Max.X-4, img.Rect.Max.Y-4
	return img.RGBAAt(x, y)
}

// TestTheSpaceBannerWithoutABannerIsNotBlank 钉住「**没有头图**的 UP 主仍然
// 显示主题色渐变（那一块不是空的）」。
//
// # 判据是「那一带不是画布底色」
//
// 没有头图时那一格画的是**主题色渐变**（`viewSpaceHeader` 那一支），不是
// 空白。所以判据是「横幅那一带里有**和页面底色不一样**的像素」。
//
// ⚠️ **这一条和上一条是同一种判据、相反的方向**：加载那一档是「还没来」，
// 这一档是「压根不会有」。两者都要求「那一块**不是空的**」。
func TestTheSpaceBannerWithoutABannerIsNotBlank(t *testing.T) {
	pics := &bannerPicServer{}
	b := newUIExplore(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": "没有头图的 UP", "sign": "这是一个签名", "level": 4,
			"face": "https://f/face-42.jpg",
			// ⚠️ **没有头图**：B 站这时候给的是空对象。
			"top_photo_v2": map[string]string{},
			"official":     map[string]any{"role": 0},
		})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"follower": 123456, "following": 12})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"relation":    map[string]any{"attribute": 0},
			"be_relation": map[string]any{"attribute": 0},
		})
	})
	b.on("/x/relation/modify", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, nil)
	})
	s, tt, _ := newSpaceShell(t, b, true, "77")
	installBannerImages(t, s, pics)
	openSpace(t, s, tt, 42)

	// 那一格**压根不画**（没有头图走的是渐变那一支），所以量的是**头图区**
	// 那一块——用头像那一格的左沿当参照。
	face, ok := tt.Find("https://f/face-42.jpg")
	if !ok {
		t.Fatalf("画面上找不到头像，文字有 %q", tt.Texts())
	}
	// 头图区是**从内容区左沿到右沿**、高 216 的一条。头像在它左下角。
	band := mygoui.Rect{X: face.X, Y: face.Y - 60, W: 400, H: 40}
	if n := bannerDistinctPixels(t, tt, band, tt.Image().RGBAAt(int(face.X), int(face.Y+face.H+8))); n == 0 {
		t.Errorf("没有头图的 UP 主，头图区那一块和页面底色**一模一样**——" +
			"那一块是空的（应该画主题色渐变）")
	}
}

// bannerDistinctPixels 数 box 里有多少个像素和 want **不一样**。
func bannerDistinctPixels(t *testing.T, tt *mygoui.Tester, box mygoui.Rect, want color.RGBA) int {
	t.Helper()
	img := tt.Image()
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if absInt(int(p.R)-int(want.R)) > 8 ||
				absInt(int(p.G)-int(want.G)) > 8 ||
				absInt(int(p.B)-int(want.B)) > 8 {
				n++
			}
		}
	}
	return n
}

// TestTheSpaceBannerIsDownloadedOnlyOnce 钉住「探测用的 key 和画用的 key
// **是同一个**（同一张图不会下两遍）。
//
// # 判据是「横幅那一趟发了几回」
//
// 横幅这一格走 `imgcache.States`（**没有**「先探测 Ready 再画」那一支），
// 所以它只该下**一次**。
//
// ⚠️ **为什么这一条值得单列**：别的缩略图有「探测」那一支（`spaceThumbZoom`
// 那一处），而那一支**必须**和 `States` 画的那一个用同一个 key——用
// `Get(url)` 探测的话，探测下一份**原图**、画的时候再下一份**按尺寸的**，
// 同一张封面下两遍（票 #621 的评审挑出来的）。
//
// 横幅这一格没有那一支，所以这一条断的是「**将来有人给它加了一个探测**，
// 而那个探测用的是原图」。
func TestTheSpaceBannerIsDownloadedOnlyOnce(t *testing.T) {
	pics := &bannerPicServer{}
	_, tt := bannerSpaceShell(t, pics, "https://i0.hdslb.com/bfs/archive/banner.png")
	waitFrames(t, tt, "横幅画出来", func() bool {
		return bannerRedPixels(t, tt, bannerBox(t, tt)) > 100
	})
	// 再画几帧（探测那一支若有，每帧都会发一次）。
	for range 5 {
		tt.Frame()
	}
	n := 0
	for _, p := range pics.seen() {
		if strings.Contains(p, "banner") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("横幅那一趟发了 %d 次（%v）——同一张图不该下两遍"+
			"（探测和画用的 key 不是同一个）", n, pics.seen())
	}
}
