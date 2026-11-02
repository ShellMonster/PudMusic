package ui

// 顶栏搜索框与建议面板的**几何**（2026-11 重设计）。
//
// 旧面板有两个看得见的歪：锚点挂在**框里那个输入框**上，于是面板左缘对齐的是
// 文字起点（比胶囊左缘缩进 `searchPadX+iconMd+searchGap` = 42）；宽度写死
// `suggestWidth`(360)，比胶囊（旧 240）还往右凸出去一截。重设计后面板锚在外层
// 胶囊上、宽度跟着胶囊走（窄窗胶囊收缩时保一个 `suggestWidth` 的下限）——
// 面板和框是一个整体。
//
// 判据走「带 Label 的元素的矩形」（biugo-706 的教训：像素比色找边线不可靠，
// 几何锚点只用带 Label 的元素）。锚是**面板本体**：它挂着 `suggestPanelLabel`
// 这个无障碍名字（实现里加的），`Find` 返回的就是面板的矩形；输入框自带
// `searchLabel`，它的矩形左缘减去放大镜那一段（`searchPadX+iconMd+searchGap`）
// 就是胶囊左缘。
//
// ⚠️ 联想词行不能当锚：`Find("周杰")` 命中的是命中段那个 **Text**（按下标凸出
// 那段字，实测 25.8 宽），不是 FillWidth 的那一行。

import "testing"

// capsuleLeft 从输入框的矩形反推胶囊左缘：输入框左边是 左内边距 + 放大镜 + 缝。
func capsuleLeft(fieldX float32) float32 {
	return fieldX - (searchPadX + iconMd + searchGap)
}

// TestSuggestPanelAlignsWithTheSearchCapsule 钉住：面板左缘 = 胶囊左缘，宽度跟胶囊。
//
// 用默认窗口（1200×800）：这一档顶栏放得下整宽的胶囊，面板就该和它一样宽。
func TestSuggestPanelAlignsWithTheSearchCapsule(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦")
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词那一档出来", func() bool { return tt.HasText("周杰") })
	// 进场是一段透明度动画，几何不跟着变；但把它推完，量出来的是终态。
	settleSuggestEnter(t, tt)

	panel, ok := tt.Find(suggestPanelLabel)
	if !ok {
		t.Fatalf("浮层上没有挂着 %q 的面板，文字有 %q", suggestPanelLabel, tt.Texts())
	}
	field, ok := tt.Find(searchLabel)
	if !ok {
		t.Fatalf("顶栏上没有搜索框（%q），文字有 %q", searchLabel, tt.Texts())
	}

	if d := panel.X - capsuleLeft(field.X); d < -1 || d > 1 {
		t.Errorf("面板左缘 x=%.1f，胶囊左缘 x=%.1f（差 %.1f）——面板没有跟搜索框左对齐",
			panel.X, capsuleLeft(field.X), d)
	}
	wantW := float32(searchWidth)
	if d := panel.W - wantW; d < -1 || d > 1 {
		t.Errorf("面板宽 %.1f，want %.1f（= 胶囊宽 %d）——面板宽度没有跟着胶囊走",
			panel.W, wantW, searchWidth)
	}
}

// TestSuggestPanelKeepsAFloorInANarrowWindow 钉住窄窗那一档：胶囊被 `MinWidth(0)`
// 压到几十像素时，面板缩到 `suggestWidth` 为止，不再跟着缩——再缩里面那排 chip
// 就摆不开了。左对齐照旧。
func TestSuggestPanelKeepsAFloorInANarrowWindow(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦")
	s, tt := newSuggestShell(t, f, at(900, 560))
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词那一档出来", func() bool { return tt.HasText("周杰") })
	settleSuggestEnter(t, tt)

	panel, ok := tt.Find(suggestPanelLabel)
	if !ok {
		t.Fatalf("浮层上没有挂着 %q 的面板，文字有 %q", suggestPanelLabel, tt.Texts())
	}
	field, ok := tt.Find(searchLabel)
	if !ok {
		t.Fatalf("顶栏上没有搜索框（%q），文字有 %q", searchLabel, tt.Texts())
	}

	if d := panel.X - capsuleLeft(field.X); d < -1 || d > 1 {
		t.Errorf("窄窗下面板左缘 x=%.1f，胶囊左缘 x=%.1f（差 %.1f）——左对齐没有保住",
			panel.X, capsuleLeft(field.X), d)
	}
	if d := panel.W - float32(suggestWidth); d < -1 || d > 1 {
		t.Errorf("窄窗下面板宽 %.1f，want 下限 %d——面板跟着胶囊缩没了", panel.W, suggestWidth)
	}
}

// TestClearingTheSearchBoxClosesThePanel 钉住锚点换到胶囊之后，点清空 × 的净行为
// **没有变**：框清空、面板收起。
//
// 锚在胶囊上之后，点框上任何地方都不再算框架的 `PressedOutside`——但清空键是
// 一枚可聚焦的按钮，按下那一帧焦点从输入框挪到它身上，「失焦收起」那条规则
// （`viewSearchBox`）照样把面板关掉。实测（2026-11-02 探针）：点完
// `searchSuggest=false`、面板不在画面上、焦点不在框里。这一条钉的就是「锚点
// 的改动没有顺手改掉这个行为」。
func TestClearingTheSearchBoxClosesThePanel(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦")
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词那一档出来", func() bool { return tt.HasText("周杰") })

	if err := tt.Click("清空"); err != nil {
		t.Fatalf("点不到清空键：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if s.searchKeyword != "" {
		t.Errorf("点了清空之后框里还是 %q", s.searchKeyword)
	}
	if _, open := tt.Find(suggestPanelLabel); open || s.searchSuggest {
		t.Errorf("点了清空之后面板还开着（searchSuggest=%v）——清空键夺走焦点，失焦那条该把它收掉",
			s.searchSuggest)
	}
}
