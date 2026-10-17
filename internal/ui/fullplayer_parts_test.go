// 分集面板：Biu 的 `components/full-screen-player/page-list.tsx` +
// 满屏上那个把手（`index.tsx:404-417`）。
//
// 走的是**整条链**：真的假 B 站（`biliFixture`）回多 P 列表 → `App.Parts` 缓存 →
// 面板画出来 → 点一行 → `App.PlayPart` 换集。所以「入口可用」验的是用户真的能
// 从满屏换到另一集，不是「面板那个函数被调到了」。
package ui

import (
	"net/http"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// multiPages 是一份**三 P** 的回包。
//
// 之所以要造一份：三 P 才有「选」的意思，Biu 的入口条件是
// `playItem.hasMultiPart`（`index.tsx:404`），而默认那份 fixture 只回一集。
func multiPages() []any {
	return []any{
		map[string]any{"cid": 777, "page": 1, "part": "第一集", "duration": 260},
		map[string]any{"cid": 778, "page": 2, "part": "第二集", "duration": 300},
		map[string]any{"cid": 779, "page": 3, "part": "", "duration": 280},
	}
}

// multiParts 是 multiPages 的强类型版本（`App.Parts` 给回来的就是它）。
func multiParts() []bili.VideoPart {
	return []bili.VideoPart{
		{Cid: "777", Page: 1, Part: "第一集"},
		{Cid: "778", Page: 2, Part: "第二集"},
		{Cid: "779", Page: 3, Part: ""},
	}
}

// onlineTester 建一个真的在放**在线**歌的界面（挂着一台假 B 站）。
//
// 为什么不是本地文件：分集是 B 站的概念，`App.Parts` 判的是「当前项是 mv」。
//
// blockPages 是「让分集问不到」（顶掉 pagelist）。⚠️ 队列里那一项**自带 cid**，
// 于是放歌那一步的 `withCid` 压根不问分 P（见 online.go），被顶掉的只有
// `App.Parts` 那一问——正好造出「歌放得起来、可是分集问不到」这个状态。
func onlineTester(t *testing.T, pages []any, blockPages bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.setVideoPages(pages)
	if blockPages {
		serve := f.Server.Config.Handler
		f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/x/player/pagelist" {
				http.Error(w, "风控", http.StatusForbidden)
				return
			}
			serve.ServeHTTP(w, r)
		})
	}

	s, tt := newShell(t)
	tt.SetDark(true)
	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})

	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVmultiPart",
		Cid: "777", Title: "多 P 的那一首",
	}); err != nil {
		t.Fatalf("把在线的那首放进队列：%v", err)
	}
	settleSearch(t, tt, "在线那首开始播", func() bool { return s.app.Snapshot().HasTrack })
	return s, tt
}

// TestThePartsEntryOpensAndSwitchesTheEpisode 是「分集面板入口可用」那一条。
//
// 四段：入口在 → 面板列出三集 → 正在放的那一集认得出来 → 点一行真的换了集
// （队列里那一项的 cid 就地改掉，面板自己收起来）。
func TestThePartsEntryOpensAndSwitchesTheEpisode(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)

	// 面板的数据是**每一帧**问 App 要的，而问 App 会触发第一次取分集。
	// 把手在**还没问到**的时候就摆着（见 viewFullParts 那段），所以这里要等到
	// 三条真的到齐了再点进去——不然点开的是「正在问这一首有哪几集」。
	s.openFullPlayer()
	settleSearch(t, tt, "分集问回来了", func() bool { return len(s.app.Parts()) == 3 })
	if _, ok := tt.Find("显示分集列表"); !ok {
		t.Fatalf("问回来了把手还没摆，文字有 %q", tt.Texts())
	}

	if err := tt.Click("显示分集列表"); err != nil {
		t.Fatalf("点不了分集入口：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	// 第三集**没有标题**（B 站有时候不给）：这时要说「第 3 集」，
	// 不是把行留成空白——空标题在列表里和「这一行没画出来」长得一模一样。
	for _, want := range []string{"第一集", "第二集", "第 3 集"} {
		if !tt.HasText(want) {
			t.Fatalf("面板里没有 %q，文字有 %q", want, tt.Texts())
		}
	}

	if err := tt.Click("第 3 集"); err != nil {
		t.Fatalf("点不了第三集：%v（文字：%q）", err, tt.Texts())
	}
	settleSearch(t, tt, "换到第三集", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && it.Cid == "779"
	})

	// 队列里**还是那一项**（不是插了一首新的）：多 P 的两集在 `Item.Identity()`
	// 里是同一首，插一项新的会让抽屉和队列里同时出现同一首。
	if n := len(s.app.Queue().Items()); n != 1 {
		t.Errorf("队列里有 %d 项，换集之后应该还是 1 项", n)
	}
	// 选完就收：Biu 的 PageList `onPressItem={onClose}`。
	if s.full.partsOpen {
		t.Error("选完一集之后面板还开着")
	}
}

// TestThereIsNoPartsEntryForASinglePartVideo 断入口的**反向**那一半。
//
// Biu 的条件是 `playItem.hasMultiPart`——只有一个分 P 的稿子没有「选」的
// 意思，摆一个把手在那儿只会让人以为点得动什么。
func TestThereIsNoPartsEntryForASinglePartVideo(t *testing.T) {
	s, tt := onlineTester(t, nil, false) // 默认那一份：一集

	s.openFullPlayer()
	settleSearch(t, tt, "分集问回来了", func() bool { return len(s.app.Parts()) > 0 })
	tt.Frame()

	if n := len(s.app.Parts()); n != 1 {
		t.Fatalf("fixture 回的是 %d 集，want 1", n)
	}
	if _, ok := tt.Find("显示分集列表"); ok {
		t.Error("只有一个分 P 还摆了分集把手")
	}
}

// TestThePartsPanelSaysWhyWhenTheyCannotBeAsked 是「问不到就说问不到」。
//
// 一片空白的列表在界面上和「这一首没有分集」长得一模一样；而实际上分集是问 B 站
// 问来的，问不到是网络/风控的事，不是这首没有分集。
//
// 🔴 **这一条是从把手点进去的**，不是直接写 `s.full.partsOpen = true`。第一版
// 就是直接写的，于是那句话在真机上是**死的**：把手只在 `len(parts) > 1` 时才摆，
// 一条都问不到的时候面板从哪儿也进不去，只有测试里进得去。
func TestThePartsPanelSaysWhyWhenTheyCannotBeAsked(t *testing.T) {
	s, tt := onlineTester(t, nil, true)

	s.openFullPlayer()
	settleSearch(t, tt, "问失败", func() bool { return s.app.PartsErr() != nil })
	tt.Frame()

	// 问不到的时候把手也得摆着，否则那句人话没地方显示。
	if _, ok := tt.Find("显示分集列表"); !ok {
		t.Fatalf("一条分集都没问到，把手却没摆，文字有 %q", tt.Texts())
	}
	if err := tt.Click("显示分集列表"); err != nil {
		t.Fatalf("点不了分集入口：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("问不出分集") {
		t.Errorf("面板上没有说为什么，文字有 %q", tt.Texts())
	}
}

// TestPartRowsAreKeyedByTheirCid 钉住铁律四：分集那一列的行身份是 **cid**。
//
// 行号当身份的话，列表往前插一行会让后面所有行的「正在放」整体错配——而这一列
// 会随 B 站改稿而变，插入是常事。
func TestPartRowsAreKeyedByTheirCid(t *testing.T) {
	rows := partRowsFor(multiParts(), app.Item{Cid: "778"})
	if len(rows) != 3 {
		t.Fatalf("造了 %d 行，want 3", len(rows))
	}
	seen := map[string]bool{}
	for i, r := range rows {
		if r.key == "" {
			t.Fatalf("第 %d 行的 key 是空串", i)
		}
		if seen[r.key] {
			t.Fatalf("第 %d 行的 key %q 和前面某一行重了", i, r.key)
		}
		seen[r.key] = true
	}
	if !rows[1].active || rows[0].active || rows[2].active {
		t.Errorf("「正在放」认错了行：%v %v %v", rows[0].active, rows[1].active, rows[2].active)
	}
	// 少一集（往前删掉一行）之后，剩下的行身份必须**不变**——变了就说明它是行号。
	shorter := partRowsFor(multiParts()[:2], app.Item{Cid: "778"})
	if shorter[0].key != rows[0].key || shorter[1].key != rows[1].key {
		t.Errorf("删掉最后一行之后前面两行的身份变了：%q/%q → %q/%q",
			rows[0].key, rows[1].key, shorter[0].key, shorter[1].key)
	}
}

// TestSwitchingEpisodesDoesNotCloseTheFullPlayer 是第一轮评审的 critical。
//
// `App.PlayPart` 换集是 `ClearTrack()` + 异步取流：这两步之间（几百毫秒）
// `HasTrack` 恒为 false。而 `viewFullPlayer` 原来在这里 `closeFullPlayer()`，
// 于是**每一次换集都会把满屏关掉**——用户点一下分集面板就被踢回普通窗口，
// 而分集面板这一票的全部用处正好没了一半。
//
// 判据是满屏自己还在（`full.open`）而且界面上那些字还在（真的画出来了）。
func TestSwitchingEpisodesDoesNotCloseTheFullPlayer(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	s.openFullPlayer()
	settleSearch(t, tt, "分集问回来了", func() bool { return len(s.app.Parts()) == 3 })

	if err := tt.Click("显示分集列表"); err != nil {
		t.Fatalf("点不了分集入口：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click("第 3 集"); err != nil {
		t.Fatalf("点不了第三集：%v（文字：%q）", err, tt.Texts())
	}

	// 一帧一帧地看：**换集的那一瞬**也不能关。
	for i := range 20 {
		tt.Frame()
		if !s.full.open {
			t.Fatalf("第 %d 帧满屏自己关上了（HasTrack=%v）——换集时它不该关",
				i, s.app.Snapshot().HasTrack)
		}
		if !tt.HasText("关闭全屏") {
			t.Fatalf("第 %d 帧满屏的顶栏不见了，文字有 %q", i, tt.Texts())
		}
	}
	// 新歌接上之后满屏还在放新歌（不是卡在空状态上）。
	settleSearch(t, tt, "换到第三集", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && it.Cid == "779" && s.app.Snapshot().HasTrack
	})
	if !s.full.open {
		t.Error("换完之后满屏自己关上了")
	}
}

// TestSwitchingToAnotherOnlineSongDoesNotCloseTheFullPlayer 是同一条的另一半：
// `App.PlayAt` 换另一首在线歌也是 ClearTrack + 异步 play，放完自动连播走的也是
// 这一条路——而那正是这个应用的主场景。
func TestSwitchingToAnotherOnlineSongDoesNotCloseTheFullPlayer(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	s.openFullPlayer()

	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVsecond", Cid: "1", Title: "另一首",
	}); err != nil {
		t.Fatalf("加第二首：%v", err)
	}
	if err := s.app.PlayAt(s.app.Queue().Len() - 1); err != nil {
		t.Fatalf("换到第二首：%v", err)
	}

	for i := range 20 {
		tt.Frame()
		if !s.full.open {
			t.Fatalf("第 %d 帧满屏自己关上了（HasTrack=%v）——换在线歌时它不该关",
				i, s.app.Snapshot().HasTrack)
		}
		if !tt.HasText("关闭全屏") {
			t.Fatalf("第 %d 帧满屏的顶栏不见了，文字有 %q", i, tt.Texts())
		}
	}
}

// TestAFullPlayerWithNoTrackShowsAnEmptyStateInsteadOfVanishing 断那一屏长什么样。
//
// 「没有歌」不是「关掉」——Biu 那边是 `{!playItem ? <Empty/> : …}`（index.tsx:214）。
// 这里量的是：满屏还开着、顶栏还在（关掉的那一条在）、中间是那句空状态。
func TestAFullPlayerWithNoTrackShowsAnEmptyStateInsteadOfVanishing(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)

	s.app.ClearTrack()
	tt.Frame()

	if !s.full.open {
		t.Fatal("清掉播放器之后满屏自己关上了")
	}
	if !tt.HasText(fullNothing.Title) {
		t.Errorf("没有歌的那一屏没有说「暂无播放内容」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("关闭全屏") {
		t.Errorf("没有歌的那一屏连关掉的那一条都没了，文字有 %q", tt.Texts())
	}
	// 还能关：Esc 仍然管用。
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.full.open {
		t.Error("空状态那一屏按 Esc 关不掉")
	}
}
