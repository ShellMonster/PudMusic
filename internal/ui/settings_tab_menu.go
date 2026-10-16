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
	s.settingSection(c, menuGroupName)
	// 一格复选框 Biu 摆的是一个标题 + 一组勾选框（`flex items-start space-x-[100px]`），
	// 这里照抄那个左右分栏的排法。
	ui.Row(c).FillWidth().Gap(24).AlignItems(ui.Start).Children(func() {
		ui.Text(c, menuGroupName).FontSize(14).Bold().Width(menuLabelWidth)
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
