// 收藏夹里 type=21（合集）那一行的「进内部」入口（票 #683，缺陷 B）。
//
// # 用户报的那句话
//
// 双击一条合集 → 队列 1 首、无播放、无错误；右键菜单里只有一句「这一条是合集/
// 剧集，没有音频流。打开里面的单集就能放」——**而没有任何一个能真的打开它的
// 入口**。用户照着那句话做不了任何事。
//
// # 为什么不改双击
//
// 双击是「播放」的语义（`widgets/list` 的 `rowPlay`），合集本来就播不了。
// 所以入口放在右键菜单里：那里已经摆着那一句说明，把它旁边补上一项真的能按的
// 动作最自然。
//
// # 合集 id 从哪儿来
//
// `bili.Media.ID` 对 type=21 是**合集 id**（`docs/fav/list.md`：`id | 内容 id |
// 视频稿件：avid / 音频：auid / 视频合集：视频合集id`），`app.ItemOfMedia` 把它
// 落在 `Item.CollectionID` 上。合集页要的正是这个 id（不是 avid）。
//
// ⚠️ **电影（type=24）不给这一项**：文档没说清 24 的 `id` 是什么（收藏夹里
// 它和合集同一档，而 24 是「合集」那种容器还是稿件本身没有旁证）。查不清就不
// 猜——菜单里照旧只有那一句说明。
package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// collectionMediaOf 造一条**合集**（type=21）的内容。
//
// `id` 是**合集 id**（不是 avid），而 `bvid` 是空串——合集不是稿件，没有 bvid。
// 这两点就是这一条用例要和真接口对齐的地方。
func collectionMediaOf(id int, title string) map[string]any {
	return map[string]any{
		"id": id, "type": 21, "title": title, "bvid": "",
		"cover": "", "duration": 0, "attr": 0, "page": 0,
		"upper": map[string]any{"name": "UP " + itoa(id), "mid": id},
	}
}

// TestTheCollectionRowOffersAWayIn 是本缺陷的主干那一条。
//
// 右键一条 type=21 的行：菜单里有「打开这个合集」，按下去**真的导航到**
// `/collection/{合集 id}?type=21`（也就是歌单页那一页）。
func TestTheCollectionRowOffersAWayIn(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", collectionMediaOf(777777, "一个合集"))
	waitSongs(t, tt, s, 1)
	tt.Frame()

	if err := tt.RightClick("一个合集"); err != nil {
		t.Fatalf("右键不了那一行：%v（文字有 %q）", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem("打开这个合集"); err != nil {
		t.Fatalf("菜单里没有「打开这个合集」：%v（菜单是 %q）", err, tt.Menu())
	}
	waitFrames(t, tt, "进到合集页", func() bool {
		return s.router.Location() == "/collection/777777?type=21"
	})

	// 进去之后这一页认得出来这是**合集**（不是收藏夹）。
	if s.collection == nil {
		t.Fatal("进了合集页却没有容器状态")
	}
	if got := s.collection.ref.Type; got != bili.CollectionAlbum {
		t.Errorf("进的那一页类型是 %d，want 21（合集）", got)
	}
}

// TestAnOrdinaryRowHasNoCollectionEntry 反面：普通投稿那一行**不许**摆这一项。
//
// 摆了的话那一项按下去会打开一个 id 对不上的页面（`Media.ID` 对 type=2 是 avid，
// 不是合集 id）。这条同时钉住「菜单的形状没有整体变」。
func TestAnOrdinaryRowHasNoCollectionEntry(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", mediaOf(1, "普通的一首"))
	waitSongs(t, tt, s, 1)
	tt.Frame()

	if err := tt.RightClick("普通的一首"); err != nil {
		t.Fatal(err)
	}
	for _, item := range tt.Menu() {
		if item == "打开这个合集" {
			t.Errorf("普通投稿那一行摆了「打开这个合集」：菜单是 %q", tt.Menu())
		}
	}
	_ = s
}

// TestTheCollectionEntrySurvivesTheItemBeingUnplayable 钉住「放不了不等于没入口」。
//
// 合集那条的播放入口是灰的（没有音频流），而「打开这个合集」**必须是亮的**——
// 它正是那一行唯一能做的事。
func TestTheCollectionEntrySurvivesTheItemBeingUnplayable(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", collectionMediaOf(777777, "一个合集"))
	waitSongs(t, tt, s, 1)
	tt.Frame()

	if err := tt.RightClick("一个合集"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("立即播放"); err == nil {
		t.Error("合集那一行，「立即播放」竟然能按")
	}
	// 菜单末尾仍然是那一句说明（它说的是「为什么」和「还能怎么办」）。
	items := tt.Menu()
	if len(items) == 0 {
		t.Fatal("菜单是空的")
	}
	if last := items[len(items)-1]; last == "打开这个合集" {
		t.Errorf("「打开这个合集」排到了最后——最后那一项是给用户看的那句说明，菜单是 %q", items)
	}
	if err := tt.ChooseMenuItem("打开这个合集"); err != nil {
		t.Errorf("合集那一行，「打开这个合集」按不动：%v", err)
	}
}

// TestTheCollectionItemCarriesTheSeasonID 钉住「合集 id 真的落到了 Item 上」。
//
// 这一条在数据层：`app.ItemOfMedia` 对 type=21 要把 `Media.ID`（合集 id）放进
// `CollectionID`。不落的话右键菜单那一项根本摆不出来（判据是它非 0）。
func TestTheCollectionItemCarriesTheSeasonID(t *testing.T) {
	it := app.ItemOfMedia(bili.Media{ID: 777777, Type: bili.MediaCollection, Title: "一个合集"})
	if it.CollectionID != 777777 {
		t.Errorf("合集项的 CollectionID 是 %d，want 777777（Media.ID 对 type=21 就是合集 id）", it.CollectionID)
	}
	if it.Type != "collection" {
		t.Errorf("合集项的 Type 是 %q，want collection", it.Type)
	}

	// ⚠️ **电影（24）不填**：文档没说清 24 的 id 是什么，查不清就不猜。
	movie := app.ItemOfMedia(bili.Media{ID: 88, Type: bili.MediaMovie, Title: "一部电影"})
	if movie.CollectionID != 0 {
		t.Errorf("电影项的 CollectionID 是 %d，want 0（那一个 id 是什么没查清）", movie.CollectionID)
	}
}
