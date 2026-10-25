package ui

// 这一组盯「**冷启动那一段界面上有一句话**」（票 #478）。
//
// # 它盯的是什么
//
// 冷启动恢复上次那一首时：**窗口开了、界面在那儿、就是没声音**——
// 而播放条上原来写的是「还没有打开文件」。
//
// ⚠️ **那一句是假话**：应用不是「没有文件」，它是在**等上一首**（那一趟
// 取流还在路上）。用户看到的是「我什么都没干，它怎么不动」——而他不知道
// 自己在等什么。
//
// 所以启动那一段要换成一句「正在启动应用…」。
//
// # 判据是**画面**
//
// SPEC 的 Testing Decisions 说 UI 测试断渲染结果。而这一族有一处例外：
// 「启动完了之后那句话要收掉」——那一处断的是状态（见
// `internal/app/startup_test.go`），而这里是画面。

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// TestTheStartupSaysItIsPreparing 盯「启动那一段，界面上有一句话」。
func TestTheStartupSaysItIsPreparing(t *testing.T) {
	_, tt := coverBarTester(t, app.State{Starting: true})
	if !tt.HasText("正在启动应用…") {
		t.Errorf("启动那一段没有那句话，文字有 %q", tt.Texts())
	}
	// 🔴 **「还没有打开文件」那句话必须让位**：两句都在屏幕上，用户读到的是
	// 「它既没有文件，又在启动」——而事实是「它在等上一首」。
	if tt.HasText("还没有打开文件") {
		t.Errorf("启动那一段还在说「还没有打开文件」，文字有 %q", tt.Texts())
	}
}

// TestTheStartupLineGoesAwayWhenTheAppIsReady 盯「准备好了就不说那句话」。
//
// ⚠️ **它不能一直亮着**：应用准备好了之后还写「正在启动」，用户会以为
// 应用没起来——而那比什么都不说更糟。
func TestTheStartupLineGoesAwayWhenTheAppIsReady(t *testing.T) {
	_, tt := coverBarTester(t, app.State{})
	if tt.HasText("正在启动应用…") {
		t.Errorf("应用好了还在说「正在启动应用…」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("还没有打开文件") {
		t.Errorf("没歌时那句「还没有打开文件」不在，文字有 %q", tt.Texts())
	}
}

// TestTheStartupLineIsNotShownWhenThereIsATrack 盯「**有歌的时候不许说
// 「正在启动」**」。
//
// ⚠️ **有歌的时候界面上摆的是歌名**：用户看得见应用已经准备好了，
// 而那一句话会跟歌名抢位置（而歌名那一格是给歌名的）。
func TestTheStartupLineIsNotShownWhenThereIsATrack(t *testing.T) {
	_, tt := coverBarTester(t, app.State{Starting: true, HasTrack: true, Title: "已经放起来的那一首"})
	if tt.HasText("正在启动应用…") {
		t.Errorf("有歌的时候还在说「正在启动应用…」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("已经放起来的那一首") {
		t.Errorf("歌名不在，文字有 %q", tt.Texts())
	}
}
