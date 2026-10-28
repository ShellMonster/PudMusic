// 语义色当**字色**用时该取的那一档：危险 / 警告 / 成功。
//
// # 为什么有这一层
//
// 上游 HeroUI 那三个色相（危险 `#f31260`、警告 `#f5a524`、成功 `#17c964`）
// **深浅两套同值**，而它们浅色底下当字色用时对比度不够：
//
//	成功 2.19:1 ／ 警告 2.04:1 ／ 危险 4.14:1
//
// （数在 `internal/ui/theme.go` 的语义色那一段。）
//
// **色值本身不能改**——改了就是「自己发明了一个上游没有的配色」，而
// `semantic_test.go` 那一组就盯着这三个值。但**当字色用的时候可以另取一档**：
// 色相不变，只压深——这正是主色那一档（`widgets/ink` 的 `AccentOn`）一直在做的事，
// 语义色这一票（票 #241 / 报告 F-22）跟进的就是同一件事。
//
// # 三条规矩
//
//  1. **当字色** → 走这里（`dangerText` / `warningText` / `successText`）。
//  2. **当底色**（确认键、Toast）→ 那一档要的是「底上的前景色」，见
//     `confirmmodal.go` 的 `dialogOnDanger` 和 `toast.go`。**别用这里。**
//  3. **当图形色**（图标、圆点、描边）→ **用主题里那一个原值**：图形不受 4.5:1
//     约束（3:1 即可），而改了它们就改了上游那三枚图标的颜色。
//
// ⚠️ **这三个函数算的是「当字色」那一档，不是主题里的那一个**：拿
// `t.Danger` 去和它们的结果比，会以为改了主题——主题里那三个值一个字节都没动。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// dangerText 是危险色当**字色**用时该取的那一档（压深到与底色 ≥4.5:1）。
func dangerText(t *ui.Theme) ui.Color { return ink.SemanticOn(t, t.Danger) }

// warningText 是警告色当**字色**用时该取的那一档（压深到与底色 ≥4.5:1）。
func warningText(t *ui.Theme) ui.Color { return ink.SemanticOn(t, t.Warning) }

// successText 是成功色当**字色**用时该取的那一档（压深到与底色 ≥4.5:1）。
func successText(t *ui.Theme) ui.Color { return ink.SemanticOn(t, t.Success) }

// readableOn 是「压在语义色底上的字」：先用惯用的那个字色（`preferred`），读不出来
// （对比度 < 3:1）才换成白 / 近黑里更清楚的那一个。
//
// 为什么要有它：主题方案（暖纸 / 暮光 / 柔雾）的语义色和经典那三个上游色不是一个深浅——
// 浅色下的警告是深棕 `#9A5B06`、成功是深绿 `#2E7D4F`（惯用的近黑字压上去不到 3:1），
// 深色下的危险是浅红 `#F0716B`（惯用的白字压上去不到 3:1）。经典那三个色配惯用字色都
// ≥ 4.15:1，**不会触发换色**，所以经典的 Toast / 确认键一个像素不变。
func readableOn(bg, preferred ui.Color) ui.Color {
	if ink.Ratio(preferred, bg) >= 3 {
		return preferred
	}
	white, dark := ui.RGB(255, 255, 255), ui.RGB(24, 24, 27)
	if ink.Ratio(white, bg) >= ink.Ratio(dark, bg) {
		return white
	}
	return dark
}
