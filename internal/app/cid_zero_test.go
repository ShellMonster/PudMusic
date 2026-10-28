package app

// 「分 P id 是 0」的那一条（票：首页榜单点不动）。
//
// # 为什么这一族存在
//
// `Item.Cid` 是**字符串**，而在线播放那一路判的是 `Cid != ""`（`withCid`：
// 「已经有 cid 的原样返回，缺的去问一次」）。于是**字符串 `"0"` 会冒充一个
// 真的分 P id**：
//
//	withCid 看到 "0" ≠ "" → 不去问分 P
//	PlayURL 看到 "0" ≠ "" → 把 cid=0 真发出去 → B 站回 -400「请求错误」
//
// 而 B 站有一批接口**就是不给分 P id**（音乐榜的 `creation_first_cid` 我们
// 原来没解析；新歌速递上游直接给 `"cid": "0"`）——那批接口给的数字 cid 是
// **零**，拼成字符串就是 `"0"`。
//
// 这一族盯的是那个**拼字符串的地方**：零必须变成**空串**，空串才让
// `withCid` 知道「缺了，得问一次」。

import "testing"

// TestTheQueueItemHasNoPartIdWhenTheUpstreamHasNone 盯零 → 空串。
//
// ⚠️ **判据是「空串」而不是「0」**：判 `Cid == "0"` 的话，改成别的写法
// （比如带空格）就又漏过去了；而 `withCid` 认的就是空串。
func TestTheQueueItemHasNoPartIdWhenTheUpstreamHasNone(t *testing.T) {
	it := onlineItem("BV1x", 0, "一首歌", "封面", "歌手", 1)
	if it.Cid != "" {
		t.Errorf("分 P id = %q，want 空串（\"0\" 会被当成一个真的分 P id 传下去，"+
			"B 站回 -400，而那一层问分 P 的补救也因此被跳过）", it.Cid)
	}
}

// TestTheQueueItemKeepsThePartIdWhenTheUpstreamHasOne 盯**有分 P id 时照旧带着**。
//
// 这一条是上面那一条的反面：改成「一律空串」的话，有分 P id 的条目会白白多
// 问一次网络往返（`withCid` 每次都要问 `/x/player/pagelist`）。
func TestTheQueueItemKeepsThePartIdWhenTheUpstreamHasOne(t *testing.T) {
	it := onlineItem("BV1x", 27922599802, "一首歌", "封面", "歌手", 1)
	if it.Cid != "27922599802" {
		t.Errorf("分 P id = %q，want \"27922599802\"", it.Cid)
	}
}

// TestANegativePartIdIsAlsoTreatedAsNone 盯**负数**也当没有。
//
// B 站的 cid 不会是负的，而负数拼成字符串是 `"-1"`——它和 `"0"` 一样会冒充
// 一个真的分 P id（B 站会回 -400）。所以判据是「小于等于零」，不是「等于零」。
func TestANegativePartIdIsAlsoTreatedAsNone(t *testing.T) {
	it := onlineItem("BV1x", -1, "一首歌", "封面", "歌手", 1)
	if it.Cid != "" {
		t.Errorf("分 P id = %q，want 空串", it.Cid)
	}
}
