// 「点一首歌」在队列上该做什么：照上游 Biu 的 `play()`（票 #608）。
//
// 上游那一条（`_research/biu/src/store/play-list.ts:556-583`）分三支：
//
//	正在放的就是这一首   → 暂停着就恢复，别的什么都不做
//	队列里已经有这一首   → 把当前项**定位过去**（`set({ playId: existItem.id })`），
//	                       不重复入队、不清空队列
//	队列里没有           → 追加到队尾并立即播
//
// 本项目原来只有第三支（`EnqueueAndPlayNow` 的注释里明写着「已经在队列里的时候
// 照样再加一份」）。票 #608 把第二支补上：用户把一批歌加进播放列表之后，回到
// 投稿 / 搜索结果里点其中一首，应该是「跳过去放它」，而不是把整条队列换成
// 「从这一首往后」——更不能把同一首歌塞进队列两遍。
package app_test

import (
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// TestEnqueueAndPlayNowSelectsAnItemThatIsAlreadyInTheQueue 是这一票的核心。
//
// 钉三件事：
//
//  1. **队列长度不变**——不再追加第二份；
//  2. **当前项指过去**——「下一首」从它后面接着走；
//  3. **放的是它**——用户按的是哪一首，响的就是哪一首。
func TestEnqueueAndPlayNowSelectsAnItemThatIsAlreadyInTheQueue(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)

	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().Title == "a" })

	// 点队列中间那一首：它已经在队列里。
	if err := a.EnqueueAndPlayNow(items(paths[1])[0]); err != nil {
		t.Fatalf("EnqueueAndPlayNow()：%v", err)
	}
	waitFor(t, 2*time.Second, "换到第二首", func() bool { return a.Snapshot().Title == "b" })

	got := a.Queue().Items()
	if len(got) != 3 {
		t.Fatalf("队列里有 %d 首，want 3（已经在队列里的那一首不该再加一份）", len(got))
	}
	if cur := a.Queue().CurrentIndex(); cur != 1 {
		t.Errorf("当前项是第 %d 项，want 1（定位到用户点的那一首）", cur)
	}
	if title := a.Snapshot().Title; title != "b" {
		t.Errorf("播放条上是 %q，want b", title)
	}
}

// TestEnqueueAndPlayNowAppendsASongThatIsNotInTheQueue 钉住另一支：
// 队列里没有它的时候照旧追加到队尾（队列 +1），并立即播它。
//
// ⚠️ **这一支和上面那一支是一条判据的两面**：把「已经在队列里」判错的两种
// 后果正好相反——判成「没有」是队列里多一份，判成「有」是点了没声音。
func TestEnqueueAndPlayNowAppendsASongThatIsNotInTheQueue(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)

	if err := a.PlayFrom(items(paths[0], paths[1])...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().Title == "a" })

	if err := a.EnqueueAndPlayNow(items(paths[2])[0]); err != nil {
		t.Fatalf("EnqueueAndPlayNow()：%v", err)
	}
	waitFor(t, 2*time.Second, "换到第三首", func() bool { return a.Snapshot().Title == "c" })

	got := a.Queue().Items()
	if len(got) != 3 {
		t.Fatalf("队列里有 %d 首，want 3（新的一首追加到队尾）", len(got))
	}
	if got[2].Path != paths[2] {
		t.Errorf("队尾是 %q，want c", got[2].Path)
	}
	if cur := a.Queue().CurrentIndex(); cur != 2 {
		t.Errorf("当前项是第 %d 项，want 2（刚追加的那一首）", cur)
	}
}

// TestEnqueueAndPlayNowLocatesTheDuplicateNotTheFirstCopy 钉住「同一首在队列里
// 出现两遍」那一档：定位到**用户点的那一份**（下标），不是第一个能认出来的。
//
// 队列里同一首排两遍是能造出来的（老的 `EnqueueAndPlayNow` 就会这么干，落盘的
// 队列里也可能有），而那一档的错法很隐蔽：`selectAt` 指错一份的话，「下一首」
// 从错的位置往后走，用户按了一次下一首却听到了队列中段的歌。
func TestEnqueueAndPlayNowLocatesTheDuplicateNotTheFirstCopy(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 3)

	// 手动摆一条含重复项的队列：[a, b, a]。
	a.Queue().Reset(items(paths[0], paths[1], paths[0])...)
	if err := a.PlayAt(1); err != nil { // 先在 b 上
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "先在第二首上", func() bool { return a.Snapshot().Title == "b" })

	// 点第三项那一份 a：队列里已经有 a（第一项），但用户点的是第三项。
	if err := a.PlayAt(2); err != nil {
		t.Fatalf("PlayAt(2)：%v", err)
	}
	waitFor(t, 2*time.Second, "换到第三项", func() bool { return a.Snapshot().Title == "a" })

	if cur := a.Queue().CurrentIndex(); cur != 2 {
		t.Errorf("当前项是第 %d 项，want 2（用户点的是第三项那一份）", cur)
	}
	if n := len(a.Queue().Items()); n != 3 {
		t.Errorf("队列里有 %d 首，want 还是 3", n)
	}
}

// TestEnqueueAndPlayNowDoesNotClearTheQueueAroundIt 钉住「点一首歌不清空队列」。
//
// 用户原来的队列里有三首，点一首**不在**里面的：队列变成四首，原来那三首
// 一个不少、顺序不变——这是这一票和「从这一首往后听」最关键的那条分界。
func TestEnqueueAndPlayNowDoesNotClearTheQueueAroundIt(t *testing.T) {
	a := newApp(t)
	paths := copies(t, 4)

	if err := a.PlayFrom(items(paths[0], paths[1], paths[2])...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "第一首开始播", func() bool { return a.Snapshot().Title == "a" })

	if err := a.EnqueueAndPlayNow(items(paths[3])[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "换到第四首", func() bool { return a.Snapshot().Title == "d" })

	got := a.Queue().Items()
	if len(got) != 4 {
		t.Fatalf("队列里有 %d 首，want 4（原来那三首必须留着）", len(got))
	}
	for i, want := range []string{paths[0], paths[1], paths[2], paths[3]} {
		if got[i].Path != want {
			t.Errorf("第 %d 项是 %q，want %q", i, got[i].Path, want)
		}
	}
}

// TestEnqueueAndPlayNowPlaysTheQueuedCopyNotTheCallersCopy 钉住「播的是队列里
// 那一份，不是调用方递进来的这一份」（票 #608 的评审补的）。
//
// 用户点的那个列表行，数据可能来自一个**简版**接口（只有 bvid + 标题），而队列里
// 那一份是当初入队时的完整数据。放简版的话，在线取流会缺分 P id / 封面，症状是
// 「这一首明明在队列里，点它却报错」。
//
// 判据取的是**播放状态里的标题**（`App.play` 在那一条路上写的是它拿到的那个
// item 的 Title）：队列里那一份写的是「队列里的标题」，调用方递进来的写的是
// 「列表上的标题」——播的是哪一份，读出来就是哪一个。
func TestEnqueueAndPlayNowPlaysTheQueuedCopyNotTheCallersCopy(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))
	a := newOnlineApp(t, f, t.TempDir())

	if err := a.PlayFrom(onlineItem("BV1xx", "123", "队列里的标题")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "队列里的标题")

	// 调用方递进来的是同一首（同一个 bvid）的**简版**：标题不一样、cid 空着。
	thin := onlineItem("BV1xx", "", "列表上的标题")
	if err := a.EnqueueAndPlayNow(thin); err != nil {
		t.Fatalf("EnqueueAndPlayNow()：%v", err)
	}
	playing(t, a, "队列里的标题")

	if n := len(a.Queue().Items()); n != 1 {
		t.Errorf("队列里有 %d 首，want 1（同一首不该再加一份）", n)
	}
	if got := a.Snapshot().Title; got == "列表上的标题" {
		t.Errorf("播的是调用方递进来的简版（%q），应该是队列里那一份", got)
	}
}
