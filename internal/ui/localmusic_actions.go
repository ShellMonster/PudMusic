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
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
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
	//
	// ⚠️ **danger 那一档是红的**（上游 `local-music/index.tsx:180` 的
	// `type: "danger"`）：一个绿色的「删除」和行里那个红色的「删除文件」摆在一起，
	// 是在说两件不一样的事——而这两个按钮指的是**同一个动作**。
	if confirmModal(c, &p.askOpen, confirmSpec{
		tone:    toneDanger,
		title:   deleteDialogTitle,
		body:    deleteDialogBody,
		detail:  tr.Path,
		confirm: deleteConfirmLabel,
		cancel:  deleteCancelLabel,
	}) == confirmAccepted {
		p.deleteTrack(tr)
	}
	// ⚠️ 判「关掉了」用 `!p.askOpen`：**弹窗刚打开的那一帧 `confirmModal`
	// 报的是「还开着」**（还没有人按任何按钮），而这一帧 `*open` 还是 true。
	// 两个条件都满足才清掉待删项——判错的话弹窗画出来的那一瞬间目标就没了，
	// 按钮还在而按下去什么也不发生。
	if !p.askOpen {
		p.askTrack = localmusic.Track{}
	}
}

// viewRemoveDirDialog 是移除目录那道确认。
//
// ⚠️ **warning 那一档是橙的，不是红的**（上游 `local-music/index.tsx:113` 的
// `type: "warning"`）：这一下只从列表里拿掉目录、不删文件（正文里写着这一句），
// 比「删文件」轻一档——而红色会让用户以为歌也没了。
func (p *localPage) viewRemoveDirDialog(c *ui.Context) {
	dir := p.rmPath
	if dir == "" {
		return
	}
	if confirmModal(c, &p.rmOpen, confirmSpec{
		tone:    toneWarning,
		title:   removeDirTitle,
		body:    removeDirBody,
		detail:  dir,
		confirm: "移除",
		cancel:  deleteCancelLabel,
	}) == confirmAccepted {
		p.removeRoot(dir)
		p.dirLabel = allDirs
	}
	if !p.rmOpen {
		p.rmPath = ""
	}
}

// deleteTrack 真的删。
//
// ⚠️ 删完之后**不**在这里手动改列表：那一行是 `App.DeleteLocalTrack` 改的
// （它顺带把缓存和监听都顾上）。这里只负责把结果说出来。
func (p *localPage) deleteTrack(tr localmusic.Track) {
	if err := p.app.DeleteLocalTrack(tr.ID); err != nil {
		p.say("删不掉："+err.Error(), notice.Danger)
		return
	}
	p.say("已从磁盘上删掉「"+tr.Title+"」", notice.Success)
}

// ─────────────────────────────────────────────────────────────────────────────
// 操作
// ─────────────────────────────────────────────────────────────────────────────

// playAll 从当前筛选出来的第一首开始播，把队列**换成**整批（Biu 的 `playAll`）。
func (p *localPage) playAll() {
	if len(p.rows) == 0 {
		return
	}
	p.playRow(0)
}

// playRow 把队列换成第 i 首往后的那一段并从第 i 首开始播。
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
	p.say(fmt.Sprintf("已把 %d 首加进播放队列", len(p.rows)), notice.Success)
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
			p.say("选不了目录："+err.Error(), notice.Danger)
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
			p.say("这个目录已经在里面了", notice.Plain)
			return
		}
	}
	if p.app.SettingsReadOnly() {
		p.say("设置只读，加不了目录", notice.Danger)
		return
	}
	next := append(append([]string(nil), cur...), dir)
	if _, err := p.app.UpdateSettings(func(s *app.Settings) {
		s.LocalMusicDirs = next
	}); err != nil {
		p.say("存不住这个目录："+err.Error(), notice.Danger)
		return
	}
	// `begin` 每一帧都会拿设置来对一遍根，对不上就换根并重扫——所以这里
	// 不需要自己再调一次 SetLocalRoots。
	p.say("已添加「"+dirName(dir)+"」，正在扫描", notice.Success)
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
		p.say("设置只读，移不了目录", notice.Danger)
		return
	}
	if _, err := p.app.UpdateSettings(func(s *app.Settings) {
		s.LocalMusicDirs = next
	}); err != nil {
		p.say("存不住这次修改："+err.Error(), notice.Danger)
		return
	}
	p.say("已从列表里移除「"+dirName(dir)+"」，文件一个没动", notice.Success)
}

// say 说一句话。notify 是 nil 时什么也不说（那样不会崩）。
//
// ⚠️ **语气跟着话一起走**（票 #180）：成功是绿的、失败是红的——而这一页的
// 话有成的也有没成的，只给一句「删不掉」是说不清的。
func (p *localPage) say(text string, tone notice.Tone) {
	if p.notify != nil {
		p.notify(text, tone)
	}
}
