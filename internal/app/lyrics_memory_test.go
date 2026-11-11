package app

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
)

func memEntry(text string) lyricMemoryEntry {
	return lyricMemoryEntry{
		entry: lyrics.Entry{Lyrics: "[00:01.00]" + text},
		rows:  []lyrics.Row{{At: time.Second, Text: text}},
		title: text,
	}
}

// TestLyricMemoryKeepsTheMostRecent 钉住 LRU 的两个行为：最近用过的留下，
// 最老的那一份让位。没有上限的话，听一整天就攒成第二歌词库。
func TestLyricMemoryKeepsTheMostRecent(t *testing.T) {
	var m lyricMemory
	for i := 0; i < lyricMemoryLimit+1; i++ {
		key := string(rune('a' + i))
		e := memEntry(key)
		m.put(key, e)
	}
	if _, ok := m.get("a", "a", ""); ok {
		t.Error("最老的那一份没有被淘汰")
	}
	if _, ok := m.get(string(rune('a'+lyricMemoryLimit)), "", ""); !ok {
		t.Error("刚放进去的那一份读不到")
	}
}

// TestLyricMemoryChecksTheIdentity 钉住「同键不同歌不当命中」。
func TestLyricMemoryChecksTheIdentity(t *testing.T) {
	var m lyricMemory
	e := memEntry("甲")
	e.title, e.artist = "晴天", "周杰伦"
	m.put("bili-BV1-1", e)
	if _, ok := m.get("bili-BV1-1", "七里香", "周杰伦"); ok {
		t.Error("曲名对不上还是命中了")
	}
	if _, ok := m.get("bili-BV1-1", "晴天", "周杰伦"); !ok {
		t.Error("曲名和歌手都对上却不命中")
	}
}

// TestLyricMemoryUpdatesTheUsersValues 钉住偏移/字号跟着内存那份一起更新。
func TestLyricMemoryUpdatesTheUsersValues(t *testing.T) {
	var m lyricMemory
	e := memEntry("甲")
	m.put("k", e)
	updated := e.entry
	updated.OffsetMS = -700
	updated.FontSize = 32
	m.updateEntry("k", updated)

	got, ok := m.get("k", "", "")
	if !ok {
		t.Fatal("更新之后读不到")
	}
	if got.entry.OffsetMS != -700 || got.entry.FontSize != 32 {
		t.Errorf("用户值 = %+v，want -700 / 32", got.entry)
	}
	if len(got.rows) != 1 || got.rows[0].Text != "甲" {
		t.Errorf("更新用户值把歌词行弄丢了：%+v", got.rows)
	}
}

// TestLyricMemoryDoesNotStoreEmptyLyrics 钉住「没有歌词不占内存」。
func TestLyricMemoryDoesNotStoreEmptyLyrics(t *testing.T) {
	var m lyricMemory
	m.put("k", lyricMemoryEntry{entry: lyrics.Entry{Lyrics: ""}})
	if _, ok := m.get("k", "", ""); ok {
		t.Error("没有歌词的那一份被存进内存了")
	}
}
