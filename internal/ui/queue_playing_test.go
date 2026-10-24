// 队列抽屉「正在播放」那一行的**底色**（票 #294 / 报告 §1 的 S-11）。
//
// 上游那一行走的是 HeroUI 的 `variant="flat" color="primary"`
// （`music-playlist-drawer/list-item.tsx:32`），flat+primary 展开是
// `bg-primary/20 text-primary-600`——**主色 20% 那一层淡底**。浅色下落在抽屉
// 面板底（#ffffff）上是 **#d2f7df**（上游实图那个数）。
//
// 我们原来是**中性灰底**（`flatCurrent` 那一档，和侧栏菜单当前项、左栏那三处
// 共用）。2026-10-24 拍板按上游改：DESIGN §七保护的是「**主色当字色**的对比
// 度」，不是「主色当底色」；那一层淡底是**状态识别**——一眼看出哪一首在放。
//
// ⚠️ **别做成实心主色底**：实心那一档是「被选中的选项」（`flatSolid`），票 #197
// 刚把列表选中行的实心主色底去掉——那一档说的是「选的是哪一个值」，这一档说的
// 是「这一首在放」。
package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheQueuePlayingRowUsesAPalePrimaryBackground 断「正在播放」那一行的底是
// **淡主色底**（深浅两套都量）。
//
// 三个判据缺一不可：
//
//   - **底就是主色 20% 那一层**——只断「不是中性灰」的话，随便换成别的浅色也过；
//   - **不是实心主色底**——那是改之前的样子（票 #197 去掉的那一档）；
//   - **和旁边那一行不一样**——旁边那行没在放，它的底是面板底（#ffffff）。
func TestTheQueuePlayingRowUsesAPalePrimaryBackground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			theme := BuildTheme(app.DefaultSettings(""), dark)
			want := playingRowBg(theme)

			// 上游那个数：浅色下 ≈#d2f7df（主色 #1ed760 的 20% 铺在 #ffffff 上）。
			// ⚠️ **这是硬编码的那个数**：它认的是「主色 20%」这条配方，不是「当前
			// 主色 20%」——用户换了主色时下面那条（want）才是判据。
			if !dark && want != mygoui.Hex("#d2f7df") {
				t.Errorf("浅色下「正在播放」那一行的底是 %v，want #d2f7df（上游那个数）", want)
			}

			b := newTestShell(t, at(winW, winH), systemIsDark(dark))
			// ⚠️ **s 那一栏不能少**：抽屉的进出场要走完才量得到底色
			//（Tester 的钟站着不动，不推它的话面板还在窗口外面），
			// 而「走完了没有」的判据是那副外壳上的阶段（见 `settleDrawer`）。
			h := &harness{app: b.a, tt: b.tt, s: b.s}
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
			img := h.tt.Image()

			// 那一块里既有底也有字，而底占绝大多数——**众数才是底**。
			got := modeOf(bandPixels(img, cur))
			t.Logf("%s：正在播放那一行 底 %v，旁边那一行 底 %v", name, got,
				modeOf(bandPixels(img, sib)))

			if !nearRGBA(got, rgbaOf(want), 3) {
				t.Errorf("%s：正在播放那一行的底色是 %v，want %v（主色两成那一层）",
					name, got, want)
			}
			if nearRGBA(got, rgbaOf(theme.Accent), 2) {
				t.Errorf("%s：正在播放那一行是**实心主色底** %v——那是「被选中的选项」"+
					"那一档（票 #197 刚去掉的那种）", name, got)
			}
			if nearRGBA(got, modeOf(bandPixels(img, sib)), 3) {
				t.Errorf("%s：正在播放那一行和旁边那一行是同一个底色 %v——"+
					"「哪一首在放」就看不出来了", name, got)
			}
		})
	}
}

// TestTheTextOnThePalePrimaryRowIsReadable 断「淡主色底上的字读得出来」。
//
// 判据是 **WCAG 正文 AA 的 4.5:1**（浅色下正文 16 / 副题 14，都是普通字号）。
//
// ⚠️ **量的是结构体里的那几个数，不是画面上的像素**：字是抗锯齿画的，画面上
// 量的那个「最深的像素」是字色和底色的混合，算出来的对比度会**偏低于**真实值
// ——那是量法的问题，不是界面的问题。底色那一层是实心的（没有抗锯齿），所以
// 底色可以量像素，字色不行（`queue_playing_test.go` 里那条量的是底）。
//
// ⚠️ **主色是用户可设的**：所以换几个色相都不同的主色再算一遍——写死一个绿的
// 话，用户挑了别的色这一条就会红。
func TestTheTextOnThePalePrimaryRowIsReadable(t *testing.T) {
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

				// 歌名：主色当字，按**那一层淡底**算。
				if r := ink.Ratio(playingRowText(theme), bg); r < 4.5 {
					t.Errorf("%s 主色 %s：歌名 %v 压淡底 %v 只有 %.2f:1（< 4.5）",
						name, hex, playingRowText(theme), bg, r)
				}
				// 副题：次要色，同样按那一层淡底算（深色下原样就够）。
				if r := ink.Ratio(playlistSubtitleColor(theme, queueRowCurrent), bg); r < 4.5 {
					t.Errorf("%s 主色 %s：副题 %v 压淡底 %v 只有 %.2f:1（< 4.5）",
						name, hex, playlistSubtitleColor(theme, queueRowCurrent), bg, r)
				}
				if hex == "#1ed760" {
					t.Logf("%s：淡底 %v，歌名 %v（%.2f:1），副题 %v（%.2f:1）", name, bg,
						playingRowText(theme), ink.Ratio(playingRowText(theme), bg),
						playlistSubtitleColor(theme, queueRowCurrent),
						ink.Ratio(playlistSubtitleColor(theme, queueRowCurrent), bg))
				}
			}
		})
	}
}

// TestThePalePrimaryRowKeepsItsNumbers 钉住那几个数（默认主色，深浅两套）。
//
// 判据是**具体的数**，不是「≥某个门槛」——门槛那条在上一条里。写死是为了让
// 「主色的浓度被改了」这件事立刻红：20% → 30% 时浅色那一格会变成 #b7f0c8，
// 而这一条会指着 #d2f7df 说不对。
//
// 顺带钉住「底色是**不透明**的」：底要是半透明，屏幕上的数就是它合到面板底
// 之后的结果——那一格算出来的数和画出来的不一样，对比度也就算不准了。
func TestThePalePrimaryRowKeepsItsNumbers(t *testing.T) {
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
			// 歌名：浅色下是压深的主色（#127d38），深色下就是主色本身。
			title := playingRowText(theme)
			if dark && title != theme.Accent {
				t.Errorf("深色：歌名是 %v，want 主色本身 %v", title, theme.Accent)
			}
			if !dark && title == theme.Accent {
				t.Errorf("浅色：歌名就是主色本身 %v——它压淡底只有 1.65:1，读不出来",
					title)
			}
		})
	}
}
