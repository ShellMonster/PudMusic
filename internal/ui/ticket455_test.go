package ui

// 票 #455（BMG-252）的画面行为：把「失败」和「没有」分开说。
//
// 这一票改三处：
//
//  1. 搜索页拉失败 → **失败自己的说法 + 一个重试**（不再是「没有结果」）；
//  2. 联想词那一档拉失败 → **那一档自己的说法**（不再是「空着」）；
//  3. 搜索页「用户」那一档的封面 → 图形、提示、无障碍名**三样都改成「进他的
//     UP 空间」**（不再是播放三角 + 「从这一首往后听」）。
//
// 依据是 BMG-246 的 hover / 交互反馈梳理报告（`audits/audit-home-search.md`
// 第 12 / 27 / 30 条）。那一票只梳理、不改代码。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。

import (
	"errors"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
)

// searchFetchFail 是这一组里那一趟拉页要失败时说的话。
//
// ⚠️ **它得是用户看得懂的一句**：界面上会把这一句原样摆出来（`viewLoadError`
// 底下那一行），所以不能是 "connection refused"。
var searchFetchFail = errors.New("连不上 B 站")

// TestTheSearchPageSaysItFailedInsteadOfSayingThereIsNoResult 钉住验收第 1 条：
// 搜索页拉失败时**有自己的一句话**，而且**有重试**。
//
// 原来那一支画的是「没有结果」（`noSearchResults`）：用户读到的是「B 站没有这个
// 结果，去换个词」——而真实原因可能是网络断了、登录态过期、风控。「换个词」这个
// 建议在网络故障时是错的，而且那一屏**没有任何办法重来**。
//
// 判据是三样：一、**不是**「没有结果」那一句；二、有一句说清「没拉出来」的；
// 三、有一枚能按的「重试搜索」，按下去**真的又拉了一趟**。
func TestTheSearchPageSaysItFailedInsteadOfSayingThereIsNoResult(t *testing.T) {
	st := newSearchStub(24, 24)
	st.fail = searchFetchFail
	s, tt := newSearchPage(t, "周杰伦", st)

	// 等那一趟失败落地。
	waitFrames(t, tt, "拉失败了", func() bool {
		return s.search.lists[searchTabSong].list.Paged().Err != nil
	})

	// ① 不是「没有结果」那一句。
	if tt.HasText(noSearchResults.Hint) {
		t.Errorf("拉失败却画成「没有结果」那一格，文字有 %q", tt.Texts())
	}
	// ② 失败有自己的说法：一句「没拉出来」+ 出错的原因。
	if !tt.HasText("这一块没拉出来") {
		t.Errorf("拉失败没有自己那一句，文字有 %q", tt.Texts())
	}
	if !tt.HasText(searchFetchFail.Error()) {
		t.Errorf("拉失败没有说清为什么，文字有 %q", tt.Texts())
	}
	// ③ 有重试。
	btn, ok := tt.Find("重试搜索")
	if !ok {
		t.Fatalf("拉失败却没有「重试搜索」那一枚，文字有 %q", tt.Texts())
	}

	before := len(st.callsOf(searchTabSong))
	tt.ClickAt(btn.X+btn.W/2, btn.Y+btn.H/2)
	waitFrames(t, tt, "重试之后又问了一次", func() bool {
		return len(st.callsOf(searchTabSong)) > before
	})
	if got := len(st.callsOf(searchTabSong)); got != before+1 {
		t.Errorf("按了重试之后一共问了 %d 次，want %d——重试按钮没接到事", got, before+1)
	}
}

// TestTheSearchPageKeepsTheResultsWhenALaterPageFails 钉住「翻页失败不把已有的
// 结果换成一句错」。
//
// 「拉失败」和「没有结果」要分开，但**反过来也不许合并**：屏幕上已经有一屏结果
// 了，这时候把整屏换成一句「没拉出来」，用户看的东西就白看了。末尾那一行由分页
// 列表自己给出「加载失败，点此重试」。
func TestTheSearchPageKeepsTheResultsWhenALaterPageFails(t *testing.T) {
	st := newSearchStub(24, 48)
	s, tt := newSearchPage(t, "周杰伦", st)
	waitFrames(t, tt, "第一页进来了", loadedTo(s, searchTabSong, 1))

	// 让第二页失败：改字段是有竞态的（第一页已经在后台跑过了），所以这一次走
	// 「装上之后改」那条路——`searchStub.fail` 只在 fetch 开头读，而第一页已经
	// 回来了。
	st.mu.Lock()
	st.fail = searchFetchFail
	st.mu.Unlock()

	scrollSearchList(tt)
	waitFrames(t, tt, "第二页失败了", func() bool {
		return s.search.lists[searchTabSong].list.Paged().Err != nil
	})

	// 屏幕上那一屏**还在**：**没有**被换成失败那一格。
	//
	// ⚠️ 断的是「页面级那个失败块没出现」而不是「第一行还在」：列表是虚拟的，
	// 滚到底之后第一行本来就不在画面上（那是虚拟列表的正常行为），拿它当判据
	// 量的是另一件事。
	if tt.HasText("这一块没拉出来") {
		t.Errorf("翻页失败把已经拿到的结果换成了一句错，文字有 %q", tt.Texts())
	}
	if tt.HasText(noSearchResults.Hint) {
		t.Errorf("翻页失败画成了「没有结果」那一格，文字有 %q", tt.Texts())
	}
	// 而末尾那一行是列表自己给的重试（「加载失败，点此重试」）——翻页失败归它管。
	if !tt.HasText("加载失败，点此重试") {
		t.Errorf("翻页失败却没有末尾那一行重试，文字有 %q", tt.Texts())
	}
}

// TestTheUserRowSaysGoToHisSpaceInsteadOfPlay 钉住验收第 3、4 条：搜索页「用户」
// 那一档的封面，**图形、提示文案、无障碍名三样说的是同一件事**（进他的 UP 空间）。
//
// 原来那一圈反馈是：图形 = 播放三角、提示 = 「从这一首往后听」、无障碍名 = 「<昵称>
// 从这一首往后听」——**三样都在说放歌**，而用户按下去做的是跳到那个 UP 主的主页
// （上游 `user-card.tsx:51` 的 `navigate("/user/" + mid)`）。
//
// ⚠️ **「从这一首往后听」那一套不许再出现**：它是无障碍名和提示的**同一句话**，
// 所以断言「找不到那一句」就等于断言图形、提示、无障碍名三样都换了。
func TestTheUserRowSaysGoToHisSpaceInsteadOfPlay(t *testing.T) {
	s, tt := newSearchPage(t, "周杰伦", newSearchStub(24, 24))
	settleText(t, tt, "第 1 页 第 1 首")

	if err := tt.Click("用户"); err != nil {
		t.Fatalf("点不到「用户」那个 Tab：%v", err)
	}
	waitFrames(t, tt, "用户那一页进来", loadedTo(s, searchTabUser, 1))

	title := "第 1 页 第 1 首"
	hoverShellRow(t, tt, title)

	// ① 无障碍名说的是「进他的 UP 空间」。
	if _, ok := tt.Find(hover.ProfileLabel(title)); !ok {
		t.Errorf("用户那一行的封面没有「进他的 UP 空间」那个名字，文字有 %q", tt.Texts())
	}
	// ② 「从这一首往后听」那一套不许再出现。
	if _, ok := tt.Find(hover.FromLabel(title)); ok {
		t.Errorf("用户那一行的封面上还是「从这一首往后听」那一套，文字有 %q", tt.Texts())
	}
	// ③ **歌那一档不许被这一票改**：同样的封面，歌还是「从这一首往后听」。
	//
	// ⚠️ 这一条是验收第 5 条（「列表行和网格卡片的『播放』图形没被这一票改」）：
	// 那一档的封面是**同一个控件**画出来的，两边一起改的话这一票就把「播放/暂停
	// 那套图形」也顺手改了——那是另一件事（要改另开票）。
	if err := tt.Click("歌曲"); err != nil {
		t.Fatalf("点不到「歌曲」那个 Tab：%v", err)
	}
	waitFrames(t, tt, "歌曲那一页进来", loadedTo(s, searchTabSong, 1))
	hoverShellRow(t, tt, title)
	if _, ok := tt.Find(hover.FromLabel(title)); !ok {
		t.Errorf("歌曲那一行的封面上没有「从这一首往后听」那一套了——这一票不该动它，文字有 %q",
			tt.Texts())
	}
	if _, ok := tt.Find(hover.ProfileLabel(title)); ok {
		t.Errorf("歌曲那一行的封面上却摆着「进他的 UP 空间」那一套，文字有 %q", tt.Texts())
	}
}

// TestTheUserCardSaysGoToHisSpaceInsteadOfPlay 钉住**网格**那一档的「用户」也是
// 同一件事。
//
// 设置里的「数据显示样式」有列表 / 网格 / 紧凑三档，而网格那一档画的是另一套封面
// 反馈（`hover.CardCover`：右下角一个 40 的主色圆钮 + 封面底部一条渐变）——
// **同一个动作两套画法**，所以「用户」那一档在网格里也得换同一套说法。
//
// ⚠️ **网格那一档的表头不画**（`searchpage.go` 的 `viewSearchResultsBody` 那一支），
// 所以判据只能落在封面那一圈反馈上。
func TestTheUserCardSaysGoToHisSpaceInsteadOfPlay(t *testing.T) {
	set := app.DefaultSettings("")
	set.DisplayMode = app.DisplayModeCard
	st := newSearchStub(6, 6)
	s, tt := newSearchPage(t, "周杰伦", st, withSettings(set))
	settleText(t, tt, "第 1 页 第 1 首")

	if err := tt.Click("用户"); err != nil {
		t.Fatalf("点不到「用户」那个 Tab：%v", err)
	}
	waitFrames(t, tt, "用户那一页进来", loadedTo(s, searchTabUser, 1))

	// ⚠️ **用第二格**：网格那一档里，框架给**每一行**（不是每一格）标了那一行
	// 第一首歌名（`ui.List` 的 `ListState.Label`），于是 `tt.Find(第一首)` 给回的是
	// **整行**那个盒子，指针落在整行的边上、没落在任何一格上。第二格给回的是它
	// 自己那一小块（歌名），指针正落在那一格上。
	title := "第 1 页 第 2 首"
	hoverShellRow(t, tt, title)

	if _, ok := tt.Find(hover.ProfileLabel(title)); !ok {
		t.Errorf("网格那一档的用户格子没有「进他的 UP 空间」那个名字，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find(hover.FromLabel(title)); ok {
		t.Errorf("网格那一档的用户格子上还是「从这一首往后听」那一套，文字有 %q", tt.Texts())
	}
}
