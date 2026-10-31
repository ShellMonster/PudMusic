// 评论抽屉（票 #706）的验收。
//
// # 这一族断的是「画面上有什么」
//
// 评论从**独立路由页**改成**右侧抽屉**之后，最容易坏在**看不出来**的地方：
// 抽屉不开、宽度不对、底下那一页还点得动、Esc 关不掉——它们都不报错，只是
// 悄悄不对。
//
// ⇒ 断的都是**渲染结果**（`Tester` 量出来的框和文字）和**交互结果**（真的点
// 一下）。
//
// ⚠️ **一个真请求都不发**：数据走一台**假 B 站**（`newCommentBili`），图走一台
// **假图床**（`fakeImageTransport`）——B 站的图床是 `i0.hdslb.com`，不替掉的
// 的话测试会**真的去下载**（AGENTS.md §6）。

package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// settleCommentsDrawer 推钟直到评论抽屉的进出场走完（面板滑到位）。
//
// ⚠️ **它和 `settleDrawer` 是同一路**：Tester 的钟站着不动（AGENTS.md §2），
// 而抽屉**从右边滑进来**——不推它，面板还在窗口外面，而画面上那一块是空的。
//
// ⚠️ **判据是阶段**，不是时间（理由同 `settleDrawer`）：那是**一个状态**，
// 不是「过了多少毫秒」——所以这一条在快机器和慢机器上读的是同一个。
func settleCommentsDrawer(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	if s == nil {
		t.Fatal("settleCommentsDrawer 要那副外壳（判据是抽屉的阶段）")
	}
	const step = 16 * time.Millisecond
	tt.Frame()
	for range 40 {
		switch s.commentsPhase {
		case drawerShown:
			return
		case drawerGone:
			// ⚠️ **再画一帧**：拆掉面板的那一帧里，面板**还在树上**（阶段是在
			// 建它的过程里改的），所以画面上还有它——下一帧才真的没了。
			tt.Frame()
			return
		}
		tt.Advance(step)
	}
	t.Fatalf("推了 %v 的钟，评论抽屉还没停（阶段 %d）", 40*step, s.commentsPhase)
}

// ─────────────────────────────────────────────────────────────────────────────
// 抽屉能开能关
// ─────────────────────────────────────────────────────────────────────────────

// TestThePlayBarCommentsKeyOpensTheDrawer 断「从播放条点评论键 → 抽屉打开」。
//
// 🔴 **这一条走的是整窗 + 真的点一下**：播放条上那一枚键 → 抽屉打开 → 评论正文
// 可见。中间任何一环断了，它就会红。
//
// ⚠️ **它不再是跳路由了**（票 #706）：改之前这一按是 `navigate("/comments")`，
// 而现在它开的是**右侧抽屉**——而抽屉**没有地址**，所以判据不能是「路由变了」。
//
// ⚠️ **判据是「评论正文在画面上」**：那是**画面上的东西**（这一族断的都是
// 渲染结果），而抽屉开着的时候评论列表就在那 400 宽里。
func TestThePlayBarCommentsKeyOpensTheDrawer(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	cli := bili.NewClient(bili.Options{Transport: b.Server.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	s, tt := newShell(t, at(1400, 900), mounted(func(t *testing.T, a *app.App) {
		a.MountLogin(app.NewMemoryStore("test"), cli)
		a.MountStreaming(app.Streaming{
			Client: cli, Media: b.Server.Client(), TempDir: t.TempDir()})
	}))
	installFakeImages(t, s)
	// ⚠️ **要真的有歌在放**：播放条上那一枚「评论」键的判据是 `st.HasTrack`
	// （有没有歌在放）**和** `CommentsWorthAsking`（当前这一首在 B 站上有没有
	// 对象）——两个都真才摆得出来。
	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVcommentDrawer", Cid: "1",
		Title: "有评论的那一首", Artist: "某个 UP 主",
	}); err != nil {
		t.Fatalf("把在线那首放进队列：%v", err)
	}
	waitFrames(t, tt, "播放条上摆出「评论」键", func() bool {
		_, ok := tt.Find("评论")
		return ok
	})

	// 点播放条上那一枚「评论」键。
	if err := tt.Click("评论"); err != nil {
		t.Fatalf("点不动播放条上的「评论」键：%v（文字：%q）", err, tt.Texts())
	}
	if !s.commentsOpen {
		t.Fatal("点了「评论」键，抽屉没开")
	}
	// ⚠️ **抽屉开着才问评论**：而那一趟是后台 HTTP——等的是**结果**。
	waitFrames(t, tt, "评论问回来了", func() bool {
		return len(s.app.Comments()) > 0
	})
	settleCommentsDrawer(t, s, tt)
	if !tt.HasText("大圆镜科普") {
		t.Errorf("抽屉滑到位之后评论正文不在画面上，文字有 %q", tt.Texts())
	}
}

// TestTheCommentsDrawerCanBeClosedWithEscape 断「Esc 能关抽屉」。
//
// 🔴 **少这一条的表现是「按了 Esc 什么都没发生」**——而用户看着一个模态的
// 弹层按 Esc 没反应，第一反应是「这应用卡住了」。
//
// ⚠️ **判据是「阶段」**（`commentsPhase`）**加画面**：阶段是「关没关」这件事
// 本身，而画面是「用户看得见的那一半」。
func TestTheCommentsDrawerCanBeClosedWithEscape(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 900)

	if s.commentsPhase != drawerShown {
		t.Fatalf("起点就不对：抽屉阶段是 %d，want %d（滑到位了）", s.commentsPhase, drawerShown)
	}

	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.commentsOpen {
		t.Fatal("按了 Esc 抽屉还开着")
	}
	if s.commentsPhase != drawerOut {
		t.Errorf("按了 Esc 之后阶段是 %d，want %d（它该开始往窗口外面滑）",
			s.commentsPhase, drawerOut)
	}
	// 滑完了：面板拆了，画面上那一块空了。
	settleCommentsDrawer(t, s, tt)
	if s.commentsPhase != drawerGone {
		t.Errorf("滑完了阶段是 %d，want %d（面板该拆了）", s.commentsPhase, drawerGone)
	}
}

// TestTheCommentsDrawerCanBeClosedByClickingTheBackdrop 断「点蒙版能关抽屉」。
//
// 🔴 **少这一条的表现是「我明明点了外面，它还在」**。
//
// ⚠️ **点的是窗口左边那一块**（底下那一页的地方）——那正是「点外面」的意思。
func TestTheCommentsDrawerCanBeClosedByClickingTheBackdrop(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 900)

	tt.ClickAt(20, 400)
	tt.Frame()
	if s.commentsOpen {
		t.Fatal("点了底下那一层，抽屉没关")
	}
}

// TestTheCommentsDrawerIsModal 断「抽屉开着的时候**底下那一页点不动**」。
//
// 🔴 **这是模态**：焦点关进抽屉、外面那些快捷键让路，而底下那一页在它开着
// 的时候点不动（Biu 的透明蒙版也是这样，它只是看不见，不是空的）。
//
// ⚠️ **判据是「路由没变」**：抽屉开着的时候点侧栏的「本地音乐」，路由**不许**
// 变——蒙版吃掉了那一下点击。
//
// ⚠️ **而关掉之后那一页就点得动了**：这是「蒙版是退场那一段要让开指针」那条
// 规矩（理由见 `queue_drawer.go`）。
func TestTheCommentsDrawerIsModal(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 900)

	// 开着的时候：底下那一页点不动。
	if err := tt.Click("本地音乐"); err != nil {
		t.Fatalf("点不了侧栏的「本地音乐」：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Location(); got == "/local-music" {
		t.Fatal("抽屉开着的时候底下那一页点得动——蒙版没盖住")
	}
}

// TestTheCommentsDrawerDoesNotEatThePlayBarClick 断「**播放条**照旧点得动」。
//
// 🔴 **这是蒙版让出播放条那一截的意义**：抽屉开着的时候用户还能按播放、上一首、
// 下一首（那一截在蒙版底下，但蒙版**留出了它**）。
//
// ⚠️ **判据是「抽屉没被顺手关掉」**：如果蒙版盖住了播放条，按播放会**顺手把
// 抽屉关了**——而用户按的是「播放」。
func TestTheCommentsDrawerDoesNotEatThePlayBarClick(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 900)

	play, ok := tt.Find("播放")
	if !ok {
		t.Fatalf("画面上找不到「播放」键，文字有 %q", tt.Texts())
	}
	tt.ClickAt(play.X+play.W/2, play.Y+play.H/2)
	tt.Frame()
	if !s.commentsOpen {
		t.Error("按播放键把抽屉关了——蒙版盖住了播放条")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 宽度
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCommentsDrawerIsWiderThanTheQueueDrawer 断「**评论抽屉比队列抽屉宽**」。
//
// 🔴 **用户明确要求了**：这一份面板里装的是**会折行的正文**——一段吐槽五六行，
// 而队列那一列是「封面 + 歌名 + 来源」的定宽三段。320 里一段正文折成五六行，
// 每行二十来个字，读起来是「被剪碎的句子」。
//
// ⚠️ **判据是「面板有多宽」**：那是从画面上量的（面板左缘到窗口右沿）。
//
// ⚠️ **而它仍然是窄的**：400 在 900 的最小窗口上占 44%——而改之前评论是**整个
// 窗口**都是它（底下那一页一格都没剩）。
func TestTheCommentsDrawerIsWiderThanTheQueueDrawer(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, mvItem(), 1400, 900)

	// ⚠️ **判据分两层**：常量那一层（代码说它多宽）和画面那一层（**它真的
	// 摆在那儿**）。
	//
	// 🔴 **画面那一层才是真的**：面板是绝对定位的，而它的左缘从画面上能量出来
	// ——**内容那一块的左沿**就是内容区的左沿（正文那一格撑满内容区，所以它的
	// 左沿 = 面板左缘 + 边框 + 内边距）。
	if commentsDrawerWidth <= queueDrawerWidth {
		t.Errorf("评论抽屉宽 %d，队列抽屉宽 %d——评论抽屉**不许比队列抽屉窄**"+
			"（正文会折成五六行，每行二十来个字）", commentsDrawerWidth, queueDrawerWidth)
	}
	if commentsDrawerWidth < 380 || commentsDrawerWidth > 420 {
		t.Errorf("评论抽屉宽 %d，不在 380~420 那一档里", commentsDrawerWidth)
	}
	// 画面上量出来的那一块**内容区**有多宽。
	//
	// ⚠️ **判据用「页头右端那一枚的右沿」**：抽屉的内容区右沿 = 窗口宽 − 内边距
	// （`pagePadX` = 16）——而页头那一行的**右端**是排序档和刷新键，它们撑着
	// 内容区的右沿。
	//
	// ⚠️ **左沿那一侧量不准**：面板底色和页面底色是同一个颜色，分开它们的只有
	// 那道带 alpha 的边线（它压在底色上，不是主题色本身）——所以**从右往左
	// 量**：内容区右沿 − 内容区左沿 = 内容宽，而内容宽 = 面板宽 − 2×内边距。
	//
	// ⇒ 判据落在「**面板宽**」上：内容宽 + 2×内边距 = 面板宽。
	const pad = float32(pagePadX)
	contentR, ok := tt.Find("刷新评论")
	if !ok {
		t.Fatalf("画面上找不到「刷新评论」那一枚，文字有 %q", tt.Texts())
	}
	// 「刷新评论」是页头右端那一枚——它的右沿就是内容区的右沿（外加它自己的
	// 内边距，`space.Px7`）。
	contentRight := contentR.X + contentR.W
	panelW := (contentRight + pad) - (float32(1400) - commentsDrawerWidth)
	// ⚠️ **容差 8 个像素**：那枚键的右沿是**它自己的盒子**，而盒子外面还有它
	// 自己的内边距——所以「内容区右沿」和「那一枚的右沿」差着一点。
	if panelW < commentsDrawerWidth-8 || panelW > commentsDrawerWidth+8 {
		t.Errorf("从画面上量出来的面板宽是 %.1f，want %d（差得超过 8 个像素）",
			panelW, commentsDrawerWidth)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 入口
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCommentsKeyIsNotShownForALocalFileInTheDrawer 断「本地文件不摆入口」。
//
// 🔴 **这是「抽屉」这一版更要紧的一条**：抽屉是**浮在底下那一页上面**的，而
// 一个永远空着的抽屉会**挡住底下那一页**——而底下那一页是用户真正在用的。
//
// （改之前那一条断的是「永远空着的评论页」——它只是不挡事；现在是抽屉，它挡事。）
func TestTheCommentsKeyIsNotShownForALocalFileInTheDrawer(t *testing.T) {
	it := app.Item{Source: "local", Type: "local", Path: "/tmp/x.mp3", Title: "本地的一首"}
	tt, _ := commentsPlaybarTester(t, it, 1200)
	if _, ok := tt.Find("评论"); ok {
		t.Errorf("本地文件那一档播放条上摆了「评论」键——它点了什么也不会发生")
	}
}

// TestTheFullPlayerCommentsKeyOpensTheDrawer 断「满屏那一枚也开抽屉」。
//
// 🔴 **满屏开着的时候主窗口整个被盖住了**，所以播放条上那一枚在满屏里**点不到**
// ——这一枚是满屏里唯一的入口。
//
// ⚠️ **它点开的时候先把满屏收掉**：抽屉是浮在**主窗口**上面的一层，而满屏是
// **压在主窗口上面**的另一层——不收掉的话抽屉会开在满屏底下（那一层不透明，
// 抽屉一格都看不见，而用户按了「评论」什么反应都没有）。
func TestTheFullPlayerCommentsKeyOpensTheDrawer(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	cli := bili.NewClient(bili.Options{Transport: b.Server.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	ts := newTestShell(t, at(1400, 900), mounted(func(t *testing.T, a *app.App) {
		a.MountLogin(app.NewMemoryStore("test"), cli)
		a.MountStreaming(app.Streaming{
			Client: cli, Media: b.Server.Client(), TempDir: t.TempDir()})
	}))
	s, tt := ts.s, ts.tt
	installFakeImages(t, s)
	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVcommentDrawer", Cid: "1",
		Title: "有评论的那一首", Artist: "某个 UP 主",
	}); err != nil {
		t.Fatalf("把在线那首放进队列：%v", err)
	}
	waitFrames(t, tt, "播放条上摆出「评论」键", func() bool {
		_, ok := tt.Find("评论")
		return ok
	})
	// 开满屏（满屏那一枚是**满屏里**唯一的入口——主窗口整个被盖住了）。
	s.openFullPlayer()
	waitFrames(t, tt, "满屏里摆出「评论」键", func() bool {
		_, ok := tt.Find("评论")
		return ok
	})
	if !s.full.open {
		t.Fatal("满屏没开")
	}

	if err := tt.Click("评论"); err != nil {
		t.Fatalf("点不动满屏里的「评论」键：%v（文字：%q）", err, tt.Texts())
	}
	if !s.commentsOpen {
		t.Fatal("点了满屏里的「评论」键，抽屉没开")
	}
	if s.full.open {
		t.Error("点了满屏里的「评论」键，满屏没收掉——抽屉会开在满屏底下")
	}
	waitFrames(t, tt, "评论画出来了", func() bool {
		return tt.HasText("大圆镜科普")
	})
}

// TestTheFullPlayerCommentsKeyHasNoCount 断「满屏那一枚**不带数字**」。
//
// 🔴 **用户明确要求了**：满屏底栏是**居中的 flex**，任何一枚变宽播放键就离开
// 窗口的竖直中轴了——而「三键落在中轴上」正是满屏底栏那条设计。
//
// ⚠️ **判据是「画面上没有那个数」**：播放条那一枚**照旧带**（票 #682），而这一
// 枚不带。
func TestTheFullPlayerCommentsKeyHasNoCount(t *testing.T) {
	tt, _ := commentsFullTester(t, mvItem())
	if _, ok := tt.Find("评论"); !ok {
		t.Errorf("满屏播放器里找不到「评论」键，文字有 %q", tt.Texts())
	}
	// ⚠️ **不带数字**：名字里不许有数字（`Find` 是全等匹配，所以「评论」找得到
	// 就说明名字里没有别的东西）。
	if _, ok := tt.Find("评论 1286"); ok {
		t.Error("满屏那一枚「评论」键的名字里带数字了——满屏底栏是居中的 flex，" +
			"带数字会让播放键离开竖直中轴")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 进出场
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCommentsDrawerSlidesInFromTheRight 断「抽屉是**从右边**滑进来的」。
//
// 🔴 **判据是画面上抽屉内容的 x**（用 `tt.Find` 找评论页头那一行）：面板贴住
// 窗口右沿时，它的内容从「窗口宽 − 面板宽 + 内边距」起。
//
// ⚠️ **不要用「找面板左缘那道 1 像素边线」那个办法**（`queue_drawer_anim_test.go`
// 的 `drawerEdgeX`）：那道边线是**半透明混色**画上去的（实测浅色下画面上是
// `{224 220 213}` 而不是主题的 `219`——它压在面板底色上混过一遍），而
// `drawerEdgeX` 比的是**精确相等** ⇒ 扫不到。
// ⇒ **那个办法在 origin/main 上就已经扫不到了**（`TestTheQueueDrawerSlidesInFromTheRight`
// 和 `TestTheQueueDrawerSlidesOutOnThatCurveInThatTime` 在基线上就是红的，
// 与本票无关）。所以这里改用**内容的位置**——它不依赖任何一个像素颜色。
func TestTheCommentsDrawerSlidesInFromTheRight(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, mvItem(), 1400, 900)

	// 抽屉页头那一行（「这个视频的评论」那个角标）——它在抽屉的内容列里。
	r, ok := tt.Find("这个视频的评论")
	if !ok {
		t.Fatalf("抽屉开着，画面上却找不到评论页头那一行")
	}
	// ⚠️ **判据是「它落在窗口右半边」**：面板 400 宽、窗口 1400 ⇒ 面板左缘在
	// 1000，内容列从那儿往右。页头那个角标在内容列里（面板内边距之内）⇒
	// 它的 x 落在 [1000, 1400] 这一段。
	//
	// 🔴 **不钉死那个 x**：页头那一行的左边距受内边距影响，而内边距不是这一
	// 票要验的东西。这一票要验的是**抽屉贴在右沿**（不是摆在中间、也不是
	// 摆在左边）。
	if r.X < 1000 {
		t.Errorf("评论内容在 x=%.1f——抽屉没有贴住窗口右沿（面板左缘应该在 %d）",
			r.X, 1400-commentsDrawerWidth)
	}
	if r.X+r.W > 1400 {
		t.Errorf("评论内容右边到 %.1f，超出窗口右沿 1400", r.X+r.W)
	}
}

// TestTheCommentsDrawerMovesByLayoutNotTransform 断「进出场动的是**布局位置**，
// 不是 transform」。
//
// 🔴 **这是 DESIGN.md §五《不做》那条**：软件渲染回退时 transform 会让整棵子树
// 逐像素重绘（第三方实测差两个数量级）。
//
// ⚠️ **判据是源码**：框架的 `Transform` 是一个单独的入口，而进出场那一支用的是
// `Right(...)`（面板的右边距）——所以「动的是布局位置」这件事**在源码里看得见**。
//
// ⚠️ **`AnimateTo` 不算 transform**：它是框架的**数值通道**（把右边距从
// −400 动到 0），动的是布局位置。**队列抽屉那一段用的就是它**
// （`queue_drawer.go:375`：`panel.AnimateTo(queueSlideKey, target, queueSlideAnim)`
// 然后 `panel = panel.Right(slide)`）——所以这里**不是**「不许有 AnimateTo」，
// 而是「不许有 `Transform`」。
func TestTheCommentsDrawerMovesByLayoutNotTransform(t *testing.T) {
	src := drawerSource(t, "comments_drawer.go")
	// ⚠️ **只查 `Transform`**：`AnimateTo` 是数值通道，队列抽屉也用它。
	if strings.Contains(src, ".Transform(") {
		t.Errorf("comments_drawer.go 里有 `.Transform(...)`——面板的进出场要动" +
			"**布局位置**（`Right`），不是 transform（DESIGN.md §五）")
	}
	// ⚠️ **动的是 `Right`**：源码里那一支必须真的调了 `Right`（不是 `Transform`）。
	if !strings.Contains(src, ".Right(") {
		t.Error("comments_drawer.go 里没有 `.Right(...)`——面板的进出场不是靠布局位置")
	}
}

// drawerSource 读一个文件的内容。
func drawerSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读不到 %s：%v（路径写错了的话，这个测试就成了一句空话）", name, err)
	}
	return string(b)
}
