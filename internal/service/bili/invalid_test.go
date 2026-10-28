package bili

// 「这一条已经失效」的判据。
//
// # 为什么它住在服务层
//
// `attr` 是 **B 站给的那个字段**（0 正常，1/9 失效），解析在这一层
// （`collection.go` 的 `resourceListData`）。判据从字段到结论只有一步，
// 放在解析它的这一层是最省事的——而**省事的意思是「只有一处」**：
// 界面层、应用层各判一次的话，改判据时会漏掉一处，而漏掉的那一处症状
// 是「还有失效的歌在被无谓地请求」（用户等的是一串串取流超时）。
//
// # 它和 `Playable` 是两件事
//
//	`Playable`	这一条**有没有音频流**（合集 / 电影没有）
//	`Invalid`	这一条**还在不在**（稿件被删了、或者换成了别的链接）
//
// 两者都要「放不了」，但**说的不是同一句话**：
// 合集是「打开里面的单集就能放」，失效是「这一条已经没了」。
// 混成一句的话，用户照着做的下一步就不对。
//
// ⚠️ **失效的条目照样出现在列表里**（这是 B 站给的事实，界面照画）：
// 直接不画的话，用户会发现「收藏夹里的歌自己少了几首」而且不知道为什么。

import "testing"

// TestAMediaIsInvalidWhenAttrSaysSo 钉住判据本身。
//
// ⚠️ **attr 的取值**：B 站那边 `0` 是正常，`1` 是「其他原因删除」，
// `9` 是「UP 自己删除」（上游 `service/fav-resource.ts:95` 的注释）。
// 所以**非 0 就是失效**——判据是「不是 0」，不是「是 1 或 9」。
func TestAMediaIsInvalidWhenAttrSaysSo(t *testing.T) {
	for _, attr := range []int{0, 1, 9} {
		m := Media{Type: MediaVideo, Attr: attr}
		want := attr != 0
		if got := m.Invalid(); got != want {
			t.Errorf("attr=%d：判据说失效=%v，want %v", attr, got, want)
		}
	}
}

// TestAnInvalidMediaIsStillPlayableByType 钉住两件事是**分开的**。
//
// 🔴 **这一条是本文件存在的理由**：把「失效」并进 `Playable` 会让
// `Playable` 同时回答两个问题（「有没有音频流」和「还在不在」），
// 而这两个问题的**下一步不一样**——合集是「打开里面的单集」，失效是
// 「它已经没了」��两者的说明文字不能共用一句。
//
// 而 `Playable` 那一层现在**只**回答「有没有音频流」：合集 / 电影靠它
// 排除在批量操作之外（`app/sidebar_folders.go` 的 `playableItems`）。
func TestAnInvalidMediaIsStillPlayableByType(t *testing.T) {
	// 一条**类型上能放、但已经失效**的投稿。
	m := Media{Type: MediaVideo, Bvid: "BVx", Attr: 1}
	if !m.Playable() {
		t.Error("一条 type=2 的投稿被判成「没有音频流」——`Playable` 判的是类型，不是有效性")
	}
	if !m.Invalid() {
		t.Error("attr=1 的条目没有被判成失效")
	}
}
