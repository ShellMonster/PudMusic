// 「当前在哪」在应用里只有一种画法（票 #234）。
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
// 每一处都和 `flatCurrent`（`flatbutton.go`）**同一个配方**：
//
//	浅色	主色很淡的一层底（14%） + 正文色
//	深色	中性灰底 + 主色字
//
// 底色是拿**旁边那一格**当参照量出来的——旁边那一格没有底，量到的那一层就是
// 这一格身下的那块底板。于是判据不写死任何面板颜色：侧栏是 `t.Surface`、
// 页面和抽屉是 `t.Background`，测出来的数自己会对上。
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
}

// assertCurrentIsFlat 断「当前那一格是 flatCurrent 那一档，不是实心主色」。
//
// ⚠️ **两个判据缺一不可**：
//
//   - 「不是实心主色」——那是改之前的样子（F-06：248×50 的实心大块）；
//   - 「就是 flatCurrent 那一档」——只断前一条的话，一块**别的**浅色也过。
func assertCurrentIsFlat(t *testing.T, sp currentSpot, img *image.RGBA, dark bool) {
	t.Helper()
	cur := modeOf(bandPixels(img, sp.cur))
	sib := modeOf(bandPixels(img, sp.sib))
	t.Logf("%s：当前那一格 %v %v，旁边那一格 %v %v", sp.name, sp.cur, cur, sp.sib, sib)

	theme := BuildTheme(app.DefaultSettings(""), dark)

	// 判据一：**不是实心主色**。
	if nearRGBA(cur, rgbaOf(theme.Accent), 2) {
		t.Errorf("%s：当前那一格是实心主色 %v——那是改之前的样子（F-06：248×50 的"+
			"实心大块，比主操作还响）", sp.name, cur)
	}

	// 判据二：**就是 flatCurrent 那一档**。
	want := flatCurrentBg(theme)
	if !dark {
		// 浅色那一档是半透明的：合到**旁边那一格**身下的底板上才是屏幕上的数。
		want = want.Over(rgbaToColor(sib))
	}
	if !nearRGBA(cur, rgbaOf(want), 3) {
		t.Errorf("%s：当前那一格的底色是 %v，want %v（flatCurrent 那一档）", sp.name, cur, want)
	}
}

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
				assertCurrentIsFlat(t, currentSpot{cur, sib, "关注页分组"}, tt.Image(), dark)
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
				assertCurrentIsFlat(t, currentSpot{cur, sib, "动态页作者栏"}, tt.Image(), dark)
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
				assertCurrentIsFlat(t, currentSpot{cur, sib, "侧栏收藏夹"}, tt.Image(), dark)
			})

			t.Run("队列抽屉", func(t *testing.T) {
				b := newTestShell(t, at(winW, winH), systemIsDark(dark))
				h := &harness{app: b.a, tt: b.tt}
				enqueue(t, h, copiesOf(t, 2)...)
				openDrawer(t, h)

				// ⚠️ **用行尾那颗「⋯」找正在放的那一行**：歌名「a」在**播放条上也有
				// 一个**（正在放的就是它），而 `Find` 给的是头一个——播放条上那一个。
				// 「这一首的操作」只有抽屉里有，而且第一处就是第一行（正在放的那一行）。
				cur, ok := h.tt.Find("这一首的操作")
				if !ok {
					t.Fatalf("抽屉里没有那一行，文字有 %q", h.tt.Texts())
				}
				sib, ok := findRow(h, "b")
				if !ok {
					t.Fatalf("抽屉里没有「b」那一行，文字有 %q", h.tt.Texts())
				}
				assertCurrentIsFlat(t, currentSpot{cur, sib, "队列抽屉"}, h.tt.Image(), dark)
			})
		})
	}
}
