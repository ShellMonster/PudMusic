package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

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
