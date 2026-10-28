// 「把这一首放进哪些收藏夹」那个弹窗的测试。
//
// 宿主和那台假 B 站在 foldertest_test.go 里。

package ui

import (
	"strconv"
	"sync"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 收藏夹选择弹窗
// ─────────────────────────────────────────────────────────────────────────────

// openSelectFromMenu 从一首歌的右键菜单点「收藏」，把选择弹窗打开。
func openSelectFromMenu(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.RightClick("歌 一"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("收藏"); err != nil {
		t.Fatalf("右键菜单里没有「收藏」：%v（菜单是 %v）", err, tt.Menu())
	}
}

// TestTheSelectDialogLoadsEveryFolderOneHundredAtATime 选择弹窗把全量都拉回来。
//
// 🔴 这一条钉的是**步长就是 100**。翻页的循环在 app 层，而它的步长照抄
// `bili.CreatedFolderPageSize`——两处一漂就是「翻到第二页时步长变了」，
// 用户有 150 个收藏夹时第 101 个开始会重复或者漏掉，而界面上看不出来。
func TestTheSelectDialogLoadsEveryFolderOneHundredAtATime(t *testing.T) {
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{
		folderPage(1, bili.CreatedFolderPageSize),
		folderPage(1000, 3), // 最后一页不满
	}
	s, tt := selectPageShell(t, f)
	openSelectFromMenu(t, tt)
	settleFrame(tt)

	waitFrames(t, tt, "两页都翻完", func() bool {
		return s.folderSelect != nil && s.folderSelect.loaded && len(s.folderSelect.list) == bili.CreatedFolderPageSize+3
	})

	ps, pn := f.asks()
	if len(ps) != 2 {
		t.Fatalf("发了 %d 次列表请求（页码 %v），want 2", len(ps), pn)
	}
	for _, got := range ps {
		if got != "100" {
			t.Errorf("ps = %q，want 100", got)
		}
	}
	// 最后一页不满 → **不该再多翻一页**：多翻的那一页是空的，而界面上的
	// 表现是列表末尾多出一行空白的收藏夹，勾它等于往一个不存在的收藏夹里
	// 放东西。
	if pn[0] != "1" || pn[1] != "2" {
		t.Errorf("页码 = %v，want [1 2]", pn)
	}
	// 第一页第一个和第二页第一个都要真的画出来（列表是虚拟滚动的，
	// 第二个在视口外——所以这里只查状态，不查画面）。
	if s.folderSelect.list[0].Title != "收藏夹 1" {
		t.Errorf("第一个 = %q", s.folderSelect.list[0].Title)
	}
	if s.folderSelect.list[100].Title != "收藏夹 1000" {
		t.Errorf("第二个页的第一个 = %q", s.folderSelect.list[100].Title)
	}
}

// TestTheSelectDialogSelectsSeveralAndAll 选择弹窗能多选、也能全选。
func TestTheSelectDialogSelectsSeveralAndAll(t *testing.T) {
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{folderPage(1, 4)}
	s, tt := selectPageShell(t, f)
	openSelectFromMenu(t, tt)
	settleFrame(tt)
	waitFolders(t, tt, s, 4)

	wantText(t, tt, "还没有收藏在任何收藏夹里")
	wantText(t, tt, "收藏夹 1")

	// 点第一行。
	if err := tt.Click("收藏夹 1"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	wantText(t, tt, "已选 1 个收藏夹")

	// 全选。
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	wantText(t, tt, "已选 4 个收藏夹")
	if !s.folderSelect.allSelected() {
		t.Error("点了全选，allSelected 却说没全选")
	}

	// 再点一下是全不选。
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	wantText(t, tt, "已选 0 个收藏夹")
}

// TestTheSelectDialogSubmitIsAsync 提交是异步的：点了确认弹窗不立刻关。
//
// ⚠️ 关得早一点看不出问题——B 站那边失败的时候界面会说一句，而用户已经看到
// 「改好了」。要钉的是**点下去的那一帧弹窗还在、按钮在转**。
func TestTheSelectDialogSubmitIsAsync(t *testing.T) {
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	defer release()
	f := newFolderBili(t, gate)
	f.pages = [][]map[string]any{folderPage(1, 3)}
	s, tt := selectPageShell(t, f)
	openSelectFromMenu(t, tt)
	settleFrame(tt)
	waitFolders(t, tt, s, 3)

	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if err := tt.Click("确认"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	if !tt.HasText("提交中…") {
		t.Fatalf("点确认之后按钮没有转圈，文字有 %q", tt.Texts())
	}
	if s.folderSelect == nil || !s.folderSelect.open {
		t.Fatal("点确认之后弹窗立刻关了，那不是异步")
	}

	// ⚠️ **提交中点外面 / Esc 也不能关**：理由同编辑弹窗那一条——B 站那边
	// 成功的时候用户看不到任何东西，于是他会以为没生效再点一次确认，
	// 于是同一个收藏夹被放两次。
	tt.ClickAt(8, 8)
	settleFrame(tt)
	tt.Key(0, mygoui.KeyEscape)
	settleFrame(tt)
	if s.folderSelect == nil || !s.folderSelect.open {
		t.Fatal("提交中的收藏夹选择框被点外面 / Esc 关掉了")
	}

	release()
	waitFrames(t, tt, "提交结果回来之后弹窗关掉", func() bool {
		return s.folderSelect == nil || !s.folderSelect.open
	})
	deals := f.deals()
	if len(deals) != 1 {
		t.Fatalf("发了 %d 次 deal，want 1", len(deals))
	}
	if got := deals[0].get("add_media_ids"); got != "1,2,3" {
		t.Errorf("add_media_ids = %q，want 1,2,3", got)
	}
	if got := deals[0].get("rid"); got == "" {
		t.Error("rid 没发过去")
	}
}

// TestTheSelectDialogSendsOnlyTheDifference 提交只发差集。
//
// ⚠️ 全量发过去的表现是「本来就在里面的那几个被删一遍再加回来」——B 站的
// `del_media_ids` 和 `add_media_ids` 是同一趟请求里的两个字段，同时出现在
// 两个里的那一条最后状态是未知的。
func TestTheSelectDialogSendsOnlyTheDifference(t *testing.T) {
	f := newFolderBili(t, nil)
	page := folderPage(1, 3)
	// 第 2 个本来就在里面（fav_state=1）。
	page[1]["fav_state"] = 1
	f.pages = [][]map[string]any{page}

	s, tt := selectPageShell(t, f)
	openSelectFromMenu(t, tt)
	settleFrame(tt)
	waitFolders(t, tt, s, 3)

	// 初始勾选照接口来的：已经在里面的那个是勾上的。
	wantText(t, tt, "已选 1 个收藏夹")

	// 勾上第一个，第二个取消掉 → 加 1、拿 1。
	if err := tt.Click("收藏夹 1"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if err := tt.Click("收藏夹 2"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	wantText(t, tt, "已选 1 个收藏夹")

	if err := tt.Click("确认"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "提交结果回来", func() bool { return len(f.deals()) == 1 })

	deals := f.deals()
	if got := deals[0].get("add_media_ids"); got != "1" {
		t.Errorf("add_media_ids = %q，want 1", got)
	}
	if got := deals[0].get("del_media_ids"); got != "2" {
		t.Errorf("del_media_ids = %q，want 2", got)
	}
}

// TestTheSelectDialogRefusesLocalFiles 本地文件不弹这个框。
//
// ⚠️ 本地歌不在 B 站上，没有 media_id。给一个 0 的话 B 站回 -400，而用户看到
// 的是「收藏失败」——他不知道自己收藏的是一首本地歌。
func TestTheSelectDialogRefusesLocalFiles(t *testing.T) {
	f := newFolderBili(t, nil)
	s, tt := folderShell(t, f)
	settleFrame(tt)

	s.openFolderSelect(app.Item{Source: "local", Path: "/tmp/a.flac", Title: "本地歌"})
	settleFrame(tt)
	if s.folderSelect != nil {
		t.Fatal("本地文件也弹出了收藏夹选择框")
	}
	if !tt.HasText("还没有可以收藏") {
		t.Errorf("没有说清楚为什么不能收藏，文字有 %q", tt.Texts())
	}
}

// waitFolders 等选择弹窗的列表拉回来。
func waitFolders(t *testing.T, tt *mygoui.Tester, s *Shell, n int) {
	t.Helper()
	waitFrames(t, tt, strconv.Itoa(n)+" 个收藏夹加载完", func() bool {
		return s.folderSelect != nil && s.folderSelect.loaded && len(s.folderSelect.list) == n
	})
}

// selectPageShell 造一个停在歌单页上、里面有一首歌的外壳。
//
// 走真的歌单页（而不是直接调 `openFolderSelect`）是因为「从一首歌的右键菜单
// 点收藏」本身就是这一票接进去的那一段——直接调那个函数的话，接线断了也
// 测不出来。
func selectPageShell(t *testing.T, f *folderBili) (*Shell, *mygoui.Tester) {
	t.Helper()
	f.lock()
	f.info["media_count"] = 1
	f.unlock()

	s, tt := folderShell(t, f)
	s.navigate("/collection/42")
	waitSongs(t, tt, s, 1)
	return s, tt
}
