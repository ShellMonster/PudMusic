// 满屏封面墙：布局纯函数 + 界面行为（点一格就放那一首、拖动挪墙、甩出去会停、静止不要帧）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderQueueWallShots
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestLayWallFitsShortQueuesOnScreen 锁定：一屏放得下的队列整面墙居中、拖不动。
func TestLayWallFitsShortQueuesOnScreen(t *testing.T) {
	for _, n := range []int{1, 5, 7, 10, 18} {
		l := layWall(n, 1200, 600)
		if l.bounds.W != 0 || l.bounds.H != 0 {
			t.Errorf("n=%d：放得下却能拖，bounds=%+v（%d×%d）", n, l.bounds, l.cols, l.rows)
		}
		if mid := l.bounds.X + l.w/2; !within(mid, 600, 0.5) {
			t.Errorf("n=%d：墙没有横向居中，中线 %.1f", n, mid)
		}
		if l.cols*l.rows < n || (l.rows-1)*l.cols >= n {
			t.Errorf("n=%d：%d 列 %d 行装不下或多一行", n, l.cols, l.rows)
		}
	}
	if l := layWall(5, 1200, 600); l.rows != 1 {
		t.Errorf("5 首放得下一行，却排成 %d 行", l.rows)
	}
}

// TestLayWallLongQueuesPanBothWays 锁定：长队列排成比视口更扁、两个方向都能走的墙，
// 停下时最外一格离视口边 40。
func TestLayWallLongQueuesPanBothWays(t *testing.T) {
	l := layWall(200, 1200, 600)
	if l.bounds.W <= 0 || l.bounds.H <= 0 {
		t.Fatalf("200 首应两个方向都能走，bounds=%+v", l.bounds)
	}
	if l.cols <= l.rows {
		t.Errorf("墙应比视口扁：%d 列 %d 行", l.cols, l.rows)
	}
	if !within(l.bounds.X+l.bounds.W, 40, 0.01) || !within(l.bounds.X, 1200-40-l.w, 0.01) {
		t.Errorf("横向停靠范围 %+v，want 左缘停在 40、右缘停在 1160", l.bounds)
	}
}

// TestWallCellsDoNotOverlapAndHitBack 锁定：每一格不重叠，点格子正中认回同一格，点缝认不出。
func TestWallCellsDoNotOverlapAndHitBack(t *testing.T) {
	l := layWall(47, 1000, 500)
	for i := range l.n {
		r := l.cell(i)
		if got := l.hit(r.X+r.W/2, r.Y+r.H/2); got != i {
			t.Fatalf("第 %d 格正中认成 %d", i, got)
		}
		if got := l.hit(r.X+r.W+wallGap/2, r.Y+r.H/2); got >= 0 && got == i {
			t.Fatalf("第 %d 格右边的缝认成了它自己", i)
		}
		for j := i + 1; j < l.n; j++ {
			o := l.cell(j)
			if r.X < o.X+o.W && o.X < r.X+r.W && r.Y < o.Y+o.H && o.Y < r.Y+r.H {
				t.Fatalf("第 %d、%d 格重叠：%+v %+v", i, j, r, o)
			}
		}
	}
	if got := l.hit(-1, 10); got != -1 {
		t.Errorf("墙外认成了 %d", got)
	}
}

// TestWallCentreAndVisibleRange 锁定：centre 把那一格放到视口正中（走得到时），
// visible 覆盖到所有落进视口的格子。
func TestWallCentreAndVisibleRange(t *testing.T) {
	l := layWall(200, 1200, 600)
	mid := 95
	at := l.centre(mid)
	r := l.cell(mid)
	if !within(r.X+at.X+r.W/2, 600, 0.5) || !within(r.Y+at.Y+r.H/2, 300, 0.5) {
		t.Errorf("第 %d 格没有到正中：%+v + %+v", mid, r, at)
	}
	if !l.shows(mid, at) {
		t.Errorf("摆到正中后 shows 说它不在视口里")
	}
	r0, r1, c0, c1 := l.visible(at)
	for i := range l.n {
		c := l.cell(i)
		x, y := c.X+at.X, c.Y+at.Y
		inView := x < l.viewW && x+c.W > 0 && y < l.viewH && y+c.H > 0
		row, col := i/l.cols, i%l.cols
		listed := row >= r0 && row <= r1 && col >= c0 && col <= c1
		if inView && !listed {
			t.Fatalf("第 %d 格在视口里却不在 visible 范围（行 %d-%d 列 %d-%d）", i, r0, r1, c0, c1)
		}
	}
	if built := (r1 - r0 + 1) * (c1 - c0 + 1); built >= l.n/2 {
		t.Errorf("visible 建了 %d 格，墙一共 %d 格——没有只建看得见的", built, l.n)
	}
}

// TestWallEdgeFadeIsSoft 锁定：离边超过柔边宽是 1，中心压在边上是 0，中间单调。
func TestWallEdgeFadeIsSoft(t *testing.T) {
	const tile = 100
	at := func(x float32) float32 { return wallEdgeFade(mygoui.Rect{X: x, Y: 200, W: tile, H: tile}, 1000, 600) }
	if got := at(300); got != 1 {
		t.Errorf("视口中间的格子 fade=%v，want 1", got)
	}
	if got := at(-tile / 2); got != 0 {
		t.Errorf("中心压在左边的格子 fade=%v，want 0", got)
	}
	prev := float32(-1)
	for x := float32(-tile / 2); x <= wallFade; x += 5 {
		if v := at(x); v < prev {
			t.Fatalf("x=%v 时 fade 变小了：%v < %v", x, v, prev)
		} else {
			prev = v
		}
	}
}

// TestWallScrollStopsAtTheEdges 锁定：触控板推墙跟手、到边为止。
func TestWallScrollStopsAtTheEdges(t *testing.T) {
	l := layWall(200, 1200, 600)
	var p wallPan
	p.follow(l, 0, "a")
	start := p.at()
	p.scroll(l, 30, 10)
	if got := p.at(); !within(got.X, start.X-30, 0.01) || !within(got.Y, start.Y-10, 0.01) {
		t.Errorf("推 (30,10) 后 at=%+v，want 从 %+v 跟着手指走", got, start)
	}
	p.scroll(l, 1e6, 1e6)
	if got := p.at(); got != (mygoui.Point{X: l.bounds.X, Y: l.bounds.Y}) {
		t.Errorf("推到头 at=%+v，want 停在 %+v", got, l.bounds)
	}
}

// ---- 界面 ----

// wallCoverPNG 画一张合成封面：色相按 i 走，左上一块亮斑。
func wallCoverPNG(t *testing.T, i int) []byte {
	t.Helper()
	const n = 120
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	hues := [][3]uint8{{196, 64, 52}, {52, 120, 196}, {60, 160, 110}, {210, 160, 50}, {130, 80, 180}, {40, 40, 48}, {200, 110, 140}}
	h := hues[i%len(hues)]
	for y := range n {
		for x := range n {
			k := 1 - float64(x+y)/float64(2*n)*0.6
			c := color.RGBA{uint8(float64(h[0]) * k), uint8(float64(h[1]) * k), uint8(float64(h[2]) * k), 255}
			if (x-30)*(x-30)+(y-34)*(y-34) < 200+i%5*60 {
				c = color.RGBA{240, 236, 228, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// wallShell 造一个开着满屏封面墙的外壳：n 首各不相同的本地歌（同一份 FLAC 的软链接），
// 暂停在第 0 首。coverURL 非空时每首挂一张封面。
func wallShell(t *testing.T, n int, dark bool, w, h int, coverURL string) *testShell {
	t.Helper()
	b := newTestShell(t, at(w, h), systemIsDark(dark), withClock(newFakeClock(time.Unix(1_700_000_000, 0))))
	src, err := filepath.Abs(flacPath(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for i := range n {
		path := filepath.Join(dir, "song"+strconv.Itoa(i)+".flac")
		if err := os.Symlink(src, path); err != nil {
			t.Fatal(err)
		}
		it := app.LocalItem(path)
		it.Title = "第 " + strconv.Itoa(i+1) + " 首"
		it.Artist = "某位歌手"
		if coverURL != "" {
			it.Cover = coverURL + "/c" + strconv.Itoa(i) + ".png"
		}
		if err := b.a.Enqueue(it); err != nil {
			t.Fatalf("入队：%v", err)
		}
	}
	waitFrames(t, b.tt, "歌开起来了", func() bool { return b.a.Snapshot().HasTrack })
	b.a.Pause()
	b.s.openFullPlayer()
	waitFrames(t, b.tt, "满屏开出来了", func() bool { return b.s.full.open })
	settleFull(t, b.tt)
	if err := b.tt.Click("以封面墙查看播放队列"); err != nil {
		t.Fatalf("点封面墙键：%v（文字 %q）", err, b.tt.Texts())
	}
	b.tt.Frame()
	settleFull(t, b.tt)
	if _, ok := b.tt.Find("播放队列封面墙"); !ok {
		t.Fatalf("封面墙没开出来，文字 %q", b.tt.Texts())
	}
	return b
}

// TestClickingAWallCellPlaysThatTrack 锁定：点墙上一格，就从那一首放起；当前那一格居中。
func TestClickingAWallCellPlaysThatTrack(t *testing.T) {
	b := wallShell(t, 12, true, fullWinW, fullWinH, "")
	defer b.Close()
	r, ok := b.tt.Find("第 9 首")
	if !ok {
		t.Fatalf("墙上没有第 9 首：%q", b.tt.Texts())
	}
	b.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	waitFrames(t, b.tt, "第 9 首放起来", func() bool {
		it, ok := b.a.CurrentItem()
		return ok && it.Title == "第 9 首"
	})
	if q := b.a.Queue().CurrentIndex(); q != 8 {
		t.Errorf("队列当前项 = %d，want 8", q)
	}
}

// TestTheWallCentresTheCurrentTrackOnOpen 锁定：长队列打开时正在放的那一首在墙的视口正中。
func TestTheWallCentresTheCurrentTrackOnOpen(t *testing.T) {
	b := wallShell(t, 80, true, fullWinW, fullWinH, "")
	defer b.Close()
	if err := b.a.PlayAt(41); err != nil {
		t.Fatal(err)
	}
	b.a.Pause()
	b.s.full.wall.toggle()
	b.s.full.wall.toggle()
	b.tt.Frame()
	settleFull(t, b.tt)
	r, ok := b.tt.Find("第 42 首")
	if !ok {
		t.Fatalf("第 42 首不在视口里：%q", b.tt.Texts())
	}
	area, _ := b.tt.Find("播放队列封面墙")
	if !within(r.X+r.W/2, area.X+area.W/2, 1) || !within(r.Y+r.H/2, area.Y+area.H/2, 1) {
		t.Errorf("第 42 首中心 (%.1f,%.1f)，视口中心 (%.1f,%.1f)",
			r.X+r.W/2, r.Y+r.H/2, area.X+area.W/2, area.Y+area.H/2)
	}
}

// TestDraggingAndFlingingTheWallMovesItAndSettles 锁定：拖墙，格子跟着走；甩出去之后会停下、
// 停下后不再要帧；停下来的墙仍在可停范围内。
func TestDraggingAndFlingingTheWallMovesItAndSettles(t *testing.T) {
	b := wallShell(t, 120, true, fullWinW, fullWinH, "")
	defer b.Close()
	tt := b.tt
	before, ok := tt.Find("第 1 首")
	if !ok {
		t.Fatalf("没有第 1 首：%q", tt.Texts())
	}
	area, _ := tt.Find("播放队列封面墙")
	x, y := area.X+area.W/2, area.Y+area.H/2
	tt.Press(x, y)
	for i := 1; i <= 10; i++ {
		tt.Advance(16 * time.Millisecond)
		tt.Move(x-float32(i)*12, y-float32(i)*4)
	}
	tt.Frame()
	mid := b.s.full.wall.pan.at()
	tt.Release(x-120, y-40)
	tt.Frame()
	if after, ok := tt.Find("第 1 首"); ok && within(after.X, before.X, 1) {
		t.Errorf("拖了 120 像素，第 1 首没动：%+v → %+v", before, after)
	}
	for range 200 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		if !tt.FramePending() {
			break
		}
	}
	if tt.FramePending() {
		t.Fatalf("甩出去 3.2 秒还在要帧")
	}
	l := layWall(b.a.Queue().Len(), area.W, area.H)
	got := b.s.full.wall.pan.at()
	if got != l.clamp(got) {
		t.Errorf("停下的位置 %+v 越过了可停范围 %+v", got, l.bounds)
	}
	if got.X >= mid.X {
		t.Errorf("往左甩，停下时 x=%.1f 没有比松手时 %.1f 更靠左", got.X, mid.X)
	}
}

// TestTrackpadPansTheWall 锁定：触控板两指推墙，墙立刻跟着走，不要额外的滑行帧。
func TestTrackpadPansTheWall(t *testing.T) {
	b := wallShell(t, 120, true, fullWinW, fullWinH, "")
	defer b.Close()
	tt := b.tt
	area, _ := tt.Find("播放队列封面墙")
	before := b.s.full.wall.pan.at()
	tt.Scroll(area.X+area.W/2, area.Y+area.H/2, 50, 30)
	tt.Frame()
	got := b.s.full.wall.pan.at()
	if !within(got.X, before.X-50, 0.5) || !within(got.Y, before.Y-30, 0.5) {
		t.Errorf("推 (50,30) 后 at %+v → %+v", before, got)
	}
	// 指针下面换了一格，悬停放大那一下要走完（160ms）：推钟到它停。
	for n := 0; n < 30 && tt.FramePending(); n++ {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}
	if tt.FramePending() {
		t.Errorf("推完之后还在要帧")
	}
	if after := b.s.full.wall.pan.at(); after != got {
		t.Errorf("推完之后墙自己又走了：%+v → %+v", got, after)
	}
}

// TestTheIdleWallRequestsNoFrames 锁定：墙静止、歌暂停时不要帧。
func TestTheIdleWallRequestsNoFrames(t *testing.T) {
	b := wallShell(t, 60, true, fullWinW, fullWinH, "")
	defer b.Close()
	b.tt.Frame()
	if b.tt.FramePending() {
		t.Fatalf("静止的封面墙还在要帧")
	}
}

// TestPressingAGlidingWallCatchesItWithoutPlaying 锁定：墙甩出去还在滑时按一下是「接住」，
// 不是点那一首。
func TestPressingAGlidingWallCatchesItWithoutPlaying(t *testing.T) {
	b := wallShell(t, 120, true, fullWinW, fullWinH, "")
	defer b.Close()
	tt := b.tt
	area, _ := tt.Find("播放队列封面墙")
	x, y := area.X+area.W/2, area.Y+area.H/2
	tt.Press(x, y)
	for i := 1; i <= 6; i++ {
		tt.Advance(8 * time.Millisecond)
		tt.Move(x-float32(i)*30, y)
	}
	tt.Release(x-180, y)
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
	if !b.s.full.wall.pan.moving {
		t.Fatalf("甩出去之后墙没有在滑")
	}
	before := b.a.Queue().CurrentIndex()
	tt.ClickAt(x, y)
	tt.Frame()
	tt.Frame()
	if got := b.a.Queue().CurrentIndex(); got != before {
		t.Errorf("接住滑行的那一按放了第 %d 首（原来 %d）", got, before)
	}
}

// TestRenderQueueWallShots 出封面墙的图：短队列 / 长队列 × 深 / 浅 × 宽 / 窄。
func TestRenderQueueWallShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/c"), ".png")
		i, _ := strconv.Atoi(name)
		w.Write(wallCoverPNG(t, i))
	}))
	defer srv.Close()
	for _, tc := range []struct {
		name string
		n    int
		w, h int
	}{{"small", 6, fullWinW, fullWinH}, {"large", 90, fullWinW, fullWinH}, {"narrow", 40, 760, 640}} {
		for _, dark := range []bool{true, false} {
			theme := "light"
			if dark {
				theme = "dark"
			}
			t.Run(tc.name+"-"+theme, func(t *testing.T) {
				b := wallShell(t, tc.n, dark, tc.w, tc.h, srv.URL)
				defer b.Close()
				waitFrames(t, b.tt, "封面下回来了", func() bool {
					return b.s.images.Get(srv.URL + "/c0.png").Ready()
				})
				b.tt.Move(4, float32(tc.h)/2) // 指针挪开，顶栏那枚键的提示收起来
				for range 20 {
					b.tt.Advance(50 * time.Millisecond)
					b.tt.Frame()
					time.Sleep(5 * time.Millisecond)
				}
				settleFull(t, b.tt)
				save(t, dir, "wall-"+tc.name+"-"+theme+".png", b.tt)
			})
		}
	}
}
