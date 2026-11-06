// 设置页上的「关于应用」那一段：当前版本、检查更新、以及更新日志弹窗的开关。
//
// 照 Biu 的 `src/pages/settings/system-settings.tsx` 最末那一段：标题「关于应用」，
// 左边「当前版本 x.y.z」（有这个才接一个箭头和新版本号），右边一个按钮。
//
// 🔴 铁律二照旧：这一份一次都不调 `SetTheme`（主题在每一帧开头由 `viewMain`
// 装好，放到切换按钮的回调里只调一次会被框架静默丢掉——不报错、不崩溃、不打
// 日志，就是主题悄悄跳回系统色）。

package ui

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// versionLine 是「当前版本 x.y.z → a.b.c」那一行，设置页和弹窗共用一份。
//
// ⚠️ **必须是同一份**：两处各写一遍的话，有一处改了字号或者箭头方向，用户会在
// 弹窗里看着一个朝右的箭头、在设置页上看着一个朝左的，而它们说的是同一件事。
//
// 照 Biu 的 `system-settings.tsx`：`<span>当前版本 {appVersion}</span>` 后面
// （有这个才画）`<RiArrowRightLongLine/>` + 主色的新版本号，连「箭头朝右」这个
// 方向都照抄——Biu 那一段是横排的，箭头朝左会读成「版本往回退」。
func (s *Shell) versionLine(c *ui.Context, key string, size float32, weight int, st app.UpdateState) {
	t := c.Theme()
	ui.Row(c).Key(key).Gap(space.Px6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "当前版本 "+app.VersionLabel(st.Current)).FontSize(scale.Font(c, size)).FontWeight(weight)
		if st.Available() {
			ui.Text(c, "→").FontSize(scale.Font(c, size)).TextColor(t.TextMuted)
			// 主色当**字**：浅色下换成与底色 ≥4.5:1 的变体（理由见
			// `widgets/ink`）。深色下原样。
			ui.Text(c, st.Latest).FontSize(scale.Font(c, size)).FontWeight(weight).TextColor(ink.AccentOn(t))
		}
	})
}

// updateHint 是那一行下面那句话：上一次检查是什么时候，或者为什么不能检查。
//
// 为什么不能检查的时候把话说全：一个按下去只会说「不能」的按钮，用户除了去问人
// 没有别的办法知道该怎么办。
func (s *Shell) updateHint(st app.UpdateState) string {
	if why := st.UpdateUnavailableReason(); why != "" {
		return why
	}
	switch st.Phase {
	case app.UpdateChecking:
		// ⚠️ 这一档原来没有，于是「正在查」的时候下面那句话还是「点右边查一下有
		// 没有新版本」——按钮已经按下去了，那句话却还在教用户怎么按。
		return "正在问更新源有没有新版本"
	case app.UpdateDownloading:
		return "正在下载新版本"
	case app.UpdateReady:
		return updateReadyText + "，退出后重新打开就是它"
	case app.UpdateAvailable:
		return "点右边看这次的更新日志，想不想下由你定"
	case app.UpdateFailed:
		// ⚠️ **分清「没查成」和「没装成」**。这一档是两件事共用一个相位（查和装
		// 都会落到 `UpdateFailed`），而「上一次没查成：signature: ed25519
		// verification failed」这句话在骗用户：验签没过的是**下载安装**那一段，
		// 他的检查是成功的。让他以为自己网络不好或者版本没发出来，下一次他还是
		// 会先怀疑那两样，而不是重试下载。
		//
		// 判据是 `Available()`——它认的是「`Latest` 非空且和正在跑的不是同一个
		// 版本」。走到装那一段，`Latest` 必然非空（`StartUpdateDownload` 要求它
		// 非空）；而查失败时 `check()` 只在 `rel == nil`（已是最新）那支清空
		// `Latest`，那条路同时把相位设回 `Idle`。所以这一个判断就够。
		if st.Available() {
			return "没能装上：" + st.Err
		}
		return "上一次没查成：" + st.Err
	}
	if st.LastCheck.IsZero() {
		return "点右边查一下有没有新版本"
	}
	return "上次检查：" + updateCheckedOn(st.LastCheck)
}

// updateCheckedOn 把检查时刻写成一句话。
//
// **只到日期，不到时刻**（`2026-10-17`）：这一行回答的是「它到底查没查过」，
// 而查的间隔是一小时（`updateCheckInterval`）——写上时刻，用户会觉得那串时刻每
// 小时都在变，而那正是它在做的事，反而让人以为它在不停打扰网络。
func updateCheckedOn(t time.Time) string {
	return t.Local().Format("2006-01-02")
}

// updateEntryButton 是设置页上那个按钮的三样东西：写什么、按下去做什么、在不在忙。
//
// ⚠️ **「在忙」只认真的在忙的那两档**（正在查、正在下）。「这一版不能自动更新」
// 那一档按钮也是关着的，但它**不是在忙**——它上面不该转圈（转圈会让用户以为还
// 在查），只是关着，而**为什么**写在下面那句话里。
func (s *Shell) updateEntryButton(st app.UpdateState) (label string, onClick func(), busy bool) {
	switch st.Phase {
	case app.UpdateDownloading:
		return updateEntryBusy, nil, true
	case app.UpdateChecking:
		return updateCheckingLabel, nil, true
	case app.UpdateAvailable, app.UpdateReady:
		// 「装好了」也算「有东西可看」：弹窗里那一句「安装包已下载完成」和那个
		// 「退出并安装更新」正是这时候要看和要按的。
		return updateBtnView, s.openUpdateDialog, false
	}
	if !st.Enabled {
		// 不能更新的时候按下去什么也不发生（onClick 是 nil），而**为什么**写在
		// 下面那句话里。
		return updateBtnCheck, nil, false
	}
	return updateBtnCheck, s.checkForUpdates, false
}

// checkForUpdates 记录这是用户主动发起的检查。
//
// 检查完成后由 `updateOpen` 判断结果：只有真的发现新版本才自动打开弹窗；
// 已是最新或检查失败都只保留设置页上的结果提示。
func (s *Shell) checkForUpdates() {
	s.update.checkPending = true
	s.update.checkObserved = false
	s.app.CheckUpdates()
}

// openUpdateDialog 只把弹窗打开，不去查。
//
// 不调 `CheckUpdates` 是有意的：那会从头再来一趟网络，而弹窗要画的那些内容
// （更新日志、版本号）已经在状态里了。
func (s *Shell) openUpdateDialog() { s.update.wanted = true }

// reportUpdateNotice 把更新器留给界面的那一句提示弹成 Toast。**每帧都调**。
//
// 去重在 `App.TakeUpdateNotice` 那一头（取过就没了），所以这里每帧问一句是安全
// 的——屏幕上一直闪着同一句话比更新功能本身出问题还难解释。
//
// 它跟着 `reportSettingsError` 放在同一处，是因为它们是同一类东西：状态层在后台
// 攒了一句话，界面在每帧开头取一次。**不许**改成「查到了才回调」——那要从后台
// goroutine 直接调 `addToast`，而提示列表归界面所有，跨 goroutine 写它就违反
// 了铁律一。
func (s *Shell) reportUpdateNotice() {
	if note, tone := s.app.TakeUpdateNotice(); note != "" {
		s.addToast(note, tone)
	}
}
