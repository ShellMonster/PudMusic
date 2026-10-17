package media

import "testing"

// ─────────────────────────────────────────────────────────────────────────────
// 缩略图那一排按钮
//
// 这一层断的是**规则**：几个按钮、顺序、什么时候可按、按了等于哪条命令。
// 它一个系统调用都不发，所以这一组用例在没有 Windows 的机器上也全跑。
//
// 它们钉住的东西有一个共同点：**错了都不会报错，只会「按了没反应」**。
// 真机上那种错要盯很久才归得到这里。
// ─────────────────────────────────────────────────────────────────────────────

// TestThumbbarIsPrevPlayPauseNext 钉住顺序和数量。
//
// 顺序一改，用户看惯了一眼的那三个图标就跳位了，而这种跳位**每次播放状态
// 变化都会发生一次**（中间那个的图标要换），症状是「点着点着图标跑位了」。
func TestThumbbarIsPrevPlayPauseNext(t *testing.T) {
	for _, playing := range []bool{false, true} {
		bs := thumbbarButtons(true, playing)
		if len(bs) != 3 {
			t.Fatalf("有 %d 个按钮，want 3", len(bs))
		}
		want := []Command{CommandPrev, CommandPause, CommandNext}
		if !playing {
			want[1] = CommandPlay
		}
		for i, b := range bs {
			if b.Command != want[i] {
				t.Errorf("第 %d 个按钮是 %v，want %v（playing=%v）", i, b.Command, want[i], playing)
			}
		}
	}
}

// TestThumbbarIDsSurviveAStateChange 是这一层**最重要**的一条。
//
// 按钮 ID 是系统回传 WM_COMMAND 时带回来的那个数（放在低位），我们靠它认出
// 是哪个按钮。ID 如果跟着播放状态变，就会出现这么一条路：歌在放，用户点中间
// 那个「暂停」，但那一刻状态刚被刷新成暂停、ID 已经换了，我们认出的是别的按钮
// ——要么发成「上一首」，要么发成「播放」（于是按了暂停反而开始播）。
//
// 症状是「偶尔按暂停会变成上一首」，**不报错、不打日志**，因为那条路在系统
// 那边是合法的。
func TestThumbbarIDsSurviveAStateChange(t *testing.T) {
	playing := thumbbarButtons(true, true)
	paused := thumbbarButtons(true, false)
	none := thumbbarButtons(false, false)
	for i := range playing {
		if playing[i].ID != paused[i].ID {
			t.Errorf("第 %d 个按钮在播放中和暂停中的 ID 不同：%#x / %#x", i, playing[i].ID, paused[i].ID)
		}
		if playing[i].ID != none[i].ID {
			t.Errorf("第 %d 个按钮在有歌和无歌时的 ID 不同：%#x / %#x", i, playing[i].ID, none[i].ID)
		}
	}
}

// TestThumbbarIDsAreUnique 断 ID 互不相同。
//
// 两个按钮撞 ID 的后果是「按其中一个两个都响」——界面上的表现是按一下跳两首，
// 而那一票（BMG-14）刚把「一次按键切两首」当成要防的事。
func TestThumbbarIDsAreUnique(t *testing.T) {
	seen := map[uint32]bool{}
	for _, b := range thumbbarButtons(true, true) {
		if seen[b.ID] {
			t.Errorf("命令 ID %#x 出现了两次", b.ID)
		}
		seen[b.ID] = true
	}
}

// TestThumbbarIDsDoNotCollideWithTheStandardNotificationCodes 断 ID 避开了
// WM_COMMAND 的高位通知码那一段。
//
// Windows 的 `WM_COMMAND` 把低位当命令 ID、**高位当通知码**，而通知码那一段
// （BN_CLICKED 之类）是被各种控件占着的。撞上了的症状是「同一个窗口上别的
// 控件按一下，我们这边也收到」，而反过来「我们按的按钮被当成别的控件的通知」。
func TestThumbbarIDsDoNotCollideWithTheStandardNotificationCodes(t *testing.T) {
	const lowReserved = 0x0100
	for _, b := range thumbbarButtons(true, true) {
		if b.ID < lowReserved {
			t.Errorf("命令 ID %#x 落进了低位保留区（< %#x）", b.ID, lowReserved)
		}
	}
}

// TestNoTrackMeansEveryButtonIsDisabled 钉住「没有歌的时候按钮还在，但是灭的」。
//
// 反过来那条（没有歌就把整条按钮栏撤掉）看着更干净，症状也更糟：用户把鼠标悬停
// 到任务栏按钮上什么都没出来，会以为是程序卡了。
func TestNoTrackMeansEveryButtonIsDisabled(t *testing.T) {
	for _, b := range thumbbarButtons(false, false) {
		if b.Enabled {
			t.Errorf("没有歌时按钮 %#x 还是可按的", b.ID)
		}
	}
}

// TestATrackEnablesEveryButton 断有歌的时候三个都能按。
//
// 和上一条是一对：把上一条的实现改成「没有歌就 Enabled=false、其余 true」这种
// 逐个判断时，只断「全灭」是断不出来的——所以这条必须有。
func TestATrackEnablesEveryButton(t *testing.T) {
	for _, playing := range []bool{false, true} {
		for _, b := range thumbbarButtons(true, playing) {
			if !b.Enabled {
				t.Errorf("有歌时按钮 %#x 却是灭的（playing=%v）", b.ID, playing)
			}
		}
	}
}

// TestTheMiddleButtonFollowsTheGlyphOnScreen 断「按当前画出来的那一个键发命令」。
//
// 这是**不**发 Toggle 的理由：中间那一个按钮上画的是播放三角还是两条竖杠，
// 用户就是按那一个。我们发过去的是他看到的那一个，而不是「反过来」。
//
// 去掉这个判断（两条路都发 CommandToggle）在真机上就是：刚播完、状态还没刷新
// 的那一瞬，按「暂停」把歌按停。反过来更糟——按「播放」把正在放的歌按停。
func TestTheMiddleButtonFollowsTheGlyphOnScreen(t *testing.T) {
	playing := thumbbarButtons(true, true)[thumbbarPlayPause]
	if playing.Glyph != glyphPause {
		t.Errorf("在放时中间那个画的是 %v，want 暂停", playing.Glyph)
	}
	if playing.Command != CommandPause {
		t.Errorf("在放时中间那个发的是 %v，want pause", playing.Command)
	}

	paused := thumbbarButtons(true, false)[thumbbarPlayPause]
	if paused.Glyph != glyphPlay {
		t.Errorf("停着时中间那个画的是 %v，want 播放", paused.Glyph)
	}
	if paused.Command != CommandPlay {
		t.Errorf("停着时中间那个发的是 %v，want play", paused.Command)
	}
}

// TestNoButtonEverSaysToggle 钉住「缩略图上不存在 Toggle」。
//
// Toggle 只属于耳机线控那条路（同一个物理键既管播放又管暂停）。缩略图上它是
// 两件不同的事——按钮上的图标会跟着状态换。往这儿塞 Toggle 等于把「用户看到
// 的」和「我们发的」拆开。
func TestNoButtonEverSaysToggle(t *testing.T) {
	for _, hasTrack := range []bool{false, true} {
		for _, playing := range []bool{false, true} {
			for _, b := range thumbbarButtons(hasTrack, playing) {
				if b.Command == CommandToggle {
					t.Errorf("按钮 %#x 发了 toggle", b.ID)
				}
			}
		}
	}
}

// TestCommandIDsRoundTrip 断「ID → 位置」认得回来。
//
// 收到 WM_COMMAND 走的就是这一条（thumbbar_windows.go 里那一次）。认错位置就是
// 按下一首放上一首——**系统不会报任何错**，因为那本来就是一个合法的按钮 ID。
func TestCommandIDsRoundTrip(t *testing.T) {
	for _, playing := range []bool{false, true} {
		for want, b := range thumbbarButtons(true, playing) {
			got, ok := thumbbarCommandOfID(b.ID)
			if !ok {
				t.Fatalf("按钮 %#x 自己发出去的，认不回来", b.ID)
			}
			if int(got) != want {
				t.Errorf("按钮 %#x 认成了位置 %d，want %d", b.ID, got, want)
			}
		}
	}
}

// TestACommandIDFromSomebodyElseIsNotOurs 断不认的 ID 被拒掉。
//
// 同一个窗口上还会发来别的 WM_COMMAND（MyGo 的原生菜单、别的控件）。把不认识
// 的 ID 也当成自己的按钮，回一条 CommandPlay——用户点了一下设置菜单里的东西，
// 歌莫名其妙开始放。
func TestACommandIDFromSomebodyElseIsNotOurs(t *testing.T) {
	for _, id := range []uint32{0, 1, 0x0203, thumbbarFirstID + thumbbarCommandCount, 0xFFFF} {
		if _, ok := thumbbarCommandOfID(id); ok {
			t.Errorf("ID %#x 被认成了我们的按钮", id)
		}
	}
}

// TestGlyphLookupFollowsTheSameRuleAsTheButtons 断图标那条路和命令那条路同源。
//
// 两条路各自算一遍的话早晚会漂：某一次改了中间那个的图标，命令那边跟着改了，
// 图标这边忘了改，于是「按下去是暂停、画出来还是播放」。
func TestGlyphLookupFollowsTheSameRuleAsTheButtons(t *testing.T) {
	for _, playing := range []bool{false, true} {
		bs := thumbbarButtons(true, playing)
		for i, b := range bs {
			got, ok := thumbbarGlyphOfID(b.ID, true, playing)
			if !ok {
				t.Fatalf("按钮 %#x 认不出图标", b.ID)
			}
			if got != b.Glyph {
				t.Errorf("位置 %d：查出来的图标是 %v，按钮上写的是 %v", i, got, b.Glyph)
			}
		}
	}
}

// TestGlyphLookupRejectsForeignIDs 和上面那条是一对，见
// TestACommandIDFromSomebodyElseIsNotOurs。
func TestGlyphLookupRejectsForeignIDs(t *testing.T) {
	if _, ok := thumbbarGlyphOfID(0x0200+9, true, true); ok {
		t.Error("一个不存在的按钮 ID 查出了图标")
	}
}
