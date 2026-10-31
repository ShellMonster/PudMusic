// `ArchiveStats` 里那三个数（票 #701）。
//
// # 这一组断的是「一趟回包里的三栏」
//
// `stat` 里的 `reply` / `favorite` / `like` 在**同一个对象**里，而那一趟
// （`/x/web-interface/view`）播放条本来就要发（票 #682 的评论数就在里头）。
// 所以这三个数**零新增网络往返**。
//
// 🔴 **最要紧的一条是「形状漂移只丢数字」**：新加的两栏（`favorite` / `like`）
// 是**我们不用它也会读**的字段，而 `ArchiveStats` 是收藏（`ridAndType`）和
// 一键三连的必经之路——`json.Unmarshal` 遇到类型对不上会让**整个反序列化**
// 失败，于是一个我们不用的字段换个形状就能把收藏和三连一起打死。
// ⇒ 新字段必须待在**第二遍解码**里（那遍失败只当「没有这个数」）。
package bili

import (
	"context"
	"net/http"
	"testing"
)

// TestArchiveStatsReadsTheThreeCounts 钉住「一趟回包里有那三个数」。
//
// ⚠️ **字段名照 bilibili-API-collect 的 `docs/video/info.md`**：
//
//	stat.reply     评论数
//	stat.favorite  收藏数
//	stat.like      获赞数
//
// 🔴 **三个数必须同时拿到**：播放条上三枚键各要一个，而它们在同一个回包里
// ——分三次问就是「一次能拿到的东西问了 B 站三次」。
func TestArchiveStatsReadsTheThreeCounts(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"aid": 4242, "bvid": "BV1xx",
			"stat": map[string]any{
				"view": 100, "danmaku": 3, "reply": 1286,
				"favorite": 2468, "like": 9999, "dislike": 0,
			},
		}))
	})
	st, err := f.client(t, Options{}).ArchiveStats(context.Background(), "BV1xx")
	if err != nil {
		t.Fatal(err)
	}
	if st.Aid != 4242 {
		t.Errorf("Aid 是 %d，want 4242", st.Aid)
	}
	if st.Reply != 1286 {
		t.Errorf("Reply 是 %d，want 1286（`stat.reply` 是评论数）", st.Reply)
	}
	if st.Favorite != 2468 {
		t.Errorf("Favorite 是 %d，want 2468（`stat.favorite` 是收藏数）", st.Favorite)
	}
	if st.Like != 9999 {
		t.Errorf("Like 是 %d，want 9999（`stat.like` 是获赞数）", st.Like)
	}
}

// TestArchiveStatsKeepsTheAidWhenTheNewCountsDrift 钉住**新加的两栏形状漂移
// 只丢数字，不打死整条链路**。
//
// 🔴 **这是票 #682 对抗性审查定下的规矩，在这一票上再加两栏**：收藏
// （`ridAndType`）和一键三连都走 `ArchiveStats` → `ArchiveID`，而
// `json.Unmarshal` 遇到类型对不上会让**整个反序列化**失败。于是 `stat.like`
// 或 `stat.favorite` 换个形状（写成字符串、变成数组……），就会把收藏和三连
// 一起打死——**我们压根不用它**的那两栏，把我们要的东西带走了。
//
// ⇒ 三个数都在**第二遍**解码里，那遍失败只当「没有这个数」。
func TestArchiveStatsKeepsTheAidWhenTheNewCountsDrift(t *testing.T) {
	for _, tc := range []struct {
		name string
		stat any
	}{
		{"like 是字符串", map[string]any{"reply": 12, "favorite": 3, "like": "999"}},
		{"favorite 是字符串", map[string]any{"reply": 12, "favorite": "2468", "like": 9}},
		{"两个都是字符串", map[string]any{"reply": 12, "favorite": "1", "like": "2"}},
		{"stat 是数组", []any{}},
		{"stat 是字符串", "没有这一栏"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeBili(t)
			f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, jsonData(t, 0, "0", map[string]any{
					"aid": 4242, "bvid": "BV1xx", "stat": tc.stat,
				}))
			})
			c := f.client(t, Options{})

			// 🔴 最要紧的一条：收藏 / 三连走的那条路必须照旧。
			aid, err := c.ArchiveID(context.Background(), "BV1xx")
			if err != nil {
				t.Fatalf("ArchiveID 报错了：%v——一个不用的字段把收藏/三连打死了", err)
			}
			if aid != 4242 {
				t.Errorf("ArchiveID 回的是 %d，want 4242", aid)
			}

			// `ArchiveStats` 自己也不许报错：aid 要对。
			st, err := c.ArchiveStats(context.Background(), "BV1xx")
			if err != nil {
				t.Fatalf("ArchiveStats 报错了：%v", err)
			}
			if st.Aid != 4242 {
				t.Errorf("ArchiveStats 的 Aid 是 %d，want 4242", st.Aid)
			}
			// ⚠️ **`flexInt` 的语义**：写成数字字符串时照样读出来（读不出来当 0）。
			if tc.name == "like 是字符串" && st.Like != 999 {
				t.Errorf("like 是 \"999\" 时读成 %d，want 999（flexInt 的语义）", st.Like)
			}
			if tc.name == "favorite 是字符串" && st.Favorite != 2468 {
				t.Errorf("favorite 是 \"2468\" 时读成 %d，want 2468（flexInt 的语义）", st.Favorite)
			}
			// 🔴 **形状整个变了（数组 / 字符串）那一档**：三个数全是「没有」，
			// 而**不许报错**——`aid` 才是我们要的东西。
			if tc.name == "stat 是数组" || tc.name == "stat 是字符串" {
				if st.Reply != 0 || st.Favorite != 0 || st.Like != 0 {
					t.Errorf("stat 整个坏了，却读出了数 %+v——那一档该全是「没有」", st)
				}
			}
		})
	}
}

// TestArchiveStatsRejectsNegativeCounts 钉住**负数是上游的垃圾，按「没有这个数」算**。
//
// 🔴 把 -1 当收藏数画出来是「-1 次收藏」，比不画糟得多——而界面上那一格画的是
// 一个数，用户看不出它是「上游的垃圾」。
func TestArchiveStatsRejectsNegativeCounts(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathView, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"aid": 4242, "bvid": "BV1xx",
			"stat": map[string]any{"reply": -1, "favorite": -5, "like": -9},
		}))
	})
	st, err := f.client(t, Options{}).ArchiveStats(context.Background(), "BV1xx")
	if err != nil {
		t.Fatal(err)
	}
	if st.Reply != 0 || st.Favorite != 0 || st.Like != 0 {
		t.Errorf("拿到 %+v，want 三个数都是 0（负数按「没有这个数」算）", st)
	}
}
