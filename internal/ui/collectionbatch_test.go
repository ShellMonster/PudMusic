// 歌单内容页的多选与批量那一半的测试（`collectionbatch.go`）。
//
// ⚠️ **这一组里每个「等」都用结果当判据**，没有一个是「跑够几帧」：
// 后台那一趟跑在 goroutine 里，而 Tester 一帧只要几十微秒——按帧数等就是
// 按运气等，慢机器上偶发地红。

package ui

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// 多选
// ─────────────────────────────────────────────────────────────────────────────

// enterMulti 开多选态。
func enterMulti(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("多选"); err != nil {
		t.Fatalf("点多选：%v", err)
	}
	settleFrame(tt)
}

// TestPickingARowRemembersTheSongNotItsNumber 勾的是那**一首歌**，不是「第几行」。
//
// 🔴 铁律四：翻页（自动续拉）只会在尾部追加，序号不会漂；但**换排序 / 刷新**
// 会整个换掉列表。所以勾选一律按 ID 记，批量操作按 ID 回查。
func TestPickingARowRemembersTheSongNotItsNumber(t *testing.T) {
	b, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	enterMulti(t, tt)

	if err := tt.Click("选择 歌 二"); err != nil {
		t.Fatalf("点第二行的勾：%v", err)
	}
	settleFrame(tt)

	if got := len(s.collection.picked); got != 1 {
		t.Fatalf("勾上了 %d 行，want 1", got)
	}
	// 换排序之后列表整个换了（服务器这次换个顺序回），勾的那一首还是那一首。
	b.setMedias(mediaOf(3, "歌 三"), mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	if err := tt.Click("排序：最近收藏"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("最多播放"); err != nil {
		t.Fatal(err)
	}
	waitSongs(t, tt, s, 3)
	// 换排序按设计清掉勾选（内容整个变了，留着是骗用户），所以这里验的是
	// 「行的身份跟着歌走」：同一首在列表里的位置变了，身份一个字没变。
	ids := map[string]int{}
	for i := range s.collection.list.List().Len() {
		m, ok := s.collection.list.List().At(i)
		if !ok {
			t.Fatal("取不到那一行")
		}
		ids[m.Title] = i
	}
	if ids["歌 二"] != 2 {
		t.Errorf("「歌 二」跑到了第 %d 行", ids["歌 二"])
	}
	got, _ := s.collection.list.List().At(2)
	if want := "bili:2:2"; got.ID != want {
		t.Errorf("「歌 二」那一行的身份是 %q，want %q（行身份必须是 B 站那条内容，"+
			"不是序号）", got.ID, want)
	}
}

// TestSelectAllIsStrictLengthEquality 「全选」判定是严格长度相等，不是集合比较。
//
// ⚠️ 这条是这一票点名的验收项。改成集合比较的话，「三首里选了两首」在某个
// 歌单上会被画成全选，而用户点「批量删除」会删掉他没勾的那一首。
//
// 断言分三段：一首都还没加载出来时不是全选；勾满三首是全选；**勾了两首加一个
// 已经不在列表里的 id**（数量凑够了）也不是全选。
func TestSelectAllIsStrictLengthEquality(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	enterMulti(t, tt)

	p := s.collection
	if p.allSelected() {
		t.Fatal("一首都没勾就说全选了")
	}

	for _, title := range []string{"歌 一", "歌 二"} {
		if err := tt.Click("选择 " + title); err != nil {
			t.Fatalf("点 %q 的勾：%v", title, err)
		}
		settleFrame(tt)
	}
	wantText(t, tt, "已选 2 首")
	if p.allSelected() {
		t.Error("勾了两首（共三首）就说全选了——全选必须是全部勾上")
	}

	// 凑够数量：塞一个列表里没有的身份进去，长度相等了但集合不等。
	p.picked["bili:999:2"] = true
	if p.allSelected() {
		t.Error("长度凑够了就算全选：集合里混进了不在列表里的行（集合比较的错法）")
	}
	delete(p.picked, "bili:999:2")

	// 勾满三首就是全选。
	if err := tt.Click("选择 歌 三"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if !p.allSelected() {
		t.Errorf("三首都勾上了却不是全选：%v", p.picked)
	}
	wantText(t, tt, "已选 3 首")
}

// TestSelectAllOnAnEmptyFolderIsNotSelected 一个收藏夹里的歌一条都没加载出来时，
// 「全选」画成勾上的话，用户会以为接下来点一下删的是全部，而实际是零条。
func TestSelectAllOnAnEmptyFolderIsNotSelected(t *testing.T) {
	_, s, tt := collectionShell(t, "12345")
	waitSongs(t, tt, s, 0)
	if got := s.collection.list.Paged().Loaded; got != 0 {
		t.Fatalf("列表里有 %d 行，want 0", got)
	}
	// 空容器走的是「没有收藏夹」那一支（见 viewCollection），多选那条路
	// 走不到——那就直接问判定本身。
	if s.collection.allSelected() {
		t.Error("空列表被判成全选了")
	}
}

// TestLeavingMultiModeClearsThePicks 退出多选要顺手清掉勾选：
// 留着的话下次进来会出现「刚开就有一堆被勾上的行」。
func TestLeavingMultiModeClearsThePicks(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	enterMulti(t, tt)

	if err := tt.Click("选择 歌 一"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if len(s.collection.picked) != 1 {
		t.Fatalf("勾上了 %d 行", len(s.collection.picked))
	}
	if err := tt.Click("退出多选"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if len(s.collection.picked) != 0 {
		t.Errorf("退出多选之后还留着 %d 个勾", len(s.collection.picked))
	}
	if s.collection.list.List().HasSelection() {
		t.Error("退出多选之后勾选接线还装在列表上（复选框会继续画出来）")
	}
}

// ─────────────────────────────────────────────────────────────────────────────

// 批量删除（二次确认）
// ─────────────────────────────────────────────────────────────────────────────

// TestDeletingAsksFirst 点「批量删除」不会直接删——确认框先问一次。
//
// ⚠️ 那个框现在是**应用自绘**的（票 #176 / G33，形状照上游的
// `components/confirm-modal`），而**遮罩照样吃掉点在外面那一下**——不会
// 「穿过去」按到下面的按钮上（那才是「误删一整批」那一类错的来源）。
func TestDeletingAsksFirst(t *testing.T) {
	b, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	enterMulti(t, tt)
	for _, title := range []string{"歌 一", "歌 二"} {
		if err := tt.Click("选择 " + title); err != nil {
			t.Fatal(err)
		}
		settleFrame(tt)
	}

	if err := tt.Click("批量删除"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	wantText(t, tt, "确认删除选中的 2 首吗？")
	if n := len(b.removedBatches()); n != 0 {
		t.Fatalf("确认框还没点确定就删了 %d 批", n)
	}

	// 取消：什么也不删。
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	dontWantText(t, tt, "确认删除选中的 2 首吗？")
	if n := len(b.removedBatches()); n != 0 {
		t.Errorf("取消了还是删了 %d 批", n)
	}

	// 再来一次，这次确定。
	if err := tt.Click("批量删除"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	// ⚠️ 删是**后台 goroutine** 里的活儿（`startCollectionDelete` 立刻返回），
	// 而 Tester 三帧只要几十微秒——按帧数等就是按运气等，慢机器上会偶发地红。
	// 上面那句「取消了还是删了 N 批」也是靠这一条才有意义：真删了的话它会在
	// 这一段等待里出现。
	waitFor(t, "删除那一批发出去", func() bool { return len(b.removedBatches()) == 1 })

	got := b.removedBatches()
	// 一条都不能少，而且 id 和 type 要成对（只给 id 的话 B 站删不掉任何东西
	// **而且不报错**）。
	if parts := strings.Split(got[0], ","); len(parts) != 2 {
		t.Errorf("删的是 %q，want 两对 id:type", got[0])
	}
	sorted := strings.Split(got[0], ",")
	sort.Strings(sorted)
	if sorted[0] != "1:2" || sorted[1] != "2:2" {
		t.Errorf("删的是 %v，want [1:2 2:2]", sorted)
	}
}

// TestDeletingSaysWhenItCannotFindAllOfThem 勾了 5 首却只找齐 3 条完整信息时，
// **要说出来**，不能闷头删 3 条然后显示成功。
//
// `pickedMedias` 拿不到的那几条只会被静默跳过——而界面上写的是「已选 5 首」。
// 用户看着删除成功、刷新之后那两首还在，而界面上从头到尾没有一句话说过
// 「有两首没删掉」。那种「少做了一半还说做完了」比报错难查得多。
func TestDeletingSaysWhenItCannotFindAllOfThem(t *testing.T) {
	b, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)
	enterMulti(t, tt)
	for _, title := range []string{"歌 一", "歌 二"} {
		if err := tt.Click("选择 " + title); err != nil {
			t.Fatal(err)
		}
		settleFrame(tt)
	}

	// 模拟「查得到这一行、但完整信息那张表里没有它」：现在这两首都会被跳过。
	s.collection.media.reset()

	if err := tt.Click("批量删除"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "那句话说出来", func() bool {
		return tt.HasText("有 2 首查不到完整信息，这次先删其余的")
	})
	// 一条都找不齐时不发请求——**也不说「先勾几首再删」**：用户明明勾了，
	// 那是一句假话，而它会把真正的原因盖掉。
	dontWantText(t, tt, "先勾几首再删")
	if n := len(b.removedBatches()); n != 0 {
		t.Errorf("一条都找不齐还是发了 %d 批删除请求", n)
	}
}

// TestEscapeCancelsTheDelete Esc = 取消，而且**下一帧不许再问一遍**。
func TestEscapeCancelsTheDelete(t *testing.T) {
	b, s, tt := collectionShellNoFrame(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	enterMulti(t, tt)
	for _, title := range []string{"歌 一", "歌 二"} {
		if err := tt.Click("选择 " + title); err != nil {
			t.Fatal(err)
		}
		settleFrame(tt)
	}
	if err := tt.Click("批量删除"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	tt.Key(0, ui.KeyEscape)
	settleFrame(tt)
	dontWantText(t, tt, "确认删除选中的 2 首吗？")
	if n := len(b.removedBatches()); n != 0 {
		t.Errorf("Esc 之后还是删了 %d 批", n)
	}
}

// TestDeletingWithNothingPickedSaysSo 一条都没勾就点删除：说一句，不发请求。
func TestDeletingWithNothingPickedSaysSo(t *testing.T) {
	b, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"))
	waitSongs(t, tt, s, 1)
	enterMulti(t, tt)

	if err := tt.Click("批量删除"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if n := len(b.removedBatches()); n != 0 {
		t.Errorf("没勾任何东西还是发了 %d 次删除请求", n)
	}
	wantText(t, tt, "先勾几首再删")
}

// ─────────────────────────────────────────────────────────────────────────────
// 批量下载
// ─────────────────────────────────────────────────────────────────────────────

// TestDownloadingSaysItIsNotWiredYet 没挂下载服务时**不假装成功**。
//
// 假装成功的话界面上会弹一句「下载任务已添加」，而用户等一小时什么也不会发生。
//
// ⚠️ 这一条盯的是**没挂上**那一支（`MountCollections` 没传 `Downloader`，也就是
// 测试和「这一次启动没装下载服务」那两条路）。挂上了的那一条是
// `TestTheDownloadInterfaceIsWiredOnceImplemented`。
func TestDownloadingSaysItIsNotWiredYet(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)
	enterMulti(t, tt)
	if err := tt.Click("选择 歌 一"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	// 按钮是**禁用**的（按不动）：下载服务还没挂上。
	//
	// ⚠️ 按一个禁用的按钮**不报错**（Tester 照点，只是 `Clicked()` 恒为
	// false），所以判据是「按下去之后界面上什么都没变」。
	wantText(t, tt, "批量下载")
	if err := tt.Click("批量下载"); err != nil {
		t.Fatalf("找「批量下载」这个按钮：%v", err)
	}
	settleFrame(tt)
	dontWantText(t, tt, "下载任务已添加")

	// ⋯ 菜单里的「下载全部音频」能点（那是菜单项，禁不住），
	// 而它**也要先问一次**——下载会占磁盘和带宽，按错一次就是几百兆白下。
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if err := tt.ChooseMenuItem("下载全部音频"); err != nil {
		t.Fatalf("点「下载全部音频」：%v", err)
	}
	settleFrame(tt)
	wantText(t, tt, "确认下载选中的 2 首吗？")
	if err := tt.Click("下载"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	wantText(t, tt, "下载还没挂上：这个应用没把下载服务装进来")
	dontWantText(t, tt, "下载任务已添加")
}

// TestTheDownloadInterfaceIsWiredOnceImplemented 下载服务接上之后，这条路要真的走。
//
// 这一条钉的是**接线**：勾上的那几首 + 它们来自哪个容器，原样交过去。
//
// 生产上挂的是 App 自己（`MountCollections` 传 `Downloader: a`），这里用桩是
// 为了验「这一页确实把东西交出去了」，而桩不碰盘也不发网络。
func TestTheDownloadInterfaceIsWiredOnceImplemented(t *testing.T) {
	b, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"), mediaOf(2, "歌 二"), mediaOf(3, "歌 三"))
	waitSongs(t, tt, s, 3)

	d := &recordingDownloader{}
	s.app.MountCollections(app.CollectionRefs{
		Client: collectionClient(t, b), Settle: 0, Downloader: d,
	})
	// ⚠️ **先画一帧再点**：`MountCollections` 换掉的是整份依赖（连同元信息的
	// 缓存），下一帧头图区回到未加载态——那三行字不画，整页比加载完矮一截。
	// 不画这一帧的话，`Click("多选")` 按的是上一帧的坐标，而按钮已经往上移了。
	// （这一页不会自己重拉元信息；换依赖不是用户会遇到的事，测试也不验它。）
	tt.Frame()
	tt.Frame()
	enterMulti(t, tt)
	for _, title := range []string{"歌 一", "歌 三"} {
		if err := tt.Click("选择 " + title); err != nil {
			t.Fatal(err)
		}
		settleFrame(tt)
	}
	if err := tt.Click("批量下载"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	// 下载也要先问一次（验收点名的「批量下载、批量删除能走通并有二次确认」）。
	wantText(t, tt, "确认下载选中的 2 首吗？")
	if err := tt.Click("下载"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	waitFor(t, "下载那一批交过去", func() bool { return len(d.got) == 2 })
	if d.got[0].ID != 1 || d.got[1].ID != 3 {
		t.Errorf("交过去的是 %v，want id 1 和 3", d.got)
	}
	if d.folder.ID != 12345 {
		t.Errorf("folder = %v，want 12345", d.folder)
	}
}

// recordingDownloader 是给「接上了」那条路用的桩。
type recordingDownloader struct {
	mu     sync.Mutex
	got    []bili.Media
	folder app.CollectionRef
}

func (d *recordingDownloader) EnqueueDownload(_ context.Context, folder app.CollectionRef, medias []bili.Media) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.got, d.folder = medias, folder
	return nil
}

func collectionClient(t *testing.T, b *favBili) *bili.Client {
	t.Helper()
	transport := &app.CredentialTransport{
		Base:   b.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	return cli
}

// collectionShellNoFrame 是 collectionShell 少跑几帧的那一版（用例自己控制）。
func collectionShellNoFrame(t *testing.T, id string, medias ...map[string]any) (*favBili, *Shell, *ui.Tester) {
	t.Helper()
	b := newCollectionBili(t)
	b.setMedias(medias...)
	s, tt := newShell(t)
	s.app.SetWake(s.wake.invalidate)
	s.app.MountCollections(app.CollectionRefs{Client: collectionClient(t, b), Settle: 0})
	s.navigate("/collection/" + id)
	return b, s, tt
}

// ─────────────────────────────────────────────────────────────────────────────
// 收藏星标
// ─────────────────────────────────────────────────────────────────────────────

// TestTheStarToggles 点了星标之后要真的改成收藏状态，并且说一句话。
//
// ⚠️ 「等 500 毫秒再刷新」那条规矩在 app 层（`favoriteSettleDelay`），
// 那边有专门的测试钉住；这一组管的是「点了之后画出来变成什么样」。
func TestTheStarToggles(t *testing.T) {
	b, s, tt := collectionShell(t, "12345", mediaOf(1, "歌 一"))
	waitSongs(t, tt, s, 1)

	if err := tt.Click("收藏这个收藏夹"); err != nil {
		t.Fatalf("点星标：%v", err)
	}
	waitFor(t, "收藏请求到了 B 站", b.isFaved)
	// ⚠️ **这里用的是真正的 500 毫秒**（界面上没有把 Settle 调小）。故意的：
	// 别的用例等的是「画出来变成什么样」，这一条顺带把「等够 500ms 才刷新」
	// 在界面上也走通一遍——app 层钉的是顺序和那个常数本身。
	//
	// ⚠️ **判据里带着那句话**，不能只等 `Faved`：app 那边是**先** `cache.put(info)`
	// （Faved 变 true）**再** `notice.add(...)`（那句话进界面），两句之间有一个
	// 窗口；而 `waitFrames` 是「画一帧 → 再问判据」，所以判据一旦为真就立刻返回，
	// 读到的却是**上一帧**——那一句还没画上去。满载时那个窗口足够宽到偶发红
	// （症状是「收藏成功了，可画面上只有『正在收藏…』」）。
	waitFrames(t, tt, "收藏状态刷新回界面", func() bool {
		return s.app.Collection(s.collection.ref).Faved && tt.HasText("已收藏这个收藏夹")
	})
	wantText(t, tt, "已收藏这个收藏夹")
}

// ─────────────────────────────────────────────────────────────────────────────
// 崩了不许带走整个进程
// ─────────────────────────────────────────────────────────────────────────────

// TestAPanicInTheCollectionPageIsCaught view 里 panic 会一路穿出 Cocoa 事件循环
// 直接杀死进程（框架的 Dispatch 和 drain 都没有 recover）。这一页外面包着
// `Shell.page` 那一道 Guard —— 崩了换错误页，进程还活着。
func TestAPanicInTheCollectionPageIsCaught(t *testing.T) {
	s, tt := newShell(t)
	s.pageBody = func(*ui.Context, *ui.Route, Route) { panic("这一页崩了") }
	s.navigate("/collection/12345")
	tt.Frame()
	tt.Frame()
	wantText(t, tt, "界面出错了")
}

// TestEmptyCollectionSaysNoFavorites 容器拉回来了、里面一条都没有：画「没有收藏夹」。
func TestEmptyCollectionSaysNoFavorites(t *testing.T) {
	_, s, tt := collectionShell(t, "12345")
	waitSongs(t, tt, s, 0)
	wantText(t, tt, noFavorites.Title)
	wantText(t, tt, noFavorites.Hint)
}

// TestTheNotMountedPageSaysSo 没挂接口时画得出来，但要告诉用户为什么。
func TestTheNotMountedPageSaysSo(t *testing.T) {
	s, tt := newShell(t)
	s.navigate("/collection/12345")
	tt.Frame()
	tt.Frame()
	wantText(t, tt, "暂时打不开收藏夹")
	wantText(t, tt, "这一页要 B 站接口，它还没挂上")
}

func newEmptyFolderClient(t *testing.T) *bili.Client {
	t.Helper()
	b := newCollectionBili(t)
	b.setMedias()
	return collectionClient(t, b)
}
