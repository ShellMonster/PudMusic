// 「顶栏搜索框 + 联想面板」重设计（2026-11）的 E2E：从「敲一个词」开始，到
// 「键盘选一条联想词、回车落到搜索页」为止，中间走的是真的输入事件、真的
// 防抖、真的联想请求、真的路由——不是把 `submitSearch` 单独叫来验。
//
//	go test ./internal/ui/ -run TestSearchingFromTheKeyboardEndToEnd
//
// # 这一条为什么必须存在
//
// 这次重设计把「面板往哪儿贴、词怎么选中、回车搜什么」全换了一遍，而这些
// 件件都压在**一条链**上：输入框的事件 → 防抖 → 联想返回 → 面板摆出来 →
// 方向键挪 → 回车搜 + 填回框 + 收面板 + 切路由。中间任何一环断了，上面那些
// 分着测的用例都绿、而用户那边是「回车搜的还是那半截词」。
//
// ⚠️ **一个字都不许打真 B 站**：夹具是本地假服务器（`biliFixture`）。
package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestSearchingFromTheKeyboardEndToEnd 钉住「敲词 → 联想出来 → 方向键选中 →
// 回车 → 落到搜索页、框里是那条词、面板收起、历史记下了」这一整条。
//
// 判据是**用户看得见的三样**：URL 上的 `q`（搜的是哪条词）、框里的字（填回去
// 了）、浮层关没关。不是「某个函数被调了」。
func TestSearchingFromTheKeyboardEndToEnd(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦 晴天", "周杰伦 新歌")
	s, tt := newSuggestShell(t, f)
	tt.Frame()

	// 敲一个词 → 防抖走完 → 联想那一档出来。
	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词那一档出来", func() bool { return tt.HasText("晴天") })
	settleSuggestEnter(t, tt)

	// 没有历史时联想就是头一档：按一下 ↓ 选中第一条「周杰伦 晴天」。
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if got := s.router.Path(); got != searchPath {
		t.Fatalf("回车之后在 %q，want %q——没有落到搜索页", got, searchPath)
	}
	if got := s.router.Query("q"); got != "周杰伦 晴天" {
		t.Errorf("回车之后 URL 上的 q 是 %q，want %q——搜的不是选中的那条词",
			got, "周杰伦 晴天")
	}
	if got := s.searchKeyword; got != "周杰伦 晴天" {
		t.Errorf("回车之后框里是 %q，want %q——那条词没填回框里", got, "周杰伦 晴天")
	}
	if s.searchSuggest {
		t.Errorf("回车之后浮层还开着（searchSuggest=true）")
	}
	// 搜过的那条词进了历史（`submitSearch` 的 `hist.add`）。
	found := false
	for _, item := range s.hist.list() {
		if item.Value == "周杰伦 晴天" {
			found = true
		}
	}
	if !found {
		t.Errorf("搜过的「周杰伦 晴天」没进历史，历史有 %v", s.hist.list())
	}
}
