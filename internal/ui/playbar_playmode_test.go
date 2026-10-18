package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"

	mygoui "github.com/egoist/mygo/ui"
)

// 播放条上的播放模式按钮。
//
// # 这一组在盯什么
//
// 「一首歌放完之后下一首是哪首」在 `internal/app` 那一层量（队列的状态机）。
// 这一层量的是**控件自己**：按钮在不在、上面写的是不是当前那一档、按一下换没
// 换、换完字有没有跟着变、右键那一列里四档齐不齐。
//
// 少了这一层，故障的样子是「功能都对了，界面上却没有那个按钮」——而用户只会
// 知道「Biu 那边有个按钮，我这边没有」。

// modeBarTester 画播放条，State 每帧从 App 现读（和真窗口里一模一样）。
//
// 现读是必须的：这一组要验的正是「按下去 → App 改了 → 界面下一帧读到」。传一份
// 钉死的 State 进来，等于把要验的那半段先接上了假线。
func modeBarTester(t *testing.T, width int) (*mygoui.Tester, *app.App) {
	t.Helper()
	a := app.New()
	var b *testShell
	b = newTestShell(t, onApp(a), at(width, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, a.Snapshot())
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, a
}

// modeBarTesterWithState 用一份钉死的 State 画播放条。
//
// **只给量布局的那几条用**：那一组量的是「控件有没有把别的挤坏」，喂进去的
// State 是素材（真开一首文件的话标题是文件名，短得挤不出问题），不是被验的
// 那一半。行为那一组必须走 modeBarTester 现读。
func modeBarTesterWithState(t *testing.T, width int, st app.State) (*mygoui.Tester, *app.App) {
	t.Helper()
	var b *testShell
	b = newTestShell(t, at(width, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, st)
		}
	}))
	tt := b.tt
	tt.Frame()
	return tt, b.a
}

// TestThePlayModeButtonSaysWhichModeItIs 盯按钮上的字就是当前那一档。
//
// 断「画面上那个短标签」而不是 App 里的枚举：枚举对了而按钮不认它，用户看到的
// 是**一个没有字的按钮**——空按钮不报错、不消失，按下去也不知道换成了什么。
func TestThePlayModeButtonSaysWhichModeItIs(t *testing.T) {
	tt, a := modeBarTester(t, 760)
	for _, m := range app.PlayModes {
		a.SetPlayMode(m)
		tt.Frame()
		if !tt.HasText(m.Label()) {
			t.Errorf("档位是 %q，按钮上却没有 %q：%q", m, m.Label(), tt.Texts())
		}
		if got := a.Snapshot().PlayMode; got != m {
			t.Errorf("设了 %q，状态里读到的是 %q", m, got)
		}
	}
}

// TestClickingThePlayModeButtonWalksTheFourModes 盯按一下换一格。
//
// 档序照 Biu 的 `getPlayModeList`（顺序 → 循环 → 随机 → 单曲 → 回顺序），所以
// 这一条钉的就是「按几下能转回同一档」——用户来回按的次数是他记住这一件事的
// 唯一依据。
//
// ⚠️ 断的是**按钮上的字跟着变了**，不只是 App 里的枚举变了：State 是每帧现读
// 的，App 改了而界面没被叫醒的话，画面上还是旧的那一档。
func TestClickingThePlayModeButtonWalksTheFourModes(t *testing.T) {
	tt, a := modeBarTester(t, 760)

	a.SetPlayMode(app.PlayModeSequence)
	tt.Frame()

	seen := []app.PlayMode{}
	for range len(app.PlayModes) {
		if err := tt.Click("播放模式"); err != nil {
			t.Fatalf("点不到播放模式按钮：%v（画面：%q）", err, tt.Texts())
		}
		tt.Frame()
		cur := a.Snapshot().PlayMode
		seen = append(seen, cur)
		if !tt.HasText(cur.Label()) {
			t.Errorf("换到 %q，按钮上却是 %q 那一档的字：%q", cur, cur.Label(), tt.Texts())
		}
	}
	want := []app.PlayMode{app.PlayModeLoop, app.PlayModeRandom, app.PlayModeSingle, app.PlayModeSequence}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("按了 %d 下 = %v，want %v（Biu 的档序）", len(seen), seen, want)
		}
	}
}

// TestThePlayModeMenuListsEveryMode 盯右键那一列里四档都在。
//
// 少一档就是「用户想用单曲，翻遍了浮层没有」；多一档就是界面上出现了算法不认的
// 值，按下去被吸到别的档位上，而按钮写的又是吸过去的那个——用户完全不知道自己
// 刚才按的到底是什么。
//
// ⚠️ 右键出来的是**系统菜单**而不是窗口里的一片元素，所以要经 `Menu()` 读、
// 经 `ChooseMenuItem` 选（`Texts()` 里看不见它——这一点在写这一条时被坑过）。
func TestThePlayModeMenuListsEveryMode(t *testing.T) {
	tt, _ := modeBarTester(t, 760)

	// 没点开时，浮层里的档位不该出现在画面上（按钮上的那一档当然在）。
	if tt.HasText(app.PlayModeRandom.Desc()) {
		t.Errorf("没点开就能在画面上看到浮层里的档位：%q", tt.Texts())
	}

	if err := tt.RightClick("播放模式"); err != nil {
		t.Fatalf("右键打不开播放模式那一列：%v（画面：%q）", err, tt.Texts())
	}
	menu := tt.Menu()
	for _, m := range app.PlayModes {
		if !slices.Contains(menu, m.Desc()) {
			t.Errorf("浮层里没有 %q：%q", m.Desc(), menu)
		}
	}
	if len(menu) != len(app.PlayModes) {
		t.Errorf("浮层里有 %d 项 %q，want 刚好四档", len(menu), menu)
	}
}

// TestChoosingAModeFromTheMenuTakesIt 盯从那一列里选一档真的会换。
func TestChoosingAModeFromTheMenuTakesIt(t *testing.T) {
	tt, a := modeBarTester(t, 760)
	a.SetPlayMode(app.PlayModeLoop)
	tt.Frame()

	for _, m := range []app.PlayMode{app.PlayModeSingle, app.PlayModeRandom, app.PlayModeSequence} {
		if err := tt.RightClick("播放模式"); err != nil {
			t.Fatalf("右键打不开播放模式那一列：%v", err)
		}
		if err := tt.ChooseMenuItem(m.Desc()); err != nil {
			t.Fatalf("选不了 %q：%v", m.Desc(), err)
		}
		tt.Frame()
		if got := a.Snapshot().PlayMode; got != m {
			t.Fatalf("选了 %q，档位却是 %q", m, got)
		}
		if !tt.HasText(m.Label()) {
			t.Errorf("选了 %q，按钮上不是 %q 那一档的字：%q", m, m.Label(), tt.Texts())
		}
	}
}

// TestTheKeepPagesOrderSwitchOnlyShowsUpWhileShuffling 盯「随机时保持分集顺序」。
//
// 两个方向都要，理由各一条：
//
//   - **只在随机那一档里出现**：别的档位里它什么都不改变，摆出来只会让人以为
//     它在别的档里也有用（Biu 也是只在 `playMode === Random` 时给这个开关）。
//   - **摆出来就真的能改**：只画一个开关而按下去没反应，比不画更糟——用户会
//     以为「我按了，它坏了」。
func TestTheKeepPagesOrderSwitchOnlyShowsUpWhileShuffling(t *testing.T) {
	tt, a := modeBarTester(t, 760)
	const keep = "保持分集顺序"

	a.SetPlayMode(app.PlayModeLoop)
	tt.Frame()
	if err := tt.RightClick("播放模式"); err != nil {
		t.Fatalf("右键打不开：%v", err)
	}
	if slices.Contains(tt.Menu(), keep) {
		t.Errorf("循环那一档里摆着 %q（%q）——别的档位里它什么都不改变", keep, tt.Menu())
	}

	a.SetPlayMode(app.PlayModeRandom)
	tt.Frame()
	if !a.Snapshot().KeepPagesOrderInRandom {
		t.Fatal("刚进随机档时「保持分集顺序」是关的，Biu 的初始值是开")
	}
	if err := tt.RightClick("播放模式"); err != nil {
		t.Fatalf("右键打不开：%v", err)
	}
	if !slices.Contains(tt.Menu(), keep) {
		t.Fatalf("随机那一档里没有 %q：%q", keep, tt.Menu())
	}

	if err := tt.ChooseMenuItem(keep); err != nil {
		t.Fatalf("按不了 %q：%v", keep, err)
	}
	tt.Frame()
	if a.Snapshot().KeepPagesOrderInRandom {
		t.Errorf("按了 %q 之后它还开着——这个开关按下去没反应", keep)
	}
}

// TestThePlayModeButtonDoesNotSquashTheTitle 盯这个按钮没有把歌名挤没。
//
// 这一条是这一票里唯一一条「新控件把老控件挤坏」的防线。挤坏了之后**界面看不出
// 坏了**：字还在那儿，只是一个字一行、或者被 88 高的播放条裁掉半个。
//
// 它把 760 也算进来——比窗口下限（`WindowOptions` 里的 `MinWidth: 900`）还窄。
// 那一档是这个播放条最紧的状态：右边那一行（音量 120 + 倍速 64 + 加歌 56 +
// 三个 12 的缝）已经满座，中间那一列的富余就靠「按钮摆在哪一行」挣出来
// （见 playbar.go 里 viewProgress 那段）。
func TestThePlayModeButtonDoesNotSquashTheTitle(t *testing.T) {
	for _, width := range []int{760, 900, 1000, 1200, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			// 钉死的 State：这里要的是一根很长的歌名，喂 State 比真开一首文件
			// 省事得多（真开的话标题是文件名，短得挤不出问题）。
			tt, _ := modeBarTesterWithState(t, width, app.State{
				HasTrack: true,
				Title:    "一首歌的名字很长很长很长很长",
				Rate:     1,
				PlayMode: app.PlayModeRandom,
			})

			title, ok := tt.Find("一首歌的名字很长很长很长很长")
			if !ok {
				t.Fatalf("宽度 %d：歌名不在画面上：%q", width, tt.Texts())
			}
			if title.W < 40 || title.W < title.H {
				t.Errorf("宽度 %d：歌名只剩 %.1f×%.1f，已经被挤成竖条了",
					width, title.W, title.H)
			}
			if title.H > playbarHeight || title.Y+title.H > playbarHeight {
				t.Errorf("宽度 %d：歌名（y=%.1f 高 %.1f）被 %d 高的播放条裁了",
					width, title.Y, title.H, playbarHeight)
			}
			// 按钮自己也要**整个在画面上**：挤没了的那一半会像歌名一样不出错，
			// 只是不见了。
			btn, ok := tt.Find("随机")
			if !ok {
				t.Fatalf("宽度 %d：模式按钮不见了：%q", width, tt.Texts())
			}
			if btn.Y+btn.H > playbarHeight || btn.X+btn.W > float32(width) {
				t.Errorf("宽度 %d：模式按钮 {%.1f %.1f %.1fx%.1f} 有一半在播放条外面",
					width, btn.X, btn.Y, btn.W, btn.H)
			}
		})
	}
}
