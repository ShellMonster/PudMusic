package ui

// openQueueDrawer / openCommentsDrawer 是两个右侧抽屉的唯一打开入口。
//
// 抽屉的阶段机仍各自负责进退场，但用户意图只有一份：打开其中一个时，
// 另一个立刻收到关闭意图。这样普通窗口、满屏播放器、播放条和快捷键不会
// 各自维护一套互斥规则。

// openQueueDrawer 打开播放列表，并关闭评论抽屉的用户意图。
func (s *Shell) openQueueDrawer() {
	s.commentsOpen = false
	s.drawerOpen = true
}

// toggleQueueDrawer 切换播放列表。已经打开时只关闭，不会因为评论抽屉的
// 退场阶段仍在画而再次打开评论。
func (s *Shell) toggleQueueDrawer() {
	if s.drawerOpen {
		s.drawerOpen = false
		return
	}
	s.openQueueDrawer()
}

// openCommentsDrawer 打开评论，并关闭播放列表抽屉的用户意图。
func (s *Shell) openCommentsDrawer() {
	s.drawerOpen = false
	s.commentsOpen = true
}

// closeQueueDrawer 关闭播放列表的用户意图。
func (s *Shell) closeQueueDrawer() { s.drawerOpen = false }

// closeCommentsDrawer 关闭评论抽屉的用户意图。
func (s *Shell) closeCommentsDrawer() { s.commentsOpen = false }
