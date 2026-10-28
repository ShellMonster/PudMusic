// 票 #345：静音在迷你播放器那一扇窗里要**看得出来**（BMG-180 评审提的）。
//
// # 这一组断的是**画面**，不是「调了哪个函数」
//
// 迷你窗原来**一个字**都不说静音：音量滑块停在用户设的那一档（20%），而那一
// 档在静音时**一动不动**（静音是它自己的一格，票 #311）——于是「滑块停在
// 20%」既可能是音量 20%，也可能是静音，光看滑块分不出来。
//
// 断法是三样：
//
//  1. **画的是哪一张图**：静音时那枚图标必须是「取消静音」那张（和播放条上
//     同一套 `volumeIcon`，两扇窗画的是同一张图）；
//  2. **它看得见**（有墨迹，不是一个空按钮）；
//  3. **点它能取消静音**（不然这扇窗里就没有任何取消静音的办法了——滑块只
//     改音量，改不了静音那一格）。
package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// miniMuteIconLabel 是迷你窗那枚图标**未静音**时的可及名字。
//
// ⚠️ **和播放条上那一枚同名**（上游 `aria-label={isMuted ? "取消静音" : "静音"}`
// 那个做法）：两扇窗里同一个动作叫同一个名字，读屏软件听到的话也一致。
const miniMuteIconLabel = "静音"

// miniMuteIconLabelMuted 是它**静音**时的可及名字。
const miniMuteIconLabelMuted = "取消静音"

// TestTheMiniWindowShowsTheMuteState 钉住「静音时迷你窗里看得见」。
//
// 断法是**图标换了**：静音那一格一动不动（音量还是用户设的那一档），所以
// **唯一**能说明「现在是静音」的画法就是那枚图标换成静音那张。
//
// ⚠️ **断的是「画出来的图变了」而不是「有个叫『取消静音』的控件」**：那个名字
// 是无障碍名字，**看不见**——它变了而画面没变，这一条就白钉了。所以这里量的是
// **图标那一块的墨迹**：静音前后必须是**两张不同的图**。
func TestTheMiniWindowShowsTheMuteState(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.2)
	tt := miniTester(s)
	tt.Frame()

	// 未静音：那一枚叫「静音」。
	if _, ok := tt.Find(miniMuteIconLabel); !ok {
		t.Fatalf("未静音时迷你窗里没有 %q 那一枚，文字有 %q",
			miniMuteIconLabel, tt.Texts())
	}
	upCount, upShape := miniMuteGlyph(t, tt, miniMuteIconLabel)
	if upCount == 0 {
		t.Fatalf("%q 那一枚里没有图形——它是个空按钮", miniMuteIconLabel)
	}

	// 静音：名字变成「取消静音」（上游 `aria-label` 那个做法）。
	a.ToggleMute()
	tt.Frame()
	if _, ok := tt.Find(miniMuteIconLabelMuted); !ok {
		t.Fatalf("静音之后迷你窗里没有 %q 那一枚，文字有 %q",
			miniMuteIconLabelMuted, tt.Texts())
	}
	muteCount, muteShape := miniMuteGlyph(t, tt, miniMuteIconLabelMuted)
	if muteCount == 0 {
		t.Errorf("%q 那一枚里没有图形——它是个空按钮，看不出静音",
			miniMuteIconLabelMuted)
	}

	// **和「响」那一档不是同一张图**：静音图标（喇叭带叉）和响（喇叭带波纹）
	// 必须不同，否则两态画出来一样，等于没画。
	if muteShape == upShape {
		t.Errorf("静音和响画出来的是同一张图（形状一模一样）——静音看不出来")
	}
}

// TestClickingTheMiniMuteIconUnmutes 钉住「点它能取消静音」。
//
// ⚠️ **不做可点的话，这扇窗里就没有取消静音的办法**：滑块只改音量那一格，
// 而静音是**另一格**（票 #311），滑块动不了它。
//
// 断法是**真的按下去**：断言 `App.Snapshot().Muted` 真的翻过来，而音量那一格
// **一动不动**（还是用户设的那一档——这就是 #311 的语义）。
func TestClickingTheMiniMuteIconUnmutes(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.2)
	a.ToggleMute()
	if !a.Snapshot().Muted {
		t.Fatal("起点就不对：没静音")
	}
	tt := miniTester(s)
	tt.Frame()

	if err := tt.Click(miniMuteIconLabelMuted); err != nil {
		t.Fatalf("点不动 %q 那一枚：%v（文字：%q）",
			miniMuteIconLabelMuted, err, tt.Texts())
	}
	tt.Frame()

	st := a.Snapshot()
	if st.Muted {
		t.Error("点了之后还是静音的")
	}
	if st.Volume != 0.2 {
		t.Errorf("点了之后音量变成了 %v，want 0.2——取消静音不该动音量那一格", st.Volume)
	}

	// 再点一下：又静音回去（这一枚是**开关**，不是一个只读的指示器）。
	tt.Frame()
	if err := tt.Click(miniMuteIconLabel); err != nil {
		t.Fatalf("点不动 %q 那一枚：%v（文字：%q）",
			miniMuteIconLabel, err, tt.Texts())
	}
	tt.Frame()
	if !a.Snapshot().Muted {
		t.Error("再点一下没有静音回去")
	}
}

// TestTheMiniMuteIconFollowsTheApp 钉住「迷你窗那枚图标跟着应用层那一格走」。
//
// ⚠️ **它不许自己存一份静音状态**：迷你窗和主窗口是**两个窗口、一份状态**
// （同一个 `App`），所以图标必须**读** `App.Snapshot().Muted`，而不是在
// 壳上留一个自己的字段——否则两扇窗会各说各话（这正是「窗口状态不同步」
// 那一类错）。这里用「从主窗口那边改状态，迷你窗那一枚跟着换」来断它。
func TestTheMiniMuteIconFollowsTheApp(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.2)
	tt := miniTester(s)
	tt.Frame()

	// 未静音：那一枚叫「静音」。
	if _, ok := tt.Find(miniMuteIconLabel); !ok {
		t.Fatalf("未静音时迷你窗里没有 %q 那一枚，文字有 %q",
			miniMuteIconLabel, tt.Texts())
	}

	// **从别处**改状态（不是点迷你窗这一枚）：迷你窗那一枚必须跟着换。
	a.ToggleMute()
	tt.Frame()
	if _, ok := tt.Find(miniMuteIconLabelMuted); !ok {
		t.Errorf("应用层静音之后，迷你窗那一枚还是 %q，文字有 %q——它在读自己的一份状态",
			miniMuteIconLabel, tt.Texts())
	}

	a.ToggleMute()
	tt.Frame()
	if _, ok := tt.Find(miniMuteIconLabel); !ok {
		t.Errorf("应用层取消静音之后，迷你窗那一枚还是 %q，文字有 %q",
			miniMuteIconLabelMuted, tt.Texts())
	}
}

// TestTheMiniMuteIconSaysItIsMuted 钉住「无障碍名字说的是当前状态」。
//
// ⚠️ **图标那一枚的名字是「按下去会怎样」**（上游 `aria-label` 那个做法），
// 所以未静音时它叫「静音」（按下去就静音），静音时叫「取消静音」。
//
// 这条是给读屏软件用的：它看不见图标，只能听名字——名字说「取消静音」，
// 用户才知道自己现在是静音的。
func TestTheMiniMuteIconSaysItIsMuted(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.2)
	tt := miniTester(s)
	tt.Frame()

	// 未静音时**不该**出现「取消静音」那个名字（它说的是「按下去会怎样」）。
	if _, ok := tt.Find(miniMuteIconLabelMuted); ok {
		t.Errorf("没静音的时候就有 %q 那一枚了，文字有 %q",
			miniMuteIconLabelMuted, tt.Texts())
	}

	a.ToggleMute()
	tt.Frame()
	if _, ok := tt.Find(miniMuteIconLabelMuted); !ok {
		t.Errorf("静音之后没有 %q 那一枚，文字有 %q",
			miniMuteIconLabelMuted, tt.Texts())
	}
}

// TestTheMiniMuteStateSurvivesARestart 钉住「静音那一格落盘，UI 层没绕开」。
//
// ⚠️ **UI 层不许自己记一份静音**：它必须走 `App`（落盘在 `App` 那一层）。
// 这一条从**界面**这一头验：点迷你窗那一枚 → 关掉设置 → 重开 → 那一枚
// 仍然说「取消静音」。
//
// （应用层那一份的落盘由 `internal/app` 的 `TestMuteIsItsOwnFlag` 断；这里断的是
// **界面这一层没有绕过它**。）
func TestTheMiniMuteStateSurvivesARestart(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	// ⚠️ **设置要挂在这个 App 上**：落盘是 `App` 那一层做的（`App.persistVolume`），
	// 而 `newMiniShell` 建的那个 App **没挂设置**（`persistVolume` 在没挂设置时
	// 静默跳过）。所以这里自己挂一份，路径留在测试目录里。
	path := filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.2)
	tt := miniTester(s)
	tt.Frame()

	if err := tt.Click(miniMuteIconLabel); err != nil {
		t.Fatalf("点不动 %q 那一枚：%v（文字：%q）",
			miniMuteIconLabel, err, tt.Texts())
	}
	tt.Frame()
	if !a.Snapshot().Muted {
		t.Fatal("点了之后没静音")
	}
	// 落盘是**异步**的（后台合并写），所以先让 `App` 把这一份写完。
	if err := a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	// 重开一个应用读同一份设置。
	again := app.New()
	t.Cleanup(again.Close)
	if err := again.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	if !again.Snapshot().Muted {
		t.Error("重开之后静音那一格没了——界面这一层记了一份自己的状态")
	}
	if got := again.Snapshot().Volume; got != 0.2 {
		t.Errorf("重开之后音量是 %v，want 0.2——静音把音量吃掉了", got)
	}
}

// TestTheMiniMuteIconMatchesThePlayBar 钉住「两扇窗画的是同一张图」。
//
// ⚠️ **这是「一致」那一条的落点**：主窗口播放条上那一枚（`playbar_volume.go`）
// 和迷你窗新加的那一枚，**都必须走 `volumeIcon`**（哪一档配哪一张由那个函数
// 独家说了算，`icons_test.go` 的 `TestTheStateIconsOnlyReachTheScreenThroughTheirFunction`
// 盯着「状态图标只准从那几个函数出去」）。
//
// 所以这里量的是**画出来的图形**：静音时两扇窗那一枚的形状必须一样（都是喇叭
// 带叉那张），不一样就说明其中一扇窗另起了一套。
func TestTheMiniMuteIconMatchesThePlayBar(t *testing.T) {
	// 主窗口：静音那一档画的是 mute 那张（`volumeIcon` 独家决定）。
	bar := playBarTester(t, app.State{HasTrack: true, Title: "一首歌", Rate: 1, Volume: 0.2, Muted: true}, 1200, false)
	barRect, ok := volumeKeyOnTheBar(bar)
	if !ok {
		t.Fatalf("播放条上找不到音量那一枚，文字有 %q", bar.Texts())
	}
	_, barShape := glyphInRect(bar, barRect)
	if barShape == "" {
		t.Fatalf("播放条上那一枚里没有图形")
	}

	// 迷你窗：同一张图。
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.2)
	a.ToggleMute()
	tt := miniTester(s)
	tt.Frame()
	miniRect, ok := tt.Find(miniMuteIconLabelMuted)
	if !ok {
		t.Fatalf("静音之后迷你窗里没有 %q 那一枚，文字有 %q",
			miniMuteIconLabelMuted, tt.Texts())
	}
	_, miniShape := glyphInRect(tt, miniRect)
	if miniShape == "" {
		t.Fatalf("迷你窗那一枚里没有图形")
	}

	// ⚠️ **比的是形状而不是逐像素**：两枚的底色一样（都是各自窗口的卡片底），
	// 而**尺寸不一样**（播放条那枚是 32 的命中区、迷你窗那枚是 22），所以逐像素
	// 比必然不等——比的是**图形本身**的形状。
	if !sameShape(barShape, miniShape) {
		t.Errorf("两扇窗静音时画的不是同一张图：\n播放条 %q\n迷你窗 %q",
			barShape, miniShape)
	}
}

// sameShape 比两段形状串是不是同一张图（**按图形自身的包围盒归一化**后比）。
//
// ⚠️ **不能逐格比**：两枚的**尺寸不一样**（播放条那枚是 32 的命中区、迷你窗
// 那枚是 22），同一张 SVG 按比例缩放之后墨迹的**格数**就不一样了——逐格比
// 必然不等，而那不是「不同的图」。
//
// 所以先**裁到图形自己的包围盒**（去掉四周的空白），再缩到同一个格子数。
// 线宽差半个像素不会改轮廓（那不是另一张图，是同一张图的不同缩放），而
// 「喇叭带叉」和「喇叭带波纹」的轮廓**一定**不同。
func sameShape(a, b string) bool {
	na, nb := normalizeShape(a), normalizeShape(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb
}

// shapeGrid 是归一化之后的形状格子数（24×24）。
//
// 24 是「喇叭带叉」和「喇叭带波纹」这两张图都还分得开的最小格子数（实测
// 两者归一化之后的轮廓在这一个尺度上不同）。
const shapeGrid = 24

// normalizeShape 把形状串**裁到墨迹的包围盒**再缩到 shapeGrid × shapeGrid。
//
// ⚠️ **先裁再缩**：不裁的话，图形在框里的**位置**（播放条那枚的图形在 32 的
// 命中区里居中，迷你窗那枚在 22 的里面居中，比例一样但绝对偏移不同）会把缩放
// 后的格子错开。裁掉四周的空白之后，剩下的就是图形本身。
//
// ⚠️ **缩的是「哪些格子有墨迹」，不是墨迹的浓淡**：线宽差半个像素不该改判。
func normalizeShape(s string) string {
	rows := strings.Split(strings.TrimRight(s, "\n"), "\n")
	// 墨迹的包围盒。
	minX, minY, maxX, maxY := -1, -1, -1, -1
	for y, r := range rows {
		for x := 0; x < len(r); x++ {
			if r[x] != 'x' {
				continue
			}
			if minX < 0 || x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if minY < 0 || y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if minX < 0 {
		return ""
	}
	w, h := maxX-minX+1, maxY-minY+1

	var b strings.Builder
	for gy := 0; gy < shapeGrid; gy++ {
		for gx := 0; gx < shapeGrid; gx++ {
			// 目标格子对应源矩形里的那一小块：有一格墨迹就算有。
			x0 := minX + gx*w/shapeGrid
			x1 := minX + (gx+1)*w/shapeGrid
			y0 := minY + gy*h/shapeGrid
			y1 := minY + (gy+1)*h/shapeGrid
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			has := false
			for y := y0; y < y1 && !has; y++ {
				if y < 0 || y >= len(rows) {
					continue
				}
				row := rows[y]
				for x := x0; x < x1 && x < len(row); x++ {
					if row[x] == 'x' {
						has = true
						break
					}
				}
			}
			if has {
				b.WriteByte('x')
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// glyphInRect 量出 r 那一块里的图形（墨迹形状），底色用众数找。
//
// 找不到图形就返回空串（调用方据此报错）。
func glyphInRect(tt *mygoui.Tester, r mygoui.Rect) (int, string) {
	img := tt.Image()
	x0, y0 := int(r.X), int(r.Y)
	x1, y1 := int(r.X+r.W), int(r.Y+r.H)

	counts := map[color.RGBA]int{}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < 0 || y < 0 || x >= img.Rect.Dx() || y >= img.Rect.Dy() {
				continue
			}
			counts[img.RGBAAt(x, y)]++
		}
	}
	var bg color.RGBA
	best := 0
	for p, n := range counts {
		if n > best {
			bg, best = p, n
		}
	}

	var shape strings.Builder
	ink := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x < 0 || y < 0 || x >= img.Rect.Dx() || y >= img.Rect.Dy() {
				continue
			}
			if img.RGBAAt(x, y) == bg {
				shape.WriteByte('.')
				continue
			}
			ink++
			shape.WriteByte('x')
		}
		shape.WriteByte('\n')
	}
	return ink, shape.String()
}

// miniMuteGlyph 量出迷你窗那枚图标里的**图形**（墨迹像素数，和它的形状）。
//
// ⚠️ **量的是图形而不是控件的框**：框在静音前后一模一样（都是 22×22），
// 量框什么都断不出来。图形是画出来的那几个像素——静音那张（喇叭带叉）和响那张
// （喇叭带波纹）的**形状**不一样，而形状正是「看得出来」的全部。
//
// 底色用**众数**找：那一枚画在迷你窗的卡片底上，而图形是前景色，两者差得开。
// （同一套思路见 `icons_test.go` 的 `scanGlyph`。）
//
// 找不到那一枚就返回 (0, "")，调用方据此报错。
func miniMuteGlyph(t *testing.T, tt *mygoui.Tester, label string) (int, string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		return 0, ""
	}
	return glyphInRect(tt, r)
}

// TestTheMuteGlyphDiffersFromTheLoudOne 钉住「归一化之后两张图仍然分得开」。
//
// ⚠️ **这是给上一条兜底的**：那条比的是「两扇窗静音时画的是同一张图」，而
// 这一条比的是「静音那张和响那张**不是**同一张图」。两条合起来才说明
// 归一化没有把不同的图判成一样的（否则上一条会**假绿**）。
func TestTheMuteGlyphDiffersFromTheLoudOne(t *testing.T) {
	bar := playBarTester(t, app.State{HasTrack: true, Title: "一首歌", Rate: 1, Volume: 0.9, Muted: true}, 1200, false)
	muteRect, ok := volumeKeyOnTheBar(bar)
	if !ok {
		t.Fatalf("播放条上找不到音量那一枚，文字有 %q", bar.Texts())
	}
	_, muteShape := glyphInRect(bar, muteRect)

	loud := playBarTester(t, app.State{HasTrack: true, Title: "一首歌", Rate: 1, Volume: 0.9}, 1200, false)
	loudRect, ok := loud.Find("静音")
	if !ok {
		t.Fatalf("播放条上找不到「静音」那一枚，文字有 %q", loud.Texts())
	}
	_, loudShape := glyphInRect(loud, loudRect)

	if sameShape(muteShape, loudShape) {
		t.Errorf("静音那张和响那张归一化之后一模一样——静音看不出来")
	}
}

// TestTheMiniMuteIconIsThereWithoutATrack 钉住「没歌时那枚也在」。
//
// ⚠️ **静音是全局的一格，不是「这一首歌」的事**：主窗口播放条上那枚
// **无条件画**（`playbar_volume.go` 的 `viewVolumeButton` 不看有没有歌），
// 迷你窗这一枚要一样——不然「没歌时那一扇窗上的音量图标不见了」就成了
// 「静音状态到底还在不在」的另一个说不清。
func TestTheMiniMuteIconIsThereWithoutATrack(t *testing.T) {
	s, a := newMiniShell(t, nil)
	a.SetVolume(0.2)
	a.ToggleMute()
	tt := miniTester(s)
	tt.Frame()

	if _, ok := tt.Find(miniMuteIconLabelMuted); !ok {
		t.Errorf("没歌的时候迷你窗里没有 %q 那一枚，文字有 %q",
			miniMuteIconLabelMuted, tt.Texts())
	}
}

// TestTheMiniVolumeTipSaysItIsMuted 钉住「滑块那一枚的提示在静音时说实话」。
//
// ⚠️ **滑块停在 20% 而现在一点声都没有**——提示写「音量 20%」是**误导**
// （那一格记的是用户设的音量，可声音被静音了）。
//
// 断法是提示**那一句**：静音的时候它必须说「已静音」。
//
// ⚠️ **和主窗口那一侧不一样**：播放条音量浮层里的百分比照旧是那一档（浮层旁边
// 就有那枚图标，说的是同一件事），而迷你窗这扇窗的滑块是**常驻**的，提示是
// 唯一能读到「现在没声」的一行字。
func TestTheMiniVolumeTipSaysItIsMuted(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.2)
	tt := miniTester(s)
	tt.Frame()

	// 未静音：提示是「音量 20%」。
	bar, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("画面上找不到音量条，文字有 %q", tt.Texts())
	}
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()
	if _, ok := tt.Find("音量 20%"); !ok {
		t.Errorf("未静音时滑块上没有 %q 那一枚提示，文字有 %q", "音量 20%", tt.Texts())
	}

	// 静音：提示必须改口。
	a.ToggleMute()
	tt.Frame()
	bar, ok = tt.Find("音量")
	if !ok {
		t.Fatalf("静音之后画面上找不到音量条，文字有 %q", tt.Texts())
	}
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()
	if _, ok := tt.Find("已静音（音量 20%）"); !ok {
		t.Errorf("静音时滑块上没有 %q 那一枚提示，文字有 %q", "已静音（音量 20%）", tt.Texts())
	}
}

// TestTheMiniMuteIconSaysCancelMuteWhenThereIsNoSound 钉住「没声的时候名字要说
// 「取消静音」」。
//
// # 这一条治的是**名字和动作相反**
//
// 「音量 0」在听感上也是没声，而**老设置文件里只有 `volume` 没有 `isMuted`**
// （`isMuted` 是票 #311 才加的那一格）——那种文件恢复出来是**音量 0 且
// 没静音**（`App.MountSettings` 把两格分开恢复，见 `settings_mount.go`）。
//
// 这一档下：
//   - 播放条那枚叫「**取消静音**」（`playbar_volume.go` 的判据是
//     `st.Muted || st.Volume <= 0`）；
//   - 按下去的实际动作是**取消静音**（`App.ToggleMute` 的 `volume <= 0` 分支把
//     音量放回一半）。
//
// ⚠️ **只按 `st.Muted` 判的话，这一档下迷你窗那枚会叫「静音」**：名字说
// 「按下去会静音」，实际却是「取消静音」——**读屏软件读到的名字和按下去的结果
// 相反**，而两扇窗对同一个状态说了两个话。
func TestTheMiniMuteIconSaysCancelMuteWhenThereIsNoSound(t *testing.T) {
	// 造出那个状态：**老设置文件**（只有 `volume`，没有 `isMuted`）。
	dir := writableTempDir(t, "settings")
	path := filepath.Join(dir, app.SettingsFileName)
	if err := os.WriteFile(path, []byte(`{"volume":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	st := a.Snapshot()
	if st.Volume != 0 || st.Muted {
		t.Fatalf("造不出那个状态：Volume=%v Muted=%v（要的是音量 0 且没静音）",
			st.Volume, st.Muted)
	}
	s := NewMini(a, newTestShell(t, onApp(a)).s, nil)
	t.Cleanup(s.closePerWindow)
	s.router.Transition = mygoui.TransitionNone
	tt := miniTester(s)
	tt.Frame()

	// 迷你窗：叫「取消静音」。
	if _, ok := tt.Find(miniMuteIconLabelMuted); !ok {
		t.Errorf("音量 0 且没静音的时候，迷你窗那枚不叫 %q，文字有 %q",
			miniMuteIconLabelMuted, tt.Texts())
	}
	if _, ok := tt.Find(miniMuteIconLabel); ok {
		t.Errorf("音量 0 且没静音的时候，迷你窗那枚**还**叫 %q——名字和按下去的动作相反",
			miniMuteIconLabel)
	}

	// **主窗口播放条那枚叫同一个名字**：两扇窗对同一个状态得说同一句话。
	bar := playBarTester(t, st, 1200, false)
	if _, ok := volumeKeyOnTheBar(bar); !ok {
		t.Fatalf("播放条上找不到音量那一枚，文字有 %q", bar.Texts())
	}
	if _, ok := bar.Find(miniMuteIconLabelMuted); !ok {
		t.Errorf("播放条那枚不叫 %q，而迷你窗那枚叫 %q——两扇窗对同一个状态说了两个话",
			miniMuteIconLabelMuted, miniMuteIconLabel)
	}
}
