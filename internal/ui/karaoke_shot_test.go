// 逐字歌词的出图材料（深色满屏）：唱到「路灯」两个字时，前两个字亮、后面暗。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderKaraokeShots
package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
)

// karaokeShotYRC 把 fullShotLRC 的第一句换成逐字版本：每字 375ms，
// 4.0 秒时正好唱完「路」「灯」。
const karaokeShotYRC = "[3000,3000](3000,375,0)路(3375,375,0)灯(3750,375,0)把" +
	"(4125,375,0)影(4500,375,0)子(4875,375,0)拉得很长"

func TestRenderKaraokeShots(t *testing.T) {
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
			// 先出同一位置的普通 LRC 那一张：逐字改动只该改颜色，不该改
			// 字号、行高和位置。两张对着看，布局差一个像素都看得出来。
			b.a.SeekTo(4 * time.Second)
			settleFull(t, b.tt)
			save(t, dir, name+"-karaoke-plain.png", b.tt)

			b.a.PutLyricsDoc(lyrics.Doc{
				LRC:        fullShotLRC,
				Translated: fullShotTrans,
				YRC:        karaokeShotYRC,
			})
			waitFrames(t, b.tt, "逐字歌词进了画面", func() bool {
				rows := b.a.Lyrics().Rows
				return len(rows) > 0 && len(rows[0].Words) > 0
			})
			settleFull(t, b.tt)
			save(t, dir, name+"-karaoke.png", b.tt)
		})
	}
}
