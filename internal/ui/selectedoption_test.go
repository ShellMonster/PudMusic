// 「被选中的选项」在应用里只有一种画法（票 #239；审计 F-14）。
//
// # 改之前是三样
//
// 设置页那些**选项**（主题三档、窗口关闭两档、下拉项）选中的时候，各画各的：
//
//	设置页的档位按钮	淡主色底 + 正文色（`flatCurrent`，和左栏「当前在哪」同一套）
//	下拉项的高亮	实心主色底 + `t.AccentText`
//	主 Tab		透明底 + 正文色（选中由滑块说）
//
// 用户在设置页里点两下就能看出「这两个不是一伙的」——而它们问的是同一个问题：
// 「这一项被选中了没有」。
//
// # 现在只有一样：实心主色底 + `t.AccentText` 字
//
// 这就是 `flatSolid`（`internal/ui/flatbutton.go`），也是上游的画法：HeroUI 的
// `Radio` 选中项是 `bg-primary` 实心底 + `text-primary-foreground`，Biu 的
// 倍速浮层是 `color="primary" variant="solid"`。
//
// ⚠️ **主 Tab 不在这一族里**：它是**导航**（切的是「在哪一页」），而这些是**选项**
// （选的是「哪一个值」）。主 Tab 的选中由**滑块**说——上游 HeroUI 的
// `Tabs variant="solid"` 就是灰轨 + 滑块，改成实心主色块是偏离上游，而一排 Tab
// 里飘一块绿比滑块响得多（见 `tabs.go` 的文件头）。
//
// # 判据是**画面上那两格**
//
// 底取**众数**（那一格里绝大多数像素是底，字只占少数），字取**离底最远**的那一个
// （抗锯齿画出来的那串过渡色里最远的那一头就是字本身）。
//
// ⚠️ **深浅两套都要跑**：主色在两套下是同一个值，而 `t.AccentText`（字）是按主色
// 的亮度挑的黑或白——默认那个绿够亮，所以两套都是**深色字**。
package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// assertSolidAccentOption 断「这一格是『被选中的选项』那一档」。
//
// ⚠️ **底和字都要断**：只断底的话，一块**别的**主色块（主色当底、字不是
// `AccentText`）也过——而那样的话绿底上的字可能读不出来。
func assertSolidAccentOption(t *testing.T, tt *mygoui.Tester, label string, theme *mygoui.Theme) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("画面上找不到 %q，文字有 %q", label, tt.Texts())
	}
	img := tt.Image()
	px := bandPixels(img, r)
	if len(px) == 0 {
		t.Fatalf("%q 那一格在画面上是空的", label)
	}
	bg := modeOf(px)
	ink := farthestFrom(img, r, bg)
	t.Logf("%q：底 %v 字 %v", label, bg, ink)

	if !nearRGBA(bg, rgbaOf(theme.Accent), 3) {
		t.Errorf("%q 那一格的底色是 %v，want %v（实心主色）", label, bg, theme.Accent)
	}
	if !nearRGBA(ink, rgbaOf(theme.AccentText), 24) {
		t.Errorf("%q 那一格的字色是 %v，want %v（主色底上的字）", label, ink, theme.AccentText)
	}
}

// TestTheSelectedOptionLooksTheSameEverywhere 钉住「被选中的选项只有一种画法」。
//
// 三处：设置页主题三档里选中的那一档、窗口关闭两档里选中的那一档、下拉项的高亮。
// 深浅两套都跑。
func TestTheSelectedOptionLooksTheSameEverywhere(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ① 主题三档：默认那一档是「跟随系统」。
			t.Run("设置页的档位按钮", func(t *testing.T) {
				tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
				theme := BuildTheme(app.DefaultSettings(""), dark)
				assertSolidAccentOption(t, tt, "跟随系统", theme)
			})

			// ② 窗口关闭两档：默认那一档是「隐藏到托盘」。
			//
			// ⚠️ **要用 `settingRect` 把它滚进可视区**：设置页外面套了一层
			// `Scroll`，这一项在第一屏之外，而 `tt.Find` 找到的东西**可能在
			// 屏幕外**——那样量出来的是空的（第一版就这么写，量到 0 个像素）。
			//
			// ⚠️ **每一段都自己造一个外壳**：滚���是**往下**滚的，而「音质偏好」
			// 那个下拉在「隐藏到托盘」**上面**——同一个外壳里滚不过去。
			t.Run("窗口关闭那两档", func(t *testing.T) {
				tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
				theme := BuildTheme(app.DefaultSettings(""), dark)
				settingRect(t, tt, s, "隐藏到托盘")
				assertSolidAccentOption(t, tt, "隐藏到托盘", theme)
			})

			// ③ 下拉项的高亮：指针落在哪一项，哪一项就是「实心主色底」。
			//
			// ⚠️ **高亮不是「当前值」**：当前值那一档是**淡底、没有主色**
			// （`settings_enum.go` 的 `it.value == *value` 那一支）。所以这里
			// 量的是**指针落上去**的那一项，不是「自动」那一项。
			//
			// ⚠️ **点的是 trigger 上画着的那个字**（「自动」），不是它的无障碍
			// 名字（「选择音质偏好」）：后者在画面上**不占地方**，按坐标点会点空。
			t.Run("下拉项的高亮", func(t *testing.T) {
				tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
				theme := BuildTheme(app.DefaultSettings(""), dark)
				settingRect(t, tt, s, "自动")
				if err := tt.Click("自动"); err != nil {
					t.Fatalf("点不开「音质偏好」那个下拉：%v，文字有 %q", err, tt.Texts())
				}
				tt.Frame()
				opt, ok := tt.Find("无损")
				if !ok {
					t.Fatalf("下拉里没有「无损」那一项，文字有 %q", tt.Texts())
				}
				tt.Move(opt.X+opt.W/2, opt.Y+opt.H/2)
				tt.Frame()
				assertSolidAccentOption(t, tt, "无损", theme)
			})
		})
	}
}
