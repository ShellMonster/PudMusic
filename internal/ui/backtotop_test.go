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

// TestTheHomePageHasABackToTop 钉住**首页**有那一枚。
//
// 首页是「歌最多的两页」之一（另一页是本地音乐），而它原先**没有**回顶——
// 翻到第 80 首要一路把滚动条拖回去。
func TestTheHomePageHasABackToTop(t *testing.T) {
	_, tt := newHome(t, fullSongs())

	if tt.HasText(backTopLabel) {
		t.Fatalf("还没滚就出现回顶按钮了")
	}

	tt.Scroll(homeW/2, homeH/2, 0, 1200)
	// ⚠️ **等条件，不数帧**：滚轮那一下要等布局把它收进去（机器忙的时候几帧不够，
	// 而「不够」的样子就是「按钮没出现」——分不出是慢还是坏）。
	waitFrames(t, tt, "首页滚下去之后回顶按钮出现", func() bool {
		return tt.HasText(backTopLabel)
	})

	// 点它：回到顶上。
	if err := tt.Click(backTopLabel); err != nil {
		t.Fatalf("点不到回顶按钮：%v", err)
	}
	waitFrames(t, tt, "回到顶上之后回顶按钮消失", func() bool {
		return !tt.HasText(backTopLabel)
	})
	pos := listPos(tt, homeTabs[0])
	if len(pos) == 0 || pos[0].title != homeTabs[0]+" 1" {
		t.Errorf("回到顶上之后画着的是 %v，want 从「%s 1」开始", titles(pos), homeTabs[0])
	}
}

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

	// 默认是**表格**那一档（`localStyleTable` = 0，见 `localmusic.go`）。
	checkBackToTop(t, tt, "表格")

	// 切到**歌曲**那一档：再走一遍。
	if err := tt.Click(localStyles[localStyleSong]); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
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

// TestTheTableRowHeightsAreWhatTheOffsetsAssume 钉住那两张表格的行高，就是回顶
// 按钮换算像素时用的那两个数。
//
// ⚠️ **它们是换算出来的，不是量出来的**：`ui.Table` 的行是它**内部那个滚动容器**
// 滚的，而那个容器的像素偏移框架没露出来（`TrackScroll` 挂在表格那一格上挂不到
// 它），所以那两页的「滚过了多少」只能从「顶上那一行是第几行」换算。
//
// 换了行高（改了封面的大小、改了操作那一列的字号）而忘了改这两个数，按钮就会早
// 一点或晚一点出现——那正是「400 像素」这个数被悄悄改掉的样子。
//
// ⚠️ **留 1 像素的余量**：行高是浮点排版的结果（那一行里最高的是操作那一列的
// 键，而键的高度是字号的行高倍数），逐位相等不是「对」的证据。
func TestTheTableRowHeightsAreWhatTheOffsetsAssume(t *testing.T) {
	// 下载记录那一页：行高 = 封面 + 表格那一行的上下内边距。
	rows := make([]download.Snapshot, 0, 6)
	for i := range 6 {
		rows = append(rows, dlSnapshot("第 "+strconv.Itoa(i+1)+" 首", download.StatusDone, 100, 100))
	}
	_, tt := newRows(t, rows...)
	a, ok := tt.Find("第 1 首")
	if !ok {
		t.Fatalf("画面上找不到第一行，文字有 %q", tt.Texts())
	}
	b, ok := tt.Find("第 2 首")
	if !ok {
		t.Fatalf("画面上找不到第二行，文字有 %q", tt.Texts())
	}
	if got := b.Y - a.Y; got < dlRowPx-1 || got > dlRowPx+1 {
		t.Errorf("下载记录那一页的行距是 %v，回顶按钮换算用的是 %v——差了就说明那两页的"+
			"「滚过了多少」算错了", got, dlRowPx)
	}

	// 本地音乐那一页：行高 = 操作那一列的键 + 表格那一行的上下内边距。
	root := t.TempDir()
	for i := range 6 {
		libSong(t, root, "第 "+strconv.Itoa(i+1)+" 首.mp3")
	}
	ttLocal, _, _, _ := shellOnLocalMusic(t, root)
	la, ok := ttLocal.Find("第 1 首")
	if !ok {
		t.Fatalf("画面上找不到第一行，文字有 %q", ttLocal.Texts())
	}
	lb, ok := ttLocal.Find("第 2 首")
	if !ok {
		t.Fatalf("画面上找不到第二行，文字有 %q", ttLocal.Texts())
	}
	if got := lb.Y - la.Y; got < localTableRowPx-1 || got > localTableRowPx+1 {
		t.Errorf("本地音乐那一页的行距是 %v，回顶按钮换算用的是 %v——差了就说明那一页的"+
			"「滚过了多少」算错了", got, localTableRowPx)
	}
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
