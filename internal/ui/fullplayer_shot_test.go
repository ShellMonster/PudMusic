// 全屏播放器、弹层和抽屉的出图（原型 `ref/{dark,light}-full.png` 等的对照材料）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullPlayerShots
//
// 深浅两套都出：全屏里面是固定暗色，但抽屉（队列 / 评论）、封面取色和背景压在**当前主题**上，
// 浅色主题下的差别只有出了图才看得见。封面一律是合成的（原型里的真实专辑图不许进仓库）。
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
)

// syntheticCoverPNG 画一张合成封面：深红底、一团亮红（像人像的脸）、几道斜线。
func syntheticCoverPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)*0.5, float64(h)*0.55
	for y := range h {
		for x := range w {
			dx, dy := (float64(x)-cx)/float64(w), (float64(y)-cy)/float64(h)
			d := dx*dx*2.2 + dy*dy*1.4
			r := 70 + 170*max(0, 1-d*4)
			g := 14 + 36*max(0, 1-d*4)
			b := 16 + 30*max(0, 1-d*4)
			if (x+y)%97 < 3 && x > w/2 {
				r, g, b = 230, 90, 80
			}
			img.Set(x, y, color.RGBA{R: uint8(min(r, 255)), G: uint8(g), B: uint8(b), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fullShotLRC / fullShotTrans 是出图用的歌词和译文（合成，十行）。
const fullShotLRC = "[00:00.00](前奏)\n[00:03.00]路灯把影子拉得很长\n[00:06.00]我把没说完的话留在站台\n" +
	"[00:09.00]晚风经过旧的唱片行\n[00:12.00]有一首歌刚好在放\n[00:15.00]你说时间会把一切调慢\n" +
	"[00:18.00]我只记得那天的光\n[00:21.00]夜航电台还没有收班\n[00:24.00]我们在雨里数着站牌\n[00:27.00]有人把音量调得很小\n"
const fullShotTrans = "[00:03.00]The streetlights stretch the shadows long\n[00:06.00]I left my unfinished words at the platform\n" +
	"[00:09.00]The night wind passes the old record shop\n[00:12.00]A song happens to be playing\n"

// fullShotShell 造一个开着满屏的外壳：合成封面、暂停在 6 秒、挂着歌词。
func fullShotShell(t *testing.T, dark bool, srvURL string) *testShell {
	t.Helper()
	b := newTestShell(t, at(fullWinW, fullWinH), systemIsDark(dark),
		withClock(newFakeClock(time.Unix(1_700_000_000, 0))))
	item := app.LocalItem(flacPath(t))
	item.Cover = srvURL + "/cover.png"
	item.Title = "渺小"
	item.Artist = "田馥甄"
	if err := b.a.Enqueue(item); err != nil {
		t.Fatalf("把歌放进队列：%v", err)
	}
	waitFrames(t, b.tt, "歌开起来了", func() bool { return b.a.Snapshot().HasTrack })
	waitFrames(t, b.tt, "封面下回来了", func() bool { return b.s.images.Get(item.Cover).Ready() })
	b.a.Pause()
	b.a.SeekTo(6 * time.Second)
	b.a.PutLyrics(fullShotLRC, fullShotTrans)
	b.s.openFullPlayer()
	waitFrames(t, b.tt, "满屏开出来了", func() bool { return b.s.full.open })
	settleFull(t, b.tt)
	return b
}

func TestRenderFullPlayerShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := syntheticCoverPNG(t, 800, 800)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(body) }))
	defer srv.Close()

	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			b := fullShotShell(t, dark, srv.URL)
			defer b.Close()
			save(t, dir, name+"-full.png", b.tt)

			pops := []struct {
				file string
				open func()
			}{
				{"pop-quality", func() { b.s.full.popQuality = true }},
				{"pop-rate", func() { b.s.full.popRate = true }},
				{"pop-sleep", func() { b.s.full.popSleep = true }},
				{"pop-eq", func() { b.s.full.popEQ = true }},
				{"pop-fprefs", func() { b.s.full.popPrefs = true }},
			}
			for _, p := range pops {
				b.s.closeFullPops()
				p.open()
				b.tt.Frame()
				settleFull(t, b.tt)
				save(t, dir, name+"-full-"+p.file+".png", b.tt)
			}
			b.s.closeFullPops()
			b.tt.Frame()

			b.s.drawerOpen = true
			b.tt.Frame()
			settleFull(t, b.tt)
			save(t, dir, name+"-full-queue.png", b.tt)
			b.s.drawerOpen = false
			b.tt.Frame()
			settleFull(t, b.tt)
		})
	}
}

// drawerShotShell 造一个主窗口（首页）+ 九首歌的队列，正在放第 0 首，暂停。
func drawerShotShell(t *testing.T, dark bool, srvURL string) *testShell {
	t.Helper()
	b := newTestShell(t, at(fullWinW, fullWinH), systemIsDark(dark),
		withClock(newFakeClock(time.Unix(1_700_000_000, 0))))
	titles := []string{"渺小", "你就不要想起我", "说到爱", "言不由衷", "小情歌", "寓言", "富士山下", "爱人错过", "浴室"}
	artists := []string{"田馥甄", "田馥甄", "蔡健雅", "徐佳莹", "苏打绿", "王菲", "陈奕迅", "告五人", "deca joins"}
	for i, ti := range titles {
		it := app.LocalItem(flacPath(t))
		it.Title, it.Artist = ti, artists[i]
		it.Cover = srvURL + "/cover" + strconv.Itoa(i%3) + ".png"
		if err := b.a.Enqueue(it); err != nil {
			t.Fatal(err)
		}
	}
	waitFrames(t, b.tt, "歌开起来了", func() bool { return b.a.Snapshot().HasTrack })
	b.a.Pause()
	b.tt.Frame()
	return b
}

func TestRenderDrawerShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := syntheticCoverPNG(t, 200, 200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(body) }))
	defer srv.Close()
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			b := drawerShotShell(t, dark, srv.URL)
			b.s.drawerOpen = true
			b.tt.Frame()
			settleFull(t, b.tt)
			save(t, dir, name+"-queue.png", b.tt)
		})
	}
}

func TestRenderCommentsDrawerShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			b := newCommentBili(t, "reply-main.json")
			_, tt := newCommentsShell(t, b, mvItem(), fullWinW, fullWinH, systemIsDark(dark))
			commentsWait(t, tt, "表情图下来了", func() bool { return magentaPixels(tt) > 0 })
			save(t, dir, name+"-comments.png", tt)
			if err := tt.Click("共52条回复"); err == nil {
				tt.Frame()
				settleFull(t, tt)
				save(t, dir, name+"-comments-replies.png", tt)
			}
		})
	}
}

func TestRenderLyricSearchShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			netease := neteaseSource(cand("1", "渺小"), cand("2", "渺小（Live）"), cand("3", "渺小 - 钢琴版"))
			netease.previewDoc = lyrics.Doc{LRC: fullShotLRC}
			s, tt := lyricSearchShell(t, netease, lrclibSource())
			tt.SetDark(dark)
			openTheSearchDialog(t, tt)
			waitFrames(t, tt, "候选回来了", func() bool { return tt.HasText("渺小（Live）") })
			save(t, dir, name+"-lyricsearch.png", tt)
			if err := tt.Click("渺小（Live）"); err != nil {
				t.Fatal(err)
			}
			waitFrames(t, tt, "预览回来了", func() bool { return s.app.LyricPreview().Adoptable })
			settleFull(t, tt)
			save(t, dir, name+"-lyricsearch-picked.png", tt)
		})
	}
}
