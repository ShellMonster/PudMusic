package ui

import (
	"bytes"
	"cmp"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
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

// rowHasRing 报那一行的上沿有没有焦点环。
//
// 判据是**那个像素和这一行的底色不是一回事**：焦点环是主题里那个 Focus 色（半透明
// 的主色）压在行沿上的，而没有环的那一行上沿就是行自己的底色。
//
// ⚠️ **不看「离主色多近」**：焦点环是半透明的，它离底色比离主色近；而主色换一个
// 色相（绿的、黄的）那一套判据就飘了。这里量的是「和底色差了多少」。
//
// ⚠️ **行的上沿要**取整**（`round32`），不是截断**（`int`）：行的 Y 可能是小数
// （列表容器在页面里的位置是算出来的，票 #290 给稍后再看页头加了一枚 32 高的
// 按钮之后，那一页的行就落在 286.8 这样的位置上）。截断取到的是**行上面**那一
// 像素（上一行的底色），于是「没有环」的那一行也会被当成有环——判据就废了。
// 取整取到的是行自己的上沿：没有环时那两处是同一个底色，有环时上沿那一处是
// 环的那条线。
func rowHasRing(t *testing.T, tt *ui.Tester, title string) bool {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	x := int(r.X + r.W/2)
	y := round32(r.Y)
	if x < 0 || x >= 1200 || y < 0 || y+6 >= 800 {
		t.Fatalf("「%s」那一行在画面外", title)
	}
	// 这一行自己的底色：往下挪 6 像素，避开环，也还在行的内边距里（8）。
	bg := tt.Image().RGBAAt(x, y+6)
	return tt.Image().RGBAAt(x, y) != bg
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

// localSongs 造 n 首**本地**的歌：歌名是「本地 1」到「本地 n」，文件是同一个
// FLAC 素材的 n 份拷贝（路径不同——路径就是本地歌的身份，两首一样的路径在队列
// 里是同一首）。
func localSongs(t *testing.T, n int) []list.Music {
	t.Helper()
	b, err := os.ReadFile(flac(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := make([]list.Music, n)
	for i := range out {
		name := filepath.Join(dir, string(rune('a'+i))+".flac")
		if err := os.WriteFile(name, b, 0o644); err != nil {
			t.Fatal(err)
		}
		out[i] = list.Music{
			ID:    "local-" + string(rune('a'+i)),
			Title: "本地 " + string(rune('1'+i)),
			Owner: "本地的 UP 主",
			Item:  app.LocalItem(name),
		}
	}
	return out
}
