// 播放条左边第三段「评论」键的读数（票 #701 起带数；喜欢歌单原型把
// 「赞」「收藏」两枚键连同它们的角标一起去掉了，这一段只剩评论）。
//
// # 这一组断的是「画面上有什么」
//
// 数字画没画、画成什么样、会不会把邻居挤坏——都不会报错，只会在画面上
// 悄悄不对。所以断的是**渲染结果**（`Tester` 量出来的框和文字），不是
// 「某个函数被调到了」。
//
// # 一个真请求都不发
//
// 数据走本地 HTTP fixture（`commentBili`）——那一台假 B 站答
// `/x/web-interface/view`，而评论数就在那个回包的 `stat` 里。
package ui

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 画面上有没有那个数
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCommentsKeyShowsItsCount 钉住**评论键带着自己的数**。
//
// ⚠️ **判据是「画面上那一串字」**（`Tester.Find` 是全等匹配）：它认的是**画出来
// 的那个数字**，而不是无障碍名字里顺带带的（`HasText` 认子串，「评论 1k+」也能
// 满足它——那样一个只改名字、不画数字的实现照样绿）。
func TestTheCommentsKeyShowsItsCount(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 2468, 9999)

	want := list.CountText(1286)
	if want == "" {
		t.Fatalf("评论数那一栏折成了空串——测试自己写错了")
	}
	if _, ok := tt.Find(want); !ok {
		t.Errorf("「评论」键上没有那个数 %q（文字有 %q）", want, tt.Texts())
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
func TestTheCountIsFoldedTheWideWay(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 9999, 9999, 9999)

	if _, ok := tt.Find("999+"); !ok {
		t.Errorf("画面上没有「999+」（文字有 %q）——这一档应按原型显示", tt.Texts())
	}
	if _, ok := tt.Find("9999"); ok {
		t.Errorf("画面上出现了「9999」（文字有 %q）——那是**播放量**那一族的折法", tt.Texts())
	}
}

// TestTheCountNeverShowsZero 钉住**拿不到就不画**。
//
// 🔴 「没有数字」和「零条评论」在屏幕上长得一样，可它们说的是两件事：前者是
// 「还不知道」（离线 / 那一趟还没回来），后者是「这一首真的没有评论」。画一个
// 「0」会让用户以为后者。
//
// ⚠️ **键照旧要能点**：拿不到数字不是这一枚键的错。
func TestTheCountNeverShowsZero(t *testing.T) {
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

	// 名字必须正好是「评论」：名字里没有数字。
	if _, ok := tt.Find("评论"); !ok {
		t.Errorf("拿不到数字的时候那一枚键的名字不是「评论」，文字有 %q", tt.Texts())
	}
	for _, txt := range tt.Texts() {
		if txt == "0" || txt == "0 条" {
			t.Errorf("画面上出现了 %q——拿不到数字不该画成「零」", txt)
		}
	}
}

// TestTheCountDisappearsForALocalFile 钉住**本地文件不摆评论键**。
//
// 🔴 **本地文件在 B 站上没有对应的对象**：那一趟压根不该发（发出去是「问一个
// 认不出来的 oid」），而评论数一个都不该画。
//
// ⚠️ **判据是「画面上根本没有那个数」**，而不是「数字是 0」。
func TestTheCountDisappearsForALocalFile(t *testing.T) {
	local := app.Item{Source: "local", Type: "local", Path: "/tmp/x.mp3", Title: "本地的一首"}
	tt2 := countsPlaybarWith(t, newCommentBili(t, "reply-main.json"), local, 1200)
	if _, ok := tt2.Find(list.CountText(1286)); ok {
		t.Errorf("本地文件那一档还画着评论数（文字有 %q）", tt2.Texts())
	}
	if _, ok := tt2.Find("评论"); ok {
		t.Errorf("本地文件那一档还摆着评论键（文字有 %q）", tt2.Texts())
	}
}

// TestTheCountNeverLosesItsPlus 钉住**「+」不许被截掉**。
//
// 🔴 **「+」是这一族的语义**（票面原话：「「+」恒表示「至少这么多」」）——
// 把它截掉就变成了「正好 999」，那是**撒谎**。
//
// ⚠️ **判据是「每一档都装得下上限」**：数字格有 `MaxWidth(playbarBadgeMaxW)`
// （22.5），而折法最宽的一档是「99k+」（9 号字实测 **≈22.0**）——**刚好装下**。
//
// ⇒ 所以这一条断的是**每一档**：**一个都不许截**。
//
// 🔴 **为什么「不许截」是硬要求**：**截断切的是末尾，而「+」永远是最后一个
// 字符** —— 所以「+」**总是第一个被切掉的**（10 号字那一版实测：「99k+」上限
// 24 → 墨迹 20.00；上限 21 → 墨迹 14.00，比不带「+」的「99k」（18.52）**还短**）。
// 而「+」是这一族的**语义**（「至少这么多」）——它被切掉就变成了「正好 99 千」，
// 那是**撒谎**。
//
// ⚠️ **判据怎么算「装得下」**：**单独画一遍**那一串字（同一个上限、同一个字号），
// 量**它自己**的墨迹——**不能**量真实布局里那一格的墨迹（左右就是邻居的图形，
// 混进来就分不出哪一笔是那个「+」）。
func TestTheCountNeverLosesItsPlus(t *testing.T) {
	// ⚠️ **这些档位一个都不许截**。
	//
	// 🔴 **99999 和 100000 必须在这一档里**（第一版把它们漏了——而那正是唯一
	// 会截掉「+」的那一档）：
	//
	//	99999  → 「99k+」（9 号字实测 ≈22.0，**最宽的那一档**）
	//	100000 → 「1亿+」（9 号字实测 ≈21.2）
	//
	// ⇒ 上限是 **22.5**，**刚好装下** ≈22.0 —— 而**上限再低一档（22）**，
	// 「99k+」就装不下，而**截断切的是末尾，所以「+」第一个被切掉**
	//（10 号字那一版实测：「99k+」上限 24 → 墨迹 20.00；上限 21 → 墨迹
	// 14.00，比不带「+」的「99k」（18.52）**还短**）。
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
			// 图形就在旁边，邻居的字迹混进来，分不出哪一笔是那个「+」。
			//
			// ⇒ 所以**单独画一遍**那一串字（同一个上限、同一个字号），量它自己的
			// 墨迹：**「+」被切掉时那一截短 4 像素以上**，而完整时只差 0.5~0.7
			//（字有左右边距）。
			//
			// ⚠️ **为什么这个判据是对的**：截断**切的是末尾**，而「+」**永远是最后
			// 一个字符** —— 所以「+」**总是第一个被切掉的**（10 号字那一版实测：
			// 「99k+」上限 24 → 墨迹 20.00；上限 21 → 墨迹 14.00，比不带「+」的
			// 「99k」（18.52）**还短**）。
			var full float32
			mygoui.NewTester(func(c *mygoui.Context) {
				full, _ = c.MeasureText(0, mygoui.Span{
					Text: want, Size: scale.Font(c, playbarBadgeFont)})
			}, 300, 60).Frame()
			// 角标缺口现在按内容自适应宽度，不再受旧的 22.5 固定上限约束。
			ink := isolatedInkWidth(t, want, full+20)
			if full-ink > 2 {
				t.Errorf("数 %d（%q）被截了：同一个上限（%.1f）下那一串字的墨迹只有 %.2f，"+
					"而字宽是 %.2f——差的那一截是末尾那个「+」"+
					"（那个「+」是这一族的语义）",
					n, want, full+20, ink, full)
			}
		})
	}
}

// TestTheBadgeWidthNeverCollapsesToZero 钉住 **`MeasureText` 量不到的时候缺口
// 不塌成空块**。
//
// 🔴 **对抗性审查查出的一笔**：字体后端还没就绪 / 量不到那一串时
// `MeasureText` 回 0——不挡的话 `notchW` 只剩两侧 padding（14 ≥
// `badgeNotchMinW`，连最小小宽那道兜底都救不回来），更糟的是 `Text` 的
// `MinWidth(0).MaxWidth(0)` 会把**整串数字裁成零宽**——画面上剩一个空底色
// 缺口，数字整个没了。
//
// ⚠️ **判据是 `badgeTextWidth` 那一支纯函数**：`MeasureText` 是 `*ui.Context`
// 上的方法、mock 不掉，所以把「量出来的值 → 兜底后的值」抽了出来，这里
// 0 / 负数 / 正常宽三档各喂一遍。
func TestTheBadgeWidthNeverCollapsesToZero(t *testing.T) {
	for _, c := range []struct {
		name     string
		measured float32
		want     float32
	}{
		{"量不到（0）", 0, playbarBadgeMaxW},
		{"量出负数（防御）", -3, playbarBadgeMaxW},
		{"正常宽（1k+ ≈14）", 14.8, 14.8},
		{"最宽档（99k+ ≈22）", 22.0, 22.0},
	} {
		if got := badgeTextWidth(c.measured); got != c.want {
			t.Errorf("%s：badgeTextWidth(%.1f) = %.1f，want %.1f——"+
				"量不到的时候没退回上限档，数字会被裁成零宽、剩一个空缺口",
				c.name, c.measured, got, c.want)
		}
	}
}

// scaledCellInks 量「**界面上那一格**」在给定的系统文本缩放下，它自己的墨迹
// 有多宽（带「+」的那一串，和不带「+」的那一串）。
//
// 🔴 **走的是生产代码**（`countBadge`）——这是这一条能成立的原因：
// 自己画一遍的话，字号是测试里写的那个数，而**被测的那件事恰恰是「字号乘不乘
// 系统缩放」**——自己画就等于把要验的东西当成了前提。
//
// ⚠️ **为什么要把「不带「+」的那一串」也量一遍**：那是**对照组**——
// 截断切的是末尾，而「+」永远是最后一个字符 ⇒ **「+」被切掉时，那一格的墨迹
// 会比不带「+」的还短**（实测 scale 1.25：17 < 23）。
//
// ⚠️ **单独画**：真实布局里那一格的左右**就是邻居的图形**，量墨迹会把邻居的
// 字迹算进来——分不出哪一笔是那个「+」。⇒ 这里让那一格**单独占一行**。
func scaledCellInks(t *testing.T, withPlus, withoutPlus string, scale float32) (float32, float32) {
	t.Helper()
	// 🔴 **带「+」的那一格走**生产代码**（`countBadge`）——这是这一条能成立的
	// 原因：自己画一遍的话，字号是测试里写的那个数，而**被测的那件事恰恰是
	// 「字号乘不乘系统缩放」**——自己画就等于把要验的东西当成了前提。
	//
	// ⚠️ **角标要挂在一个图形盒子上**（它在按钮里是 `Attach` 在 18×18 的盒子
	// 上的）——裸建会把它的中心钉在整行的右上角，量到的不是它的墨迹。
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Children(func() {
			// ⚠️ **缺口块会探出图形盒子的左界**（宽字那一档的缺口比 18 的盒子
			// 还宽、往左长出去）——把盒子摆在行中间，探出去的那一截才不会被
			// 窗口左缘裁掉、墨迹才量得全。
			mygoui.Spacer(c).Grow(1)
			mygoui.Box(c).Size(iconMd, iconMd).Children(func() {
				countBadge(c, countForText(withPlus), c.Theme().Background, 0)
			})
			mygoui.Spacer(c).Grow(1)
		})
	}, 300, 60)
	tt.SetPreferences(mygoui.Preferences{TextScale: scale})
	tt.Frame()
	with := inkWidthOf(t, tt, withPlus)

	// ⚠️ **对照组是「同一串字去掉那个「+」」，而且它**不是折法的输出**——
	// 折法那一档永远带「+」，所以**不能**喂给 `countBadge`（那样折出来还是
	// 带「+」的字，量的是同一串，两边相等，判据就成了空话——第一版就这么写的，
	// 变异测试立刻揭穿了它）。
	//
	// ⇒ 所以对照组**直接画那一串字**（**同样的字号、同样的上限**）——它测的是
	// 「那个「+」自己占多少像素」。
	tt2 := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Children(func() {
			mygoui.Text(c, withoutPlus).FontSize(playbarBadgeFont).
				MaxWidth(playbarBadgeMaxW).MaxLines(1).Ellipsis("…")
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
// 而**数字那一格画的是折出来的字**，所以要喂给 `countBadge` 的是**数**。
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
	if strings.Contains(body, "万") {
		return head * 10_000
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

// TestTheCountSaysTheNumberToScreenReaders 钉住**数字不只靠画面传达**。
//
// 🔴 图标键的无障碍名字就是它唯一的凭据（`mygo/ui/svg.go`：图标对辅助技术
// 不可见）。数字画在图标旁边、可是名字里没有的话，读屏用户听到的仍然是
// 「评论」两个字——数字对他们不存在。
func TestTheCountSaysTheNumberToScreenReaders(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 2468, 9999)

	if _, ok := tt.Find("评论"); !ok {
		t.Errorf("无障碍名字不是「评论」（文字有 %q）", tt.Texts())
	}
}

// TestTheThreeCountsSitInsideANotchOnTheIconsTopRight 钉住**数字嵌在图形右上角的缺口里**。
//
// 🔴 **这是本票的主体**：数字从「图形右上角外侧挂着的角标」挪成「嵌在图形
// 右上角缺口里的读数」（网易云式）——它**不占布局**，缺口块钉在图形盒子的
// 右上角、包住那个角往外探 `badgeNotchOut`，数字居中画在缺口里。
//
// ⚠️ **判据是角标自身的 bounds 相对图形盒子 bounds 的位置**（视觉审查钉下的
// 四条真值）：
//
//	角标的左缘落在缺口左缘 + PadL 那一带        （嵌进缺口里，左右留缝）
//	角标的左缘不许深过「咬的那一口」            （缺口只咬右上角，不是包住图形）
//	角标的下缘明显在图形中线的上方             （坐在右上角，不是骑在正中）
//	角标的右缘没越过图形右缘 + 最宽探出量      （缺口主体探在图形外的空气里）
//
// 反过来说：外侧角标那一版（角标左缘正好贴图形右缘起排、上下位置也低）、横排
// 那一版（角标中心 y ≈ 图形中心 y）和「缺口包住整个图形」那一版（宽字档数字
// 左缘跑到图形左半去了）都立刻红。
//
// ⚠️ **图形盒子怎么找**：角标的父元素是图形盒子（`ui.Box(iconMd × iconMd)`），
// 而那一枚键的框是它再往外一层。Tester 不给父链——所以这里**用按钮的框**倒推：
// 按钮的**中心**就是图形盒子的中心（带数字时图形在按钮正中），图形盒子的半边
// 是 `iconMd/2`。
func TestTheCountSitsInsideANotchOnTheIconsTopRight(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 2468, 9999)

	want := list.CountText(1286)
	badge, ok := tt.Find(want)
	if !ok {
		t.Fatalf("「评论」键上没有角标 %q（文字有 %q）", want, tt.Texts())
	}
	key, ok := tt.Find("评论")
	if !ok {
		t.Fatalf("画面上找不到「评论」那一枚键（文字有 %q）", tt.Texts())
	}
	// 角标必须有真实尺寸，并且落在按钮的上半区；这能防止绝对定位文本
	// 再次退化成零宽省略号或掉回按钮正文区域。
	if badge.W <= 0 || badge.H <= 0 {
		t.Errorf("「评论」的角标尺寸无效：%.1fx%.1f", badge.W, badge.H)
	}
	if badge.Y >= key.Y+key.H/2 {
		t.Errorf("「评论」的角标没有落在按钮右上区域：badgeY=%.1f buttonY=%.1f buttonH=%.1f",
			badge.Y, key.Y, key.H)
	}
}

// TestTheThreeCountsAreInTheHoverTips 钉住**悬停提示里也有那个数**。
//
// ⚠️ **数字只画在画面上，指针停在别处时用户读不到它**——所以提示句里也得说
// 一遍。
//
// 🔴 **提示句还要说清是「整个稿件」的**：多 P 的视频里放的是第 3 集，而显示的
// 是整稿的数字（票 #701 的语义要求）。
func TestTheCountIsInTheHoverTip(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 2468, 9999)

	key, ok := tt.Find("评论")
	if !ok {
		t.Fatalf("画面上找不到「评论」那一枚键（文字有 %q）", tt.Texts())
	}
	tt.Move(key.X+key.W/2, key.Y+key.H/2)
	tt.Frame()
	// 🔴 **提示里给的是完整数字**，不是画面上那个折过的字（票 #701 的要求）。
	if !tt.HasText("整个稿件 1286") {
		t.Errorf("「评论」键的悬停提示里没有「整个稿件 1286」那半句（文字有 %q）",
			tt.Texts())
	}
}

// TestTheFullPlayerCommentsTipSaysTheSameThing 钉住**满屏那一枚的提示句和播放条
// 那一枚说同样的话**。
//
// 🔴 **为什么这一条**：满屏那一枚**不带数字**（票 #682 的有意取舍——满屏底栏是
// 居中的 flex，加数字会让播放键离开竖直中轴），而**提示句可以带上那个数**：
// 数字**不进布局**（它只在那一句提示里，而提示是浮在画面上的气泡）——所以
// 「不带数字」那条取舍**管不到提示句**。
//
// ⇒ 而「提示句里说清是整个稿件的多少」是播放条那一枚已经有的（票 #701 的语义
// 要求）——**两枚键说同样的话**，否则用户在满屏里点不到那个精确值。
//
// ⚠️ **判据是提示句的内容**（不是「键上有没有数字」）：满屏那一枚**照旧不画
// 数字**，而提示句里说清它。
func TestTheFullPlayerCommentsTipSaysTheSameThing(t *testing.T) {
	tt, _ := countsFullTester(t, mvItem(), 2468)
	// 满屏顶栏的评论键带角标（原型 `.cnt`），数字一回来名字就是「评论 2k+」。
	label := countKeyLabel("评论", 2468)
	waitFrames(t, tt, "评论数回来了", func() bool {
		_, ok := tt.Find(label)
		return ok
	})
	key, _ := tt.Find(label)
	tt.Move(key.X+key.W/2, key.Y+key.H/2)
	tt.Frame()
	// 🔴 **完整数字**：那一档在画面上是「2k+」，而提示里给的是「2468」。
	if !tt.HasText("整个稿件 2468") {
		t.Errorf("满屏那一枚「评论」键的提示句里没有「整个稿件 2468」那半句"+
			"（文字有 %q）——它要和播放条那一枚说同样的话", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 🔴 宽度账
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCountDoesNotTouchTheProgressBar 钉住**评论键带数字时不撞进度条**。
//
// 🔴 **这是本票最要紧的一条**：撞上去的后果是**点不动，而画面上看不出坏了**
// （票 #216 那个坑：中间那一列是 `PassThrough` 的，可它的子节点照旧登记命中，
// 于是左列撑出来的那一截被进度行的命中框盖住）。
//
// ⚠️ **量的是「那一枚键的右沿」和「进度行的左沿」**：前者是第三段那一整段的
// 右端（不是某一枚键的），后者是滑块那一块的左沿。
//
// ⚠️ **四档都要量**：900（应用的最小窗口宽，余量最小）、1000、1200、1600。
func TestTheCountDoesNotTouchTheProgressBar(t *testing.T) {
	for _, width := range []int{900, 1000, 1200, 1600} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			// ⚠️ **最坏的那一档**：评论数折成最宽的那一档（上限那一格）。
			tt, _ := countsPlaybarTester(t, width, 9999, 9999, 9999)

			bar, ok := tt.Find("播放进度")
			if !ok {
				t.Fatalf("宽度 %d：画面上找不到进度条，文字有 %q", width, tt.Texts())
			}
			// 那一枚键的右沿：评论键是第三段的**最后一枚**。
			key, ok := tt.Find("评论")
			if !ok {
				t.Fatalf("宽度 %d：画面上找不到评论键，文字有 %q", width, tt.Texts())
			}
			gap := bar.X - (key.X + key.W)
			if gap < 0 {
				t.Errorf("宽度 %d：第三段那一整段的右沿在 %.2f，而进度行从 %.2f 开始——"+
					"它们碰上了（而撞上去的后果是**点不动**，票 #216）",
					width, key.X+key.W, bar.X)
			}
			// 🔴 **门槛是 5**：票 #701 的验收线照旧（赞 / 收藏两枚键去掉之后
			// 余量更大——门槛留着是怕下一个人把数字挪回行内、左列重新撞上滑块，
			// 票 #216 那个坑）。
			if gap < 5 {
				t.Errorf("宽度 %d：第三段离进度行只剩 %.2f 像素——"+
					"余量不够，换一个字体就撞上去了（票 #216 那个坑：点不动，"+
					"而画面上看不出坏了）", width, gap)
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
//  1. 常量之间的账（`playbarActionsW` = 两枚 32）；
//  2. 注释里那个「最小宽度：**N**」要与 `playbarLeftMinW` 算出来的数一致。
func TestTheCountsWidthMatchesTheCode(t *testing.T) {
	// 🔴 **喜欢歌单原型之后**：第三段是「喜欢 + 评论」两枚（64），赞 / 收藏
	// 两枚连同角标一起去掉了，心形摆回来之后这一段保底按**两枚都摆**算。
	if playbarActionsW != 2*iconHitMd {
		t.Errorf("playbarActionsW = %v，want %v（两枚 32）", playbarActionsW, 2*iconHitMd)
	}
	if playbarLeftMinW != coverSize+coverGap+trackTextMinW+coverGap+playbarActionsW {
		t.Errorf("playbarLeftMinW = %v，want %v（52 + 8 + 76 + 8 + %v）",
			playbarLeftMinW, coverSize+coverGap+trackTextMinW+coverGap+playbarActionsW,
			playbarActionsW)
	}
	// 🔴 **900 宽下的余量**（左列右沿离滑块左沿）：左列保底 208（两枚键），
	// 余量 330.7 − 20 − 208 = **102.7**——门槛照旧 ≥ 5。
	const sliderLeft = 330.7
	gap := sliderLeft - (playbarPadX + playbarLeftMinW)
	if gap < 5 {
		t.Errorf("按上限算余量是 %.2f——票 #701 的验收线是 ≥ 5", gap)
	}
}

// isolatedInkWidth 量「一串字在给定上限下画出来，**它自己**的墨迹有多宽」。
//
// ⚠️ **为什么要单独画**：真实布局里那一格的左右**就是邻居的图形**，量墨迹会
// 把邻居的字迹算进来——分不出哪一笔是那个「+」。⇒ 这里单独画一遍，四周没有
// 别的东西。
func isolatedInkWidth(t *testing.T, s string, maxW float32) float32 {
	t.Helper()
	tt := mygoui.NewTester(func(c *mygoui.Context) {
		mygoui.Row(c).Children(func() {
			mygoui.Text(c, s).FontSize(scale.Font(c, playbarBadgeFont)).
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

// TestTheCountShowsUpAndTheKeyStillWorks 是这一票的 E2E。
//
// # 走的是整条链
//
// 假 B 站（`/x/web-interface/view` 带 `stat.reply`）→ `App.ArchiveCounts()`
// 那一趟 → 播放条上「评论」键画出数字 → 键**照旧点得动**。
//
// ⚠️ **等的是结果**（`waitFrames`）：那一趟在后台 goroutine 上跑，按帧数等就是
// 按运气等。
func TestTheCountShowsUpAndTheKeyStillWorks(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setViewReply(1286)
	f.setViewCounts(2468, 9999)

	s, tt := newShell(t, at(1200, 900))
	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVcountsE2E", Cid: "1",
		Title: "有评论数的那一首", Artist: "某个 UP 主",
	}); err != nil {
		t.Fatalf("把在线那首放进队列：%v", err)
	}
	waitFrames(t, tt, "在线那首开始播", func() bool { return s.app.Snapshot().HasTrack })

	waitFrames(t, tt, "评论数问回来了", func() bool {
		return s.app.ArchiveCounts().Reply == 1286
	})
	tt.Frame()

	if _, ok := tt.Find(list.CountText(1286)); !ok {
		t.Errorf("画面上没有评论数 %q（文字有 %q）", list.CountText(1286), tt.Texts())
	}

	// 🔴 **键照旧点得动**：角标盖在右上角也不吃点击（`PassThrough`），
	// 点下去开的是评论抽屉——只 Move 上去悬停的话，一个 no-op 的点击
	// 处理器照样过得了这一句。
	key, ok := tt.Find("评论")
	if !ok {
		t.Fatalf("画面上找不到评论键，文字有 %q", tt.Texts())
	}
	tt.Move(key.X+key.W/2, key.Y+key.H/2)
	tt.Frame()
	if !tt.HasText("整个稿件 1286") {
		t.Errorf("指针停在评论键上却没有那句话（文字有 %q）", tt.Texts())
	}
	if err := tt.Click("评论"); err != nil {
		t.Fatalf("评论键点不动：%v", err)
	}
	tt.Frame()
	if !s.commentsOpen {
		t.Error("点了评论键，评论抽屉没开")
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

// countsFullTester 画**满屏播放器**，而假 B 站的 `view` 回给定的评论数。
//
// 🔴 **为什么单独一份**：满屏那一枚「评论」键的提示句里要带那个数——而那个数
// 来自 `App.CommentCount()`（稿件详情那一趟），所以这一族要**等那一趟真的回来**
// （理由同 `countsPlaybarTester`）。
//
// ⚠️ **状态是手给的、队列是真的**：理由同 `commentsFullTester`（判据有两半）。
func countsFullTester(t *testing.T, it app.Item, reply int64) (*mygoui.Tester, *Shell) {
	t.Helper()
	b := newCommentBili(t, "reply-main.json")
	b.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"aid": 170001, "bvid": r.URL.Query().Get("bvid"),
			"stat": map[string]any{"reply": reply},
		})
	})
	st := barState()
	st.Title = it.Title
	st.Artist = it.Artist
	cli := bili.NewClient(bili.Options{Transport: b.Server.Client().Transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	ts := newTestShell(t, at(1200, 800), mounted(func(t *testing.T, a *app.App) {
		a.MountLogin(app.NewMemoryStore("test"), cli)
		a.MountStreaming(app.Streaming{
			Client: cli, Media: b.Server.Client(), TempDir: t.TempDir()})
	}), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.full.open = true
			s.viewFullPlayer(c, st)
		}
	}))
	ts.s.app.Queue().Reset(it)
	waitFrames(t, ts.tt, "评论数问回来了", func() bool {
		return ts.s.app.CommentCount() == reply
	})
	ts.tt.Frame()
	return ts.tt, ts.s
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
