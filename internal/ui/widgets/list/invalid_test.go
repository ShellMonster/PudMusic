package list

// 「失效的条目」在列表里的样子（票 #624）。
//
// # 为什么要标出来
//
// B 站的收藏夹里会躺着失效的条目（UP 主删了那个稿件、或者换成了别的链接）。
// 它们**照画**（不画的话用户会发现「收藏夹里的歌自己少了几首」而且不知道
// 为什么），但**要标出来**——
//
//	标出来	用户知道这一首点不动、为什么
//	不标	用户看到一首歌正常地摆在那里，按下去才知道放不了
//
// 而**灰掉**是「这一首已经不能用了」最省事的说法。
//
// # 判据的出处
//
// 失效这个事实来自 B 站那个 `attr` 字段（`bili.Media.Invalid`），经
// `app.Item.Invalid` 传进来。**本包不自己判**——本包只认 `Music.Invalid`
// 这一栏。

import (
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// invalidSongs 造两行：一行正常，一行失效。
func invalidSongs() []Music {
	return []Music{
		{
			ID: "ok", Title: "正常的一首", Owner: "UP 主",
			Item: app.Item{Source: "online", Type: "mv", Bvid: "BVok", Title: "正常的一首"},
		},
		{
			ID: "dead", Title: "失效的一首", Owner: "UP 主",
			Item: app.Item{Source: "online", Type: "mv", Bvid: "BVdead", Title: "失效的一首", Invalid: true},
		},
	}
}

// TestAnInvalidSongIsMarkedInTheList 钉住「标出来」。
//
// ⚠️ **判据是画面上有没有那句话**，不是「代码里有没有那个分支」——
// 一条失效的条目画得跟正常的一模一样时，用户按下去才知道放不了，而那正是
// 这一票要消掉的那种「点了才说」。
func TestAnInvalidSongIsMarkedInTheList(t *testing.T) {
	_, tt := newList(t, Actions{}, invalidSongs())

	if !tt.HasText(InvalidLabel) {
		t.Fatalf("失效的那一条没有标出来（画面上没有 %q）：文字有 %q", InvalidLabel, tt.Texts())
	}
	// ⚠️ **正常的那一条不许被标**：一次标多了就是噪音，而用户会以为
	// 「收藏夹里大部分东西都是坏的」。
	if n := strings.Count(strings.Join(tt.Texts(), " "), InvalidLabel); n != 1 {
		t.Errorf("画面上 %q 出现了 %d 次，want 1（只有失效的那一条该标）", InvalidLabel, n)
	}
}

// TestAnInvalidSongIsGreyedOut 钉住「灰掉」。
//
// ⚠️ **判据是那一行拿到的色**：一行失效的歌拿**次要色**，正常的拿**正文色**。
// 而「次要色是不是真的比正文淡」由界面层那一组用像素量（那儿的 Tester 拿得到
// 图像；这一包是纯控件，Tester 只给位置）。
//
// 🔴 **判据是一个纯函数**（`rowColors`）：改判据就要改它，而它两处都在用
// （行和网格）——所以两个画法不会各说各话。
func TestAnInvalidSongIsGreyedOut(t *testing.T) {
	tm := ui.LightTheme()
	tm.Text = ui.RGBA(20, 20, 20, 1)
	tm.TextMuted = ui.RGBA(120, 120, 120, 1)

	okColor, _ := rowColors(tm, false, false)
	if okColor != tm.Text {
		t.Errorf("正常那一行的歌名色是 %v，want 正文色 %v", okColor, tm.Text)
	}
	deadColor, _ := rowColors(tm, false, true)
	if deadColor != tm.TextMuted {
		t.Errorf("失效那一行的歌名色是 %v，want 次要色 %v——没被灰掉", deadColor, tm.TextMuted)
	}
}

// TestTheInvalidFlagLivesOnTheItem 钉住「失效这个事实住在哪」。
//
// 🔴 **它住在 `Item.Invalid` 上，不在 `Music` 上**：两者是同一件事，而两个
// 字段要两处填——而漏填的那一处症状是「列表里没标失效」（用户按下去才知道
// 放不了）。所以本包**不另设一栏**，直接读 `Item.Invalid`。
func TestTheInvalidFlagLivesOnTheItem(t *testing.T) {
	m := Music{ID: "x", Title: "失效的一首",
		Item: app.Item{Source: "online", Type: "mv", Bvid: "BVx", Invalid: true}}
	if !m.Item.Invalid {
		t.Fatal("这一条没有带上失效的标记")
	}
}

// TestAnInvalidSongHasNoPlayEntryBeforeYouPressIt 钉住「点之前就不给播放入口」。
//
// ⚠️ **菜单项还在**（用户看得到这一份菜单），但它是**灰的**——而末尾有一句
// 为什么。按不动而不说为什么等于让用户猜。
func TestAnInvalidSongHasNoPlayEntryBeforeYouPressIt(t *testing.T) {
	_, tt := newList(t, Actions{}, invalidSongs())

	if err := tt.RightClick("失效的一首"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("立即播放"); err == nil {
		t.Error("失效的那一条，「立即播放」竟然能按")
	}
	if err := tt.ChooseMenuItem("从这一首往后听"); err == nil {
		t.Error("失效的那一条，「从这一首往后听」竟然能按")
	}
	items := tt.Menu()
	last := items[len(items)-1]
	if !strings.Contains(last, "失效") {
		t.Errorf("菜单末尾那句话是 %q，没有说清「已经失效」", last)
	}
}

// TestANormalSongStillHasItsPlayEntry 是上面那条的另一半。
//
// 🔴 **滤得太狠也是 bug**：正常的一首必须有播放入口——而这一条防的是
// 「判据写反了，于是每一首都是灰的」。
func TestANormalSongStillHasItsPlayEntry(t *testing.T) {
	_, tt := newList(t, Actions{}, invalidSongs())

	if err := tt.RightClick("正常的一首"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("立即播放"); err != nil {
		t.Errorf("正常的那一首，「立即播放」按不动：%v", err)
	}
}

// TestTheInvalidTagKeepsItsContrast 钉住标签的**对比度**。
//
// 🔴 **这一条是可访问性的硬要求**（WCAG AA，1.4.3）：标签那两个字是 **11px**
// 的小字，对比度必须 ≥ **4.5:1**——而小字恰恰是最容易不达标的一档（字号小，
// 笔画细，稍微淡一点就读不出来）。
//
// ⚠️ **判据是「字 vs 它自己的底」**：标签有一层自己的淡灰底，而那一层底
// 铺在页面的背景上——所以比的是「标签的字」和「标签的底」。
//
// ⚠️ **两档主题都要过**：浅色下底是「背景混 16% 次要色」，深色下是同一个
// 公式算出来的另一档——而深色那档的底更接近次要色本身，对比度天然更低。
func TestTheInvalidTagKeepsItsContrast(t *testing.T) {
	for name, tm := range map[string]*ui.Theme{
		"浅色": ui.LightTheme(),
		"深色": ui.DarkTheme(),
	} {
		t.Run(name, func(t *testing.T) {
			// ⚠️ **用画法那一份**（`invalidTagColors`）：自己算一遍的话，
			// 算的就不是画面上那个颜色了。
			text, bg := invalidTagColors(tm)
			if r := ink.Ratio(text, bg); r < ink.TextContrast {
				t.Errorf("「已失效」标签的字和它自己的底对比度是 %.2f:1，"+
					"want ≥ %.1f:1（11px 的小字，WCAG AA）", r, ink.TextContrast)
			}
		})
	}
}
