// 评论页（票 #573）的验收。
//
// # 这一族断的是「画面上有什么」
//
// 评论页最容易坏在**看不出来**的地方：页头那句话摆错了（用户以为自己在读歌的
// 评论，而他在读视频的）、表情没成图（画面上留着 `[妙啊]` 三个字）、长正文把行
// 撑破了。这些都不会报错，只是悄悄不对。
//
// ⇒ 断的都是**渲染结果**：页头那两句话分别摆在什么条目上、那一枚「UP 主」
// 徽章出现在谁身上、画面上有没有洋红色的那一小块（表情图）、正文有没有被
// 折成「…」。
//
// # 一个真请求都不发
//
// 数据走一台**假 B 站**（`newUIExplore`），回的是**真接口录下来的 fixture**
// （`internal/service/bili/testdata/comment/`）——形状是真的，地址是假的。
// 图也走一台假图床（`fakeImageTransport`）：B 站的图床是 `i0.hdslb.com`，
// 不替掉的话测试会**真的去下载**（AGENTS.md §6）。
package ui

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 一台假 B 站 + 一台假图床
// ─────────────────────────────────────────────────────────────────────────────

// commentBili 是评论页那几页用的假 B 站。
type commentBili struct {
	*uiExplore

	mu sync.Mutex
	// files 是提前读进内存的几份 fixture（**不许**在处理请求的 goroutine 里
	// 读文件、更不许调 `t.Fatal`——那不是测试线程）。
	files map[string][]byte
	// mainFile / latestFile / page2 是三档各回哪一份。
	mainFile   string
	latestFile string
	page2      string
	// mainRaw 是「主列表那一档回这一份」（nil = 回 `files[mainFile]`）。
	//
	// ⚠️ **它是给「长正文」那一条用的**：真 fixture 里那一条正文只有一百来
	// 个字，在宽窗口上根本折不到 6 行——于是「折起来」那条压根触发不了。
	mainRaw []byte
	// hold 非 nil 时，**第二页那一趟卡在这个闸门上**（用来验「正在拉」）。
	hold chan struct{}
	// calls 记着主列表那几趟的查询串。
	calls []url.Values
}

// newCommentBili 装一台假 B 站。
//
// ⚠️ **回的是真 fixture**：形状（字段名、层级、`cursor` 那一套）照着真接口
// 录下来的那份，而**内容**是挑过的六条——其中有一条是 UP 主自己发的
// （`mid == upper.mid`），有一条带 `up_reply=true` 但不是 UP 主发的。
func newCommentBili(t *testing.T, mainFile string) *commentBili {
	t.Helper()
	b := &commentBili{
		uiExplore:  newUIExplore(t),
		files:      map[string][]byte{},
		mainFile:   mainFile,
		latestFile: "reply-main-latest.json",
		page2:      "reply-main-mode3-p2.json",
	}
	for _, n := range []string{
		"reply-main.json", "reply-main-latest.json", "reply-main-mode3-p2.json",
		"reply-main-upreply.json", "reply-reply.json", "reply-main-empty.json",
	} {
		b.files[n] = commentFixtureBytes(t, n)
	}

	// bvid → avid。
	b.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"aid": 170001, "bvid": r.URL.Query().Get("bvid")})
	})

	// 主列表。
	b.on("/x/v2/reply/main", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		b.mu.Lock()
		b.calls = append(b.calls, q)
		var raw []byte
		var hold chan struct{}
		switch {
		case q.Get("pagination_str") != "":
			raw = b.files[b.page2]
			// ⚠️ **只有翻页那一趟卡闸门**：第一页必须回来（不然界面上是
			// 「正在拉评论…」，而那条要验的是**末尾那一行**的样子。
			hold = b.hold
		case q.Get("mode") == "2":
			raw = b.files[b.latestFile]
		default:
			raw = b.files[b.mainFile]
			if b.mainRaw != nil {
				raw = b.mainRaw
			}
		}
		b.mu.Unlock()
		if hold != nil {
			// ⚠️ **卡住第二页**：这一趟还在路上的时候，界面上要有一句
			// 「正在拉」——而那一段只有在这一趟没回来的时候才看得见。
			<-hold
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})

	// 楼中楼。
	b.on("/x/v2/reply/reply", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b.files["reply-reply.json"])
	})
	return b
}

// mainCallCount 数主列表被问了几回。
func (b *commentBili) mainCallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.calls)
}

// lastMainCall 是最后一次问主列表时的查询串。
func (b *commentBili) lastMainCall() url.Values {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.calls) == 0 {
		return nil
	}
	return b.calls[len(b.calls)-1]
}

// commentFixtureBytes 读真接口录下来的那份 fixture。
func commentFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("..", "service", "bili", "testdata", "comment", name)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读不到 fixture %s：%v", name, err)
	}
	return raw
}

// commentLongBody 是「第一条评论的正文很长」那一档用的那句正文。
//
// ⚠️ **真 fixture 里那一条正文只有一百来个字**：在 1400 宽的窗口上它折成
// 两三行，压根碰不到「最多 6 行」那道线——于是「折起来」那条根本触发不了
// （量出来的结果是「没折」，而那不是折叠坏了，是没到折叠的时候）。
const commentLongBody = "这是一句很长很长的评论，用来把正文撑到好几行，" +
	"确认它被折起来、末尾是省略号、而且没有把那一行撑破。"

// commentFixtureWithLongBody 是「第一条评论的正文很长」的那一份 fixture。
//
// ⚠️ **形状照着真 fixture，只把正文换长的**：字段名、层级、`reply_control`
// 那一套一个字都没动——折起来这件事要验的是排版，而排版只跟正文有关。
func commentFixtureWithLongBody(t *testing.T, name string) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(commentFixtureBytes(t, name), &doc); err != nil {
		t.Fatalf("fixture %s 读不懂：%v", name, err)
	}
	data, _ := doc["data"].(map[string]any)
	replies, _ := data["replies"].([]any)
	if len(replies) == 0 {
		t.Fatalf("fixture %s 里一条评论都没有", name)
	}
	first, _ := replies[0].(map[string]any)
	content, _ := first["content"].(map[string]any)
	content["message"] = strings.Repeat(commentLongBody, 12)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("fixture %s 编不出来：%v", name, err)
	}
	return raw
}

// fakeImageTransport 是一台假图床。
//
// ⚠️ **B 站的图床不许真连**（AGENTS.md §6）：fixture 里的头像和表情地址都是
// `i0.hdslb.com`，不替掉的话这一族测试会**真的去下载**。
//
// 🔴 **表情那一族给一张洋红色的图**：这是「表情真的成图了」那条断言的凭据
// ——洋红色在界面别的地方根本不出现（主题色是绿的），所以画面上数出洋红像素
// 就等于数出了表情图。
type fakeImageTransport struct {
	mu    sync.Mutex
	asked []string
}

func (f *fakeImageTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.asked = append(f.asked, r.URL.String())
	f.mu.Unlock()

	img := image.NewRGBA(image.Rect(0, 0, 18, 18))
	c := color.RGBA{R: 200, G: 200, B: 200, A: 255}
	if strings.Contains(r.URL.Path, "/bfs/emote/") {
		c = color.RGBA{R: 255, G: 0, B: 255, A: 255}
	}
	for y := 0; y < 18; y++ {
		for x := 0; x < 18; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"image/png"}},
		Body:       io.NopCloser(bytes.NewReader(buf.Bytes())),
		Request:    r,
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 外壳
// ─────────────────────────────────────────────────────────────────────────────

// newCommentsShell 造一个停在评论页上的外壳。
//
// ⚠️ **当前这一首是用队列摆的**（`Queue().Reset`）：`CommentsWorthAsking` 判的是
// 「当前这一首」，而队列是那一首的唯一入口（`app.CurrentItem`）。
func newCommentsShell(t *testing.T, b *commentBili, it app.Item, w, h int, opts ...shellOpt) (*Shell, *mygoui.Tester) {
	t.Helper()
	cli := bili.NewClient(bili.Options{Transport: b.Server.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	all := append([]shellOpt{at(w, h), mounted(func(t *testing.T, a *app.App) {
		a.MountLogin(app.NewMemoryStore("test"), cli)
		a.MountStreaming(app.Streaming{
			Client: cli, Media: b.Server.Client(), TempDir: t.TempDir()})
	})}, opts...)
	ts := newTestShell(t, all...)
	s := ts.s
	installFakeImages(t, s)
	s.app.Queue().Reset(it)
	s.navigate(commentsPath)
	ts.tt.Frame()
	commentsWait(t, ts.tt, "评论拉回来了", func() bool {
		return len(s.app.Comments()) > 0
	})
	// 🔴 **还要等「画出来了」**：上面等的是**应用层**拿到数据了，而行是
	// `syncComments` 在**下一帧**建的（它是每帧的副作用那一层）。只等应用层
	// 的话，下面那些断「画面上有什么」的用例会在**行还没建出来**的时候就去
	// 数——数出来的是 0，而那是**测早了**，不是功能坏了。
	//
	// ⚠️ **判据取「第一条评论的昵称在画面上」**：那是**画面上的东西**
	// （这一族断的都是渲染结果），而昵称每一份 fixture 都不一样，所以从
	// 应用层那条评论里取。
	first := s.app.Comments()[0]
	commentsWait(t, ts.tt, "评论画出来了", func() bool {
		return ts.tt.HasText(first.Uname)
	})
	return s, ts.tt
}

// installFakeImages 把封面缓存换成那台假图床。
//
// ⚠️ **收尾要把原来那一份还回去**（理由同 `downloadlist_ops_test.go`）：收尾
// 走的是 `Shell.Close`，它在**调用那一刻**才读 `s.images` 这个字段——所以还回去
// 的那一句得排在基座挂的那次收尾**前面**跑（`t.Cleanup` 后挂的先跑）。
func installFakeImages(t *testing.T, s *Shell) *fakeImageTransport {
	t.Helper()
	tr := &fakeImageTransport{}
	cache := imgcache.New(imgcache.Options{Client: &http.Client{Transport: tr}})
	keep := s.images
	keepRemove := s.imagesWakeRemove
	t.Cleanup(func() {
		cache.Close()
		s.images = keep
		s.imagesWakeRemove = keepRemove
	})
	s.images = cache
	s.imagesWakeRemove = cache.AddWake(s.wake.invalidate)
	return tr
}

// commentsWait 边跑帧边等 cond。
//
// ⚠️ **判据一律是结果**（后台那一趟跑在 goroutine 上，按帧数等就是按运气等）。
//
// ⚠️ **宽限给得宽**（30 秒）：这一族要起一台假 B 站、跑一趟后台请求，而机器
// 忙起来的时候（别的 agent 同时在跑 `go test`）那一趟要好几秒——宽限太窄的话
// 测出来的是「机器慢」，不是「功能坏」。
func commentsWait(t *testing.T, tt *mygoui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了三十秒还没「%s」，文字有 %q", what, tt.Texts())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// mvItem 是「一个 B 站视频」那一档的条目。
func mvItem() app.Item {
	return app.Item{
		Source: "online", Type: "mv", Bvid: "BV1xx411c7mD", Cid: "1",
		Title: "一个视频", Artist: "某个 UP 主",
	}
}

// audioItem 是「一首 B 站音乐库音频」那一档的条目。
func audioItem() app.Item {
	return app.Item{
		Source: "online", Type: "audio", Sid: "563526",
		Title: "一首歌", Artist: "某个歌手",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 页头：这些评论到底是谁的评论
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCommentsHeaderSaysWhoseCommentsTheseAre 断**页头那句话**。
//
// 🔴 **这是这一票最要紧的一件事**：三种条目拿到的评论**语义不同**——
//
//	audio  这一**首歌**的评论（实测原文是「太短了」「求全部」）
//	mv    **这个视频**的评论（在聊视频本身、UP 主、弹幕梗）
//	local 没有评论
//
// ⇒ 页头必须**分别**摆出两句话，而且用户**一眼**就分得清。
//
// ⚠️ **判据是「另一句话不许同时出现」**：只断「有『这个视频的评论』」的话，
// 一个两句话都摆的实现照样绿——而那正是用户分不清的形状。
func TestTheCommentsHeaderSaysWhoseCommentsTheseAre(t *testing.T) {
	for _, tc := range []struct {
		name    string
		it      app.Item
		want    string
		notWant string
	}{
		{"mv", mvItem(), "这个视频的评论", "这首歌的评论"},
		{"audio", audioItem(), "这首歌的评论", "这个视频的评论"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newCommentBili(t, "reply-main.json")
			_, tt := newCommentsShell(t, b, tc.it, 1200, 900)

			if !tt.HasText(tc.want) {
				t.Errorf("页头没有「%s」——用户分不清自己在读谁的评论，文字有 %q",
					tc.want, tt.Texts())
			}
			if tt.HasText(tc.notWant) {
				t.Errorf("页头把「%s」也摆上了——那是另一种条目的说法，"+
					"两句话同时在，用户分不清，文字有 %q", tc.notWant, tt.Texts())
			}
			// 歌名 / 视频标题 + UP 名 / 作者名也得在（页头不是只有那一句话）。
			if !tt.HasText(tc.it.Title) {
				t.Errorf("页头没有这一首的名字 %q，文字有 %q", tc.it.Title, tt.Texts())
			}
			if !tt.HasText(tc.it.Artist) {
				t.Errorf("页头没有 %q，文字有 %q", tc.it.Artist, tt.Texts())
			}
		})
	}
}

// TestTheCommentsPageIsNotInTheSidebarMenu 断它**不进侧栏菜单**。
//
// ⚠️ **它是「当前这一首」的附属页，不是一个栏目**：摆进侧栏的话，用户会以为
// 那是一个能一直待着的页面，而它讲的东西每换一首就全变了。
func TestTheCommentsPageIsNotInTheSidebarMenu(t *testing.T) {
	if rt := lookup(commentsPath); rt.InMenu {
		t.Errorf("评论页进侧栏菜单了——它是「当前这一首」的附属页，不是一个栏目")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 「UP 主」徽章
// ─────────────────────────────────────────────────────────────────────────────

// TestTheUpperBadgeOnlyShowsForTheUpperAuthor 断「UP 主」那一枚徽章的判据。
//
// 🔴 **判据是 `mid == upper.mid`**（这一条是 UP 主自己发的），**不是**
// `reply_control.up_reply`（那个是「UP 主回复过这一条」——实测拿它当判据，
// 会给几十条普通用户的评论挂上 UP 主标记）。
//
// ⚠️ **两档都要断**：
//
//	reply-main.json          一条 mid == upper.mid 的 ⇒ 恰好一枚徽章
//	reply-main-upreply.json  一条 up_reply=true 但 mid != upper.mid 的 ⇒ **一枚都没有**
//
// 只断第一档的话，「拿 `up_reply` 当判据」那个实现照样绿（它也会给那一条挂上）。
func TestTheUpperBadgeOnlyShowsForTheUpperAuthor(t *testing.T) {
	for _, tc := range []struct {
		file string
		want int
	}{
		{"reply-main.json", 1},
		{"reply-main-upreply.json", 0},
	} {
		t.Run(tc.file, func(t *testing.T) {
			b := newCommentBili(t, tc.file)
			_, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

			got := countExactText(tt, "UP 主")
			if got != tc.want {
				t.Errorf("画面上有 %d 枚「UP 主」徽章，而它该有 %d 枚——"+
					"判据是 `mid == upper.mid`，不是 `reply_control.up_reply`",
					got, tc.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 表情要成图
// ─────────────────────────────────────────────────────────────────────────────

// TestTheEmoteIsDrawnAsAnImage 断**表情占位符渲染成图**。
//
// 🔴 **判据是画面上有洋红色**：那台假图床给表情那一族发的是一张洋红色的图，
// 而洋红色在界面别的地方根本不出现（主色是绿的）——所以画面上数出洋红像素
// 就等于数出了表情图。
//
// ⚠️ **另一半是「`[妙啊]` 那三个字不许在画面上」**：正文是自己排版自己画的，
// 而框架的文本控件**只给 inline object 留位置、不画它**（`mygo/ui/paint.go` 的
// `textForeground` 跳过 `g.Object`）——不自己画的话那一格是空的，而正文里
// 留着字面量的话用户看到的是「[妙啊]」三个字。
func TestTheEmoteIsDrawnAsAnImage(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	// 等那张洋红色的图下来（图片缓存是后台下的，图好了才画得出来）。
	commentsWait(t, tt, "表情图下来了", func() bool {
		return magentaPixels(tt) > 0
	})

	if n := magentaPixels(tt); n == 0 {
		t.Errorf("画面上一个洋红像素都没有——表情没成图（文字有 %q）", tt.Texts())
	}
	// ⚠️ **字面量不许在**：正文里的 `[妙啊]` 换成了图，而**给辅助技术的那份
	// 文字**换成了「［表情］」——所以画面上既不该有「[妙啊]」，也不该有
	// 「［表情］」（那是给读屏软件的那一份，不是画出来的那一份）。
	for _, txt := range tt.Texts() {
		if strings.Contains(txt, "[妙啊]") {
			t.Errorf("画面上留着表情的字面量 %q——它该是一张图", txt)
		}
	}
}

// magentaPixels 数画面上有多少个洋红像素。
func magentaPixels(tt *mygoui.Tester) int {
	img := tt.Image()
	if img == nil {
		return 0
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R > 200 && p.G < 60 && p.B > 200 {
				n++
			}
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// 长正文折叠
// ─────────────────────────────────────────────────────────────────────────────

// TestALongCommentIsFoldedWithoutBreakingTheRow 断**长正文被折起来**。
//
// 🔴 **「折起来」是看画面**：第一条评论的正文很长（真 fixture 里那一段
// 「若将地球的四十六亿年浓缩为一天……」），它折行之后超出 6 行——折起来的话
// 画面上那一段文字**末尾是省略号**，而**整段原文不在画面上**。
//
// ⚠️ **「不破版」量的是那一行的排版高度**：正文那一格的高度是**排版算出来
// 的**（`commentBody.h`），而它不许超过 6 行。这一条量的是那一格缓存下来的
// 高度——因为「一行的盒子有多高」在画面上没有可见的边界（那个盒子上不画
// 任何东西），量不出来。
func TestALongCommentIsFoldedWithoutBreakingTheRow(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureWithLongBody(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	// 画面上有一段文字是以省略号收尾的。
	folded := false
	for _, txt := range tt.Texts() {
		// ⚠️ **要连着正文一起看**：界面别处也有带省略号的文字（歌名被截断
		// 的时候），只看「以『…』收尾」的话，一个根本没折的正文也能蒙混过关。
		if strings.HasSuffix(txt, "…") && strings.Contains(txt, commentLongBody[:20]) {
			folded = true
			break
		}
	}
	if !folded {
		t.Errorf("画面上没有任何一段以「…」收尾的文字——长正文没有被折起来，文字有 %q",
			tt.Texts())
	}

	// 整段原文不在画面上。
	//
	// ⚠️ **比的是整段**（`HasText` 是「包含」）：只比一句的话，折起来之后
	// 留在画面上的那一段**前缀**里就有它——于是「折起来了」和「没折」分不开。
	if tt.HasText(strings.Repeat(commentLongBody, 12)) {
		t.Errorf("长正文整段都在画面上——它没有被折起来")
	}

	// 那一格的高度不许超过 6 行。
	const rpid = int64(321942994928)
	body := s.comments.bodies[rpid]
	if body == nil {
		t.Fatalf("第一条评论没有排版缓存（rpid %d）", rpid)
	}
	if maxH := float32(commentBodyLines) * body.lineH; body.h > maxH+0.5 {
		t.Errorf("折起来的那一格有 %.1f 高，而 %d 行只有 %.1f——它破版了",
			body.h, commentBodyLines, maxH)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 排序
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSortHasOnlyTwoTiers 断**排序只有两档**。
//
// 🔴 **B 站那个「评论」（`mode=1`）和「最新评论」（`mode=2`）是同一份内容**
// （2026-10-27 真机回归实测，票 #591）：三页逐条比过 rpid，一模一样；而
// `cursor.support_mode` 恒为 `[2, 3]` 不含它——**那一档早就名存实亡**。
//
// ⇒ 界面上**不许**摆成两个内容一模一样的档位（用户切过去会以为「切了档
// 但没生效」）。这一条钉的是「摆出来的档数 == `bili.CommentSorts()` 的档数」。
func TestTheSortHasOnlyTwoTiers(t *testing.T) {
	if got, want := len(commentsSortLabels), len(bili.CommentSorts()); got != want {
		t.Errorf("界面上摆了 %d 档排序，而该摆的是 %d 档（`bili.CommentSorts()`）",
			got, want)
	}
	for _, label := range commentsSortLabels {
		if label == "评论" {
			t.Errorf("排序里有一档叫「评论」——它给的是「最新评论」的同一份内容" +
				"（票 #591 的实测），摆出来用户会以为切了档却没生效")
		}
	}
}

// TestSwitchingTheSortStartsFromTheFirstCommentAgain 断**换档之后第一屏重新
// 从第一条开始**。
//
// ⚠️ **判据是「新的那一档的第一条在画面上，而旧那一档的第一条不在」**：
// 只断「新的第一条在」的话，一个「换档之后不清空、旧的留在上面」的实现照样
// 能过（两档的第一条可能不同，但旧的那些还堆在上面）。
func TestSwitchingTheSortStartsFromTheFirstCommentAgain(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	if !tt.HasText("大圆镜科普") {
		t.Fatalf("最热那一档的第一条不在画面上，文字有 %q", tt.Texts())
	}

	if err := tt.Click("最新"); err != nil {
		t.Fatalf("点不动「最新」那一档：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "换到最新那一档了", func() bool {
		return tt.HasText("U咩瓦帕瓦U咩瓦帕瓦")
	})

	// ⚠️ **换档之后发出去的那一趟要带 mode=2**。
	if mode := b.lastMainCall().Get("mode"); mode != "2" {
		t.Errorf("换档之后发出去的那一趟带的是 mode=%q，而它该是 2（最新评论）", mode)
	}
	// 旧那一档的第一条不在画面上了。
	if tt.HasText("大圆镜科普") {
		t.Errorf("换档之后旧那一档的第一条还在画面上——第一屏没有重新从第一条开始")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 翻页
// ─────────────────────────────────────────────────────────────────────────────

// TestTheListLoadsMoreWhenItReachesTheEnd 断**滚到底自动续拉**。
//
// ⚠️ **判据是「主列表被问了两回」**：第一页回来的那一档 `cursor.is_end` 是
// false（还有下一页），而末尾那一行进画面了就会续拉。
func TestTheListLoadsMoreWhenItReachesTheEnd(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	commentsWait(t, tt, "第二页拉回来了", func() bool {
		return b.mainCallCount() >= 2
	})

	// ⚠️ **第二页那一趟必须带着游标**：不带的话 B 站回的是第一页，而界面上
	// 看起来「翻页了但内容没变」。
	if ps := b.lastMainCall().Get("pagination_str"); ps == "" {
		t.Errorf("第二页那一趟没有带游标——B 站会回第一页，而界面上看不出来")
	}
}

// TestTheLoadingRowSaysItIsLoading 断**拉的时候有「正在拉」的样子**。
//
// 🔴 **那一段只有在这一趟还在路上的时候才看得见**：所以这一条把第二页那一趟
// **卡住**（闸门），等界面上出现「正在拉」再放行。
func TestTheLoadingRowSaysItIsLoading(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	hold := make(chan struct{})
	b.mu.Lock()
	b.hold = hold
	b.mu.Unlock()
	// ⚠️ **收尾一定要放行**：那台假 B 站的 `Close` 会等在途请求结束，而
	// 那一趟卡在闸门上——不放行的话**收尾就挂住了**（而挂住的不是产品，
	// 是测试自己）。
	t.Cleanup(func() {
		select {
		case <-hold:
		default:
			close(hold)
		}
	})

	_, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	commentsWait(t, tt, "末尾那一行在拉了", func() bool {
		return tt.HasText("正在拉")
	})
	// 放行：那一趟回来之后闸门就开了（闸门是关着的，测试自己收尾时放开）。
	close(hold)
}

// ─────────────────────────────────────────────────────────────────────────────
// 入口
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCommentsKeyIsNotShownForALocalFile 断**本地文件不摆入口**。
//
// 🔴 **不是摆一个点不动的键**：本地音乐在 B 站上没有对应的对象，而问一个
// 认不出来的 oid 的症状是「静默拿到空列表」——于是界面上会摆一个永远空着的
// 评论页。给一个按了没用的键比不给更糟。
func TestTheCommentsKeyIsNotShownForALocalFile(t *testing.T) {
	it := app.Item{Source: "local", Type: "local", Path: "/tmp/x.mp3", Title: "本地的一首"}
	tt, _ := commentsPlaybarTester(t, it, 1200)
	if _, ok := tt.Find("评论"); ok {
		t.Errorf("本地文件那一档播放条上摆了「评论」键——它点了什么也不会发生")
	}
}

// TestTheCommentsKeyIsShownForAnOnlineTrack 断**在线条目摆入口**。
func TestTheCommentsKeyIsShownForAnOnlineTrack(t *testing.T) {
	tt, _ := commentsPlaybarTester(t, audioItem(), 1200)
	if _, ok := tt.Find("评论"); !ok {
		t.Errorf("播放条上找不到「评论」键，文字有 %q", tt.Texts())
	}
}

// TestTheCommentsKeyDoesNotTouchTheProgressBar 断**多出来的那一枚键不碰进度行**。
//
// 🔴 **这是票 #573 在播放条那一档的账**：左边那一列的保底宽从 240 涨到 272
// （多了一枚 32 的键），窄窗口下它**撑出轨道**的那一截也长了 32——而那一截
// 离进度行还有多远，是**量出来的**，不是算出来的。
func TestTheCommentsKeyDoesNotTouchTheProgressBar(t *testing.T) {
	for _, width := range []int{900, 1000, 1200} {
		tt, _ := commentsPlaybarTester(t, audioItem(), width)
		key, ok := tt.Find("评论")
		if !ok {
			t.Fatalf("宽度 %d：播放条上找不到「评论」键，文字有 %q", width, tt.Texts())
		}
		bar, ok := tt.Find("播放进度")
		if !ok {
			t.Fatalf("宽度 %d：画面上找不到进度条，文字有 %q", width, tt.Texts())
		}
		if key.X+key.W > bar.X {
			t.Errorf("宽度 %d：「评论」键的右沿在 %.1f，而进度行从 %.1f 开始——"+
				"它们碰上了", width, key.X+key.W, bar.X)
		}

		// 🔴 **悬停要出那句话**：命中链的第一个环是悬停（点击更难断）——
		// 左列撑出轨道的那一截伸进中间那一列，而中间那一列是 `PassThrough`
		// 的（它自己那一个框不接指针）；可是中间那一列的**子节点**照旧登记，
		// 于是「读数那一行」那一块有可能把这一枚键盖住。
		//
		// ⚠️ **悬停点是键的正中心**（图形在那里，18 × 18，居中在 32 的盒子里），
		// 而不是盒子的角上——角上那一小块和读数那一行是挨着的（900 宽下：键的
		// 盒子 y=22..54，读数 y=48.8..65.2），而**图形和读数上下不挨**
		//（图形 y=28..46）。
		tt.Move(key.X+key.W/2, key.Y+key.H/2)
		tt.Frame()
		if !tt.HasText("看这一首的评论") {
			t.Errorf("宽度 %d：指针停在「评论」键的正中心上（%.1f, %.1f）却没有那句话——"+
				"它被进度那一行的命中框盖住了（点下去也就没反应，而界面上看不出坏了）",
				width, key.X+key.W/2, key.Y+key.H/2)
		}
	}
}

// TestTheFullPlayerHasACommentsKey 断**满屏播放器里也有那一枚键**。
//
// 🔴 **满屏里必须有**：满屏开着的时候主窗口整个被盖住了（`viewFullPlayer`
// 底下那一整层一个像素都看不见），于是**播放条上那一枚在满屏里点不到**——
// 满屏里那一枚是唯一的入口。
func TestTheFullPlayerHasACommentsKey(t *testing.T) {
	tt, _ := commentsFullTester(t, audioItem())
	if _, ok := tt.Find("评论"); !ok {
		t.Errorf("满屏播放器里找不到「评论」键，文字有 %q", tt.Texts())
	}

	local := app.Item{Source: "local", Type: "local", Path: "/tmp/x.mp3", Title: "本地的一首"}
	tt2, _ := commentsFullTester(t, local)
	if _, ok := tt2.Find("评论"); ok {
		t.Errorf("本地文件那一档满屏里摆了「评论」键——它点了什么也不会发生")
	}
}

// commentsFullTester 只画满屏播放器，而队列里摆着给定的那一首。
func commentsFullTester(t *testing.T, it app.Item, opts ...shellOpt) (*mygoui.Tester, *Shell) {
	t.Helper()
	st := barState()
	st.Title = it.Title
	st.Artist = it.Artist
	all := append([]shellOpt{at(1200, 800), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.full.open = true
			s.viewFullPlayer(c, st)
		}
	})}, opts...)
	ts := newTestShell(t, all...)
	ts.s.app.Queue().Reset(it)
	ts.tt.Frame()
	return ts.tt, ts.s
}

// commentsPlaybarTester 只画播放条，而队列里摆着给定的那一首。
//
// ⚠️ **播放条那一行的状态是手给的**（`HasTrack: true`），而队列里那一首是
// **真的摆进去的**：两样都要，因为「那一枚键摆不摆」看的是**两个**判据——
// `st.HasTrack`（有没有歌在放）和 `s.app.CommentsWorthAsking()`（当前这一首
// 在 B 站上有没有对应的对象）。
//
// ⚠️ **为什么状态要手给**：`app.State.HasTrack` 是「播放器在不在」
// （`a.player != nil`），而这一族测试不真的取流——于是那一格恒是假，
// 而播放条上**所有**跟「当前这一首」有关的键都不摆（分集 / 收藏 / 赞 / 评论）。
// 播放条那一族的老测试也是手给状态的（`barState()`），这里是同一路。
func commentsPlaybarTester(t *testing.T, it app.Item, w int, opts ...shellOpt) (*mygoui.Tester, *Shell) {
	t.Helper()
	st := barState()
	st.Title = it.Title
	st.Artist = it.Artist
	all := append([]shellOpt{at(w, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.viewPlayBar(c, st)
		}
	})}, opts...)
	ts := newTestShell(t, all...)
	ts.s.app.Queue().Reset(it)
	ts.tt.Frame()
	return ts.tt, ts.s
}
