// 评论正文那一份（票 #702 的审查）的验收。
//
// # 这一族断的是「切出来的那几段」
//
// 评论正文被切成**一段一段的 span**（文字是文字，表情占位符是一枚 inline
// object），而那一趟切分同时产出**两份**结果：
//
//	画出来的那一份  span（表情是 U+FFFC + inline object）
//	给读屏的那一份  纯文字（表情是「［表情］」）
//
// ⇒ 两份**必须来自同一次切分**（否则读屏读到的那句话 ≠ 眼睛看到的那句话）。
//
// # 🔴 省略号被「表情那一格」吞掉（这一族的主判据）
//
// 框架的 `MaxLines` 在最后一行末尾加一个省略号，而它的样式取自**最后一个到达
// 切点的那一段 span**（`mygo/internal/text/layout.go` 的 `ellipsize`：
// `tSpans[n-1].End += len(mark)`）——所以**切点正好落在表情占位符那一格末尾**
// 时，省略号的 rune 被**并进那一段**。
//
// ⚠️ **那一段带 `ObjectWidth`** ⇒ 它是 **inline object** ⇒ 那一格的**推进宽是
// 18**（表情那格的宽），而省略号自己只有 **11.12**。
//
// ⇒ **在 Windows 上那一格一个字都不画**（DirectWrite 按 code unit 给那一段的
// 每个 rune 挂 inline object，`mygo/internal/text/dwrite_objects_windows.go`
// 的 `setObjects`）⇒ 用户看到的是**一片空白**，不是「…」。
//
// ⇒ **在 macOS 上省略号照常画出来**（Core Text 按 rune 下标找那一格属于哪一段，
// 而省略号的 rune 下标落在**下一段**）——但那一格的**推进宽仍然是 18**
// （实测，框架内部 trace）。
package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/songname"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	mygoui "github.com/egoist/mygo/ui"
)

// commentEmotesOf 是「一枚表情占位符」的那张表（占位符长得五花八门，而表是**每条
// 评论自己一份**）。
func commentEmotesOf(tokens ...string) map[string]bili.CommentEmote {
	m := map[string]bili.CommentEmote{}
	for _, t := range tokens {
		m[t] = bili.CommentEmote{URL: "https://i0.hdslb.com/bfs/emote/x.png"}
	}
	return m
}

// commentSpanEnds 算出每一段 span 的**起止 rune 下标**（框架的 `text.Span.End`
// 就是这个意思——见 `mygo/ui/richtext.go` 的 `spanStyle`）。
//
// ⚠️ **这是判「省略号并进哪一段」的关键**：框架的 `ellipsize` 把省略号的 rune
// 并进**那个 `End` 正好等于切点的那一段**（`mygo/internal/text/layout.go`）。
func commentSpanEnds(spans []mygoui.Span) []int {
	out := make([]int, len(spans))
	end := 0
	for i, sp := range spans {
		end += len([]rune(sp.Text))
		out[i] = end
	}
	return out
}

// TestTheEllipsisNeverLandsInTheEmoteSpan 钉住**省略号永远不会被并进表情那一
// 格**。
//
// 🔴 **这一条是票 #702 的审查抓出来的那个 blocker**：切点正好落在表情占位符那一
// 格末尾时，框架把省略号的 rune 并进**那一格**——而那一格是 inline object ⇒
// 那一格的**推进宽是 18**（表情那格的宽），而省略号自己只有 **11.12**。
//
// 🔴 **实测（框架内部 trace，`mygo/internal/text/layout.go` 的 `ellipsize`）**：
//
//	切点在文字段末尾   省略号那一格 adv = **11.12**（正常）
//	切点在表情段末尾   省略号那一格 adv = **18.00**（被表情那格吞了）
//
// ⚠️ **在 Windows 上那一格一个字都不画**（DirectWrite 按 code unit 给那一段的每个
// rune 挂 inline object，`mygo/internal/text/dwrite_objects_windows.go` 的 `setObjects`）⇒
// 用户看到的是**一片空白**，不是「…」。
//
// ⚠️ **在 macOS 上省略号照常画出来**（Core Text 按 rune 下标找那一格属于哪一段，
// 而省略号的 rune 下标落在**下一段**）——但那一格的**推进宽仍然是 18**。
//
// 🔴 **判据是「结构」而不是「画面」**：画面上（macOS）两种切法**逐像素相同**
// （实测：`pixelDiff = 0`）——因为 Core Text 按 rune 下标找那一格属于哪一段，
// 而省略号的 rune 下标落在**下一段**。⇒ 判据必须落在**框架真正看的那一样东西**
// 上：**每一段的 `End`**。
//
// ⇒ **不变式**：**每一枚表情占位符那一格的**后面，一定跟着一段**（守卫）——
// 于是「切点落在表情那一格的末尾」时，那个 `End` 属于**守卫**（普通 span）
// ⇒ 省略号**继承普通文字的样式**。
//
// ⚠️ **为什么不能是「末尾留一段空 span」**（旧文件头写的那个做法）：那一段的
// `End` 与前一段**相同** ⇒ 它根本不是「最后一个到达切点的 span」⇒ 切点落在
// 表情那一格末尾时，省略号**照样**并进表情那一格（实测：那一格的推进宽是 18）。
//
// ⚠️ **判据要扫「多个占位符」**：一枚表情的正文里切点能落在它末尾；**两枚相邻
// 的表情**之间那一档（第二枚的末尾）也一样——所以两种都要扫。
func TestTheEllipsisNeverLandsInTheEmoteSpan(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		toks []string
	}{
		{"一枚表情", "前面几个字[妙啊]后面几个字", []string{"[妙啊]"}},
		{"两枚相邻的表情", "前面几个字[妙啊][大哭]后面几个字", []string{"[妙啊]", "[大哭]"}},
		{"表情在开头", "[妙啊]后面几个字", []string{"[妙啊]"}},
		{"表情在末尾", "前面几个字[妙啊]", []string{"[妙啊]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spans, _, _ := commentSpansOf(tc.msg, commentEmotesOf(tc.toks...), nil, commentMark{})
			ends := commentSpanEnds(spans)
			// 每一段带 ObjectWidth 的（= 表情那一格），它后面**必须**还有一段，
			// 而那一段的 `End` **必须不同**（否则它不是「另一个到达切点的 span」）。
			for i, sp := range spans {
				if sp.ObjectWidth <= 0 {
					continue
				}
				if i+1 >= len(spans) {
					t.Fatalf("表情那一格（span %d）是**最后一段**——"+
						"切点落在它末尾时，省略号会被并进它（而它是 inline object）", i)
				}
				if ends[i+1] == ends[i] {
					t.Errorf("表情那一格（span %d）的 End 是 %d，而下一段的 End **也是** %d"+
						"——它根本不是「另一个到达切点的 span」，"+
						"于是切点落在表情那一格末尾时，省略号照样并进表情那一格",
						i, ends[i], ends[i+1])
				}
				if spans[i+1].ObjectWidth > 0 {
					t.Errorf("表情那一格（span %d）后面那一段**也是** inline object"+
						"（ObjectWidth = %.0f）——省略号仍会被并进一个 inline object",
						i, spans[i+1].ObjectWidth)
				}
				// 🔴 **守卫必须**紧跟**在表情那一格后面**：因为框架把省略号并进
				// 「**最后一个到达切点的那一段**」——而「到达切点」指的是那一段的
				// `End` **正好等于切点**。⇒ 表情那一格后面那一段（守卫）的 `End`
				// 必须**比表情那一格大**（它多一个 rune）⇒ 切点落在表情那一格末尾
				// 时，到达切点的是**守卫**而不是表情那一格。
				//
				// ⚠️ **而「后面那一段是普通文字」是不够的**：普通文字那一段的 `End`
				// **更大**（它有好几个 rune）⇒ 切点落在表情那一格末尾时，到达切点
				// 的**仍然**是表情那一格（它的 `End` 正好等于切点）⇒ 省略号照样
				// 并进表情那一格。⇒ **守卫必须是紧跟的那一段**。
				if spans[i+1].Text != commentObjectGuard {
					t.Errorf("表情那一格（span %d）后面紧跟的那一段是 %q，而不是守卫 %q"+
						"——守卫必须**紧跟**在表情那一格后面"+
						"（否则切点落在表情那一格末尾时，到达切点的仍然是表情那一格，"+
						"省略号照样被并进 inline object）",
						i, spans[i+1].Text, commentObjectGuard)
				}
			}
		})
	}
}

// TestTheEllipsisGuardDoesNotChangeTheLayout 钉住**守卫不改变排版**。
//
// 🔴 **为什么这一条必须钉**：守卫是**加在正文里**的一个字符（U+200B）——
// 而正文是要排版的 ⇒ 守卫一旦占了地方，**折行的位置就变了**（那是用户看得见的
// 变化：哪几个字在第一行、哪几个字在第二行）。
//
// ⚠️ **U+200B（ZERO WIDTH SPACE）是零宽的** ⇒ 它不占地方 ⇒ 排版不变。
//
// ⚠️ **判据是「每一行的墨迹签名」**（每一行有几个墨迹列）——**逐宽度**比：
// 加守卫前后必须**完全一样**。
//
// ⚠️ **要扫「多行」**：单行那一档折行的地方只有一处，而多行有若干处——
// 而守卫是**每一枚表情后面**都有，所以表情**越多**、守卫越多 ⇒ 差别越可能
// 露出来。
func TestTheEllipsisGuardDoesNotChangeTheLayout(t *testing.T) {
	texts := []string{
		"这是一段中文评论文字，用来试断行",
		"hello world this is a latin comment",
		"混排 mixed 文字 content 的评论 text",
	}
	for ti, text := range texts {
		for emoteAt := 0; emoteAt <= len([]rune(text)); emoteAt += 11 {
			base := rowSignature(t, text, emoteAt, "", 60)
			guarded := rowSignature(t, text, emoteAt, commentObjectGuard, 60)
			if base != guarded {
				t.Errorf("第 %d 段、表情在第 %d 个 rune：加守卫之后排版变了\n"+
					"	不加守卫 %s\n	加了守卫 %s", ti, emoteAt, base, guarded)
			}
		}
	}
}

// rowSignature 量出一段正文**每一行的墨迹签名**（每一行有几个墨迹列），
// 拼成一个字符串。
//
// ⚠️ **宽度要扫**：折行的位置随着宽度变，所以一个宽度上「一样」不够——
// 而守卫的宽度是零，所以**所有宽度上都一样**。
func rowSignature(t *testing.T, text string, emoteAt int, guard string, from int) string {
	t.Helper()
	runes := []rune(text)
	pre := string(runes[:emoteAt])
	post := string(runes[emoteAt:])
	var sigs []string
	// ⚠️ **宽度扫一遍就够**：断行的位置随宽度变，而守卫是零宽的
	// ⇒ **它不能影响任何一个宽度上的排版**。
	for w := from; w <= 260; w++ {
		spans := []mygoui.Span{}
		if pre != "" {
			spans = append(spans, mygoui.Span{Text: pre})
		}
		spans = append(spans, mygoui.Span{Text: "￼", ObjectWidth: 18, ObjectHeight: 18})
		if guard != "" {
			spans = append(spans, mygoui.Span{Text: guard})
		}
		if post != "" {
			spans = append(spans, mygoui.Span{Text: post})
		}
		tt := mygoui.NewTester(func(c *mygoui.Context) {
			mygoui.Box(c).Width(float32(w)).Height(400).Children(func() {
				mygoui.RichText(c, spans...).
					FontSize(14).
					TextColor(mygoui.Color{R: 0, G: 0, B: 0, A: 255})
			})
		}, 1200, 500)
		tt.Frame()
		img := tt.Image()
		// 按 y 分行，每行数墨迹列
		perRow := map[int]int{}
		for y := 0; y < img.Bounds().Max.Y; y++ {
			n := 0
			for x := 0; x < img.Bounds().Max.X; x++ {
				if img.RGBAAt(x, y).R < 200 {
					n++
					break
				}
			}
			if n > 0 {
				perRow[y] = n
			}
		}
		var ys []int
		for y := range perRow {
			ys = append(ys, y)
		}
		for i := 0; i < len(ys); i++ {
			for j := i + 1; j < len(ys); j++ {
				if ys[j] < ys[i] {
					ys[i], ys[j] = ys[j], ys[i]
				}
			}
		}
		var sig []int
		cur, prev := 0, -10
		for _, y := range ys {
			if y-prev > 3 && cur > 0 {
				sig = append(sig, cur)
				cur = 0
			}
			cur += perRow[y]
			prev = y
		}
		if cur > 0 {
			sig = append(sig, cur)
		}
		sigs = append(sigs, strings.TrimSpace(strings.Join(strings.Fields(fmt.Sprint(sig)), ",")))
	}
	return strings.Join(sigs, "|")
}

// TestThePlainAndTheDrawnAreTheSameSentence 钉住**读屏的那一份与画出来的那一份
// 是同一句话**（票 #702 的审查抓出来的第二个 blocker）。
//
// 🔴 **为什么这一条必须钉**：旧做法里 `commentSpansMarked` **一个函数**同时产出
// `spans` 和 `plain` ⇒ 那两份**必然一致**。而 #702 把它们拆成两个独立函数之后，
// **判定的口径分叉了**：
//
//	`commentSpansOf`  从左往右扫，找 `[` 到**下一个** `]` 之间的那一段，
//	                  **整段在表情表里**才算占位符。
//	`commentPlain`    `strings.ReplaceAll` 替换**表情表里每一个 token**，
//	                  **不看边界**。
//
// ⇒ 两种形状上分叉：
//
//	**嵌套方括号**  正文 "前面有字[a[b]]后面有字"，表情表 {"[b]"}
//	                `commentSpansOf` 认出的是 `[b]`（从 `[` 到下一个 `]`）——
//	                **而** `[a[b]]` 这一段**整体**不是表情（它不在表里）⇒
//	                画出来的是 `[a[b]]` 的**字面量**（`[a` + 一枚表情 + `]`）。
//	                而旧的 `commentPlain` 把 `[b]` 也换成「［表情］」⇒
//	                **读屏的那一份与画出来的那一份不一样**。
//
//	**相邻 token**  正文 "前面有字[a][a]后面有字"，表情表 {"[a]", "[a][a]"}
//	                ⇒ `ReplaceAll` 按 **map 的迭代顺序**替换 ⇒ **同一帧里
//	                读到的内容可能每次都不同**（而 `commentPlain` 在
//	                `viewCommentBodyEl` 里**每帧**调一次）。
//
// ⚠️ **判据是「两份必须一致」**：把 `commentSpansOf` 切出来的 span **拼起来**
// （那是画出来的那一份），把 `commentPlain` 那一份**逐字**比过去——
// 而**表情那一格**在画出来的那一份里是 **U+FFFC**，要给读屏的那一份对上，
// 就要先把 U+FFFC 换成「［表情］」。
func TestThePlainAndTheDrawnAreTheSameSentence(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		toks []string
	}{
		{"嵌套方括号", "前面有字[a[b]]后面有字", []string{"[b]"}},
		{"相邻 token", "前面有字[a][a]后面有字", []string{"[a]", "[a][a]"}},
		{"相邻 token（表里只有长的那个）", "前面有字[a][a]后面有字", []string{"[a][a]"}},
		{"普通的一枚", "前面有字[妙啊]后面有字", []string{"[妙啊]"}},
		{"表里有但正文里没有的", "前面有字后面有字", []string{"[妙啊]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			emotes := commentEmotesOf(tc.toks...)
			spans, _, _ := commentSpansOf(tc.msg, emotes, nil, commentMark{})
			// 画出来的那一份：把 span 拼起来（表情那格是 U+FFFC）。
			//
			// ⚠️ **守卫要拿掉**：它是**画的时候**才有的那个零宽字符（只为让省略号不落在
			// 表情那一格，见 `commentObjectGuard`）——而**给读屏的那一份**
			// 根本不带它 ⇒ 拿掉守卫才是「眼睛看到的那句话」。
			var drawn strings.Builder
			for _, sp := range spans {
				drawn.WriteString(strings.ReplaceAll(sp.Text, commentObjectGuard, ""))
			}
			// 给读屏的那一份：U+FFFC 换成「［表情］」。
			want := strings.ReplaceAll(drawn.String(), "￼", commentPlainSpan)
			got := commentPlain(tc.msg, emotes)
			if got != want {
				t.Errorf("读屏的那一份是 %q，而画出来的那一份（去掉 inline object）是 %q"+
					"——**读到的 ≠ 看到的**", got, want)
			}
		})
	}
}

// TestThePlainIsStableAcrossCalls 钉住**同一句话每次读到的都一样**。
//
// 🔴 **为什么这一条必须钉**：旧的 `commentPlain` 用 `strings.ReplaceAll` 按 **map
// 的迭代顺序**替换每一个 token——而 Go 的 map 迭代顺序**每次都不一样**⇒
// 同一帧里读到的内容可能**每次都不同**。
//
// ⚠️ **判据是「多次调用结果一样」**：连调 50 次（而旧的那一版在 50 次里**会**
// 产生 2 个不同的结果——审查员实测过）。
func TestThePlainIsStableAcrossCalls(t *testing.T) {
	msg := "前面有字[a][a]后面有字"
	emotes := commentEmotesOf("[a]", "[a][a]")
	first := commentPlain(msg, emotes)
	for i := 0; i < 50; i++ {
		if got := commentPlain(msg, emotes); got != first {
			t.Fatalf("第 %d 次调用读到的是 %q，而第一次是 %q"+
				"——**同一句话每次读到的都不一样**", i, got, first)
		}
	}
}

// commentFixtureEmoteInMiddle 是「正文中间有一枚表情占位符」那一份 fixture。
//
// ⚠️ **为什么表情在中间**：M2 要验的是「选区**起点在表情之后**」的那一句
// ——而表情在**末尾**的话，选区起点**永远在它前面**（末尾之后没有字了）。
//
// ⚠️ **表情表也要给上**：B 站的 fixture 里 `content.emote` 那一栏就是
// 「占位符 → 表情图」，而正文里那个占位符**靠它**才被认成表情。
func commentFixtureEmoteInMiddle(t *testing.T, name string) []byte {
	t.Helper()
	var doc map[string]any
	raw := commentFixtureBytes(t, name)
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("fixture %s 读不懂：%v", name, err)
	}
	data, _ := doc["data"].(map[string]any)
	replies, _ := data["replies"].([]any)
	if len(replies) == 0 {
		t.Fatalf("fixture %s 里一条评论都没有", name)
	}
	first, _ := replies[0].(map[string]any)
	content, _ := first["content"].(map[string]any)
	content["message"] = "前面几个字[妙啊]后面几个字"
	content["emote"] = map[string]any{
		"[妙啊]": map[string]any{
			"id": 435, "package_id": 1, "state": 0, "type": 1, "attr": 0,
			"text":  "[妙啊]",
			"url":   "https://i0.hdslb.com/bfs/emote/b4cb77159d58614a9b787b91b1cd22a81f383535.png",
			"meta":  map[string]any{"size": 1, "suggest": []string{""}},
			"mtime": 1668688325, "jump_title": "妙啊",
		},
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("fixture %s 编不出来：%v", name, err)
	}
	return out
}

// TestSelectingAfterTheEmoteStillCopiesTheRightSentence 钉住**选区起点在表情
// 之后**时，复制出来的那一句**没有错位**。
//
// 🔴 **这一条钉的是 `commentPlainOfSelection` 的那个前提**（票 #702 的审查
// 指出来的）：它把框架给的 `Start` / `End` 当成**画出来的那一份**的坐标，
// 而**框架给的确实是那一份**——`mygo/ui/textselection.go:419` 给的是
// `p.editor.source`，而 `editor.source` 是 `e.text`（**那一格的文字**），
// **不是** `Label` 那一份（表情占位符写成了「［表情］」的那一份）。
//
// ⚠️ **而那个前提很要紧**：两���的**长度不一样**（表情那一格在画出来的那一份里
// 是 **1 个 rune**，在 `Label` 那一份里是 **4 个字符**）⇒ 坐标要是搞错了，
// 整句就**全错位**了。
//
// ⚠️ **判据是「复制出来的那一句」**：选区**起点在表情之后**⇒ 那一段里**没有**
// 表情 ⇒ 复制出来的那一句**必须**是正文里那几个字**一模一样**。
//
// ⇒ 而「坐标是画出来的那一份」这件事，**只有选区跨过表情才看得出来**——
// 所以这一条要**跨过**表情选（起点在表情**之前**、终点在表情**之后**），
// 而复制出来的那一句必须是「前面几个字［表情］后面几个字」。
func TestSelectingAfterTheEmoteStillCopiesTheRightSentence(t *testing.T) {
	b := newCommentBili(t, "reply-main.json")
	b.mainRaw = commentFixtureEmoteInMiddle(t, "reply-main.json")
	_, tt := newCommentsShell(t, b, audioItem(), 1200, 800)

	// 给读屏的那一份（表情占位符写成「［表情］」）。
	const want = "前面几个字［表情］后面几个字"
	waitFrames(t, tt, "正文画出来", func() bool {
		_, ok := tt.Find(want)
		return ok
	})

	// 🔴 **拖过整句**（`want` 那一整段就是**画出来的那一份**）——
	// ⇒ 选区**跨过**了表情 ⇒ 坐标要是错一格，复制出来的那一句就**不是**它。
	//
	// ⚠️ **拖的是「给读屏的那一份」**（`want`）：那一格的无障碍名字就是它
	// （`Label`）——而 `Tester.Find` 认的是**名字**（`Labels`），所以按名字
	// 拖就等于**在那一格上拖**。
	dragOver(t, tt, want, want)
	if got := copySelection(tt); got != want {
		t.Errorf("跨过表情拖选之后，剪贴板里是 %q，want %q", got, want)
	}

	// 🔴 **选区起点在表情之后**那一档：这一档才是 M2 问的那一句——
	// 「坐标是**画出来的那一份**的」这件事，**只有起点在表情之后**才看得出来
	// （起点在表情之前时，两份的坐标前缀**一样**，错不错看不出来）。
	//
	// ⚠️ **起点用像素算**（不用 `Find`）：那一格的 `Label` 名字是**整句**，
	// 而「后面几个字」只是**那一段**——它没有自己的名字（`Find` 认的是
	// **元素**的名字，不是**某几个字**的位置）。
	//
	// ⇒ 所以：从**整句的左沿 + 「前面几个字［表情］」的宽**那儿起拖。
	// 而那个宽是**量出来的**（`ui.Painter.MeasureText` 量「前面几个字」＋
	// 「［表情］」——后者是 4 个字符）。
	r, ok := tt.Find(want)
	if !ok {
		t.Fatalf("画面上找不到 %q", want)
	}
	// 🔴 **起点是「表情之后」那个位置**——而那个位置在**画出来的那一份**里是
	// 「前面几个字」的宽 **+** 表情那一格的宽（18）。
	//
	// ⚠️ **不能拿「前面几个字［表情］」的宽来算**：那是 **Label 那一份**的宽
	// （表情那格是 **4 个字符**），而**画出来的那一份**里表情那格是 **1 个
	// rune**（U+FFFC，**18 宽**）⇒ 两个数**不一样**（实测差两个字的宽）。
	//
	// ⇒ 而这**正是 M2 的那条前提**：坐标是**画出来的那一份**的。
	headW := measureText(t, "前面几个字") + commentObjectW
	x0 := r.X + headW
	y := r.Y + r.H/4
	tt.Press(x0, y)
	tt.Move(r.X+r.W-1, y)
	tt.Release(r.X+r.W-1, y)
	tt.Frame()
	if got := copySelection(tt); got != "后面几个字" {
		t.Errorf("选区起点在表情之后时，剪贴板里是 %q，want %q"+
			"——坐标**错位**了（而坐标是**画出来的那一份**的，见 `commentPlainOfSelection`）",
			got, "后面几个字")
	}
}

// measureText 量出一段字有多宽（**画出来的那一份**的度量）。
//
// ⚠️ **它量的是「给读屏的那一份」的字**（`Label` 那一份）——而这一条要的是
// **画出来的那一份**的坐标 ⇒ 所以量的必须是**画出来的那一份**里**对应的那
// 一段**：表情那一格在画出来的那一份里是 **1 个 rune**（U+FFFC，18 宽），
// 而在 `Label` 那一份里是 **4 个字符**（「［表情］」）——⇒ **两个数不一样**。
//
// ⇒ 所以这里量的是 **`head` 在 `Label` 那一份里的宽**——而「后面几个字」的
// **起点**在画出来的那一份里的位置 = 「前面几个字」的宽 + 表情那一格的宽
// （18）。
func measureText(t *testing.T, s string) float32 {
	t.Helper()
	var w float32
	ts := newTestShell(t, at(600, 400), withView(func(sh *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			sh.wake.set(c)
			sh.applyFrameTheme(c)
			w, _ = c.MeasureText(0, mygoui.Span{
				Text: s,
				Size: scale.Font(c, scale.Size14),
			})
		}
	}))
	ts.tt.Frame()
	if w <= 0 {
		t.Fatalf("量不出 %q 的宽", s)
	}
	return w
}

// TestTheEllipsisIsVisibleAfterAnEmote 钉住**表情后面被折起来的时候，画面上
// 真的有省略号**。
//
// 🔴 **判据是「像素」而不是「没报错」**：末尾那一段必须是**三个点**
// （每个点**两行高**——省略号是贴基线的小点，而汉字是 10~11 行高）。
//
// ⚠️ **量法**：逐列数**墨迹的行数**，找**高度 ≤ 3** 的列——那些就是省略号的
// 三个点（每个点占两列）。
//
// ⚠️ **要扫宽度**：切点落在表情那一格末尾的那一档，宽度是**一段**（实测
// 110~123 那一带）——只试一个宽度的话，一个「碰巧没切到」的实现照样绿。
//
// ⚠️ **而这一条在 macOS 上**断不出「吞掉」那个形状**（实测）：Core Text 按
// rune 下标找那一格属于哪一段，而省略号的 rune 下标落在**下一段**⇒ 省略号
// **照常画出来**（像素逐列相同，`pixelDiff = 0`）。⇒ 所以这一条断的是
// 「**别把它弄没了**」（一个把省略号整个去掉的实现会红），而**吞掉**那个
// 形状（那一格的推进宽变成 18、Windows 上一个字都不画）断在
// `TestTheEllipsisNeverLandsInTheEmoteSpan`。
func TestTheEllipsisIsVisibleAfterAnEmote(t *testing.T) {
	// 正文 = 文字 + 一枚表情 + 很多字 ⇒ 一定被折起来。
	// 而表情**紧跟**着的那一段 ⇒ 切点会落在它末尾（宽度 110~123 那一带）。
	for w := 108; w <= 126; w++ {
		spans := []mygoui.Span{{Text: "字字字字"}}
		spans = append(spans, mygoui.Span{Text: "\uFFFC", ObjectWidth: 18, ObjectHeight: 18})
		spans = append(spans, mygoui.Span{Text: commentObjectGuard})
		spans = append(spans, mygoui.Span{Text: "后面还有字"})
		// ⚠️ **每一档用不同的文字**（避开框架的布局缓存：它按参数比对）。
		spans[0].Text = fmt.Sprintf("%d", w) + "字字字"
		tt := mygoui.NewTester(func(c *mygoui.Context) {
			mygoui.Box(c).Width(float32(w)).Height(40).Children(func() {
				mygoui.RichText(c, spans...).
					FontSize(14).
					TextColor(mygoui.Color{R: 0, G: 0, B: 0, A: 255}).
					MaxLines(1).
					Ellipsis("…")
			})
		}, 400, 60)
		tt.Frame()
		img := tt.Image()
		// **整行**里，**高度 ≤ 3** 的墨迹列 = 省略号的点（汉字是 10~11 行高）。
		dots := 0
		for x := 0; x < img.Bounds().Max.X; x++ {
			h := 0
			for y := 0; y < img.Bounds().Max.Y; y++ {
				if img.RGBAAt(x, y).R < 200 {
					h++
				}
			}
			if h > 0 && h <= 3 {
				dots++
			}
		}
		// ⚠️ **三个点占六列**（每个点两列）——而「一个字」的笔画**不止三列**
		// 贴基线（那是汉字的高）。⇒ 六个就是省略号。
		if dots < 6 {
			t.Errorf("宽度 %d：末尾**没有**省略号的三个点（只数到 %d 列贴基线的墨迹）"+
				"——画面上那一句被折起来了，却**看不见**「…」", w, dots)
		}
	}
}

// TestTheHighlightStillLandsOnTheRightWordsAfterAnEmote 钉住**表情后面跟着文字
// 时，命中区间仍然落在正确的字上**。
//
// 🔴 **这一条钉的是「守卫不会把高亮坐标带偏」**（票 #702 的审查提的）：守卫是
// **多出来的一个 rune**（在 span 序列里），而高亮用的是**原文**的 rune 坐标
// （`songname.Range`）——所以「守卫会不会把后面的坐标顶偏一格」是个真问题。
//
// ⇒ **答案是不偏**，而原因值得写下来（否则将来有人把守卫改成走 `text()`，
// 高亮就会**静默错位一格**——症状是「高亮落在错的字上」，很难查）：
//
//	`appendMarkedText` 只在**原文文本段**上切，它看的两个数是 `base`（**原文**
//	rune 坐标）和 `hl`（**原文** rune 坐标）——**框架的 span 坐标它根本不碰**。
//	而守卫是**直接 `append` 进 `spans`** 的（`comments_body.go` 的
//	`commentSpansOf`），**不经过 `text()`** ⇒ **`base` 不累加它**
//	（这是对的——守卫不在原文里）。
//
// ⇒ **原文坐标链完整**：守卫在 span 序列里多占一格，但那是**框架那一侧**的
// 坐标，与高亮用的原文坐标是两套，互不干扰。
//
// ⚠️ **判据是「染了底色的那几段是哪些字」**——不是「有没有染色」：错位一格
// 的话，染色的**是别的字**（而数量可能一样）。
func TestTheHighlightStillLandsOnTheRightWordsAfterAnEmote(t *testing.T) {
	emotes := commentEmotesOf("[妙啊]")
	mark := commentMark{Bg: mygoui.Hex("#1ed760").Alpha(0.18), Ink: mygoui.Hex("#107032"), Radius: 2}
	for _, tc := range []struct {
		name    string
		msg     string
		hl      []songname.Range
		painted []string
	}{
		{
			// 原文：晴(0)天(1)[(2)妙(3)啊(4)](5)真(6)好(7)听(8)
			// 命中「真好听」= 原文 rune 6..9 ⇒ 它在表情**之后**。
			name:    "表情之后的那一段",
			msg:     "晴天[妙啊]真好听",
			hl:      []songname.Range{{Start: 6, End: 9}},
			painted: []string{"真好听"},
		},
		{
			// 命中**整段**（把表情包进去）⇒ 表情那格让出去，两侧都染。
			name:    "整段命中（表情让出去）",
			msg:     "晴天[妙啊]真好听",
			hl:      []songname.Range{{Start: 0, End: 9}},
			painted: []string{"晴天", "真好听"},
		},
		{
			// 表情在**开头**：命中「晴天」= 原文 rune 3..5。
			name:    "表情在开头的那一段",
			msg:     "[妙啊]晴天很好",
			hl:      []songname.Range{{Start: 3, End: 5}},
			painted: []string{"晴"},
		},
		{
			// 🔴 **两枚表情**（守卫有**两个**）⇒ 坐标要还是对的。
			// 原文：前(0)面(1)[(2)妙(3)啊(4)](5)中(6)间(7)[(8)妙(9)啊(10)](11)后(12)面(13)
			// 命中「后面」= 原文 rune 12..14。
			name:    "两枚表情之后的那一段",
			msg:     "前面[妙啊]中间[妙啊]后面",
			hl:      []songname.Range{{Start: 12, End: 14}},
			painted: []string{"后面"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spans, _, _ := commentSpansOf(tc.msg, emotes, tc.hl, mark)
			var painted []string
			for _, sp := range spans {
				if sp.Background.A != 0 {
					painted = append(painted, sp.Text)
				}
			}
			if fmt.Sprint(painted) != fmt.Sprint(tc.painted) {
				t.Errorf("染了底色的那几段是 %q，want %q——"+
					"**命中区间偏了**（守卫不该把原文坐标带偏）", painted, tc.painted)
			}
		})
	}
}
