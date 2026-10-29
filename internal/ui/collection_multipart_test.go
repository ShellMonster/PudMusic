// 收藏夹里的多 P 投稿（票 #683）。
//
// 收藏夹那个接口（`/x/v3/fav/resource/list`）**不给分 P id**，所以从收藏夹放的
// 那一首起播之前 `Item.Cid` 是空的，cid 是取流那一趟（`app.withCid`）才补出来
// 的。补出来的那一份原来只喂给了弹幕（`notePendingCid`），队列项自己没被更新
// ——于是分集抽屉里「正在播放」一行都标不出来（`partRowsFor` 的 active 计数是
// 0），要点一次别的集才恢复正常。
//
// 这一组走**整条链**：真的假 B 站（`favBili` 回收藏夹内容、`biliFixture` 回
// 多 P 列表和取流）→ 双击那一行 → `App.EnqueueAndPlayNow` → 补 cid → 队列项
// 写回。断的是「用户从收藏夹点一首多 P，分集抽屉立刻认出正在放的是哪一集」，
// 不是「某个函数被调到了」。
package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// binaryTester 备一台**回多 P 列表**的假 B 站，挂到 app 上。
//
// `mountQueueStreaming` 那一台只回一集（它是别的票的夹具），而这一组要的是
// 「分集抽屉里有几行可比」。素材用真的 FLAC（`newBiliFixture` 自己读好的那一份），
// 所以起播走的是真的解码链。
func mountMultiPartStreaming(t *testing.T, a *app.App) {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.setVideoPages(multiPages())
	a.MountStreaming(app.Streaming{Client: f.client(), Media: f.Client(), TempDir: t.TempDir()})
}

// doubleClickRow 双击画面上那一行（收藏夹页的播放手势）。
//
// ⚠️ 单击只是选中，双击才播——这是 `widgets/list` 的 `RowDoubleClicked`
// （和 `collection_invalid_test.go` 里那条双击用例同一个手势）。
func doubleClickRow(t *testing.T, tt *mygoui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到那一行 %q，文字有 %q", title, tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
}

// TestPlayingAMultiPartFromTheCollectionWritesTheCidBack 是缺陷 D 的钉子。
//
// 收藏夹里那一首**没有 cid**（接口不给），双击放起来之后：
//
//  1. 队列当前项的 cid 不再是空串（写回来了）；
//  2. `partRowsFor` 里**正好一行** active（分集抽屉认得出正在放的是哪一集）。
//
// ⚠️ 第 2 条是用户报的那句话本身（「点里面的歌单播放不了」里的那一半：
// 抽屉里没有任何一行标着正在放，用户不知道自己在听哪一集）。
func TestPlayingAMultiPartFromTheCollectionWritesTheCidBack(t *testing.T) {
	b := newCollectionBili(t)
	b.setMedias(mediaOf(1, "多 P 的一首"))
	_, s, tt := collectionShellFrom(t, b, nil, "", "12345")
	waitSongs(t, tt, s, 1)
	tt.Frame()

	mountMultiPartStreaming(t, s.app)

	doubleClickRow(t, tt, "多 P 的一首")
	waitFrames(t, tt, "多 P 那一首放起来", func() bool { return s.app.Snapshot().HasTrack })
	waitFrames(t, tt, "分集问回来", func() bool { return len(s.app.Parts()) == 3 })

	cur, ok := s.app.CurrentItem()
	if !ok {
		t.Fatal("放起来了却没有当前项")
	}
	if cur.Cid == "" {
		t.Fatalf("起播补出来的 cid 没写回队列项（当前项：%+v）", cur)
	}
	// 队列里**还是那一项**：写回是就地补 cid，不是插一项新的。
	if n := len(s.app.Queue().Items()); n != 1 {
		t.Errorf("队列里有 %d 项，want 1（补 cid 不该改变队列形状）", n)
	}

	active := 0
	for _, row := range partRowsFor(s.app.Parts(), cur) {
		if row.active {
			active++
		}
	}
	if active != 1 {
		t.Errorf("分集列表里有 %d 行标着正在放，want 1（当前项 cid=%q，分集有 %d 条）",
			active, cur.Cid, len(s.app.Parts()))
	}
}

// TestWritingBackTheCidDoesNotSplitTheDrawerRow 钉住缺陷 D 和缺陷 A 的交界。
//
// 补 cid 会让那一项的身份从 `mv:<bvid>` 变成 `mv:<bvid>|<cid>`（见 `trackKey`），
// 而抽屉的行身份就是身份——处理不好的话「起播」那一下会在抽屉里多出一行
// （旧身份那一行还在缓存里），或者高亮丢掉（新的身份和旧的选中状态对不上）。
//
// 判据是**用户看得见的两件事**：抽屉里只画一行、顶上计数是 (1)。
func TestWritingBackTheCidDoesNotSplitTheDrawerRow(t *testing.T) {
	b := newCollectionBili(t)
	b.setMedias(mediaOf(1, "多 P 的一首"))
	_, s, tt := collectionShellFrom(t, b, nil, "", "12345")
	waitSongs(t, tt, s, 1)
	tt.Frame()

	mountMultiPartStreaming(t, s.app)

	// ⚠️ **先放、后开抽屉**：抽屉是模态浮层，开着的时候底下那一页点不动
	// （`viewQueueDrawer` 的 `back.Modal()`）——开着抽屉双击那一行只会关掉抽屉。
	doubleClickRow(t, tt, "多 P 的一首")
	waitFrames(t, tt, "那一首放起来而且 cid 补回来了", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && s.app.Snapshot().HasTrack && it.Cid != ""
	})

	// 抽屉是「播放条上那个键点开」的；打开之后它每一帧按队列快照重算行。
	if err := tt.Click("播放列表"); err != nil {
		t.Fatalf("点不了播放列表：%v（画面上的字：%q）", err, tt.Texts())
	}
	settleDrawer(t, s, tt)
	tt.Frame()

	if got := len(s.app.Queue().Items()); got != 1 {
		t.Fatalf("队列里有 %d 项，want 1（补 cid 不该让队列多一项）", got)
	}
	if got := len(s.queueRows); got != 1 {
		t.Errorf("抽屉里画着 %d 行，want 1（补 cid 之后那一行还是同一行）：%+v", got, s.queueRows)
	}
	if !tt.HasText("(1)") {
		t.Errorf("抽屉顶上写的不是 (1)，文字有 %q", tt.Texts())
	}
}
