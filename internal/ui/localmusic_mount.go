package ui

import "github.com/ShellMonster/Biu_Go/internal/app"

// MountLocalMusic 挂上本地音乐库。
//
// **必须在开窗口之前调**，但**不是为了读盘**（本地音乐不像下载要恢复队列，
// 没有什么要在开窗前准备好）。真正的原因是：挂上之后 `App.Close` 才知道要
// 关掉那几摊文件监听，而 `Close` 是在退出流程里跑的。
//
// 挂不上也不拦开窗口——这一层没有任何会失败的东西，所以这里没有 error 可回。
// 万一以后有了，`app.LocalMusicMounted` 会让那一页画一句人话而不是崩。
//
// ⚠️ 这里只递零件（文件监听），不发请求也不读盘，所以**早晚无所谓**。
// 真正会碰磁盘的第一遍扫描是那一页的第一帧发起的（`localPage.begin`）。
func MountLocalMusic(a *app.App) {
	a.MountLocalMusic(app.LocalMusic{Watch: mygoDirWatch{}})
}
