package ui

import (
	"strconv"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// 这一组验「回到顶部」那一枚。
//
// 上游那一枚是 `fixed right-10 bottom-[120px]`，滚过 400 像素才出现
// （`src/components/scroll-container/index.tsx`）。那四个数**照抄**，不重新量。
//
// ⚠️ **画法只有一份**（`widgets/list` 的 `BackToTop`）：这一枚原先在两个地方各写了
// 一份（`widgets/list` 的分页那一层，和界面层那两页自己的分页列表），两份的数一样、
// 代码不一样——改一份就有一页对不上。
//
// 这一组钉的是**每一页滚到底都有那一枚**：上游有 12 处 `enableBackToTop`，对应我们
// 这边 10 个不同的页面（收藏夹那一族上游是三页、我们是一页）——那 12 处对应的页面
// 我们全都要有。

// backTopLabel 是那一枚的无障碍名字（图标对辅助技术不可见，它同时也是 Tester
// 认得这一枚的唯一凭据）。
const backTopLabel = "返回顶部"

// TestTheLocalMusicPageHasABackToTop 钉住**本地音乐**有那一枚。
//
// 它是「歌最多的两页」里的另一页（本地文件能有几千首），上游那一页也有回顶
// （`src/pages/local-music/index.tsx:185`）。
//
// ⚠️ **这一页有两档（表格 / 歌曲），两档都要有那一枚**：用户在两档之间换来换去，
// 少一档就是「这一档里没有」。两档的滚法还不一样——表格那一档自己滚（阈值从行号
// 换算），歌曲那一档是列表（阈值按行算）——所以**两档各走一遍**。
func TestTheLocalMusicPageHasABackToTop(t *testing.T) {
	root := t.TempDir()
	for i := range 40 {
		libSong(t, root, "第 "+strconv.Itoa(i+1)+" 首.mp3")
	}
	tt, _, _, _ := shellOnLocalMusic(t, root)
	// 原型样式之后只有一档（曲目行列表；表格那一档随旧设计删了）。
	checkBackToTop(t, tt, "歌曲")
}

// checkBackToTop 验当前那一档：滚下去有那一枚、点它回到顶上、回到顶上之后它消失。
func checkBackToTop(t *testing.T, tt *ui.Tester, which string) {
	t.Helper()
	if tt.HasText(backTopLabel) {
		t.Fatalf("（%s）还没滚就出现回顶按钮了", which)
	}
	tt.Scroll(600, 400, 0, 2000)
	waitFrames(t, tt, "（"+which+"）滚下去之后回顶按钮出现", func() bool {
		return tt.HasText(backTopLabel)
	})
	if err := tt.Click(backTopLabel); err != nil {
		t.Fatalf("（%s）点不到回顶按钮：%v", which, err)
	}
	waitFrames(t, tt, "（"+which+"）回到顶上之后回顶按钮消失", func() bool {
		return !tt.HasText(backTopLabel)
	})
}

// TestTheDownloadListHasABackToTop 钉住**下载记录**有那一枚。
//
// ⚠️ **这一页的表格自己滚**，而它内部那个滚动容器的像素偏移拿不到（`ui.Table` 没把
// 它露出来），所以这里的「滚过了多少」是从**顶上那一行是第几行**换算的
// （`dlRowPx`）。这一条钉的是「有那一枚」和「点它回到顶上」。
func TestTheDownloadListHasABackToTop(t *testing.T) {
	rows := make([]download.Snapshot, 0, 40)
	for i := range 40 {
		rows = append(rows, dlSnapshot("第 "+strconv.Itoa(i+1)+" 首", download.StatusDone, 100, 100))
	}
	_, tt := newRows(t, rows...)

	if tt.HasText(backTopLabel) {
		t.Fatalf("还没滚就出现回顶按钮了")
	}

	// 滚到表格下面去。
	tt.Scroll(dlWide/2, 500, 0, 4000)
	waitFrames(t, tt, "下载记录滚到底之后回顶按钮出现", func() bool {
		return tt.HasText(backTopLabel)
	})

	if err := tt.Click(backTopLabel); err != nil {
		t.Fatalf("点不到回顶按钮：%v", err)
	}
	waitFrames(t, tt, "回到顶上之后回顶按钮消失", func() bool {
		return !tt.HasText(backTopLabel)
	})
}

// TestTheFollowPageHasABackToTop 钉住**我的关注**有那一枚。
//
// ⚠️ **这一页右边那一格自己滚**（那些用户卡片是方的，行高跟着内容走），所以它的
// 位置是**像素**而不是行数——挂一个 `ScrollState` 上去，让框架把偏移写回来。
func TestTheFollowPageHasABackToTop(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 40, uiFollowUserRange(1, 40))
	uiFollowSearch(b, "", nil)

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "关注的人到了", func() bool { return len(f.users) == 40 && f.tagStarted })
	tt.Frame()

	if tt.HasText(backTopLabel) {
		t.Fatalf("还没滚就出现回顶按钮了")
	}

	// ⚠️ **反复滚，按钮出现为止**（同这一页既有的 `scrollToEnd`）：这一页自己的锚点
	// 机制（`keepFollowAnchor`）偶尔会把**第一次**滚轮吞掉——那是这一页的既有问题
	// （不是这一票引入的，见收尾说明的「Also noticed」），而这里要验的是「滚下去
	// 之后有那一枚」。
	waitFrames(t, tt, "关注那一格滚下去之后回顶按钮出现", func() bool {
		tt.Scroll(followScrollX, followScrollY, 0, 800)
		return tt.HasText(backTopLabel)
	})
}

// TestTheSettingsPageHasABackToTop 钉住**设置**有那一枚。
//
// 设置页滚的是**整页**（外面那个 `ui.Scroll`），不是某一栏。
func TestTheSettingsPageHasABackToTop(t *testing.T) {
	tt, s, _, _ := shellWithSettings(t, app.ThemeModeLight, false)
	s.navigate(settingsPath)
	tt.Frame()
	tt.Frame()

	if tt.HasText(backTopLabel) {
		t.Fatalf("还没滚就出现回顶按钮了")
	}

	tt.Scroll(450, 300, 0, 2000)
	waitFrames(t, tt, "设置页滚下去之后回顶按钮出现", func() bool {
		return tt.HasText(backTopLabel)
	})
}

// TestTheCollectionHasABackToTop 钉住**回顶按钮**。
//
// ⚠️ **这一枚本来就有**（`widgets/list/backtotop.go` 是全仓唯一实现，
// 阈值 400 像素、距右 40、距下 120，照抄上游 `src/components/scroll-container/
// index.tsx` 的 `fixed right-10 bottom-[120px]`）——但**这一页没有测试钉住它**
// （上游那边收藏夹是三页、我们是一页，而 `backtotop_test.go` 里没有收藏夹那一条）。
//
// 所以这一条是**补上那一条**，顺带钉住「点它回到顶上」。
func TestTheCollectionHasABackToTop(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", collectionMedias(40)...)
	waitSongs(t, tt, s, 40)
	tt.Frame()

	if tt.HasText(backTopLabel) {
		t.Fatalf("还没滚就出现回顶按钮了")
	}

	tt.Scroll(600, 500, 0, 2000)
	waitFrames(t, tt, "歌单页滚下去之后回顶按钮出现", func() bool {
		return tt.HasText(backTopLabel)
	})

	// 点它：回到顶上。
	if err := tt.Click(backTopLabel); err != nil {
		t.Fatalf("点不到回顶按钮：%v", err)
	}
	waitFrames(t, tt, "回到顶上之后回顶按钮消失", func() bool {
		return !tt.HasText(backTopLabel)
	})
	// ⚠️ **判据是头图区回到了画面上**：回到顶上意味着**头图区又回来了**——
	// 而它现在跟着列表走，所以「回到顶上」= 头图区重新出现。
	if _, ok := tt.Find("通勤路上"); !ok {
		t.Error("回到顶上之后头图区没有回来——回顶按钮回的不是顶")
	}
}
