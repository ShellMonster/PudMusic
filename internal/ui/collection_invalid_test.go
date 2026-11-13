package ui

// 「失效的条目」在收藏夹页上的样子（票 #624）。
//
// # 这一组钉的是**整条链路**
//
//	B 站那个 `attr` 字段	`bili.Media.Attr` → `bili.Media.Invalid`
//	进队列项	`app.ItemOfMedia`（`Item.Invalid`）
//	画在列表里	`list` 那一档：灰掉 + 一枚「已失效」标签
//	菜单里	播放入口是**灰的**，末尾有一句为什么
//	进队列	**滤掉**——所以应用**不为它发取流请求**
//
// 这五样是一条链，而链子断在哪一节的症状都不同（标签没了 / 菜单能按了 /
// 队列里多了一首 / 白跑了一趟取流）。所以这一组从**界面上看**钉住整条链，
// 而不是在每一层各写一条单测——那样链子断在中间时，两边的单测都还是绿的。
//
// ⚠️ **这一组全是在默认设置下跑的**（`HideInvalidMedias` 默认关）：失效条目
// 照画、标出来、菜单灰、队列滤掉。开关打开之后的样子由下面那一组钉
// （`TestHidingInvalidEntries...` 起）。
//
// ⚠️ **全部走内存里的假 B 站**（`favBili` / `streamFixture`），一条真接口都不打。

import (
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// invalidMediaOf 造一条**失效**的内容（`attr=1`）。
//
// ⚠️ **attr=1 是「其他原因删除」**（B 站那一档），而 `attr=9` 是「UP 自己删除」
// ——判据是「不是 0」，两档都算失效（见 `bili.Media.Invalid`）。
func invalidMediaOf(id int, title string) map[string]any {
	m := mediaOf(id, title)
	m["attr"] = 1
	return m
}

// TestAnInvalidEntryIsMarkedInTheCollectionPage 钉住「标出来」。
//
// 🔴 **判据是画面上有那枚标签**：失效的条目**照画**（不画的话用户会发现
// 「收藏夹里的歌自己少了几首」而且不知道为什么），但要**灰掉 + 标出来**。
func TestAnInvalidEntryIsMarkedInTheCollectionPage(t *testing.T) {
	_, s, tt := collectionShell(t, "12345",
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
	)
	waitSongs(t, tt, s, 2)
	tt.Frame()

	if !tt.HasText(list.InvalidLabel) {
		t.Fatalf("失效的那一条没有标出来（画面上没有 %q）：文字有 %q",
			list.InvalidLabel, tt.Texts())
	}
	// ⚠️ **正常的那一条不许被标**：一次标多了就是噪音，而用户会以为
	// 「收藏夹里大部分东西都是坏的」。
	if n := countText(tt, list.InvalidLabel); n != 1 {
		t.Errorf("画面上 %q 出现了 %d 次，want 1（只有失效的那一条该标）", list.InvalidLabel, n)
	}
}

// TestAnInvalidEntryHasNoPlayEntryInTheCollectionPage 钉住「点之前就不给播放入口」。
//
// ⚠️ **菜单项还在**（用户看得到这一份菜单），但它是**灰的**——而末尾有一句
// 为什么。按不动而不说为什么等于让用户猜。
func TestAnInvalidEntryHasNoPlayEntryInTheCollectionPage(t *testing.T) {
	_, s, tt := collectionShell(t, "12345", invalidMediaOf(2, "失效的一首"))
	waitSongs(t, tt, s, 1)
	tt.Frame()

	if err := tt.RightClick("失效的一首"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("立即播放"); err == nil {
		t.Error("失效的那一条，「立即播放」竟然能按")
	}
	items := tt.Menu()
	last := items[len(items)-1]
	if !strings.Contains(last, "失效") {
		t.Errorf("菜单末尾那句话是 %q，没有说清「已经失效」", last)
	}
}

// TestPlayingAllSkipsTheInvalidEntries 钉住「播放全部」滤掉失效的那些。
//
// 🔴 **这是这一票真正的目的**：失效的条目**不该进队列**——进去了应用就要为
// 它白跑一趟带签名的取流往返（实测 200~400ms），而那一趟注定失败。用户等的
// 就是这一串串超时。
//
// 判据是**队列里有几首**。
func TestPlayingAllSkipsTheInvalidEntries(t *testing.T) {
	medias := []map[string]any{
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
		mediaOf(3, "也正常的一首"),
	}
	_, s, tt := collectionShell(t, "12345", medias...)
	waitSongs(t, tt, s, 3)
	tt.Frame()

	// ⚠️ **取流要另挂**：这个收藏夹的夹具只伺服收藏夹内容，不伺服取流
	// （`/x/player/pagelist` 那边它 404）。
	mountQueueStreaming(t, s.app)

	if err := tt.Click("播放全部"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "队列换成这一页的歌", func() bool {
		got := s.app.Queue().Items()
		return len(got) == 2
	})
	got := s.app.Queue().Items()
	if len(got) != 2 {
		t.Fatalf("队列里有 %d 首，want 2（失效的那首不该进来）：%+v", len(got), got)
	}
	for _, it := range got {
		if it.Bvid == "BV12" {
			t.Fatal("失效的那首进了队列")
		}
	}
}

// TestPlayingAllSaysHowManyWereSkipped 钉住「跳过了要说一声」。
//
// 🔴 **用户数得出来**：列表里画着、标着「已失效」的那些，一首都不在队列里。
// 不说的话，用户看到的是「这一页有 3 首，而只放了 2 首」，而界面上一个字的
// 解释都没有——那几个字只差一个括号，却决定用户会不会去找那首少的歌。
func TestPlayingAllSaysHowManyWereSkipped(t *testing.T) {
	medias := []map[string]any{
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
	}
	_, s, tt := collectionShell(t, "12345", medias...)
	waitSongs(t, tt, s, 2)
	tt.Frame()

	mountQueueStreaming(t, s.app)

	if err := tt.Click("播放全部"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "说清跳过了几首", func() bool {
		for _, tx := range tt.Texts() {
			if strings.Contains(tx, "失效") && strings.Contains(tx, "没放进来") {
				return true
			}
		}
		return false
	})
}

// TestDoubleClickingAnInvalidRowDoesNotAskForAStream 钉住「双击」那一条路。
//
// 🔴 **这一条是审查补上的**（票 #624 的评审第 1 条）：双击 / 回车 / 点封面上的
// 播放三角走的是 `EnqueueAndPlayNow`，而它原先**不判失效**——于是失效的条目
// 照样进队列、照样**白跑一趟带签名的取流往返**（实测 200~400ms），而那一趟
// 注定失败。
//
// 而双击是收藏夹页**最常用**的播放手势——所以这一条是这一票的核心判据。
//
// ⚠️ **判据是取流接口被问了几次**：光断「队列里有几首」不够——那一首进队列
// 之后**起播**才是那一趟取流，而「起播失败之后被自动跳过」（`autoSkipDead`）
// 会把队列挪回去，于是「队列里几首」那个判据量不到那一趟往返。
func TestDoubleClickingAnInvalidRowDoesNotAskForAStream(t *testing.T) {
	b := newCollectionBili(t)
	b.setMedias(invalidMediaOf(1, "失效的一首"))
	_, s, tt := collectionShellFrom(t, b, nil, "", "12345")
	waitSongs(t, tt, s, 1)
	tt.Frame()

	// ⚠️ **取流要挂上**：这一条要验的就是「取流**没有**发生」，而那台假 B 站
	// 得**会伺服**取流——不然「没发生」是因为「压根不会」，不是因为我拦住了。
	mountQueueStreaming(t, s.app)
	before := b.playURLHits()

	// 双击那一行。
	r, ok := tt.Find("失效的一首")
	if !ok {
		t.Fatalf("画面上找不到那一行：文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	for range 10 {
		tt.Frame()
	}

	// 🔴 **取流一次都不该有**。
	if got := b.playURLHits(); got != before {
		t.Errorf("取流接口被问了 %d 次，want %d（失效的那一条不该触发取流）", got, before)
	}
	// 🔴 **队列里也不该有它**。
	if got := s.app.Queue().Items(); len(got) != 0 {
		t.Errorf("队列里有 %d 首，want 0：%+v", len(got), got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 「隐藏失效内容」开着的时候（`Settings.HideInvalidMedias`）
// ─────────────────────────────────────────────────────────────────────────────

// withHideInvalid 是外壳选项：把「隐藏失效内容」那一格打开。
//
// ⚠️ **它走的是启动那条路**（`withSettings` → `MountSettings`），不是建完
// 外壳再 `UpdateSettings`——后者会把「刚打开就有在途请求」那一支一起带上，
// 而我们要验的是「建列表的时候就是开着的」这个干净状态。
func withHideInvalid() shellOpt {
	st := app.DefaultSettings("")
	st.HideInvalidMedias = true
	return withSettings(st)
}

// TestHidingInvalidEntriesRemovesTheRowsAndKeepsTheHeaderTotal 钉住打开之后
// 界面上看见的两件事：
//
//	失效的行不画	「这里少了两首」变成「这里只剩这些」
//	表头还是那个总数	B 站说有 3 首，已隐藏 1 首——**不是**写成「2 首」
//
// 后一半是这一票最容易做错的：表头读的是 B 站的 `media_count`，不是
// 列表里还剩几行。
func TestHidingInvalidEntriesRemovesTheRowsAndKeepsTheHeaderTotal(t *testing.T) {
	_, s, tt := collectionShellOpts(t, []shellOpt{withHideInvalid()},
		"", "12345",
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
		mediaOf(3, "也正常的一首"),
	)
	waitFrames(t, tt, "三行都收到", func() bool {
		return s.collection != nil && s.collection.list.Paged().Received() == 3
	})
	tt.Frame()

	if tt.HasText("失效的一首") {
		t.Errorf("失效的那一行画出来了，文字有 %q", tt.Texts())
	}
	if !tt.HasText("正常的一首") || !tt.HasText("也正常的一首") {
		t.Errorf("正常的行没画全，文字有 %q", tt.Texts())
	}
	if got := s.collection.list.Paged().Loaded; got != 2 {
		t.Errorf("列表里有 %d 行，want 2", got)
	}
	wantText(t, tt, "3 首")
	wantText(t, tt, "已隐藏 1 首失效内容")
}

// TestTheHeaderDurationStillCountsHiddenRows 钉住「总时长是收到过的每一行之和」。
//
// 🔴 **判据是「6 分钟」出现在表头**：两条 185 秒的内容加起来是 6 分 10 秒，
// 而**可见的只有一行**——旧实现拿「可见行 == 总数」当完成判据，隐藏一开
// 永远等不到，一个时长都不写。
func TestTheHeaderDurationStillCountsHiddenRows(t *testing.T) {
	_, s, tt := collectionShellOpts(t, []shellOpt{withHideInvalid()},
		"", "12345",
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
	)
	waitFrames(t, tt, "两行都收到", func() bool {
		return s.collection != nil && s.collection.list.Paged().Received() == 2
	})
	tt.Frame()

	wantText(t, tt, "6 分钟")
}

// TestAllInvalidCollectionShowsTheAllHiddenEmptyState 钉住「全都失效」是
// 自己的空状态，不是落进「这个收藏夹本来就是空的」。
func TestAllInvalidCollectionShowsTheAllHiddenEmptyState(t *testing.T) {
	_, s, tt := collectionShellOpts(t, []shellOpt{withHideInvalid()},
		"", "12345",
		invalidMediaOf(1, "失效的一首"),
		invalidMediaOf(2, "也失效的一首"),
	)
	waitFrames(t, tt, "全都收到、停稳", func() bool {
		return s.collection != nil && s.collection.list.Paged().Settled()
	})
	tt.Frame()

	wantText(t, tt, onlyInvalid.Title)
	dontWantText(t, tt, noFavorites.Title)
	dontWantText(t, tt, noSearchResults.Title)
	if got := s.collection.list.Paged().Hidden; got != 2 {
		t.Errorf("Hidden = %d，want 2", got)
	}
	// ⚠️ **操作条上两枚键该是灰的**：一行可见的都没有，点了也只会得到一句
	// 「这里还没有可以播的歌」——让用户先看见状态再决定要不要去开开关。
	if a, ok := tt.Access("播放全部"); !ok || !a.Disabled {
		t.Errorf("全都失效时「播放全部」该是灰的，Access = %+v", a)
	}
	if a, ok := tt.Access("随机播放"); !ok || !a.Disabled {
		t.Errorf("全都失效时「随机播放」该是灰的，Access = %+v", a)
	}
}

// TestTheAllHiddenEmptyStateOffersAWayBack 钉住恢复路：空状态里的那一枚键
// 直接把开关拨回去，失效行重新出现，不用再跑到设置页找。
func TestTheAllHiddenEmptyStateOffersAWayBack(t *testing.T) {
	_, s, tt := collectionShellOpts(t, []shellOpt{withHideInvalid()},
		"", "12345",
		invalidMediaOf(1, "失效的一首"),
		invalidMediaOf(2, "也失效的一首"),
	)
	waitFrames(t, tt, "全都收到、停稳", func() bool {
		return s.collection != nil && s.collection.list.Paged().Settled()
	})
	tt.Frame()

	// ⚠️ 那一枚键在空状态的滚动区里，可能在播放条下面——滚上来再点（同
	// `clickAfterScrollingIntoView` 在「去发现音乐」上的用法）。
	clickAfterScrollingIntoView(t, tt, "显示失效内容")
	waitFrames(t, tt, "开关拨回去、两行都回来", func() bool {
		return s.collection != nil && s.collection.list.Paged().Loaded == 2
	})
	if s.app.SettingsSnapshot().HideInvalidMedias {
		t.Error("点了「显示失效内容」之后设置里还开着")
	}
	if !tt.HasText("失效的一首") || !tt.HasText("也失效的一首") {
		t.Errorf("两行没回来，文字有 %q", tt.Texts())
	}
}

// TestSearchingForOnlyInvalidRowsShowsAllHiddenNotNoResults 钉住「搜出来的
// 全是失效的」和「这个词不存在」是两种空：前者给「显示失效内容」那条路，
// 后者给「清空搜索」那条路，说混了用户会对错地方。
func TestSearchingForOnlyInvalidRowsShowsAllHiddenNotNoResults(t *testing.T) {
	_, s, tt := collectionShellOpts(t, []shellOpt{withHideInvalid()},
		"", "12345",
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的告白气球"),
	)
	waitFrames(t, tt, "两行都收到", func() bool {
		return s.collection != nil && s.collection.list.Paged().Received() == 2
	})
	tt.Frame()

	// 搜一个只命中失效那行的词（走 `applyCollectionKeyword` 那一条正路——
	// 界面上那一层是输入框，而我们要验的是「词发出去之后」的结果）。
	p := s.collection
	if !s.applyCollectionKeyword(p, "告白气球") {
		t.Fatal("词没变，这条用例造错了")
	}
	s.reloadCollection(p)
	waitFrames(t, tt, "搜索落地、停稳", func() bool {
		return s.collection != nil && s.collection.list.Paged().Settled() &&
			s.collection.list.Paged().Received() == 1
	})
	tt.Frame()

	wantText(t, tt, onlyInvalidInSearch.Title)
	dontWantText(t, tt, noSearchResults.Title)
}

// TestAPageFailureWinsOverTheAllHiddenState 钉住判据顺序：**失败排在
// 「全都失效」前面**。第一页全失效、第二页拉失败，用户要的是重试入口，
// 不是一句「全都失效了」（那一支没有重试键）。
func TestAPageFailureWinsOverTheAllHiddenState(t *testing.T) {
	b := newCollectionBili(t)
	b.setPaging(1)
	// ⚠️ **要给两行**：一行的话第一页就到头了（`has_more` false），
	// 失败的那一页根本不会被问——这条要的是「第一页全失效、第二页拉失败」。
	b.setMedias(invalidMediaOf(1, "失效的一首"), invalidMediaOf(2, "也失效的一首"))
	b.failOnPage(2)
	_, s, tt := collectionShellFrom(t, b, []shellOpt{withHideInvalid()}, "", "12345")

	waitFrames(t, tt, "第一页落地、第二页失败", func() bool {
		return s.collection != nil && s.collection.list.Paged().Err != nil
	})
	tt.Frame()

	wantText(t, tt, "加载失败，点此重试")
	dontWantText(t, tt, onlyInvalid.Title)
}

// TestAllInvalidPageKeepsPagingUntilAValidRowArrives 钉住跨页：第一页全是
// 失效的、后面还有，自动续拉照样往下翻——不能在「没有可见行」那里停住。
func TestAllInvalidPageKeepsPagingUntilAValidRowArrives(t *testing.T) {
	b := newCollectionBili(t)
	b.setPaging(1)
	b.setMedias(
		invalidMediaOf(1, "失效的一首"),
		mediaOf(2, "正常的一首"),
	)
	_, s, tt := collectionShellFrom(t, b, []shellOpt{withHideInvalid()}, "", "12345")

	waitFrames(t, tt, "翻到第二页、看见正常的", func() bool {
		return s.collection != nil && s.collection.list.Paged().Loaded == 1
	})
	tt.Frame()

	if got := b.seenPages(); len(got) < 2 {
		t.Errorf("只翻了 %v 页，want 至少 [1 2]：全失效页不能停手", got)
	}
	if got := s.collection.list.Paged().Hidden; got != 1 {
		t.Errorf("Hidden = %d，want 1", got)
	}
	wantText(t, tt, "正常的一首")
}

// TestTurningHidingOffRebuildsTheListAndRevealsInvalidRows 钉住「设置变了
// 列表重建」：关掉之后失效行回来，表头那句「已隐藏」消失——计数属于新实例，
// 旧的那份不会跟过来。
func TestTurningHidingOffRebuildsTheListAndRevealsInvalidRows(t *testing.T) {
	_, s, tt := collectionShellOpts(t, []shellOpt{withHideInvalid()},
		"", "12345",
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
	)
	waitFrames(t, tt, "两行都收到", func() bool {
		return s.collection != nil && s.collection.list.Paged().Received() == 2
	})
	tt.Frame()

	if _, err := s.app.UpdateSettings(func(st *app.Settings) { st.HideInvalidMedias = false }); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "新列表把两行都拉回来", func() bool {
		return s.collection != nil && s.collection.list.Paged().Loaded == 2
	})
	tt.Frame()

	if got := s.collection.list.Paged().Hidden; got != 0 {
		t.Errorf("新列表 Hidden = %d，want 0（旧列表的计数跟过来了）", got)
	}
	wantText(t, tt, "失效的一首")
	dontWantText(t, tt, "已隐藏")
}

// TestAPageFromTheOldQueryDoesNotPolluteTheNewList 钉住「在途结果不串实例」：
// 切换隐藏开关重建列表之后，**旧列表还在路上的那一页**不能把它的隐藏数写进
// 新列表——计数是跟着实例走的，重建就是新的一份。
func TestAPageFromTheOldQueryDoesNotPolluteTheNewList(t *testing.T) {
	b := newCollectionBili(t)
	// ⚠️ **holdList 只建一次**：夹具读到非 nil 就等，我们放行之后清掉它
	// （重建的那一趟还会走同一个 handler，留着的话第二趟也卡住了）。
	b.holdList = make(chan struct{})
	b.setMedias(invalidMediaOf(1, "失效的一首"), mediaOf(2, "正常的一首"))

	_, s, tt := collectionShellFrom(t, b, []shellOpt{withHideInvalid()}, "", "12345")
	// 等第一页**真的在路上**了再动开关——不然「在途」这个前提就不成立。
	waitFrames(t, tt, "第一页在途", func() bool {
		return s.collection != nil && s.collection.list.Paged().Loading
	})
	if _, err := s.app.UpdateSettings(func(st *app.Settings) { st.HideInvalidMedias = false }); err != nil {
		t.Fatal(err)
	}
	// 跑一帧让 `syncCollection` 发现设置变了、把列表重建掉（旧 gen 作废）。
	tt.Frame()
	// 现在放行旧列表那趟在途请求——它回来的时候新列表已经换过 gen 了。
	close(b.holdList)
	b.holdList = nil
	waitFrames(t, tt, "新列表把两行都拉回来", func() bool {
		return s.collection != nil && s.collection.list.Paged().Loaded == 2
	})

	if got := s.collection.list.Paged().Hidden; got != 0 {
		t.Errorf("新列表 Hidden = %d，want 0：旧列表在途的结果把隐藏数写进来了", got)
	}
}

// TestPlayingAllDoesNotReportHiddenRowsAsSkipped 钉住「隐藏」和「跳过」不是
// 同一件事：隐藏的行根本没进 `allMedias`，Toast 不该再说「跳过了 N 首」——
// 表头的「已隐藏 1 首」已经说过了，再说一遍就是同一件事数了两遍。
func TestPlayingAllDoesNotReportHiddenRowsAsSkipped(t *testing.T) {
	_, s, tt := collectionShellOpts(t, []shellOpt{withHideInvalid()},
		"", "12345",
		mediaOf(1, "正常的一首"),
		invalidMediaOf(2, "失效的一首"),
	)
	waitFrames(t, tt, "两行都收到", func() bool {
		return s.collection != nil && s.collection.list.Paged().Received() == 2
	})
	tt.Frame()

	mountQueueStreaming(t, s.app)
	if err := tt.Click("播放全部"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "队列换成这一页的歌", func() bool {
		return len(s.app.Queue().Items()) == 1
	})

	if got := s.app.Queue().Items(); len(got) != 1 {
		t.Fatalf("队列里有 %d 首，want 1（失效的那首连队列都进不来）", len(got))
	}
	// ⚠️ **Toast 不该说「跳过」**：隐藏的行不是「没放进来」，是压根不在列表里。
	for _, tx := range tt.Texts() {
		if strings.Contains(tx, "没放进来") || strings.Contains(tx, "跳过") {
			t.Errorf("Toast 说了「%s」：隐藏的行不该再被数一遍", tx)
		}
	}
}
