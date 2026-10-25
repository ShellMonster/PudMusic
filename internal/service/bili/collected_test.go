// 「我收藏的收藏夹」那个接口的测试（`collected/list`）。
//
// ⚠️ 和 collection_test.go 顶上那句话一样：这里没有一个断言在证明「B 站真的
// 这么返回」。它证明的是**我们发出去的形状**（参数名、platform=web）和
// **我们读回来的字段**（尤其是那个官方文档漏写的 `type`）。真接口的兼容性
// 只能靠真机回归。

package bili

import (
	"context"
	"net/http"
	"testing"
)

// TestCollectedFoldersAsksForTheWebPlatform 钉住 `platform=web`。
//
// 官方文档的参数表里写着「填写 web 返回值才会包含用户收藏的视频合集」。
// 省了它的话收藏夹（11）还在，而合集（21）**一整个不返回**——侧栏上少了
// 一整类东西，而界面上看不出任何异样。
//
// ⚠️ 这一条是**发出去的形状**，不是「B 站回不回」：B 站收不收这个参数我们
// 不知道，而省掉它的后果是静默的（少一整类），所以只能钉住我们发了。
func TestCollectedFoldersAsksForTheWebPlatform(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFavCollected, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("platform"); got != "web" {
			t.Errorf("platform = %q，want web（省了它 B 站就不返回「收藏的视频合集」）", got)
		}
		if got := r.URL.Query().Get("up_mid"); got != "900" {
			t.Errorf("up_mid = %q，want 900", got)
		}
		if got := r.URL.Query().Get("pn"); got != "2" {
			t.Errorf("pn = %q，want 2（传 0 要夹到 1）", got)
		}
		if got := r.URL.Query().Get("ps"); got != "50" {
			t.Errorf("ps = %q，want 50", got)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"count": 1,
			"list": []map[string]any{
				{"id": 1, "title": "入站必刷", "mid": 412466388, "type": 11, "state": 0},
			},
		}))
	})

	got, hasMore, err := f.client(t, Options{}).CollectedFolders(context.Background(), 900, 2)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore {
		t.Error("has_more = true，want false")
	}
	if len(got) != 1 {
		t.Fatalf("拿到 %d 条，want 1", len(got))
	}
}

// TestCollectedFoldersReadsTheTypeAndTheOwner 钉住读回来的字段。
//
// ⚠️ **`type` 是官方文档漏写的那一格**：文档的字段表里没有它，而线上响应
// 里有（11 收藏夹 / 21 合集 / 31 系列）。上游 Biu 直接按它分支，而它是一个
// 已发布的客户端——字段不存在的话那段代码跑不起来。
//
// 少读这一格的后果是「点一个合集，按收藏夹打开」——界面上看不出异样。
func TestCollectedFoldersReadsTheTypeAndTheOwner(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFavCollected, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"count": 3,
			"list": []map[string]any{
				{"id": 496307088, "title": "入站必刷", "mid": 412466388, "type": 11, "media_count": 55,
					"cover": "http://i0.hdslb.com/bfs/album/otherguy.jpg"},
				{"id": 700, "title": "合集 甲", "mid": 42, "type": 21, "media_count": 3},
				{"id": 701, "title": "系列 乙", "mid": 43, "type": 31, "media_count": 4},
			},
		}))
	})

	got, _, err := f.client(t, Options{}).CollectedFolders(context.Background(), 900, 1)
	if err != nil {
		t.Fatal(err)
	}
	// ⚠️ **封面是「原主人」的那一张**（票 #270）：这一组里的收藏夹全是别人的，
	// 而 B 站给的就是那个收藏夹自己的封面——不是收藏它的人的封面。
	want := []CollectedFolder{
		{ID: 496307088, Title: "入站必刷", Cover: "http://i0.hdslb.com/bfs/album/otherguy.jpg",
			Mid: 412466388, Type: 11, MediaCount: 55},
		{ID: 700, Title: "合集 甲", Mid: 42, Type: 21, MediaCount: 3},
		{ID: 701, Title: "系列 乙", Mid: 43, Type: 31, MediaCount: 4},
	}
	if len(got) != len(want) {
		t.Fatalf("拿到 %d 条，want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条 = %+v，want %+v", i, got[i], want[i])
		}
	}
}

// TestCollectedFoldersSkipsTheInvalidOnes 钉住失效的那几条要被滤掉。
//
// `state != 0` 是「主人把收藏夹删了，而这一条还留着」。摆出来的话用户点进去
// 看到的是一个打不开的页面。
func TestCollectedFoldersSkipsTheInvalidOnes(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFavCollected, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"count": 2,
			"list": []map[string]any{
				{"id": 1, "title": "还有效的", "mid": 7, "type": 11, "state": 0},
				{"id": 2, "title": "失效的那个", "mid": 8, "type": 11, "state": 1},
			},
		}))
	})

	got, _, err := f.client(t, Options{}).CollectedFolders(context.Background(), 900, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("拿到 %d 条，want 1（失效的那一条要滤掉）", len(got))
	}
	if got[0].Title != "还有效的" {
		t.Errorf("剩下的是 %q，want 还有效的", got[0].Title)
	}
}

// TestCollectedFoldersFallsBackToFavoriteWhenTheTypeIsMissing 钉住「认不出
// 类型时按收藏夹处理」。
//
// 官方文档里**没有** `type` 这一格——所以线上万一不发它，我们不能把整组都
// 当成「认不出来」。按收藏夹处理是最多打不开，而「整条失败」会让用户以为
// 自己收藏的不是收藏夹。
func TestCollectedFoldersFallsBackToFavoriteWhenTheTypeIsMissing(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFavCollected, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"count": 1,
			"list": []map[string]any{
				{"id": 1, "title": "没有 type 的那一条", "mid": 7},
			},
		}))
	})

	got, _, err := f.client(t, Options{}).CollectedFolders(context.Background(), 900, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("拿到 %d 条，want 1", len(got))
	}
	if got[0].Type != int(CollectionFavorite) {
		t.Errorf("类型是 %d，want %d（认不出来时按收藏夹处理）",
			got[0].Type, int(CollectionFavorite))
	}
}

// TestCollectedFoldersKnowsThereAreMorePages 钉住「后面还有没有」是按
// **count** 算的。
//
// ⚠️ **不能按这一页的条数算**：这一页里可能有被滤掉的失效条目（`state != 0`），
// 按条数判的话「一页 50 条里有 3 条失效」就会说「没有下一页」——
// 而用户收藏夹列表的后几页就这么没了。
func TestCollectedFoldersKnowsThereAreMorePages(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFavCollected, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			// count 说有 120 条，也就是三页；而这一页只回了 1 条。
			"count": 120,
			"list": []map[string]any{
				{"id": 1, "title": "这一页只有一条", "mid": 7, "type": 11, "state": 0},
			},
		}))
	})

	_, hasMore, err := f.client(t, Options{}).CollectedFolders(context.Background(), 900, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore {
		t.Error("has_more = false，want true（count 说有 120 条）")
	}
}

// TestCollectedFoldersRefusesNobody 收藏夹主人 id 不对就报错，不发请求。
//
// 0 号用户的收藏夹列表 B 站会回一堆别人的东西或者干脆 -400，而两种结果在
// 界面上都表现为「收藏夹列表是空的」。
func TestCollectedFoldersRefusesNobody(t *testing.T) {
	f := newFakeBili(t)
	if _, _, err := f.client(t, Options{}).CollectedFolders(context.Background(), 0, 1); err == nil {
		t.Fatal("up_mid=0 也发出去了，应该报错")
	}
}
