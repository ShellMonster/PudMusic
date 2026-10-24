// 歌词那一句「这份词是怎么来的」的验收。
//
// # 为什么要这一句
//
// 歌词有两处来路：B 站自己带出来的（取流那一趟顺带回来的）和用户在「找歌词」
// 里搜来的。用户分不清这两者时，看到一份词会以为「这是应用找来的」——而他下一
// 次换一首歌时就会去点「找歌词」，尽管这一首**根本不用点**。

package ui

import (
	"math"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
)

// TestTheLyricsSayWhereTheyCameFrom 盯那一句说明。
//
// ⚠️ **两处都要有**：迷你播放器那一页和满屏播放器那一屏（它们共用
// `viewLyricLines`，而那一处是**唯一**画这句话的地方）。
func TestTheLyricsSayWhereTheyCameFrom(t *testing.T) {
	for _, c := range []struct {
		what   string
		origin string
		want   string
	}{
		{"B 站自动带出来的", lyrics.OriginBili, "歌词来自 B 站"},
		{"用户搜来的", lyrics.OriginSearch, "歌词来自搜索"},
	} {
		t.Run(c.what, func(t *testing.T) {
			s, tt := lyricShell(t)
			s.app.PutLyricsFrom(c.origin, lyricsOfN(20), "")
			settleLyrics(t, s, tt)

			if !tt.HasText(c.want) {
				t.Fatalf("歌词区上没有 %q，文字有 %q", c.want, tt.Texts())
			}
		})
	}
}

// TestTheFullPlayerAlsoSaysWhereTheyCameFrom 满屏那一屏也有那一句。
func TestTheFullPlayerAlsoSaysWhereTheyCameFrom(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyricsFrom(lyrics.OriginBili, fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	if !tt.HasText("歌词来自 B 站") {
		t.Fatalf("满屏上没有 %q，文字有 %q", "歌词来自 B 站", tt.Texts())
	}
}

// TestTheOriginLineSitsWhereTheLyricsSit 盯说明句与歌词**对齐**。
//
// ⚠️ **歌词的行有两层横向留白**（内容列自己的 + 每一行自己的），所以**字**是从
// 两倍 `padX` 之后开始的。说明句只加一层的话，它会比歌词的字偏左一个 `padX`
// （迷你播放器那页 12px、满屏那一屏 8px）——而这一处的注释说的是「跟着一边儿」。
func TestTheOriginLineSitsWhereTheLyricsSit(t *testing.T) {
	s, tt := lyricShell(t)
	s.app.PutLyricsFrom(lyrics.OriginBili, lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	origin, ok := tt.Find("歌词来自 B 站")
	if !ok {
		t.Fatalf("歌词区上没有那句话，文字有 %q", tt.Texts())
	}
	line, ok := tt.Find("第0句")
	if !ok {
		t.Fatalf("歌词区上没有词，文字有 %q", tt.Texts())
	}
	if d := math.Abs(float64(origin.X - line.X)); d > 0.5 {
		t.Errorf("说明句在 x=%.1f，歌词在 x=%.1f，差 %.1f —— 它们没有对齐",
			origin.X, line.X, d)
	}
}

// TestAnUnknownOriginSaysNothing 不知道来路时不画那一句。
//
// ⚠️ **「不知道」不该被说成「搜索来的」**——那句话是错的。老缓存里没有这一栏
// （`lyrics.Entry.Origin`），那些歌就干脆不画这一句。
func TestAnUnknownOriginSaysNothing(t *testing.T) {
	s, tt := lyricShell(t)
	// ⚠️ **空串 = 不知道**：这一份是直接塞进状态里的，没有来路。
	s.app.PutLyricsFrom("", lyricsOfN(20), "")
	settleLyrics(t, s, tt)

	if tt.HasText("歌词来自 B 站") || tt.HasText("歌词来自搜索") {
		t.Fatalf("不知道来路却画了那一句，文字有 %q", tt.Texts())
	}
}
