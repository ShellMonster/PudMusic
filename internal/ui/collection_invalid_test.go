package ui

// 「失效的条目」在收藏夹页上的样子（票 #624）。
//
// # 这一组钉的是**整条链路**
//
//	B 站那个 `attr` 字段	`bili.Media.Attr` → `bili.Media.Invalid`
//	进队列项	`app.ItemOfMedia`（`Item.Invalid`）
//	画在列表里	`list` 那一档：灰掉 + 一枚「已失效」标签
//	菜单里	播放入口是**灰的**，末尾有一句为什么
//	进队列	**滤掉**——所以应用**不为它发取流请求**
//
// 这五样是一条链，而链子断在哪一节的症状都不同（标签没了 / 菜单能按了 /
// 队列里多了一首 / 白跑了一趟取流）。所以这一组从**界面上看**钉住整条链，
// 而不是在每一层各写一条单测——那样链子断在中间时，两边的单测都还是绿的。
//
// ⚠️ **全部走内存里的假 B 站**（`favBili` / `streamFixture`），一条真接口都不打。

import (
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// invalidMediaOf 造一条**失效**的内容（`attr=1`）。
//
// ⚠️ **attr=1 是「其他原因删除」**（B 站那一档），而 `attr=9` 是「UP 自己删除」
// ——判据是「不是 0」，两档都算失效（见 `bili.Media.Invalid`）。
func invalidMediaOf(id int, title string) map[string]any {
	m := mediaOf(id, title)
	m["attr"] = 1
	return m
}

// TestAnInvalidEntryIsMarkedInTheCollectionPage 钉住「标出来」。
//
// 🔴 **判据是画面上有那枚标签**：失效的条目**照画**（不画的话用户会发现
// 「收藏夹里的歌自己少了几首」而且不知道为什么），但要**灰掉 + 标出来**。
func TestAnInvalidEntryIsMarkedInTheCollectionPage(t *testing.T) {
	_, s, tt := collectionShell(t, "12345",
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
	)
	waitSongs(t, tt, s, 2)
	tt.Frame()

	if !tt.HasText(list.InvalidLabel) {
		t.Fatalf("失效的那一条没有标出来（画面上没有 %q）：文字有 %q",
			list.InvalidLabel, tt.Texts())
	}
	// ⚠️ **正常的那一条不许被标**：一次标多了就是噪音，而用户会以为
	// 「收藏夹里大部分东西都是坏的」。
	if n := countText(tt, list.InvalidLabel); n != 1 {
		t.Errorf("画面上 %q 出现了 %d 次，want 1（只有失效的那一条该标）", list.InvalidLabel, n)
	}
}

// TestAnInvalidEntryHasNoPlayEntryInTheCollectionPage 钉住「点之前就不给播放入口」。
//
// ⚠️ **菜单项还在**（用户看得到这一份菜单），但它是**灰的**——而末尾有一句
// 为什么。按不动而不说为什么等于让用户猜。
func TestAnInvalidEntryHasNoPlayEntryInTheCollectionPage(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", invalidMediaOf(2, "失效的一首"))
	waitSongs(t, tt, s, 1)
	tt.Frame()

	if err := tt.RightClick("失效的一首"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("立即播放"); err == nil {
		t.Error("失效的那一条，「立即播放」竟然能按")
	}
	items := tt.Menu()
	last := items[len(items)-1]
	if !strings.Contains(last, "失效") {
		t.Errorf("菜单末尾那句话是 %q，没有说清「已经失效」", last)
	}
}

// TestPlayingAllSkipsTheInvalidEntries 钉住「播放全部」滤掉失效的那些。
//
// 🔴 **这是这一票真正的目的**：失效的条目**不该进队列**——进去了应用就要为
// 它白跑一趟带签名的取流往返（实测 200~400ms），而那一趟注定失败。用户等的
// 就是这一串串超时。
//
// 判据是**队列里有几首**。
func TestPlayingAllSkipsTheInvalidEntries(t *testing.T) {
	medias := []map[string]any{
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
		mediaOf(3, "也正常的一首"),
	}
	_, s, tt := collectionShell(t, "12345", medias...)
	waitSongs(t, tt, s, 3)
	tt.Frame()

	// ⚠️ **取流要另挂**：这个收藏夹的夹具只伺服收藏夹内容，不伺服取流
	// （`/x/player/pagelist` 那边它 404）。
	mountQueueStreaming(t, s.app)

	if err := tt.Click("播放全部"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "队列换成这一页的歌", func() bool {
		got := s.app.Queue().Items()
		return len(got) == 2
	})
	got := s.app.Queue().Items()
	if len(got) != 2 {
		t.Fatalf("队列里有 %d 首，want 2（失效的那首不该进来）：%+v", len(got), got)
	}
	for _, it := range got {
		if it.Bvid == "BV12" {
			t.Fatal("失效的那首进了队列")
		}
	}
}

// TestPlayingAllSaysHowManyWereSkipped 钉住「跳过了要说一声」。
//
// 🔴 **用户数得出来**：列表里画着、标着「已失效」的那些，一首都不在队列里。
// 不说的话，用户看到的是「这一页有 3 首，而只放了 2 首」，而界面上一个字的
// 解释都没有——那几个字只差一个括号，却决定用户会不会去找那首少的歌。
func TestPlayingAllSaysHowManyWereSkipped(t *testing.T) {
	medias := []map[string]any{
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
	}
	_, s, tt := collectionShell(t, "12345", medias...)
	waitSongs(t, tt, s, 2)
	tt.Frame()

	mountQueueStreaming(t, s.app)

	if err := tt.Click("播放全部"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "说清跳过了几首", func() bool {
		for _, tx := range tt.Texts() {
			if strings.Contains(tx, "失效") && strings.Contains(tx, "没放进来") {
				return true
			}
		}
		return false
	})
}
