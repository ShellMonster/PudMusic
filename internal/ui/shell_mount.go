// 这一块是**挂载与杂项**：把「要落盘的那几样」挂到盘上，外加几件和窗口、视图都
// 不相干、但也没别处可放的小事。
//
//   - `mountSearchHistory` / `mountShortcuts` —— 搜索历史和快捷键挂到盘上。
//   - `coverCacheDir` —— 封面磁盘缓存的位置（一个接缝，测试要把它替掉）。
//   - `pickDir` —— 本地音乐页「添加目录」那一次系统对话框。
//   - `viewLocalMusic` —— 本地音乐页。
//   - `openFeedback` —— 把用户送到 issue 区。
//   - `enqueuePicked` —— 文件框选回来的路径排进队列。
package ui

import (
	"log"
	"path/filepath"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/platform"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// coverCacheDir 是封面磁盘缓存的位置：**应用自己的缓存目录**下单独一个子目录。
//
// `mygo.PathCache` 是 `os.UserCacheDir()` 拼上应用名（macOS 上是
// `~/Library/Caches/BiuGo`），系统认定的「这些都可以删」的地方——不是用户文件
// 目录，也不在应用数据目录（`Biu MyGo`）里，所以清缓存不会碰到设置和历史。
// 再套一层 `cover-images` 是为了不让别的东西混进这个「整目录都归缓存」的地方。
//
// ⚠️ **测试必须把它替成临时目录**（`newTestShell` 替了）：无头 Tester 里
// `mygo.App.Path` 照样会返回一个真的、可写的目录，不替的话每条测试都会往用户
// 真正的缓存目录里写封面，而且测试之间串数据。
//
// 拿不到缓存目录不算错误：返回空串 = 不落盘，封面照常从网上下来，只是下次启动
// 要再下一次。为缓存挡住窗口不值当。
var coverCacheDir = func() string {
	dir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		log.Printf("biu: 拿不到缓存目录，封面这次不落盘：%v", err)
		return ""
	}
	return filepath.Join(dir, "cover-images")
}

// pickDir 是本地音乐页「添加目录」要用的那一次系统对话框。
//
// 界面上没有它就进不去任何一个目录，而这一页的第一句空状态就是「点右上角
// 「添加目录」选一个文件夹」——那句话和这个函数是同一件事的两半。
//
// ⚠️ 返回的路径**不保证**是绝对路径（macOS 的对话框有时给相对路径），
// `app.SetLocalRoots` 那边会自己补成绝对路径。
func (s *Shell) pickDir() (string, error) {
	return platform.PickDirectory(s.win)
}

// viewLocalMusic 是本地音乐页。
//
// 页面级的 recover 在 `Shell.page` 上（和其余 14 条路由共用那道 Guard），
// 这一页自己的 view 另外还包了一层（`localPage` 构造时包，见 localmusic.go）。
func (s *Shell) viewLocalMusic(c *ui.Context, _ *ui.Route) {
	s.local.View(c)
}

// mountSearchHistory 把搜索历史挂到盘上。
//
// 拿不到应用数据目录不算错误——记一条日志，接着用内存里那一份。搜索照常能用，
// 只是关掉之后不记得搜过什么。为一件可有可无的东西挡住开窗口不值当。
func (s *Shell) mountSearchHistory() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，搜索历史这次关掉就没了：%v", err)
		return
	}
	if err := s.hist.Close(); err != nil {
		log.Printf("biu: 收尾内存里的搜索历史失败：%v", err)
	}
	s.hist = newSearchHistory(searchHistoryPath(dir))
}

// mountShortcuts 把快捷键挂到盘上。和搜索历史同一套规矩：拿不到应用数据目录不算
// 错误——记一条日志，接着用内存里那一份，设置页照常能录，只是关掉之后不记得。
func (s *Shell) mountShortcuts() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，快捷键这次关掉就没了：%v", err)
		return
	}
	if err := s.keys.Close(); err != nil {
		log.Printf("biu: 收尾内存里的快捷键失败：%v", err)
	}
	s.keys = newShortcutKeys(shortcutPath(dir))
}

// openFeedback 把用户送到 issue 区——和头像下拉里那个「问题反馈」是同一个地址
// （`navbar_avatar.go` 的 `feedbackURL`）。
//
// ⚠️ **用 `c.OpenURL` 而不是 `Window.Open`**：后者收到 nil 会直接把进程带走
// （见 navbar_avatar.go 那一段注释），而这一页是在崩溃之后画出来的，那一刻
// 什么都是不可信的。
//
// ⚠️ 它**同时记一笔**（`lastFeedbackURL`）：无头 Tester 里 `OpenURL` 走不了事件
// 循环，于是「点得动」这件事除了「按钮在」之外一句都验不到。记一笔是给测试的
// 那一件证据，生产上多写一个字段的代价可以忽略。
func (s *Shell) openFeedback(c *ui.Context) {
	s.lastFeedbackURL = feedbackURL
	c.OpenURL(feedbackURL)
}

// enqueuePicked 把文件框选回来的每一条路径都排进队列。
//
// 一条都不能少：少了的那几条在界面上看不出任何异样，用户只会以为是自己没选上。
//
// 为什么单独拎出来：文件框要系统面板，测试里调不了；路径进队列这一步是纯逻辑，
// 拎出来才测得到（见 shell_pick_test.go）。弹框本身只有一个调用方
// （pickAndEnqueue），在这里没有第二条路。
func (s *Shell) enqueuePicked(paths []string) {
	items := make([]app.Item, len(paths))
	for i, p := range paths {
		items[i] = app.LocalItem(p)
	}
	s.app.Enqueue(items...)
	// ⚠️ **随机档要说一句**：那一档的洗牌袋是**池子**——加进来的歌立刻在池中，
	// 这一轮就轮到它（见 app/queue_shuffle.go）。用户看不见池子里有什么。
	if n := s.app.PlayMode().EnqueueNotice(); n != "" {
		s.addToast(n, notice.Success)
	}
}
