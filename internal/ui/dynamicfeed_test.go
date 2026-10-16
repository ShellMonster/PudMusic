// 动态流那一页（「用户动态」）的测试。
//
// 假 B 站、壳、等状态的几个工具在 upspace_test.go 里（同一个包，共用）。
package ui

import (
	"net/http"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// 动态流那一页
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAuthorListPagesAndEachRowGoesToThatSpace 是「动态流左侧作者栏能
// 翻页、能点进对应 UP 空间」那条验收的钉子。
func TestTheAuthorListPagesAndEachRowGoesToThatSpace(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
	})
	// 一页 2 个、`total` 说 5 个 → 还能翻。三页各自给不同的名字，
	// 好断言「翻页真的翻了」。
	pages := map[string][]any{
		"1": {
			map[string]any{"mid": 11, "uname": "第一个 UP", "face": "https://f/1.jpg"},
			map[string]any{"mid": 12, "uname": "第二个 UP"},
		},
		"2": {
			map[string]any{"mid": 13, "uname": "第三页的一个"},
		},
		"3": {
			map[string]any{"mid": 14, "uname": "第四页的一个"},
		},
		"4": {map[string]any{"mid": 15, "uname": "第五页的一个"}},
		"5": {map[string]any{"mid": 16, "uname": "第六页的一个"}},
	}
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		pn := r.URL.Query().Get("pn")
		// 超过 total 之后 B 站给的是**空 list**（而不是报错），照它那样回：
		// 不这么造的话作者栏会一直翻一直有，而真机上早就停了。
		if pn == "5" {
			pn = ""
		}
		uiExploreJSON(w, 0, map[string]any{"total": 5, "list": pages[pn]})
	})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "第一页关注的人到了", func() bool { return len(f.authors) >= 2 })
	tt.Frame()

	if !tt.HasText("全部") {
		t.Errorf("作者栏第一行该是「全部」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("第一个 UP") {
		t.Errorf("关注的人没画出来，文字有 %q", tt.Texts())
	}

	// 翻页：滚到作者栏底部（哨兵进画面）就会拉下一页。
	for i := 0; i < 5 && len(f.authors) < 5; i++ {
		if !f.auth.hasMore {
			break
		}
		tt.Scroll(60, 400, 0, -2000)
		tt.Frame()
		before := len(f.authors)
		waitFrames(t, tt, "作者栏翻到下一页", func() bool { return len(f.authors) > before })
		tt.Frame()
	}
	if len(f.authors) != 5 {
		t.Fatalf("翻了之后作者栏有 %d 个人，want 5（total 就是 5）", len(f.authors))
	}
	if !showsText(tt, "第五页的一个") {
		t.Errorf("翻页之后新一页的人没画出来，文字有 %q", tt.Texts())
	}

	// 点一个作者 = 跳进他的 UP 空间。
	if err := tt.Click("第三页的一个"); err != nil {
		t.Fatal(err)
	}
	if got := s.router.Path(); got != "/user/13" {
		t.Errorf("点了作者跳到了 %q，want /user/13", got)
	}
}

// TestTheAuthorListIsNotShownWithoutALogin 钉住「没登录不假装有内容」。
//
// 作者栏列的是「我关注的人」，没有登录态就没有这个列表。B 站那个接口会回
// -101，而 -101 在这一层和「网络断了」长得一模一样。
func TestTheAuthorListIsNotShownWithoutALogin(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")

	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "动态到了", func() bool { return f.started })
	tt.Frame()

	if !tt.HasText("登录之后能看到你关注的人") {
		t.Errorf("没登录时作者栏该说清为什么是空的，文字有 %q", tt.Texts())
	}
	if b.hit("/x/relation/followings") {
		t.Error("没登录还去问了关注列表（那个接口会回 -101）")
	}
}

// TestTheDynamicFeedEmptyStateSaysWhatToDo 钉住动态流空着时不是一片白。
func TestTheDynamicFeedEmptyStateSaysWhatToDo(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, true, "77")

	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "动态到了", func() bool { return f.started })
	tt.Frame()

	if !tt.HasText("暂无动态") {
		t.Errorf("空着的动态流是一句都没说，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 崩溃兜底
// ─────────────────────────────────────────────────────────────────────────────

// TestBothPagesSurviveAPanicInTheirView 🔴「view 里 panic 会杀死进程」。
//
// 这两条路由接在 `builtPage` 上，而 `Shell.page` 在外面包了一层 recover。
// 这一条钉的是「新增的 view 走了那一层」——它要是没走，这里就会看到
// 整个测试进程被带走。
func TestBothPagesSurviveAPanicInTheirView(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "会崩的 UP", 0, 1)
	for _, p := range []string{
		"/x/polymer/web-dynamic/v1/feed/space", "/x/space/wbi/arc/search",
		"/x/v3/fav/folder/created/list", "/x/polymer/web-space/seasons_series_list",
		"/x/polymer/web-dynamic/v1/feed/all", "/x/relation/followings",
	} {
		p := p
		b.on(p, func(w http.ResponseWriter, r *http.Request) {
			uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
		})
	}
	s, tt, _ := newSpaceShell(t, b, false, "")

	for _, target := range []string{upPathOf(42), dynamicFeedPath} {
		s.navigate(target)
		tt.Frame()
		if s.crash != nil {
			t.Fatalf("%s 这一页崩了（%v）——它应该被页级 recover 接住才对", target, s.crash)
		}
	}
}
