// 下载这一段的测试：进度那四个数、**增量回退的兜底**、以及下载中途退出应用
// （SPEC 118）。
//
// 和 `update_test.go` 分开是因为它们盯的不是同一件事：那边盯「查」——查到了什么、
// 说了什么、存住了没有；这边盯「下」——下到哪儿了、什么时候换成了整个包、以及
// 中途退出之后盘上留下了什么。

package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// 进度：速度 / 已传 / 总量 / 百分比
//
// 这四条是这一票的验收词。它们全在 `UpdateState` 里，而**两位小数那一层留给
// 界面写**——所以这一层断言的是那个原始的百分比，界面的测试断言写出来的那串字。

// 断的是：每一项都对得上字节数，速度是真按时间算出来的，百分比不是整数除法。
func TestTheProgressCarriesBytesSpeedAndPercent(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	var mid UpdateState
	f.be.mu.Lock()
	f.be.install = func(_ context.Context, progress func(int64, int64)) error {
		f.at(100 * time.Millisecond)
		emit(progress, 1<<20, 8<<20) // 1 MB / 8 MB
		f.at(300 * time.Millisecond)
		emit(progress, 3<<20, 8<<20) // 3 MB / 8 MB
		// ⚠️ 快照是在**下载中**取的：那才是界面上真正在画的那一份。下完了之后
		// 界面上换的是「安装包已下载完成」，速度不再有意义，而这一层会把采样点
		// 清掉——拿「下完之后」的那一份来断言速度，验的是一个没有人看的数。
		mid = f.snapshot()
		return nil
	}
	f.be.mu.Unlock()

	f.app.StartUpdateDownload()
	<-f.be.installDone

	st := mid
	if st.Phase != UpdateDownloading {
		t.Fatalf("下载中的相位是 %v，要 UpdateDownloading", st.Phase)
	}
	if st.Transferred != 3<<20 || st.Total != 8<<20 {
		t.Errorf("字节是 %d / %d，要 %d / %d", st.Transferred, st.Total, 3<<20, 8<<20)
	}
	// 2 MB / 200 ms = 10.48576 MB/s（首末两个采样点之间）。
	want := int64(2<<20) * 5
	if diff := st.Speed - want; diff > want/100+1 || diff < -(want/100+1) {
		t.Errorf("速度是 %d B/s，要约 %d B/s", st.Speed, want)
	}
	if st.Percent != 37.5 {
		t.Errorf("百分比是 %v，要 37.5", st.Percent)
	}
	if st.FellBack {
		t.Error("一次正常的下载被当成了回退全量")
	}
	// 下完之后是 Ready：界面上换的是「退出并安装更新」。
	if got := f.snapshot().Phase; got != UpdateReady {
		t.Errorf("下完之后的相位是 %v，要 UpdateReady", got)
	}
}

// 速度只用一个采样点的时候是 0 —— 别拿「0 B/s」当成卡住了。
func TestTheSpeedNeedsTwoSamplesBeforeItSaysAnything(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	release := make(chan struct{})
	f.be.mu.Lock()
	f.be.install = func(_ context.Context, progress func(int64, int64)) error {
		emit(progress, 1024, 1<<20)
		<-release
		return nil
	}
	f.be.mu.Unlock()

	f.app.StartUpdateDownload()
	waitFor(t, func() bool { return f.snapshot().Transferred == 1024 }, "第一次进度")
	if got := f.snapshot().Speed; got != 0 {
		t.Errorf("一个采样点就报速度 %d", got)
	}
	close(release)
	<-f.be.installDone
}

// ─────────────────────────────────────────────────────────────────────────────
// 增量回退：**这一票的已知问题**，不许藏

// 增量补丁没能把新版本拼出来，框架改下整个归档。
//
// 断的是三件用户能看见的事：`FellBack` 为真（界面据此说一句话）、进度**从新
// 的那一趟算**（不是接着旧的算，否则百分比会大于 100）、以及速度从头算（不
// 是拿两趟的字节差除时间，那是个假速度）。
//
// ⚠️ 这一支是实测出来的行为（dmg 装来的应用第一次增量最容易走到），所以它有
// 自己的测试钉着，不是「理论上不会发生」。
func TestADeltaThatFailsFallsBackToTheWholeArchiveAndSaysSo(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	const delta = 2 << 20 // 增量包 2 MB
	const whole = 64 << 20
	var mid UpdateState
	f.be.mu.Lock()
	f.be.install = func(_ context.Context, progress func(int64, int64)) error {
		// 增量包下到一半——甚至快下完了。
		f.at(100 * time.Millisecond)
		emit(progress, delta/2, delta)
		f.at(200 * time.Millisecond)
		emit(progress, delta, delta)
		// 框架改下整个归档：total 变了、downloaded 从头开始。
		f.at(300 * time.Millisecond)
		emit(progress, whole/10, whole)
		f.at(400 * time.Millisecond)
		emit(progress, whole/4, whole)
		// 同上：在**下载中**取快照，那才是界面上正在画的那一份。
		mid = f.snapshot()
		return nil
	}
	f.be.mu.Unlock()

	f.app.StartUpdateDownload()
	<-f.be.installDone

	st := mid
	if !st.FellBack {
		t.Fatal("增量改下全量了，FellBack 还是 false —— 界面就不会说那句话")
	}
	if st.Total != whole || st.Transferred != whole/4 {
		t.Errorf("字节是 %d / %d，要 %d / %d", st.Transferred, st.Total, whole/4, whole)
	}
	// 百分比回到 25 附近，**不是** 100+：两趟的数字不能串起来。
	if st.Percent < 24.9 || st.Percent > 25.1 {
		t.Errorf("百分比是 %v，要 25 附近（串成两趟的话会 >100）", st.Percent)
	}
	// 速度按**这一趟**算：whole/4 - whole/10，落在 300ms 和 400ms 之间（100ms）。
	want := int64(whole/4-whole/10) * 10
	if diff := st.Speed - want; diff > want/50+1 || diff < -(want/50+1) {
		t.Errorf("速度是 %d B/s，要约 %d B/s（跨两趟算就是个假数）", st.Speed, want)
	}
	if st.Phase != UpdateDownloading {
		t.Errorf("下载中的相位是 %v，要 UpdateDownloading", st.Phase)
	}
	if got := f.snapshot().Phase; got != UpdateReady {
		t.Errorf("下完之后的相位是 %v，要 UpdateReady", got)
	}
}

// 一个**普通**的下载第一个采样点也有 total —— 它不是回退。
//
// 这一条是上一条的反面，断的是「每一次下载都弹一句『增量没拼出来』」这个坏
// 毛病：一句每次都错的话比不说更糟，它会让用户对整个更新功能失去信任。
func TestTheFirstSampleOfADownloadIsNotAFallback(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	const whole = 40 << 20
	f.be.mu.Lock()
	f.be.install = func(_ context.Context, progress func(int64, int64)) error {
		emit(progress, 0, whole)
		emit(progress, whole/2, whole)
		return nil
	}
	f.be.mu.Unlock()

	f.app.StartUpdateDownload()
	<-f.be.installDone

	if got := f.snapshot().FellBack; got {
		t.Error("一次从头到尾 total 都没变的下载被当成了回退")
	}
}

// 下载中途又从头开始（总量没变、字节倒回去了）也算回退。
//
// 认的是「这一趟重新开始了」这个**现象**，不是「总量变了」那一个条件：框架换了
// 实现的话先换总量还是先退字节都不一定，而用户看见的都是同一件事——进度条倒退。
func TestARestartWithTheSameTotalIsStillAFallback(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	const whole = 40 << 20
	f.be.mu.Lock()
	f.be.install = func(_ context.Context, progress func(int64, int64)) error {
		emit(progress, whole/2, whole)
		emit(progress, 0, whole)
		emit(progress, whole/2, whole)
		return nil
	}
	f.be.mu.Unlock()

	f.app.StartUpdateDownload()
	<-f.be.installDone

	if got := f.snapshot().FellBack; !got {
		t.Error("字节倒回去了却没认成回退")
	}
}

// 百分比夹得住：字节超过总量时是 100，不是 100+。
//
// 不是因为「不可能发生」，而是因为进度条画出去会满到框外面去，而那种一帧的
// 畸形会被用户记成「这个应用有问题」。
func TestThePercentIsClampedToTheBox(t *testing.T) {
	for _, c := range []struct{ transferred, total int64 }{
		{0, 0},             // 还不知道大小
		{1, 0},             // 还不知道大小，但已经有字节了
		{5, 10},            // 一半
		{-1, 10},           // 不可能，但别让它变成 -10
		{11, 10},           // 超了
		{1 << 40, 1 << 20}, // 差了十万倍
	} {
		p := percentOf(c.transferred, c.total)
		if p < 0 || p > 100 {
			t.Errorf("percentOf(%d, %d) = %v，跑到 [0,100] 外了", c.transferred, c.total, p)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 下载中退出应用（SPEC 118）

// 下载到一半退出：应用没被动过、没有重启、留下的那份文件是完整且自洽的。
//
// 断的是四件事，而每一件对应一种真实的损坏：
//
//  1. `Relaunch` 没被调（调了就是「退到一半又起来了」，用户看见应用自己弹回来）
//  2. 磁盘上那份文件**只有那两个键**——没有一个「正在下 / 下到一半」的字段，
//     所以中途退出之后读回来的东西不可能是半截的
//  3. 取消之后 `Install` 返回的 ctx 错**没有**被当成失败（说「下载失败」是撒谎：
//     磁盘上那份应用一个字节都没动）
//  4. 相位回到「有更新可看」，下一次进设置页还看得见
func TestQuittingWhileDownloadingLeavesNothingBroken(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	started := make(chan struct{})
	cancelled := make(chan struct{})
	f.be.mu.Lock()
	f.be.install = func(ctx context.Context, progress func(int64, int64)) error {
		defer close(cancelled)
		close(started)
		emit(progress, 4<<20, 64<<20)
		// 模拟一个肯等 ctx 的下载：退出时被取消才返回。
		<-ctx.Done()
		return ctx.Err()
	}
	f.be.mu.Unlock()

	f.app.StartUpdateDownload()
	<-started
	f.app.Close() // 用户退出应用

	<-cancelled
	if n := f.be.relaunched(); n != 0 {
		t.Errorf("退出的时候重起了 %d 次进程", n)
	}
	if got := f.app.UpdateSnapshot().Phase; got != UpdateAvailable {
		t.Errorf("退出后的相位是 %v，要回到 UpdateAvailable（不能停在「下载中」）", got)
	}

	// 落盘的那份：能读、是合法 JSON、只有那两个键。
	b, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatalf("读 %s：%v", f.path, err)
	}
	var file map[string]any
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatalf("落盘的那份不是合法 JSON：%v\n%s", err, b)
	}
	for k := range file {
		switch k {
		case "schemaVersion", "lastCheck", "knownVersion":
		default:
			t.Errorf("落盘的那份里有 %q 这一键——那多半是一个没被设计过的状态", k)
		}
	}
	if got, _ := file["knownVersion"].(string); got != "0.2.0" {
		t.Errorf("knownVersion 是 %q，要 0.2.0", got)
	}
	// 重启一次读回来：还是「有更新可看」，没有半截下载的残留。
	again := New()
	t.Cleanup(again.Close)
	again.MountUpdates(f.be, f.path)
	if st := again.UpdateSnapshot(); st.Phase != UpdateAvailable || st.Transferred != 0 || st.Total != 0 {
		t.Errorf("重启之后 Phase=%v 已传=%d 总量=%d", st.Phase, st.Transferred, st.Total)
	}
}

// 正在查的时候退出：同样不留任何东西。
//
// 查一半的网络请求会被取消掉，而相位不许停在「正在查」——那三个字正是「等一会儿
// 就好」的一句话，而此刻已经没有什么会来了。
func TestQuittingWhileCheckingIsQuiet(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", nil)
	f.be.mu.Lock()
	f.be.holdCheck = true
	f.be.mu.Unlock()

	f.app.CheckUpdates() // 起一个后台 goroutine 去查
	waitFor(t, func() bool { return f.snapshot().Phase == UpdateChecking }, "没进「正在查」")

	f.app.Close()

	if got := f.snapshot().Phase; got == UpdateChecking {
		t.Error("退出之后相位还停在「正在查」")
	}
	if n := f.app.TakeUpdateNotice(); n != "" {
		t.Errorf("退出之后留了提示 %q", n)
	}
}

// 退出之后不再接新的检查：不会有一个后台 goroutine 在应用没了之后还回来投递状态。
//
// 这里**直接调内部那一条**而不是 `App.CheckUpdates`：后者是 `go u.check(true)`，
// 而那个 goroutine 什么时候落地没法同步——于是测的就不是「它有没有改状态」，
// 而是「调度有没有帮我赶在断言前面」，那是个假测试。
func TestNoCheckSurvivesTheQuit(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", nil)
	stop := f.app.StartAutoChecks()
	stop()
	before := f.snapshot()

	f.app.Close() // 退出应用；Close 顺带把后台那个定时器也停了

	f.app.mu.Lock()
	u := f.app.updates
	f.app.mu.Unlock()
	u.check(true) // 应用已经收了：这一趟必须立刻什么也不干

	after := f.snapshot()
	if after.Phase != before.Phase || after.Latest != before.Latest {
		t.Errorf("退出之后的检查改了状态：%v/%q → %v/%q",
			before.Phase, before.Latest, after.Phase, after.Latest)
	}
	if n := f.app.TakeUpdateNotice(); n != "" {
		t.Errorf("退出之后的检查留了提示 %q", n)
	}
}

// 装之前发现中间发了新的一版：退回「有更新可看」，并重查一次。
//
// 不重查的话界面上摆的还是那个旧版本，用户再按一次还是撞同一句——那是个死
// 循环，而用户看见的是「我明明点了下载，它就是不动」。
func TestInstallingAVersionThatJustGotSupersededGoesBackAndChecksAgain(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	f.be.mu.Lock()
	f.be.install = func(context.Context, func(int64, int64)) error { return ErrUpdateSuperseded }
	f.be.rel = &UpdateRelease{Version: "0.3.0"}
	f.be.mu.Unlock()

	f.app.StartUpdateDownload()
	<-f.be.installDone
	waitFor(t, func() bool { return f.snapshot().Latest == "0.3.0" }, "重查之后版本号没换")

	st := f.snapshot()
	if st.Phase != UpdateAvailable {
		t.Errorf("相位是 %v，要回到 UpdateAvailable", st.Phase)
	}
	if st.Err != "" {
		t.Errorf("这一趟不是失败，却留了一句 %q", st.Err)
	}
	if got := f.be.wanted(); len(got) != 1 || got[0] != "0.2.0" {
		t.Errorf("装的是 %v，要 0.2.0", got)
	}
}

// 装失败：原因留在状态里，界面上要能拿它去提 issue。
func TestAFailedInstallSaysWhy(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	f.be.mu.Lock()
	f.be.install = func(context.Context, func(int64, int64)) error {
		return errors.New("signature: verification failed")
	}
	f.be.mu.Unlock()

	f.app.StartUpdateDownload()
	<-f.be.installDone

	st := f.snapshot()
	if st.Phase != UpdateFailed {
		t.Fatalf("相位是 %v，要 UpdateFailed", st.Phase)
	}
	if !strings.Contains(st.Err, "verification failed") {
		t.Errorf("原因是 %q", st.Err)
	}
	// 失败之后还能再来一次：相位回到 Available 就能再按「重试下载」。
	f.app.DismissUpdate()
	if got := f.snapshot().Phase; got != UpdateIdle {
		t.Errorf("关掉弹窗之后相位是 %v，要 Idle", got)
	}
}

// 下到一半**不给关**：DismissUpdate 在下载中是一个空操作。
//
// 理由：进度条正在走，而这一关用户以为「取消了」，其实几十 MB 还在后台往磁盘上
// 写。Biu 那个弹窗也是这么写的（`isKeyboardDismissDisabled`）。
func TestTheDialogCannotBeDismissedWhileDownloading(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	hold := make(chan struct{})
	f.be.mu.Lock()
	f.be.install = func(ctx context.Context, progress func(int64, int64)) error {
		emit(progress, 1<<20, 64<<20)
		<-ctx.Done()
		return ctx.Err()
	}
	f.be.mu.Unlock()

	f.app.StartUpdateDownload()
	waitFor(t, func() bool { return f.snapshot().Phase == UpdateDownloading }, "没进下载中")

	f.app.DismissUpdate()
	if got := f.snapshot().Phase; got != UpdateDownloading {
		t.Errorf("下载中关掉了弹窗：相位变成 %v", got)
	}
	close(hold)
}

// 有更新可看的时候关掉弹窗：相位回到 Idle，但**那一版还记着**。
func TestDismissingAnAvailableUpdatePutsItBack(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, true)

	f.app.DismissUpdate()
	if got := f.snapshot().Phase; got != UpdateIdle {
		t.Fatalf("相位是 %v，要 Idle", got)
	}
	f.checkAndWait(t, true)
	if !f.snapshot().Available() {
		t.Error("重新查一次之后说有更新，却认不出来了")
	}
}

// 不能更新的那一种（开发构建 / 装在只读位置）：要有一句能照着做的话。
func TestABuildThatCannotUpdateSaysWhy(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", nil)
	f.be.mu.Lock()
	f.be.enabled = false
	f.be.mu.Unlock()

	f.app.mu.Lock()
	f.app.updates.enabled = false // Enabled() 是在挂上时问的那一次（见 MountUpdates）
	f.app.mu.Unlock()

	st := f.snapshot()
	if st.Enabled {
		t.Fatal("Enabled 说是")
	}
	if why := st.UpdateUnavailableReason(); why == "" {
		t.Error("不能更新，却没给一句原因")
	}
	if st.UpdateUnavailableReason() == (UpdateState{Enabled: true}).UpdateUnavailableReason() {
		t.Error("能更新的时候也说了同一句")
	}
}
