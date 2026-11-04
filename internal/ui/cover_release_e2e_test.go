package ui

// 票 #673 的界面那一半：**滚过一百多首之后，最早那几张封面的像素被放掉，
// 滚回去还能正确画出来**。
//
// # 为什么这一层也要一条
//
// 缓存自己那一层（`widgets/imgcache/release_test.go`）已经钉住了「连续 N 帧没
// 被要就放像素、再要时重新解码」。这一条验的是**真的在滚的界面**：
//
//   - `BeginFrame` 是 `Shell.viewMain` 每帧调的（没有它，释放这一档压根不跑）；
//   - 每一格封面每一帧都走 `imgcache.View` → `GetSized`（画面上那几张因此
//     永远 `onScreen`，不会被放掉）；
//   - 滚过去之后那些封面**不再被要**，于是它们才轮到释放。
//
// 少任何一环，缓存那一层的测试都是绿的，而真机上一张都放不掉。
//
// # 判据
//
//	滚过 200 首之后   内存账远低于 200 张全留着（释放真的发生了）
//	滚回顶上          那一格画出来的是**封面**（不是骨架屏），而且内存账又涨回去
//	                   （重新解码真的发生了）
//
// ⚠️ **不用「刚 Get 一定 Loading」当判据**：重新解码跑在后台 worker 上，一次
// `Get` 之内可能已经解完——那样的断言会在快机器上偶发（缓存那一层用的是
// `entry.released` 这个确定性的标记，这里没法用：`entry` 不在这个包里）。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// flatPNG 编一张纯色 PNG。纯色是为了「滚回来那一格画的是不是这张图」能用**一个
// 像素**判：首页那一屏的布局量出来之前，渐变封面取样点容易落在字上。
func flatPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
