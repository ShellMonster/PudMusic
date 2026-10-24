package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组是票 #358：**开右侧的队列抽屉时，中间那一列的封面来回闪**。
//
// # 现象（用户实测 2026-10-24）
//
// 点开右侧的队列抽屉 → 中间列表的封面（那一格图形）抽搐、闪动——连续几帧跳，
// 不是抖一下就停。
//
// # 根因
//
// 抽屉里那二十几行**同时**要封面（它们是第一次露出来，那一刻全都要下），
// 一下多出几十张；缓存的字节上限（`imgcache.DefaultMaxBytes`，96MB）于是超了，
// LRU 开始挤——而它挤的顺序是「最久没用的先走」，**画面上正画着的那几张也在
// 里面**（它们只是这一帧刚被画过，在表里排得靠前，但前面还堵着刚要下来的
// 几十张 0 字节的：那几十张不占预算，挤的循环跳过它们继续往前，于是就挤到了
// 画面上那几张）。
//
// 挤掉的那一张下一帧就退回「正在加载」的占位——于是那一格闪一下，接着重新
// 下一次、再解一次码。**用户看见的就是「封面在有图和占位之间来回切」。**
//
// # 修法
//
// 挤的时候**跳过画面上正画着的那几张**（`widgets/imgcache` 的 `entry.onScreen`）。
// 宁可缓存超一点预算，也不要让用户看见封面闪。
//
// # 判据
//
// 和 issue #208 那一条同一副尺子：**已经就绪的那一张，不许退回占位**
// （`imgcache.State.Loading` 就是界面上那个占位）。
//
// ⚠️ **位图也钉住**：占位退回又回来时位图是**换了一个对象**的——
// 「不是每帧新建对象」这条票面要的就是它。
func TestOpeningTheQueueDrawerDoesNotMakeTheListCoversFlash(t *testing.T) {
	// ⚠️ **主题方案要各跑一遍**：这一包的测试默认量的是**经典**外观
	// （`app.DefaultThemePreset` 在 `diskguard_test.go` 的 TestMain 里被改成
	// 经典），而**真程序的默认是暖纸**——用户看见的那一版界面是暖纸。
	// 挤缓存这件事与配色无关，但「占位那一档」的颜色跟着主题走，所以两套
	// 都跑（票面点名要「至少再验一套非经典的」）。
	for _, preset := range []app.ThemePreset{app.ThemePresetClassic, app.ThemePresetWarmPaper} {
		t.Run(string(preset), func(t *testing.T) {
			openingTheQueueDrawerDoesNotMakeTheListCoversFlash(t, preset)
		})
	}
}

// openingTheQueueDrawerDoesNotMakeTheListCoversFlash 是上面那条的本体（按主题
// 方案各跑一遍）。
func openingTheQueueDrawerDoesNotMakeTheListCoversFlash(t *testing.T, preset app.ThemePreset) {
	body := coverJPEG(t, 1920, 1080)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	// 一屏歌，每一首都带封面地址。
	//
	// ⚠️ **两摊都要有**：新歌速递那一块网格（只有「流行」那一档画，见
	// homepage_newmusic.go）和下面那一档列表，各画各的封面——两摊加起来
	// 才够把缓存撑破。
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

	// 主题方案：**挂一份带方案名的设置**（设置要先挂上，`UpdateSettings` 才
	// 有地方落——「app: 还没有挂上设置，先调 MountSettings」）。
	st := app.DefaultSettings("")
	st.ThemePreset = preset
	s, tt := newShell(t, withSettings(st))
	s.home.SetSongs(songs)
	// 切到「流行」那一档：新歌速递那一块网格只在这一档上（上游也是这么摆的）。
	s.home.tab = homePopTab
	homeFrame(tt)
	// ⚠️ **推钟**：切 Tab 有过渡（票 #368），而 Tester 的钟站着不动——不推钟
	// 的话这一屏停在过渡的第一帧上（内容还在淡入），而这一条量的是封面上墙
	// 之后画面上还有没有占位。
	settleHome(tt)

	// 缓存的字节上限压到「一屏装不下」——真机上是**加上抽屉那二十几行**才超的，
	// 而那二十几张原图解码后每张 11MB（存下来缩到 768 边长，约 1.8MB），
	// 测试里造不出那么多张（造得出来也是几十兆的下载和解码，几秒钟）。
	//
	// ⚠️ **压小之后这一屏自己就装不下了**：那正是要验的局面——挤的循环会跑，
	// 而「画面上正画着的那几张」在修好之后是**不许挤**的。
	s.images.SetMaxBytes(imgcache.DefaultMaxBytes / 8)

	// 队列里摆二十首，封面也是那个服务器上的。
	//
	// ⚠️ **URL 要是不同的**：同一个 URL 在缓存里只有一条，那二十行画的还是
	// 首页那几张，压根撑不破预算。
	items := make([]app.Item, 0, 20)
	for i := range 20 {
		items = append(items, app.Item{
			Source: "local", Type: "audio",
			Title: "队列 " + strconv.Itoa(i),
			Cover: srv.URL + "/queue" + strconv.Itoa(i) + ".jpg",
		})
	}
	s.app.Queue().Reset(items...)

	// 这一屏的封面地址（首页那两摊）。
	urls := make([]string, 0, len(songs.tabs[homePopTab])+len(songs.new))
	for _, m := range append(songs.tabs[homePopTab], songs.new...) {
		urls = append(urls, m.Cover)
	}

	// 先等这一屏的封面都到齐（到齐之前退回占位是正常的：那一张还没下完）。
	ready := map[string]bool{}
	for range 600 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		for _, u := range urls {
			if s.images.Get(u).Ready() {
				ready[u] = true
			}
		}
		if len(ready) >= 20 {
			break
		}
	}
	if len(ready) == 0 {
		t.Fatalf("一帧都没画到封面——这一屏根本没画封面（文字有 %q）", tt.Texts())
	}
	t.Logf("这一屏画到了 %d 张封面，内存账 %d 字节（上限 %d）",
		len(ready), s.images.UsedBytes(), imgcache.DefaultMaxBytes/8)

	// **开抽屉**。抽屉那二十几行这时候第一次露出来，全都要封面——预算就是
	// 在这一刻超的。
	s.drawerOpen = true

	// 连着画一百帧：这一屏的封面不许退回占位（那正是「闪」）。
	coversStayPut(t, tt, s, urls, "开抽屉之后")

	// 出图：深浅两套各存一张（`BIU_SHOT_DIR` 不设就跳过）。
	//
	// ⚠️ **画面上要看得见的是「抽屉开着、中间那一列的封面全是图」**——那一格
	// 正是闪的那一格（闪的时候它会在「有图」和「占位」之间来回切）。
	shotDir := os.Getenv("BIU_SHOT_DIR")
	if shotDir == "" {
		return
	}
	save(t, shotDir, "81-浅色-开抽屉封面不闪-"+string(preset)+".png", tt)
	tt.SetDark(true)
	tt.Frame()
	save(t, shotDir, "81-深色-开抽屉封面不闪-"+string(preset)+".png", tt)
}

// ─────────────────────────────────────────────────────────────────────────────
// 历史记录页：**它一直闪**（用户 2026-10-24 报的**第二处**）
// ─────────────────────────────────────────────────────────────────────────────
//
// # 现象
//
// 用户原话：「**历史记录 页的icon一直在闪动**」——**不是点开抽屉才闪，是一直闪**。
//
// # 根因：和抽屉那一处**是同一个**
//
// 历史记录页的行也走 `widgets/list` 的封面那一路（`hover.RowCover` →
// `imgcache.ViewTitled`），所以它的封面也是**缓存里的一条**。而那一页一屏就有
// 十几张封面，缓存的字节上限（96MB）一超，LRU 就开始挤——**挤掉的正是画面上
// 正画着的那几张**，于是它们退回占位、重新下一次、再解一次码……
//
// **于是它一直闪**：挤 → 退占位 → 重新下 → 到了 → 又被挤 → ……（这一条就是
// issue #208 说的「有图 ↔ 占位反复切换」，只是那一页的封面更多、更容易超）。
//
// 抽屉那一处是「**一开抽屉**才闪」——因为是抽屉那十几行**同时**要封面把上限
// 顶破了；历史记录这一页是「**它自己那一屏**就把上限顶破了」。
//
// ⚠️ **所以修法是同一处**（`widgets/imgcache` 的挤的循环跳过画面上正画着的
// 那几张），而这一条把它**钉住**：将来动 `visibleFrames` 或 `BeginFrame` 的
// 接线，这一页会悄悄闪回去。
func TestTheHistoryPageCoversDoNotKeepFlashing(t *testing.T) {
	// 一个假图床：封面从这儿下（**不打真 B 站**）。
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(coverJPEG(t, 1920, 1080))
	}))
	defer imgSrv.Close()

	// 假 B 站：一页历史记录，每一条都带封面地址。
	f := newLibFake(t)
	f.mu.Lock()
	f.history[1] = historyBodyWithCovers(20, imgSrv.URL)
	f.mu.Unlock()

	tt, s := libraryShell(t, f, historyPath)
	// 缓存的字节上限压到「一屏装不下」——真机上是这一页自己的封面把它顶破的。
	s.images.SetMaxBytes(imgcache.DefaultMaxBytes / 8)

	// 这一屏的封面地址。
	urls := historyCoverURLs(20, imgSrv.URL)

	// 等这一页的封面都到齐（到齐之前退回占位是正常的：那一张还没下完）。
	ready := map[string]bool{}
	for range 600 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		for _, u := range urls {
			if s.images.Get(u).Ready() {
				ready[u] = true
			}
		}
		if len(ready) >= 10 {
			break
		}
	}
	if len(ready) == 0 {
		t.Fatalf("一帧都没画到封面——这一页根本没画封面（文字有 %q）", tt.Texts())
	}
	t.Logf("这一屏画到了 %d 张封面，内存账 %d 字节（上限 %d）",
		len(ready), s.images.UsedBytes(), imgcache.DefaultMaxBytes/8)

	// 记下每一张就绪的封面**是哪一个位图对象**：闪一次就是换了一个对象。
	bitmaps := map[string]any{}
	for _, u := range urls {
		if st := s.images.Get(u); st.Ready() {
			bitmaps[u] = st.Bitmap
		}
	}

	// **再开队列抽屉**：抽屉里那二十行又来一批新封面——真机上「一开抽屉就闪」
	// 就是这么来的。历史记录这一页不需要它（它自己那一屏就超了），但走一遍
	// 更接近用户看到的那个局面。
	items := make([]app.Item, 0, 20)
	for i := range 20 {
		items = append(items, app.Item{
			Source: "local", Type: "audio",
			Title: "队列 " + strconv.Itoa(i),
			Cover: imgSrv.URL + "/q" + strconv.Itoa(i) + ".jpg",
		})
	}
	s.app.Queue().Reset(items...)
	s.drawerOpen = true

	// 连着画一百帧：这一页的封面不许退回占位（那正是「一直闪」）。
	for frame := range 100 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		for _, u := range urls {
			st := s.images.Get(u)
			if st.Ready() {
				if old, ok := bitmaps[u]; ok && old != any(st.Bitmap) {
					t.Fatalf("第 %d 帧：%s 的封面**换了位图对象**"+
						"（%v → %v）——它被挤掉又下一次了（这就是「一直闪」）",
						frame, u, old, st.Bitmap)
				}
				continue
			}
			if _, ok := bitmaps[u]; ok {
				t.Fatalf("第 %d 帧：%s 从有图退回占位了（Loading=%v Err=%v）——\n"+
					"这就是「历史记录页的 icon 一直在闪」（票 #358）。\n"+
					"（内存账 %d 字节，缓存上限 %d 字节）",
					frame, u, st.Loading, st.Err, s.images.UsedBytes(), imgcache.DefaultMaxBytes/8)
			}
		}
	}
}

// coversStayPut 断「这一屏的封面不许来回切」——首页、历史记录、稍后再看三处
// 共用这一副尺子。
//
// what 是这一页的名字（报错时用）。
func coversStayPut(t *testing.T, tt *mygoui.Tester, s *Shell, urls []string, what string) {
	t.Helper()
	// 记下每一张就绪的封面**是哪一个位图对象**：闪一次就是换了一个对象。
	bitmaps := map[string]any{}
	for _, u := range urls {
		if st := s.images.Get(u); st.Ready() {
			bitmaps[u] = st.Bitmap
		}
	}
	for frame := range 100 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		for _, u := range urls {
			st := s.images.Get(u)
			if st.Ready() {
				if old, ok := bitmaps[u]; ok && old != any(st.Bitmap) {
					t.Fatalf("%s 第 %d 帧：%s 的封面**换了位图对象**"+
						"（%v → %v）——它被挤掉又下一次了（这就是「闪」）",
						what, frame, u, old, st.Bitmap)
				}
				continue
			}
			if _, ok := bitmaps[u]; ok {
				t.Fatalf("%s 第 %d 帧：%s 从有图退回占位了（Loading=%v Err=%v）——\n"+
					"这就是「封面在有图和占位之间来回切」（票 #358）。\n"+
					"（内存账 %d 字节，缓存上限 %d 字节）",
					what, frame, u, st.Loading, st.Err, s.images.UsedBytes(), imgcache.DefaultMaxBytes/8)
			}
		}
	}
}

// historyBodyWithCovers 装一页历史记录，每一条都带封面地址（封面在那个假图床上）。
//
// ⚠️ **和 `historyBody` 差在封面地址上**：那一份把封面写成空串（于是界面上是
// 「没有地址」那一档，不进缓存）；这一份给真的地址，于是封面走缓存——
// 而闪的就是缓存里那一条。
func historyBodyWithCovers(count int, coverURL string) string {
	items := make([]map[string]any, 0, count)
	for i := range count {
		items = append(items, map[string]any{
			"title":       "看过 " + strconv.Itoa(i),
			"cover":       historyCoverURL(coverURL, i),
			"author_name": "UP " + strconv.Itoa(i),
			"author_mid":  11,
			"view_at":     now.Add(-time.Duration(i) * time.Hour).Unix(),
			"progress":    10,
			"duration":    100,
			"kid":         float64(170001 + i),
			"history": map[string]any{
				"bvid": "BVSAME", "cid": 4001 + i, "business": "archive",
			},
		})
	}
	return biliOK(map[string]any{
		"has_more": false,
		"page":     map[string]any{"pn": 1},
		"list":     items,
	})
}

// historyCoverURL 是第 i 条历史记录的封面地址。
func historyCoverURL(base string, i int) string {
	return base + "/h" + strconv.Itoa(i) + ".jpg"
}

// historyCoverURLs 是那一页所有封面的地址。
func historyCoverURLs(count int, base string) []string {
	urls := make([]string, 0, count)
	for i := range count {
		urls = append(urls, historyCoverURL(base, i))
	}
	return urls
}

// TestTheLaterPageCoversDoNotKeepFlashing 普查的第三页：**稍后再看**。
//
// 它和历史记录走的是**同一套行**（`internal/ui/librarypage.go` 的
// `viewLibraryRowHead` → `hover.RowCover` → 同一个 `imgcache`），所以它也是
// 「一屏封面把缓存的字节上限顶破 → 挤掉画面上正画着的那几张 → 一直闪」。
//
// **这一条把「同一套代码路径 ⇒ 同一个修法」这件事量出来**：三页（首页、
// 历史记录、稍后再看）量过，其余画封面的画面按同一条路径推。
func TestTheLaterPageCoversDoNotKeepFlashing(t *testing.T) {
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(coverJPEG(t, 1920, 1080))
	}))
	defer imgSrv.Close()

	f := newLibFake(t)
	f.mu.Lock()
	f.later[1] = laterBodyWithCovers(20, imgSrv.URL)
	f.mu.Unlock()

	tt, s := libraryShell(t, f, laterPath)
	s.images.SetMaxBytes(imgcache.DefaultMaxBytes / 8)

	urls := laterCoverURLs(20, imgSrv.URL)
	ready := map[string]bool{}
	for range 600 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		for _, u := range urls {
			if s.images.Get(u).Ready() {
				ready[u] = true
			}
		}
		if len(ready) >= 10 {
			break
		}
	}
	if len(ready) == 0 {
		t.Fatalf("一帧都没画到封面——这一页根本没画封面（文字有 %q）", tt.Texts())
	}
	t.Logf("这一屏画到了 %d 张封面，内存账 %d 字节（上限 %d）",
		len(ready), s.images.UsedBytes(), imgcache.DefaultMaxBytes/8)

	coversStayPut(t, tt, s, urls, "稍后再看页")
}

// laterBodyWithCovers 装一页「稍后再看」，每一条都带封面地址。
//
// ⚠️ **和 `laterBody` 差在 `pic` 上**：那一份把封面写成空串（于是界面上是
// 「没有地址」那一档，不进缓存）；这一份给真的地址。
func laterBodyWithCovers(count int, coverURL string) string {
	items := make([]map[string]any, 0, count)
	for i := range count {
		items = append(items, map[string]any{
			"aid": int64(170001 + i), "bvid": fmt.Sprintf("BVlater%02d", i),
			"cid":      int64(3001 + i),
			"title":    "稍后 " + strconv.Itoa(i),
			"pic":      laterCoverURL(coverURL, i),
			"duration": 185, "progress": 0,
			"pubdate": int64(1700000000), "add_at": now.AddDate(0, 0, -i).Unix(), "is_pgc": false,
			"owner": map[string]any{"mid": 11, "name": "UP " + strconv.Itoa(i)},
			"stat":  map[string]any{"view": int64(1000 * (i + 1))},
		})
	}
	return biliOK(map[string]any{"count": count, "list": items})
}

// laterCoverURL 是第 i 条「稍后再看」的封面地址。
func laterCoverURL(base string, i int) string {
	return base + "/l" + strconv.Itoa(i) + ".jpg"
}

// laterCoverURLs 是那一页所有封面的地址。
func laterCoverURLs(count int, base string) []string {
	urls := make([]string, 0, count)
	for i := range count {
		urls = append(urls, laterCoverURL(base, i))
	}
	return urls
}
