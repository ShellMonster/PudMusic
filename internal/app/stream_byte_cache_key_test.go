package app

import "testing"

// 这一组钉「字节缓存的键」：
//
//	同一首歌不管从哪条路进来（收藏带 sid / 队列恢复不带 sid），
//	键必须**是同一个**——不然字节写在 A 键下、用 B 键去查，
//	缓存永远不命中（2026-11-02 的实测：同一首歌八天取流九次）。
//
// # 为什么 sid 必须从 mv 的键里剔掉
//
// sid（音乐库 auid）是**收藏 / 合集**那一侧带来的旁支身份，而 mv 那一路
// 取流问的是 `bvid + cid`（`playurl`），sid 从头到尾不参与取流。
// 它在场与否只取决于「用户从哪个页面点的这一首」——那是**入口的形状**，
// 不是**歌的身份**。

func TestTheByteCacheTrackKeyIsTheSameFromBothEntrances(t *testing.T) {
	fromCollection := Item{Source: "online", Type: "mv", Bvid: "BV1xx", Cid: "42", Sid: "999"}
	fromRestore := Item{Source: "online", Type: "mv", Bvid: "BV1xx", Cid: "42"}
	if byteCacheTrack(fromCollection) != byteCacheTrack(fromRestore) {
		t.Errorf("同一首歌的两个入口算出了两个键：%q ≠ %q",
			byteCacheTrack(fromCollection), byteCacheTrack(fromRestore))
	}
}

func TestTheByteCacheTrackKeyStillSeparatesPagesAndKinds(t *testing.T) {
	// 不同的分 P 是不同的音频，键必须不同。
	p1 := Item{Source: "online", Type: "mv", Bvid: "BV1xx", Cid: "42"}
	p2 := Item{Source: "online", Type: "mv", Bvid: "BV1xx", Cid: "43"}
	if byteCacheTrack(p1) == byteCacheTrack(p2) {
		t.Error("不同分 P 的键撞上了")
	}
	// 音乐库那一路只认 sid：bvid / cid 在不在都一样。
	a := Item{Source: "online", Type: "audio", Sid: "7"}
	aWithNoise := Item{Source: "online", Type: "audio", Sid: "7", Cid: "42"}
	if byteCacheTrack(a) != "online|audio||7" {
		t.Errorf("音乐库的键形状变了：%q", byteCacheTrack(a))
	}
	if byteCacheTrack(a) != byteCacheTrack(aWithNoise) {
		t.Errorf("音乐库的键被多余的栏目拆开了：%q ≠ %q",
			byteCacheTrack(a), byteCacheTrack(aWithNoise))
	}
}

func TestLegacyByteCacheTrackRecognizesOnlyOldMVKeys(t *testing.T) {
	// 旧键：mv 带着非空的 sid（2026-11-02 之前的形状）。
	old := "online|mv|BV1xx|42|999"
	newTrack, ok := legacyByteCacheTrack(old)
	if !ok {
		t.Errorf("旧键没被认出来：%q", old)
	}
	if want := "online|mv|BV1xx|42|"; newTrack != want {
		t.Errorf("迁移目标算错了：%q，want %q", newTrack, want)
	}
	// 已经是新形状的（sid 空）：不动。
	if _, ok := legacyByteCacheTrack("online|mv|BV1xx|42|"); ok {
		t.Error("新形状的键被当成旧的迁移了")
	}
	// 音乐库的键两版同形：不动。
	if _, ok := legacyByteCacheTrack("online|audio||7"); ok {
		t.Error("音乐库的键被当成旧的迁移了")
	}
	// 缺了 sid 栏的五段之外不是我们的键：不动。
	if _, ok := legacyByteCacheTrack("local|mv|BV1xx|42"); ok {
		t.Error("四段的键被当成旧的迁移了")
	}
}
