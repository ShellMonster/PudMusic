// 搜索结果页的画面验收（T7）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSearchResultsShots
//
// 歌曲 / 视频 / UP 主三个 Tab、空状态，深浅两套各出一张，对照 `ref/*-search-*.png`。
// 数据全是合成的（不含任何真实专辑图）。
package ui

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// shotSearchRows 是出图用的合成结果。
func shotSearchRows(tab int) []list.Music {
	mk := func(id, title, owner string, mid int64, plays int64, dur time.Duration) list.Music {
		return list.Music{
			ID: id, Title: title, Owner: owner, View: plays, Duration: dur,
			Item: app.Item{Source: app.SourceOnline, Type: "mv", Bvid: id, Title: title, Artist: owner, ArtistMid: mid},
		}
	}
	if tab == searchTabUser {
		u := func(mid, name, sign string, fans int64, lv int) list.Music {
			return list.Music{
				ID: "u:" + mid, Title: name, Owner: sign, Fans: fans, Level: lv,
				Item: app.Item{Source: app.SourceOnline, Type: app.ItemTypeUser, Bvid: mid, Title: name},
			}
		}
		return packUserPairs([]list.Music{
			u("1", "陈奕迅 Eason", "歌手", 3_200_000, 6),
			u("2", "陈奕迅歌迷会", "分享陈奕迅的演唱会与专辑，每周更新现场与幕后", 128_000, 5),
			u("3", "夜航电台", "深夜里一个人听歌的地方。", 56_000, 4),
			u("4", "一只耳朵", "不定期搬运好听的现场。", 9_800, 4),
		})
	}
	return []list.Music{
		mk("BV1", "【陈奕迅】浮夸", "一只耳朵", 1, 790_000, 4*time.Minute+43*time.Second),
		mk("BV2", "【陈奕迅】富士山下", "夜航电台", 2, 920_000, 4*time.Minute+19*time.Second),
		mk("BV3", "【陈奕迅】爱情转移", "一只耳朵", 1, 1_060_000, 4*time.Minute+20*time.Second),
		mk("BV4", "十年（现场版）陈奕迅", "夜航电台", 2, 5_300_000, 3*time.Minute+51*time.Second),
	}
}

func TestRenderSearchResultsShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, c := range []struct {
			name string
			tab  int
			kw   string
			none bool
		}{
			{"search-song", searchTabSong, "陈奕迅", false},
			{"search-video", searchTabVideo, "陈奕迅", false},
			{"search-user", searchTabUser, "陈奕迅", false},
			{"search-empty", searchTabSong, "不存在的歌", true},
		} {
			t.Run(mode+"-"+c.name, func(t *testing.T) {
				s, tt := newShell(t, systemIsDark(dark))
				s.search.fetch = func(tab int, _ context.Context, page int) (list.Page, error) {
					if c.none || page > 1 {
						return list.Page{}, nil
					}
					rows := shotSearchRows(tab)
					tl := &s.search.lists[tab]
					tl.mu.Lock()
					tl.loaded = append(tl.loaded, rows...)
					tl.total = len(rows)
					tl.mu.Unlock()
					return list.Page{Songs: rows}, nil
				}
				s.search.tab = c.tab
				s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape(c.kw))
				for i := 0; i < 20; i++ {
					tt.Advance(50 * time.Millisecond)
					tt.Frame()
				}
				save(t, dir, mode+"-"+c.name+".png", tt)
			})
		}
	}
}
