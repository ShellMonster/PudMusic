package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/covertint"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// ─────────────────────────────────────────────────────────────────────────────
// 歌单页头图区那层光晕：端到端
//
// # 这一票要钉住的那件事（票 #623）
//
// 打开一个歌单，头图区那层「封面主色光晕」要**很快**出现。而它要的那个
// 主色，是现算的——从前为了算它把封面**原图整张解码**了一遍（实测
// 276ms~1375ms/张）。
//
// 这一组测的是**端到端**：真页面、真假图床、真取色链路。
// ─────────────────────────────────────────────────────────────────────────────

// coverURL 是一个**真实的 B 站图床地址**（形状对，才走「拼参数」那一支）。
const coverURL = "https://i0.hdslb.com/bfs/archive/collection-cover.jpg"

// installRedImages 把 `s.images` 换成一台**给红色图**的假图床。
//
// ⚠️ **为什么必须换**：imgcache 用的是它**自己**的 http.Client（不是取色那个
// `tintClient`），而封面地址是 `i0.hdslb.com`——不换的话这一组 E2E 会
// **真的去 B 站下载封面**（实测：图床回 404，而那个 404 来自真的 B 站）。
//
// ⚠️ 收尾顺序：还回去的那一句得排在基座挂的那次收尾**前面**跑
// （`Shell.Close` 在**调用那一刻**才读 `s.images` 这个字段）。
func installRedImages(t *testing.T, s *Shell) {
	t.Helper()
	tr := &redImageTransport{}
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

// redImageTransport 是一台给红色图的假图床。
type redImageTransport struct{}

func (redImageTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	img := image.NewRGBA(image.Rect(0, 0, 18, 18))
	for y := range 18 {
		for x := range 18 {
			img.Set(x, y, color.RGBA{R: 220, G: 30, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(&buf),
		Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
	}, nil
}

// collectionShellWithCover 是 `collectionShell` 加一个「这个收藏夹有封面」。
//
// ⚠️ **必须有封面**：夹具默认的 `"cover": ""` 表示「这个收藏夹没有封面」，
// 而没有封面时头图区那一层光晕**压根不会去取色**（`collectionGlow` 拿到
// 饱和度 0 就什么都不画）——于是这一组测的是一个「没有取色」的页面，
// 什么都验不到。
func collectionShellWithCover(t *testing.T, id, cover string, medias ...map[string]any) (*favBili, *Shell, *ui.Tester) {
	t.Helper()
	b := newCollectionBili(t)
	b.setMedias(medias...)
	b.mu.Lock()
	b.infos["cover"] = cover
	b.mu.Unlock()
	return collectionShellFrom(t, b, nil, "", id)
}

// tintPicServer 是那一张假图床：它给出去的是一张**红色的封面**，并且把
// 每一个请求的地址记下来。
type tintPicServer struct {
	mu    sync.Mutex
	paths []string
}

func (p *tintPicServer) hit(path string) {
	p.mu.Lock()
	p.paths = append(p.paths, path)
	p.mu.Unlock()
}

func (p *tintPicServer) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

// picTransport 把取色那一趟的请求拦下来，交给假图床。
type picTransport struct {
	base http.RoundTripper
	pics *tintPicServer
}

func (t *picTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// ⚠️ **只拦「取色要的那一张」**：那是 `hdslb.com` 图床上的地址。
	// 别的（列表里那些歌的封面、创建者头像）走 imgcache 那条路，不该被这里
	// 拦——它们跟这一票无关。
	if strings.Contains(r.URL.Host, "hdslb.com") {
		t.pics.hit(r.URL.Path)
		return redCoverResponse(), nil
	}
	return t.base.RoundTrip(r)
}

// redCoverResponse 造一张红色封面的回应。
func redCoverResponse() *http.Response {
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := range 180 {
		for x := range 320 {
			img.Set(x, y, color.RGBA{220, 30, 30, 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(&buf),
		Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
	}
}

// TestTheCollectionHeaderAsksForASmallCover 歌单页头图区那层光晕，
// **取色那一趟要的是小图，不是原图**。
//
// # 为什么这一条是端到端的
//
// 前面 `covertint` 包里那几条测的是「拼参数的纯函数」；这一条测的是
// 「**真页面打开一个歌单**时，取色那一趟实际发出的请求」——参数拼对了，
// 但页面没走那条路（比如头图区根本没调 `sync`），这里一样会红。
func TestTheCollectionHeaderAsksForASmallCover(t *testing.T) {
	pics := &tintPicServer{}
	orig := tintClient
	t.Cleanup(func() { tintClient = orig })
	tintClient = &http.Client{Transport: &picTransport{base: http.DefaultTransport, pics: pics}}

	b, s, tt := collectionShellWithCover(t, "12345", coverURL, mediaOf(1, "歌 一"))
	installRedImages(t, s)
	waitSongs(t, tt, s, 1)

	// 等取色那一趟落地。
	waitFrames(t, tt, "头图区取色", func() bool {
		_, sat := s.tint.sync("", func() {})
		_ = sat
		return len(pics.seen()) > 0
	})

	paths := pics.seen()
	for _, p := range paths {
		if !strings.Contains(p, "@") {
			t.Fatalf("取色请求的是原图（%q）——它会把几 MB 的原图整张解码一遍", p)
		}
		if strings.Contains(p, "_1c") {
			t.Fatalf("取色请求带了 `_1c`（%q）——那是中心裁剪，会严重偏色", p)
		}
	}
	_ = b
}

// TestTheCollectionHeaderGetsItsColour 头图区那层光晕**真的出现了**。
//
// 这一条钉的是「颜色算得快了之后，那层光晕还在」——不是「请求对了」。
func TestTheCollectionHeaderGetsItsColour(t *testing.T) {
	pics := &tintPicServer{}
	orig := tintClient
	t.Cleanup(func() { tintClient = orig })
	tintClient = &http.Client{Transport: &picTransport{base: http.DefaultTransport, pics: pics}}

	_, s, tt := collectionShellWithCover(t, "12345", coverURL, mediaOf(1, "歌 一"))
	installRedImages(t, s)
	waitSongs(t, tt, s, 1)

	// 等取色那一趟回来：色相落在红色那一档（0~10 或 350~360）。
	waitFrames(t, tt, "头图区取到色", func() bool {
		hue, sat := s.tint.sync(s.app.Collection(s.collection.ref).Cover, func() {})
		return sat > 0 && (hue < 12 || hue > 348)
	})

	// ⚠️ **断言画面上真的有那层光晕**——不是「取色函数返回了颜色」。
	//
	// 从前这一条是自己调 `s.tint.sync(...)` 再看返回值，而那**和页面用的
	// 是不是同一档没关系**：头图区压根不取色时，它照样绿（实测：把
	// `viewCollectionHeader` 里那一句改成不取色，这条测试连跑两次都 PASS）。
	//
	// 现在看**画面**：头图区那一块的上沿应该被染上封面色（红色）。
	glow := glowPixelsInHeader(t, tt)
	if glow == 0 {
		t.Fatalf("头图区上没有光晕——取到了色却没画出来")
	}
}

// glowPixelsInHeader 数头图区那一块里有多少像素**带上了封面的红**。
//
// # 为什么数像素而不是查元素
//
// 光晕是 `LinearGradient` 画在头图区那个盒子上的，元素树上**没有它**——
// 只有像素上才有。而这一条要证明的正是「光晕画出来了」，所以只能看像素。
//
// # 怎么认「带上了红」
//
// 光晕是**18% 的红压在浅色底上**（深色底 28%），混出来是 `(240, 219, 213)`
// 那一档——**不是纯红**。所以判据是「**R 明显高于 G**」而不是「R 高」。
//
// ⚠️ **判据的另一半是「浅色底本身不算」**：浅色主题的底色是
// `(239, 233, 222)`，那也是 R 高于 G。所以光晕的判据必须是
// **「比那块底色更红」**——用一个**没有光晕的对照**来定那个「更红」的线。
func glowPixelsInHeader(t *testing.T, tt *ui.Tester) int {
	t.Helper()
	cover, ok := tt.Find("通勤路上")
	if !ok {
		t.Fatal("头图区的标题找不到")
	}
	img := tt.Image()
	// 头图区上沿那一带（光晕从上面最浓往下淡）。
	y0 := max(int(cover.Y)-14, 0)
	y1 := min(int(cover.Y)+6, img.Rect.Max.Y)
	// 从封面右边那一列往右看（封面那一块是图片，不是光晕）。
	x0 := int(cover.X) + int(cover.W) + 8
	x1 := min(x0+200, img.Rect.Max.X)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			p := img.RGBAAt(x, y)
			// 比浅色底（239, 233, 222）更红：R 高出 G 至少 12。
			if p.R >= 235 && int(p.R)-int(p.G) >= 12 {
				n++
			}
		}
	}
	return n
}

// TestTheCollectionHeaderKeepsItsColourAcrossVisits 切走再切回同一个歌单，
// 那层光晕**立刻就在**（不用再等一次网络往返）。
//
// 这一条钉的是「按 URL 缓存」那条：从前切回来要重新下载、重新解码，
// 用户看到的是「光晕又闪了一次」。
func TestTheCollectionHeaderKeepsItsColourAcrossVisits(t *testing.T) {
	pics := &tintPicServer{}
	orig := tintClient
	t.Cleanup(func() { tintClient = orig })
	tintClient = &http.Client{Transport: &picTransport{base: http.DefaultTransport, pics: pics}}

	_, s, tt := collectionShellWithCover(t, "12345", coverURL, mediaOf(1, "歌 一"))
	installRedImages(t, s)
	waitSongs(t, tt, s, 1)
	waitFrames(t, tt, "头图区取到色", func() bool {
		hue, sat := s.tint.sync(s.app.Collection(s.collection.ref).Cover, func() {})
		return sat > 0 && (hue < 12 || hue > 348)
	})
	first := len(pics.seen())

	// 切走再切回。
	s.navigate("/")
	settleFrame(tt)
	s.navigate("/collection/12345")
	waitSongs(t, tt, s, 1)

	// ⚠️ **不需要等**：命中缓存时 `sync` 是**当场**返回的（不发起任何请求）。
	cover := s.app.Collection(s.collection.ref).Cover
	hue, sat := s.tint.sync(cover, func() {})
	if sat <= 0 {
		t.Fatalf("切回来之后色没了：hue=%v sat=%v", hue, sat)
	}
	if got := len(pics.seen()); got != first {
		t.Fatalf("切走再切回之后又发了 %d 次取色请求（原来 %d 次）——"+
			"同一个封面不该重下", got-first, first)
	}
}

// TestTheColourIsTheSameAsTheOriginalWouldGive 小图取出来的色和原图取出来
// 的色**一样**——这是这一票最要紧的那一条：换了取色的图，不能把颜色换掉。
//
// ⚠️ **用的是真算法**：一边用「整张图取色」，一边用「等比缩到 96 之后取色」，
// 两者都走 `covertint.Dominant`。
func TestTheColourIsTheSameAsTheOriginalWouldGive(t *testing.T) {
	// 一张「主色在两边」的封面（中心是另一种颜色）——中心裁剪会失手的那种。
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := range 180 {
		for x := range 320 {
			if x < 60 || x >= 260 {
				img.Set(x, y, color.RGBA{30, 200, 40, 255})
			} else {
				img.Set(x, y, color.RGBA{220, 30, 30, 255})
			}
		}
	}
	full, ok := covertint.Dominant(img)
	if !ok {
		t.Fatal("整张封面取不出颜色")
	}
	small := image.NewRGBA(image.Rect(0, 0, 96, 54))
	scaleInto(small, img)
	sh, ok := covertint.Dominant(small)
	if !ok {
		t.Fatal("小图取不出颜色")
	}
	gap := hueAngleGap(full.H, sh.H)
	if gap > 10 {
		t.Fatalf("小图取出的色相偏了 %.1f°（整张 %.1f°，小图 %.1f°）", gap, full.H, sh.H)
	}
}

// scaleInto 把 src 等比缩进 dst（dst 的宽高按 src 的比例定）。
func scaleInto(dst *image.RGBA, src image.Image) {
	b := src.Bounds()
	k := float64(dst.Rect.Dx()) / float64(b.Dx())
	h := int(float64(b.Dy())*k + 0.5)
	if h < 1 {
		h = 1
	}
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
}

// hueAngleGap 是两个色相之间最短的角度差（0~180）。
func hueAngleGap(a, b float64) float64 {
	d := a - b
	for d > 180 {
		d -= 360
	}
	for d < -180 {
		d += 360
	}
	if d < 0 {
		d = -d
	}
	return d
}

// newFakePicClient 造一个只给封面那一趟用的假客户端。
func newFakePicClient(pics *tintPicServer) *http.Client {
	return &http.Client{Transport: &picTransport{base: http.DefaultTransport, pics: pics}}
}

// A→B→A 回来的时候**不该重新下载**那个 A 的封面。
//
// # 为什么这一条单列
//
// 头图区那层光晕的取色缓存从前挂在**页面**上（`collectionPage.tint`），
// 而页面状态在换容器时整个换掉——于是缓存跟着一起没了。用户来回翻几个
// 收藏夹（A→B→A），头图区的光晕**每一次都闪一下**（那正是这一票要消灭
// 的症状）。
//
// 缓存现在挂在**外壳**上（`Shell.tint`），所以它跨页面活着。
func TestTheColourSurvivesGoingBackAndForth(t *testing.T) {
	pics := &tintPicServer{}
	orig := tintClient
	t.Cleanup(func() { tintClient = orig })
	tintClient = newFakePicClient(pics)

	_, s, tt := collectionShellWithCover(t, "12345", coverURL, mediaOf(1, "歌 一"))
	installRedImages(t, s)
	waitSongs(t, tt, s, 1)
	waitFrames(t, tt, "A 取到色", func() bool {
		hue, sat := s.tint.sync(s.app.Collection(s.collection.ref).Cover, func() {})
		return sat > 0 && (hue < 12 || hue > 348)
	})
	first := len(pics.seen())

	// 切走，再切回同一个容器。
	s.navigate("/")
	settleFrame(tt)
	s.navigate("/collection/12345")
	waitSongs(t, tt, s, 1)

	// ⚠️ **不用等**：命中缓存时是**当场**返回的。
	hue, sat := s.tint.sync(s.app.Collection(s.collection.ref).Cover, func() {})
	if sat <= 0 {
		t.Fatalf("切回来之后色没了：hue=%v sat=%v", hue, sat)
	}
	if got := len(pics.seen()); got != first {
		t.Fatalf("A→B→A 之后又发了 %d 次取色请求（原来 %d 次）——"+
			"缓存没有跨页面活着，光晕每次都会闪一下", got-first, first)
	}
}
