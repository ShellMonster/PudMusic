//go:build darwin

package media

// 真机上的 Objective-C 那一半。跑在开发机上，不需要开窗口、不需要联网。
//
// 这一份是**唯一**能证明「键名对了、系统认了、回调进得来」的自动化测试，
// 而它断不了的两件事（控制中心长什么样、媒体键按下去到不到）只能靠真机
// 肉眼确认——那部分记在任务报告里，不在这里假装测过。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// mpTestPlatform 装一个真的 macOS 平台那一半，跳过取封面（那要联网）。
func mpTestPlatform(t *testing.T) *mpSession {
	t.Helper()
	p, err := newPlatform(Options{Fetch: func(string) ([]byte, error) { return nil, nil }})
	if err != nil {
		t.Fatalf("装不上 macOS 媒体会话：%v", err)
	}
	return p.(*mpSession)
}

// ─────────────────────────────────────────────────────────────────────────────
// 胶水层：发消息、Foundation 类型
// ─────────────────────────────────────────────────────────────────────────────
//
// 这一层造出来的对象都是自己持有的（+1），测试里也一样要当场 release——
// 测试比产品代码更容易漏，漏了就是一条谁也查不着的偶发崩溃。

// TestNSStringRoundTrip 盯字符串进得去也出得来，含中文和 emoji。
//
// 中文是这一层最可能坏的地方：`initWithBytes:length:encoding:` 按字节算
// 长度、按 UTF-8 解码，用 C 字符串那一条路（`stringWithUTF8String:`）在
// 中文上是**按字节截断**的，而歌名全是中文——症状是锁屏上的歌名少一半。
func TestNSStringRoundTrip(t *testing.T) {
	for _, want := range []string{"", "hello", "一首歌", "无损 FLAC 🎵", "a\x00b"} {
		held := newString(want)
		got := goString(held)
		release(held)
		if want == "" {
			// 空串故意返回 nil 对象（见 nsString），所以这里只能是空。
			if got != "" {
				t.Errorf("空串变成了 %q", got)
			}
			continue
		}
		if got != want {
			t.Errorf("往返之后 %q 变成了 %q", want, got)
		}
	}
}

// TestNSNumberDouble 盯 double 走的是浮点寄存器那一路。
//
// ⚠️ 这是这一层最容易坏的一处：把 double 按整数传过去的话，值不会变，
// 它会变成另一个**恰好落在寄存器里**的数——于是一切看起来都活着，
// 只是时长和进度全是垃圾，而且没有任何报错。
func TestNSNumberDouble(t *testing.T) {
	for _, want := range []float64{0, 1, 199.5, 3600} {
		n := newNumberDouble(want)
		got := sendDouble(n, "doubleValue")
		release(n)
		if got != want {
			t.Errorf("%.1f 变成了 %.1f", want, got)
		}
	}
}

// TestDictionaryRoundTrip 盯字典里的键和值都拿得回来。
func TestDictionaryRoundTrip(t *testing.T) {
	d := newMutableDictionary()
	if d == 0 {
		t.Fatal("建不出 NSMutableDictionary")
	}
	defer release(d)
	putString(d, keyTitle, "歌名")
	putNumber(d, keyRate, 1)

	if got := goString(objectForKey(d, keyTitle)); got != "歌名" {
		t.Errorf("取回来的是 %q，want 歌名", got)
	}
	if got := sendDouble(objectForKey(d, keyRate), "doubleValue"); got != 1 {
		t.Errorf("速率取回来是 %v，want 1", got)
	}
	// 放 0 值不该把字典搞坏——Objective-C 收到 nil 会抛
	// `object cannot be nil`，而这一层宁可不放。
	dictPut(d, keyArtist, 0)
}

// ─────────────────────────────────────────────────────────────────────────────
// Now Playing：写进去的东西要能被系统读回来，而且**用的是它认的键名**
// ─────────────────────────────────────────────────────────────────────────────

// nowPlayingCenter 是 `MPNowPlayingInfoCenter.defaultCenter`。
func nowPlayingCenter() id {
	return send(id(class("MPNowPlayingInfoCenter")), "defaultCenter")
}

// infoValue 是系统里那份曲目信息。
//
// ⚠️ **它归系统所有**，释放它就是多释放一次——实测那会当场段错误。
// 只有这一层自己 `new*` 造出来的东西才归我们管。
func infoValue(center id) id {
	return send(center, "nowPlayingInfo")
}

// objectForKey 从字典里取一个键。返回的对象由字典持有，**不要 release**。
func objectForKey(dict id, key string) id {
	k := newString(key)
	defer release(k)
	return send(dict, "objectForKey:", uintptr(k))
}

// objValue 取一个键并把结果抄成字符串。
func objValue(dict id, key string) string {
	return goString(objectForKey(dict, key))
}

// numValue 取一个键的数值。
func numValue(dict id, key string) float64 {
	return sendDouble(objectForKey(dict, key), "doubleValue")
}

// TestNowPlayingInfoRoundTrip 盯写进去的系统读得回来，值也对。
//
// ⚠️ 它断不了「键名对不对」：`nowPlayingInfo` 是原样存回来给你读的，
// 键名拼错它照样原样返回。但它断得住「值写错了」——时长写成毫秒、
// 进度写反了，这里立刻就看出来。
func TestNowPlayingInfoRoundTrip(t *testing.T) {
	p := mpTestPlatform(t)
	center := nowPlayingCenter()

	p.apply(NowPlaying{
		State:    StatePlaying,
		Title:    "一首歌",
		Artist:   "某某",
		Duration: 3*time.Minute + 30*time.Second,
		Position: 90 * time.Second,
	})

	info := infoValue(center)
	if info == 0 {
		t.Fatal("写进去之后 nowPlayingInfo 是空的")
	}
	if got := objValue(info, keyTitle); got != "一首歌" {
		t.Errorf("歌名是 %q，want 一首歌", got)
	}
	if got := objValue(info, keyArtist); got != "某某" {
		t.Errorf("艺人是 %q，want 某某", got)
	}
	if got := numValue(info, keyDuration); got != 210 {
		t.Errorf("时长是 %v 秒，want 210", got)
	}
	if got := numValue(info, keyElapsed); got != 90 {
		t.Errorf("进度是 %v 秒，want 90", got)
	}
	// 播放速率是必设的（系统不看我们的音频会话）。
	if got := numValue(info, keyRate); got != 1 {
		t.Errorf("在放时的速率是 %v，want 1", got)
	}

	// 收尾：这一份是进程全局的，别留给别的用例看。
	p.apply(NowPlaying{State: StateNone})
}

// TestThePlaybackRateFollowsTheState 盯速率跟着状态走。
//
// 暂停/停止时必须是 0：非 0 的话控制中心会以为歌还在放，进度条自己往前
// 爬，而歌是停着的——用户看锁屏就能看出不对劲。
func TestThePlaybackRateFollowsTheState(t *testing.T) {
	p := mpTestPlatform(t)
	center := nowPlayingCenter()

	for _, c := range []struct {
		state State
		want  float64
	}{
		{StatePlaying, 1},
		{StatePaused, 0},
		{StateStopped, 0},
	} {
		p.apply(NowPlaying{State: c.state, Title: "一首歌", Duration: time.Minute})
		info := infoValue(center)
		if info == 0 {
			t.Fatalf("状态 %v 之后 nowPlayingInfo 是空的", c.state)
		}
		got := numValue(info, keyRate)
		if got != c.want {
			t.Errorf("状态 %v 时速率是 %v，want %v", c.state, got, c.want)
		}
	}
	p.apply(NowPlaying{State: StateNone})
}

// TestPlaybackStateIsAlwaysSet 盯 setPlaybackState: 真的被调了。
//
// 这是 macOS 上的**必设项**：系统不看我们的音频会话，只看这个数。
// 少了它控制中心的按钮状态会错，而且不报错。
//
// 断法是「回调之后立刻读 `playbackState`」——那正是 `setPlaybackState:`
// 写进去的地方。值对不上就是那一行没走到（或者走了但传错了）。
func TestPlaybackStateIsAlwaysSet(t *testing.T) {
	p := mpTestPlatform(t)
	center := nowPlayingCenter()

	for _, c := range []struct {
		state State
		want  int
	}{
		{StatePlaying, sysStatePlaying},
		{StatePaused, sysStatePaused},
		{StateStopped, sysStateStopped},
	} {
		p.apply(NowPlaying{State: c.state, Title: "一首歌", Duration: time.Minute})
		if got := sendInt(center, "playbackState"); got != c.want {
			t.Errorf("状态 %v 之后系统那边是 %d，want %d", c.state, got, c.want)
		}
	}

	// 没有歌的时候系统那边应该是「停了」，不是「不知道」——
	// unknown 在控制中心上留着一个不知道该显示什么的按钮。
	p.apply(NowPlaying{State: StateNone})
	if got := sendInt(center, "playbackState"); got != sysStateStopped {
		t.Errorf("清空之后系统那边是 %d，want %d（stopped）", got, sysStateStopped)
	}
}

// TestClearingRemovesTheNowPlayingInfo 盯「没有歌」是真的清掉了。
//
// 用的是 `setNowPlayingInfo: nil`（系统没有 `clearNowPlayingInfo` 那个方法）。
// 不清的话退出去之后锁屏上还留着一首已经不存在的歌。
func TestClearingRemovesTheNowPlayingInfo(t *testing.T) {
	p := mpTestPlatform(t)
	center := nowPlayingCenter()

	p.apply(NowPlaying{State: StatePlaying, Title: "一首歌", Duration: time.Minute})
	if infoValue(center) == 0 {
		t.Fatal("前面的写入没生效，后面的清空就断不出东西")
	}
	p.apply(NowPlaying{State: StateNone})
	if info := infoValue(center); info != 0 {
		t.Errorf("清空之后 nowPlayingInfo 还在（%s）", goString(send(info, "description")))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 封面
// ─────────────────────────────────────────────────────────────────────────────

// testPNG 造一张能解开的小图。
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestTheArtworkClassAnswersWithTheImage 盯那个 Objective-C 子类真的答得出图。
//
// 这是整条链上最需要证明的一处：它要过三个自己写的关——`RegisterClass`
// 造出来的子类、`Go 函数当 IMP`、以及 **IMP 的签名**（`NSSize` 那个结构
// 参数）。签名错了的话系统问它要图，它读到的是垃圾，而那在锁屏上表现为
// 一张花屏或者干脆什么都没有，而且不报错。
//
// 所以这里**自己**发起一次请求（用和系统一模一样的调用形式），而不是等
// 系统什么时候想起来问。
func TestTheArtworkClassAnswersWithTheImage(t *testing.T) {
	if artworkClass == 0 {
		t.Fatal("封面子类没注册上（artworkClass 是 0）")
	}

	src := nsImageFromBytes(testPNG(t, 32, 32))
	if src == 0 {
		t.Fatal("一段合法的 PNG 解不出 NSImage")
	}
	defer release(src)
	// 解不开的字节 NSImage 不报错，它给你一个**尺寸 0×0** 的空对象。
	// 那张图放进锁屏上就是一块空白——所以这里核尺寸。
	if got := sendSize(src, "size"); got.W != 32 || got.H != 32 {
		t.Fatalf("NSImage 尺寸是 %v，want 32×32（图没解开）", got)
	}

	art := newArtwork(src)
	if art == 0 {
		t.Fatal("造不出 MPMediaItemArtwork")
	}
	if imageOf(art) != src {
		t.Fatal("新造的封面对象在图册里认不出来")
	}

	type asked struct {
		img id
		err id
	}
	done := make(chan asked, 1)
	completion := objc.NewBlock(func(_ objc.Block, image objc.ID, err objc.ID) {
		done <- asked{img: image, err: err}
	})
	defer completion.Release()

	requestArtwork(art, 64, 64, completion)

	select {
	case got := <-done:
		if got.err != 0 {
			t.Errorf("回调带着错误：%s", goString(send(got.err, "localizedDescription")))
		}
		if got.img != src {
			t.Errorf("回调回来的不是我们那张图（%v，want %v）", got.img, src)
		}
		// size 参数要真的传到了 IMP 那里：两个 double 都要是给的值。
		// 这里过了就同时证明了两件事——NSSize 拆成两个 double 在调用约定上
		// 和按结构传是等价的，而且纯 Go 那边的封送这一次是通的。
		// （IMP 里不能往测试里写东西，所以是它在那边记一笔。）
		if w, h := requestedArtworkSize(); w != 64 || h != 64 {
			t.Errorf("IMP 收到的是 %v×%v，want 64×64（NSSize 传歪了）", w, h)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("5 秒内那个方法没有回调过来——IMP 的签名大概率不对")
	}
}

// TestAnArtworkWithoutAnImageAnswersNil 盯「没有图」时给的是 nil 而不是乱给。
//
// 认不出图却硬给一个对象的话，锁屏上是一块花屏，而那比没有封面糟得多
// （没有封面系统会画自己的占位）。
func TestAnArtworkWithoutAnImageAnswersNil(t *testing.T) {
	art := newArtwork(nsImageFromBytes(testPNG(t, 8, 8)))
	forgetImage(art)

	done := make(chan id, 1)
	completion := objc.NewBlock(func(_ objc.Block, image objc.ID, _ objc.ID) {
		done <- image
	})
	defer completion.Release()

	requestArtwork(art, 32, 32, completion)
	select {
	case got := <-done:
		if got != 0 {
			t.Error("没有图的时候回调了一个非空对象——锁屏上会是一块花屏")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("5 秒内没有回调")
	}
}

// requestArtwork 发起一次「给我这张封面」的请求，走的是和系统一模一样的
// 那条消息。
func requestArtwork(art id, w, h float64, completion objc.Block) {
	msgRequestImage(art, sel(artworkSelector), w, h, completion)
}

// TestBytesThatAreNotAnImageAreRejected 盯解不开的字节不会硬造一个对象。
func TestBytesThatAreNotAnImageAreRejected(t *testing.T) {
	for _, b := range [][]byte{[]byte("这不是一张图"), nil, {}} {
		img := nsImageFromBytes(b)
		if img != 0 {
			release(img)
			t.Errorf("拿一段 %d 字节的垃圾造出了一个 NSImage", len(b))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 反复推：这一段是为了钉住一个**曾经必崩**的形状
// ─────────────────────────────────────────────────────────────────────────────

// TestRepeatedPushesWithGapsDoNotCrash 盯「隔一段时间推一次」这个形状。
//
// 这不是「顺便跑一跑」：这一段曾经**每次都崩**，而且崩在纯 native 代码里——
// 连一行 Go 栈都没有，`signal: segmentation fault` 就是全部。触发它的正是
// 这一层最早用的那个构造：通过 purego 驱动 Objective-C 的**自动释放池**。
// 那套池子是**每线程**的状态，而这里的调用来自 Go 的任意 goroutine，那些
// goroutine 随时会换 OS 线程，「谁负责放它」就没人说得清了。
//
// 现在这一层造出来的每个对象都是自己持有的（`new*` → 用完 `release`），
// 一次 `publish` 造七八个、放七八个，一个都不留在系统的池子里；
// 顺带所有 ObjC 动作都排在一条钉住的 OS 线程上（`objcthread_darwin.go`）。
//
// ⚠️ **别把这个用例调快**。老代码是在**第二三次**推的时候崩的，间隔大一点
// 才能让 goroutine 有机会换线程；把它压成紧凑循环它反而不会崩（实测：
// 紧凑 200 轮不崩，带间隔 120 轮必崩）。这是这个 bug 最难缠的地方。
func TestRepeatedPushesWithGapsDoNotCrash(t *testing.T) {
	const rounds = 60
	const gap = 100 * time.Millisecond

	sess := New(Options{})
	if !sess.Available() {
		t.Fatal("会话没起来")
	}
	defer sess.Close()
	sess.SetHandler(func(Command, time.Duration) {})

	for i := 0; i < rounds; i++ {
		sess.Publish(NowPlaying{
			State:    StatePlaying,
			Title:    "一首歌",
			Duration: time.Minute,
			Position: time.Duration(i) * gap,
		})
		time.Sleep(gap)
	}

	// 走完了还要看系统那边有没有落下最后一份。
	center := nowPlayingCenter()
	if got := objValue(infoValue(center), keyTitle); got != "一首歌" {
		t.Errorf("推了 %d 轮之后系统那边是 %q，want 一首歌", rounds, got)
	}
}

// TestTheArtworkArrivesAfterAPush 盯「封面下好之后会补推一次」。
//
// 那条补推是**绕开**去抖的：去抖比的是「用户听见的东西变没变」，封面到货
// 不该被它吃掉（用户换歌时歌名和进度是立刻的，封面晚几百毫秒）。
// 少了这一步，锁屏上的封面就永远停在上一首——而界面上明明已经是新歌了。
func TestTheArtworkArrivesAfterAPush(t *testing.T) {
	p := mpTestPlatform(t)
	fetched := make(chan struct{}, 4)
	p.fetch = func(string) ([]byte, error) {
		select {
		case fetched <- struct{}{}:
		default:
		}
		return testPNG(t, 64, 64), nil
	}

	// 走 apply 而不是 push：取封面是在 apply 里发起的（push 只管往系统写）。
	p.apply(NowPlaying{State: StatePlaying, Title: "有封面的一首", Duration: time.Minute})
	if v := objectForKey(infoValue(nowPlayingCenter()), keyArtwork); v != 0 {
		t.Error("推的那一份里居然已经有封面了——封面是异步的")
	}

	// 换歌：地址变了，去下。
	p.apply(NowPlaying{
		State:      StatePlaying,
		Title:      "有封面的一首",
		Duration:   time.Minute,
		ArtworkURL: "https://example.invalid/cover.png",
	})

	select {
	case <-fetched:
	case <-time.After(5 * time.Second):
		t.Fatal("封面没有被取")
	}
	// 等它下好之后那一次补推。
	deadline := time.Now().Add(5 * time.Second)
	var art id
	for time.Now().Before(deadline) {
		if v := objectForKey(infoValue(nowPlayingCenter()), keyArtwork); v != 0 {
			art = v
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if art == 0 {
		t.Fatal("封面下好了，但系统那一份里还是没有 artwork")
	}
	if cls := goString(send(send(art, "class"), "description")); cls != artworkClassName {
		t.Errorf("artwork 的类是 %q，want %q", cls, artworkClassName)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 所有权：这一段是为了钉住一个**评审探出来**的错
// ─────────────────────────────────────────────────────────────────────────────

// 池子那两个符号**只在测试里**取——产品代码一次都不碰它（碰它就是那个必崩的
// 段错误，见 objc_darwin.go 顶上）。
func poolForTest(t *testing.T) (push func() uintptr, pop func(uintptr)) {
	t.Helper()
	h, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		t.Skipf("拿不到 libobjc：%v", err)
	}
	pa, err := purego.Dlsym(h, "objc_autoreleasePoolPush")
	if err != nil {
		t.Skipf("没有 objc_autoreleasePoolPush：%v", err)
	}
	po, err := purego.Dlsym(h, "objc_autoreleasePoolPop")
	if err != nil {
		t.Skipf("没有 objc_autoreleasePoolPop：%v", err)
	}
	purego.RegisterFunc(&push, pa)
	purego.RegisterFunc(&pop, po)
	return push, pop
}

// TestEveryOwnedHelperSurvivesAnAutoreleasePoolPop 盯「这一层造出来的每个对象
// 都是自己持有的」。
//
// 这是评审探出来的一个真错：`newNumberDouble` 原来用的是
// `+[NSNumber numberWithDouble:]`——一个**工厂方法**，它交回来的对象归自动
// 释放池所有。按「自己持有、用完自己 release」的规矩去用它就多放了一次。
//
// **为什么平时看不出来**：这一层故意不建池子，于是那个 autorelease 找不到主人、
// 一直挂着，恰好被后面那一次 release 抵消——**看着是好的**。要让它现形，
// 就得在同一条线程上真的推一个池子，然后 pop：pop 会对着同一个对象再放一次，
// 进程当场段错误（不是断言失败，是测试进程直接死）。
//
// ⚠️ 池子**必须和活儿在同一条线程上**：自动释放池属于压它的那个线程。
// 而这一层的 ObjC 动作都排在 `onObjCThread` 那条钉住的线程上——池子也就必须
// 推在那条线程上。推在别处再 pop，会变成「在 A 线程 pop 了 B 线程的池子」，
// 那崩的是测试自己（踩过一次）。
//
// 所以这个用例的断言是「跑完还在」，它证明的是**这一层不在系统那边欠下任何
// 引用**——而那正是这一层敢自己 release 的前提。

// TestEveryOwnedHelperSurvivesAnAutoreleasePoolPop 盯「这一层造出来的每个对象
// 都是自己持有的」。
//
// 这是评审探出来的一个真错：`newNumberDouble` 原来用的是
// `+[NSNumber numberWithDouble:]`——一个**工厂方法**，它交回来的对象归自动
// 释放池所有。按「自己持有、用完自己 release」的规矩去用它就多放了一次。
//
// **为什么平时看不出来**：这一层故意不建池子，于是那个 autorelease 找不到主人、
// 一直挂着，恰好被后面那一次 release 抵消——**看着是好的**。要让它现形，
// 就得在同一条线程上真的推一个池子，然后 pop：pop 会对着同一个对象再放一次。
//
// ⚠️ 两个坑，都是踩出来的：
//
//  1. 池子**必须和活儿在同一条线程上**。自动释放池属于压它的线程，而这一层的
//     ObjC 动作都排在 `onObjCThread` 那条钉住的线程上——池子也就必须推在那条
//     线程上。推在别处再 pop 是「在 A 线程 pop 了 B 线程的池子」，崩的是测试自己。
//  2. **pop 之后必须再用一次那些对象**。多放一次不会当场报错，只是引用计数
//     归零；真正炸的是下一次往那个野指针上发消息。不碰它，用例就永远绿——
//     第一版的写法就是这样，反向验证时它对着有 bug 的代码也过了。
//
// 所以这个用例的断言是「pop 之后再读，值还在」。它证明的是**这一层不在系统那边
// 欠下任何引用**——而那正是这一层敢自己 release 的前提。
//
// （别改用 retainCount 去判形状：tagged pointer 上它返回的是
// 9223372036854775807 这种溢出值，量不出东西来。池子才是那个真判据。）
func TestEveryOwnedHelperSurvivesAnAutoreleasePoolPop(t *testing.T) {
	push, pop := poolForTest(t)

	// ⚠️ 调的是 `applyOnObjCThread` 而不是 `apply`——后者会再排一次
	// `onObjCThread`，而我们已经在那条线程上了，那才是死等（见
	// objcthread_darwin.go 顶上「这里不能重入」那段）。
	onObjCThread(func() {
		pool := push()
		d := newMutableDictionary()
		putString(d, "s", "一首歌")
		putNumber(d, "n", 1.5)
		// 这一句就是要抓的那一下：任何 autorelease 来的对象都会在这里被
		// 多放一次，引用计数归零。
		pop(pool)

		// 然后再用一次。多放过的已经是野指针，这里就是当场段错误。
		if got := objValue(d, "s"); got != "一首歌" {
			t.Errorf("pop 之后歌名变成了 %q", got)
		}
		if got := numValue(d, "n"); got != 1.5 {
			t.Errorf("pop 之后数值变成了 %v", got)
		}
		release(d)
	})

	// 整条推走一遍：那条路上每一步都造了东西。
	p := mpTestPlatform(t)
	onObjCThread(func() {
		pool := push()
		p.applyOnObjCThread(NowPlaying{State: StatePlaying, Title: "一首歌", Duration: time.Minute})
		pop(pool)
	})
	// pop 之后再从系统那份里读一次：那本字典是系统 retain 着的，值也归它，
	// 所以这里读到的必须是活的。
	center := nowPlayingCenter()
	if got := objValue(infoValue(center), keyTitle); got != "一首歌" {
		t.Errorf("pop 之后系统那份的歌名是 %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 评审第二轮指出来的两处，各配一条用例
// ─────────────────────────────────────────────────────────────────────────────

// TestAWriteFromTheWrongThreadIsRefused 盯「写系统只能在那条线程上」。
//
// 评审指出：封面补推原本在**下载协程**上直接调 `push`，而 apply（暂停、停止、
// 清空）在另一条路。`push` 一次发两条系统消息，两次 push 一交错就留下
// 「元数据说暂停、状态说在放」——而校正只在播放中跑，这种错**不会自己好回来**。
//
// 修法是补推也走 `onObjCThread`，再在 `push` 顶上加一道闸门：不在那条线程上
// 就**拒写**。这条用例断的就是那道闸门——从测试协程（不是那条线程）直接调
// `push`，系统那边必须什么都没变。
//
// 拒写而不是硬写，是故意的：一次不写只是这一帧没更新，下一次 publish 自然
// 补上；写坏了却是「进度不动但按钮显示在放」，而且没人会发现。
func TestAWriteFromTheWrongThreadIsRefused(t *testing.T) {
	p := mpTestPlatform(t)
	onObjCThread(func() {
		p.applyOnObjCThread(NowPlaying{State: StatePlaying, Title: "先写进去的一首", Duration: time.Minute})
	})
	before := objValue(infoValue(nowPlayingCenter()), keyTitle)

	// 测试协程不是钉住的那条线程。
	p.push(NowPlaying{State: StatePaused, Title: "从错的线程写进来的", Duration: time.Minute}, 0)

	if got := objValue(infoValue(nowPlayingCenter()), keyTitle); got != before {
		t.Errorf("从错的线程写进去了：歌名变成了 %q（want %q）", got, before)
	}
	if got := objValue(infoValue(nowPlayingCenter()), keyTitle); got == "从错的线程写进来的" {
		t.Error("从错的线程进来的那一份被写进去了")
	}
}

// TestTheArtworkRepushRunsOnTheSameThreadAsEverythingElse 盯封面补推也在那条线程上。
//
// 这条断的是**修复本身**而不是闸门：闸门只保证「不在那条线程上就不写」，
// 不保证「封面到货时一定会写」。如果补推被拒了，锁屏上的封面就永远停在
// 上一首——而界面上已经是新歌了。
//
// 所以这里放一张真的封面进去，然后等它出现在系统那份里。
func TestTheArtworkRepushRunsOnTheSameThreadAsEverythingElse(t *testing.T) {
	p := mpTestPlatform(t)
	p.fetch = func(string) ([]byte, error) { return testPNG(t, 64, 64), nil }

	p.apply(NowPlaying{
		State:      StatePlaying,
		Title:      "有封面的一首",
		Duration:   time.Minute,
		ArtworkURL: "https://example.invalid/cover.png",
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if objectForKey(infoValue(nowPlayingCenter()), keyArtwork) != 0 {
			return // 到了：补推真的写进去了
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("封面下好了，但补推没有写进系统——补推多半被线程那道闸门拒掉了")
}

// TestTheMediaTypeIsAnEnumNotAString 盯 `MPNowPlayingInfoPropertyMediaType`
// 装的是枚举（NSNumber），不是字符串。
//
// 头文件里这个属性声明的是 `MPNowPlayingInfoMediaType`（NS_ENUM）。给它一个
// NSString，系统**认不出来**——而认不出来的表现是静默忽略：歌名、进度、封面
// 全照常显示，只是这一栏不起作用。
func TestTheMediaTypeIsAnEnumNotAString(t *testing.T) {
	p := mpTestPlatform(t)
	p.apply(NowPlaying{State: StatePlaying, Title: "一首歌", Duration: time.Minute})

	v := objectForKey(infoValue(nowPlayingCenter()), keyMediaType)
	if v == 0 {
		t.Fatal("系统那份里没有 mediaType")
	}
	if desc := goString(send(v, "description")); desc != "1" {
		t.Errorf("mediaType 的值是 %q（%s），want 1（MPNowPlayingInfoMediaTypeAudio）",
			desc, goString(send(v, "class")))
	}
}

// TestTheArtworkRepushCannotResurrectAPause 盯「封面补推不会把暂停按回在放」。
//
// 评审第二轮指出的第二个问题：补推原本是「在锁里取一份快照 → 解锁 → 排队写」。
// 排队和执行之间有间隙，用户在这空隙里按了暂停，apply 先跑完（系统已经是
// 暂停），补推随后把**过期的「在放」快照**整体写回去——暂停被按掉了。
//
// 串行化只挡住了「两次 push 各自的两条 setter 互相穿插」，没挡住「旧快照晚写」。
// 所以这条用例逼出那个空隙：让 apply（暂停）在封面下好**之前**完成，然后才让
// 补推执行，最后读回系统那份——状态必须是暂停，不能是又变回在放。
//
// 它也顺带钉住「快照在写的那一步现取」这件事：补推写的时候用的是**那一刻**
// 的状态，而不是几十毫秒之前的那份。
func TestTheArtworkRepushCannotResurrectAPause(t *testing.T) {
	p := mpTestPlatform(t)

	fetched := make(chan struct{})
	release := make(chan struct{})
	p.fetch = func(string) ([]byte, error) {
		close(fetched)
		<-release // 卡住：让「暂停」确定地在封面就绪之前跑完
		return testPNG(t, 48, 48), nil
	}

	p.apply(NowPlaying{
		State:      StatePlaying,
		Title:      "有封面的一首",
		Duration:   time.Minute,
		ArtworkURL: "https://example.invalid/cover.png",
	})
	<-fetched // 封面在下了

	// 趁它卡着，用户按了暂停。
	//
	// ⚠️ 快照里**必须带着同一个 ArtworkURL**——`App` 交出来的每一份都带着封面
	// 地址（`mediaSnapshotLocked` 里 `ArtworkURL: a.cover`），暂停只改状态。
	// 这里抹掉它就等于换了一首没有封面的歌，那条守卫会正确地把封面丢掉，
	// 断的就不是交错那件事了。
	p.apply(NowPlaying{
		State:      StatePaused,
		Title:      "有封面的一首",
		Duration:   time.Minute,
		ArtworkURL: "https://example.invalid/cover.png",
	})
	if got := sendInt(nowPlayingCenter(), "playbackState"); got != sysStatePaused {
		t.Fatalf("暂停没写进去（playbackState=%d），后面那条用例就断不出东西", got)
	}

	close(release) // 封面下好了，补推该跑了

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if objectForKey(infoValue(nowPlayingCenter()), keyArtwork) != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	center := nowPlayingCenter()
	if got := objectForKey(infoValue(center), keyArtwork); got == 0 {
		t.Fatal("封面没有补推上去")
	}
	// 🔴 关键那条：补推不能把暂停按回在放。
	if got := sendInt(center, "playbackState"); got != sysStatePaused {
		t.Errorf("封面补推把状态写成了 %d（want %d = paused）——它把过期的「在放」快照写回来了",
			got, sysStatePaused)
	}
	if got := numValue(infoValue(center), keyRate); got != 0 {
		t.Errorf("封面补推把速率写成了 %v（want 0）——进度条会和「暂停」打架", got)
	}
}

// TestTheArtworkRepushDoesNotResurrectAClear 盯「清空之后不会被补推写回」。
//
// 清空（没有歌了）比暂停更重：补推若把旧快照写回来，锁屏上会**凭空出现一首
// 正在播放的歌**——用户明明已经退出了。
func TestTheArtworkRepushDoesNotResurrectAClear(t *testing.T) {
	p := mpTestPlatform(t)

	fetched := make(chan struct{})
	release := make(chan struct{})
	p.fetch = func(string) ([]byte, error) {
		close(fetched)
		<-release
		return testPNG(t, 48, 48), nil
	}

	p.apply(NowPlaying{
		State:      StatePlaying,
		Title:      "有封面的一首",
		Duration:   time.Minute,
		ArtworkURL: "https://example.invalid/cover.png",
	})
	<-fetched

	p.apply(NowPlaying{State: StateNone}) // 退出 / 没有歌了
	if info := infoValue(nowPlayingCenter()); info != 0 {
		t.Fatalf("清空没生效（nowPlayingInfo 还在），后面那条用例就断不出东西")
	}

	close(release)
	time.Sleep(300 * time.Millisecond) // 给补推留足它跑完的时间

	if info := infoValue(nowPlayingCenter()); info != 0 {
		t.Errorf("封面补推把已经清空的那份写回来了（%s）——锁屏上会凭空多出一首歌",
			objValue(info, keyTitle))
	}
}
