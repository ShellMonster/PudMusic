package imgcache

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// 这一组验的是缓存的**界面出口**：四种状态（正在加载 / 就绪 / 拉失败 / 没有
// 地址）画出来的东西不一样，而且**用户分得开**。跑的是 MyGo 的 Tester，和真窗口
// 一模一样的绘制，只是画到内存。
//
// # 为什么断的是**像素**而不是文字
//
// 改之前那一格写着「载入中」/「封面加载失败」，那时候断文字就够用。改成上游那
// 样之后（`components/image/index.tsx:22-32`）那一格**一个字都不写**——所以现在
// 唯一能断「画出来的是哪一档」的只有画面本身：边框在不在、有没有图形、三档的
// 像素指纹两两不同。
//
// 这也正是 `TestTheThreeCoverStatesLookDifferentFromEachOther` 那一条存在的理由：
// 分得开这件事，只能在画面上验。

// TestCoverShowsPlaceholderThenImage 是 1.11 的验收：先占位，图来了自己换上去。
func TestCoverShowsPlaceholderThenImage(t *testing.T) {
	var arrived atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived.Add(1)
		<-release
		w.Write(pngBytes(t, 40, 40))
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	url := srv.URL + "/cover.png"

	// ⚠️ 画在窗口左上角（`View` 是整个 view 函数），所以那一格就是 (0,0)-(199,119)。
	tt := ui.NewTester(func(ctx *ui.Context) { View(ctx, c, url, 200, 120) }, 320, 200)
	loading := fingerprint(tt.Image(), 200, 120)
	if hasBorder(tt.Image(), 200, 120) {
		t.Errorf("正在加载那一格画出了边框——边框是「拿不到」的标记，正在加载连它都不画（%s）",
			strings.Join(loading, " "))
	}

	waitFor(t, 5*time.Second, "请求发出去了", func() bool { return arrived.Load() > 0 })
	close(release)

	waitFor(t, 5*time.Second, "图下完之后占位换成了图", func() bool {
		tt.Frame()
		return !sameColors(fingerprint(tt.Image(), 200, 120), loading)
	})
}

// TestCoverFailsIntoABorderedImageIcon 验「拉失败」是**描边框 + 一枚图片图标**，
// 而不是一个写着一行小字的灰块。
//
// ⚠️ 断的是**边框和图形**，不是某一句字：那一格现在一个字都不写（上游也是），
// 而「有个描边框」正是用户把它认成「这一格拿不到一张图」的凭据。
func TestCoverFailsIntoABorderedImageIcon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "没了", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	url := srv.URL + "/404.png"

	tt := ui.NewTester(func(ctx *ui.Context) { View(ctx, c, url, 200, 120) }, 320, 200)
	var failed []string
	waitFor(t, 5*time.Second, "画出失败态（边框 + 图片图标）", func() bool {
		tt.Frame()
		failed = fingerprint(tt.Image(), 200, 120)
		return hasBorder(tt.Image(), 200, 120) && len(failed) > 1
	})
	// 一圈边框 + 一枚图标 = 好几种颜色；只有一个颜色就是一块纯色块，而那正是
	// 「分不出是加载中还是加载失败」的那一档。
	if len(failed) < 2 {
		t.Errorf("失败那一格只有一种颜色（%v）——边框和那枚图片图标都没画出来",
			failed)
	}
	if _, _, _, _, ok := glyphBox(tt.Image(), 200, 120); !ok {
		t.Errorf("失败那一格里找不到图形（%v）——只有一圈边框中间是空的", failed)
	}
}

// TestTheThreeCoverStatesLookDifferentFromEachOther 是这一票核心那一条：
// **三种状态在画面上两两不同**。
//
// 🔴 这条不能靠「每一档画了什么」的几条断言凑出来——三条各自对，画出来仍然可能
// 两两一样（比如三档都画了边框、差别只在底色，而底色那一档在深色主题里分不开）。
// 唯一结实的是**两两比**：任何一档改得和另一档一样，这条立刻红。
//
// 夹具是内存 HTTP（`httptest`），一个真 B 站接口都没打。
func TestTheThreeCoverStatesLookDifferentFromEachOther(t *testing.T) {
	// 一个一直不返回的服务器 = 「永远在加载」那一档。
	//
	// ⚠️ 它靠一个 channel 卡住而不是 `time.Sleep`：`Server.Close` 会等所有
	// 连接结束，而一个正在 sleep 的处理函数要等满那 30 秒——整包测试白挂 30 秒
	// （第一版就是这么写的）。收尾时先关 channel，处理函数立刻返回。
	hold := make(chan struct{})
	stuck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-hold
	}))
	defer func() { close(hold); stuck.Close() }()
	// 一个立刻 404 的 = 「拉失败」那一档。
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "没了", http.StatusNotFound)
	}))
	defer broken.Close()

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ⚠️ **两套主题都要断**：这一档的差别一部分在底色上，而底色在浅色
			// 和深色下是反着的（浅色下「深一点」看得出，深色下要往亮的那头挪）。
			// 只断浅色的话，深色下三档糊成一片照样全绿。
			shots := map[string][]string{}
			for _, c := range []struct {
				what string
				url  string
			}{
				{"正在加载", stuck.URL + "/cover.png"},
				{"拉失败", broken.URL + "/404.png"},
				{"没有地址", ""},
			} {
				cache := New(Options{})
				tt := ui.NewTester(func(ctx *ui.Context) { View(ctx, cache, c.url, 200, 120) }, 320, 200)
				tt.SetDark(dark)
				tt.Frame()
				if c.what != "正在加载" {
					// 等它真的落到那一档（404 是后台一趟 HTTP）。
					waitFor(t, 5*time.Second, c.what+"那一档画出来了", func() bool {
						tt.Frame()
						return c.what == "没有地址" || cache.Get(c.url).Err != nil
					})
				}
				tt.Frame()
				shots[c.what] = fingerprint(tt.Image(), 200, 120)
				cache.Close()
			}
			for _, pair := range [][2]string{
				{"正在加载", "拉失败"},
				{"正在加载", "没有地址"},
				{"拉失败", "没有地址"},
			} {
				a, b := pair[0], pair[1]
				if sameColors(shots[a], shots[b]) {
					t.Errorf("「%s」和「%s」画出来一模一样——用户分不出这两种，"+
						"而这一票要的正是分得出（指纹 %v）", a, b, shots[a])
				}
			}
		})
	}
}

// TestTheIconGrowsWithTheCover 断那枚图片图标**跟着封面缩放**（上游的 40%）。
//
// ⚠️ 「封面那一格最小只有 32 像素」：图标写死一个像素数的话，在 32 那档会占掉
// 半个封面、在满屏那张又小得看不见。
//
// 🔴 判据是**各档的比例彼此接近**，而不是「正好等于 40%」：`image-line.svg`
// 的图形在它自己那个 24×24 的方框里并没有画满（那正是图标边长和图形外接框不是
// 一个数的缘故），量出来是 0.33 左右。钉住 40% 的话，下次换个图形更满一点的
// 图标就红，而那不是坏了；钉住「四档比例一致」才真的挡住「图标不缩放」这一种。
func TestTheIconGrowsWithTheCover(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "没了", http.StatusNotFound)
	}))
	defer srv.Close()

	var ratios []float64
	for _, side := range []int{32, 48, 80, 120} {
		cache := New(Options{})
		url := srv.URL + "/404.png"
		tt := ui.NewTester(func(ctx *ui.Context) {
			View(ctx, cache, url, float32(side), float32(side))
		}, side+40, side+40)
		tt.Frame()
		waitFor(t, 5*time.Second, "失败那一档画出来了", func() bool {
			tt.Frame()
			return cache.Get(url).Err != nil
		})
		tt.Frame()

		minX, _, maxX, _, ok := glyphBox(tt.Image(), side, side)
		if !ok {
			t.Fatalf("封面 %d 像素那一格里找不到图形", side)
		}
		ratios = append(ratios, float64(maxX-minX+1)/float64(side))
		cache.Close()
	}
	for i, r := range ratios {
		lo, hi := ratios[0], ratios[0]
		for _, o := range ratios {
			lo, hi = min32f(lo, o), max32f(hi, o)
		}
		// 允许四档之间差 25%：小那一档抗锯齿吃掉的比例更大，量出来本来就偏小。
		if hi > lo*1.25 {
			t.Errorf("各档图形占封面的比例差太多：%v——图标没有跟着封面一起缩放"+
				"（最小 %.2f，最大 %.2f，第 %d 档是 %.2f）", ratios, lo, hi, i, r)
		}
	}
}

// min32f / max32f 取两个里小的 / 大的那个。
func min32f(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max32f(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// TestCoverWithoutURLIsNotAskedFor 验没有地址时不发请求。
func TestCoverWithoutURLIsNotAskedFor(t *testing.T) {
	c := New(Options{})
	t.Cleanup(c.Close)

	tt := ui.NewTester(func(ctx *ui.Context) { View(ctx, c, "", 200, 120) }, 320, 200)
	n := 0
	c.entries.Range(func(_, _ any) bool { n++; return true })
	if n != 0 {
		t.Errorf("空地址也在缓存里落了 %d 条", n)
	}
	// 而画面上仍然画的是「拿不到」那一格（有边框 + 图标），不是一片空白。
	if !hasBorder(tt.Image(), 200, 120) {
		t.Error("空地址那一格画出来是空白——用户看到的是「这里缺一块」，" +
			"而不是「这一格拿不到图」")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 量画面的三个小工具
//
// **为什么只能从像素上量**：MyGo 的 Icon 没有元素树可以查（`mygo/ui/svg.go`
// 的文档：图标是装饰，辅助技术看不见它），而无头 Tester 的 `Find` 只认画出来的
// 文字和 `Label` 起的名字。这一格一个字都不写，于是「画出来没有」只能从画面上看。
//
// ⚠️ **三个工具都拿「那一格左上角就是 (0,0)」当前提**（`View` 画在窗口左上角，
// 见下面每个调用点传进去的 w/h）。边框是**画在格子里面**的（mygo 的 Border 文档：
// 「the border takes room within the element's size」，和 CSS 一样），所以
// y=0 那一行就是边框、y=1 开始才是底色——弄反了这一位，`hasBorder` 会对每一档
// 都报同一个结果，而那正是它最容易被写错的地方。
// ─────────────────────────────────────────────────────────────────────────────

// fingerprint 报那一格里**出现了哪几种颜色**（去重，不计次序）。
//
// 它是「两档画得一样不一样」这件事的判据：不比某一个点（那一个点可能恰好落在
// 两档都一样的底色上），而是把整格的颜色集合拿来比。
func fingerprint(img image.Image, w, h int) []string {
	seen := map[string]bool{}
	var out []string
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			k := hexOf(img.At(x, y))
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// hasBorder 报那一圈描边框在不在。
//
// 判据是**最上面那一行和它下面一行不同**：有边框时 y=0 是那条线、y=1 是底色；
// 没有边框时两行都是同一块底色。
//
// ⚠️ 取的是**顶边中点**而不是四角：那一格跟着主题圆角（`View` 的 `Radius` +
// `Clip`），四角那一圈被圆角啃掉了，在那儿量「有没有描边」量的是圆角。
func hasBorder(img image.Image, w, h int) bool {
	return img.At(w/2, 0) != img.At(w/2, 1)
}

// glyphBox 报图形（那枚图片图标）在那一格里的**外接框**。
//
// 量的范围是**边框以内**（从 3 开始，跳过边框那一圈和它的抗锯齿边），底色取
// 左上角那一小块——那里必然没有图形（图形是居中的）。
func glyphBox(img image.Image, w, h int) (minX, minY, maxX, maxY int, ok bool) {
	bg := img.At(4, 4)
	minX, minY, maxX, maxY = w, h, -1, -1
	for y := 3; y < h-3; y++ {
		for x := 3; x < w-3; x++ {
			if img.At(x, y) == bg {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	return minX, minY, maxX, maxY, maxX >= 0
}

// hex 把一个像素写成一个能进失败信息的短字符串。
//
// ⚠️ 取高 8 位：RGBA 是 16 位的，而界面测试只关心 8 位那一档精度；用满 16 位
// 的话抗锯齿边上的每一个像素都是一个「独一无二的颜色」，颜色集合会被噪声撑爆。
func hexOf(c color.Color) string {
	const digits = "0123456789abcdef"
	r, g, b, _ := c.RGBA()
	b8 := []byte{
		digits[(r>>12)&0xf], digits[(r>>8)&0xf],
		digits[(g>>12)&0xf], digits[(g>>8)&0xf],
		digits[(b>>12)&0xf], digits[(b>>8)&0xf],
	}
	return "#" + string(b8)
}

// sameColors 报两个颜色集合是不是同一个（次序无关）。
//
// 比的是**集合**而不是次序：像素扫描的先后和「画得对不对」没关系，
// 而次序一变就判成两档不同，那是假绿。
func sameColors(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
	}
	return true
}

// TestTheLoadedCoverHasAnEdgeOfItsOwn 钉住「有图那一档也描一圈边」
// （票 #244 / UX 报告 §2 的 E2）。
//
// # 为什么要这一档也有边
//
// 原来四档里只有三档有边界：「正在加载」没有，「拉失败」「没有地址」有
// （`drawPlaceholder` 的 `Border(1, t.Border)`）——**「图取到了、图本身是深色」
// 那一档没有**。于是一张近黑的封面（`#12141a` 那一档）在深色底上和行的底色
// 几乎一样，用户分不清那是「封面是黑的」还是「封面没加载出来」。
//
// # 判据是**顶边那一圈**
//
// `hasBorder` 量的是「最上面那一行和它下面一行不同」：有边时 y=0 是那条线、
// y=1 是底色（这里是图）。而「正在加载」那一档**没有**边（骨架屏连它都不画——
// 先给一圈线再撤掉会让那一格闪一下），所以这一条不会和骨架屏搞混。
//
// ⚠️ **深色主题**：浅色底下白封面和行底接近，深色底下近黑封面和行底接近——
// 两套主题里各有一档最难分辨，而这一条量的是**边在不在**（不是边有多明显），
// 所以两套主题里画法必须一样。
func TestTheLoadedCoverHasAnEdgeOfItsOwn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 一张近黑的封面（深色底下最难分辨的那一档）。
		w.Write(darkPNGBytes(t, 40, 40))
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	url := srv.URL + "/cover.png"

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := ui.NewTester(func(ctx *ui.Context) { View(ctx, c, url, 200, 120) }, 320, 200)
			tt.SetDark(dark)
			waitFor(t, 5*time.Second, "图换上来了，而且那一圈边在", func() bool {
				tt.Frame()
				return hasBorder(tt.Image(), 200, 120)
			})
			// ⚠️ **边在还不够**：那一格中间得是**图**，不是占位（占位那一档也有
			// 边）。中间那一块是图的颜色就说明换上了。
			if got := hexOf(tt.Image().At(100, 60)); got != "#12141a" {
				t.Errorf("那一格中间是 %s，want #12141a（图换上来了吗）", got)
			}
		})
	}
}

// darkPNGBytes 造一张近黑的纯色 PNG（深色底下最难分辨的那一档封面）。
func darkPNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 0x12, G: 0x14, B: 0x1a, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
