package ui

// 随机档加歌时的那一句提示（票 #387）。
//
// 随机那一档的洗牌袋是**池子**：加进来的歌立刻在池中，这一轮就轮到它（见
// app/queue_shuffle.go）。而用户看不见池子里是什么——他加了一首歌，得知道它
// **这一轮就放得到**，而不是「要等下一轮」。
//
// ⚠️ **别写「随机下这一轮放不到」**：那不是我们要的行为（票 #387，2026-10-25 人
// 定的：随机档加进来的歌这一轮就轮到它）。

import (
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// TestAddingASongInRandomModeSaysItWillPlayThisRound 盯「**随机档加歌时要说
// 一句**」。
//
// 判据有两头：说清「这一轮就轮到它」，而**不是**「这一轮放不到」——后者是旧
// 行为，而旧行为已经改成池子了（说它等于对用户说了一句假话）。
func TestAddingASongInRandomModeSaysItWillPlayThisRound(t *testing.T) {
	s, _ := newShell(t)
	s.app.SetPlayMode(app.PlayModeRandom)
	// 先摆一首歌在队列里：队列不是空的时候「添加到队列」只是排着（不空的那条
	// 路不会去开播放器，这一档要验的只是那句话）。
	s.app.Queue().Append(copiesOf(t, 1)...)

	s.listActions().Add(copiesOf(t, 1))

	got := lastToast(s)
	if !strings.Contains(got, "这一轮") {
		t.Errorf("说的是 %q，没有说清这一轮就轮到它", got)
	}
	if strings.Contains(got, "放不到") {
		t.Errorf("说的是 %q——那不是我们要的行为（随机档加进来的歌这一轮就轮到它）", got)
	}
}

// TestAddingASongOutsideRandomModeSaysNothing 盯「**其余三档一句话都不说**」。
//
// 那一档的下一首是按索引算的，加进来的歌排在哪就是哪——没有什么要解释的，而
// 多余的一句话是噪音。
func TestAddingASongOutsideRandomModeSaysNothing(t *testing.T) {
	for _, mode := range []app.PlayMode{app.PlayModeSequence, app.PlayModeLoop, app.PlayModeSingle} {
		s, _ := newShell(t)
		s.app.SetPlayMode(mode)
		s.app.Queue().Append(copiesOf(t, 1)...)

		s.listActions().Add(copiesOf(t, 1))

		if got := lastToast(s); got != "" {
			t.Errorf("%s 档说了 %q，want 什么也不说（那一档下一首是按索引算的）", mode, got)
		}
	}
}
