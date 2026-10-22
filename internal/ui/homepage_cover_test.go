package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组是「首页推荐的封面在有图 ↔ 占位之间反复切换」那个 bug 的界面那一半
// （issue #208）。
//
// 判据就是票面写的那一条：**同一个 URL 连着画 N 帧，状态不回到 Loading**。
//
// # 根因（两半，缺一不可）
//
// 1. **存下来的那一张图太大**：B 站的封面是 1920×1080 的原图，解码之后
//    RGBA 加 mipmap 链是 11MB，而界面上最大的那一格是满屏播放器那张
//    （384 DIP）。一屏十几张就是 170MB，远超缓存的字节上限——于是 LRU 只能把
//    **正在显示着的**封面挤掉，挤掉的那一张下一帧就退回占位，重新下一次。
//    （缩放那一半在 `widgets/imgcache/scale.go`。）
// 2. **LRU 的字节账会漏**：一条在「解完码」之前被挤掉的条目，解码回来之后
//    还会被记一笔账，而那一笔**再也挤不掉**——挤的循环最后把整张表清空还挤
//    不下，之后每来一张图就把表里剩下的全挤掉。（那一半在 `widgets/imgcache/lru.go`。）
//
// # 为什么这一条要在界面这一层验
//
// 缓存那一层已经有一条同样的判据（`widgets/imgcache` 里的
// `TestTheSameCoverStaysReadyAcrossFrames`）。这一条量的是**真的在画的那一屏**：
// 首页（新歌速递那一块网格 + 下面那一档列表），画的是真窗口那一套布局，
// 走的是真的 `imgcache.View`。界面这一层才看得出来「一屏封面到底有几张」——
// 而那正是根因的第一半要的那个数。

// coverJPEG 编一张 w×h 的 JPEG，当封面用。
//
// ⚠️ **要 1920×1080**：B 站的封面就是这么大（`service/bili` 那几个接口给的
// `pic` 没有缩放参数），而这一条要验的就是「一屏封面按原图存会超出缓存的
// 字节上限」。用小图的话这一屏压根装得下，挤不掉，也就验不到什么。
func coverJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x / 8), G: uint8(y / 8), B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 55}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestTheHomeCoversDoNotGoBackToPlaceholder 是 issue #208 的界面那一半。
//
// **同一个 URL 连着画 N 帧，状态不回到 Loading。**
//
// 判据问的是缓存给的状态（`imgcache.State.Loading` 就是界面上那个占位），
// 而不是像素：像素要判「这一格画的是图还是骨架」得先知道骨架是什么颜色，
// 而骨架的颜色跟着主题走（深浅两套还不一样）——像素那一层放在最后单独做。
func TestTheHomeCoversDoNotGoBackToPlaceholder(t *testing.T) {
	body := coverJPEG(t, 1920, 1080)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	// 一屏歌，每一首都带封面地址。
	//
	// ⚠️ **两摊都要有**：新歌速递那一块网格（只有「流行」那一档画，见
	// homepage_newmusic.go）和下面那一档列表，各画各的封面——而这一屏上
	// 看得见的封面加起来要超过缓存的字节上限（按原图存是 11MB 一张），
	// 挤得动，才验得到「挤掉正在显示着的那一张」这件事。
	withCovers := func(prefix string, n int) []list.Music {
		songs := homeSongsOf(prefix, n)
		for i := range songs {
			songs[i].Cover = srv.URL + "/cover" + prefix + strconv.Itoa(i) + ".jpg"
		}
		return songs
	}
	songs := homeSongs{
		tabs: [homeTabCount][]list.Music{homePopTab: withCovers(homeTabs[homePopTab], 30)},
		new:  withCovers("新歌", 12),
	}

	s, tt := newHome(t, songs)
	// 切到「流行」那一档：新歌速递那一块网格只在这一档上（上游也是这么摆的）。
	s.home.tab = homePopTab
	homeFrame(tt)

	urls := make([]string, 0, len(songs.tabs[homePopTab])+len(songs.new))
	for _, m := range append(songs.tabs[homePopTab], songs.new...) {
		urls = append(urls, m.Cover)
	}

	// 画到这一屏的封面都到齐为止；**每一帧都问一遍：已经就绪的那一张，
	// 不许退回占位**。
	//
	// ⚠️ **问的时机是「画完之后」**：占位是那一帧画出来的东西，状态得在
	// 那一帧之后才问——问早了问的是上一帧的状态。
	ready := map[string]bool{}
	before, quiet := 0, 0
	for frame := range 600 {
		tt.Frame()
		for _, u := range urls {
			st := s.images.Get(u)
			if st.Ready() {
				ready[u] = true
				continue
			}
			if ready[u] {
				t.Fatalf("第 %d 帧：%s 从有图退回占位了（Loading=%v Err=%v）——\n"+
					"这就是「封面在有图和占位之间反复切换」那件事（issue #208）。\n"+
					"（内存账 %d 字节，缓存上限 %d 字节）",
					frame, u, st.Loading, st.Err, s.images.UsedBytes(), imgcache.DefaultMaxBytes)
			}
		}
		// 到齐了就停：连续二十帧没有新的封面就绪，说明这一屏的都到了
		//（没被画到的那几张根本不会被要——列表是虚拟的）。
		if len(ready) == before {
			quiet++
			if quiet >= 20 {
				break
			}
		} else {
			quiet = 0
		}
		before = len(ready)
		time.Sleep(2 * time.Millisecond)
	}
	if len(ready) == 0 {
		t.Fatalf("一帧都没画到封面——这一屏根本没画封面（文字有 %q）", tt.Texts())
	}
	t.Logf("这一屏画到了 %d 张封面，内存账 %d 字节", len(ready), s.images.UsedBytes())

	// 再连着画三十帧：这一段是「都到齐之后」的那一段，票面要的就是它
	//（「首页连续 30 秒不再有图 ↔ 占位反复」）。
	for frame := range 30 {
		tt.Frame()
		for _, u := range urls {
			st := s.images.Get(u)
			if st.Ready() {
				continue
			}
			if ready[u] {
				t.Fatalf("都到齐之后的第 %d 帧：%s 退回占位了（Loading=%v Err=%v）——\n"+
					"封面在有图和占位之间反复切换（issue #208）",
					frame, u, st.Loading, st.Err)
			}
		}
		time.Sleep(2 * time.Millisecond)
	}

	// 逐像素：那一格画的是**图**，不是骨架。
	//
	// ⚠️ **状态是「就绪」还差一步**：状态说「有图」，画面上是不是真的画出来了
	// 是另一件事（缩放、裁剪、画的那一支）。所以再取一个像素看看：骨架屏是
	// 一块**平色**（`Surface` 往 `Background` 挪一半），而封面是一张渐变图，
	// 差得开。
	coverPixelShowsTheImage(t, s, tt)

	// 出图：深浅两套各存一张（`BIU_SHOT_DIR` 不设就跳过）。
	shotDir := os.Getenv("BIU_SHOT_DIR")
	if shotDir == "" {
		return
	}
	save(t, shotDir, "80-浅色-首页封面不再闪.png", tt)
	tt.SetDark(true)
	tt.Frame()
	save(t, shotDir, "80-深色-首页封面不再闪.png", tt)
}

// coverPixelShowsTheImage 逐像素验「新歌速递」那一格的封面画的是图。
//
// ⚠️ **取样点在那一格的左上角内侧**：那一格的结构是「封面 + 歌名 + UP 名」
// （见 homepage_newmusic.go 的 viewCard），封面是这一格的第一样东西、在它
// 的左上角，边长是方的。`Tester.Find` 认出来的是**那一格**（它把整张卡片
// 的盒子给了出来：宽是格子的宽，高是封面加两行字），所以从它的左上角往里
// 取一个点就落在封面上。
//
// ⚠️ **要往里挪一个圆角**：封面是圆角裁剪的（`imgcache.View` 自己带
// `Clip()`），角上那一点是**裁掉的**，取到的是页面底色而不是图。
//
// 判据是「那个点不是骨架屏的颜色」：骨架屏是 `Surface` 往 `Background` 挪
// 一半（`imgcache/view.go` 的 `skeletonMix`），是一块平色；而这张封面是
// 一张渐变图——差得开。
func coverPixelShowsTheImage(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	rect, ok := tt.Find(songName("新歌", 1))
	if !ok || rect.W <= 0 {
		t.Fatalf("找不到新歌速递那一格的歌名——这一屏没画它（文字有 %q）", tt.Texts())
	}
	theme := BuildTheme(s.app.SettingsSnapshot(), false)
	x := int(rect.X) + int(theme.Radius) + 2
	y := int(rect.Y) + int(theme.Radius) + 2
	img := tt.Image()
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		t.Fatalf("取样点 (%d, %d) 不在画面上", x, y)
	}
	got := img.RGBAAt(x, y)

	// ⚠️ **0.5 是 `imgcache` 里那个 `skeletonMix`**（`widgets/imgcache/view.go`）：
	// 骨架屏的底色是从 `Surface` 往 `Background` 挪一半。
	skeleton := rgbaOf(theme.Surface.Mix(theme.Background, 0.5))
	if near(got, skeleton) {
		t.Errorf("那一格画的是骨架屏（取样点 (%d,%d) 的颜色 %v 和骨架屏 %v 一样）——\n"+
			"状态说「有图」，画面上却是占位", x, y, got, skeleton)
	}
}
