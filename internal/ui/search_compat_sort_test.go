package ui

func sortOf(tab, i int) int {
	n := searchTabSortCount(tab)
	if i < 0 || i >= n {
		return 0
	}
	return i
}

func searchTabSortCount(tab int) int {
	if tab == searchTabUser {
		return len(searchUserSorts)
	}
	return len(searchVideoSorts)
}
