// 设置页的「快捷键」Tab：原型 `SETTINGS.shortcut`——启用全局快捷键开关 + 一张三列表
// （命令 / 应用内快捷键 / 全局快捷键），每一格是 `.kbd-rec` 录制键（录制中 / 未设置两种状态）。
//
// 它读的不是 `app.Settings`：快捷键在**单独一份文件**里（见 shortcuts.go）。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// enableGlobalLabel 是开关的标题。
const enableGlobalLabel = "启用全局快捷键"

// 表的三列：命令吃剩下的，两个键列定宽（`.keys td` 左右 20）。
const (
	keysAppColW    = 240
	keysGlobalColW = 140
)

// settingsShortcuts 是快捷键那一页。
func (s *Shell) settingsShortcuts(c *ui.Context) {
	p := tok.Of(c.Theme())
	cur := s.keys.snapshot()
	ro := s.keys.readOnly()

	// 这一帧画到了录制格——viewShortcuts 靠它判断「正在录」还有没有意义。
	s.recorderShown = true

	if ro {
		ui.Text(c, "快捷键只读：这份文件来自更新的版本，这一版一个字都不会写回去。").
			FontSize(scale.Font(c, scale.Size12)).TextColor(tok.Of(c.Theme()).Err).
			Margin(space.None, space.None, space.Px12, space.None)
	}
	s.setGroup(c, "keys", "快捷键设置", func() {
		s.card(c, "keys", ro, func(cd *setCard) {
			cd.row("globalKeys", enableGlobalLabel, "应用在后台时，全局那一列依然生效", func() {
				on := cur.EnableGlobalShortcuts
				if pud.Switch(c, "set-sw:globalKeys", &on, "开关："+enableGlobalLabel) && !ro {
					s.applyShortcutChange(enableGlobalLabel, func() error { return s.keys.setEnableGlobal(on) })
				}
			})

			// 表头（`.keys th`：12 号 500 text-3，上 12 下 4、左右 20）。
			ui.Row(c).Key("keys-head").FillWidth().Padding(space.Px12, setRowPadX, space.Px4, setRowPadX).Children(func() {
				head := func(s string) ui.Element {
					return ui.Text(c, s).FontSize(scale.Font(c, scale.Size12)).FontWeight(500).TextColor(p.Text3).SingleLine()
				}
				head("命令").Grow(1)
				head("应用内快捷键").Width(keysAppColW)
				ui.Row(c).Width(keysGlobalColW).Justify(ui.End).Children(func() { head("全局快捷键") })
			})

			// 焦点那一截在循环**之外**按，见 `keepRecordingFocus` 的注释。
			var rec ui.Element
			for i, in := range cur.Shortcuts {
				global, _ := findShortcut(in.ID, cur.GlobalShortcuts)
				if r := s.shortcutRow(c, in, global, cur, ro, i > 0); r.Valid() {
					rec = r
				}
			}
			s.keepRecordingFocus(rec)
		})
		ui.Text(c, "点一下快捷键再按下新组合；Esc 取消，Delete 清空。").
			FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text3).
			Margin(space.Px8, space.None, space.None, space.None)
	})
}

// shortcutRow 是「命令 + 两个录制格」那一行（`.keys td`：上下 14、左右 20、行间 1px line）。
//
// ⚠️ key 用命令 id（铁律四）。
func (s *Shell) shortcutRow(c *ui.Context, in, global shortcutItem, all shortcutSettings, ro, divider bool) (rec ui.Element) {
	p := tok.Of(c.Theme())
	// 名对不上的时候用这一帧的 `in.Name`：用户改了设置文件里的名字，界面上跟的是他改过的。
	name := in.Name
	if name == "" {
		name = global.Name
	}
	if divider {
		ui.Box(c).Key("shortcut-div:" + in.ID).FillWidth().Height(1).Background(p.Line)
	}
	ui.Row(c).Key("shortcut:"+in.ID).FillWidth().Padding(space.Px14, setRowPadX).AlignItems(ui.Center).Children(func() {
		ui.Text(c, name).FontSize(scale.Font(c, scale.Size14)).TextColor(p.Text).Grow(1)

		inSlot := shortcutSlot{id: in.ID}
		globalSlot := shortcutSlot{global: true, id: in.ID}
		other, bad := conflictOf(all.Shortcuts, in.ID, in.Shortcut)
		ui.Row(c).Width(keysAppColW).Children(func() {
			if inCell := s.recorderCell(c, in, inSlot, conflictText(other, bad), ro); inCell.Valid() {
				rec = inCell
			}
		})

		other, bad = conflictOf(all.GlobalShortcuts, global.ID, global.Shortcut)
		ui.Row(c).Width(keysGlobalColW).Justify(ui.End).Children(func() {
			s.recorderCell(c, global, globalSlot, conflictText(other, bad), ro)
		})
	})
	return rec
}

// recorderCell 是一格录制框，底下那一行是冲突时的那句话（放在框外面：放在框里的话框要变高，
// 两列的行高就不一样了）。
func (s *Shell) recorderCell(c *ui.Context, item shortcutItem, slot shortcutSlot, conflict string, ro bool) (rec ui.Element) {
	ui.Column(c).Gap(space.Px2).Children(func() {
		rec = s.shortcutRecorder(c, item, slot, conflict, ro)
		if conflict != "" {
			ui.Text(c, conflict).FontSize(scale.Font(c, scale.Size11)).TextColor(tok.Of(c.Theme()).Err)
		}
	})
	return rec
}

// conflictText 是冲突时那句话。Biu：`与"${existing.name}"冲突`。
//
// 那个 ok 一路带上来是为了让「描红」和「写字」是同一个判断。
func conflictText(other shortcutItem, ok bool) string {
	if !ok {
		return ""
	}
	return "与「" + other.Name + "」冲突"
}

// applyShortcutChange 存一次改动；存不住就弹提示。存住之后把全局那一列重新登记一遍，菜单上
// 那些命令项的加速键也要跟着变（票 #716：旧键会被菜单吃掉）。
func (s *Shell) applyShortcutChange(what string, fn func() error) {
	if err := fn(); err != nil {
		s.addToast(what+"没存上："+err.Error(), notice.Danger)
		return
	}
	s.syncGlobalShortcuts()
	s.refreshMenuAccel()
}
