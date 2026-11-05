// 「被选中的选项」的画法（原型版）。
//
// 旧口径（票 #239）是"实心主色底 + AccentText 字"一种画法，被原型取代。原型里按控件分三种：
//
//	主题卡 / 显示样式卡   `.theme-card[aria-checked]`：2px 主色描边（没选中是 line，悬停 line-strong）
//	下拉 / 弹层的选项     `.pop-opt`：悬停 `hover` 底；选中是 accent-text 600 字加右边的对勾
//	窗口关闭              已经是开关（`关闭窗口时留在菜单栏`），不再是两档选项
//
// 判据仍然是画面上的像素；深浅两套都跑，造壳带 `systemIsDark(dark)`。
package ui

import (
	"image/color"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// cardEdge 取一张卡左边缘中点的颜色（描边画在元素内侧，第 1 像素就是描边）。
func cardEdge(t *testing.T, tt *mygoui.Tester, label string) (mygoui.Rect, [4]uint8) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", label, tt.Texts())
	}
	px := tt.Image().RGBAAt(int(r.X)+1, int(r.Y+r.H/2))
	return r, [4]uint8{px.R, px.G, px.B, px.A}
}

// TestTheSelectedOptionLooksTheSameEverywhere 钉住原型里"被选中"的三种画法各自成立。
func TestTheSelectedOptionLooksTheSameEverywhere(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ① 主题卡：默认选中「跟随系统」，描边是主色；没选中的「深色」卡描边是 line。
			t.Run("主题卡的描边", func(t *testing.T) {
				tt, _, _, _ := shellOnAppearance(t, app.DefaultSettings(""), systemIsDark(dark))
				p := tok.Of(BuildTheme(app.DefaultSettings(""), dark))
				tt.Move(600, 20) // 指针挪开：悬停会把描边换成 line-strong
				tt.Frame()
				tt.Advance(300 * time.Millisecond)
				tt.Frame()
				r, got := cardEdge(t, tt, "跟随系统")
				want := rgbaOf(p.Accent)
				if !nearRGBA(color.RGBA{got[0], got[1], got[2], got[3]}, want, 3) {
					t.Errorf("选中的主题卡 %v 左边缘是 %v，want 主色描边 %v", r, got, want)
				}
				_, other := cardEdge(t, tt, "浅色")
				if nearRGBA(color.RGBA{other[0], other[1], other[2], other[3]}, want, 3) {
					t.Errorf("没选中的主题卡也是主色描边 %v——那就没有「选中」这一说了", other)
				}
			})

			// ② 下拉项：指针落在哪一项，哪一项换 hover 底、字是正文色。
			//
			// ⚠️ 点的是 trigger 上画着的字（默认档「高品质」），不是它的无障碍名字。
			t.Run("下拉项的悬停", func(t *testing.T) {
				tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""), systemIsDark(dark))
				p := tok.Of(BuildTheme(app.DefaultSettings(""), dark))
				settingRect(t, tt, s, "高品质")
				if err := tt.Click("高品质"); err != nil {
					t.Fatalf("点不开「音质偏好」那个下拉：%v，文字有 %q", err, tt.Texts())
				}
				tt.Frame()
				opt, ok := tt.Find("无损")
				if !ok {
					t.Fatalf("下拉里没有「无损」那一项，文字有 %q", tt.Texts())
				}
				tt.Move(opt.X+opt.W/2, opt.Y+opt.H/2)
				tt.Frame()
				tt.Advance(300 * time.Millisecond) // hover 底是 160ms 渐变
				tt.Frame()
				img := tt.Image()
				bg := modeOf(bandPixels(img, opt))
				// hover 令牌是半透明的，画面上是它压在弹层底（surface）上的合成色。
				if want := rgbaOf(p.Hover.Over(p.Surface)); !nearRGBA(bg, want, 4) {
					t.Errorf("悬停的「无损」底色是 %v，want hover 底 %v", bg, want)
				}
				if ink := farthestFrom(img, opt, bg); !nearRGBA(ink, rgbaOf(p.Text), 24) {
					t.Errorf("悬停的「无损」字色是 %v，want 正文色 %v", ink, p.Text)
				}
			})
		})
	}
}
