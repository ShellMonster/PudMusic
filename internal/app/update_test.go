package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 更新这一摊的测试跑在**真磁盘**上（那份 `app-update.json` 真写真读）和**真
// goroutine** 上（安装真在后台跑，真取消）。
//
// 断言的是「状态机走到哪一档」和「落盘的那份文件里有什么」——不是「调了哪个
// 函数」。后者只能证明这一层有那个方法，证明不了界面上的那个入口会因为什么
// 走错。

// fakeBackend 是不碰框架的假更新源。
//
// 它能造出真实世界里那几种要紧的情况：查到新版本、查到最新、查失败、装失败、
// **装到一半换成全量**（增量失败），以及**装到一半被取消**。
type fakeBackend struct {
	mu      sync.Mutex
	enabled bool
	version string
	// rel 是 Check 要回来的那一版；nil 就是「已经是最新」。
	rel      *UpdateRelease
	checkErr error
	// install 是 Install 干的那件事。由测试自己写，用 emit 造进度。
	install func(ctx context.Context, progress func(downloaded, total int64)) error
	// wants 是 Install 收到的那一版版本号，记下来好断言。
	wants    []string
	relaunch int
	// holdCheck 让 Check 一直等着直到 ctx 被取消（模拟一个查了很久的网络）。
	holdCheck bool
	// installDone 在 Install 返回之后被关掉，测试拿它等那一趟收工。
	installDone chan struct{}
}

func newFakeBackend(version string, rel *UpdateRelease) *fakeBackend {
	return &fakeBackend{
		enabled: true, version: version, rel: rel,
		installDone: make(chan struct{}),
	}
}

func (b *fakeBackend) Enabled() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.enabled }

func (b *fakeBackend) Version() string { b.mu.Lock(); defer b.mu.Unlock(); return b.version }

func (b *fakeBackend) Check(ctx context.Context) (*UpdateRelease, error) {
	b.mu.Lock()
	hold, rel, err := b.holdCheck, b.rel, b.checkErr
	b.mu.Unlock()
	if hold {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return rel, nil
}

func (b *fakeBackend) Install(ctx context.Context, want UpdateRelease, progress func(int64, int64)) error {
	b.mu.Lock()
	b.wants = append(b.wants, want.Version)
	install := b.install
	b.mu.Unlock()
	defer func() {
		select {
		case <-b.installDone:
		default:
			close(b.installDone)
		}
	}()
	if install == nil {
		return nil
	}
	return install(ctx, progress)
}

func (b *fakeBackend) Relaunch() { b.mu.Lock(); b.relaunch++; b.mu.Unlock() }

func (b *fakeBackend) setRel(rel *UpdateRelease) {
	b.mu.Lock()
	b.rel = rel
	b.mu.Unlock()
}

func (b *fakeBackend) relaunched() int { b.mu.Lock(); defer b.mu.Unlock(); return b.relaunch }

func (b *fakeBackend) wanted() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.wants...)
}

// emit 造一次进度上报。
func emit(progress func(int64, int64), downloaded, total int64) {
	if progress != nil {
		progress(downloaded, total)
	}
}

// updFixture 是挂好了更新的一个 App，加一个能推时间的假时钟。
type updFixture struct {
	app  *App
	be   *fakeBackend
	path string
	// base 是假时钟的原点，nowAt 是此刻。at() 用的是**相对 base 的绝对偏移**，
	// 所以 `at(100ms); at(300ms)` 之间的间隔是 200ms 而不是 200ms 的累加——
	// 写成相对的会让「两个采样点差多久」变成一个要心算的东西。
	base, nowAt time.Time
}

// newUpdFixture 挂一个更新器。rel 传 nil 就是「已经是最新」。
//
// 时钟是假的：速度是**两个采样点之间的时间差**除出来的，真时钟下同一个测试
// 每次跑出来的数字都不一样（几十毫秒的调度抖动就能让 1 MB/s 变成 0.8 MB/s），
// 而速度是要断言的东西之一。
func newUpdFixture(t *testing.T, version string, rel *UpdateRelease) *updFixture {
	t.Helper()
	f := &updFixture{
		app:   New(),
		be:    newFakeBackend(version, rel),
		path:  filepath.Join(t.TempDir(), "app-update.json"),
		base:  time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
		nowAt: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}
	f.app.MountUpdates(f.be, f.path)
	t.Cleanup(f.app.Close)
	// 推进时钟并推满速度的采样窗口，让「上一次采样」落在已知的位置上。
	f.app.mu.Lock()
	f.app.updates.now = func() time.Time { return f.nowAt }
	f.app.mu.Unlock()
	return f
}

// at 把假时钟拨到离原点 d 处，让**下一次**进度上报记在这个时刻上。
func (f *updFixture) at(d time.Duration) { f.nowAt = f.base.Add(d) }

// checkAndWait 查一次更新并等它落地。
//
// `updater.check` 是**同步**的（它就在这一次调用里走完那一个网络往返并写下结
// 果），所以这里直接调就行，不需要等——真应用层之所以把它扔进 goroutine，是
// 因为调用它的是界面那条线程，见 `App.CheckUpdates` 的注释。
func (f *updFixture) checkAndWait(t *testing.T, user bool) {
	t.Helper()
	f.app.mu.Lock()
	u := f.app.updates
	f.app.mu.Unlock()
	if u == nil {
		t.Fatal("更新没挂上")
	}
	u.check(user)
	if got := u.state.phase; got == UpdateChecking {
		t.Fatalf("查完了相位还停在 Checking")
	}
}

// snapshot 是当前那份状态。
func (f *updFixture) snapshot() UpdateState { return f.app.UpdateSnapshot() }

// ─────────────────────────────────────────────────────────────────────────────

// 一次普通的「查到新版本」。
//
// 断的四件事：相位到了 `Available`、版本号和更新日志都在、界面上那些字有的
// 依据（Notes/Current/Latest）、以及**存住了**（下次开应用还记得）。
func TestACheckFindsTheNewVersionAndKeepsIt(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{
		Version: "0.2.0",
		Notes:   "### 修好了\n\n- 播放列表不再乱序",
		Date:    time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
	})

	f.checkAndWait(t, true)
	st := f.snapshot()

	if st.Phase != UpdateAvailable {
		t.Fatalf("相位是 %v，要 Available", st.Phase)
	}
	if !st.Available() {
		t.Errorf("Available() 说不有更新：Latest=%q Current=%q", st.Latest, st.Current)
	}
	if st.Latest != "0.2.0" || st.Current != "0.1.0" {
		t.Errorf("版本号是 %q ← %q", st.Current, st.Latest)
	}
	if !strings.Contains(st.Notes, "播放列表") {
		t.Errorf("更新日志没带过来：%q", st.Notes)
	}
	if want := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC); !st.Released.Equal(want) {
		t.Errorf("发布日期是 %v，要 %v", st.Released, want)
	}
	// 用户自己按的检查：要给一句「有更新」以外的话吗？——Biu 那边按完是弹窗，
	// 而弹窗是我们开着的，所以这一句留白（见 check 的注释）。
	if n := f.app.TakeUpdateNotice(); n != "" {
		t.Errorf("用户按的那一次留了提示 %q", n)
	}

	// 存住了：重启之后还在。
	f.app.Close()
	again := New()
	t.Cleanup(again.Close)
	again.MountUpdates(f.be, f.path)
	if got := again.UpdateSnapshot(); !got.Available() || got.Latest != "0.2.0" {
		t.Errorf("重启之后 Latest=%q Phase=%v", got.Latest, got.Phase)
	}
}

// 已经是最新：相位回到 Idle，而且**给用户一句明明白白的话**。
func TestAnAppThatIsUpToDateSaysSo(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", nil)
	f.checkAndWait(t, true)

	if got := f.snapshot().Phase; got != UpdateIdle {
		t.Fatalf("相位是 %v，要 Idle", got)
	}
	if n := f.app.TakeUpdateNotice(); n != "当前版本为最新版本" {
		t.Errorf("提示是 %q", n)
	}
	// 查成功了，时刻要记住；界面那一行「上次检查：…」靠它。
	if f.snapshot().LastCheck.IsZero() {
		t.Error("查成功了却没记下时刻")
	}
}

// 后台那一次查到了新版本：留一句提示（界面上弹成 Toast），**不硬弹窗**。
func TestABackgroundCheckLeavesANoticeInsteadOfOpeningAnything(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", &UpdateRelease{Version: "0.2.0"})
	f.checkAndWait(t, false)

	if n := f.app.TakeUpdateNotice(); n != "发现新版本 0.2.0" {
		t.Errorf("提示是 %q", n)
	}
	// 取过就没了：Toast 弹两遍比不弹更糟。
	if n := f.app.TakeUpdateNotice(); n != "" {
		t.Errorf("提示被取了两次：%q", n)
	}
}

// 后台查失败：不弹提示（用户没按任何键，突然冒一句红字只会让人以为应用坏了）。
func TestABackgroundCheckThatFailsKeepsQuiet(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", nil)
	f.be.mu.Lock()
	f.be.checkErr = errors.New("dial tcp: lookup updates.example.com: no such host")
	f.be.mu.Unlock()

	f.checkAndWait(t, false)

	if got := f.snapshot().Phase; got != UpdateFailed {
		t.Fatalf("相位是 %v，要 UpdateFailed", got)
	}
	if n := f.app.TakeUpdateNotice(); n != "" {
		t.Errorf("后台那次也留了提示：%q", n)
	}
	// 但原因得留在状态里：用户自己按的时候要看见它。
	if !strings.Contains(f.snapshot().Err, "no such host") {
		t.Errorf("原因没留下来：%q", f.snapshot().Err)
	}
}

// 用户自己按的检查失败：要说清楚。
func TestACheckTheUserAskedForSaysWhatWentWrong(t *testing.T) {
	f := newUpdFixture(t, "0.1.0", nil)
	f.be.mu.Lock()
	f.be.checkErr = errors.New("i/o timeout")
	f.be.mu.Unlock()

	f.checkAndWait(t, true)

	if n := f.app.TakeUpdateNotice(); !strings.Contains(n, "i/o timeout") {
		t.Errorf("提示是 %q，要带原因", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 那一份文件

// 落盘的形状：schema 号在，两样都存得住。
func TestThePrefsFileRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app-update.json")
	p := newUpdatePrefs(path)
	p.load() // 文件还没有：不是错误

	when := time.Date(2026, 10, 17, 8, 30, 0, 0, time.UTC)
	p.put(updatePrefsFile{LastCheck: when, KnownVersion: "1.2.3"})

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got updatePrefsFile
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != updatePrefsSchema {
		t.Errorf("schemaVersion 是 %d", got.SchemaVersion)
	}
	if !got.LastCheck.Equal(when) || got.KnownVersion != "1.2.3" {
		t.Errorf("读回来是 %+v", got)
	}

	q := newUpdatePrefs(path)
	q.load()
	if l := q.last(); l.KnownVersion != "1.2.3" {
		t.Errorf("新的一份读回来是 %+v", l)
	}
}

// 一个坏掉的文件：不崩，不往回写，下一次检查照常。
func TestACorruptPrefsFileIsIgnoredNotFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app-update.json")
	if err := os.WriteFile(path, []byte("{这不是 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := newUpdatePrefs(path)
	p.load()

	if got := p.last(); got.KnownVersion != "" || !got.LastCheck.IsZero() {
		t.Errorf("从坏文件里读出了 %+v", got)
	}
	// 还能写回去（写回去就是修好了它）。
	p.put(updatePrefsFile{KnownVersion: "9.9.9"})
	if got := p.last(); got.KnownVersion != "9.9.9" {
		t.Errorf("写回去之后是 %+v", got)
	}
}

// 别的、更新的版本写的文件：一个字都不许写回去。
func TestAPrefsFileFromANewerVersionIsLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app-update.json")
	raw := []byte(`{"schemaVersion": 99, "knownVersion": "9.9.9"}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	p := newUpdatePrefs(path)
	p.load()
	p.put(updatePrefsFile{KnownVersion: "1.0.0"})

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(raw) {
		t.Errorf("别人（更新的版本）写的文件被改了：\n%s", after)
	}
}

// 拿不到应用数据目录：只在内存里，更新照常用。
//
// ⚠️ 这里钉的是「别把路径凑出来」：`UpdatePrefsPath("")` 必须返回空串而不是
// `app-update.json`——后者会让文件落在当前工作目录里，而从 Finder 启动时那是
// `/`（写不进去），从终端启动时又是另一处。
func TestNoUserDataDirMeansMemoryOnly(t *testing.T) {
	if got := UpdatePrefsPath(""); got != "" {
		t.Errorf("UpdatePrefsPath(\"\") 是 %q，要空串", got)
	}
	if got := UpdatePrefsPath("/tmp/x"); got != filepath.Join("/tmp/x", "app-update.json") {
		t.Errorf("UpdatePrefsPath 是 %q", got)
	}

	be := newFakeBackend("0.1.0", &UpdateRelease{Version: "0.2.0"})
	a := New()
	t.Cleanup(a.Close)
	a.MountUpdates(be, UpdatePrefsPath(""))
	a.CheckUpdates()
	waitFor(t, func() bool { return a.UpdateSnapshot().Available() }, "内存里也该查得到更新")
}

// 没挂更新：那些方法一个都不许崩（界面每帧都会问一句挂上没有）。
func TestWithoutTheUpdaterEveryEntryPointIsQuiet(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	if a.UpdateMounted() {
		t.Fatal("没挂却说挂了")
	}
	if got := a.UpdateSnapshot(); got.Phase != UpdateIdle || got.Enabled {
		t.Errorf("没挂却给出了 %+v", got)
	}
	a.CheckUpdates()
	a.StartUpdateDownload()
	a.QuitAndInstall()
	a.DismissUpdate()
	stop := a.StartAutoChecks()
	stop()
	if n := a.TakeUpdateNotice(); n != "" {
		t.Errorf("没挂却留了提示 %q", n)
	}
}
