// 顶栏「滚过了」那条底线（原型 index.html:219-220 `.top { border-bottom: 1px solid transparent }`
// + `.top.scrolled { border-bottom-color: var(--line) }`，app.js:1686 `scrollTop > 4` 就挂上）。
//
// 原型只有一个滚动容器 `#view`；这里每一页各管各的滚动（`ui.Scroll` + `ScrollState`、
// `ui.List` + `ListState`、`list.PagedList` …），框架也没有「子孙滚了多少」的通用读法，
// 于是按当前路由去读那一页自己的滚动状态。只读，不建任何控件、不改页面状态。
package ui

import "github.com/egoist/mygo/ui"

// navScrolledPx 是滚过多少像素顶栏才画底线（原型 app.js:1686 `v.scrollTop > 4`）。
const navScrolledPx = 4

// pageScrolled 报当前路由页的内容区是不是滚离了顶部。
//
// ⚠️ 没挂 `ScrollState` 的列表（`ListState` / `PagedList` 按行换算那一档）读不出精确
// 像素：它们用「第一可见行离开了第 0 行」或 `ScrollOffset() > 0` 判，页内头部还在画面上
// 时算没滚过——底线会比原型晚出现一点，回顶时照样消失。
func (s *Shell) pageScrolled() bool {
	path := s.router.Path()
	switch lookup(path).Match {
	case homePath:
		return s.home != nil && s.home.scroll.Y > navScrolledPx
	case settingsPath:
		return s.settings.scroll.Y > navScrolledPx
	case statsPath:
		return s.statsPage.scroll.Y > navScrolledPx
	case downloadListPath:
		return s.downloads != nil && s.downloads.scroll.Y > navScrolledPx
	case followPath:
		return s.follow != nil && s.follow.scroll.Y > navScrolledPx
	case laterPath, historyPath:
		p := s.libraryPageAt(path)
		return p != nil && p.rows != nil && p.rows.scroll.Y > navScrolledPx
	case localMusicPath:
		return s.local != nil && s.local.cards != nil && s.local.cards.ScrollOffset() > navScrolledPx
	case collectionMatch:
		return s.collection != nil && s.collection.list != nil && s.collection.list.ScrollOffset() > navScrolledPx
	case searchPath:
		st := &s.search
		if st.tab < 0 || st.tab >= len(st.lists) || st.lists[st.tab].list == nil {
			return false
		}
		return st.lists[st.tab].list.ScrollOffset() > navScrolledPx
	case dynamicFeedPath:
		return s.feedPage != nil && listScrolled(&s.feedPage.state)
	case upPath:
		mid, ok := upSpaceMidOf(path)
		if !ok {
			return false
		}
		sp := s.upSpaces[mid]
		if sp == nil {
			return false
		}
		if sp.tab == upTabVideo {
			return sp.videos != nil && sp.videos.ScrollOffset() > navScrolledPx
		}
		if sp.tab < 0 || sp.tab >= len(sp.state) {
			return false
		}
		return listScrolled(&sp.state[sp.tab])
	}
	return false
}

// listScrolled 是只有 `ListState` 的列表的近似判法：第一可见行不再是第 0 行。
func listScrolled(st *ui.ListState) bool {
	first, _ := st.Visible()
	return first > 0
}
