package ui

// 刘海固定黑底、不随主题（DESIGN §0′.T5）：浅色主题下刘海里的控件也必须是
// 深色那套配色。
//
// 曾经出错的行为：刘海窗照设置装浅色主题，于是进度条填充是浅色正文色 #141518、
// 轨道是浅色 line-strong #CFD1D4，图标键悬停底是「黑 4.5%」（压在黑底上等于没有），
// 开着的模式键是浅色的 accent-text #B8300F——黑底上要么看不见、要么发闷。

import (
	"image/color"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

func notchNear(got color.RGBA, want mygoui.Color, tol int) bool {
	d := func(a, b uint8) bool {
		v := int(a) - int(b)
		return v >= -tol && v <= tol
	}
	return d(got.R, want.R) && d(got.G, want.G) && d(got.B, want.B)
}

// TestNotchControlsKeepTheDarkPaletteUnderLightTheme 锁住：浅色主题下，刘海的
// 进度条轨道 #3a3a3e / 走过的一段 #ecebe8，图标键悬停是深色那档 hover 底，
// 开着的模式键是深色档的 accent-text。
func TestNotchControlsKeepTheDarkPaletteUnderLightTheme(t *testing.T) {
	n, _ := newNotchShellWith(t, app.Settings{ThemeMode: app.ThemeModeLight})
	if err := enqueueTwo(n.app, t); err != nil {
		t.Fatal(err)
	}
	n.notchSt.open = true
	tt := mygoui.NewTester(n.View, notchWinW, notchWinH)
	n.repaint = tt.Frame
	waitFor(t, "时长读出来了", func() bool { return n.app.Snapshot().Duration > 0 })
	if n.app.Snapshot().Playing {
		n.app.Toggle()
	}
	n.app.SeekTo(n.app.Snapshot().Duration / 2)
	waitFor(t, "停在一半", func() bool {
		st := n.app.Snapshot()
		return !st.Playing && st.Position > 0
	})
	tt.Frame()
	settleNotch(t, tt)

	// 进度条：左 20% 处是走过的那一段，右 85% 处是没走的轨道（指针不在上面，没有旋钮）。
	r, ok := tt.Find("播放进度")
	if !ok {
		t.Fatalf("没找到进度条，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	cy := r.Y + r.H/2
	if got := pixelAt(img, r.X+r.W*0.2, cy); !notchNear(got, notchFill, 3) {
		t.Errorf("进度走过的一段 = %v，want #ecebe8（浅色主题下不能取 #141518）", got)
	}
	if got := pixelAt(img, r.X+r.W*0.85, cy); !notchNear(got, notchRail, 3) {
		t.Errorf("进度轨道 = %v，want #3a3a3e", got)
	}

	// 图标键悬停底：深色 hover（白 7%）压在黑底上，量键的左上角（圆角里面、图形外面）。
	b, ok := tt.Find("随机播放")
	if !ok {
		t.Fatal("没找到随机播放键")
	}
	tt.Move(b.X+b.W/2, b.Y+b.H/2)
	tt.Frame()
	settleNotch(t, tt)
	hover := mygoui.RGB(0, 0, 0).Mix(mygoui.RGB(255, 255, 255), float32(tok.Full.Hover.A)/255)
	if got := pixelAt(tt.Image(), b.X+5, b.Y+b.H/2); !notchNear(got, hover, 3) {
		t.Errorf("随机键悬停底 = %v，want 白 7%% 压黑 %v", got, hover)
	}
	tt.Move(0, 0)

	// 开着的随机键：图形是深色档 accent-text（比浅色档亮得多，R 通道 > 浅色档的 0xB8）。
	n.app.SetPlayMode(app.PlayModeRandom)
	tt.Frame()
	settleNotch(t, tt)
	img = tt.Image()
	var maxR uint8
	for y := int(b.Y); y < int(b.Y+b.H); y++ {
		for x := int(b.X); x < int(b.X+b.W); x++ {
			maxR = max(maxR, img.RGBAAt(x, y).R)
		}
	}
	light := mygoui.Hex("#B8300F") // 浅色朱砂的 accent-text（DESIGN §0′《颜色令牌》）
	if int(maxR) <= int(light.R)+8 {
		t.Errorf("开着的随机键最亮 R = %d，没比浅色 accent-text（%v）亮：还在用浅色配色", maxR, light)
	}
}

// notchShapeBottom 是刘海黑色形状在窗口正中那一列的最低一行（找不到 = -1）。
func notchShapeBottom(tt *mygoui.Tester) int {
	img := tt.Image()
	x := img.Rect.Dx() / 2
	for y := img.Rect.Dy() - 1; y >= 0; y-- {
		if img.RGBAAt(x, y).A > 200 {
			return y
		}
	}
	return -1
}

// TestNotchOpensWithoutOvershoot 锁住：刘海展开是 240ms ease-out、不回弹
// （原型 update-notch.html:74 `.28s ease`、index.html:895 `var(--t-med) var(--ease-out)`）。
// 曾经出错的行为：520ms、bounce 0.28 的弹簧，展开时形状先冲过目标高度再弹回来。
func TestNotchOpensWithoutOvershoot(t *testing.T) {
	n, _ := newNotchShell(t)
	if err := enqueueTwo(n.app, t); err != nil {
		t.Fatal(err)
	}
	if n.app.Snapshot().Playing {
		n.app.Toggle() // 暂停：频谱不要帧，形状的动画停了 FramePending 才会落下
	}
	tt := mygoui.NewTester(n.View, notchWinW, notchWinH)
	n.repaint = tt.Frame
	settleNotch(t, tt)

	n.notchSt.open = true
	tt.Frame()
	tallest := -1
	for range 240 {
		tt.Advance(time.Second / 60)
		tt.Frame()
		tallest = max(tallest, notchShapeBottom(tt))
		if !tt.FramePending() {
			break
		}
	}
	if tt.FramePending() {
		t.Fatal("推了 4 秒的钟，刘海展开还没停")
	}
	final := notchShapeBottom(tt)
	if tallest > final {
		t.Fatalf("展开途中形状底到过 y=%d，停稳在 y=%d：冲过了目标（还是回弹弹簧）", tallest, final)
	}
}

// TestNotchIdleOpenIsATextButton 锁住：空闲卡的「打开」是透明底的文字键，
// 卡里没有主色的播放图标块（原型 update-notch.html:112-116、:237）。
// 曾经出错的行为：左边多一块 36 的主色播放图标，「打开」是 hover 底的胶囊。
func TestNotchIdleOpenIsATextButton(t *testing.T) {
	n, _ := newNotchShell(t)
	n.notchSt.open = true
	tt := mygoui.NewTester(n.View, notchWinW, notchWinH)
	n.repaint = tt.Frame
	settleNotch(t, tt)
	b, ok := tt.Find("打开")
	if !ok {
		t.Fatalf("没找到「打开」，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	// 键的上沿正中（32 高的键、14 号字，字上面那几行）是刘海的黑底，不是胶囊底。
	if got := pixelAt(img, b.X+b.W/2, b.Y+2); !notchNear(got, mygoui.RGB(0, 0, 0), 2) {
		t.Errorf("「打开」键底 = %v，want 透明（露出刘海黑底）", got)
	}
	// 卡片左段（文案左边那一截）没有主色块：整段每个像素都不偏红。
	card, _ := tt.Find("未在播放")
	for y := int(card.Y) - 30; y < int(card.Y+card.H)+10; y++ {
		for x := int(card.X) - 40; x < int(card.X)-2; x++ {
			if p := img.RGBAAt(x, y); p.A > 200 && int(p.R)-int(p.B) > 60 {
				t.Fatalf("文案左边 (%d,%d) 有主色像素 %v：还画着播放图标块", x, y, p)
			}
		}
	}
}
