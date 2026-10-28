package app_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 「上次听到哪儿」这一格的验收（票 #347）。
//
// ⚠️ **一律走夹具**：素材是仓库里那个 FLAC（4 秒长），设置文件落在临时目录里。
// **不打真 B 站**——在线的那一项只验「落盘的是稳定标识」和「恢复时守卫安静跳过」，
// 不去取流。

// playbackMount 挂一份设置在临时目录里，返回 App 和设置文件路径。
func playbackMount(t *testing.T) (*app.App, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	a := app.New()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	return a, path
}

// restart 模拟「退出再打开」：把这一份设置落盘收尾，然后拿同一个文件开一个新 App。
//
// ⚠️ **它走的是真的那条路**：`Close` → `CloseSettings`（落盘 + 停后台写），
// 和界面层关窗口时做的是同一串（见 internal/ui/shell_window.go 的 Close）。
func playbackRestart(t *testing.T, a *app.App, path string) *app.App {
	t.Helper()
	a.Close()
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	b := app.New()
	if err := b.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.Close()
		_ = b.CloseSettings()
	})
	return b
}

// playbackStored 读设置文件里落盘的那一格。
func playbackStored(t *testing.T, path string) app.PlaybackSnapshot {
	t.Helper()
	s, err := playbackStoredRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// playbackStoredRaw 读设置文件里落盘的那一格，**读不出来的错一起给回来**。
//
// 轮询落盘的时候用这个（不是 playbackStored）：设置文件在**第一次落盘之前
// 根本不存在**（见 LoadSettings——文件不在就按默认值开起来，不建文件），
// 而 playbackStored 读不到就 t.Fatal——拿它轮询会在第一次落盘之前把测试
// 判死。
func playbackStoredRaw(path string) (app.PlaybackSnapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return app.PlaybackSnapshot{}, err
	}
	var f struct {
		AppSettings struct {
			Playback app.PlaybackSnapshot `json:"playback"`
		} `json:"appSettings"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return app.PlaybackSnapshot{}, fmt.Errorf("设置文件读不出来：%w", err)
	}
	return f.AppSettings.Playback, nil
}

// playbackStoredSettings 读设置文件里的 appSettings 那一段（原始的那几格）。
//
// ⚠️ **读的是盘上真的那份**：不是内存里的 store。有些断言要验的就是「文件到
// 底落没落」——落盘那一格每一拍写一次的话，设置存储的防抖窗口会被一直顶掉，
// 文件一次也不落（而内存里那份是对的，看不出来）。
func playbackStoredSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("设置文件读不出来：%v", err)
	}
	var f struct {
		AppSettings map[string]any `json:"appSettings"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("设置文件读不出来：%v", err)
	}
	return f.AppSettings
}

// TestTheQueueTheCurrentItemAndThePositionSurviveARestart 是这一票的主验收：
// 队列顺序、当前项、当前播放位置都落盘了，重启之后回到原位。
func TestTheQueueTheCurrentItemAndThePositionSurviveARestart(t *testing.T) {
	paths := copies(t, 3)
	a, path := playbackMount(t)

	// 放三首，从头开始放，然后切到第二首、听到中间、暂停。
	if err := a.PlayFrom(items(paths[0], paths[1], paths[2])...); err != nil {
		t.Fatal(err)
	}
	if err := a.PlayAt(1); err != nil {
		t.Fatal(err)
	}
	a.ScrubEnd(0.5)
	a.Toggle() // 暂停：位置才停得住，断言才不含糊

	want := a.Snapshot().Position
	if want <= 0 {
		t.Fatalf("拖完之后位置是 %v，want > 0", want)
	}

	b := playbackRestart(t, a, path)
	if err := b.RestorePlayback(); err != nil {
		t.Fatal(err)
	}

	// 队列顺序。
	got := b.Queue().Items()
	if len(got) != 3 {
		t.Fatalf("恢复出来的队列有 %d 项，want 3", len(got))
	}
	for i, it := range got {
		if it.Path != paths[i] {
			t.Errorf("队列第 %d 项是 %q，want %q", i, it.Path, paths[i])
		}
	}

	// 当前项。
	cur, ok := b.Queue().Current()
	if !ok {
		t.Fatal("恢复之后队列里没有当前项")
	}
	if cur.Path != paths[1] {
		t.Errorf("恢复出来的当前项是 %q，want %q", cur.Path, paths[1])
	}

	// 播放位置。
	st := b.Snapshot()
	if st.Position != want {
		t.Errorf("恢复出来的位置是 %v，want %v", st.Position, want)
	}

	// ⚠️ **位置不许自己跑掉**：后台时钟每一拍都在读播放器的位置（见 app.go 的
	// tick），而恢复出来的那一首**还没有开过声**——位置要是被读成 0，进度条
	// 就弹回开头，而落盘的那一格也跟着变成 0。
	//
	// 等几拍再看一眼：这一条钉的是「停在原位」在**一段时间之后**还成立。
	time.Sleep(300 * time.Millisecond)
	if got := b.Snapshot().Position; got != want {
		t.Errorf("等了几拍之后位置变成了 %v，want %v（位置不该自己跑掉）", got, want)
	}
}

// TestTheRestoredTrackIsPausedAndDoesNotMakeAnySound 是「停在原位且暂停」那一条：
// 启动时不该突然出声。
func TestTheRestoredTrackIsPausedAndDoesNotMakeAnySound(t *testing.T) {
	paths := copies(t, 1)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	a.ScrubEnd(0.5)
	a.Toggle() // 暂停

	b := playbackRestart(t, a, path)
	if err := b.RestorePlayback(); err != nil {
		t.Fatal(err)
	}

	st := b.Snapshot()
	if !st.HasTrack {
		t.Fatal("恢复之后播放条上什么都没有（歌名和进度条都不在了）")
	}
	if st.Playing {
		t.Error("恢复之后自己在放了——启动时不该出声")
	}
	if st.Position <= 0 {
		t.Errorf("恢复之后的位置是 %v，want > 0（要停在原位）", st.Position)
	}
	if st.Title == "" {
		t.Error("恢复之后歌名是空的")
	}
}

// TestWhatIsStoredIsTheStableIDAndNotTheIndex 钉住「用稳定 ID 不用下标」。
func TestWhatIsStoredIsTheStableIDAndNotTheIndex(t *testing.T) {
	paths := copies(t, 2)
	a, path := playbackMount(t)

	// 一个本地的、一个在线的（在线的那一个只落盘，不取流）。
	a.Queue().Append(items(paths[0])[0])
	a.Queue().Append(app.Item{
		Source: app.SourceOnline, Type: "mv", Bvid: "BV1Test", Title: "在线那一首",
	})
	a.SavePlayback()
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}

	snap := playbackStored(t, path)
	if len(snap.Items) != 2 {
		t.Fatalf("落盘了 %d 项，want 2", len(snap.Items))
	}
	if want := "local:" + paths[0]; snap.Items[0].ID != want {
		t.Errorf("第一项的标识是 %q，want %q", snap.Items[0].ID, want)
	}
	if want := "mv:BV1Test"; snap.Items[1].ID != want {
		t.Errorf("第二项的标识是 %q，want %q", snap.Items[1].ID, want)
	}
	// 显示用的字段也一起存：恢复出来的队列在抽屉里要画得出歌名。
	if snap.Items[1].Title != "在线那一首" {
		t.Errorf("第二项的歌名是 %q，want %q", snap.Items[1].Title, "在线那一首")
	}
}

// TestTheCurrentItemIsFoundByIdentityAndNotByIndex 钉住「当前项也是按标识找的」。
//
// 这一条和「守卫」是一起来的：存的时候队列里有三条，中间那条的文件后来没了，
// 恢复的时候它被跳过——**下标全变了**。要是恢复按下标找，指着的就不是上次
// 听到的那一首了。
func TestTheCurrentItemIsFoundByIdentityAndNotByIndex(t *testing.T) {
	paths := copies(t, 3)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0], paths[1], paths[2])...); err != nil {
		t.Fatal(err)
	}
	if err := a.PlayAt(2); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停

	// 中间那条的文件没了（用户删了它，或者换了机器）。
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}

	b := playbackRestart(t, a, path)
	if err := b.RestorePlayback(); err != nil {
		t.Fatal(err)
	}

	got := b.Queue().Items()
	if len(got) != 2 {
		t.Fatalf("恢复出来的队列有 %d 项，want 2（文件没了的那一条被跳过了）", len(got))
	}
	if got[0].Path != paths[0] || got[1].Path != paths[2] {
		t.Fatalf("恢复出来的队列是 %q / %q，want %q / %q",
			got[0].Path, got[1].Path, paths[0], paths[2])
	}
	// 当前项是**按标识**找的：它原来在下标 2，跳过一条之后变成下标 1。
	cur, ok := b.Queue().Current()
	if !ok {
		t.Fatal("恢复之后队列里没有当前项")
	}
	if cur.Path != paths[2] {
		t.Errorf("恢复出来的当前项是 %q，want %q（要按标识找，不是按下标）", cur.Path, paths[2])
	}
}

// TestARestoredEntryThatCannotBeResolvedIsSkippedQuietly 是「恢复有守卫」那一条：
// 认不出来的那一条安静跳过，整个恢复不崩。
func TestARestoredEntryThatCannotBeResolvedIsSkippedQuietly(t *testing.T) {
	paths := copies(t, 1)
	a, path := playbackMount(t)

	a.Queue().Append(items(paths[0])[0])
	// 三条认不出来的：前缀不对的、空的、本地路径没了的。
	a.Queue().Append(app.Item{Source: app.SourceOnline, Type: "mv", Title: "空的 bvid"})
	a.Queue().Append(app.Item{Source: app.SourceOnline, Type: "audio", Title: "空的 sid"})
	a.SavePlayback()
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}

	// 手改设置文件，塞一条认不出来的进去（模拟别人写的 / 手改过的文件）。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	as := doc["appSettings"].(map[string]any)
	pb := as["playback"].(map[string]any)
	pb["items"] = []any{
		map[string]any{"id": "wat:不是这三种"},
		map[string]any{"id": "local:" + paths[0]},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}

	b := playbackRestart(t, a, path)
	if err := b.RestorePlayback(); err != nil {
		t.Fatalf("恢复不该报错：%v", err)
	}
	got := b.Queue().Items()
	if len(got) != 1 {
		t.Fatalf("恢复出来的队列有 %d 项，want 1（认不出来的那条被跳过了）", len(got))
	}
	if got[0].Path != paths[0] {
		t.Errorf("剩下那一项是 %q，want %q", got[0].Path, paths[0])
	}
}

// TestTheVolumeAndTheQueueAreRestoredFromTheSameWrite 是文件头那张表的机器版本：
// 「用户刚拖过队列、又改了音量、又切了歌，然后退出」——下次启动恢复出来的必须
// 是最后那个状态，不是某一处的旧值。
func TestTheVolumeAndTheQueueAreRestoredFromTheSameWrite(t *testing.T) {
	paths := copies(t, 2)
	a, path := playbackMount(t)

	// 用户动过音量。
	a.SetVolume(0.25)
	// 用户拖过队列（换过顺序、加过一首）。
	if err := a.PlayFrom(items(paths[0], paths[1])...); err != nil {
		t.Fatal(err)
	}
	// 用户切了歌。
	if err := a.PlayAt(1); err != nil {
		t.Fatal(err)
	}
	a.Toggle()

	b := playbackRestart(t, a, path)
	if err := b.RestorePlayback(); err != nil {
		t.Fatal(err)
	}

	// 音量是用户最后动的那一档。
	if got := b.Snapshot().Volume; got != 0.25 {
		t.Errorf("恢复出来的音量是 %v，want 0.25", got)
	}
	// 队列是用户最后拖的那一份。
	got := b.Queue().Items()
	if len(got) != 2 {
		t.Fatalf("恢复出来的队列有 %d 项，want 2", len(got))
	}
	cur, ok := b.Queue().Current()
	if !ok {
		t.Fatal("恢复之后队列里没有当前项")
	}
	if cur.Path != paths[1] {
		t.Errorf("恢复出来的当前项是 %q，want %q（要的是用户最后切的那首）", cur.Path, paths[1])
	}
}

// TestQuittingForcesTheQueueToBeSaved 钉住「退出时强制落一次」：
// 防抖窗口里退出，队列状态不能丢。
func TestQuittingForcesTheQueueToBeSaved(t *testing.T) {
	paths := copies(t, 1)
	a, path := playbackMount(t)

	// 改了队列，**不等防抖窗口**，直接退出。
	a.Queue().Append(items(paths[0])[0])

	// 退出（和界面层关窗口时做的是同一串，见 internal/ui/shell_window.go 的 Close）。
	a.Close()
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	snap := playbackStored(t, path)
	if len(snap.Items) != 1 {
		t.Fatalf("退出时落盘的队列有 %d 项，want 1", len(snap.Items))
	}
	if want := "local:" + paths[0]; snap.Items[0].ID != want {
		t.Errorf("落盘的标识是 %q，want %q", snap.Items[0].ID, want)
	}
}

// TestTheRestoredOnlineItemIsRestoredQuietly 是在线那一项的恢复：
// 没有取流零件（没挂登录 / 没网）时**安静地跳过**，不崩、不出声。
func TestTheRestoredOnlineItemIsRestoredQuietly(t *testing.T) {
	a, path := playbackMount(t)

	a.Queue().Append(app.Item{
		Source: app.SourceOnline, Type: "mv", Bvid: "BV1Test", Title: "在线那一首",
	})
	a.SavePlayback()
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}

	b := playbackRestart(t, a, path)
	if err := b.RestorePlayback(); err != nil {
		t.Fatalf("恢复不该报错：%v", err)
	}
	// 队列恢复出来了（这是「队列顺序落盘了」那一半）。
	got := b.Queue().Items()
	if len(got) != 1 {
		t.Fatalf("恢复出来的队列有 %d 项，want 1", len(got))
	}
	if got[0].Identity() != "mv:BV1Test" {
		t.Errorf("恢复出来的那一项是 %q，want mv:BV1Test", got[0].Identity())
	}
	// 取不到流：安静跳过——没有播放器，也没有一句红字。
	st := b.Snapshot()
	if st.HasTrack {
		t.Error("取不到流却把播放器装上了")
	}
	if st.Err != "" {
		t.Errorf("恢复失败时冒了一句红字：%q（应该是安静跳过）", st.Err)
	}
	if st.Loading {
		t.Error("恢复失败之后还停在「正在取流」那一档")
	}
}

// TestThePositionIsClampedToTheTrackLength 钉住「位置超了怎么办」：
// 存的时候是上一首歌的长度，歌换了之后那个位置就超了——要夹住，不是崩。
func TestThePositionIsClampedToTheTrackLength(t *testing.T) {
	paths := copies(t, 1)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	a.ScrubEnd(0.9)
	a.Toggle()
	a.SavePlayback()
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}

	// 手改设置文件，把位置写成 10 小时。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	as := doc["appSettings"].(map[string]any)
	as["playback"].(map[string]any)["positionMs"] = int64(10 * 3600 * 1000)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}

	b := playbackRestart(t, a, path)
	if err := b.RestorePlayback(); err != nil {
		t.Fatalf("恢复不该报错：%v", err)
	}
	st := b.Snapshot()
	if st.Position > st.Duration {
		t.Errorf("恢复出来的位置 %v 超出了时长 %v", st.Position, st.Duration)
	}
	if st.Position <= 0 {
		t.Errorf("恢复出来的位置是 %v，want > 0", st.Position)
	}
}

// TestNothingToRestoreLeavesTheAppAlone 是一次没有「上次听到哪儿」的应用：
// 恢复不该动任何东西。
func TestNothingToRestoreLeavesTheAppAlone(t *testing.T) {
	a, path := playbackMount(t)
	if err := a.RestorePlayback(); err != nil {
		t.Fatalf("恢复不该报错：%v", err)
	}
	if got := a.Queue().Len(); got != 0 {
		t.Errorf("恢复之后队列里有 %d 项，want 0", got)
	}
	st := a.Snapshot()
	if st.HasTrack || st.Playing || st.Position != 0 {
		t.Errorf("恢复之后播放状态变了：%+v", st)
	}
	// 时间那一栏别被写坏：位置是 0，存回去也该是 0。
	a.SavePlayback()
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	snap := playbackStored(t, path)
	if snap.Position != 0 {
		t.Errorf("空队列落盘的位置是 %v，want 0", snap.Position)
	}
}

// TestThePositionIsKeptWhilePlaying 是「位置在放的时候也在落」那一条：
// 播了几秒之后退出，落盘的位置不是 0。
func TestThePositionIsKeptWhilePlaying(t *testing.T) {
	paths := copies(t, 1)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	// 等它真的走起来（位置 > 0）。
	waitFor(t, 3*time.Second, "位置开始往前跑", func() bool {
		return a.Snapshot().Position > 0
	})

	a.Close()
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	snap := playbackStored(t, path)
	if snap.Position <= 0 {
		t.Errorf("退出时落盘的位置是 %v，want > 0", snap.Position)
	}
}

// ---- 复审提的那几处的回归保护 ----

// TestPausedDoesNotWriteEveryTick 钉住「暂停时不每一拍都落盘」。
//
// 落盘是往整个设置文件里写一遍，而设置存储的防抖是「每次改动都把 120ms 的
// 窗口往后推」——落得太勤就把那个窗口一直顶掉，后果有两个：
//
//   - 界面被叫醒 20 次/秒（一次叫醒 = 一帧）；
//   - **暂停期间用户改的任何设置都不落盘**（直到退出那一次才写）。
//
// 这一条量的是第一件，第二件由 TestASettingChangeWhilePausedStillLandsOnDisk
// 量。
func TestPausedDoesNotWriteEveryTick(t *testing.T) {
	paths := copies(t, 1)
	a, _ := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停

	// 数 2 秒里界面被叫醒几次（一次叫醒 = 一帧）。
	var wakes int
	a.SetWake(func() { wakes++ })
	time.Sleep(2 * time.Second)
	if wakes > 5 {
		t.Errorf("暂停 2 秒里界面被叫醒了 %d 次，want ≤ 5（每一拍落一次盘就是 40 次）", wakes)
	}
}

// TestASettingChangeWhilePausedStillLandsOnDisk 钉住「暂停期间改的设置要落盘」。
//
// 那一格每一拍写一次的话，设置存储的防抖窗口会被一直顶掉，文件一次也不落——
// 于是用户暂停着把音量调小，重启之后又是 100%。
//
// ⚠️ **读的是盘上那份，不是内存里那份**：内存里那份是对的（本票那个错法也是
// 内存里对的、盘上不落）。
func TestASettingChangeWhilePausedStillLandsOnDisk(t *testing.T) {
	paths := copies(t, 1)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停
	// 等那一格自己落一次（它一落就把设置存储的防抖窗口顶掉了）。
	time.Sleep(700 * time.Millisecond)

	a.SetVolume(0.25)
	// ⚠️ **不显式 flush**：要验的就是「设置存储自己的防抖窗口有没有被顶掉」。
	time.Sleep(1500 * time.Millisecond)

	got := playbackStoredSettings(t, path)["volume"]
	f, ok := got.(float64)
	if !ok {
		t.Fatalf("落盘的 volume 是 %v（%T），want 一个数", got, got)
	}
	if f != 0.25 {
		t.Errorf("落盘的音量是 %v，want 0.25（暂停期间改的设置必须落盘）", f)
	}
}

// TestAScrubWhilePlayingIsSavedPromptly 钉住那张表里「用户动了一下就尽快写」。
//
// 拖了进度条之后不用等 5 秒那个窗口：落盘走的是「尽快写」那条路（它自己的
// 下限间隔是半秒；判据放宽到 3 秒，理由见下面那段注释）。
func TestAScrubWhilePlayingIsSavedPromptly(t *testing.T) {
	paths := copies(t, 1)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, "位置开始往前跑", func() bool {
		return a.Snapshot().Position > 0
	})
	a.ScrubEnd(0.8) // 4 秒的歌 → 3.2 秒

	// 半秒之内就该落盘（那一格走的是「尽快写」那条路）。
	//
	// ⚠️ **等条件，不等死时间**：这一格是后台时钟那一拍写的（tick 每 50ms
	// 一次，见 app.go 的 tick），机器忙的时候那一拍会被拖后——这里原来写死
	// 700ms，满载时飘过（2026-10-25 在板子上：落盘的位置还是拖之前的 53ms）。
	// 给死时间量到的是「当天机器有多闲」，不是「走的是哪条路」——这一族等
	// 异步的东西一律等条件（见 app_test.go 的 waitFor）。
	//
	// 3 秒是**判据**不是时间预算：那一格的下限间隔是半秒（下一拍最多再晚
	// 50ms），3 秒给机器留了余量；而 5 秒那个窗口是「只是歌在往前走」才走
	// 的路——3 秒内落下来，就说明走的是「尽快写」那条。
	//
	// （没直接用 `waitFor`：它失败时只说「还没发生」，这里还要把**落到了多少**
	// 一起说出来——见下面那句报错。）
	var pos int64
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := playbackStoredRaw(path); err == nil {
			pos = s.Position
			if pos >= 2000 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pos < 2000 {
		t.Errorf("落盘的位置是 %d ms，want ≥ 2000（拖完就该落，不用等 5 秒那个窗口）", pos)
	}
}

// TestARestoredLocalFileThatCannotBeDecodedIsSkippedQuietly 钉住「安静跳过」。
//
// 「文件在、但解不开」也是「取不到」：用户换了个机器、文件被别的东西改了、
// 编码是这台机器解不了的。启动时冒一句红字是应用在说自己，不是在说给用户听。
//
// （在线那一首取不到流是安静的，见
// TestTheRestoredOnlineItemIsRestoredQuietly。）
func TestARestoredLocalFileThatCannotBeDecodedIsSkippedQuietly(t *testing.T) {
	paths := copies(t, 1)
	// 一个「在、但解不开」的文件。
	bad := filepath.Join(t.TempDir(), "bad.flac")
	if err := os.WriteFile(bad, []byte("这根本不是音频"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, path := playbackMount(t)
	// 当前项就是那个解不开的。
	//
	// ⚠️ **用 `Move` 选中它，不去放它**：放它会立刻报错（那是对的——用户自己
	// 点了一首放不出来的歌，就该有一句人话）。
	a.Queue().Append(app.LocalItem(bad), items(paths[0])[0])
	if _, ok := a.Queue().Move(0); !ok {
		t.Fatal("选不中队列里那一项")
	}
	a.SavePlayback()
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}

	b := playbackRestart(t, a, path)
	if err := b.RestorePlayback(); err != nil {
		t.Fatalf("恢复不该报错：%v", err)
	}
	st := b.Snapshot()
	if st.Err != "" {
		t.Errorf("恢复失败时冒了一句红字：%q（应该是安静跳过）", st.Err)
	}
	if st.HasTrack {
		t.Error("解不开却把播放器装上了")
	}
}

// TestTheRestoreIsAnnounced 钉住「恢复出来的那一句会真的送出去」。
//
// 那句话是为**读屏软件用户**准备的（恢复出来的那一首在屏幕上是看得见的，但
// 读屏软件用户得先找到播放条）。它不是死代码：恢复那一趟真的会调 `announce`。
//
// ⚠️ **句柄是界面层装的**（`internal/ui/shell.go` 的 `New` 里 `AddAnnounce`），
// 所以恢复必须排在那个外壳建好之后——`ui.Run` 里就是这么排的（见那里那一句）。
func TestTheRestoreIsAnnounced(t *testing.T) {
	paths := copies(t, 1)
	a, path := playbackMount(t)

	if err := a.PlayFrom(items(paths[0])...); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停

	b := playbackRestart(t, a, path)
	var said []string
	b.AddAnnounce(func(text string) { said = append(said, text) })
	if err := b.RestorePlayback(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range said {
		if strings.Contains(s, "已恢复上次的位置") {
			found = true
		}
	}
	if !found {
		t.Errorf("恢复出来的那一句没有送出去（送出去的是 %q）", said)
	}
}
