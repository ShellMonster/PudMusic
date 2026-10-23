// 「当前在哪」在应用里只有一种画法（票 #234；票 #239 把它改成深浅同构）。
//
// 左栏三处（侧栏收藏夹 / 关注页分组 / 动态页作者栏）和队列抽屉的「正在播放」
// 原来各画各的：左栏三处是**实心主色大块**（动态流那一处是 248×50），队列抽屉
// 那一行是**整行实心主色**，而同一屏上的侧栏菜单当前项是「淡主色底 + 深色字」
// （深色下是中性灰底 + 主色字）。
//
// 用户不用比较两个界面就能看出「这个应用没做完」（审计 F-02 / F-06、UX B2）。
//
// # 判据是**画面上那一格的底色**
//
// 每一处都和 `flatCurrent`（`flatbutton.go`）**同一个配方**，而那个配方在票 #239
// 之后**深浅两套是同一个东西**（审计 F-15：浅色下原来是「主色当底」、深色下是
// 「主色当字」，主色的角色在两套里换了）：
//
//	浅色	中性灰底 + 主色压深的那一档字
//	深色	中性灰底 + 主色字
//
// ⚠️ **浅色那一档的底不再合到「旁边那一格」身下的底板上**：以前它是**半透明**的
// （主色 14%），屏幕上的数是它合出来的结果；现在是**不透明**的中性灰，直接就是
// 那个数。
//
// ⚠️ **取的是那一块里的「众数」**，不是某一个像素：那一块里既有底也有字（深色下
// 那一行连字都是主色），而底占绝大多数——众数才是底。
package ui

import (
	"image"
	"image/color"
	"net/http"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// currentSpot 是「当前那一格」和「旁边那一格」的位置。
type currentSpot struct {
	cur  mygoui.Rect
	sib  mygoui.Rect
	name string
	// ink 是那一格里的**字**该是什么色。
	//
	// ⚠️ **五处现在是同一个数**（票 #239 之后）：`flatCurrentText` 那一档——深色下
	// 是主色，浅色下是从主色现算的、压到 ≥4.5:1 的那个变体。
	//
	// 改之前这里分两档（左栏那四处是正文色、队列抽屉那一行是 `ink.AccentOn`），
	// 票 #239 把浅色下的底从「主色 14%」换成中性灰之后，那一档主色当字也得跟着
	// 换成「按新底算」的那一档——否则底换了、字没换。
	ink mygoui.Color
}

// assertCurrentIsFlat 断「当前那一格是 flatCurrent 那一档，不是实心主色」。
//
// ⚠️ **三个判据缺一不可**：
//
//   - 「不是实心主色」——那是改之前的样子（F-06：248×50 的实心大块）；
//   - 「底色就是 flatCurrent 那一档」——只断前一条的话，一块**别的**浅色也过；
//   - 「字色是那一档该有的」——底换了字没换，浅色下就是一行灰底上的正文色而
//     别处是主色字（深色下则是灰底上的黑字）。
func assertCurrentIsFlat(t *testing.T, sp currentSpot, img *image.RGBA, dark bool) {
	t.Helper()
	curPx := bandPixels(img, sp.cur)
	cur := modeOf(curPx)
	sib := modeOf(bandPixels(img, sp.sib))
	// 字是抗锯齿画的：那一块里离底色最远的那一个像素就是它（`farthestFrom`
	// 那个共用工具）。
	ink := farthestFrom(img, sp.cur, cur)
	t.Logf("%s：当前那一格 底 %v 字 %v，旁边那一格 底 %v", sp.name, cur, ink, sib)

	theme := BuildTheme(app.DefaultSettings(""), dark)

	// 判据一：**不是实心主色**。
	if nearRGBA(cur, rgbaOf(theme.Accent), 2) {
		t.Errorf("%s：当前那一格是实心主色 %v——那是改之前的样子（F-06：248×50 的"+
			"实心大块，比主操作还响）", sp.name, cur)
	}

	// 判据二：**底色就是 flatCurrent 那一档**。
	//
	// ⚠️ **深浅两套都是不透明的中性灰**（票 #239 之前浅色那档是半透明的主色 14%，
	// 那时要合到旁边那一格身下的底板上才是屏幕上的数）。
	want := flatCurrentBg(theme)
	if !nearRGBA(cur, rgbaOf(want), 3) {
		t.Errorf("%s：当前那一格的底色是 %v，want %v（flatCurrent 那一档）", sp.name, cur, want)
	}

	// 判据三：**字色是那一档该有的**。
	//
	// ⚠️ **容差是 24**：字是抗锯齿画的，一个像素 rarely 是纯字色——13px 的汉字
	// 笔画只有一两个像素宽，量到的那个「最远离底色的像素」是字色和底色的混合。
	// 24 是从实测来的（实测差 0~12），而两档字色之间差着几十个通道（正文色
	// #18181b 与主色压深那一档差 90+），所以这个容差拦得住「字色换了」。
	if !nearRGBA(ink, rgbaOf(sp.ink), 24) {
		t.Errorf("%s：当前那一格的字色是 %v，want %v", sp.name, ink, sp.ink)
	}
}

// farthestFrom 是那些像素里离 bg 最远的那一个——**字本身**（抗锯齿画出来的那一
// 串过渡色里，它是离底色最远的那一头）。

// bandPixels 是那一块里的像素。
func bandPixels(img *image.RGBA, r mygoui.Rect) []color.RGBA {
	var out []color.RGBA
	x0, y0 := int(r.X), int(r.Y)
	x1, y1 := int(r.X+r.W), int(r.Y+r.H)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			out = append(out, img.RGBAAt(x, y))
		}
	}
	return out
}

// nearRGBA 报两个颜色差得够不够近（每个通道的差都 ≤ tol）。
func nearRGBA(a, b color.RGBA, tol int) bool {
	return maxDiff(a, b) <= tol
}

// rgbaToColor 把量出来的像素变回 `ui.Color`（要拿去做 `Over`）。
func rgbaToColor(c color.RGBA) mygoui.Color {
	return mygoui.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// TestTheCurrentItemIsFlatEverywhere 钉住「当前在哪」只有一种画法。
//
// 四处：关注页的分组、动态页的作者栏、侧栏的收藏夹、队列抽屉的「正在播放」。
// 深浅两套都跑（DESIGN §八：深色下差距更明显）。
func TestTheCurrentItemIsFlatEverywhere(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			t.Run("关注页分组", func(t *testing.T) {
				b := newUIExplore(t)
				uiFollowTags(b, map[int]any{-10: "特别关注"})
				uiFollowList(b, 3, []any{uiFollowUser(11, "第一个 UP")})
				uiFollowSearch(b, "", nil)
				s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
				s.navigate(followPath)
				tt.Frame()
				f := s.followPageOf()
				waitFrames(t, tt, "分组到了", func() bool { return f.tagStarted })
				tt.Frame()

				cur, ok := tt.Find("全部关注(3)")
				if !ok {
					t.Fatalf("左栏没有「全部关注(3)」那一格，文字有 %q", tt.Texts())
				}
				sib, ok := tt.Find("特别关注(1)")
				if !ok {
					t.Fatalf("左栏没有「特别关注(1)」那一格，文字有 %q", tt.Texts())
				}
				theme := BuildTheme(app.DefaultSettings(""), dark)
				assertCurrentIsFlat(t, currentSpot{cur, sib, "关注页分组",
					flatCurrentText(theme, true)}, tt.Image(), dark)
			})

			t.Run("动态页作者栏", func(t *testing.T) {
				b := newUIExplore(t)
				uiSpaceData(b, "我自己", 0, 1)
				b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
					uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
				})
				b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
					uiExploreJSON(w, 0, map[string]any{"total": 1, "list": []any{
						map[string]any{"mid": 11, "uname": "第一个 UP", "face": "https://f/1.jpg"},
					}})
				})
				s, tt, _ := newSpaceShell(t, b, true, "77", systemIsDark(dark))
				s.navigate(dynamicFeedPath)
				tt.Frame()
				f := s.dynamicFeedOf()
				waitFrames(t, tt, "关注的人到了", func() bool { return len(f.authors) >= 1 })
				tt.Frame()

				cur, ok := tt.Find("全部")
				if !ok {
					t.Fatalf("作者栏没有「全部」那一行，文字有 %q", tt.Texts())
				}
				sib, ok := tt.Find("第一个 UP")
				if !ok {
					t.Fatalf("作者栏没有那个人，文字有 %q", tt.Texts())
				}
				theme := BuildTheme(app.DefaultSettings(""), dark)
				assertCurrentIsFlat(t, currentSpot{cur, sib, "动态页作者栏",
					flatCurrentText(theme, true)}, tt.Image(), dark)
			})

			t.Run("侧栏收藏夹", func(t *testing.T) {
				s, _, tt, _ := favShell(t, 2, 0, systemIsDark(dark))
				s.navigate(collectionPath(1))
				tt.Frame()

				cur, ok := tt.Find("我建的 1")
				if !ok {
					t.Fatalf("侧栏没有「我建的 1」那一行，文字有 %q", tt.Texts())
				}
				sib, ok := tt.Find("我建的 2")
				if !ok {
					t.Fatalf("侧栏没有「我建的 2」那一行，文字有 %q", tt.Texts())
				}
				theme := BuildTheme(app.DefaultSettings(""), dark)
				assertCurrentIsFlat(t, currentSpot{cur, sib, "侧栏收藏夹",
					flatCurrentText(theme, true)}, tt.Image(), dark)
			})

			t.Run("队列抽屉", func(t *testing.T) {
				b := newTestShell(t, at(winW, winH), systemIsDark(dark))
				h := &harness{app: b.a, tt: b.tt}
				enqueue(t, h, copiesOf(t, 2)...)
				openDrawer(t, h)

				// ⚠️ **用「b」那一行推**：歌名「a」在**播放条上也有一个**（正在放的
				// 就是它），而 `Find` 给的是头一个——播放条上那一个。「b」只有抽屉里
				// 有，它的框是**整行**（383×64）。
				sibRow, ok := findRow(h, "b")
				if !ok {
					t.Fatalf("抽屉里没有「b」那一行，文字有 %q", h.tt.Texts())
				}
				// 「a」是上一行（行高 64）。
				curRow := sibRow
				curRow.Y -= queueRowHeight

				// ⚠️ **取样取文字那一列**：行尾那颗「⋯」里面是图标，不是歌名的字。
				// 那一列的左边是封面（行左 8 + 封面 48 = 行左 56），所以从行左 60
				// 起取——歌名的字（行左 64 起）在里面，封面不在里面。
				cur := mygoui.Rect{X: curRow.X + 60, Y: curRow.Y + 4, W: 200, H: 56}
				sib := mygoui.Rect{X: sibRow.X + 60, Y: sibRow.Y + 4, W: 200, H: 56}
				theme := BuildTheme(app.DefaultSettings(""), dark)
				// 队列抽屉那一行的字是**主色当字**（`flatCurrentText` 那一档）——
				// 它的底是中性灰（不是页面底色），所以字是按那个底算出来的那一档。
				assertCurrentIsFlat(t, currentSpot{cur, sib, "队列抽屉",
					flatCurrentText(theme, true)}, h.tt.Image(), dark)
			})
		})
	}
}
