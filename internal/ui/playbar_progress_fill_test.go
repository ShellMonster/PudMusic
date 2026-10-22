// 播放条进度条「填过的那一段」的颜色（GitHub #218）。
//
// 这一族断的是**那一段的颜色**：平时是前景色，悬停/按下才变主色。
//
// # 这一条是**读代码**读出来的，不是量出来的
//
// 上游 `music-play-progress/index.tsx:38`：
// `color={showThumb ? "primary" : "foreground"}`——**不悬停是前景色**（深色下
// #ecedee、浅色下近黑），**悬停才主色绿**。我们原来**永远**是主色，于是用户
// 一进来就看见一条绿杠，上游那边是白杠。
//
// ⚠️ **量的时候要「播放到一半」**：位置是 0 的时候填过的那一段只有**半个旋钮**
// 那么宽（8 像素），而且整个躲在旋钮底下——量它量到的其实是旋钮，不是那一段。
//
// ⚠️ **判据是逐像素的颜色**，不是某个布尔：颜色不对的时候界面上「只是不太对」
// （一条绿杠 vs 一条白杠），不会报错、不会崩、不会打日志。
package ui

import (
	"os"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// TestTheFilledPartOfTheProgressIsForegroundUntilHovered 断「填过的那一段」平时是
// **前景色**、悬停才变**主色**。
//
// 🔴 **两档都要断，而且要断「另一档一个像素都没有」**：只断「前景色在」的话，
// 「主色也叠在上面」这种坏法照样过——而那正是改之前的样子（永远主色）。
func TestTheFilledPartOfTheProgressIsForegroundUntilHovered(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			st := barState()
			// 播到 40%：填过的那一段有宽度（位置 0 时它只有半个旋钮那么宽，
			// 而且整个躲在旋钮底下——见文件头）。
			st.Position = 40 * time.Second
			tt, s := playbarTester(t, st, 1200, 0, dark)
			// ⚠️ **再画一帧**：旋钮的位置是**上一帧**的值算出来的（`viewProgress`
			// 是在滑块建好之后才把 `s.scrub` 写成当前位置的），所以头一帧画出来
			// 的还是 0%。
			tt.Frame()

			bar, ok := tt.Find("播放进度")
			if !ok {
				t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
			}
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			foreground := rgbaOf(th.Text)
			accent := rgbaOf(th.Accent)
			spot := filledSpot(bar, 0.4)

			// 不悬停：前景色。
			if n := nearPixels(tt.Image(), spot, foreground); n == 0 {
				t.Errorf("不悬停的时候填过的那一段 %v 一个前景色像素都没有——"+
					"它该是前景色（上游 `color={showThumb ? \"primary\" : \"foreground\"}`）",
					spot)
			}
			if n := nearPixels(tt.Image(), spot, accent); n != 0 {
				t.Errorf("不悬停的时候填过的那一段 %v 有 %d 个接近主色的像素——"+
					"主色是**悬停**才上的（上游那边不悬停是一条白杠）", spot, n)
			}

			// 悬停：主色。
			tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
			// ⚠️ **要推钟**：前景色→主色是 150 毫秒的一段动画（跟「按住」那一条
			// 同一个时长），而无头 Tester 的钟站着不动——不推钟它永远走不完
			// （CLAUDE.md §2）。
			tt.Advance(300 * time.Millisecond)
			tt.Frame()

			if n := nearPixels(tt.Image(), spot, accent); n == 0 {
				t.Errorf("悬停之后填过的那一段 %v 一个接近主色的像素都没有——"+
					"它该变主色（上游 `showThumb` 那一档就是主色）", spot)
			}
			if n := nearPixels(tt.Image(), spot, foreground); n != 0 {
				t.Errorf("悬停之后填过的那一段 %v 还有 %d 个接近前景色的像素——"+
					"它该整个换成主色", spot, n)
			}
		})
	}
}

// TestRenderProgressFillShots 存图：填过的那一段在两档颜色下的样子。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderProgressFillShots
//
// ⚠️ **深浅两套 × 悬停/不悬停，四张都要出**：这一票改的是**颜色**，而颜色是逐
// 像素的东西——「深色下的前景色」和「浅色下的前景色」差得很远（#f4f4f5 对
// #18181b），只出一套看不出问题。
//
// ⚠️ **位置要停在 40%**：位置 0 的时候填过的那一段只有半个旋钮那么宽，而且整个
// 躲在旋钮底下（见文件头）——图上看不出「填过的那一段」是什么颜色。
func TestRenderProgressFillShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		st := barState()
		st.Position = 40 * time.Second

		// 不悬停：那一段是**前景色**（上游那边是白杠）。
		tt, _ := playbarTester(t, st, 1200, playbarHeight, dark)
		tt.Frame()
		save(t, dir, "218-进度条-填过的那一段-不悬停-"+name+".png", tt)

		// 悬停：那一段变**主色**，旋钮也画出来。
		tt, _ = playbarTester(t, st, 1200, playbarHeight, dark)
		tt.Frame()
		bar, ok := tt.Find("播放进度")
		if !ok {
			t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
		}
		tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
		// 颜色是 150 毫秒的一段动画：推钟等它走完（CLAUDE.md §2）。
		tt.Advance(300 * time.Millisecond)
		tt.Frame()
		save(t, dir, "218-进度条-填过的那一段-悬停-"+name+".png", tt)
	}
}

// filledSpot 是「填过的那一段」里的一块（采样颜色的地方）。
//
// ⚠️ **位置是算出来的，不是量出来的**：填过的那一段从**轨道的左沿**到**旋钮的
// 中心**（`knobslider.go` 的 `p.Fill(…, cx-track.X, …)`），而旋钮的中心是
// 「左边沿 + 半个旋钮 + (宽 − 旋钮宽) × 进度」。
//
// 取的是**这一段的中点那一小块**：离左沿（圆头）和右沿（旋钮）都远，量到的
// 就是这一段自己的颜色，而不是旋钮或者轨道。
func filledSpot(bar mygoui.Rect, frac float32) mygoui.Rect {
	kw := knobHoverLook.knobW
	cx := bar.X + kw/2 + (bar.W-kw)*frac
	mid := bar.X + (cx-bar.X)/2
	cy := bar.Y + bar.H/2
	return mygoui.Rect{
		X: mid - 4,
		Y: cy - knobHoverLook.trackW/2,
		W: 8,
		H: knobHoverLook.trackW,
	}
}
