// 设置页的「菜单」Tab：原型 `SETTINGS.menu`——侧栏各入口的显示开关、收藏夹显示开关。
//
// ## 一个键两组意思：`hiddenMenuKeys`
//
// 存的是**被藏起来的**那些键，而界面上画的是**还看得见的**那些（开关开 = 看得见 = 不在
// `hiddenMenuKeys` 里）。名单是一份全局的：菜单项存路由（"/later"），收藏夹存十进制 id；
// 写设置的那一段只处理本组的键，别的组写进来的原样留着（`mergeHiddenMenuKeys`）。
package ui

import (
	"slices"
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// settingsMenu 是菜单那一页。
func (s *Shell) settingsMenu(c *ui.Context, st app.Settings, ro bool) {
	p := tok.Of(c.Theme())
	s.setGroup(c, "menus", "侧栏菜单显示", func() {
		// 原型 `.muted` 一句说明，上收 4、下留 12（和 h3 下面的 12 叠起来是 12 + 12 - 4）。
		ui.Text(c, "关掉的入口不会出现在侧栏里，页面本身和数据都还在。").
			FontSize(scale.Font(c, scale.Size14)).TextColor(p.Text2).Wrap().
			Margin(space.Px4*-1, space.None, space.Px12, space.None)
		s.card(c, "menus", ro, func(cd *setCard) { s.pickMenuVisibility(cd, st) })
	})
	s.setGroup(c, "folders", "收藏夹显示", func() {
		s.card(c, "folders", ro, func(cd *setCard) { s.pickFavoriteVisibility(cd, st) })
	})
}

// pickMenuVisibility 是「哪些菜单项看得见」那一组开关：每个入口一行，只有标题。
func (s *Shell) pickMenuVisibility(cd *setCard, st app.Settings) {
	keys := menuGroupKeys()
	visible := visibleMenuKeys(keys, st.HiddenMenuKeys)
	for _, k := range keys {
		on := slices.Contains(visible, k)
		title := menuTitle(k)
		cd.row("menu:"+k, title, "", func() {
			if pud.Switch(cd.c, "set-sw:menu:"+k, &on, "开关：在侧栏里显示「"+title+"」") && !cd.ro {
				s.setHiddenMenuKeys(keys, withChecked(visible, k, on))
			}
		})
	}
}

// pickFavoriteVisibility 是「哪些收藏夹摆得上侧栏」那一组开关：标题 + 「N 首」。
//
// 「我创建的」在上、「我收藏的」在下，和侧栏同一个顺序。名单里还留着、可收藏夹已经不在接口
// 返回里的 id（在 B 站那边删了、或者换过账号）也摆出来，不然那几笔永远躺在设置文件里。
func (s *Shell) pickFavoriteVisibility(cd *setCard, st app.Settings) {
	c := cd.c
	hidden := hiddenMenuKeys(st)
	seen := make(map[string]bool)
	any := false
	for g := range favGroups {
		grp := app.FavoriteGroup(g)
		for _, f := range s.app.SideFavorites(grp) {
			any = true
			key := strconv.FormatInt(f.ID, 10)
			seen[key] = true
			on := !hidden[key]
			title := f.Title
			if title == "" {
				title = "（没名字）"
			}
			help := grp.String() + " · " + strconv.Itoa(f.MediaCount) + " 首"
			cd.row("fav:"+key, title, help, func() {
				if pud.Switch(c, "set-sw:fav:"+key, &on, "开关：在侧栏里显示收藏夹「"+title+"」") && !cd.ro {
					s.setFavoriteHidden(f.ID, !on)
				}
			})
		}
	}
	for _, k := range st.HiddenMenuKeys {
		if _, err := strconv.ParseInt(k, 10, 64); err != nil || seen[k] {
			continue
		}
		any = true
		off := false
		cd.row("fav:"+k, "收藏夹 "+k, "已不在列表里，打开可取消隐藏", func() {
			if pud.Switch(c, "set-sw:fav:"+k, &off, "开关：取消隐藏已不在列表里的收藏夹 "+k) && !cd.ro {
				s.setFavoriteHidden(mustID(k), false)
			}
		})
	}
	if !any {
		cd.row("fav-empty", s.favEmptyText(s.app.LoggedIn()), "", nil)
	}
}

// setFavoriteHidden 把这一个收藏夹在 `hiddenMenuKeys` 里写进 / 拿掉。
//
// 不走 `setHiddenMenuKeys`：那一个要的是「这一组的 keys + 勾上的 keys」，
// 而收藏夹这组没有「全选」，逐行写就行——只动这一个 id，名单里别的键
// （菜单项、别的收藏夹）一个都不碰。
func (s *Shell) setFavoriteHidden(id int64, hide bool) {
	key := strconv.FormatInt(id, 10)
	s.applySetting("收藏夹显示", func(cur *app.Settings) {
		if hide {
			if !slices.Contains(cur.HiddenMenuKeys, key) {
				cur.HiddenMenuKeys = append(slices.Clone(cur.HiddenMenuKeys), key)
			}
			return
		}
		cur.HiddenMenuKeys = slices.DeleteFunc(slices.Clone(cur.HiddenMenuKeys),
			func(k string) bool { return k == key })
	})
}

// mustID 把一个十进制字符串转回 id。**只有 ParseInt 过得去的才走到这**
// （见 pickFavoriteVisibility 里 orphans 的判定）。
func mustID(k string) int64 {
	id, _ := strconv.ParseInt(k, 10, 64)
	return id
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
