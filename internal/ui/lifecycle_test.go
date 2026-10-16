package ui

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo"
)

// TestPlanWindowClose 验「点了关闭按钮之后窗口去哪儿」。
//
// 里面藏着两条会互相咬的规则，少一条就出大问题：
//   - 设置说「关掉就退出」时不能拦，否则用户退不出去；
//   - **已经在退出流程里时一律放行**——退出流程本身就是靠关窗口走完的
//     （框架的 prepareQuit 遍历窗口挨个 close），这里再按设置拦一次，
//     ⌘Q 就变成了「窗口没了、进程还在」。
func TestPlanWindowClose(t *testing.T) {
	cases := []struct {
		quitting bool
		opt      app.CloseWindowOption
		want     closeAction
		why      string
	}{
		{false, app.CloseWindowHide, closeToBackground, "默认：收起来，播放不停"},
		{true, app.CloseWindowHide, closeProceed, "退出途中不能让设置拦路，否则退不出去"},
		{false, app.CloseWindowExit, closeProceed, "用户要的就是退出"},
		{true, app.CloseWindowExit, closeProceed, "退出途中本来就该关"},
	}
	for _, c := range cases {
		if got := planWindowClose(c.quitting, c.opt); got != c.want {
			t.Errorf("planWindowClose(%v, %q) = %v，want %v（%s）",
				c.quitting, c.opt, got, c.want, c.why)
		}
	}
}

// TestAppMenuDropsTheWebOnlyItems 验「重新载入」不会冒出来。
//
// 框架默认的 macOS 菜单带着「重新载入 ⌘R / 强制重新载入 / 缩放」。窗口里是
// MyGo 自己画的原生界面，没有页面可以载入，框架碰到这些角色直接 return：菜单
// 项还在、快捷键还占着，按下去什么都不发生。
func TestAppMenuDropsTheWebOnlyItems(t *testing.T) {
	roles := menuRoles(appMenu())
	for _, r := range []mygo.MenuRole{
		mygo.RoleReload, mygo.RoleForceReload,
		mygo.RoleResetZoom, mygo.RoleZoomIn, mygo.RoleZoomOut,
	} {
		if roles[r] {
			t.Errorf("菜单里还有 %q：原生界面按了没反应，用户只会以为界面卡死了", r)
		}
	}

	if runtime.GOOS != "darwin" {
		return // 下面这几项只在 macOS 上装菜单，其余平台框架一条都不装
	}
	// 留着的那几项不能顺手砍掉：退出 ⌘Q 是「窗口收起来之后想真的退出」唯一的
	// 路（见 planWindowClose），编辑菜单是后面搜索框要用的。
	for _, r := range []mygo.MenuRole{
		mygo.RoleQuit, mygo.RoleHide, mygo.RoleClose, mygo.RoleSelectAll,
		mygo.RoleToggleDevTools, mygo.RoleToggleFullScreen, mygo.RoleMinimize,
	} {
		if !roles[r] {
			t.Errorf("菜单里没有 %q 了", r)
		}
	}
}

// menuRoles 把菜单树里出现过的角色收成一张表。
func menuRoles(m *mygo.Menu) map[mygo.MenuRole]bool {
	out := map[mygo.MenuRole]bool{}
	var walk func(items []*mygo.MenuItem)
	walk = func(items []*mygo.MenuItem) {
		for _, it := range items {
			if it.Role != "" {
				out[it.Role] = true
			}
			walk(it.Submenu)
		}
	}
	walk(m.Items())
	return out
}

// fakeAutoStart 记下系统那一份被调了什么。真系统要过事件循环，无头环境里
// 调不到（见 autoStartService 的注释），所以这一层替掉它。
type fakeAutoStart struct {
	on     bool   // 系统里的现状
	setErr error  // 系统动不了时的错
	calls  []bool // 被 set 过的值，按顺序
}

func (f *fakeAutoStart) set(on bool) error {
	f.calls = append(f.calls, on)
	if f.setErr != nil {
		return f.setErr
	}
	f.on = on
	return nil
}

func (f *fakeAutoStart) enabled() bool { return f.on }

// TestSetAutoStartTouchesTheSystemBeforeTheSetting 验那条顺序。
//
// 反过来的话会留下「设置说开、系统说关」——启动时的对齐恰好只补这一种，于是
// 用户下次开机发现根本没自启，而界面上那个开关还亮着。
func TestSetAutoStartTouchesTheSystemBeforeTheSetting(t *testing.T) {
	s, fake := newShellForAutoStart(t)

	if err := s.SetAutoStart(true); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || !fake.calls[0] {
		t.Fatalf("系统那边被调了 %v，want 一次 set(true)", fake.calls)
	}
	if !s.app.SettingsSnapshot().AutoStart {
		t.Error("系统开了，设置里没存上")
	}

	if err := s.SetAutoStart(false); err != nil {
		t.Fatal(err)
	}
	if fake.on {
		t.Error("关了之后系统里还开着")
	}
	if s.app.SettingsSnapshot().AutoStart {
		t.Error("关了之后设置里还存着 true")
	}
}

// TestSetAutoStartKeepsTheSettingWhenTheSystemRefuses 验系统动不了时不碰设置。
func TestSetAutoStartKeepsTheSettingWhenTheSystemRefuses(t *testing.T) {
	s, fake := newShellForAutoStart(t)
	fake.setErr = errors.New("用户没给权限")

	if err := s.SetAutoStart(true); err == nil {
		t.Fatal("系统动不了，却报成功：设置页会告诉用户「已经开了」")
	}
	if s.app.SettingsSnapshot().AutoStart {
		t.Error("系统没动，设置却改了：那是一个下次启动对不齐、用户又看不见的谎")
	}
}

// TestSyncAutoStartOnlyFillsTheGap 验启动时只补「设置说开、系统说关」。
func TestSyncAutoStartOnlyFillsTheGap(t *testing.T) {
	cases := []struct {
		name              string
		applied, wantAuto bool
		wantCalls         int
	}{
		{"设置说开、系统里没有：补上", false, true, 1},
		{"已经对上了：不重复调", true, true, 0},
		{"设置说关：不动，哪怕系统里开着", true, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, fake := newShellForAutoStart(t)
			fake.on = c.applied
			if _, err := s.app.UpdateSettings(func(st *app.Settings) {
				st.AutoStart = c.wantAuto
			}); err != nil {
				t.Fatal(err)
			}

			s.syncAutoStart()
			if len(fake.calls) != c.wantCalls {
				t.Errorf("系统那边被调了 %v 次，want %d", len(fake.calls), c.wantCalls)
			}
		})
	}
}

// newShellForAutoStart 建一个挂着真设置（落在临时目录里）的外壳，
// 并把系统那一份换成假的。
func newShellForAutoStart(t *testing.T) (*Shell, *fakeAutoStart) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	dir := t.TempDir()
	if err := a.MountSettings(filepath.Join(dir, app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.CloseSettings() })

	s := New(a)
	fake := &fakeAutoStart{}
	s.autostart = fake
	return s, fake
}
