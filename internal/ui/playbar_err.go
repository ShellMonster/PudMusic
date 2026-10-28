// 播放条上那一句错的「**详情层**」（票 #320）。
//
// # 这一层是为了什么
//
// `bili.Friendly` 那张表里的话**信息量很大**——「B 站没有给这一条单独的音频流
// （歌单、活动那一种多半是这样）。在 B 站里打开它听，或者换一首放」四十来个
// 字。而播放条上那一行只有 **140 像素宽**（1200 宽的窗口，实测），12 号字放得
// 下十来个字，还带省略号——于是用户常常只读到半句话（「打不开：B 站没有给这…」）。
//
// 76 高的播放条装不下两行（那是**布局**的账，见 `viewTrackError` 里那段注释），
// 所以完整那一句只能去别处拿。这儿给了两个去处：
//
//	悬停	提示框（`viewTrackError` 里 `withTip` 那一处，票 #282 的评审加的）
//	点它	**这一层**：播放条上方一块小面板
//
// # 为什么还要「点开」这一层（悬停不是已经能看全文了吗）
//
// 三个理由：
//
//  1. **提示框跟着指针走**：读完一句得盯着它不动，指针一移开就没了——而四十个
//     字读完要几秒。
//  2. **提示框给不了「复制」**：用户要拿这句话去提 issue、问人。面板里那一句
//     是**可选中的**（`Selectable()`），拖一下就能复制（Cmd+C，右键菜单里也有
//     「复制」）——和更新对话框里那句错误一个做法（`update_dialog.go`）。
//  3. **提示框是「你碰巧悬停到了」**：那一行在播放条最左边，窄窄一条，指针扫
//     过去就过去了。点得开的那一层是**用户自己叫出来的**。
//
// # 面板里**有**那两枚动作（它们原来在播放条那一行上）
//
// ⚠️ **错误文案从那一列里搬走了**（票：左下角出错那三行太碎）：那一行的宽度
// 只有 75.7 像素（实测），一句 26 个字的话上屏只剩十来个。而「在 B 站打开」
// 那一枚按钮和那句字**挤在同一行**——所以它也跟着搬进这一层。
//
// ⚠️ **「重试」不在这一层**：它在**播放键上**——那一枚在可重试时本身就是
// 「重试」（`viewErrorPlayKey`），而**同一个动作摆两处**用户会问「这两个是
// 一回事吗」（播放条上「下载」那一枚的注释就写着这条，`viewPlayBarRight`）。
//
// # 它是浮层，不占地方
//
// 面板建在**覆盖层**里（`ui.Popover`），播放条那一行一个字都不多——76 高还是
// 76 高（票 #320 的验收盯的就是这个）。

package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// errFullBody 是那一句错的**完整正文**（不带「打不开：」那个前缀）。
//
// 🔴 **悬停提示和详情面板都要它，所以只有一个出口**：两处说的话不一样的话，
// 用户会读到两句互相矛盾的话（而他只会记住后读到的那一句）。
//
// ⚠️ **`ErrDetail` 和 `Err` 不一样的那一档只有「取流超时」**（票 #282 的评审：
// 短的那句上屏、长的进提示）；其余的错误两栏是同一句（`setErrText` 落的是
// `setErrTextDetail(msg, msg, …)`），所以这里取到的那一句就是完整那一句。
func errFullBody(st app.State) string {
	if st.ErrDetail != "" && st.ErrDetail != st.Err {
		return st.ErrDetail
	}
	return st.Err
}

// viewErrorDetail 是那块「详情」面板：完整那一句。
//
// ⚠️ **用的是外面那一帧的 `st`，不是现读 `App.Snapshot()`**：面板要说的必须是
// **播放条上那一句**——两者同一个来源，才不会「条上说的是一句、面板里说的是
// 另一句」。
//
// （`viewRateMenu` 那个浮层是现读 Snapshot 的，理由是它挑的是**档位**——那个
// 东西用户随时在改，而这里说的是**刚失败的那一句**，它和条上那一行必须是同一
// 句。）
func (s *Shell) viewErrorDetail(c *ui.Context, st app.State) {
	t := c.Theme()
	body := errFullBody(st)
	if body == "" {
		// 错已经没了（重试成功了）：这一层没什么可说的，就不画。
		//
		// ⚠️ **空面板比没有更坏**：它写着「打不开」，而其实能播了。
		return
	}
	ui.Column(c).Key("playbar:err-detail").Label("错误详情").
		Gap(space.Px8).Padding(space.Px8, space.Px12).MaxWidth(t.Space(80)).
		Children(func() {
			// ⚠️ **标题是「打不开」而不是「错误详情」**：播放条上那一行写的是
			// 「打不开：……」，这里用同一个说法，两处对得上。而
			// `Label("错误详情")` 是给读屏软件和测试认的（`Tester.Find` 认的是
			// 文字和 `Label`，不认 `Key`）——它是这一块的无障碍名字。
			ui.Text(c, "打不开").FontSize(scale.Font(c, scale.Size11)).TextColor(t.TextMuted)
			// 🔴 **可选中**：用户要拿这句话去提 issue（和更新对话框里那句错误
			// 一个做法，`update_dialog.go`）。
			//
			// ⚠️ **`Wrap()` 不能省**：不换行的话这一句会按它自己的宽度排成一行
			// （四十几个字 ≈ 480 像素），窄窗口上就顶出屏幕了。
			//
			// ⚠️ **宽度上限 320**（`t.Space(80)`，和悬停提示那一块同宽）：更宽
			// 的话那句话排成一行、面板变成一条横幅；更窄的话它要排成五六行，
			// 面板高得挡住半个内容区。
			ui.Text(c, body).FontSize(scale.Font(c, scale.Size12)).
				TextColor(dangerText(t)).Wrap().Selectable()
			// 🔴 **「重试」和「在 B 站打开」都在这一层**（理由见文件头）：它们原来
			// 和那句被截碎的话挤在同一行（75.7 像素 + 62 像素），而错误文案搬走
			// 之后那一行就没有它们的位置了。
			//
			// ⚠️ **「重试」只给可重试的那一档**（`bili.Recoverable` 那张表）：给一个
			// 按了也没用的重试比不给更糟——用户按两次发现还是那样，就不再信这个
			// 应用了。
			//
			// ⚠️ **「在 B 站打开」只给有地址的那一档**：本地文件、或者压根没有稿件
			// 身份的那一条，摆一枚点了打不开的按钮比不给更糟（理由见
			// `titleClickable`）。
			ui.Row(c).Gap(space.Px8).Children(func() {
				if st.CanRetry {
					if s.withTip(c, flatButton(c, "重试").FontSize(scale.Font(c, scale.Size12)),
						"再试一次这一首").Clicked() {
						s.retryPlay()
					}
				}
				if st.BiliURL != "" {
					if s.withTip(c, flatButton(c, "在 B 站打开").FontSize(scale.Font(c, scale.Size12)),
						"在 B 站里听这一首").Clicked() {
						s.openExternalURL(st.BiliURL)
					}
				}
			})
		})
}
