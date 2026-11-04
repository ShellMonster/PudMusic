package ui

// 迷你播放器的**版面**和**元素身份**这两组（原型 `.mini`，DESIGN.md §0′.T5）。
//
// 窗口是一张 380 宽、收起时 194 高的卡片：1 像素描边 + 拖动条 44 + 主区 120（封面 72、
// 歌名 24 + 歌手 20 + 控制行 8 + 36）+ 进度行 16 + 底内边距 12 + 1 像素描边。
// 这一组量的是**这笔账**：封面落在哪儿、三个控制键各占多宽、两端的时间和进度条怎么分。
// 数全部**写死字面量**（取自原型 CSS 与参考图实测），不去引用 `miniplayer.go` 里的常量——
// 两边用同一个常量的话，常量一改、期望跟着改，测试照样全绿。
//
// 另一半（窗口开关、控制、两个窗口共享状态）在 `miniplayer_test.go`。

import (
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 版面
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniWindowIsTheCardSize 盯「窗口就是原型那张卡片」：380 × 194。
//
// 194 是原型各段高度加起来的数（见文件头）；它同时是卷帘收起时的窗高，
// `StateKey` 落盘的也是它。
func TestTheMiniWindowIsTheCardSize(t *testing.T) {
	if miniWidth != 380 {
		t.Errorf("迷你窗宽 %d，want 380（原型 `.mini { width: 380px }`）", miniWidth)
	}
	if want := 1 + 44 + 120 + 16 + 12 + 1; miniHeight != want || miniHeight != 194 {
		t.Errorf("迷你窗收起时高 %d，want %d（描边 1 + 拖动条 44 + 主区 120 + 进度 16 + 底 12 + 描边 1）",
			miniHeight, want)
	}
}

// TestTheMiniCoverIsSeventyTwoAtTheCardPadding 盯封面：72×72、圆角 8，落在
// 卡片内边距之后（x = 描边 1 + 内边距 16 = 17）；竖直方向在主区那一行里居中
// （主区内容 88 高，封面 72，所以比内容顶低 8：y = 1 + 44 + 16 + 8 = 69）。
//
// ⚠️ **判据是像素而不是控件的尺寸**：封面那一格是 `ui.Box`，没有可及名字，
// `Tester.Find` 找不到它；而「那一格被画成了多大、摆在哪儿」正是要钉的东西。
// 封面夹具是一张纯色图（`coverColor`），一扫就知道它画在哪儿。
//
// ⚠️ **等的是「图真的画上来了」**：图是异步来的，而等待条件必须是「画面上出现
// 了那个颜色」——`miniCoverBox` 返回空框就是还没到。
func TestTheMiniCoverIsSeventyTwoAtTheCardPadding(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：播放键上写的是「播放」，而播放器的钟别来搅和
	tt := miniTester(s)

	waitFor(t, "封面画上来了", func() bool {
		tt.Frame()
		return !miniCoverBox(tt).Empty()
	})
	box := miniCoverBox(tt)
	// 封面图外面有一圈 1 像素的边（`imgcache.ViewTitled` 画的，深色下近黑的封面也要有边），
	// 那一圈画在格子里面——格子仍是 72×72，图本身让出 2 像素。
	const frame = 1
	if box.Dx() != 72-2*frame || box.Dy() != 72-2*frame {
		t.Errorf("封面是 %dx%d，want %dx%d（72 的格子减去那一圈 1 像素的边，原型 `.mini-cover`），画在 %v",
			box.Dx(), box.Dy(), 72-2*frame, 72-2*frame, box)
	}
	if box.Min.X != 17+frame || box.Min.Y != 69+frame {
		t.Errorf("封面的左上角在 (%d,%d)，want (%d,%d)（描边 1 + 内边距 16；主区内居中；再让出那圈边）",
			box.Min.X, box.Min.Y, 17+frame, 69+frame)
	}
	// 右边那一列从封面右缘 + 16 起：x = 17 + 72 + 16 = 105；上一首键就是它的第一格。
	prev, ok := tt.Find("上一首")
	if !ok {
		t.Fatalf("画面上找不到「上一首」，文字有 %q", tt.Texts())
	}
	if prev.X != 105 {
		t.Errorf("右边那一列从 x=%.1f 起，want 105（封面右缘 89 + 列间距 16）", prev.X)
	}
}

// TestTheMiniControlsFollowThePrototypeGrid 盯控制行和进度行的格子：
//
//   - 上一首 / 播放 / 下一首各 36×36、间距 4：x = 105 / 145 / 185，y = 113
//     （主区顶 61 + 歌名 24 + 歌手 20 + 上边距 8）；
//   - 喜欢、歌词靠右，右缘在 380 − 1 − 16 = 363；
//   - 进度行：两端时间各 40 宽，进度条从 17 + 40 + 12 = 69 起，到 363 − 40 − 12 = 311 止。
//
// 播放键是 36 的实心圆（正文色底），而不是 44（播放条那一枚）。
func TestTheMiniControlsFollowThePrototypeGrid(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	tt := miniTester(s)
	tt.Frame()

	for _, want := range []struct {
		label string
		x, y  float32
	}{
		{"上一首", 105, 113},
		{"播放", 145, 113},
		{"下一首", 185, 113},
	} {
		r, ok := tt.Find(want.label)
		if !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", want.label, tt.Texts())
		}
		if r.W != 36 || r.H != 36 || r.X != want.x || r.Y != want.y {
			t.Errorf("%q 的框是 (%.1f,%.1f %.1f×%.1f)，want (%.0f,%.0f 36×36)",
				want.label, r.X, r.Y, r.W, r.H, want.x, want.y)
		}
	}

	// 歌词键是控制行最右一格：右缘 363。
	lyr, ok := tt.Find("歌词")
	if !ok {
		t.Fatalf("画面上找不到「歌词」键，文字有 %q", tt.Texts())
	}
	if lyr.X+lyr.W != 363 || lyr.W != 36 {
		t.Errorf("「歌词」键的框是 (%.1f..%.1f)，want 右缘 363、宽 36", lyr.X, lyr.X+lyr.W)
	}

	// 进度条：从 69 到 311，高 16。
	bar := progressRect(t, tt)
	if bar.X != 69 || bar.X+bar.W != 311 || bar.H != 16 {
		t.Errorf("进度条是 x=%.1f..%.1f 高 %.1f，want 69..311 高 16（两端时间各 40 + 间距 12）",
			bar.X, bar.X+bar.W, bar.H)
	}

	// 回到主窗口的键在拖动条右端：右缘 380 − 1 − 8 = 371，36×36，顶 1 + 8 = 9。
	back, ok := tt.Find("切换mini/完整模式")
	if !ok {
		t.Fatalf("画面上找不到「切换mini/完整模式」，文字有 %q", tt.Texts())
	}
	if back.W != 36 || back.H != 36 || back.X+back.W != 371 || back.Y != 9 {
		t.Errorf("回到主窗口的键是 (%.1f,%.1f %.1f×%.1f)，want 右缘 371、顶 9、36×36",
			back.X, back.Y, back.W, back.H)
	}
}

// TestTheMiniCardKeepsEverythingInsideTheWindow 盯「没有任何控件掉出窗口、也没有
// 两个控件互相压着」（命中链只认最上面那个，被压住的那个永远按不到，而 `Find` 照样
// 找得到它的名字）。
func TestTheMiniCardKeepsEverythingInsideTheWindow(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	tt := miniTester(s)
	tt.Frame()

	w, h := float32(miniWidth), float32(miniHeight)
	names := []string{"上一首", "播放", "下一首", "切换mini/完整模式", "播放进度", "歌词"}
	rects := make(map[string]mygoui.Rect, len(names))
	for _, n := range names {
		r, ok := tt.Find(n)
		if !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", n, tt.Texts())
		}
		if r.X < 0 || r.Y < 0 || r.X+r.W > w+0.5 || r.Y+r.H > h+0.5 {
			t.Errorf("%q 的框 (%.1f,%.1f %.1f×%.1f) 掉到 %d×%d 外面去了",
				n, r.X, r.Y, r.W, r.H, miniWidth, miniHeight)
		}
		rects[n] = r
	}
	for i, a1 := range names {
		for _, b1 := range names[i+1:] {
			if overlap(rects[a1], rects[b1]) {
				t.Errorf("%q 和 %q 的框重叠了（%v / %v）", a1, b1, rects[a1], rects[b1])
			}
		}
	}
}

// TestTheMiniKeysAreIcons 盯那几个键**真的是图标键**：框就是图标键的框（36×36），
// 而且里面真的画了图形（`scanGlyph` 的墨迹不为空）——一个空的图标键点得到、也叫得出
// 名字，但画面上什么都没有。
//
// ⚠️ **「按名字找得到」本身就是「有可及名字」这一条的证据**：MyGo 的
// `Tester.Find` 找的是「有文字**或有可及名字**」的元素，而图标键画不出字——找得到，
// 说明那个名字确实挂在按钮上（图标对辅助技术不可见，`mygo/ui/svg.go` 的 Icon 文档：
// 「unless Label names them」）。
func TestTheMiniKeysAreIcons(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：播放键上写的是「播放」
	tt := miniTester(s)
	tt.Frame()

	for _, want := range []struct {
		label string
		size  float32
	}{
		{"上一首", 36},
		{"播放", 36},
		{"下一首", 36},
		{"切换mini/完整模式", 36},
		{"歌词", 36},
	} {
		r, ok := tt.Find(want.label)
		if !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", want.label, tt.Texts())
		}
		// `Tester.Find` 报的是**画出来的那一块**（内部和父框的裁剪区求交），
		// 所以「被挤扁了」在这里表现为框变小。
		if r.W < want.size-0.5 || r.H < want.size-0.5 {
			t.Errorf("%q 的键是 %.1f×%.1f，want 至少 %.0f×%.0f：它不是图标键的框",
				want.label, r.W, r.H, want.size, want.size)
		}
		if s := scanGlyph(tt, want.label); s.ink == "" {
			t.Errorf("%q 那一格里没画图形（只有底色 %v）——图标键上什么都没有，"+
				"名字还在、按得动，可用户看不见它", want.label, s.bg)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 元素身份（铁律四）
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniControlsKeepTheirIdentityWhenTheLabelChanges 盯铁律四在这个窗口上。
//
// 默认的元素身份是「父 ID + 兄弟序号」，而迷你窗口上**播放和暂停是两个不同的按钮**
// （`st.Playing` 分支）：字在两个字之间换来换去，元素也跟着换一个。没有稳定 key 的
// 情况下，这个键每换一次字就换一个新身份，它的按下/悬停状态跟着重来一次——**界面上看
// 不出坏了**，只是按下去偶尔要按两下。
//
// ⚠️ **key 必须按角色给（`mini-play`），不能按按钮上的字给**：按字给的话每换一次
// 字 key 就变，等于每一帧都是新元素——那就等于没给。
//
// 断法：按住这个键、跨过「播放 ↔ 暂停」那一次切换之后松手，它必须**仍然**把播放/
// 暂停切回去。按住的是一个身份、松手落在另一个身份上时，这一次按压就白按了。
func TestTheMiniControlsKeepTheirIdentityWhenTheLabelChanges(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	tt := miniTester(s)
	// `enqueueTwo` 让第一首开始放，所以此刻键上写的是「暂停」。
	if !tt.HasText("暂停") {
		t.Fatalf("起点就不对：正在放而键上不是「暂停」，文字有 %q", tt.Texts())
	}

	// 先把它按下去。
	btn, ok := tt.Find("暂停")
	if !ok {
		t.Fatalf("画面上找不到「暂停」，文字有 %q", tt.Texts())
	}
	cx, cy := btn.X+btn.W/2, btn.Y+btn.H/2
	tt.Press(cx, cy)

	// 按住的这几帧里播放状态变了：按钮上的字从「暂停」变成「播放」。
	a.Toggle() // 暂停
	tt.Frame()
	if !tt.HasText("播放") {
		t.Fatalf("按住的时候按钮没有换成「播放」，文字有 %q", tt.Texts())
	}

	// 同一个位置松手：必须落在同一个键上，于是**又切回去**（重新开始放）。
	tt.Release(cx, cy)
	tt.Frame()
	if st := a.Snapshot(); !st.Playing {
		t.Error("松手之后还是暂停的：按住和松手落在两个不同的元素上了——这一帧的" +
			"元素身份跟着按钮上的字变了（铁律四：key 要按角色给）")
	}
}
