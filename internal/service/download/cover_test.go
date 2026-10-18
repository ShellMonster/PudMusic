package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/mux/muxtest"
)

// 这一组盯的是「下完一首歌之后它旁边有一张封面，而且那张封面和音频对得上名字」。
//
// ⚠️ 音频走的是 `cdn` 那台懂 Range 的假服务器（和其余各组同一台），而**封面
// 单独一台**：那是 B 站图床，和放音频的 CDN 不是一台。混在一台上的话，「封面
// 请求不打 Range」这件事就没有被验到——而真机上 B 站的 CDN 是不认 Range 的，
// 代码里一个 Range 头打过去就是一次 200 + 整个文件。
//
// 被 mock 掉的是「图床肯不肯给」和「给的是不是图」，**不是**「存到哪儿」：
// 落盘名、扩展名、同名避让、已存在就不重下，全都在真的跑。

// fakeJPEG 是一段真的 JPEG 文件头 + 一点正文。
//
// ⚠️ 正文是按 `payloadByte` 算出来的、断言比对的是**逐字节**：所以一个「把错误页
// 也写下去」的实现照样绿——错误页的字节不一样，而长度「看着也像一张图」。
var fakeJPEG = func() []byte {
	b := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'B', 'I', 'U'}
	for i := range 64 {
		b = append(b, payloadByte(0, int64(i)))
	}
	return b
}()

// coverServer 是一台假图床。
type coverServer struct {
	srv *httptest.Server

	mu     sync.Mutex
	hits   int // 一共被要了几次
	status int // 回给请求的状态码，0 = 200
	body   []byte
}

func newCoverServer(t *testing.T) *coverServer {
	t.Helper()
	c := &coverServer{status: 200, body: fakeJPEG}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c.mu.Lock()
		c.hits++
		status, body := c.status, c.body
		c.mu.Unlock()
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(status)
		if status == 200 {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *coverServer) url() string { return c.srv.URL + "/cover.jpg" }

func (c *coverServer) hitCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

func (c *coverServer) fail() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status, c.body = http.StatusNotFound, []byte("no such cover")
}

// heal 让它重新好好给。
func (c *coverServer) heal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status, c.body = http.StatusOK, fakeJPEG
}

// coverManager 建一个真下载、真落盘的队列，音频从 cdn 下、封面从 cover 下。
//
// audioExt 是音频落盘的扩展名（照 B 站给的那个后缀走，见 `extFor`），所以测试
// 里想让封面叫什么名字就靠它。
func coverManager(t *testing.T, cdn *cdn, dir string) *Manager {
	t.Helper()
	m, err := New(Options{
		Client: http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) {
			return &Source{
				Bvid: bvid, Cid: cid, Quality: 30280, Label: "320K",
				URLs: []string{cdn.srv.URL + "/song" + audioExt + "?upsig=x&deadline=9999999999"},
			}, nil
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close("") })
	return m
}

// audioExt 是这一组里音频落盘的扩展名。假 CDN 照着地址上的后缀给（`extFor` 的
// 顺序是「地址 > 来源说的 > 入队时的临时值」），而 B 站绝大多数是 AAC-in-MP4。
const audioExt = ".m4a"

// waitCoverFile 等后台那一步把封面落下来。
//
// ⚠️ **必须等**：存封面**不在**队列那条串行的路上（`startCoverSave` 立刻返回），
// 而这一组盯的正是「它存对了没有」。`StatusDone` 只说明**歌**下完了，而封面比它
// 晚那么一小会儿——测的是封面，就等封面。
//
// 用自己的循环而不是本包那个 `waitUntil`，只为让等不到时那句话里有**哪一个文件**。
func waitCoverFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 10s，%s 还是没有：存封面那一步没走完", path)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// readFile 读一个文件，缺了就报出来是哪一步缺的。
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s：%v", path, err)
	}
	return b
}

// TestTheCoverLandsNextToTheSongAndSharesItsName 断最要紧的那一条：
// **封面在音频旁边，文件名只差一个后缀**。
//
// 钉的是「差一个后缀」而不是「有个叫 `.jpg` 的文件」：封面存到别处（缓存目录、
// 子目录、拿 bvid 命名的）都满足后一条，而用户找歌时找的是自己下下来的那个
// 文件名旁边的那张图。
func TestTheCoverLandsNextToTheSongAndSharesItsName(t *testing.T) {
	cdn, cover := newCDN(t, 1024), newCoverServer(t)
	dir := t.TempDir()
	m := coverManager(t, cdn, dir)

	snap, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "封面测试歌", Cover: cover.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, m, snap.ID, StatusDone)

	// 封面和音频同一个目录、同一个词干，差一个后缀。
	wantAudio := filepath.Join(dir, "封面测试歌"+audioExt)
	if done.Dest != wantAudio {
		t.Fatalf("音频落到了 %q，want %q", done.Dest, wantAudio)
	}
	wantCover := filepath.Join(dir, "封面测试歌.jpg")
	if got := CoverPathFor(done.Dest); got != wantCover {
		t.Fatalf("CoverPathFor(%q) = %q，want %q", done.Dest, got, wantCover)
	}
	waitCoverFile(t, wantCover)
	if got := readFile(t, wantCover); string(got) != string(fakeJPEG) {
		t.Errorf("封面内容是 %d 字节，want 图床给的那 %d 字节逐字相同"+
			"（存成别的图——多半是错误页——时长度照样「像是」一张）", len(got), len(fakeJPEG))
	}
	if got := cover.hitCount(); got != 1 {
		t.Errorf("图床被要了 %d 次，want 1（一次就够）", got)
	}
}

// TestTwoSongsWithTheSameNameGetTheirOwnCover 断「同名避让之后仍然对得上」。
//
// 这一条是上一条**看不出来的那一半**：两首同名的歌落盘时是 `歌名.m4a` 和
// `歌名 (1).m4a`（`destFor` 的同名避让），而封面照 **落盘名** 推——于是第二首
// 的封面必须是 `歌名 (1).jpg`。从歌名重新算一遍的写法会给两首都算出 `歌名.jpg`，
// 于是第二首的封面盖掉了第一首的，而**两首的音频都好好的**。
func TestTwoSongsWithTheSameNameGetTheirOwnCover(t *testing.T) {
	cdn, cover := newCDN(t, 1024), newCoverServer(t)
	dir := t.TempDir()
	m := coverManager(t, cdn, dir)

	first, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: "c1", Title: "同名", Cover: cover.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	f := waitStatus(t, m, first.ID, StatusDone)

	second, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: "c2", Title: "同名", Cover: cover.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := waitStatus(t, m, second.ID, StatusDone)

	if s.Dest == f.Dest {
		t.Fatalf("两首同名歌落到了同一个文件 %q——那上面两首互相盖掉了，"+
			"这一条验的就是这一种", s.Dest)
	}
	for _, snap := range []Snapshot{f, s} {
		waitCoverFile(t, CoverPathFor(snap.Dest))
		if got := readFile(t, CoverPathFor(snap.Dest)); string(got) != string(fakeJPEG) {
			t.Errorf("%s 旁边的封面内容不对（%d 字节）", snap.Dest, len(got))
		}
	}
}

// TestACoverThatCannotBeFetchedDoesNotFailTheSong 断「封面拿不到**不是**这一首
// 的错」。
//
// 歌已经在 `Dest` 上了。图床 404、封面地址过期、用户在一个断网的网络上点下载
// ——这些都让这一首变不成「出错」。而那一行只有一句红字可说，说「下载失败」时
// 用户会以为那首歌没了，然后重新下一遍几百兆。
func TestACoverThatCannotBeFetchedDoesNotFailTheSong(t *testing.T) {
	cdn, cover := newCDN(t, 1024), newCoverServer(t)
	cover.fail()
	dir := t.TempDir()
	m := coverManager(t, cdn, dir)

	snap, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "没有封面的歌", Cover: cover.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, m, snap.ID, StatusDone)

	if done.Err != "" {
		t.Errorf("封面没下下来却给这一行留了红字 %q——歌已经在盘上了", done.Err)
	}
	st, err := os.Stat(done.Dest)
	if err != nil || st.Size() == 0 {
		t.Errorf("歌没落盘（%v）：封面失败不许牵连它", err)
	}
	// 等它**试过**（`hitCount` 是那条线数的），否则这一条量的是一个还没开始的
	// 动作 —— 那时盘上当然还没有封面，量不出任何东西。
	waitUntil(t, func() bool { return cover.hitCount() == 1 })
	if _, err := os.Stat(CoverPathFor(done.Dest)); err == nil {
		t.Error("图床说了 404，硬盘上却多出一张封面")
	}
}

// TestACoverAlreadyOnDiskIsNotFetchedAgain 断「已经有一份就不再下一次」。
//
// 这一条同时钉住两件事：**没有第二次网络往返**（判据是图床被要的次数，
// 而不是「文件还在」——重下一遍内容一样时两者长得完全一样），以及**不覆盖**
// 盘上那一份（重试时写的是同一张图，可一旦不是同一张，覆盖就等于删掉用户
// 自己换上去的那张）。
func TestACoverAlreadyOnDiskIsNotFetchedAgain(t *testing.T) {
	cdn, cover := newCDN(t, 1024), newCoverServer(t)
	dir := t.TempDir()
	m := coverManager(t, cdn, dir)

	// 先摆一份**不是**图床会给的那张。
	placed := filepath.Join(dir, "自己挑的封面.jpg")
	if err := os.WriteFile(placed, []byte("用户自己放的那张"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "自己挑的封面", Cover: cover.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, snap.ID, StatusDone)

	if got := cover.hitCount(); got != 0 {
		t.Errorf("盘上已经有封面了，图床还是被要了 %d 次", got)
	}
	if got := string(readFile(t, placed)); got != "用户自己放的那张" {
		t.Errorf("盘上那一份被改成了 %q", got)
	}
}

// TestTheCoverNameFollowsTheNameThatActuallyLanded 断「用的是**落盘之后**那个
// 名字」。
//
// 入队时给的扩展名只是临时值（`Task.Ext` 的注释：那一刻还没有地址），真正开始
// 下之前会按取流地址上的后缀重算一次。这一条让那一次重算真的发生：入队时写
// `.mp3`，落盘时变成 `.flac`。
//
// 盖不住的那一种错：从**入队时**那个名字推封面名。出来的 `歌名.jpg` 和盘上的
// `歌名.flac` 差的不只是一个后缀，而用户是照着歌名找封面的——他把两样都拷到
// 另一个盘时，封面就没了。
func TestTheCoverNameFollowsTheNameThatActuallyLanded(t *testing.T) {
	cdn, cover := newCDN(t, 1024), newCoverServer(t)
	dir := t.TempDir()
	m := coverManager(t, cdn, dir)

	snap, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "换了个容器", Cover: cover.url(),
		Dir: dir, Ext: ".mp3", // 入队那一刻给的**临时**后缀
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, m, snap.ID, StatusDone)

	if !strings.HasSuffix(done.Dest, audioExt) {
		t.Fatalf("落盘名是 %q，want 以 %q 结尾（这一条要验的就是「临时后缀被换掉"+
			"之后封面名跟着换」）", done.Dest, audioExt)
	}
	if got := CoverPathFor("换了个容器.mp3"); got == CoverPathFor(done.Dest) {
		t.Fatalf("CoverPathFor 对入队时那个名字和对落盘那个名字给的是同一个 %q——"+
			"这一条验的就是它俩不一样", got)
	}
	waitCoverFile(t, CoverPathFor(done.Dest))
	if got := readFile(t, CoverPathFor(done.Dest)); string(got) != string(fakeJPEG) {
		t.Errorf("落盘名旁边没有封面（内容 %d 字节）", len(got))
	}
}

// TestAVideoDoesNotGetACoverThatIsNotItsCover 断视频那一路**不存**封面。
//
// B 站给的是**稿件的封面**，而落盘的是交织出来的 `.mp4`——那一帧画面和那张图
// 不是一回事。把它存成 `.mp4` 的同名图，本地播放器会拿它当视频的第一帧，而用户
// 看到的是一张和视频内容毫无关系的图。
//
// 规格里点名的也是「下载**歌曲**时」：存这一份是为以后的本地音乐库备料，而音乐
// 库读的是歌。
//
// ⚠️ 这一条走的是**真的下完一支 MV**（两条 dash 流下完再交织，`videoHarness`），
// 不是一条出错的视频任务。差别在于：出错的视频任务在交织之前就停了，而
// `runOne` 压根不会调它——那种任务验的是「出错的那条路上没有封面」，而**下完的**
// 这一条验的才是「视频不存封面」。两件事，判据也不同（下面断的是图床被要了几次
// 加上盘上有没有多出那个文件）。
func TestAVideoDoesNotGetACoverThatIsNotItsCover(t *testing.T) {
	cover := newCoverServer(t)
	h := newVideoHarness(t, muxtest.VideoSpec(120), muxtest.AudioSpec(4))

	snap, err := h.m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "一支 MV", Dir: h.dir, Cover: cover.url(), Video: true,
	})
	if err != nil {
		t.Fatalf("入队：%v", err)
	}
	h.wait(snap.ID, StatusDone)

	if got := cover.hitCount(); got != 0 {
		t.Errorf("下完的 MV 去要了 %d 次封面：B 站那个地址是**稿件**的封面，"+
			"不是这一帧画面", got)
	}
	if entries, _ := os.ReadDir(h.dir); len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("MV 下完之后下载目录里有 %v，want 只有那一个 .mp4", names)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 🔴 存封面不许挡着队列
//
// 这一组是评审的 [major]：原来存封面是 `run` 末尾一句**同步**的调用，而在
// `runOne` 把状态落成 `StatusDone` **之前**。于是一台「接了连接不回包」的图床就
// 能把整条队列卡住。
// ─────────────────────────────────────────────────────────────────────────────

// hangingCoverServer 是一台**接了连接、一个字都不回**的假图床。
//
// ⚠️ 收尾要先把 `done` 关掉再 `Close()`：`httptest.Server.Close` 会等所有在飞的
// 请求返回，而这些 handler 都堵在 `done` 上。顺序反了就是测试自己把自己挂死。
type hangingCoverServer struct {
	srv *httptest.Server

	mu   sync.Mutex
	hits int
}

func newHangingCoverServer(t *testing.T) *hangingCoverServer {
	t.Helper()
	c := &hangingCoverServer{}
	done := make(chan struct{})
	c.srv = httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		c.mu.Lock()
		c.hits++
		c.mu.Unlock()
		<-done
	}))
	t.Cleanup(func() {
		close(done)
		c.srv.Close()
	})
	return c
}

func (c *hangingCoverServer) url() string { return c.srv.URL + "/cover.jpg" }

func (c *hangingCoverServer) hitCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

// TestACoverHostThatNeverAnswersDoesNotStallTheQueue 钉住评审的 [major]。
//
// 判据是**第二首真的下完了**，而它只可能在第一首的封面还在挂着的时候成立。
//
// ⚠️ 挂着的那个挂法是「接了连接不回包」：`Options.Client` 没有设 `Timeout`
// （`app.CredentialTransport.Client(nil)`），而 `DefaultTransport` 的
// `ResponseHeaderTimeout` 是 0——所以**只有用户自己按暂停 / 退出才解得开**。
// 换成「连不上」或「回 404」都不算数：那两种会立刻返回，卡不住任何东西。
//
// 这一条在同步那一版上会红成什么：5 秒后第一首的 1024 字节已经完完整整在盘上，
// 而 `Status` 还是 `running`（界面上画出来是「下载中」+ 100% 的条）；再排第二首
// 进去，20 秒后它仍是 `queued`、一个字节没下——因为 `loop` 是 `m.runOne(id)` 同步
// 跑完一首才下一首。
func TestACoverHostThatNeverAnswersDoesNotStallTheQueue(t *testing.T) {
	hanging := newHangingCoverServer(t)
	cdn := newCDN(t, 1024)
	dir := t.TempDir()
	m := coverManager(t, cdn, dir)

	first, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: "c1", Title: "第一首", Cover: hanging.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 图床此刻正挂着，而这一首**照样**变成「下完」——歌在盘上了，封面是装饰。
	first0 := waitStatus(t, m, first.ID, StatusDone)
	if _, err := os.Stat(first0.Dest); err != nil {
		t.Fatalf("歌没落盘：%v", err)
	}

	// 🔴 **下面这一条才是判据本体**：队列是串行的（`loop` 一次下一首），
	// 而第一首的封面还挂着。第一首下不完，第二首就一个字节都下不了。
	second, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: "c2", Title: "第二首", Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	s2 := waitStatus(t, m, second.ID, StatusDone)
	if _, err := os.Stat(s2.Dest); err != nil {
		t.Errorf("第二首没落盘：%v", err)
	}
	// ⚠️ **等它被要过**，不要立刻读：那条线是后台的，而计数是它写的。
	// 立刻读的话满载时会读到 0，于是这一条变成「压根没存封面」那一种——
	// 量不到「存了、但没挡住队列」，而那正是它要量的。
	waitUntil(t, func() bool { return hanging.hitCount() == 1 })
}

// TestClosingTheAppDoesNotWaitOnAHangingCover 断**退得出**。
//
// 上一条是「下载不被挡住」，这一条是「退出不被挡住」——那是同一件事的另外半边：
// 存封面的线跟着 `m.ctx` 走，而 `Close` 先 `m.quit()`（取消它）**之后**才
// `m.coverWG.Wait()`。所以退出不会去等一个不会回来的图床。
//
// ⚠️ 判据是 **`Close` 自己回来了**，而不是「那条线退了」：后者要数 goroutine，
// 而本包的 fixture 每台 `httptest` 服务器自己就留着连接 goroutine（`internal/app`
// 的 `online_test.go:1114` 记着同一个坑）。
func TestClosingTheAppDoesNotWaitOnAHangingCover(t *testing.T) {
	hanging := newHangingCoverServer(t)
	cdn := newCDN(t, 1024)
	dir := t.TempDir()
	m := coverManager(t, cdn, dir)

	snap, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "挂着的那首", Cover: hanging.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, snap.ID, StatusDone)
	// 图床已经被要了、那条线正挂着。
	waitUntil(t, func() bool { return hanging.hitCount() == 1 })

	done := make(chan error, 1)
	go func() { done <- m.Close("") }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Close 报错：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("关掉应用没有退出来——它在等一个不会回话的图床")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 落盘要原子
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCoverIsWrittenThroughATemporaryFile 钉住「临时文件 → rename」。
//
// 🔴 直接写最终名的坏处是：`exists()` 只看「有没有」，所以**写到一半断电 / 磁盘满**
// 会在盘上留一份半张图，而之后**每一次重试都被它挡掉**——那一首从此没有封面，
// 界面上那一行还写着「下载完成」，而用户自己看不出来。
//
// 判据是「存完之后的目录里只有该有的那两个文件」：一个直接写最终名的实现在这里
// 照样绿（它不留临时文件），所以**还要看下一条**。
func TestTheCoverIsWrittenThroughATemporaryFile(t *testing.T) {
	cdn, cover := newCDN(t, 1024), newCoverServer(t)
	dir := t.TempDir()
	m := coverManager(t, cdn, dir)

	snap, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "走临时的歌", Cover: cover.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, snap.ID, StatusDone)
	waitCoverFile(t, filepath.Join(dir, "走临时的歌.jpg"))

	// 存完之后目录里只有音频和封面：临时文件已经被 rename 掉了。
	if got := namesIn(t, dir); !slices.Equal(got, []string{"走临时的歌.jpg", "走临时的歌" + audioExt}) {
		t.Errorf("存完之后目录里有 %v，want 只有音频和封面两个"+
			"（多出来的那个是 rename 没走干净的临时文件）", got)
	}
}

// TestTheFinalNameOnlyEverAppearsWhole 断「最终那个名字**只可能整个出现**」。
//
// ⚠️ 上面那条（存完之后目录里只有两个文件）**抓不住**这一半：一个直接
// `os.WriteFile(path, …)` 的实现同样不留临时文件，同样绿。可坏就坏在它写的
// 那一瞬间——写到一半崩溃 / 磁盘满，盘上就是一份半张图，而 `hasCover` 说「有了」，
// 之后每一次重试都被挡掉。
//
// 所以这一条**在看它写的过程中**：临时文件是看得见的（它就在同一个目录里），
// 而最终那个名字在 rename 之前**根本不该存在**。
//
// ⚠️ 「看得见」要一份**够慢**的正文：64 MB 写一次要几十到几百毫秒，而一个
// 几十 KB 的封面写得比一次轮询还快，那就量不出任何东西了。正文多大只影响这条的
// 观察窗口，不影响被测代码——它拿到的是一个 `[]byte`。
func TestTheFinalNameOnlyEverAppearsWhole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "大正文"+coverExt)
	data := make([]byte, 64<<20)

	go func() { _ = writeCoverAtomically(path, data) }()

	sawTmp := false
	for range 20000 { // 最多看两秒（每次 100µs）
		names := namesIn(t, dir)
		for _, n := range names {
			if strings.HasPrefix(n, coverTmpPrefix) {
				sawTmp = true
			}
		}
		if _, err := os.Stat(path); err == nil && !sawTmp {
			// 最终名字在**还没见到临时文件**时就出现了：那就是直接写过去的。
			// 再等一拍，确认它不是在同一瞬间被 rename 出来的。
			if !sawTmp && coversWhole(path, len(data)) {
				t.Errorf("最终那个名字 %s 在写完之前就出现了：这是直接写过去的"+
					"（写到一半断电就留下一份半张图）", path)
				return
			}
		}
		if sawTmp {
			break
		}
		time.Sleep(100 * time.Microsecond)
	}
	if !sawTmp {
		t.Fatalf("两秒里一次都没看到 %s 开头的临时文件——量不出来"+
			"（正文 %d 字节可能写得太快），这一条在当前机器上无效", coverTmpPrefix, len(data))
	}
	// 看到过临时文件之后，它必须被 rename 走：一个半张图被留在盘上比没有更糟。
	waitUntil(t, func() bool { _, err := os.Stat(path); return err == nil })
	if got := namesIn(t, dir); !slices.Equal(got, []string{"大正文" + coverExt}) {
		t.Errorf("落定之后目录里有 %v，want 只有最终那个封面"+
			"（临时文件没被 rename 走 = 一份半张图留在用户的下载目录里）", got)
	}
}

// coversWhole 报 path 上那个文件是不是已经有 want 个字节了（还可能在写）。
func coversWhole(path string, want int) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() == int64(want)
}

// TestAFailedCoverSaveLeavesNothingBehindThatWouldBlockEveryRetry 断「失败不许
// 留下一个会把之后每一次重试都挡掉的东西」。
//
// ⚠️ 判据不是「盘上有没有封面」——那个在失败时本来就该没有。判据是**重来一遍能存
// 得上**：一个「写了一半的最终名」的实现在这里绿（它的第一次确实失败了），而它留下
// 的那份会让第二次也失败——而**那才是伤到用户的那一半**。
func TestAFailedCoverSaveLeavesNothingBehindThatWouldBlockEveryRetry(t *testing.T) {
	cdn, cover := newCDN(t, 1024), newCoverServer(t)
	dir := t.TempDir()
	m := coverManager(t, cdn, dir)

	// ⚠️ **先让图床坏掉再入队**：存封面是在后台的（`startCoverSave` 立刻返回），
	// 先入队后调用 `fail()` 的话，那条线有可能已经把一次 200 拿到手了——于是这一
	// 条量的是「存成功了」，而它要量的是「存失败了之后留下了什么」。
	cover.fail()
	snap, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "先失败的歌", Cover: cover.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, snap.ID, StatusDone)
	waitUntil(t, func() bool { return cover.hitCount() == 1 })

	// 第一次失败之后：最终那个名字上**什么都没有**（半张图是最坏的一种），临时
	// 文件也没有（它是被 defer 收掉的）。
	if names := namesIn(t, dir); len(names) != 1 {
		t.Errorf("失败之后目录里有 %v，want 只有音频那一个——"+
			"留下的东西（半张图 / 临时文件）会把之后每一次重试都挡掉", names)
	}

	// 第二次（换一首，触发一次新的存）：图床好了，这一次必须存得上。
	cover.heal()
	again, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: "c2", Title: "先失败的歌", Cover: cover.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, again.ID, StatusDone)
	waitCoverFile(t, CoverPathFor(again.Dest))

	if got := readFile(t, CoverPathFor(again.Dest)); string(got) != string(fakeJPEG) {
		t.Errorf("重来一遍存上的封面不对（%d 字节）——上一次失败留下了什么，"+
			"把这一次挡掉了", len(got))
	}
}

// TestADirectoryInTheCoversPlaceIsNotMistakenForACover 断「同名处是一个**目录**
// 时，不许把它当成现成的封面」。
//
// ⚠️ `os.Stat` 对目录一样返回成功，而直接 `os.WriteFile` 到那个名字会
// `is a directory` 失败。两个后果都坏：
//
//	当「已经有了」  用户自己建了个同名文件夹（`歌名.jpg/`），于是这一首**永远**
//	                没有封面，而 `startCoverSave` 连图床都不问
//	当「没有」      `hasCover` 判它不是普通文件，于是真去存 —— 而 rename 到一个
//	                目录上会失败。那就报错，**不覆盖那个目录**
func TestADirectoryInTheCoversPlaceIsNotMistakenForACover(t *testing.T) {
	cdn, cover := newCDN(t, 1024), newCoverServer(t)
	dir := t.TempDir()
	// 用户自己在那个名字上放了个目录。
	if err := os.Mkdir(filepath.Join(dir, "有目录的歌"+coverExt), 0o755); err != nil {
		t.Fatal(err)
	}
	m := coverManager(t, cdn, dir)

	snap, err := m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid, Title: "有目录的歌", Cover: cover.url(), Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, snap.ID, StatusDone)
	// 判据是**图床被问过**：一个「目录 = 已经有封面」的实现一次都不会问，
	// 而那正是这一条要挡的那一种。
	waitUntil(t, func() bool { return cover.hitCount() == 1 })

	// 而那个目录还在：rename 到目录上失败时，我们报错，不去动它。
	st, err := os.Stat(filepath.Join(dir, "有目录的歌"+coverExt))
	if err != nil || !st.IsDir() {
		t.Errorf("用户自己建的那个目录被动了（%v）——它不是我们的东西", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────

// namesIn 列出一个目录里的文件名（不含临时目录本身）。
func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}
