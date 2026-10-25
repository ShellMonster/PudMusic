package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// 这些测试跑在 MyGo 的 headless Tester 上：真主题、真画一帧、真读像素。
//
// 断言面是**渲染结果**（画面上有什么字、二维码区域是不是白底），
// 不是「调了哪个函数」——`SPEC.md` 的 Testing Decisions 就是这么定的：
// UI 测试断言渲染结果，不是实现细节。

// ─────────────────────────────────────────────────────────────────────────────
// 一台假 B 站
// ─────────────────────────────────────────────────────────────────────────────

// uiBili 是一台给界面测试用的假 B 站。
type uiBili struct {
	*httptest.Server
	addr string
	// mu 护住 scans：测试 goroutine 会往里塞下一次要回什么，
	// 服务器 goroutine 同时在读。没有锁的话 `-race` 会报，
	// 而偶发的那次「读到半个 slice」会让测试莫名其妙地卡住。
	mu sync.Mutex
	// scans 是轮询依次回的状态码，用完就转到 sticky。
	scans []int
	// sticky 是 scans 取空之后**一直**回的状态码。
	//
	// ⚠️ 这一条是为了治一个 flaky：早一版队列取空就固定回 86101，
	// 于是「已扫待确认」这个状态只存在一次轮询（测试里是 2 ms），
	// 采样点稍有延迟就错过——整包跑的时候十轮红三轮，单独跑 6/6 过。
	//
	// 真 B 站在用户点确认之前**就一直**回 86090，所以 sticky 就是它的
	// 真实形态：要让「已扫待确认」成为一个能站着被断言的状态，
	// 服务器就得一直这么说。
	sticky int
}

// setScans 设好接下来几次轮询依次回什么，用完之后回 sticky。
func (b *uiBili) setScans(sticky int, codes ...int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sticky, b.scans = sticky, codes
}

func (b *uiBili) nextScan() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.scans) == 0 {
		return b.sticky
	}
	code := b.scans[0]
	b.scans = b.scans[1:]
	return code
}

// newUIBili 建一台假 B 站。
//
// **只有「成功」和「过期」能靠 scans 走完**：那两个是终态，轮询循环
// 收到之后自己就停了。「等扫码」和「已扫待确认」必须靠 sticky 站住，
// 理由见 `sticky` 那段注释。
func newUIBili(t *testing.T, sticky int, scans ...int) *uiBili {
	t.Helper()
	b := &uiBili{sticky: sticky, scans: scans}
	b.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/passport-login/web/qrcode/generate":
			// 申请二维码那次会顺带下一个 buvid —— **不是**登录凭据，
			// 留着是为了验「不是登录凭据的 cookie 不进登录态」。
			http.SetCookie(w, &http.Cookie{Name: "buvid", Value: "not-a-credential", Path: "/"})
			writeUIJSON(t, w, 0, "0", map[string]any{
				"url":        "https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=fixture",
				"qrcode_key": "fixture-qrcode-key-000000000000000",
			})
		case "/x/passport-login/web/qrcode/poll":
			code := b.nextScan()
			if code == 0 {
				// 真 B 站是在**同一个响应**里把 SESSDATA 和 bili_jct 一起
				// 下发的，这里照抄那个形态。
				http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess-from-ui-fixture", Path: "/"})
				http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "jct-from-ui-fixture", Path: "/"})
			}
			writeUIJSON(t, w, 0, "0", map[string]any{
				"code": code, "message": "fixture",
				"refresh_token": "rt-ui", "timestamp": time.Now().UnixMilli(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.Close)
	b.addr = strings.TrimPrefix(b.URL, "https://")
	return b
}

// hostOfFixture 是 fixture 地址里的主机名。
func hostOfFixture(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func writeUIJSON(t *testing.T, w http.ResponseWriter, code int, msg string, data any) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"code": code, "message": msg, "ttl": 1, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// newLoginApp 装一个指向假 B 站的 App。
func newLoginApp(t *testing.T, b *uiBili) *app.App {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)

	transport := &app.CredentialTransport{Base: b.Server.Client().Transport, Policy: app.DefaultInjectPolicy()}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	a.MountLogin(app.NewMemoryStore("test"), cli)
	a.SetLoginPollInterval(2 * time.Millisecond)
	return a
}

// loginTester 是一个停在登录页的 Tester。
func loginTester(t *testing.T, a *app.App) (*mygoui.Tester, *Shell) {
	t.Helper()
	// 这一支是「停在登录页」：造 Tester 之前先 Push 好路由，于是 Tester 的第一帧
	// 画的就是登录页，不用先画整窗再切过去。
	b := newTestShell(t, onApp(a), at(900, 600), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			ApplyTheme(c, app.DefaultSettings(""), c.Theme().Dark)
			s.router.Push("/login")
			s.View(c)

		}
	}))
	return b.tt, b.s
}

// waitQRCode 等二维码画出来。
func waitQRCode(t *testing.T, a *app.App) {
	t.Helper()
	waitFor(t, "二维码出来", func() bool { return a.LoginState().QR != nil })
}

// waitLogin 等登录状态变成想要的那个。
func waitLogin(t *testing.T, a *app.App, want app.LoginPhase) {
	t.Helper()
	waitFor(t, fmt.Sprintf("登录相位到 %v", want), func() bool { return a.LoginState().Phase == want })
}

// ─────────────────────────────────────────────────────────────────────────────
// 三种状态在屏幕上长得不一样
// ─────────────────────────────────────────────────────────────────────────────

// TestLoginPageShowsTheThreeStatesEachWithItsOwnWords 是这组的主线。
//
// 三态少一态的代价是用户卡在转圈里，或者扫完了还被告知「请扫码」。
// 这里逐个状态画一帧，断言画面上**真的有那句话**。
func TestLoginPageShowsTheThreeStatesEachWithItsOwnWords(t *testing.T) {
	cases := []struct {
		name   string
		sticky int
		scans  []int
		want   app.LoginPhase
		say    string
		absent string
	}{
		{
			// 服务器一直说「还没扫」——用户没举手机之前就是这个形态。
			name:   "等扫码",
			sticky: 86101,
			want:   app.LoginWaiting,
			say:    "请用 bilibili 手机客户端扫码",
			absent: "已扫码",
		},
		{
			// 服务器一直说「扫了没确认」——用户在手机上还没点确认之前
			// 就是这个形态，也正是最容易让用户以为「没扫成功」的那一段。
			name:   "已扫待确认",
			sticky: 86090,
			want:   app.LoginScanned,
			say:    "已扫码，请在手机上确认",
			absent: "请用 bilibili 手机客户端扫码",
		},
		{
			// 过期是终态，循环自己会停，队列给一次就够。
			name:   "二维码失效",
			sticky: 86038,
			scans:  []int{86038},
			want:   app.LoginExpired,
			say:    "过期",
			absent: "请用 bilibili 手机客户端扫码",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newUIBili(t, c.sticky, c.scans...)
			a := newLoginApp(t, b)
			tt, _ := loginTester(t, a)
			tt.Frame()

			a.BeginLogin()
			// 先等二维码真的画出来了。
			//
			// ⚠️ 不能直接等 Phase：`beginLogin` 一上来就把 Phase 设成
			// LoginWaiting（提示是「正在向 B 站申请二维码…」），
			// 只等 Phase 的话会在二维码到位**之前**就取样，
			// 拿到的提示自然是错的。这种测试失败是 flaky 的经典形态。
			waitQRCode(t, a)
			waitLogin(t, a, c.want)
			tt.Frame()

			words := screenText(tt)
			if !strings.Contains(words, c.say) {
				t.Errorf("画面上没有 %q。\n画面上的字是：\n%s", c.say, words)
			}
			if c.absent != "" && strings.Contains(words, c.absent) {
				t.Errorf("画面上出现了不该有的 %q。\n画面上的字是：\n%s", c.absent, words)
			}
		})
	}
}

// TestLoginPageOffersRefreshOnlyWhenExpired 验按钮跟着状态换。
//
// 「刷新」在二维码还有效时摆着（哪怕灰掉）只会让用户以为扫码失败了。
func TestLoginPageOffersRefreshOnlyWhenExpired(t *testing.T) {
	b := newUIBili(t, 86101)
	a := newLoginApp(t, b)
	tt, _ := loginTester(t, a)
	tt.Frame()

	a.BeginLogin()
	waitQRCode(t, a)
	tt.Frame()
	if strings.Contains(screenText(tt), "刷新二维码") {
		t.Error("二维码还有效时就摆出了「刷新二维码」")
	}

	b2 := newUIBili(t, 86038, 86038)
	a2 := newLoginApp(t, b2)
	tt2, _ := loginTester(t, a2)
	tt2.Frame()
	a2.BeginLogin()
	waitQRCode(t, a2)
	waitLogin(t, a2, app.LoginExpired)
	tt2.Frame()

	words := screenText(tt2)
	if !strings.Contains(words, "刷新二维码") {
		t.Errorf("过期之后没有「刷新二维码」按钮。\n画面上的字是：\n%s", words)
	}
	if !strings.Contains(words, "已失效") && !strings.Contains(words, "过期") {
		t.Errorf("过期之后没把原因说出来。\n画面上的字是：\n%s", words)
	}
}

// TestExpiredQRCodeCanBeRefreshedFromTheUI 验刷新真的换了新码。
func TestExpiredQRCodeCanBeRefreshedFromTheUI(t *testing.T) {
	b := newUIBili(t, 86101, 86038)
	a := newLoginApp(t, b)
	tt, s := loginTester(t, a)
	tt.Frame()

	a.BeginLogin()
	waitQRCode(t, a)
	waitLogin(t, a, app.LoginExpired)
	tt.Frame()
	old := a.LoginState().QR

	if err := tt.Click("刷新二维码"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	waitLogin(t, a, app.LoginWaiting)

	if a.LoginState().QR == old {
		t.Error("点了刷新，二维码还是原来那一张")
	}
	_ = s
}

// TestCancelStopsTheQRCodeFromTheUI 验取消。
func TestCancelStopsTheQRCodeFromTheUI(t *testing.T) {
	b := newUIBili(t, 86101)
	a := newLoginApp(t, b)
	tt, _ := loginTester(t, a)
	tt.Frame()

	a.BeginLogin()
	waitQRCode(t, a)
	tt.Frame()

	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if p := a.LoginState().Phase; p != app.LoginIdle {
		t.Errorf("点了取消之后状态是 %v，want LoginIdle", p)
	}
	if words := screenText(tt); !strings.Contains(words, "开始扫码登录") {
		t.Errorf("取消之后没有回到「开始扫码登录」。\n画面上的字是：\n%s", words)
	}
}

// TestLoginSuccessShowsLoggedInAndLogout 验成功后有退登入口。
//
// 退登是验收项，藏进设置页等于没有。
func TestLoginSuccessShowsLoggedInAndLogout(t *testing.T) {
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, _ := loginTester(t, a)
	tt.Frame()

	a.BeginLogin()
	waitLogin(t, a, app.LoginDone)
	tt.Frame()

	words := screenText(tt)
	if !strings.Contains(words, "已登录") {
		t.Errorf("登录成功之后没显示已登录。\n画面上的字是：\n%s", words)
	}
	if !strings.Contains(words, "退出登录") {
		t.Errorf("登录成功之后没有退登按钮。\n画面上的字是：\n%s", words)
	}
	if strings.Contains(words, "SESSDATA") || strings.Contains(words, "sess-from-ui-fixture") {
		t.Errorf("画面上出现了凭据：\n%s", words)
	}
}

// TestLogoutFromTheUILetsYouLogInAgain 验退登之后能重新登。
func TestLogoutFromTheUILetsYouLogInAgain(t *testing.T) {
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, _ := loginTester(t, a)
	tt.Frame()

	a.BeginLogin()
	waitLogin(t, a, app.LoginDone)
	tt.Frame()

	if err := tt.Click("退出登录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if a.LoggedIn() {
		t.Error("点了退出登录之后还是登录态")
	}
	if words := screenText(tt); !strings.Contains(words, "开始扫码登录") {
		t.Errorf("退登之后没有回到「开始扫码登录」。\n画面上的字是：\n%s", words)
	}

	// 再点一次应该能重新登进去。
	if err := tt.Click("开始扫码登录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	b.setScans(0)
	waitLogin(t, a, app.LoginDone)
}

// ─────────────────────────────────────────────────────────────────────────────
// 二维码：本地渲染，到像素
// ─────────────────────────────────────────────────────────────────────────────

// TestQRCodeReachesThePixels 是「二维码本地渲染，不联网取图」这条验收
// 在界面这一侧的证据。
//
// 断言两件事：
//  1. 二维码区域**真的是白底黑块**——扫码 App 扫的是像素，不是控件。
//  2. 深色主题下**也是白底**——深底二维码有一部分 App 直接判成
//     「这不是二维码」。
func TestQRCodeReachesThePixels(t *testing.T) {
	b := newUIBili(t, 86101)
	a := newLoginApp(t, b)
	tt, sh := loginTester(t, a)
	_ = sh
	tt.SetDark(true) // 深色主题
	tt.Frame()

	a.BeginLogin()
	waitQRCode(t, a)
	tt.Frame()

	img := tt.Image()
	if img.Bounds().Dx() != 900 || img.Bounds().Dy() != 600 {
		t.Fatalf("画面是 %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}

	// **把屏幕上的像素读回模块**，跟 App 里那张二维码对一遍。
	//
	// 前两版都栽在「找那块白」上：
	//   · 数整张图的白像素包围盒 → 深色主题下文字是浅色的，包围盒是整块画面；
	//   · 数整张图的黑像素     → 深色主题下**背景**就是黑的，三十几万个，
	//                          于是误判成「二维码是深底的」。
	//
	// 现在不猜坐标，也不找包围盒：先找**最长的那条连续白线**。
	// 二维码那一块的**第一行整行都是白的**（4 个模块的留白 + 白底），
	// 全画面再没有第二样东西能有这么长的白线——文字是逐个字断开的。
	// 找到它就等于拿到了那一块的左上角和边长。
	x0, y0, side := longestWhiteRun(img, qrSide*3/4)
	if side == 0 {
		t.Fatal("整个画面上都没有一条够宽的白线——二维码那块根本没画出来")
	}
	if side < qrSide*3/4 {
		t.Errorf("最长的白线只有 %d 像素，二维码那块该有 %d 宽", side, qrSide)
	}

	// 逐模块对：屏幕上的像素该是黑就是黑。
	//
	// 这一步是真的在验「画到屏幕上的那张 = B 站给的那条地址编码出来的」，
	// 而不是「画面上有黑白像素」。尺寸算错、缩放错、错位、反色、
	// 画了一半——任何一种都会在这里露出来。
	code := a.LoginState().QR
	total := code.Size() + 2*4 // 含留白
	per := float64(side) / float64(total)
	if per != float64(int(per)) {
		t.Fatalf("一个模块占了 %.3f 个像素，不是整数——扫不出来", per)
	}

	white, dark, mismatched := 0, 0, 0
	for my := range code.Size() {
		for mx := range code.Size() {
			px := x0 + int((float64(mx)+4.5)*per)
			py := y0 + int((float64(my)+4.5)*per)
			if px < 0 || py < 0 || px >= img.Bounds().Dx() || py >= img.Bounds().Dy() {
				continue
			}
			got := img.RGBAAt(px, py)
			isWhite := got.R > 200 && got.G > 200 && got.B > 200
			if isWhite {
				white++
			} else {
				dark++
			}
			// isWhite 为真表示屏幕上**浅**，对应 code.Dark 为假——
			// 所以两者「不一致」的条件是相等，不是不等。
			if isWhite == code.Dark(mx, my) {
				mismatched++
			}
		}
	}
	if dark == 0 || white == 0 {
		t.Fatalf("二维码区域里没有黑白混合（白 %d / 黑 %d），图根本没画出来", white, dark)
	}
	// 允许 3% 的误差：模块边界上有一圈抗锯齿的灰，采中心点偶尔会
	// 采到边上那一格。真正「画错位」的时候错的是一大片，不是几个点。
	if rate := float64(mismatched) / float64(code.Size()*code.Size()); rate > 0.03 {
		t.Errorf("屏幕上有 %d/%d 个模块（%.1f%%）跟编码出来的不一致（白 %d / 黑 %d）——"+
			"缩放、错位或者反色了", mismatched, code.Size()*code.Size(),
			100*rate, white, dark)
	}

	// 深色主题下**留白也必须是白的**——深底二维码有一部分扫码 App 直接
	// 判成「这不是二维码」，而那正是长按截图分享之后最容易发生的。
	//
	// 这里验的是留白那一圈，不是「白像素比黑像素多」：
	// 一张 37×37 中档纠错的二维码，深色模块本来就接近一半，
	// 拿比例当判据会在不同版本上随机红。
	for i := range total {
		for _, p := range [][2]int{{i, 0}, {i, total - 1}, {0, i}, {total - 1, i}} {
			px := x0 + int((float64(p[0])+0.5)*per)
			py := y0 + int((float64(p[1])+0.5)*per)
			if c := img.RGBAAt(px, py); c.R < 240 || c.G < 240 || c.B < 240 {
				t.Fatalf("留白那一圈上有深色像素 (%d,%d) = %v——"+
					"二维码不是白底的，部分扫码 App 会判成「不是二维码」", p[0], p[1], c)
			}
		}
	}
}

// longestWhiteRun 是画面上最长的那条连续近白像素线：起点和长度。
func longestWhiteRun(img *image.RGBA, minLen int) (x0, y0, length int) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		start := -1
		for x := b.Min.X; x <= b.Max.X; x++ {
			white := false
			if x < b.Max.X {
				p := img.RGBAAt(x, y)
				white = p.R > 240 && p.G > 240 && p.B > 240
			}
			if white {
				if start < 0 {
					start = x
				}
				continue
			}
			if start >= 0 && x-start > length {
				x0, y0, length = start, y, x-start
			}
			start = -1
		}
	}
	if length < minLen {
		return 0, 0, 0
	}
	return x0, y0, length
}

// TestQRCodeBitmapIsReusedAcrossFrames 验二维码不会每帧重建。
//
// `ui.Bitmap` 是 GPU 纹理，每帧新建一个就是显存一路涨。
// 症状是「跑了一小时之后显存占用比刚启动时高很多」，而那种泄漏
// 只有在长跑之后才显形，所以必须在测试里钉住。
func TestQRCodeBitmapIsReusedAcrossFrames(t *testing.T) {
	b := newUIBili(t, 86101)
	a := newLoginApp(t, b)
	tt, s := loginTester(t, a)
	tt.Frame()

	a.BeginLogin()
	waitQRCode(t, a)
	tt.Frame()

	first := s.qrBitmap
	if first == nil {
		t.Fatal("二维码出来了，位图却是 nil")
	}
	for range 5 {
		tt.Frame()
		if s.qrBitmap != first {
			t.Fatalf("第几帧换了新的 Bitmap（%p → %p），显存会一直涨", first, s.qrBitmap)
		}
	}

	// 换了二维码之后应该换一张新的（缓存的键是那张二维码本身）。
	// 不换的话，界面上画的是**上一张码**——而它已经被扫过了，
	// 用户看到的是「扫了没反应」。
	s.qrFor = nil
	tt.Frame()
	if s.qrBitmap == first {
		t.Error("换了二维码之后还在用旧位图——界面上画的是上一张码")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 路由
// ─────────────────────────────────────────────────────────────────────────────

// TestLoginRouteIsNotInTheSidebar 验登录页不在侧栏菜单里。
//
// 它是从顶栏进去的。Biu 把登录挂在头像下拉里，放进侧栏会让用户
// 为了登录先翻一遍菜单。
func TestLoginRouteIsNotInTheSidebar(t *testing.T) {
	for _, r := range routes {
		if r.Match == "/login" && r.InMenu {
			t.Error("登录页被加进了侧栏菜单")
		}
	}
	if lookup("/login").Title == notFound.Title {
		t.Error("登录路由没登记")
	}
}

// TestNavHasALoginEntry 验顶栏上找得到登录的入口。
func TestNavHasALoginEntry(t *testing.T) {
	a := app.New()
	s := newTestShell(t, onApp(a)).s
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, app.DefaultSettings(""), c.Theme().Dark)
		s.View(c)
	}, 900, 600)
	tt.Frame()

	words := screenText(tt)
	if !strings.Contains(words, "登录") {
		t.Errorf("顶栏上没有登录入口。\n画面上的字是：\n%s", words)
	}
}

// newTesterFor 建一个每次都强制重画登录页的 Tester。
func newTesterFor(t *testing.T, a *app.App, s *Shell) *mygoui.Tester {
	t.Helper()
	return mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, app.DefaultSettings(""), c.Theme().Dark)
		s.router.Push("/login")
		s.View(c)
	}, 900, 600)
}

// screenText 是这一帧画面上所有的字。
//
// 断言面是**渲染结果**（`SPEC.md` 的 Testing Decisions：UI 测试断言
// 渲染结果，不是「调了哪个函数」），所以这里读的是 Tester 记录下来的
// 文本，不是状态。
func screenText(tt *mygoui.Tester) string { return strings.Join(tt.Texts(), "\n") }

// ─────────────────────────────────────────────────────────────────────────────
// 降级：没有凭据后端的时候
// ─────────────────────────────────────────────────────────────────────────────

// TestLoginPageSaysTheCredentialsCannotBeKept 验「没有后端」时的提示。
//
// 这条路是天天走的：非 macOS / Windows 平台上根本没有系统保护存储。
// 界面上必须**明说**「登录态关掉应用就没了」——用户以为凭据存住了，
// 关掉再打开发现登出，比一开始就说清楚糟糕得多。
//
// 之前这里会因为 `mgr.BackendName()` 走到空接口上的方法调用而 SIGSEGV，
// 而现在 NewManager(nil) 会换成一个「哪儿都存不住」的后端。
func TestLoginPageSaysTheCredentialsCannotBeKept(t *testing.T) {
	b := newUIBili(t, 0)
	a := app.New()
	t.Cleanup(a.Close)
	// 故意不给后端：这就是 Linux 上的真实情况。
	transport := &app.CredentialTransport{
		Base:   b.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	a.MountLogin(nil, cli)
	a.SetLoginPollInterval(2 * time.Millisecond)

	tt, _ := loginTester(t, a)
	tt.Frame()

	a.BeginLogin()
	// **等登录真的完成**再画一帧。
	//
	// 只等二维码出现是不够的：那一帧画出来的还是「等扫码」，
	// 而断言读的是**画面上的字**——于是会看到「明明登录成功了，
	// 界面上还在请扫码」。状态变了但界面还没跟上，
	// 这正是即时模式里「异步结果必须唤醒」的坑。
	waitLogin(t, a, app.LoginDone)
	tt.Frame()

	if !a.LoggedIn() {
		t.Fatal("没有后端也该能登进去（只是存不住）")
	}
	words := screenText(tt)
	if !strings.Contains(words, "凭据后端不可用") {
		t.Errorf("没有告诉用户登录态存不住。\n画面上的字是：\n%s", words)
	}
	if !strings.Contains(words, "已登录") {
		t.Errorf("没有显示已登录状态。\n画面上的字是：\n%s", words)
	}
}

// TestTheNotLoggedInScreenHasItsOwnIllustration 钉住「没登录」那一屏有插画。
//
// # 为什么这一条要紧
//
// 应用里**每一个别的空状态都有一枚图标**（本地音乐是磁盘、下载记录是下载箭头、
// 搜索是放大镜……），而「还没登录」那一支原来**光秃秃三行字**——偏偏它是用户
// 第一次打开应用最常撞到的那一屏（UX 报告 §3-F 的 F1，中高）。
//
// 判据是**像素**：图标是装饰，元素树里查不到（`mygo/ui/svg.go`：「图标是装饰，
// 辅助技术看不见它」），所以只能看标题上面那条带里有没有画上去的东西。
//
// ⚠️ **深浅两套都要断**：图标是单色的、跟着祖先的文字色走，浅色下看得清的那一
// 枚，深色下可能糊成一团。
func TestTheNotLoggedInScreenHasItsOwnIllustration(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ⚠️ **B 站客户端要挂着、但不登录**：那一页的检查顺序是「先『客户端
			// 没挂上』，再『没登录』」（见 loginprompt.go 的文件头），而没挂客户端
			// 时画的是**另一支**（空状态 + 「这一页暂时加载不出来」）——那不是
			// 这一条要验的。
			s, tt := newShell(t, systemIsDark(dark), mounted(func(_ *testing.T, a *app.App) {
				cli := bili.NewClient(bili.Options{})
				a.MountLogin(app.NewMemoryStore("test"), cli)
				a.MountExplore(cli)
			}))
			s.navigate(followPath)
			waitFrames(t, tt, "「还没登录」那一支画出来了", func() bool {
				return tt.HasText("还没登录")
			})
			if n := paintedInBand(t, tt, "还没登录"); n < 20 {
				t.Errorf("「还没登录」那一屏的插画那一带只画了 %d 个像素（want ≥ 20）——"+
					"它和别页的空状态长得不一样（别的页都有一枚图标）", n)
			}
		})
	}
}
