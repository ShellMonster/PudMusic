// 喜欢歌单绑定那一票的用例：**用户看得见的流程**。
//
// 被测的是这样一个功能：播放条 / 满屏上的「喜欢」（原「收藏」那一键的位置）
// 不再开「放到哪些收藏夹」那个弹窗——它绑定到**一格**收藏夹，喜欢的歌自动
// 进去。第一次点喜欢时（还没绑定）弹「创建 / 选择已有 / 暂时不用」问一次。
//
// # 这一族断的是流程，不是实现
//
// 每一条都从「用户点了一下」开始，到「界面上看得见的那一样 / 假 B 站收到的
// 那一趟」为止——不去看 `Shell` 的字段，也不去看 `App` 的内部状态。看得见
// 的判据只有两类：
//
//   - **画面**：弹窗在不在、键在不在、字对不对（`tt.Find` / `tt.HasText`）；
//   - **网络**：假 B 站收没收到那一趟、收的是哪一格（`f.dealCalls` /
//     `f.folderAddCalls`）。
//
// 实现怎么长是它的自由——「绑定存哪一格」「弹窗怎么建」都是它自己的事。
// 这里钉的是**用户会注意到的那个错**：点了喜欢没绑定、绑了进错格、绑定没了
// 还往它写、播放器上还留着赞 / 收藏那两枚键。
//
// # 怎么跑
//
//	go test ./internal/ui/ -run 'TestThe|TestALike|TestCreating|TestChoosing|TestSkipping'
//
// ⚠️ **fixture 在 `likedplaylistfixture_test.go`**：那台假 B 站会答「我创建的
// 收藏夹」分页（`created/list`）、新建收藏夹（`folder/add`）和写收藏
// （`resource/deal`），并把每一次写记下来。
package ui

import (
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// boundFolderID 是「已绑定」那一组种进设置的那一格。
//
// ⚠️ **它是 `created/list` 里真有的那一格**（fixture 的默认那两格之一）：
// 绑定是「挑一格 B 站真有的收藏夹」，断言「后续喜欢进的是它」靠的正是这
// 一格的 id。
const boundFolderID = fixtureFolderID // 100，fixture 的「默认收藏夹」

// newLikedEnv 是「没绑定」的那一份：正在放一首可赞的 B 站 mv，
// 设置里没有 `likedPlaylist`。
func newLikedEnv(t *testing.T) (*Shell, *mygoui.Tester, *biliFixture) {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.onLike(false, false, false)
	s, tt := newLikedBindShell(t, f, 0)
	return s, tt, f
}

// newLikedBoundEnv 是「已绑定」的那一份：设置里 `likedPlaylist` 指着
// boundFolderID，界面照旧在放一首可赞的 mv。
func newLikedBoundEnv(t *testing.T) (*Shell, *mygoui.Tester, *biliFixture) {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.onLike(false, false, false)
	s, tt := newLikedBindShell(t, f, boundFolderID)
	return s, tt, f
}

// likeTheSong 从用户那一端「喜欢」当前这首：点播放条上「喜欢」那一枚。
//
// ⚠️ **它是「喜欢」，不是「赞」**：这一票把「赞」和「收藏」从播放器上拿
// 走，留的入口叫「喜欢」（心形——把这首放进歌单，原「收藏」那一键的职责）。
func likeTheSong(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click("喜欢"); err != nil {
		t.Fatalf("点不了「喜欢」：%v（文字：%q）", err, tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 首次喜欢：弹「创建 / 选择已有 / 暂时不用」
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFirstLikeAsksWhereToPutIt 断「**还没绑定**时，点喜欢会问一次去哪」。
//
// 这是整个功能的入口：用户不知道「喜欢歌单」这回事，而应用要在他第一次用
// 「喜欢」时问清——不是默默地收进默认收藏夹（那是旧行为，和现在的不一样）。
func TestTheFirstLikeAsksWhereToPutIt(t *testing.T) {
	_, tt, _ := newLikedEnv(t)

	likeTheSong(t, tt)

	// 弹窗必须给三个出口：创建、选择已有、暂时不用。缺一个，用户的那一路就断了。
	waitFrames(t, tt, "出现了「去哪」那一问", func() bool {
		return tt.HasText("创建新的") || tt.HasText("选择已有歌单")
	})
	for _, want := range []string{"创建新的", "选择已有歌单", "暂时不用"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("首次喜欢的弹窗里没有 %q 那一枚，文字有 %q", want, tt.Texts())
		}
	}
}

// TestChoosingAnExistingFolderBindsAndAdds 断「选已有歌单 = 绑定它 + 这首进它」。
//
// 这一趟在假 B 站上是**两笔**：`created/list` 拉出列表让用户挑，挑完「绑定并
// 加入」把这首写进 `add_media_ids` 那一格。绑的是哪一格、进的是哪一格，必须
// 是**同一个 id**——用户挑的是「放这里」，不是「绑这里、放进默认格」。
func TestChoosingAnExistingFolderBindsAndAdds(t *testing.T) {
	_, tt, f := newLikedEnv(t)

	likeTheSong(t, tt)
	waitFrames(t, tt, "首次弹窗出来了", func() bool { return tt.HasText("创建新的") })

	// 「选择已有歌单」→ 列表 → 挑一格 → 「绑定并加入」。
	if err := tt.Click("选择已有歌单"); err != nil {
		t.Fatalf("点不了「选择已有歌单」：%v，文字有 %q", err, tt.Texts())
	}
	// 列表是真拉回来的（fixture 那两格）。
	waitFrames(t, tt, "已有歌单拉回来了", func() bool { return tt.HasText(fixtureFolderTitle) })
	if err := tt.Click(fixtureFolderTitle); err != nil {
		t.Fatalf("点不了「%s」那一格：%v，文字有 %q", fixtureFolderTitle, err, tt.Texts())
	}
	if err := tt.Click("绑定并加入"); err != nil {
		t.Fatalf("点不了「绑定并加入」：%v，文字有 %q", err, tt.Texts())
	}

	// 假 B 站必须收到那一趟写，且收进的是用户挑的那一格。
	waitFramesFor(t, tt, 30*time.Second, "写收藏那一趟发出去了", func() bool {
		return len(f.dealCalls()) > 0
	})
	if got := f.dealCalls()[0].Get("add_media_ids"); got != strconv.FormatInt(fixtureFolderID, 10) {
		t.Errorf("收进的格子是 %q，want %d（用户挑的那一格）", got, fixtureFolderID)
	}
}

// TestCreatingAFolderBindsAndAdds 断「新建 = 建成那一格 + 绑定它 + 这首进它」。
//
// 这一趟是三笔：`folder/add` 建出新格 → 拿到新 id → `deal` 把这首写进新格。
// 界面上用户只填了名字、按了「创建并加入」——但**进的是刚建的那一格**，不是
// 默认收藏夹（B 站回的新 id 才算数）。
func TestCreatingAFolderBindsAndAdds(t *testing.T) {
	_, tt, f := newLikedEnv(t)

	likeTheSong(t, tt)
	waitFrames(t, tt, "首次弹窗出来了", func() bool { return tt.HasText("创建新的") })

	if err := tt.Click("创建新的"); err != nil {
		t.Fatalf("点不了「创建新的」：%v，文字有 %q", err, tt.Texts())
	}
	waitFrames(t, tt, "新建表单出来了", func() bool { return tt.HasText("创建并加入") })
	if err := tt.Click("创建并加入"); err != nil {
		t.Fatalf("点不了「创建并加入」：%v，文字有 %q", err, tt.Texts())
	}

	// 假 B 站得先收到「新建」，再收到「写进新格」。两趟都要数出来——只数后一
	// 趟的话，「跳过了新建、直接写进默认格」那一档照样绿。
	waitFramesFor(t, tt, 30*time.Second, "新建那一趟发出去了", func() bool {
		return len(f.folderAddCalls()) > 0
	})
	waitFramesFor(t, tt, 30*time.Second, "写进新格那一趟发出去了", func() bool {
		return len(f.dealCalls()) > 0
	})
	// fixture 的 folder/add 回新 id 300。进那一格才算「建完就放进去」。
	if got := f.dealCalls()[0].Get("add_media_ids"); got != "300" {
		t.Errorf("新建后收进的格子是 %q，want 300（刚建出来的那一格）", got)
	}
}

// TestSkippingThePromptFallsBackToFolderPicker 断「暂时不用 = 回原来的收藏夹选择」。
//
// 「暂时不用」不是「取消喜欢」——用户还是要把这首收起来，只是不绑歌单。所以
// 它落到**原来的那条路**：「放到哪些收藏夹」那个多选弹窗（`folderSelect`），
// 让用户照旧挑格子。
func TestSkippingThePromptFallsBackToFolderPicker(t *testing.T) {
	_, tt, _ := newLikedEnv(t)

	likeTheSong(t, tt)
	waitFrames(t, tt, "首次弹窗出来了", func() bool { return tt.HasText("暂时不用") })

	if err := tt.Click("暂时不用"); err != nil {
		t.Fatalf("点不了「暂时不用」：%v，文字有 %q", err, tt.Texts())
	}

	// 原收藏夹选择弹窗的判据是它的标题——那个弹窗一直叫「添加到收藏夹」。
	waitFrames(t, tt, "落回了原收藏夹选择", func() bool { return tt.HasText("添加到收藏夹") })
}

// ─────────────────────────────────────────────────────────────────────────────
// 已绑定：喜欢直接进那一格，不再问
// ─────────────────────────────────────────────────────────────────────────────

// TestALikeWithBoundFolderAddsWithoutAsking 断「绑了之后喜欢**不再问**」。
//
// 这是绑定的全部意义：第一次问过之后，之后每一次喜欢都直接进那一格。再弹
// 一次「去哪」就是绑定没生效——用户每点一次都被拦一下。
func TestALikeWithBoundFolderAddsWithoutAsking(t *testing.T) {
	_, tt, f := newLikedBoundEnv(t)

	likeTheSong(t, tt)

	// 写那一趟得真的发出去，且收进的是绑定那一格。
	waitFramesFor(t, tt, 30*time.Second, "喜欢那一趟写发出去了", func() bool {
		return len(f.dealCalls()) > 0
	})
	if got := f.dealCalls()[0].Get("add_media_ids"); got != strconv.FormatInt(boundFolderID, 10) {
		t.Errorf("收进的格子是 %q，want %d（绑定的那一格）", got, boundFolderID)
	}
	// 而且**没再问**——「去哪」那个弹窗一格都不该出现。
	for _, bad := range []string{"创建新的", "选择已有歌单", "暂时不用", "添加到收藏夹"} {
		if tt.HasText(bad) {
			t.Errorf("绑定了还喜欢还弹出 %q，文字有 %q", bad, tt.Texts())
		}
	}
}

// TestTheBoundFolderIsKeptAcrossSongs 断「绑定是**设置**里的事，换歌还在」。
//
// 绑定存进 `Settings.LikedPlaylist`，不是记在当前这一首上——不然换一首歌
// 就丢一次绑定，用户以为这功能坏了。
func TestTheBoundFolderIsKeptAcrossSongs(t *testing.T) {
	s, tt, f := newLikedBoundEnv(t)

	likeTheSong(t, tt)
	waitFramesFor(t, tt, 30*time.Second, "第一首喜欢发出去了", func() bool {
		return len(f.dealCalls()) > 0
	})

	// 换一首再放（同一台假 B 站、同一份设置）。绑定不该因为换了首歌就没了。
	if err := s.app.EnqueueAndPlayNow(likeItem2()); err != nil {
		t.Fatalf("把第二首放进队列：%v", err)
	}
	waitFrames(t, tt, "第二首开始播", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && it.Bvid == likeItem2().Bvid
	})
	likeTheSong(t, tt)
	waitFramesFor(t, tt, 30*time.Second, "第二首喜欢也发出去了", func() bool {
		return len(f.dealCalls()) >= 2
	})
	if got := f.dealCalls()[1].Get("add_media_ids"); got != strconv.FormatInt(boundFolderID, 10) {
		t.Errorf("第二首收进的格子是 %q，want %d（绑定还在）", got, boundFolderID)
	}
	for _, bad := range []string{"创建新的", "选择已有歌单", "暂时不用"} {
		if tt.HasText(bad) {
			t.Errorf("换了首歌又把「去哪」弹出来了 %q，文字有 %q", bad, tt.Texts())
		}
	}
}

// likeItem2 是换一首歌：同一路的 mv，另一个 bvid。
func likeItem2() app.Item {
	it := likeItem()
	it.Bvid = "BVlikeTest2"
	return it
}

// ─────────────────────────────────────────────────────────────────────────────
// 设置更换绑定
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSettingsShowAndChangeTheBinding 断「设置里看得见绑的是哪格，能换」。
//
// 用户在设置页看到「喜欢歌单 · 绑定的那一格」，按「更换」→ 列表 → 挑另一格
// → 绑定换成它。换完之后**下一次**喜欢就得进新格——这是「换绑真的生效了」。
func TestTheSettingsShowAndChangeTheBinding(t *testing.T) {
	s, tt, f := newLikedBoundEnv(t)

	// 「喜欢歌单」那一行在设置页的「菜单」Tab 里（`settings_tab_menu.go` 的
	// `pickLikedFolder`）——默认停在「常规」，不切换永远找不到那一行。
	s.goToSettingsTab(settingsTabMenu)
	settleText(t, tt, "喜欢歌单")
	if !tt.HasText("绑定的那一格") {
		t.Fatalf("设置里没写着绑的是哪一格，文字有 %q", tt.Texts())
	}
	// 那一行在滚动容器里，800 高的窗口下「更换」在视口外——往下滚一截
	// 把它带进视口再点（`Scroll` 的 dy 是滚动偏移，正数往下）。
	if r, ok := tt.Find("更换"); ok && r.Y+r.H > likeWinH {
		tt.Scroll(600, 400, 0, 600)
	}
	waitFrames(t, tt, "「更换」进视口了", func() bool {
		r, ok := tt.Find("更换")
		return ok && r.Y+r.H <= likeWinH
	})
	if err := tt.Click("更换"); err != nil {
		t.Fatalf("点不了设置里的「更换」：%v，文字有 %q", err, tt.Texts())
	}

	// 列表拉回来，挑另一格（fixture 的「第二格」，id 200）。
	waitFrames(t, tt, "歌单列表拉回来了", func() bool { return tt.HasText("第二格") })
	if err := tt.Click("第二格"); err != nil {
		t.Fatalf("点不了「第二格」：%v，文字有 %q", err, tt.Texts())
	}
	// ⚠️ **从设置进来换绑时确认钮是「绑定」，不是「绑定并加入」**：那一下
	// `likePending` 是 nil（换绑定本身不收歌），`viewLikeBindPick` 里
	// `pending` 为假时按钮文案就是「绑定」。「绑定并加入」只在「点喜欢时挑
	// 歌单」那一条路上（那时候真有一首要收）。
	if err := tt.Click("绑定"); err != nil {
		t.Fatalf("点不了「绑定」：%v，文字有 %q", err, tt.Texts())
	}

	// 换绑之后下一次喜欢进新格（200），不再是 boundFolderID。
	likeTheSong(t, tt)
	waitFramesFor(t, tt, 30*time.Second, "换绑后喜欢那一趟发出去了", func() bool {
		return len(f.dealCalls()) > 0
	})
	if got := f.dealCalls()[0].Get("add_media_ids"); got != "200" {
		t.Errorf("换绑之后收进的格子是 %q，want 200（刚换上的那一格）", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 绑定丢失：退回原收藏夹选择
// ─────────────────────────────────────────────────────────────────────────────

// TestALostBindingFallsBackToFolderPicker 断「绑定那格没了 → 回原选择」。
//
// 「没了」造的是 B 站那一端：deal 回一个业务错误（收藏夹被删 / 无权限）。
// 用户看到的不该是「喜欢失败」一句就完——他**还是要收藏这首**，所以落回原
// 来那个「放到哪些收藏夹」多选弹窗，让他自己挑一格。
func TestALostBindingFallsBackToFolderPicker(t *testing.T) {
	_, tt, f := newLikedBoundEnv(t)
	// 绑定那一格在 B 站那边「没了」：写它会被拒。
	f.failDeal(-403)

	likeTheSong(t, tt)

	// 那一趟写发出去了、被拒了，然后界面落回原收藏夹选择。
	waitFramesFor(t, tt, 30*time.Second, "写那一趟发出去了", func() bool {
		return len(f.dealCalls()) > 0
	})
	waitFrames(t, tt, "落回了原收藏夹选择", func() bool { return tt.HasText("添加到收藏夹") })
}

// ─────────────────────────────────────────────────────────────────────────────
// 播放器上不再出现赞 / 收藏，但喜欢入口还在
// ─────────────────────────────────────────────────────────────────────────────

// TestThePlayBarKeepsLikeButDropsLikeAndFav 断播放条：
// 「赞」和「收藏」两枚键（连同角标数字）没了，「喜欢」还在。
//
// 这三枚本来就挨在一行，断「两枚没了、一枚还在」才验得出「拿掉的是社交键、
// 留下的是入口」。只断「赞没了」的话，把三枚全删了也照样绿。
func TestThePlayBarKeepsLikeButDropsLikeAndFav(t *testing.T) {
	_, tt, _ := newLikedBoundEnv(t)

	if _, ok := tt.Find("喜欢"); !ok {
		t.Errorf("播放条上没有「喜欢」那个入口，文字有 %q", tt.Texts())
	}
	// 「赞」和「收藏」两枚都不在了。断**名字**而不是图标——读屏听得见的那个
	// 名字才是「这枚键在不在」的判据。
	for _, gone := range []string{"赞", "收藏"} {
		if _, ok := tt.Find(gone); ok {
			t.Errorf("播放条上还留着 %q 那一枚（它该被拿走了），文字有 %q", gone, tt.Texts())
		}
	}
}

// TestTheFullPlayerKeepsLikeButDropsThumbAndFav 断满屏：
// 大拇指（赞）和心形（收藏）没了，「喜欢」还在。
//
// 满屏那一屏有它自己一套键（`full:thumb` / `full:heart`），和播放条不是同
// 一份代码——只断播放条的话，满屏上那一套会漏掉。
func TestTheFullPlayerKeepsLikeButDropsThumbAndFav(t *testing.T) {
	_, tt, _ := newLikedBoundEnv(t)

	openFull(t, tt)

	if _, ok := tt.Find("喜欢"); !ok {
		t.Errorf("满屏上没有「喜欢」那个入口，文字有 %q", tt.Texts())
	}
	for _, gone := range []string{"赞", "收藏"} {
		if _, ok := tt.Find(gone); ok {
			t.Errorf("满屏上还留着 %q 那一枚（它该被拿走了），文字有 %q", gone, tt.Texts())
		}
	}
}
