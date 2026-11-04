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
// 所以这一支：**一个请求都不发**，界面上给一枚头像位的插画、一句「还没登录」
// 和一个「去登录」。
//
// # 旗标在路由表里
//
// 「这一页要不要拦」是 `Route.RequiresLogin`（router.go），而「要不要摆进
// 侧栏」是 `Route.HideInMenuWhenLoggedOut`——**两件事，两个标志**。这一票之前
// 它们是同一个 `NeedsLogin`，于是「拦」那一层没有地方放。
//
// ⚠️ **两页的检查顺序：先「B 站客户端没挂上」，再「没登录」**（见 later.go /
// history.go）。理由是那一句「这一页暂时加载不出来」说的是**这一次**怎么了，而
// 「没登录」说的是**这个账号**怎么了——前者更具体。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// laterNotLoggedIn 是「稍后再看」那一页没登录时说的话。
//
// ⚠️ **它和「还没关注任何人」必须是两句**（理由同 `followNotLoggedIn`）：说成
// 「你还没收藏任何东西」的话，用户登录完发现还是空的，会以为是登录坏了。
//
// ⚠️ **三页共用同一枚插画**（`iconEmptyLogin`）：这一支说的是**同一个状态**
// （这个账号还没登进来），不是三个不同的状态。别的空状态是「一页一枚」
// （`DESIGN.md` §七），因为那些说的是「这一页自己的东西没有」；这里三页摆同一枚
// 是对的——用户看到的是「没登录」。
var laterNotLoggedIn = emptyState{
	Title: "还没登录",
	Hint:  "稍后再看是跟着账号走的——先扫码登录，登录之后你在别处加的会出现在这里",
	Icon:  iconEmptyLogin,
}

// historyNotLoggedIn 是「历史记录」那一页没登录时说的话。
var historyNotLoggedIn = emptyState{
	Title: "还没登录",
	Hint:  "历史记录是跟着账号走的——先扫码登录，登录之后听过的歌会出现在这里",
	Icon:  iconEmptyLogin,
}

// recentNotLoggedIn 是首页「最近播放」那一档没登录时说的话。
var recentNotLoggedIn = emptyState{
	Title: "还没登录",
	Hint:  "最近播放是跟着账号走的——先扫码登录，登录之后听过的歌会出现在这里",
	Icon:  iconEmptyLogin,
}

// recentEmpty 是首页「最近播放」那一档登录了、但账号里一条观看记录都没有时说的话。
// 它和没登录是两件事：这里的下一步是回到推荐去听点什么。
var recentEmpty = emptyState{
	Title: "还没有最近播放",
	Hint:  "听过的歌会按时间出现在这里",
	Icon:  iconEmptyHistory,
}

// loginPromptWhat 是各处「登录后查看…」那句里的「…」（原型 `loginPrompt(what)`）。键是各页给
// `viewNeedsLogin` 的稳定 key。
var loginPromptWhat = map[string]string{
	"page:" + laterPath:   "稍后再看",
	"page:" + historyPath: "历史记录",
	"page:" + followPath:  "我的关注",
	"home:recent:login":   "最近播放",
}

// viewNeedsLogin 画「登录后查看」：原型 `loginPrompt`，也就是 T0 的 `pud.LoginPrompt`（虚线框 + 锁的
// 插画 + 「登录后查看{what}」+ 主色「扫码登录」键）。点登录键去登录页，登录完回首页。
//
// e 是各页原来给的空状态文案（保留签名，免得别的页同时改）；这里只用 key 查出 what。
func (s *Shell) viewNeedsLogin(c *ui.Context, key string, _ emptyState) {
	s.applyFrameTheme(c)
	what := loginPromptWhat[key]
	if what == "" {
		what = "这些内容"
	}
	winW, _ := c.Size()
	padX := space.PageHorizontal(winW - s.sideDrawnWidth(s.app.SettingsSnapshot()))
	ui.Column(c).Key(key).Grow(1).FillWidth().Padding(space.None, padX).Children(func() {
		if pud.LoginPrompt(c, key+":prompt", what) {
			s.goLogin(homePath)
		}
	})
}
