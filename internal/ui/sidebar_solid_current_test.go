package ui

// 侧栏「当前在哪」改成**实心主色底**（票：用户要「深底色 + 白字」）。
//
// # 为什么改
//
// 原来那一档是 `accentSoft` 淡底（主色 14% / 深色 18%），而实测它和侧栏底色
// 的对比度只有 **1.09:1**（暖纸浅色）到 **1.29:1**（靛夜深色）——**几乎分不
// 出来**。用户要的是「一眼看出当前选的是哪一个」。
//
// 顺带修一个**算错了底板**的缺陷：`accentSoft` 拿的是**页面底色**
// `t.Background` 去混主色，而这一行实际压在**侧栏底色**（方案的 `sidebar`）
// 上——两者不是同一个色（暖纸浅色下 `#F8F4ED` vs `#EFE9DE`）。所以那一档底色
// 算得比设计意图更淡。
//
// # 口径
//
// 这一档是「**当前在哪**」——侧栏菜单当前项 + 侧栏收藏夹当前行，**同一列的
// 同一件事**，所以两处**同一个出口**。
//
// ⚠️ **字色不能写死白字**：主色是用户可设的，而深色方案的主色是**亮色**
// （暖纸深色 `#E58A5A`），白字压上去只有 **2.59:1**——读不出来。所以字取
// `t.AccentText`：那是框架按主色亮度挑出来的黑或白（`applyAccent`），**在主色
// 底上一定读得出来**。
//
// ⚠️ **次要字（「N 首」）也要按底重算**：`t.TextMuted` 压在实心主色底上只有
// **1.02:1 ~ 1.34:1**，完全读不出来。

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestTheSelectedSidebarRowIsSurface2 断选中那一行是 `surface-2` 淡底（原型
// `.fav-item[aria-current]`），没选中的那一行不是。深浅两套都断。
func TestTheSelectedSidebarRowIsSurface2(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
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

			want := rgbaOf(tok.Of(theme).Surface2)
			got := modeOf(bandPixels(tt.Image(), cur))
			if !nearRGBA(got, want, 3) {
				t.Errorf("选中那一行的底色是 %v，want %v（surface-2）", got, want)
			}
			// 旁边那一格**不该**也是这个底（不然「当前在哪」没信号）。
			sibPx := modeOf(bandPixels(tt.Image(), sib))
			if nearRGBA(sibPx, want, 3) {
				t.Errorf("没选中那一行也是 surface-2 底 %v——那就没有「当前」这一说了", sibPx)
			}
		})
	}
}
