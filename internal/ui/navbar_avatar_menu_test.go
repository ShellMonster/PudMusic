// 头像下面那一层下拉的测试（票 #363）。
//
// # 这一层是**自绘的浮层**，不是系统菜单
//
// 原来那一层走的是 MyGo 的 `Element.Menu`（系统菜单）。这一票把它换成自绘
// 的浮层，理由有两条（见 navbar_avatar.go 的文件头）：系统菜单的样式跟当前
// 的设计规范不搭；菜单项要能带图标，而系统菜单那一格放不下图标。
//
// 换掉之后，系统菜单那四件好事**一件都不能丢**（当初用系统菜单就是为了
// 它们）：点外面关、Esc 关、键盘能上下走、选中项有反馈。这一组测试量的就是
// 这四件，加上「六项都在、顺序照上游」和「每一项都按得动」。
//
// # 量的是**画面**，不是状态
//
// 「亮着的是哪一项」「每一项前面有没有图标」「浮层画在哪儿」这几条量的是
// 像素——它们是这一票真正要改的东西（画面上摆的是什么），而状态字段只
// 用来定位，不用来当判据。
package ui

import (
	"image/color"
	"slices"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
//  helpers
// ─────────────────────────────────────────────────────────────────────────────

// openAvatarMenu 点开头像下面那一层下拉，并且确认它真的开了。
func openAvatarMenu(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click(avatarLabel); err != nil {
		t.Fatalf("点不到头像：%v，文字有 %q", err, tt.Texts())
	}
	if _, ok := tt.Find(avatarMenuLabel); !ok {
		t.Fatalf("头像下面没有浮层，文字有 %q", tt.Texts())
	}
}

// avatarMenuLabels 读浮层里那几项的名字，按画面上的顺序。
//
// ⚠️ **从浮层自己的名字往后数**：浮层画在覆盖层里（它比主树晚画），所以
// 浮层里的字是这一帧最后那几个字。而「设置」在侧栏里也有一项，按名字找会
// 撞车——`tt.Find` 返回的是先建的那个。从浮层那个名字往后数就撞不上了。
func avatarMenuLabels(tt *mygoui.Tester) []string {
	texts := tt.Texts()
	at := slices.Index(texts, avatarMenuLabel)
	if at < 0 {
		return nil
	}
	return texts[at+1:]
}

// avatarMenuIndexOf 报某一项在下拉里的序号（从 0 起）。
//
// 找不到就 -1——调用方拿它去点一项，-1 会让那条测试红，而不是**点错一项**
// （点错一项照样绿，而它验的根本不是那一项）。
func avatarMenuIndexOf(label string) int {
	for i, item := range avatarMenuItems {
		if item.label == label {
			return i
		}
	}
	return -1
}

// avatarMenuPanel 找下拉那块板子在画面上的位置。
func avatarMenuPanel(tt *mygoui.Tester) (mygoui.Rect, bool) { return tt.Find(avatarMenuLabel) }

// avatarMenuRow 算下拉里第 i 项在画面上的位置。
//
// ⚠️ **按位置算，不按名字找**：「设置」在侧栏里也有一项（DESIGN §九「侧栏多
// 一条设置」），而无头 Tester 的 `Find` 返回的是**先建的那个**——按名字点
// 会点到侧栏上去，而侧栏那一项做的事也一样（都是去设置页），于是测试照样绿，
// 而它验的根本不是下拉里的那一项。
//
// 位置是从浮层那块板子算出来的：描边 1 + 内边距 4 → 内容从 5 开始，
// 每一项 36 高。
func avatarMenuRow(tt *mygoui.Tester, i int) (mygoui.Rect, bool) {
	panel, ok := avatarMenuPanel(tt)
	if !ok {
		return mygoui.Rect{}, false
	}
	const inset = 5
	return mygoui.Rect{
		X: panel.X + inset,
		Y: panel.Y + inset + float32(i)*avatarMenuRowH,
		W: panel.W - 2*inset,
		H: avatarMenuRowH,
	}, true
}

// clickAvatarMenuItem 点下拉里的第 i 项。
func clickAvatarMenuItem(t *testing.T, tt *mygoui.Tester, i int) {
	t.Helper()
	if i < 0 || i >= len(avatarMenuItems) {
		t.Fatalf("下拉里没有第 %d 项", i+1)
	}
	r, ok := avatarMenuRow(tt, i)
	if !ok {
		t.Fatalf("画面上找不到下拉里第 %d 项", i+1)
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
}

// avatarMenuTheme 读这一帧真正用的那份主题。
//
// ⚠️ **读的是缓存里的那一份**（`themeCache.base`）：主题是每帧开头装上去的
// （铁律一），而测试要算的期望颜色必须和画面上那一帧用的是同一份。
func avatarMenuTheme(s *Shell) *mygoui.Theme { return s.theme.base }

// toRGBA 把主题的颜色换成图片上的那种。
func toRGBA(c mygoui.Color) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}

// avatarMenuRowBG 读下拉里第 i 项**左边那一小块**的颜色。
//
// ⚠️ **取的是行内最左边那一列**（面板描边 1 + 内边距 4 之后、行内左边距 12
// 之前那一段）：那里既没有字也没有图标，量到的就是**那一行的底色**。
func avatarMenuRowBG(tt *mygoui.Tester, i int) (color.RGBA, bool) {
	panel, ok := avatarMenuPanel(tt)
	if !ok {
		return color.RGBA{}, false
	}
	row, ok := avatarMenuRow(tt, i)
	if !ok {
		return color.RGBA{}, false
	}
	img := tt.Image()
	return img.RGBAAt(int(panel.X)+6, int(row.Y+row.H/2)), true
}

// ─────────────────────────────────────────────────────────────────────────────
//  关掉的那几件
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAvatarMenuClosesOnEscape 验 Esc 关得掉这一层。
//
// ⚠️ 这是**当初用系统菜单的理由之一**（系统菜单自带 Esc 关），换成自绘浮层
// 之后它不能丢——而它是框架那一层自己管的（`ui.PopoverBase` → `popover`：
// `panel.OverlayShortcut(0, KeyEscape)`）。
func TestTheAvatarMenuClosesOnEscape(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	if _, ok := tt.Find(avatarMenuLabel); ok {
		t.Errorf("按了 Esc 之后浮层还开着，文字有 %q", tt.Texts())
	}
}

// TestTheAvatarMenuClosesWhenClickingOutside 验点外面关得掉这一层。
//
// ⚠️ 这也是**当初用系统菜单的理由之一**。自己拼浮层的话这一条得自己实现，
// 漏了就是「点外面也没反应，面板永远开着」。
func TestTheAvatarMenuClosesWhenClickingOutside(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	// 点内容区中间那块空白（那儿什么都没有）。
	tt.ClickAt(600, 400)
	tt.Frame()

	if _, ok := tt.Find(avatarMenuLabel); ok {
		t.Errorf("点了外面之后浮层还开着，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  键盘那一半
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAvatarMenuWalksWithTheArrowKeys 验上下键走得了，回车选得了。
//
// ⚠️ **这一条量的是「画面上亮着的是哪一项」**：状态字段（下标）只用来定位，
// 判据是那一项**真的铺了淡底**——「下标变了而底没跟着变」是这一类改动最
// 常见的坏法（底是照着一个已经过期的下标铺的）。
//
// ⚠️ **刚打开时什么也没选中**（上游 HeroUI 就是这样）：浮层亮着第一项的话，
// 回车直接进扫码登录页——那一下用户根本不是想登录。
func TestTheAvatarMenuWalksWithTheArrowKeys(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	th := avatarMenuTheme(s)
	lit := toRGBA(accentSoft(th))
	plain := toRGBA(s.cardBackground(th))

	// 刚打开的时候**什么也没亮**（上游 HeroUI 打开时也不高亮任何一项）。
	for i := range avatarMenuItems {
		if got, _ := avatarMenuRowBG(tt, i); got != plain {
			t.Fatalf("刚打开时第 %d 项的底是 %v，want 普通底 %v（刚打开不该高亮任何一项）",
				i+1, got, plain)
		}
	}

	// 按一下 ↓：亮的是**第一项**。
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	if got, _ := avatarMenuRowBG(tt, 0); got != lit {
		t.Errorf("按了一下 ↓ 之后第一项的底是 %v，want 亮底 %v", got, lit)
	}
	if got, _ := avatarMenuRowBG(tt, 1); got != plain {
		t.Errorf("按了一下 ↓ 之后第二项的底是 %v，want 普通底 %v", got, plain)
	}

	// 再按一下 ↓：往下走一格。
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	if got, _ := avatarMenuRowBG(tt, 1); got != lit {
		t.Errorf("按了两下 ↓ 之后第二项的底是 %v，want 亮底 %v", got, lit)
	}
	if got, _ := avatarMenuRowBG(tt, 0); got != plain {
		t.Errorf("按了两下 ↓ 之后第一项还亮着（底 %v），want 普通底 %v", got, plain)
	}

	// 按一下 ↑：回到第一项。
	tt.Key(0, mygoui.KeyUp)
	tt.Frame()
	if got, _ := avatarMenuRowBG(tt, 0); got != lit {
		t.Errorf("按了一下 ↑ 之后第一项的底是 %v，want 亮底 %v", got, lit)
	}
}

// TestTheAvatarMenuWalksWhileThePointerRestsOnAnItem 验**指针停在菜单项上**的
// 时候方向键照样走得了。
//
// ⚠️ **这是这一票评审抓出来的那一条**：指针和键盘共用「当前是第几项」这一个
// 下标，而悬停**每一帧**都改它——于是键盘刚走的那一格被指针改回去，
// 「键盘能上下走」只在指针不在菜单项上的时候才成立。
//
// 判据是**画面**：按 ↓ 之后第二项得铺上亮底，而第一项（指针还停在那儿）得
// 回到普通底。改之前的症状是「按了 ↓ 之后画面逐像素没有变化」。
func TestTheAvatarMenuWalksWhileThePointerRestsOnAnItem(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	th := avatarMenuTheme(s)
	lit := toRGBA(accentSoft(th))
	plain := toRGBA(s.cardBackground(th))

	// 指针挪到第一项上（**停在那儿不动**）。
	row, ok := avatarMenuRow(tt, 0)
	if !ok {
		t.Fatal("画面上找不到下拉里第一项")
	}
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if got, _ := avatarMenuRowBG(tt, 0); got != lit {
		t.Fatalf("指针停在第一项上时它的底是 %v，want 亮底 %v", got, lit)
	}

	// 指针**不动**，按 ↓：亮的那一项要往下走。
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	if got, _ := avatarMenuRowBG(tt, 1); got != lit {
		t.Errorf("指针停在第一项上按 ↓ 之后第二项的底是 %v，want 亮底 %v——"+
			"悬停把键盘走的那一格改回去了", got, lit)
	}
	if got, _ := avatarMenuRowBG(tt, 0); got != plain {
		t.Errorf("指针停在第一项上按 ↓ 之后第一项还亮着（底 %v），want 普通底 %v——"+
			"画面上两项同时亮着", got, plain)
	}

	// ⚠️ **指针挪到第三项上时，指针说了算**：它一动，亮的那一项就跟过去。
	row2, ok := avatarMenuRow(tt, 2)
	if !ok {
		t.Fatal("画面上找不到下拉里第三项")
	}
	tt.Move(row2.X+row2.W/2, row2.Y+row2.H/2)
	tt.Frame()
	if got, _ := avatarMenuRowBG(tt, 2); got != lit {
		t.Errorf("指针挪到第三项上之后它的底是 %v，want 亮底 %v", got, lit)
	}
	if got, _ := avatarMenuRowBG(tt, 1); got != plain {
		t.Errorf("指针挪到第三项上之后第二项还亮着（底 %v），want 普通底 %v", got, plain)
	}
}

// TestTheAvatarMenuSelectsWithTheKeyboard 验回车选的是**亮着的那一项**。
//
// ⚠️ 这一条钉的是「键盘走到哪儿、回车就做哪一项」：走神了做另一项
// （比如走到「退出登录」却把登录给做了）是这一类改动最贵的那种错。
//
// ⚠️ **浮层刚打开时什么也没选中**（上游 HeroUI 就是这样），所以这里要先按
// 两下 ↓ 走到第二项「个人资料」。
func TestTheAvatarMenuSelectsWithTheKeyboard(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	// ↓ ↓ 走到第二项「个人资料」。
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	// 没登录时那一项会说一句「还没登录」——这一条要的是「走对了那一项」。
	tt.Key(0, mygoui.KeyEnter)
	tt.Frame()

	if got := toastTexts(s.visibleToasts()); len(got) != 1 {
		t.Fatalf("回车之后有 %d 条提示 %q，want 1 条（走的是「个人资料」）", len(got), got)
	}
	if _, ok := tt.Find(avatarMenuLabel); ok {
		t.Error("回车之后浮层还开着")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  样子
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAvatarMenuHangsUnderTheAvatar 验浮层锚在头像**下面**，而且整个在窗口里。
//
// ⚠️ 头像在顶栏最右端：面板要是挂出去就有半截在窗口外面（上游 HeroUI 那个
// 下拉是靠 shift 摆回来的，我们锚在右端）。
//
// ⚠️ **这一条量的是「画面上的结果」，不是「我们选了哪一边」**：框架自己会
// 「翻转」——面板挂在锚点那一边放不下的时候，它翻到另一边去
//（`mygo/ui/layout.go` 的 `alongTarget`）。所以两种锚法在窄窗口上画出来是
// 一模一样的，而这一条要管的是**用户看见的那个结果**：面板在头像下面、
// 右沿和头像对齐、整个在窗口里。
func TestTheAvatarMenuHangsUnderTheAvatar(t *testing.T) {
	b := newTestShell(t, at(1600, 800))
	tt := b.tt
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	avatar, ok := tt.Find(avatarLabel)
	if !ok {
		t.Fatalf("画面上找不到%q", avatarLabel)
	}
	panel, ok := avatarMenuPanel(tt)
	if !ok {
		t.Fatal("画面上找不到那一层浮层")
	}
	if panel.Y < avatar.Y+avatar.H {
		t.Errorf("浮层顶边 %v 在头像底边 %v 之上——它不是挂在下面的", panel.Y, avatar.Y+avatar.H)
	}
	right, want := panel.X+panel.W, avatar.X+avatar.W
	if right < want-1 || right > want+1 {
		t.Errorf("浮层右沿是 %v，头像右沿是 %v——两者该对齐", right, want)
	}
	// 而且它整个在窗口里（1600 宽那一个）。
	if panel.X < 0 || panel.X+panel.W > 1600 {
		t.Errorf("浮层挂在窗口外面：%v", panel)
	}
}

// TestTheAvatarMenuPanelLooksLikeACard 验浮层是**卡片那一档**的料。
//
// ⚠️ **量的是底色**：底色是 `surface1`（方案里）/ 控件层（经典），走的是
// `Shell.cardBackground`——设计规范 §零 说的「浮层底色 surface1、圆角
// radiusLg、阴影 shadow.Modal」里的第一条。底色写死的话，这一条会红。
func TestTheAvatarMenuPanelLooksLikeACard(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	panel, ok := avatarMenuPanel(tt)
	if !ok {
		t.Fatal("画面上找不到那一层浮层")
	}
	want := toRGBA(s.cardBackground(avatarMenuTheme(s)))
	// 取浮层**上沿中间**那一个点：那里没有圆角、也没有描边以外的任何东西。
	got := tt.Image().RGBAAt(int(panel.X+panel.W/2), int(panel.Y)+2)
	if got != want {
		t.Errorf("浮层的底色是 %v，want %v（surface1 那一档）", got, want)
	}
	if w := panel.W; w != avatarMenuW {
		t.Errorf("浮层宽 %v，want %v", w, avatarMenuW)
	}
}

// TestEveryAvatarMenuItemHasAnIcon 验每一项前面都画着图标。
//
// ⚠️ **量的是「图标那一格里有没有画上东西」**：上游那六项每一项都带一枚
// 18px 的图标（`startContent`），而系统菜单那一格放不下图标——这正是这一票
// 换掉它的理由之一，所以「图标在不在」得钉住。
//
// 量的地方是行内最左边那一格（行内左边距 12 之后那一枚 18 见方）：那里
// 除了图标没有别的东西。
func TestEveryAvatarMenuItemHasAnIcon(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	img := tt.Image()
	for i := range avatarMenuItems {
		row, ok := avatarMenuRow(tt, i)
		if !ok {
			t.Fatalf("画面上找不到下拉里第 %d 项", i+1)
		}
		bg, ok := avatarMenuRowBG(tt, i)
		if !ok {
			t.Fatalf("读不到第 %d 项的底色", i+1)
		}
		// 图标那一格：行内左边距 12 之后，18 见方，垂直居中。
		x0 := int(row.X) + 12
		y0 := int(row.Y + (row.H-avatarMenuIconSize)/2)
		drawn := 0
		for y := y0; y < y0+int(avatarMenuIconSize); y++ {
			for x := x0; x < x0+int(avatarMenuIconSize); x++ {
				if img.RGBAAt(x, y) != bg {
					drawn++
				}
			}
		}
		if drawn == 0 {
			t.Errorf("第 %d 项（%q）前面那一格里什么都没画——上游那六项每一项都带一枚图标",
				i+1, avatarMenuItems[i].label)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  退出登录那一项
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDangerItemKeepsItsContrastOnTheHighlightedRow 验「退出登录」那一项
// **被高亮的时候**字还读得出来。
//
// ⚠️ **底换了，字要按这层底现算**（仓库的规矩，见 `flatbutton.go` 的
// `flatCurrentText`）：同一个危险字压在**面板底**上是 4.97~5.75:1，而压在
// 当前项那层 `accentSoft` 淡底上只有 **4.34~4.45:1**——差一点点到不了 4.5。
// 而那一档正是「键盘走到退出登录」或者「鼠标悬停它」的那一瞬。
//
// ⚠️ **四个方案 × 深浅两套都要过**：那一层淡底是每个方案自己的
// `accentSoft`（浅色 14%、深色 18%），对比度也跟着方案走。
func TestTheDangerItemKeepsItsContrastOnTheHighlightedRow(t *testing.T) {
	danger := avatarMenuItem{label: "退出登录", danger: true}
	for _, preset := range []app.ThemePreset{
		app.ThemePresetClassic, app.ThemePresetWarmPaper,
		app.ThemePresetDuskAmber, app.ThemePresetSoftPeach,
	} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			st := app.DefaultSettings("")
			st.ThemePreset = preset
			st.ThemeMode = app.ThemeModeLight
			if dark {
				st.ThemeMode = app.ThemeModeDark
			}
			tt, s, _, _ := shellOnSettings(t, st)
			th := avatarMenuTheme(s)
			bg := accentSoft(th)
			col := avatarMenuTextColor(th, danger, bg, true)
			if got := ink.Ratio(col, bg); got < 4.5 {
				t.Errorf("%s %s：当前那一项的危险字在淡底上是 %.2f:1，want ≥4.5——"+
					"底换了，字要按这层底现算", preset, shade, got)
			}
			_ = tt
		}
	}
}

// TestTheAvatarMenuAsksBeforeLoggingOut 验「退出登录」先问一句。
//
// ⚠️ **取消就什么都不做**：退登是不可逆的那一下，而确认框是仓库现成的那一道
// （`confirmModal`）。取消 / Esc / 点外面都算「取消」。
func TestTheAvatarMenuAsksBeforeLoggingOut(t *testing.T) {
	s, tt := newLoggedInShell(t, "2")
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	clickAvatarMenuItem(t, tt, avatarMenuIndexOf("退出登录"))
	if !tt.HasText("确认退出登录？") {
		t.Fatalf("没有确认框，文字有 %q", tt.Texts())
	}

	// 按「取消」。
	if err := tt.Click("取消"); err != nil {
		t.Fatalf("点不到取消键：%v，文字有 %q", err, tt.Texts())
	}
	if !s.app.LoggedIn() {
		t.Error("按了取消之后还是退登了")
	}
	if tt.HasText("确认退出登录？") {
		t.Error("按了取消之后确认框还开着")
	}
}
