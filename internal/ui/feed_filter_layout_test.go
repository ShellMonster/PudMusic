package ui

import (
	"fmt"
	"net/http"
	"testing"
)

// TestTheAuthorCollapseControlStaysWhereExpandWas 钉住「展开和收起是同一枚
// 开关，位置不跑」。
//
// # 曾经出错的行为
//
// 展开后「收起」被单独放进一条右对齐的新行，跑到整个作者流的最下面。作者
// 一多，它离用户刚点的「+N」隔六七行，甚至落到首屏之外，看起来像不见了。
//
// 这一条要求收起键接在第 12 位作者后面，也就是「+N」原来所在的位置；点它
// 之后「+N」也回到同一个落点。
func TestTheAuthorCollapseControlStaysWhereExpandWas(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
	})
	authors := make([]any, 0, 20)
	for i := 1; i <= 20; i++ {
		authors = append(authors, map[string]any{
			"mid": 100 + i, "uname": fmt.Sprintf("第 %d 个 UP", i),
		})
	}
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"total": len(authors), "list": authors, "has_more": false,
		})
	})

	s, tt, _ := newSpaceShell(t, b, true, "77", at(1400, 900))
	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "作者到了", func() bool { return len(f.authors) == len(authors) })
	tt.Frame()

	expand, ok := tt.Find("+8")
	if !ok {
		t.Fatalf("没找到展开键，文字有 %q", tt.Texts())
	}
	if err := tt.Click("+8"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	twelfth, ok := tt.Find("第 12 个 UP")
	if !ok {
		t.Fatalf("展开后没找到第 12 位作者，文字有 %q", tt.Texts())
	}
	collapse, ok := tt.Find("收起")
	if !ok {
		t.Fatalf("展开后没找到收起键，文字有 %q", tt.Texts())
	}
	if d := absF((collapse.Y + collapse.H/2) - (twelfth.Y + twelfth.H/2)); d > 1 {
		t.Errorf("收起键和第 12 位作者不在同一行：中心相差 %.1f", d)
	}
	if d := collapse.X - (twelfth.X + twelfth.W); d < 0 || d > feedAuthorChipMaxW {
		t.Errorf("收起键没有紧接在第 12 位作者后面：间距 %.1f", d)
	}
	if d := absF(collapse.X - expand.X); d > 1 {
		t.Errorf("收起键跑到了 %.1f，展开键原来在 %.1f", collapse.X, expand.X)
	}

	if err := tt.Click("收起"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	again, ok := tt.Find("+8")
	if !ok {
		t.Fatalf("收起之后展开键没回来，文字有 %q", tt.Texts())
	}
	if d := absF((again.Y + again.H/2) - (collapse.Y + collapse.H/2)); d > 1 {
		t.Errorf("收起之后开关跳了 %.1f", d)
	}
}
