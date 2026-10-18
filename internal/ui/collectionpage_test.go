// 歌单内容页的测试。
//
// 跑的是 MyGo 的 Tester：和真窗口一模一样的布局、点击、下拉和系统确认框，
// 只是画到内存里。所以「头图区和批量操作条都在」「三种排序都能点」「全选的
// 判定是严格长度相等」「删除之前要问一次」都能自动验。
//
// B 站是假的（见 favBili）。断言面是**渲染结果**——画面上有什么字、点了之后
// 变成了什么样，不是「调了哪个函数」。

package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 一台假 B 站
// ─────────────────────────────────────────────────────────────────────────────

// favBili 是歌单这一票专用的那台假 B 站。
//
// ⚠️ 它和 login_test.go 里那个同名的东西**不是同一个类型**，是两个文件里
// 各自声明的（Go 允许不同文件里同名）。这一台只认收藏夹那几个接口。
type favBili struct {
	*httptest.Server

	mu sync.Mutex
	// infos 是 folder/info 的固定回答。
	infos map[string]any
	// medias 是内容列表的条目，**按当前排序排好的**。
	medias []map[string]any
	// orders 记下每一次内容列表请求带的 order，按顺序。
	orders []string
	// faved 是此刻收藏了没有（folder/info 就照它回）。
	faved bool
	// removed 记下每一次 batch-del 的 resources。
	removed []string
	// deleted 记下每一次 folder/del 的 media_ids（「删除收藏夹」和侧栏
	// 「删除」共用它）。
	deleted []string
	// cleaned 记下每一次 resource/clean 的 media_id（「清除失效内容」那一项）。
	cleaned []string
	// rejectDel 为真时 folder/del 回 -400（B 站拒绝删）。见 rejectFolderDelete。
	rejectDel bool
	// paths 记下每一次请求打到了哪条路径（发没发请求本身就是一条断言：
	// 「为了一个认不出的项去问 B 站」是白跑的一趟）。
	paths []string
}

func newCollectionBili(t *testing.T) *favBili {
	t.Helper()
	b := &favBili{
		infos: map[string]any{
			"id": 12345, "title": "通勤路上", "intro": "早上听",
			"attr": 2, "media_count": 3, "cover": "",
			"upper": map[string]any{"mid": 1, "name": "UP 小明"},
		},
	}
	b.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.paths = append(b.paths, r.URL.Path)
		switch r.URL.Path {
		case "/x/v3/fav/folder/info":
			state := 0
			if b.faved {
				state = 1
			}
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"id":          12345,
				"title":       b.infos["title"],
				"intro":       b.infos["intro"],
				"attr":        b.infos["attr"],
				"media_count": b.infos["media_count"],
				"cover":       b.infos["cover"],
				"fav_state":   state,
				"upper":       b.infos["upper"],
			})
		case "/x/v3/fav/resource/list":
			b.orders = append(b.orders, r.URL.Query().Get("order"))
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"medias": b.medias, "has_more": false,
			})
		case "/x/v3/fav/folder/fav", "/x/v3/fav/folder/unfav":
			_ = r.ParseForm()
			b.faved = r.URL.Path == "/x/v3/fav/folder/fav"
			writeCollectionJSON(t, w, 0, "0", nil)
		case "/x/v3/fav/resource/batch-del":
			_ = r.ParseForm()
			b.removed = append(b.removed, r.PostForm.Get("resources"))
			writeCollectionJSON(t, w, 0, "0", nil)
		case "/x/v3/fav/folder/del":
			_ = r.ParseForm()
			b.deleted = append(b.deleted, r.PostForm.Get("media_ids"))
			if b.rejectDel {
				writeCollectionJSON(t, w, -400, "请求错误", nil)
				return
			}
			writeCollectionJSON(t, w, 0, "0", 0)
		case "/x/v3/fav/resource/clean":
			_ = r.ParseForm()
			b.cleaned = append(b.cleaned, r.PostForm.Get("media_id"))
			writeCollectionJSON(t, w, 0, "0", 0)
		case "/x/web-interface/view":
			// bvid → avid。列表页的条目大多只有 bvid，而收藏夹那个弹窗要的是
			// avid（`app.ResolveFavoriteID` 走的就是这一个查询）。
			bvid := r.URL.Query().Get("bvid")
			n := 0
			for _, c := range bvid {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				}
			}
			if n == 0 {
				n = 90000 + len(bvid)
			}
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"aid": n, "bvid": bvid, "title": "看着像",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.Close)
	return b
}

// setMedias 换掉内容列表会回的条目。
func (b *favBili) setMedias(in ...map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.medias = in
	b.infos["media_count"] = len(in)
}

// mediaOf 造一条内容。id 决定身份，title 是界面上能看见的那一行。
func mediaOf(id int, title string) map[string]any {
	return map[string]any{
		"id": id, "type": 2, "title": title, "bvid": "BV1" + strconv.Itoa(id),
		"cover": "", "duration": 185, "attr": 0, "page": 1,
		"upper": map[string]any{"name": "UP " + strconv.Itoa(id)},
	}
}

// setOwnerMid 换掉容器的创建者。
//
// 「这个收藏夹是不是我自己建的」是 ⋯ 菜单里那两个破坏性项的显示条件（见
// `app.CanDeleteCollection`），而它读的是元信息里的 `upper.mid`——所以要把
// 「别人建的」那一支演出来，只能改这里。
func (b *favBili) setOwnerMid(mid int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.infos["upper"] = map[string]any{"mid": mid, "name": "UP 小明"}
}

// setAttr 换掉容器的 attr。0 是「公开、且是 B 站发下来的默认收藏夹」。
func (b *favBili) setAttr(attr int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.infos["attr"] = attr
}

// cleanedFolders 是被清过失效内容的那些收藏夹 id。
func (b *favBili) cleanedFolders() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.cleaned...)
}

// rejectFolderDelete 让这一台假 B 站**拒绝删收藏夹**（回 -400）。
//
// 为什么需要它：B 站拒绝是这条路上最要紧的一种结果——收藏夹还在、而界面已经按
// 「它没了」在处理了（把用户踢走、把状态扔掉）。只有真的发一个 -400 才走得到。
func (b *favBili) rejectFolderDelete() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rejectDel = true
}

func (b *favBili) seenOrders() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.orders...)
}

func (b *favBili) removedBatches() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.removed...)
}

// deletedFolders 是被删掉的那些收藏夹 id（side「删除」那一项）。
func (b *favBili) deletedFolders() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.deleted...)
}

// hitsOf 数打到某条路径上的请求有几回。
func (b *favBili) hitsOf(path string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, p := range b.paths {
		if p == path {
			n++
		}
	}
	return n
}

func (b *favBili) isFaved() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.faved
}

func writeCollectionJSON(t *testing.T, w http.ResponseWriter, code int, msg string, data any) {
	t.Helper()
	body := map[string]any{"code": code, "message": msg, "ttl": 1, "data": data}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// ─────────────────────────────────────────────────────────────────────────────
// 宿主
// ─────────────────────────────────────────────────────────────────────────────

// collectionShell 造一个停在某个容器页上的外壳 + Tester。
//
// ⚠️ **内容要在建外壳之前定下来**：容器页有两趟后台请求——元信息（头图区）
// 和内容列表（下面那一列）。建外壳的时候它们都已经跑完了，之后再改 fixture
// 只会改到「下一次」拉的东西，而界面上还停在旧的那一版（头图区的元信息是
// 缓存着的，列表也不会自己重拉）。要换内容就重新建一个外壳。
func collectionShell(t *testing.T, id string, medias ...map[string]any) (*favBili, *Shell, *ui.Tester) {
	t.Helper()
	return collectionShellAs(t, "", id, medias...)
}

// collectionShellAs 是上面那一个，额外**登录成 mid**（空串 = 不登录）。
//
// ⚠️ **登录是这一票的必需品**：⋯ 菜单里「清除失效内容」「删除收藏夹」只在
// 「这个收藏夹是自己建的」时才摆得出来，而那个判断的第一位是 `SelfMid()`
// ——不登录它恒为 0，于是这两个菜单项**永远摆不出来**，这一票就在界面层
// 验不到任何东西。
//
// `MountLogin` 用内存 Store（不碰钥匙串，见 homepage_feed_test.go 的同一条
// 规矩），随后直接装一份登录态进去。store 读回来是「没登录过」，那是正常的
// 起点——真正装上去的是后面那一句。
func collectionShellAs(t *testing.T, mid string, id string, medias ...map[string]any) (*favBili, *Shell, *ui.Tester) {
	t.Helper()
	b := newCollectionBili(t)
	b.setMedias(medias...)
	// ⚠️ **挂一份真设置**（临时目录）：侧栏那一行是从 `SettingsSnapshot` 读
	// 顺序的，而「删收藏夹」「从侧栏移除」收尾时要写回那个顺序（`forgetFavorite`）。
	// 不挂的话那一步回 `ErrSettingsNotMounted`，日志里多一行「顺序存不住」——
	// 而这条页面**在真机上顺序是存得住的**，让测试跑在一个假故障上没意思。
	s, tt := newShell(t, withSettings(app.DefaultSettings("")))
	// ⚠️ 装上唤醒：后台那一趟（拉元信息、收藏之后刷新）跑完之后要能叫醒界面，
	// 而真窗口里这一句是在 `Shell.Attach` 里装的。测试不走 Attach，得自己装，
	// 否则「后台改完状态界面不动」这一类在测试里根本走不到。
	s.app.SetWake(s.wake.invalidate)

	transport := &app.CredentialTransport{
		Base:   b.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	// settle 调到 0：真值 500ms 是「等 B 站改完」那一条（app 那边有专门的
	// 测试钉它），这一组要的是「点了之后画出来变成什么样」，不是等多久。
	s.app.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})

	if mid != "" {
		s.app.MountLogin(app.NewMemoryStore("collection-test"), cli)
		s.app.TestOnlyAdoptLogin(mid)
	}

	s.navigate("/collection/" + id)
	// 等容器拉回来（头图区第一帧是「正在加载…」）。
	waitFrames(t, tt, "容器元信息加载完", func() bool {
		return s.collection != nil && s.app.Collection(s.collection.ref).Loaded
	})
	return b, s, tt
}

// settleFrame 连跑几帧，让界面把这一帧的样子画完。
//
// ⚠️ **它不等后台**。后台那一趟（拉元信息、拉内容页）跑在 goroutine 里，
// 而 Tester 的 `Frame()` 一帧只要几十微秒——跑八帧后台多半还没回来。
// 「等后台」用 `waitFrames`，它的判据是**结果**，不是帧数。
//
// ⚠️ 这里数帧是有理由的：它等的不是任何后台结果，而是「MyGo 一遍 `Frame()` 会跑
// 两遍 view、点击被消费时再多一遍」这件事——要等的是**那个固定的遍数走完**，而
// 它和机器负载无关。凡是等后台结果的地方都不许用它，改 `waitFrames` 加条件。
func settleFrame(tt *ui.Tester) {
	for range 3 {
		tt.Frame()
	}
}

// waitFor 一边跑帧一边等 cond 为真（最多两秒）。
//
// ⚠️ 判据一律是**结果**（画出来没有、内容加载完没有），不是「跑了多少帧」：
// 后台那一趟什么时候回来跟机器负载有关，按帧数等就是按运气等，慢机器上会
// 偶发地红。
// ⚠️ 名字里的「Frame」是关键：**它边等边跑帧**（`tt.Frame()`），而这一组里
// 另有不跑帧的轮询（`waitUntilTrue`）。后台那一趟的状态是投递进去的，
// 不跑帧就看不见——而「跑够几帧」当判据在慢机器上会偶发地红，所以判据一律是
// 这里传进来的 cond（结果），帧只是让它有机会被看见。
//
// waitSongs 等这一页已经拉到了 n 首歌。
func waitSongs(t *testing.T, tt *ui.Tester, s *Shell, n int) {
	t.Helper()
	waitFrames(t, tt, strconv.Itoa(n)+" 首歌加载完", func() bool {
		return s.collection != nil && s.collection.list.Paged().Loaded == n
	})
}

// wantText 断言画面上有这句话，说清楚没有的是谁。
func wantText(t *testing.T, tt *ui.Tester, what string) {
	t.Helper()
	if !tt.HasText(what) {
		t.Errorf("%s 没画出来，文字有 %q", what, tt.Texts())
	}
}

// dontWantText 断言画面上**没有**这句话。
func dontWantText(t *testing.T, tt *ui.Tester, what string) {
	t.Helper()
	if tt.HasText(what) {
		t.Errorf("画面上出现了 %q（不该出现），文字有 %q", what, tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 头图区和操作条
// ─────────────────────────────────────────────────────────────────────────────

// TestCollectionShowsTheHeaderAndTheOpsBar 顶部头图区和批量操作条都在。
//
// 这两条是这一票点名的验收项，而且是「在不在」型的——**元素在框里也不算数**
// （画出来看不见的检查见下面那一段）。
func TestCollectionShowsTheHeaderAndTheOpsBar(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)

	// ① 头图区：标题、类型 + 条目数、创建者。
	wantText(t, tt, "通勤路上")
	wantText(t, tt, "早上听")
	wantText(t, tt, "公开收藏夹 • 3 条视频")
	wantText(t, tt, "UP 小明")

	// ② 操作条：四个动作 + 排序 + 多选开关。
	for _, label := range []string{"播放全部", "添加到播放列表", "多选", "排序：最近收藏"} {
		wantText(t, tt, label)
	}
	wantText(t, tt, "☆") // 星标：此刻没收藏

	// ③ 列表：那一页的歌真的画出来了。
	for _, title := range []string{"歌 一", "歌 二", "歌 三"} {
		if _, ok := tt.Find(title); !ok {
			t.Errorf("列表里没有 %q，文字有 %q", title, tt.Texts())
		}
	}

	// ④ 画出来还得看得见：头图区那个大标题和窗口底色不同色。
	at, ok := tt.Find("通勤路上")
	if !ok {
		t.Fatalf("标题框找不到")
	}
	img := tt.Image()
	p := img.RGBAAt(int(at.X)+2, int(at.Y+at.H/2))
	if p == img.RGBAAt(4, 4) {
		t.Error("标题和窗口底色同色，画出来看不见")
	}
}

// TestCollectionShowsTheBatchBarOnlyInMultiMode 批量操作条是多选态才出现的。
func TestCollectionShowsTheBatchBarOnlyInMultiMode(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)

	dontWantText(t, tt, "批量删除")
	if err := tt.Click("多选"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	for _, label := range []string{"全选", "已选 0 首", "批量下载", "批量删除", "退出多选"} {
		wantText(t, tt, label)
	}
}

// TestPrivateFoldersSaySo 头图区第三行的前半截跟着 attr 的第 0 位走。
func TestPrivateFoldersSaySo(t *testing.T) {
	b, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"))
	b.mu.Lock()
	b.infos["attr"] = 1 // 私密
	b.mu.Unlock()
	// 重新拉一次元信息：头图区那份是缓存着的，接口变了它不会自己变。
	s.app.LoadCollection(s.collection.ref)
	// ⚠️ 判据是**画面上那句话**而不是「缓存里那个字段变了」：缓存改完到下一帧
	// 画出来之间还有一步，只查缓存的话断言会在那一步之前就通过。
	waitFrames(t, tt, "新的元信息画出来", func() bool {
		return tt.HasText("私密收藏夹 • 1 条视频")
	})
	wantText(t, tt, "私密收藏夹 • 1 条视频")
}

// ─────────────────────────────────────────────────────────────────────────────
// 三种排序
// ─────────────────────────────────────────────────────────────────────────────

// TestAllThreeSortsReachTheInterface 三种排序都能用，而且真的发给了接口。
//
// ⚠️ 排序是**接口**排的（Biu 的 order=mtime/view/pubtime）。在本地重排的话
// 分页只对一种排序有效——翻到第二页会拿到「按另一种顺序的前 20 条」，而界面
// 上看不出任何异样。所以这里断言的是**发出去的参数**。
func TestAllThreeSortsReachTheInterface(t *testing.T) {
	b, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"))
	waitSongs(t, tt, s, 1)

	want := []string{"mtime", "view", "pubtime"}
	for i, label := range []string{"最多播放", "最近投稿"} {
		if err := tt.Click("排序：" + collectionSortLabel(t, i)); err != nil {
			t.Fatalf("点排序下拉（第 %d 档）：%v", i+1, err)
		}
		if err := tt.Click(label); err != nil {
			t.Fatalf("选 %q：%v", label, err)
		}
		wantText(t, tt, "排序："+label)

		// ⚠️ 判据是**服务器真的收到了那一次请求**，不是「下拉上的字变了」。
		// 换档会重建整个分页列表，而那一页是后台拉回来的——查得早了会看到
		// 上一次的 order，于是这条用例变成按运气过的。
		n := i + 2
		waitFrames(t, tt, "第 "+strconv.Itoa(n)+" 次内容请求带着 order="+want[i+1], func() bool {
			got := b.seenOrders()
			return len(got) >= n && got[n-1] == want[i+1]
		})
	}
}

// collectionSortLabel 是第 i 档排序在界面上写的字（i 从 0 起）。
func collectionSortLabel(t *testing.T, i int) string {
	t.Helper()
	return bili.CollectionSorts[i].Label()
}

// TestSortingReloadsFromTheFirstPage 换排序要从第一页重来——
// B 站的翻页只对一种排序有效，接着往下拉会拉到重复的。
func TestSortingReloadsFromTheFirstPage(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)

	if got := s.collection.list.Paged().Loaded; got != 3 {
		t.Fatalf("换排序之前有 %d 行，want 3", got)
	}
	if err := tt.Click("排序：最近收藏"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("最多播放"); err != nil {
		t.Fatal(err)
	}
	// 换了列表之后要重新等它把第一页拉回来：换排序不是本地重排。
	waitSongs(t, tt, s, 3)
	if got := s.collection.list.Paged().Page; got != 1 {
		t.Errorf("换排序之后停在了第 %d 页，want 1", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// 「可放」不许是界面上的谎言
// ─────────────────────────────────────────────────────────────────────────────

// TestEveryPlayableCollectionEntryIsAcceptedByThePlayer 钉住「可放 == 能放」。
//
// `bili.Media.Playable` 说能放的那些，界面就会照着画、照着进队列
// （`itemsOfMedias` 是唯一的筛子）。所以它和「播放器认不认这一项」必须**一致**：
//
//	Playable() == true  → 翻出来的 Item 一定要能被播放那一层接受
//	Playable() == false → 一定要被挡住（合集 / 电影放进去就是一条打不开的条目）
//
// 🔴 这一条是被一次审计逼出来的：判据照抄了 Biu 的 `[2, 12]`，而 sid 那一路
// （`/audio/music-service-c/url`）没接——于是 type=12 一路绿过所有测试，
// 照常画、照常进队列，抽屉里还标着「B 站音乐」，按播放才报错。
//
// 往 `Playable` 里加一种类型的人，不接上取流路径就会在这里红。
func TestEveryPlayableCollectionEntryIsAcceptedByThePlayer(t *testing.T) {
	for _, typ := range []int{
		bili.MediaVideo, bili.MediaAudio, // 这两种说能放
		21, 24, 99, // 合集 / 电影 / 将来冒出来的：说不能放
	} {
		m := bili.Media{ID: 1293494, Type: typ, Bvid: "BV1xx", Title: "一条"}

		items := itemsOfMedias([]bili.Media{m})
		ready := len(items) > 0 && items[0].OnlineReady()

		if ready != m.Playable() {
			t.Errorf("type=%d：Playable=%v 而界面上那条 OnlineReady=%v——"+
				"「可放」和「能放」必须一致", typ, m.Playable(), ready)
		}
	}
}

// TestAMusicLibraryEntryCarriesItsIdentity 盯 sid 这一路该带的都带了。
//
// 少了 Sid，取流那一层就没有身份（`app.streamFor` 只认 sid），而症状是
// 「界面上一条条都对，一去播就报错」。
func TestAMusicLibraryEntryCarriesItsIdentity(t *testing.T) {
	it := onlineItemOf(bili.Media{
		ID: 1293494, Type: bili.MediaAudio,
		Title: "月光", Cover: "https://i0.hdslb.com/c.jpg", Owner: "某某音乐台",
	})
	if it.Type != "audio" || it.Sid != "1293494" {
		t.Errorf("翻出来的项是 %+v，want Type=audio Sid=1293494", it)
	}
	if it.Artist != "某某音乐台" {
		t.Errorf("艺人 = %q，want 某某音乐台（收藏夹已经给了，犯不上再问一次）", it.Artist)
	}
	if !it.OnlineReady() {
		t.Error("音乐库条目翻出来却放不了")
	}
}

// TestCollectionEntriesFeedTheMediaSession 收藏夹那一行不再让艺人名空着。
//
// 收藏夹接口本来就给 UP 主名，而这一栏之前一直空着——于是锁屏上的「艺人」
// 永远是空的。Biu 那边占 `ownerName` 的也是这一栏（`store/play-list.ts:157`）。
func TestCollectionEntriesFeedTheMediaSession(t *testing.T) {
	for _, typ := range []int{bili.MediaVideo, bili.MediaAudio} {
		if got := onlineItemOf(bili.Media{ID: 1, Type: typ, Owner: "某某 UP"}).Artist; got != "某某 UP" {
			t.Errorf("type=%d 的艺人 = %q，want 某某 UP", typ, got)
		}
	}
}
