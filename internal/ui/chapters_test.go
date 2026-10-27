package ui

import (
	"image"
	"image/color"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 进度条上的章节点（票 #585）。
//
// # 这一族断的是三件事
//
//	点画出来了	有章节的时候，轨道在章节起点那里「断了一截」（背景色的缺口）
//	点了会吸附	点在两个相邻的点之间、当前就在其中一个点上 → 跳到另一个点
//	拖不吸附	拖动永远按原样 seek——章节内部要能自由拖
//
// # 吸附的规则是用户 2026-10-26 口述的那一条
//
// 迷你窗那条进度条又矮又窄，点是冲着它去的：点中了却落在两点之间，比点不中
// 还难受——所以「点」要吸附，「拖」不吸附。规则见 `chapters.go` 的文件头。
//
// ⚠️ **判据是画面上的像素和 App 的位置，不是函数名**：点画没画、seek 落在哪，
// 都只有画面和状态说得清。

// chapterPoints 是 fixture 给的章节：1s 起、2.5s 起（素材那首 FLAC 有 4 秒，
// 两个起点都在它里面）。
func chapterPoints() []any {
	return []any{
		map[string]any{"from": 1, "to": 2.5, "content": "前奏"},
		map[string]any{"from": 2.5, "to": 4, "content": "正题"},
	}
}

// chapterTester 建一个真的在放**在线**歌、而且这一首**带章节**的界面。
//
// 和 `onlineTester`（fullplayer_parts_test.go）是同一个形状：假的 B 站回取流 +
// 章节，歌真的出声——`ScrubEnd` 那一下要真的 seek，得有播放器和时长在。
func chapterTester(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.setViewPoints(chapterPoints())
	return chapterTesterWith(t, f)
}

// chapterTesterWith 是 chapterTester 的后半截，fixture 由调用方给（要造
// 「问不到章节」那一档的时候用）。
func chapterTesterWith(t *testing.T, f *biliFixture) (*Shell, *mygoui.Tester) {
	t.Helper()
	s, tt := newShell(t)
	tt.SetDark(true)
	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	if err := s.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVchapters", Cid: "1",
		Title: "带章节的那一首",
	}); err != nil {
		t.Fatalf("把带章节的那首放进队列：%v", err)
	}
	waitFrames(t, tt, "在线那首开始播", func() bool { return s.app.Snapshot().HasTrack })
	waitFrames(t, tt, "章节问回来了", func() bool { return len(s.app.Snapshot().Chapters) == 2 })
	return s, tt
}

// TestChapterClickTargetFollowsTheUserRule 把吸附规则**表驱动**地过一遍。
//
// 🔴 **规则的每一条都要有自己的行**：这一条是用户口述的行为，没有上游可对——
// 哪一行红了，就是哪一条规矩被改了，一眼看得出来。
func TestChapterClickTargetFollowsTheUserRule(t *testing.T) {
	chs := []app.Chapter{
		{At: 10 * time.Second},
		{At: 30 * time.Second},
		{At: 70 * time.Second},
	}
	const (
		a = 10 * time.Second
		b = 30 * time.Second
		c = 70 * time.Second
	)
	for name, tt := range map[string]struct {
		click, pos time.Duration
		want       time.Duration
		ok         bool
	}{
		// 用户口述的两条：
		"在B点A和B之间→A": {click: 20 * time.Second, pos: b, want: a, ok: true},
		"在A点A和B之间→B": {click: 20 * time.Second, pos: a, want: b, ok: true},
		// 「在那一章里」也算「在这个点上」：pos 在 B 那一章的中间，点 (A,B) 之间 → A。
		"在B那一章里点A和B之间→A": {click: 20 * time.Second, pos: 40 * time.Second, want: a, ok: true},
		"在A那一章里点A和B之间→B": {click: 20 * time.Second, pos: 15 * time.Second, want: b, ok: true},
		// 当前在别处：就近。
		"在C点A和B之间靠A→A": {click: 15 * time.Second, pos: c, want: a, ok: true},
		"在C点A和B之间靠B→B": {click: 25 * time.Second, pos: c, want: b, ok: true},
		"打平算左边的":       {click: 20 * time.Second, pos: c, want: a, ok: true},
		// 严格压线的不算「之间」：原样 seek（点中 B 就是 B，不用绕）。
		"正好点在A上→原样": {click: a, pos: c, want: 0, ok: false},
		"正好点在B上→原样": {click: b, pos: a, want: 0, ok: false},
		// 点在所有点之外：原样。
		"点在开头段→原样": {click: 5 * time.Second, pos: b, want: 0, ok: false},
		"点在末段→原样":  {click: 90 * time.Second, pos: a, want: 0, ok: false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := chapterClickTarget(tt.click, tt.pos, chs)
			if ok != tt.ok {
				t.Fatalf("ok = %v，want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("落点 = %v，want %v", got, tt.want)
			}
		})
	}

	// 没有章节（或少于两个点）永远原样。
	if _, ok := chapterClickTarget(20*time.Second, b, nil); ok {
		t.Error("没有章节还吸附了")
	}
	if _, ok := chapterClickTarget(20*time.Second, b, chs[:1]); ok {
		t.Error("只有一个点也吸附了")
	}
}

// TestTheChapterDotsShowUpOnThePlayBar 断「点画出来了」：章节起点那里，轨道的
// 厚度里出现了**背景色**的像素（缺口）；没有章节的时候同一个地方一个都没有。
//
// 🔴 **深浅两套都断**：缺口用的是主题底色，两套主题里它和轨道的对比方向相反
// （浅色是白点压灰轨，深色是黑点压灰轨），只断一套的话另一套可能整个没画。
func TestTheChapterDotsShowUpOnThePlayBar(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 有章节：两个缺口都在。
			st := barState()
			st.Chapters = []app.Chapter{{At: 30 * time.Second}, {At: 60 * time.Second}}
			tt, s := playbarTester(t, st, 1200, 0, dark)
			bar, ok := tt.Find("播放进度")
			if !ok {
				t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
			}
			bg := rgbaOf(BuildTheme(s.app.SettingsSnapshot(), dark).Background)
			for _, frac := range []float32{0.3, 0.6} {
				if n := notchPixels(tt.Image(), bar, frac, 8, bg); n == 0 {
					t.Errorf("frac %.1f 那个章节起点上没有缺口（背景色像素 0 个）", frac)
				}
			}

			// 没有章节：同样的两处一个缺口都没有——画面和没有这个功能一个样。
			tt2, _ := playbarTester(t, barState(), 1200, 0, dark)
			bar2, ok := tt2.Find("播放进度")
			if !ok {
				t.Fatalf("画面上找不到进度条，文字有 %q", tt2.Texts())
			}
			for _, frac := range []float32{0.3, 0.6} {
				if n := notchPixels(tt2.Image(), bar2, frac, 8, bg); n != 0 {
					t.Errorf("没有章节的时候 frac %.1f 那里也有 %d 个背景色像素——"+

						"轨道凭空断了", frac, n)
				}
			}
		})
	}
}

// notchPixels 数「轨道上 frac 那一处」的背景色像素。
//
// 只量**轨道的厚度**（中线 ±2 像素，4 像素那条轨道正好盖住）：滑块元素其余的
// 地方本来就是透的、露着窗口底色——量宽了的话「没有缺口」那一档永远是红的。
// pad 是旋钮留在轨道里的那半个宽，和滑块的取值映射是同一个数。
func notchPixels(img *image.RGBA, bar mygoui.Rect, frac float32, pad float32, bg color.RGBA) int {
	x := bar.X + pad + (bar.W-2*pad)*frac
	cy := bar.Y + bar.H/2
	// 中线上那两行（轨道 4 像素，头尾两行是抗锯齿）。
	return nearPixels(img, mygoui.Rect{X: x - 2, Y: cy - 1, W: 5, H: 2}, bg)
}

// TestTheMiniProgressShowsTheChapterDots 断迷你窗那条进度条也画点，而且画在
// **框架滑块的轨道上**——它的轨道中线照 `coreSlider` 是 `r.Y + Space(2.5)`，
// 不是元素框的正中（那一档元素只有 12 高）。差半格的话点就浮在轨道外面了。
//
// 这一条走的是**整条链**（真的假 B 站回章节），和播放条那条像素测试
// （手摆状态）是两条腿：一条验「数据到了点上」，一条验「点的画法」。
func TestTheMiniProgressShowsTheChapterDots(t *testing.T) {
	s, a := newMiniShell(t, func() {})
	f := newBiliFixture(t, 1)
	f.setViewPoints(chapterPoints())
	a.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	if err := a.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BVchapters", Cid: "1",
		Title: "带章节的那一首",
	}); err != nil {
		t.Fatalf("把带章节的那首放进队列：%v", err)
	}
	tt := miniTester(s)
	waitFrames(t, tt, "在线那首开始播", func() bool { return a.Snapshot().HasTrack })
	waitFrames(t, tt, "章节问回来了", func() bool { return len(a.Snapshot().Chapters) == 2 })
	tt.Frame()

	bar, ok := tt.Find("播放进度")
	if !ok {
		t.Fatalf("迷你窗里找不到进度条，文字有 %q", tt.Texts())
	}
	dur := a.Snapshot().Duration
	if dur <= 2500*time.Millisecond {
		t.Fatalf("素材只有 %v，章节的起点（1s/2.5s）放不进去", dur)
	}
	bg := rgbaOf(BuildTheme(a.SettingsSnapshot(), false).Background)
	cy := bar.Y + float32(10) // coreSlider 的轨道中线：r.Y + Space(2.5)
	for _, frac := range []float32{
		float32(1.0 / dur.Seconds()), float32(2.5 / dur.Seconds()),
	} {
		x := bar.X + 10 + (bar.W-20)*frac // 框架的取值映射：kw = Space(5) = 20
		if n := nearPixels(tt.Image(), mygoui.Rect{X: x - 2, Y: cy - 2, W: 5, H: 5}, bg); n == 0 {
			t.Errorf("frac %.2f 那个章节起点上没有缺口", frac)
		}
	}
}

// TestAClickBetweenChapterDotsSnapsToTheChapterStart 断「点了会吸附」，两个方向
// 都断（用户口述的那两条）：在 B 点之间 → A；在 A 点之间 → B。
//
// 判据是 **App 的播放位置**：`ScrubEnd` 会同步把位置写过去，播放器也真的 seek
// 到那儿——容差 350 毫秒是给后台那个 50ms 的时钟留的（它会把位置用播放器的
// 真实位置覆盖回来，而那也是 seek 过去的位置，只是歌曲还在往前走）。
func TestAClickBetweenChapterDotsSnapsToTheChapterStart(t *testing.T) {
	s, tt := chapterTester(t)
	dur := s.app.Snapshot().Duration
	if dur <= 2600*time.Millisecond {
		t.Fatalf("素材只有 %v，吸附的两个落点（1s/2.5s）放不进去", dur)
	}
	// 先停住：歌曲一边放一边走的话，「当前在哪个点上」会随机器快慢漂——
	// 慢的机器上两步之间能走出半首，吸附的方向就不是断言里那个了。
	s.app.Pause()
	waitFrames(t, tt, "停住了", func() bool { return !s.app.Snapshot().Playing })
	bar, ok := tt.Find("播放进度")
	if !ok {
		t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
	}
	const pad = float32(8)
	// 点在 1.75s：严格落在 (1s, 2.5s) 这个空档的中间，两边都够远。
	clickFrac := float32(1.75 / dur.Seconds())
	click := func() {
		x := bar.X + pad + (bar.W-2*pad)*clickFrac
		y := bar.Y + bar.H/2
		tt.Press(x, y)
		tt.Release(x, y)
		tt.Frame()
	}
	near := func(got, want time.Duration) bool {
		d := got - want
		if d < 0 {
			d = -d
		}
		return d <= 350*time.Millisecond
	}
	waitAt := func(want time.Duration, what string) {
		t.Helper()
		waitFrames(t, tt, what, func() bool {
			return near(s.app.Snapshot().Position, want)
		})
	}

	// 方向一：当前在 B（2.5s），点 A 和 B 之间 → 跳到 A（1s）。
	s.app.SeekTo(2500 * time.Millisecond)
	waitAt(2500*time.Millisecond, "到位 2.5s")
	click()
	waitAt(1*time.Second, "吸附到 A（1s）")

	// 方向二：当前在 A（1s），点 A 和 B 之间 → 跳到 B（2.5s）。
	s.app.SeekTo(1 * time.Second)
	waitAt(1*time.Second, "到位 1s")
	click()
	waitAt(2500*time.Millisecond, "吸附到 B（2.5s）")
}

// TestADragBetweenChapterDotsDoesNotSnap 断「拖不吸附」：按下去挪了指针的那一下
// 永远按原样 seek——章节内部要能自由拖，这是吸附规则的另一半。
func TestADragBetweenChapterDotsDoesNotSnap(t *testing.T) {
	s, tt := chapterTester(t)
	dur := s.app.Snapshot().Duration
	bar, ok := tt.Find("播放进度")
	if !ok {
		t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
	}
	// 先停住：歌曲一边放一边走的话，「拖到 2.0s」和「它自己走到了 2.0s 附近」
	// 分不开。
	s.app.Pause()
	waitFrames(t, tt, "停住了", func() bool { return !s.app.Snapshot().Playing })

	const pad = float32(8)
	x := func(sec float64) float32 {
		return bar.X + pad + (bar.W-2*pad)*float32(sec/dur.Seconds())
	}
	y := bar.Y + bar.H/2
	tt.Press(x(1.6), y)
	tt.Move(x(2.0), y)
	tt.Release(x(2.0), y)
	tt.Frame()

	// 松手之后位置在 2.0s 附近，而不是任何一个章节起点。
	got := s.app.Snapshot().Position
	if d := got - 2*time.Second; d > 350*time.Millisecond || d < -350*time.Millisecond {
		t.Errorf("拖动之后位置在 %v，want 2s 附近", got)
	}
	if near2(got, 1*time.Second) || near2(got, 2500*time.Millisecond) {
		t.Errorf("拖动被吸附到了章节起点（%v）——拖动不该吸附", got)
	}
}

// near2 是 |got-want| ≤ 350ms（和上面那条吸附测试同一个容差）。
func near2(got, want time.Duration) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= 350*time.Millisecond
}

// TestRenderChapterDotsShots 把带章节点的进度条存成 PNG，留给人眼对照（票 #585
// 的视觉审查）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderChapterDotsShots
//
// 播放条那一族走**手摆状态**（画法看的），迷你窗那一族走**整条链**（数据到点
// 也一起看）。位置摆在 45s：三个点（30/60/90s）一个在旋钮前、一个在旋钮后、
// 一个快到头——三种底色上的缺口一眼都看得到。
func TestRenderChapterDotsShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		st := barState()
		st.Position = 45 * time.Second
		st.Chapters = []app.Chapter{
			{At: 30 * time.Second, Title: "前奏"},
			{At: 60 * time.Second, Title: "正题"},
			{At: 90 * time.Second, Title: "尾声"},
		}
		_, tt := shotTester(t, st, dark)
		saveCrop(t, dir, "01-播放条-章节点-"+name+".png", tt, 0, coverWinH)

		// 迷你窗：走整条链（假 B 站回章节），整扇窗存下来。
		s, a := newMiniShell(t, func() {})
		f := newBiliFixture(t, 1)
		f.setViewPoints(chapterPoints())
		a.MountStreaming(app.Streaming{
			Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
		})
		if err := a.Enqueue(app.Item{
			Source: "online", Type: "mv", Bvid: "BVchapters", Cid: "1",
			Title: "带章节的那一首",
		}); err != nil {
			t.Fatalf("把带章节的那首放进队列：%v", err)
		}
		mt := miniTester(s)
		waitFrames(t, mt, "在线那首开始播", func() bool { return a.Snapshot().HasTrack })
		waitFrames(t, mt, "章节问回来了", func() bool { return len(a.Snapshot().Chapters) == 2 })
		saveCrop(t, dir, "02-迷你窗-章节点-"+name+".png", mt, 0, miniHeight)
	}
}
