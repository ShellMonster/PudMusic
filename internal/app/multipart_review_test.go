// 票 #683 复审里「敢把两集认成一首」的那几处判据（BLOCKER 1、MAJOR 2、MINOR 9）。
//
// 这一份是 `package app`：要直接比 `sameStreamTarget` / `Identity` / `SameTrack`
// 这几条未导出的判据。
package app

import "testing"

// TestIndexOfStreamTargetPrefersTheExactEpisode 钉住「精确匹配优先于宽松匹配」。
//
// 队列里先有一项**不知道是哪一集**的同一个 bvid（收藏夹 / 搜索结果来的），
// 后面才是用户点的那一集。`sameStreamTarget` 对「一边有 cid 一边没有」判同一
// ——于是拿它从头扫，返回的永远是**第一项**（未知的那一项），用户点的「第 2 集」
// 被顶掉，取流请求带的是第一集的 cid。
func TestIndexOfStreamTargetPrefersTheExactEpisode(t *testing.T) {
	q := NewQueue()
	unknown := Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "不知道哪一集"}
	second := Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "222", Title: "第二集"}
	q.Reset(unknown, second)

	at, ok := q.indexOfStreamTarget(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "222"})
	if !ok {
		t.Fatal("队列里明明有那一集，却找不到")
	}
	if at != 1 {
		t.Errorf("定位到第 %d 项，want 1（精确匹配的那一项；第 0 项是「不知道是哪一集」）", at)
	}
}

// TestIndexOfStreamTargetStillFallsBackToTheUnknownEpisode 是上面那一条的另一半：
// 队列里**只有**「不知道是哪一集」的那一项时，点同一首还是要定位过去（不追加）。
func TestIndexOfStreamTargetStillFallsBackToTheUnknownEpisode(t *testing.T) {
	q := NewQueue()
	unknown := Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "不知道哪一集"}
	q.Reset(unknown)

	at, ok := q.indexOfStreamTarget(Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "222"})
	if !ok || at != 0 {
		t.Errorf("indexOfStreamTarget = %d/%v，want 0/true（只有那一项，只能定位到它）", at, ok)
	}
}

// TestSameStreamTargetTellsAnAudioFromAVideo 钉住「先判种类」。
//
// `ItemOfMedia` 对**所有**类型都填 `Sid = Media.ID`，而 avid 和 auid 是同一个
// 数值空间——不判种类的话，队列里正在放的 auid=12345 和收藏夹里 avid=12345 的
// 那一行会被认成同一首：列表上那一行在跳、点它播的是「音乐库那一首」。
func TestSameStreamTargetTellsAnAudioFromAVideo(t *testing.T) {
	audio := Item{Source: "online", Type: "audio", Sid: "12345", Title: "音乐库那一首"}
	video := Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD", Sid: "12345", Title: "收藏夹那一行"}
	if sameStreamTarget(audio, video) {
		t.Error("音乐库条目和视频行被判成「同一段流」——Sid 撞上了同一个数")
	}
	if sameStreamTarget(video, audio) {
		t.Error("反过来也判成了同一段流")
	}
	// 同类还是要认得出。
	if !sameStreamTarget(audio, Item{Source: "online", Type: "audio", Sid: "12345"}) {
		t.Error("两个音乐库条目（同一个 sid）没被判成同一段流")
	}
	if !sameStreamTarget(video, Item{Source: "online", Type: "mv", Bvid: "BV1xx411c7mD"}) {
		t.Error("同一个投稿（一行有 cid、一行没有）没被判成同一段流")
	}
}

// TestAnMVWithoutABvidHasNoIdentity 钉住「bvid 为空时 `Identity()` 是空串」。
//
// 原来那一支会拼出 `mv:|<cid>`：两首认不出 bvid 的坏项各带一个 cid 就判成了
// 两首（而它们连是哪一首都不知道），空串那条「没法判重」的约定也被绕过去了。
func TestAnMVWithoutABvidHasNoIdentity(t *testing.T) {
	broken := Item{Source: "online", Type: "mv", Cid: "777"}
	if got := broken.Identity(); got != "" {
		t.Errorf("认不出 bvid 的 mv 有了身份 %q，want 空串", got)
	}
	other := Item{Source: "online", Type: "mv", Cid: "888"}
	if SameTrack(broken, other) {
		t.Error("两项都没有 bvid、只是 cid 不同，被判成了同一首")
	}
}

// TestFillCidCoversEveryCopyOfTheCurrentEpisode 是 `writeBackCid` 那一半的单元版
// （整链那一条在 `multipart_restore_test.go`）。
//
// 队列里同一集排两份，两份的身份都是 `mv:BV1`。补 cid 只补当前那一份的话，
// 抽屉那一行会被拆成两行（见 `fillCidIf` 的注释）。
func TestFillCidCoversEveryCopyOfTheCurrentEpisode(t *testing.T) {
	q := NewQueue()
	dup := Item{Source: "online", Type: "mv", Bvid: "BV1", Title: "同一集"}
	q.Reset(dup, dup)
	cur, ok := q.Current()
	if !ok {
		t.Fatal("没有当前项")
	}
	if !q.fillCidIf(cur, "777") {
		t.Fatal("补 cid 什么都没改")
	}
	items := q.Items()
	if items[0].Cid != "777" || items[1].Cid != "777" {
		t.Errorf("补完之后的 cid 是 %q / %q，want 两个 777（同一集的两份身份要一样）",
			items[0].Cid, items[1].Cid)
	}
	if items[0].Identity() != items[1].Identity() {
		t.Errorf("两份的身份不一样：%q / %q——抽屉会画成两行",
			items[0].Identity(), items[1].Identity())
	}
	// 已经是确定的某一集时一个字都不改。
	if q.fillCidIf(items[0], "888") {
		t.Error("当前的 cid 已经有值了，还被改成了另一集")
	}
}
