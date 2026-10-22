// 本地音乐页的「按下去会发生什么」：两道确认框、播放/入队、添加和移除目录。
//
// 拆出来单独一个文件是因为 `localmusic.go` 装不下（它已经画完了整页的
// 三种样式）。这一段是**动作**，和那一段的**画法**不共享任何东西：这里没有
// 一次 `ui.Xxx`，那边的每一个控件都在 `localmusic.go` 里。
//
// 🔴 铁律二照旧：这一段一次都不调 `SetTheme`（主题在每一帧开头由
// `viewMain` 装好，��到回调里只调一次会被框架静默丢掉）。

package ui

import (
	"fmt"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 两道确认
// ─────────────────────────────────────────────────────────────────────────────

// viewDeleteDialog 是删文件那道确认。
func (p *localPage) viewDeleteDialog(c *ui.Context) {
	tr := p.askTrack
	if tr.Path == "" {
		return
	}
	// 正文后面带上完整路径：歌名可能重名（「同名.mp3」在好几个目录里），
	// 而按下按钮之后**不可恢复**——用户得能确认自己删的是哪一个文件。
	if p.confirmDialog(c, &p.askOpen, "dialog:delete", deleteDialogTitle,
		deleteDialogBody, tr.Path, deleteConfirmLabel, true) {
		p.deleteTrack(tr)
	}
	// ⚠️ 判「关掉了」用 `!p.askOpen`：**弹窗刚打开的那一帧 `confirmDialog`
	// 返回 false**（还没有人按任何按钮），而这一帧 `*open` 还是 true。
	// 两个条件都满足才清掉待删项——判错的话弹窗画出来的那一瞬间目标就没了，
	// 按钮还在而按下去什么也不发生。
	if !p.askOpen {
		p.askTrack = localmusic.Track{}
	}
}

// viewRemoveDirDialog 是移除目录那道确认。
func (p *localPage) viewRemoveDirDialog(c *ui.Context) {
	dir := p.rmPath
	if dir == "" {
		return
	}
	if p.confirmDialog(c, &p.rmOpen, "dialog:remove-dir", removeDirTitle,
		removeDirBody, dir, "移除", true) {
		p.removeRoot(dir)
		p.dirLabel = allDirs
	}
	if !p.rmOpen {
		p.rmPath = ""
	}
}

// confirmDialog 画一道确认框，这一帧用户按了确认就返回 true。
//
// # 为什么不用 `ui.AlertDialog`
//
// 它把**最后一个按钮一律做成主按钮**（用主题的强调色，也就是绿的），
// 而「删除」这个按钮要红色的：Biu 上游那个弹窗写的就是 `type: "danger"`
// （`local-music.ts:179`）。一个绿色的「删除」和行里那个红色的「删除文件」
// 摆在一起，是在说两件不一样的事——而这两个按钮指的是**同一个动作**。
//
// `ui.Modal` 把「点背景 / 按 Esc 关掉」做好了（`Dismissed` 报它），那一半
// 不用自己写；剩下的（面板长什么样、按钮什么颜色）就是这一摊自己的事。
//
// ⚠️ 两个按钮都用 `ui.Button` 而不是 `ui.PrimaryButton`：后者一定是强调色，
// 而这里要的是「取消不显眼、确认显眼且是红的」。
func (p *localPage) confirmDialog(c *ui.Context, open *bool, key, title, body,
	detail, confirm string, danger bool) bool {
	if !*open {
		return false
	}
	t := c.Theme()
	pressed := ""

	// ⚠️ `ui.Modal` 自己已经把那块面板摆好并给了它内边距/圆角/背景
	//（`mygo/ui/widgets.go` 的 `coreModal`），`fn` 往里放的是**面板的内容**。
	// 在里面再套一个 `ui.Column` 会让按钮的命中区域和面板对不上——症状是
	// 「按钮看得见、点不动」。
	ui.Modal(c, open, func() {
		// 三句话量成一个 420 宽的块（路径那一行在 420 下大概能放 45 个字符）；
		// 按钮行是面板的直接子元素。
		ui.Column(c).Key(key).Width(dialogWidth).Gap(dialogGap).
			Children(func() {
				ui.Text(c, title).FontSize(scale.Font(c, 16)).Bold()
				ui.Text(c, body).FontSize(scale.Font(c, 13)).TextColor(t.TextMuted)
				// 完整路径单独一行：它是用户按下按钮之前能用来认人的**唯一**
				// 东西（歌名会重名，而按下去不可恢复）。
				ui.Text(c, detail).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
			})
		ui.Row(c).AlignSelf(ui.End).Gap(8).Children(func() {
			ok := ui.Button(c, deleteCancelLabel)
			if ok.Clicked() {
				pressed = deleteCancelLabel
			}
			yes := ui.Button(c, confirm)
			if danger {
				// ⚠️ 红色的底配白字：主题的 Danger 在浅色下是 #dc2626、深色下
				// 是 #ef4444，两边都够深，白字都读得清（`ui/theme.go` 119/146 行）。
				yes = yes.Background(t.Danger).TextColor(dialogOnDanger)
			}
			if yes.Clicked() {
				pressed = confirm
			}
		})
	})

	if pressed == "" {
		return false
	}
	*open = false
	return pressed == confirm
}

// deleteTrack 真的删。
//
// ⚠️ 删完之后**不**在这里手动改列表：那一行是 `App.DeleteLocalTrack` 改的
// （它顺带把缓存和监听都顾上）。这里只负责把结果说出来。
func (p *localPage) deleteTrack(tr localmusic.Track) {
	if err := p.app.DeleteLocalTrack(tr.ID); err != nil {
		p.say("删不掉：" + err.Error())
		return
	}
	p.say("已从磁盘上删掉「" + tr.Title + "」")
}

// ─────────────────────────────────────────────────────────────────────────────
// 操作
// ─────────────────────────────────────────────────────────────────────────────

// playAll 从当前筛选出来的第一首开始播，把整批接进队列（Biu 的 `playAll`）。
func (p *localPage) playAll() {
	if len(p.rows) == 0 {
		return
	}
	p.playRow(0)
}

// playRow 从第 i 首往后接进队列并开始播。
func (p *localPage) playRow(i int) {
	if i < 0 || i >= len(p.rows) {
		return
	}
	p.acts.Play(p.itemsOf(p.rows[i:]))
}

// queueAll 把当前筛选出来的整批追加到队列末尾（Biu 的 `addAllToPlaylist`）。
func (p *localPage) queueAll() {
	if len(p.rows) == 0 {
		return
	}
	p.acts.Add(p.itemsOf(p.rows))
	p.say(fmt.Sprintf("已把 %d 首加进播放队列", len(p.rows)))
}

// itemsOf 把若干项变成播放队列要的条目。
func (p *localPage) itemsOf(rows []localmusic.Track) []app.Item {
	out := make([]app.Item, len(rows))
	for i, tr := range rows {
		out[i] = app.LocalItem(tr.Path)
	}
	return out
}

// addDirectory 弹系统框选一个文件夹，加进设置。
//
// ⚠️ **必须从后台 goroutine 调**：`mygo.Dialog.Open` 把请求 post 到主线程
// 再等系统回调，主线程自己进去等就是死等，事件循环再也转不起来
// （和 `platform.PickAudioFile` 同一个道理）。
//
// ⚠️ 这一条要能从无头测试里验，所以真弹框的那一步换成了一个可注入的函数
// （`pickDir`）——测试给一个立刻返回的桩，验的是「选完之后设置有没有写进去」。
func (p *localPage) addDirectory() {
	go func() {
		dir, err := p.pickDir()
		if err != nil {
			p.say("选不了目录：" + err.Error())
			return
		}
		if dir == "" {
			return // 用户取消了，不是错误
		}
		p.addRoot(dir)
	}()
}

// addRoot 把一个目录加进设置里；已经在了就说一句，不重复加。
func (p *localPage) addRoot(dir string) {
	cur := p.app.SettingsSnapshot().LocalMusicDirs
	for _, have := range cur {
		if have == dir {
			p.say("这个目录已经在里面了")
			return
		}
	}
	if p.app.SettingsReadOnly() {
		p.say("设置只读，加不了目录")
		return
	}
	next := append(append([]string(nil), cur...), dir)
	if _, err := p.app.UpdateSettings(func(s *app.Settings) {
		s.LocalMusicDirs = next
	}); err != nil {
		p.say("存不住这个目录：" + err.Error())
		return
	}
	// `begin` 每一帧都会拿设置来对一遍根，对不上就换根并重扫——所以这里
	// 不需要自己再调一次 SetLocalRoots。
	p.say("已添加「" + dirName(dir) + "」，正在扫描")
}

// askRemoveDir 记下要移的目录并打开确认。真正的那一步在 viewRemoveDirDialog。
func (p *localPage) askRemoveDir() {
	dir := p.currentRoot(p.app.LocalMusicState().Roots)
	if dir == "" {
		return
	}
	p.rmPath, p.rmOpen = dir, true
}

// removeRoot 把一个目录从设置里拿掉。**一个文件都不删。**
func (p *localPage) removeRoot(dir string) {
	cur := p.app.SettingsSnapshot().LocalMusicDirs
	next := make([]string, 0, len(cur))
	for _, have := range cur {
		if have != dir {
			next = append(next, have)
		}
	}
	if p.app.SettingsReadOnly() {
		p.say("设置只读，移不了目录")
		return
	}
	if _, err := p.app.UpdateSettings(func(s *app.Settings) {
		s.LocalMusicDirs = next
	}); err != nil {
		p.say("存不住这次修改：" + err.Error())
		return
	}
	p.say("已从列表里移除「" + dirName(dir) + "」，文件一个没动")
}

// say 说一句话。notify 是 nil 时什么也不说（那样不会崩）。
func (p *localPage) say(text string) {
	if p.notify != nil {
		p.notify(text)
	}
}
