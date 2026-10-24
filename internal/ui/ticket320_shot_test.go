package ui

// 票 #320 的画面验收：播放条上那一句错的「**详情层**」。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket320Shots
//
// # 深浅两套都要出
//
// 这一票改的是**红字 + 一块浮层**：红字在浅色下是压在白底上的，深色下是压在
// 深灰底上的——而浮层自己有底、有边、有影，两套底色下那圈边和那层影是不是还
// 看得出来，只有出图才知道。
//
// # 播放条要摆在窗口底下
//
// 浮层是往**上**弹的（底下是窗口边，没有位置）。摆在顶上时它会被窗口裁掉半
// 张——那样出的是一张「浮层被裁了」的图，看不出它到底长什么样。

import (
	"fmt"
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// TestRenderTicket320Shots 把这一票的两处各存一张 PNG，深浅各一套。
func TestRenderTicket320Shots(t *testing.T) {
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
			st := app.State{Err: errLongSentence}

			// 一、播放条上那一句错：**一行 + 省略号**（它就该是这样——
			// 76 高的播放条装不下两行，完整那一句在详情层里）。
			tt, _ := errBarTester(t, &st, 1200, 320, dark)
			// 指针停在窗口中间那块空地上：它一悬停到那一句上，提示框就出来了，
			// 而这一张要看的是「播放条上那一句长什么样」。
			tt.Move(600, 120)
			tt.Frame()
			saveRect(t, dir, fmt.Sprintf("80-播放条-那一句错-%s.png", name), tt,
				0, 240, 520, 80)

			// 二、详情层：点开之后，完整那一句排在上面那块面板里。
			tt, _ = errBarTester(t, &st, 1200, 320, dark)
			openErrDetail(t, tt, errLongSentence)
			save(t, dir, fmt.Sprintf("81-播放条-详情层-整窗-%s.png", name), tt)
			saveRect(t, dir, fmt.Sprintf("82-播放条-详情层-%s.png", name), tt,
				0, 180, 420, 140)
		})
	}
}
