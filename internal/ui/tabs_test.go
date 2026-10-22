package ui

import (
	"image"
	"image/color"
	"math"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组量的是主 Tab 那条**灰底轨道 + 白色滑块**（票 #171）。
//
// 数值的出处见 tabs.go 的文件头：上游 HeroUI 的 `Tabs variant="solid"`
// （`@heroui/theme@2.4.25` 的 `components/tabs.ts`）。所有断言都读**画面**：
//
//   - 轨道和滑块的颜色、位置、尺寸都是逐像素扫出来的；
//   - 动效用 Tester 的假时钟推（`Advance` + `FramePending`），不数帧；
//   - 深浅两套都量，深色下那两档色差更小，最容易画成「一条看不出边界的带」。

// tabBar 是一次量出来的轨道和滑块，坐标都是窗口像素。
type tabBar struct {
	track mygoui.Rect
	pill  mygoui.Rect
}

// tabBarAt 扫出这一排 Tab 的轨道和滑块。
//
// labels 是这一排的全部标签（要用**第一块和最后一块**定横向范围，免得把左边
// 侧栏那块同样颜色的底也算进来）。选中是哪一块不影响这里——滑块正在半路上时
// 也要量得出来（动画那一组就是这么用的）。
//
// 扫法：
//  1. 第一块标签的中线竖着扫**轨道色**，最上/最下就是轨道的上下边；
//  2. 轨道竖向中点横着扫轨道色，最左/最右就是轨道的左右边；
//  3. 轨道范围内横着扫滑块色，得到滑块左右；
//  4. 滑块中线竖着扫滑块色，得到滑块上下。
//
// 不能按「整张图里找白色」判滑块：浅色下页面底色也是白的，会把整张图算进去。
func tabBarAt(t *testing.T, tt *mygoui.Tester, labels []string, dark bool) tabBar {
	t.Helper()
	img := tt.Image()
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	th := BuildTheme(app.DefaultSettings(""), dark)
	track, pill := rgbaOf(trackColor(th)), rgbaOf(sliderColor(th))

	first, ok := tt.Find(labels[0])
	if !ok || first.W <= 0 || first.H <= 0 {
		t.Fatalf("画面上找不到第一块 Tab %q，文字有 %q", labels[0], tt.Texts())
	}
	last, ok := tt.Find(labels[len(labels)-1])
	if !ok || last.W <= 0 || last.H <= 0 {
		t.Fatalf("画面上找不到最后一块 Tab %q，文字有 %q", labels[len(labels)-1], tt.Texts())
	}

	// 1. 轨道上下：第一块标签中线那一列。
	//    中间被标签的字和滑块隔开都不怕——轨道色只在最上面和最下面各 4 像素，
	//    取最小/最大就是轨道的边。
	//
	//    ⚠️ 只在标签上下各 30 像素里扫：页面上还有别的 Surface 色的东西（设置页
	//    的预览卡、首页的卡片底），全图竖扫会把它们和轨道并成一大段，中点也就
	//    不落在 Tab 那一行了。
	cx := int(first.X + first.W/2)
	band := max(0, int(first.Y+first.H/2)-30)
	bandEnd := min(h-1, int(first.Y+first.H/2)+30)
	ty0, ty1 := -1, -1
	for y := band; y <= bandEnd; y++ {
		if img.RGBAAt(cx, y) == track {
			if ty0 < 0 {
				ty0 = y
			}
			ty1 = y
		}
	}
	if ty0 < 0 {
		t.Fatalf("第一块 Tab %q 那一列没有轨道色 %v——灰底轨道没画出来（文字有 %q）",
			labels[0], track, tt.Texts())
	}

	// 2. 轨道左右：只看这一排附近，别把侧栏那块同色的底算进来。
	mid := (ty0 + ty1) / 2
	lo := max(0, int(first.X)-40)
	hi := min(w-1, int(last.X+last.W)+40)
	tx0, tx1 := -1, -1
	for x := lo; x <= hi; x++ {
		if img.RGBAAt(x, mid) == track {
			if tx0 < 0 {
				tx0 = x
			}
			tx1 = x
		}
	}
	if tx0 < 0 {
		t.Fatalf("轨道那一行（y=%d）没有轨道色 %v", mid, track)
	}

	// 3. 滑块左右。
	px0, px1 := -1, -1
	for x := tx0; x <= tx1; x++ {
		if img.RGBAAt(x, mid) == pill {
			if px0 < 0 {
				px0 = x
			}
			px1 = x
		}
	}
	if px0 < 0 {
		t.Fatalf("轨道里没有滑块色 %v——选中的那一块没画出来（轨道 %d..%d）", pill, tx0, tx1)
	}

	// 4. 滑块上下：那一块的中线。
	pcx := (px0 + px1) / 2
	py0, py1 := -1, -1
	for y := ty0; y <= ty1; y++ {
		if img.RGBAAt(pcx, y) == pill {
			if py0 < 0 {
				py0 = y
			}
			py1 = y
		}
	}
	return tabBar{
		track: mygoui.Rect{X: float32(tx0), Y: float32(ty0), W: float32(tx1 - tx0 + 1), H: float32(ty1 - ty0 + 1)},
		pill:  mygoui.Rect{X: float32(px0), Y: float32(py0), W: float32(px1 - px0 + 1), H: float32(py1 - py0 + 1)},
	}
}

// centerX 是滑块的中点。
func (b tabBar) centerX() float32 { return b.pill.X + b.pill.W/2 }

// textCenterX 是某一块标签文字的中线。
func textCenterX(t *testing.T, tt *mygoui.Tester, label string) float32 {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatalf("画面上找不到 %q，文字有 %q", label, tt.Texts())
	}
	return r.X + r.W/2
}

// newSettingsTabShell 打开设置页（主 Tab 是 md 那档：40）。
func newSettingsTabShell(t *testing.T, dark, reduce bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	opts := []shellOpt{withSettings(app.DefaultSettings(""))}
	if dark {
		opts = append(opts, systemIsDark(true))
	}
	if reduce {
		opts = append(opts, withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}))
	}
	s, tt := newShell(t, opts...)
	s.navigate("/settings")
	waitFrames(t, tt, "设置页的主 Tab 画出来", func() bool {
		r, ok := tt.Find(settingsTabNames[0])
		return ok && r.W > 0 && r.H > 0
	})
	// 滑块要等**上一帧**的布局量出来才画（tabs.go 的 mainTabs），多画一帧。
	tt.Frame()
	tt.Frame()
	return s, tt
}

// TestTheMainTabBarHasAGrayTrackAndASelectedBlock 钉住「灰底轨道 + 选中的那一块」。
//
// 判据全是像素：
//   - 轨道是主题的 `Surface`（浅色 #f4f4f5、深色 #27272a）；
//   - 滑块是主题的 `Background`（浅色白）/ `SurfacePressed`（深色）；
//   - 滑块正对着选中那一块的字（中线对齐），并且字还画在滑块**上面**
//     （字色像素还在——滑块要是盖住了文字，那一块就只剩一块白板）。
//
// 深浅两套都量：深色下轨道和滑块只差 20 来个通道值，最容易画成一条看不出边
// 界的东西。
func TestTheMainTabBarHasAGrayTrackAndASelectedBlock(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := newSettingsTabShell(t, dark, false)
			bar := tabBarAt(t, tt, settingsTabNames, dark)

			th := BuildTheme(app.DefaultSettings(""), dark)
			if got, want := bar.track.H, tabMD.track; got != want {
				t.Errorf("轨道高 %.0f，want %.0f（上游 md 的 tab 是 h-8 + 上下各 4）", got, want)
			}
			if got, want := bar.pill.H, tabMD.block; got != want {
				t.Errorf("滑块高 %.0f，want %.0f（上游 md 是 h-8）", got, want)
			}
			if d := bar.centerX() - textCenterX(t, tt, settingsTabNames[0]); math.Abs(float64(d)) > 1.5 {
				t.Errorf("滑块中线在 %.1f，选中的「%s」中线在 %.1f，差 %.1f——"+
					"选中块要正对着选中那一块",
					bar.centerX(), settingsTabNames[0],
					textCenterX(t, tt, settingsTabNames[0]), d)
			}

			// 滑块外面那一圈（轨道）和滑块本身，就是主题里那两个色。
			if got := tt.Image().RGBAAt(int(bar.track.X)+2, int(bar.track.Y+bar.track.H/2)); got != rgbaOf(trackColor(th)) {
				t.Errorf("轨道左边那一点是 %v，want %v", got, rgbaOf(trackColor(th)))
			}
			if got := tt.Image().RGBAAt(int(bar.pill.X)+2, int(bar.pill.Y+bar.pill.H/2)); got != rgbaOf(sliderColor(th)) {
				t.Errorf("滑块左边那一点是 %v，want %v", got, rgbaOf(sliderColor(th)))
			}

			// 文字还在滑块上面：选中那一块的字框里要有字色的像素。
			if !hasColor(tt.Image(), labelRect(t, tt, settingsTabNames[0]), rgbaOf(th.Text)) {
				t.Errorf("选中的「%s」那一块里一个字的像素都没有——滑块把字盖住了",
					settingsTabNames[0])
			}
		})
	}
}

// labelRect 是某块标签文字在画面上的框，找不到就直接失败。
func labelRect(t *testing.T, tt *mygoui.Tester, label string) mygoui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatalf("画面上找不到 %q，文字有 %q", label, tt.Texts())
	}
	return r
}

// hasColor 报 r 里面有没有 want 这个颜色（至少一个像素）。
func hasColor(img *image.RGBA, r mygoui.Rect, want color.RGBA) bool {
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if image.Pt(x, y).In(img.Bounds()) && img.RGBAAt(x, y) == want {
				return true
			}
		}
	}
	return false
}

// TestTheSelectedBlockSlidesWithTheChoice 钉住「切换时选中块滑过去」。
//
// 三条判据：
//  1. 点完的第一帧还在起点（没跳过去）；
//  2. 推钟到半路，滑块在**起点和终点之间**（不是一步到位）；
//  3. 推钟到动画走完，滑块正好停在新的那一块上。
//
// ⚠️ **推钟用 `Advance` + `FramePending`，不数帧**：Tester 的钟站着不动，
// 数帧的等法会静默失效（CLAUDE.md §2）。
func TestTheSelectedBlockSlidesWithTheChoice(t *testing.T) {
	_, tt := newSettingsTabShell(t, false, false)
	labels := settingsTabNames
	start := tabBarAt(t, tt, labels, false)
	from := start.centerX()

	if err := tt.Click(labels[1]); err != nil {
		t.Fatalf("点不到 %q：%v", labels[1], err)
	}
	tt.Frame()
	first := tabBarAt(t, tt, labels, false)
	if math.Abs(float64(first.centerX()-from)) > 0.5 {
		t.Fatalf("点完第一帧滑块就到了 %.1f（起点 %.1f）——这一帧该还在起点，"+
			"不然「中间态」那一条永远量不到", first.centerX(), from)
	}

	to := textCenterX(t, tt, labels[1])
	found := false
	for range 100 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		got := tabBarAt(t, tt, labels, false).centerX()
		if got > min(from, to)+2 && got < max(from, to)-2 {
			found = true
			break
		}
		if !tt.FramePending() {
			break
		}
	}
	if !found {
		t.Fatalf("推钟的整段里滑块一次都没停在 %.0f 和 %.0f 中间——"+
			"要么没动，要么一步跳过去了", from, to)
	}

	for range 100 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		if !tt.FramePending() {
			break
		}
	}
	end := tabBarAt(t, tt, labels, false)
	if d := end.centerX() - to; math.Abs(float64(d)) > 1.5 {
		t.Errorf("动画走完之后滑块中线在 %.1f，「%s」中线在 %.1f，差 %.1f——"+
			"滑块要正好停在新的那一块上", end.centerX(), labels[1], to, d)
	}
}

// TestReduceMotionPutsTheBlockAtTheEndAtOnce 钉住「系统开着减少动态效果时，
// 第一帧就在终点」。
//
// 这一条和上面那条是一对：上面验的是**默认**那一档第一帧还在起点，这里验的是
// 开了偏好之后第一帧已经到终点。两条一起看才说明「直达」真的接在这条路上，
// 而不是「根本没有滑动」。
func TestReduceMotionPutsTheBlockAtTheEndAtOnce(t *testing.T) {
	_, tt := newSettingsTabShell(t, false, true)
	labels := settingsTabNames
	from := tabBarAt(t, tt, labels, false).centerX()

	if err := tt.Click(labels[1]); err != nil {
		t.Fatalf("点不到 %q：%v", labels[1], err)
	}
	tt.Frame()
	end := tabBarAt(t, tt, labels, false)
	to := textCenterX(t, tt, labels[1])
	if math.Abs(float64(end.centerX()-from)) < 2 {
		t.Fatalf("点完第一帧滑块还在起点 %.1f——测试本身没走动（终点 %.1f）", from, to)
	}
	if d := end.centerX() - to; math.Abs(float64(d)) > 1.5 {
		t.Errorf("开了减少动态效果之后第一帧滑块在 %.1f，「%s」在 %.1f，差 %.1f"+
			"——要直接到终点", end.centerX(), labels[1], to, d)
	}
	if tt.FramePending() {
		t.Errorf("开了减少动态效果之后界面还在要帧——说明还在走过渡")
	}
}

// TestTheMainTabHeightMatchesUpstreamSizes 钉住两档高度：md 40 / lg 44。
//
// 上游的算法是「Tab 高 + 轨道上下各 4」：md 的 `h-8`（32）+ 8 = 40，lg 的
// `h-9`（36）+ 8 = 44（DESIGN.md「Tab」一节）。首页那一排是 lg，其余三处是 md
// （上游 `music-recommend/index.tsx:263` 和另外三处的 `size`）。
func TestTheMainTabHeightMatchesUpstreamSizes(t *testing.T) {
	t.Run("设置页是 md", func(t *testing.T) {
		_, tt := newSettingsTabShell(t, false, false)
		bar := tabBarAt(t, tt, settingsTabNames, false)
		if got, want := bar.track.H, tabMD.track; got != want {
			t.Errorf("设置页轨道高 %.0f，want %.0f", got, want)
		}
		if got, want := bar.pill.H, tabMD.block; got != want {
			t.Errorf("设置页滑块高 %.0f，want %.0f", got, want)
		}
	})

	t.Run("首页是 lg", func(t *testing.T) {
		_, tt := newHome(t, homeSongs{
			tabs: [homeTabCount][]list.Music{
				homeSongsOf(homeTabs[0], 3),
				homeSongsOf(homeTabs[1], 3),
				homeSongsOf(homeTabs[2], 3),
			},
		})
		bar := tabBarAt(t, tt, homeTabs[:], false)
		if got, want := bar.track.H, tabLG.track; got != want {
			t.Errorf("首页轨道高 %.0f，want %.0f——首页那一排上游是 size=\"lg\"", got, want)
		}
		if got, want := bar.pill.H, tabLG.block; got != want {
			t.Errorf("首页滑块高 %.0f，want %.0f", got, want)
		}
	})
}

// TestRenderMainTabShots 把两档主 Tab 深浅两套存成 PNG，留给人眼和上游并排看。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderMainTabShots
func TestRenderMainTabShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := newSettingsTabShell(t, dark, false)
			save(t, dir, "主Tab-md-"+name+".png", tt)

			_, homeTT := newHome(t, fullSongs())
			// 首页的 shell 是另造的，深色偏好要单独设。
			homeTT.SetDark(dark)
			homeTT.Frame()
			homeTT.Frame()
			save(t, dir, "主Tab-lg-"+name+".png", homeTT)
		})
	}
}
