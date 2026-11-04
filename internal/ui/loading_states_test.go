package ui

// 这一组钉的是一批**「有加载态、但画得看不见」**的地方（2026-10-27）。
//
// 每一条的错法都是静默的：不报错、不崩、不写日志，界面只是「看着不对劲」——
// 一屏封面在等的时候是一排空白、拉失败之后那块灰永远不变、切歌那一瞬间就写
// 「暂无歌词」。所以判据全是**画面上**的东西（颜色、有没有字、是不是动）。

import (
	"image"
	"sort"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shimmer"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheCoverLoadingFillIsVisibleOnALightPage 钉住「封面加载中」那一档的底色
// **在浅色页面上看得出来**。
//
// 🔴 **判据是「实打实画出来的像素」**：那一档原先是一个 `Surface` 往
// `Background` 混一半的平色（#fafafa），压在 #ffffff 的页面上**每通道只差 4**
// ——一屏 48 像素的封面在浅色下仍然是一排空白。现在它是**前景色 8%**
// （约 #ebebeb），每通道差 20。
//
// ⚠️ **取样点必须在「高光带扫不到」的位置，而且要真的画一遍**：上一版这一条
// 是拿 `shimmer.BaseAlpha` 现算一遍数学——把实现改回旧的混色它照样绿（实测过），
// 那是**假测试**。现在画一格、读像素。
//
// ⚠️ **怎么保证取样点不在高光带上**：`shimmer` 的高光是一道 35% 宽的竖带，
// 按钟从左扫到右。取**左上角往里 10 像素**那个点，然后在若干个相位上各取一次——
// 只要**有一点**落在带外，那一点就是纯底色。判据用「最暗的那一次」（带外的
// 那一次），因为高光只会把它提亮。
func TestTheCoverLoadingFillIsVisibleOnALightPage(t *testing.T) {
	b := newTestShell(t, at(400, 300), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			imgcache.Loading(c, 200, 200, imgcache.Shape{})
		}
	}))
	// 🔴 **判据不认主题色，只认「这一格和它旁边的空白页差多少」**：
	//
	// 上一版拿 `BuildTheme(设置, 浅色)` 去算页面底色——而画面上那一份是
	// `applyFrameTheme` 装的（默认主题方案把底色换成了 #f8f4ed），两者差 4~8 个
	// 通道值，于是在门槛边上翻脸（实测量到 27，真实值 48）。
	//
	// 现在两边都从**同一张图**上取：那一格里的取样点 vs 它右边那块空白页。
	b.tt.Frame()

	// ⚠️ **多取几个点、每个点扫几个相位、最后取最小值**：高光是一道 35% 宽的
	// 竖带按钟扫过，单点单次很可能正落在带上（那就量成了「带里最亮的色」）。
	// 取最小值等于「这个格子最暗的那一处、最暗的那一次」——也就是**纯底色**。
	//
	// 往里 8 稳住：避开抗锯齿的边，也避开正中那枚音符图标（它是 `TextMuted`
	// 再往背景色混 25%，比底色深，会把「差多少」量成图标和底色的差）。
	// 那一格是 200×200，画在 (0,0)。页面底从**窗口里**取——(250,150) 是那一格
	// 右边那块空白页（别取到 300,300：那已经在窗口外，读回来是全透明，差值算出
	// 来是个假数）。
	const pageX, pageY = 250, 150

	// 🔴 **判据是「整格像素里和页面底色的距离的中位数」**：
	//
	// 扫光是一道 35% 宽的竖带在走，**任何单点都可能正落在带上**（实测最小差
	// 21 = 带上最亮那一处）；而正中那枚音符又把中间那一块拉深。取**中位数**
	// 等于「这个格子里占面积最大的那个色」，也就是**底色本身**——它不随相位变
	// （实测六个相位都是 48）。
	b.tt.Frame()
	base := b.tt.Image().RGBAAt(pageX, pageY)
	page := [3]uint8{base.R, base.G, base.B}

	img := b.tt.Image()
	var ds []int
	for y := 4; y < 196; y++ {
		for x := 4; x < 196; x++ {
			// 跳过正中那枚音符（`placeholderIconRatio * 200 = 80` 见方那一块）。
			if x > 60 && x < 140 && y > 60 && y < 140 {
				continue
			}
			p := img.RGBAAt(x, y)
			ds = append(ds, absChannel(p.R, page[0])+
				absChannel(p.G, page[1])+absChannel(p.B, page[2]))
		}
	}
	sort.Ints(ds)
	worst := ds[len(ds)/2]
	// 门槛 40 → 30（「深夜唱片行」§0′）：占位块的底就是原型的 `--surface-2`，压在 `--bg` 上
	// 浅色只差 35、深色差 31；原型的骨架就是这个对比度，不为这条测试把底色调深。
	if worst < 30 {
		t.Fatalf("封面加载中那一档压在页面底色上**最暗**只差 %d（门槛 30）——"+
			"浅色下那等于一块空白，用户分不出「还在加载」和「压根没有图」", worst)
	}
}

// absChannel 是两个通道值的绝对差。
func absChannel(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// TestTheCoverLoadingCellShimmers 钉住「封面加载中」那一档**会动**。
//
// 🔴 **判据是两帧的像素**：扫光是一道从左往右走的高光，同一格隔一段时间取两次
// 样，颜色必须不同。不闪的话那一档只是一块灰——而一屏几十块灰看起来就是
// 「这一页坏了」。
func TestTheCoverLoadingCellShimmers(t *testing.T) {
	b := newTestShell(t, at(400, 300), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			imgcache.Loading(c, 200, 200, imgcache.Shape{})
		}
	}))
	b.tt.Frame()
	before := b.tt.Image()
	b.tt.Advance(shimmer.Period / 2)
	b.tt.Frame()
	after := b.tt.Image()

	if changedPixelsIn(before, after, 200, 200) == 0 {
		t.Fatal("隔了半个周期那一格一个像素都没变——「加载中」没有扫光，" +
			"一屏几十张封面在等的时候看起来是一排不动的灰块")
	}
}

// changedPixelsIn 数两张图里左上角 w×h 那一块有多少像素不一样。
func changedPixelsIn(a, b *image.RGBA, w, h int) int {
	n := 0
	bounds := a.Bounds()
	for y := range h {
		for x := range w {
			if x >= bounds.Dx() || y >= bounds.Dy() {
				continue
			}
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// TestTheCollectionHeaderDoesNotClaimZeroMedia 钉住收藏夹页头**元信息没回来时
// 不写「0 条视频」**。
//
// 🔴 **那个 0 是假数**：`CollectionInfo` 在 `!Loaded` 时是零值，照写就是
// 「公开收藏夹 • 0 条视频」——而真实值可能是几百。用户会相信它，它看起来也
// 完全正常。和 UP 空间那边「粉丝数 0」是同一种错。
func TestTheCollectionHeaderDoesNotClaimZeroMedia(t *testing.T) {
	b := newTestShell(t, blockedBiliOpts(t, true)...)
	s, tt := b.s, b.tt
	s.navigate(collectionPathPrefix + "12345")
	tt.Frame()
	tt.Frame()

	for _, line := range tt.Texts() {
		if hasSuffix(line, "• 0 条视频") {
			t.Fatalf("元信息还没回来，页头就写了「%s」——那是个假数"+
				"（真实条数可能是几百），用户会相信它", line)
		}
	}
}

// hasSuffix 是 strings.HasSuffix 的本地版（这一组只 import 少数几个包）。
func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// TestTheCollectionOwnerRowKeepsItsSlot 钉住收藏夹页头的创建者那一行在元信息
// 没回来时**留位**（摆骨架），而不是整行不画。
//
// 🔴 **不留位的后果**：数据到了那一行凭空冒出来，头图区高度跳一截，下面的
// 操作条和列表跟着往下走——用户看到的是「这一页自己动了一下」。
func TestTheCollectionOwnerRowKeepsItsSlot(t *testing.T) {
	b := newTestShell(t, blockedBiliOpts(t, true)...)
	s, tt := b.s, b.tt
	s.navigate(collectionPathPrefix + "12345")
	tt.Frame()
	tt.Frame()

	if !tt.HasText(collectionOwnerLoadingText) {
		t.Fatalf("创建者那一行没留位（找不到 %q）——数据到了它会凭空冒出来，"+
			"把下面的内容顶下去；文字有 %q", collectionOwnerLoadingText, tt.Texts())
	}
}

// TestTheShimmerFlagIsTheWindowsOwnFlag 钉住「看不见的窗口不要扫光帧」那道开关
// **两个包装的是同一个指针**。
//
// 🔴 **各装一份的后果**：波形停了、骨架屏还在每帧要帧——一个看不见的窗口
// 照样在烧电（票 #156 治的就是那个）。
func TestTheShimmerFlagIsTheWindowsOwnFlag(t *testing.T) {
	b := newTestShell(t, at(800, 600))
	if got, want := shimmer.VisibleFlag(), b.s.visibleFlag(); got != want {
		t.Fatal("`shimmer` 装的不是这一扇窗那一个「看不看得见」开关——" +
			"两个包各拿一份的话，失焦之后一边停一边还在烧电")
	}
}
