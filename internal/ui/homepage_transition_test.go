package ui

// 切 Tab 的过渡（票 #368）。
//
// 这一组钉三件事：
//
//  1. **切 Tab 有过渡**（不是硬切）；
//  2. **「流行」切进去不闪**：那一档还在拉的时候，**旧的内容留在画面上**，
//     「正在加载」那一格**不出现**；
//  3. **过渡走完之后，画面上就是新那一档**（过渡没有把内容停在半路上）。
//
// 判据是**画面**（像素）而不是状态字段：这一票动的是「画面上是什么」，
// 而「状态字段对了」不等于「画面对了」。
//
// # 为什么第一帧 ≠ 稳定帧就是「有过渡」的证据
//
// 硬切的话，新内容一上来就是最终样子——第一帧和稳定之后**一模一样**。
// 所以「两者不同」正是「中间有过一段」的证据；反过来，一个「切过去之后画面
// 变了、但变了两次」的实现（先空一下再换）也是绿的——那一条由下面「不闪」
// 那一组钉。

import (
	"image"
	"image/color"
	"math"
	"net/http"
	"runtime"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// 内容那一块在窗口里的位置（1200×800 的默认窗口）。
//
// ⚠️ **只比这一块**：点一下 Tab 会让主 Tab 那个滑块滑过去、选中的那一格换颜色
// ——那是**另一个动画**（`tabs.go` 的弹簧），它动不动的都跟「内容有没有过渡」
// 无关。整个窗口比的话，一个硬切的实现在这里也是绿的。
const (
	homeContentLeft = 224 // 侧栏右边那条沿（侧栏 200 + 内容区左边距）
	homeContentTop  = 120 // Tab 那一排下面
)

// contentChanged 是两帧在**内容那一块**里颜色变了的像素数。
func contentChanged(a, b *image.RGBA) int {
	var n int
	for y := homeContentTop; y < a.Rect.Max.Y; y++ {
		for x := homeContentLeft; x < a.Rect.Max.X; x++ {
			if color.RGBAModel.Convert(a.RGBAAt(x, y)) != color.RGBAModel.Convert(b.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

// TestSwitchingTabsIsNotAHardCut 钉住「切 Tab 有过渡」。
//
// 四步：记下切换前的画面 → 点另一个 Tab → 记下**切换后第一帧**的画面 →
// 推钟让过渡走完，再记一张。
//
// ⚠️ **推钟而不是数帧**（AGENTS.md §2）：Tester 的钟站着不动，「画了几帧」
// 数的不是时间；而过渡是按钟走的，不推钟它永远走不完。
func TestSwitchingTabsIsNotAHardCut(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	before := snapshot(tt)

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[1], err, tt.Texts())
	}
	tt.Frame()
	during := snapshot(tt)

	tt.Advance(homeBodyDuration + 100*time.Millisecond)
	tt.Frame()
	after := snapshot(tt)

	// 硬切的话这两张一模一样。
	if n := contentChanged(during, after); n == 0 {
		t.Errorf("切换之后的第一帧和过渡走完之后**一模一样**（内容那一块一个像素都没变）：" +
			"这是硬切，没有过渡")
	} else if n < 500 {
		t.Errorf("切换之后的第一帧和过渡走完之后只差 %d 个像素——"+
			"那一段过渡短得看不出（实测这一块在换的像素有两万多个：两档的行一样多，"+
			"每一行的字和那一格底色都在换）", n)
	}

	// 走完之后画面上是**新那一档**的歌。
	if !shows(tt, songName(homeTabs[1], 1)) {
		t.Errorf("过渡走完之后画面上没有新那一档的歌，文字有 %q", tt.Texts())
	}
	// 旧那一档的歌不在了。
	if shows(tt, songName(homeTabs[0], 1)) {
		t.Errorf("过渡走完之后旧那一档的歌还在画面上，文字有 %q", tt.Texts())
	}
	// 切之前那张画面和走完之后那张**不一样**（不然上面那条就是白断的）。
	if contentChanged(before, after) == 0 {
		t.Errorf("切换前后画面上一模一样：切过去之后画的还是原来那一档")
	}
}

// TestSwitchingToATabThatIsStillLoadingKeepsTheOldContentOnScreen 钉住
// **「流行」切进去不闪**。
//
// 走的是真路子：假 B 站把「流行」那一档要的那一趟按在门口不放，用户点了
// 「流行」之后那一趟还没回来。
//
// 三件事：
//
//  1. **旧的内容留在画面上**（「音乐 推的第一首」还在）；
//  2. **「正在加载流行推荐」不出现**（那一格只在真的没内容可画时才有——
//     票 #368 的验收第 3 条）；
//  3. 那一趟回来之后，画面上换成「流行」自己的歌。
//
// ⚠️ **第 2 条是这一票的正题**：此前切过去的第一帧画的是「正在加载」那一格，
// 数据到了再换成列表——画面上就是「空 → 一屏内容」的一跳（票 #368 的原话）。
func TestSwitchingToATabThatIsStillLoadingKeepsTheOldContentOnScreen(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)

	// 把「流行」那一档（音乐榜）要的那一趟按住在门口。
	gate := make(chan struct{})
	b.mu.Lock()
	rank := b.handlers[uiHomeMusicRank]
	b.mu.Unlock()
	b.on(uiHomeMusicRank, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		rank(w, r)
	})
	t.Cleanup(func() {
		// 用例在断言完之前就 return 了的话，别把收尾卡在这一趟上。
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档拉回来了", func() bool { return showsText(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homePopTab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[homePopTab], err, tt.Texts())
	}
	homeFrame(tt)

	// 那一档还在拉：屏幕上留着的应该是**旧的内容**。
	if !showsText(tt, "音乐 推的第一首") {
		t.Errorf("切到「流行」而它还在拉的时候，旧的内容不见了，文字有 %q", tt.Texts())
	}
	if showsText(tt, "正在加载流行推荐") {
		t.Errorf("切到「流行」而它还在拉的时候画了「正在加载流行推荐」——" +
			"那一格只在真的没内容可画时才该出现（票 #368）")
	}
	if showsText(tt, lookup(homePath).Empty.Title) {
		t.Errorf("切到「流行」而它还在拉的时候画了空状态，文字有 %q", tt.Texts())
	}

	// 放它过去：那一屏自己换掉（这一次**有**过渡，见上面那一条）。
	close(gate)
	waitFramesClock(t, tt, "「流行」那一档拉回来了", func() bool {
		return showsText(tt, "流行 榜上的歌")
	})
	if showsText(tt, "音乐 推的第一首") {
		t.Errorf("「流行」那一档到货了，画面上还是旧那一档的歌，文字有 %q", tt.Texts())
	}
}

// TestSwitchingToATabThatIsStillLoadingKeepsTheOldContentEvenAfterTheClockMoves
// 钉住**「切到一档而它还在拉」时不会因为钟走了就换过去**（票 #238 的评审）。
//
// # 这一条和上一条差在哪儿
//
// 上一条**只画帧不推钟**（Tester 的钟站着不动，AGENTS.md §2），于是它看不见
// 「过了 100ms 之后」的样子。而真窗口里钟**是走的**——退场那一段 100ms 一过，
// 内容就换过去了，而那一档的歌**还没到**，屏幕上于是出现「正在加载流行推荐」
// 那一格（票 #368 明令不许出现的那一格）。
//
// # 根因
//
// 「它到了没有」这件事**只能在 sync 层认**（见 homepage.go 的 `syncHome`）：
// sync 层跑在 view 之前，而那一档的请求也是**在那一层**发出去的（`pull()`）。
// 在 view 层认的话，那一帧的请求**还没发出去**（`loadingOf` 还是假），于是会
// 以为「它已经有着落了」而立刻开始换。
//
// ⚠️ **推着钟走 400ms**：真窗口里钟是走的，而这一段过渡是按钟走的。
func TestSwitchingToATabThatIsStillLoadingKeepsTheOldContentEvenAfterTheClockMoves(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)

	// 把「流行」那一档（音乐榜）要的那一趟按住在门口。
	gate := make(chan struct{})
	b.mu.Lock()
	rank := b.handlers[uiHomeMusicRank]
	b.mu.Unlock()
	b.on(uiHomeMusicRank, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		rank(w, r)
	})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档拉回来了", func() bool { return showsText(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homePopTab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[homePopTab], err, tt.Texts())
	}
	// ⚠️ **推着钟走 400ms**（真窗口里钟是走的）：退场那一段 100ms、进场 200ms，
	// 400ms 足够走完一整段。
	for range 25 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}

	// 那一档还在拉：屏幕上留着的应该是**旧的内容**。
	if !showsText(tt, "音乐 推的第一首") {
		t.Errorf("切到「流行」而它还在拉的时候（推着钟走了 400ms），旧的内容不见了，文字有 %q",
			tt.Texts())
	}
	// 而「正在加载流行推荐」那一格**不许出现**（票 #368 的验收第 3 条）。
	if showsText(tt, "正在加载流行推荐") {
		t.Errorf("切到「流行」而它还在拉的时候画了「正在加载流行推荐」——\n"+
			"那一格只在真的没内容可画时才有（票 #368）")
	}
}

// TestTheLoadingCellOnlyShowsWhenThereIsNothingToShow 钉住「正在加载」那一格
// **只在真的没内容可画时**出现。
//
// 两个方向都要：
//
//   - **启动时**（屏幕上确实一首歌都没有）→ 那一格出现；
//   - **切到一档而它还在拉**（屏幕上还有上一档）→ 那一格**不**出现。
//
// 第二个方向由上面那一条钉；这一条补的是第一个方向——少了它，一个「那一格
// 永远不出现」的实现在上面那一条里也是绿的。
func TestTheLoadingCellOnlyShowsWhenThereIsNothingToShow(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)
	gate := make(chan struct{})
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		uiExploreJSON(w, 0, homeArchiveData("音乐"))
	})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "那一趟已经发出去了", func() bool { return b.hit(uiHomeRegionFeed) })
	homeFrame(tt)

	// 屏幕上确实一首歌都没有 → 那一格该在。
	if !showsText(tt, "正在加载音乐推荐") {
		t.Errorf("启动时屏幕上什么都没有，却没说「正在加载」，文字有 %q", tt.Texts())
	}
	// 而空状态**不**该在（那是「B 站真的没有推荐」的说话方式）。
	if showsText(tt, lookup(homePath).Empty.Title) {
		t.Errorf("正在拉的时候画了空状态，文字有 %q", tt.Texts())
	}

	close(gate)
	waitFrames(t, tt, "拉回来了", func() bool { return showsText(tt, "音乐 推的第一首") })
}

// TestTheTransitionDoesNotCostAFrameEveryFrame 钉住「过渡没有让稳态多花」。
//
// ⚠️ **判据是「稳态下不再要帧」**：过渡那 200ms 里界面当然要一帧一帧地画，
// 而**过渡走完之后**它必须安静下来——一个「过渡一直转着」的实现在这里会红
// （而那种实现每帧都在占 CPU）。
//
// ⚠️ **推钟再断**：Tester 的钟站着不动，不推钟的话过渡永远走不完，
// 「安静下来」这件事就永远不成立（AGENTS.md §2）。
func TestTheTransitionDoesNotCostAFrameEveryFrame(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v", homeTabs[1], err)
	}
	// ⚠️ **推 800ms，不是 200ms**：这一档还有**另一个**动画在跑——主 Tab 那个
	// 滑块的弹簧（`tabs.go` 的 `TabSlide`，实测 453ms 才收手，见 DESIGN.md
	// §五《主 Tab 滑块为什么是弹簧》）。要断的是「有没有东西永远转下去」，
	// 所以得等两个都停下来。
	tt.Advance(800 * time.Millisecond)
	tt.Frame()

	// 过渡走完之后：界面不再要帧。
	if tt.FramePending() {
		t.Error("过渡走完之后界面还在一帧一帧地要——那一段动画没有停下来")
	}
	// 再画一帧也不该再要（稳态）。
	tt.Frame()
	if tt.FramePending() {
		t.Error("稳态下还在要帧")
	}
}

// TestTheOldContentLeavesBeforeTheNewOneArrives 钉住**中间不同时画两层**
// （票 #238 的 F1：交叉淡入淡出在头两三帧把旧内容和新内容同时画在画面上）。
//
// # 判据是**画面上有什么**
//
// 切 Tab 之后：旧的那一档的字**还在**，新的一档的字**不在**。
//
// ⚠️ **旧的那一块必须还在**（而不只是「新的不在」）：交叉淡入淡出的时候，旧的
// 那一块变成了框架**复制出来的那一份**（`mygo/ui/transition.go` 的 `ghostOf`），
// 而那一份是**不可见的**（`flagInert`）——所以它的字**不在画面上**，而新的那一
// 块**已经在**（只是淡入）。那个实现在「新的不在」这一条上是绿的，所以这一条
// 少不得。
//
// ⚠️ **一路推到过渡走完**：每一帧都查一遍「两块不在同一帧里」。判据是**状态**
// （哪一档的字在画面上），不是时间。
func TestTheOldContentLeavesBeforeTheNewOneArrives(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	old := songName(homeTabs[0], 1)
	fresh := songName(homeTabs[1], 1)

	if err := tt.Click(homeTabs[1]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[1], err, tt.Texts())
	}
	// 切之后的第一帧：旧的那一块还在（它在淡出），新的那一块**还没建**。
	tt.Frame()
	if !shows(tt, old) {
		t.Errorf("切 Tab 之后的第一帧上旧的那一块（%q）不在画面上了——\n"+
			"它应该**先淡出**，新的那一块后进（DESIGN.md §五《规则》第 10 条：\n"+
			"删除时透明度先变、添加时透明度后变）", old)
	}
	if shows(tt, fresh) {
		t.Errorf("切 Tab 之后的第一帧上新的那一块（%q）已经在画面上了——\n"+
			"旧的和新的**同时**画着，那就是重影", fresh)
	}

	// 一路推到过渡走完：任何一帧上都不能两块同时在。
	for i := range 40 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		oldShown, newShown := shows(tt, old), shows(tt, fresh)
		if oldShown && newShown {
			t.Fatalf("第 %d 帧上旧的那一块（%q）和新的那一块（%q）**同时**在画面上——\n"+
				"那是重影（DESIGN.md §五《规则》第 10 条）", i, old, fresh)
		}
		if !oldShown && newShown {
			// 换完了：旧的那一块没了，新的那一块在。
			return
		}
	}
	t.Fatalf("推了 40 帧（640ms），过渡还没走完：旧的那一块 %q 和新的那一块 %q "+
		"都不在画面上，文字有 %q", old, fresh, tt.Texts())
}

// TestThePopTabAppearsWhileTheNewMusicIsStillLoading 钉住**「流行」那一档在
// 「新歌速递」还在拉的时候就出现**（票 #238 的 F3）。
//
// # 走的是真路子
//
// 假 B 站把**新歌速递**那一趟按在门口不放，而**音乐榜**那一趟放行——于是「流行」
// 自己的歌到了，新歌速递还在拉。此前这一档**整档都不出现**（新歌速递也算进了
// 「这一档齐了没有」），屏幕上一直是上一档的内容，而**全程没有任何反馈**。
//
// # 两件事
//
//  1. **「流行」那一档出现了**（它自己的歌在画面上）；
//  2. **新歌速递那一块自己说它在拉**（"正在加载新歌速递"）——用户看得见**哪一
//     块在等**。
func TestThePopTabAppearsWhileTheNewMusicIsStillLoading(t *testing.T) {
	b := newUIExplore(t)
	homeArchives(b)

	// 把「新歌速递」那一趟按住在门口（**不是**音乐榜那一趟——那一趟到了，「流行」
	// 自己的歌也就到了）。
	gate := make(chan struct{})
	b.mu.Lock()
	newMusic := b.handlers[uiHomeNewMusic]
	b.mu.Unlock()
	b.on(uiHomeNewMusic, func(w http.ResponseWriter, r *http.Request) {
		<-gate
		newMusic(w, r)
	})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档拉回来了", func() bool { return showsText(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homePopTab]); err != nil {
		t.Fatalf("点不到 %q 那一档：%v，文字有 %q", homeTabs[homePopTab], err, tt.Texts())
	}
	// ⚠️ **推着钟等**：切 Tab 那一段过渡是按钟走的（旧的先淡出、新的后淡进），
	// 而新歌速递还在拉的时候那一档**照样出现**——它不等新歌速递。
	waitFramesClock(t, tt, "「流行」那一档出现了", func() bool {
		return showsText(tt, "流行 榜上的歌")
	})
	// 而新歌速递那一块**自己说它在拉**——用户看得见哪一块在等。
	if !showsText(tt, "正在加载新歌速递") {
		t.Errorf("切到「流行」而新歌速递还在拉的时候，界面上没有说哪一块在等，文字有 %q",
			tt.Texts())
	}
	// ⚠️ **旧的那一档不许还在**：这一档已经出现了，屏幕上不该还有上一档的歌。
	if showsText(tt, "音乐 推的第一首") {
		t.Errorf("「流行」那一档已经出现了，画面上还留着上一档的歌，文字有 %q", tt.Texts())
	}
}

// TestTheFirstFrameAfterSwitchingTabsCostsNoMoreThanTheSteadyState 钉住
// **切 Tab 的第一帧没有多分配一笔**（票 #238 的 F4）。
//
// # 盯的是哪一笔
//
// 框架的 `Exit` 那一半是**复制一份上一帧的样子**（`mygo/ui/transition.go` 的
// `ghostOf`），而那份复制品要**重新排一遍旧列表的文字**——实测那一下是
// **204 KB**（旧实现：切 Tab 之后的第一帧是 536 次 / 204.5 KB，稳态是
// 205 次 / 6.8 KB，差 30 倍）。现在退场那一段**不写 `Exit`**（旧的那一块照
// 常建，淡出是它自己的不透明度），所以那一笔没了。
//
// ⚠️ **量的是「点一下那一下」**（`tt.Click` 自己的 settle）：**第一帧就在那
// 里面**——Tester 的 `Click` 会画帧，而框架那份复制品正是在**切 Tab 之后的
// 第一帧**建的。量 settle **之后**那一帧就量不到它了（那一帧是稳态那一档）。
//
// ⚠️ **判据是分配，不是时间**（AGENTS.md §2）：每帧分配多少次是「这一帧建了
// 多少个对象」的计数，不受机器快慢影响。
//
// ⚠️ **切三次，取最小的那一次**：测量窗口里可能有别的线程的一次性开销（音频
// 解码器那一笔约 130 KB，见 framebudget_test.go 那一段），而那种开销**只会加
// 不会减**——取最小就把它挡在外面了。
//
// ⚠️ **上限照着旧实现量的**：旧实现那一下是 11,679 次 / 2.34 MB（摊到二十来
// 帧上是稳态的 2.4 倍 / 15 倍），所以这两个上限（2.5 倍 / 5 倍）都拦得住它。
func TestTheFirstFrameAfterSwitchingTabsCostsNoMoreThanTheSteadyState(t *testing.T) {
	s, tt := newHome(t, fullSongs())
	// 稳态：先量几帧（量之前先画几帧，把「只有头几帧才有」的一次性开销付掉）。
	for range 8 {
		tt.Frame()
	}
	steady, _ := measureHomeFrameCost(t, s, tt, 10, false)

	first := homeClickCost(t, s, tt, []int{1, 2, 1})

	const allocsFactor = 2.0
	const bytesFactor = 2.5
	if first.allocs > steady.allocs*allocsFactor {
		t.Errorf("切 Tab 的那一下每帧分配了 %.0f 次，稳态是 %.0f 次（上限 %.1f 倍）——\n"+
			"那一帧里**复制了一份上一帧的样子**（框架的 `Exit` 那一半），\n"+
			"而那份复制品要重新排一遍旧列表的文字",
			first.allocs, steady.allocs, allocsFactor)
	}
	if first.bytes > steady.bytes*bytesFactor {
		t.Errorf("切 Tab 的那一下每帧分配了 %.0f 字节，稳态是 %.0f 字节（上限 %.1f 倍）——\n"+
			"那一帧里**复制了一份上一帧的样子**（框架的 `Exit` 那一半），\n"+
			"而那份复制品要重新排一遍旧列表的文字（实测那一笔是 204 KB）",
			first.bytes, steady.bytes, bytesFactor)
	}
}

// TestTheFadeOutStartsAtFullOpacity 钉住**退场那一段的第一帧是满亮的**。
//
// ⚠️ **零值就是「整棵子树不画」**：框架的 `mygo/ui/paint.go` 里有
// `opacity<=0.001` 就跳过整棵子树——不透明度是 0 的话，屏幕上只剩页面底色，
// 用户看见的是一下「闪」（真窗口里每次切 Tab 都有）。
//
// ⚠️ **这一条断的是纯函数**：阶段刚换的那一帧如果**忘了算**，不透明度就是
// **零值**（`homeSwap` 的零值）。所以不透明度一律由 `alphaAt` 算出来，而这里
// 断的就是它——**不透明度不许「留着上一帧的数」**。
//
// ⚠️ **Tester 里看不见那一帧**：`tt.Click` 的 settle 在**同一个钟**上一次画
// 二十帧（`mygo/ui/headless.go`），后面十九帧把第一帧盖掉了——所以这里断的
// 是**算式**，不是画面。
func TestTheFadeOutStartsAtFullOpacity(t *testing.T) {
	now := time.Unix(2026, 0)
	s := homeSwap{phase: homeSwapOut, to: 1, at: now, from: 1}
	if got := s.alphaAt(now); got != 1 {
		t.Errorf("退场那一段的第一帧不透明度是 %v（want 1）——\n"+
			"0 的意思是「整棵子树不画」（框架的 paint.go），那一帧屏幕上只剩页面底色",
			got)
	}
	// 进场那一段的第一帧：新的一块已经有四成（`homeFadeInLead`），**不是空的**。
	in := homeSwap{phase: homeSwapIn, at: now}
	if got := in.alphaAt(now); got <= 0 {
		t.Errorf("进场那一段的第一帧不透明度是 %v（want > 0）——\n"+
			"交接那一帧要是 0，屏幕上就只剩页面底色（一下闪）", got)
	}
}

// TestTheBodyTransitionDoesNotCopyTheOldContent 钉住**退场那一段不复制旧内容**
// （票 #238 的 F4 的根因）。
//
// ⚠️ **这是白盒**：它盯的是那个过渡本身写没写 `Exit`。
//
// 框架的 `Exit` 那一半是**复制一份上一帧的样子**再让它走完（`mygo/ui/
// transition.go` 的 `ghostOf`），而那份复制品要**重新排一遍旧列表的文字**——
// 实测那一下是 **204 KB**（切 Tab 之后的第一帧）。不写 `Exit`，退场那一段里旧
// 的那一块**照常建**（它就是屏幕上那一块），淡出是**它自己的不透明度**。
//
// ⚠️ **`Enter` 的不透明度是 1 而不是 0**：淡入是**我们自己**算的（见
// homepage_transition.go 的 `homeFadeInCurve`），而框架这一半只管**位置**
// （升上来 8 DIP）。写成 0 的话两边的淡入会**叠乘**——新的那一块会淡入两遍。
func TestTheBodyTransitionDoesNotCopyTheOldContent(t *testing.T) {
	if homeBodyTransition.Exit != nil {
		t.Errorf("内容那一块的过渡还写着 `Exit`——框架会复制一份上一帧的样子，\n"+
			"而那份复制品要重新排一遍旧列表的文字（实测 204 KB），\n"+
			"而且它和新的那一块**同时在画面上**（重影）。\n"+
			"退场那一段要改成**旧的那一块自己的不透明度**（见 homepage_transition.go）")
	}
	if homeBodyTransition.Enter == nil {
		t.Fatal("内容那一块的过渡没有 `Enter`——新的那一块不会升上来")
	}
	if homeBodyTransition.Enter.Opacity != 1 {
		t.Errorf("内容那一块的 `Enter` 不透明度是 %v（want 1）——\n"+
			"淡入是**我们自己**算的（`homeFadeInCurve`），框架这一半只管位置；\n"+
			"写成 0 的话两边的淡入会叠乘，新的那一块会淡入两遍",
			homeBodyTransition.Enter.Opacity)
	}
}

// homeClickCost 量「点一下那一下」的分配（按画了几帧摊平成每帧），取几次里
// **最小**的那一个。
//
// ⚠️ **量的是 `tt.Click` 自己**：**第一帧就在它的 settle 里**（Tester 的
// `Click` 会画帧，见 `mygo/ui/headless.go`）。量 settle **之后**那一帧量不到
// 第一帧——而框架那份复制品正是在第一帧建的。
//
// ⚠️ **按「画了几遍首页」摊平**：那一次 settle 在界面还在要帧的时候会一次画
// 二十帧（`mygo/ui/headless.go` 的 `settle`），而切 Tab 那一段过渡正在要帧——
// 不摊平的话「每帧」这个单位就不是一帧了。
//
// ⚠️ **取最小**：测量窗口里可能有别的线程的一次性开销，而那种开销**只会加不
// 会减**。
func homeClickCost(t *testing.T, s *Shell, tt *mygoui.Tester, tabs []int) homeFrameCost {
	t.Helper()
	best := homeFrameCost{allocs: math.MaxFloat64, bytes: math.MaxFloat64}
	for _, tab := range tabs {
		old := runtime.GOMAXPROCS(1)
		s.builds = buildTally{}
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		if err := tt.Click(homeTabs[tab]); err != nil {
			t.Fatalf("点不到 %q 那一档：%v", homeTabs[tab], err)
		}
		runtime.ReadMemStats(&after)
		runtime.GOMAXPROCS(old)
		frames := float64(s.builds.home)
		if frames <= 0 {
			t.Fatalf("点 %q 那一下首页一帧都没建——量不到东西", homeTabs[tab])
		}
		cost := homeFrameCost{
			allocs: float64(after.Mallocs-before.Mallocs) / frames,
			bytes:  float64(after.TotalAlloc-before.TotalAlloc) / frames,
		}
		if cost.allocs < best.allocs {
			best.allocs = cost.allocs
		}
		if cost.bytes < best.bytes {
			best.bytes = cost.bytes
		}
		// 把那一段过渡走完，再切下一档。
		tt.Advance(homeFadeOut + homeFadeIn + 100*time.Millisecond)
		tt.Frame()
	}
	return best
}

// homeFrameCost 是量出来的一帧的分配。
type homeFrameCost struct {
	allocs float64
	bytes  float64
}

// measureHomeFrameCost 量 n 帧的平均分配，返回这一批画了几帧。
//
// ⚠️ **单核**（`GOMAXPROCS=1`）：免得后台线程在测量窗口里被调度到，把**别的
// 线程**的分配记到界面这一帧的账上（同 framebudget_test.go 的那三条约定）。
//
// ⚠️ **按「画了几遍首页」摊平**：Tester 的 `Frame()` 在界面还在要帧的时候会
// **一次画二十帧**（`mygo/ui/headless.go` 的 `settle`）——稳态下界面不要帧，
// 那是「一帧就是一帧」，而切 Tab 那一段过渡正在要帧。
//
// ⚠️ **advance 是「推钟画一帧」**：Tester 的钟站着不动，而 `Advance` 要等钟走到
// **下一个刷新时刻**才画那一帧（`mygo/ui/headless.go` 的 `due`）——推 20ms 正好
// 画一帧（刷新是 60Hz，也就是 16.7ms 一帧）。
func measureHomeFrameCost(t *testing.T, s *Shell, tt *mygoui.Tester, n int, advance bool) (homeFrameCost, float64) {
	t.Helper()
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	s.builds = buildTally{}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range n {
		if advance {
			tt.Advance(20 * time.Millisecond)
		} else {
			tt.Frame()
		}
	}
	runtime.ReadMemStats(&after)
	frames := float64(s.builds.home)
	if frames <= 0 {
		t.Fatalf("这一批里首页一帧都没建——量不到东西")
	}
	return homeFrameCost{
		allocs: float64(after.Mallocs-before.Mallocs) / frames,
		bytes:  float64(after.TotalAlloc-before.TotalAlloc) / frames,
	}, frames
}

// 免得「这一组用不到」被当成「没写」：那个 `snapshot` 是满屏那一族留下的
// （`fullplayer_review_test.go`），这一族借它用。
var _ = mygoui.Smooth
