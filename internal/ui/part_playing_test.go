// 分集抽屉里「正在放」那一集的**底色和底上的字**（票 #323）。
//
// 上游那一行走的是 HeroUI 的 `variant="flat" color="primary"`
// （`layout/playbar/left/page-list.tsx`），flat+primary 展开是
// `bg-primary/20 text-primary-600`——**主色 20% 那一层淡底**。浅色下落在浮层面板
// 底（#ffffff）上是 **#d2f7df**（上游实图那个数），深色下落在 #18181b 上是
// **#193e29**。
//
// 我们原来是**实心主色底**——和上游差着一个数，而队列抽屉的「正在播放」那一行在
// 票 #294 已经改回上游那一档了。**同一件事，两个抽屉差着两种画法**——票 #323 把
// 这一处补上，两个抽屉现在共用同一份配方（`playingrow.go`）。
//
// ⚠️ **别把它做成实心主色底**：实心那一档是「被选中的选项」（`flatSolid`），
// 票 #197 刚把列表选中行的实心主色底去掉——那一档说的是「选的是哪一个值」，
// 这一档说的是「这一集在放」。
package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	mygoui "github.com/egoist/mygo/ui"
)

// TestThePartDrawerPlayingRowKeepsItsNumbers 钉住那几个数（默认主色，深浅两套）。
//
// 判据是**具体的数**，不是「≥某个门槛」——门槛那条在下面那条里。写死是为了让
// 「主色的浓度被改了」「字色换了一档」这类事立刻红：20% → 30% 时浅色那一格会变成
// #b7f0c8，而这一条会指着 #d2f7df 说不对。
//
// 顺带钉住「底色是**不透明**的」：底要是半透明，屏幕上的数就是它合到面板底之后的
// 结果——那一格算出来的数和画出来的不一样，对比度也就算不准了。
//
// ⚠️ **那三个字色和队列抽屉那一行是同一套**（`playingrow.go`）：浅色下标题是
// 压深的主色（#127d38）、编号是次要色按同一个淡底重算（#6b6b74）；深色下标题就是
// 主色本身、编号原样就够。
func TestThePartDrawerPlayingRowKeepsItsNumbers(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			theme := BuildTheme(app.DefaultSettings(""), dark)

			bg := playingRowBg(theme)
			want := mygoui.Hex("#d2f7df")
			if dark {
				// 主色 #1ed760 的 20% 铺在 #18181b 上。
				want = mygoui.Hex("#193e29")
			}
			if bg != want {
				t.Errorf("%s：淡主色底是 %v，want %v", name, bg, want)
			}
			if bg.A != 255 {
				t.Errorf("%s：淡主色底带着 alpha %d——屏幕上的数是它合到面板底之后的"+
					"结果，对比度就按不准了", name, bg.A)
			}

			// 标题：主色当字，按那一层淡底算。
			title := partRowText(theme, true, false)
			if dark {
				if title != theme.Accent {
					t.Errorf("深色：标题是 %v，want 主色本身 %v", title, theme.Accent)
				}
			} else {
				if title != mygoui.Hex("#127d38") {
					t.Errorf("浅色：标题是 %v，want #127d38（压深的主色）", title)
				}
				if title == theme.Accent {
					t.Errorf("浅色：标题就是主色本身 %v——它压淡底只有 1.65:1，读不出来",
						title)
				}
			}

			// 编号：次要色，按同一层淡底重算（它跟没在放的那些行里的编号是同一档）。
			num := partRowText(theme, true, true)
			if dark {
				if num != theme.TextMuted {
					t.Errorf("深色：编号是 %v，want 次要色 %v（深色下原样就够）",
						num, theme.TextMuted)
				}
			} else if num != mygoui.Hex("#6b6b74") {
				t.Errorf("浅色：编号是 %v，want #6b6b74（次要色按淡底重算）", num)
			}

			// 没在放的那些行：一个字都不改（底是面板底，字是正文色 / 次要色）。
			if got := partRowText(theme, false, false); got != theme.Text {
				t.Errorf("%s：没在放的那一行标题是 %v，want 正文色 %v", name, got, theme.Text)
			}
			if got := partRowText(theme, false, true); got != theme.TextMuted {
				t.Errorf("%s：没在放的那一行编号是 %v，want 次要色 %v", name, got, theme.TextMuted)
			}
		})
	}
}

// TestTheTextOnThePalePartRowIsReadable 断「淡主色底上的字读得出来」。
//
// 判据是 **WCAG 正文 AA 的 4.5:1**（标题 13 / 编号 12，都是普通字号）。
//
// ⚠️ **量的是结构体里的那几个数，不是画面上的像素**：字是抗锯齿画的，画面上量的
// 那个「最深的像素」是字色和底色的混合，算出来的对比度会**偏低于**真实值——那是
// 量法的问题，不是界面的问题。底色那一层是实心的（没有抗锯齿），所以底色可以量
// 像素，字色不行。
//
// ⚠️ **主色是用户可设的**：所以换几个色相都不同的主色再算一遍——写死一个绿的
// 话，用户挑了别的色这一条就会红。
func TestTheTextOnThePalePartRowIsReadable(t *testing.T) {
	accents := []string{
		"#1ed760", // Biu 的绿（默认）
		"#ff0000", // 深红
		"#ffe600", // 亮黄
		"#2563eb", // 中蓝
		"#808080", // 灰
		"#00ffff", // 青
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			for _, hex := range accents {
				s := app.DefaultSettings("")
				s.PrimaryColor = hex
				theme := BuildTheme(s, dark)
				bg := playingRowBg(theme)

				for _, c := range []struct {
					what string
					ink  mygoui.Color
				}{
					{"标题", partRowText(theme, true, false)},
					{"编号", partRowText(theme, true, true)},
				} {
					if r := ink.Ratio(c.ink, bg); r < 4.5 {
						t.Errorf("%s 主色 %s：%s %v 压淡底 %v 只有 %.2f:1（< 4.5）",
							name, hex, c.what, c.ink, bg, r)
					}
				}
				if hex == "#1ed760" {
					t.Logf("%s：淡底 %v，标题 %v（%.2f:1），编号 %v（%.2f:1）", name, bg,
						partRowText(theme, true, false),
						ink.Ratio(partRowText(theme, true, false), bg),
						partRowText(theme, true, true),
						ink.Ratio(partRowText(theme, true, true), bg))
				}
			}
		})
	}
}
