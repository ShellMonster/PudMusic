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
package ui

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
