package ui

// 迷你播放器的**版面**和**元素身份**这两组。
//
// 320×100 是写死的（SPEC 需求 43 抄 Biu 的数字），而这一票往里塞了封面、歌名、
// 音量、退出、进度、上一首 / 播放 / 下一首。装不下的时候症状特别隐蔽：控件**还在**
//（无头 Tester 读的是控件的文本，被裁的只是画出来的那几个字），而版面引擎会把挤不
// 下的控件按比例缩掉、**一个都不掉出窗口**。
//
// 所以这一组量的不是「有没有掉出去」，而是那笔**账**：右边那一列有多宽、控件各自
// 要多少、以及画出来的那几个字有没有被裁。
//
// 另一半（窗口开关、控制、两个窗口共享状态）在 `miniplayer_test.go`。

import (
	"math"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 版面
// ─────────────────────────────────────────────────────────────────────────────

// biuMiniCoverCol 是 Biu 那边封面那一列的宽：`h-full w-[100px]`
// （`pages/mini-player/index.tsx:28`）。
//
// ⚠️ **写死字面量，不去用 `miniplayer.go` 里的 `miniCoverCol`**：两边都用同一个
// 常量的话，常量一改、期望跟着改，测试照样全绿——而「封面列 100」正是这一票
// （#168 / G10）要钉死的那一个数。`playbar_cover_test.go` 里那几个 `biu*`
// 常量是同一个道理。
const biuMiniCoverCol = 100

// TestTheMiniCoverColumnIsBiuWidth 盯「封面列 100」（票 #168 / G10）。
//
// 断的是**画面上画出来的那张封面**：88×88、落在 (6,6)——于是封面列就是
// 6（卡片内边距）+ 88 + 6 = 100，而右边那一列从 x=106 起（进度条量出来的）。
//
// ⚠️ **判据是像素而不是控件的尺寸**：封面那一格是 `ui.Box`，没有可及名字，
// `Tester.Find` 找不到它；而「那一格被画成了多大、摆在哪儿」正是这一票要钉的
// 东西。封面夹具是一张纯色图（`coverColor`），一扫就知道它画在哪儿。
//
// ⚠️ **等的是「图真的画上来了」**：图是异步来的，而等待条件必须是「画面上出现
// 了那个颜色」——`miniCoverBox` 返回空框就是还没到。
func TestTheMiniCoverColumnIsBiuWidth(t *testing.T) {
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

	// 封面本身：100 − 左右各 6 = 88 的方图，落在卡片内边距之后。
	//
	// ⚠️ **画面上量到的是「图」，不是「那一格」**：那一格外面还有一圈 1 像素的
	// 边（票 #244 / UX 报告 §2 的 E2：深色下一张近黑的封面和行的底色几乎一样，
	// 「图取到了」那一档也要描一圈边），而**那一圈边是画在格子里面那一圈**——
	// 所以格子还是 88×88、位置一点没动，图本身让出来 2 像素。
	const frame = 1
	if box.Dx() != 88-2*frame || box.Dy() != 88-2*frame {
		t.Errorf("封面是 %dx%d，want %dx%d（封面列 %d 减去左右各 %d 的内边距，"+
			"再减去那一圈 1 像素的边），画在 %v",
			box.Dx(), box.Dy(), 88-2*frame, 88-2*frame, biuMiniCoverCol, miniPad, box)
	}
	if box.Min.X != miniPad+frame || box.Min.Y != miniPad+frame {
		t.Errorf("封面的左上角在 (%d,%d)，want (%d,%d)：它没有贴着卡片内边距（"+
			"那一圈 1 像素的边算在里面）",
			box.Min.X, box.Min.Y, miniPad+frame, miniPad+frame)
	}
	// 封面列 = 左边 6 + 封面 88 + 右边 6 = 100。
	//
	// ⚠️ **算的是「格子」**：格子的左边是图再往左 1 像素（那一圈边），右边也是。
	if got, want := box.Min.X-frame+box.Dx()+2*frame+miniPad, biuMiniCoverCol; got != want {
		t.Errorf("封面那一列是 %d 宽，want %d（Biu 的 `w-[100px]`）", got, want)
	}
	// 右边那一列从 106 起（封面列 100 + 那一列自己的 6 的内边距）。
	if got, want := progressRect(t, tt).X, float32(biuMiniCoverCol+miniPad); got != want {
		t.Errorf("进度条从 x=%.1f 开始，want %.1f：封面列不是 %d 宽",
			got, want, biuMiniCoverCol)
	}
}

// TestTheMiniVolumeSliderSurvivesTheBiggerCover 盯「封面变大之后音量还在，而且
// 没被挤瘦」。
//
// 票 #168 的一条规定是**音量保留**（上游的迷你播放器没有音量，是用户定的）。
// 封面那一列从 64 涨到 100，右边那一列就窄了 36——而那一行摆的是
// 「歌名 + 音量 + 退出」，挤不下了的话**版面引擎会把控件按比例缩掉**：
// 音量的滑槽悄悄短一截，而**界面上看不出坏了**（它还在那儿，只是不那么满了）。
//
// 所以断两样：音量**还在**（找得到它），而且**还是 80 宽**（没被缩）。
func TestTheMiniVolumeSliderSurvivesTheBiggerCover(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	tt := miniTester(s)

	bar, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("画面上找不到音量条，文字有 %q", tt.Texts())
	}
	// 80 是我们自己定的那个数（上游没有音量，抄不到），写死字面量：拿常量去比
	// 常量，常量一改两边一起变，这条就白钉了。
	const wantW = 80
	if bar.W != wantW {
		t.Errorf("音量条是 %.1f 宽，want %d：它被挤瘦了（封面列占了 100 之后，"+
			"右边那一列只剩 208，而版面引擎会把挤不下的控件按比例缩掉）", bar.W, wantW)
	}
}

// TestTheMiniCardFitsInThreeHundredAndTwentyByOneHundred 盯「320×100 装得下」。
//
// 窗口尺寸是写死的 320×100（Biu 的数字），而这一票在封面右边那一列里放了标题 +
// 音量 + 退出、进度、上一首 + 播放 + 下一首。装不下的症状很隐蔽：控件**还在**
// （无头 Tester 读的是控件的文本，被裁的只是画出来的那几个字），而版面引擎会把挤
// 不下的控件按比例缩掉、**一个都不掉出窗口**——所以「有没有掉出去」「有没有互相
// 压着」这两条断言自己量不出来。
//
// 量出来的那条是**账**：右边那一列有多宽（从那条 `Grow(1)` 的进度条上量，它铺满
// 这一列的内容框），而两行控件各自要多少。列一变窄账就对不上，而对不上的后果是
// 「上一首」被缩成「上一…」。
//
// ⚠️ **一定要带封面跑这一条**：封面在的时候那一列只有 208 宽（320 - 100 - 12），
// 没有封面时是 308——宽了 100 的那一档量不到真正紧的那一档。
func TestTheMiniCardFitsInThreeHundredAndTwentyByOneHundred(t *testing.T) {
	const gap = 6 // 两行都是 `Gap(6)`

	s, a := newMiniShell(t, nil)
	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：播放键上写的是「播放」而不是「暂停」
	tt := miniTester(s)
	progress := progressRect(t, tt)

	// 封面列在不在，是这一条量的是不是「紧的那一档」的分水岭：
	// 封面列 100 + 它右边那一列 6 的内边距 = 106。没有封面时进度条从 6 开始。
	if got, want := progress.X, float32(miniCoverCol+miniPad); got != want {
		t.Fatalf("封面没有让出左边那一列：进度条从 x=%.1f 开始（want %.1f），"+
			"这一条量到的不是最挤的那一档", got, want)
	}

	// 账一：控制那一行。三个键 + 两道缝。
	transport := 2*miniStepW + miniPlayW + 2*gap
	// 账二：标题那一行。歌名有下限（`MinWidth`，再窄就一个字都不剩）、音量、退出。
	titleRow := miniTitleMinW + miniVolumeW + miniBackBtnW + 2*gap
	for _, b := range []struct {
		what string
		need float32
	}{{"上一首 + 播放 + 下一首", float32(transport)}, {"歌名 + 音量 + 退出", float32(titleRow)}} {
		if progress.W+0.5 < b.need {
			t.Errorf("%s 要 %.0f 宽，而右边那一列只有 %.0f：版面引擎会把挤不下的控件"+
				"按比例缩掉（控件还在、文本也还在，只是画出来的那几个字被裁了）",
				b.what, b.need, progress.W)
		}
	}

	// 另外两条：都掉到窗口外面去了，或者互相压着（命中链只认最上面那个，被压住的
	// 那个永远按不到，而 Find 照样找得到它的名字）。
	w, h := float32(miniWidth), float32(miniHeight)
	names := []string{"上一首", "播放", "下一首", "退出", "音量", "播放进度"}
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
				t.Errorf("%q 和 %q 的框重叠了（%v / %v）：命中链只认最上面那个，"+
					"被压住的那个永远按不到，而 Find 照样找得到它的名字",
					a1, b1, rects[a1], rects[b1])
			}
		}
	}
}

// TestTheMiniKeysAreIcons 盯那四个键**真的是图标键**。
//
// 这条原来是 `TestTheMiniLabelsAreNotTruncated`：那时候四个键是写着「上一首」的
// 文字键，判据是「画出来的框不窄于那几个字的自然宽」。这一票把它们换成了图标键
// （上游 `pages/mini-player/index.tsx:165-212` 那一排），于是那个判据整条都不再
// 成立——现在要断的是反过来的一件事：**框就是图标键的框，而且里面真的画了图形**。
//
// 两条一起断，缺一不可：
//
//   - 框的宽高**等于** `miniStepW` / `miniPlayW`（不是「不大于」）：文字键那会儿
//     这两个数是 64 / 72，谁把它们改回去了，这里立刻红。
//   - 框里有一枚图形（`scanGlyph` 的墨迹不为空）：一个空的图标键点得到、也叫得出
//     名字，但画面上什么都没有。
//
// ⚠️ **「按名字找得到」本身就是「有可及名字」这一条的证据**：MyGo 的
// `Tester.Find` 找的是「有文字**或有可及名字**」的元素（`mygo/ui/runtime.go:1065`
// 那一段），而图标键画不出字——找得到，说明那个名字确实挂在按钮上（图标对辅助
// 技术不可见，`mygo/ui/svg.go` 的 Icon 文档：「unless Label names them」）。
func TestTheMiniKeysAreIcons(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：播放键上写的是「播放」
	tt := miniTester(s)
	progress := progressRect(t, tt) // 同上：封面在，才是最挤的那一档

	for _, want := range []struct {
		label string
		size  float32
	}{
		{"上一首", miniStepW},
		{"播放", miniPlayW},
		{"下一首", miniStepW},
		{"退出", miniBackBtnW},
	} {
		r, ok := tt.Find(want.label)
		if !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", want.label, tt.Texts())
		}
		// `Tester.Find` 报的是**画出来的那一块**（内部和父框的裁剪区求交，
		// `mygo/ui/runtime.go:1066`），所以「被挤扁了」在这里表现为框变小。
		if r.W < want.size-0.5 || r.H < want.size-0.5 {
			t.Errorf("%q 的键是 %.1f×%.1f，want 至少 %.0f×%.0f：它不是图标键的框",
				want.label, r.W, r.H, want.size, want.size)
		}
		if s := scanGlyph(tt, want.label); s.ink == "" {
			t.Errorf("%q 那一格里没画图形（只有底色 %v）——图标键上什么都没有，"+
				"名字还在、按得动，可用户看不见它", want.label, s.bg)
		}
	}

	// 居中也在这里断：三个键一共 100 宽，而这一列有 208——左对齐的话右边空
	// 108 像素，**播放键落在窗口左侧**，而它头顶那条进度条横跨整列
	// （见 `viewMiniTransport` 的注释）。播放键的中点得在进度条中点底下。
	play, ok := tt.Find("播放")
	if !ok {
		t.Fatalf("画面上找不到「播放」，文字有 %q", tt.Texts())
	}
	if d := math.Abs(float64(play.X+play.W/2) - float64(progress.X+progress.W/2)); d > 2 {
		t.Errorf("播放键的中点在 x=%.1f，而进度条的中点在 x=%.1f（差 %.1f）——"+
			"三个键没居中，左边挤成一团、右边空着",
			play.X+play.W/2, progress.X+progress.W/2, d)
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
