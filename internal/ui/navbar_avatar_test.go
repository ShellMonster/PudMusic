// 顶栏右上角那个头像的测试（issue #95）。
//
// ## 这里量的是**画面**，不是状态
//
// 规格的 Testing Decisions 写死了：界面测试断言**渲染结果**（元素存在、文字、
// 位置、图标有没有画出来），不是「调了哪个函数」。所以这一组量的是那一格 28
// 见方里到底画了什么——是别人的头像、是一个人形、还是一个出了毛病的色块。
//
// ## 验收要的那两条不许合并
//
// 「登录态显示真实头像」「未登录显示未登录的样子」在 issue #95 里是**两条**，
// 还专门加了一句「防止以后被合并成一个」。合并之后的样子是什么？未登录时也
// 显示某张图（或者登录时也显示人形）——界面上看不出坏了，**也没有任何一条
// 测试会红**。所以最后那一条 `TestTheAvatarNeverLooksTheSameTwoWays`
// 量的是「两种形态画出来确实不一样」这件事本身。
//
// ## 假 B 站是**自己**的，不借 upspace_test.go 那个
//
// 那个按完整路径派发，而头像地址是服务层拼过的（带 B 站图床的裁剪参数）。要借它
// 就得在测试里把那个参数再抄一遍——抄过来的那一份正是「断言里用了被测对象的
// 常量」那种写法：两边一起错，测试照样绿。这里改成按前缀派发，于是测试不必知道
// 服务层拼了什么。
package ui

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 一台假 B 站
// ─────────────────────────────────────────────────────────────────────────────

// avatarNavPath 是「当前登录的是谁、我的头像在哪」那个接口。
const avatarNavPath = "/x/web-interface/nav"

// avatarFacePrefix 是头像那一支的地址前缀。
//
// ⚠️ **只认前缀**：服务层会在后面拼上 B 站图床的裁剪参数，而测试不知道那是
// 什么——也不该知道（见文件头）。顺带钉住一件事：拼上去的参数是**接在文件名
// 后面**的，所以这一条认前缀而不是认文件名，那一段在不在都不影响。
const avatarFacePrefix = "/bfs/face/"

// avatarFixture 是给这一组用的假 B 站。
type avatarFixture struct {
	*httptest.Server

	mu sync.Mutex
	// nav 是 nav 接口回的 data。
	nav map[string]any
	// faceStatus 是头像那一支回的状态码。200 给一张真图，别的就是「拿不到」。
	faceStatus int
	// paths 记下每个请求的路径，用来断言「压根没发」。
	paths []string
}

func newAvatarFixture(t *testing.T) *avatarFixture {
	t.Helper()
	f := &avatarFixture{faceStatus: http.StatusOK}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		nav, status := f.nav, f.faceStatus
		f.mu.Unlock()

		switch {
		case r.URL.Path == avatarNavPath:
			avatarJSON(w, 0, nav)
		case strings.HasPrefix(r.URL.Path, avatarFacePrefix):
			if status != http.StatusOK {
				http.Error(w, "这张图拿不到", status)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(avatarFixturePNG())
		default:
			http.Error(w, "这台 fake 不提供这个接口", http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// withALoginIn 把 nav 装成「登录了，头像在这儿」。
func (f *avatarFixture) withALoginIn() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nav = map[string]any{
		"isLogin": true, "mid": 2, "uname": "我",
		"face": f.URL + avatarFacePrefix + "mine.png",
	}
}

func (f *avatarFixture) refuseTheFace() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faceStatus = http.StatusInternalServerError
}

// countHit 数打到某条路径上的请求有几回。
func (f *avatarFixture) countHit(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.paths {
		if p == path {
			n++
		}
	}
	return n
}

func avatarJSON(w http.ResponseWriter, code int, data any) {
	raw, err := json.Marshal(map[string]any{
		"code": code, "message": "0", "ttl": 1, "data": data,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// avatarFixturePNG 是这台 fake 给的头像：一整块洋红。
//
// ⚠️ **这个颜色不许出现在别的界面上**：它是「画面上画着那张头像」的判据，
// 而判据和被测物重合时「判据改成什么」都不会红。洋红在深浅两套主题里都不是
// 任何一个颜色。
func avatarFixturePNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, avatarImageColor)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// avatarImageColor 是那张假头像的颜色。判据的**对面**是「别处别出现它」。
var avatarImageColor = color.RGBA{R: 255, G: 0, B: 255, A: 255}

// ─────────────────────────────────────────────────────────────────────────────
// 造外壳
// ─────────────────────────────────────────────────────────────────────────────

// newAvatarShell 造一个指着这台 fake 的外壳。
//
// ⚠️ **只挂登录，不挂 MountExplore**：这一组要断言「未登录时一个请求都不发」，
// 而挂了探索之后动态流/首页那些也可能是往 nav 上问的（WBI 密钥就取自那儿），
// 那样这条断言量的就是「这一轮有没有别的功能在跑」，不是「头像有没有乱问」。
//
// ⚠️ **不是 `navbar_test.go` 里那个 `loggedInWith`**：那一个给的是
// `bili.NewClient(bili.Options{})`——**一个压根不通到任何地方的客户端**，
// 适合只验「菜单项接到了哪」的那一组。这一组要的是**指着这台假 B 站的**
// 客户端（头像地址就是从那儿拿的），所以两个都得有。
//
// mid 空串 = 没登录。
func newAvatarShell(t *testing.T, f *avatarFixture, mid string) (*Shell, *mygoui.Tester) {
	t.Helper()
	return newAvatarShellIn(t, f, mid, false)
}

// newAvatarShellIn 换得了系统外观那一份。
//
// 深浅两套都要跑一遍的理由：墨色和底色的**差值**是这一组判据的根，而它跟着
// 主题变。浅色下「人形 vs 字母」分得开、深色下合拢的话，那一条测试只跑浅色
// 就是绿的——而深色才是大部分人用的那一套。
func newAvatarShellIn(t *testing.T, f *avatarFixture, mid string, systemDark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, systemIsDark(systemDark), mounted(func(_ *testing.T, a *app.App) {
		transport := &app.CredentialTransport{
			Base:   f.Server.Client().Transport,
			Policy: app.DefaultInjectPolicy(),
		}
		cli := bili.NewClient(bili.Options{Transport: transport})
		cli.TestOnlyPointHostsAtFixture(f.URL)
		a.MountLogin(app.NewMemoryStore("test"), cli)
		if mid != "" {
			a.TestOnlyAdoptLogin(mid)
		}
	}))
	return b.s, b.tt
}

// ─────────────────────────────────────────────────────────────────────────────
// 量画面
// ─────────────────────────────────────────────────────────────────────────────

// avatarBox 是头像那一格的位置。没找到就是零值 + false。
func avatarBox(tt *mygoui.Tester) (mygoui.Rect, bool) { return tt.Find(avatarLabel) }

// ⚠️⚠️ **下面三个函数每一帧都会被问一次，而 `tt.Image()` 是贵的那一个**
//
// `Tester.Image` 内部走的是 `headless.image()`，它把整张画布 `RGBA()` 出来
// ——**那是复制 + 转换 1200×800 的每一个像素**。在循环里调它，一次判断就是
// 六百多次整幅转换。
//
// 实测：这么写的时候「等头像画出来」要 11 秒，而且**时好时坏**（同一个用例
// 连跑三次，两次绿一次红）——因为 waitFrames 的 10 秒保险丝有一大半被这个
// 判断本身吃掉了，而真图有时候来得及、有时候来不及。
//
// 所以下面每一个都**只取一次**画面，剩下的都从 `img` 里读。
func avatarShowsThePicture(tt *mygoui.Tester) bool {
	r, ok := avatarBox(tt)
	if !ok {
		return false
	}
	// 量的是**整格**，不是中心一个点：图片铺满整格，所以「整格有四分之一以上
	// 是那张图的颜色」才是「画的是它」。反过来「中心那一点是洋红」这种判据在
	// **画的是人形**的两档里也可能凑巧为真——那种假绿最难查。
	return countColorIn(tt.Image(), r, avatarImageColor)*4 > int(r.W*r.H)
}

// avatarFigure 量出「那一格里画出来的那个东西」的形状。
//
// 返回**最长的一段连续的墨**（几个像素宽）和**整格一共多少个墨点**。
//
// ## 为什么不能只问「不是一整块底色」
//
// 这么问过一次，钉不住任何东西——实测两种改法都照样全绿：
//
//   - 把那个人形换成恒定的字母「B」（issue #95 明令不许的那一格）：绿。
//   - 那一格**什么都不画**，连底色和那句交代都去掉：还是绿。
//
// 原因是边框 + 圆角裁剪本来就让那一格不是纯色。**问错了问题，回答对了也没有用。**
//
// ## 这两个数各自量的是什么
//
//   - **最长的连段**量的是**横向铺开多少**：人形的肩膀是 `side*0.6` 宽
//     （实测 17 px / 28），而 12 号粗体「B」最宽的那一横只有 7 px。一个字母
//     再大也铺不满一格的六成——这是「不是字母」最直接的一条。
//   - **墨点总数**量的是**画了多少**：人形是 211 点，字母是 50 点。防备的是
//     「一条又宽又细的线」那种改法——连段过了、总量不够。
//
// ## 「墨」是怎么认出来的
//
// **底色取那一格的众数颜色**（实测浅色 `{244 244 245}`、深色 `{39 39 42}`），
// 而不是去问主题的 `Surface`——那样每一套主题都要把值抄一份，而抄错的症状是
// **这一条在某个主题里永远是空的**。
//
// 「墨」是和底色**三个通道里差得最多的那个超过 40**。这个数是量出来的：
// 边框离底色只有 15（所以**边框不算墨**——它围着整格，不算的话连段永远是 28），
// 而人形是 122（浅色 `{113 113 122}`）和 122（深色 `{161 161 170}`）。
// 深浅两套主题量出来的数字**一模一样**：17/211 和 7/50。
const (
	// avatarInkDelta 是「这个点算不算墨」的界。理由见上。
	avatarInkDelta = 40
	// avatarFigureMinRun / avatarFigureMinInk 是「画出来的是人形」的界。
	//
	// 实测：人形 17 / 211，12 号粗体「B」7 / 50，**什么都不画 0 / 0**。
	// 两个数都取在正中间，两边各留 5 px / 70 点的余量。
	avatarFigureMinRun = 12
	avatarFigureMinInk = 120
)

func avatarFigure(tt *mygoui.Tester) (widestRun, inkPixels int) {
	r, ok := avatarBox(tt)
	if !ok {
		return 0, 0
	}
	img := tt.Image()
	x0, y0, x1, y1 := pixelsOf(r)

	// 底色 = 那一格里出现次数最多的那个颜色。**一趟数完**，不是每个点重新数一遍。
	hist := map[color.RGBA]int{}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			hist[img.RGBAAt(x, y)]++
		}
	}
	bg, most := color.RGBA{}, -1
	for c, n := range hist {
		if n > most {
			bg, most = c, n
		}
	}

	run := 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if isInk(img.RGBAAt(x, y), bg) {
				run++
				inkPixels++
				continue
			}
			widestRun = max(widestRun, run)
			run = 0
		}
		widestRun = max(widestRun, run)
	}
	return widestRun, inkPixels
}

// isInk 报这个点算不算「画上去的东西」。
//
// ⚠️ **三个通道的差要先各自取绝对值，再取最大的那个。** 写成「先取最大、再取
// 绝对值」的话，墨色比底色**深**的那一档（告警红 `{220 38 38}` 压在浅粉
// `{249 216 216}` 上）差值全是负数，取最大得到的是「最不明显的那个通道的差」
// ——实测那条「图没下来」的一档因此一个墨点都数不出来，而这一条是绿的。
//
// 这个坑是量出来的：未登录那一档数得出 17/211 是**碰巧**——浅色下
// `TextMuted` 比底色深、深色下比底色浅，而底色一个在 244 一个在 39，两次都
// 让三个差值同号，所以看不出来。它一换到底色偏上就翻了。
func isInk(c, bg color.RGBA) bool {
	d := max3(absDiff(c.R, bg.R), absDiff(c.G, bg.G), absDiff(c.B, bg.B))
	return d > avatarInkDelta
}

func max3(a, b, c int) int { return max(a, max(b, c)) }

// avatarHasFigure 报「那一格里画出了一个撑得开的图形」（见 avatarFigure）。
func avatarHasFigure(tt *mygoui.Tester) bool {
	run, ink := avatarFigure(tt)
	return run >= avatarFigureMinRun && ink >= avatarFigureMinInk
}

// avatarHasColor 数一数那一格里有多少个点是 want 这个颜色。
//
// 为什么要有「数」而不是「有没有」：告警色那一档和未登录那一档画的是**同一个
// 人形**，只有颜色不同；而圆边那一圈抗锯齿会让「有没有某一个点正好是纯告警色」
// 取决于它落在第几个小数像素上。数「整块脑袋里有多少个」才是稳的——纯色区域
// 十几个像素，抗锯齿的一圈两三个。
func avatarHasColor(tt *mygoui.Tester, want mygoui.Color) int {
	r, ok := avatarBox(tt)
	if !ok {
		return 0
	}
	return countColorIn(tt.Image(), r, color.RGBA{R: want.R, G: want.G, B: want.B, A: want.A})
}

func pixelsOf(r mygoui.Rect) (x0, y0, x1, y1 int) {
	x0, y0 = int(r.X), int(r.Y)
	x1, y1 = int(r.X+r.W)-1, int(r.Y+r.H)-1
	return max(x0, 0), max(y0, 0), max(x1, 0), max(y1, 0)
}

func countColorIn(img *image.RGBA, r mygoui.Rect, want color.RGBA) int {
	x0, y0, x1, y1 := pixelsOf(r)
	n := 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if img.RGBAAt(x, y) == want {
				n++
			}
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// 登录态：画的是我自己的头像
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAvatarShowsMyOwnPictureOnceLoggedIn 验登录之后那一格画的是真头像。
//
// 这是 issue #95 的第一条验收。判据是**画面上那一块洋红**——不是「调了
// `App.AccountNav`」、不是「缓存里有一张图」。后者在图片压根没画出来的时候
// 同样成立。
func TestTheAvatarShowsMyOwnPictureOnceLoggedIn(t *testing.T) {
	f := newAvatarFixture(t)
	f.withALoginIn()
	_, tt := newAvatarShell(t, f, "2")

	waitFrames(t, tt, "头像换成我自己的那张图", func() bool { return avatarShowsThePicture(tt) })

	if _, ok := tt.Find(avatarNoteGuest); ok {
		t.Error("登录了却还标着「未登录」——两种形态被合并成一个了")
	}
}

// TestTheAvatarOnlyAsksOnceEvenThoughEveryFrameAsks 验「每一帧问一次」没有变成
// 「每一帧打一次」。
//
// 顶栏是整个应用重画最勤的一处，而「问一次」这个闸门住在 view 里（`avatarSync`）。
// 写错了的样子是**画面完全正常**——头像照样出来——只是那台 B 站被问了几十次，
// 而登录之后每一条都是带凭据的请求。
//
// 所以这一条量的不是画面，是请求条数：画了 30 帧之后，问的还是**一个**。
func TestTheAvatarOnlyAsksOnceEvenThoughEveryFrameAsks(t *testing.T) {
	f := newAvatarFixture(t)
	f.withALoginIn()
	_, tt := newAvatarShell(t, f, "2")

	// 先等**那一趟真的发出去了**（造外壳时画的第一帧就发），
	// 再画 30 帧。
	//
	// ⚠️ **这里等的是请求发出，不是头像画出来**。等「画出来」的话，闸门写松
	// 之后那台 fake 会被几十个并发请求压住，判据先超时——于是报出来的是
	// 「等不到头像」而不是「问得太多」，而后者才是这一条要说的事。
	waitFor(t, "去问了 B 站一次", func() bool { return f.countHit(avatarNavPath) >= 1 })
	for range 30 {
		tt.Frame()
	}

	if n := f.countHit(avatarNavPath); n != 1 {
		t.Errorf("画了 30 帧之后那台 B 站被问了 %d 次，want 1——"+
			"闸门住在 view 里，写松了一点就变成每一帧一个带凭据的请求", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 未登录：画的是「没有人」
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAvatarSaysNobodyIsLoggedIn 验没登录时那一格是「未登录」的样子。
//
// 规格写的是「**不是字母占位符**」——原来那个恒定的「B」是应用自己的名字，
// 用户看到的是一个坏掉的占位符。
//
// 这台 fake 明确回了「登录了、头像在这儿」。所以这一条要是偷偷发了 nav，回来
// 的还是一个**看起来完全能用**的头像——于是判据必须落在「那一格没有图」上。
func TestTheAvatarSaysNobodyIsLoggedIn(t *testing.T) {
	f := newAvatarFixture(t)
	f.withALoginIn() // ⚠️ 这台 fake 随时愿意回答一个登录态
	_, tt := newAvatarShell(t, f, "")

	waitFrames(t, tt, "头图标成未登录", func() bool {
		_, ok := tt.Find(avatarNoteGuest)
		return ok
	})

	if avatarShowsThePicture(tt) {
		t.Error("没登录却把某张头像画上去了")
	}
	if n := f.countHit(avatarNavPath); n != 0 {
		t.Errorf("没登录也去问了 B 站 %d 次（请求 %v）——"+
			"顶栏每一帧问一次，凭据过期之后这就是每秒几十条 -101 噪声", n, f.paths)
	}
}

// TestTheAvatarShowsAPersonAndNotALetterWhenNobodyIsLoggedIn 钉住 issue #95
// 那条「**不是字母占位符**」。
//
// ## 这一条是复审补上的：原来那条钉不住
//
// 原来写的是「那一格不是一整块底色」（`avatarHasInk`）。复审把两种改法各试了
// 一遍，**整包全绿**：
//
//   - 把人形换回恒定的字母「B」——**正是 issue #95 明令不许的那一格**；
//   - 那一格**什么都不画**。
//
// 原因是问错了问题：边框 + 圆角裁剪本来就让那一格不是纯色。而那条用例的注释
// 写着「这一条钉住画的是那个**人形**」，它做不到——注释说的和它量的不是一回事。
//
// ## 现在量的是**形状**
//
// 人形的肩膀横着铺开 `side*0.6`（实测 **17 px / 28**），12 号粗体「B」最宽的
// 那一横只有 **7 px**。所以「最长的那一段连续墨有多长」直接分开这两者，而墨点
// 总量（**211** 对 **50**）再补一刀，防的是「又宽又细」那种改法。
//
// 深浅两套主题各跑一遍：数字量出来一模一样（17/211 与 7/50），而只跑浅色的话
// 有人改了深色的那套颜色，这一���还是绿的。
func TestTheAvatarShowsAPersonAndNotALetterWhenNobodyIsLoggedIn(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			f := newAvatarFixture(t)
			_, tt := newAvatarShellIn(t, f, "", dark)

			// 未登录那一格是**同步**画出来的（没有后台那一趟），所以等的是那句话，
			// 而不是 `avatarHasFigure`——等它的话，这一格要是真坏了，报出来的是一句
			// 「等了 10 秒」，而这一条要说的事是「它长成了什么样」。
			waitFrames(t, tt, "那一格摆成未登录", func() bool {
				_, ok := tt.Find(avatarNoteGuest)
				return ok
			})

			run, ink := avatarFigure(tt)
			if run < avatarFigureMinRun || ink < avatarFigureMinInk {
				t.Errorf("那一格画出来的是「最长的一段墨 %d px、整格 %d 个墨点」，"+
					"而人形是 %d px / %d 个——**一个字母撑不开一格**"+
					"（12 号粗体「B」实测 7 px / 50 点，**什么都不画 0 / 0**），"+
					"issue #95 明令不许字母占位符",
					run, ink, avatarFigureMinRun, avatarFigureMinInk)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 图没下来
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAvatarSaysSomethingWhenThePictureDoesNotComeBack 验图下不来时有一个交代。
//
// 这是 issue #95 的第三条验收，括号里那半句是重点：「**不是空白**」。
//
// 那台 fake 对头像地址一律回 500，而 B 站确实会这样（地址带的那一段参数过期、
// 账号被风控、图被删）。界面上这时候最容易出的就是「一个 28 见方的空白」——
// 而空白和「这个账号本来就没头像」在界面上分不开。
func TestTheAvatarSaysSomethingWhenThePictureDoesNotComeBack(t *testing.T) {
	f := newAvatarFixture(t)
	f.withALoginIn()
	f.refuseTheFace()
	_, tt := newAvatarShell(t, f, "2")

	waitFrames(t, tt, "头像说出加载失败", func() bool {
		_, ok := tt.Find(avatarNoteNoPicture)
		return ok
	})
	if !avatarHasFigure(tt) {
		t.Error("加载失败那一档是一格空白——在顶栏上那是一个看不出是什么的洞")
	}
	if avatarShowsThePicture(tt) {
		t.Error("压根没下下来却画上了那张图")
	}
}

// TestAFaceThatComesBackForTheWrongAccountNeverLands 钉住「退登那一刻在途的那一趟
// 要作废」这道 `gen` 守卫。
//
// ## 它钉的那个后果
//
// 用户点「退出登录」时，**头像那一趟可能正在飞**。它回来的时候带着的是上一个
// 账号的头像地址，而用户已经退登了——写进去的话，顶栏上又出现了一个人的脸，
// 而界面上**没有任何一个地方**会说「刚才那趟是退登之前的」。症状是退登之后
// 头像过一会儿自己又变了。
//
// ## 为什么**直接调** `fetchAvatarFace`，而不是造一个真的在途请求
//
// 造真的在途请求要卡住那台 fake 的 nav 那一支、放行、再赌它什么时候落地——
// 而「什么时候落地」没有任何东西会通知界面（那一趟落地时如果代次对不上，
// **连 `wake.invalidate` 都不调**，见 `fetchAvatarFace` 的注释）。
//
// 直接调它就没了这个赌：`**此刻没登录**，所以 `AccountNav` 立刻回零值、
// 一个请求都不发**，那一趟是**同步**走完的。断的是「代次对不上时能不能写」，
// 不是「网络什么时候回来」。
//
// ## 变异验过
//
// 把 `fetchAvatarFace` 里那道 `if a.gen != gen` 短路掉（退登之后在途的那一趟
// 照样把上一个账号的结果写回来），这一条立刻红。
func TestAFaceThatComesBackForTheWrongAccountNeverLands(t *testing.T) {
	f := newAvatarFixture(t)
	f.withALoginIn()
	s, tt := newAvatarShell(t, f, "2")
	tt.SetFocused(true)

	// 先让这一格真的问过一次、代次真的涨上去。
	waitFrames(t, tt, "头像画出来", func() bool { return avatarShowsThePicture(tt) })
	if err := s.app.Logout(); err != nil {
		t.Fatalf("退登失败：%v", err)
	}
	// 退登之后画一帧：`avatarSync` 看到登录态没了，收回未登录，代次也 +1。
	waitFrames(t, tt, "收回未登录", func() bool {
		_, ok := tt.Find(avatarNoteGuest)
		return ok
	})

	// 那一趟带着**上一个账号**的结果回来。`staleGen` 是退登前那一代的代次。
	s.fetchAvatarFace(staleAvatarGen(t, s))
	tt.Frame()

	if avatarShowsThePicture(tt) {
		t.Error("一个作废的结果把上一个账号的头像画回来了——退登之后顶栏上还有一个人的脸")
	}
	if _, ok := tt.Find(avatarNoteGuest); !ok {
		t.Error("一个作废的结果把那一格改成了别的样子，文字有：", tt.Texts())
	}
}

// staleAvatarGen 读出「当前代次减一」——也就是刚刚作废的那一代。
//
// 直接读字段：**同一个包里这么读不用给生产代码加一个只有测试用的出口**。
func staleAvatarGen(t *testing.T, s *Shell) uint64 {
	t.Helper()
	s.avatar.mu.Lock()
	g := s.avatar.gen
	s.avatar.mu.Unlock()
	if g == 0 {
		t.Fatalf("代次是 0，这一格压根没问过——起点就不对")
	}
	return g - 1
}

// ─────────────────────────────────────────────────────────────────────────────
// 两条形态不许合并
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAvatarNeverLooksTheSameTwoWays 钉住 issue #95 的第三条验收：
// 「登录态与未登录态分别有测试，**防止以后被合并成一个**」。
//
// ## 为什么上面那几条各自钉不住这件事
//
// 「登录了画头像」「没登录画未登录」两条测试各自都成立，可它们**一起被合并**
// 的时候也各自都成立——比如有人为了「代码短一点」把两档都画成同一张图，或者
// 都画成人形。症状是未登录的用户在顶栏上看见别人的脸，而**没有一条测试会红**。
//
// 所以这一条量的不是「某一种形态长什么样」，是「**任意两种形态画出来差得多**」。
//
// ## 判据为什么是「差得多」而不是「一点都不一样」
//
// 三种形态各拍下那一格的像素，两两比。三件事让「逐像素完全相等」这个判据
// **不能用**，每一件都实测过：
//
//  1. **那两格不在同一个位置。** 顶栏上「登录」和「退出登录」两个按钮宽度不
//     同，登录态一变，头像被推的位置差了几十个像素（实测 1074.4 对 1102.2）。
//  2. **同一个形状落在不同的小数位置上，圆边那一圈抗锯齿的颜色就不一样。**
//     实测：两种形态画出来明明是同一张，逐像素比有 **44 个点**不同——占 784
//     个点的 6%。也就是说「完全相等」这个判据**永远不成立**，连真的合并也测不出。
//  3. **别拿「有没有某一个点正好是纯告警色」当判据。** 那一格只有 28 见方，
//     画笔画的圆边全是抗锯齿的：实测那一档里没有一个像素是纯
//     `#dc2626`，最接近的是 `{221 41 41}` 和 `{229 95 95}`。
//
// 所以判据是**不一样的比例**：小于四分之一算「一样」。噪声底是 6%，而真的合并
// （把两档画成同一个样子）会让那一整块圆变色，差 90% 以上——中间空得很开，
// 阈值挪到十分之一也一样成立。
func TestTheAvatarNeverLooksTheSameTwoWays(t *testing.T) {
	// 未登录。
	guest := newAvatarFixture(t)
	_, guestTT := newAvatarShell(t, guest, "")
	waitFrames(t, guestTT, "未登录那一格", func() bool { return avatarHasFigure(guestTT) })
	guestShot := avatarShot(t, guestTT)

	// 已登录，图下不下来：出了毛病的那一档。
	//
	// ⚠️ **等的是那句话，不是「那一格有东西」**：`avatarHasFigure` 在「图还在路上」
	// 那一档也成立（那也是个人形），所以拿它当条件会**拍到还没出毛病的那一帧**，
	// 于是这一条比的是「加载中」和「未登录」——两个本来就该长得一样的东西，
	// 然后报一句「两种形态被合并了」。实测踩过。
	broken := newAvatarFixture(t)
	broken.withALoginIn()
	broken.refuseTheFace()
	_, brokenTT := newAvatarShell(t, broken, "2")
	waitFrames(t, brokenTT, "加载失败那一格", func() bool {
		_, said := brokenTT.Find(avatarNoteNoPicture)
		return said && avatarHasFigure(brokenTT)
	})
	brokenShot := avatarShot(t, brokenTT)

	// 已登录，图下得来：真头像。
	ready := newAvatarFixture(t)
	ready.withALoginIn()
	_, readyTT := newAvatarShell(t, ready, "2")
	waitFrames(t, readyTT, "真头像那一格", func() bool { return avatarShowsThePicture(readyTT) })
	readyShot := avatarShot(t, readyTT)

	for _, c := range []struct {
		a, b  []color.RGBA
		aName string
		bName string
	}{
		{guestShot, readyShot, "未登录", "登录了"},
		{guestShot, brokenShot, "未登录", "加载失败"},
		{brokenShot, readyShot, "加载失败", "登录了"},
	} {
		if d := avatarDiff(c.a, c.b); d < avatarSameThreshold {
			t.Errorf("%s 和 %s 画出来只差 %.1f%%（判据是「一样」= 低于 %.0f%%）——"+
				"两种形态被合并成一个了", c.aName, c.bName, d*100, avatarSameThreshold*100)
		}
	}
}

// avatarSameThreshold 是「这两张算同一张」的界。理由见上面那段（噪声底 6%，
// 真的合并在 90% 以上）。
const avatarSameThreshold = 0.25

// avatarShot 拍下头像那一格的像素。
//
// ⚠️ **`tt.Image()` 每一帧只取一次**：它内部把整张 1200×800 的画布转换出来，
// 在循环里调它，一次判断就是六百多次整幅转换——实测那样写「等头像画出来」
// 要 11 秒，而且时好时坏（10 秒的保险丝有一大半被这个判断本身吃掉了）。
func avatarShot(t *testing.T, tt *mygoui.Tester) []color.RGBA {
	t.Helper()
	r, ok := avatarBox(tt)
	if !ok {
		t.Fatalf("画面上找不到%q", avatarLabel)
	}
	img := tt.Image()
	x0, y0, x1, y1 := pixelsOf(r)
	out := make([]color.RGBA, 0, (x1-x0+1)*(y1-y0+1))
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			out = append(out, img.RGBAAt(x, y))
		}
	}
	return out
}

// avatarDiff 是两张同样大小的截图里不一样的比例。长度不等时算全不一样。
func avatarDiff(a, b []color.RGBA) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 1
	}
	diff := 0
	for i := range a {
		if a[i] != b[i] {
			diff++
		}
	}
	return float64(diff) / float64(len(a))
}
