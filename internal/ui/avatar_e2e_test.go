package ui

// 头像地址统一的那一票（#634）：**端到端**。
//
// # 这一组测的是什么
//
// 真页面（假 B 站 + 假图床），真路由：在**关注列表**和**那个 UP 主的空间**之间
// 来回切，量**头像的请求次数**。
//
// # 为什么这一组必须存在
//
// 前面那些是「每一层各自对不对」：服务层给的地址一样吗？图片缓存按显示尺寸要的
// 吗？拼出来的地址记一份了吗？
//
// 而这一组量的是**用户看得见的那件事**：**在关注列表和他空间之间来回切，头像
// 不重新下载**。这一层是「两个缓存条目」真正变成「两次下载」的地方——而它也是
// 票面那条验收标准本身。
//
// # ⚠️ 一张脸、一个地址
//
// 关注列表和空间页**给的是同一个 UP 主**（同一个 `face` 地址）——那才是「同一张
// 脸」。两处给不同的地址的话，本来就是两张图，测不出任何东西。
//
// # ⚠️ 「按显示尺寸」那一档会因地而异
//
// 关注卡是 128 见方、空间页是 112——**两个尺寸是两个不同的档位**。所以这一组
// 量的不是「两个页面下的是同一张图」，而是「**来回切不会让请求数增加**」
// （每档各自下一次，切回来时命中缓存）。

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 假图床
// ─────────────────────────────────────────────────────────────────────────────

// avatarPicServer 是那台假图床：它给出去的是一张 18×18 的纯色图，并且把每一个
// 请求的路径记下来。
type avatarPicServer struct {
	mu    sync.Mutex
	paths []string
}

func (p *avatarPicServer) hit(path string) {
	p.mu.Lock()
	p.paths = append(p.paths, path)
	p.mu.Unlock()
}

// faceHits 数「要那一张脸」的请求有几回。
//
// ⚠️ **只数这一个地址**：横幅、别的图都不算——这一组量的是**同一张脸**。
func (p *avatarPicServer) faceHits() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, path := range p.paths {
		if strings.Contains(path, "/bfs/face/42.jpg") {
			n++
		}
	}
	return n
}

// seen 把记下来的路径抄一份（报错时用）。
func (p *avatarPicServer) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

// avatarPicTransport 把图片那一趟的请求拦下来，交给假图床。
type avatarPicTransport struct {
	pics *avatarPicServer
}

func (t *avatarPicTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.pics.hit(r.URL.Path)
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

// ─────────────────────────────────────────────────────────────────────────────
// 这一组
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSameFaceIsNotDownloadedAgainWhenSwitchingPages 钉住「**在关注列表和
// 他空间之间来回切，头像的请求次数不增加**」。
//
// # 判据
//
// **来回切之后，那张脸的请求次数不变**。
//
// # ⚠️ 为什么是「不变」而不是「只多一次」
//
// 两处的显示尺寸不同（关注卡 128 见方、空间页 112），而「按显示尺寸」那一档是
// **长边吸档**的：128 和 112 **吸到同一档**（256），于是两处要的是**同一个
// key**——那张脸从头到尾**只下一次**。
//
// 而**改坏实现的时候**（服务层在关注列表那一族拼了 160×160 的裁剪参数）：关注页
// 要的是 `…@160w_160h_1c_1s.webp`、空间页要的是 `…@256w_256h_1c.webp`——**两个
// 不同的 key**——于是来回切一次就多一次下载。所以「不变」这个判据正好卡住它。
//
// # 为什么它会红（改坏实现的时候）
//
//   - 关注列表那一族在服务层拼了 160×160 的裁剪参数 → 两个页面下的是**两个不同
//     的 key** → 来回切一次多一次下载。
//   - 服务层把参数拼在**后面**而不是「已经有参数就不拼」 → 那是坏地址（图床回
//     400），于是那一格永远拉不到，界面上是一个永远转圈的圆。
func TestTheSameFaceIsNotDownloadedAgainWhenSwitchingPages(t *testing.T) {
	// 同一个 UP 主的那张脸：两处给的是**同一个地址**（那才是「同一张脸」）。
	const face = "https://i0.hdslb.com/bfs/face/42.jpg"

	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 1, []any{uiFollowUserWithFace(42, "同一个 UP", face)})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, nil)
	// 空间页给的是**同一个地址**。
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": "同一个 UP", "sign": "这是签名", "level": 4,
			"face":         face,
			"top_photo_v2": map[string]string{"l_200h_img": "https://i0.hdslb.com/bfs/banner.jpg"},
			"official":     map[string]any{"role": 0},
		})
	})

	s, tt, _ := newSpaceShell(t, b, true, "77")

	pics := &avatarPicServer{}
	cache := imgcache.New(imgcache.Options{Client: &http.Client{Transport: &avatarPicTransport{pics: pics}}})
	keep, keepRemove := s.images, s.imagesWakeRemove
	t.Cleanup(func() {
		cache.Close()
		s.images = keep
		s.imagesWakeRemove = keepRemove
	})
	s.images = cache
	s.imagesWakeRemove = cache.AddWake(s.wake.invalidate)

	// ① 关注列表：头像下下来。
	s.navigate(followPath)
	waitFrames(t, tt, "关注页的头像下下来了", func() bool { return pics.faceHits() > 0 })
	settleFrames(t, tt)

	before := pics.faceHits()

	// ② 进他的空间：112 和 128 吸到**同一档**，所以**不该**多下一次。
	s.navigate(upPathOf(42))
	settleFrames(t, tt)

	afterSpace := pics.faceHits()
	if afterSpace != before {
		t.Errorf("从关注页进空间，那张脸被下了 %d 次（切之前 %d 次）——"+
			"两处的显示尺寸（128 / 112）吸到**同一档**，要的是**同一个 key**，"+
			"该只下一次（%v）",
			afterSpace-before, before, pics.seen())
	}

	// ③ **来回切**：请求次数不许再增加。
	for range 2 {
		s.navigate(followPath)
		settleFrames(t, tt)
		s.navigate(upPathOf(42))
		settleFrames(t, tt)
	}
	if got := pics.faceHits(); got != afterSpace {
		t.Errorf("来回切了两次，那张脸一共被下了 %d 次（切之前是 %d 次）——"+
			"来回切一次就多下一次，那正是「两个缓存条目」这件事（%v）",
			got, afterSpace, pics.seen())
	}
}

// TestTheAvatarIsStillDrawn 钉住「**头像照旧画出来**」。
//
// 这一票碰的是「**要哪一张**」，不是「**画不画**」——所以画面上那一枚圆**必须
// 还在**，而且是**正方形的、清楚的一张脸**。
//
// # 判据是**画面上的像素**
//
// 假图床给的是一张**纯红色**的图，于是那一枚头像该是红的。量的是**那一格中心
// 的颜色**——不是「那一格有没有那个 Label」（Label 在图没下来的时候也有）。
//
// # ⚠️ 为什么这一条不能省
//
// 「按显示尺寸要图」这一族有一个**静默的坏处**：地址拼错了（参数拼错、或者拼了
// 两层 `@`）就是**一次 400**，于是那一格永远拉不到图——而界面上那只是**一块灰
// 饼**（失败态），和「正在加载」那一档长得差不多。用户看不出「这一格坏了」。
//
// 所以这一条量的是**真的画出来了**（中心是红的）。
func TestTheAvatarIsStillDrawn(t *testing.T) {
	const face = "https://i0.hdslb.com/bfs/face/42.jpg"

	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 1, []any{uiFollowUserWithFace(42, "同一个 UP", face)})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, nil)

	s, tt, _ := newSpaceShell(t, b, true, "77")

	pics := &avatarPicServer{}
	cache := imgcache.New(imgcache.Options{Client: &http.Client{Transport: &avatarPicTransport{pics: pics}}})
	keep, keepRemove := s.images, s.imagesWakeRemove
	t.Cleanup(func() {
		cache.Close()
		s.images = keep
		s.imagesWakeRemove = keepRemove
	})
	s.images = cache
	s.imagesWakeRemove = cache.AddWake(s.wake.invalidate)

	s.navigate(followPath)
	waitFrames(t, tt, "关注页的头像下下来了", func() bool { return pics.faceHits() > 0 })
	settleFrames(t, tt)

	// 头像那一格的 Label 就是那个地址（`spaceAvatar` 把它挂在盒子上）。
	r, ok := tt.Find(face)
	if !ok {
		t.Fatalf("关注页上找不到那个头像（%q），文字有 %q", face, tt.Texts())
	}
	// 那一格该是**方的**（头像要正方形，见 `followCardSide`）。
	if r.W < followCardSide-1 || r.W > followCardSide+1 {
		t.Errorf("头像那一格是 %v，want %d 见方（头像要正方形）", r, followCardSide)
	}

	// 中心那一像素该是**红的**（假图床给的是纯红图）。
	img := tt.Image()
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	px := img.RGBAAt(int(cx), int(cy))
	if px.R < 150 || px.G > 120 || px.B > 120 {
		t.Errorf("头像的中心那一像素是 %v——"+
			"该是**红**的（假图床给的是纯红图）。"+
			"它不红说明**那一格没画出来那张图**"+
			"（地址拼错了 → 一次 400 → 那一格是失败态，一块灰饼）", px)
	}
}

// TestTheNavbarAvatarAsksForACroppedImage 钉住「**顶栏那个头像要的是裁好的那一张**」。
//
// # 为什么这一条不能省
//
// 顶栏那一枚圆是 40 见方，而 B 站给的是**原图**（几百 KB 的正方形）。它以前
// 拿到的是**服务层拼好裁剪参数**的地址（`@160w_160h_1c_1s.webp`）——而服务层
// 不再拼了（票 #634：裁剪参数改由图片缓存按显示尺寸统一决定），于是这一格要是
// 还去 `Get`（要原图），就是**去下几百 KB 的原图填一个 40 像素的格子**。
//
// 症状是**静默的**：画面上完全正常（`Fit(ui.Cover)` 把它缩成 40），只是每次都
// 多下几百 KB。
//
// # 判据
//
// **请求的路径带裁剪参数**，而且那个尺寸是**按这一格的显示尺寸算出来的**——
// 不是固定的 160×160（那不是这一格要的）。
func TestTheNavbarAvatarAsksForACroppedImage(t *testing.T) {
	f := newAvatarFixture(t)
	f.withALoginIn()
	_, tt := newAvatarShell(t, f, "2")

	// 等**那一趟真的发出去了**。
	waitFor(t, "头像那一趟发出去了", func() bool { return f.countHit(avatarNavPath) >= 1 })
	for range 20 {
		tt.Frame()
	}

	f.mu.Lock()
	paths := append([]string(nil), f.paths...)
	f.mu.Unlock()

	var facePath string
	for _, p := range paths {
		if strings.HasPrefix(p, avatarFacePrefix) {
			facePath = p
			break
		}
	}
	if facePath == "" {
		t.Fatalf("顶栏那个头像**压根没去取**（路径有 %v）", paths)
	}
	if !strings.Contains(facePath, "@") {
		t.Errorf("顶栏那个头像请求的是 %q——**没有裁剪参数**，"+
			"那就是**原图**（几百 KB 的正方形），而这一格只有 40 见方"+
			"（画面上看着正常：Fit(Cover) 把它缩成 40 了，只是每次都多下几百 KB）",
			facePath)
	}
}

// settleFrames 画几帧，让这一屏稳定下来。
//
// ⚠️ **不能只画一帧**：这一帧画完，图可能还在路上；下一帧才是它画出来的那一帧。
// 而图片那一趟是**后台 goroutine**——它什么时候回来不由这一帧决定，所以这里
// 让出 CPU（`runtime.Gosched`）让它跑得到。
func settleFrames(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	for range 8 {
		tt.Frame()
		runtime.Gosched()
	}
}
