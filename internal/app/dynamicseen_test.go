// 「动态看到哪儿了」那一格：落盘、按账号分家、只往前挪。
//
// 这一格是红点那一摊的**记忆**：没有它，「用户看过了」这件事只活在内存里，
// 于是用户点掉那个点之后，五分钟它又回来（上游 Biu 就是这样），而重启之后
// 判定又是另一回事。
package app

import (
	"path/filepath"
	"testing"
	"time"
)

// newSeenApp 装一个带设置和登录态的 App。
//
// 登录走的是**真的**那条路（`Account.mgr.Adopt`，见 explore_test.go 的
// loginExplore），不是硬塞一个 bool——于是「SelfMid 能不能读出来」一并是真的。
func newSeenApp(t *testing.T, path, mid string) *App {
	t.Helper()
	a := New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	a.MountLogin(NewMemoryStore("seen-test"), nil)
	loginExplore(a, mid)
	return a
}

// TestTheDynamicBaselineSurvivesARestart 验「看到哪儿了」**落盘**。
//
// ⚠️ **不落盘的话每次启动的判定都是乱的**：重启之后基准回到 0，于是问 B 站
// 的时候问的是「从头算」，那个点就一直亮着——而用户明明看过了。
func TestTheDynamicBaselineSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), SettingsFileName)
	at := time.Unix(1792141200, 0)

	a := newSeenApp(t, path, "77")
	a.MarkDynamicSeen(at)
	if got := a.DynamicBaseline(); got != at.Unix() {
		t.Fatalf("记完之后基线是 %d，want %d", got, at.Unix())
	}
	// 落盘要真的落到盘上（设置存储是后台合并写的）。
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	b := newSeenApp(t, path, "77")
	if got := b.DynamicBaseline(); got != at.Unix() {
		t.Errorf("重启之后基线是 %d，want %d（不落盘的话每次启动的判定都是乱的）",
			got, at.Unix())
	}
}

// TestTheDynamicBaselineIsPerAccount 验基准是**按账号**的，而且**切回去还在**。
//
// 动态流是「我关注的人在发什么」，那是按账号的：换账号之后那是另一份
// 「没看过的动态」，基准不能接着上一个账号的用。
//
// ⚠️ **切回去也要在**：基准存成一张表（不是一个格子）——只存一格的话，切到 B
// 的时候就把 A 的基准冲掉了，切回 A 之后那个点又会按「全部没看过」亮起来，
// 而用户明明看过了。
func TestTheDynamicBaselineIsPerAccount(t *testing.T) {
	a := newSeenApp(t, filepath.Join(t.TempDir(), SettingsFileName), "77")
	at := time.Unix(1792141200, 0)
	a.MarkDynamicSeen(at)

	// 换账号：那是另一份「没看过的动态」，从头算。
	loginExplore(a, "88")
	if got := a.DynamicBaseline(); got != 0 {
		t.Errorf("换账号之后基线还是 %d，want 0（那是另一份「没看过」）", got)
	}

	// 记 B 的，再切回 A：A 的基准**还在**。
	bAt := time.Unix(1792142000, 0)
	a.MarkDynamicSeen(bAt)
	loginExplore(a, "77")
	if got := a.DynamicBaseline(); got != at.Unix() {
		t.Errorf("切回 A 之后基线是 %d，want %d（A 的基准不该被 B 冲掉）", got, at.Unix())
	}
	// B 的那份也还在。
	loginExplore(a, "88")
	if got := a.DynamicBaseline(); got != bAt.Unix() {
		t.Errorf("切回 B 之后基线是 %d，want %d", got, bAt.Unix())
	}
}

// TestTheDynamicBaselineOnlyMovesForward 验基准**只往前挪**。
//
// 用户把机器的时间调回去（或者时区换了）的时候，基线不能倒退——倒退一步，
// 「已经看过的」又变成「没看过」，那个点会诈尸。
func TestTheDynamicBaselineOnlyMovesForward(t *testing.T) {
	a := newSeenApp(t, filepath.Join(t.TempDir(), SettingsFileName), "77")
	later := time.Unix(1792141200, 0)
	earlier := time.Unix(1780000000, 0)

	a.MarkDynamicSeen(later)
	a.MarkDynamicSeen(earlier)
	if got := a.DynamicBaseline(); got != later.Unix() {
		t.Errorf("基线退回了 %d，want %d（只往前挪）", got, later.Unix())
	}
}

// TestTheDynamicBaselineIsZeroBeforeAnythingIsSeen 验「一次都没看过」是 0。
//
// 0 那一档是「从头算」——用户第一次打开就能看见那个点（上游 Biu 传的就是 0）。
func TestTheDynamicBaselineIsZeroBeforeAnythingIsSeen(t *testing.T) {
	a := newSeenApp(t, filepath.Join(t.TempDir(), SettingsFileName), "77")
	if got := a.DynamicBaseline(); got != 0 {
		t.Errorf("没看过的时候基线是 %d，want 0", got)
	}
}
