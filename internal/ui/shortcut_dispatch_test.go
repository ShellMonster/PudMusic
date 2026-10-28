// 快捷键「真的生效」这一组：录下来的键会不会让事情发生。
//
// # 这一组量的是哪几件事
//
//	录进去的那一串认得回来        parseAccel 是 accelFromEvent 的反函数，两边走岔
//	                            的症状是「设置页一切正常，按下去什么也不发生」
//	命中的快捷键会触发那条命令    应用内分发走的是哪一条
//	输入框优先                    焦点在搜索框里时快捷键不许抢按键（票面点名的硬要求）
//	正在录的那一格优先            引擎会先问「有没有人认领这个组合键」，而录制格
//	                            靠 HandleInput 吃键——不主动让开就录不进 ⌘P
//	全局那一列真的登记上去了      用一个记登记的桩替掉系统那一份（理由和
//	                            autoStartService 一样：无头环境没有事件循环）
//	登记不上要降级                注册失败不能让整个快捷键功能瘫掉
package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 桩
// ─────────────────────────────────────────────────────────────────────────────

// 记住每一条被触发过的命令。
//
// 真的那一份是 runShortcut：它要 `App.Toggle` 之类，而那些方法在没有播放器时
// 什么都不做（`App.Toggle` 第一句就是 `if p == nil { return }`）——于是「按了键
// 之后走到哪一条」在无头环境里量不到。换成记 id 的就量得到了，而且应用内那一路
// 和全局那一路共用同一个 `onShortcut`（见 Shell 的注释），所以测一次就够。
func watchCommands(s *Shell) *[]string {
	var got []string
	s.onShortcut = func(id string) { got = append(got, id) }
	return &got
}

// fakeHotkeys 是「向系统登记一个全局快捷键」那一半的桩。
type fakeHotkeys struct {
	// registered 是登记成功过的 accelerator，按登记顺序。
	registered []string
	// fns 是和 registered 一一对应的回调。
	fns []func()
	// cleared 记着撤过几次。
	cleared int
	// failFor 是登记不上就返回错误的那些 accelerator（多半是别的应用占了）。
	failFor map[string]bool
}

func newFakeHotkeys() *fakeHotkeys { return &fakeHotkeys{failFor: map[string]bool{}} }

func (f *fakeHotkeys) register(acc string, fn func()) error {
	if f.failFor[acc] {
		return errors.New("这个键已经被别的应用占了")
	}
	f.registered = append(f.registered, acc)
	f.fns = append(f.fns, fn)
	return nil
}

func (f *fakeHotkeys) unregisterAll() {
	f.cleared++
	f.registered = nil
	f.fns = nil
}

// has 报某个 accelerator 现在登记着没有。
func (f *fakeHotkeys) has(acc string) bool {
	for _, a := range f.registered {
		if a == acc {
			return true
		}
	}
	return false
}

// fire 按下 acc 那一条。
func (f *fakeHotkeys) fire(t *testing.T, acc string) {
	t.Helper()
	for i, a := range f.registered {
		if a == acc {
			f.fns[i]()
			return
		}
	}
	t.Fatalf("全局那一列里没有登记过 %q，登记过的是 %q", acc, f.registered)
}

// shellWithHotkeys 造一个替掉了系统登记那一半的外壳，顺带开始记命令。
func shellWithHotkeys(t *testing.T) (*Shell, *mygoui.Tester, *fakeHotkeys, *[]string) {
	t.Helper()
	s, tt := newShell(t)
	h := newFakeHotkeys()
	s.hotkeys = h
	return s, tt, h, watchCommands(s)
}

// shellOnShortcutPage 造一个**停在设置页「快捷键设置」那一页上**的外壳，同样替掉
// 系统登记那一半、开始记命令。
//
// 录制那一类的测试必须走这一条：录制格在设置页上，不在设置页就根本画不出来
// （症状是 `clickCell` 说「找不到录制格」，而文字里一个快捷键都没有）。
func shellOnShortcutPage(t *testing.T) (*Shell, *mygoui.Tester, *fakeHotkeys, *[]string) {
	t.Helper()
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	h := newFakeHotkeys()
	s.hotkeys = h
	got := watchCommands(s)
	openSettingsTab(t, tt, "快捷键设置")
	return s, tt, h, got
}

// ─────────────────────────────────────────────────────────────────────────────
// 录进去的那一串认得回来
// ─────────────────────────────────────────────────────────────────────────────

// TestEveryRecordedKeyComesBackAsItself 验 parseAccel 是 accelFromEvent 的反函数。
//
// 两条方向各写一遍的话，走岔的症状是最安静的那一种：设置页里录得进去、显示得
// 正常、存下去也对，而**按下去什么也不发生**。所以每个能录的键 × 每一种修饰键
// 都得原样回来。
//
// ⚠️ 期望值写的是 `ui.Cmd` 而不是照抄事件的 `ui.Super`/`ui.Ctrl`：存下来的那一串
// 里两者合成 `CommandOrControl`，反解出来的也只该是同一个 `ui.Cmd`。照抄事件
// 那一侧的话，mac 上过、非 mac 上红。
func TestEveryRecordedKeyComesBackAsItself(t *testing.T) {
	mods := []mygoui.Modifiers{
		0,
		mygoui.Super,
		mygoui.Ctrl,
		mygoui.Alt,
		mygoui.Shift,
		mygoui.Super | mygoui.Alt | mygoui.Shift,
	}
	var keys []mygoui.Key
	for k := range recordableKeys {
		keys = append(keys, k)
	}
	if len(keys) < 50 {
		t.Fatalf("能录的键只有 %d 个，比预想的少——是不是 recordableKeys 变了", len(keys))
	}
	for _, m := range mods {
		for _, k := range keys {
			a, ok := accelFromEvent(keyEvent(m, k))
			if !ok {
				t.Fatalf("%v + %v 居然录不进去", m, k)
			}
			want := m
			if m&(mygoui.Super|mygoui.Ctrl) != 0 {
				want = (m &^ (mygoui.Super | mygoui.Ctrl)) | mygoui.Cmd
			}
			gotMods, gotKey, ok := parseAccel(a)
			if !ok {
				t.Errorf("%q 解析不回来（它是 recordableKeys 里那个键录出来的）", a)
				continue
			}
			if gotMods != want || gotKey != k {
				t.Errorf("%q 回来的是 %v+%v，want %v+%v", a, gotMods, gotKey, want, k)
			}
		}
	}
}

// TestTheShortcutsHandEditedIntoTheFileStillWork 验手改过的设置文件里的写法。
//
// 那一串是给 Biu 看的，用户从 Biu 那边导一份过来是常事，而导出/手写出来的写法
// 比我们自己录的更多：小写的键、带 `Command`（不是 CommandOrControl）的修饰键、
// 键本身就是加号的 `CommandOrControl++`。解析不了它们的话，用户搬一次配置就
// 搬坏一半快捷键。
func TestTheShortcutsHandEditedIntoTheFileStillWork(t *testing.T) {
	cases := []struct {
		acc      accelerator
		wantMods mygoui.Modifiers
		wantKey  mygoui.Key
	}{
		{"CommandOrControl+P", mygoui.Cmd, mygoui.KeyP},
		{"commandorcontrol+shift+p", mygoui.Cmd | mygoui.Shift, mygoui.KeyP},
		{"Command+Alt+K", mygoui.Super | mygoui.Alt, mygoui.KeyK},
		{"Ctrl+K", mygoui.Ctrl, mygoui.KeyK},
		{"Left", 0, mygoui.KeyLeft},
		{"CommandOrControl+Left", mygoui.Cmd, mygoui.KeyLeft},
		{"F12", 0, mygoui.KeyF12},
		{"f5", 0, mygoui.KeyF5},
		{"Space", 0, mygoui.KeySpace},
		{"CommandOrControl+/", mygoui.Cmd, mygoui.KeySlash},
		// 键本身就是加号：末尾两个加号，拆的时候最后一段是空的。
		{"CommandOrControl++", mygoui.Cmd, mygoui.KeyEqual},
	}
	for _, c := range cases {
		mods, key, ok := parseAccel(c.acc)
		if !ok {
			t.Errorf("%q 解析不出来", c.acc)
			continue
		}
		if mods != c.wantMods || key != c.wantKey {
			t.Errorf("%q 解析成 %v+%v，want %v+%v", c.acc, mods, key, c.wantMods, c.wantKey)
		}
	}
	// 而认不出来的那几种必须老实说认不出来，而不是硬猜一个键——硬猜出来的症状是
	// 「按 A 结果另一个动作响了，而界面上一个字都没说」。
	for _, bad := range []accelerator{"", "CommandOrControl+Nope", "CommandOrControl+", "F13", "Shift"} {
		if _, _, ok := parseAccel(bad); ok {
			t.Errorf("%q 居然解析成功了", bad)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 应用内分发
// ─────────────────────────────────────────────────────────────────────────────

// TestAPressedShortcutRunsItsCommand 验按下去真的会触发那条命令。
//
// 这一条是「快捷键录进去了什么也不发生」那个问题的正面：默认键 ⌘P 走一遍。
func TestAPressedShortcutRunsItsCommand(t *testing.T) {
	_, tt, _, got := shellWithHotkeys(t)
	tt.Frame() // 先画一帧：这一帧才登记上 ⌘P，下一帧才有得命中
	tt.Key(mygoui.Cmd, mygoui.KeyP)
	tt.Frame()

	if len(*got) != 1 || (*got)[0] != "togglePlay" {
		t.Errorf("按了 ⌘P 之后触发的是 %v，want [togglePlay]", *got)
	}
	// 没按的那个不响：修饰键是一个不差地比的（⌘P 不该被 ⌘⇧P 顺带触发）。
	tt.Key(mygoui.Cmd|mygoui.Shift, mygoui.KeyP)
	tt.Frame()
	if len(*got) != 1 {
		t.Errorf("按了 ⌘⇧P 之后触发了 %v——修饰键只多一个 Shift 就不该响", *got)
	}
	// 没设键的那一下同样不响。
	tt.Key(mygoui.Cmd, mygoui.KeyJ)
	tt.Frame()
	if len(*got) != 1 {
		t.Errorf("按了没设过的 ⌘J 之后触发了 %v", *got)
	}
}

// TestAShortcutWithNoModifierRunsWhenNothingElseHasTheFocus 验不带修饰键的那种。
//
// 录出来可以是 `P`、`Left` 这样的裸键（Biu 的录制就是这么收的）。焦点不在任何
// 输入框时它就该响——否则那一格录进去就等于没录。
func TestAShortcutWithNoModifierRunsWhenNothingElseHasTheFocus(t *testing.T) {
	s, tt, _, got := shellWithHotkeys(t)
	// 把「上一首」改成裸的 ←：方向键不带修饰键时在列表里是有别的意思的，
	// 所以这里用一个字母，避免和滚动混在一起说。
	if err := s.keys.set(shortcutSlot{id: "prev"}, "K"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, mygoui.KeyK)
	tt.Frame()

	if len(*got) != 1 || (*got)[0] != "prev" {
		t.Errorf("按了裸的 K 之后触发的是 %v，want [prev]", *got)
	}
}

// TestTheSearchBoxGetsTheKeyBeforeAnyShortcut 验输入框优先（票面点名的硬要求）。
//
// 焦点在搜索框里按一个**不带修饰键**的快捷键，字必须进输入框，而那条命令不许
// 触发。两半都要验：字没进去就是「输入框被快捷键吞了」，命令触发了就是
// 「快捷键把字抢走了」——用户在这两种情况下都会立刻发现这个功能坏了。
//
// 这一条由引擎保证，不是我们判的：`keyDownPhysical` 先问焦点上的输入框「这一下
// 归不归你」（`editor.wants`，`mygo/ui/editor_navigation.go:27`），归它的话按键
// 在**进快捷键路由之前**就被拿走了（`ui/input.go:694`）。所以这一条同时也是
// 「框架那一层没变」的证据。
//
// ⚠️ 用 `TypeKey` 而不是 `Key`：真键盘是「键下去 → 文字上来 → 键起来」三件事，
// `Tester.Key` 只发键不带文字，量到的是「按键被谁接走」而不是「字有没有进去」。
func TestTheSearchBoxGetsTheKeyBeforeAnyShortcut(t *testing.T) {
	s, tt, _, got := shellWithHotkeys(t)
	// 录一条裸键的快捷键：带修饰键的 ⌘P 输入框本来就不吃（那是编辑键的语义），
	// 抢按键这件事只有在裸键上才谈得上。
	if err := s.keys.set(shortcutSlot{id: "togglePlay"}, "P"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if !tt.Focused(searchLabel) {
		t.Fatalf("点了搜索框它仍然没有焦点——这条测的就不是「焦点在输入框里」了")
	}

	tt.TypeKey(0, mygoui.KeyP, "p")
	tt.Frame()

	if s.searchKeyword != "p" {
		t.Errorf("按了 P 之后搜索框里是 %q，want \"p\"：快捷键把字抢走了", s.searchKeyword)
	}
	if len(*got) != 0 {
		t.Errorf("焦点在搜索框里按 P 竟然触发了 %v——输入框优先这一条破了", *got)
	}

	// 焦点离开输入框之后，同一个键就该触发那条命令了。这一半是钉住「让路」不是
	// 「这个键压根没人要」：只验前一半的话，一个把所有快捷键都关掉的实现照样通过。
	//
	// 用 Tab 把焦点挪走（MyGo 的 `moveFocus`）：按一个别的快捷键不行——那一个同样
	// 会被这一列登记着，压根挪不动焦点。
	tt.Key(0, mygoui.KeyTab)
	tt.Frame()
	if tt.Focused(searchLabel) {
		t.Fatalf("按了 Tab 之后焦点还在搜索框上——那后半句量的就还是「焦点在输入框里」")
	}
	tt.Key(0, mygoui.KeyP)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != "togglePlay" {
		t.Errorf("焦点不在输入框时按 P 触发的是 %v，want [togglePlay]", *got)
	}
}

// TestTheRecorderCellStillGetsAKeyThatIsARegisteredShortcut 验正在录的那一格优先。
//
// 引擎在把按键交给焦点上的元素**之前**先问「有没有人认领这个组合键」
// （`rt.claimed`，`ui/input.go:629`），认领了就不交给那个元素。窗口登记了 ⌘P 的
// 下一帧，正在录的那一格就收不到 ⌘P——而用户想录的往往正是这个键。
//
// 症状极难自己发现：录不进别的键（那几条没被登记，收得到），只有「把 ⌘P 改成别的
// 之后又想改回来」这一种操作会卡住，而那看起来像是「这一格坏了」。
func TestTheRecorderCellStillGetsAKeyThatIsARegisteredShortcut(t *testing.T) {
	s, tt, _, _ := shellOnShortcutPage(t)
	if got := s.keys.snapshot().GlobalShortcuts[0].Shortcut; got != "CommandOrControl+Alt+P" {
		t.Fatalf("默认的全局键不是 CommandOrControl+Alt+P，是 %q", got)
	}

	// 点**全局**那一格，然后按 ⌘P：⌘P 是应用内登记着的那一条。
	clickCell(t, tt, "全局快捷键：播放/暂停")
	if got := s.recording; got != (shortcutSlot{global: true, id: "togglePlay"}) {
		t.Fatalf("点了那一格没有进入录制（recording=%+v）", got)
	}
	tt.Key(mygoui.Cmd, mygoui.KeyP)
	tt.Frame()

	if got := s.keys.snapshot().GlobalShortcuts[0].Shortcut; got != "CommandOrControl+P" {
		t.Errorf("全局那一格录成了 %q，want CommandOrControl+P：这一格被窗口登记的 ⌘P 挡住了", got)
	}
	// 录完之后应用内那一条立刻回来。**后半句不是可有可无的**：录完就一直是录制状态的话，
	// 六条命令会当场全哑而且再也回不来（见
	// TestTheShortcutsComeBackTheMomentARecordingFinishes，那一条量的是同一条路）。
	if s.recording != (shortcutSlot{}) {
		t.Errorf("录完之后还在录 %+v", s.recording)
	}
}

// TestTheGlobalShortcutsStepAsideWhileRecording 验**全局那一份**也要让开。
//
// 应用内那一份让开是给引擎看的（`rt.claimed`）；全局那一份让开是给**系统**看的：
// macOS 的 Carbon 热键压根到不了焦点上的元素（真机上量过：⌥⌘N 录制格收得到，
// ⌥⌘K 只看到全局热键触发、录制格一行都没有）。不撤的话，**那六个默认全局键在任何
// 一格都录不进去**——而用户看到的是「我明明点了那一格，按了那个键，它却不理我」。
//
// 只用应用内那一列测不出来：⌘P 登记在引擎里，⌘⌥P 登记在系统上，两者各管一路。
func TestTheGlobalShortcutsStepAsideWhileRecording(t *testing.T) {
	s, tt, hot, _ := shellOnShortcutPage(t)
	s.syncGlobalShortcuts()
	if len(hot.registered) != 6 {
		t.Fatalf("开录之前登记了 %d 条，want 6 条（%q）", len(hot.registered), hot.registered)
	}

	// 点中一个录制格（还没按任何键）。
	clickCell(t, tt, "应用内快捷键：播放/暂停")
	if s.recording == (shortcutSlot{}) {
		t.Fatal("点了那一格没有进入录制")
	}
	tt.Frame()
	if len(hot.registered) != 0 {
		t.Errorf("正在录的时候系统上还登记着 %q——那几个组合键压根录不进去", hot.registered)
	}

	// 按 Esc 退出录制：它们要回来。
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if len(hot.registered) != 6 {
		t.Errorf("退出录制之后系统上是 %q，want 6 条", hot.registered)
	}
}

// TestTheShortcutsComeBackTheMomentARecordingFinishes 验「让开」收得干净。
//
// 这一条量的是头一版真实踩到的那件事：`s.recording` 录完之后从没被清过，于是
// viewShortcuts 从此一帧不登记——**六条命令在本次运行剩下的时间里全部失效，
// 而界面上一个字都不提**。那一格画着新键、带一圈主色描边，看着一切正常。
//
// 走三步：录完立刻按、走回首页按、重新打开设置页按。三步都得响，才说明它不是
// 「碰巧在某一帧好了」。
func TestTheShortcutsComeBackTheMomentARecordingFinishes(t *testing.T) {
	s, tt, _, got := shellOnShortcutPage(t)
	tt.Frame()

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	tt.Key(mygoui.Cmd|mygoui.Shift, mygoui.KeyV)
	tt.Frame()
	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "CommandOrControl+Shift+V" {
		t.Fatalf("没录进去，存的是 %q", got)
	}

	// ① 录完立刻按 ⌘M（切换 mini/完整模式）。
	tt.Key(mygoui.Cmd, mygoui.KeyM)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != "toggleMiniMode" {
		t.Fatalf("录完立刻按 ⌘M 触发的是 %v，want [toggleMiniMode]——快捷键还是哑的", *got)
	}

	// ② 走回首页再按。走这一段是因为「那一页不画了」正是最容易卡住录制状态的地方。
	s.navigate("/")
	tt.Frame()
	tt.Key(mygoui.Cmd, mygoui.KeyM)
	tt.Frame()
	if len(*got) != 2 {
		t.Errorf("走回首页之后按 ⌘M 触发的是 %v，want 第二次也响", *got)
	}

	// ③ 重新打开设置页、停在快捷键那一页再按。
	s.navigate("/settings")
	tt.Frame()
	openSettingsTab(t, tt, "快捷键设置")
	tt.Key(mygoui.Cmd, mygoui.KeyM)
	tt.Frame()
	if len(*got) != 3 {
		t.Errorf("重新打开快捷键设置之后按 ⌘M 触发的是 %v，want 第三次也响", *got)
	}
}

// TestWalkingAwayMidRecordingDoesNotSilenceTheShortcutsForever 验兜底那一条。
//
// 用户点中一个录制格，然后**没按任何键就走了**——点侧栏回首页、点别的设置 Tab。
// 「正在录」只在快捷键设置那一页上才有意义，而 `FocusBind` 并不保证离开时把变量
// 清零（实测：点一格再点侧栏，它还留着）。留着 = 快捷键整份不响。
//
// 这条走的正是 `viewShortcuts` 里「这一帧没画录制格就清掉」那半句。
func TestWalkingAwayMidRecordingDoesNotSilenceTheShortcutsForever(t *testing.T) {
	s, tt, _, got := shellOnShortcutPage(t)
	tt.Frame()

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	tt.Key(0, mygoui.KeyEscape) // 回到「没在录」，下面要的是「点了就走开」那一条路
	tt.Frame()
	clickCell(t, tt, "应用内快捷键：播放/暂停")
	if s.recording == (shortcutSlot{}) {
		t.Fatal("点了那一格没有进入录制")
	}

	// 没按任何键就点侧栏回首页。
	s.navigate("/")
	tt.Frame()
	tt.Frame() // 录制状态的兜底要等到「这一帧没画录制格」才生效，留两帧给它

	tt.Key(mygoui.Cmd, mygoui.KeyM)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != "toggleMiniMode" {
		t.Errorf("点了一格就走开之后按 ⌘M 触发的是 %v，want [toggleMiniMode]"+
			"（录制状态卡住了，快捷键整份不响）", *got)
	}
}

// TestTwoSpellingsOfTheSameKeyAreNotBothRegistered 验同一个键的两种写法。
//
// `CommandOrControl+P` 和 `Cmd+P` 是同一个组合键（`accelModFromName` 把 cmd/
// command/super/meta/win 都认成 `ui.Cmd`），而它们逐字不相等。只比串的话两套机制
// 会各自认领同一个组合键——正好落进「窗口在前台时它响两次」那件事。
//
// 通过界面录不出这种状态（`accelFromEvent` 总是写 CommandOrControl），只有手改过
// 设置文件才会，而那正是「从 Biu 那边导一份配置过来」会发生的事。
func TestTwoSpellingsOfTheSameKeyAreNotBothRegistered(t *testing.T) {
	s, _, hot, _ := shellWithHotkeys(t)
	if err := s.keys.set(shortcutSlot{id: "togglePlay"}, "CommandOrControl+P"); err != nil {
		t.Fatal(err)
	}
	if err := s.keys.set(shortcutSlot{global: true, id: "togglePlay"}, "Cmd+P"); err != nil {
		t.Fatal(err)
	}
	s.syncGlobalShortcuts()

	if hot.has("Cmd+P") {
		t.Errorf("Cmd+P 和应用内的 CommandOrControl+P 是同一个键，两套机制都登记了：%q", hot.registered)
	}
	// 而另外五条一个都不能少——防的是「认成同一个」把不相干的键也一起吞掉。
	if len(hot.registered) != 5 {
		t.Errorf("登记了 %d 条，want 5 条：%q", len(hot.registered), hot.registered)
	}
	// 而 CommandOrControl+P 与 Ctrl+P 在 mac 上**不是**同一个键（前者 Super、
	// 后者 Ctrl），不能被错当成同一个。
	if err := s.keys.set(shortcutSlot{global: true, id: "togglePlay"}, "Ctrl+P"); err != nil {
		t.Fatal(err)
	}
	s.syncGlobalShortcuts()
	if !hot.has("Ctrl+P") {
		t.Errorf("Ctrl+P 和 mac 上的 CommandOrControl+P 不是同一个键，却被当成同一个吞掉了：%q",
			hot.registered)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 六条命令各自做什么
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSixCommandsCallTheirOwnThing 验每一条命令接到了自己的动作上。
//
// 逐条点名，因为「接错一条」的症状是「按了播放暂停在跳音量」——界面上一行提示
// 都没有，而用户只会得出「这应用的快捷键是坏的」这个结论。
//
// 用真的那一份 `runShortcut`：音量读得到（`App.SetVolume` 不需要播放器），
// 迷你播放器是**另一个窗口**（看主窗口的外壳上有没有挂上那个迷你外壳就够了——
// 建窗口要事件循环，那一步在 `ensureMiniWindow`）。播放/暂停、上一首、下一首三条
// 要播放器，没有歌时它们什么都不做——那正是「队列是空的按了也不出错」。
func TestTheSixCommandsCallTheirOwnThing(t *testing.T) {
	s, tt := newShell(t)
	tt.Frame()

	// 音量：一步 0.05（Biu 的 `volume + 0.05`），钳在 0..1。
	s.app.SetVolume(0.5)
	s.runShortcut("volumeUp")
	if got := s.app.Snapshot().Volume; got != 0.55 {
		t.Errorf("按了一次音量加之后是 %v，want 0.55", got)
	}
	s.runShortcut("volumeDown")
	if got := s.app.Snapshot().Volume; got != 0.5 {
		t.Errorf("按了一次音量减之后是 %v，want 0.5", got)
	}
	// 到顶之后不会冲出去：冲出去的那一下是「突然大声」。
	for range 100 {
		s.runShortcut("volumeUp")
	}
	if got := s.app.Snapshot().Volume; got != 1 {
		t.Errorf("一直按音量加之后是 %v，want 1（钳住）", got)
	}
	for range 100 {
		s.runShortcut("volumeDown")
	}
	if got := s.app.Snapshot().Volume; got != 0 {
		t.Errorf("一直按音量减之后是 %v，want 0（钳住）", got)
	}

	// 迷你播放器：开出来，再关掉。
	//
	// ⚠️ 断的是「主窗口的外壳上有没有挂着那个迷你外壳」，不是路由——迷你播放器现在
	// 是**另一个窗口**（见 `miniplayer.go`），和 `/mini-player` 那一条主窗口里的
	// 页面是两件事。而真窗口建不出来（`mygo.NewWindow` 要应用 ready），所以这一条
	// 走的是 `newMiniShell`/`dropMini` 那两半——它们就是 `openMiniPlayer` /
	// `closeMiniPlayer` 的内容。
	s.runShortcut("toggleMiniMode")
	if s.mini == nil {
		t.Fatal("按了切换 mini 之后没有迷你播放器那个外壳")
	}
	tt.Frame()
	s.runShortcut("toggleMiniMode")
	if s.mini != nil {
		t.Error("再按一次切换 mini 之后它还挂着——这个键切不回来")
	}
	tt.Frame()

	// 播放/暂停、上一首、下一首：没有歌时它们什么都不做，而**不是**把界面弄坏。
	for _, id := range []string{"togglePlay", "prev", "next"} {
		s.runShortcut(id)
	}
	tt.Frame()
	if !tt.HasText("还没有打开文件") {
		t.Errorf("空队列里按了三条播放命令之后播放条不空了，文字有 %q", tt.Texts())
	}
}

// TestAShortcutRunsTheSameCommandWhicheverWayItArrives 验两条路走的是同一个动作。
//
// 应用内分发（帧里）和全局分发（系统回调里）接的是同一个 `onShortcut`。它们各自
// 换一份实现的话，其中一条会静悄悄地变成别的动作。
func TestAShortcutRunsTheSameCommandWhicheverWayItArrives(t *testing.T) {
	s, tt, hot, got := shellWithHotkeys(t)
	tt.Frame()
	s.syncGlobalShortcuts()

	tt.Key(mygoui.Cmd, mygoui.KeyP)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != "togglePlay" {
		t.Fatalf("应用内那一条触发的是 %v，want [togglePlay]", *got)
	}

	// 全局那一条是 ⌘⌥P（默认比应用内多一个 ⌥）。
	hot.fire(t, "CommandOrControl+Alt+P")
	if len(*got) != 2 || (*got)[1] != "togglePlay" {
		t.Errorf("全局那一条触发的是 %v，want 第二个也是 togglePlay", *got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 全局注册
// ─────────────────────────────────────────────────────────────────────────────

// TestTheGlobalColumnIsRegisteredOnStartup 验启动时按设置把全局那一列登记上。
//
// 用的是存下来的那一串，一个字都不翻译（见 shortcut_global.go 的文件头）。
func TestTheGlobalColumnIsRegisteredOnStartup(t *testing.T) {
	s, _, hot, _ := shellWithHotkeys(t)

	s.syncGlobalShortcuts()

	want := defaultShortcutSettings().GlobalShortcuts
	if len(hot.registered) != len(want) {
		t.Fatalf("登记了 %d 条，want %d 条（%q）", len(hot.registered), len(want), hot.registered)
	}
	for i, it := range want {
		if hot.registered[i] != it.Shortcut {
			t.Errorf("第 %d 条登记的是 %q，want %q", i, hot.registered[i], it.Shortcut)
		}
	}
	// 而应用内那一列**不该**登记到系统上：它压根不碰系统登记，登记了的话在别的
	// 应用里按 ⌘P 会去抢别人的 ⌘P。
	for _, acc := range hot.registered {
		if acc == "CommandOrControl+P" {
			t.Errorf("应用内那一条 ⌘P 也登记到系统上了：%q", hot.registered)
		}
	}
}

// TestTurningGlobalShortcutsOffUnregistersThem 验那个总开关。
//
// 关掉之后系统上一个都不能留着：留着的话界面上写着「关掉了」，而在别的应用里
// 按 ⌘⌥P 本应用还在响应——而**重启之后**那个键就再也不会被本应用登记回去。
func TestTurningGlobalShortcutsOffUnregistersThem(t *testing.T) {
	s, tt, hot, _ := shellOnShortcutPage(t)
	s.syncGlobalShortcuts()
	if len(hot.registered) == 0 {
		t.Fatal("一条都没登记，后面的验算无从谈起")
	}

	// 走**设置页上那个开关**而不是直接改存储：这一条钉的是接线，而调存储再手动
	// 通知一次的话，「设置页改完没人通知」照样通过。
	clickGlobalToggle(t, tt)

	if len(hot.registered) != 0 {
		t.Errorf("在设置页上关掉之后还登记着 %q", hot.registered)
	}
	// 再打开要重新登记上：全撤全建而不是逐条改（见 syncGlobalShortcuts 的注释）。
	clickGlobalToggle(t, tt)
	if len(hot.registered) != 6 {
		t.Errorf("再打开之后登记了 %d 条，want 6 条", len(hot.registered))
	}
}

// TestRecordingAGlobalShortcutRegistersTheNewKey 验改一个全局键之后立刻重新登记。
//
// 这一条钉的是接线：设置页改完之后有没有人去通知登记的那一半。没通知的话用户
// 看到的是「设置页上写着 ⌘⌥M，在别的应用里按 ⌘⌥P 还有反应」——改了个键却像是
// 没改。
func TestRecordingAGlobalShortcutRegistersTheNewKey(t *testing.T) {
	s, _, hot, _ := shellWithHotkeys(t)
	s.syncGlobalShortcuts()

	// ⌘⌥W 是那六条默认键之外的——故意避开 ⌘⌥M（切换 mini 那一条），用了它就是
	// 「同列冲突」，那是另一件事（设置页上会描红），而这一条量的是「改一个键之后
	// 登记有没有跟着改」。
	s.applyShortcut(shortcutSlot{global: true, id: "togglePlay"}, "CommandOrControl+Alt+W")

	if hot.has("CommandOrControl+Alt+P") {
		t.Errorf("旧的 ⌘⌥P 还登记着：%q", hot.registered)
	}
	if !hot.has("CommandOrControl+Alt+W") {
		t.Errorf("新的 ⌘⌥W 没有登记：%q", hot.registered)
	}

	// 而改的是**应用内**那一列时，全局那一列原样不动：整份逐条比，别只看一条
	// （只看一条的话，「改应用内那一条顺手把全局那一条也撤了」会漏过去）。
	before := strings.Join(hot.registered, ",")
	s.applyShortcut(shortcutSlot{id: "togglePlay"}, "CommandOrControl+K")
	if got := strings.Join(hot.registered, ","); got != before {
		t.Errorf("改应用内那一列把全局那一列也动了：\n 改之前 %s\n 改之后 %s", before, got)
	}
}

// TestResettingTheShortcutsRegistersThemAgain 验「恢复默认」也重新登记。
//
// 恢复默认改的是**整份**两列，而它走的是 `applyShortcutChange` 那条出口。两条
// 出口漏掉一条的症状一样：点完「恢复默认」，别的应用里还是旧的键。
func TestResettingTheShortcutsRegistersThemAgain(t *testing.T) {
	_, tt, hot, _ := shellOnShortcutPage(t)
	// 先改一个全局键，再点「恢复默认」——两次都走设置页。两次的出口不同
	// （一个走 applyShortcut，一个走 applyShortcutChange），而症状一样。
	clickCell(t, tt, "全局快捷键：播放/暂停")
	tt.Key(mygoui.Cmd|mygoui.Alt, mygoui.KeyZ)
	tt.Frame()
	if !hot.has("CommandOrControl+Alt+Z") {
		t.Fatalf("改完之后没有登记新键：%q", hot.registered)
	}

	if err := tt.Click("恢复默认"); err != nil {
		t.Fatalf("点不到「恢复默认」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	if hot.has("CommandOrControl+Alt+Z") {
		t.Errorf("恢复默认之后那个改过的键还登记着：%q", hot.registered)
	}
	if !hot.has("CommandOrControl+Alt+P") {
		t.Errorf("恢复默认之后没有登记回默认值：%q", hot.registered)
	}
}

// TestAGlobalShortcutThatIsAlsoAnInAppOneIsNotRegistered 验两列撞同一个键时只登记一半。
//
// 冲突只在**同一列之内**判（设置页明写着「两列互不相干」），所以「应用内 ⌘P、
// 全局也 ⌘P」是允许存在的状态。而两套机制在不同平台上的重叠方式不一样：
// macOS 的 Carbon 热键会吃掉这个组合键，X11 是 root 上抓的、不管焦点在哪都响。
// 两种都会在「窗口在前台」时把它响两次——「暂停之后立刻又接着播」就是它的样子。
func TestAGlobalShortcutThatIsAlsoAnInAppOneIsNotRegistered(t *testing.T) {
	s, tt, hot, got := shellWithHotkeys(t)
	s.applyShortcut(shortcutSlot{global: true, id: "togglePlay"}, "CommandOrControl+P")
	tt.Frame()

	if hot.has("CommandOrControl+P") {
		t.Errorf("和应用内那一条重了的 ⌘P 也登记到系统上了：%q", hot.registered)
	}
	// 应用内那一半照常工作：让路的只是全局那一半。
	tt.Frame()
	tt.Key(mygoui.Cmd, mygoui.KeyP)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != "togglePlay" {
		t.Errorf("应用内那一条被连坐了，触发的是 %v", *got)
	}
	// 而且要说给用户听——不登记是有代价的（这个键在别的应用里不响了）。
	if !hasToastContaining(tt, "播放/暂停") {
		t.Errorf("没告诉用户哪一条没登记上，画面上有 %q", tt.Texts())
	}
}

// TestOneGlobalShortcutThatCannotRegisterDoesNotTakeTheRestDown 验注册失败要降级。
//
// 全局快捷键是加分项：别的应用先占了那个键、Wayland 上没有桌面 portal、
// Linux 上没有 X11 会话——这些都会让 Register 返回错误。这时候必须**继续往下
// 走**：剩下的照常登记，应用内那一列压根不碰系统所以照常用，界面上弹一句说清
// 哪几条没登记上。
//
// 反过来做——因为一条失败就把整个快捷键功能关掉——才是这一条真正要防的。
func TestOneGlobalShortcutThatCannotRegisterDoesNotTakeTheRestDown(t *testing.T) {
	s, tt, hot, got := shellWithHotkeys(t)
	hot.failFor["CommandOrControl+Alt+P"] = true
	s.syncGlobalShortcuts()

	if hot.has("CommandOrControl+Alt+P") {
		t.Error("失败的那一条居然登记上了")
	}
	if len(hot.registered) != 5 {
		t.Errorf("失败一条之后只登记了 %d 条，want 5 条：注册失败把后面的也带走了", len(hot.registered))
	}
	tt.Frame() // 提示是浮层上的字，得先画一帧才看得见
	if !hasToastContaining(tt, "播放/暂停") {
		t.Errorf("没告诉用户哪一条没登记上，画面上有 %q", tt.Texts())
	}
	// 应用内那一列照常用。
	tt.Frame()
	tt.Key(mygoui.Cmd, mygoui.KeyP)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != "togglePlay" {
		t.Errorf("全局那一条注册失败把应用内那一条也带走了，触发的是 %v", *got)
	}
}

// TestEveryGlobalShortcutThatFailsStillLetsTheOthersRun 验每一条注册失败都只是它自己。
//
// 上面那一条钉的是「失败一条」；这一条钉的是「每一条都要能失败」。前者会因为
// 实现写成「第一条失败就 return」而红——而那种写法在真实机器上极常见：用户
// 自己常用的那个键被别的应用占了，于是六条里一条都登记不上。
func TestEveryGlobalShortcutThatFailsStillLetsTheOthersRun(t *testing.T) {
	for _, fail := range []string{
		"CommandOrControl+Alt+P", "CommandOrControl+Alt+Left", "CommandOrControl+Alt+Right",
		"CommandOrControl+Alt+Up", "CommandOrControl+Alt+Down", "CommandOrControl+Alt+M",
	} {
		t.Run(fail, func(t *testing.T) {
			s, _, hot, _ := shellWithHotkeys(t)
			hot.failFor[fail] = true
			s.syncGlobalShortcuts()
			if len(hot.registered) != 5 {
				t.Errorf("%s 失败之后登记了 %d 条，want 5 条", fail, len(hot.registered))
			}
		})
	}
}

// TestQuittingUnregistersTheGlobalShortcuts 验退出时撤掉。
//
// 进程退出时系统本来就会放掉这些登记，而 OnWillQuit 之后紧接着就是退出——所以
// 这一条真正管的是「托盘点退出」到进程真死之间的那几帧：这几帧里别的应用抢到
// 同一个键的话，用户刚退出去的东西可能立刻又被别的一个进程接住。
func TestQuittingUnregistersTheGlobalShortcuts(t *testing.T) {
	s, _, hot, _ := shellWithHotkeys(t)
	s.syncGlobalShortcuts()
	before := hot.cleared

	s.uninstallGlobalShortcuts()

	if hot.cleared != before+1 {
		t.Errorf("退出时撤了 %d 次，want 1 次", hot.cleared-before)
	}
	if len(hot.registered) != 0 {
		t.Errorf("退出之后还登记着 %q", hot.registered)
	}
}

// TestSyncingTwiceLeavesNoDuplicate 验同步是幂等的。
//
// 全撤全建这个形状本来就是为幂等去的（`Register` 撞上同一个 accelerator 会直接
// 报错，见 `mygo/modules.go:560`）。而设置页上「录一个键」「恢复默认」「那个开关」
// 三条出口都会走到同步，同一个键被同步两次是常事。
func TestSyncingTwiceLeavesNoDuplicate(t *testing.T) {
	s, _, hot, _ := shellWithHotkeys(t)
	s.syncGlobalShortcuts()
	s.syncGlobalShortcuts()

	if len(hot.registered) != 6 {
		t.Errorf("同步两次之后登记着 %d 条，want 6 条", len(hot.registered))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 设置页上写给用户看的那一句
// ─────────────────────────────────────────────────────────────────────────────

// TestTheShortcutPageSaysWhatTheTwoColumnsAreFor 验这一页把两列的分别说清。
//
// 一句已经不做的话留在页面上，比没有更坏：用户按了 ⌘P 发现能用了，而页面上仍然
// 写着「按下去什么也不会发生」，于是反过来以为应用出了别的毛病。
//
// 「切换mini/完整模式」那一行也在这一条里：它切的是**两个窗口**（主窗口收起来 +
// 开一个 320×100 的小窗），所以从别的应用按它同样有用——而别的五条只是往这个进程
// 的状态里写一笔，界面上看不出变化。不说出来的话，读者会以为那一列的六条是一个
// 意思。
func TestTheShortcutPageSaysWhatTheTwoColumnsAreFor(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	for _, want := range []string{"应用内那一列", "全局那一列", "输入框", "切换mini/完整模式」那一条切的是窗口"} {
		if !tt.HasText(want) {
			t.Errorf("这一页没在说「%s」是怎么回事，文字有 %q", want, tt.Texts())
		}
	}
	if tt.HasText("这一版先把键录下来存住") {
		t.Errorf("这一页还在说「只录不生效」，而快捷键现在生效了，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

// clickGlobalToggle 点设置页底下那个「启用全局快捷键」开关，点一次翻一次。
func clickGlobalToggle(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click(enableGlobalLabel); err != nil {
		t.Fatalf("点不到「%s」那个开关：%v，文字有 %q", enableGlobalLabel, err, tt.Texts())
	}
	tt.Frame()
}

// hasToastContaining 报画面上有没有一句含 want 的提示。
//
// Toast 是右下角浮层上的文字，用 `HasText`（子串匹配）就够了：这里找的是一句
// 完整的话的一部分，而那一整页上不会有第二句话含同样的字。
func hasToastContaining(tt *mygoui.Tester, want string) bool {
	return tt.HasText(want)
}

func TestABareSpaceTogglesPlayback(t *testing.T) {
	_, tt, _, got := shellWithHotkeys(t)
	tt.Frame()
	tt.Key(0, mygoui.KeySpace)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != "togglePlay" {
		t.Errorf("按空格触发的是 %v，want [togglePlay]", *got)
	}
}

func TestSpaceDoesNotActivateTheFocusedButton(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	got := watchCommands(s)
	openSettingsTab(t, tt, "菜单设置")
	tt.Frame()
	tt.Key(0, mygoui.KeySpace)
	tt.Frame()
	if len(*got) == 0 || (*got)[len(*got)-1] != "togglePlay" {
		t.Errorf("点过标签之后按空格触发的是 %v，want 以 togglePlay 结尾", *got)
	}
}

func TestPickingATrackFadeLength(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	tt.Frame()
	tt.Scroll(700, 400, 0, 700)
	tt.Frame()
	openSelect(t, tt, "选择淡入淡出时长")
	if err := tt.Click("5 秒"); err != nil {
		t.Fatalf("找不到切歌淡入淡出的 5 秒档：%v（%q）", err, tt.Texts())
	}
	tt.Frame()
	if got := s.app.SettingsSnapshot().TrackFadeSeconds; got != 5 {
		t.Errorf("点了 5 秒，设置里是 %d", got)
	}
}

func TestPickingAnEQPreset(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	tt.Frame()
	tt.Scroll(700, 400, 0, 700)
	tt.Frame()
	openSelect(t, tt, "选择均衡器预设")
	if err := tt.Click("摇滚"); err != nil {
		t.Fatalf("找不到均衡器的摇滚档：%v", err)
	}
	tt.Frame()
	if got := s.app.SettingsSnapshot().EQPreset; got != "rock" {
		t.Errorf("点了摇滚，设置里是 %q", got)
	}
}

// TestTheSeekShortcutsMoveThePosition 验 ⇧← / ⇧→ 真的会快退 / 快进。
//
// 这两条是「只登记在应用内」的自加命令（`inAppOnlyShortcuts`）：Biu 的六条
// 之外，音乐播放器里「跳过几秒」是键盘党的高频动作。
//
// ⚠️ **先按停再量**：素材只有 4 秒，放着不管的话位置自己会走，断言就跟播放
// 抢时间——`Toggle` 暂停之后 tick 不跑（`ScrubEnd` 的注释），位置才钉得住。
//
// ⚠️ **快进那一下用 `seekBy` 直调**：4 秒的素材上按 ⇧→ 必然钳到末尾，而
// 「拖到末尾」之后播放器自己会走完这一首（`ScrubEnd` 的既有行为）——断言
// 就得跟「队列放完」那套语义抢时机。键盘 → 分发 → `seekBy` 这条路 ⇧← 已经
// 走通了，钳制是同一段算术，直调补上就行。
func TestTheSeekShortcutsMoveThePosition(t *testing.T) {
	s, tt := newShell(t)
	// 没有歌的时候按了要什么都不做：一根不知道多长的进度条无处可跳。
	tt.Frame() // 先画一帧：这一帧才登记上快捷键
	tt.Key(mygoui.Shift, mygoui.KeyRight)
	tt.Frame()
	if got := s.app.Snapshot().Position; got != 0 {
		t.Errorf("没歌的时候按 ⇧→ 位置变成了 %v", got)
	}

	openATrack(t, s)
	s.app.Toggle() // 按停，位置才钉得住
	tt.Frame()
	s.app.ScrubEnd(0.5) // 4 秒素材的中点：2s
	if got := s.app.Snapshot().Position; got != 2*time.Second {
		t.Fatalf("起点位置是 %v，want 2s", got)
	}

	// 键盘那一条路：⇧← 从 2s 退 5s，钳到 0。
	tt.Key(mygoui.Shift, mygoui.KeyLeft)
	tt.Frame()
	if got := s.app.Snapshot().Position; got != 0 {
		t.Errorf("按了 ⇧← 之后位置是 %v，want 0（2s − 5s 钳到 0）", got)
	}

	// 快进的算术和钳制：往前 2s 到 2s；越过末尾的那一下钳到时长。
	s.seekBy(2 * time.Second)
	if got := s.app.Snapshot().Position; got != 2*time.Second {
		t.Errorf("seekBy(2s) 之后位置是 %v，want 2s", got)
	}
	s.seekBy(time.Minute)
	if got := s.app.Snapshot().Position; got != s.app.Snapshot().Duration {
		t.Errorf("越过末尾之后位置是 %v，want 钳到时长 %v", got, s.app.Snapshot().Duration)
	}
}
