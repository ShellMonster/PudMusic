package app

import (
	"os"
	"path/filepath"
	"testing"
)

// `App.IsPlaying`：列表上「正在播放」那一行的判据（对照审计的 A24）。
//
// 这一组全部在**队列这一层**跑，开不了播放器也不需要——它量的是「哪一首正在
// 放」这个问题本身答得对不对，而答案只和队列有关（`queue.InsertNext` 那一支
// 落位、以及 `PlayAt` 换当前项，都不需要真的出声）。

// onlineItemOf 造一个在线条目（B 站的 mv），和 `onlineItem` 那一个参数表对齐。
func onlineItemOf(bvid, title string) Item {
	return Item{Source: "online", Type: "mv", Bvid: bvid, Cid: "1", Title: title}
}

// TestIsPlayingFindsTheSongWhereverTheQueuePutIt 是这一层最要紧的一条。
//
// 队列是 `[Z, A]`、正在放的是 **A**（当前项下标 1），而 A 在别处的下标可以是
// 任何数。按下标认的话答案会跟着队列的位置走——而界面上那一行的位置是列表
// 自己的，两套位置永远不会对上。
func TestIsPlayingFindsTheSongWhereverTheQueuePutIt(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	z, songA := onlineItemOf("BV0", "Z"), onlineItemOf("BV1", "A")
	a.Queue().Reset(z, songA)
	if _, ok := a.Queue().Move(1); !ok { // 当前项落在 A（队列第 2 首）
		t.Fatal("队列走不到第二首")
	}

	if !a.IsPlaying(songA) {
		t.Errorf("正在放的是 A，IsPlaying(A) = false")
	}
	if a.IsPlaying(z) {
		t.Errorf("IsPlaying(Z) = true，而 Z 不是正在放的那首")
	}
	// 队列里根本没有的那一首：不是正在放。
	if a.IsPlaying(onlineItemOf("BV9", "不在队列里")) {
		t.Errorf("IsPlaying 对一首不在队列里的歌说了 true")
	}
}

// TestIsPlayingSurvivesTheQueueBeingRearranged 钉住「重排了也还对」。
//
// 「下一首播放」插在当前**后面**：当前项的下标不变，而它后面那一整段全挪了
// 一格（见 `Queue.InsertNext`）。判据要是认下标，这里就露馅——插完之后答的
// 仍然是同一首，**但只有认歌才答得对**（插进来的那一首不属于正在放）。
func TestIsPlayingSurvivesTheQueueBeingRearranged(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	first := onlineItemOf("BV1", "第一首")
	mid := onlineItemOf("BV2", "第二首")
	last := onlineItemOf("BV3", "第三首")
	// Reset 把当前项落在第一首（队列非空，所以下面插队不会起播）。
	if _, ok := a.Queue().Reset(first, mid, last); !ok {
		t.Fatal("Reset 之后连第一首都没有")
	}

	inserted := onlineItemOf("BV4", "插进来的")
	if err := a.EnqueueNext(inserted); err != nil {
		t.Fatalf("排下一首失败：%v", err)
	}
	// 队列变了样：插进来的排在当前项后面，而当前项还是第一首。
	if got := a.Queue().Items(); len(got) != 4 || got[1].Bvid != "BV4" {
		t.Fatalf("队列不是「第一首、插进来的、…」：%v", got)
	}

	if !a.IsPlaying(first) {
		t.Errorf("插了一首之后，正在放的那首不再被认出来了——" +
			"症状是列表上的高亮跳到了旁边那一首，而声音没换")
	}
	if a.IsPlaying(inserted) {
		t.Errorf("插进来的那一首被说成正在放了，而它是**下一首**")
	}
}

// TestIsPlayingIsAboutTheSongNotAboutThatCopy 钉住判据是**同一首**，不是同一份。
//
// 同一首歌在队列里排了两份（B 站的内容列表不判重，抽屉才去重），而当前项可能
// 指着**第二份**。认「同一份」的话界面会高亮第一份——用户点的、听的明明是
// 第二份，而两首的歌名一模一样，界面上看不出错。
func TestIsPlayingIsAboutTheSongNotAboutThatCopy(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	first := onlineItemOf("BV1", "同一首")
	second := onlineItemOf("BV1", "同一首") // 另一份：结构体值拷贝，下标不同
	a.Queue().Reset(first, second)
	if _, ok := a.Queue().Move(1); !ok {
		t.Fatal("队列走不到第二份")
	}

	if !a.IsPlaying(first) {
		t.Errorf("当前项指着第二份，而 IsPlaying(第一份) = false——" +
			"按整项比的话两首都认不出，界面上两行都不高亮")
	}
}

// TestIsPlayingSaysNoForAnItemItCannotName 认不出是哪一首的一律说「不在放」。
//
// 空标识是「还没接来源的占位项」。把它当正在放的话，界面上会出现一个说不清
// 自己在指哪里的高亮——而那种高亮比没有更坏。
func TestIsPlayingSaysNoForAnItemItCannotName(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	nameless := Item{Source: "online", Type: "mv", Title: "还没有来源的一项"}
	a.Queue().Reset(nameless)
	if _, ok := a.Queue().Move(0); !ok {
		t.Fatal("队列走不到第一项")
	}

	if a.IsPlaying(nameless) {
		t.Errorf("认不出是哪一首的一项被说成正在放了")
	}
}

// TestIsPlayingSaysNoWhenNothingIsPlaying 空队列、没有当前项时一律 false。
func TestIsPlayingSaysNoWhenNothingIsPlaying(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	if a.IsPlaying(onlineItemOf("BV1", "一首歌")) {
		t.Errorf("还没有放任何东西，IsPlaying 却说了 true")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UP 主从队列项一路走到界面上
// ─────────────────────────────────────────────────────────────────────────────

// ownerTestFLAC 是一份能真的放起来的测试素材。
//
// ⚠️ 不复用 `app_test.go` 里那个 `flac`：那一份在**外部**测试包
// （`package app_test`），而这个文件在包内（它要碰 `Queue.Reset` 这些内部
// 形状）。两个包同名不冲突，可同名也不共享。
func ownerTestFLAC(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	return p
}

// TestPlayHandsTheUPOwnerToTheState 盯 `play` 真的把 UP 名交给界面。
//
// 播放条上那一行 UP 名读的是 `State.Artist` / `State.ArtistMid`，而它们只在
// `play` 这一处被填。漏了这一步的症状是「每一页的队列项都带着 UP 主，可播放条
// 上那一行就是不画」——数据全对，只差最后一跳。
//
// ⚠️ 这里拿一个**本地文件配上一个 UP 名**来构造队列项。真实数据里本地项不带
// UP 名（`Item.Artist` 那段），而这一条要量的是 `play` 这一跳本身：它对
// 每一项一视同仁，给什么传什么。
func TestPlayHandsTheUPOwnerToTheState(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	it := LocalItem(ownerTestFLAC(t))
	it.Artist, it.ArtistMid = "某个 UP 主", 4242
	if err := a.Enqueue(it); err != nil {
		t.Fatalf("放不了：%v", err)
	}

	st := a.Snapshot()
	if st.Artist != "某个 UP 主" {
		t.Errorf("状态里的 UP 名是 %q，want %q——播放条上那一行整个不画", st.Artist, "某个 UP 主")
	}
	if st.ArtistMid != 4242 {
		t.Errorf("状态里的 UP 编号是 %d，want 4242——名字显示得出来却点不动", st.ArtistMid)
	}
}

// TestTheUPOwnerIsClearedWithTheTrack 盯名字和编号**一起**清干净。
//
// 只清名字的话，界面上「没有 UP 名」那一行不画了，而下一首（同一套接线）
// 的名字写进去之前那半帧里编号还指着上一个 UP 主——用户点进的是**别人的空间**，
// 而界面上看不出任何异样。
func TestTheUPOwnerIsClearedWithTheTrack(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	it := LocalItem(ownerTestFLAC(t))
	it.Artist, it.ArtistMid = "某个 UP 主", 4242
	if err := a.Enqueue(it); err != nil {
		t.Fatalf("放不了：%v", err)
	}

	a.ClearTrack()
	st := a.Snapshot()
	if st.Artist != "" || st.ArtistMid != 0 {
		t.Errorf("停下来之后还留着 UP 名 %q / 编号 %d，want 空串和 0",
			st.Artist, st.ArtistMid)
	}
}
