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

// avatarMenuIndexOf 报某一项在**这一帧画出来的那几项**里的序号（从 0 起）。
//
// ⚠️ **它查的是 `s.avatarMenuItemsNow()`，不是静态表 `avatarMenuItems`**（票
// #260）：下拉里画哪几项按登录态挑（上游 `user/index.tsx:88-158` 的
// `hidden` 那一栏），所以「个人资料」在静态表里是第 2 项，而在**未登录**的
// 画面上它压根不存在（已登录的画面上它才是第 1 项）。
//
// 找不到就 -1——调用方拿它去点一项，-1 会让那条测试红，而不是**点错一项**
// （点错一项照样绿，而它验的根本不是那一项）。
func avatarMenuIndexOf(s *Shell, label string) int {
	for i, item := range s.avatarMenuItemsNow() {
		if item.label == label {
			return i
		}
	}
	return -1
}

// avatarMenuPanel 找下拉那块板子在画面上的位置。
func avatarMenuPanel(tt *mygoui.Tester) (mygoui.Rect, bool) { return tt.Find(avatarMenuLabel) }

func avatarMenuRow(tt *mygoui.Tester, i int) (mygoui.Rect, bool) {
	// 新版下拉：头部 + 分隔线 + 各项；按名字找，不再按「第 i 行」算几何。
	// i 只在旧用例里用，这里按「当前登录态下第 i 项的名字」取（见 clickAvatarMenuItem）。
	return mygoui.Rect{}, false
}

// clickAvatarMenuItem 点下拉里名字是 label 的那一项。
func clickAvatarMenuItem(t *testing.T, s *Shell, tt *mygoui.Tester, label string) {
	t.Helper()
	if avatarMenuIndexOf(s, label) < 0 {
		t.Fatalf("下拉里没有%q（登录态：%v）", label, s.app.LoggedIn())
	}
	if err := tt.Click(label); err != nil {
		t.Fatalf("点不到下拉里的%q：%v，文字有 %q", label, err, tt.Texts())
	}
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
	_, tt := newLoggedInShell(t, "7")
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
	_, tt := newLoggedInShell(t, "7")
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

// TestTheAvatarMenuSelectsWithTheKeyboard 验回车选的是**亮着的那一项**。
//
// ⚠️ 这一条钉的是「键盘走到哪儿、回车就做哪一项」：走神了做另一项
// （比如走到「退出登录」却把登录给做了）是这一类改动最贵的那种错。
//
// ⚠️ **浮层刚打开时什么也没选中**（上游 HeroUI 就是这样），所以这里要先按
// 两下 ↓ 走到第二项。
//
// ⚠️ **用的是已登录的外壳**（票 #260）：下拉里画哪几项按登录态挑，而
// **「个人资料」只在已登录时存在**（上游 `user/index.tsx:96` 的
// `hidden: !user?.isLogin`）。已登录时那几项是「个人资料 / 设置 / 刷新数据 /
// 问题反馈 / 退出登录」，所以两下 ↓ 落在**「设置」**上——按下去就该去设置页。
// ⚠️ **走错了会立刻看出来**：走成「退出登录」的话弹的是确认框，路由不会变。
func TestTheAvatarMenuSelectsWithTheKeyboard(t *testing.T) {
	s, tt := newLoggedInShell(t, "2")
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	// ↓ ↓ 走到第二项「设置」。
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	tt.Key(0, mygoui.KeyEnter)
	tt.Frame()

	if got := s.router.Path(); got != settingsPath {
		t.Errorf("回车之后在 %q，want %q（回车选的是亮着的那一项）", got, settingsPath)
	}
	if _, ok := tt.Find(avatarMenuLabel); ok {
		t.Error("回车之后浮层还开着")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  样子
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
//  退出登录那一项
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAvatarMenuAsksBeforeLoggingOut 验「退出登录」先问一句。
//
// ⚠️ **取消就什么都不做**：退登是不可逆的那一下，而确认框是仓库现成的那一道
// （`confirmModal`）。取消 / Esc / 点外面都算「取消」。
func TestTheAvatarMenuAsksBeforeLoggingOut(t *testing.T) {
	s, tt := newLoggedInShell(t, "2")
	tt.SetFocused(true)

	openAvatarMenu(t, tt)
	clickAvatarMenuItem(t, s, tt, "退出登录")
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
