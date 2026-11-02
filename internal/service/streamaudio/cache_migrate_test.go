package streamaudio

import (
	"os"
	"testing"
)

// 这一组钉「键换形状之后的搬迁」（2026-11-02）。
//
// 缓存键曾经照抄 `streamKey`（五栏全拼，mv 带着入口带来的 sid），改成归一键
// 之后，**旧键下已经下完的那些份不能就这么丢了**——丢一份用户就多下一整首。
// `Migrate` 把旧键的份搬到新键下；搬不动的（目标位已有更好的）照规矩处理。
func TestMigrateMovesOldShapedEntries(t *testing.T) {
	c := NewCache(t.TempDir())
	oldTrack, newTrack, tier, pref := "online|mv|BV1|42|999", "online|mv|BV1|42|", "q30280", "0"

	writeBytesFile(t, c, oldTrack, tier, 100)
	if err := c.SaveBytes(oldTrack, tier, 100, "192K", Meta{Title: "歌"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SavePref(oldTrack, pref, tier); err != nil {
		t.Fatal(err)
	}

	if got := c.Migrate(func(track string) (string, bool) {
		if track == oldTrack {
			return newTrack, true
		}
		return "", false
	}); got != 1 {
		t.Fatalf("搬了 %d 份，want 1", got)
	}

	// 新键下查得到：字节、记录、映射三样都在。
	b, ok := c.Lookup(newTrack, pref)
	if !ok {
		t.Fatal("搬过去之后查不到了")
	}
	if b.Size != 100 || b.Label != "192K" || b.Meta.Title != "歌" {
		t.Errorf("搬过去的记录缺了东西：%+v", b)
	}
	// 盘上那份字节就是原来那份（内容一个字节不差）。
	if st, err := os.Stat(c.BytesPath(newTrack, tier)); err != nil || st.Size() != 100 {
		t.Errorf("搬过去的字节不对：size=%v err=%v", st, err)
	}
	// 旧键下什么都不剩。
	for _, p := range []string{c.BytesPath(oldTrack, tier), c.bytesMetaPath(oldTrack, tier), c.prefPath(oldTrack, pref)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("旧键的文件还在：%s", p)
		}
	}
}

func TestMigrateKeepsTheCompleteTargetAndDropsTheOldOne(t *testing.T) {
	c := NewCache(t.TempDir())
	oldTrack, newTrack, tier := "online|mv|BV1|42|999", "online|mv|BV1|42|", "q30280"

	// 目标位上已经有一份完整的（新形状，后来下的）。
	writeBytesFile(t, c, newTrack, tier, 200)
	if err := c.SaveBytes(newTrack, tier, 200, "192K", Meta{Title: "新的"}); err != nil {
		t.Fatal(err)
	}
	// 旧键下也有一份完整的（旧形状，当初下的）。
	writeBytesFile(t, c, oldTrack, tier, 100)
	if err := c.SaveBytes(oldTrack, tier, 100, "192K", Meta{Title: "旧的"}); err != nil {
		t.Fatal(err)
	}

	if got := c.Migrate(func(track string) (string, bool) {
		if track == oldTrack {
			return newTrack, true
		}
		return "", false
	}); got != 0 {
		t.Fatalf("不该搬却搬了 %d 份", got)
	}
	// 目标位上的那份原封不动。
	b, ok := c.Complete(newTrack, tier)
	if !ok || b.Size != 200 || b.Meta.Title != "新的" {
		t.Errorf("目标位上的完整份被动了：%+v ok=%v", b, ok)
	}
	// 旧的那份清掉了。
	if _, err := os.Stat(c.BytesPath(oldTrack, tier)); !os.IsNotExist(err) {
		t.Error("旧键那份没被清掉")
	}
}

func TestMigratePrefersTheCompleteMoverOverAnIncompleteTarget(t *testing.T) {
	c := NewCache(t.TempDir())
	oldTrack, newTrack, tier := "online|mv|BV1|42|999", "online|mv|BV1|42|", "q30280"

	// 目标位上只有一份「正在下」的记录（StartBytes 的形状：不完整）。
	if err := c.StartBytes(newTrack, tier); err != nil {
		t.Fatal(err)
	}
	// 旧键下是一份完整的。
	writeBytesFile(t, c, oldTrack, tier, 100)
	if err := c.SaveBytes(oldTrack, tier, 100, "192K", Meta{Title: "旧的"}); err != nil {
		t.Fatal(err)
	}

	if got := c.Migrate(func(track string) (string, bool) {
		if track == oldTrack {
			return newTrack, true
		}
		return "", false
	}); got != 1 {
		t.Fatalf("该搬的没搬：%d", got)
	}
	if b, ok := c.Complete(newTrack, tier); !ok || b.Size != 100 {
		t.Errorf("完整的那份没顶上去：ok=%v b=%+v", ok, b)
	}
}

func TestMigrateLeavesEverythingElseAlone(t *testing.T) {
	c := NewCache(t.TempDir())
	track, tier, pref := "online|mv|BV1|42|", "q30280", "0"

	writeBytesFile(t, c, track, tier, 100)
	if err := c.SaveBytes(track, tier, 100, "192K", Meta{}); err != nil {
		t.Fatal(err)
	}
	if err := c.SavePref(track, pref, tier); err != nil {
		t.Fatal(err)
	}

	// remap 什么都不认：什么都不该动。
	if got := c.Migrate(func(string) (string, bool) { return "", false }); got != 0 {
		t.Fatalf("不该搬却搬了 %d 份", got)
	}
	if _, ok := c.Lookup(track, pref); !ok {
		t.Error("不该动的被动了")
	}
}
