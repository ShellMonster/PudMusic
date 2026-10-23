// 「这一页要登录才有内容」那一支：三页共用（我的关注 / 稍后再看 / 历史记录）。
//
// # 为什么它是单独一支，而不是空状态
//
// 空状态说的是「**这里没有**」——一个结论。而这一支说的是「**你要先登录**」：
// 用户从地址栏或者前进后退进到「稍后再看」「历史记录」，界面上标题、日期
// 筛选、关键词框、表头**全都在**，然后列表空着，或者末尾一行「加载失败，点
// 此重试」——**一句都没提「你要先登录」**。用户会以为应用坏了或者网络有问题。
//
// 而那一页确实**不该有内容**：那几个接口全都要 Cookie，没登录时它们一律回
// -101，而 -101 和「网络断了」在这一层**长得一模一样**（`app.ErrNotLoggedIn`）。
// 所以这一支：**一个请求都不发**，界面上给一句「还没登录」和一个「去登录」。
//
// # 旗标在路由表里
//
// 「这一页要不要拦」是 `Route.RequiresLogin`（router.go），而「要不要摆进
// 侧栏」是 `Route.HideInMenuWhenLoggedOut`——**两件事，两个标志**。这一票之前
// 它们是同一个 `NeedsLogin`，于是「拦」那一层没有地方放。
//
// ⚠️ **两页的检查顺序：先「B 站客户端没挂上」，再「没登录」**（见 later.go /
// history.go）。理由是那一句「还没接上 B 站接口」说的是**这一次**怎么了，而
// 「没登录」说的是**这个账号**怎么了——前者更具体。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// laterNotLoggedIn 是「稍后再看」那一页没登录时说的话。
//
// ⚠️ **它和「还没关注任何人」必须是两句**（理由同 `followNotLoggedIn`）：说成
// 「你还没收藏任何东西」的话，用户登录完发现还是空的，会以为是登录坏了。
var laterNotLoggedIn = emptyState{
	Title: "还没登录",
	Hint:  "稍后再看是跟着账号走的——先扫码登录，登录之后你在别处加的会出现在这里",
}

// historyNotLoggedIn 是「历史记录」那一页没登录时说的话。
var historyNotLoggedIn = emptyState{
	Title: "还没登录",
	Hint:  "历史记录是跟着账号走的——先扫码登录，登录之后听过的歌会出现在这里",
}

// viewNeedsLogin 画「这一页要登录才有内容」那一支。
//
// ⚠️ **它不是空状态那一套**：标题是**页标题那个字号**（`pageTitleSize`，24）
// 和字重（400，不加粗），下面跟着一句提示和一枚「去登录」——而空状态是 15px
// 的一行标题加一句 12px 的提示、居中带一枚插画。形状不同就是另一支
// （`emptyicon_test.go` 的 allowlist 上有它）。
//
// ⚠️ **它不发任何请求**：这一支画出来的时候，那一页的接口一个都不该发。
func (s *Shell) viewNeedsLogin(c *ui.Context, key string, e emptyState) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题（见 theme.go）。
	s.applyFrameTheme(c)

	t := c.Theme()
	ui.Column(c).Key(key).Grow(1).Center().Gap(space.Px8).Padding(space.Px24).Children(func() {
		ui.Text(c, e.Title).FontSize(scale.Font(c, pageTitleSize))
		ui.Text(c, e.Hint).FontSize(scale.Font(c, 13)).TextColor(t.TextMuted)
		// 给一个能立刻做完的事：这一页在没登录之前永远是这个样子，而
		// `/login` 就是扫码登录那一页（router.go 的 routes 表里那一条）。
		if ui.PrimaryButton(c, "去登录").Clicked() {
			s.navigate(loginPath)
		}
	})
}
