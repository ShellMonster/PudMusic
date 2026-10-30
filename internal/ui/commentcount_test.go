// 播放条上那枚「评论」键旁边的数字（票 #682）。
//
// # 这一组断的是「画面上有什么」
//
// 数字是一枚**读数**：它画没画、画在哪儿、会不会把邻居挤坏——三样都不会报错，
// 只会在画面上悄悄不对。所以断的是渲染结果（`Tester` 量出来的框和文字），
// 不是「某个函数被调到了」。
//
// # 一个真请求都不发
//
// 数据走本地 HTTP fixture：`commentBili`（`comments_test.go`）那台假 B 站答
// `/x/web-interface/view`——这一张回包里带 `stat.reply`，而播放条那个数就是
// 从那儿来的（`bili.ArchiveStats`）。
package ui

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 数字本身
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCommentKeyShowsTheCount 钉住**播放条那一枚键上画出了评论数**。
//
// 🔴 判据是「画面上有 `PlayCountText(1286)` 那一串字」（「1286」）：它就是
// 播放量那一列同一个折法（「1.2万」那种），所以这里不自己拼数字。
//
// ⚠️ **「评论」这两个字也要在**（无障碍名字 + 画面上那枚键的身份）：只断数字
// 的话，一个把图标换成纯数字的实现照样绿。
func TestTheCommentKeyShowsTheCount(t *testing.T) {
	tt, _ := commentCountPlaybarTester(t, mvItem(), 1200, 1286)

	if !tt.HasText("评论") {
		t.Errorf("播放条上找不到「评论」键，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find(list.PlayCountText(1286)); !ok {
		t.Fatalf("「评论」键上没有评论数 %q（文字有 %q）——那一枚键只画了图标",
			list.PlayCountText(1286), tt.Texts())
	}
}

// TestTheCommentKeySaysTheNumberToScreenReaders 钉住**数字不只靠画面传达**。
//
// 🔴 图标键的无障碍名字就是它唯一的凭据（`mygo/ui/svg.go`：图标对辅助技术
// 不可见）。评论数画在图标旁边、可是名字里没有的话，读屏用户听到的仍然是
// 「评论」两个字——数字对他们不存在。
//
// ⚠️ **判据是名字里带量级词**（「万」那一档）：1286 这个名字里带的是「1286」
// 还是「1.2万」都可以（`PlayCountText` 的折法），但**必须带上那个数**。
func TestTheCommentKeySaysTheNumberToScreenReaders(t *testing.T) {
	tt, _ := commentCountPlaybarTester(t, mvItem(), 1200, 12345)

	want := "评论 " + list.PlayCountText(12345)
	key, ok := tt.Find(want)
	if !ok {
		t.Fatalf("无障碍名字不是 %q（文字有 %q）——读屏用户听不到评论数",
			want, tt.Texts())
	}
	// 悬停提示里也要有那个数：数字只画在画面上，指针停在别处时用户读不到它。
	tt.Move(key.X+key.W/2, key.Y+key.H/2)
	tt.Frame()
	if !tt.HasText("条）") {
		t.Errorf("悬停提示里没有「N 条」那半句（文字有 %q）——数字只画在画面上不够",
			tt.Texts())
	}
}

// TestTheCommentCountNeverShowsZero 钉住**拿不到就不画**。
//
// 🔴 「没有数字」和「零条评论」在屏幕上长得一样，可它们说的是两件事：前者是
// 「还不知道」（离线 / 那一趟还没回来），后者是「这一首真没人评论」。画一个
// 「0」会让用户以为后者。
//
// ⚠️ **键照旧要能点**：拿不到数字不是这一枚键的错——它打开的那一页照旧有
// 东西可看（而那一页自己会说「还没有人评论」）。
//
// ⚠️ **判据分两层**：画面上不许出现「0」，而且那一枚键的**名字**必须正好是
// 「评论」（名字里没有数字）。第二层是**不依赖时序**的那一半：`stat.reply`
// 是 0 的时候 `PlayCountText(0)` 是空串，于是数字那一格根本不会被建出来
// ——这一条与「那一趟回来没有」无关。
func TestTheCommentCountNeverShowsZero(t *testing.T) {
	// `stat.reply` 是 0：B 站没给这个数（或者真的一条都没有）——两种都不画。
	b := newCommentBili(t, "reply-main.json")
	b.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"aid": 170001, "bvid": r.URL.Query().Get("bvid"),
			"stat": map[string]any{"reply": 0},
		})
	})
	tt := commentCountPlaybarWith(t, b, mvItem(), 1200)

	// ⚠️ **等那一趟真的发出去**（不是等「拿到了」——`CommentCount() == 0` 在
	// 那一趟发出去**之前**也成立）。发出去之后是两个状态之一：还在路上 / 已经
	// 拿到 0 条——**两个状态都不许画数字**，所以这里不必等第二个。
	waitFrames(t, tt, "稿件详情那一趟发出去了", func() bool {
		return b.countHit("/x/web-interface/view") > 0
	})
	tt.Frame()

	// 名字必须正好是「评论」：`Find` 是全等匹配，它找得到就说明名字里没有数字。
	if _, ok := tt.Find("评论"); !ok {
		t.Fatalf("拿不到数字的时候那一枚键的名字不是「评论」，文字有 %q", tt.Texts())
	}
	for _, txt := range tt.Texts() {
		if txt == "0" || txt == "0 条" {
			t.Errorf("画面上出现了 %q——拿不到数字不该画成「零条评论」", txt)
		}
	}
}

// TestTheZeroCountIsSpelledAsNoNumberAtAll 是上一条的**不依赖时序**那一半：
// 0 那个数在字面上就是「没有」，不是一个「0」。
//
// 🔴 上面那条 UI 用例只能证明「某一帧上没有 0」；而「0 会不会被画出来」在实现上
// 是 `list.PlayCountText(0)` 与 `commentsKeyLabel` 两处决定的事——所以这里直接
// 钉那两处。**两层一起**才挡得住「等到数落下来之后画个 0」那一档（那一档在
// UI 用例上有 50% 的运气）。
func TestTheZeroCountIsSpelledAsNoNumberAtAll(t *testing.T) {
	if got := list.PlayCountText(0); got != "" {
		t.Errorf("PlayCountText(0) = %q，want 空串——0 是「没有这个数」，不是「零条」", got)
	}
	if got := commentsKeyLabel(0); got != "评论" {
		t.Errorf("commentsKeyLabel(0) = %q，want 「评论」——名字里不该多一个 0", got)
	}
	if got := commentsKeyTip(0); got != "看这一首的评论" {
		t.Errorf("commentsKeyTip(0) = %q，want 不带数字的那一句", got)
	}
	// 非 0 那一档要真的带上数字（不然上面两条可以靠「永远不带」蒙混过关）。
	if got := commentsKeyLabel(1286); got != "评论 1286" {
		t.Errorf("commentsKeyLabel(1286) = %q，want 「评论 1286」", got)
	}
	if got := commentsKeyTip(1286); !strings.Contains(got, "1286") {
		t.Errorf("commentsKeyTip(1286) = %q，里面没有那个数", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 宽度账
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCountDoesNotSqueezeTheTitleOrTheSlider 是这一票的宽度回归。
//
// # 改之前 / 改之后的账（实测，深色主题，同一首歌）
//
//	窗口   评论键宽（无数字 → 有数字）  歌名宽（改之前 → 有数字）  让位   滑块左沿
//	 900     32 → 67                       76 → 76                 0    330.7
//	1000     32 → 67                       76 → 76                 0    358.2
//	1200     32 → 67                       76 → 76                 0    413.2
//	1600     32 → 67                      117.6 → 82.6            35    523.2
//
//	（「有数字」取的是数字吃到上限 33 的那一档，即 ≥ 100 万条——那是**最宽**
//	 的一档；「1286」那一档是 57.9，歌名一格都不让。）
//
// 🔴 **让位只在 1600 那一档发生，且让的正好是评论键长出来的那 35**（缝 2 +
// 数字 33）：窄窗口（900~1280）上左边那一列由**保底宽**说话（`playbarLeftMinW`
// 272 → 307），所以加宽落在**左列自己**身上（撑出轨道的那一截从 98.4 变成
// 133.4），歌名那一段仍然是它的保底 76（`trackTextMinW`）。1600 上轨道够宽、
// 文字列吃的是余量，于是那 35 从它这儿出。
//
// 🔴 **不许让破保底**：让完 1600 上还剩 82.6 > 76。
//
// 🔴 **进度行一个像素都不许让**：它在中间那一列里，压根不在左列里，所以左列
// 撑出去的那 35 **不该**碰到它——900 宽下评论键右沿 323，滑块左沿 330.7，
// 中间剩下 7.7（`TestTheCommentsKeyDoesNotTouchTheProgressBar` 那条盯的是
// 「不碰」，这一条盯的是「一寸不让」）。
//
// ⚠️ **比的是「有数字 / 没数字」两档**：同一个窗口、同一首歌、同一个 bvid，
// 只有 `stat.reply` 不同——于是差别只有评论键那 35，没有别的变量。
func TestTheCountDoesNotSqueezeTheTitleOrTheSlider(t *testing.T) {
	const longTitle = "一首歌的名字很长很长很长很长"
	const longArtist = "一个非常非常非常非常非常非常长的 UP 主名字"
	// 那 1 是浮点噪声的容差（同一档同一首歌的两次测量抖 0.0~0.1）。
	const slack = 1
	for _, width := range []int{900, 1000, 1200, 1600} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			it := app.Item{
				Source: "online", Type: "mv", Bvid: "BVcountWidth", Cid: "1",
				Title: longTitle, Artist: longArtist, ArtistMid: 1,
			}
			without := commentCountWidths(t, it, width, 0)
			with := commentCountWidths(t, it, width, 1234567) // 数字吃到上限 33 那一档

			keyGrew := with["评论键"] - without["评论键"]
			if keyGrew <= slack {
				t.Fatalf("宽度 %d：带数字的评论键 %.1f，不带数字的 %.1f——那一档量错了（这一条比不了）",
					width, with["评论键"], without["评论键"])
			}
			for _, what := range []string{longTitle, longArtist} {
				// 🔴 让的**只能是评论键长出来的那一截**。
				if gave := without[what] - with[what]; gave > keyGrew+slack {
					t.Errorf("宽度 %d：「%s」让了 %.1f，而评论键只长了 %.1f——"+
						"数字花掉了别的东西的宽度", width, what, gave, keyGrew)
				}
				// 🔴 让完不许破那一列的保底。
				if with[what] < trackTextMinW-slack {
					t.Errorf("宽度 %d：带数字之后「%s」只剩 %.1f 宽（保底 %d）——"+
						"文字列让破了", width, what, with[what], trackTextMinW)
				}
			}
			// 🔴 进度行：**一个像素都不许变**。
			for _, what := range []string{"播放进度左沿", "播放进度宽"} {
				if d := absFloat(with[what] - without[what]); d > slack {
					t.Errorf("宽度 %d：%s 从 %.1f 变成 %.1f（差 %.1f）——评论数把它挤了",
						width, what, without[what], with[what], d)
				}
			}
			// 评论键的右沿也不许压到进度行上。
			if key, bar := with["评论键右沿"], with["播放进度左沿"]; key > bar {
				t.Errorf("宽度 %d：评论键右沿在 %.1f，而进度行从 %.1f 开始——它们碰上了",
					width, key, bar)
			}
		})
	}
}

// commentCountWidths 量一档播放条上那几样各多宽。
func commentCountWidths(t *testing.T, it app.Item, width int, reply int64) map[string]float32 {
	t.Helper()
	tt, _ := commentCountPlaybarTester(t, it, width, reply)
	out := map[string]float32{}
	if r, ok := tt.Find(commentsKeyLabel(reply)); ok {
		out["评论键"] = r.W
		out["评论键右沿"] = r.X + r.W
	} else {
		t.Fatalf("宽度 %d / %d 条：画面上找不到评论键，文字有 %q", width, reply, tt.Texts())
	}
	for _, what := range []string{it.Title, it.Artist} {
		if r, ok := tt.Find(what); ok {
			out[what] = r.W
		} else {
			t.Fatalf("宽度 %d / %d 条：画面上找不到 %q，文字有 %q", width, reply, what, tt.Texts())
		}
	}
	if r, ok := tt.Find("播放进度"); ok {
		out["播放进度左沿"] = r.X
		out["播放进度宽"] = r.W
	} else {
		t.Fatalf("宽度 %d / %d 条：画面上找不到进度条，文字有 %q", width, reply, tt.Texts())
	}
	return out
}

// TestTheWidthCommentsMatchTheCode 钉住**那一大段宽度注释与代码/实测一致**。
//
// 🔴 这一段注释是「为什么左列保底是这么多」的唯一说明，而下一个人调宽度时就是
// 照着它算的——**写错一个数就是把人带偏一次**（票 #682 的审查抓到两处：
// 最小宽度写的 305，实际 307；「四档都是 76」里 1600 其实是 82.6）。
//
// 判据分三层：
//
//  1. 常量之间的账（`playbarActionsW` = 三枚 32 + 评论键 67 = 163）；
//  2. 注释里那个「最小宽度：**N**」要与 `playbarLeftMinW` 算出来的数一致；
//  3. 「歌名的宽」那段要与**实测**一致：1600 那一档不是 76，注释里必须写出量到
//     的那个数（`fmt.Sprintf("%.1f")` 的同一个格式）。
func TestTheWidthCommentsMatchTheCode(t *testing.T) {
	// 1. 常量的账。
	if playbarCommentsW != iconHitMd+commentCountGap+commentCountMaxW {
		t.Errorf("playbarCommentsW = %d，want %d（32 + 缝 + 数字格）",
			playbarCommentsW, iconHitMd+commentCountGap+commentCountMaxW)
	}
	if playbarCommentsW != 67 {
		t.Errorf("playbarCommentsW = %d，want 67（票 #682 量出来的那一档）", playbarCommentsW)
	}
	if playbarActionsW != 163 {
		t.Errorf("playbarActionsW = %d，want 163（三枚 96 + 评论键 67）", playbarActionsW)
	}
	if playbarLeftMinW != 307 {
		t.Errorf("playbarLeftMinW = %d，want 307（52 + 8 + 76 + 8 + 163）", playbarLeftMinW)
	}
	if commentCountGap != 2 {
		t.Errorf("commentCountGap = %d，want 2", commentCountGap)
	}

	// 2/3. 注释要与代码和实测对得上。
	b, err := os.ReadFile("playbar.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)

	// 注释里那句「最小宽度：**N**」。
	claimed := -1
	for _, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, "playbarLeftMinW 是左边那一列的最小宽度") {
			continue
		}
		if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(
			strings.TrimSpace(line), "// playbarLeftMinW 是左边那一列的最小宽度：**")),
			"%d", &claimed); err != nil {
			t.Fatalf("读不出那一行注释里的宽度：%q（%v）", line, err)
		}
	}
	if claimed < 0 {
		t.Fatal("playbar.go 里找不到「playbarLeftMinW 是左边那一列的最小宽度」那一行注释")
	}
	if claimed != playbarLeftMinW {
		t.Errorf("注释说最小宽度是 %d，代码算出来是 %d——照着注释算的人会被带偏",
			claimed, playbarLeftMinW)
	}

	// 歌名的宽：量四档，把「不是 76 的那一档」的真实数写进注释里。
	const longTitle = "一首歌的名字很长很长很长很长"
	it := app.Item{
		Source: "online", Type: "mv", Bvid: "BVcommentWidthDoc", Cid: "1",
		Title: longTitle, Artist: "某个 UP 主", ArtistMid: 1,
	}
	for _, width := range []int{900, 1000, 1200, 1600} {
		got := commentCountWidths(t, it, width, 1234567)[longTitle]
		if got == trackTextMinW {
			continue
		}
		// 那一档不是保底宽（轨道说话）：注释里得写出这个数，而不是笼统地说
		// 「四档都是 76」。
		want := fmt.Sprintf("%.1f", got)
		if !strings.Contains(src, want) {
			t.Errorf("宽度 %d 上歌名实测 %.1f，而 playbar.go 的宽度注释里没有这个数"+
				"——注释说的和量出来的不一致", width, got)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E：整窗、真的点一下
// ─────────────────────────────────────────────────────────────────────────────

// TestThePlayBarShowsTheCommentCountThenDropsItForALocalFile 是这一票的 E2E。
//
// # 走的是整条链
//
// 假 B 站（`/x/web-interface/view` 带 `stat.reply`）→ `App.CommentCount()`
// 那一趟 → 播放条上画出数字 → 切到本地文件 → 那一枚键**整个不画**。
//
// ⚠️ **等的是结果**（`waitFrames`）：那一趟在后台 goroutine 上跑，按帧数等就是
// 按运气等。
//
// ⚠️ **切歌用的是 `Queue().Reset`**：那是「用户点了另一首」在数据层的等价动作，
// 而 `CommentsWorthAsking` 判的正是队列的当前项。
func TestThePlayBarShowsTheCommentCountThenDropsItForALocalFile(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setViewReply(2468)

	s, tt := newShell(t, at(1200, 900))
	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	// ⚠️ **在线那一首**：`CommentsWorthAsking` 判的是「当前项是 B 站的对象」。
	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVcommentCount", Cid: "1",
		Title: "有评论的那一首", Artist: "某个 UP 主",
	}); err != nil {
		t.Fatalf("把在线那首放进队列：%v", err)
	}
	waitFrames(t, tt, "在线那首开始播", func() bool { return s.app.Snapshot().HasTrack })

	// ⚠️ **`Find` 是全等匹配**（画面上那一段字正好是「2468」），所以它认的是
	// **画出来的**那个数字，不是无障碍名字里顺带带的（`HasText` 认子串，
	// 「评论 2468」也能满足它——那样一个只改名字、不画数字的实现照样绿）。
	want := list.PlayCountText(2468)
	waitFrames(t, tt, "播放条上出现评论数", func() bool {
		_, ok := tt.Find(want)
		return ok
	})
	// 那一枚键的无障碍名字里也要有它（读屏用户听得见）。
	if _, ok := tt.Find(commentsKeyLabel(2468)); !ok {
		t.Fatalf("评论键的名字里没有数字，文字有 %q", tt.Texts())
	}

	// 切到本地文件：评论键**整个不画**（`CommentsWorthAsking` 为 false）。
	s.app.Queue().Reset(app.LocalItem(flacPath(t)))
	waitFrames(t, tt, "本地那一首开始播", func() bool { return s.app.Snapshot().HasTrack })

	if _, ok := tt.Find("评论"); ok {
		t.Errorf("切到本地文件之后还摆着「评论」键，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find(want); ok {
		t.Errorf("切到本地文件之后评论数 %q 还留在画面上，文字有 %q", want, tt.Texts())
	}
	if tt.HasText(want) {
		t.Errorf("切到本地文件之后评论数 %q 还留在某一段文字里，文字有 %q", want, tt.Texts())
	}
}

// commentCountPlaybarTester 只画播放条，假 B 站的 `view` 回给定的评论数。
//
// ⚠️ **状态是手给的、队列是真的**：判据有两半——`st.HasTrack`（有没有歌在放，
// 播放条那一族的老测试也这么给）和 `CommentsWorthAsking`（当前这一首在 B 站上
// 有没有对象，看队列）。给两样才摆得出那一枚键（理由见
// `commentsPlaybarTester`）。
func commentCountPlaybarTester(t *testing.T, it app.Item, w int, reply int64) (*mygoui.Tester, *Shell) {
	t.Helper()
	b := newCommentBili(t, "reply-main.json")
	b.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"aid": 170001, "bvid": r.URL.Query().Get("bvid"),
			"stat": map[string]any{"reply": reply},
		})
	})
	ts := commentCountShell(t, b, it, w)
	// 等**应用层**拿到那个数（播放条那一帧才会画出它）。
	waitFrames(t, ts.tt, "评论数问回来了", func() bool {
		return ts.s.app.CommentCount() == reply
	})
	ts.tt.Frame()
	return ts.tt, ts.s
}

// commentCountPlaybarWith 用**指定的那台假 B 站**画播放条（不等任何条件）。
//
// ⚠️ **它和 `commentCountPlaybarTester` 分开是因为 `reply=0` 那一档**：
// `CommentCount() == 0` 在那一趟发出去**之前**也成立——拿它当「问回来了」的
// 判据会测早了（见 `TestTheCommentCountNeverShowsZero`）。
func commentCountPlaybarWith(t *testing.T, b *commentBili, it app.Item, w int) *mygoui.Tester {
	t.Helper()
	ts := commentCountShell(t, b, it, w)
	ts.tt.Frame()
	return ts.tt
}

// commentCountShell 造一个画播放条的外壳，数据走给定的那台假 B 站。
func commentCountShell(t *testing.T, b *commentBili, it app.Item, w int) *testShell {
	t.Helper()
	cli := bili.NewClient(bili.Options{Transport: b.Server.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)

	st := barState()
	st.Title = it.Title
	st.Artist = it.Artist
	all := append([]shellOpt{at(w, coverWinH), mounted(func(t *testing.T, a *app.App) {
		a.MountLogin(app.NewMemoryStore("test"), cli)
		a.MountStreaming(app.Streaming{
			Client: cli, Media: b.Server.Client(), TempDir: t.TempDir()})
	})}, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.viewPlayBar(c, st)
		}
	}))
	ts := newTestShell(t, all...)
	installFakeImages(t, ts.s)
	ts.s.app.Queue().Reset(it)
	return ts
}

// findCommentsKey 在画面上找那一枚评论键，**带不带数字都认**。
//
// ⚠️ **`Tester.Find` 是全等匹配**，而票 #682 起那一枚的名字里可能带评论数
// （`commentsKeyLabel`）——写死 `Find("评论")` 的话，一拿到数字它就找不到了，
// 于是量得到的是「那一枚键不在」，而画面上它明明在。
func findCommentsKey(tt *mygoui.Tester) (mygoui.Rect, bool) {
	for _, txt := range tt.Texts() {
		if txt == "评论" || strings.HasPrefix(txt, "评论 ") {
			return tt.Find(txt)
		}
	}
	return mygoui.Rect{}, false
}
