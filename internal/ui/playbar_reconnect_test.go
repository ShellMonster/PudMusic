package ui

// 播放条上的「正在重连」（票 #315）。
//
// # 这一组盯的是「断流之后用户看到什么」
//
// 以前这一档**一个字都没有**：字节源在后台换地址 / 换流、应用在从播放位置
// 重开，而界面上只是「歌停着、界面不动、没有字」——用户等多久都像应用死了。
//
// 加上退避之后更糟：那 7 秒的等待原本是**完全静默**的。
//
// 改之后播放条上是：
//
//	「正在重连」	在救（灰字，不是红字——红字是「出错了」）
//	一句失败的话	救不回来了（错的那句优先，见下面那条）

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	mygoui "github.com/egoist/mygo/ui"
)

// TestThePlayBarSaysItIsReconnecting 盯「正在重连」那一句话。
//
// 🔴 **判据是画面上有没有那句话**（不是「有没有转圈」）：转圈是动着的，
// 无头 Tester 抓不住它；而那句话是用户真正读到的东西。
func TestThePlayBarSaysItIsReconnecting(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "响到一半的那一首", Playing: true,
		Reconnecting: true, ReconnectAttempt: 2,
	}, 1000)

	if !tt.HasText("正在重连") {
		t.Errorf("播放条上没有「正在重连」：%q", tt.Texts())
	}
}

// TestTheReconnectingLineHasASpinner 盯「正在重连」那一行里有一枚转圈。
//
// ⚠️ **判据是它的无障碍名字**：转圈没有可见的字，而它是唯一能认出「这里在
// 等」的凭据（无头 Tester 也只认字和名字）。
func TestTheReconnectingLineHasASpinner(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "响到一半的那一首", Playing: true,
		Reconnecting: true,
	}, 1000)

	if _, ok := tt.Find("正在重连"); !ok {
		t.Errorf("「正在重连」那一行里没有转圈：%q", tt.Texts())
	}
}

// TestTheErrorWinsOverTheReconnectingLine 盯**错的那句优先**。
//
// 「正在重连」和错误那一行占**同一个位置**（播放条上只有一行宽）。出错
// 之后用户要看到的是「为什么不行、还能怎么办」，不是「还在重连」——一句
// 错比一枚转圈信息量大。
func TestTheErrorWinsOverTheReconnectingLine(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "响到一半的那一首",
		Err: "在线流拉不下来了", Reconnecting: true,
	}, 1000)

	if tt.HasText("正在重连") {
		t.Errorf("出错了还在说「正在重连」：%q", tt.Texts())
	}
	if !tt.HasText("在线流拉不下来了") {
		t.Errorf("失败之后界面上没有那句话：%q", tt.Texts())
	}
}

// TestTheReconnectingLineFitsOnOneLine 盯**那句话一行放得下**。
//
// 🔴 **判据是「这句话本身有多宽」**（和界面上用的是同一个字号）：
// 播放条上那一行只有 140 像素宽（1200 宽的窗口，实测），12 号字放得下十来个
// 字。一句长话上屏只剩「网络不稳，正在…」这样的碎片——那不是「一句能看懂的
// 话」。
//
// 完整那一句（带第几次）在悬停提示里，屏幕上短、提示里全。
func TestTheReconnectingLineFitsOnOneLine(t *testing.T) {
	const msg = "正在重连"
	var measured float32
	mygoui.NewTester(func(c *mygoui.Context) {
		measured, _ = c.MeasureText(0, mygoui.Span{
			Text: msg, Size: scale.Font(c, scale.Size12)})
	}, 300, 60).Frame()

	if measured > 140 {
		t.Errorf("「%s」宽 %.1f，而那一行只有 140 像素——放不下，屏幕上只剩碎片",
			msg, measured)
	}
}

// TestRenderPlayBarReconnectShots 把播放条上那三档状态存成 PNG。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarReconnectShots
//
// **深浅两套都出**：这一档是灰字，而灰字在深色底下比在浅色底下难认得多
// （DESIGN §8 那条「深浅两套都要看」在这里不是形式）。
//
// 三档摆在一起是为了**对比**：那一行只有一行宽，三句话互斥，而「正在重连」
// 是唯一一句**不是红字**的——只看一眼浅色会以为它和「正在取流」差不多。
func TestRenderPlayBarReconnectShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	base := app.State{
		HasTrack: true, Title: "响到一半的那一首",
		Artist: "某个 UP 主", ArtistMid: 12345,
		Duration: 3 * time.Minute, Position: 42 * time.Second,
		Rate: 1, Volume: 0.8, PlayMode: app.PlayModeSequence,
	}
	shots := []struct {
		file string
		st   app.State
	}{
		{"正在重连", app.State{
			HasTrack: base.HasTrack, Title: base.Title, Artist: base.Artist, ArtistMid: base.ArtistMid,
			Duration: base.Duration, Position: base.Position, Rate: base.Rate, Volume: base.Volume,
			PlayMode: base.PlayMode, Playing: true,
			Reconnecting: true, ReconnectAttempt: 2,
		}},
		{"正在取流", app.State{
			HasTrack: base.HasTrack, Title: base.Title, Artist: base.Artist, ArtistMid: base.ArtistMid,
			Duration: base.Duration, Position: base.Position, Rate: base.Rate, Volume: base.Volume,
			PlayMode: base.PlayMode, Playing: true,
			Loading: true,
		}},
		{"重连失败", app.State{
			HasTrack: base.HasTrack, Title: base.Title, Artist: base.Artist, ArtistMid: base.ArtistMid,
			Duration: base.Duration, Position: base.Position, Rate: base.Rate, Volume: base.Volume,
			PlayMode: base.PlayMode,
			Err:      "在线流拉不下来了",
		}},
	}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			for _, sh := range shots {
				st := sh.st
				b := newTestShell(t, at(1200, 800), systemIsDark(dark),
					withView(func(s *Shell) func(*mygoui.Context) {
						return func(c *mygoui.Context) {
							s.wake.set(c)
							s.viewPlayBar(c, st)
						}
					}))
				b.tt.Frame()
				save(t, dir, "断流重连-"+sh.file+"-"+name+".png", b.tt)
			}
		})
	}
}
