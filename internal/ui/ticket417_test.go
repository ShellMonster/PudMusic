package ui

// 「名字型可点击元素改成下划线」那一票（#417）的验收。
//
// # 用户说的是哪一处
//
// 2026-10-25：「那个可点击的效果的不要做成一个胶囊一样的包着那个名称的，
// 用正常的下划线就行，像 biu 里的那样」。
//
// 改之前那个名字是用**扁平按钮**画的：左右各留一段内边距（14）+ 圆角 → 名字被
// 一个**胶囊**包着；指针落上去时 `flat.feel` 再往那个胶囊上压一层半透明前景色 →
// **悬停时那块底牌才显出来**。上游 Biu 的作者名是 `hover:underline`——**没有按钮
// 壳、没有底色、没有胶囊**。
//
// # 这一组断的是「画面上的两件事」
//
//   1. **悬停时名字底下长出一道下划线**：判据是**那一档主色的像素**多了——
//      下划线画在 `ink.AccentOn(主题)` 上（见 `widgets/flat` 的 `Link`），而名字
//      自己的字色是次要色那一档，两者**不是同一个颜色**，所以数得出来。
//   2. **名字没有被一个胶囊包着**：判据是**名字左边那一段**（胶囊的左右内边距会
//      盖到那儿）在悬停前后**一模一样**。
//
// ⚠️ **判据是「像素变了没有」，不是「某个颜色是某个数」**：那要写死一个平台相关
// 的值，而这一组的每一处底色都跟着主题走。
//
// ⚠️ **反面也要断**：同一张卡片上那几枚**动作键**（赞 / 下一首播放 / 下载音频）
// 悬停时**仍然要**有那一层淡底——它们是按钮，不是名字链接。把它们一起拍平的话，
// 「哪一个是动作」就没有了，而那是票面点名要保的东西。

import (
	"image"
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// linkAccentPixels 数 box 里有多少像素是那一档主色（差得在 2 以内就算）。
//
// ⚠️ **为什么数「主色的像素」而不是「多出来的墨迹」**：下划线画在
// `ink.AccentOn(主题)` 上，而名字自己的字是次要色那一档——两者**不是同一个
// 颜色**，所以「主色的像素从 0 变成一整条」就是「下划线长出来了」，而别的什么
// （那一行的悬停底、字自己的笔画）都不会混进来。
func linkAccentPixels(img *image.RGBA, box mygoui.Rect, want color.RGBA) int {
	n := 0
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		for x := int(box.X); x < int(box.X+box.W); x++ {
			if maxDiff(img.RGBAAt(x, y), want) <= 2 {
				n++
			}
		}
	}
	return n
}

// linkBandLeft 是「字左边那一段」——胶囊的内边距会盖到那儿。
//
// ⚠️ **8 像素**：扁平按钮那份配方的左右内边距是 14（`widgets/flat` 的
// `flat.Button` 的 `t.Space(3.5)`），而这一段要落在**胶囊之内**，所以 8 < 14。
func linkBandLeft(r mygoui.Rect) mygoui.Rect {
	return mygoui.Rect{X: r.X - 8, Y: r.Y, W: 8, H: r.H}
}

// linkInkIn 数 box 里「不是底色」的像素有多少。
func linkInkIn(img *image.RGBA, box mygoui.Rect, bg color.RGBA) int {
	return inkPixels(img, box, bg)
}

// linkHoverOn 把指针停在 r 的正中间，再画一帧。
func linkHoverOn(t *testing.T, tt *mygoui.Tester, r mygoui.Rect) {
	t.Helper()
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

// assertNameLinkUnderlines 断「那个名字悬停时带一道下划线」。
func assertNameLinkUnderlines(t *testing.T, tt *mygoui.Tester, accent color.RGBA, label, what string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("%s上找不到 %q，文字有 %q", what, label, tt.Texts())
	}
	// 先把指针挪开：悬停反馈是「指针在不在上面」决定的，指针本来就在的话，
	// 「悬停前」那一帧量到的就不是常态。
	noPointer(tt)

	before := linkAccentPixels(tt.Image(), r, accent)
	linkHoverOn(t, tt, r)
	after := linkAccentPixels(tt.Image(), r, accent)
	if after <= before {
		t.Errorf("%s：指针停在 %q 上之后，那个名字底下没有长出下划线"+
			"（主色的像素 %d → %d）——上游那一处是 `hover:underline`", what, label, before, after)
	}
}

// assertNoCapsule 断「那个名字没有被一个胶囊包着」。
//
// ⚠️ **只在「指针不在那一行上」的时候量得出来**：框架给「指针压着的那一行」铺
// 一层 `t.SurfaceHover`，那一层盖满整行——列表里那一行的名字左边那一段本来就会
// 变（那是**行**的反馈，不是名字的）。所以这条只用在**行外**的地方（动态条目
// 那一行、播放条那一行）。
func assertNoCapsule(t *testing.T, tt *mygoui.Tester, bg color.RGBA, label, what string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("%s上找不到 %q，文字有 %q", what, label, tt.Texts())
	}
	noPointer(tt)
	before := linkInkIn(tt.Image(), linkBandLeft(r), bg)
	linkHoverOn(t, tt, r)
	after := linkInkIn(tt.Image(), linkBandLeft(r), bg)
	if after != before {
		t.Errorf("%s：指针停在 %q 上之后，字左边那一段变了（%d → %d 个不是底色的像素）——"+
			"那个名字被一个胶囊包着（旧画法：左右各 14 像素的内边距 + 圆角）",
			what, label, before, after)
	}
}

// TestTheDynamicAuthorNameIsALinkNotAPill 断**用户点名的那一处**：动态条目上的
// UP 主昵称。
//
// ⚠️ 这一条走的是 `dynamicCardShell`（真的 UP 空间那一页），所以那一页的主题
// 是应用自己的（暖纸）——底色是算出来的，不是画面上随便取的一个像素。
func TestTheDynamicAuthorNameIsALinkNotAPill(t *testing.T) {
	b := newUIExplore(t)
	s, tt, _ := dynamicCardShell(t, b, uiDynamic("v1", "正文"))
	th := BuildTheme(s.app.SettingsSnapshot(), false)
	accent := flatColor(ink.AccentOn(th))
	assertNameLinkUnderlines(t, tt, accent, "作者v1", "动态条目")
	assertNoCapsule(t, tt, flatColor(th.Background), "作者v1", "动态条目")
}

// TestThePlaybarOwnerNameIsALinkNotAPill 断播放条上那个 UP 名。
//
// ⚠️ **这里自己装主题**（`applyFrameTheme`）：只画播放条的外壳绕开了 `viewMain`，
// 而主题是 `viewMain` 开头那一步装的——不装的话量的是框架默认那份（白底），
// 而下划线的颜色是按应用的主题算的。
func TestThePlaybarOwnerNameIsALinkNotAPill(t *testing.T) {
	tt, s := playbarLinkTester(t, app.State{
		HasTrack: true, Title: "一首歌", Artist: "某个 UP 主", ArtistMid: 12345,
	})
	th := BuildTheme(s.app.SettingsSnapshot(), false)
	accent := flatColor(ink.AccentOn(th))
	assertNameLinkUnderlines(t, tt, accent, "某个 UP 主", "播放条")
	assertNoCapsule(t, tt, flatColor(th.Background), "某个 UP 主", "播放条")
}

// TestTheListOwnerNameIsALinkNotAPill 断列表行里那个 UP 名。
//
// ⚠️ **这里只断「有下划线」，不断「没有胶囊」**：那一行的左边盖着**行**的悬停底
// （框架给「指针压着的那一行」铺的那一层），量不出名字自己有没有胶囊。
//
// ⚠️ 而且旧画法那一处**本来就没有胶囊**：`ownerButton` 给的是
// `Padding(0, 0)`，胶囊正好等于字自己的盒子，看不出形状——它那会儿的悬停反馈
// 是「一层灰蒙蒙的前景色盖在字上」。改完是「一道下划线」。
func TestTheListOwnerNameIsALinkNotAPill(t *testing.T) {
	songs := homeSongs{
		tabs: [homeTabCount][]list.Music{0: homeSongsWithOwnerMids("a", 3)},
	}
	s, tt := newHome(t, songs)
	th := BuildTheme(s.app.SettingsSnapshot(), false)
	accent := flatColor(ink.AccentOn(th))
	assertNameLinkUnderlines(t, tt, accent, "a 的 UP 主 2", "列表行")
}

// TestTheActionButtonsAreStillButtons 断**反面**：同一张卡片上那几枚动作键
// 悬停时**仍然有**那一层淡底——它们是按钮，不是名字链接。
//
// ⚠️ **这是和上面那一组对着断的**：只断「名字变成下划线了」的话，一个把所有
// 可点元素一起拍平的实现照样全绿——而那正好是票面点名不要的（「按钮该是按钮
// 还是按钮」）。
func TestTheActionButtonsAreStillButtons(t *testing.T) {
	b := newUIExplore(t)
	item := uiDynamicArchive("v1", "BV1play")
	setLike(item, 12, false)
	s, tt, _ := dynamicCardShell(t, b, item)
	bg := flatColor(BuildTheme(s.app.SettingsSnapshot(), false).Background)

	r, ok := tt.Find("赞 12")
	if !ok {
		t.Fatalf("画面上没有「赞 12」，文字有 %q", tt.Texts())
	}
	noPointer(tt)
	before := linkInkIn(tt.Image(), linkBandLeft(r), bg)
	linkHoverOn(t, tt, r)
	after := linkInkIn(tt.Image(), linkBandLeft(r), bg)
	if after == before {
		t.Errorf("指针停在「赞 12」上之后，那一枚字键周围一点变化都没有（%d → %d）——"+
			"它还是一枚**按钮**，悬停要有反馈（扁平配方压一层半透明前景色）", before, after)
	}
}

// TestTheNameLinkIsKeyboardReachable 断「键盘可达」没丢。
//
// ⚠️ **下划线只是视觉**：命中区、无障碍名字、键盘可达三样都得还在。这一条断的
// 是最后那一样——Tab 能走到那个名字上，回车能按下去（上游那一套是 `<a>`，键盘
// 一样能走）。
func TestTheNameLinkIsKeyboardReachable(t *testing.T) {
	tt, s := playbarLinkTester(t, app.State{
		HasTrack: true, Title: "一首歌", Artist: "某个 UP 主", ArtistMid: 12345,
	})
	// Tab 走到那个名字上（播放条上能拿焦点的东西有好几个，所以多按几下）。
	found := false
	for range 40 {
		tt.Key(0, mygoui.KeyTab)
		tt.Frame()
		if tt.Focused("某个 UP 主") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("按了 40 次 Tab 也没走到那个 UP 名上——它是**名字型链接**，键盘得能走到")
	}
	tt.Key(0, mygoui.KeyEnter)
	tt.Frame()
	want := upPathOf(12345)
	if got := s.router.Path(); got != want {
		t.Errorf("键盘按下去之后路由是 %q，want %q", got, want)
	}
}

// TestTheUnderlineColorFollowsTheTheme 断「下划线的颜色没有写死」。
//
// 判据是**画面上那道线的颜色**等于 `ink.AccentOn(主题)`——那一档是**从当前主色
// 现算**的（主色是用户可设的），深浅两套都 ≥4.5:1。写死一个绿的话这一条就红。
func TestTheUnderlineColorFollowsTheTheme(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s := playbarLinkTester(t, app.State{
				HasTrack: true, Title: "一首歌", Artist: "某个 UP 主", ArtistMid: 12345,
			})
			if dark {
				tt.SetDark(true)
				tt.Frame()
			}
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			want := flatColor(ink.AccentOn(th))

			r, ok := tt.Find("某个 UP 主")
			if !ok {
				t.Fatalf("找不到 UP 名，文字有 %q", tt.Texts())
			}
			noPointer(tt)
			linkHoverOn(t, tt, r)

			// 那道线是**一整条实线**（`OpFill`），所以画面上一定有像素**正好**是
			// 那一档主色——找找有没有。
			if linkAccentPixels(tt.Image(), r, want) == 0 {
				t.Errorf("那道下划线的颜色不是主题算出来的那一档：画面上找不到一个"+
					"正好是 `ink.AccentOn` 给的 %v 的像素", want)
			}
			// 深浅两套都要读得出来。
			if ratio := ink.Ratio(ink.AccentOn(th), th.Background); ratio < 4.5 {
				t.Errorf("下划线那一档与底色的对比度只有 %.2f:1（< 4.5）", ratio)
			}
		})
	}
}

// playbarLinkTester 造一个只画播放条的外壳，**并且装上应用自己的主题**。
//
// ⚠️ **主题要自己装**：`applyFrameTheme` 是在 `viewMain` 的开头调的，而这里换掉
// 了整支 view——不装的话这一帧用的是框架默认那份（白底 / 灰字），量出来的
// 「底色」和下划线的颜色都对不上应用真正在用的那一套。
func playbarLinkTester(t *testing.T, st app.State) (*mygoui.Tester, *Shell) {
	t.Helper()
	b := newTestShell(t, at(1200, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.applyFrameTheme(c)
			s.viewPlayBar(c, st)
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
}
