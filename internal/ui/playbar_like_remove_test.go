// 播放器 UI「点赞 / 收藏」移除这一票的测试（喜欢歌单原型）。
//
// 原型把「赞（thumb）」「收藏（star）」两枚键从播放器上拿掉了，只留下
// 「喜欢」心形——那是用户自己的**喜欢歌单绑定入口**（点开「把这一首放进
// 哪些收藏夹」那个弹窗），不是 B 站的点赞，也不是三连。
//
// 这一族断的是**用户可观察的行为**：
//
//	播放条左列：没有「赞」「收藏」两枚键，也没有它们的数量角标；
//	            「分集」「评论」还在
//	播放条封面：短按照旧开满屏；**长按不再触发三连**（B 站一趟都收不到）
//	满屏：没有赞键（连带「长按三连」那句提示和赞数角标）；
//	      「喜欢」心形还在，点了开收藏夹弹窗
//	菜单「歌曲」栏：没有「赞」那一项，「添加到收藏夹…」还在
//
// ⚠️ 长按那几条用**测试时钟**推进（`fakeClock`），不靠墙上毫秒。

package ui

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// favEnv 是一台会答赞 / 收藏夹的假 B 站加上一个正在放在线歌的界面，
// **歌单那一摊也挂着**（「喜欢」心形点了要开绑定弹窗，没挂只会说一句
// 「收藏夹现在打不开」）。
//
// ⚠️ **收藏夹走 `folderBili`**（答 `created/list` 和 `resource/deal`）：
// `collectionBili` 那一台只管收藏夹**内容页**，没有 deal / created-list
// 那两条路——绑定流程的写和拉都会落空成 404。
func favEnv(t *testing.T) *likeEnv {
	t.Helper()
	env := newLikeEnv(t, false, false, false)
	// ⚠️ **绑定流程要设置**：`BindLikedFolder` 落盘在 `Settings.LikedFolder`，
	// 不挂的话绑定写不进、心形永远停在「没绑过」那一档。
	if err := env.s.app.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	fb := newFolderBili(t, nil)
	cli := fixtureClient(t, fb)
	env.s.app.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	env.dealBili = fb
	env.tt.Frame()
	return env
}

// TestThePlaybarHasNoLikeOrFavoriteButton 断「赞」「收藏」两枚键不在播放条上，
// 而「分集」「评论」还在。
//
// ⚠️ **连着数量一起断**（无障碍名字里带过计数）：票 #701 给那两枚键带过
// 角标——原型里没有它们。
func TestThePlaybarHasNoLikeOrFavoriteButton(t *testing.T) {
	tt, _ := leftBarTester(t, multiPages(), 1200)

	for _, label := range []string{"分集", "评论"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("画面上找不到「%s」：它不该跟着赞 / 收藏一起走掉，文字有 %q", label, tt.Texts())
		}
	}
	for _, label := range []string{"赞", "已赞", "收藏", "已收藏"} {
		if _, ok := tt.Find(label); ok {
			t.Errorf("播放条上还摆着「%s」：原型里这一枚已经去掉了，文字有 %q", label, tt.Texts())
		}
	}
}

// TestLongPressingTheCoverDoesNotTriple 断「长按封面 2.5 秒」不再发一键三连。
//
// 三连入口跟着赞键一起走了（喜欢歌单原型里没有三连）——而长按**之后的
// 松手也不许再吞掉那一次 click**：现在按住封面再松手，和短按一样是开满屏。
func TestLongPressingTheCoverDoesNotTriple(t *testing.T) {
	env := newLikeEnv(t, false, false, false)
	s, tt, f := env.s, env.tt, env.f

	x, y := coverPoint(t, tt)
	tt.Press(x, y)
	advanceLike(env, 3*time.Second)
	if got := f.triplePostCount(); got != 0 {
		t.Fatalf("长按封面发了 %d 趟三连：三连入口已经去掉，want 0", got)
	}
	tt.Release(x, y)
	tt.Frame()
	if !s.full.open {
		t.Error("长按封面松手之后满屏没开：三连没了，这一按就是普通的一按")
	}
}

// TestHalfLongPressingTheCoverStillOpensFullPlayer 是上面那一条的另一面：
// 以前按住 600ms（进度环出来、三连还没发）松手，那一次 click 被吞掉、
// 满屏不开（票 #681）。没有三连之后这条吞掉的规矩整个不存在。
func TestHalfLongPressingTheCoverStillOpensFullPlayer(t *testing.T) {
	env := newLikeEnv(t, false, false, false)
	s, tt := env.s, env.tt

	x, y := coverPoint(t, tt)
	tt.Press(x, y)
	advanceLike(env, 600*time.Millisecond)
	tt.Release(x, y)
	tt.Frame()
	if !s.full.open {
		t.Error("按住 600ms 松手之后满屏没开：没有三连了，这一按不许再被吞")
	}
}

// TestTheFullPlayerHasNoThumbButKeepsTheHeart 断满屏左栏：没有「赞」键
// （连带「长按三连」提示和赞数角标），「喜欢」心形还在。
func TestTheFullPlayerHasNoThumbButKeepsTheHeart(t *testing.T) {
	env := favEnv(t)
	s, tt := env.s, env.tt
	s.openFullPlayer()
	tt.Frame()

	if _, ok := tt.Find("喜欢"); !ok {
		t.Fatalf("满屏里找不到「喜欢」心形：它是喜欢歌单的绑定入口，文字有 %q", tt.Texts())
	}
	for _, label := range []string{"赞", "已赞", "收藏", "已收藏"} {
		if _, ok := tt.Find(label); ok {
			t.Errorf("满屏里还摆着「%s」：原型里赞 / 收藏入口已经去掉了，文字有 %q", label, tt.Texts())
		}
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, "三连") {
			t.Errorf("满屏里还留着「三连」的提示 %q：长按三连已经没有了", text)
		}
	}
}

// TestTheFullPlayerHeartRunsTheLikeBindingFlow 断「喜欢」心形和列表行上
// 那颗心走**同一条**喜欢歌单绑定流程：没绑过先问三选一，而不是直接弹
// 「放进哪些收藏夹」的多选框（那一档是「暂时不用」和「绑定失效」才落的）。
//
// 🔴 **满屏要收掉**：绑定弹窗排在 `viewFullPlayer` **之前**建，满屏盖在
// 它上面——不收掉的话弹窗开在满屏底下（票 #706 的坑，评论抽屉同一道闸）。
//
// 🔴 **断的是 likeBind，不是 folderSelect**：两条路开在同一个壳上，拿
// `folderSelect != nil` 当判据的话，「绕回旧多选框」那一版照样绿——而
// 那正是本票要拦的病（播放器心形绕过绑定流程）。
func TestTheFullPlayerHeartRunsTheLikeBindingFlow(t *testing.T) {
	env := favEnv(t)
	s, tt := env.s, env.tt
	s.openFullPlayer()
	tt.Frame()

	if err := tt.Click("喜欢"); err != nil {
		t.Fatalf("点「喜欢」：%v（文字有 %q）", err, tt.Texts())
	}
	if s.full.open {
		t.Error("点心形之后满屏还开着：绑定弹窗会被它压在底下")
	}
	waitFrames(t, tt, "绑定三选一开出来", func() bool { return s.likeBind != nil })
	if s.folderSelect != nil {
		t.Error("满屏心形直接弹了收藏夹多选框：它绕过了绑定流程")
	}
	if tt.HasText("添加到收藏夹") {
		t.Error("画面上是「添加到收藏夹」那句：多选框开了，绑定流程没走")
	}
	if got := env.f.likePostsCount(); got != 0 {
		t.Errorf("点「喜欢」发了 %d 趟点赞请求：心形是喜欢歌单入口，不是 B 站点赞", got)
	}
}

// TestTheFullPlayerHeartGoesStraightIntoTheBoundFolder 断「已绑定」那一档：
// 满屏心形点下去**不弹任何框**，直接把这一首写进绑定的那一格——和列表
// 行上那颗心同一个行为（`TestLikeIntoBoundFolderWritesDirectly` 在列表那
// 一侧钉的是同一件事）。
func TestTheFullPlayerHeartGoesStraightIntoTheBoundFolder(t *testing.T) {
	env := favEnv(t)
	s, tt := env.s, env.tt
	s.app.BindLikedFolder(app.LikedFolder{ID: 42, Title: "我的喜欢"})
	s.openFullPlayer()
	tt.Frame()

	if err := tt.Click("喜欢"); err != nil {
		t.Fatalf("点「喜欢」：%v（文字有 %q）", err, tt.Texts())
	}
	waitFrames(t, tt, "写进绑定歌单那一趟回来", func() bool {
		return len(env.dealBili.deals()) == 1
	})
	ds := env.dealBili.deals()
	if got := ds[0].get("add_media_ids"); got != "42" {
		t.Errorf("deal 的 add_media_ids = %q，want 42（绑定的那一格）", got)
	}
	if s.folderSelect != nil || s.likeBind != nil {
		t.Error("已绑定时心形不该弹任何框：它该直接写进绑定的那一格")
	}
}

// miniLikeEnv 造一个迷你窗外壳 + 它的主壳，主壳上挂着「设置 + 歌单 +
// 正在放的在线 mv」——迷你那枚心形要的全套。
//
// ⚠️ **弹窗画在主壳那一支**（`viewFolderDialogs` 只在主窗口那一支调），
// 所以断言看的是 `main.likeBind` / `main.folderSelect`，不是迷你自己那
// 一份——写到副壳上等于开了一个看不见的窗。
func miniLikeEnv(t *testing.T) (*Shell, *Shell) {
	t.Helper()
	// ⚠️ **走 `main.newMiniShell` 那条生产路**：`NewMini` 自己不会把
	// `mini.main` 接回主壳（那一半在 `newMiniShell` 里），直接 `NewMini`
	// 的话弹窗路由不到主壳上（`s.main` 是 nil，`likeCurrent` 落回副壳）。
	//
	// ⚠️ **fixture 必须先于 `newTestShell` 建**：`newTestShell` 里那个
	// `mounted` 闭包当场就跑（建 App 时），而 `t.Cleanup` 是 LIFO——
	// fixture 晚一步建的话它的 `Close` 会排在 `Shell.Close` 前面跑，
	// 测试还活着、服务器已经没了（「connection refused」就是它）。
	f := newBiliFixture(t, 1)
	fb := newFolderBili(t, nil)
	cli := fixtureClient(t, fb)
	var a *app.App
	main := newTestShell(t, mounted(func(t *testing.T, ap *app.App) {
		a = ap
		if err := ap.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
			t.Fatal(err)
		}
		ap.MountStreaming(app.Streaming{
			Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
		})
		store := app.NewMemoryStore("test")
		if err := store.Save(context.Background(), app.Session{SESSDATA: "sess", DedeUserID: "7"}); err != nil {
			t.Fatal(err)
		}
		ap.MountLogin(store, cli)
		// MountLogin 的 restore 会拿 Session.Cookies（空的）重 Seed 一遍
		// jar，把 `fixtureClient` 种的 DedeUserID 冲掉——再装回去。
		if err := cli.SeedCookies([]*http.Cookie{{Name: "DedeUserID", Value: "7", Path: "/"}}); err != nil {
			t.Fatal(err)
		}
		ap.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	})).s
	s := main.newMiniShell()
	t.Cleanup(s.closePerWindow)
	if err := a.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVmini", Cid: "1", Sid: "7",
		Title: "迷你里要喜欢的那一首", Artist: "UP", ArtistMid: 12345,
	}); err != nil {
		t.Fatalf("把在线的那首放进队列：%v", err)
	}
	waitFor(t, "在线那首开始播", func() bool { return a.Snapshot().HasTrack })
	return s, main
}

// TestTheMiniHeartRunsTheLikeBindingFlow 断迷你窗那枚心形走的是**绑定
// 流程**（没绑过 → 主壳上的三选一），不是绕回旧多选框。
func TestTheMiniHeartRunsTheLikeBindingFlow(t *testing.T) {
	s, main := miniLikeEnv(t)

	s.likeCurrent()

	if main.likeBind == nil || !main.likeBind.open {
		t.Fatalf("迷你心形之后主壳的绑定三选一没开（likeBind = %+v）", main.likeBind)
	}
	if main.folderSelect != nil {
		t.Error("迷你心形直接弹了收藏夹多选框：它绕过了绑定流程")
	}
	if s.likeBind != nil || s.folderSelect != nil {
		t.Error("弹窗写进了迷你外壳自己那一份：那一扇不画它，用户永远看不见")
	}
}

// TestTheMenuHasNoLikeItem 断菜单「歌曲」栏没有「赞」那一项，
// 「添加到收藏夹…」还在（喜欢歌单的入口）。
func TestTheMenuHasNoLikeItem(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	m := appMenu(s)
	s.menu = m

	// 「歌曲」那一栏是第 5 栏（应用 / 文件 / 编辑 / 播放 / **歌曲** / …）。
	col := m.Items()[4]
	for _, it := range col.Submenu {
		if it.Label == "赞" {
			t.Error("菜单里还有「赞」那一项：播放器上的赞键已经去掉了，菜单上留一个能点的是骗人")
		}
	}
	if it := m.ItemByID(menuFavoriteID); it == nil {
		t.Error("菜单里没有「添加到收藏夹…」那一项：它是喜欢歌单的入口，不该走")
	}
}
