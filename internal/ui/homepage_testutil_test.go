package ui

import (
	"bytes"
	"cmp"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// 首页测试和别的页面测试共用的小工具。旧首页（六个翻页 Tab）删掉之后，这些和首页的
// 具体摆法无关的辅助留了下来：很多别的测试拿它们当「放一屏歌、数帧、找字」的台子。

// homeW / homeH 是无头窗口的大小，和应用默认窗口一致。
const (
	homeW = 1200
	homeH = 800
)

// songName 是第 i 首（1 起）的歌名。
func songName(prefix string, i int) string { return fmt.Sprintf("%s %d", prefix, i) }

// homeSongsOf 造 n 行歌：歌名是「<prefix> 1」到「<prefix> n」，ID 带着 prefix，不串、不撞。
func homeSongsOf(prefix string, n int) []list.Music {
	out := make([]list.Music, n)
	for i := range out {
		out[i] = list.Music{
			ID:    prefix + "-" + strconv.Itoa(i),
			Title: songName(prefix, i+1),
			Owner: prefix + " 的 UP 主",
			Item:  app.Item{Title: songName(prefix, i+1)},
		}
	}
	return out
}

// homeTestCache 造一个封面缓存。AddWake 不能省：图下完之后不唤醒，界面会永远停在占位。
func homeTestCache(t *testing.T) *imgcache.Cache {
	t.Helper()
	c := imgcache.New(imgcache.Options{})
	c.AddWake(func() {})
	t.Cleanup(c.Close)
	return c
}

// homeFrame 连跑几帧：一帧不够，列表要重新排版，行高是量出来的。
func homeFrame(tt *ui.Tester) {
	for range 3 {
		tt.Frame()
	}
}

// settleHome 推着钟走一段让过渡走完（Tester 的钟站着不动，AGENTS.md §2）。
func settleHome(tt *ui.Tester) {
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
}

// shows 报 s 此刻在不在画面上。不能用 `tt.HasText`：它是子串匹配，`tt.Find` 是全等匹配。
func shows(tt *ui.Tester, s string) bool {
	r, ok := tt.Find(s)
	return ok && r.W > 0 && r.H > 0
}

// rowPos 是画面上画着的某一行：它是哪一首、上面离窗口顶多远。
type rowPos struct {
	title string
	y     float32
}

// listPos 是 prefix 那一档此刻画着的那些行，从上往下排。盒子是空的（宽 0）的要跳过：
// 虚拟列表在视口外多建的几行有 y 但没有面积。
func listPos(tt *ui.Tester, prefix string) []rowPos {
	var out []rowPos
	for i := 1; i <= 200; i++ {
		r, ok := tt.Find(songName(prefix, i))
		if !ok || r.W == 0 || r.H == 0 {
			continue
		}
		out = append(out, rowPos{songName(prefix, i), r.Y})
	}
	slices.SortFunc(out, func(a, b rowPos) int {
		if a.y != b.y {
			return cmp.Compare(a.y, b.y)
		}
		return cmp.Compare(a.title, b.title)
	})
	return out
}

// titles 是给报错用的一行摘要：这一屏里画着的头几首和最后一首。
func titles(pos []rowPos) string {
	if len(pos) == 0 {
		return "（一首歌都没画出来）"
	}
	head := make([]string, 0, 4)
	for i, p := range pos {
		if i == 3 {
			head = append(head, "…")
			break
		}
		head = append(head, p.title)
	}
	return fmt.Sprintf("%v 一直到 %s", head, pos[len(pos)-1].title)
}

// coverJPEG 编一张 w×h 的 JPEG，当封面用（B 站封面就是 1920×1080）。
func coverJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x / 8), G: uint8(y / 8), B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 55}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
