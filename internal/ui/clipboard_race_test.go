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
	"errors"
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
// 什么顺序」——收敛那一档会**把新的一份再写一遍**（见 `copyClipboard.commit`），
// 所以顺序是 `[text cover text]` 而最后是 `text`。
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

// TestACoverAlreadyInsideTheWriteDoesNotWinOverALaterText 断**「已经进了写剪贴板
// 那一步」的旧封面不会盖掉后来那一段文字**。
//
// ⚠️ **这一条量的是另外两条量不到的那个缝隙**：前两条里，旧封面那一趟
// **整个被作废了**（它在**写之前**就发现号过时了，于是什么都没写）。
// 而这一条是**另一种交错**——旧封面那一趟**已经通过了那个检查、已经进了写
// 剪贴板那一步**，而那一步**还没落地**：
//
//	10:00:00  按「复制封面」
//	10:00:01  旧封面通过了检查，进了写剪贴板那一步（**卡着**：那一步要走到
//	          主线程，可能慢）
//	10:00:02  按「复制歌曲信息」——**写完了**，剪贴板里是那段文字
//	10:00:03  旧封面的那一步**落地**——剪贴板里又变成那张旧封面
//
// 用户最后按的是文字，而剪贴板里是图。**「比过了号」不等于「写进去的时候
// 仍然是那一档」**——中间还夹着一次真正的写（这正是评审指出来的那一处）。
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

	sh, tt, clip := copyShell(t, app.State{
		HasTrack: true, Title: "一首歌", CoverURL: srv.URL + "/cover.png",
	})

	slot := &clipSlot{}
	// ⚠️ **旧封面的那一步卡住**：模拟「那一步还没落地」的那一刻。
	arrived := make(chan struct{})
	release := make(chan struct{})
	var once bool
	sh.writeClipboardImage = func(data []byte) error {
		if !once {
			once = true
			close(arrived)
		}
		<-release
		slot.write("cover")
		return clip.writeImage(data)
	}
	sh.writeClipboardText = func(text string) error {
		slot.write("text")
		return clip.writeText(text)
	}

	// 第一次：复制封面（卡在写剪贴板那一步）。
	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyCoverLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyCoverLabel, err)
	}
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("旧封面那一趟没进写剪贴板那一步")
	}

	// 第二次：复制歌曲信息（这一趟**写完了**）。
	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyInfoLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyInfoLabel, err)
	}
	if got := slot.is(); got != "text" {
		t.Fatalf("复制歌曲信息之后剪贴板里是 %q，want \"text\"", got)
	}

	// 放行旧封面那一步（它现在才落地——**盖掉**了刚才那段文字）。
	close(release)
	clip.waitDone(t, 10*time.Second)

	// ⚠️ **判据是「最后留在剪贴板里的是哪一份」**：用户最后按的是文字，
	// 所以最后必须是文字。
	if got := slot.is(); got != "text" {
		t.Errorf("剪贴板里最后是 %q，want \"text\"（用户最后按的是歌曲信息）"+
			"——写的顺序：%v", got, slot.writes())
	}
	// ⚠️ **过时的封面那一趟不许说「封面已复制」**：它收敛之后剪贴板里是**那段
	// 文字**，而用户最后按的也是文字——说「封面已复制」是在骗人（用户会以为
	// 剪贴板里是那张图，粘出去却是文字）。
	//
	// ⚠️ **要画帧才看得到**：提示是**画面上的状态**，而 `HasText` 读的是
	// **最后一帧**——那一趟跑完之后还没画过帧，所以这里先画几帧。
	tt.Frame()
	tt.Frame()
	if tt.HasText("封面已复制") {
		t.Errorf("过时的封面那一趟说了「封面已复制」——剪贴板里其实是歌曲信息"+
			"（写的顺序：%v）", slot.writes())
	}
}

// TestAFailedConvergeSaysSo 断**收敛写回去失败有看得见的反馈**。
//
// ⚠️ **这一条量的是「收敛的那一下写失败了」**：旧封面那一趟已经把剪贴板盖成
// 了它那张过时的图，而**把后来那一份写回去**这一步失败了——于是剪贴板里留着
// 的**不是用户要的那一份**，而用户刚被告知「歌曲信息已复制」。
//
// ⚠️ **这一种失败最静默**：画面上什么提示都没有，而用户粘出来的是一张他没
// 要过的图。票面写着「不静默失败」，所以这一档**必须说一句话**。
//
// ⚠️ **它不能被「过时的那一趟不吵用户」那条门吞掉**：那一条挡的是「我这一趟
// 过时了」（用户后来按了别的，那一档会自己说），而这一种是「**剪贴板里不是
// 你要的那一份**」——两件事不一样。
func TestAFailedConvergeSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(headerPNG(t, 8))
	}))
	defer srv.Close()

	sh, tt, clip := copyShell(t, app.State{
		HasTrack: true, Title: "一首歌", CoverURL: srv.URL + "/cover.png",
	})

	slot := &clipSlot{}
	arrived := make(chan struct{})
	release := make(chan struct{})
	var once bool
	// ⚠️ **旧封面那一步卡住**：模拟「那一步还没落地」的那一刻。
	sh.writeClipboardImage = func(data []byte) error {
		if !once {
			once = true
			close(arrived)
		}
		<-release
		slot.write("cover")
		return clip.writeImage(data)
	}
	// ⚠️ **文本的写：第一次成功、第二次失败**。
	//
	// ⚠️ **第二次是「收敛写回去」那一下**：收敛写回去的是**剪贴板里那一档**
	// （也就是文本那一档）的写——而文本那一档的写函数**是在它自己被发起的
	// 那一刻就被捕获了的**（`copySongInfo` 里 `write := s.writeClipboardText`
	// 之后闭包才拿着它）。所以这一桩必须**在文本那一趟发起之前**就装好——
	// 事后再改接缝不生效（那一趟闭包里拿的是旧的）。
	var textWrites int
	sh.writeClipboardText = func(text string) error {
		textWrites++
		if textWrites > 1 {
			return errors.New("剪贴板写不进去了")
		}
		slot.write("text")
		return clip.writeText(text)
	}

	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyCoverLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyCoverLabel, err)
	}
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("旧封面那一趟没进写剪贴板那一步")
	}

	// 后按的那一次：复制歌曲信息。
	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyInfoLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyInfoLabel, err)
	}

	close(release)
	clip.waitDone(t, 10*time.Second)

	// ⚠️ **判据：必须有看得见的反馈**（提示是画面上的状态，要画帧才看得到）。
	waitFrames(t, tt, "收敛失败那一句说出来了", func() bool {
		return tt.HasText("剪贴板里现在是别的内容")
	})
}
