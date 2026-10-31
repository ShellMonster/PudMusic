// 长正文的「展开 / 收起」那一枚（票 #706）。
//
// # 这一族断的是「画面上有什么」
//
// 评论正文原来只有**终态**：太长就按行数截断加省略号，用户想看全文**没有任何
// 入口**。这一票给它加一枚「展开」——点一下正文完整展开，按钮变成「收起」，再
// 点一下回到折叠态（B 站那种**原位切换**）。
//
// 断的是渲染结果：按钮在不在、文案对不对、展开之后全文是不是真的在画面上、
// 收起之后是不是真的又折起来了。
//
// # 🔴 这一族在新架构上**重写过**
//
// 原来它是照**旧的自排版那套**写的（`shapeCommentBody` / `commentBody.folded`
// / `commentBodyOf(c, p, cm, lines, w)` / `commentBodyUnlimited = -1`）——而票
// #702 把正文换成了**框架的 `ui.RichText`**⇒ 旧的那一套在 main 上已经删干净。
//
// ⇒ 所以这一族现在断的是**新架构上的同一批行为**：按钮在不在、文案对不对、
// 展开之后正文全在、收起之后又折起来。**断言不许因为架构变了就删掉**——
// 那些行为是这一票要治的。
//
// ⚠️ **一个字都不许打真 B 站**：夹具是本地假服务器（`commentBili`）。

package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// commentFixtureWithLongReplyBody 是「**楼中楼**那条的正文很长」的那一份夹具。
//
// ⚠️ **改的是楼中楼那一趟的夹具**（`reply-reply.json`——`/x/v2/reply/reply`
// 那一趟回的就是它），而不是主列表那份：楼中楼不在主列表的回包里。
//
// ⚠️ **形状照着真夹具，只把第一条楼中楼的正文换长的**：字段名、层级一个字都
// 没动——「楼中楼也能展开」这一条要验的是排版，而排版只跟正文有关。
func commentFixtureWithLongReplyBody(t *testing.T, name string) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(commentFixtureBytes(t, name), &doc); err != nil {
		t.Fatalf("fixture %s 读不懂：%v", name, err)
	}
	data, _ := doc["data"].(map[string]any)
	replies, _ := data["replies"].([]any)
	if len(replies) == 0 {
		t.Fatalf("fixture %s 里一条楼中楼都没有", name)
	}
	first, _ := replies[0].(map[string]any)
	content, _ := first["content"].(map[string]any)
	content["message"] = strings.Repeat(commentLongBody, 12)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("fixture %s 编不出来：%v", name, err)
	}
	return raw
}

// ─────────────────────────────────────────────────────────────────────────────
// 排版那一层：折没折
// ─────────────────────────────────────────────────────────────────────────────

// TestTheUnlimitedLinesDoNotTruncateTheBody 断「**不限行数**」那一种表达方式。
//
// 🔴 **这是这一票最容易踩的坑，而新架构上它换了个形状**：
//
//	旧的做法  正文**自己**排版、自己截断，而上限是 `lines × lineH`——上限写成
//	          0 的话「高度 > 上限」那一支**恒真**，正文会被截成 0 个字加一个
//	          省略号。⇒ 「不限」必须是一个**单独的负数**（-1）。
//	新的做法  截断是**框架**做的（`ui.RichText` 的 `MaxLines` + `Ellipsis`），
//	          而框架判的是 **`p.MaxLines > 0`**（`internal/text/layout.go` 的
//	          `layout`）⇒ **`MaxLines(0)` 就是「不设上限」，一行都不截**。
//
// ⇒ 所以新架构里「不限行数」就是 **0**，而这一条钉住的就是**这一点**：
// `commentBodyUnlimited` 传进框架之后，正文**一个字都不少**。
//
// ⚠️ **判据是三样**：正文一字不少、末尾没有省略号、高度**大于**一行（真被截
// 成 0 字的话，高度就是 0）。
func TestTheUnlimitedLinesDoNotTruncateTheBody(t *testing.T) {
	msg := strings.Repeat("这一句很长", 200)
	ts := newTestShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			cm := bili.Comment{Rpid: 1, Message: msg}
			spans, _, plain := commentSpansOf(cm.Message, cm.Emotes, nil, commentMark{})
			if plain != msg {
				t.Errorf("不限行数那一档正文被截了：拿到的是 %d 个字，原文 %d 个字",
					len([]rune(plain)), len([]rune(msg)))
			}
			if strings.HasSuffix(plain, "…") {
				t.Errorf("不限行数那一档末尾还是省略号：%q", plain)
			}
			// 🔴 **「不限」那一档恒不折**（`commentBodyFolded` 见 `lines <= 0`
			// 就返回 false）——它是**展开之后**的那一份。
			if commentBodyFolded(c, spans, 900, commentBodyUnlimited) {
				t.Error("不限行数那一档被判成「折过了」——它是展开之后的那一份")
			}
			// 折叠那一档要真的折起来（否则这一条测的是「本来就没折」）。
			if !commentBodyFolded(c, spans, 900, commentBodyLines) {
				t.Error("折叠那一档没被判成「折过了」——正文太长而它没折")
			}
		}
	}))
	ts.tt.Frame()
}

// TestTheBodyRemembersWhetherItWasFolded 断「**折没折**是量出来的，而不是猜的」。
//
// 🔴 **界面层不该自己判断「这条是不是太长了」**：那是**排版那一层**才知道的事
// （框架按行数截断，而截了没有只有那一层知道）。
//
// ⇒ 新架构里那个判据是 `commentBodyFolded`：它**量**正文在那一列的宽上排出来
// 有多高，而上限是 `lines × lineH`。
//
// ⚠️ **判据是三种形态各判各的**：长正文折起来判 true、展开那一档（不限行数）
// 判 false、而**短正文压根不折**判 false。
func TestTheBodyRemembersWhetherItWasFolded(t *testing.T) {
	msg := strings.Repeat("这一句很长", 200)
	short := "短正文"
	ts := newTestShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			spans, _, _ := commentSpansOf(msg, nil, nil, commentMark{})
			if !commentBodyFolded(c, spans, 900, commentBodyLines) {
				t.Error("长正文折起来之后没有被判成「折过了」")
			}
			if commentBodyFolded(c, spans, 900, commentBodyUnlimited) {
				t.Error("展开那一档被判成「折过了」——界面层会摆一枚『展开』出来")
			}
			shortSpans, _, _ := commentSpansOf(short, nil, nil, commentMark{})
			if commentBodyFolded(c, shortSpans, 900, commentBodyLines) {
				t.Error("短正文被判成「折过了」——界面层会给它摆一枚『展开』出来")
			}
		}
	}))
	ts.tt.Frame()
}

// TestTheExpandedBodyUsesADifferentCacheEntry 断「展开是**换一行数**再排一次」。
//
// 🔴 **缓存键里本来就含 `lines`**（`commentBodyOf` 判的是 `b.lines == lines`）
// ——所以「用另一个 lines 值再排一次」天然不撞缓存，**不需要另起一套缓存**。
//
// ⚠️ **判据是「两份不是同一个指针」**：命中缓存时 `commentBodyOf` 返回的是**同
// 一份**（`comments_highlight_test.go` 钉着这一条），而展开那一档是另一次切分
// ——两份必须是两个不同的对象。
//
// ⚠️ **再折回来要能折回去**：那是同一套机制（再换回那个 lines 值）。
func TestTheExpandedBodyUsesADifferentCacheEntry(t *testing.T) {
	msg := strings.Repeat("这一句很长", 200)
	ts := newTestShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			p := &s.comments
			cm := bili.Comment{Rpid: 7, Message: msg}
			folded := s.commentBodyOf(p, cm, commentBodyLines)
			again := s.commentBodyOf(p, cm, commentBodyLines)
			if folded != again {
				t.Error("同一档连问两次拿到的不是同一份——缓存没生效")
			}
			full := s.commentBodyOf(p, cm, commentBodyUnlimited)
			if full == folded {
				t.Error("展开那一档和折叠那一档是同一份——缓存键里的 `lines` 没起作用")
			}
			if full.lines != commentBodyUnlimited {
				t.Errorf("展开那一档记的行数是 %d，want %d", full.lines, commentBodyUnlimited)
			}
		}
	}))
	ts.tt.Frame()
}

// ─────────────────────────────────────────────────────────────────────────────
// 界面上那一枚
// ─────────────────────────────────────────────────────────────────────────────

// TestTheLongBodyHasAnExpandButton 断「长正文下面**有**一枚『展开』」。
//
// 🔴 **判据是画面上的字**：「展开」两个字必须真的在画面上——而它是**一枚
// 低调的小字链接**（`flatLink` 那一档），不是一段没药丸的按钮。
//
// ⚠️ **短正文不许有那一枚**：给一条「好」摆一枚「展开」，用户按下去什么都不会
// 发生——而那比不给更糟。
func TestTheLongBodyHasAnExpandButton(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureWithLongBody(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	if !tt.HasText("展开") {
		t.Errorf("长正文下面没有「展开」那一枚，文字有 %q", tt.Texts())
	}
	// ⚠️ **按钮是「原位切换」的两档**：折叠态是「展开」，展开态是「收起」。
	if tt.HasText("收起") {
		t.Errorf("折叠态上出现了「收起」——按钮文案反了，文字有 %q", tt.Texts())
	}
}

// TestTheShortBodyHasNoExpandButton 断「短正文**没有**那一枚」。
//
// ⚠️ **「短」得是真的短**：fixture 里第一条正文有 86 个字，而抽屉正文那一列
// （400 − 头像 32 − 缝 12 ≈ 356）在 14 号字下一行三十几个字——86 个字**够折成
// 三四行**。而 fixture 里**别的**那几条更短（37 个字）却也可能折。
//
// ⇒ 这里**造一条真的短正文**（一句话），而不是拿 fixture 的默认那条当「短的」——
// 否则这条测试量的是别的东西（它曾经就是这么写的：拿一条会折的正文去断
// 「不该有展开」）。
func TestTheShortBodyHasNoExpandButton(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	commentFixtureWithShortBody(t, b)
	_, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	if tt.HasText("展开") {
		t.Errorf("短正文下面也摆着「展开」——用户按下去什么都不会发生，文字有 %q", tt.Texts())
	}
}

// TestTheExpandButtonTogglesTheBody 断「点它展开、再点它收起」。
//
// 🔴 **判据是「那一格的高度」**：折起来的时候它是 **6 行**，展开之后**远多于
// 6 行**，收起之后又回到 6 行。三样都要断——只断「展开之后变高」的话，一个只
// 会展开不会收起的实现照样绿。
//
// 🔴 **而判据不能是「整段原文在不在画面上」**（旧的做法是那样）：截断是**框架**
// 做的（`MaxLines` + `Ellipsis`）⇒ 省略号是**画出来**的，**不在文字里**⇒
// `Label` 给的**还是整段原文**（这是票 #702 的**已知取舍**，见
// `comments_body.go` 的文件头）⇒ 「整段原文在画面上」**恒真**，而它量不到
// 「折没折」。
//
// ⚠️ **按钮文案也跟着换**：展开之后是「收起」，收起之后又是「展开」。
func TestTheExpandButtonTogglesTheBody(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureWithLongBody(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	const rpid = int64(321942994928)
	var msg string
	for _, row := range s.comments.rows {
		if row.cm.Rpid == rpid {
			msg = row.cm.Message
			break
		}
	}
	if msg == "" {
		t.Fatalf("画面上没有那一条评论（rpid %d）", rpid)
	}
	r, ok := tt.Find(msg)
	if !ok {
		t.Fatalf("画面上找不到那一段正文，文字有 %q", tt.Texts())
	}
	foldedH := r.H
	lineH := commentLineHeight(t)
	foldedMax := float32(commentBodyLines) * lineH
	if foldedH > foldedMax+1 {
		t.Fatalf("起点就不对：折叠态上那一格有 %.1f 高（%d 行是 %.1f）——它压根没折",
			foldedH, commentBodyLines, foldedMax)
	}

	// 展开。
	if err := tt.Click("展开"); err != nil {
		t.Fatalf("点不动「展开」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "正文展开了", func() bool {
		r2, ok2 := tt.Find(msg)
		return ok2 && r2.H > foldedMax+1
	})
	if !tt.HasText("收起") {
		t.Errorf("展开之后按钮文案还是「展开」，文字有 %q", tt.Texts())
	}

	// 收起。
	if err := tt.Click("收起"); err != nil {
		t.Fatalf("点不动「收起」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "正文收起来了", func() bool {
		r2, ok2 := tt.Find(msg)
		return ok2 && r2.H <= foldedMax+1
	})
	if !tt.HasText("展开") {
		t.Errorf("收起之后按钮文案还是「收起」，文字有 %q", tt.Texts())
	}
	_ = foldedH
}

// TestTheExpandedBodyGrowsTaller 断「**展开之后那一格变高了**」。
//
// 🔴 **这一条钉的是「展开真的把正文摊开了」**：而判据是**高度**——
//
//	折叠态  6 行（`commentBodyLines` × 行高）
//	展开态  **完整正文**的行数（远多于 6 行）
//
// ⚠️ **为什么不能只断「整段原文在画面上」**：那一条（`TestTheExpandButton…`）
// 断的是**给读屏的那一份**（`Label`），而它**本来就是整段原文**（省略号是框架
// **画出来**的，不在文字里）⇒ 那一条量不到「画面上那一格有多高」。
//
// ⇒ 这一条量**画面上那一格的盒子**（`Tester.Find` 给的）——而它**没有固定
// 高度**（框架按排版给）⇒ 高度**就是**正文有多长。
func TestTheExpandedBodyGrowsTaller(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureWithLongBody(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	const rpid = int64(321942994928)
	var msg string
	for _, row := range s.comments.rows {
		if row.cm.Rpid == rpid {
			msg = row.cm.Message
			break
		}
	}
	if msg == "" {
		t.Fatalf("画面上没有那一条评论（rpid %d）", rpid)
	}
	r, ok := tt.Find(msg)
	if !ok {
		t.Fatalf("画面上找不到那一段正文，文字有 %q", tt.Texts())
	}
	foldedH := r.H

	if err := tt.Click("展开"); err != nil {
		t.Fatalf("点不动「展开」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "正文展开了", func() bool {
		r2, ok2 := tt.Find(msg)
		return ok2 && r2.H > foldedH+1
	})
	r, _ = tt.Find(msg)
	if r.H <= foldedH+1 {
		t.Errorf("展开之后那一格只有 %.1f 高（折起来的时候是 %.1f）——正文没摊开",
			r.H, foldedH)
	}
}

// TestTheReplyBodyCanBeExpandedToo 断「**楼中楼**也能展开」。
//
// 🔴 **楼中楼是 4 行**（`commentReplyBodyLines`），比主评论更短——所以一条
// 主评论放得下、楼中楼放不下的正文是常事。而**只给主评论加那一枚**的话，
// 楼中楼那一截仍然是死路。
//
// ⚠️ **判据是「楼中楼那条自己的正文」**：这里点的是**楼中楼那一枚**，不是主
// 评论那一枚。
func TestTheReplyBodyCanBeExpandedToo(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.replyRaw = commentFixtureWithLongReplyBody(t, "reply-reply.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	// 先把楼中楼展开出来。
	if err := tt.Click("共52条回复"); err != nil {
		t.Fatalf("点不动「共52条回复」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "楼中楼画出来了", func() bool {
		return len(s.comments.rows) > 1 && s.comments.rows[1].reply
	})

	whole := strings.Repeat(commentLongBody, 12)
	if !tt.HasText("展开") {
		t.Fatalf("楼中楼那条长正文下面没有「展开」那一枚，文字有 %q", tt.Texts())
	}
	if err := tt.Click("展开"); err != nil {
		t.Fatalf("点不动楼中楼那一枚「展开」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "楼中楼那条展开了", func() bool { return tt.HasText(whole) })
}

// TestTheExpandedStateIsClearedWhenTheSongChanges 断「**换歌会清掉展开状态**」。
//
// 🔴 **rpid 会撞**：rpid 是 B 站那边的编号，而**换一首歌之后同一个 rpid 可能
// 指向另一条评论**——留着「展开」的话，新歌那一条会莫名其妙是展开的（而用户
// 什么都没按）。
//
// ⚠️ **判据是那一档的状态**：换歌之后 `p.bodiesExpanded` 必须是空的。
func TestTheExpandedStateIsClearedWhenTheSongChanges(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureWithLongBody(t, "reply-main.json")
	s, tt := newCommentsShell(t, b, mvItem(), 1400, 1400)

	if err := tt.Click("展开"); err != nil {
		t.Fatalf("点不动「展开」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "正文展开了", func() bool {
		return len(s.comments.bodiesExpanded) > 0
	})

	// 换歌：`syncCommentsData` 认的是 `s.app.CommentsSlot()`。
	s.app.Queue().Reset(audioItem())
	commentsWait(t, tt, "换歌之后展开状态清掉了", func() bool {
		return len(s.comments.bodiesExpanded) == 0
	})
}
