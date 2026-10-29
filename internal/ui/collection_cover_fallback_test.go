package ui

// 歌单页头图区那一枚封面：**接口给的那张拉不下来时，改用夹里下一首的**。
//
// # 这条链路和侧栏那一枚是同一件事
//
// B 站给的容器封面就是**第一首**的封面。第一首失效之后那个地址常常还在、图已经
// 404 了——侧栏那枚方形封面早就换成了夹里下一首的（`viewFavoriteCover` 的三层
// 兜底），而**头图区此前直接把 `info.Cover` 交给图片缓存**：于是同一页上两处对
// 同一件事给出两个答案（侧栏有图、头图是一块「拉失败」的占位）。
//
// ⚠️ **这一组要证的是画面**：判据是头图区那一块**真的换了图**，不是「某个函数
// 被调了」。函数被调了而画出来还是坏图，正是这一票要修的症状。

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

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// fallbackCoverDead 是**第一首**的封面地址：那一条已经失效，图床回 404。
//
// ⚠️ **它必须是一个「形状对、内容没了」的地址**（`/bfs/` 底下、带 .jpg）：
// 那样 `imgcache` 才会真的去请求它、真的收到 404——而它画出来的是「拉失败」
// 那一档，不是「本来就没有」。这两档在界面上长得不一样，混了的话这一条会退化成
// 测「空地址画占位」。
const fallbackCoverDead = "https://i0.hdslb.com/bfs/archive/dead-first.jpg"

// fallbackCoverLive 是**第二首**的封面地址：它给一张纯蓝的图。
const fallbackCoverLive = "https://i0.hdslb.com/bfs/archive/second-live.jpg"

// fallbackBlue 是第二首那张封面的颜色。
//
// ⚠️ **界面上别处没有这个颜色**（主题是绿的、底色是米色、别的图是红的），
// 所以「数蓝色的像素」只会数到那一张图。
var fallbackBlue = color.RGBA{R: 30, G: 30, B: 220, A: 255}

// fallbackRed 是假图床给**别的图**（列表行、头像）的颜色。
var fallbackRed = color.RGBA{R: 220, G: 30, B: 30, A: 255}

// fallbackPics 是一台假图床：它把每一个请求的路径记下来，并按地址给图。
type fallbackPics struct {
	mu    sync.Mutex
	paths []string
}

func (p *fallbackPics) hit(path string) {
	p.mu.Lock()
	p.paths = append(p.paths, path)
	p.mu.Unlock()
}

// seen 把记下来的路径抄一份。
func (p *fallbackPics) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

// saw 报有没有请求过 path 这个**子串**。
func (p *fallbackPics) saw(part string) bool {
	for _, p := range p.seen() {
		if strings.Contains(p, part) {
			return true
		}
	}
	return false
}

// fallbackPicTransport 是图片缓存那一趟的网络层。
type fallbackPicTransport struct{ pics *fallbackPics }

func (t *fallbackPicTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.pics.hit(r.URL.Path)
	switch {
	case strings.Contains(r.URL.Path, "dead-first"):
		// 那条已经没了：4xx 是永久失败，`imgcache` 不会重试。
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
		}, nil
	case strings.Contains(r.URL.Path, "second-live"):
		return solidImage(fallbackBlue), nil
	default:
		return solidImage(fallbackRed), nil
	}
}

// solidImage 造一张纯色 JPEG 的回应。
func solidImage(c color.RGBA) *http.Response {
	img := image.NewRGBA(image.Rect(0, 0, 18, 18))
	for y := range 18 {
		for x := range 18 {
			img.Set(x, y, c)
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

// installFallbackImages 把 `s.images` 换成那台假图床。
func installFallbackImages(t *testing.T, s *Shell, pics *fallbackPics) {
	t.Helper()
	cache := imgcache.New(imgcache.Options{Client: &http.Client{Transport: &fallbackPicTransport{pics: pics}}})
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

// coverMedia 造一条**带封面**的内容。attr 非 0 就是失效的。
func coverMedia(id int, title, cover string, attr int) map[string]any {
	m := mediaOf(id, title)
	m["cover"] = cover
	m["attr"] = attr
	return m
}

// collectionShellWithDeadFirstCover 造一个「第一首失效、第二首正常」的歌单，
// 而**容器元信息给的封面还是第一首那张**（B 站就是这么算的）。
func collectionShellWithDeadFirstCover(t *testing.T) (*favBili, *Shell, *ui.Tester) {
	t.Helper()
	b := newCollectionBili(t)
	b.setMedias(
		coverMedia(1, "失效的那首", fallbackCoverDead, 1),
		coverMedia(2, "第二首", fallbackCoverLive, 0),
	)
	b.mu.Lock()
	b.infos["cover"] = fallbackCoverDead
	b.mu.Unlock()
	return collectionShellFrom(t, b, nil, "", "12345")
}

// headerCoverRect 是头图区那枚封面在画面上的框。
//
// # 为什么从标题倒推
//
// 头图区那一行是「封面 200×168 + 间距 16 + 右边那一列字」（见
// `viewCollectionHeader`），而封面自己**没有文字**（`imgcache.View` 不挂
// label）——`tt.Find` 找不到它。标题是右边那一列的第一行，它的左上角就在
// 封面右边过去 216 的位置、和封面的上沿齐平。
//
// ⚠️ **和 `glowPixelsInHeader` 用的是同一个锚点**（那边从标题往右看光晕，
// 这边从标题往左看封面）——两处量的都是同一块头图区。
func headerCoverRect(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	title, ok := tt.Find("通勤路上")
	if !ok {
		t.Fatalf("头图区的标题找不到，文字有 %q", tt.Texts())
	}
	return ui.Rect{
		X: title.X - collectionCoverW - space.Px16,
		Y: title.Y,
		W: collectionCoverW,
		H: collectionCoverH,
	}
}

// countColourIn 数框里有多少像素「是那个颜色」（每通道相差不超过 tol）。
//
// ⚠️ 差值用包里已有的 `absInt`（`icons_test.go`）——同包测试共用一份小工具。
func countColourIn(img *image.RGBA, r ui.Rect, want color.RGBA, tol int) int {
	x1 := min(int(r.X+r.W), img.Rect.Max.X)
	y1 := min(int(r.Y+r.H), img.Rect.Max.Y)
	n := 0
	for y := max(int(r.Y), 0); y < y1; y++ {
		for x := max(int(r.X), 0); x < x1; x++ {
			p := img.RGBAAt(x, y)
			if absInt(int(p.R)-int(want.R)) <= tol &&
				absInt(int(p.G)-int(want.G)) <= tol &&
				absInt(int(p.B)-int(want.B)) <= tol {
				n++
			}
		}
	}
	return n
}

// TestTheCollectionHeaderShowsTheFallbackCoverWhenTheFirstIsDead 头图区那一块
// **真的换成了第二首的封面**。
//
// 🔴 **判据是像素**：第一首那张是 404，所以「画出来的不是它」这件事只能看
// 画面——第二首那张是纯蓝的，而那个颜色界面上别处没有。
func TestTheCollectionHeaderShowsTheFallbackCoverWhenTheFirstIsDead(t *testing.T) {
	pics := &fallbackPics{}
	// ⚠️ **取色那一趟也要换掉**：头图区一拿到封面就会去取它的主色，而生产那个
	// `tintClient` 会**真的打 i0.hdslb.com**（AGENTS：测试不许碰真接口）。
	installFallbackTint(t, pics)
	b, s, tt := collectionShellWithDeadFirstCover(t)
	installFallbackImages(t, s, pics)
	waitSongs(t, tt, s, 2)

	// 等头图区那一块变成蓝的。
	waitFrames(t, tt, "头图区换上兜底封面", func() bool {
		return countColourIn(tt.Image(), headerCoverRect(t, tt), fallbackBlue, 24) > 2000
	})

	// ① 那张 404 的封面**确实被请求过**：这一条证明「兜底是因为它拉不下来」，
	//    而不是因为这一页压根没试过接口给的那张。
	if !pics.saw("dead-first") {
		t.Fatalf("接口给的那张封面一次都没请求——那兜底是被别的东西触发的：%v", pics.seen())
	}
	// ② 兜底那张是按**头图区自己的尺寸**要的（`200w_168h_1c`），不是列表行
	//    那一档小图：`GetSized` 的键是 (地址, 宽, 高)，要错了尺寸等于白画。
	if !pics.saw("@200w_168h_1c.webp") {
		tail := pics.seen()
		if len(tail) > 12 {
			tail = tail[:12]
		}
		t.Fatalf("头图区没有按 200×168 那一档要过图：%v", tail)
	}

	// ③ 头图区那块真的铺满了蓝（不是只描了一条边）。
	r := headerCoverRect(t, tt)
	blue := countColourIn(tt.Image(), r, fallbackBlue, 24)
	total := int(r.W * r.H)
	if blue < total*3/4 {
		t.Fatalf("头图区只有 %d/%d 个像素是兜底那张封面——那一块还是没换过来", blue, total)
	}

	// ④ 兜底只拉一页：页面自己的第一页 + 兜底那一趟，一共两次。
	if n := b.hitsOf("/x/v3/fav/resource/list"); n != 2 {
		t.Errorf("内容列表被拉了 %d 次，want 2（页面自己的第一页 + 兜底那一次）", n)
	}
} // TestTheCollectionHeaderDoesNotAskForAFallbackWhenTheCoverWorks 封面正常时
// **一次兜底都不问**。
//
// # 为什么这一条也要有
//
// 兜底那一趟是「第一次问就发一趟后台请求」。要是头图区每帧无条件地问，那么
// 每个歌单都会**多拉一页内容**——而那一页恰好是用户正在看的那一页，界面上
// 完全看不出来（只是多了一趟网络）。
func TestTheCollectionHeaderDoesNotAskForAFallbackWhenTheCoverWorks(t *testing.T) {
	pics := &fallbackPics{}
	installFallbackTint(t, pics)
	b := newCollectionBili(t)
	b.setMedias(
		coverMedia(1, "正常的一首", fallbackCoverLive, 0),
		coverMedia(2, "第二首", fallbackCoverLive, 0),
	)
	b.mu.Lock()
	b.infos["cover"] = fallbackCoverLive
	b.mu.Unlock()
	_, s, tt := collectionShellFrom(t, b, nil, "", "12345")
	installFallbackImages(t, s, pics)
	waitSongs(t, tt, s, 2)

	// 等头图区那张封面真的下来（那张也是蓝的——这一条只关心请求次数，
	// 但要等图落地才算走到稳态）。
	waitFrames(t, tt, "头图区封面下来", func() bool {
		return countColourIn(tt.Image(), headerCoverRect(t, tt), fallbackBlue, 24) > 2000
	})
	// 再多画几帧：兜底要是被每帧问一次，这里就会多出请求。
	for range 10 {
		tt.Frame()
	}
	if n := b.hitsOf("/x/v3/fav/resource/list"); n != 1 {
		t.Errorf("封面是好的，内容列表却被拉了 %d 次——头图区在无条件地问兜底"+
			"（那一趟是白花的一页网络）", n)
	}
}

// TestTheCollectionHeaderGlowFollowsTheFallbackCover 光晕的颜色跟着**兜底那张**
// 走，不是第一首（失效那张）的。
//
// # 为什么
//
// 光晕是「封面主色」在头图区那层渐变。兜底换了图而光晕没换的话，用户看到的是
// 「封面是第二首的、光晕是第一首的」——而第一首那张往往**根本没有颜色可给**
// （图都没了），于是症状是「头图有封面、却没有光晕」。
func TestTheCollectionHeaderGlowFollowsTheFallbackCover(t *testing.T) {
	pics := &fallbackPics{}
	installFallbackTint(t, pics)
	b, s, tt := collectionShellWithDeadFirstCover(t)
	installFallbackImages(t, s, pics)
	waitSongs(t, tt, s, 2)

	// 等取色那一趟回来：色相落在蓝色那一档（around 240）。
	waitFrames(t, tt, "兜底封面的色取到", func() bool {
		cover := s.collectionHeaderCover(s.collection.ref, s.app.Collection(s.collection.ref))
		if cover != fallbackCoverLive {
			return false
		}
		hue, sat := s.tint.sync(cover, func() {})
		return sat > 0 && hue > 200 && hue < 270
	})

	// ⚠️ **断言画面**：头图区右上方那一带被染上了蓝（不是只看 `sync` 的返回值
	// ——那和页面用没用它是两件事）。
	if n := blueGlowPixelsInHeader(t, tt); n == 0 {
		t.Fatalf("头图区没有蓝色光晕——封面换了兜底那张，光晕却没用它的颜色")
	}
	_ = b
}

// installFallbackTint 把取色那一趟的客户端换成一台「按地址给颜色」的假图床。
//
// ⚠️ **失效那张要给 404**：它要是也给一张蓝图，这一条就分不出「光晕用的是
// 兜底那张」还是「用的是第一张」了。
func installFallbackTint(t *testing.T, pics *fallbackPics) {
	t.Helper()
	orig := tintClient
	t.Cleanup(func() { tintClient = orig })
	tintClient = &http.Client{Transport: tintPicTransport{pics: pics}}
}

// tintPicTransport 只在取色那一趟上生效：失效地址 404，别的给蓝图。
type tintPicTransport struct{ pics *fallbackPics }

func (t tintPicTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.pics.hit(r.URL.Path)
	if strings.Contains(r.URL.Path, "dead-first") {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
		}, nil
	}
	return solidImage(fallbackBlue), nil
}

// blueGlowPixelsInHeader 数头图区那一块里有多少像素**带上了封面的蓝**。
//
// 取法照 `glowPixelsInHeader`（那条数的是红）：从标题右边往外看，因为封面本身
// 在那儿左边、它也是蓝的——不避开的话数到的是封面不是光晕。
//
// 判据是「B 明显高于 R」：光晕是 18% 的蓝压在浅色底（239,233,222）上，混出来
// 是 (200,196,215) 那一档——**不是纯蓝**；而底色本身 B 比 R 低 17。
func blueGlowPixelsInHeader(t *testing.T, tt *ui.Tester) int {
	t.Helper()
	cover, ok := tt.Find("通勤路上")
	if !ok {
		t.Fatal("头图区的标题找不到")
	}
	img := tt.Image()
	y0 := max(int(cover.Y)-14, 0)
	y1 := min(int(cover.Y)+6, img.Rect.Max.Y)
	x0 := int(cover.X) + int(cover.W) + 8
	x1 := min(x0+200, img.Rect.Max.X)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			p := img.RGBAAt(x, y)
			if int(p.B)-int(p.R) >= 10 && p.B >= 205 {
				n++
			}
		}
	}
	return n
}
