// 「复制这一行的歌名」这一族（票 #685）。
//
// # 为什么是 Cmd+C 而不是菜单项
//
// 列表行**已经有**「复制歌曲信息」那一项（`menu.go` 的 `copy-info`），而它
// 复制出来的第一行就是「歌名 - UP 名」，再补一项「复制歌名」是同一件事的
// 两个入口。用户真正缺的是**不用点菜单**的那一下：选中一行、⌘C，粘出来的
// 就该是那一行的歌名。
//
// # 为什么不给行标题加 `Selectable`
//
// 🔴 **实测过**（这一票的票面就记着这条）：行标题一旦 `Selectable`，点这段
// 文字就**不再选中那一行**（框架把这一次按下当成了选字的起点，`Selected` 停在
// -1）。而「单击选中、双击播放」是这一列最要紧的两种手势——不能为了让标题可
// 选中而把它们弄坏。所以走键盘：列表本身拿焦点，`Selected` 由点击 / 方向键挪。
package list

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/egoist/mygo/ui"
)

// copyRecorder 记「复制」和「说了一句话」这两样。
type copyRecorder struct {
	copied  []string
	notices []string
}

func (r *copyRecorder) actions(extra Actions) Actions {
	a := extra
	a.Copy = func(text string) { r.copied = append(r.copied, text) }
	a.Notify = func(text string, _ notice.Tone) { r.notices = append(r.notices, text) }
	return a
}

// TestCmdCCopiesTheSelectedRowTitle 断**选中一行之后 ⌘C 复制的是那一行的歌名**。
//
// ⚠️ **两条路都走一遍**（`Key` 与 `Command`）：
//
//	Key(Cmd, KeyC)	无头 Tester / 按键那一路（`InputKeyDown`）
//	Command("copy")	macOS 的「编辑 › 拷贝」菜单那一路（`InputCommand`）
//
// 只做一条的话，另一条在真机上就是「按了没反应」——而那种坏法在测试里看不出来。
func TestCmdCCopiesTheSelectedRowTitle(t *testing.T) {
	for _, route := range []struct {
		name  string
		press func(*ui.Tester)
	}{
		{"按键", func(tt *ui.Tester) { tt.Key(ui.Cmd, ui.KeyC) }},
		{"编辑菜单", func(tt *ui.Tester) { tt.Command("copy") }},
	} {
		t.Run(route.name, func(t *testing.T) {
			rec := &copyRecorder{}
			_, tt := newList(t, rec.actions(Actions{}), []Music{
				{ID: "a", Title: "第一首歌", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "第一首歌"}},
				{ID: "b", Title: "第二首歌", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV2", Title: "第二首歌"}},
			})

			r, ok := tt.Find("第二首歌")
			if !ok {
				t.Fatalf("画面上找不到第二行，文字有 %q", tt.Texts())
			}
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			tt.Frame()

			route.press(tt)
			tt.Frame()

			if len(rec.copied) != 1 || rec.copied[0] != "第二首歌" {
				t.Errorf("%s之后复制出来的是 %q，want 一条「第二首歌」", route.name, rec.copied)
			}
		})
	}
}

// TestCmdCCopiesTheCleanedTitle 断复制出去的是**界面上显示的那个名字**。
//
// 列表画的是清洗过的歌名（`title.Clean`，去掉了「【4K】」「官方MV」那些视频
// 痕迹，见 `titleline.go`）——复制出去的要是同一个。复制出「【4K修复】晴天」
// 而界面上写着「晴天」，用户会以为复制错了行。
func TestCmdCCopiesTheCleanedTitle(t *testing.T) {
	rec := &copyRecorder{}
	_, tt := newList(t, rec.actions(Actions{}), []Music{
		{ID: "a", Title: "【4K修复】晴天", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "【4K修复】晴天"}},
	})
	// 画面上显示的是清洗过的那个名字（这一条先把它量出来，不写死清洗规则）。
	shown, ok := ShowOf(Music{Title: "【4K修复】晴天"}).Title, true
	if !ok || shown == "" {
		t.Fatalf("清洗之后没有名字")
	}
	if _, ok := tt.Find(shown); !ok {
		t.Fatalf("列表里画的不是清洗过的名字 %q，文字有 %q", shown, tt.Texts())
	}

	r, _ := tt.Find(shown)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()

	if len(rec.copied) != 1 || rec.copied[0] != shown {
		t.Errorf("复制出来的是 %q，want 界面上那个 %q", rec.copied, shown)
	}
}

// TestCmdCCopiesNothingWithoutASelection 断**没选中行时 ⌘C 什么都不动**。
//
// 这一条是「别把剪贴板抢走」：剪贴板里原来是别的东西（用户刚从别处复制的），
// 进列表按一下 ⌘C 不该把它抹掉——列表没有选中项，就没有「这一行的歌名」可复制。
func TestCmdCCopiesNothingWithoutASelection(t *testing.T) {
	rec := &copyRecorder{}
	_, tt := newList(t, rec.actions(Actions{}), []Music{
		{ID: "a", Title: "第一首歌", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "第一首歌"}},
	})

	tt.SetClipboard("别处复制来的")
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	tt.Command("copy")
	tt.Frame()

	if len(rec.copied) != 0 {
		t.Errorf("没有选中任何一行，却复制了 %q", rec.copied)
	}
}

// TestCopyingSaysSo 断**复制完说一句**。
//
// ⚠️ **静默的复制是最坏的那一种**：用户按了 ⌘C，界面上什么都没有，他不知道
// 是复制成功了还是这一次按键丢了。列表的右键菜单那一项也是这么做的
// （`menu.go` 的 `copy-info`：「已复制歌曲信息」）。
func TestCopyingSaysSo(t *testing.T) {
	rec := &copyRecorder{}
	_, tt := newList(t, rec.actions(Actions{}), []Music{
		{ID: "a", Title: "第一首歌", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "第一首歌"}},
	})

	r, _ := tt.Find("第一首歌")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()

	if len(rec.notices) != 1 {
		t.Errorf("复制完说了 %q，want 恰好一句", rec.notices)
	}
}

// TestClickingStillSelectsAndDoubleClickingStillPlays 是本票的回归判据：
// **这一列的手势一个都没变**。
//
// 🔴 **它就是「能不能给行标题加 Selectable」的答案**：票面实测过、这里再钉一遍
// ——加了 `Selectable` 之后点标题文字就不再选中那一行。所以方案是 ⌘C，不是
// 给标题加可选。
//
// ⚠️ **判据是 `Selected()` 和一行的 `PlayNow`**：只断「没崩」的话，一个
// 「点一下既不选中也不播放」的实现照样绿。
func TestClickingStillSelectsAndDoubleClickingStillPlays(t *testing.T) {
	var played []app.Item
	l, tt := newList(t, Actions{PlayNow: func(item app.Item) { played = append(played, item) }}, []Music{
		{ID: "a", Title: "第一首歌", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "第一首歌"}},
		{ID: "b", Title: "第二首歌", Item: app.Item{Source: "online", Type: "mv", Bvid: "BV2", Title: "第二首歌"}},
	})

	r, ok := tt.Find("第二首歌")
	if !ok {
		t.Fatalf("画面上找不到第二行，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if got, ok := l.Selected(); !ok || got.ID != "b" {
		t.Fatalf("单击之后 Selected() = (%q, %v)，want (b, true)", got.ID, ok)
	}
	if len(played) != 0 {
		t.Errorf("单击就播放了 %d 次——单击只该选中", len(played))
	}

	// 双击：第二下落在同一个点上，框架会把它算成一次双击。
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if len(played) != 1 || played[0].Bvid != "BV2" {
		t.Errorf("双击之后「立即播放」收到 %v，want 只有 BV2 那一首", played)
	}
}
