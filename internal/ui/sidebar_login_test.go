// 未登录时侧栏那三个入口不摆（issue #96 的第一条验收）。
//
// 跑的是 MyGo 的 Tester：和真窗口一模一样的布局与绘制，只是画到内存里。

package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// loginOnlyTitles 是「要登录才有内容」的那几页的名字。
//
// **从 routes 表里数，不另写一份**：写死三个名字的话，哪天给路由加了
// `HideInMenuWhenLoggedOut` 而忘了改这里，这条测试就只验了三个里的两个而全绿。
func loginOnlyTitles() []string {
	var out []string
	for _, rt := range routes {
		if rt.HideInMenuWhenLoggedOut {
			out = append(out, rt.Title)
		}
	}
	return out
}

// loginOnlyMatches 是同样那几页的路由（`hiddenMenuKeys` 存的是这个）。
func loginOnlyMatches() []string {
	var out []string
	for _, rt := range routes {
		if rt.HideInMenuWhenLoggedOut {
			out = append(out, rt.Match)
		}
	}
	return out
}

// TestTheSidebarHidesTheLoginOnlyEntriesUntilYouLogIn 是这一票的第一条验收：
// 未登录时侧栏里找不到那三个词，登录之后它们都在。
//
// ## 判据是「找不到」而不是「少了几项」
//
// 「侧栏少了几行」这件事可以有很多种原因（折叠了、设置里藏了、窗口窄），
// 每一件都会让一个数出来行数的断言变绿，而用户看见的仍然是「我的关注不见了」。
// 只有「那三个词一个都不在画面上」才是用户真正看得见的那件事。
//
// ⚠️ 顺带钉住一句约束：**「登录后可见」那一行不许写出那三个名字**。它和它们
// 摆在同一个侧栏里，一旦它写出「历史记录」，`HasText("历史记录")` 就永远成立，
// 这一条立刻变成空跑。改那句提示的时候记得回来。
func TestTheSidebarHidesTheLoginOnlyEntriesUntilYouLogIn(t *testing.T) {
	names := loginOnlyTitles()
	if len(names) != 3 {
		t.Fatalf("标了 HideInMenuWhenLoggedOut 的路由是 %d 条 %q，want 3（关注 / 稍后再看 / 历史记录）",
			len(names), names)
	}

	t.Run("没登录", func(t *testing.T) {
		_, tt := newShell(t)
		tt.Frame()

		for _, w := range names {
			if tt.HasText(w) {
				t.Errorf("没登录时侧栏上还摆着「%s」，文字有 %q", w, tt.Texts())
			}
		}
		// 少了几项得给一句解释，不然用户以为是应用坏了。
		if want := loggedOutHint(len(names)); !tt.HasText(want) {
			t.Errorf("没有那句「%s」，文字有 %q", want, tt.Texts())
		}
		// 而收下它的另外几页照摆——否则「未登录时侧栏只剩四个入口」也是一种坏。
		for _, w := range []string{"推荐音乐", "本地音乐", "下载记录"} {
			if !tt.HasText(w) {
				t.Errorf("没登录时「%s」也不见了，文字有 %q", w, tt.Texts())
			}
		}
	})

	t.Run("登录了", func(t *testing.T) {
		_, tt := newLoggedInShell(t, "7")
		tt.Frame()

		for _, w := range names {
			if !tt.HasText(w) {
				t.Errorf("登录之后侧栏上没有「%s」，文字有 %q", w, tt.Texts())
			}
		}
		if hint := loggedOutHint(len(names)); tt.HasText(hint) {
			t.Errorf("登录之后还在说「%s」，文字有 %q", hint, tt.Texts())
		}
	})
}

// TestTheSidebarKeepsQuietWhenTheUserHidThoseEntriesThemselves 钉住提示的那一句
// 前提：**用户自己在设置里把那几项藏起来时，不摆「登录后可见」**。
//
// 这一句的前提是「侧栏上少了几项，原因是没登录」。而「在设置 → 菜单里取消勾选」
// 是同一个结果的另一个原因——那时再说「登录后可见」是拿一句不成立的话去填空白，
// 而用户会照着它去登录，登完发现那几项还是不在。
func TestTheSidebarKeepsQuietWhenTheUserHidThoseEntriesThemselves(t *testing.T) {
	st := app.DefaultSettings("")
	st.HiddenMenuKeys = loginOnlyMatches()

	_, tt := newShell(t, withSettings(st))
	tt.Frame()

	for _, w := range loginOnlyTitles() {
		if tt.HasText(w) {
			t.Errorf("设置里藏了「%s」，侧栏上还摆着，文字有 %q", w, tt.Texts())
		}
	}
	if want := loggedOutHint(len(loginOnlyTitles())); tt.HasText(want) {
		t.Errorf("三项都是用户自己藏的，却还在说「%s」，文字有 %q", want, tt.Texts())
	}
	if !tt.HasText("推荐音乐") {
		t.Fatalf("连没藏的那些也不摆了，文字有 %q", tt.Texts())
	}
}

// TestTheLoggedOutHintIsAWayIntoTheLoginPage 钉住「登录后可见」那一句**点得动**。
//
// # 为什么这一条要紧
//
// 原来它是一句纯文字：它解释了「侧栏上少了三行」，却**没给下一步**——用户读完
// 只能自己去顶栏找那枚**没有文字**的图标（UX 报告 §3-A 的 A1，高）。
//
// 判据是**点下去之后落在哪一页**：光断言「画面上有这句话」的话，把那句换成一枚
// 死物也照样绿。
func TestTheLoggedOutHintIsAWayIntoTheLoginPage(t *testing.T) {
	s, tt := newShell(t)
	tt.Frame()

	hint := loggedOutHint(len(loginOnlyTitles()))
	if !tt.HasText(hint) {
		t.Fatalf("侧栏上没有那句 %q，文字有 %q", hint, tt.Texts())
	}
	if err := tt.Click(hint); err != nil {
		t.Fatalf("点不了那句 %q：%v", hint, err)
	}
	tt.Frame()

	if got := s.router.Path(); got != loginPath {
		t.Errorf("点了那句提示之后在 %q，want %q（扫码登录那一页）", got, loginPath)
	}
}
