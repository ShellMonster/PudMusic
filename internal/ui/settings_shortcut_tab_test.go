package ui

import (
	"image"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// inkCount 数一小片画面里有几个深色像素。
func inkCount(img *image.RGBA, r mygoui.Rect) int {
	n := 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if inkAt(img, float32(x), float32(y)) {
				n++
			}
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// 快捷键那一页
// ─────────────────────────────────────────────────────────────────────────────

// TestRecordingAShortcutStoresIt 验录进去存得住，而且画成符号。
//
// ⚠️ 必须**先点那一格**：录制的这一格靠焦点吃键（`HandleInput`），而焦点是被点
// 出来的。直接 `tt.Key` 不点，键会走全局的语义——症状是「测试里按了没反应」，
// 很容易被误判成「录制功能没做」。
func TestRecordingAShortcutStoresIt(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	if got := s.recording; got != (shortcutSlot{id: "togglePlay"}) {
		t.Fatalf("点了那一格没有进入录制（recording=%+v）", got)
	}
	tt.Key(mygoui.Super, mygoui.KeyK)
	tt.Frame()

	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "CommandOrControl+K" {
		t.Errorf("录成 %q，want CommandOrControl+K", got)
	}
	// 画面上摆的是符号（mac 上 ⌘），不是那一串英文——Biu 也是这么画的。
	if mac := macSymbols(); mac && !tt.HasText("⌘+K") {
		t.Errorf("画出来的不是 ⌘+K，文字有 %q", tt.Texts())
	}
	// 两列互不相干。
	if got := s.keys.snapshot().GlobalShortcuts[0].Shortcut; got != "CommandOrControl+Alt+P" {
		t.Errorf("全局那一列被连坐改成了 %q", got)
	}
}

// TestEscapeWhileRecordingBlursAndRecordsNothing 验 Escape：失焦，且不算录入。
//
// 两半都要验，因为它们是同一个键的两种错法：
//
//   - 算录入了 → 用户的 Cancel 变成了「把 Esc 设成播放/暂停的快捷键」。
//   - 没失焦 → 用户按 Esc 以为退出了，下一次按键又被这一格吃掉录进去。
func TestEscapeWhileRecordingBlursAndRecordsNothing(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	if s.recording != (shortcutSlot{}) {
		t.Errorf("按了 Esc 之后还在录 %+v", s.recording)
	}
	if tt.Focused("应用内快捷键：播放/暂停") {
		t.Error("按了 Esc 之后焦点还留在那一格上——下一次按键又会被吃掉")
	}
	if tt.HasText(recordingHint) {
		t.Error("按了 Esc 之后那一格还摆着「按下新的快捷键」")
	}
	// 收起之后那一格照常显示它现在的键（照 Biu：录的时候字也不变）。
	if !tt.HasText(pick(macSymbols(), "⌘+P", "Ctrl+P")) {
		t.Errorf("Esc 之后那一格没有照常显示它现在的键，文字有 %q", tt.Texts())
	}
	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "CommandOrControl+P" {
		t.Errorf("按了 Esc 反而把键改了：%q", got)
	}

	// 真的走了：接下来的键不再被吃掉。
	tt.Key(mygoui.Super, mygoui.KeyJ)
	tt.Frame()
	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "CommandOrControl+P" {
		t.Errorf("Esc 之后按的键又被录进去了：%q", got)
	}
}

// TestBackspaceAndDeleteClearTheCell 验 Backspace / Delete 清空这一格。
func TestBackspaceAndDeleteClearTheCell(t *testing.T) {
	for _, key := range []mygoui.Key{mygoui.KeyBackspace, mygoui.KeyDelete} {
		tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
		openSettingsTab(t, tt, "快捷键设置")

		clickCell(t, tt, "应用内快捷键：播放/暂停")
		tt.Key(0, key)
		tt.Frame()

		if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "" {
			t.Errorf("%v 没有清空那一格，还是 %q", key, got)
		}
		// 正在录的那一格画的是提示（照 Biu：没绑键时那一格是空的，正在录才提示）；
		// 退出录制之后才看得出它画的是「未设置」。
		tt.Key(0, mygoui.KeyEscape)
		tt.Frame()
		if !hasExactText(tt, "未设置") {
			t.Errorf("%v 清空之后那一格画的不是「未设置」，文字有 %q", key, tt.Texts())
		}
		s.keys.Close()
	}
}

// TestAKeyThatCannotBeAShortcutIsNotStored 验非法键不存。
//
// 症状最好认的那一条是 Insert：按下去什么都没发生，而那一格**应该**描红一下
// 让用户知道「刚才那一下没录上」。所以这一条两样都验。
func TestAKeyThatCannotBeAShortcutIsNotStored(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	tt.Key(0, mygoui.KeyInsert)
	tt.Frame()

	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got != "CommandOrControl+P" {
		t.Errorf("录不进的那个键被存进去了：%q", got)
	}
	if s.rejected != (shortcutSlot{id: "togglePlay"}) {
		t.Errorf("录不进去的那一格没有标红，rejected=%+v", s.rejected)
	}

	// 下一个合法的键把红去掉。
	tt.Key(mygoui.Super, mygoui.KeyK)
	tt.Frame()
	if s.rejected != (shortcutSlot{}) {
		t.Errorf("录进了一个合法的键之后红还没消：%+v", s.rejected)
	}
}

// TestRecordingMovesToWhicheverCellWasClicked 验录制的位置跟着点走。
//
// 这一条钉的是一个很难查的 bug：焦点在**建元素的过程中**被按回上一格，于是「点了
// 音量加、开始录的还是播放/暂停」，界面上没有任何报错。
func TestRecordingMovesToWhicheverCellWasClicked(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "应用内快捷键：播放/暂停")
	clickCell(t, tt, "应用内快捷键：下一首")
	if got := s.recording; got != (shortcutSlot{id: "next"}) {
		t.Fatalf("点了「下一首」之后在录 %+v", got)
	}
	tt.Key(mygoui.Super, mygoui.KeyN)
	tt.Frame()
	got := s.keys.snapshot()
	checkShortcut(t, "应用内", got.Shortcuts[0], "togglePlay", "播放/暂停", "CommandOrControl+P")
	checkShortcut(t, "应用内", got.Shortcuts[2], "next", "下一首", "CommandOrControl+N")
}

// TestTheGlobalColumnIsRecordedSeparately 验全局那一列也录得进去。
//
// 两列是**两份**设置。全局那列录不进去的话，用户会发现「Biu 里全局能用，这边不能」，
// 而界面上那一格和另一列长得一模一样。
func TestTheGlobalColumnIsRecordedSeparately(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	clickCell(t, tt, "全局快捷键：音量加")
	tt.Key(mygoui.Super|mygoui.Alt, mygoui.KeyI)
	tt.Frame()

	got := s.keys.snapshot()
	checkShortcut(t, "全局", got.GlobalShortcuts[3], "volumeUp", "音量加", "CommandOrControl+Alt+I")
	checkShortcut(t, "应用内", got.Shortcuts[3], "volumeUp", "音量加", "CommandOrControl+Up")
}

// TestTwoCellsWithTheSameKeyAreMarkedAsConflicting 验冲突。
//
// 判据照 Biu：同一列之内比（`shortcut-settings.tsx:23-39`），空键不算冲突。
// 画出来的是红描边 + 一句「与「音量加」冲突」。
func TestTwoCellsWithTheSameKeyAreMarkedAsConflicting(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	if tt.HasText("与「") {
		t.Fatalf("默认那六条里就有冲突，文字有 %q", tt.Texts())
	}

	clickCell(t, tt, "应用内快捷键：上一首")
	tt.Key(mygoui.Super, mygoui.KeyP)
	tt.Frame()

	if got := s.keys.snapshot().Shortcuts[1].Shortcut; got != "CommandOrControl+P" {
		t.Fatalf("没录进去：%q", got)
	}
	if !tt.HasText("与「播放/暂停」冲突") {
		t.Errorf("撞了却没说出来，文字有 %q", tt.Texts())
	}

	// 把它改成不撞的，红字就没了。
	clickCell(t, tt, "应用内快捷键：上一首")
	tt.Key(mygoui.Super, mygoui.KeyL)
	tt.Frame()
	if tt.HasText("与「") {
		t.Errorf("改成不撞的了还留着冲突提示，文字有 %q", tt.Texts())
	}
}

// 快捷键这一页现在把两列分别管什么说出来了（`shortcut_dispatch_test.go` 里的
// TestTheShortcutPageSaysWhatTheTwoColumnsAreFor 钉着）。原来钉着「这一版只录
// 不生效」的那一条在快捷键接上生效之后就成了页面上的假话——一句已经不做的话留在
// 页面上比没有更坏：用户按了 ⌘P 发现能用了，而页面上仍写着「按下去什么也不会
// 发生」，于是反过来以为应用出了别的毛病。

// TestAShortcutFileFromANewerVersionIsRefusedOnThePage 验只读时页面上也动不了。
//
// 逐项点是因为只读是整份文件的性质：某一格被挡住了而另一格还能改，用户会以为那
// 一次存住了，下次升级回来才发现丢了一半。
func TestAShortcutFileFromANewerVersionIsRefusedOnThePage(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))
	// 造一份「来自更新版本」的快捷键文件。
	s.keys = newShortcutKeys(writeShortcutFileFromNewer(t))
	openSettingsTab(t, tt, "快捷键设置")

	if !tt.HasText("快捷键只读") {
		t.Fatalf("这一页没说清为什么改不了，文字有 %q", tt.Texts())
	}
	clickCell(t, tt, "应用内快捷键：播放/暂停")
	tt.Key(mygoui.Super, mygoui.KeyK)
	tt.Frame()

	if got := s.keys.snapshot().Shortcuts[0].Shortcut; got == "CommandOrControl+K" {
		t.Error("只读的时候居然录进去了")
	}
	_ = a
}

// ─────────────────────────────────────────────────────────────────────────────
// 每一格是不是真的画出来了
// ─────────────────────────────────────────────────────────────────────────────

// TestEveryRecorderCellActuallyDrawsItsKey 验十二个录制格里**那个键真的画在框里**。
//
// 这条是被一条真 bug 逼出来的：`ui.Text(...).Grow(1)` 后面跟一个 `ui.Button`，
// MyGo 会把那个 Text 压成 0.7 DIP 高——框还在、标签还在（Label 不画也在）、
// `Find` 也找得到（返回一个 0x0 的框），可是**一个字都没画出来**。
//
// 所以这一条只能读**像素**：`HasText` 验不到（标签是画不画都在的），`Find` 验不到
// （框在、字不在）。数那一格里的深色像素，十二格都得有。
//
// ⚠️ 那个 × 只在「不录的时候」才画，所以**正在录的那一格不会**中招——症状是
// 「打开这一页全是空框，一格一个键都看不见」。
func TestEveryRecorderCellActuallyDrawsItsKey(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	img := tt.Image()
	cur := defaultShortcutSettings()
	for _, group := range []struct {
		items  []shortcutItem
		header string
	}{
		{cur.Shortcuts, "应用内"},
		{cur.GlobalShortcuts, "全局"},
	} {
		for _, it := range group.items {
			text := formatAccel(accelerator(it.Shortcut))
			r, ok := tt.Find(text)
			if !ok {
				t.Fatalf("%s那一列没有「%s」这个键，文字有 %q", group.header, it.Name, tt.Texts())
			}
			if r.H <= 1 {
				t.Errorf("%s「%s」那一格里的字塌成了 %.1f DIP 高"+
					"（`Grow(1)` 的 Text 后面跟一个 Button 就是这个下场，见 shortcutRecorder）",
					group.header, it.Name, r.H)
				continue
			}
			if ink := inkCount(img, r); ink == 0 {
				t.Errorf("%s「%s」那一格里有字的位置 %q 框高 %.1f，却一个深色像素都没有："+
					"字没画出来", group.header, it.Name, text, r.H)
			}
		}
	}
}

// TestTheRecorderCellsKeepTheirIdentityWhenTheCommandListChanges 验**行和格的身份
// 是命令 id，不是它在列表里的位置**（🔴 铁律四）。
//
// 命令列表不是写死的：它来自用户自己的 `shortcuts.json`，而列表里既可以有用户
// 自己加的命令，也可以没有我们默认的那几条（少一条的会补回来、补在**末尾**）。所以
// 「前面多出一条」「上面那条没了」「顺序变了」都是会真的发生的事。
//
// 默认的元素身份是「父 ID + 兄弟序号」（`ui/context.go`），三种情况都会让序号从
// 某一格开始整体错位一格——于是用户点着「上一首」在录，录完一看，改到「音量减」
// 头上去了，而且界面上没有任何报错。这一条盯的就是这个。
func TestTheRecorderCellsKeepTheirIdentityWhenTheCommandListChanges(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(shortcutSettings) shortcutSettings
	}{
		{"前面多出一条命令（用户自己加的）", func(s shortcutSettings) shortcutSettings {
			s.Shortcuts = append([]shortcutItem{
				{ID: "mine", Name: "我加的", Shortcut: "CommandOrControl+9"},
			}, s.Shortcuts...)
			return s
		}},
		{"上面那条没了", func(s shortcutSettings) shortcutSettings {
			s.Shortcuts = s.Shortcuts[1:]
			return s
		}},
		{"整个顺序倒过来", func(s shortcutSettings) shortcutSettings {
			rev := make([]shortcutItem, len(s.Shortcuts))
			for i := range s.Shortcuts {
				rev[i] = s.Shortcuts[len(s.Shortcuts)-1-i]
			}
			s.Shortcuts = rev
			return s
		}},
	}
	for _, c := range cases {
		tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
		openSettingsTab(t, tt, "快捷键设置")

		// 先点第二行那一列（`prev`）开始录。
		clickCell(t, tt, "全局快捷键：上一首")
		if got := s.recording; got != (shortcutSlot{global: true, id: "prev"}) {
			t.Fatalf("%s：点了「上一首」之后在录 %+v", c.name, got)
		}

		s.keys.mu.Lock()
		s.keys.cur = c.mutate(s.keys.cur)
		s.keys.mu.Unlock()
		tt.Frame()

		if got := s.recording; got != (shortcutSlot{global: true, id: "prev"}) {
			t.Errorf("%s：录制跑到别的命令上去了（%+v，want prev）——"+
				"行和格的身份跟的是位置，不是命令 id（🔴 铁律四）", c.name, got)
		}
		if !tt.Focused("全局快捷键：上一首") {
			t.Errorf("%s：焦点没跟着「上一首」走", c.name)
		}
		s.keys.Close()
	}
}

// TestEveryRecorderCellHasAKeyOfItsOwn 钉住那十二个 key 是**两两不同**的，而且是从
// 命令 id 上来的。
//
// 上面那条盯的是行为；这一条盯的是「key 到底是什么」。两半分开是因为方向相反：
// 上面那条在「状态存成命令 id」时是绿的（今天就是这样），这一条在「key 被整个去掉」
// 时仍然是绿的——真正让上面那条变红的是 `keepRecordingFocus` 每一帧把焦点按回正在
// 录的那一格，它顺带把「身份错了」也盖住了。两条一起在，才说得清现在靠的是哪
// 一样、以及哪一样是规矩要求的。
func TestEveryRecorderCellHasAKeyOfItsOwn(t *testing.T) {
	cur := defaultShortcutSettings()
	seen := map[string]shortcutSlot{}
	for _, item := range cur.Shortcuts {
		for _, slot := range []shortcutSlot{{id: item.ID}, {global: true, id: item.ID}} {
			key := shortcutFieldKey(slot)
			if key == "" {
				t.Errorf("%+v 这一格没有 key", slot)
				continue
			}
			if !strings.Contains(key, slot.id) {
				t.Errorf("「%s」的 key 是 %q，里面没有命令 id——那是位置，不是身份", item.Name, key)
			}
			// 位置不能是 key 的一部分：把一条命令换到别的位置之后 key 必须不变。
			if moved := shortcutFieldKey(shortcutSlot{id: "whatever", global: slot.global}); moved == key {
				t.Errorf("%+v 和别的命令共用了同一个 key %q", slot, key)
			}
			if other, dup := seen[key]; dup {
				t.Errorf("%+v 和 %+v 共用了同一个 key %q", slot, other, key)
			}
			seen[key] = slot
		}
	}
	if len(seen) != 12 {
		t.Errorf("一共 %d 个 key，want 12（六条命令 × 两列）", len(seen))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 那一格右上角那个 ×
// ─────────────────────────────────────────────────────────────────────────────

// TestTheClearButtonInEveryRecorderCellActuallyClears 验十二个 × **每一个都点得动**、
// 而且点了真的会清空。
//
// 这一条是被一个从头带进来的 bug 逼出来的，两处原因叠在一起，缺一处都看不出来：
//
//  1. **那一格是 `ui.Box`（列）**，子元素从上往下排，而且每一个都拉满列宽——于是
//     「键 + ×」变成上下两层，× 那一块（184x29.3）整个掉到 30 高的框**外面**，落在
//     命中区之外。整格扫 60 个点，一个都点不到它。
//  2. **× 只在「没在录」的时候摆**：点它的那一下指针事件先落地，我们的
//     `InputPointerDown` 立刻把 `s.recording` 设成这一格，紧接着 view 才跑——那一帧
//     「正在录」成立，× 根本没被建出来，`Clicked()` 读不到。
//
// 症状是「整格扫 60 个点：清空 0 个，50 个反而把那一格切进录制状态」。两处都改成
// Biu 的样子（那一格是 Row；× 只要有值就摆着）之后才有这一条。
func TestTheClearButtonInEveryRecorderCellActuallyClears(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	cur := defaultShortcutSettings()
	for _, group := range [][]shortcutItem{cur.Shortcuts, cur.GlobalShortcuts} {
		for _, item := range group {
			label := "清空「" + item.Name + "」的快捷键"
			r, ok := tt.Find(label)
			if !ok {
				t.Fatalf("没有「%s」那个 ×，文字有 %q", label, tt.Texts())
			}
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			tt.Frame()

			got := s.keys.snapshot()
			list := got.Shortcuts
			if _, isGlobal := findShortcut(item.ID, got.GlobalShortcuts); isGlobal &&
				sameShortcut(got.GlobalShortcuts, got.Shortcuts) {
				list = got.GlobalShortcuts
			}
			if it, _ := findShortcut(item.ID, list); it.Shortcut != "" {
				t.Errorf("点了「%s」的 × 之后那一格还是 %q", label, it.Shortcut)
			}
			// 清空是一个收尾动作：不该把这一格留在「等着录下一个键」的状态里。
			if s.recording != (shortcutSlot{}) {
				t.Errorf("点了「%s」的 × 之后还在录 %+v", label, s.recording)
			}
			// 清空之后那一格画的是「未设置」，× 也不摆了。
			if !hasExactText(tt, "未设置") {
				t.Errorf("清空之后那一格画的不是「未设置」，文字有 %q", tt.Texts())
			}
			// 摆回默认值，好让下一个 × 有东西可清。
			if err := s.keys.set(shortcutSlot{id: item.ID}, item.Shortcut); err != nil {
				t.Fatal(err)
			}
			if _, isGlobal := findShortcut(item.ID, s.keys.snapshot().GlobalShortcuts); isGlobal &&
				sameShortcut(s.keys.snapshot().GlobalShortcuts, s.keys.snapshot().Shortcuts) {
				if err := s.keys.set(shortcutSlot{global: true, id: item.ID}, item.Shortcut); err != nil {
					t.Fatal(err)
				}
			}
			tt.Frame()
		}
	}
}

// sameShortcut 报两列的内容一模一样（默认设置里两列只差一个 Alt）。
func sameShortcut(a, b []shortcutItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestEveryRecorderCellKeepsItsClearButtonInside 验 × **在格子里面**，键在格子里
// **上下居中**。
//
// 这一条盯的是上一条那个 bug 的另一半（几何那一半）：上一条点的是 × 的**中心**，
// 而 × 掉出框外时它的中心仍然可点（只是点在框外那块空白上），所以只验「点得动」
// 抓不到「画成两层框、× 压着下一行」。
func TestEveryRecorderCellKeepsItsClearButtonInside(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "快捷键设置")

	cur := defaultShortcutSettings()
	for _, item := range append(append([]shortcutItem{}, cur.Shortcuts...), cur.GlobalShortcuts...) {
		cell, ok1 := tt.Find("应用内快捷键：" + item.Name)
		if !ok1 {
			cell, ok1 = tt.Find("全局快捷键：" + item.Name)
		}
		if !ok1 {
			t.Fatalf("没有「%s」那一格，文字有 %q", item.Name, tt.Texts())
		}
		x, ok2 := tt.Find("清空「" + item.Name + "」的快捷键")
		if !ok2 {
			t.Fatalf("没有「%s」那个 ×，文字有 %q", item.Name, tt.Texts())
		}
		if x.X < cell.X || x.Y < cell.Y ||
			x.X+x.W > cell.X+cell.W || x.Y+x.H > cell.Y+cell.H {
			t.Errorf("「%s」那个 × 掉出了格子：× %.1f,%.1f %.1fx%.1f，格 %.1f,%.1f %.1fx%.1f",
				item.Name, x.X, x.Y, x.W, x.H, cell.X, cell.Y, cell.W, cell.H)
		}

		// 键在格子里上下居中：文字的中线和格子的中线差不到两个像素。
		key, ok3 := tt.Find(formatAccel(accelerator(item.Shortcut)))
		if !ok3 {
			t.Fatalf("「%s」那一格里没有键 %q", item.Name, item.Shortcut)
		}
		mid := func(r mygoui.Rect) float32 { return r.Y + r.H/2 }
		if d := mid(key) - mid(cell); d > 2 || d < -2 {
			t.Errorf("「%s」那一格里的键没有上下居中：键中线 %.1f，格子中线 %.1f",
				item.Name, mid(key), mid(cell))
		}
		// 键也不能被挤到第二行（`ui.Box` 是列，排成两层就是那个症状）。
		if key.Y+key.H > cell.Y+cell.H {
			t.Errorf("「%s」那一格里的键掉出了格子", item.Name)
		}
	}
}
