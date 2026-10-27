// 评论列表的**翻页**：游标走哪一条、三个排序档都能不能翻。
//
// # 这一组盯的是票 #591
//
// 真机回归（2026-10-27，只发 GET）量出来的三件事：
//
//  1. `cursor.next` 在「评论」那一档（`mode=1`）上**带上去必挂**——
//     B 站回 `-400 min_score(1.000000) must be <= max_score(0.000000)`。
//     根因是 `cursor.support_mode` **恒为 `[2, 3]`，不含 1**。
//  2. `cursor.pagination_reply.next_offset` + `pagination_str` **三档都能翻**。
//  3. `mode=1` 与 `mode=2` 给的是**同一份内容**（第 1、2 页的 rpid 都逐条
//     相同），而 B 站给它们的 `cursor.name` 却是不同的（「评论」/「最新评论」）。
//
// # fixture 是**真接口录下来的**
//
// `testdata/comment/reply-main-mode{1,2,3}-p{1,2}.json` 是 2026-10-27 用
// **真接口**（真网络、真登录态、只发 GET）录的两页响应，每一份都挑了六条评论
// （形状原样，只是条数削了）。
package bili

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// commentFixtureOffset 读一份 fixture 里的 `cursor.pagination_reply.next_offset`。
func commentFixtureOffset(t *testing.T, file string) string {
	t.Helper()
	var doc struct {
		Data struct {
			Cursor struct {
				PaginationReply struct {
					NextOffset string `json:"next_offset"`
				} `json:"pagination_reply"`
			} `json:"cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(commentFixture(t, file)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Data.Cursor.PaginationReply.NextOffset == "" {
		t.Fatalf("%s 里没有 pagination_reply.next_offset", file)
	}
	return doc.Data.Cursor.PaginationReply.NextOffset
}

// commentSortFixtures 是三个排序档各自的「真接口录下来的两页」。
var commentSortFixtures = []struct {
	mode int
	name string
	p1   string
	p2   string
}{
	{int(CommentSortHot), "热门评论", "reply-main-mode3-p1.json", "reply-main-mode3-p2.json"},
	{int(CommentSortDefault), "评论", "reply-main-mode1-p1.json", "reply-main-mode1-p2.json"},
	{int(CommentSortLatest), "最新评论", "reply-main-mode2-p1.json", "reply-main-mode2-p2.json"},
}

// commentPagingFake 是一台**照着真接口的脾气**答的假 B 站。
//
// 🔴 **它只要看见 `next` 就回 -400**——真接口在「评论」那一档（mode=1）上
// 就是这么答的（`min_score(1.000000) must be <= max_score(0.000000)`）。
//
// ⚠️ **对另外两档它也这么答**，那是故意的：我们要证明的是**三档都不再发那个
// 游标**（发它 = 那一档必挂），而不是「另外两档发它没事」。
//
// 它照着真接口的另一条脾气：**给一个认不出的 offset，它回的是第一页**
// （实测：不报错，code 0 + 第一页）——所以「翻页成功」不能只看 code。
//
// 它**按请求里的 `mode` 给对应的那一档**（真接口就是这样：mode=1 回
// 「评论」、mode=2 回「最新评论」、mode=3 回「热门评论」）。
func commentPagingFake(t *testing.T) (*fakeBili, *Client) {
	t.Helper()
	pages := map[int][2]string{}
	offsets := map[int]string{}
	for _, tc := range commentSortFixtures {
		pages[tc.mode] = [2]string{commentFixture(t, tc.p1), commentFixture(t, tc.p2)}
		offsets[tc.mode] = commentFixtureOffset(t, tc.p1)
	}

	f := newFakeBili(t)
	f.on(pathReplyMain, func(w http.ResponseWriter, r *http.Request) {
		// 🔴 **老游标一律 -400**（理由见这个函数的注释）。
		if r.URL.Query().Has("next") {
			writeJSON(w, `{"code":-400,"message":"min_score(1.000000) must be <= max_score(0.000000)","ttl":1}`)
			return
		}
		mode, err := strconv.Atoi(r.URL.Query().Get("mode"))
		if err != nil {
			writeJSON(w, `{"code":-400,"message":"invalid mode","ttl":1}`)
			return
		}
		page, ok := pages[mode]
		if !ok {
			writeJSON(w, `{"code":-400,"message":"invalid mode","ttl":1}`)
			return
		}
		p1, p2 := page[0], page[1]
		ps := r.URL.Query().Get("pagination_str")
		if ps == "" {
			writeJSON(w, p1)
			return
		}
		// 认不出的偏移量 ⇒ 回第一页（真接口就是这个脾气）。
		var got struct {
			Offset string `json:"offset"`
		}
		if err := json.Unmarshal([]byte(ps), &got); err != nil {
			writeJSON(w, `{"code":-400,"message":"请求错误","ttl":1}`)
			return
		}
		if got.Offset == offsets[mode] {
			writeJSON(w, p2)
			return
		}
		writeJSON(w, p1)
	})
	return f, f.client(t, Options{})
}

// commentRpids 拿一页评论的 rpid（**评论的主键**）。
func commentRpids(page CommentPage) []int64 {
	out := make([]int64, 0, len(page.Replies))
	for _, r := range page.Replies {
		out = append(out, r.Rpid)
	}
	return out
}

// commentRpidSet 把 rpid 收成一个集合（比对着看两页有没有重）。
func commentRpidSet(page CommentPage) map[int64]bool {
	out := map[int64]bool{}
	for _, r := range page.Replies {
		out[r.Rpid] = true
	}
	return out
}

// TestAllThreeSortsReachTheSecondPage 钉住「三个排序档都能翻到第二页」。
//
// 🔴 **这一条是票 #591 的验收**：翻第二页**每一档都得成**，而每一页的内容
// **必须互不相同**（拿 rpid 比）。
//
// ⚠️ **「内容互不相同」不是理所当然的**：B 站在 `mode=1` 那一档上给的就是
// 「最新评论」的同一份（`TestTheCommentSortGivesTheSameContentAsTheLatestOne`
// 钉住那件事），而**同一份内容翻第二页时两页仍然是不同的**——这里比的是
// **同一档的第一页 vs 第二页**。
func TestAllThreeSortsReachTheSecondPage(t *testing.T) {
	for _, tc := range commentSortFixtures {
		t.Run(tc.name, func(t *testing.T) {
			_, c := commentPagingFake(t)

			first, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, tc.mode, "", 20)
			if err != nil {
				t.Fatalf("第一页拉不出来：%v", err)
			}
			if len(first.Replies) == 0 {
				t.Fatal("第一页一条评论都没有")
			}
			// ⚠️ **没有偏移量就没有第二页**：B 站给的是 `next_offset`。
			if first.NextOffset == "" {
				t.Fatal("第一页没有给出偏移量（cursor.pagination_reply.next_offset）")
			}

			second, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, tc.mode, first.NextOffset, 20)
			if err != nil {
				t.Fatalf("第二页拉不出来：%v", err)
			}
			if len(second.Replies) == 0 {
				t.Fatal("第二页一条评论都没有")
			}
			// 🔴 **两页的内容必须不一样**——否则「翻页」只是把第一页又摆了一遍。
			seen := commentRpidSet(first)
			for _, rp := range commentRpids(second) {
				if seen[rp] {
					t.Errorf("第二页又出现了第一页那条评论（rpid %d）", rp)
				}
			}
			// 第二页还要给出**下一个**偏移量（不然第三页翻不了）。
			if second.NextOffset == "" {
				t.Error("第二页没有给出下一个偏移量")
			}
		})
	}
}

// TestThePagingNeverSendsTheBrokenCursor 钉住「翻页不发 `cursor.next`」。
//
// 🔴 **`cursor.next` 在「评论」那一档（mode=1）上带上去必挂**：B 站回
// `-400 min_score(1.000000) must be <= max_score(0.000000)`（实测 2026-10-27）。
//
// 根因：`cursor.support_mode` **恒为 `[2, 3]`，不含 1**——B 站已经不支持
// mode=1 这一档的游标了。
//
// ⇒ 翻页**只能**走 `cursor.pagination_reply.next_offset` +
// `pagination_str={"offset":"…"}`（实测三档都能用它翻）。
func TestThePagingNeverSendsTheBrokenCursor(t *testing.T) {
	for _, tc := range commentSortFixtures {
		t.Run(tc.name, func(t *testing.T) {
			f, c := commentPagingFake(t)
			first, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, tc.mode, "", 20)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, tc.mode, first.NextOffset, 20); err != nil {
				t.Fatalf("第二页拉不出来：%v", err)
			}
			for _, rec := range f.requests() {
				if rec.Path != pathReplyMain {
					continue
				}
				// 🔴 **不许发 `next`**——那台假 B 站见着它就回 -400（真接口在
				// 「评论」档上也是这么答的）。
				if rec.Query.Has("next") {
					t.Fatalf("发了 next=%q：这一档在真接口上会回 -400", rec.Query.Get("next"))
				}
			}
		})
	}
}

// TestTheCommentSortGivesTheSameContentAsTheLatestOne 钉住「『评论』那一档
// 就是『最新评论』的同一份内容」。
//
// 🔴 **这一条是票 #591 的另一半**：B 站已经不支持 mode=1 这一档的独立游标
// （`support_mode` 恒为 `[2, 3]`），而它给的**内容**也跟「最新评论」一模一样
// ——实测第 1 页 19 条、第 2 页 18 条，rpid **逐条相同**。
//
// ⇒ 界面上**不能**把它和「最新评论」摆成两个不同的东西（摆成两个一模一样的
// 档，用户会以为「切了档但没生效」）。判据是 `CommentSort.SameContentAs`。
//
// ⚠️ **只看 `cursor.name` 分不出来**：B 站给这两档的名字是「评论」和
// 「最新评论」——**名字不同，内容相同**。
func TestTheCommentSortGivesTheSameContentAsTheLatestOne(t *testing.T) {
	_, c := commentPagingFake(t)

	comment, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortDefault), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortLatest), "", 20)
	if err != nil {
		t.Fatal(err)
	}

	// ⚠️ **B 站给这两档的名字是不同的**——所以「名字」不能当判据。
	if comment.Name == latest.Name {
		t.Fatalf("两档的名字一样（%q）——这一条 fixture 就证明不了「名字不同、内容相同」", comment.Name)
	}
	// 🔴 **而内容一模一样**。
	got, want := commentRpids(latest), commentRpids(comment)
	if len(got) == 0 || len(want) == 0 {
		t.Fatal("有一档没读到评论")
	}
	if len(got) != len(want) {
		t.Fatalf("「最新评论」%d 条，「评论」%d 条，want 一样多", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 条不一样：「最新评论」rpid %d，「评论」rpid %d", i, got[i], want[i])
		}
	}

	// ⇒ 界面那一层据此判断「这两档是不是同一份东西」。
	if !CommentSortDefault.SameContentAs(CommentSortLatest) {
		t.Error("「评论」和「最新评论」没有被认成同一份内容")
	}
	if CommentSortDefault.SameContentAs(CommentSortHot) {
		t.Error("「评论」和「热门评论」被认成了同一份内容（实测它们的内容不同）")
	}
}

// TestTheHotSortGivesItsOwnContent 钉住「热门评论是**另一份**内容」。
//
// 这是上一条的**反面**：如果三档都成了同一份内容，这条会红——所以它证明
// 「`SameContentAs` 没有把一切都认成一档」。
func TestTheHotSortGivesItsOwnContent(t *testing.T) {
	_, c := commentPagingFake(t)
	hot, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortHot), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortLatest), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	hotRpids := commentRpidSet(hot)
	same := 0
	for _, rp := range commentRpids(latest) {
		if hotRpids[rp] {
			same++
		}
	}
	if same > 0 {
		t.Errorf("「热门评论」和「最新评论」有 %d 条 rpid 相同——它们不该是同一份", same)
	}
}

// TestTheOffsetIsSentVerbatim 钉住「偏移量一个字都不改」。
//
// ⚠️ **改一个字符 B 站不报错**——它回的是**第一页**（实测：拿一个乱写的
// offset 去问，它回 code 0 + 第一页）。所以「原样带回去」不是讲究，是**必须**：
// 改了的话，界面上「加载更多」按下去会**什么都不变**。
func TestTheOffsetIsSentVerbatim(t *testing.T) {
	f, c := commentPagingFake(t)
	first, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortDefault), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	const offset = "CAESEDE4MzYxMTQ3MTIyOTA0NjAaCAoGgN/2za8JMgQIu+YJ"
	if first.NextOffset != offset {
		t.Fatalf("读出来的偏移量 = %q，want %q", first.NextOffset, offset)
	}
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortDefault), offset, 20); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	var got struct {
		Offset string `json:"offset"`
	}
	if err := json.Unmarshal([]byte(rec.Query.Get("pagination_str")), &got); err != nil {
		t.Fatalf("pagination_str 不是合法 JSON：%v", err)
	}
	if got.Offset != offset {
		t.Fatalf("发出去的偏移量 = %q，want %q", got.Offset, offset)
	}
}

// TestTheSortsOnScreenAreNotTwoOfTheSame 钉住「界面上不会摆出两个一模一样的
// 档」。
//
// 🔴 **「评论」（`mode=1`）和「最新评论」（`mode=2`）在 B 站那边给的是同一份
// 内容**（实测，理由见 `CommentSortDefault` 的注释）。
//
// ⇒ 摆成两个不同的档，用户切过去看到的是**一模一样的内容**，于是以为「切了
// 档但没生效」——而这一档**本来就没有独立内容**。
func TestTheSortsOnScreenAreNotTwoOfTheSame(t *testing.T) {
	got := CommentSorts()
	if len(got) == 0 {
		t.Fatal("一档都没给")
	}
	// ⚠️ **两档之内不能有「同一份内容」的两档**。
	for i := range got {
		for j := i + 1; j < len(got); j++ {
			if got[i].SameContentAs(got[j]) {
				t.Errorf("第 %d 档（%d）和第 %d 档（%d）在 B 站那边是同一份内容"+
					"——摆出来用户会以为「切了档但没生效」", i, got[i], j, got[j])
			}
		}
	}
	// 而「评论」那一档**不该在里面**（它既没有独立内容，B 站的
	// `support_mode` 也不含 1）。
	for _, s := range got {
		if s == CommentSortDefault {
			t.Error("「评论」那一档（mode=1）被摆出来了：它给的就是「最新评论」的同一份")
		}
	}
}
