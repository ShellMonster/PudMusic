package ui

// 搜索框热词占位这一族。全部走假时钟（`withClock`）和本地 fixture，不碰真的
// music.163.com，也不睡墙钟（AGENTS.md §2：动画/定时用测试时钟和条件等待）。
//
// # 判据分两层
//
// 占位词画在框架输入框的内部（`editor.placeholder`），无头 Tester 的
// `Find`/`Texts` 看不见它——那是 a11y 的 Label，不是文字节点。所以：
//
//   - **「该显示哪个词」**读 `s.hotwordPlaceholder()`（纯函数，逐字断言）；
//   - **「真的画上去了」**比对框里那块像素：和无词时的「搜索」基线比、和上一
//     帧比。状态里记着、画面上没有的那种坏，靠后一层抓（基座文件头那句的教训）。

import (
	"image"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/hotwords"
	mygoui "github.com/egoist/mygo/ui"
)

// hotWordWords 是测试词池。三个词两两字面完全不同，像素比对分得开。
var hotWordWords = []string{"季候风", "我不难过", "一张相片"}

// withHotWordWords 给 App 摆一个现成的词池（不走网络）。
// ⚠️ 走 `mounted` 而不是 `stubbed`：`withClock` 已经占了 stub 那一格，两个
// `stubbed` 后来的会盖掉先来的，假时钟就丢了。
func withHotWordWords() shellOpt {
	return mounted(func(_ *testing.T, a *app.App) {
		a.HotwordsOf().TestOnlySetWords(hotWordWords)
	})
}

// hotWordNeteaseFixture 起一台回固定热搜词的假网易云。
func hotWordNeteaseFixture(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withHotWordClient 挂一个指着 fixture 的热词客户端（走真的拉取路）。
func withHotWordClient(t *testing.T, srv *httptest.Server) shellOpt {
	return mounted(func(_ *testing.T, a *app.App) {
		c := hotwords.New(nil)
		c.TestOnlyPointHostsAtFixture(srv.URL)
		a.MountHotwords(c)
	})
}

// hotWordGate 是一台**先卡住再放行**的假网易云：基线（「搜索」那一帧）要在
// 词回来之前拍下来，不受后台那一趟快慢的影响。
type hotWordGate struct {
	*httptest.Server
	mu   sync.Mutex
	body string
	open chan struct{}
}

func newHotWordGate(t *testing.T, body string) *hotWordGate {
	t.Helper()
	g := &hotWordGate{body: body, open: make(chan struct{})}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-g.open
		g.mu.Lock()
		body := g.body
		g.mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *hotWordGate) release() { close(g.open) }

// hotWordCrop 抠出搜索框输入区那一块（往里缩 2 像素，避开描边和底色边沿）。
func hotWordCrop(t *testing.T, tt *mygoui.Tester) *image.RGBA {
	t.Helper()
	r, ok := tt.Find(searchLabel)
	if !ok {
		t.Fatalf("顶栏上没有搜索框，文字有 %q", tt.Texts())
	}
	const inset = 2
	x1, y1 := int(r.X)+inset, int(r.Y)+inset
	x2, y2 := int(r.X+r.W)-inset, int(r.Y+r.H)-inset
	return tt.Image().SubImage(image.Rect(x1, y1, x2, y2)).(*image.RGBA)
}

// hotWordSame 报两块抠图是不是逐像素一样；不一样时报出第一处差异的位置。
func hotWordSame(t *testing.T, what string, a, b *image.RGBA) bool {
	t.Helper()
	if a.Bounds() != b.Bounds() {
		t.Fatalf("%s：两次抠图的范围不一样（%v vs %v）", what, a.Bounds(), b.Bounds())
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				t.Logf("%s：第一处差异在 (%d,%d)：%v vs %v",
					what, x, y, a.RGBAAt(x, y), b.RGBAAt(x, y))
				return false
			}
		}
	}
	return true
}

// hotWordShell 造「挂着词池 + 假时钟」的外壳，指针挪开搜索框（别让悬停抬底色）。
func hotWordShell(t *testing.T, opts ...shellOpt) (*Shell, *mygoui.Tester, *fakeClock) {
	t.Helper()
	fc := newFakeClock(time.Unix(1700000000, 0))
	opts = append([]shellOpt{withClock(fc)}, opts...)
	s, tt := newShell(t, opts...)
	tt.Move(900, 500)
	return s, tt, fc
}

// TestTheEmptySearchBoxRotatesHotWords 钉轮换本身：第一个词先显示满一档，到点
// 换下一个，走完一圈回环——回环那一帧和第一帧逐像素一样（同一个词，画出来就该
// 是同一块像素）。
func TestTheEmptySearchBoxRotatesHotWords(t *testing.T) {
	s, tt, fc := hotWordShell(t, withHotWordWords())
	tt.Frame()

	if got := s.hotwordPlaceholder(); got != hotWordWords[0] {
		t.Fatalf("占位词 = %q，want %q", got, hotWordWords[0])
	}
	if s.hotword.stop == nil {
		t.Fatal("词池在手、框是空的，轮换的定时器却没装上")
	}
	first := hotWordCrop(t, tt)

	// 没到点：不换，画面一个像素都不动。
	fc.Advance(searchHotWordRotate - time.Second)
	tt.Frame()
	if got := s.hotwordPlaceholder(); got != hotWordWords[0] {
		t.Errorf("没到点就换了词：%q", got)
	}
	if !hotWordSame(t, "没到点那帧", first, hotWordCrop(t, tt)) {
		t.Error("没到点，占位词那块像素却动了")
	}

	// 到点：换下一个。
	fc.Advance(time.Second)
	tt.Frame()
	if got := s.hotwordPlaceholder(); got != hotWordWords[1] {
		t.Fatalf("到点没换：占位词 = %q，want %q", got, hotWordWords[1])
	}
	if hotWordSame(t, "到点那帧", first, hotWordCrop(t, tt)) {
		t.Error("状态里换了词，画面上那块像素却还是旧的")
	}

	// 走完一圈回到第一个词，像素和第一帧完全一样。
	fc.Advance(searchHotWordRotate)
	tt.Frame()
	if got := s.hotwordPlaceholder(); got != hotWordWords[2] {
		t.Fatalf("第三个词 = %q，want %q", got, hotWordWords[2])
	}
	fc.Advance(searchHotWordRotate)
	tt.Frame()
	if got := s.hotwordPlaceholder(); got != hotWordWords[0] {
		t.Fatalf("走完一圈没回环：%q，want %q", got, hotWordWords[0])
	}
	if !hotWordSame(t, "回环那帧", first, hotWordCrop(t, tt)) {
		t.Error("回环显示的是同一个词，像素却和第一帧不一样")
	}
}

// TestHotWordRotationPausesWhileFocused 钉聚焦暂停：框里拿着焦点（正要打字）时，
// 到点也不换、定时器收掉。失焦后恢复走的和「词池刚到时装表」是同一支
// （TestTheEmptySearchBoxRotatesHotWords 钉过），不在这里重复一遍——那条路要
// 先 Esc 收浮层再真把焦点递出去，测的架子比被测的东西还多。
//
// ⚠️ 这一条**不做像素比对**：点进框底色本身就抬一档（20%→30%，票 #453 那一族），
// 抠图整块都在变，分不出「词没换」和「底色变了」。画上去的证明在
// TestTheEmptySearchBoxRotatesHotWords 里（同一个画法），这里钉的是状态和定时器。
func TestHotWordRotationPausesWhileFocused(t *testing.T) {
	s, tt, fc := hotWordShell(t, withHotWordWords())
	tt.Frame()

	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不进搜索框：%v", err)
	}
	tt.Frame()
	if !s.searchFocused {
		t.Fatal("点了搜索框，焦点却不在它身上——这一条的前提要先成立")
	}

	fc.Advance(3 * searchHotWordRotate)
	tt.Frame()
	if got := s.hotwordPlaceholder(); got != hotWordWords[0] {
		t.Errorf("聚焦时还在换词：%q，want %q", got, hotWordWords[0])
	}
	if s.hotword.stop != nil {
		t.Error("聚焦时轮换的定时器还醒着——为一个不换的东西白醒")
	}
}

// TestTypingStopsTheHotWordRotation 钉打字那一档：框里有字，占位词让位、
// 定时器收掉。把字删干净之后词池照旧可用（Refresh 的闸在业务层，框空了自然续上）。
func TestTypingStopsTheHotWordRotation(t *testing.T) {
	s, tt, _ := hotWordShell(t, withHotWordWords())
	tt.Frame()
	if s.hotword.stop == nil {
		t.Fatal("起点就不对：定时器没装上")
	}

	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不进搜索框：%v", err)
	}
	tt.Type("周")
	tt.Frame()

	if got := s.hotwordPlaceholder(); got != searchLabel {
		t.Errorf("打了字占位词还是热词：%q，want %q", got, searchLabel)
	}
	if s.hotword.stop != nil {
		t.Error("打了字轮换的定时器还醒着")
	}

	// 删干净（全选删不掉的话退一格也够——这里就一个字）。
	tt.TypeKey(0, mygoui.KeyBackspace, "")
	tt.Frame()
	if got := s.hotwordPlaceholder(); got != hotWordWords[0] {
		t.Errorf("删干净之后占位词没回到热词：%q，want %q", got, hotWordWords[0])
	}
}

// TestTheHotWordPlaceholderFallsBackToSearch 钉没词的那一支：没挂上、或挂了
// 还没拉到，占位词就是「搜索」，定时器不装。
func TestTheHotWordPlaceholderFallsBackToSearch(t *testing.T) {
	s, tt, _ := hotWordShell(t)
	tt.Frame()
	if got := s.hotwordPlaceholder(); got != searchLabel {
		t.Errorf("没词池时占位词 = %q，want %q", got, searchLabel)
	}
	if s.hotword.stop != nil {
		t.Error("没词池却装了轮换的定时器")
	}
}

// TestTheSearchBoxPicksUpNetEaseHotWords 端到端走一遍真的拉取路：view 问
// `Refresh` → 业务层后台拉 → fixture 回词 → wake → 下一帧占位词换成第一个词。
// 基线（「搜索」）在放行 fixture 之前拍，不受后台那一趟快慢的影响。
func TestTheSearchBoxPicksUpNetEaseHotWords(t *testing.T) {
	body := `{"code":200,"result":{"hots":[{"first":"季候风"},{"first":"我不难过"}]}}`
	g := newHotWordGate(t, body)
	s, tt, fc := hotWordShell(t, withHotWordClient(t, g.Server))
	tt.Frame()
	baseline := hotWordCrop(t, tt)

	g.release()
	var first *image.RGBA
	waitFrames(t, tt, "网易云的词上屏", func() bool {
		if len(s.app.HotwordsOf().Words()) == 0 {
			return false
		}
		if got := s.hotwordPlaceholder(); got != "季候风" {
			t.Fatalf("词回来了但占位词 = %q，want 季候风", got)
		}
		first = hotWordCrop(t, tt)
		return !hotWordSame(t, "上屏那帧", baseline, first)
	})

	// 端到端之后轮换照样走：到点换成第二个词。
	fc.Advance(searchHotWordRotate)
	tt.Frame()
	if got := s.hotwordPlaceholder(); got != "我不难过" {
		t.Errorf("端到端之后到点没换：%q，want 我不难过", got)
	}
}

// TestAFailingNetEaseStaysSilentAndCheap 钉异常那一条：网易云一直挂的时候，
// 占位词停在「搜索」，界面不出任何声；而且**退避**挡住了重试风暴——推了
// 五十次轮换的钟、画了几十帧，fixture 也只被叫过一次（失败记了时刻，退避期内
// 业务层根本不发）。
//
// ⚠️ **1 分钟退避的边界本身钉不住**：业务层的退避用的是墙钟（`time.Since`，
// 和热榜歌名 `RefreshHotSongs` 同一形状），假时钟推不动它——这条测试证的是
// 「不会每帧/每档重发」，「恰好一分钟后再问」没有回归覆盖，是已知盲区。
func TestAFailingNetEaseStaysSilentAndCheap(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	s, tt, fc := hotWordShell(t, withHotWordClient(t, srv))
	waitFrames(t, tt, "第一趟失败落定", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls > 0
	})
	if got := s.hotwordPlaceholder(); got != searchLabel {
		t.Errorf("拉不到时占位词 = %q，want %q", got, searchLabel)
	}

	for range 50 {
		fc.Advance(searchHotWordRotate)
		tt.Frame()
	}
	if got := s.hotwordPlaceholder(); got != searchLabel {
		t.Errorf("推了五十档之后占位词 = %q，want 还是 %q", got, searchLabel)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("fixture 被叫了 %d 次，want 1（退避期内不许重试风暴）", calls)
	}
}

// TestTheFullPlayerStopsTheHotWordTimer 钉满屏那一档：满屏一开，顶栏不画，
// 轮换的表必须收掉——不然它每 5 秒叫醒窗口画一帧没人看的满屏。关掉满屏回来，
// 表照常装回去。
func TestTheFullPlayerStopsTheHotWordTimer(t *testing.T) {
	s, tt, _ := hotWordShell(t, withHotWordWords())
	tt.Frame()
	if s.hotword.stop == nil {
		t.Fatal("起点就不对：定时器没装上")
	}

	s.full.open = true
	tt.Frame()
	if s.hotword.stop != nil {
		t.Error("满屏开着，轮换的定时器还醒着")
	}

	s.full.open = false
	tt.Frame()
	if s.hotword.stop == nil {
		t.Error("退出满屏回到首页，轮换的定时器没装回去")
	}
}

// TestACrashedShellStopsTheHotWordTimer 钉崩溃页那一档：页面 panic 之后整窗走
// viewCrash，`syncHotwords` 到不了——表要在画崩溃页的那一帧就收掉，不然它每
// 5 秒叫醒一次画一帧没人看的错误页。
func TestACrashedShellStopsTheHotWordTimer(t *testing.T) {
	s, tt, _ := hotWordShell(t, withHotWordWords())
	tt.Frame()
	if s.hotword.stop == nil {
		t.Fatal("起点就不对：定时器没装上")
	}

	s.crash = s.newCrashInfo("侧栏", "坏掉了")
	tt.Frame()
	if s.hotword.stop != nil {
		t.Error("崩了之后轮换的定时器还醒着")
	}
}
