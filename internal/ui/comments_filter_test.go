// 「只看正文里提到这首歌」那一枚开关（票 #574）的验收。
//
// # 这一族断的是「筛完之后画面上有什么」
//
// 这一票要断的几件事：
//
//	默认   开关**默认是关的**——这一页默认摆的是这一首的**全部**评论
//	筛完   只剩正文里提到这首歌的那些评论
//	零命中 那句话要说清楚「这个视频的 N 条评论里没有提到这首歌」
//	查完了 那一句话**只有在这一首的评论都查过之后**才说得出来
//	楼中楼 楼中楼按它**自己**的正文留
//
// ⚠️ **数据走一台假 B 站**（真 fixture 改的），**一个字都不许打真 B 站**
// （AGENTS.md §6）。
package ui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/songname"
)

// mentionItem 是「一个视频，而它的歌名是『晴天』」那一档的条目。
//
// ⚠️ **mv 那一档**：它的评论是**这个视频**的评论（里面大部分不是在聊这首歌），
// 而这一票就是为那一档做的。
func mentionItem() app.Item {
	it := mvItem()
	it.Title = "晴天"
	return it
}

// commentFixtureWithTotal 是「总数与条数说得对得上」的那一份 fixture。
//
// ⚠️ **它改两样**：`cursor.all_count`（B 站给的评论总数）与 `cursor.is_end`
// （后面还有没有）。这两个数是**零命中那一句话的判据**（票 #574 的评审：翻到
// 12 条而 B 站说总数是 184226 的时候，不能宣称那 184226 条都查过了）。
func commentFixtureWithTotal(t *testing.T, name string, total int, isEnd bool) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(commentFixtureBytes(t, name), &doc); err != nil {
		t.Fatalf("fixture 读不懂：%v", err)
	}
	data, _ := doc["data"].(map[string]any)
	cursor, _ := data["cursor"].(map[string]any)
	cursor["all_count"] = total
	cursor["is_end"] = isEnd
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("fixture 编不出来：%v", err)
	}
	return out
}

// commentFixturePage 是「n 条评论、rpid 从 base 开始」的那一页 fixture。
//
// ⚠️ **形状照着真 fixture**（member / content / reply_control 那一套一个字都
// 没改），只把**条数与 rpid** 换成自己编的——「翻页那一族」要的是「每页
// 带来多少条**新的**评论」。
//
// ⚠️ **正文里没有歌名**：这一族要验的是「翻遍了也没有提到这首歌」。
func commentFixturePage(t *testing.T, name string, n int, base int64, total int, isEnd bool) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(commentFixtureBytes(t, name), &doc); err != nil {
		t.Fatalf("fixture 读不懂：%v", err)
	}
	data, _ := doc["data"].(map[string]any)
	old, _ := data["replies"].([]any)
	if len(old) == 0 {
		t.Fatalf("fixture 里一条评论都没有")
	}
	// ⚠️ **拿真 fixture 的第一条当模板**（形状一个字都不改），只改 rpid、
	// mid 与正文。
	tmplRaw, err := json.Marshal(old[0])
	if err != nil {
		t.Fatalf("模板编不出来：%v", err)
	}
	replies := make([]any, 0, n)
	for i := 0; i < n; i++ {
		var c map[string]any
		if err := json.Unmarshal(tmplRaw, &c); err != nil {
			t.Fatalf("模板读不懂：%v", err)
		}
		c["rpid"] = base + int64(i)
		content, _ := c["content"].(map[string]any)
		content["message"] = fmt.Sprintf("第 %d 条评论，跟这首歌没关系", i+1)
		member, _ := c["member"].(map[string]any)
		// ⚠️ **`member.mid` 在真形状里是**字符串**（`bili.commentReply.Member.Mid`
		// 是 `string`）——给数字的话整页读不出来（`json: cannot unmarshal
		// number into Go struct field …member.mid of type string`）。
		member["mid"] = strconv.FormatInt(base+int64(i), 10)
		replies = append(replies, c)
	}
	data["replies"] = replies
	cursor, _ := data["cursor"].(map[string]any)
	cursor["all_count"] = total
	cursor["is_end"] = isEnd
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("fixture 编不出来：%v", err)
	}
	return out
}

// commentFixtureMentionNoMore 是「第一条评论提到了这首歌，而 B 站说到底了」
// 那一份 fixture。
//
// ⚠️ **形状照着真 fixture，只改两样**：第一条评论的正文，和 `cursor.is_end`
// （那一档是「后面还有没有」的输入）。
func commentFixtureMentionNoMore(t *testing.T, mention string) []byte {
	t.Helper()
	raw := commentFixtureWithNoMore(t, "reply-main.json")
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("fixture 读不懂：%v", err)
	}
	data, _ := doc["data"].(map[string]any)
	replies, _ := data["replies"].([]any)
	if len(replies) == 0 {
		t.Fatalf("fixture 里一条评论都没有")
	}
	first, _ := replies[0].(map[string]any)
	content, _ := first["content"].(map[string]any)
	content["message"] = "「" + mention + "」这一段我循环了一整天"
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("fixture 编不出来：%v", err)
	}
	return out
}

// commentFixtureMentionOnlyTheArtist 是「唯一提到人的那一条只提到了**歌
// 手**」的那一份 fixture。
//
// ⚠️ **总数给的是 6**（这一页真的就给了 6 条）⇒ 零命中那句话说的是**完整**的
// 那一句（「这个视频的 6 条评论里没有提到这首歌」）。
func commentFixtureMentionOnlyTheArtist(t *testing.T, mention string) []byte {
	t.Helper()
	raw := commentFixtureWithTotal(t, "reply-main.json", 6, true)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("fixture 读不懂：%v", err)
	}
	data, _ := doc["data"].(map[string]any)
	replies, _ := data["replies"].([]any)
	if len(replies) == 0 {
		t.Fatalf("fixture 里一条评论都没有")
	}
	first, _ := replies[0].(map[string]any)
	content, _ := first["content"].(map[string]any)
	content["message"] = "「" + mention + "」这一段我循环了一整天"
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("fixture 编不出来：%v", err)
	}
	return out
}

// TestTheMentionFilterIsOffByDefault 断「开关默认是关的」。
//
// 🔴 **默认摆的是全部评论**：这一页是「这一首的评论」，而筛选是用户自己挑
// 出来的窄视图——默认就筛的话，用户读评论时看到的东西比他点的那个东西少。
func TestTheMentionFilterIsOffByDefault(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureMentionNoMore(t, "晴天")
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1400)

	// 那一枚开关在页头上。
	if _, ok := tt.Find("开关：只看提到歌名的评论"); !ok {
		t.Fatalf("页头上没有那枚开关，文字有 %q", tt.Texts())
	}
	// ⚠️ **判据是「没提到歌名的那一条也在」**：它不在，就说明默认已经在筛了。
	if !tt.HasText("耄耋_不是耄") {
		t.Errorf("默认状态下没有「耄耋_不是耄」——默认不该筛，文字有 %q", tt.Texts())
	}
	// 页头那一行也还是「共 N 条」，不是筛选后的条数。
	if tt.HasText("条提到这首歌") {
		t.Errorf("默认状态下页头摆的是筛选后的条数，文字有 %q", tt.Texts())
	}
}

// TestTheMentionFilterShowsOnlyTheCommentsThatMentionTheSong 断「筛完只剩提到
// 歌名的那些」。
func TestTheMentionFilterShowsOnlyTheCommentsThatMentionTheSong(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureMentionNoMore(t, "晴天")
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1400)

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "筛完了", func() bool {
		return tt.HasText("这一段我循环了一整天")
	})
	// ⚠️ **没提到歌名的那一条不许还在**。
	if tt.HasText("耄耋_不是耄") {
		t.Errorf("没提到歌名的那一条还在画面上，文字有 %q", tt.Texts())
	}
	// 页头那一行换成筛选后的条数（不然用户以为别的评论不见了）。
	if !tt.HasText("1 条提到这首歌") {
		t.Errorf("页头没有说「1 条提到这首歌」，文字有 %q", tt.Texts())
	}
}

// TestTheMentionFilterFindsTheSongNameInsideTheTitle 断「歌名藏在标题里那一档
// 也能筛出来」。
//
// 🔴 **B 站的标题有几种常见形状**（票 #574 的评审：「New("【晴天】") 只认出了
// 「【晴天】」，正文里提到「晴天」的那一条被判成没提到」）：【晴天】、
// 「歌手 - 歌名」、紧凑连字符、冒号、**两种分隔符挨在一起**。
func TestTheMentionFilterFindsTheSongNameInsideTheTitle(t *testing.T) {
	for _, title := range []string{
		"【晴天】",
		"周杰伦-晴天",
		"晴天：电影主题曲",
		// ⚠️ **「歌手-歌名：说明」那一档**（票 #574 的第二轮评审）：两种分隔
		// 符挨在一起的时候要**先切外层的**，不然只认出「歌名：说明」⇒ 正文里
		// 提到「歌名」的那一条被判成没提到 ⇒ 界面显示零命中。
		"周杰伦-晴天：电影主题曲",
		// ⚠️ **「歌手：歌名 (Live)」那一档**（票 #574 第三轮评审的 F7）：版本
		// 说明要**先剥掉**再看——不剥的话那个 "Live" 让整段被当成说明 ⇒ 歌名
		// 跑到左边（歌手）去了 ⇒ 提到「晴天」的那一条被判成没提到。
		"周杰伦：晴天 (Live)",
	} {
		t.Run(title, func(t *testing.T) {
			b := newCommentBili(t, "reply-main.json")
			b.mainRaw = commentFixtureMentionNoMore(t, "晴天")
			it := mvItem()
			it.Title = title
			_, tt := newCommentsShell(t, b, it, 1400, 1400)

			if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
				t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
			}
			commentsWait(t, tt, "认出了歌名", func() bool {
				return tt.HasText("这一段我循环了一整天")
			})
			if !tt.HasText("1 条提到这首歌") {
				t.Errorf("标题 %q 里的歌名没认出来，文字有 %q", title, tt.Texts())
			}
		})
	}
}

// TestTheMentionFilterDoesNotCountTheArtistAsAMention 断「只提到歌手的那一条
// 不算提到这首歌」。
//
// 🔴 **提到歌手不等于提到这首歌**（票 #574 的第二轮评审：「周杰伦：晴天」那一
// 档把「周杰伦」收进了别名表，于是正文只提到歌手的评论也被算成提到了歌；
// 第三轮评审的 F7：「周杰伦：晴天 (Live)」那一档同样如此）。
func TestTheMentionFilterDoesNotCountTheArtistAsAMention(t *testing.T) {
	// ⚠️ **两种标题都要断**：带版本说明的那一档（"(Live)"）最容易把歌名认
	// 丢。
	for _, title := range []string{"周杰伦：晴天", "周杰伦：晴天 (Live)"} {
		t.Run(title, func(t *testing.T) {
			b := newCommentBili(t, "reply-main.json")
			// ⚠️ **这一份里唯一提到人的那一条只提到了歌手**。
			b.mainRaw = commentFixtureMentionOnlyTheArtist(t, "周杰伦")
			it := mvItem()
			it.Title = title
			_, tt := newCommentsShell(t, b, it, 1400, 1400)

			if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
				t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
			}
			// 零命中（这一份里一条都没提到「晴天」）⇒ 说的是「没有提到这首歌」。
			commentsWait(t, tt, "说清了没有提到这首歌", func() bool {
				return tt.HasText("这个视频的 6 条评论里没有提到这首歌")
			})
			if tt.HasText("1 条提到这首歌") {
				t.Errorf("只提到歌手的那一条被算成提到这首歌了，文字有 %q", tt.Texts())
			}
		})
	}
}

// TestTheMentionFilterSaysThatNobodyMentionedTheSong 断「零命中那句话」。
//
// 🔴 **这句话是这一票的验收项**：命中为 0 时要说清楚「这个视频的 N 条评论里
// 没有提到这首歌」——而 N 是**这一首的评论总数**（这一份 fixture 里 B 站给的
// 是 6，而它真的就给了 6 条）。
func TestTheMentionFilterSaysThatNobodyMentionedTheSong(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	// ⚠️ **这一份里没有一条提到「晴天」**（真 fixture 的正文），而 B 站说
	// 到底了（`is_end`）——于是那句话是**翻完**之后说的。
	b.mainRaw = commentFixtureWithTotal(t, "reply-main.json", 6, true)
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1400)

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "说清了没有提到这首歌", func() bool {
		return tt.HasText("这个视频的 6 条评论里没有提到这首歌")
	})
}

// TestTheMentionFilterDoesNotClaimToHaveCheckedEveryComment 断「没查完不许说
// 查完了」。
//
// 🔴 **B 站说到底了不等于我们查完了**（票 #574 的评审）：实测有一档是「翻了
// 12 条，B 站说到底了，而它给的总数是 184226」——说「这个视频的 184226 条
// 评论里没有提到这首歌」是撒谎：我们只看过 12 条。
func TestTheMentionFilterDoesNotClaimToHaveCheckedEveryComment(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	// 第一页：6 条，总数 388，还没到底。
	b.mainRaw = commentFixtureWithTotal(t, "reply-main.json", 388, false)
	// 第二页：另外 6 条（rpid 与第一页不撞），而总数变成了 184226。
	b.page2Raw = commentFixtureWithTotal(t, "reply-main-mode3-p2.json", 184226, false)
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1400)

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	// 界面自己往下翻：第二页那 6 条也还是没有提到歌名，而 B 站给的总数是
	// 184226 ——于是它只能说「已翻到的 12 条」。
	commentsWait(t, tt, "说的是实话", func() bool {
		return tt.HasText("已翻到的 12 条评论里还没有提到这首歌")
	})
	if tt.HasText("184226 条评论里没有提到这首歌") {
		t.Errorf("只查过 12 条，却宣称查完了 184226 条，文字有 %q", tt.Texts())
	}
}

// TestTheMentionFilterLooksThroughTheWholeVideoBeforeItSaysNobodyDid 断「那句
// 话是查完这一首的评论之后才说的」。
//
// 🔴 **只翻了第一页就说「没有」，那是撒谎**：这一首有 24 条评论（4 页），而
// 用户翻下去就看到一条提到歌名的评论——于是他觉得这个功能在骗人。
//
// ⇒ 筛选开着而一条都没命中时，界面自己把这一首的评论翻完（翻到条数盖住
// B 站给的总数为止）。
//
// ⚠️ **判据不是「又请求了一页」**（票 #574 的评审：把扫描停在第一页，这条
// 用例照样绿）——**判据是那句话里的数**：它说的是 24，而 24 就是这一首的
// 总数 ⇒ 它真的翻到了最后一条。
//
// ⚠️ **四页是刻意的**：把扫描停在第 13 条那一档（评审用的变异），这一首就
// 只查了 12 条 ⇒ 那句话变成「已翻到的 12 条…」⇒ **这条会红**。
func TestTheMentionFilterLooksThroughTheWholeVideoBeforeItSaysNobodyDid(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	// 四页，每页 6 条（rpid 互不撞），总数 24。
	b.mainRaw = commentFixturePage(t, "reply-main.json", 6, 1000, 24, false)
	b.pageSeq = [][]byte{
		commentFixturePage(t, "reply-main-mode3-p2.json", 6, 2000, 24, false),
		commentFixturePage(t, "reply-main-mode3-p2.json", 6, 3000, 24, false),
		commentFixturePage(t, "reply-main-mode3-p2.json", 6, 4000, 24, true),
	}
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1400)

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "翻到了最后一条", func() bool {
		return tt.HasText("这个视频的 24 条评论里没有提到这首歌")
	})
	// ⚠️ **四页都翻了**（第一页是进页面时就问过的，后面三页是筛选自己翻的）。
	if got := b.mainCallCount(); got != 4 {
		t.Errorf("主列表被问了 %d 趟，want 4 趟（第一页 + 三趟翻页）——"+
			"少的那几趟就是没翻完", got)
	}
}

// TestTheMentionFilterAlsoLooksAtTheReplies 断「楼中楼按它自己的正文留」。
//
// 🔴 **两件事都要对**（票 #574 的评审）：
//
//	主评论提到了、楼中楼没提到   楼中楼**不摆**（它跟这首歌没关系）
//	主评论没提到、楼中楼提到了   主评论**照摆**（它是那段对话的上下文），
//	                          摆的是那条提到了的楼中楼
func TestTheMentionFilterAlsoLooksAtTheReplies(t *testing.T) {
	p := &commentsPage{
		mentionOnly: true,
		names:       songname.New("晴天", "周杰伦"),
		expanded:    map[int64]bool{10: true, 20: true},
	}
	cm := []bili.Comment{
		{Rpid: 10, Message: "晴天真好听"},
		{Rpid: 20, Message: "这条主评论跟歌没关系"},
	}
	replies := func(root int64) []bili.Comment {
		switch root {
		case 10:
			return []bili.Comment{{Rpid: 11, Message: "回复完全不相关"}}
		case 20:
			return []bili.Comment{{Rpid: 21, Message: "这条回复提到了晴天"}}
		}
		return nil
	}
	p.rebuildRows(cm, replies)

	got := map[int64]bool{}
	for _, r := range p.rows {
		got[r.cm.Rpid] = true
	}
	if !got[10] {
		t.Errorf("提到了歌名的那一条主评论不见了：%v", got)
	}
	if got[11] {
		t.Errorf("没提到歌名的那一条楼中楼还在：%v", got)
	}
	if !got[20] {
		t.Errorf("只有楼中楼提到了歌名，主评论却被整条藏起来了：%v", got)
	}
	if !got[21] {
		t.Errorf("提到了歌名的那一条楼中楼不见了：%v", got)
	}
	// ⚠️ **计数只算提到了的那些**（主评论 20 自己没提到，不算）。
	if p.hits != 2 {
		t.Errorf("命中数是 %d，want 2（主评论 10 + 楼中楼 21）", p.hits)
	}
}

// TestTheMentionFilterCanBeTurnedOffFromTheEmptyState 断「零命中那一格给了一条
// 退路」。
//
// 🔴 **这一格空着是用户自己按出来的**：他按了筛选，而这一首的评论里没有一条
// 提到歌名——那他就得能**按一下就回到全部评论**，不用他去页头找那枚开关。
func TestTheMentionFilterCanBeTurnedOffFromTheEmptyState(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureWithNoMore(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1400)

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "说清了没有提到这首歌", func() bool {
		return tt.HasText("没有提到这首歌")
	})
	// ⚠️ **那一枚键得在**：它就是「怎么才能不空」。
	if err := tt.Click("看全部评论"); err != nil {
		t.Fatalf("点不了「看全部评论」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "评论回来了", func() bool {
		return tt.HasText("耄耋_不是耄")
	})
	// 而筛选确实关掉了（页头不再摆筛选后的条数）。
	if tt.HasText("条提到这首歌") {
		t.Errorf("关掉之后页头还摆着筛选后的条数，文字有 %q", tt.Texts())
	}
}
