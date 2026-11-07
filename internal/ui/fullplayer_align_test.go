package ui

// 全屏播放器对齐原型的几处（DESIGN §0′.T4）：窄一档的两栏缝、退场时长、当前句字色、
// 倍速键悬停字色。数值写死字面量（理由见 fullplayer_test.go 文件头）。

import (
	"image"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// brightest 是 r 那一块里最亮的那个通道值（量字色用：字的实心笔画能到满覆盖）。
func brightest(img *image.RGBA, r mygoui.Rect) uint8 {
	var m uint8
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := pixelAt(img, float32(x), float32(y))
			m = max(m, p.R, p.G, p.B)
		}
	}
	return m
}

// TestTheFullColumnsCloseUpBelow1180 锁住：窗口 ≤ 1180 时两栏缝 40、留白 40
// （原型 index.html:916）。1100 宽：栏宽 (1100 − 80 − 40) / 2 = 490，右栏从 40 + 490 + 40 = 570 起。
// 曾经出错的行为：缝恒为 64，右栏从 582 起。
func TestTheFullColumnsCloseUpBelow1180(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	tt.SetSize(1100, 800)
	openFullDirect(t, s, tt)
	r, ok := tt.Find("歌词提前 0.5 秒")
	if !ok {
		t.Fatalf("没有歌词旋钮：%q", tt.Texts())
	}
	if !within(r.X, 570, 1) {
		t.Errorf("右栏左沿 x=%.1f，want 570（两栏缝 40）", r.X)
	}
}

// TestTheFullPlayerExitTakes240ms 锁住：退场 240ms ease-in（原型 index.html:677 `full-out 240ms`），
// 测试时钟推过 240ms 树就拆了。曾经出错的行为：退场用 360ms，推过 240ms 还在退。
func TestTheFullPlayerExitTakes240ms(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFullDirect(t, s, tt)
	s.closeFullPlayer()
	tt.Frame()
	if !s.full.open || s.full.phase != fullOut {
		t.Fatalf("关闭后应先进退场：open=%v phase=%v", s.full.open, s.full.phase)
	}
	tt.Advance(240*time.Millisecond + time.Second/60)
	tt.Frame()
	if s.full.open || s.full.phase != fullGone {
		t.Errorf("推过 240ms 退场还没完：open=%v phase=%v", s.full.open, s.full.phase)
	}
}

// TestTheCurrentLyricIsPureWhite 锁住：当前句 #fff（原型 index.html:865 `.ly.cur { color: #fff }`），
// 其余句仍是 #ecebe8 的 38%。曾经出错的行为：所有句都是 #ecebe8，当前句最亮只到 236。
func TestTheCurrentLyricIsPureWhite(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFullDirect(t, s, tt)
	r, ok := tt.Find("第一句")
	if !ok {
		t.Fatalf("没有第一句：%q", tt.Texts())
	}
	// 当前句的「在场」动画（360ms）推完：推测试钟直到那一句亮到 #ecebe8 那一档以上
	// （改前的当前句色），条件等待、不数帧；推够 2 秒还没亮就按没亮判。
	got := brightest(tt.Image(), r)
	for range 120 {
		if got >= 0xEC {
			break
		}
		tt.Advance(time.Second / 60)
		tt.Frame()
		got = brightest(tt.Image(), r)
	}
	if got < 250 {
		t.Errorf("当前句最亮的像素 = %d，want 255（#fff）", got)
	}
}

// TestTheRateKeyBrightensOnHover 锁住：倍速键悬停时字色从 #d8d7d4 提到正文色 #ecebe8
// （原型 index.html:226 `.icon-btn:hover { color: var(--text) }`）。曾经出错的行为：只加了悬停底。
func TestTheRateKeyBrightensOnHover(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFullDirect(t, s, tt)
	b, ok := tt.Find("倍速")
	if !ok {
		t.Fatalf("没有倍速键：%q", tt.Texts())
	}
	idle := brightest(tt.Image(), b)
	tt.Move(b.X+b.W/2, b.Y+b.H/2)
	tt.Frame()
	settleFull(t, tt)
	hot := brightest(tt.Image(), b)
	if idle > 0xD8+2 {
		t.Errorf("平时倍速字最亮 = %d，want ≤ 0xd8", idle)
	}
	if hot < 0xEC-2 {
		t.Errorf("悬停时倍速字最亮 = %d，want 0xec（正文色）", hot)
	}
}
