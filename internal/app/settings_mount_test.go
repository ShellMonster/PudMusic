package app_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/testproc"
)

// 这些测试盯的是「设置挂在 App 上」这一层：读盘、给快照、改、落盘、退出前
// flush。store 自己的行为（原子写、迁移链、合并写）在 settings_test.go 里。

// mounted 造一个挂了设置的 App：先把一份设置写到盘上，再挂上去。
func mounted(t *testing.T, s app.Settings) *app.App {
	t.Helper()
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	st, err := app.OpenSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(cur *app.Settings) { *cur = s }); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	return a
}

// TestMountSettingsReadsWhatIsOnDisk 验「启动时读盘」：盘上是什么，挂上去
// 之后给界面的就是什么。
func TestMountSettingsReadsWhatIsOnDisk(t *testing.T) {
	want := app.DefaultSettings("")
	want.ThemeMode = app.ThemeModeDark
	want.BorderRadius = 19

	a := mounted(t, want)
	if got := a.SettingsSnapshot(); got.ThemeMode != app.ThemeModeDark || got.BorderRadius != 19 {
		t.Errorf("挂上去之后读到的是 %+v", got)
	}
}

// TestSettingsSnapshotWithoutAMount 验没挂 store 时给的是默认值，不是零值。
//
// 每帧都要调它，零值的设置会让主题变成黑底黑字——「忘了挂」不该是一种
// 能画出来的状态。
func TestSettingsSnapshotWithoutAMount(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	got := a.SettingsSnapshot()
	if !reflect.DeepEqual(got, app.DefaultSettings("")) {
		t.Errorf("没挂设置时给的不是默认值：%+v", got)
	}
	if got.ThemeMode != app.ThemeModeSystem || got.PrimaryColor == "" || got.BorderRadius <= 0 {
		t.Errorf("这份设置画出来的界面会不对：%+v", got)
	}
}

// TestUpdateSettingsWithoutAMountIsAnError 验忘了挂设置时改设置会拿到错，
// 而不是悄悄改一份谁也读不到的东西。
func TestUpdateSettingsWithoutAMountIsAnError(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	if _, err := a.UpdateSettings(func(s *app.Settings) { s.ThemeMode = app.ThemeModeDark }); !errors.Is(err, app.ErrSettingsNotMounted) {
		t.Errorf("拿到的是 %v，want ErrSettingsNotMounted", err)
	}
}

// TestSetThemeModeSurvivesAReopen 验「改了设置 → 关掉 → 再挂一次」档位还在。
//
// 界面层每次切档位走的就是这条：Update → 后台合并写 → 退出前 Flush。
func TestSetThemeModeSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)

	first := app.New()
	if err := first.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	if err := first.SetThemeMode(app.ThemeModeDark); err != nil {
		t.Fatal(err)
	}
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	first.Close()

	// 重启。
	second := app.New()
	t.Cleanup(func() {
		second.Close()
		_ = second.CloseSettings()
	})
	if err := second.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	if got := second.SettingsSnapshot().ThemeMode; got != app.ThemeModeDark {
		t.Errorf("重启后档位是 %q，want dark", got)
	}
}

// TestUpdateSettingsWakesTheInterface 验改完设置会叫醒界面。
//
// 即时模式里改完状态不会自己再画一帧：不唤醒的话，界面上还是改之前那一份
// ——「切换没反应」。
func TestUpdateSettingsWakesTheInterface(t *testing.T) {
	a := app.New()
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})

	// 后台落盘那一下也会唤醒（另一个 goroutine），所以用原子计数。
	var frames atomic.Int64
	a.SetWake(func() { frames.Add(1) })
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	frames.Store(0)

	if err := a.SetThemeMode(app.ThemeModeLight); err != nil {
		t.Fatal(err)
	}
	if frames.Load() == 0 {
		t.Error("改完设置没有叫醒界面")
	}
}

// TestBrokenSettingsFileStillStarts 验设置文件坏了：设置照样挂上，程序照常
// 能起来，但错要交得出去让用户看见。
func TestBrokenSettingsFileStillStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	write(t, path, "这不是 JSON")

	a := app.New()
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	err := a.MountSettings(path)
	if !errors.Is(err, app.ErrSettingsCorrupt) {
		t.Fatalf("拿到的是 %v，want ErrSettingsCorrupt", err)
	}

	// 坏文件被备份走，剩下的是一份可用的默认值。
	if _, statErr := os.Stat(path + ".bak"); statErr != nil {
		t.Errorf("坏掉的设置文件没有备份下来：%v", statErr)
	}
	if got := a.SettingsSnapshot(); !reflect.DeepEqual(got, app.DefaultSettings("")) {
		t.Errorf("坏文件之后给的设置是 %+v", got)
	}

	// 设置还能改——不能因为读不出来就锁死。
	if err := a.SetThemeMode(app.ThemeModeDark); err != nil {
		t.Errorf("读盘失败之后设置改不动：%v", err)
	}

	// 同一个错只交一次：界面每帧都问一次，不去重就是屏幕上一直闪同样的话。
	if err := a.TakeSettingsError(); err == nil {
		t.Error("读盘失败没有报给界面")
	}
	if err := a.TakeSettingsError(); err != nil {
		t.Errorf("同一个错报了两遍：%v", err)
	}
}

// TestSettingsFromANewerVersionStayReadOnly 验「设置来自更新的版本」时一个字
// 都不许写回去。
//
// 一个字都不能写：用户的设置会被一个旧版本按旧语义改写，等升级回来已经面目
// 全非——这是最难查的一类数据丢失。
func TestSettingsFromANewerVersionStayReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	write(t, path, `{"schemaVersion": 99, "appSettings": {"themeMode": "dark"}}`)

	a := app.New()
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	if err := a.MountSettings(path); !errors.Is(err, app.ErrSettingsFromNewer) {
		t.Fatalf("拿到的是 %v，want ErrSettingsFromNewer", err)
	}
	if !a.SettingsReadOnly() {
		t.Error("来自更新版本的设置没有转成只读")
	}
	if err := a.SetThemeMode(app.ThemeModeLight); !errors.Is(err, app.ErrSettingsFromNewer) {
		t.Errorf("只读的设置被改了：%v", err)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("别人写的新版文件被我们挪走了——无权处置")
	}
}

// TestWriteFailureIsReportedOnce 验落盘失败（磁盘满、目录没了）会报给界面，
// 且只报一次。
func TestWriteFailureIsReportedOnce(t *testing.T) {
	// 挂在一个不存在的目录下：CreateTemp 一定失败，不依赖「磁盘满」这种
	// 没法在测试里造出来的状态。
	dir := t.TempDir()
	a := app.New()
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	if err := a.MountSettings(filepath.Join(dir, "没有这个目录", app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	if err := a.SetThemeMode(app.ThemeModeDark); err != nil {
		t.Fatal(err)
	}

	// 后台有 120ms 的合并写窗口，等它撞上去。
	waitFor(t, 2*time.Second, "落盘失败上报", func() bool {
		return a.TakeSettingsError() != nil
	})
	if err := a.TakeSettingsError(); err != nil {
		t.Errorf("同一个落盘失败报了两遍：%v", err)
	}

	// 内存里的档位还是改成功了的——写不出去不等于改不动。
	if got := a.SettingsSnapshot().ThemeMode; got != app.ThemeModeDark {
		t.Errorf("落盘失败之后内存里的档位是 %q", got)
	}
}

// TestCloseSettingsCanBeCalledTwice 验收尾可以重复调：窗口关一次、退出再关
// 一次，不该 panic 也不该报错。
func TestCloseSettingsCanBeCalledTwice(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	if err := a.CloseSettings(); err != nil {
		t.Errorf("第二次收尾：%v", err)
	}
}

// TestFlushSettingsWithoutAMount 验没挂设置时 flush 不报错。
//
// 退出时一定会 flush 一次，挂没挂上都得是安全的。
func TestFlushSettingsWithoutAMount(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.FlushSettings(); err != nil {
		t.Errorf("没挂设置时 flush 报错：%v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 真的杀进程重启
// ─────────────────────────────────────────────────────────────────────────────

// mountChildEnv 是「我现在是那个被拉起来改设置的子进程」的暗号。
const mountChildEnv = "BIU_MOUNT_CHILD"

// TestMountedSettingsSurviveAProcessRestart 是验收项「改设置 → 杀掉进程 →
// 重启 → 设置还在」的字面版本，走的是**界面层真正会走的那条路**：
// 挂到 App 上、切档位、收尾。
//
// settings_test.go 那条验的是 store 自己。这一条多验的是 App 这一层：换档位
// 之后调 CloseSettings 有没有把后台还欠着的那一次写完。SIGKILL 拦不住，
// 所以「同步收尾」这一步少了它就断。
func TestMountedSettingsSurviveAProcessRestart(t *testing.T) {
	if os.Getenv(mountChildEnv) != "" {
		mountChildRoundTrip(t)
		return
	}

	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	runMountChild(t, path, "write")
	runMountChild(t, path, "read")

	// 第三遍：父进程自己再读一次，跨进程的结果确实落到磁盘上了。
	s, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.ThemeMode != app.ThemeModeDark {
		t.Errorf("两个子进程过去之后读到的是 %+v", s)
	}
}

// mountChildRoundTrip 是子进程里跑的那一趟：写完立刻退出，不给后台写盘
// 任何机会，能存住就只能是那次同步 Flush 的功劳。
func mountChildRoundTrip(t *testing.T) {
	path := os.Getenv("BIU_MOUNT_PATH")
	mode := os.Getenv("BIU_MOUNT_MODE")

	a := app.New()
	defer a.Close()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	if mode == "write" {
		if err := a.SetThemeMode(app.ThemeModeDark); err != nil {
			t.Fatal(err)
		}
	}
	got := a.SettingsSnapshot().ThemeMode
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	if mode == "read" && got != app.ThemeModeDark {
		t.Errorf("重启后读到的是 %q，want dark", got)
	}
}

// runMountChild 拉起一次本测试二进制自己。-test.run 让它只跑这一条。
func runMountChild(t *testing.T, path, mode string) {
	t.Helper()
	cmd := exec.Command(testproc.Path(t), "-test.run=TestMountedSettingsSurviveAProcessRestart", "-test.count=1")
	cmd.Env = append(os.Environ(),
		mountChildEnv+"=1",
		"BIU_MOUNT_PATH="+path,
		"BIU_MOUNT_MODE="+mode,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("子进程 %s 失败了：%v\n%s", mode, err, out)
	}
}

// TestSettingsFailureIsVisibleWithoutAStore 验「设置存不住」这件事用户看得见。
//
// 拿不到设置文件路径时没有 store 可挂，但**不能就这么安静**：那时候界面用
// 默认值跑着，用户改了主题，下次打开又变回默认，而界面上一个字的提示都没有。
// 只在日志里留一行是不够的——日志只有排障的人看得到。
func TestSettingsFailureIsVisibleWithoutAStore(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	a.RecordSettingsFailure(errors.New("拿不到应用数据目录，这次的改动关掉应用就没了"))

	// 同一个错只报一次：界面每帧都问一次。
	err := a.TakeSettingsError()
	if err == nil {
		t.Fatal("设置存不住这件事没有报给界面")
	}
	if again := a.TakeSettingsError(); again != nil {
		t.Errorf("同一个错报了两遍：%v", again)
	}

	// 没有 store 也不能把界面带崩：给默认值、拒绝改动、收尾安全。
	if !reflect.DeepEqual(a.SettingsSnapshot(), app.DefaultSettings("")) {
		t.Errorf("存不住的时候给的设置不是默认值：%+v", a.SettingsSnapshot())
	}
	if _, err := a.UpdateSettings(func(s *app.Settings) { s.ThemeMode = app.ThemeModeDark }); !errors.Is(err, app.ErrSettingsNotMounted) {
		t.Errorf("没有 store 时改设置拿到的是 %v", err)
	}
	if a.SettingsReadOnly() {
		t.Error("没有 store 不等于只读")
	}
	if err := a.FlushSettings(); err != nil {
		t.Errorf("收尾时报错：%v", err)
	}
	if err := a.CloseSettings(); err != nil {
		t.Errorf("收尾时报错：%v", err)
	}
}

// TestRecordSettingsFailureIgnoresNil 顺带钉一下空错不进账：否则界面上会冒出
// 一条「设置：」这样的空提示。
func TestRecordSettingsFailureIgnoresNil(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	a.RecordSettingsFailure(nil)
	if err := a.TakeSettingsError(); err != nil {
		t.Errorf("记了一个空错：%v", err)
	}
}
