package ui

// 票 #260 的判据：顶栏右端那一块。
//
// # 这一票改了三件事
//
//  1. **登录之后顶栏不再有独立的「退出」按钮**——退登走头像下拉里那一项。
//  2. **头像下拉按登录态隐藏**（照上游 `user/index.tsx:88-158` 的 `hidden`）。
//  3. **头像放大到上游那个尺寸**（40，见 `navbar_avatar.go` 的 `avatarSize`）。
//
// ⚠️ **判据都在画面上**：顶栏右端那一块到底摆了什么、头像画出来的那一枚圆
// 有多大——这些是用户看见的东西，而状态字段只用来定位，不当判据。
import "testing"

// TestTheLoggedInTopBarHasNoStandaloneLogoutButton 钉住「登录之后顶栏上没有
// 独立的「退出」按钮」。
//
// ⚠️ **用户 2026-10-25 原话**：「右上角的，独立的退出按钮删掉，参考 biu 的
// 做就行，点开头像后展开比如设置、资料、退出之类的」。
//
// ⚠️ **找的是「退出登录」这四个字**：那枚图标键的无障碍名就是它
// （`iconKey(c, iconLogout, "退出登录", …)`），而无头 Tester 的 `Find` 找的
// 是「显示着这个字**或者**标着这个名字」的元素（`mygo/ui/headless.go:444`，
// 那是**精确**匹配）。
//
// ⚠️ **浮层是关着的**：头像下拉里那一项也写着「退出登录」，而浮层一开就
// 找得到它了——所以这一条量的正是「顶栏上**平时**有没有那一枚」。
func TestTheLoggedInTopBarHasNoStandaloneLogoutButton(t *testing.T) {
	_, tt := newLoggedInShell(t, "2")
	tt.Frame()

	if _, ok := tt.Find("退出登录"); ok {
		t.Errorf("登录之后顶栏上还有一枚独立的「退出登录」按钮——"+
			"退登只走头像下拉里那一项，文字有 %q", tt.Texts())
	}
}

// TestTheGuestTopBarStillHasTheLoginButton 钉住「**未登录**时顶栏那枚「登录」
// 按钮还在」。
//
// ⚠️ **这一枚不能跟着一起删掉**：删了就没有登录入口了——而顶栏头像下拉里
// 虽然也有一项「登录」，可那一项要**先点开头像**才看得见（用户 2026-10-25
// 只说了删那枚独立的「退出」）。
func TestTheGuestTopBarStillHasTheLoginButton(t *testing.T) {
	_, tt := newShell(t)
	tt.Frame()

	r, ok := tt.Find("登录")
	if !ok {
		t.Fatalf("没登录时顶栏上那一枚「登录」不见了——登录入口不能没，文字有 %q", tt.Texts())
	}
	if r.W <= 0 || r.H <= 0 {
		t.Errorf("那一枚「登录」在 %v，量不出来", r)
	}
}

// TestTheAvatarHitAreaCoversTheCircleItDraws 钉住「头像那一格的**命中区**
// 装得下它画出来的那一枚圆」。
//
// ⚠️ **命中区比画出来的那一枚还小的话，圆的外沿那几像素点不到**——而这一格
// 是顶栏里唯一的「个人入口」（票 BMG-253 给它加那一圈悬停环就是为了让它
// 更像一个能按的东西）。
//
// ⚠️ **量的是命中区那一格**：无头 Tester 的 `Find` 给的是**标着这个名字的
// 那一格**（头像那一枚按钮），不是画出来的圆——画出来的那一枚圆另有
// `TestTheTopBarEndsWithTheUpstreamSizedAvatar` 在量。
func TestTheAvatarHitAreaCoversTheCircleItDraws(t *testing.T) {
	_, tt := newShell(t)
	tt.Frame()

	hit, ok := tt.Find(avatarLabel)
	if !ok {
		t.Fatalf("画面上找不到顶栏那个头像，文字有 %q", tt.Texts())
	}
	if hit.W < avatarSize || hit.H < avatarSize {
		t.Errorf("头像那一格的命中区是 %v，而画出来的那一枚圆是 %d 见方——"+
			"圆的外沿那几像素点不到", hit, avatarSize)
	}
}

// TestTheOtherTopBarKeysKeptTheirHitArea 钉住「这一票**没碰**顶栏另外那几枚
// 图标键」。
//
// ⚠️ **这一票只动头像那一格**：打开文件 / 迷你 / 动态红点那几枚的命中区还是
// 32（`iconHitMd`），图形也还是 18（`iconMd`）——用户说的是「头像要大一点」，
// 不是「整排都要大」。
func TestTheOtherTopBarKeysKeptTheirHitArea(t *testing.T) {
	_, tt := newShell(t)
	tt.Frame()

	for _, label := range []string{"打开文件", "迷你"} {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("顶栏上找不到 %q，文字有 %q", label, tt.Texts())
		}
		if r.W != iconHitMd || r.H != iconHitMd {
			t.Errorf("%q 那一格的命中区是 %v，want %d×%d——这一票只动头像那一格",
				label, r, iconHitMd, iconHitMd)
		}
	}
}

// TestTheAvatarMenuItemIsAlwaysClickable 钉住「下拉里那一项**点得动**」，
// 以及「确认框里那几颗键**点得动**」。
//
// # 这两条在 2026-10-25 之前是**偶发**红的
//
// 症状是「按下去没反应」：菜单还开着、什么都没发生（确认框那一条是「确认框
// 还开着、退登没发生」）。根因是**浮层里元素的身份**：框架给元素的身份是
// 「父的身份 + 兄弟序号」（`mygo/ui/context.go` 的 `newElement`），而**浮层里
// 的东西会多会少**——一层浮层开着的时候它占着浮层里的一个位置，它关掉的時候
// 那个位置就没了。于是**旁边那几层的身份跟着变**。
//
// 而**按下和抬起之间隔着帧**：按下记在旧身份上，抬起之后那一项已经是新身份
// 了——于是那一下**点空**。实测 40 次里有 5~6 次（机器越忙越容易）。
//
// ⚠️ **钉死它的办法**：给这两层各一个自己的身份——
// `navbar_avatar.go` 的 `viewAvatarMenu` 里那一句 `c.Key("avatar-menu-panel")`，
// 和 `confirmmodal.go` 的 `confirmModal` 里那一句
// `c.Key("confirm-backdrop:…")`。身份不再跟着浮层里的兄弟序号变。
//
// # 为什么这一条是**统计**的
//
// 那个偶发**跟机器忙不忙有关**：忙的时候 40 遍里有 5~6 遍点空，空闲的机器上
// 一遍都不空。所以判据只能是「点了就响」——**每一次**都要有反应。
//
// 20 遍是折中：机器忙的时候它几乎必红（12% 上下 → 20 遍里漏掉的机会不到
// 7%），空闲的机器上它不红（而那时候那个偶发本来也不发生）。
// **修好之后是 0**（实测 30×20 遍全中）。
func TestTheAvatarMenuItemIsAlwaysClickable(t *testing.T) {
	const rounds = 20
	for i := 0; i < rounds; i++ {
		s, tt := newLoggedInShell(t, "2")
		tt.SetFocused(true)

		// ① 下拉里那一项：按下要弹「确认退出登录？」。
		openAvatarMenu(t, tt)
		clickAvatarMenuItem(t, s, tt, "退出登录")
		if !tt.HasText("确认退出登录？") {
			t.Fatalf("第 %d 遍点空了——下拉里那一项按下去没有反应"+
				"（菜单还开着：%v）", i+1, s.avatarMenuOpen)
		}
		// ② 确认框里那颗「确认」：按下要真的退登。
		if err := tt.Click("确认退出登录"); err != nil {
			t.Fatalf("第 %d 遍点不到确认键：%v，文字有 %q", i+1, err, tt.Texts())
		}
		if s.app.LoggedIn() {
			t.Fatalf("第 %d 遍点了「确认」却还是登录态——确认框里那颗键没点着", i+1)
		}
	}
}
