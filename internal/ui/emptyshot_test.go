package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
)

// 这一族把**空状态、封面占位的三种样子、错误页**存成 PNG，留给人眼和 Biu 上游
// 的截图并排看。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderEmptyCoverErrorShots
//
// # ⚠️ **深浅两套都要出**
//
// 这一族验的每一件事都有两副面孔：
//
//   - **图标是单色的、跟着祖先的文字色走**（`mygo/ui/svg.go` 的 drawIcon 收的
//     就是文字色），浅色下是深灰、深色下是亮白——浅色下看得清的图标，深色下
//     可能糊成一团。
//   - **封面骨架屏那一格的底色是从 `Surface` 往 `Background` 挪的**（见
//     `widgets/imgcache/view.go`），而 `Surface` 和 `Background` 在两套主题里
//     的大小关系是反的：浅色下两个色差得开、深色下本来就接近。只出浅色等于
//     没验这一条。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染，钥匙串里那份真登录
// 态一次都没被读（和 `hover_shot_test.go` 那一族同一个约定）。

// TestRenderEmptyCoverErrorShots 存图：深浅各一套。
func TestRenderEmptyCoverErrorShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	// 指针停在窗口右下那片空地上：不摆 hover 的那几张要的是「平时」。
	away := [2]float32{1180, 770}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ⚠️ **壳要带 `systemIsDark`**：不带的话「深色」那一档存出来和
			// 「浅色」一模一样——第一版就这样，而要验的恰恰是深色。
			shotEmptyStates(t, dir, name, dark)
			shotCoverStates(t, dir, name, dark)
			shotErrorPage(t, dir, name, dark)
			_ = away // 指针停在右下，不摆 hover（那一族是 #125 的）
		})
	}
}

// shotEmptyStates 存七个页面的空状态。
//
// ⚠️ **走真实路由**：切过去、画出来、存图。没有一条是靠测试直接把 `emptyState`
// 交给渲染器的——那种写法存出来的是「这句话画出来是这句话」，而不是「用户点进
// 这一页看到的是这一页」。
func shotEmptyStates(t *testing.T, dir, theme string, dark bool) {
	t.Helper()
	for _, p := range []struct {
		file  string
		what  string
		path  string
		title string
	}{
		{"40-空状态-推荐音乐", "推荐音乐", "/", lookup("/").Empty.Title},
		{"41-空状态-我的关注", "我的关注", followPath, lookup(followPath).Empty.Title},
		{"42-空状态-稍后再看", "稍后再看", laterPath, lookup(laterPath).Empty.Title},
		{"43-空状态-历史记录", "历史记录", historyPath, lookup(historyPath).Empty.Title},
		// ⚠️ **这一条等的是「还没有下载目录」**（报告 F3 那一档）：这一族的外壳
		// 没挂下载，`DownloadDir()` 是空串——页面画的是「连落盘的地方都没有」
		// 那一格，不是路由表里的 `noDownloads`（「暂无内容」）。曾经等的是后者，
		// 等的却是一句这页画不出来的话，在 main 上一直红。
		{"44-空状态-下载记录", "下载记录", downloadListPath, noDownloadDir.Title},
		{"45-空状态-搜索", "搜索", searchPath, searchIdle.Title},
	} {
		func() {
			// ⚠️ **每个外壳都要带 `systemIsDark`**：不带的话深浅两套存出来是
			// 同一个文件（第一版就这么做的，而那一版的图等于只有一半）。
			b := newTestShell(t, systemIsDark(dark))
			s, tt := b.s, b.tt
			s.navigate(p.path)
			waitFrames(t, tt, p.what+"的空状态画出来了", func() bool {
				return tt.HasText(p.title)
			})
			tt.Move(1180, 770)
			homeFrame(tt)
			save(t, dir, p.file+"-"+theme+".png", tt)
			b.Close()
		}()
	}

	// 收藏夹那一页要一个「回空收藏夹」的 B 站（容器拉回来了、里面一条都没有），
	// 所以它自己造一个外壳。
	func() {
		b := newTestShell(t, systemIsDark(dark))
		s, tt := b.s, b.tt
		s.app.MountCollections(app.CollectionRefs{Client: newEmptyFolderClient(t)})
		s.navigate("/collection/12345")
		waitCollectionHeader(t, s, tt)
		waitFrames(t, tt, "收藏夹的空状态画出来了", func() bool {
			return tt.HasText(noFavorites.Title)
		})
		tt.Move(1180, 770)
		homeFrame(tt)
		save(t, dir, "46-空状态-收藏夹-"+theme+".png", tt)
		b.Close()
	}()
}

// shotCoverStates 存封面那一格的**三种**样子并排看「分不分得开」。
//
// ⚠️ **九块摆在一屏上**（三态 × 三个尺寸）。三态各存一张的话，看的人要在脑子
// 里把三张拼起来才说得出「分得开」，而「一块纯色」和「描边框加一枚图标」在小图
// 上恰恰最容易看混；并排摆着，那条分界一眼就在。
//
// ⚠️ **三个尺寸是 48 / 96 / 160**：48 是列表那一档、96 接近网格、160 是收藏夹头图
// 那一档。三档并排才看得出「图标跟着封面缩放」——而「图标不缩放」是这一族最容易
// 发生又最不容易一眼看出的坏。
func shotCoverStates(t *testing.T, dir, theme string, dark bool) {
	t.Helper()
	// 卡住不回的 = 永远「正在加载」；立刻 404 的 = 「拉失败」。
	hold := make(chan struct{})
	stuck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-hold
	}))
	defer func() { close(hold); stuck.Close() }()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "没了", http.StatusNotFound)
	}))
	defer broken.Close()

	type cell struct {
		label string
		url   string
	}
	cols := []struct {
		label string
		cells []cell
	}{
		{"正在加载", []cell{{"骨架屏", stuck.URL + "/a.png"}}},
		{"拉失败", []cell{{"描边框 + 图片图标", broken.URL + "/b.png"}}},
		{"没有地址", []cell{{"描边框 + 图片图标（底色深一档）", ""}}},
	}

	b := newTestShell(t, at(1200, 800), systemIsDark(dark),
		withView(func(s *Shell) func(*ui.Context) {
			return func(c *ui.Context) {
				s.applyFrameTheme(c)
				ui.Row(c).Fill().Gap(28).Padding(24).AlignItems(ui.Start).Children(func() {
					for _, col := range cols {
						ui.Column(c).Gap(14).AlignItems(ui.Center).Children(func() {
							for _, side := range []float32{48, 96, 160} {
								imgcache.View(c, s.images, col.cells[0].url, side, side)
							}
							ui.Text(c, col.label).FontSize(13)
							ui.Text(c, "48 / 96 / 160").FontSize(11).TextColor(c.Theme().TextMuted)
						})
					}
				})
			}
		}))
	// 三块「拉失败」那一列要等后台那一趟真的回来（404 是异步的），而「正在
	// 加载」那一列要**一直**停在骨架屏上。
	waitFrames(t, b.tt, "失败那一格画出描边框", func() bool {
		return b.s.images.Get(broken.URL+"/b.png").Err != nil
	})
	homeFrame(b.tt)
	save(t, dir, "47-封面占位-三种状态-"+theme+".png", b.tt)
	b.Close()
}

// shotErrorPage 存错误页。
func shotErrorPage(t *testing.T, dir, theme string, dark bool) {
	t.Helper()
	for _, c := range []struct {
		file  string
		where string
		path  string
	}{
		{"50-错误页-崩在某一页", "页面「下载记录」", downloadListPath},
		{"51-错误页-崩在侧栏", "侧栏", laterPath},
	} {
		b := newTestShell(t, at(1200, 800), systemIsDark(dark),
			stubbed(func(_ *testing.T, s *Shell) {
				s.navigate(c.path)
				s.crash = s.newCrashInfo(c.where,
					"runtime error: index out of range [5] with length 3")
			}))
		homeFrame(b.tt)
		save(t, dir, c.file+"-"+theme+".png", b.tt)
		b.Close()
	}
}
