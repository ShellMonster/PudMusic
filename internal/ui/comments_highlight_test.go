// 「筛选开着的时候，评论里命中的歌名高亮出来」（票 #684）的验收。
//
// # 这一族断的是**画面上的像素**
//
// 「某个 span 的 Background 被设了」不算数：那一层底色可能被后画的东西盖掉、
// 可能落在截断线之外、也可能颜色和底几乎一样（等于没高亮）。⇒ 判据是
// **开关打开前后正文那一块的像素变了**，而且变了的是**主色那层淡底**的颜色。
//
// ⚠️ **数据的形状要挑**：第一条评论提到歌名，后面几条没有。打开筛选之后第一条
// 仍在第一位（它是命中的那一条），所以正文那一格的**位置不变**——前后两张图能
// 逐像素比。
//
// ⚠️ **一个真请求都不发**：假 B 站 + 真 fixture（理由见 `comments_test.go` 的
// 文件头）。
package ui

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/songname"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	mygoui "github.com/egoist/mygo/ui"
)

// commentShotCopy 把 Tester 那一帧拷下来（`tt.Image()` 的像素是**下一帧会被
// 覆写**的那块缓冲，前后两张图要各自留一份）。
//
// ⚠️ **名字带 `commentShot`**：本包已经有别人的 `copyOf` / `diffIn` 一类的
// 辅助（`like_test.go`、`like_btn_test.go`）——同名会让整个包编不过，而
// 合并时 Git 看不见这种冲突，只有编译器看得见。
func commentShotCopy(tt *mygoui.Tester) *image.RGBA {
	src := tt.Image()
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	return dst
}

// commentShotDiff 数两块图在同一块矩形里有几个像素不一样。
func commentShotDiff(a, b *image.RGBA, r image.Rectangle) int {
	r = r.Intersect(a.Bounds()).Intersect(b.Bounds())
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// commentShotDiffColor 是两个颜色的通道差的最大值（比像素用的是同一个口径，
// 见 `flatbutton_test.go` 的 `maxDiff`——这里不收 `*testing.T`，因为它是在
// 扫描循环里调的）。
func commentShotDiffColor(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x) - int(y)
		}
		return int(y) - int(x)
	}
	return max(d(a.R, b.R), max(d(a.G, b.G), d(a.B, b.B)))
}

// washPixelsIn 数一块矩形里有几个像素是「主色那层淡底」的颜色。
//
// ⚠️ **颜色是现算的**（`commentMarkOf` 那套：主色 18% 压在页面底色上）：主色
// 是用户可设的，写死一个绿的话，换了主色这一条就量不到东西。
//
// ⚠️ **容差 8**：底色是实心填的（圆角那几处有抗锯齿），而抗锯齿的边缘像素是
// 底色和行底色的混合——数得进来只会让判据更宽，不会让「根本没画」蒙混过关。
func washPixelsIn(tt *mygoui.Tester, dark bool, r image.Rectangle) int {
	img := tt.Image()
	r = r.Intersect(img.Bounds())
	th := BuildTheme(app.DefaultSettings(""), dark)
	wantColor := th.Accent.Alpha(commentMarkAlpha).Over(th.Background)
	want := color.RGBA{R: wantColor.R, G: wantColor.G, B: wantColor.B, A: wantColor.A}
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if commentShotDiffColor(p, want) <= 8 {
				n++
			}
		}
	}
	return n
}

// TestTheMentionedSongNameIsHighlightedInTheCommentBody 断「筛选开着时正文里
// 命中的那一段染上了主色底」。
//
// 🔴 **判据是像素**：
//
//	关着   正文那一块**一个**主色底像素都没有（高亮是筛选的一部分，不是常驻
//	       装饰——常驻的话满屏都是色块，用户看不出哪一条是「筛出来的」）
//	打开   正文那一块出现了主色底像素，而且**只有那一块变了**
//	关回去 同一块又回到关着时的样子
func TestTheMentionedSongNameIsHighlightedInTheCommentBody(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newCommentBili(t, "reply-main.json")
			b.mainRaw = commentFixtureMentionNoMore(t, "晴天")
			_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1000, systemIsDark(dark))

			// 第一条评论的正文（`viewCommentBodyEl` 用 `.Label(plain)` 把正文交给
			// 框架，所以 `Find` 找得到它）。
			const body = "「晴天」这一段我循环了一整天"
			r, ok := tt.Find(body)
			if !ok {
				t.Fatalf("画面上找不到第一条评论的正文，文字有 %q", tt.Texts())
			}
			rect := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))

			off := commentShotCopy(tt)
			if n := washPixelsIn(tt, dark, rect); n != 0 {
				t.Fatalf("筛选还没开，正文里就有 %d 个主色底像素——高亮不该常驻", n)
			}

			if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
				t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
			}
			commentsWait(t, tt, "筛完了", func() bool {
				return tt.HasText("1 条提到这首歌")
			})
			// ⚠️ **正文的内容一个字都没变**（高亮是装饰）：给辅助技术的那一份
			// 还是那条评论的原文。
			if !tt.HasText(body) {
				t.Fatalf("高亮之后正文的文字变了，文字有 %q", tt.Texts())
			}
			if n := washPixelsIn(tt, dark, rect); n == 0 {
				t.Errorf("筛选开着，正文里却一个主色底像素都没有——命中的歌名没高亮")
			}
			on := commentShotCopy(tt)
			if n := commentShotDiff(off, on, rect); n == 0 {
				t.Errorf("开关打开前后正文那一块的像素一模一样——高亮没画出来")
			}

			// 关回去：同一块又是关着时的样子。
			if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
				t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
			}
			commentsWait(t, tt, "筛选关掉了", func() bool {
				return tt.HasText("共 388 条")
			})
			off2 := commentShotCopy(tt)
			if n := commentShotDiff(off, off2, rect); n != 0 {
				t.Errorf("关掉筛选之后正文那一块有 %d 个像素和打开之前不一样——高亮没消失", n)
			}
			if n := washPixelsIn(tt, dark, rect); n != 0 {
				t.Errorf("关掉筛选之后还有 %d 个主色底像素——高亮没消失", n)
			}
		})
	}
}

// TestTheReplyMentionIsHighlightedToo 断「楼中楼那一档也高亮」。
//
// 🔴 **两个调用点是同一条路**（`viewCommentBodyEl` → `commentBodyOf` →
// `shapeCommentBody`，只是行数不同）：主评论 6 行、楼中楼 4 行。漏掉楼中楼
// 的症状是「主评论染了色，展开的回复一条都没染」——而筛选开着时楼中楼也是
// 按它自己的正文筛的（票 #574），用户自然也会问「它哪儿提到了」。
func TestTheReplyMentionIsHighlightedToo(t *testing.T) {
	const replyBody = "这条回复说晴天很好听"

	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureMentionNoMore(t, "晴天")
	b.files["reply-reply.json"] = commentFixtureReplyMentions(t, replyBody)
	_, tt := newCommentsShell(t, b, mentionItem(), 1400, 1400)

	// 展开第一梯楼中楼。
	if err := tt.Click("共52条回复"); err != nil {
		t.Fatalf("点不动「共 52 条回复」：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "楼中楼展开了", func() bool {
		return tt.HasText(replyBody)
	})
	r, ok := tt.Find(replyBody)
	if !ok {
		t.Fatalf("画面上找不到那条楼中楼，文字有 %q", tt.Texts())
	}
	rect := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
	if n := washPixelsIn(tt, false, rect); n != 0 {
		t.Fatalf("筛选还没开，楼中楼里就有 %d 个主色底像素", n)
	}
	off := commentShotCopy(tt)

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	commentsWait(t, tt, "筛完了", func() bool {
		return tt.HasText("条提到这首歌")
	})
	on := commentShotCopy(tt)
	if n := commentShotDiff(off, on, rect); n == 0 {
		t.Errorf("开关打开前后楼中楼正文那一块一模一样——楼中楼没高亮")
	}
	if n := washPixelsIn(tt, false, rect); n == 0 {
		t.Errorf("筛选开着，楼中楼正文里一个主色底像素都没有——它没高亮")
	}
}

// commentFixtureReplyMentions 是「第一条楼中楼的正文换成 replyBody」那一份
// fixture。
//
// ⚠️ **形状照真 fixture**（member / content / reply_control 一个字都不改），
// 只把正文换掉——这一条要验的是排版与高亮，而它们只跟正文有关。
func commentFixtureReplyMentions(t *testing.T, replyBody string) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(commentFixtureBytes(t, "reply-reply.json"), &doc); err != nil {
		t.Fatalf("fixture 读不懂：%v", err)
	}
	data, _ := doc["data"].(map[string]any)
	replies, _ := data["replies"].([]any)
	if len(replies) == 0 {
		t.Fatalf("fixture 里一条楼中楼都没有")
	}
	first, _ := replies[0].(map[string]any)
	content, _ := first["content"].(map[string]any)
	content["message"] = replyBody
	delete(content, "emote")
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("fixture 编不出来：%v", err)
	}
	return out
}

// TestTheHighlightInkStaysReadableOnItsOwnWash 断「高亮那一段的字压在自己的
// 淡底上还读得出来」。
//
// 🔴 **WCAG 正文 AA 的 4.5:1**（刻度见 `widgets/ink` 的 `TextContrast`）。
//
// ⚠️ **两个底都要过**：正文画在列表行里，而那一行**悬停的时候底色换成
// `t.SurfaceHover`**（`mygo/ui/list.go`）——高亮那层淡底压在上面，合出来的
// 颜色比压在页面底色上更暗（浅色）/ 更亮（深色）。只按页面底色算的话，指针
// 一停上去这一段的字就可能掉到 4.5 以下。
//
// ⚠️ **主色是用户可设的**：换几个色相都不同的主色再算一遍——写死一个绿的话，
// 用户挑了别的色这一条就会红。
func TestTheHighlightInkStaysReadableOnItsOwnWash(t *testing.T) {
	accents := []string{
		"#1ed760", // Biu 的绿（默认）
		"#ff0000", // 深红
		"#ffe600", // 亮黄
		"#2563eb", // 中蓝
		"#808080", // 灰
		"#00ffff", // 青
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			for _, hex := range accents {
				s := app.DefaultSettings("")
				s.PrimaryColor = hex
				theme := BuildTheme(s, dark)
				mark := commentMarkOf(theme)
				if mark.Bg.A == 0 {
					t.Fatalf("主色 %s：高亮底色是透明的", hex)
				}
				for _, c := range []struct {
					what string
					bg   mygoui.Color
				}{
					{"页面底色", theme.Background},
					{"行悬停底", theme.SurfaceHover},
				} {
					bg := mark.Bg.Over(opaqueBackground(c.bg))
					if r := ink.Ratio(mark.Ink, bg); r < ink.TextContrast {
						t.Errorf("%s 主色 %s：高亮字 %v 压自己的淡底 %v（%s）只有 %.2f:1（< 4.5）",
							name, hex, mark.Ink, bg, c.what, r)
					}
				}
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 切 span 那一层的单元判据
// ─────────────────────────────────────────────────────────────────────────────

// sameSpans 报两串 span 是不是同一份（逐段比文本与画法）。
//
// ⚠️ **它比的是「画法」**：高亮那几段的字色和底色是**烘进 span 里**的
// （`ui.Span.Color` / `Background`），而配色一变那些字段就变了——所以只比
// 文本的话，「配色变了却没重切」那个实现照样绿。
func sameSpans(a, b []mygoui.Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTheEmoteTokenIsNeverPainted 断「命中区间盖住表情占位符时，那一格不染色」。
//
// 🔴 **那一格画的是一张图，不是字**：给它套一层底色的话，画面上就是表情图
// 四周多出一块方方的色块（`Span.Background` 会贴着字形外沿画，而 U+FFFC 的
// 「字形」就是那一枚 18×18 的 object）。⇒ 表情占位符整段让出去。
//
// ⚠️ **判据是「没有一个 span 同时带着底色和 U+FFFC」**，不是「区间切得对不对」：
// 前者才是画面上看得见的那件事。
func TestTheEmoteTokenIsNeverPainted(t *testing.T) {
	emotes := map[string]bili.CommentEmote{
		"[妙啊]": {URL: "https://i0.hdslb.com/bfs/emote/x.png"},
	}
	// 正文：「晴天[妙啊]真好听」——命中「晴天[妙啊]真好听」整段（把占位符包进去）。
	const msg = "晴天[妙啊]真好听"
	hl := []songname.Range{{Start: 0, End: 9}}
	mark := commentMark{Bg: mygoui.Hex("#1ed760").Alpha(0.18), Ink: mygoui.Hex("#107032"), Radius: 2}
	spans, urls, _ := commentSpansOf(msg, emotes, hl, mark)
	if len(urls) != 1 {
		t.Fatalf("表情图有 %d 张，want 1", len(urls))
	}
	for i, sp := range spans {
		if strings.ContainsRune(sp.Text, '\uFFFC') && sp.Background.A != 0 {
			t.Errorf("第 %d 段 %q 既是表情占位符又带着底色——那一格会多出一块方色块",
				i, sp.Text)
		}
	}
	// ⚠️ **给读屏那一份一个字都不许变**（高亮是装饰）。
	if plain := commentPlain(msg, emotes); plain != "晴天［表情］真好听" {
		t.Errorf("读屏那一份是 %q，want「晴天［表情］真好听」", plain)
	}
}

// TestTheHighlightSurvivesAnEmoteInTheMiddle 断「占位符两侧的文字照样染色」。
//
// ⚠️ **只把那一格让出去，不是整段都不染**：整段跳过的话，命中那一段里凡是
// 带表情的评论就一点高亮都没有——而「提到了」这件事仍然是真的。
func TestTheHighlightSurvivesAnEmoteInTheMiddle(t *testing.T) {
	emotes := map[string]bili.CommentEmote{
		"[妙啊]": {URL: "https://i0.hdslb.com/bfs/emote/x.png"},
	}
	const msg = "晴天[妙啊]真好听"
	hl := []songname.Range{{Start: 0, End: 9}}
	mark := commentMark{Bg: mygoui.Hex("#1ed760").Alpha(0.18), Ink: mygoui.Hex("#107032"), Radius: 2}
	spans, _, _ := commentSpansOf(msg, emotes, hl, mark)
	var painted []string
	for _, sp := range spans {
		if sp.Background.A != 0 {
			painted = append(painted, sp.Text)
		}
	}
	if len(painted) != 2 || painted[0] != "晴天" || painted[1] != "真好听" {
		t.Errorf("染了底色的那几段是 %q，want [「晴天」「真好听」]", painted)
	}
}

// TestTheSpanTextsStillSpellTheWholeBody 断「切完之后拼起来还是那句话」。
//
// 🔴 **切 span 只是画法**：`ShapeRichText` 拿的是一串 span，而它的文字必须
// 逐字等于原文——少一个字，画面上那句话就少一个字（而屏幕上那句话和读屏
// 读到的那一份（`plain`）就对不上了）。
func TestTheSpanTextsStillSpellTheWholeBody(t *testing.T) {
	emotes := map[string]bili.CommentEmote{
		"[妙啊]": {URL: "https://i0.hdslb.com/bfs/emote/x.png"},
	}
	cases := []struct {
		msg string
		hl  []songname.Range
	}{
		{"晴天真的绝了", []songname.Range{{Start: 0, End: 2}}},
		{"晴天[妙啊]真好听", []songname.Range{{Start: 0, End: 9}}},
		{"[妙啊]晴天", []songname.Range{{Start: 3, End: 5}}},
		{"晴天", []songname.Range{{Start: 0, End: 2}, {Start: 0, End: 2}}},
		{"let it go 真好听", []songname.Range{{Start: 0, End: 9}}},
		{"晴天", nil},
	}
	mark := commentMark{Bg: mygoui.Hex("#1ed760").Alpha(0.18), Ink: mygoui.Hex("#107032"), Radius: 2}
	for _, tc := range cases {
		spans, urls, _ := commentSpansOf(tc.msg, emotes, tc.hl, mark)
		var got strings.Builder
		for _, sp := range spans {
			got.WriteString(sp.Text)
		}
		want := tc.msg
		if strings.Contains(tc.msg, "[妙啊]") {
			want = strings.ReplaceAll(tc.msg, "[妙啊]", "\uFFFC")
		}
		if got.String() != want {
			t.Errorf("正文 %q：切完拼起来是 %q，want %q", tc.msg, got.String(), want)
		}
		if n := strings.Count(want, "\uFFFC"); n != len(urls) {
			t.Errorf("正文 %q：%d 枚占位符配了 %d 张图", tc.msg, n, len(urls))
		}
	}
}

// 🔴 **「截断之后，截断点之后的高亮丢掉」这一档不再需要**（票 #702）：
//
// 旧的实现里**截断是这一页自己做的**（自己二分、自己量高度），所以**截断
// 点之后的那一截正文**要**从 span 里切掉**——留着它只会去切一段不存在的文字。
//
// 而现在**截断是框架做的**（`ui.RichText` 的 `MaxLines` + `Ellipsis`）：框架
// 自己知道切到哪儿，**画出来的**只有切点之前的那几行——而**高亮是按原文的
// rune 坐标**烘进 span 里的，于是切点之后的那几段**画不出来**（它们不在那一
// 行上）⇒ **不用裁**。
//
// ⚠️ **「那一段染着底色」这件事仍然由画面上的像素盯着**
// （`TestALongBodyWithAMentionIsStillHighlighted` 那一族）——而这一族现在量的
// ���「折起来之后**还在画面上**的那几行里，命中的那一段还染着底色」。

// TestALongBodyWithAMentionIsFoldedAndStillHighlighted 断「长正文折起来之后，
// 截断点之前的那一段还染着色」。
//
// 🔴 **两种错法都能被这条抓住**：
//
//	不裁区间   切 span 时那一段的坐标跑到了正文之外（画面上那句话的字数不对，
//	           或者 `ShapeRichText` 拿到的 span 拼起来不是正文）
//	整段不染   为了躲开那种错，干脆在截断那一档不染——那正好把用户最需要
//	           高亮的长评论漏掉
//
// ⚠️ **判据仍然是画面上的像素**（理由见文件头）：找正文那一格用它的
// `plain`（折起来之后的那一句，末尾带省略号）。
func TestALongBodyWithAMentionIsFoldedAndStillHighlighted(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureLongBodyMentioning(t, "晴天")
	s, tt := newCommentsShell(t, b, mentionItem(), 1400, 1000)

	if err := tt.Click("开关：只看提到歌名的评论"); err != nil {
		t.Fatalf("点不了那枚开关：%v（文字：%q）", err, tt.Texts())
	}
	var msg string
	commentsWait(t, tt, "长正文折起来并高亮", func() bool {
		for _, row := range s.comments.rows {
			if b, ok := s.comments.bodies[row.cm.Rpid]; ok && b.mark.Bg.A != 0 {
				msg = row.cm.Message
				return true
			}
		}
		return false
	})
	// ⚠️ **判据是「那一格只有 6 行那么高」**：折起来是**框架**做的事
	//（`MaxLines` + `Ellipsis`），而省略号是**画出来**的、**不在文字里**——
	// 所以「那一段以『…』收尾」那个旧判据**不再成立**（`Label` 给的是整段
	// 原文）。
	//
	// ⚠️ **量的是画面上那一格的盒子**（`Tester.Find` 给的）：正文那一格**没有
	// 固定高度**（框架按排版给），而它装的是**折起来之后**的那几行。
	if !strings.HasPrefix(msg, "晴天") {
		t.Fatalf("折起来的正文不是从歌名开头：%q", msg)
	}
	r, ok := tt.Find(msg)
	if !ok {
		t.Fatalf("画面上找不到折起来的那句正文，文字有 %q", tt.Texts())
	}
	lineH := commentLineHeight(t)
	if r.H > float32(commentBodyLines)*lineH+1 {
		t.Errorf("这条正文那一格有 %.1f 高，而 %d 行只有 %.1f——它没有被折起来",
			r.H, commentBodyLines, float32(commentBodyLines)*lineH)
	}
	if r.H >= lineH*10 {
		t.Errorf("这条正文那一格有 %.1f 高（10 行以上）——它压根没折", r.H)
	}
	rect := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
	if n := washPixelsIn(tt, false, rect); n == 0 {
		t.Errorf("折起来的正文里一个主色底像素都没有——截断那一档把高亮丢了")
	}
}

// commentFixtureLongBodyMentioning 是「第一条评论的正文以歌名开头、长到一定
// 折起来」那一份 fixture。
//
// ⚠️ **歌名摆在最前面**：那样它一定落在折起来之后还看得见的那几行里——
// 而「截断点之后的高亮不许画」那一半由 `TestTheRangesAreClippedToTheFoldedText`
// 直接钉住。
func commentFixtureLongBodyMentioning(t *testing.T, mention string) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(commentFixtureBytes(t, "reply-main.json"), &doc); err != nil {
		t.Fatalf("fixture 读不懂：%v", err)
	}
	data, _ := doc["data"].(map[string]any)
	replies, _ := data["replies"].([]any)
	if len(replies) == 0 {
		t.Fatalf("fixture 里一条评论都没有")
	}
	first, _ := replies[0].(map[string]any)
	content, _ := first["content"].(map[string]any)
	content["message"] = mention + "真的绝了，" + strings.Repeat(commentLongBody, 12)
	cursor, _ := data["cursor"].(map[string]any)
	cursor["all_count"] = 6
	cursor["is_end"] = true
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("fixture 编不出来：%v", err)
	}
	return out
}

// TestTheHighlightFollowsTheTheme 断「换了主题（深浅 / 主色）之后，已经排好的
// 那一份正文会重排」。
//
// 🔴 **高亮那几段的字色和底色是烘进 span 里的**（`ui.Span.Color` /
// `Background`），而正文那一份是**缓存**着的（`commentBodySpans`）——键里不带
// 配色的话，用户换了深浅两套之后那几段还是上一次主题的颜色（其余的字是画的
// 时候从主题现取的，于是页面上就出现了「一半新色、一半旧色」）。
//
// ⚠️ **判据是重排了没有**（`mark` 变了 ⇒ 缓存不命中 ⇒ 重新排一份），不是
// 「那两段颜色对不对」——后者要求把主题换掉再画一帧，而换主题在无头 Tester
// 里要换设置（`systemIsDark` 是**建外壳时**的选项）。
func TestTheHighlightFollowsTheTheme(t *testing.T) {
	p := &commentsPage{mentionOnly: true, names: songname.New("晴天", "周杰伦")}
	cm := bili.Comment{Rpid: 1, Message: "晴天真好听"}

	// ⚠️ **版面按 `p.mark` 烘进去**，所以这里把两份配色先后摆上，看缓存认得
	// 出它们不是同一份。
	ts := newTestShell(t, at(1200, 900), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			p.mark = commentMarkOf(c.Theme())
			first := s.commentBodyOf(p, cm).spans
			// 同一份配色再要一次：**必须是同一份**（缓存命中了）。
			again := s.commentBodyOf(p, cm).spans
			if !sameSpans(first, again) {
				t.Errorf("配色没变，正文却重切了（缓存没命中）")
			}
			if got := p.bodies[cm.Rpid]; got == nil || got.mark != p.mark {
				t.Errorf("缓存下来的配色是 %+v，want %+v", got, p.mark)
			}
			// 换一份配色（模拟用户换了深浅）：**必须重排**。
			//
			// ⚠️ **换的是深浅，不是主色**：默认主题方案（`DefaultThemePreset`）
			// 会盖掉主色（`applyPreset` 在那之后），所以只改 `PrimaryColor`
			// 这一份主题**一个字节都不变**——那样这条用例量到的会是「没变所以
			// 没重排」，而它想钉的是「变了必须重排」。
			p.mark = commentMarkOf(BuildTheme(app.DefaultSettings(""), true))
			second := s.commentBodyOf(p, cm).spans
			if sameSpans(first, second) {
				t.Errorf("配色变了，正文却用的还是缓存那一份——高亮那几段会是旧主题的颜色")
			}
			// ⚠️ **缓存里记的必须是**新**的那份配色**：不然下一帧拿到的还是旧
			// 配色（而高亮的字色和底色是**烘进 span 里**的）。
			if got := p.bodies[cm.Rpid]; got.mark != p.mark {
				t.Errorf("重切之后缓存里的配色是 %+v，want %+v", got.mark, p.mark)
			}
		}
	}))
	ts.tt.Frame()
}

// TestTheCachedBodyFollowsANewAliasTable 断「别名表换了之后，正文排版缓存
// 不能还留着旧表算出来的那一份」。
//
// 🔴 **缓存键里没有别名表**（`commentBodySpans` 的键是 rpid / 配色）：换歌
// （或者同一首的标题、艺人变了）时 `syncComments` 换了 `p.names`，而那一份
// 缓存还留着——筛选用的是新歌名，正文里标的却是旧歌名的位置。
//
// ⚠️ **判据是「新切了一份」**：缓存命中时 `commentBodySpans` 返回的是**同一
// 份**（`TestTheMatchingRunsOncePerCommentNotPerFrame` 钉住的那条路），所以
// 换了别名表之后必须是**另一份**——那一份是拿新表的 `Matches` 切的
// （`commentBodySpans` 里唯一跑匹配的地方）。
//
// ⚠️ **紧接着再 sync 一次（什么都没变）必须还是同一份**：每帧都清的话排版
// 缓存整个作废，帧预算那一档（`framebudget_test.go`）会红——所以这一条也把
// 「只在别名表真的换了时清」钉住。
func TestTheCachedBodyFollowsANewAliasTable(t *testing.T) {
	const (
		oldName = "晴天"
		newName = "告白气球"
		body    = "晴天和告白气球都好听"
	)
	// 当前这一首叫**新**歌名：`syncComments` 那一支靠标题变了认出「别名表要
	// 重算」。
	it := mvItem()
	it.Title = newName

	var first, second, again *commentBody
	var gotTitle string
	done := false
	ts := newTestShell(t, at(1200, 900),
		stubbed(func(_ *testing.T, s *Shell) { s.app.Queue().Reset(it) }),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				if done {
					return
				}
				done = true
				s.wake.set(c)
				s.applyFrameTheme(c)
				p := &s.comments
				// 摆成「上一帧还是《晴天》」：槽位一致（不走换歌那一支）、
				// 别名表是旧的、开关开着、配色算好。
				p.slot = s.app.CommentsSlot()
				p.srcTitle, p.srcArtist = oldName, it.Artist
				p.srcMention = true
				p.names = songname.New(oldName, it.Artist)
				p.mentionOnly = true
				p.mark = commentMarkOf(c.Theme())

				cm := bili.Comment{Rpid: 7, Message: body}
				s.commentBodyOf(p, cm)
				first = p.bodies[cm.Rpid]

				// 这一帧当前这一首叫「告白气球」⇒ 走别名表重算那一支。
				s.syncComments(c)
				gotTitle = p.srcTitle
				s.commentBodyOf(p, cm)
				second = p.bodies[cm.Rpid]

				// 什么都没变的下一帧：排版缓存**必须还在**。
				s.syncComments(c)
				s.commentBodyOf(p, cm)
				again = p.bodies[cm.Rpid]
			}
		}))
	ts.tt.Frame()

	if gotTitle != newName {
		t.Fatalf("`syncComments` 没走别名表重算那一支（srcTitle 是 %q）——这条用例的前提不成立", gotTitle)
	}
	if first == nil || second == nil || again == nil {
		t.Fatalf("正文没排出来：first=%v second=%v again=%v", first, second, again)
	}
	if second == first {
		t.Errorf("别名表换成《%s》了，正文却还是缓存里那一份——高亮区间还是按旧歌名《%s》算的",
			newName, oldName)
	}
	if again != second {
		t.Errorf("这一帧别名表没变，正文却重切了——排版缓存被每帧清掉了（帧预算会红）")
	}
}

// TestTheMatchingRunsOncePerCommentNotPerFrame 断「正文排好之后，每一帧不再
// 跑一遍匹配」。
//
// 🔴 **`Matches` 每条评论分配十几次**（它要拼一份归一化串和一张映射表），而
// 这一段每一帧都要问一遍每一条评论。缓存命中的时候还得先算一遍匹配的话，一屏
// 八行就是**每帧一百多次白花的分配**——`framebudget_test.go` 那一档（88 次）
// 会红。
//
// ⚠️ **判据是「同一份」**：`commentBodySpans` 命中缓存时返回的是**同一份**
// ——而匹配只在那一次重切里跑。
func TestTheMatchingRunsOncePerCommentNotPerFrame(t *testing.T) {
	var first *commentBody
	ts := newTestShell(t, at(1200, 900), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.comments.names = songname.New("晴天", "某个 UP 主")
			s.comments.mentionOnly = true
			s.comments.mark = commentMarkOf(c.Theme())
			cm := bili.Comment{Rpid: 7, Message: "晴天真好听"}
			s.commentBodyOf(&s.comments, cm)
			b1 := s.comments.bodies[cm.Rpid]
			s.commentBodyOf(&s.comments, cm)
			b2 := s.comments.bodies[cm.Rpid]
			s.commentBodyOf(&s.comments, cm)
			b3 := s.comments.bodies[cm.Rpid]
			if b1 != b2 || b2 != b3 {
				t.Errorf("同一份正文（配色没变）却重切了")
			}
			first = b1
		}
	}))
	ts.tt.Frame()
	if first == nil {
		t.Fatal("那一帧没排出版本")
	}
	if first.mark.Bg.A == 0 {
		t.Errorf("那一份正文没带上高亮配色")
	}
}
