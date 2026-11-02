package streamaudio

import (
	"os"
	"testing"
	"time"
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

// TestMigrateKeepsEveryTiersPref 盯「同一首的多档，每一档的偏好都要搬过去」
// （2026-11-02 审查②的 P1-1）。
//
// 旧实现按**条目**搬，搬完一条就去动 pref——而同一首的两个档在 readAll 里
// 是**散开的**（目录序）：先处理的档会把两个 pref 文件**都删掉**，而后处理
// 那一档的字节还没到新键下，它的 pref 被判「新键下没有完整字节」不搬——
// 旧文件已经删了，这条偏好就这么丢了。多档用户（既下过 192K 又下过无损）
// 升级后部分偏好要整首重下。
//
// 🔴 **档名挑过**：`q30216` 的记录文件名排目录序**在前**——正好造出
// 「先处理 q30216、后处理 q30280」这个触发顺序。
func TestMigrateKeepsEveryTiersPref(t *testing.T) {
	c := NewCache(t.TempDir())
	oldTrack, newTrack := "online|mv|BV1|42|999", "online|mv|BV1|42|"
	for _, tc := range []struct {
		tier string
		pref string
		size int64
	}{
		{tier: "q30216", pref: "1", size: 100},
		{tier: "q30280", pref: "2", size: 200},
	} {
		writeBytesFile(t, c, oldTrack, tc.tier, int(tc.size))
		if err := c.SaveBytes(oldTrack, tc.tier, tc.size, "x", Meta{}); err != nil {
			t.Fatal(err)
		}
		if err := c.SavePref(oldTrack, tc.pref, tc.tier); err != nil {
			t.Fatal(err)
		}
	}

	if got := c.Migrate(func(track string) (string, bool) {
		if track == oldTrack {
			return newTrack, true
		}
		return "", false
	}); got != 2 {
		t.Fatalf("搬了 %d 份，want 2", got)
	}
	for _, pref := range []string{"1", "2"} {
		if _, ok := c.Lookup(newTrack, pref); !ok {
			t.Errorf("pref %q 搬丢了——多档用户的这个偏好升级后要整首重下", pref)
		}
	}
}

// TestMigrateMovesOverABrokenTarget 盯「目标位『记录说完整、字节对不上』时，
// 旧键下的好份要顶上去」（审查②的 P1-2；#729 已把判据换成 `Complete()`，
// 这条是钉住它的回归网——旧实现信裸 flag 会把唯一的好份直接清掉）。
func TestMigrateMovesOverABrokenTarget(t *testing.T) {
	c := NewCache(t.TempDir())
	oldTrack, newTrack, tier := "online|mv|BV1|42|999", "online|mv|BV1|42|", "q30280"

	// 目标位：记录说 200，盘上只有 100——`Complete` 的对账判据拒发它。
	writeBytesFile(t, c, newTrack, tier, 100)
	if err := c.SaveBytes(newTrack, tier, 200, "192K", Meta{}); err != nil {
		t.Fatal(err)
	}
	// 旧键下是一份真完整的好份。
	writeBytesFile(t, c, oldTrack, tier, 300)
	if err := c.SaveBytes(oldTrack, tier, 300, "192K", Meta{}); err != nil {
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
	if b, ok := c.Complete(newTrack, tier); !ok || b.Size != 300 {
		t.Errorf("好份没顶上去：ok=%v b=%+v", ok, b)
	}
}

// TestMigrateMovesOverAnExpiredTarget 盯「目标位已过期（TTL 超时）时，
// 新鲜的旧份要顶上去」（审查②的 P1-3；同上，回归网）。过期的目标位
// `Lookup` 永远 miss，把新鲜份清掉等于白扔一份缓存。
func TestMigrateMovesOverAnExpiredTarget(t *testing.T) {
	c := NewCache(t.TempDir())
	oldTrack, newTrack, tier := "online|mv|BV1|42|999", "online|mv|BV1|42|", "q30280"

	// 目标位：完整，但字节文件的 mtime 推到 TTL 之外（8 天前）。
	writeBytesFile(t, c, newTrack, tier, 200)
	if err := c.SaveBytes(newTrack, tier, 200, "192K", Meta{}); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(c.BytesPath(newTrack, tier), stale, stale); err != nil {
		t.Fatal(err)
	}
	// 旧键下是新鲜的好份。
	writeBytesFile(t, c, oldTrack, tier, 300)
	if err := c.SaveBytes(oldTrack, tier, 300, "192K", Meta{}); err != nil {
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
	if b, ok := c.Complete(newTrack, tier); !ok || b.Size != 300 {
		t.Errorf("新鲜的份没顶上去：ok=%v b=%+v", ok, b)
	}
}
