package ui

// 频谱画成柱状图（票 #374）的出图：深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket374Shots
//
// # 这一族要看的是「一根一根的柱」
//
// 柱子之间留着两成的缝，而每一根的高度是它自己那一格的值——整幅图上看得见
// 「左边几根高、右边几根矮」，而不是一条上下起伏的线。
//
// ⚠️ **深浅两套都要出**：柱子的颜色是 `#666`，而满屏那一块的底色跟着主题走——
// 浅色下底色浅、柱子深，深色下底色深、柱子浅。只看一套的话，「柱子有没有立
// 起来」这件事看不出来。
//
// ⚠️ **出图本身不是判据**：频谱每一帧都在变（那正是它在动），所以两次出图
// 不一样是正常的。判据是 `fullplayer_spectrum_test.go` 里那几条（它们量的是
// 像素），出图是给人看的。

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderTicket374Shots 存图：满屏那一块频谱，深浅各一套。
func TestRenderTicket374Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt, _ := fullTester(t)
			// ⚠️ `fullTester` 固定深色（它那一族量的是「画面上接近白的像素」），
			// 这里要两套，所以自己翻回去——主题是**每帧**现读的（`applyFrameTheme`）。
			tt.SetDark(dark)
			s.app.SetFullPlayerPrefs(app.FullPlayerPrefs{ShowSpectrum: true})
			openFull(t, tt)
			// 把开场那段共享元素转场走完（假时钟推 1.5 秒）。
			for range 5 {
				tt.Advance(300 * time.Millisecond)
			}
			waitSpectrumData(t, s, tt)

			save(t, dir, "01-满屏播放器-频谱-"+name+".png", tt)
			// 只存频谱那一块：整窗的截图里那 40 高只是一条，看不清柱子。
			box, ok := tt.Find("频谱")
			if !ok {
				t.Fatalf("画面上找不到频谱那个区域")
			}
			saveCrop(t, dir, "02-频谱-那一块-"+name+".png", tt, int(box.Y)-24, int(box.H)+48)
		})
	}
}

// waitSpectrumData 等频谱里真的有东西。
//
// ⚠️ **要等的是「有谱」，不是「有那一块」**：播放器开口的头 150 毫秒是淡入
// （增益从 0 爬到 1，见 `internal/service/audio/fade.go`），那一段解出来的幅度
// 全是 0——频谱于是塌成**那条 2 像素的底排**（每一格都是底排比例，一格不差）。
// 只等「有数据了」的话，等到的是那一段，存出来的图上是一排等高的小墩子，看不出
// 「一根一根的柱」这件事。
//
// 所以这里等的是**某一格明显高过底排**（底排是 2/40 = 0.05）。
func waitSpectrumData(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	const floor = 2.0 / fullSpectrumHeight
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if bars := s.app.SpectrumBars(48, float32(fullSpectrumHeight)); tallest(bars) > floor+0.05 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("五秒了频谱还是一条底排：解码器没有吐出谱")
}

// tallest 是这一排里最高的那一格。
func tallest(bars []float32) float32 {
	var hi float32
	for _, v := range bars {
		if v > hi {
			hi = v
		}
	}
	return hi
}
