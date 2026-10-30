// 合集 / 系列页那几件**做不到的事**：搜索、排序、写操作（票 #683 的复审）。
//
// 这一组全是「用户能不能按到它、按了会发生什么」——判据一律是**画面上有没有那个
// 控件 / 请求发没发出去**，不是「某个函数被调到了」。
//
// 三条共用一个背景：合集 / 系列的内容接口**没有** `keyword` 和 `order` 参数，
// 也没有「从这里删内容 / 清失效 / 收藏整页」那套动作。界面上照旧画出来的话：
//
//	搜索框  一敲词列表就整页换成「加载失败，点此重试」，还弹一句带 `biu:` 的错
//	排序    三档发出去的请求一模一样（死控件）
//	星标    把 season_id 当收藏夹 mlid 发出去（注定失败的写操作）
package ui

import (
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// TestTheSeasonPageSearchIsDisabledAndNeverSendsAKeyword 钉住复审第 6 条。
//
// 那两个接口没有 `keyword` 参数，`LoadCollectionPage` 于是对非空词返回
// 「还不能按歌名找」——而列表控件会把整页换成「加载失败，点此重试」并弹一句
// 带内部前缀的错。用户看到的是「这一页坏了」，而不是「这一类还不支持搜索」。
//
// 🔴 **判据是两件事**：
//
//  1. 那枚放大镜**按不动**（点它不展开输入框）；
//  2. 就算硬往里敲，500ms 的钟走完之后也**不发请求**、列表一行不少
//     （`keyword` 压根没被接进 fetcher）。
func TestTheSeasonPageSearchIsDisabledAndNeverSendsAKeyword(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  bili.CollectionType
		id   string
	}{
		{"合集", bili.CollectionAlbum, "1227671"},
		{"系列", bili.CollectionSeries, "534501"},
	} {
		t.Run(c.name, func(t *testing.T) {
			clock := newFakeClock(time.Unix(2026, 0))
			b := newCollectionBili(t)
			b.setMedias(mediaOf(1, "第一集"), mediaOf(2, "第二集"))
			_, s, tt := collectionShellTypedWithClock(t, b, clock, c.typ, c.id)
			waitSongs(t, tt, s, 2)
			tt.Frame()

			before := b.seenKeywords()
			// ① 点它：**不展开**（展开了的话那个输入框就有名字了，敲得进字）。
			if err := tt.Click(collectionSearchLabel); err != nil {
				t.Fatalf("%s页上连那枚搜索键都没有，文字有 %q", c.name, tt.Texts())
			}
			settleFrame(tt)
			if s.collection.search.open {
				t.Errorf("%s页的搜索键还能展开——它应该按不动", c.name)
			}
			// ② 把防抖的钟走完：一个请求都不该发出去。
			clock.Advance(searchDebounce + time.Second)
			settleFrame(tt)
			tt.Frame()

			if after := b.seenKeywords(); len(after) != len(before) {
				t.Errorf("%s页发了 %d 次内容请求（关键词 %q）——那一类接口不收 keyword",
					c.name, len(after)-len(before), after[len(before):])
			}
			if s.collection.keyword != "" {
				t.Errorf("%s页的搜索词变成了 %q——它压根不该被接进 fetcher", c.name, s.collection.keyword)
			}
			if got := s.collection.list.Paged().Loaded; got != 2 {
				t.Errorf("%s页的列表变成了 %d 行（被搜索整页换掉了），want 2", c.name, got)
			}
			if tt.HasText("加载失败，点此重试") {
				t.Errorf("%s页被搜索整页换成了失败态，文字有 %q", c.name, tt.Texts())
			}
		})
	}
}

// collectionShellTypedWithClock 是 `collectionShellTyped` 加一个假时钟。
func collectionShellTypedWithClock(t *testing.T, b *favBili, clock *fakeClock,
	typ bili.CollectionType, id string) (*favBili, *Shell, *ui.Tester) {
	t.Helper()
	s, tt := newShell(t, withSettings(app.DefaultSettings("")), withClock(clock))
	s.app.SetWake(s.wake.invalidate)
	transport := &app.CredentialTransport{
		Base:   b.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	s.app.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	s.navigate("/collection/" + id + "?type=" + strconv.Itoa(int(typ)))
	waitFrames(t, tt, "容器元信息加载完", func() bool {
		return s.collection != nil && s.app.Collection(s.collection.ref).Loaded
	})
	return b, s, tt
}

// TestTheSeasonPageHasNoSortDropdown 钉住复审第 7 条。
//
// 合集 / 系列的内容接口只有一个 `sort_reverse`（倒序），没有收藏夹那三个
// `order`；而这一层还没把 `sort_reverse` 接下去。照收藏夹那三档画的话，三档
// 发出去的请求**一模一样**——用户选了「最多播放」，回来的还是同一批、同一个
// 顺序（死控件，而且界面上完全看不出来）。
//
// 🔴 **判据是「那个下拉压根不在」**：留两档的话其中一档仍然是假的（见
// `collectionSortSupported`）。
func TestTheSeasonPageHasNoSortDropdown(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  bili.CollectionType
		id   string
	}{
		{"合集", bili.CollectionAlbum, "1227671"},
		{"系列", bili.CollectionSeries, "534501"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, s, tt := collectionShellTyped(t, c.typ, c.id, mediaOf(1, "第一集"))
			waitSongs(t, tt, s, 1)
			tt.Frame()

			for _, label := range []string{"排序：", "最近收藏", "最多播放", "最近投稿"} {
				if tt.HasText(label) {
					t.Errorf("%s页上摆着排序控件（%q），而这一类只有一个 sort_reverse、"+
						"那三档发出去的请求一模一样", c.name, label)
				}
			}
		})
	}
}

// TestTheSeriesPageHidesTheOwnerOnlyActions 钉住复审第 5 条里「系列→隐藏」那一半。
//
// 「清除失效内容」「删除收藏夹」是收藏夹的那套动作（`/x/v3/fav/folder/*`）。
// 系列页上摆出来的话，按下去只会得到一句 B 站的「请求错误」，而用户以为是自己
// 的账号出了问题。合集那边「删除收藏夹」同样不适用（合集不是收藏夹），但
// 「清除失效内容」在合集上也没有对应动作——两类的意思一样：**这几项不摆**。
//
// ⚠️ **登录态要给上**：不登录的话 `CanDeleteCollection` 恒 false，这一条会
// 假装通过（那两项本来就不摆）。
func TestTheSeasonPageHidesTheFolderOnlyActions(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  bili.CollectionType
		id   string
	}{
		{"合集", bili.CollectionAlbum, "1227671"},
		{"系列", bili.CollectionSeries, "534501"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, s, tt := collectionShellTypedAs(t, c.typ, c.id, "1", mediaOf(1, "第一集"))
			waitSongs(t, tt, s, 1)
			// ⚠️ **元信息里的创建者要是「我」**，不然判据根本走不到那两项。
			waitFrames(t, tt, "头图区标成自己建的", func() bool {
				info := s.app.Collection(s.collection.ref)
				return info.Loaded && info.OwnerMid == 1
			})
			if !s.app.CanDeleteCollection(s.collection.ref) {
				t.Fatalf("%s页没被判成「自己建的」（这个前提不成立的话，这一条会假通过）：%+v",
					c.name, s.app.Collection(s.collection.ref))
			}

			for _, label := range []string{"清除失效内容", "删除收藏夹"} {
				if chooseMore(t, tt, label) == nil {
					t.Errorf("%s页的 ⋯ 菜单里摆着 %q——那是收藏夹的动作，按下去只会得到一句 B 站的错",
						c.name, label)
				}
				_ = tt.Click("取消")
				settleFrame(tt)
			}
		})
	}
}

// TestTheSeasonStarSendsTheSeasonID 钉住复审第 5 条的主线：合集页点星标打的是
// `fav/season/fav` 且带 `season_id`。
func TestTheSeasonStarSendsTheSeasonID(t *testing.T) {
	b, s, tt := collectionShellTyped(t, bili.CollectionAlbum, "1227671", mediaOf(1, "第一集"))
	waitSongs(t, tt, s, 1)
	tt.Frame()

	// ⚠️ **那一句带的是「视频合集」**（不是「收藏夹」）：用户点的是合集，页面上也
	// 写着「视频合集」——写「收藏这个收藏夹」是在说另一件事。
	if err := tt.Click("收藏这个视频合集"); err != nil {
		t.Fatalf("点不了星标：%v（画面上的字：%q）", err, tt.Texts())
	}
	waitFrames(t, tt, "合集收藏请求发出去", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.seasonForms) > 0
	})

	b.mu.Lock()
	forms := append([]map[string][]string(nil), b.seasonForms...)
	folderHits := 0
	for _, p := range b.paths {
		if p == "/x/v3/fav/folder/fav" || p == "/x/v3/fav/folder/unfav" {
			folderHits++
		}
	}
	b.mu.Unlock()

	if folderHits != 0 {
		t.Errorf("合集页的星标打了 %d 次收藏夹那个接口——那一路把 season_id 当 media_id 发出去", folderHits)
	}
	if len(forms) == 0 {
		t.Fatal("一次合集收藏请求都没发")
	}
	if got := forms[0]["season_id"]; len(got) == 0 || got[0] != "1227671" {
		t.Errorf("season_id = %v，want [1227671]", got)
	}
}

// TestTheSeasonPageBatchBarHasNoDelete 钉住「合集 / 系列页的批量操作条上不摆
// 批量删除」——那个动作只对收藏夹成立。
//
// ⚠️ **批量下载留着**：它按 bvid 走下载服务，和容器类型无关。
func TestTheSeasonPageBatchBarHasNoDelete(t *testing.T) {
	_, s, tt := collectionShellTyped(t, bili.CollectionAlbum, "1227671", mediaOf(1, "第一集"))
	waitSongs(t, tt, s, 1)
	tt.Frame()

	if err := tt.Click("多选"); err != nil {
		t.Fatalf("点不了多选：%v（画面上的字：%q）", err, tt.Texts())
	}
	settleFrame(tt)

	if tt.HasText("批量删除") {
		t.Errorf("合集页的批量操作条上摆着「批量删除」——合集里删不了内容（那一路注定失败）")
	}
	if !tt.HasText("批量下载") {
		t.Errorf("合集页的批量操作条上连「批量下载」都没了，文字有 %q", tt.Texts())
	}
}

// collectionShellTypedAs 是 `collectionShellTyped` 加载一个登录态。
func collectionShellTypedAs(t *testing.T, typ bili.CollectionType, id, mid string, medias ...map[string]any) (*favBili, *Shell, *ui.Tester) {
	t.Helper()
	b := newCollectionBili(t)
	b.setMedias(medias...)
	s, tt := newShell(t, withSettings(app.DefaultSettings("")))
	s.app.SetWake(s.wake.invalidate)

	transport := &app.CredentialTransport{
		Base:   b.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	s.app.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	s.app.MountLogin(app.NewMemoryStore("season-ops-test"), cli)
	s.app.TestOnlyAdoptLogin(mid)

	s.navigate("/collection/" + id + "?type=" + strconv.Itoa(int(typ)))
	waitFrames(t, tt, "容器元信息加载完", func() bool {
		return s.collection != nil && s.app.Collection(s.collection.ref).Loaded
	})
	return b, s, tt
}

// TestTheSeasonPageHeaderIsFilledIn 钉住复审第 4 条在界面上的样子：合集 / 系列的
// 头图区**真的有**标题和条数（不是停在占位），而且一次 `folder/info` 都不打。
//
// 原来头图区那一趟无条件打收藏夹那个接口——对合集 / 系列的 id 必回 -400：头图区
// 永远是「正在加载收藏夹信息」的骨架，而且每开一次页面弹一句「打不开这个收藏夹」。
func TestTheSeasonPageHeaderIsFilledIn(t *testing.T) {
	for _, c := range []struct {
		name  string
		typ   bili.CollectionType
		id    string
		title string
		kind  string
		count int
	}{
		{"合集", bili.CollectionAlbum, "1227671", "一个合集", "视频合集", 3},
		{"系列", bili.CollectionSeries, "534501", "一个系列", "视频系列", 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			medias := []map[string]any{mediaOf(1, "第一集"), mediaOf(2, "第二集"), mediaOf(3, "第三集")}
			b, s, tt := collectionShellTyped(t, c.typ, c.id, medias[:c.count]...)
			waitSongs(t, tt, s, c.count)
			tt.Frame()

			wantText(t, tt, c.title)
			wantText(t, tt, c.kind+" • "+strconv.Itoa(c.count)+" 条视频")
			// 骨架那一句不该还挂着（那是「还没拉到」那一档）。
			dontWantText(t, tt, "正在加载收藏夹信息")

			if got := b.hitsOf("/x/v3/fav/folder/info"); got != 0 {
				t.Errorf("%s页的头图区打了 %d 次 folder/info——那个接口对它只会回错", c.name, got)
			}
			// 也不该弹那句「打不开」。
			if n, ok := s.app.TakeCollectionNotice(s.collection.ref); ok {
				t.Errorf("%s页弹了一句 %q（头图区其实拉到了）", c.name, n.Text)
			}
		})
	}
}

// TestTheSeriesPageHasNoStarButton 钉住「系列页不摆星标」。
//
// B 站没有「收藏一个系列」这个动作（上游 `operation/index.tsx` 对
// `VideoSeries` 就不画它）。摆出来按下去只会得到一句看不懂的错，而用户以为
// 是账号出了问题。合集那边有这个动作（`fav/season/fav`），照旧画。
func TestTheSeriesPageHasNoStarButton(t *testing.T) {
	for _, c := range []struct {
		name     string
		typ      bili.CollectionType
		id       string
		wantStar bool
	}{
		{"合集", bili.CollectionAlbum, "1227671", true},
		{"系列", bili.CollectionSeries, "534501", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, s, tt := collectionShellTyped(t, c.typ, c.id, mediaOf(1, "第一集"))
			waitSongs(t, tt, s, 1)
			tt.Frame()

			has := tt.HasText("收藏这个收藏夹") || tt.HasText("取消收藏")
			if has != c.wantStar {
				t.Errorf("%s页的星标：在=%v，want %v", c.name, has, c.wantStar)
			}
		})
	}
}
