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

// TestNSStringRoundTrip 盯字符串进得去也出得来，含中文和 emoji。
//
// 中文是这一层最可能坏的地方：`initWithBytes:length:encoding:` 按字节算
// 长度、按 UTF-8 解码，用 C 字符串那一条路（`stringWithUTF8String:`）在
// 中文上是**按字节截断**的，而歌名全是中文——症状是锁屏上的歌名少一半。
func TestNSStringRoundTrip(t *testing.T) {
	for _, want := range []string{"", "hello", "一首歌", "无损 FLAC 🎵", "a\x00b"} {
		got := goString(nsString(want))
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
		if got := sendDouble(nsNumberDouble(want), "doubleValue"); got != want {
			t.Errorf("%.1f 变成了 %.1f", want, got)
		}
	}
}

// TestDictionaryRoundTrip 盯字典里的键和值都拿得回来。
func TestDictionaryRoundTrip(t *testing.T) {
	d := nsMutableDictionary()
	if d == 0 {
		t.Fatal("建不出 NSMutableDictionary")
	}
	dictPut(d, keyTitle, nsString("歌名"))
	dictPut(d, keyRate, nsNumberDouble(1))

	if got := goString(send(d, "objectForKey:", uintptr(nsString(keyTitle)))); got != "歌名" {
		t.Errorf("取回来的是 %q，want 歌名", got)
	}
	if got := sendDouble(send(d, "objectForKey:", uintptr(nsString(keyRate))), "doubleValue"); got != 1 {
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

// infoValue 从系统里那份曲目信息里取一个键。
func infoValue(center id, key string) id {
	return send(center, "nowPlayingInfo")
}

// TestNowPlayingInfoRoundTrip 盯写进去的系统读得回来，值也对。
//
// ⚠️ 它断不了「键名对不对」：`nowPlayingInfo` 是原样存回来给你读的，
// 键名拼错它照样原样返回。但它断得住「值写错了」——时长写成毫秒、
// 进度写反了，这里立刻就看出来。
func TestNowPlayingInfoRoundTrip(t *testing.T) {
	p := mpTestPlatform(t)
	center := nowPlayingCenter()

	p.push(NowPlaying{
		State:    StatePlaying,
		Title:    "一首歌",
		Artist:   "某某",
		Duration: 3*time.Minute + 30*time.Second,
		Position: 90 * time.Second,
	}, 0)

	info := infoValue(center, keyTitle)
	if info == 0 {
		t.Fatal("写进去之后 nowPlayingInfo 是空的")
	}
	if got := goString(send(info, "objectForKey:", uintptr(nsString(keyTitle)))); got != "一首歌" {
		t.Errorf("歌名是 %q，want 一首歌", got)
	}
	if got := goString(send(info, "objectForKey:", uintptr(nsString(keyArtist)))); got != "某某" {
		t.Errorf("艺人是 %q，want 某某", got)
	}
	if got := sendDouble(send(info, "objectForKey:", uintptr(nsString(keyDuration))), "doubleValue"); got != 210 {
		t.Errorf("时长是 %v 秒，want 210", got)
	}
	if got := sendDouble(send(info, "objectForKey:", uintptr(nsString(keyElapsed))), "doubleValue"); got != 90 {
		t.Errorf("进度是 %v 秒，want 90", got)
	}
	// 播放速率是必设的（系统不看我们的音频会话）。
	if got := sendDouble(send(info, "objectForKey:", uintptr(nsString(keyRate))), "doubleValue"); got != 1 {
		t.Errorf("在放时的速率是 %v，want 1", got)
	}

	// 收尾：这一份是进程全局的，别留给别的用例看。
	p.push(NowPlaying{State: StateNone}, 0)
}

// TestThePlaybackRateFollowsTheState 盯速率跟着状态走。
//
// 暂停/停止时必须是 0：非 0 的话控制中心会以为歌还在放，进度条自己往前
// 爬，而歌是停着的——用户看锁屏就能看出���不对。
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
		p.push(NowPlaying{State: c.state, Title: "一首歌", Duration: time.Minute}, 0)
		info := infoValue(center, keyRate)
		got := sendDouble(send(info, "objectForKey:", uintptr(nsString(keyRate))), "doubleValue")
		if got != c.want {
			t.Errorf("状态 %v 时速率是 %v，want %v", c.state, got, c.want)
		}
	}
	p.push(NowPlaying{State: StateNone}, 0)
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
		p.push(NowPlaying{State: c.state, Title: "一首歌", Duration: time.Minute}, 0)
		if got := sendInt(center, "playbackState"); got != c.want {
			t.Errorf("状态 %v 之后系统那边是 %d，want %d", c.state, got, c.want)
		}
	}

	// 没有歌的时候系统那边应该是「停了」，不是「不知道」——
	// unknown 在控制中心上留着一个不知道该显示什么的按钮。
	p.push(NowPlaying{State: StateNone}, 0)
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

	p.push(NowPlaying{State: StatePlaying, Title: "一首歌", Duration: time.Minute}, 0)
	if infoValue(center, keyTitle) == 0 {
		t.Fatal("前面的写入没生效，后面的清空就断不出东西")
	}
	p.push(NowPlaying{State: StateNone}, 0)
	if info := infoValue(center, keyTitle); info != 0 {
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
	if img := nsImageFromBytes([]byte("这不是一张图")); img != 0 {
		t.Error("拿一段垃圾造出了一个 NSImage")
	}
	if img := nsImageFromBytes(nil); img != 0 {
		t.Error("拿空字节造出了一个 NSImage")
	}
}
