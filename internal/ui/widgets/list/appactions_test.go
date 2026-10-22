package list

import (
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
)

// `AppActions` 那两份新接线的**落位**（对照审计的 A9 和 A24）。
//
// 这一组不用桩：`AppActions` 接的就是 `*app.App`，而「插在当前后面」和
// 「哪一首正在放」这两个答案都只有 `app` 那边答得对。所以这里拿一个真的 App
// 摆一条真的队列，断言菜单里按一下之后队列的**顺序**、以及高亮认的是不是
// **那一首**。
//
// 摆的是本地项（`app.LocalItem`），理由是这一层不碰网络：判重标识、插入位置、
// 「哪一首在放」这三件事和它是本地还是在线无关，而本地项能真的排进队列而
// 不需要去开播放器。

// seedQueue 摆一条三首的队列并让第一首成为当前项。
//
// ⚠️ 走 `Queue.Reset` 而不是 `Enqueue`：`Reset` 只摆不播，而 `Enqueue` 见到空队列
// 会**直接起播**——这一层没有播放器，起播会一路走到「这一首还不能放」。
func seedQueue(t *testing.T, a *app.App, names ...string) []app.Item {
	t.Helper()
	items := make([]app.Item, len(names))
	for i, n := range names {
		items[i] = app.LocalItem("/" + n + ".flac")
	}
	if _, ok := a.Queue().Reset(items...); !ok {
		t.Fatal("队列摆不进去")
	}
	return items
}

// TestAppActionsPlayNextLandsRightAfterTheCurrentOne 盯「下一首播放」接的是
// `EnqueueNext` 而不是 `Enqueue`。
//
// ⚠️ 两个的症状在界面上**一模一样**（按下去都有反应），差的是一小时之后：
// 接成接队尾的话，用户要等整张列表放完才听得见这一首。所以这里断的是队列的
// **顺序**，不是「有没有变」。
func TestAppActionsPlayNextLandsRightAfterTheCurrentOne(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	items := seedQueue(t, a, "一", "二", "三")

	acts := AppActions(a, nil)
	inserted := app.LocalItem("/插进来的.flac")
	acts.PlayNext(inserted)

	got := a.Queue().Items()
	if len(got) != 4 {
		t.Fatalf("按了一下，队列里是 %d 首，want 4 首", len(got))
	}
	if got[1].Identity() != inserted.Identity() {
		t.Errorf("插进去的那首排在第 %d 位（%v），want 第 2 位（当前项的后面）——"+
			"它接成了「添加到队列」", 2, ids(got))
	}
	// 当前项没动：用户正在听的那首还在放。
	if cur, ok := a.Queue().Current(); !ok || cur.Identity() != items[0].Identity() {
		t.Errorf("按了一下「下一首播放」，当前项变成了 %q", cur.Title)
	}
}

// TestAppActionsPlayNextSaysSoWhenItCannotQueue 钉住失败时有一句话。
//
// 「按了没反应」和「按了但队列没变」都更难解释：用户分不清是自己按错了还是
// 应用坏了。
//
// ⚠️ **这一条只在队列原本是空的时候才走得到错误那一支**（`App.EnqueueNext`：
// 队列非空时它只管插，不起播，见 queue.go）。所以这里摆的是一个**打不开的**
// 文件——用户听不见任何声音，而界面上得有一句话。
func TestAppActionsPlayNextSaysSoWhenItCannotQueue(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	var said []string
	acts := AppActions(a, func(s string, _ notice.Tone) { said = append(said, s) })
	acts.PlayNext(app.LocalItem(filepath.Join(t.TempDir(), "没有这个文件.mp3")))

	if len(said) == 0 {
		t.Errorf("排不进去却一句话也没有（队列是 %v）", ids(a.Queue().Items()))
	}
}

// TestAppActionsFindTheSongThatIsPlaying 盯高亮的判据接对了。
//
// 它量的是**接线**那一半（`AppActions.IsPlaying` 认不认得出正在放的那首）；
// 画出来的那一半在 `playing_test.go` 里。
//
// 队列是 [一, 二]，当前项挪到第二首。于是「正在放的是二」这件事和下标无关。
func TestAppActionsFindTheSongThatIsPlaying(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	items := seedQueue(t, a, "一", "二")

	acts := AppActions(a, nil)
	if acts.IsPlaying(items[1]) {
		t.Errorf("还没挪当前项就说第二首在放了（当前项是 %q）", playingTitle(a))
	}
	if _, ok := a.Queue().Move(1); !ok {
		t.Fatal("队列走不到第二首")
	}
	if !acts.IsPlaying(items[1]) {
		t.Errorf("当前项已经是第二首了，IsPlaying 却说不是")
	}
	if acts.IsPlaying(items[0]) {
		t.Errorf("第一首不是正在放的那首，IsPlaying 却说了是")
	}
}

// TestAppActionsIsPlayingIsReadEveryFrame 盯判据是**每帧现读**的。
//
// 闭包拿的是 `a`，所以换歌之后下一帧自然读到新的那一首。缓存住的话症状是
// 「列表上的高亮钉在上一首，而播放条上的歌名已经是新的了」——界面上两处各说
// 各话。
func TestAppActionsIsPlayingIsReadEveryFrame(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	items := seedQueue(t, a, "一", "二", "三")

	acts := AppActions(a, nil)
	if _, ok := a.Queue().Move(2); !ok {
		t.Fatal("队列走不到第三首")
	}
	if !acts.IsPlaying(items[2]) {
		t.Fatalf("一开始就没有认出来（队列是 %v）", ids(a.Queue().Items()))
	}

	// 换歌：队列整个换掉，当前项落在新队列的第一首。
	other := seedQueue(t, a, "四", "五")
	if acts.IsPlaying(items[2]) {
		t.Errorf("换歌之后还在说旧的那首在放")
	}
	if !acts.IsPlaying(other[0]) {
		t.Errorf("换歌之后认不出新的那一首")
	}
}

// ids 把队列里几首的判重标识列出来，好让报错那句话读得懂。
func ids(items []app.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Identity()
	}
	return out
}

// playingTitle 是当前项的歌名（没有当前项时是空串）。
func playingTitle(a *app.App) string {
	it, ok := a.Queue().Current()
	if !ok {
		return ""
	}
	return it.Title
}
