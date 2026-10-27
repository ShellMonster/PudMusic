package ui

// 「复制」那一族的**先后顺序**测试（票 #556 的评审意见）。
//
// # 这一族量的是「剪贴板里最后留着的是不是用户最后按的那一个」
//
// 「复制封面」那一趟要下网络 + 解一张图 + 编一张 PNG，全在**后台 goroutine**
// 上（主线程做会冻住界面）。于是**完成顺序和按下去的顺序可以不一样**：
//
//	按「复制封面」（慢） → 按「复制歌曲信息」（快） → 封面那一趟跑完了
//
// 如果后台那一趟不管先后，剪贴板里最后留着的就是**封面**——而用户最后按的
// 是**歌曲信息**。症状是静默的：提示弹出来的是「封面已复制」（那一趟确实
// 成功了），而剪贴板里的东西和用户最后一次的选择对不上。
//
// # 为什么用「阻塞的 HTTP 夹具」而不是「睡一会儿」
//
// 完成顺序必须是**确定的**：靠 `time.Sleep` 造顺序，机器一忙就翻，而翻了的
// 时候这条测试是**绿的**（它断的是「最后留着谁」，而顺序反了的时候恰好
// 也可能绿）。所以夹具**真的阻塞**：请求来了先等一个信号，放行之后才回。
//
// ⚠️ **不打真 B 站**：夹具是本地的 `httptest` 服务。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ---- 能控住完成顺序的夹具 ----

// gatedServer 是一个**能挡住自己**的 HTTP 服务。
//
// 一个 URL 对应一个闸门：请求到了先等 `release` 被关掉（或超时），
// 然后才把图交回去。
type gatedServer struct {
	srv *httptest.Server

	mu sync.Mutex
	// gates 按路径记着闸门。
	gates map[string]*copyGate
}

type copyGate struct {
	// arrived 在请求到了的时候被关掉（等它 = 等请求到了）。
	arrived chan struct{}
	// release 放行之后请求才回。
	release chan struct{}
	// once 保证 arrived 只被关一次。
	once sync.Once
}

func newGatedServer(t *testing.T) *gatedServer {
	t.Helper()
	g := &gatedServer{gates: map[string]*copyGate{}}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		gate := g.gates[r.URL.Path]
		g.mu.Unlock()
		if gate == nil {
			w.Write(headerPNG(t, 8))
			return
		}
		gate.once.Do(func() { close(gate.arrived) })
		select {
		case <-gate.release:
		case <-time.After(10 * time.Second):
			t.Error("闸门等超时了——放行的信号没来")
		}
		w.Write(headerPNG(t, 8))
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// gate 给某个路径装一个闸门，返回它。
func (g *gatedServer) gate(t *testing.T, path string) *copyGate {
	t.Helper()
	gate := &copyGate{arrived: make(chan struct{}), release: make(chan struct{})}
	g.mu.Lock()
	g.gates[path] = gate
	g.mu.Unlock()
	return gate
}

// headerPNG 造一张 w 宽的 PNG（**宽度能认出来**就够了：判据是「剪贴板里
// 最后那一张是几宽的」）。
func headerPNG(t *testing.T, w int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, 8))
	for y := range 8 {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(x * 13), B: uint8(x * 29), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// copyShellWithState 造一个外壳，而**它的状态是活的**（一个指针）。
//
// ⚠️ **为什么要有这一档**：这一族要测的是「两次复制**内容不同**」的顺序，
// 而两次复制之间的状态得**换掉**（换一张封面）——`copyShell` 那一档的状态
// 是死的（建的时候定死），换不了。
func copyShellWithState(t *testing.T, st *app.State) (*Shell, *mygoui.Tester, *clipStub) {
	t.Helper()
	clip := &clipStub{}
	sh := newTestShell(t, at(760, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, *st)
			s.viewToasts(c)
		}
	}), stubbed(func(_ *testing.T, s *Shell) {
		s.writeClipboardText = clip.writeText
		s.writeClipboardImage = clip.writeImage
		s.copyDone = clip.done
	}))
	return sh.s, sh.tt, clip
}

// ---- 判据 ----

// clipboardIs 报剪贴板里最后是**图**还是**文本**，以及那个图的宽度。
//
// ⚠️ **判据是「最后是什么」**：桩把两次写都记着，而顺序对不对要看**最后
// 落进去的那一次**——所以桩记的是「最后一次写的那个」。
func clipKind(clip *clipStub) (isImage bool, width int) {
	data := clip.imageIs()
	if len(data) == 0 {
		return false, 0
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return true, -1
	}
	return true, img.Bounds().Dx()
}

// clipSlot 是**一个格子**的剪贴板：后来的覆盖先来的。
//
// ⚠️ **为什么不用 `clipStub` 那一套**：`clipStub` 把文本和图**分开记**
// （`text` 和 `image` 两个字段），所以「图把文本盖掉了」这件事在它那儿
// **看不出来**——两个字段各自都还在。而**真的剪贴板是一个格子**：
// 写进去什么，剪贴板里就是什么，后来的覆盖先来的。
//
// ⚠️ **它记下写的顺序**：判据要看的不只是「最后是什么」，还有「写了几次、
// 什么顺序」——「盖掉了又改回来」那一套会**把新的一份再写一遍**，顺序于是是
// `[text cover text]` 而最后是 `text`。而现在那一套**删掉了**：过时的内容
// **压根不落地**，所以顺序里**不会**有过时的那一份。
type clipSlot struct {
	mu    sync.Mutex
	value string
	order []string
}

// write 往格子里写一样东西（后来的覆盖先来的）。
func (c *clipSlot) write(what string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = what
	c.order = append(c.order, what)
}

// is 报格子里现在是什么。
func (c *clipSlot) is() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// writes 报写的顺序（**照抄一份**，免得调用方在锁外看）。
func (c *clipSlot) writes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.order)
}

// ---- 用例 ----

// TestTheLastCopyActionWinsOverAnEarlierCover 断**后按的那一次赢**。
//
// ⚠️ **场景**：先按「复制封面」（那一趟被闸门挡着），再按「复制歌曲信息」
// （同步写完），最后放行封面那一趟。
//
// ⚠️ **判据**：剪贴板里最后是**文本**，而且封面那一趟**一个字都没写进去**
// （`imageCalls` 是 0）——不是「写进去又被改回来」。
func TestTheLastCopyActionWinsOverAnEarlierCover(t *testing.T) {
	g := newGatedServer(t)
	gate := g.gate(t, "/slow.png")

	_, tt, clip := copyShell(t, app.State{
		HasTrack: true, Title: "一首歌", CoverURL: g.srv.URL + "/slow.png",
	})

	// 第一次：复制封面（会被闸门挡住，跑不完）。
	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyCoverLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyCoverLabel, err)
	}
	// 等那一趟真的走到网络上了（再放行才是「先发起的」）。
	select {
	case <-gate.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("封面那一趟没走到网络上")
	}

	// 第二次：复制歌曲信息（同步写完）。
	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyInfoLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyInfoLabel, err)
	}
	if clip.textIs() == "" {
		t.Fatal("歌曲信息没进剪贴板")
	}

	// 放行封面那一趟。
	close(gate.release)

	// 等它跑完（它跑完时**什么都没写**——被后来的那一次作废了——所以只能靠
	// 那个「结束了」的信号等，不能等剪贴板里的内容）。
	clip.waitDone(t, 10*time.Second)

	if n := clip.imageCalls(); n != 0 {
		t.Errorf("过时的封面那一趟还是写了 %d 次剪贴板——用户最后按的是歌曲信息", n)
	}
	if clip.textIs() == "" {
		t.Error("剪贴板里没有歌曲信息了")
	}
}

// TestAnEarlierCoverDoesNotOverwriteALaterCover 断**两次封面复制也是后按的赢**。
//
// ⚠️ **场景**：先按「复制封面」（那一趟被闸门挡着，一张 40 宽的），换一张
// 封面（60 宽的）再按「复制封面」，最后放行第一个。
//
// ⚠️ **判据**：剪贴板里最后是**后按的那一张**（60 宽），不是先按的那张
// （40 宽）——宽度不同，所以「最后留着谁」是**认得出来的**（只数「写了几次」
// 认不出来：两次都写了，顺序才是关键）。
func TestAnEarlierCoverDoesNotOverwriteALaterCover(t *testing.T) {
	// ⚠️ **两个 URL 指向同一台服务器**：封面缓存按 URL 记，而这一条要的是
	// 「两个不同的封面」——所以是两个路径（`/slow.png` 和 `/fast.png`）。
	gate := &copyGate{arrived: make(chan struct{}), release: make(chan struct{})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.png" {
			gate.once.Do(func() { close(gate.arrived) })
			select {
			case <-gate.release:
			case <-time.After(10 * time.Second):
			}
			w.Write(headerPNG(t, 40))
			return
		}
		w.Write(headerPNG(t, 60))
	}))
	defer srv.Close()

	st := &app.State{HasTrack: true, Title: "一首歌", CoverURL: srv.URL + "/slow.png"}
	_, tt, clip := copyShellWithState(t, st)

	// 第一次：复制封面（被挡着）。
	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyCoverLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyCoverLabel, err)
	}
	select {
	case <-gate.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("封面那一趟没走到网络上")
	}

	// 换一张封面，再复制一次（这一趟不挡）。
	st.CoverURL = srv.URL + "/fast.png"
	tt.Frame()
	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyCoverLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyCoverLabel, err)
	}
	// 等第二次真的写进去了。
	waitFor(t, "后按的那一张进剪贴板了", func() bool {
		isImage, w := clipKind(clip)
		return isImage && w == 60
	})

	// 放行第一次。
	close(gate.release)
	// 等**两趟都跑完**（只等一趟的话，可能在先发起的那一趟还没写的时候就
	// 断言了——那样这条测试就抓不住那个 bug）。
	clip.waitDoneCount(t, 10*time.Second, 2)

	isImage, w := clipKind(clip)
	if !isImage {
		t.Fatal("剪贴板里没有图")
	}
	if w != 60 {
		t.Errorf("剪贴板里最后是 %d 宽的那一张，want 60（后按的那一次赢）", w)
	}
}

// TestOnlyOneSectionRunsAtATimeOnTheMainThread 断**主线程是单线程的**。
//
// ⚠️ **这一条量的是「串行」这个前提本身**：整个修法（「判据和写放进同一个
// 主线程段」）**就靠这一条**——主线程是单线程的，所以同一时刻只有一段在跑，
// 于是「判号」到「写」之间插不进另一次复制。
//
// ⚠️ **而这个前提是「模型」给的，不是「框架」给的**：框架那一档
// （`mygo.RunOnMain`）在**主线程上直接执行**（`mygo/loop.go:94` 的
// `isMainThread()` 短路），而在**后台**调它会**排队并等它跑完**——排的那条
// 队列由**主线程**排空。所以「同一时刻只有一段在跑」这件事，取决于**执行段
// 的那个线程只有一个**。
//
// ⚠️ **测试里怎么保证这一点**：假主线程（`mainThreadStub`）的**两档都拿同一
// 把锁**——那把锁就是「单线程」在测试里的替身（见 `mainThreadStub` 的文件头）。
func TestOnlyOneSectionRunsAtATimeOnTheMainThread(t *testing.T) {
	main := newMainThreadStub(t)

	// ⚠️ **第一段卡住**：它跑着的期间，第二段**进不来**。
	arrived := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})

	go main.run(func() {
		close(arrived)
		<-release
		close(firstDone)
	})
	<-arrived

	// ⚠️ **第二段排进去**：它**排在第一段后面**，所以第一段不放行，它就
	// 进不来——而这正是「串行」。
	secondRan := make(chan struct{})
	go main.run(func() { close(secondRan) })

	select {
	case <-secondRan:
		t.Fatal("第一段还卡着，第二段却跑起来了——主线程不是单线程的，" +
			"「判据和写放进同一段」这个修法的前提就不成立")
	case <-time.After(200 * time.Millisecond):
		// 正是要的样子：第二段进不来。
	}

	close(release)
	<-firstDone
	select {
	case <-secondRan:
	case <-time.After(10 * time.Second):
		t.Fatal("第一段放行之后第二段没跑起来")
	}
}

// TestTheMainThreadSectionGoesStraightThroughWhileABackgroundOneWaits 断**主线程
// 档绕过队列、后台档排队**。
//
// ⚠️ **这个差别就是 F1 那个竞态的机制**：后台那一趟（封面）把段**投进队列**
// ——它**还在排队**，没执行；而用户按文本走的是**主线程档**，那一档**绕过
// 队列直接执行**——于是文本**先**落地，队列里那个旧封面的段**随后**才执行。
//
// ⚠️ **所以判据必须在那一段里**：若它在**投递之前**就判了（旧封面的号那时
// 还是最新的），它执行时会把文本**覆盖掉**——见
// `TestACoverAlreadyInsideTheWriteDoesNotWinOverALaterText`（那条测的就是
// 「判据在段里」挡住了它）。
//
// ⚠️ **而这一条量的是「两档真的是两档」**：如果桩把两档**混成一样**（都排队），
// 那个交错**压根不会发生**，而测它的那些用例**照样是绿的**——于是那条修法
// 就成了一个没人验证过的传说。
func TestTheMainThreadSectionGoesStraightThroughWhileABackgroundOneWaits(t *testing.T) {
	main := newMainThreadStub(t)

	// ⚠️ **后台那一段排进队列，然后被闸门卡住**：它**还在排队**，没执行。
	release := main.blockNext()
	backgroundDone := make(chan struct{})
	go func() {
		defer close(backgroundDone)
		main.runBackground(func() {})
	}()
	waitFor(t, "后台那一段被取出来了", func() bool {
		return main.queuedCount() >= 1
	})

	// ⚠️ **主线程档直接跑**：它**不等**队列里那一段。
	mainThreadRan := make(chan struct{})
	go func() {
		main.run(func() { close(mainThreadRan) })
	}()
	select {
	case <-mainThreadRan:
		// 正是要的样子：主线程档绕过了队列。
	case <-time.After(5 * time.Second):
		t.Fatal("主线程档被队列里那一段挡住了——「主线程档绕过队列」" +
			"这个前提不成立，而那个竞态的机制也就不成立")
	}

	release()
	<-backgroundDone
}

// TestACoverAlreadyInsideTheWriteDoesNotWinOverALaterText 断**「已经进了主线程
// 那一段」的旧封面不会盖掉后来那一段文字**。
//
// ⚠️ **这一条量的正是「判据在主线程段里」**：判据若在**投递之前**（段外），
// 旧封面那时候还是最新的一档，于是它执行的时候会**把文本盖掉**——那就是
// F1 那个 bug。
//
// # 场景怎么造的
//
// 假主线程（`mainThreadStub`）有**两档**（见它的文件头）：**主线程档**
// 直接执行、**后台档**排队。而封面那一趟走的是**后台档**——于是它的段
// **可以还在队列里排队**。闸门（`blockNext`）就卡在那个段的**开头**：
//
//	10:00:00  按「复制封面」——后台编 PNG，然后要把段投进主线程队列
//	10:00:01  那一段**进了后台队列**，但被**闸门卡在段的开头**
//	          （判号还没跑，写还没发生）——**它还在排队**
//	10:00:02  用户按「复制歌曲信息」——**主线程档，绕过队列直接执行**
//	          ⇒ 文本**先**落地
//	10:00:03  放行闸门：旧封面的段这才执行（判号 → 写 → 说）
//
// ⚠️ **判据是「旧封面一档都不写」**：它执行的时候文本已经占过号了——所以它
// **压根不写**。剪贴板里从头到尾**只有那段文字**。
//
// ⚠️ **判据是「一个格子」**：真的剪贴板是**一个**格子（后来的覆盖先来的），
// 而 `clipStub` 把文本和图**分开记**，所以「图把文本盖掉了」在它那儿
// **看不出来**——两个字段各自都还在。这一条用的是 `clipSlot`（一个格子 +
// 写的顺序）。
func TestACoverAlreadyInsideTheWriteDoesNotWinOverALaterText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(headerPNG(t, 8))
	}))
	defer srv.Close()

	// ⚠️ **自己拿一个假主线程**：这一条要**看着它**放行闸门，而基座里那个
	// 拿不到闸门（见 `copyShellWith`）。
	main := newMainThreadStub(t)
	sh, tt, clip := copyShellWith(t, app.State{
		HasTrack: true, Title: "一首歌", CoverURL: srv.URL + "/cover.png",
	}, func(s *Shell) {
		s.runOnMain = main.run
		s.runOnMainBackground = main.runBackground
	})

	slot := &clipSlot{}
	sh.writeClipboardText = func(text string) error {
		slot.write("text")
		return clip.writeText(text)
	}
	sh.writeClipboardImage = func(data []byte) error {
		slot.write("cover")
		return clip.writeImage(data)
	}

	// ⚠️ **闸门先装上**：它卡的是「**接下来那一段**」——而**接下来那一段就是
	// 旧封面那一段**（封面那一趟是**唯一**走后台档的那一趟）。
	//
	// ⚠️ **为什么必须先装**：闸门在段**从队列里被取出来**的时候读，而那一段
	// 是封面那一趟在**编完 PNG 之后**才投进来的——那时候再装，它已经跑过了。
	release := main.blockNext()

	// 第一次：复制封面（后台编完 PNG 之后要把段投进主线程队列）。
	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyCoverLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyCoverLabel, err)
	}

	// ⚠️ **等封面那一趟的段被取出来**：它**卡在闸门上**，函数体还没跑——
	// 所以判号和写都还没发生，而它**占着队列**。
	waitFor(t, "封面那一趟的段被取出来了", func() bool {
		return main.queuedCount() >= 1
	})

	// 第二次：复制歌曲信息（**用户最后按的是这个**）。
	//
	// ⚠️ **主线程档**：**绕过队列直接执行**——所以它**不等**旧封面那一段，
	// 于是文本**先**落地。这正是生产上发生的事（菜单回调跑在主线程上，而
	// `onMain` 在主线程上短路，见 `mygo/loop.go:94`）。
	//
	// ⚠️ **在测试自己的 goroutine 里调**：那正是「主线程档」（直接执行）。
	//
	// ⚠️ **不碰 Tester**：Tester 的 `Frame`/`Find` 读的是帧里的状态，而那一段
	// 还卡着——从另一个 goroutine 碰它会有数据竞争。这里直接调**菜单回调
	// 调的那一个函数**（`viewCopyMenu` → `copySongInfo`），而它就是生产上
	// 「用户按下去」走的路。
	sh.copySongInfo(app.State{HasTrack: true, Title: "一首歌"})

	// ⚠️ **判据一：文本已经落地了**——它绕过了队列。
	if got := slot.is(); got != "text" {
		t.Fatalf("复制歌曲信息之后剪贴板里是 %q，want \"text\""+
			"（写的顺序：%v）", got, slot.writes())
	}

	// 放行旧封面那一段：它现在才执行。
	release()
	// 等那一趟后台复制结束（它跑完时什么都没写——判号不通过）。
	clip.waitDone(t, 10*time.Second)

	// ⚠️ **判据二：剪贴板里最后还是那段文字**——旧封面**没进来**。
	if got := slot.is(); got != "text" {
		t.Errorf("剪贴板里最后是 %q，want \"text\"（用户最后按的是歌曲信息）"+
			"——写的顺序：%v", got, slot.writes())
	}
	// ⚠️ **判据三：旧封面那一趟**一个字都没写**——不是「写进去又被改回来」。
	// 而「盖掉了又改回来」那一套会写**两次**（`[text cover]`）——那正是它
	// 造出来的窗口（中间那一瞬间剪贴板里是过时的那一份）。
	writes := slot.writes()
	if len(writes) != 1 || writes[0] != "text" {
		t.Errorf("写的顺序是 %v，want [text]——"+
			"旧封面那一趟**一个字都不该写**（它执行的时候文本已经占过号了）", writes)
	}
	// ⚠️ **过时的封面那一趟不许说「封面已复制」**：剪贴板里是**那段文字**，
	// 而用户最后按的也是文字——说「封面已复制」是在骗人（用户会以为剪贴板里
	// 是那张图，粘出去却是文字）。
	//
	// ⚠️ **要画帧才看得到**：提示是**画面上的状态**，而 `HasText` 读的是
	// **最后一帧**——那一趟跑完之后还没画过帧，所以这里先画几帧。
	tt.Frame()
	tt.Frame()
	if tt.HasText("封面已复制") {
		t.Errorf("过时的封面那一趟说了「封面已复制」——剪贴板里其实是歌曲信息"+
			"（写的顺序：%v）", writes)
	}
}
