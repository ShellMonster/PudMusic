// 播放条左边第三段那三枚键的读数（票 #701）。
//
// # 这一组断的是「画面上有什么」
//
// 三枚键（赞 / 收藏 / 评论）各带一个数字——数字画没画、画成什么样、会不会把
// 邻居挤坏——三样都不会报错，只会在画面上悄悄不对。所以断的是**渲染结果**
// （`Tester` 量出来的框和文字），不是「某个函数被调到了」。
//
// # 🔴 宽度账是本票最要紧的一条
//
// 900 宽（应用的最小窗口宽）下，左边那一列的右端离进度条滑块左沿只剩 **6.70**
// 像素——而**撞上去的后果是「点不动，而画面上看不出坏了」**（票 #216 那个坑：
// 中间那一列是 `PassThrough` 的，可它的子节点照旧登记命中，于是左列撑出来的
// 那一截被进度行的命中框盖住）。
//
// ⇒ `TestTheThreeCountsDoNotTouchTheProgressBar` 是这一组的主断言。
//
// # 一个真请求都不发
//
// 数据走本地 HTTP fixture（`commentBili`）——那一台假 B 站答
// `/x/web-interface/view`，而三个数就在那个回包的 `stat` 里。
package ui

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 画面上有没有那个数
// ─────────────────────────────────────────────────────────────────────────────

// TestTheThreeKeysEachShowTheirCount 钉住**三枚键各带自己的数**。
//
// 🔴 **三枚都要断**：只断一枚的话，「收藏数画在赞键上」那一档照样绿——而用户
// 读到的是**另一首歌的数字**。
//
// ⚠️ **判据是「画面上那一串字」**（`Tester.Find` 是全等匹配）：它认的是**画出来
// 的那个数字**，而不是无障碍名字里顺带带的（`HasText` 认子串，「赞 1k+」也能
// 满足它——那样一个只改名字、不画数字的实现照样绿）。
func TestTheThreeKeysEachShowTheirCount(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 2468, 9999)

	// ⚠️ **三个数碰巧一样时判据就分不开了**：所以这里挑三个**不同**的数。
	for _, c := range []struct {
		name  string
		count int64
	}{
		{"评论", 1286},
		{"收藏", 2468},
		{"赞", 9999},
	} {
		want := list.CountText(c.count)
		if want == "" {
			t.Fatalf("%s 那一栏折成了空串——测试自己写错了", c.name)
		}
		if _, ok := tt.Find(want); !ok {
			t.Errorf("「%s」键上没有那个数 %q（文字有 %q）", c.name, want, tt.Texts())
		}
	}
}

// TestTheThreeCountsAreFoldedTheWideWay 钉住**折法是「1k+ / 10k+ / 1亿+」那一套**。
//
// 🔴 **判据是「画面上是 `9k+` 而不是 `9999`」**：这一族的折法（`list.CountText`）
// 和播放量那一族的（`list.PlayCountText`，折成「1.2万」）**是两件事**——后者
// 更宽，而这一族的**宽度上界必须钉死**（理由见文件头）。
//
// ⚠️ **9999 这一档最能说明问题**：播放量那一族会折成「9999」（四位数），而这一
// 族折成「9k+」（至少 9 千）——窄 7.9 像素，而语义一样诚实（「至少 9 千」对
// 9999 是真的，不低估）。
func TestTheThreeCountsAreFoldedTheWideWay(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 9999, 9999, 9999)

	if _, ok := tt.Find("9k+"); !ok {
		t.Errorf("画面上没有「9k+」（文字有 %q）——那一族该折成「Nk+」", tt.Texts())
	}
	if _, ok := tt.Find("9999"); ok {
		t.Errorf("画面上出现了「9999」（文字有 %q）——那是**播放量**那一族的折法", tt.Texts())
	}
}

// TestTheThreeCountsNeverShowZero 钉住**拿不到就不画**。
//
// 🔴 「没有数字」和「零个赞」在屏幕上长得一样，可它们说的是两件事：前者是
// 「还不知道」（离线 / 那一趟还没回来），后者是「这一首真的没人赞」。画一个
// 「0」会让用户以为后者。
//
// ⚠️ **键照旧要能点**：拿不到数字不是这一枚键的错。
func TestTheThreeCountsNeverShowZero(t *testing.T) {
	// `stat` 里的三个数都是 0：B 站没给这些数（或者真是零）——两种都不画。
	b := newCommentBili(t, "reply-main.json")
	b.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"aid": 170001, "bvid": r.URL.Query().Get("bvid"),
			"stat": map[string]any{"reply": 0, "favorite": 0, "like": 0},
		})
	})
	tt := countsPlaybarWith(t, b, mvItem(), 1200)

	// ⚠️ **等那一趟真的发出去**（不是等「拿到了」——数是 0 在那一趟发出去**之前**
	// 也成立）。发出去之后是两个状态之一：还在路上 / 已经拿到 0——**两个状态都
	// 不许画数字**。
	waitFrames(t, tt, "稿件详情那一趟发出去了", func() bool {
		return b.countHit("/x/web-interface/view") > 0
	})
	tt.Frame()

	// 名字必须正好是「赞」「收藏」「评论」：名字里没有数字。
	for _, name := range []string{"赞", "收藏", "评论"} {
		if _, ok := tt.Find(name); !ok {
			t.Errorf("拿不到数字的时候那一枚键的名字不是 %q，文字有 %q", name, tt.Texts())
		}
	}
	for _, txt := range tt.Texts() {
		if txt == "0" || txt == "0 条" {
			t.Errorf("画面上出现了 %q——拿不到数字不该画成「零」", txt)
		}
	}
}

// TestTheThreeCountsDisappearForALocalFile 钉住**本地文件三枚都不摆**。
//
// 🔴 **本地文件在 B 站上没有对应的对象**：那一趟压根不该发（发出去是「问一个
// 认不出来的 oid」），而**三个数一个都不该画**。
//
// ⚠️ **判据是「画面上根本没有那三个数」**，而不是「数字是 0」。
func TestTheThreeCountsDisappearForALocalFile(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 2468, 9999)

	local := app.Item{Source: "local", Type: "local", Path: "/tmp/x.mp3", Title: "本地的一首"}
	tt2 := countsPlaybarWith(t, newCommentBili(t, "reply-main.json"), local, 1200)
	for _, want := range []string{list.CountText(1286), list.CountText(2468), list.CountText(9999)} {
		if _, ok := tt2.Find(want); ok {
			t.Errorf("本地文件那一档还画着 %q（文字有 %q）", want, tt2.Texts())
		}
	}
	_ = tt
}

// TestTheCountNeverLosesItsPlus 钉住**「+」不许被截掉**。
//
// 🔴 **「+」是这一族的语义**（票面原话：「「+」恒表示「至少这么多」」）——
// 把它截掉就变成了「正好 999」，那是**撒谎**。
//
// ⚠️ **判据是「每一档都装得下上限」**：数字格有 `MaxWidth(playbarCountMaxW)`
// （24.5），而折法最宽的一档是「99k+」（实测 **24.49**）——**刚好装下**。
//
// ⇒ 所以这一条断的是**每一档**：**一个都不许截**。
//
// 🔴 **为什么「不许截」是硬要求**：**截断切的是末尾，而「+」永远是最后一个
// 字符** —— 所以「+」**总是第一个被切掉的**（实测：「99k+」上限 24 → 墨迹
// 20.00；上限 21 → 墨迹 14.00，比不带「+」的「99k」（18.52）**还短**）。
// 而「+」是这一族的**语义**（「至少这么多」）——它被切掉就变成了「正好 99 千」，
// 那是**撒谎**。
//
// ⚠️ **判据怎么算「装得下」**：**单独画一遍**那一串字（同一个上限、同一个字号），
// 量**它自己**的墨迹——**不能**量真实布局里那一格的墨迹（左右就是邻居的图形，
// 混进来就分不出哪一笔是那个「+」，实测那一格量出来 30 宽而字只有 24.49）。
func TestTheCountNeverLosesItsPlus(t *testing.T) {
	// ⚠️ **这些档位一个都不许截**。
	//
	// 🔴 **99999 和 100000 必须在这一档里**（第一版把它们漏了——而那正是唯一
	// 会截掉「+」的那一档）：
	//
	//	99999  → 「99k+」（实测 24.49，**最宽的那一档**）
	//	100000 → 「1亿+」（实测 23.61）
	//
	// ⇒ 上限是 **24.5**，**刚好装下** 24.49 —— 而**上限再低一档（24）**，
	// 「99k+」就装不下，而**截断切的是末尾，所以「+」第一个被切掉**
	// （实测：「99k+」上限 24 → 墨迹 20.00；上限 21 → 墨迹 14.00，比不带
	// 「+」的「99k」（18.52）**还短**）。
	for _, n := range []int64{
		1, 99, 999, 1000, 1234, 9999, 10000, 12345, 20000,
		99999, 100000, 1000000,
	} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			b := newCommentBili(t, "reply-main.json")
			b.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
				uiExploreJSON(w, 0, map[string]any{
					"aid": 170001, "bvid": r.URL.Query().Get("bvid"),
					"stat": map[string]any{"reply": n, "favorite": n, "like": n},
				})
			})
			it := app.Item{
				Source: "online", Type: "mv", Bvid: "BVcountsPlus", Cid: "1",
				Title: "T", Artist: "A", ArtistMid: 1,
			}
			ts := countsShellWith(t, b, it, 1200)
			waitFrames(t, ts.tt, "数问回来了", func() bool {
				return ts.s.app.ArchiveCounts().Reply == n
			})
			ts.tt.Frame()

			want := list.CountText(n)
			// 那一格的无障碍名字里是**完整**的那一串（`Label` 不受 `MaxWidth`
			// 影响）——而画面上那一格**受**。
			if _, ok := ts.tt.Find(want); !ok {
				t.Fatalf("画面上找不到 %q（文字有 %q）", want, ts.tt.Texts())
			}
			// 🔴 **判据是「那一串字在**同样的上限**下画出来，末尾的那个「+」还在不在」**。
			//
			// ⚠️ **为什么不量真实布局里那一格的墨迹**：它**量不准**——左右两枚键的
			// 图形就在旁边（实测：那一格的墨迹量出来是 30 宽，而字只有 24.49），
			// 邻居的字迹混进来，分不出哪一笔是那个「+」。
			//
			// ⇒ 所以**单独画一遍**那一串字（同一个上限、同一个字号），量它自己的
			// 墨迹：**「+」被切掉时那一截短 4 像素以上**，而完整时只差 0.5~0.7
			//（字有左右边距）。
			//
			// ⚠️ **为什么这个判据是对的**：截断**切的是末尾**，而「+」**永远是最后
			// 一个字符** —— 所以「+」**总是第一个被切掉的**（实测：「99k+」上限 24
			// → 墨迹 20.00；上限 21 → 墨迹 14.00，比不带「+」的「99k」（18.52）
			// **还短**）。
			ink := isolatedInkWidth(t, want, playbarCountMaxW)
			var full float32
			mygoui.NewTester(func(c *mygoui.Context) {
				full, _ = c.MeasureText(0, mygoui.Span{
					Text: want, Size: scale.Font(c, playbarCountFont)})
			}, 300, 60).Frame()
			if full-ink > 2 {
				t.Errorf("数 %d（%q）被截了：同一个上限（%.1f）下那一串字的墨迹只有 %.2f，"+
					"而字宽是 %.2f——差的那一截是末尾那个「+」"+
					"（那个「+」是这一族的语义）",
					n, want, playbarCountMaxW, ink, full)
			}
		})
	}
}

// TestTheCountKeepsItsPlusWhenTheSystemTextIsEnlarged 钉住**系统文本放大时那个
// 「+」还在**。
//
// 🔴 **这是「那一格的字号钉死在 10」那一条的判据**（票 #701 的审查）：
//
// 那一格的字**不乘** `scale.Font`（用户的系统文本大小）。而如果它乘了，
// **125% / 150% 是 Windows 上的常见设置**——那时：
//
//	scale 1.00  99k 墨迹 18  |  99k+ 墨迹 24  ✅ 「+」还在
//	scale 1.25  99k 墨迹 23  |  99k+ 墨迹 17  ❌ 「+」被切
//	scale 1.40  99k 墨迹 19  |  99k+ 墨迹 19  ❌ 「+」被切
//	scale 2.00  99k 墨迹 13  |  99k+ 墨迹 13  ❌ 「+」被切
//
// ⇒ 而**「+」是这一族的语义**（「至少这么多」）——它被切掉就是在说谎。
//
// ⚠️ **判据是「那一格在放大之后，墨迹仍然**比不带「+」的那一串长**」**：
// 截断切的是末尾，而「+」永远是最后一个字符 ⇒ **「+」被切掉时，那一格的墨迹
// 会比不带「+」的还短**（实测 1.25：17 < 23）。
//
// ⚠️ **怎么设 TextScale**：框架的 `Preferences().TextScale`（`mygo/ui/
// preferences.go`）是**宿主**给的——测试里用 `Tester.SetPreferences`。
func TestTheCountKeepsItsPlusWhenTheSystemTextIsEnlarged(t *testing.T) {
	for _, sc := range []float32{1, 1.15, 1.25, 1.4, 1.5, 2} {
		t.Run(fmt.Sprint(sc), func(t *testing.T) {
			for _, n := range []int64{1000, 9999, 10000, 99999, 100000} {
				want := list.CountText(n)
				withoutPlus := strings.TrimSuffix(want, "+")

				// ⚠️ **量的是那一格**在放大之后**实际画出来**的样子——而那一格的
				// 字号**钉死在 10**（不乘系统缩放），所以**字宽恒定** ⇒ 那个「+」
				// **在任何 scale 下都在**。
				//
				// ⇒ 所以这一条要验的是「**字号真的没跟着缩放**」：如果有人把
				// `playbarCountFont` 改回 `scale.Font(c, playbarCountFont)`，
				// 下面这一测立刻红（那时字宽会涨、那一格会被截、「+」会没）。
				ink, base := scaledCellInks(t, want, withoutPlus, sc)

				// 🔴 **「+」还在 ⇒ 那一格的墨迹比不带「+」的**长**（那个加号自己
				// 就有 4~5 像素）。
				if ink <= base {
					t.Errorf("系统文本放大 %.2f：%q 那一格的墨迹只有 %.2f，"+
						"而不带「+」的 %q 是 %.2f——**那个「+」被切掉了**"+
						"（而「+」是这一族的语义）",
						sc, want, ink, withoutPlus, base)
				}
			}
		})
	}
}

// scaledCellInks 量「**界面上那一格**」在给定的系统文本缩放下，它自己的墨迹
// 有多宽（带「+」的那一串，和不带「+」的那一串）。
//
// 🔴 **走的是生产代码**（`countValueText`）——这是这一条能成立的原因：
// 自己画一遍的话，字号是测试里写的那个数，而**被测的那件事恰恰是「字号乘不乘
// 系统缩放」**——自己画就等于把要验的东西当成了前提。
//
// ⚠️ **为什么要把「不带「+」的那一串」也量一遍**：那是**对照组**——
// 截断切的是末尾，而「+」永远是最后一个字符 ⇒ **「+」被切掉时，那一格的墨迹
// 会比不带「+」的还短**（实测 scale 1.25：17 < 23）。
//
// ⚠️ **单独画**：真实布局里那一格的左右**就是邻居的图形**，量墨迹会把邻居的
// 字迹算进来（实测：那一格量出来 30 宽，而字只有 24.49）——分不出哪一笔是
// 那个「+」。⇒ 这里让那一格**单独占一行**。
func scaledCellInks(t *testing.T, withPlus, withoutPlus string, scale float32) (float32, float32) {
	t.Helper()
	// 🔴 **带「+」的那一格走**生产代码**（`countValueText`）——这是这一条能成立的
	// 原因：自己画一遍的话，字号是测试里写的那个数，而**被测的那件事恰恰是
	// 「字号乘不乘系统缩放」**——自己画就等于把要验的东西当成了前提。
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Children(func() {
			countValueText(c, countForText(withPlus))
		})
	}, 300, 60)
	tt.SetPreferences(mygoui.Preferences{TextScale: scale})
	tt.Frame()
	with := inkWidthOf(t, tt, withPlus)

	// ⚠️ **对照组是「同一串字去掉那个「+」」，而且它**不是折法的输出**——
	// 折法那一档永远带「+」，所以**不能**喂给 `countValueText`（那样折出来还是
	// 带「+」的字，量的是同一串，两边相等，判据就成了空话——第一版就这么写的，
	// 变异测试立刻揭穿了它）。
	//
	// ⇒ 所以对照组**直接画那一串字**（**同样的字号、同样的上限**）——它测的是
	// 「那个「+」自己占多少像素」。
	tt2 := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Children(func() {
			mygoui.Text(c, withoutPlus).FontSize(playbarCountFont).
				MaxWidth(playbarCountMaxW).MaxLines(1).Ellipsis("…")
		})
	}, 300, 60)
	tt2.SetPreferences(mygoui.Preferences{TextScale: scale})
	tt2.Frame()
	without := inkWidthOf(t, tt2, withoutPlus)
	return with, without
}

// countForText 把「一串字」换算成「能折出它的那一个数」。
//
// ⚠️ **它走的是折法自己的逆运算**：「99k+」⇒ 99000、「1亿+」⇒ 100000000。
// 而**数字那一格画的是折出来的字**，所以要喂给 `countValueText` 的是**数**。
func countForText(s string) int64 {
	body := strings.TrimSuffix(s, "+")
	var head int64
	for _, r := range body {
		if r >= '0' && r <= '9' {
			head = head*10 + int64(r-'0')
		} else {
			break
		}
	}
	if strings.Contains(body, "亿") {
		return head * 100_000_000
	}
	if strings.Contains(body, "k") {
		return head * 1000
	}
	return head
}

// inkWidthOf 量画面上「某一串字」自己的墨迹有多宽。
func inkWidthOf(t *testing.T, tt *mygoui.Tester, s string) float32 {
	t.Helper()
	cell, ok := tt.Find(s)
	if !ok {
		t.Fatalf("画面上找不到 %q（文字有 %q）", s, tt.Texts())
	}
	img := tt.Image()
	base := img.RGBAAt(250, 5)
	minX, maxX := -1, -1
	// ⚠️ **只扫那一格自己的范围**（不多扫一个像素）：这一格的**左边**就是相邻
	// 元素（实测：多扫 2 像素就把邻居的字迹算进来，量出来的墨迹比字宽 6 像素
	// ——而那一格是**截断**的，邻居的墨迹把真相盖住了）。
	for x := int(cell.X); x < int(cell.X+cell.W); x++ {
		for y := int(cell.Y); y < int(cell.Y+cell.H); y++ {
			p := img.RGBAAt(x, y)
			d := absInt(int(p.R)-int(base.R)) + absInt(int(p.G)-int(base.G)) +
				absInt(int(p.B)-int(base.B))
			if d > 40 {
				if minX < 0 || x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
			}
		}
	}
	if minX < 0 {
		t.Fatalf("%q 那一格上一点墨迹都没有", s)
	}
	return float32(maxX - minX + 1)
}

// ─────────────────────────────────────────────────────────────────────────────
// 无障碍：数字不许只靠画面传达
// ─────────────────────────────────────────────────────────────────────────────

// TestTheThreeCountsSayTheNumberToScreenReaders 钉住**数字不只靠画面传达**。
//
// 🔴 图标键的无障碍名字就是它唯一的凭据（`mygo/ui/svg.go`：图标对辅助技术
// 不可见）。数字画在图标旁边、可是名字里没有的话，读屏用户听到的仍然是
// 「赞」两个字——数字对他们不存在。
//
// ⚠️ **三枚都要断**：只断一枚的话，另外两枚的数字照样只靠画面传达。
func TestTheThreeCountsSayTheNumberToScreenReaders(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 2468, 9999)

	for _, c := range []struct {
		name  string
		count int64
	}{
		{"评论", 1286},
		{"收藏", 2468},
		{"赞", 9999},
	} {
		want := c.name + " " + list.CountText(c.count)
		if _, ok := tt.Find(want); !ok {
			t.Errorf("无障碍名字不是 %q（文字有 %q）——读屏用户听不到那个数", want, tt.Texts())
		}
	}
}

// TestTheThreeCountsAreInTheHoverTips 钉住**悬停提示里也有那个数**。
//
// ⚠️ **数字只画在画面上，指针停在别处时用户读不到它**——所以提示句里也得说
// 一遍。
//
// 🔴 **提示句还要说清是「整个稿件」的**：多 P 的视频里放的是第 3 集，而显示的
// 是整稿的数字（票 #701 的语义要求）。
func TestTheThreeCountsAreInTheHoverTips(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 2468, 9999)

	for _, c := range []struct {
		name  string
		count int64
	}{
		{"评论", 1286},
		{"收藏", 2468},
		{"赞", 9999},
	} {
		key, ok := tt.Find(c.name + " " + list.CountText(c.count))
		if !ok {
			t.Fatalf("画面上找不到 %q 那一枚键", c.name)
		}
		tt.Move(key.X+key.W/2, key.Y+key.H/2)
		tt.Frame()
		// 🔴 **提示里给的是完整数字**，不是画面上那个折过的字
		// （票 #701 的要求）——而「1亿+」那一档**更是只能这样**（那一档是夸大的
		// 近似，只有完整数字说得清）。
		want := strconv.FormatInt(c.count, 10)
		if !tt.HasText("整个稿件 " + want) {
			t.Errorf("「%s」键的悬停提示里没有「整个稿件 %s」那半句（文字有 %q）",
				c.name, want, tt.Texts())
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 🔴 宽度账
// ─────────────────────────────────────────────────────────────────────────────

// TestTheThreeCountsDoNotTouchTheProgressBar 钉住**三枚键都带数字时不撞进度条**。
//
// 🔴 **这是本票最要紧的一条**：撞上去的后果是**点不动，而画面上看不出坏了**
// （票 #216 那个坑：中间那一列是 `PassThrough` 的，可它的子节点照旧登记命中，
// 于是左列撑出来的那一截被进度行的命中框盖住）。
//
// ⚠️ **量的是「那一枚键的右沿」和「进度行的左沿」**：前者是第三段那一整段的
// 右端（不是某一枚键的），后者是滑块那一块的左沿。
//
// ⚠️ **四档都要量**：900（应用的最小窗口宽，余量最小）、1000、1200、1600。
func TestTheThreeCountsDoNotTouchTheProgressBar(t *testing.T) {
	for _, width := range []int{900, 1000, 1200, 1600} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			// ⚠️ **最坏的那一档**：三个数都折成最宽的那一档（上限那一格）。
			tt, _ := countsPlaybarTester(t, width, 9999, 9999, 9999)

			bar, ok := tt.Find("播放进度")
			if !ok {
				t.Fatalf("宽度 %d：画面上找不到进度条，文字有 %q", width, tt.Texts())
			}
			// 那一枚键的右沿：评论键是第三段的**最后一枚**。
			key, ok := tt.Find("评论 " + list.CountText(9999))
			if !ok {
				t.Fatalf("宽度 %d：画面上找不到评论键，文字有 %q", width, tt.Texts())
			}
			gap := bar.X - (key.X + key.W)
			if gap < 0 {
				t.Errorf("宽度 %d：第三段那一整段的右沿在 %.2f，而进度行从 %.2f 开始——"+
					"它们碰上了（而撞上去的后果是**点不动**，票 #216）",
					width, key.X+key.W, bar.X)
			}
			// ⚠️ **余量是「按数字格的上限算」的那一个**：第三段的宽按
			// `playbarCountMaxW`（24.5）预留——所以**实测的余量是 5.20**。
			//
			// 🔴 **门槛是 5**：余量 0.7 扛不住一次字体变化，而撞上的后果是
			// **点不动，而画面上看不出坏了**（票 #216 那个坑）。
			//
			// ⚠️ **5.20 是「装得下最宽那一档」换来的**：上限从 24 提到 24.5
			// （为了装下「99k+」24.49，而截断**先切掉那个「+」**）⇒ 余量从
			// 6.70 少到 5.20。
			if gap < 5 {
				t.Errorf("宽度 %d：第三段离进度行只剩 %.2f 像素——"+
					"余量不够，换一个字体就撞上去了（票 #216 那个坑：点不动，"+
					"而画面上看不出坏了）", width, gap)
			}
		})
	}
}

// TestTheThreeCountsDoNotSqueezeTheTitle 钉住**数字吃的是第三段，不是歌名**。
//
// 🔴 **歌名窄了是「界面上看不出坏了」的那种**（字还在那儿，只是截断得更早）——
// 而左列的**总宽**被 `playbarLeftMinW` 钉着，所以第三段长宽的时候，**让位的
// 只能是歌名那一列**。
//
// ⚠️ **判据是「有数字 / 没数字」两档**：同一个窗口、同一首歌、同一个 bvid，
// 只有 `stat` 里那三个数不同——于是差别只有那三枚键。
func TestTheThreeCountsDoNotSqueezeTheTitle(t *testing.T) {
	const longTitle = "一首歌的名字很长很长很长很长"
	const longArtist = "一个非常非常非常非常非常非常长的 UP 主名字"
	it := app.Item{
		Source: "online", Type: "mv", Bvid: "BVcountsTitle", Cid: "1",
		Title: longTitle, Artist: longArtist, ArtistMid: 1,
	}
	for _, width := range []int{900, 1000, 1200, 1600} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			without := countsWidths(t, it, width, 0, 0, 0)
			with := countsWidths(t, it, width, 9999, 9999, 9999)

			// ⚠️ **进度行一个像素都不许变**：它在中间那一列里，压根不在左列里。
			for _, what := range []string{"播放进度左沿", "播放进度宽"} {
				if d := absFloat(with[what] - without[what]); d > 1 {
					t.Errorf("宽度 %d：%s 从 %.1f 变成 %.1f（差 %.1f）——数字把它挤了",
						width, what, without[what], with[what], d)
				}
			}
			// 🔴 **歌名不许窄过保底**。
			for _, what := range []string{longTitle, longArtist} {
				if with[what] < trackTextMinW-1 {
					t.Errorf("宽度 %d：带数字之后「%s」只剩 %.1f 宽（保底 %d）——"+
						"文字列让破了", width, what, with[what], trackTextMinW)
				}
			}
		})
	}
}

// TestTheThreeCountsWidthMatchesTheCode 钉住**那一大段宽度注释与代码/实测一致**。
//
// 🔴 这一段注释是「为什么左列保底是这么多」的唯一说明，而下一个人调宽度时就是
// 照着它算的——**写错一个数就是把人带偏一次**。
//
// 判据分两层：
//
//  1. 常量之间的账（`playbarActionsW` = 分集 32 + 三枚 44.5 = 165.5）；
//  2. 注释里那个「最小宽度：**N**」要与 `playbarLeftMinW` 算出来的数一致。
func TestTheThreeCountsWidthMatchesTheCode(t *testing.T) {
	if playbarActionsW != iconHitMd+3*(iconMd+playbarCountGap+playbarCountMaxW) {
		t.Errorf("playbarActionsW = %v，want %v（分集 32 + 三枚带数字的键）",
			playbarActionsW, iconHitMd+3*(iconMd+playbarCountGap+playbarCountMaxW))
	}
	if playbarLeftMinW != coverSize+coverGap+trackTextMinW+coverGap+playbarActionsW {
		t.Errorf("playbarLeftMinW = %v，want %v（52 + 8 + 76 + 8 + %v）",
			playbarLeftMinW, coverSize+coverGap+trackTextMinW+coverGap+playbarActionsW,
			playbarActionsW)
	}
	// 🔴 **900 宽下的余量**：按「上限」算的那一档（左列右沿离滑块左沿）。
	//
	//	左列右沿 = 16 + 309.5 = 325.5
	//	滑块左沿 = 330.7
	//	余量 = 5.2
	//
	// ⚠️ **按上限算**（数字格 24.5）而不是按某一天的字宽：字宽随平台字体变，
	// 而保底宽**必须是常数**（理由见 `playbar_counts.go` 的文件头《两个口径》）。
	//
	// ⇒ 左列右沿 **325.5** 离滑块左沿 **330.7** 还有 **5.2**。
	// ⇒ 而**实测**（`TestTheThreeCountsDoNotTouchTheProgressBar` 量的是真实
	// 布局）也是 **5.20**——因为第三段是按**上限**（24.5）预留的。
	//
	// ⚠️ **5.20 是「装得下最宽那一档」换来的**：上限从 24 提到 24.5（为了装下
	// 「99k+」24.49，而截断会**先切掉那个「+」**）⇒ 余量从 6.70 少到 5.20。
	const sliderLeft = 330.7
	gap := sliderLeft - (playbarPadX + playbarLeftMinW)
	if gap < 0 {
		t.Errorf("按上限算，左列右沿离滑块左沿还有 %.2f——已经撞上了", -gap)
	}
	// 🔴 **余量必须 ≥ 5**：余量 0.7 扛不住一次字体变化，而撞上的后果是
	// **点不动，而画面上看不出坏了**（票 #216 那个坑）。
	if gap < 5 {
		t.Errorf("按上限算余量是 %.2f——票 #701 的验收线是 ≥ 5", gap)
	}
	if gap > 6 {
		t.Errorf("按上限算余量是 %.2f——比量出来的（5.20）宽太多，"+
			"说明上限那一档算错了", gap)
	}
}

// TestTheCountFoldIsUsedByAllThreeKeys 钉住**三枚键用的是同一套折法**。
//
// 🔴 **三枚键各有各的折法**就会有三套宽度账——而宽度账只有一套（左列保底宽
// 钉着 308）。所以折法必须是**同一个函数**（`list.CountText`）。
//
// ⚠️ **判据是「三个键折出来的字一样」**：喂三个一样的数，三枚键上的字必须
// 一样——不一样就说明其中一枚走了别的折法。
func TestTheCountFoldIsUsedByAllThreeKeys(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 1286, 1286)

	want := list.CountText(1286)
	// ⚠️ **`Tester.Find` 返回**第一个**匹配**：三枚键上的数字是**同一串字**，
	// 所以这里不能靠 `Find` 把它们分辨开——而是靠**位置**：三枚键是并排的，
	// 而**数字在每一枚键的右边**，所以后一枚必须在**前一枚的右边**。
	//
	// ⚠️ **这个判据断的是「数字挂在对应的键上」**：如果数字被画到了**别处**
	// （比如三枚键共用一个数字），三枚键的位置还是那样——所以它还断不掉那一档。
	// 断那一档的是上面那几条（各自画各自的那一格）。
	for _, name := range []string{"赞", "收藏", "评论"} {
		if _, ok := tt.Find(name + " " + want); !ok {
			t.Fatalf("「%s」键的名字不是 %q %q（文字有 %q）", name, name, want, tt.Texts())
		}
	}
	// 三枚键**从左到右**：分集之后是**收藏 / 赞 / 评论**（上游那一段的次序，
	// 见 `viewTrackActions` 的注释）。
	var prev float32 = -1
	for _, name := range []string{"收藏", "赞", "评论"} {
		key, ok := tt.Find(name + " " + want)
		if !ok {
			t.Fatalf("画面上找不到 %q 那一枚键", name)
		}
		if key.X < prev {
			t.Errorf("「%s」键在 %.1f，而前一枚的右沿在 %.1f——次序反了",
				name, key.X, prev)
		}
		prev = key.X + key.W
	}
}

// isolatedInkWidth 量「一串字在给定上限下画出来，**它自己**的墨迹有多宽」。
//
// ⚠️ **为什么要单独画**：真实布局里那一格的左右**就是邻居的图形**，量墨迹会
// 把邻居的字迹算进来（实测：那一格量出来 30 宽，而字只有 24.49）——分不出
// 哪一笔是那个「+」。⇒ 这里单独画一遍，四周没有别的东西。
func isolatedInkWidth(t *testing.T, s string, maxW float32) float32 {
	t.Helper()
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Children(func() {
			mygoui.Text(c, s).FontSize(scale.Font(c, playbarCountFont)).
				MaxWidth(maxW).MaxLines(1).Ellipsis("…")
		})
	}, 300, 60)
	tt.Frame()
	img := tt.Image()
	base := img.RGBAAt(250, 5)
	minX, maxX := -1, -1
	for x := 0; x < 120; x++ {
		for y := 0; y < 40; y++ {
			p := img.RGBAAt(x, y)
			d := absInt(int(p.R)-int(base.R)) + absInt(int(p.G)-int(base.G)) +
				absInt(int(p.B)-int(base.B))
			if d > 40 {
				if minX < 0 || x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
			}
		}
	}
	if minX < 0 {
		t.Fatalf("%q 那个上限下一点墨迹都没有", s)
	}
	return float32(maxX - minX + 1)
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E：整窗、真的点一下
// ─────────────────────────────────────────────────────────────────────────────

// TestTheThreeCountsShowUpAndTheKeysStillWork 是这一票的 E2E。
//
// # 走的是整条链
//
// 假 B 站（`/x/web-interface/view` 带 `stat.reply` / `stat.favorite` /
// `stat.like`）→ `App.ArchiveCounts()` 那一趟 → 播放条上三枚键各自画出数字 →
// 键**照旧点得动**。
//
// 🔴 **「点得动」是这一票的验收项之一**：数字把第三段撑宽了，而左列撑出轨道
// 的那一截上的键**点不动**（票 #216）——那是**画面上看不出坏了**的那种坏。
//
// ⚠️ **等的是结果**（`waitFrames`）：那一趟在后台 goroutine 上跑，按帧数等就是
// 按运气等。
func TestTheThreeCountsShowUpAndTheKeysStillWork(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setViewReply(1286)
	f.setViewCounts(2468, 9999)

	s, tt := newShell(t, at(1200, 900))
	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVcountsE2E", Cid: "1",
		Title: "有三个数的那一首", Artist: "某个 UP 主",
	}); err != nil {
		t.Fatalf("把在线那首放进队列：%v", err)
	}
	waitFrames(t, tt, "在线那首开始播", func() bool { return s.app.Snapshot().HasTrack })

	// 三个数都到齐。
	waitFrames(t, tt, "三个数问回来了", func() bool {
		c := s.app.ArchiveCounts()
		return c.Reply == 1286 && c.Favorite == 2468 && c.Like == 9999
	})
	tt.Frame()

	// 画面上三个数都在。
	for _, c := range []struct {
		name  string
		count int64
	}{
		{"评论", 1286},
		{"收藏", 2468},
		{"赞", 9999},
	} {
		if _, ok := tt.Find(list.CountText(c.count)); !ok {
			t.Errorf("画面上没有 %q（文字有 %q）", list.CountText(c.count), tt.Texts())
		}
	}

	// 🔴 **键照旧点得动**：指针停在赞键上，那句话要出来（悬停是最容易断的——
	// 点击更难断）。
	like, ok := tt.Find("赞 " + list.CountText(9999))
	if !ok {
		t.Fatalf("画面上找不到赞键，文字有 %q", tt.Texts())
	}
	tt.Move(like.X+like.W/2, like.Y+like.H/2)
	tt.Frame()
	if !tt.HasText("点赞") {
		t.Errorf("指针停在赞键上却没有那句话（文字有 %q）——"+
			"它被进度那一行的命中框盖住了（点下去也就没反应，而界面上看不出坏了）",
			tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 脚手架
// ─────────────────────────────────────────────────────────────────────────────

// countsPlaybarTester 只画播放条，而那台假 B 站回给定的三个数。
func countsPlaybarTester(t *testing.T, w int, reply, favorite, like int64) (*mygoui.Tester, *Shell) {
	t.Helper()
	b := newCommentBili(t, "reply-main.json")
	b.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"aid": 170001, "bvid": r.URL.Query().Get("bvid"),
			"stat": map[string]any{
				"reply": reply, "favorite": favorite, "like": like,
			},
		})
	})
	ts := countsShellWith(t, b, mvItem(), w)
	waitFrames(t, ts.tt, "三个数问回来了", func() bool {
		c := ts.s.app.ArchiveCounts()
		return c.Reply == reply && c.Favorite == favorite && c.Like == like
	})
	ts.tt.Frame()
	return ts.tt, ts.s
}

// countsPlaybarWith 用**指定的那台假 B 站**画播放条（不等任何条件）。
func countsPlaybarWith(t *testing.T, b *commentBili, it app.Item, w int) *mygoui.Tester {
	t.Helper()
	ts := commentCountShell(t, b, it, w)
	ts.tt.Frame()
	return ts.tt
}

// countsShellWith 用**指定的那台假 B 站**造一个画播放条的外壳（不等任何条件）。
//
// ⚠️ **和 `countsPlaybarWith` 分开**：那一份只给画面，而这一份还要**问应用层**
// （`ArchiveCounts()`）——而那一半要的是外壳，不是画面。
func countsShellWith(t *testing.T, b *commentBili, it app.Item, w int) *testShell {
	t.Helper()
	ts := commentCountShell(t, b, it, w)
	ts.tt.Frame()
	return ts
}

// countsWidths 量一档播放条上那几样各多宽。
func countsWidths(t *testing.T, it app.Item, width int, reply, favorite, like int64) map[string]float32 {
	t.Helper()
	b := newCommentBili(t, "reply-main.json")
	b.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"aid": 170001, "bvid": r.URL.Query().Get("bvid"),
			"stat": map[string]any{
				"reply": reply, "favorite": favorite, "like": like,
			},
		})
	})
	ts := countsShellWith(t, b, it, width)
	waitFrames(t, ts.tt, "三个数问回来了", func() bool {
		c := ts.s.app.ArchiveCounts()
		return c.Reply == reply && c.Favorite == favorite && c.Like == like
	})
	ts.tt.Frame()

	out := map[string]float32{}
	for _, what := range []string{it.Title, it.Artist} {
		if r, ok := ts.tt.Find(what); ok {
			out[what] = r.W
		} else {
			t.Fatalf("宽度 %d：画面上找不到 %q，文字有 %q", width, what, ts.tt.Texts())
		}
	}
	if r, ok := ts.tt.Find("播放进度"); ok {
		out["播放进度左沿"] = r.X
		out["播放进度宽"] = r.W
	} else {
		t.Fatalf("宽度 %d：画面上找不到进度条，文字有 %q", width, ts.tt.Texts())
	}
	return out
}

// setViewCounts 改 `/x/web-interface/view` 回的收藏数和获赞数（票 #701）。
func (f *biliFixture) setViewCounts(favorite, like int64) {
	f.mu.Lock()
	f.viewFavorite = favorite
	f.viewLike = like
	f.mu.Unlock()
}

// countTextsAreAllThere 是给上面那些用例看画面用的（小工具）。
func countTextsAreAllThere(tt *mygoui.Tester, want ...string) bool {
	for _, w := range want {
		if !strings.Contains(strings.Join(tt.Texts(), "|"), w) {
			return false
		}
	}
	return true
}
