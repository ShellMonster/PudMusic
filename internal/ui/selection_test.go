// 票 #197 的界面层证据：**点列表行不再整行刷实心主色底**，「正在播放」那一行
// 仍有 flat 的主色高亮。
//
// ⚠️ **这一组量的是外壳上的画面**，不是 `widgets/list` 那一层：那一层量的是控件自己
// 画的画面，而这一层量的是**页面上**那一行——中间隔着外壳、页边距、主题的装配。
// 症状是静默的：控件里画对了、页面上刷出来还是一整块绿，只有读像素才分得出来。
//
// ⚠️ **深浅两套都要量**：主色在两套主题下是同一个值，而「选中那一行」在两套下
// 长得完全不一样（浅色下是一块亮绿，深色下是一块更刺眼的绿）。
package ui

import (
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// TestClickingARowOnThePageLeavesNoSolidAccentBlock 钉验收第 1 条：点任意一行，
// 整行不再出现实心主色底。
//
// ⚠️ **名字里那个「OnThePage」不是啰嗦**：`widgets/list` 那一层有一条同名的（量的是
// 控件自己画的画面），这一条量的是**外壳上**那一行。证据台账按名字认测试，两条
// 同名的话台账就分不出说的是哪一条了。
//
// 判据是**那一行的底色**：点过之后它既不是主色（没有实心主色块），也和旁边没点过
// 的那一行一模一样。
//
// ⚠️ **指针挪到别的一行上再比**：点过之后指针还压在被点的那一行上，那一行带着
// **悬停**那一档底色——那是悬停给的，不是「选中」给的。
func TestClickingARowOnThePageLeavesNoSolidAccentBlock(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := shotHomeList(t, dark)
			th := BuildTheme(s.app.SettingsSnapshot(), dark)

			first := songName(homeTabs[0], 1)
			if err := tt.Click(first); err != nil {
				t.Fatal(err)
			}
			tt.Frame()

			chosen := rowPixel(t, tt, first)
			if isAccentPixel(chosen, th.Accent) {
				t.Errorf("点过的那一行是 %v，是实心主色底——票 #197 要的是点一行不再整行刷绿",
					chosen)
			}
			// 指针挪到别的一行上，那一行才有悬停底色；被点的那一行这时是干净的。
			hoverTitle(t, tt, songName(homeTabs[0], 2))
			tt.Frame()

			chosen = rowPixel(t, tt, first)
			other := rowPixel(t, tt, songName(homeTabs[0], 3))
			if chosen != other {
				t.Errorf("点过的那一行是 %v，没点过的那一行是 %v：点一行不该改变它的样子",
					chosen, other)
			}
		})
	}
}

// shotHomeList 造一个装着首页推荐音乐列表的外壳（列表那一档）。
func shotHomeList(t *testing.T, dark bool) (*Shell, *ui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	st.DisplayMode = app.DisplayModeList
	b := newTestShell(t, systemIsDark(dark), withSettings(st))
	s := b.s
	songs := homeSongs{new: homeSongsOf("新歌", 4)}
	for i, tab := range homeTabs {
		rows := homeSongsOf(tab, 12)
		songs.tabs[i] = rows
	}
	s.home.SetSongs(songs)
	homeFrame(b.tt)
	return s, b.tt
}

// isAccentPixel 报这个像素是不是**实心主色**。
//
// ⚠️ **判的是「和主色几乎一模一样」**，不是「偏主色」：焦点环是半透明的主色压在行沿
// 上的，它偏主色；而实心主色底是**就是**那个主色。两者必须分得开，所以这里比的是
// 差值（通道差之和 ≤ 24）。
func isAccentPixel(p color.RGBA, accent ui.Color) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a) - int(b)
		}
		return int(b) - int(a)
	}
	return d(p.R, accent.R)+d(p.G, accent.G)+d(p.B, accent.B) <= 24
}

// hoverTitle 把指针挪到 title 那一行上。
//
// ⚠️ **挪走是必要的**：指针压在哪一行，哪一行就带**悬停**那一档底色。拿悬停着的那
// 一行和另一行比颜色，比的是悬停不是选中。
func hoverTitle(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
}

// focusList 把键盘焦点走到列表上（Tab 若干次，直到列表自己那一圈焦点环出现）。
//
// anchor 是这一页上**列表里的某一行**的歌名：列表左沿就是那一行的左沿（行是撑满
// 列表宽度的），而框架画在列表上的那一圈环落在列表盒子外面 3 像素处。
//
// ⚠️ **不写死 Tab 几次**：焦点落在哪取决于这一屏上还有多少能接焦点的东西（顶栏、
// 侧栏、筛选条、每一行行尾那枚「⋯」），写死一个数的话，页面上一加东西就废了。
func focusList(t *testing.T, tt *ui.Tester, anchor string) {
	t.Helper()
	r, ok := tt.Find(anchor)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", anchor, tt.Texts())
	}
	x, y := int(r.X)-2, int(r.Y)+int(r.H)/2
	for range 40 {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		// 列表左沿外面那一列上有东西 → 列表有焦点（框架画在它外面 3 像素处）。
		if tt.Image().RGBAAt(x, y) != tt.Image().RGBAAt(x-8, y) {
			return
		}
	}
	t.Fatalf("Tab 了 40 次还没走到列表上")
}

// TestTheKeyboardRingComesBackAfterAMouseClickOnThePage 钉的是**评审点出来的
// 那一条**（外壳那一层）：鼠标点过一行之后再用方向键走位，键盘走到的那一行**必须
// 有指示**。
//
// ⚠️ **这一条是「框架那个 `focusVisible` 靠不住」的证据**：框架只在 Tab 的时候把
// `rt.focusVisible` 设成 true，指针按一下会把它清掉，而**方向键不会设回来**——
// 所以「点一行 → 按 Down」那一路上，框架的判据一路是 false，屏幕上什么都不画。
// 改动之前那一路是好的（实心主色底跟着键盘走），所以不能只满足于框架的判据。
func TestTheKeyboardRingComesBackAfterAMouseClickOnThePage(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := shotHomeList(t, dark)

			// 点第二行：指针在管，那一行什么标记都没有。
			second := songName(homeTabs[0], 2)
			if err := tt.Click(second); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if rowHasRing(t, tt, second) {
				t.Errorf("鼠标点过的那一行上沿有焦点环——指针在管的时候不该有")
			}

			// 按方向键：键盘在管了，环得跟着键盘走。
			tt.Key(0, ui.KeyDown)
			tt.Frame()
			third := songName(homeTabs[0], 3)
			if !rowHasRing(t, tt, third) {
				t.Errorf("鼠标点过之后按了方向键，键盘走到「%s」了，那一行上沿却没有焦点环",
					third)
			}
			if rowHasRing(t, tt, second) {
				t.Errorf("「%s」那一行上沿还有焦点环——环没有跟着键盘走", second)
			}
		})
	}
}

// rowHasRing 报那一行的上沿有没有焦点环。
//
// 判据是**那个像素和这一行的底色不是一回事**：焦点环是主题里那个 Focus 色（半透明
// 的主色）压在行沿上的，而没有环的那一行上沿就是行自己的底色。
//
// ⚠️ **不看「离主色多近」**：焦点环是半透明的，它离底色比离主色近；而主色换一个
// 色相（绿的、黄的）那一套判据就飘了。这里量的是「和底色差了多少」。
func rowHasRing(t *testing.T, tt *ui.Tester, title string) bool {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	x := int(r.X + r.W/2)
	y := int(r.Y)
	if x < 0 || x >= 1200 || y < 0 || y+6 >= 800 {
		t.Fatalf("「%s」那一行在画面外", title)
	}
	// 这一行自己的底色：往下挪 6 像素，避开环，也还在行的内边距里（8）。
	bg := tt.Image().RGBAAt(x, y+6)
	return tt.Image().RGBAAt(x, y) != bg
}
