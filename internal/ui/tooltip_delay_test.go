package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组量的是 #165 顺带那条 G44：**tooltip 的时延对齐上游 HeroUI 的
// 0 / 300 / 500 三档**。
//
// 上游（HeroUI v2 的 Tooltip）：
//
//	delay       出提示前停多久，默认 0；音量那一处显式 300
//	closeDelay  指针离开后停多久收，默认 500；侧栏 / 顶栏动态 / 新建收藏夹
//	            那几处显式 0
//
// 我们这一侧（`tooltip.go`）：
//
//	tipDefault  0 出、500 收
//	tipMenu     0 出、0 收（上游显式 closeDelay={0} 的那几处）
//	300         音量那个自绘浮层的 volHoverDelay（`playbar_volume.go`），
//	            和 `tipDelayVolume` 是同一个数
//
// 框架自带的 `Element.Tooltip` 是**包级固定 600ms**（`mygo@v0.5.5/ui/tooltip.go:12`），
// 所以界面层不用它——`TestNoFrameworkTooltipsInTheApp` 盯着这件事。

// tipProbe 造一个「只有一个锚点 + 一枚提示」的窗口。
//
// 提示状态挂在 Shell 上，所以必须走真外壳；`withView` 把整窗换成一个固定
// 位置的按钮，量的就只剩时延本身。
func tipProbe(t *testing.T, text string, timing tipTiming) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			ui.Column(c).Padding(40).Children(func() {
				e := ui.Button(c, "锚点").Size(80, 30)
				s.tip(c, e, text, timing)
			})
		}
	}))
	b.tt.Frame()
	return b.s, b.tt
}

// TestTooltipDefaultTiming 验默认那一档：**0ms 出、500ms 收**。
func TestTooltipDefaultTiming(t *testing.T) {
	_, tt := tipProbe(t, "默认提示", tipDefault)
	hoverOn(t, tt, "锚点")

	if !tt.HasText("默认提示") {
		t.Fatalf("指针刚停上去提示没出来——默认的 open delay 应该是 0，文字有 %q", tt.Texts())
	}

	tt.Move(1, 1)
	tt.Frame()
	tt.Advance(499 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("默认提示") {
		t.Errorf("指针离开不到 500ms 提示就收了——默认的 closeDelay 是 500")
	}
	tt.Advance(2 * time.Millisecond)
	tt.Frame()
	if tt.HasText("默认提示") {
		t.Errorf("指针离开 501ms 之后提示还挂着——默认的 closeDelay 是 500")
	}
}

// TestTooltipMenuTiming 验菜单那一档：**0ms 出、离开立刻收**。
func TestTooltipMenuTiming(t *testing.T) {
	_, tt := tipProbe(t, "菜单提示", tipMenu)
	hoverOn(t, tt, "锚点")
	if !tt.HasText("菜单提示") {
		t.Fatalf("指针刚停上去提示没出来，文字有 %q", tt.Texts())
	}

	tt.Move(1, 1)
	tt.Frame()
	if tt.HasText("菜单提示") {
		t.Errorf("指针离开之后提示还挂着——菜单那一档的 closeDelay 是 0")
	}
}

// TestTooltipVolumeTiming 验音量那一档：**300ms 出**（收还是默认的 500）。
//
// 真控件是播放条上的音量浮层（`playbar_volume.go` 的 volHoverDelay，已有
// `playbar_layout_test.go` 那组覆盖）；这里量的是同一档时延在通用提示上生效。
func TestTooltipVolumeTiming(t *testing.T) {
	_, tt := tipProbe(t, "音量提示", tipTiming{open: tipDelayVolume, close: tipDelayClose})
	hoverOn(t, tt, "锚点")

	if tt.HasText("音量提示") {
		t.Fatalf("指针刚停上去提示就出来了——音量那一档的 open delay 是 300ms")
	}
	tt.Advance(299 * time.Millisecond)
	tt.Frame()
	if tt.HasText("音量提示") {
		t.Errorf("299ms 提示就出来了——音量那一档的 open delay 是 300ms")
	}
	tt.Advance(2 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("音量提示") {
		t.Fatalf("301ms 提示还没出来，文字有 %q", tt.Texts())
	}
}

// TestTheNavBarTooltipFollowsTheDefaultTiming 验顶栏真的接上了这份提示。
//
// 颜色和尺寸那几条测的是搜索框；这一条盯的是「顶栏那几枚图标键的悬停提示也
// 走 tooltip.go」——否则只改了样式、提示还是框架那枚 600ms。
func TestTheNavBarTooltipFollowsTheDefaultTiming(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()

	hoverOn(t, tt, "打开文件")
	if !tt.HasText("打开本地音频文件") {
		t.Fatalf("顶栏「打开文件」的提示没有立刻出来（默认 0ms），文字有 %q", tt.Texts())
	}

	tt.Move(1, 1)
	tt.Frame()
	tt.Advance(501 * time.Millisecond)
	tt.Frame()
	if tt.HasText("打开本地音频文件") {
		t.Errorf("顶栏提示在指针离开 501ms 后还挂着——它没走 tipDefault 的 500ms 收")
	}
}

// hoverOn 把指针停在 s 那一格的中间，再画一帧。
func hoverOn(t *testing.T, tt *mygoui.Tester, s string) {
	t.Helper()
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("画面上没有 %q，文字有 %q", s, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

// TestNoFrameworkTooltipsInTheApp 盯着「界面层不再用框架那枚固定 600ms 的提示」。
//
// 例外只有两处：
//   - `tooltip.go` 的 `tipFrom`：页面没有接到 Shell 时（单独造一页的测试）退回
//     框架那枚。生产路径上 `tip` 一定接上了。
//   - `widgets/hover` 里那两枚三角形：那是我们自己加的「从这一首播放」，上游
//     没有对应物，所以没有要对的时延。它们不在这个包的 glob 里。
func TestNoFrameworkTooltipsInTheApp(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "tooltip.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, ".Tooltip(") {
				t.Errorf("%s:%d 还在用框架那枚固定 600ms 的提示——"+
					"换成 `s.withTip` / `tipWith`（tooltip.go）", name, i+1)
			}
		}
	}
}
