// 这一票几个测试共用的那台假 B 站和那几个宿主。
//
// 单独一个文件是因为三个测试文件（编辑弹窗 / 封面 / 收藏夹选择）都要用它们，
// 而合在一起就超过了「单个文件 800 行」那条线。
//
// 跑的是 MyGo 的 Tester：和真窗口一模一样的布局、点击、模态框和系统菜单，
// 只是画到内存里。B 站是假的（见 folderBili），文件框是桩（见 stubPicker）。
//
// 断言面是**渲染结果**——画面上有什么字、点了之后变成了什么样。
// 唯一读状态的地方是**输入框里的值**：Tester 能找到输入框（它有 Label），
// 但读不到输入框的内容（那是控件的值，不是无障碍标签）。那种地方用状态
// 断言，并在旁边写清楚为什么。

package ui

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/egoist/mygo"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 一台假 B 站
// ─────────────────────────────────────────────────────────────────────────────

// folderBili 是这一票专用的那台假 B 站。
type folderBili struct {
	*httptest.Server

	mu sync.Mutex
	// pages 是 created/list 会回的几页。最后一页故意不满，逼翻页循环走
	// 「少于一步长」那一支。
	pages [][]map[string]any
	// ps / pn 记下每一次列表请求的步长和页码。
	ps, pn []string
	// arrived 是 created/list **收到了几个请求**，记在闸门**之前**。
	//
	// ⚠️ 为什么不能拿 `pn` 顶这一份：`pn` 是在取锁之后记的，而 `listGate` 卡的
	// 就是取锁之前那一段——被卡住的那一趟在闸门放走之前一条都记不上，而
	// 「它到了没有」恰恰是刷新那条测试要等的第一个信号（等它到了才点第二下，
	// 才真的构成「连点」）。
	arrived int
	// info 是 folder/info 的固定回答。
	info map[string]any
	// askedInfo 记下 folder/info 被问了几次、问的是哪个 id。
	askedInfo []string
	// wrote 是 add / edit 记下的表单。
	wrote []urlValues
	// covers 是每一次 cover/up 收到的封面（data 地址）。
	covers []string
	// dealt 记下 deal 那一次的 add / del。
	dealt []urlValues
	// failList 让 created/list 回一个业务错误（造「拉失败」那一种）。
	failList bool
	// collected 是 collected/list（「我收藏的」那一组）回的那几条。
	collected []map[string]any

	// gate 让**写接口**那一趟卡住，直到测试把它放走。
	//
	// ⚠️ 为什么需要它：对着 localhost 的假服务器一次往返只要几毫秒，而 Tester
	// 一帧是几十微秒——点完「提交」再跑三帧，后台早跑完回来了。于是「按钮转
	// 圈」「弹窗没关」那两个中间态**观察不到**，测试只能靠碰运气（本地跑绿、
	// CI 上偶尔红）。卡住那一趟，中间态就是确定的。
	gate <-chan struct{}

	// listGate 让 **created/list**（拉「我创建的」收藏夹列表）那一趟卡住。
	// nil = 不卡。
	//
	// ⚠️ **单独一个而不是复用 `gate`**：那个是给写接口用的，而收藏夹列表是**读**
	// 接口——把它卡住的话，收藏夹选择弹窗自己就拉不出列表，`folderShell` 那一族
	// 测试会跟着挂住。要验「刷新」那条路上那个在飞状态，就得只卡这一个读接口。
	listGate <-chan struct{}
}

// urlValues 是录下来的一张表单。
type urlValues map[string][]string

func (v urlValues) get(k string) string {
	if s, ok := v[k]; ok && len(s) > 0 {
		return s[0]
	}
	return ""
}

func newFolderBili(t *testing.T, gate <-chan struct{}) *folderBili {
	t.Helper()
	f := &folderBili{gate: gate, info: map[string]any{
		"id": 42, "title": "通勤路上", "intro": "早上听", "attr": 2,
		"media_count": 3, "cover": "", "fav_state": 0,
		"upper": map[string]any{"mid": 7, "name": "UP 小明"},
	}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ⚠️ **只卡写接口**：读接口（folder/info、resource/list、created/list）
		// 是页面自己要的，卡住它们的话测试连歌单页都进不去。
		//
		// 卡在**取锁之前**：卡着的时候还要能把已经收到的请求读出来（测试要
		// 断言「发出去的那份对不对」），握着锁卡住会把两边都锁死。
		//
		// `listGate` 同理，而且它更得在锁前面：卡着的正是一个**读**请求，
		// 而测试要读的恰恰是「它收到没有」——所以收到的那一刻先记一下
		// `arrived`（见那个字段的注释）。
		if r.URL.Path == "/x/v3/fav/folder/created/list" {
			f.mu.Lock()
			f.arrived++
			f.mu.Unlock()
			if f.listGate != nil {
				<-f.listGate
			}
		}
		if f.gate != nil && isFolderWrite(r.URL.Path) {
			<-f.gate
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/x/v3/fav/folder/created/list":
			page, _ := strconv.Atoi(r.URL.Query().Get("pn"))
			f.ps = append(f.ps, r.URL.Query().Get("ps"))
			f.pn = append(f.pn, r.URL.Query().Get("pn"))
			if f.failList {
				// 业务错误（-500）：造「拉失败」那一种。
				writeFolderJSON(w, -500, "服务器开小差了", nil)
				return
			}
			list := []map[string]any{}
			if page >= 1 && page <= len(f.pages) {
				list = f.pages[page-1]
			}
			writeFolderJSON(w, 0, "0", map[string]any{
				"count": 200, "list": list, "has_more": page < len(f.pages),
			})
		case "/x/v3/fav/folder/collected/list":
			// 「我收藏的」那一组。默认是空的（多数测试只关心「我创建的」）。
			writeFolderJSON(w, 0, "0", map[string]any{
				"count": len(f.collected), "list": f.collected,
			})
		case "/x/v3/fav/resource/list":
			writeFolderJSON(w, 0, "0", map[string]any{
				"medias": []map[string]any{{
					"id": 77, "type": 12, "title": "歌 一", "bvid": "BV177",
					"cover": "", "duration": 185, "attr": 0, "page": 1,
					"upper": map[string]any{"name": "UP 一"},
				}},
				"has_more": false,
			})
		case "/x/v3/fav/folder/info":
			f.askedInfo = append(f.askedInfo, r.URL.Query().Get("media_id"))
			writeFolderJSON(w, 0, "0", f.info)
		case "/x/v3/fav/folder/add", "/x/v3/fav/folder/edit":
			_ = r.ParseForm()
			f.wrote = append(f.wrote, urlValues(r.PostForm))
			id, _ := strconv.Atoi(r.PostForm.Get("media_id"))
			if id == 0 {
				id = 999
			}
			writeFolderJSON(w, 0, "0", map[string]any{
				"id": id, "title": r.PostForm.Get("title"), "mid": 7, "type": 11, "attr": 0,
			})
		case "/x/vu/web/cover/up":
			_ = r.ParseForm()
			f.covers = append(f.covers, r.PostForm.Get("cover"))
			writeFolderJSON(w, 0, "0", map[string]any{"url": "https://i0.hdslb.com/c.jpg"})
		case "/x/v3/fav/resource/deal":
			_ = r.ParseForm()
			f.dealt = append(f.dealt, urlValues(r.PostForm))
			writeFolderJSON(w, 0, "0", nil)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// isFolderWrite 判这条路径是不是「会改东西」的那几个。
func isFolderWrite(path string) bool {
	switch path {
	case "/x/v3/fav/folder/add", "/x/v3/fav/folder/edit",
		"/x/vu/web/cover/up", "/x/v3/fav/resource/deal":
		return true
	}
	return false
}

func writeFolderJSON(w http.ResponseWriter, code int, msg string, data any) {
	body := map[string]any{"code": code, "message": msg, "ttl": 1, "data": data}
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// folderPage 造一页收藏夹（from 起编号）。
func folderPage(from, n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := range n {
		out = append(out, map[string]any{
			"id": from + i, "title": "收藏夹 " + strconv.Itoa(from+i),
			"attr": 2, "media_count": i, "fav_state": 0,
		})
	}
	return out
}

func (f *folderBili) lock()   { f.mu.Lock() }
func (f *folderBili) unlock() { f.mu.Unlock() }

// failTheList 让「我创建的」那个接口回一个业务错误（造「拉失败」那一种）。
func (f *folderBili) failTheList() {
	f.lock()
	defer f.unlock()
	f.failList = true
}

// setCollected 装好「我收藏的」那一组回什么。
func (f *folderBili) setCollected(list []map[string]any) {
	f.lock()
	defer f.unlock()
	f.collected = list
}

func (f *folderBili) asks() (ps, pn []string) {
	f.lock()
	defer f.unlock()
	return append([]string(nil), f.ps...), append([]string(nil), f.pn...)
}

func (f *folderBili) asked() []string {
	f.lock()
	defer f.unlock()
	return append([]string(nil), f.askedInfo...)
}

// arrivals 是 created/list 到过几个请求（含还卡在闸门上的那些）。
func (f *folderBili) arrivals() int {
	f.lock()
	defer f.unlock()
	return f.arrived
}

func (f *folderBili) writes() []urlValues {
	f.lock()
	defer f.unlock()
	return append([]urlValues(nil), f.wrote...)
}

func (f *folderBili) coversSent() []string {
	f.lock()
	defer f.unlock()
	return append([]string(nil), f.covers...)
}

func (f *folderBili) deals() []urlValues {
	f.lock()
	defer f.unlock()
	return append([]urlValues(nil), f.dealt...)
}

// ─────────────────────────────────────────────────────────────────────────────
// 宿主
// ─────────────────────────────────────────────────────────────────────────────

// folderShell 造一个停在首页、外壳上装好了收藏夹那一摊的 Tester。
//
// ⚠️ **唤醒一定要装**（同 collectionShell）：后台每一趟跑完都要叫醒界面，
// 而真窗口里这一句在 `Shell.Attach` 里。测试不走 Attach。
func folderShell(t *testing.T, f *folderBili) (*Shell, *mygoui.Tester) {
	t.Helper()
	s, tt := newShell(t)
	s.app.SetWake(s.wake.invalidate)

	s.app.MountCollections(app.CollectionRefs{Client: fixtureClient(t, f), Settle: 0})
	return s, tt
}

// fixtureClient 造一个**指着那台假 B 站**的客户端。
//
// 三样缺一样都不行：主机要指过去（否则真发到 B 站去）、传输层要带凭据白名单
// （否则 SESSDATA 压根不会出现在请求里，而那一层是这一组测试要守的边界之一）、
// jar 里要有 DedeUserID（「我创建了哪些收藏夹」那个接口问的是 up_mid）。
func fixtureClient(t *testing.T, f *folderBili) *bili.Client {
	t.Helper()
	transport := &app.CredentialTransport{
		Base:   f.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(f.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	if err := cli.SeedCookies([]*http.Cookie{{Name: "DedeUserID", Value: "7", Path: "/"}}); err != nil {
		t.Fatal(err)
	}
	return cli
}

// stubPicker 让「弹文件框选一张图」返回那张图。
//
// ⚠️ 这是**接缝**（`Shell.pickImage`）：真实现要去系统里弹面板，那一条在无头
// Tester 里会 panic。换成桩之后「选中的图有没有被解码、有没有被裁成 16:9、
// 传上去的是不是真的 16:9」这一整条能自动验。
func stubPicker(t *testing.T, path string) func(*mygo.Window) ([]string, error) {
	t.Helper()
	return func(*mygo.Window) ([]string, error) { return []string{path}, nil }
}

// pickerPath 造一张 PNG 写进临时目录，返回它的路径。
//
// 尺寸是 w×h，**故意不是 16:9**（除非调用方就要）：裁剪那一步要把它变成 16:9。
func pickerPath(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// seedSideFolders 给侧栏塞几个「我创建的」收藏夹。
//
// 侧栏的条目数据是服务层给的（那一张票还没开工），测试自己塞——塞进去之后
// 「新建」「修改」那两个入口才点得到。
func seedSideFolders(s *Shell, ids ...int64) {
	items := make([]app.Favorite, 0, len(ids))
	for _, id := range ids {
		items = append(items, app.Favorite{ID: id, Title: "侧栏收藏夹 " + strconv.FormatInt(id, 10), Type: 11})
	}
	s.app.SetFavorites(app.FavoritesCreated, items)
}

// seedAndSettle 塞完之后跑一帧。
//
// ⚠️ **必须跑**：`Tester.Find` 查的是**上一帧**画出来的东西，而 Tester 建起来
// 那一刻的这一帧还什么收藏夹都没有。不跑这一帧的话后面每一个 `Click` /
// `RightClick` 都会报「画面上没有这句话」，而那句话明明下一帧就在了。
func seedAndSettle(s *Shell, tt *mygoui.Tester, ids ...int64) {
	seedSideFolders(s, ids...)
	settleFrame(tt)
}
