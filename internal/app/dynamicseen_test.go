// 「动态看到哪儿了」那一格：落盘、按账号分家、只往前挪。
//
// 这一格是红点那一摊的**记忆**：没有它，「用户看过了」这件事只活在内存里，
// 于是用户点掉那个点之后，五分钟它又回来（上游 Biu 就是这样），而重启之后
// 判定又是另一回事。
//
// ⚠️ **基准是一条动态的 id**（B 站的 update_baseline 要的就是那个，不是什么
// 时间戳）——见 internal/app/dynamicseen.go 的文件头。
package app

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
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

// 两条动态的 id（B 站的动态 id 是十进制的雪花那一类，越大越新）。
const (
	seenIDOld = "954659517801431040"
	seenIDNew = "954659517801431041"
)

func TestCatchUpDynamicSeenRejectsCompletionAfterAccountChange(t *testing.T) {
	b := newExploreBili(t)
	started := make(chan struct{})
	release := make(chan struct{})
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		writeExploreJSON(w, 0, map[string]any{
			"items": []any{}, "offset": "", "has_more": false,
			"update_baseline": seenIDNew,
		})
	})
	a := newExploreApp(t, b)
	if err := a.MountSettings(filepath.Join(t.TempDir(), SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	loginExplore(a, "77")

	done := make(chan error, 1)
	go func() {
		_, err := a.CatchUpDynamicSeen(context.Background())
		done <- err
	}()
	<-started
	loginExplore(a, "88")
	close(release)

	if err := <-done; err != ErrStaleGeneration {
		t.Fatalf("stale catch-up returned %v, want ErrStaleGeneration", err)
	}
	loginExplore(a, "77")
	if got := a.DynamicBaseline(); got != "" {
		t.Fatalf("stale catch-up wrote account A baseline %q after switching accounts", got)
	}
	loginExplore(a, "88")
	if got := a.DynamicBaseline(); got != "" {
		t.Fatalf("stale catch-up wrote account B baseline %q", got)
	}
}

// ⚠️ **不落盘的话每次启动的判定都是乱的**：重启之后基准回到空串，于是问 B 站
// 的时候问的是「从头算」，那个点就一直亮着——而用户明明看过了。
func TestTheDynamicBaselineSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), SettingsFileName)

	a := newSeenApp(t, path, "77")
	a.MarkDynamicSeen(seenIDOld)
	if got := a.DynamicBaseline(); got != seenIDOld {
		t.Fatalf("记完之后基线是 %q，want %q", got, seenIDOld)
	}
	// 落盘要真的落到盘上（设置存储是后台合并写的）。
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	b := newSeenApp(t, path, "77")
	if got := b.DynamicBaseline(); got != seenIDOld {
		t.Errorf("重启之后基线是 %q，want %q（不落盘的话每次启动的判定都是乱的）",
			got, seenIDOld)
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
	a.MarkDynamicSeen(seenIDOld)

	// 换账号：那是另一份「没看过的动态」，从头算。
	loginExplore(a, "88")
	if got := a.DynamicBaseline(); got != "" {
		t.Errorf("换账号之后基线是 %q，want 空（那是另一份「没看过」）", got)
	}

	// 记 B 的，再切回 A：A 的基准**还在**。
	a.MarkDynamicSeen(seenIDNew)
	loginExplore(a, "77")
	if got := a.DynamicBaseline(); got != seenIDOld {
		t.Errorf("切回 A 之后基线是 %q，want %q（A 的基准不该被 B 冲掉）", got, seenIDOld)
	}
	// B 的那份也还在。
	loginExplore(a, "88")
	if got := a.DynamicBaseline(); got != seenIDNew {
		t.Errorf("切回 B 之后基线是 %q，want %q", got, seenIDNew)
	}
}

// TestTheDynamicBaselineOnlyMovesForward 验基准**只往前挪**。
//
// 动态 id 是雪花那一类的（越大越新），所以比一比就知道新的那个是不是更大。
// 倒退一步（两趟请求回来的顺序反了、或者用户把机器的时间调回去了）会让
// 「已经看过的」又变成「没看过」，那个点会诈尸。
func TestTheDynamicBaselineOnlyMovesForward(t *testing.T) {
	a := newSeenApp(t, filepath.Join(t.TempDir(), SettingsFileName), "77")
	a.MarkDynamicSeen(seenIDNew)
	a.MarkDynamicSeen(seenIDOld)
	if got := a.DynamicBaseline(); got != seenIDNew {
		t.Errorf("基线退回了 %q，want %q（只往前挪）", got, seenIDNew)
	}
}

// TestReadingTheBaselineWhileItIsWrittenDoesNotRace 验「读基准」和「记基准」并发时
// **不是数据竞争**。
//
// ⚠️ **这一条的牙齿在 `-race` 上**：设置存储的 `Snapshot()` 给的是**浅拷贝**
// （切片头共用同一个底层数组），而锁在返回时就放了——所以锁外读那些元素的时候，
// 另一个 goroutine 在锁里**就地**改同一个数组，就是一次数据竞争（`-race` 直接报
// `WARNING: DATA RACE`）。
//
// 而真机上正是这样并发的：轮询那一趟在**后台 goroutine** 里读它
// （`internal/ui/navbar_feed.go` 的 ask），而「动态页拉到第一页」在**界面线程**
// 上写它、「点那个点时的 catchUp」在**后台 goroutine** 里写它。
//
// 治法是**换头不就地改**（改一份新数组，外面那些读者读的还是旧数组，而旧数组从
// 那一刻起再没人改）——这也是这个包里别的键的写法（`SideMenuCollectionOrder.
// setList`、`normalize` 都是换头）。
func TestReadingTheBaselineWhileItIsWrittenDoesNotRace(t *testing.T) {
	a := newSeenApp(t, filepath.Join(t.TempDir(), SettingsFileName), "77")

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = a.DynamicBaseline()
		}
	}()

	for i := 0; i < 50; i++ {
		// ⚠️ **每次都是更新的那一条**：写要真的改到元素上（只是换头的话，
		// 「往回退」那一支就永远走不到，而那一支正是会读错别人的那种）。
		a.MarkDynamicSeen(fmt.Sprintf("%018d", 954659517801431000+i))
	}
	close(stop)
	<-done

	if got := a.DynamicBaseline(); got == "" {
		t.Error("并发写完之后基准是空的")
	}
}

// TestTheDynamicBaselineIsEmptyBeforeAnythingIsSeen 验「一次都没看过」是空的。
//
// 空串 → 问 B 站的时候传 0，那一档是「从头算」——用户第一次打开就能看见那个点
// （上游 Biu 传的就是 0）。
func TestTheDynamicBaselineIsEmptyBeforeAnythingIsSeen(t *testing.T) {
	a := newSeenApp(t, filepath.Join(t.TempDir(), SettingsFileName), "77")
	if got := a.DynamicBaseline(); got != "" {
		t.Errorf("没看过的时候基线是 %q，want 空", got)
	}
}
