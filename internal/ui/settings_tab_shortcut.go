// 设置页的第三个 Tab：快捷键。照 Biu 的
// `src/pages/settings/shortcut-settings.tsx`，连三列的宽度都照抄
// （`grid-cols-[1fr_200px_200px]`，那一行在 `:102`）。
//
// ## 冲突怎么判、判出来画成什么样
//
// 照 Biu 的 `handleChangeShortcut`（`shortcut-settings.tsx:23-39`）：**只在同一列
// 之内**比，两列互不相干——应用内的 ⌘P 和全局的 ⌘P 可以同时存在，因为全局那个
// 是在别的应用里按的。空键不算冲突（同一段里明写着「空快捷键不会冲突」）。
//
// 冲突的那一格描红，边上写一句「与「音量加」冲突」。Biu 用的是红色描边 +
// errorMessage，抄的形状。
//
// ## 每一行的 key 用命令 id，不用序号
//
// 🔴 铁律四（`SPEC.md`）：动态列表的行要给**稳定且唯一**的 key，默认的元素身份是
// 「父 ID + 兄弟序号」。这一页的行会随设置文件里有的命令而变，用序号的话前面
// 少一行，后面所有行的录制状态（`FocusBind` 记在那一格上）会整体错配——用户点
// 「音量加」结果开始录「音量减」。所以用 id。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// enableGlobalLabel 是底下那个开关的字。Biu 是 `启用全局快捷键`。
const enableGlobalLabel = "启用全局快捷键"

// settingsShortcuts 是快捷键那一页。
//
// 它读的不是 `app.Settings`：快捷键在**单独一份文件**里（Biu 自己也是单独一个
// store，见 shortcuts.go）。
func (s *Shell) settingsShortcuts(c *ui.Context) {
	t := c.Theme()
	cur := s.keys.snapshot()
	ro := s.keys.readOnly()

	// 这一帧画到了录制格——viewShortcuts 靠它判断「正在录」还有没有意义
	//（这一页不画了，录制状态就该收掉，理由见那里）。
	s.recorderShown = true

	ui.Column(c).FillWidth().Gap(12).Children(func() {
		// 顶上那一行：Biu 的 `<h2>快捷键设置</h2>` 和右边的「恢复默认」。
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "快捷键设置").FontSize(scale.Font(c, 20)).Grow(1)
			if flatButton(c, "恢复默认").FontSize(scale.Font(c, 12)).Disabled(ro).Clicked() {
				s.applyShortcutChange("快捷键", s.keys.reset)
			}
		})

		if ro {
			ui.Text(c, "快捷键只读：这份文件来自更新的版本，这一版一个字都不会写回去。").
				FontSize(scale.Font(c, 12)).TextColor(t.Danger)
		}
		// 这一页两列分别管两件事，写出来，不然用户会以为「全局」那一列只是应用内
		// 那一列的备份。挂掉的时候另有提示（syncGlobalShortcuts 弹的那一句），
		// 写在这一页的是常态。
		//
		// ⚠️ 「切换mini/完整模式」那一条要在括号里说清楚：它切的是**两个窗口**
		// （主窗口收起来 + 开一个 320×100 的小窗），所以从别的应用按它同样有
		// 用——而别的几条应用内快捷键（下一首、暂停）不行，它们只是往这个进程
		// 的状态里写一笔，界面上看不出变化。理由见 `shortcut_dispatch.go` 里
		// `toggleMiniPlayer` 的注释。
		ui.Text(c, "应用内那一列：窗口在前台时按它就有用。全局那一列：切到别的"+
			"应用时按它也有用（底下那个开关关掉就一个都不登记；「切换mini/完整模式」"+
			"那一条切的是窗口，所以别的应用在前台时它也管用）。焦点在搜索框这类"+
			"输入框里时快捷键让路，打的字照常进去。").
			FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)

		// 三列的表头。Biu 的 `grid grid-cols-[1fr_200px_200px]`（`:102`）。
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			ui.Text(c, "功能说明").FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).Grow(1)
			ui.Text(c, "应用内快捷键").FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).Width(shortcutFieldWidth)
			ui.Text(c, "全局快捷键").FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).Width(shortcutFieldWidth)
		})

		// 焦点那一截在循环**之外**按，见 `keepRecordingFocus` 的注释。
		var rec ui.Element
		for _, in := range cur.Shortcuts {
			global, _ := findShortcut(in.ID, cur.GlobalShortcuts)
			if r := s.shortcutRow(c, in, global, cur, ro); r.Valid() {
				rec = r
			}
		}
		s.keepRecordingFocus(rec)

		// 底下那个开关。Biu 放在右下角（`:134-136`）。
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			on := cur.EnableGlobalShortcuts
			if ui.Checkbox(c, &on, enableGlobalLabel).Disabled(ro).Changed() {
				s.applyShortcutChange(enableGlobalLabel, func() error {
					return s.keys.setEnableGlobal(on)
				})
			}
		})
	})
}

// shortcutRow 是「功能说明 + 两个录制格」那一行。
//
// ⚠️ key 用命令 id（铁律四，见文件头）。
func (s *Shell) shortcutRow(c *ui.Context, in, global shortcutItem, all shortcutSettings, ro bool) (rec ui.Element) {
	// 名对不上的时候用这一帧的 `in.Name`：用户改了设置文件里的名字，界面上跟的是
	// 他改过的那个，不是默认的那个。
	name := in.Name
	if name == "" {
		name = global.Name
	}

	ui.Row(c).Key("shortcut:" + in.ID).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, name).FontSize(scale.Font(c, 14)).Grow(1)

		inSlot := shortcutSlot{id: in.ID}
		globalSlot := shortcutSlot{global: true, id: in.ID}
		other, bad := conflictOf(all.Shortcuts, in.ID, in.Shortcut)
		if inCell := s.recorderCell(c, in, inSlot, conflictText(other, bad), ro); inCell.Valid() {
			rec = inCell
		}

		other, bad = conflictOf(all.GlobalShortcuts, global.ID, global.Shortcut)
		s.recorderCell(c, global, globalSlot, conflictText(other, bad), ro)
	})
	return rec
}

// recorderCell 是一格录制框，底下那一行是冲突时的那句话。
//
// 冲突的字放在**框外面**：放在框里的话框要变高，两列的行高就不再一样，表格看着
// 是歪的。
func (s *Shell) recorderCell(c *ui.Context, item shortcutItem, slot shortcutSlot, conflict string, ro bool) (rec ui.Element) {
	ui.Column(c).Width(shortcutFieldWidth).Gap(2).Children(func() {
		rec = s.shortcutRecorder(c, item, slot, conflict, ro)
		if conflict != "" {
			ui.Text(c, conflict).FontSize(scale.Font(c, 11)).TextColor(c.Theme().Danger)
		}
	})
	return rec
}

// conflictText 是冲突时那句话。Biu：`与"${existing.name}"冲突`。
//
// 那个 ok 一路带上来是为了让「描红」和「写字」是**同一个**判断：分开判的话哪天
// 改了一处，另一处还在按旧条件描红，界面上就出现一格红的却没说是撞了谁。
func conflictText(other shortcutItem, ok bool) string {
	if !ok {
		return ""
	}
	return "与「" + other.Name + "」冲突"
}

// applyShortcutChange 存一次改动；存不住就弹提示。
//
// 和 `applySetting` 同一个道理：静默丢掉是这一页最坏的失败方式——用户以为改了，
// 下次打开又变回去，中间屏幕上什么也没发生过。
//
// 存住之后要把全局那一列重新登记一遍（`syncGlobalShortcuts`）：不改的话用户在
// 这一页录了 ⌘⌥K，切到别的应用按 ⌘⌥K 还是上一条——而界面上明明白白写着新键。
func (s *Shell) applyShortcutChange(what string, fn func() error) {
	if err := fn(); err != nil {
		s.addToast(what+"没存上："+err.Error(), notice.Danger)
		return
	}
	s.syncGlobalShortcuts()
}
