package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 更新这一摊的界面测试跑在**真画一帧**上：真设置页、真弹窗、真点按钮、真读画面
// 上的字。
//
// 断言的只有两类：**画面上有什么字**，和**按下去之后状态/那一格有没有变**。
// 不断言「调了哪个函数」——那只能证明有那个方法，证明不了界面上真有一个入口。
//
// ⚠️ 真打包的应用里 `mygo.Updater.Enabled()` 永远是 false（版本、公钥、更新源
// 都是 `mygo build` 用 -ldflags 塞进去的），所以这里换的是一个假的后端。真
// 那一层只有一个薄壳，见 update_backend.go。

// fakeUpdate 是不碰框架的假更新源。
type fakeUpdate struct {
	mu      sync.Mutex
	enabled bool
	version string
	rel     *app.UpdateRelease
	err     error
	// install 是「下载并装好」那件事。由测试自己写，用 emit 造进度。
	install func(ctx context.Context, progress func(downloaded, total int64)) error
	// gate 装着的时候 install 卡住，测试拿它控制「下载到一半」。
	gate chan struct{}
	// done 在 Install 返回之后被关掉。
	done chan struct{}
	// started 在 Install 刚进来时被关掉。
	started   chan struct{}
	relaunch  int
	wantVer   []string
	checkOnce sync.Once
	// checks 记着 Check 被调了几次。检查是**后台**跑的（`App.CheckUpdates` 里
	// `go u.check(true)`），而测试要确定地等它落地——光轮询相位是不行的：按下
	// 按钮的那一刻相位还是 Idle，「相位不是 Checking」当场就成立。
	checks int
}

func newFakeUpdate(version string, rel *app.UpdateRelease) *fakeUpdate {
	return &fakeUpdate{
		enabled: true, version: version, rel: rel,
		done: make(chan struct{}), started: make(chan struct{}),
	}
}

func (b *fakeUpdate) Enabled() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.enabled }
func (b *fakeUpdate) Version() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.version
}

func (b *fakeUpdate) Check(context.Context) (*app.UpdateRelease, error) {
	b.mu.Lock()
	b.checks++
	rel, err := b.rel, b.err
	b.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return rel, nil
}

// checksDone 记着查过几次。
func (b *fakeUpdate) checksDone() int { b.mu.Lock(); defer b.mu.Unlock(); return b.checks }

func (b *fakeUpdate) Install(ctx context.Context, want app.UpdateRelease, progress func(int64, int64)) error {
	b.mu.Lock()
	b.wantVer = append(b.wantVer, want.Version)
	install := b.install
	b.mu.Unlock()

	b.checkOnce.Do(func() { close(b.started) })
	defer func() {
		select {
		case <-b.done:
		default:
			close(b.done)
		}
	}()
	if install == nil {
		return nil
	}
	return install(ctx, progress)
}

func (b *fakeUpdate) Relaunch() { b.mu.Lock(); b.relaunch++; b.mu.Unlock() }

func (b *fakeUpdate) relaunched() int { b.mu.Lock(); defer b.mu.Unlock(); return b.relaunch }

func (b *fakeUpdate) setErr(err error) { b.mu.Lock(); b.err = err; b.mu.Unlock() }

// updFixture 是停在设置页上的外壳，加一个假更新源。
type updFixture struct {
	t  *testing.T
	tt *mygoui.Tester
	s  *Shell
	a  *app.App
	be *fakeUpdate
}

// ⚠️ **opts 是给深浅两套主题用的**（变参 = 原来那些调用一个字都不用改）：不传
// `systemIsDark` 的话，「深色」那一档存出来和「浅色」逐字节相同——票 #241 的评审
// 在确认弹窗那一族踩过这个（`collectionShell` 没带 `systemIsDark`）。
func newUpdFixture(t *testing.T, rel *app.UpdateRelease, opts ...shellOpt) *updFixture {
	t.Helper()
	be := newFakeUpdate("0.1.0", rel)
	updatesFile := filepath.Join(t.TempDir(), "app-update.json")
	// 更新源必须在**建外壳之前**挂上：设置页每一帧都要问它「这个构建能不能自动
	// 更新」，而 MountUpdates 换了之后要重新问（见 app.MountUpdates 的注释）。
	b := newTestShell(t, append([]shellOpt{
		withSettings(app.DefaultSettings("")),
		mounted(func(_ *testing.T, a *app.App) {
			a.MountUpdates(be, updatesFile)
		}),
	}, opts...)...)
	tt := b.tt
	tt.Frame()
	openSettings(t, tt)
	return &updFixture{t: t, tt: tt, s: b.s, a: b.a, be: be}
}

// pressEntry 按设置页上那个按钮（**先滚进可视区再点**，理由见 clickSetting）。
//
// ⚠️ **按之前先把浮在右下角的那几条提示收掉**：那一块正好压在「检查更新」这个
// 按钮上（`TestAFailedCheckPutsTheReasonUnderTheButtonAndKeepsItPressable` 顶上
// 写的就是这件事），而提示是**到点才在画它的那一帧里被收走**的——点下去的那一帧
// 恰好是上一轮提示刚冒出来的那一帧时，它就替按钮吃了这一次点击。
//
// 那一条顶上 `shortToasts(t, time.Millisecond)` 把提示的命压到 1 毫秒，是**方向
// 对、做法不严**：从「提示会自己消失」到「按之前它一定已经消失」之间还差一个
// 「睡 1 毫秒」，而这一组里没有任何一步保证睡过。实测：不收掉的话这一条
// `-count=5` 里红 3~4 次（HEAD 单独跑也一样红），收掉之后连跑 6 次全过。
func (f *updFixture) pressEntry(t *testing.T, label string) {
	t.Helper()
	f.s.clearToasts()
	f.tt.Frame()
	clickSetting(t, f.tt, f.s, label)
}

// checkUpdate 按「检查更新」并等它**真的查完**。
//
// ⚠️ 等的是「查了几次」而不是「相位不再是 Checking」：按下按钮的那一刻相位还是
// `Idle`，而 `Idle` 也正是「查完了、已经是最新」的结果——用它当条件的话这个等待
// 当场就成立，于是测试是在断言一个还没发生的事实。
func (f *updFixture) checkUpdate(t *testing.T) {
	t.Helper()
	before := f.be.checksDone()
	f.pressEntry(t, "检查更新")
	waitFor(t, "真的去查了一次更新", func() bool { return f.be.checksDone() > before })
	waitFor(t, "查完（相位不再是 Checking）", func() bool { return f.a.UpdateSnapshot().Phase != app.UpdateChecking })
	f.tt.Frame()
}

// openDialog 按「查看更新内容」，把弹窗打开。
func (f *updFixture) openDialog(t *testing.T) {
	t.Helper()
	f.pressEntry(t, "查看更新内容")
	f.tt.Frame()
	if !f.s.update.wanted {
		t.Fatalf("弹窗没打开，文字有 %q", f.tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────

// 设置页上有「关于应用」那一段：当前版本 + 那个按钮。
//
// 断的是**入口真的在那儿**：Biu 的 `system-settings.tsx` 最末就是这一段，而我们
// 之前没有它（那一段当时整段没摆）。
func TestTheSettingsPageHasAnAboutSectionWithTheVersionAndAButton(t *testing.T) {
	f := newUpdFixture(t, nil)

	for _, want := range []string{"关于应用", "当前版本 0.1.0", "检查更新"} {
		if !f.tt.HasText(want) {
			t.Errorf("设置页上没有 %q，文字有 %q", want, f.tt.Texts())
		}
	}
	// 没有新版本的时候**不画**那个箭头和 0.2.0：Biu 那一段是
	// `isUpdateAvailable && latestVersion` 才画，而多画一个版本号会让用户以为
	// 已经有一个下好的新版本在那儿了。
	if f.tt.HasText("0.2.0") {
		t.Errorf("没有更新却画出了新版本号，文字有 %q", f.tt.Texts())
	}
}

// 查到了新版本：那一行接上箭头和新版本号，按钮变成「查看更新内容」。
func TestCheckingFindsANewVersionAndTheRowSaysSo(t *testing.T) {
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0"})

	f.checkUpdate(t)

	if !f.tt.HasText("0.2.0") {
		t.Errorf("没把新版本号接到那一行上，文字有 %q", f.tt.Texts())
	}
	if !f.tt.HasText("查看更新内容") {
		t.Errorf("按钮没变成「查看更新内容」，文字有 %q", f.tt.Texts())
	}
	// 照 Biu 那一版（`release-note-modal/index.tsx`）：标题带那个 ✨。
	f.openDialog(t)
	if !f.tt.HasText("✨ 有新版本更新") {
		t.Errorf("弹窗的标题不是 Biu 那一个，文字有 %q", f.tt.Texts())
	}
}

// 弹窗把更新日志渲染成富文本：标题、列表项、粗体，一个都不少。
//
// 断的是「**渲染了**」而不是「把原文当字符串摆出来了」——`markdown.View` 是框架
// 的富文本渲染器，后一种做法在 Biu 那边是 `<Typography content={releaseNotes}/>`。
func TestTheDialogRendersTheReleaseNotes(t *testing.T) {
	f := newUpdFixture(t, &app.UpdateRelease{
		Version: "0.2.0",
		Notes: "## 修好了什么\n\n- 播放列表不再乱序\n- 歌词滚到底会停\n\n" +
			"细节见 [这个 issue](https://example.com/issues/1)。",
	})
	f.checkUpdate(t)
	f.openDialog(t)

	for _, want := range []string{"修好了什么", "播放列表不再乱序", "歌词滚到底会停", "这个 issue"} {
		if !f.tt.HasText(want) {
			t.Errorf("弹窗上找不到 %q，文字有 %q", want, f.tt.Texts())
		}
	}
	// 「## 」这个井号没有被画出来：渲染了，而不是把原文摆上去。
	if f.tt.HasText("## 修好了什么") {
		t.Errorf("更新日志是当原文摆的，没渲染，文字有 %q", f.tt.Texts())
	}
	// 链接点得开（在浏览器里），点它不会触发下载：这一条钉的是「它是个链接」。
	r, ok := f.tt.Find("这个 issue")
	if !ok {
		t.Fatal("找不到那个链接")
	}
	f.tt.ClickAt(r.X+4, r.Y+r.H/2)
	if got := f.tt.OpenedURLs(); len(got) != 1 || got[0] != "https://example.com/issues/1" {
		t.Errorf("点链接打开的是 %v", got)
	}
}

// 更新源没给日志：Biu 那一版是居中的一句「暂无更新日志」。
func TestTheDialogSaysSoWhenThereAreNoNotes(t *testing.T) {
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0", Notes: "   \n "})
	f.checkUpdate(t)
	f.openDialog(t)

	if !f.tt.HasText("暂无更新日志") {
		t.Errorf("没写「暂无更新日志」，文字有 %q", f.tt.Texts())
	}
}

// **验收**：下载中那四个数全在画面上——速度、已传、总量、百分比两位小数。
//
// 断的是四样都**写出来了**而不是只有进度条：数字是用户判断「还差多少、还要等
// 多久」的唯一依据。百分比那一条断的是**两位**（`percent.toFixed(2)`，Biu 那边
// 就是 `toFixed(2)`）——写成整数会让人以为 17% 和 17.65% 是同一个数。
func TestTheDialogShowsSpeedTransferredTotalAndPercentWithTwoDecimals(t *testing.T) {
	const total = 68 << 20
	const transferred = 12 << 20
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0"})
	f.checkUpdate(t)
	f.openDialog(t)

	f.be.mu.Lock()
	f.be.install = func(ctx context.Context, progress func(int64, int64)) error {
		emitProgress(progress, transferred, total)
		// 卡住，直到 Close 取消（t.Cleanup 里会 Close）。这一趟必须**停在这一帧**，
		// 不然测试读到的是「下完了」那一份，而那一份界面上换的是另一套字。
		<-ctx.Done()
		return ctx.Err()
	}
	f.be.mu.Unlock()

	pressDialog(f.tt, "下载更新")
	waitFor(t, "更新进入下载中", func() bool { return f.a.UpdateSnapshot().Phase == app.UpdateDownloading })
	waitFor(t, "已传字节数报上来", func() bool { return f.a.UpdateSnapshot().Transferred == transferred })
	f.tt.Frame()

	// 已传 / 总量。Biu 那边是 `filesize(transferred)/filesize(total)`，中间没有空格。
	if !f.tt.HasText("12.0 MB/68.0 MB") {
		t.Errorf("没有「已传/总量」那一串，文字有 %q", f.tt.Texts())
	}
	// 百分比：12/68 = 17.647…%，两位小数是 17.65。
	if !f.tt.HasText("17.65%") {
		t.Errorf("百分比不是两位小数的 17.65%%，文字有 %q", f.tt.Texts())
	}
	if !hasPercentWithTwoDecimals(f.tt.Texts()) {
		t.Errorf("画面上没有一个「恰好两位小数」的百分比，文字有 %q", f.tt.Texts())
	}
	// 速度：真时钟下算出来的数不确定，所以断的是**形状**——带 /s 的一个大小。
	if !hasSpeed(f.tt.Texts()) {
		t.Errorf("没有「速度/s」那一串，文字有 %q", f.tt.Texts())
	}
}

// 百分比**总是**两位，哪怕是 0.00% 或者一个除不尽的数。
//
// 这一条单独立一个用例：Biu 那边是 `Boolean(percent) && …`（0 是假值，所以 0%
// 时不画）。这里不跟——0.00% 说的是「刚点下、还没收到第一个字节」，而**不画**
// 和「卡住了」在画面上长得一模一样。
func TestThePercentAlwaysHasExactlyTwoDecimals(t *testing.T) {
	for _, c := range []struct{ transferred, total int64 }{
		{0, 68 << 20},        // 0.00%
		{1, 3},               // 33.33%
		{2, 3},               // 66.67%
		{1, 7},               // 14.29%
		{68 << 20, 68 << 20}, // 100.00%
	} {
		got := updateBusyLabel(app.UpdateState{
			Phase: app.UpdateDownloading, Transferred: c.transferred, Total: c.total,
			Percent: percentForTest(c.transferred, c.total),
		})
		pct := strings.Fields(got)[0]
		if !regexp.MustCompile(`^\d+\.\d\d%$`).MatchString(pct) {
			t.Errorf("%d/%d 的百分比写成了 %q（整个按钮上是 %q）", c.transferred, c.total, pct, got)
		}
	}
}

// **已知问题**：增量补丁没拼出来、改下整个安装包，弹窗上要**说一句话**。
//
// 这条是实测出来的行为（dmg 装来的应用第一次增量最容易走到）。不处理的话用户
// 会看见进度条倒退、百分比变小、速度归零再跳起来，而界面上一个字都没有——三件
// 各自看起来都像 bug。
func TestTheDialogSaysSoWhenTheDeltaIsNotUsable(t *testing.T) {
	const delta = 2 << 20
	const whole = 64 << 20
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0"})
	f.checkUpdate(t)
	f.openDialog(t)

	f.be.mu.Lock()
	f.be.install = func(ctx context.Context, progress func(int64, int64)) error {
		emitProgress(progress, delta, delta) // 增量包下完了
		emitProgress(progress, whole/4, whole)
		<-ctx.Done()
		return ctx.Err()
	}
	f.be.mu.Unlock()

	pressDialog(f.tt, "下载更新")
	waitFor(t, "回退到上一版记下来了", func() bool { return f.a.UpdateSnapshot().FellBack })
	f.tt.Frame()

	if !f.tt.HasText("增量更新没拼出来") {
		t.Errorf("弹窗上没说明为什么进度条倒退了，文字有 %q", f.tt.Texts())
	}
	// 而且那些数字说的是**这一趟**：百分比回到 25 附近，不是 100+。
	if !f.tt.HasText("25.00%") {
		t.Errorf("百分比不是这一趟的 25.00%%，文字有 %q", f.tt.Texts())
	}
}

// 一个**正常**的下载：弹窗上**不**出现那句话。
//
// 这是上一条的反面。一句每次都错的话比不说更糟——它会让用户对整个更新功能失去
// 信任。
func TestTheDialogDoesNotBlameTheDeltaOnANormalDownload(t *testing.T) {
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0"})
	f.checkUpdate(t)
	f.openDialog(t)

	f.be.mu.Lock()
	f.be.install = func(ctx context.Context, progress func(int64, int64)) error {
		emitProgress(progress, 0, 64<<20)
		emitProgress(progress, 32<<20, 64<<20)
		<-ctx.Done()
		return ctx.Err()
	}
	f.be.mu.Unlock()

	pressDialog(f.tt, "下载更新")
	waitFor(t, "已传字节数到 32MB", func() bool { return f.a.UpdateSnapshot().Transferred == 32<<20 })
	f.tt.Frame()

	if f.tt.HasText("增量更新没拼出来") {
		t.Errorf("一次正常的下载被说成回退了，文字有 %q", f.tt.Texts())
	}
}

// 下到一半**关不掉**：Esc 不行，点背景也不行。
//
// 这一条是为了防一个具体的坑：`ui.Modal` 做好了「点背景 / 按 Esc 关掉」，而下到
// 到一半的时候用户「关掉了」并不等于「取消了」——几十 MB 还在后台往磁盘上写。Biu
// 那个弹窗也是这么写的（`isKeyboardDismissDisabled` / `isDismissable={false}`）。
func TestTheDialogCannotGoAwayWhileTheUpdateIsDownloading(t *testing.T) {
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0"})
	f.checkUpdate(t)
	f.openDialog(t)

	f.be.mu.Lock()
	f.be.install = func(ctx context.Context, progress func(int64, int64)) error {
		emitProgress(progress, 8<<20, 64<<20)
		<-ctx.Done()
		return ctx.Err()
	}
	f.be.mu.Unlock()

	pressDialog(f.tt, "下载更新")
	waitFor(t, "更新进入下载中", func() bool { return f.a.UpdateSnapshot().Phase == app.UpdateDownloading })
	f.tt.Frame()

	f.tt.Key(0, mygoui.KeyEscape)
	f.tt.Frame()
	if !f.s.update.wanted {
		t.Error("按 Esc 把下载中的弹窗关掉了")
	}
	if !f.tt.HasText("✨ 有新版本更新") {
		t.Errorf("下载中弹窗不见了，文字有 %q", f.tt.Texts())
	}
	// 「正在下载」那一档的按钮是**关着**的：Biu 是 `isLoading={…}`，同一个意思。
	if err := pressDialog(f.tt, "正在下载"); err == nil {
		t.Error("下载中那个按钮还能按")
	}
}

// 没在下载的时候，Esc 关得掉：用户不该被一个弹窗锁死。
func TestTheDialogCanBeDismissedWhenNothingIsDownloading(t *testing.T) {
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0"})
	f.checkUpdate(t)
	f.openDialog(t)

	f.tt.Key(0, mygoui.KeyEscape)
	f.tt.Frame()

	if f.s.update.wanted {
		t.Error("Esc 没关掉弹窗")
	}
	if f.tt.HasText("✨ 有新版本更新") {
		t.Errorf("弹窗还在画面上，文字有 %q", f.tt.Texts())
	}
	// 关掉之后那一版还记着：再按一次就又出来了。
	if !f.a.UpdateSnapshot().Available() {
		t.Error("关掉弹窗把那一版也忘了")
	}
}

// 下完了：弹窗说「安装包已下载完成」，并给一个「退出并安装更新」。
//
// 这是这一票的验收词之一，也是**真的**会退出应用的那一步。
func TestTheReadyDialogOffersToQuitAndInstall(t *testing.T) {
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0"})
	f.checkUpdate(t)
	f.openDialog(t)

	f.be.mu.Lock()
	f.be.install = func(_ context.Context, progress func(int64, int64)) error {
		emitProgress(progress, 40<<20, 40<<20)
		return nil
	}
	f.be.mu.Unlock()

	pressDialog(f.tt, "下载更新")
	waitInstallDone(t, f.be)
	f.tt.Frame()

	for _, want := range []string{"安装包已下载完成", "退出并安装更新"} {
		if !f.tt.HasText(want) {
			t.Errorf("下完之后画面上没有 %q，文字有 %q", want, f.tt.Texts())
		}
	}
	// 「下载更新」那个按钮没了——它要是还在，用户会以为还得再点一次。
	if f.tt.HasText("下载更新") {
		t.Errorf("下完之后「下载更新」还在，文字有 %q", f.tt.Texts())
	}
	if err := pressDialog(f.tt, "退出并安装更新"); err != nil {
		t.Fatalf("点不到「退出并安装更新」：%v，文字有 %q", err, f.tt.Texts())
	}
	waitFor(t, "装完真的重启了一次", func() bool { return f.be.relaunched() == 1 })
	// 装的时候要的那一版，正是界面上摆着的那一版。
	f.be.mu.Lock()
	wants := append([]string(nil), f.be.wantVer...)
	f.be.mu.Unlock()
	if len(wants) != 1 || wants[0] != "0.2.0" {
		t.Errorf("装的是 %v，要 0.2.0", wants)
	}
}

// 装失败：原因整句摆出来（用户要拿它去提 issue），并给一个「重试下载」。
func TestAFailedInstallPutsTheReasonInTheDialogAndOffersARetry(t *testing.T) {
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0"})
	f.checkUpdate(t)
	f.openDialog(t)

	f.be.mu.Lock()
	f.be.install = func(context.Context, func(int64, int64)) error {
		return errors.New("signature: ed25519: verification failed")
	}
	f.be.mu.Unlock()

	pressDialog(f.tt, "下载更新")
	waitInstallDone(t, f.be)
	f.tt.Frame()

	if !f.tt.HasText("verification failed") {
		t.Errorf("弹窗上没有把失败的原因整句摆出来，文字有 %q", f.tt.Texts())
	}
	if !f.tt.HasText("重试下载") {
		t.Errorf("没有「重试下载」，文字有 %q", f.tt.Texts())
	}
	// ⚠️ 设置页那一行**不能**说「上一次没查成」。这一次的失败是验签没过（下载安装
	// 那一段），用户的检查是成功的——告诉他「没查成」，他会去怪网络或者怪版本还没
	// 发出来，而真正该做的是重试下载。
	if f.tt.HasText("上一次没查成") {
		t.Errorf("装失败被说成了「没查成」，文字有 %q", f.tt.Texts())
	}
	if !f.tt.HasText("没能装上") {
		t.Errorf("设置页那一行没说「没能装上」，文字有 %q", f.tt.Texts())
	}
	// 按下去真的重来一次（第二趟用的是同一版）。
	if err := pressDialog(f.tt, "重试下载"); err != nil {
		t.Fatal(err)
	}
	waitInstallDone(t, f.be)
	f.be.mu.Lock()
	n := len(f.be.wantVer)
	f.be.mu.Unlock()
	if n < 2 {
		t.Errorf("按了重试却没有第二趟，只装了 %d 次", n)
	}
}

// 查失败（还不知道有没有更新）：原因整句摆在那一个按钮下面，按钮还能再按一次。
//
// ⚠️ 这一档**不开弹窗**：Biu 那边是按完弹一个 Toast（`update-check-button` 里的
// `addToast({title: "检查更新失败", description: res.error})`），而弹窗里那个位置
// 是留给「已经知道有新版本了」的画面的——查失败的时候弹窗里能画什么？一个空框。
// 所以原因摆在设置页那一行上，那个位置本来就在那儿，而且它就在按钮正下方。
func TestAFailedCheckPutsTheReasonUnderTheButtonAndKeepsItPressable(t *testing.T) {
	// 提示要**短**，但不能靠「短到 1ms 然后赌它已经过期」：它浮在右下角，正
	// 好盖住「检查更新」那一个按钮（这一行的按钮滚进可视区之后落在右下角那一块），
	// 于是那一下要么点在按钮上、要么被提示卡片吃掉。这一条原来就是赌的——实测
	// 单独跑 30 次挂 28 次（快的机器上，从「画完那一帧」到「按第二次按钮」不到
	// 1ms，提示还在屏幕上），而在整套里跑时好时坏，因为整套慢一点、1ms 反而
	// 过去了。症状报在两秒之后（"等了两秒，那个后台结果还没到"），离真因隔了三层。
	//
	// 现在是：提示活 1ms（快），而**按之前 `pressEntry` 会先把提示收掉**（准）。
	// 那一处收尾在 `pressEntry` 里——它是这一组所有「按设置页上的按钮」的共同
	// 入口，收在那里比在这里收一次更不容易漏。
	shortToasts(t, time.Millisecond)

	f := newUpdFixture(t, nil)
	f.be.setErr(errors.New("dial tcp: lookup updates.example.com: no such host"))

	f.pressEntry(t, "检查更新")
	waitFor(t, "查失败落到 UpdateFailed", func() bool { return f.a.UpdateSnapshot().Phase == app.UpdateFailed })
	f.tt.Frame()

	if !f.tt.HasText("no such host") {
		t.Errorf("设置页上没把失败原因整句摆出来，文字有 %q", f.tt.Texts())
	}
	// 这一档**才是**「没查成」。和上面那条装失败正好互为反面：同一个 `UpdateFailed`
	// 相位，两句话说错了任何一句都是在把用户往错的方向领（见 updateHint）。
	if !f.tt.HasText("上一次没查成") {
		t.Errorf("查失败没被说成「上一次没查成」，文字有 %q", f.tt.Texts())
	}
	if f.tt.HasText("没能装上") {
		t.Errorf("查失败被说成了「没能装上」，文字有 %q", f.tt.Texts())
	}
	if f.s.update.wanted {
		t.Error("查失败就自己把弹窗打开了")
	}
	// 按钮还是「检查更新」而且按得动：下一次按就是重试。
	if !f.tt.HasText("检查更新") {
		t.Errorf("按钮没留在「检查更新」，文字有 %q", f.tt.Texts())
	}
	// ⚠️ **按第二次**：`checkUpdate` → `pressEntry`，而 `pressEntry` 会先把右下角
	// 那几条提示收掉再点（理由见 `pressEntry` 的注释）。少了那一步，那一下会被
	// 提示卡片吃掉，于是这里的 `waitFor` 等的是一个永远不会发生的检查。
	f.be.setErr(nil)
	f.checkUpdate(t)
	if got := f.a.UpdateSnapshot().Phase; got != app.UpdateIdle {
		t.Errorf("再按一次之后的相位是 %v，要 Idle（这一次查成了：已是最新）", got)
	}
	// 那一行下面的话也跟着换回去了：上一句「上一次没查成：…」不该留在那儿。
	if f.tt.HasText("no such host") {
		t.Errorf("查成了却还摆着上一次失败的原因，文字有 %q", f.tt.Texts())
	}
	if !f.tt.HasText("上次检查：") {
		t.Errorf("没有「上次检查：…」那一句，文字有 %q", f.tt.Texts())
	}
}

// 这个构建不能自动更新（开发构建 / 装在只读位置）：要有一句能照着做的话，
// 而且按钮按下去什么也不发生。
//
// 断「按不下去」的方式是**看它有没有真的去查**——`pressDialog` 只负责点，点一
// 个关着的按钮不会报错，所以断言必须是「状态变了没有」。
func TestTheAboutSectionSaysWhyThisBuildCannotUpdate(t *testing.T) {
	f := newUpdFixture(t, nil)
	f.be.mu.Lock()
	f.be.enabled = false
	f.be.mu.Unlock()
	// Enabled() 是挂上时问的那一次（见 `app.MountUpdates` 的注释：每帧问一次是
	// 一次真实的磁盘探测），所以要重新挂一次才生效。
	f.a.MountUpdates(f.be, filepath.Join(t.TempDir(), "other.json"))
	f.tt.Frame()

	if !f.tt.HasText("开发构建") {
		t.Errorf("没说明为什么不能自动更新，文字有 %q", f.tt.Texts())
	}
	f.pressEntry(t, "检查更新")
	// 相位一动不动：按钮是关的。
	if got := f.a.UpdateSnapshot().Phase; got != app.UpdateIdle {
		t.Errorf("不能更新的构建上按了按钮，相位变成 %v", got)
	}
	if !f.tt.HasText("检查更新") {
		t.Errorf("按钮不见了，文字有 %q", f.tt.Texts())
	}
}

// 后台自动查到新版本：弹一条 Toast，**不硬弹窗**。
//
// 一个音乐播放器开着窗突然被更新窗盖住是最招人烦的一种做法，而 Biu 那边是顶栏上
// 一个「New」小标签（`layout/navbar/app-update`），不弹窗。
//
// ⚠️ 走的是 `StartAutoChecks` 而不是 `CheckUpdates`：后者是**用户按了**那条路，
// 它的话「发现新版本」那句话刻意留白（弹窗就是用户要的东西了），而这一条要验
// 的是自动那一次。
func TestABackgroundCheckShowsAToastInsteadOfOpeningTheDialog(t *testing.T) {
	f := newUpdFixture(t, &app.UpdateRelease{Version: "0.2.0"})

	stop := f.a.StartAutoChecks() // 自动那一次：立刻查一遍
	defer stop()
	waitFor(t, "后台那一次查到了新版本", func() bool { return f.a.UpdateSnapshot().Available() })
	f.tt.Frame()

	if f.s.update.wanted {
		t.Error("后台查到新版本就自己把弹窗打开了")
	}
	if !f.tt.HasText("发现新版本 0.2.0") {
		t.Errorf("没有那条提示，文字有 %q", f.tt.Texts())
	}
}

// 弹窗里的 view 外面包着 `ui.Guard`，和顶栏、侧栏、播放列表抽屉是同一个写法。
//
// ⚠️ 这一条**故意没有**再写一个「往弹窗里塞一个 panic」的测试。试过，代价太大：
// `markdown.View` 的块回调（`markdown.Options.Block`）在框架多趟 build 里的哪一趟
// 被调，取决于那一帧之前画过什么——于是那个 panic 有时落在 `Guard` 的动态范围内
// （被接住，测试通过），有时落在外面（**整个测试进程被带走**）。单独跑这个包
// 25 次全过，夹在整包里跑就会偶发崩掉整个二进制。
//
// 一个「有可能把整条 `go test` 带走的」测试是最坏的一种测试：它把红变成了和被测
// 东西无关的东西，而门禁命令只会看到一个红。panic 那条路 `recover_test.go` 已经有
// 两条钉着（`TestGuardKeepsPanicOutOfTheProcess`、`TestShellFallsBackToTheErrorPage`），
// 而这一段 view 的写法和 `viewMain` 里另外三个 `Guard(...)` 是同一个。

// ─────────────────────────────────────────────────────────────────────────────

// pressDialog 点弹窗里的一个东西，点不到就报错（而不是安静地什么也没发生）。
func pressDialog(tt *mygoui.Tester, label string) error {
	r, ok := tt.Find(label)
	if !ok {
		return fmt.Errorf("弹窗上没有 %q，文字有 %q", label, tt.Texts())
	}
	if r.W <= 0 || r.H <= 0 {
		return fmt.Errorf("弹窗上 %q 没有量出大小（%v）", label, r)
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	return nil
}

// waitInstallDone 等那一趟下载/安装收工。
func waitInstallDone(t *testing.T, be *fakeUpdate) {
	t.Helper()
	select {
	case <-be.done:
	case <-time.After(5 * time.Second):
		t.Fatal("等了 5 秒那一趟还没收工")
	}
}

// emitProgress 造一次进度上报。
func emitProgress(progress func(int64, int64), downloaded, total int64) {
	if progress != nil {
		progress(downloaded, total)
	}
}

// hasPercentWithTwoDecimals 说画面上有没有一个**恰好两位小数**的百分比。
func hasPercentWithTwoDecimals(texts []string) bool {
	re := regexp.MustCompile(`\d+\.\d\d%`)
	for _, s := range texts {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// hasSpeed 说画面上有没有一个「大小/s」。
//
// 真时钟下算出来的数不确定（调度抖一下就是另一个数），所以断的是**形状**。
var speedRe = regexp.MustCompile(`^\d+(\.\d)? (B|KB|MB|GB|TB)/s$`)

func hasSpeed(texts []string) bool {
	for _, s := range texts {
		if speedRe.MatchString(s) {
			return true
		}
	}
	return false
}

// percentForTest 是状态层那个百分比的算式，测试里自己算一遍好对照。
func percentForTest(transferred, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(transferred) / float64(total) * 100
}
