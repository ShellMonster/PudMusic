// 「本周单曲循环」点一首歌：把统计里的聚合键变回一项。
//
// 统计按 `Item.Identity()` 那份口径聚合（`mv:<bvid>|<cid>` / `audio:<sid>` /
// `local:<路径>`），点播放时得把它**拆回**一项。拆不回来（数据被改过、或者
// 是更早的格式）那一档不许放进队列——按下去要有一句「放不了」，而不是
// 塞一条残缺的项进去白跑一趟取流。
package app_test

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestItemOfStatsKeyRestoresAnMVEntry 钉住 mv 那一档：`mv:<bvid>|<cid>`
// 拆回「在线 + mv + bvid + cid」四样，快照里的歌名 / UP 名照填。
func TestItemOfStatsKeyRestoresAnMVEntry(t *testing.T) {
	it, err := app.ItemOfStatsKey("mv:BV1abc|c123", &app.StatsTrackStat{
		Title: "夜曲", Artist: "林俊杰", ArtistMid: 1001,
	})
	if err != nil {
		t.Fatalf("拆 mv 键失败：%v", err)
	}
	if it.Source != app.SourceOnline || it.Type != "mv" {
		t.Errorf("来源 / 类型是 %q / %q，want online / mv", it.Source, it.Type)
	}
	if it.Bvid != "BV1abc" || it.Cid != "c123" {
		t.Errorf("身份是 bvid=%q cid=%q，want BV1abc / c123", it.Bvid, it.Cid)
	}
	if it.Title != "夜曲" || it.Artist != "林俊杰" || it.ArtistMid != 1001 {
		t.Errorf("快照字段是 %+v，want 夜曲 / 林俊杰 / 1001", it)
	}
	if got := it.Identity(); got != "mv:BV1abc|c123" {
		t.Errorf("还原出来的身份是 %q，want 原来的键——播的是另一首", got)
	}
}

// TestItemOfStatsKeyRestoresAnMVEntryWithoutACid 钉住「还不知道是哪一集」的旧键
// （`mv:<bvid>`，票 #683 之前就是这个形状）：cid 留空，起播时补。
func TestItemOfStatsKeyRestoresAnMVEntryWithoutACid(t *testing.T) {
	it, err := app.ItemOfStatsKey("mv:BV1abc", &app.StatsTrackStat{Title: "夜曲"})
	if err != nil {
		t.Fatalf("拆 mv 键（无 cid）失败：%v", err)
	}
	if it.Bvid != "BV1abc" || it.Cid != "" {
		t.Errorf("身份是 bvid=%q cid=%q，want BV1abc / 空", it.Bvid, it.Cid)
	}
}

// TestItemOfStatsKeyRestoresAnAudioEntry 钉住音乐库那一档。
func TestItemOfStatsKeyRestoresAnAudioEntry(t *testing.T) {
	it, err := app.ItemOfStatsKey("audio:1293494", &app.StatsTrackStat{Title: "起风了"})
	if err != nil {
		t.Fatalf("拆 audio 键失败：%v", err)
	}
	if it.Source != app.SourceOnline || it.Type != "audio" || it.Sid != "1293494" {
		t.Errorf("还原出来的是 %+v，want online / audio / sid=1293494", it)
	}
}

// TestItemOfStatsKeyRestoresALocalEntry 钉住本地文件那一档：走 `LocalItem`
// 那条路（`IsLocal` 判据是两个字段，少一个就掉到「不是投稿」那一支）。
func TestItemOfStatsKeyRestoresALocalEntry(t *testing.T) {
	it, err := app.ItemOfStatsKey("local:/music/song.mp3", &app.StatsTrackStat{
		Title: "本地的一首", Local: true,
	})
	if err != nil {
		t.Fatalf("拆 local 键失败：%v", err)
	}
	if !it.IsLocal() || it.Path != "/music/song.mp3" {
		t.Errorf("还原出来的是 %+v，want 本地文件 /music/song.mp3", it)
	}
	if it.Title != "本地的一首" {
		t.Errorf("歌名是 %q，want 快照里的「本地的一首」（文件名只是兜底）", it.Title)
	}
}

// TestItemOfStatsKeyRefusesAKeyItCannotRead 钉住「认不出来的键放不了」：
// 空串、不认识的前缀、`mv:` 后面是空的——这三种进队列就是一条永远放不了
// 的死项，不如当场说清。
func TestItemOfStatsKeyRefusesAKeyItCannotRead(t *testing.T) {
	for _, key := range []string{"", "bogus", "mv:", "mv:|c1", "audio:", "local:"} {
		if _, err := app.ItemOfStatsKey(key, &app.StatsTrackStat{Title: "x"}); err == nil {
			t.Errorf("键 %q 居然拆出了项——它进队列就是一条死项", key)
		}
	}
}
