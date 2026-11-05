package ui

// 刘海窗的开合状态机与窗口开关。真窗那半边（NSWindow 配置、NSEvent 监听）
// 只有真机能验，这里钉的是**不靠窗口也成立**的那一半：
//
//   - `openNotchPlayer`/`closeNotchPlayer` 对外壳的开/关；
//   - `notchPoll` 那个「指针在不在触发区」的状态机（300ms 进、350ms 出、
//     「在区里挪动会重算」）；
//   - 引导那一档（`notchGreet`/`notchKeep`/`notchBye`）；
//   - 穿透：指针不在形状上时窗把点击让给桌面。
//
// ⚠️ 「刘海的 `notchSt` 长在外壳自己身上」是这一组的地基：监听那边写、
// `notchPoll` 这边读（`notch.go` 那一段注释）。

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// newNotchShell 建一个刘海外壳 + 它的主窗外壳，不建真窗（无头 Tester 里
// `mygo.NewWindow` 会 panic——和 `newMiniShell` 同一个理由）。
func newNotchShell(t *testing.T) (*Shell, *Shell) {
	t.Helper()
	sh := newTestShell(t, withSettings(app.Settings{NotchPlayer: boolPtr(false)}))
	main := sh.s
	n := main.newNotchShell()
	t.Cleanup(n.closePerWindow)
	// 假窗尺寸填上：`notchMeasure` 要走 `mygo.Screen.PrimaryDisplay`，
	// 无头 Tester 里那个返回零值——给一份真的，好让 screenX/screenY 是数。
	n.notchSt.winW, n.notchSt.winH = notchWinW, notchWinH
	n.notchSt.w0, n.notchSt.h0 = 185, 32
	n.notchSt.real = true
	return n, main
}

// cursorIn 把刘海的指针坐标写成「在触发区里的一个点」。
// 真窗那一段是 `mygo.Screen.CursorScreenPoint`，无头 Tester 里它返回零值
// ——所以这里不读它，直接把 `inside` 算好塞进状态机（`poll` 的量的是
// `inside` 之后的进出时间）。
func notchEnter(n *Shell, at time.Time) {
	n.notchSt.lastInside = at
	n.notchSt.enteredAt = at
}

func TestNotchPlayerOpensAndClosesTheShell(t *testing.T) {
	main := newTestShell(t).s
	if main.notch != nil {
		t.Fatal("刚开始刘海不该开着")
	}
	n := main.newNotchShell()
	if main.notch != n || n.main != main {
		t.Fatal("刘海和主窗口没接上")
	}
	if !main.dropNotch() {
		t.Fatal("dropNotch 说没有刘海")
	}
	if main.notch != nil {
		t.Fatal("关掉之后主窗口还指着那个外壳")
	}
	if main.dropNotch() {
		t.Fatal("第二个 dropNotch 不该说开着")
	}
}

func TestToggleNotchPlayerFromTheNotchSide(t *testing.T) {
	main := newTestShell(t, withSettings(app.Settings{NotchPlayer: boolPtr(true)})).s
	n := main.newNotchShell()
	t.Cleanup(n.closePerWindow)
	n.toggleNotchPlayer()
	if main.notch == nil {
		t.Fatal("切换刘海播放器不该销毁刘海外壳")
	}
	if main.notchEnabled() {
		t.Fatal("从刘海那侧切换后设置还开着")
	}
	main.toggleNotchPlayer()
	if main.notch == nil {
		t.Fatal("主窗口那边没把刘海开回来")
	}
	if !main.notchEnabled() {
		t.Fatal("主窗口那边切换后设置没打开")
	}
}

func TestNotchGreetOnlyOnce(t *testing.T) {
	n, _ := newNotchShell(t)
	if err := enqueueTwo(n.app, t); err != nil {
		t.Fatal(err)
	}
	// 第一次打开引导：问候一次，记下「问候过了」。
	n.notchGreet()
	if !n.notchSt.hello {
		t.Fatal("第一首歌没问候")
	}
	if !n.app.SettingsSnapshot().NotchPlayerGreeted {
		t.Fatal("问候了没记下")
	}
	// 同一首再问候：不再长出来。
	n.notchSt.hello = false
	n.notchGreet()
	if n.notchSt.hello {
		t.Fatal("问候过又长出来")
	}
}

// TestNotchTarget 钉「展开 / 两翼 / 收合」三种尺寸各是多大。
func TestNotchTarget(t *testing.T) {
	n, _ := newNotchShell(t)
	// 没歌 + 展开：idle 那张卡。
	n.notchSt.open = true
	w, h := n.notchTarget()
	if w != notchIdleW || h != n.notchSt.h0+72 {
		t.Fatalf("无歌展开是 %v×%v，want %v×%v", w, h, notchIdleW, n.notchSt.h0+72)
	}
	// 没歌 + 收着：刘海本身。
	n.notchSt.open = false
	w, h = n.notchTarget()
	if w != n.notchSt.w0 || h != n.notchSt.h0 {
		t.Fatalf("没歌时收着是 %v×%v，want %v×%v", w, h, n.notchSt.w0, n.notchSt.h0)
	}
}

// TestNotchKeepTurnsItOn 钉「引导上那个开启键把设置拨到开」。
func TestNotchKeepTurnsItOn(t *testing.T) {
	n, _ := newNotchShell(t)
	n.notchSt.hello = true
	n.notchKeep()
	if n.notchSt.hello {
		t.Fatal("keep 之后还在引导")
	}
	if !n.app.SettingsSnapshot().NotchPlayerOn() {
		t.Fatal("keep 没把开关拨到开")
	}
}

func TestNotchGreetingWorksWithoutATrack(t *testing.T) {
	n, _ := newNotchShell(t)
	n.notchGreet()
	if !n.notchSt.hello {
		t.Fatal("没有歌曲时没有显示刘海引导")
	}
	w, h := n.notchTarget()
	if w != notchHelloW || h != n.notchSt.h0+58 {
		t.Fatalf("没有歌曲时引导尺寸是 %v×%v，want %v×%v", w, h, notchHelloW, n.notchSt.h0+58)
	}
}
