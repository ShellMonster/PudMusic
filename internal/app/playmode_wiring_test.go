// 播放模式接到 App 上的那几条：**歌真的放完了会往下走**、**换档立刻生效**、
// **模式落盘**。
//
// 上一份（playmode_test.go）量的是队列那一层的状态机。这一份量的是接线：把真
// 的播放器放起来、放真的歌、等它真的放完，看界面上那一首是不是按当前档位换掉了。
// 这一层量不到的话，状态机写得再对也只是一张没接线的图——而用户撞上的症状
// （「一首歌放完就停住，必须手动点下一首」）恰恰就住在这里。
package app_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// shorts 造 n 份各 0.25 秒的 WAV。
//
// ⚠️ 一定要**短**：这一份量的是「放完自动接下一首」，那要真的等一首放完。素材
// 是四秒的话，四档各跑一遍就是十六秒——全量并行跑的时候这是一段真实的解码 +
// 真实的音频输出，而套件对自己的要求是「别给别的测试添压」。
//
// 三份不同路径，所以「换到下一首了」断言的是文件名，不是碰巧。
func shorts(t *testing.T, n int) []string {
	t.Helper()
	b := shortenWAV(t, readFile(t, fixturePath(t, "stereo16.wav")), 0.25)
	dir := t.TempDir()
	out := make([]string, n)
	for i := range n {
		out[i] = writeBytes(t, dir, string(rune('a'+i))+".wav", b)
	}
	return out
}

// writeFile 写一份文件（给「读一份手写的设置」那几条用）。
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitTitle 等播放条上的歌名变成 want。
func waitTitle(t *testing.T, a *app.App, want string, what string) {
	t.Helper()
	waitFor(t, 6*time.Second, what, func() bool {
		return a.Snapshot().Title == want
	})
}

// TestASongThatFinishesMovesOnByItself 盯那条接线本身：歌放完了会自己往下走。
//
// 这是这一票要治的症状（对照审计 BMG-47 的 A5：「用户听到一首歌结束就停住，
// 必须手动点下一首」）。它同时钉住了判据对不对：
//
//   - 必须是**自然放完**才走（这里用的是一首好好的歌）；
//   - 走的是当前档位说好的下一首（这里是循环播放，所以是第二首）；
//   - **循环会一直绕**：跑满四步回到第一首，证明不是「接一次就停」。
func TestASongThatFinishesMovesOnByItself(t *testing.T) {
	paths := shorts(t, 3)
	want := []string{"b", "c", "a", "b"}

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeLoop)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot().Title; got != "a" {
		t.Fatalf("起播的歌是 %q，want a", got)
	}

	for i, w := range want {
		waitTitle(t, a, w, "第 "+string(rune('1'+i))+" 次自动接续没接到 "+w)
	}
}

// TestAPausedSongDoesNotMoveOn 盯「按了暂停，下一首不会自己放起来」。
//
// 这是同一个钩子上**最容易出事的另一半**：`Playing() == false` 有五种情况
// （暂停、停止、关闭、播完、解码失败），判据少看一样就会在用户按下暂停的
// 50 毫秒之后自己往下跳一首歌——用户听到的是「我按了暂停，它自己换了歌」。
func TestAPausedSongDoesNotMoveOn(t *testing.T) {
	paths := shorts(t, 3)

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeLoop)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}

	a.Pause()
	if got := a.Snapshot(); got.Playing {
		t.Fatal("按了暂停，状态里还在 Playing")
	}
	// 一首 0.25 秒的歌：静置一秒多，早就够放完两次了。
	time.Sleep(1200 * time.Millisecond)
	if got := a.Snapshot(); got.Title != "a" || got.Playing {
		t.Errorf("暂停之后播放条变成了 %q/Playing=%v，want 还是 a 且没在放", got.Title, got.Playing)
	}
}

// TestSequenceStopsAfterTheLastSong 盯顺序播放在 App 这一侧也是「停」。
//
// 停下来的样子也要钉住：停在**开头**、没有在放——和 Biu 的
// `audio.currentTime = 0; audio.pause()` 一致。用户按播放就能从头再听最后那首，
// 而进度条不会从末尾卡着不动。
func TestSequenceStopsAfterTheLastSong(t *testing.T) {
	paths := shorts(t, 2)

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeSequence)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}

	waitTitle(t, a, "b", "第一首放完没有接到第二首")

	// 接下来是队尾：等这一首放完，播放条应该停在 b 的开头而且不在放。
	waitFor(t, 6*time.Second, "最后一首放完之后停下来", func() bool {
		st := a.Snapshot()
		return st.Title == "b" && !st.Playing
	})
	st := a.Snapshot()
	if st.Position != 0 {
		t.Errorf("停下来的时候位置是 %v，want 0（停在开头，不卡在末尾）", st.Position)
	}

	// 停着的时候不该自己又动起来。
	time.Sleep(800 * time.Millisecond)
	if got := a.Snapshot(); got.Playing || got.Title != "b" {
		t.Errorf("停下来之后又自己动了：%q/Playing=%v", got.Title, got.Playing)
	}
}

// TestSingleReplaysWithoutMovingOn 盯单曲播放在 App 这一侧。
//
// 走完三遍还是同一首：前两遍换新播放器，第三遍（重放）不换——换了的症状是
// 进度条从头开始闪一下、句柄来回收去，而这是同一首从头再放，本来就该那样。
func TestSingleReplaysWithoutMovingOn(t *testing.T) {
	paths := shorts(t, 3)

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeSingle)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}

	// a 放完 → 还是 a（从头再放）→ 还是 a ……连着三轮，标题一次都不许变。
	//
	// ⚠️ 每一轮都要**真的看见这一首放完**：只断「标题是 a 且在放」的话，
	// 起播那一瞬间就成立了，整条等于没跑。判据是位置——先等它走到末尾
	// （reachedEnd），再等它回到开头（重放过了）。
	for round := range 3 {
		waitFor(t, 6*time.Second, "单曲没有放到头", func() bool {
			return reachedEnd(a.Snapshot())
		})
		waitFor(t, 3*time.Second, "单曲没有从头再放", func() bool {
			st := a.Snapshot()
			return st.Title == "a" && st.Playing && st.Position < 40*time.Millisecond
		})
		if got := a.Queue().CurrentIndex(); got != 0 {
			t.Fatalf("第 %d 轮之后当前项在下标 %d，want 0（单曲播放一步都不挪）", round+1, got)
		}
	}
}

// TestChangingTheModeTakesEffectWithoutRestartingTheSong 盯硬要求那一条：
// **换档立刻生效，不用重开歌**。
//
// 断的是两件不同的事，缺一件都算坏：
//
//   - 换档**不打断正在放的那一首**：位置不许跳回 0（跳了就是重开了一遍文件，
//     用户听到的是歌从头又唱了一次）。
//   - 换档**对下一首立刻生效**：这一首放完之后要走新档的规则（这里是单曲：还是
//     它），而不是旧档的（顺序：走 b）。
//
// 只测「重开应用后是新档」是不够的——那和「换档要重开歌」是同一种坏法，用户
// 第一次按那个按钮就会撞上。
func TestChangingTheModeTakesEffectWithoutRestartingTheSong(t *testing.T) {
	paths := shorts(t, 3)

	a := newApp(t)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeSequence)
	if err := a.PlayFrom(items(paths...)...); err != nil {
		t.Fatal(err)
	}

	// 等它真的放进去一小段，再换档——在位置还是 0 的时候换，看不出有没有重开。
	waitFor(t, 3*time.Second, "歌没有开始走", func() bool {
		return a.Snapshot().Position >= 60*time.Millisecond
	})
	before := a.Snapshot().Position

	a.SetPlayMode(app.PlayModeSingle)
	if got := a.Snapshot(); got.PlayMode != app.PlayModeSingle {
		t.Fatalf("换完档，状态里还是 %q", got.PlayMode)
	}
	if got := a.Snapshot(); got.Title != "a" {
		t.Errorf("换档把正在放的歌换掉了：现在是 %q", got.Title)
	}
	if got := a.Snapshot().Position; got < before {
		t.Errorf("换档之后位置从 %v 跳回了 %v——换档把这一首重开了一遍", before, got)
	}

	// 这一首放完之后要走**新档**的规则：单曲，还是它。
	waitFor(t, 6*time.Second, "换档之后 a 放完没有重放", func() bool {
		st := a.Snapshot()
		return st.Title == "a" && st.Playing && st.Position < 40*time.Millisecond
	})
	if got := a.Queue().CurrentIndex(); got != 0 {
		t.Errorf("换到单曲之后当前项在下标 %d，want 0", got)
	}
}

// TestThePlayModeSurvivesARestart 盯模式落盘。
//
// 走设置那一套（MountSettings / CloseSettings），不用另造存储。断的是三件事：
// 写进去、重新挂上读出来、以及**没有设置文件时**落到默认档而不是空档
// （空档在界面上是一个没有字的按钮）。
func TestThePlayModeSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)

	// 存不住不算错，所以这一句不 Fatal——它失败的话后面那句「重开还是随机」
	// 会以一句更费解的话失败。
	a := app.New()
	defer a.Close()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	a.SetPlayMode(app.PlayModeRandom)
	a.SetKeepPagesOrderInRandom(false)
	if got := a.SettingsSnapshot().PlayMode; got != app.PlayModeRandom {
		t.Fatalf("设置里读到的是 %q，want random", got)
	}
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	b := app.New()
	defer b.Close()
	if err := b.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	if got := b.PlayMode(); got != app.PlayModeRandom {
		t.Errorf("重开之后是 %q，want random（这一档没落盘）", got)
	}
	if got := b.Snapshot().KeepPagesOrderInRandom; got {
		t.Error("重开之后「保持分集顺序」回到开启——这个开关没落盘")
	}
	// 换档立刻生效：重开之后按一下就该走到随机之后的那一档（单曲），
	// 不用重开任何一首歌。
	b.CyclePlayMode()
	if got, want := b.PlayMode(), app.PlayModeSingle; got != want {
		t.Errorf("重开之后按一下是 %q，want %q（档序照 Biu：顺序→循环→随机→单曲）", got, want)
	}
}

// TestAFreshInstallGetsBiuDefaultMode 盯没有设置文件时的默认档。
//
// 两个方向：一是默认档是**循环播放**（Biu 的初始值，也是「放完就停住」这个
// 症状的反面）；二是那份文件里根本没有 playMode 这一格时，normalize 要把它
// 变成默认档——一份从旧版升上来的设置正是这个样子。
func TestAFreshInstallGetsBiuDefaultMode(t *testing.T) {
	if app.DefaultPlayMode != app.PlayModeLoop {
		t.Errorf("默认档是 %q，want loop（Biu 的初始值）", app.DefaultPlayMode)
	}

	a := app.New()
	defer a.Close()
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	if got := a.PlayMode(); got != app.PlayModeLoop {
		t.Errorf("没有设置文件时的档位是 %q，want loop", got)
	}
	// ⚠️ **只有新装的是开的**：一份已经存在的设置文件里没有这一格，而 Go 的
	// bool 表达不了「没设过」，读回来就是 false（`Settings` 里那一格的注释
	// 记着这件事，`ShowSearchHistory` / `ReportPlayHistory` 也一样）。所以这一句
	// 量的是「新装」这一条路。
	if !a.Snapshot().KeepPagesOrderInRandom {
		t.Error("新装时「随机时保持分集顺序」是关的，Biu 的初始值是开（play-list.ts:373）")
	}

	// 一份没有 playMode 这一格的旧设置文件。
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	writeFile(t, path, `{"appSettings":{"themeMode":"light"}}`)
	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.PlayMode != app.PlayModeLoop {
		t.Errorf("一份没有 playMode 的设置读出来是 %q，want loop（normalize 该补上）", s.PlayMode)
	}

	// 手写坏的值也该被拉回来，而不是原样留在界面上。
	writeFile(t, path, `{"appSettings":{"playMode":"whatever"}}`)
	s, err = app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.PlayMode != app.PlayModeLoop {
		t.Errorf("playMode 手写成 \"whatever\" 时读到的是 %q，want loop", s.PlayMode)
	}
}
