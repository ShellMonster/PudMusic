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
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 版面
// ─────────────────────────────────────────────────────────────────────────────

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
// ⚠️ **一定要带封面跑这一条**：封面在的时候那一列只有 244 宽（320 - 64 - 12），
// 没有封面时是 308——宽了 64 的那一档量不到真正紧的那一档。
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
	// 封面列 64 + 它右边那一列 6 的内边距 = 70。没有封面时进度条从 6 开始。
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
func TestTheMiniKeysAreIcons(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：播放键上写的是「播放」
	tt := miniTester(s)
	progressRect(t, tt) // 同上：封面在，才是最挤的那一档

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
		// `mygo/ui/runtime.go:974`），所以「被挤扁了」在这里表现为框变小。
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
