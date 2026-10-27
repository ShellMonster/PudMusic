package ui

// 侧栏收藏夹右键菜单里那四件事（播放 / 添加到播放列表 / 删除 / 取消收藏）。
//
// 这一组盯的是**接线**：菜单项按下去，App 那一摊真的做事，而且界面上说得清
// 成了还是没成。B 站那边是一台假服务器（`favBili`）——验的是「我们把该发的
// 请求发出去了、并且把该拿的数据拿回来」，不是「B 站真的删掉了那个收藏夹」。

import (
	"slices"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// favWireShell 建一个侧栏有两组收藏夹、并且歌单那一摊也挂着的外壳。
//
// ⚠️ 歌单那一摊**必须挂上**：播放 / 添加到播放列表都要先拉内容（那是网络往返），
// 而拉内容的客户端就是 `MountCollections` 递进来的那一个。
func favWireShell(t *testing.T, created, collected int) (*Shell, *app.App, *ui.Tester, *favBili) {
	t.Helper()
	s, a, tt, _ := favShell(t, created, collected)
	b := newCollectionBili(t)
	b.setMedias(mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	cli := collectionClient(t, b)
	// ⚠️ 取流也要挂：**播放**走的是 `App.PlayFrom`，而它起播失败会把整个队列
	// 原样换回去（见 queue.go 的 PlayFrom）。没挂取流的话队列永远是空的，
	// 验到的就不是「收藏夹被放进队列了」而是「起播没成功」。
	a.MountStreaming(app.Streaming{Client: cli, Media: b.Server.Client(), Quality: bili.AudioHigh})
	a.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	tt.Frame()
	return s, a, tt, b
}

// waitQueue 跑到队列里有 n 项为止。
func waitQueue(t *testing.T, a *app.App, n int) {
	t.Helper()
	waitFor(t, "队列里有歌", func() bool { return a.Queue().Len() == n })
}

// TestPlayingAFavoriteFromTheSidebarReallyPutsItInTheQueue 「播放」这一项
// 不是一句「还没接」。
func TestPlayingAFavoriteFromTheSidebarReallyPutsItInTheQueue(t *testing.T) {
	_, a, tt, _ := favWireShell(t, 2, 0)

	if err := tt.RightClick("我建的 1"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Fatal(err)
	}

	waitQueue(t, a, 2)
	got := a.Queue().Items()
	// 认的是**稿件 id**：收藏夹里的条目没有 cid，而界面那一行画的是 bvid。
	if got[0].Bvid != "BV11" || got[1].Bvid != "BV12" {
		t.Errorf("放出来的是 %v，want BV11 / BV12", got)
	}
	// 说的是一句人话，不是一句「还没接」。
	if tt.HasText("还没接收藏夹接口") {
		t.Errorf("播放收藏夹按下去说「还没接」，文字有 %q", tt.Texts())
	}
}

// TestAddingAFavoriteToThePlaylistAppendsIt 「添加到播放列表」追加而不换掉。
func TestAddingAFavoriteToThePlaylistAppendsIt(t *testing.T) {
	_, a, tt, _ := favWireShell(t, 2, 0)

	// 先往队列里塞一首，验「添加」不是「换掉」。
	a.Queue().Append(app.Item{Source: "online", Type: "mv", Bvid: "BV99", Title: "原来那首"})

	if err := tt.RightClick("我建的 1"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加到播放列表"); err != nil {
		t.Fatal(err)
	}

	waitQueue(t, a, 3)
	got := a.Queue().Items()
	if got[0].Bvid != "BV99" {
		t.Errorf("原来那首被换掉了，队列头是 %v", got[0])
	}
}

// TestDeletingAFavoriteAsksFirst 删除是不可撤销的，所以先问一句。
//
// 两条一起钉：**框上写着撤不回来**（用户要在这一步知道会发生什么），而**点了
// 「删除」之后请求才发出去**。
func TestDeletingAFavoriteAsksFirst(t *testing.T) {
	_, a, tt, b := favWireShell(t, 3, 0)

	if err := tt.RightClick("我建的 2"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	if !tt.HasText("撤不回来") {
		t.Fatalf("框上没有说「撤不回来」，文字有 %q", tt.Texts())
	}
	if n := len(b.deletedFolders()); n != 0 {
		t.Fatalf("还没点确定就把删除请求发出去了（%d 次）", n)
	}

	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "删除请求到了 B 站", func() bool {
		return len(b.deletedFolders()) == 1
	})
	if got := b.deletedFolders(); got[0] != "2" {
		t.Errorf("删掉的是 %q，want 2（行上的那个 id）", got[0])
	}
	// 侧栏上那一行跟着没了。
	waitFrames(t, tt, "侧栏上那一行没了", func() bool {
		_, ok := tt.Find("我建的 2")
		return !ok
	})
	if n := len(a.SideFavorites(app.FavoritesCreated)); n != 2 {
		t.Errorf("侧栏上还剩 %d 行，want 2", n)
	}
}

// TestCancellingTheDeleteKeepsTheFavorite 点「取消」什么也不该发生。
func TestCancellingTheDeleteKeepsTheFavorite(t *testing.T) {
	_, a, tt, b := favWireShell(t, 3, 0)

	if err := tt.RightClick("我建的 2"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	if n := len(b.deletedFolders()); n != 0 {
		t.Errorf("点了取消还是发了 %d 次删除请求", n)
	}
	if n := len(a.SideFavorites(app.FavoritesCreated)); n != 3 {
		t.Errorf("侧栏上只剩 %d 行，want 3", n)
	}
}

// TestUnfavoritingDoesNotAsk 「取消收藏」是**可撤销**的，所以不要确认框。
//
// 这一条钉的是一个取舍：删收藏夹要问，取消收藏不问——后者撤回来只要再点一次，
// 而点错之后还弹一个框只会更烦。
func TestUnfavoritingDoesNotAsk(t *testing.T) {
	_, a, tt, b := favWireShell(t, 0, 2)

	if err := tt.RightClick("我收的 1"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("取消收藏"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	// 没有框：点完立刻就发出去了。
	if n := len(b.deletedFolders()); n != 0 {
		t.Errorf("取消收藏却发了删除请求（%d 次）", n)
	}
	waitFor(t, "取消收藏的请求到了 B 站", func() bool { return b.isFaved() == false })
	waitFor(t, "那一行从侧栏上没了", func() bool {
		return len(a.SideFavorites(app.FavoritesCollected)) == 1
	})
	// 落盘的顺序里也不能再有它，否则下次拉全量它自己长回来
	// （`Favorites.Replace` 会拿存下的顺序去排，见 app/applySavedOrder）。
	order := a.SettingsSnapshot().SideMenuCollectionOrder
	if slices.Contains(order.Collected, int64(1)) {
		t.Errorf("落盘的顺序里还留着 1：%v", order.Collected)
	}
}
