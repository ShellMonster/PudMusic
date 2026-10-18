// 设置页的第二个 Tab：菜单。照 Biu 的 `src/pages/settings/menu-settings.tsx`。
//
// Biu 那一页只有一件事：「设置侧边菜单项显示和隐藏」。本项目多两行——侧栏折叠和
// 侧栏宽度——它们也是改同一个 `app.Settings`（`sideMenuCollapsed` /
// `sideMenuWidth`），摆在这一页是因为它们讲的是同一件事：侧栏长什么样。
//
// ## 一个键两组意思：`hiddenMenuKeys`
//
// 这一格存的是**被藏起来的**那些键（`hiddenMenuKeys`），而界面上画的是**还看得
// 见的**那些。Biu 那一层是 `selectedKeys = groupKeys.filter(k => !value.includes(k))`
// （`menu-settings.tsx:30`），勾上 = 看得见 = 不在 `hiddenMenuKeys` 里。差一层
// 就差一个意思，所以这里把翻来覆去的那几句写成一个函数，不在控件旁边现推。
//
// 还有一层 Biu 有、这里也要有：**不属于这一组的键不能动**。`hiddenMenuKeys` 是
// 一份全局名单，Biu 那边别的组（个人创建的菜单、个人收藏的菜单）往同一个键里写
// 各自的 id（`menu-settings.tsx:57-101`）。那些组我们还没做（收藏夹的界面有了，
// 设置这一页还没有），但名单里的它们的 id 得原样留着——写设置的那一段只处理本组
// 的键（`setHiddenMenuKeys`）。
package ui

import (
	"slices"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// menuGroupName 是那一组的标题。Biu 的 groupName 是「系统默认菜单」。
const menuGroupName = "系统默认菜单"

// settingsMenu 是菜单那一页。
func (s *Shell) settingsMenu(c *ui.Context, st app.Settings, ro bool) {
	// ⚠️ **只摆一个标题**。Biu 那一页把同一个词写了两遍——`<h2>设置侧边菜单项显示和
	// 隐藏</h2>` 之后，分栏左边那个 `text-medium` 又是「系统默认菜单」
	// （`menu-settings.tsx:23-38`）。那边第一句是**整页**的标题、第二句是**这一组**
	// 的名字，所以不算重复；我们这一页还摆着侧栏折叠和侧栏宽度，「整页的标题」
	// 用在这里就不对了，于是只剩「这一组的名字」——所以只摆它一次。
	ui.Row(c).FillWidth().Gap(24).AlignItems(ui.Start).Children(func() {
		ui.Text(c, menuGroupName).FontSize(16).Bold().Width(menuLabelWidth)
		ui.Column(c).Grow(1).Gap(8).Children(func() {
			s.pickMenuVisibility(c, st, ro)
			ui.Divider(c).FillWidth()
			s.pickSideMenu(c, st, ro)
		})
	})
}

// menuLabelWidth 是左边那个标题占多宽。Biu 是 `space-x-[100px]`。
const menuLabelWidth = 100

// pickMenuVisibility 是「哪些菜单项看得见」那一组勾选框。
//
// ⚠️ **不能直接用 `ui.CheckboxGroup`**：它那一个「全选」是 MyGo 自己算的
// （`ui/feedback.go` 的 `all := on == len(boxes) && on > 0`），而这一票明写了一条
// 「全选」判定要**严格长度相等**（PLAN C-50：改成集合比较会让「部分选中」显示成
// 全选）。这里自己画，好让那条判定是这一页的代码而不是框架的，测试也才钉得住。
func (s *Shell) pickMenuVisibility(c *ui.Context, st app.Settings, ro bool) {
	keys := menuGroupKeys()
	// ⚠️ `visible` 是**看得见的那些键**，不是「看得见 / 看不见」的布尔数组。
	// 「全选」那一格判的是它和 `keys` 的**长度**（见 menuAllChecked），所以它
	// 必须是键的列表而不是一排 bool —— 一排 bool 和 keys 永远一样长，那条判定
	// 就成了永远为真的空话。
	visible := visibleMenuKeys(keys, st.HiddenMenuKeys)

	all := menuAllChecked(keys, visible)
	allOn := all
	// ⚠️ 那个 `Label` 不能省，而且**不能只写标题**：左边侧栏上有一列同名菜单项，
	// 界面上是「稍后再看」的那一行；不给名字的话读屏软件念出来只有「复选框」。
	if ui.Checkbox(c, &allOn, "全选").Label("全选：" + menuGroupName).
		Disabled(ro || len(keys) == 0).Changed() {
		// 全选勾上 = `keys` 一个都不藏；全选取消 = 全部藏起来。Biu 是
		// `onSelectionChange(checked ? groupKeys : [])`（`select-all-checkbox-group`）。
		s.setHiddenMenuKeys(keys, pick(allOn, keys, nil))
	}
	// 勾选框的 `*bool` 是**一帧一读的局部量**：Changed 之后立刻写进设置，下一帧
	// 再从设置里读回来。写不进去（只读）时读回旧值，勾自己弹回去。
	for _, k := range keys {
		on := slices.Contains(visible, k)
		if ui.Checkbox(c, &on, menuTitle(k)).
			Label("在侧栏里显示「" + menuTitle(k) + "」").Disabled(ro).Changed() {
			s.setHiddenMenuKeys(keys, withChecked(visible, k, on))
		}
	}
}

// menuGroupKeys 是这一组的键：侧栏的默认菜单项。
//
// ⚠️ **设置页自己不在这份表里**。我们把 `/settings` 挂进了侧栏（Biu 把它挂在头像
// 下拉里），而 Biu 的 `DefaultMenuList`（`common/constants/menus.tsx:18`）里没有
// 它——`groupKeys` 必须是 Biu 那一组，一字不多一字不少，否则用户在这页勾完之后
// 切到 Biu，那边的「系统默认菜单」会多出一个我们独有的项。
func menuGroupKeys() []string {
	var keys []string
	for _, rt := range routes {
		if rt.InMenu && rt.Match != settingsPath {
			keys = append(keys, rt.Match)
		}
	}
	return keys
}

// menuTitle 是一个菜单项显示的名字。折叠成侧栏那一列的第一个字。
func menuTitle(match string) string { return lookup(match).Title }

// menuAllChecked 是「全选」那一格的判定。
//
// **严格长度相等**，不是集合比较（PLAN C-50）。`len(keys) > 0` 且
// `len(visible) == len(keys)`。
//
// 为什么这条要挑这个形状：`visible` 是从 `keys` 里筛出来的子集，所以集合比较在
// 这里恰好也对——但它不对的那一天正是它要防的那一天。`hiddenMenuKeys` 是**一份
// 全局的名单**，别的组（个人创建的菜单、个人收藏的菜单，Biu 的
// `menu-settings.tsx:57-101`）往同一个键里写各自的 id。哪天 `visible` 改成
// 「照着设置里所有没被藏起来的键算」，多出来的那几个就会把长度撑到和 `keys`
// 一样，而集合比较会说「不一样」——于是「全选」明明该亮着，是暗的；反过来
// 一天它又会说「一样」，而其实有一项没勾上。长度相等是 Biu 自己那行的写法
// （`selectedKeys.length === groupKeys.length`），照抄它。
func menuAllChecked(keys, visible []string) bool {
	return len(keys) > 0 && len(visible) == len(keys)
}

// visibleMenuKeys 挑出「还看得见」的那些：一个键不在 `hidden` 里就算看得见。
func visibleMenuKeys(keys, hidden []string) []string {
	var out []string
	for _, k := range keys {
		if !slices.Contains(hidden, k) {
			out = append(out, k)
		}
	}
	return out
}

// withChecked 是勾掉/勾上其中一项之后的可见名单。
//
// 不动输入的那一份：它是这一帧从设置里读出来的，和 `mergeHiddenMenuKeys` 里读的
// `cur.HiddenMenuKeys` 不是同一块内存，改了它等于在没有 Update 的情况下改了设置。
func withChecked(visible []string, key string, on bool) []string {
	out := slices.Clone(visible)
	if !on {
		if i := slices.Index(out, key); i >= 0 {
			out = slices.Delete(out, i, i+1)
		}
		return out
	}
	if slices.Contains(out, key) {
		return out
	}
	return append(out, key)
}

// setHiddenMenuKeys 把「还看得见」的那一份写回 `hiddenMenuKeys`。
//
// 名字和 Biu 的 `handleSelectionChange` 对得上，语义也照它：不属于 `keys` 的那些
// （别的组写进来的）一个都不动。
func (s *Shell) setHiddenMenuKeys(keys, visible []string) {
	s.applySetting("侧栏菜单显示", func(cur *app.Settings) {
		cur.HiddenMenuKeys = mergeHiddenMenuKeys(cur.HiddenMenuKeys, keys, visible)
	})
}

// mergeHiddenMenuKeys 是 `nextHidden = outsideHidden ∪ hiddenInGroup`
// （`menu-settings.tsx:35`）照字面写出来的那一句。
//
// 「全选」那一格传进来的 `visible` 就是 `keys` 本身，它一个都不该被藏——
// `slices.Contains` 对每个键都成立，于是只剩外面那些。
func mergeHiddenMenuKeys(hidden, keys, visible []string) []string {
	var outside []string
	for _, k := range hidden {
		if !slices.Contains(keys, k) {
			outside = append(outside, k)
		}
	}
	for _, k := range keys {
		if !slices.Contains(visible, k) {
			outside = append(outside, k)
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(outside)))
}

// ─────────────────────────────────────────────────────────────────────────────
// 侧栏自己那两行
// ─────────────────────────────────────────────────────────────────────────────

// pickSideMenu 是侧栏折叠和宽度那两行。
//
// 宽度这一行**和拖把手是同一条路**：`commitSideWidth` 写的就是
// `sideMenuWidth` / `sideMenuCollapsed` 那一对（见 sidebar.go），所以这里改了
// 滑块、那边拖把手，两边不会互相打脸。
func (s *Shell) pickSideMenu(c *ui.Context, st app.Settings, ro bool) {
	s.settingRow(c, "折叠侧栏", "收起侧栏，只留一列图标",
		func() {
			s.pickSwitch(c, "折叠侧栏", "折叠侧栏", st.SideMenuCollapsed, ro,
				func(cur *app.Settings, on bool) { cur.SideMenuCollapsed = on })
		})
	w := st.SideMenuWidth
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "侧栏宽度").FontSize(12).Grow(1)
		sl := ui.Slider(c, &w, app.MinSideMenuWidth, app.MaxSideMenuWidth).
			Grow(1).Label("侧栏宽度").Disabled(ro)
		if sl.Changed() {
			s.applySetting("侧栏宽度", func(cur *app.Settings) { cur.SideMenuWidth = w })
		}
		ui.Textf(c, "%.0fpx", w).FontSize(12).Width(radiusReadoutW)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 本地音乐目录
// ─────────────────────────────────────────────────────────────────────────────

// pickLocalMusicDirs 是本地音乐目录那一组：一个一个列出来，每个后面一个 ×，
// 底下「添加文件夹」。
//
// ## 为什么摆在设置页上，而不是「本地音乐」那一页
//
// Biu 是摆在 local-music 页里的（`local-music/index.tsx` 的目录列表）。本项目
// 的 local-music **早就接上了**（本地音乐扫描与页面，issue #30 已 CLOSED，commit
// `2a28190`；那一页有自己的表格 / 列表 / 网格三档、行里的「打开文件夹」和
// 「删除文件」），而这一组目录管理器**一直留在设置页**没搬过去。
//
// ⚠️ 这一点曾经被写反过：这段注释说「local-music 现在还是一个空状态……等它接上
// 之后再搬过去」。接上早就在它之前发生了，而「等接上之后」这句话挂在代码上挂了
// 很久——读的人会以为这一页还是占位，于是要么去重做一遍，要么以为这里摆错了地方。
// **这一页和 Biu 不一致是真的**（Biu 把目录管理器摆在本地音乐页里），摆错位置是
// 一件该改的事，但它不是「因为那一页还没接上」。
func (s *Shell) pickLocalMusicDirs(c *ui.Context, st app.Settings, ro bool) {
	t := c.Theme()
	s.settingSection(c, "音乐库")
	ui.Row(c).FillWidth().Gap(24).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, "本地音乐目录").FontSize(14).Bold()
			ui.Text(c, "从这些文件夹里读取本地音乐").FontSize(12).TextColor(t.TextMuted).Wrap()
		})
		if flatButton(c, "添加文件夹").Width(96).Disabled(ro).Clicked() {
			s.pickPath("本地音乐目录", true, func(path string) {
				s.applySetting("本地音乐目录", func(cur *app.Settings) {
					// 不加重复的：同一个目录摆两遍，音乐列表里也会出现两遍。
					if !slices.Contains(cur.LocalMusicDirs, path) {
						cur.LocalMusicDirs = append(slices.Clone(cur.LocalMusicDirs), path)
					}
				})
			})
		}
	})

	if len(st.LocalMusicDirs) == 0 {
		ui.Text(c, "还没有目录").FontSize(12).TextColor(t.TextMuted).Padding(4, 0, 0, 0)
		return
	}
	// ⚠️ **重复的目录只画一次**。设置文件是用户能直接编辑的（也能从 Biu 那边来），
	// 两个一模一样的目录在文件里完全合法——而这一行的 key 就是路径：MyGo 在同一层里
	// 拿到两个一样的 key 会**当场 panic**（`ui/context.go` 的 duplicateKey），那一格
	// 带着整个设置页换成错误页，用户之前改好的设置全看不见。
	//
	// 这里去重而不是去改设置：写设置是 `internal/app` 那一层的事，那份文件不是这一票
	// 的；而重复的目录本来就该只出现一遍（Biu 的 local-music 页也是这么去的）。
	seen := make(map[string]bool, len(st.LocalMusicDirs))
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		for i, dir := range st.LocalMusicDirs {
			if seen[dir] {
				continue
			}
			seen[dir] = true
			ui.Row(c).Key("local-dir:" + dir).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				// ⚠️ 这一行的 key 用的是**路径**，不是序号（🔴 铁律四）：删掉中间
				// 那一条之后序号会整体前移一格，按序号排的身份就落到旁边那条上
				// 了，而 × 删掉的是它自己那一格还是隔壁那一格，从画面上看不出来。
				ui.Text(c, dir).FontSize(13).Grow(1).SingleLine()
				if flatButton(c, "×").Label("移除目录「" + dir + "」").
					FontSize(13).TextColor(t.TextMuted).Disabled(ro).Clicked() {
					s.removeLocalMusicDir(i)
				}
			})
		}
	})
}

// removeLocalMusicDir 删掉第 i 个目录。
//
// ⚠️ 一定要 `Clone` 之后才 `Delete`：`SettingsSnapshot` 返回的切片是**和 store
// 共享**的（`internal/app/settings.go` 的 Snapshot 注释里写着），直接 `Delete` 会
// 把 store 里的顺序改掉，而那一格还没写盘——用户下次打开看到的顺序是乱的。
func (s *Shell) removeLocalMusicDir(i int) {
	s.applySetting("本地音乐目录", func(cur *app.Settings) {
		list := slices.Clone(cur.LocalMusicDirs)
		if i < 0 || i >= len(list) {
			return
		}
		cur.LocalMusicDirs = slices.Delete(list, i, i+1)
	})
}
