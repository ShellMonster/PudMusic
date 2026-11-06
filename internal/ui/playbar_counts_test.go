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
// 角标不占布局之后，第三段回到四枚 32 + 三道 3 的缝、左列保底 281——900 宽
// （应用的最小窗口宽）下，左边那一列的右端离进度条滑块左沿是 **33.7** 像素
// （横排那一版只有 5.20）。而**撞上去的后果是「点不动，而画面上看不出坏了」**
// （票 #216 那个坑：中间那一列是 `PassThrough` 的，可它的子节点照旧登记命中，
// 于是左列撑出来的那一截被进度行的命中框盖住）。
//
// ⚠️ **33.7 是「视觉间距」，不是碰撞缓冲**（理由见 `playbar_counts.go` 的文件
// 头《两个口径》）：角标 `Attach` 不占布局、左列的宽是**常数** 281——**碰撞
// 不可能发生**。那 33.7 是**留给人眼**的。
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
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
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

	if _, ok := tt.Find("999+"); !ok {
		t.Errorf("画面上没有「999+」（文字有 %q）——这一档应按原型显示", tt.Texts())
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
// ⚠️ **判据是「每一档单独画出来，墨迹都不许比字宽窄」**：数字一整行随字长、
// 不封顶，而**截断切的是末尾、而「+」永远是最后一个字符**——任何一档
// 墨迹短一截，缺的就是那个「+」。
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
					Text: want, Size: playbarCountFont})
			}, 300, 60).Frame()
			// 数字一整行随字长、不封顶，单独画一遍同一串字（同样的字号、
			// 一个比字宽还宽的上限）量它自己的墨迹——「+」还在不在。
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
		want := c.name
		if _, ok := tt.Find(want); !ok {
			t.Errorf("无障碍名字不是 %q（文字有 %q）", want, tt.Texts())
		}
	}
}

// TestTheThreeCountsSitUnderTheIconInsideTheKey 钉住**数字落在图标正下方、整串收在键内**。
//
// 🔴 **这是本票的主体**（方案 A「垂直收束」）：数字不再悬浮在图标右上角的
// 缺口角标里——它挪到图标**正下方**，和图形**上下竖着收在同一枚键里**；
// 键加高（36 → 50），图形在上、数字在下，水平方向都和键**同一条竖直中轴**。
//
// ⚠️ **判据是数字那一格的 bounds 相对按钮 bounds 的位置**：
//
//	数字整串不许探出键的左右 / 上下边界      （收在键内，不再漂在图形外）
//	数字的上缘必须低于键的上半区             （在图标**下方**，不是右上角）
//	数字的水平中点与键的中点重合             （和图形同一根中轴，不是挂在角上）
//	数字不许压在图标那一带上                 （图标在键的上半区，中点 y < 键中点）
//
// 反过来说：缺口角标那一版（数字上缘越过键的上沿、探出图形右缘）、横排那一版
// （数字和图标同一条水平基线）都立刻红。
//
// ⚠️ **三枚都要断**：只断一枚的话，「收藏 / 评论收束了、赞还停在缺口角标」那
// 一档照样绿——而赞键的图形那一块是绝对定位的两层，和另外两枚不是同一条路径。
//
// ⚠️ **三枚键的数字折成同一串**（折法只有 4 档，「999+」那一档三枚全是）——
// `Find` 只回第一枚，所以这里用**墨迹带**（`inkBandsOf`）把三处数字各自
// 翻出来：一条带 ≈ 一处那串字，位置就是它挂在哪一枚键。
func TestTheThreeCountsSitUnderTheIconInsideTheKey(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 9999, 9999, 9999)

	want := list.CountText(9999)
	cell, ok := tt.Find(want)
	if !ok {
		t.Fatalf("画面上没有数字 %q（文字有 %q）", want, tt.Texts())
	}
	bands := inkBandsOf(t, tt, want)
	if len(bands) < 3 {
		t.Fatalf("画面上 %q 的墨迹带只有 %d 条（%v）——三枚键该各有一条",
			want, len(bands), bands)
	}

	// 数字那一行的高度带（拿第一枚的框当样板）：三枚键的数字同一行高。
	countTop, countBottom := cell.Y, cell.Y+cell.H

	for _, name := range []string{"赞", "收藏", "评论"} {
		key, ok := tt.Find(name)
		if !ok {
			t.Fatalf("画面上找不到「%s」那一枚键（文字有 %q）", name, tt.Texts())
		}
		// 这一枚键里的数字带：水平落在键的左右边界之间的那一条。
		var band *[2]float32
		for i := range bands {
			mid := (bands[i][0] + bands[i][1]) / 2
			if mid >= key.X && mid <= key.X+key.W {
				band = &bands[i]
			}
		}
		if band == nil {
			t.Errorf("「%s」键的框 [%.1f, %.1f] 里没有 %q 的墨迹带（全部带 %v）——"+
				"数字没挂在键内",
				name, key.X, key.X+key.W, want, bands)
			continue
		}
		// 🔴 **整串收在键内**：数字带的左右不许探出键的左右边界。
		if band[0] < key.X-0.5 || band[1] > key.X+key.W+0.5 {
			t.Errorf("「%s」的数字探出了键的左右边界：带=[%.1f, %.1f] key=[%.1f, %.1f]——"+
				"方案 A 要求数字整串收在键内，不再漂在图形外",
				name, band[0], band[1], key.X, key.X+key.W)
		}
		// 🔴 **数字在图标**下方**：它那一行必须整行低于键的中点，且不收出键的
		// 下沿。
		if countTop < key.Y+key.H/2 {
			t.Errorf("「%s」的数字不在键的下半区：countTop=%.1f keyY=%.1f keyH=%.1f——"+
				"方案 A 要求数字落在图标**正下方**，而不是悬浮在右上角",
				name, countTop, key.Y, key.H)
		}
		if countBottom > key.Y+key.H+0.5 {
			t.Errorf("「%s」的数字探出了键的下沿：countBottom=%.1f keyBottom=%.1f",
				name, countBottom, key.Y+key.H)
		}
		// 🔴 **同一根竖直中轴**：数字带的水平中点要和键的水平中点重合（±1 的
		// 抗锯齿与字宽取整余量）——偏了就是「挂在角上」，不是「上下收束」。
		if d := (band[0]+band[1])/2 - (key.X + key.W/2); d < -1.5 || d > 1.5 {
			t.Errorf("「%s」的数字不在键的中轴上：带中点=%.1f keyCenterX=%.1f——"+
				"方案 A 要求数字和图形共享同一根竖直中轴",
				name, (band[0]+band[1])/2, key.X+key.W/2)
		}
	}
}

// TestTheCountedKeysGrowTallerNotWider 钉住**带数字的键加高、不加宽**。
//
// 🔴 **方案 A 的形状**：数字收进键内之后，那一枚键从 36 × 36 变成**加高**的一枚
// （图形在上、数字在下），而**宽不变**——三枚键保持同高、同一根竖直中轴。
// 宽一长，第三段就把歌名 / 进度条挤回去（那是横排那一版的病）。
//
// ⚠️ **判据是「带数的键比不带数的高、而宽一样」**：对照是右列那枚「播放列表」
// 键——它和左段三枚同一份点击区（`tok.IconHit` = 36），而它不带数字。
func TestTheCountedKeysGrowTallerNotWider(t *testing.T) {
	tt, _ := countsPlaybarTester(t, 1200, 1286, 2468, 9999)

	plain, ok := tt.Find("播放列表")
	if !ok {
		t.Fatalf("画面上找不到不带数字的对照键「播放列表」（文字有 %q）", tt.Texts())
	}
	for _, name := range []string{"赞", "收藏", "评论"} {
		key, ok := tt.Find(name)
		if !ok {
			t.Fatalf("画面上找不到「%s」那一枚键（文字有 %q）", name, tt.Texts())
		}
		if key.H <= plain.H {
			t.Errorf("「%s」带数字却没有加高：keyH=%.1f plainH=%.1f——"+
				"方案 A 要求数字收进键内、键上下加高收束", name, key.H, plain.H)
		}
		if key.W > plain.W {
			t.Errorf("「%s」带数字把键撑宽了：keyW=%.1f plainW=%.1f——"+
				"数字只许占键内的高度，不许吃宽度", name, key.W, plain.W)
		}
	}
}

// TestTheThreeCountedKeysStillDoTheirJob 钉住**三枚键点下去照旧干原来的活**。
//
// 🔴 **键加高、内容变两层之后，点击区不许碎**：点那一枚键（含它下半区的数字
// 那一带）必须照旧触发原来的动作——赞发 SetLike、收藏开收藏夹弹窗、评论开
// 评论抽屉。
//
// ⚠️ **点的是键的**下半区**：方案 A 把数字摆在那——如果数字那一格自己登记了
// 命中（忘了 PassThrough），点在下半区就落不到按钮上，动作不发。那是
// 「画面上看不出坏了」的那种坏，必须真点。
func TestTheThreeCountedKeysStillDoTheirJob(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setViewReply(1286)
	f.setViewCounts(2468, 9999)

	s, tt := newShell(t, at(1200, 900))
	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	// ⚠️ **挂上登录和收藏夹**：点「收藏」那一枚要真的把收藏夹弹窗开出来，
	// 而不是只验证兜底那句 toast（`openFolderSelect` 没挂时说什么都打不开）。
	s.app.MountLogin(app.NewMemoryStore("test"), f.client())
	s.app.MountCollections(app.CollectionRefs{Client: f.client()})
	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVcountsA", Cid: "1",
		Title: "有三个数的那一首", Artist: "某个 UP 主",
	}); err != nil {
		t.Fatalf("把在线那首放进队列：%v", err)
	}
	waitFrames(t, tt, "在线那首开始播", func() bool { return s.app.Snapshot().HasTrack })
	waitFrames(t, tt, "三个数问回来了", func() bool {
		c := s.app.ArchiveCounts()
		return c.Reply == 1286 && c.Favorite == 2468 && c.Like == 9999
	})
	tt.Frame()

	// 🔴 **点键的下半区**（数字那一带）：三枚都点，三枚的动作都要发。
	//
	// ⚠️ **评论**：点完 `commentsOpen` 要 true（评论抽屉开）。
	// ⚠️ **收藏**：点完收藏夹弹窗要开（`folderSelect.open`）——bvid → avid 是
	// 后台一趟（fixture 的 `/x/web-interface/view` 回 `aid`），等的是结果。
	// ⚠️ **赞**：点完 `LikeState().Liked` 要翻成 true（`SetLike` 那一趟发出去了）。
	clickLowerHalf := func(name string) {
		t.Helper()
		key, ok := tt.Find(name)
		if !ok {
			t.Fatalf("画面上找不到「%s」那一枚键（文字有 %q）", name, tt.Texts())
		}
		// 点下半区正中——数字那一带。
		tt.ClickAt(key.X+key.W/2, key.Y+key.H*3/4)
		tt.Frame()
	}

	clickLowerHalf("评论")
	if !s.commentsOpen {
		t.Error("点了评论键的下半区，评论抽屉没开——数字那一带把点击吃了")
	}
	// ⚠️ **抽屉是模态的**：开着的时候底下那一页点不动（`Modal()` 蒙版）——
	// 点下一枚之前必须真的把它收掉（按 Esc），不是只把标志位翻回去。
	tt.Key(0, mygoui.KeyEscape)
	settleCommentsDrawer(t, s, tt)
	if s.commentsOpen {
		t.Fatal("按了 Esc 评论抽屉还开着")
	}

	clickLowerHalf("收藏")
	waitFrames(t, tt, "收藏夹弹窗开出来了", func() bool {
		return s.folderSelect != nil && s.folderSelect.open
	})
	if s.folderSelect == nil || !s.folderSelect.open {
		t.Error("点了收藏键的下半区，收藏夹弹窗没开——数字那一带把点击吃了")
	}
	// ⚠️ **弹窗也是模态的**：收掉再点下一枚（同上面抽屉那一条）——
	// 「取消」那一枚是它自己的关法（`closeFolderSelect`）。
	if err := tt.Click("取消"); err != nil {
		t.Fatalf("点不动收藏夹弹窗的「取消」（文字有 %q）", tt.Texts())
	}
	tt.Frame()
	if s.folderSelect != nil {
		t.Fatal("点了「取消」，收藏夹弹窗没收掉")
	}

	clickLowerHalf("赞")
	waitFrames(t, tt, "赞发出去了", func() bool {
		return s.app.LikeState().Liked
	})
	if !s.app.LikeState().Liked {
		t.Error("点了赞键的下半区，赞没发出去——数字那一带把点击吃了")
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
		key, ok := tt.Find(c.name)
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
			// ⚠️ **余量是「按角标不占布局」的那一档**：第三段回到四枚 32 +
			// 三道 3 的缝，左列保底 281——所以**实测的余量是 33.7**（横排那一版
			// 是 5.20，那时数字占一格、第三段 165.5）。
			//
			// 🔴 **门槛是 5**：票 #701 的验收线照旧（虽然这一票余量已经大得多——
			// 门槛留着是怕下一个人把数字挪回行内、左列重新撞上滑块，票 #216
			// 那个坑）。
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
//  1. 常量之间的账（`playbarActionsW` = 四枚 32 + 三道键间缝 3×3 = 137）；
//  2. 注释里那个「最小宽度：**N**」要与 `playbarLeftMinW` 算出来的数一致。
func TestTheThreeCountsWidthMatchesTheCode(t *testing.T) {
	// 🔴 **本票（角标）之后**：三枚键的数字**不占布局**，每一枚回到 32；
	// 键与键之间留 3 的缝（角标探出之后叠在缝上，没缝三枚带数的键糊成一串）。
	if playbarActionsW != 4*iconHitMd+3*playbarActionsGap {
		t.Errorf("playbarActionsW = %v，want %v（四枚 32 + 三道键间缝 3 × 3）",
			playbarActionsW, 4*iconHitMd+3*playbarActionsGap)
	}
	if playbarLeftMinW != coverSize+coverGap+trackTextMinW+coverGap+playbarActionsW {
		t.Errorf("playbarLeftMinW = %v，want %v（52 + 8 + 76 + 8 + %v）",
			playbarLeftMinW, coverSize+coverGap+trackTextMinW+coverGap+playbarActionsW,
			playbarActionsW)
	}
	// 🔴 **900 宽下的余量**（左列右沿离滑块左沿）。
	//
	//	左列右沿 = 16 + 281 = 297
	//	滑块左沿 = 330.7
	//	余量 = 33.7
	//
	// ⚠️ **角标不占布局**（`Attach`）：第三段回到四枚 32 + 三道 3 的缝，
	// 左列保底 281——横排那一版的 165.5 缩回 137，**28.5 净让给了进度条**。
	//
	// ⇒ 左列右沿 **297.0** 离滑块左沿 **330.7** 还有 **33.7**。
	// ⇒ 而**实测**（`TestTheThreeCountsDoNotTouchTheProgressBar` 量的是真实
	// 布局）也是 **33.7**。
	const sliderLeft = 330.7
	gap := sliderLeft - (playbarPadX + playbarLeftMinW)
	if gap < 0 {
		t.Errorf("按上限算，左列右沿离滑块左沿还有 %.2f——已经撞上了", -gap)
	}
	// 🔴 **余量必须 ≥ 5**：票 #701 的验收线照旧（虽然这一票余量已经大得多——
	// 门槛留着是怕下一个人把数字挪回行内）。
	if gap < 5 {
		t.Errorf("按上限算余量是 %.2f——票 #701 的验收线是 ≥ 5", gap)
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
	// ⚠️ **同一串字画了三遍，而 `Find` 只回第一枚**——「三枚键各自挂着
	// 各自的数」靠位置断：量画面上那串字的**所有**墨迹带（`inkBandsOf`），
	// 三枚键各要有一条落在自己左右边界之间。少一条就是「数字没挂在键里」，
	// 或者「三枚键共用了同一处数字」。
	bands := inkBandsOf(t, tt, want)
	if len(bands) < 3 {
		t.Fatalf("画面上 %q 的墨迹带只有 %d 条（%v）——三枚键该各有一条",
			want, len(bands), bands)
	}
	var prev float32 = -1
	for _, name := range []string{"赞", "收藏", "评论"} {
		key, ok := tt.Find(name)
		if !ok {
			t.Fatalf("画面上找不到 %q 那一枚键（文字有 %q）", name, tt.Texts())
		}
		if key.X < prev {
			t.Errorf("「%s」键在 %.1f，而前一枚的右沿在 %.1f——次序反了",
				name, key.X, prev)
		}
		prev = key.X + key.W
		inside := false
		for _, b := range bands {
			if b[0] < key.X+key.W && b[1] > key.X {
				inside = true
			}
		}
		if !inside {
			t.Errorf("「%s」键的框 [%.1f, %.1f] 里没有 %q 的墨迹带——"+
				"数字没挂在对应的键里（墨迹带有 %v）", name, key.X, key.X+key.W, want, bands)
		}
	}
}

// inkBandsOf 把画面上「那一串字」的墨迹**按水平带**翻出来：回若干段
// [左沿, 右沿]——同一段字里相邻字之间的缝 < 4 像素，而不同键之间隔着
// 图形和缝，远大于它——所以**一条带 ≈ 一处那串字**。
//
// ⚠️ **只扫字身高度那一段**（拿第一个匹配的框当样板），不扫整窗：别的文字
// （歌名、时间）的墨迹会混进来。而「赞 / 收藏 / 评论」三枚键上的数字**同一行
// 高**，扫那一带就够——竖着相邻的墨迹（图标笔画）也在这个高度带下面。
func inkBandsOf(t *testing.T, tt *mygoui.Tester, s string) [][2]float32 {
	t.Helper()
	cell, ok := tt.Find(s)
	if !ok {
		t.Fatalf("画面上找不到 %q（文字有 %q）", s, tt.Texts())
	}
	img := tt.Image()
	// ⚠️ **底色取播放条之外的空气**（窗口右上角）：那一带没有内容。数字是
	// 次要色，和任何一档底色都比 40 大。
	base := img.RGBAAt(img.Rect.Max.X-5, 5)
	var bands [][2]float32
	y0, y1 := int(cell.Y), int(cell.Y+cell.H)
	open, start := false, -1
	for x := 0; x < img.Rect.Max.X; x++ {
		ink := false
		for y := y0; y < y1; y++ {
			p := img.RGBAAt(x, y)
			if absInt(int(p.R)-int(base.R))+absInt(int(p.G)-int(base.G))+
				absInt(int(p.B)-int(base.B)) > 40 {
				ink = true
				break
			}
		}
		switch {
		case ink && !open:
			open, start = true, x
		case !ink && open:
			// 字内缝 < 4：留一点「带还没断」的余量再合。
			if x-start >= 1 {
				bands = append(bands, [2]float32{float32(start), float32(x)})
			}
			open = false
		}
	}
	if open {
		bands = append(bands, [2]float32{float32(start), float32(img.Rect.Max.X)})
	}
	// ⚠️ **合并隔缝 < 4 的带**：一个字的两笔之间、相邻两个字之间都是 < 4
	// 的缝，而键与键隔着一个图形——合在一起才是「一处那串字」。
	var merged [][2]float32
	for _, b := range bands {
		if len(merged) > 0 && b[0]-merged[len(merged)-1][1] < 4 {
			merged[len(merged)-1][1] = b[1]
		} else {
			merged = append(merged, b)
		}
	}
	return merged
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
			mygoui.Text(c, s).FontSize(playbarCountFont).
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
	like, ok := tt.Find("赞")
	if !ok {
		t.Fatalf("画面上找不到赞键，文字有 %q", tt.Texts())
	}
	tt.Move(like.X+like.W/2, like.Y+like.H/2)
	tt.Frame()
	if !tt.HasText("长按一键三连") {
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
