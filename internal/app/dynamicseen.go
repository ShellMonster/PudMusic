// 动态红点那一份「看到哪儿了」。
//
// # 这一份是**按账号**的
//
// 动态流是「**我关注的人**在发什么」，那是按账号的（和 dynamicFeed 那一页
// 同一个理由，见 upspace.go 的文件头）。换账号之后那是另一份「没看过的动态」，
// 基准不能接着上一个账号的用——所以基准连着 mid 一起记。
//
// # 基准是「用户上一次看动态」的时刻
//
// B 站那个「有没有新动态」的接口按一个基线时刻算「有多少条比它新」。上游 Biu
// 传的是 0（`biu/src/layout/navbar/user-feed/index.tsx:18`），于是那一档永远是
// 「全部」：用户点掉了那个点，五分钟之后它又回来（上游只是把本地的数清成 0，
// 下一轮又问回同一个数）。
//
// 我们这里记的是**用户上一次看动态的时刻**，所以点掉之后它真的不回来了，
// 直到有新的动态发出来。而 0 那一档留给「一次都没看过」（新装的、换账号的），
// 于是用户第一次打开就能看见那个点——和上游一样。
//
// # 它落在设置里
//
// 设置存储已经把 schema 版本、原子写、退出前 flush、只读保护这几件都做好了
// （见 settings.go 的文件头）。另开一份文件就等于把这些重造一遍——重造出来
// 的那份还少了只读保护：设置来自更新的版本时，那一格照样会被旧版本按旧语义
// 改写。理由和 `Playback`（上次听到哪儿）那一格一样。
package app

import (
	"context"
	"log"
	"time"
)

// DynamicBaseline 是「用户上一次看动态」的时刻（Unix 秒）。
//
// 0 = 一次都没看过（新装的、换账号的、没登录的）→ 问 B 站的时候传 0，那一档
// 是「从头算」，于是用户第一次打开就能看见那个点。
func (a *App) DynamicBaseline() int64 {
	mid := a.SelfMid()
	if mid <= 0 {
		return 0
	}
	seen := a.SettingsSnapshot().DynamicSeen
	if seen.Mid != mid {
		// 换账号了：那是另一份「没看过的动态」，从头算。
		return 0
	}
	return seen.Baseline
}

// NewDynamicCount 问一次「从上次看过的地方到现在有几条没看过的」。
//
// ⚠️ **没登录时一个请求都不发**：动态流是「我关注的人在发什么」，没有登录态
// 就没有这个列表，而 B 站对那个接口回 -101——那个 -101 在别的层和「网络断了」
// 长得一模一样。返回的是 `ErrNotLoggedIn`（credentials.go 里那一个）。
func (a *App) NewDynamicCount(ctx context.Context) (int, error) {
	if !a.LoggedIn() {
		return 0, ErrNotLoggedIn
	}
	cli := a.explore()
	if cli == nil {
		return 0, errExploreMissing
	}
	return cli.DynamicFeedUpdate(ctx, a.DynamicBaseline())
}

// MarkDynamicSeen 记下「用户看到这儿了」，落盘。
//
// at 是**用户看的那一刻**。它由调用方给——界面层有自己的钟（clock.go），测试换上
// 假时钟之后这一格也跟着换（这一层自己不去问「现在几点」）。
//
// ⚠️ **它只往前挪**：at 比已经记着的那个早（用户把机器的时间调回去了）就
// 不动它——基准往前跳会让「已经看过的」又变成「没看过」，那个点会诈尸。
func (a *App) MarkDynamicSeen(at time.Time) {
	mid := a.SelfMid()
	if mid <= 0 {
		// 没登录就没有「我关注的人的动态」，也就没有「看过」。
		return
	}
	now := at.Unix()
	_, err := a.UpdateSettings(func(s *Settings) {
		if s.DynamicSeen.Mid == mid && s.DynamicSeen.Baseline > now {
			return
		}
		s.DynamicSeen = DynamicSeen{Mid: mid, Baseline: now}
	})
	if err != nil {
		// 存不住不是致命的：这一趟顶多让那个点下次又冒出来，而用户已经看过了。
		log.Printf("biu: 记不住「动态看到哪儿了」：%v", err)
	}
}
