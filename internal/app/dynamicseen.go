// 动态红点那一份「看到哪儿了」。
//
// # 这一份是**按账号**的
//
// 动态流是「**我关注的人**在发什么」，那是按账号的（和 dynamicFeed 那一页
// 同一个理由，见 upspace.go 的文件头）。换账号之后那是另一份「没看过的动态」，
// 基准不能接着上一个账号的用——所以基准连着 mid 一起记。
//
// # 基准是**动态 id**，不是时间戳
//
// B 站那个「有没有新动态」的接口（/feed/all/update）按一个**基线**算「有多少条
// 比它新」，而那个基线就是**动态 id**。**契约在 Biu 上游源码里**（不用去查社区
// 文档）：
//
//   - `biu/src/service/web-dynamic.ts:35-36`（那个接口的参数）：「**默认为上次
//     请求 获取全部动态列表 返回的 update_baseline**，实测填 0 也可正常获取」；
//   - `biu/src/service/web-dynamic.ts:58`（/feed/all 的返回类型）：
//     `update_baseline: string; // 更新基线 (第一条记录ID)`——**第一条记录的 id**，
//     也就是「此刻最新那条动态」的 id（真实返回长这样："954659517801431040"）。
//
// ⚠️ **传错形状的后果是静默的**：时间戳不是合法的 id，于是 B 站要么回一个错
// （那个点再也不亮），要么当成「比它新的都算」（那个点永远亮着）——而用户看到
// 的只是「那个点不对」。
//
// ⚠️ **上游 Biu 永远传 0**（`biu/src/layout/navbar/user-feed/index.tsx:18`），
// 所以它那一档永远是「全部」：用户点掉了那个点，五分钟之后它又回来（上游只是
// 把本地的数清成 0，下一轮又问回同一个数）。它从没暴露过这个参数该传什么形状。
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
	"strings"
)

// DynamicBaseline 是「用户看到的那条最新动态」的 id（字符串）。
//
// 空串 = 一次都没看过（新装的、换账号的、没登录的）→ 问 B 站的时候传 0，那一档
// 是「从头算」，于是用户第一次打开就能看见那个点。
//
// ⚠️ **它按账号取**：换账号之后那是另一份「没看过的动态」，从头算；而**切回去**
// 的时候这一个账号的基准还在（那是一张表，见 DynamicSeen 的说明）。
func (a *App) DynamicBaseline() string {
	mid := a.SelfMid()
	if mid <= 0 {
		return ""
	}
	for _, e := range a.SettingsSnapshot().DynamicSeen.Baselines {
		if e.Mid == mid {
			return e.Baseline
		}
	}
	return ""
}

// NewDynamicCount 问一次「比上次看到的那条新的有几条」。
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

// CatchUpDynamicSeen 记下「用户已经追上来了」：拉一次动态流的第一页，拿它最新
// 那条动态的 id 当基准。返回的是那个 id（空串 = 没记上）。
//
// # 为什么点那个点要**重新拉一次**
//
// 顶栏那个红点的基准是**用户看过的那条最新动态的 id**。用户点那个点的时候他在
// 说「我看到这儿了」——而「用户动态」那一页可能十分钟前就开着（它自己不刷新），
// 于是光靠那一页记下来的 id 是**旧的**，B 站会一直说「还有新的」→ **那个点每
// 五分钟回来一次**（票面「点了不该马上又冒出来」）。
//
// 所以这里现拉一次第一页：它返回的 `data.update_baseline` 就是此刻最新那条的
// id（见 bili.DynamicPage.Baseline），拿它当基准，B 站下一轮就会说「没有新的」。
func (a *App) CatchUpDynamicSeen(ctx context.Context) (string, error) {
	if !a.LoggedIn() {
		return "", ErrNotLoggedIn
	}
	cli := a.explore()
	if cli == nil {
		return "", errExploreMissing
	}
	pg, err := cli.DynamicFeed(ctx, "")
	if err != nil {
		return "", err
	}
	if pg.Baseline == "" {
		// ⚠️ **到这儿说明这一页一条动态都没有**：基线的退路是「这一页第一条动态的
		// id」（见 bili.dynamicPageFrom），而这一页没有第一条。
		//
		// 不动它——宁可让那个点下次又亮，也不要记一个错的（错的会让「点进去真的
		// 消失」那条落空）。
		log.Printf("biu: 动态流这一页是空的，没有可记的更新基线")
		return "", nil
	}
	a.MarkDynamicSeen(pg.Baseline)
	return pg.Baseline, nil
}

// MarkDynamicSeen 记下「用户看到这条了」，落盘。
//
// dynID 是那条动态的 id（字符串）。
//
// ⚠️ **它只往前挪**：动态 id 是雪花那一类的，**越大越新**，所以比一比就知道
// 新的那个是不是更大。倒退一步（用户把机器的时间调回去了、或者两趟请求回来
// 的顺序反了）会让「已经看过的」又变成「没看过」，那个点会诈尸。
func (a *App) MarkDynamicSeen(dynID string) {
	mid := a.SelfMid()
	if mid <= 0 {
		// 没登录就没有「我关注的人的动态」，也就没有「看过」。
		return
	}
	dynID = strings.TrimSpace(dynID)
	if dynID == "" {
		return
	}
	_, err := a.UpdateSettings(func(s *Settings) {
		// ⚠️ **只动这一个账号那一行**：别的账号的基准得留着（切回去的时候那个点
		// 不该又按「全部没看过」亮起来）。
		for i := range s.DynamicSeen.Baselines {
			if s.DynamicSeen.Baselines[i].Mid != mid {
				continue
			}
			if !newerDynID(dynID, s.DynamicSeen.Baselines[i].Baseline) {
				// 只往前挪：已经记着的那个更新（或者一样新），就不动它。
				return
			}
			s.DynamicSeen.Baselines[i].Baseline = dynID
			return
		}
		// 这个账号还没有：加一行。
		s.DynamicSeen.Baselines = append(s.DynamicSeen.Baselines,
			DynamicSeenEntry{Mid: mid, Baseline: dynID})
	})
	if err != nil {
		// 存不住不是致命的：这一趟顶多让那个点下次又冒出来，而用户已经看过了。
		log.Printf("biu: 记不住「动态看到哪儿了」：%v", err)
	}
}

// newerDynID 报 a 是不是比 b 新。
//
// ⚠️ **动态 id 是十进制的雪花那一类**：位数一样时比字典序就是比大小，位数不一样
// 时位数多的那个更新（雪花 id 随时间增长，位数只会慢慢变多）。
//
// ⚠️ **b 是空串**（还没记过）时一律算新 —— 那是「一次都没看过」。
func newerDynID(a, b string) bool {
	if b == "" {
		return true
	}
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a > b
}
