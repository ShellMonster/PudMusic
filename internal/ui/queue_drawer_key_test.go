package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 抽屉那一列的**行身份**（铁律四），在数据这一层钉住。
//
// 界面上看得见的后果由 `queue_drawer_test.go` 那边验（同一首排两遍只画一行、
// 正在放的那行有高亮）；这里断的是更靠底的东西：**行的 key 是这一首自己的判重
// 标识，不是它在队列里的位置**。
//
// 为什么非得是身份：MyGo 默认拿「父 ID + 兄弟序号」当行的身份，前面插一行会让
// 后面所有行的状态整体错配。抽屉上错配出来的东西不少——高亮到了旁边那首、
// 菜单删掉的是别的歌、滚动位置跳了。而这一层测不到：把 key 换成行号，
// `queue_drawer_test.go` 那几条**照样是绿的**（行还是那些行，只是身份换了）。
// 所以它的判据必须写在这里：插一行之后，后面那些行的 key 逐个不变。

func mv(bvid, title string) app.Item {
	return app.Item{Source: "online", Type: "mv", Bvid: bvid, Cid: "1", Title: title}
}

// TestQueueRowKeysFollowTheSongNotItsPlace 在队头插一首，后面每一行的 key 都
// 不许变——变的就说明它是拿行号当身份的。
func TestQueueRowKeysFollowTheSongNotItsPlace(t *testing.T) {
	b, c := mv("BV2", "第二首"), mv("BV3", "第三首")

	before := queueRowsFor([]app.Item{b, c})
	after := queueRowsFor([]app.Item{mv("BV1", "第一首"), b, c})

	if len(before) != 2 || len(after) != 3 {
		t.Fatalf("去重之后 %d 行 / 插了一首之后 %d 行，want 2 / 3", len(before), len(after))
	}
	for i := range before {
		if before[i].key != after[i+1].key {
			t.Errorf("第 %d 行（%q）的身份从 %q 变成了 %q——它用的是行号",
				i+1, before[i].item.Title, before[i].key, after[i+1].key)
		}
		// 行号确实动了（所以上面那一断言不是白给的）：at 从 i 变成了 i+1。
		if before[i].at+1 != after[i+1].at {
			t.Errorf("%q 在队列里的位置是 %d → %d", before[i].item.Title, before[i].at, after[i+1].at)
		}
	}
	if after[0].key == after[1].key {
		t.Errorf("新插进来的那一行和第二首撞了身份：%q", after[0].key)
	}
}

// TestQueueRowsKeepTheFirstOfADuplicate 同一首排了两遍，抽屉留的是**第一次**
// 出现的那一条，at 指的是它在队列里的位置。
//
// at 必须是队里那个位置：点一行、删一行用的都是它，拿行号去调队列就是删错那一首。
func TestQueueRowsKeepTheFirstOfADuplicate(t *testing.T) {
	dup := mv("BV1", "第一首")
	rows := queueRowsFor([]app.Item{mv("BV2", "第二首"), dup, dup})

	if len(rows) != 2 {
		t.Fatalf("去重之后 %d 行，want 2", len(rows))
	}
	if rows[1].at != 1 {
		t.Errorf("留下那一行的 at 是 %d，want 1（它在队列里第一次出现的位置）", rows[1].at)
	}
	if rows[1].item != dup {
		t.Errorf("留下的是 %+v，want %+v", rows[1].item, dup)
	}
}

// TestQueueRowsGiveUnidentifiableItemsDistinctKeys 认不出是哪一首的项
// （`Identity` 返回空串）**不能**拿同一个空串当身份。
//
// 两个空串在 Tester 里当场 panic（重复的 key），真窗口里是两行的状态串在
// 一处、删一行连着删掉两首。它们的身份退回队列下标。
func TestQueueRowsGiveUnidentifiableItemsDistinctKeys(t *testing.T) {
	rows := queueRowsFor([]app.Item{{Title: "还没接来源"}, {Title: "也还没接来源"}})

	if len(rows) != 2 {
		t.Fatalf("认不出的项被并成了 %d 行，want 2", len(rows))
	}
	if rows[0].key == rows[1].key {
		t.Errorf("两个认不出的项撞了身份：%q", rows[0].key)
	}
	// 但它们照样能被认出来是自己那一行：两个不同的键。
	if rows[0].key != queueRowKey("", 0) || rows[1].key != queueRowKey("", 1) {
		t.Errorf("身份是 %q / %q，want 按队列下标退回去", rows[0].key, rows[1].key)
	}
}

// TestQueueRowKeyCarriesTheIdentity 身份的形状：认得出来的用判重标识，
// 认不出来的用下标——而且**带一个歌的标识撞不上的前缀**。
func TestQueueRowKeyCarriesTheIdentity(t *testing.T) {
	cases := []struct {
		name string
		id   string
		at   int
		want string
	}{
		{"认得出来的", "mv:BV1", 3, "queue-row:mv:BV1"},
		{"认不出来的", "", 3, "queue-row!3"},
	}
	for _, c := range cases {
		if got := queueRowKey(c.id, c.at); got != c.want {
			t.Errorf("%s：queueRowKey(%q, %d) = %q，want %q", c.name, c.id, c.at, got, c.want)
		}
	}
	// 认不出的那一支和认得出的那一支**永远不会撞**：`Identity` 造出来的键一律
	// 是 `<local|mv|audio>:<值>`，冒号是它的一部分，所以前缀后面的那个字节就把
	// 两串分开了。写成 `queue-row:#1` 的话，一首标识正好是 `#1` 的歌就会撞上
	// 队里第一行那个认不出的项。
	if queueRowKey("", 1) == queueRowKey("#1", 0) {
		t.Error("认不出的项和标识正好是「#1」的那一首撞了身份")
	}
}
