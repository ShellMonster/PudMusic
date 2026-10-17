package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 播放条上的缓冲态。
//
// 断网之后用户看到的就是这几像素：**有话说的那一行**。少画了它，界面上
// 的现象是「歌停在原地、不报错、进度条不动」——那和卡死没有任何区别，
// 用户既不知道该不该重试，也等不到一句交代。

// TestBufferingShowsInTheSecondRow 盯缓冲时有一句话。
func TestBufferingShowsInTheSecondRow(t *testing.T) {
	tt := qualityBarTester(t, app.State{
		HasTrack:  true,
		Title:     "会断网的一首",
		Quality:   "320K",
		Buffering: true,
	})
	if !tt.HasText(bufferingText) {
		t.Errorf("缓冲了却没有那句话，画面上的字：%q", tt.Texts())
	}
	if !tt.HasText("会断网的一首") {
		t.Errorf("缓冲时歌名不见了：%q", tt.Texts())
	}
}

// TestBufferingIsNotSaidAsAFailure 盯缓冲**不是**一句「打不开」。
//
// 用错误那一行的措辞去说「正在恢复」，用户看到的第一反应是「歌坏了」，
// 于是去清缓存重下——而正确的反应是等一等。这条断的是**措辞**：
// 无头 tester 拿不到文字颜色（`Tester` 没有这个接口），而措辞是用户
// 直接读到的，颜色只是辅助。
func TestBufferingIsNotSaidAsAFailure(t *testing.T) {
	tt := qualityBarTester(t, app.State{
		HasTrack: true, Title: "会断网的一首", Buffering: true,
	})
	for _, s := range tt.Texts() {
		if strings.HasPrefix(s, "打不开") {
			t.Errorf("缓冲被说成了「打不开」：%q", s)
		}
	}
	if tt.HasText("打不开：" + bufferingText) {
		t.Errorf("缓冲那句话被套上了「打不开：」：%q", tt.Texts())
	}
}

// TestTheErrorWinsOverBuffering 盯两者同时成立时说错误。
//
// 错误是终态、缓冲是过程。缓冲的意思是「网络马上可能就回来了」，
// 而错误的意思是「这次不行了」——用户该先看到后者。
func TestTheErrorWinsOverBuffering(t *testing.T) {
	tt := qualityBarTester(t, app.State{
		HasTrack:  true,
		Title:     "放不了的一首",
		Buffering: true,
		Err:       "连不上 B 站这条音频流了",
	})
	if !tt.HasText("打不开：连不上 B 站这条音频流了") {
		t.Errorf("错误那句话没画出来：%q", tt.Texts())
	}
	if tt.HasText(bufferingText) {
		t.Errorf("已经报错了还画着「缓冲中……」：%q", tt.Texts())
	}
}

// TestNoBufferingLineWhenNothingIsWrong 盯没事的时候不画。
//
// 一句永远挂在那里的「缓冲中……」比没有更糟：它会让用户以为每首歌都在卡着。
func TestNoBufferingLineWhenNothingIsWrong(t *testing.T) {
	tt := qualityBarTester(t, app.State{HasTrack: true, Title: "好好放着的一首"})
	if tt.HasText(bufferingText) {
		t.Errorf("好好放着却写着「缓冲中……」：%q", tt.Texts())
	}
}

// TestTheBufferingLineSitsUnderTheTitle 盯缓冲那句话的位置和错误那句一样。
//
// 沿用 `TestTheErrorSitsUnderTheTitle` 的判据：断的是两个 Rect 的**关系**，
// 不是「字在不在」——被挤成竖条、被 88 高的播放条裁掉的字**照样画了**，
// 那正是上一票把 Column 拆成裸 Row 时绿着的那个洞。
func TestTheBufferingLineSitsUnderTheTitle(t *testing.T) {
	for _, width := range []int{760, 900, 1000, 1200, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt := qualityBarTester(t, app.State{
				HasTrack:  true,
				Title:     "这一首歌的名字",
				Quality:   "无损 FLAC",
				Buffering: true,
			}, width)

			title, okT := tt.Find("这一首歌的名字")
			note, okN := tt.Find(bufferingText)
			if !okT || !okN {
				t.Fatalf("宽度 %d：歌名在不在 %v，缓冲那句在不在 %v，画面的字：%q",
					width, okT, okN, tt.Texts())
			}
			if note.Y < title.Y+title.H {
				t.Errorf("宽度 %d：缓冲那句 {%.1f,%.1f %.1fx%.1f} 没有排在歌名 "+
					"{%.1f,%.1f %.1fx%.1f} 的下面——歌名会被挤成竖条", width,
					note.X, note.Y, note.W, note.H, title.X, title.Y, title.W, title.H)
			}
			if title.H > playbarHeight || note.Y+note.H > playbarHeight {
				t.Errorf("宽度 %d：内容被 88 高的播放条裁了（歌名 %.1f 高，"+
					"缓冲那句下边在 y=%.1f）", width, title.H, note.Y+note.H)
			}
			if title.W < 40 {
				t.Errorf("宽度 %d：歌名只有 %.1f 宽（%.1f 高），已经被压成竖条了",
					width, title.W, title.H)
			}
		})
	}
}

// TestOpeningShowsTheSameLineAsBuffering 盯「正在打开」也要有话说。
//
// 用户点了一首在线流、取流接口回来了、CDN 的字节下不来——这整段里歌没响。
// 界面上必须有一句话，而且和「播放中断网」是**同一句**：用户看到的现象
// 一样，说法就该一样，否则他要学两套。
//
// ⚠️ 这一段曾经**三样全空**：没有歌名（播放器还没建好）、没有错误（没失败）、
// 没有缓冲态（`a.online` 还没装上，读不到）。而本票把源的下限从
// 「换几次流就报错」放宽成「最多等一分钟」，顺手把那段空窗拉成了一分钟。
func TestOpeningShowsTheSameLineAsBuffering(t *testing.T) {
	tt := qualityBarTester(t, app.State{
		HasTrack:  true,
		Title:     "下不来的那一首",
		Opening:   true,
		Buffering: true,
	})
	if !tt.HasText(bufferingText) {
		t.Errorf("正在打开却没有那句话，画面上的字：%q", tt.Texts())
	}
	if !tt.HasText("下不来的那一首") {
		t.Errorf("正在打开时歌名不见了：%q", tt.Texts())
	}
	// 歌还没响，所以是「播放」而不是「暂停」——这一刻按下去是空的，
	// 按钮被 Disabled(Opening) 按灰了（无头 tester 读不到灰不灰，
	// 但它必须还在画着，不能因为灰了就整个消失）。
	if !tt.HasText("播放") {
		t.Errorf("正在打开时播放按钮不见了：%q", tt.Texts())
	}
}
