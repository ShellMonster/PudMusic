// 「只看正文里提到这首歌」那一枚开关（票 #574）的验收。
//
// # 这一族断的是「筛完之后画面上有什么」
//
// 这一票要断的三件事：
//
//	默认   开关**默认是关的**——这一页默认摆的是这一首的**全部**评论
//	筛完   只剩正文里提到这首歌的那些评论
//	零命中 那句话要说清楚「这个视频的 N 条评论里没有提到这首歌」
//
// ⚠️ **数据走一台假 B 站**（真 fixture 改的），**一个字都不许打真 B 站**
// （AGENTS.md §6）。
package ui

import (
	"encoding/json"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
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

// TestTheMentionFilterSaysThatNobodyMentionedTheSong 断「零命中那句话」。
//
// 🔴 **这句话是这一票的验收项**：命中为 0 时要说清楚「这个视频的 N 条评论里
// 没有提到这首歌」——而 N 是这一首的评论总数（fixture 里 B 站给的是 388）。
func TestTheMentionFilterSaysThatNobodyMentionedTheSong(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	// ⚠️ **这一份里没有一条提到「晴天」**（真 fixture 的正文），而 B 站说
	// 到底了（`is_end`）——于是那句话是**翻完**之后说的。
	b.mainRaw = commentFixtureWithNoMore(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1400)

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "说清了没有提到这首歌", func() bool {
		return tt.HasText("这个视频的 388 条评论里没有提到这首歌")
	})
}

// TestTheMentionFilterLooksThroughTheWholeVideoBeforeItSaysNobodyDid 断「那句话
// 是在翻完之后说的」。
//
// 🔴 **只翻了第一页就说「没有」，那是撒谎**：这一首有 388 条评论，而用户翻
// 下去就看到一条提到歌名的评论——于是他觉得这个功能在骗人。
//
// ⇒ 筛选开着而一条都没命中时，界面自己把这一首的评论翻完（翻到 B 站说到底
// 了为止）。
func TestTheMentionFilterLooksThroughTheWholeVideoBeforeItSaysNobodyDid(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	// ⚠️ **这一份 B 站没说到底**（真 fixture 的 `is_end` 是 false）：于是
	// 筛选自己会继续往下翻。
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1400)

	before := b.mainCallCount()
	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "往下翻了", func() bool {
		return b.mainCallCount() > before
	})
	commentsWait(t, tt, "说清了没有提到这首歌", func() bool {
		return tt.HasText("没有提到这首歌")
	})
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
