package app

import "testing"

func TestAdoptPagesExpandsCurrentItemWithoutMovingPlayback(t *testing.T) {
	q := NewQueue()
	current := Item{
		Source: SourceOnline,
		Type:   "mv",
		Bvid:   "BV1",
		Title:  "视频标题",
		Cover:  "cover",
	}
	q.Append(LocalItem("before.mp3"), current, LocalItem("after.mp3"))
	q.Move(1)
	q.Move(1)
	before := q.CurrentIndex()

	result, ok := q.AdoptPages(current, PageSet{
		Bvid:       "BV1",
		VideoTitle: "视频标题",
		Parts: []PageSpec{
			{Cid: "101", Index: 1, Title: "第一集"},
			{Cid: "202", Index: 2, Title: "第二集"},
			{Cid: "303", Index: 3, Title: "第三集"},
		},
	})
	if !ok {
		t.Fatal("AdoptPages returned false")
	}
	if result.Added != 2 {
		t.Fatalf("added %d pages, want 2", result.Added)
	}
	if result.CurrentIndex != before {
		t.Fatalf("current index %d, want %d", result.CurrentIndex, before)
	}
	items := q.Items()
	if len(items) != 5 {
		t.Fatalf("queue length %d, want 5", len(items))
	}
	for i, want := range []struct {
		cid   string
		title string
		index int
	}{
		{"", "before", 0},
		{"101", "第一集", 1},
		{"202", "第二集", 2},
		{"303", "第三集", 3},
		{"", "after", 0},
	} {
		if items[i].Cid != want.cid || items[i].Title != want.title || items[i].PageIndex != want.index {
			t.Errorf("item %d = %#v, want cid=%q title=%q page=%d", i, items[i], want.cid, want.title, want.index)
		}
	}
	if got, _ := q.Current(); got.Cid != "101" || got.Title != "第一集" {
		t.Fatalf("current = %#v, want first page", got)
	}
}

func TestAdoptPagesUsesCurrentCidAndDoesNotDuplicateExistingPages(t *testing.T) {
	q := NewQueue()
	current := Item{Source: SourceOnline, Type: "mv", Bvid: "BV1", Cid: "202", Title: "旧标题"}
	q.Append(Item{Source: SourceOnline, Type: "mv", Bvid: "BV1", Cid: "101", Title: "已有第一集"}, current)
	q.Move(1)
	q.Move(1)

	result, ok := q.AdoptPages(current, PageSet{
		Bvid:       "BV1",
		VideoTitle: "视频标题",
		Parts: []PageSpec{
			{Cid: "101", Index: 1, Title: "第一集"},
			{Cid: "202", Index: 2, Title: "第二集"},
			{Cid: "303", Index: 3, Title: "第三集"},
		},
	})
	if !ok {
		t.Fatal("AdoptPages returned false")
	}
	if result.Added != 1 {
		t.Fatalf("added %d pages, want 1", result.Added)
	}
	items := q.Items()
	if len(items) != 3 {
		t.Fatalf("queue length %d, want 3", len(items))
	}
	if items[0].Cid != "101" || items[1].Cid != "202" || items[2].Cid != "303" {
		t.Fatalf("page cids = %q, %q, %q", items[0].Cid, items[1].Cid, items[2].Cid)
	}
	if items[0].GroupKey != "pages:BV1" || items[1].GroupIndex != 2 || items[2].GroupCount != 3 {
		t.Fatalf("page metadata not adopted: %#v", items)
	}
}

func TestAdoptPagesRejectsStaleCurrentAndExistingCollectionGroup(t *testing.T) {
	q := NewQueue()
	stale := Item{Source: SourceOnline, Type: "mv", Bvid: "BV1", Title: "视频"}
	q.Append(stale)
	q.Move(1)
	if _, ok := q.AdoptPages(Item{Source: SourceOnline, Type: "mv", Bvid: "BV1", Title: "其他"}, PageSet{
		Bvid: "BV1", Parts: []PageSpec{{Cid: "101", Index: 1}},
	}); ok {
		t.Fatal("stale expected item was adopted")
	}
	collection := stale
	collection.GroupKey = "season:1"
	q.Reset(collection)
	if _, ok := q.AdoptPages(collection, PageSet{
		Bvid: "BV1", Parts: []PageSpec{{Cid: "101", Index: 1}},
	}); ok {
		t.Fatal("collection item was adopted as page group")
	}
}

func TestAdoptPagesUpdatesUndoIndexWhenInsertingBeforeCurrent(t *testing.T) {
	q := NewQueue()
	first := LocalItem("first.mp3")
	current := Item{Source: SourceOnline, Type: "mv", Bvid: "BV1", Cid: "202", Title: "第二集"}
	q.Append(first, current)
	q.Move(1)
	q.Move(1)
	q.mu.Lock()
	q.undo = stepUndo{ok: true, prevCur: 1}
	q.mu.Unlock()

	if _, ok := q.AdoptPages(current, PageSet{
		Bvid: "BV1",
		Parts: []PageSpec{
			{Cid: "101", Index: 1, Title: "第一集"},
			{Cid: "202", Index: 2, Title: "第二集"},
		},
	}); !ok {
		t.Fatal("AdoptPages returned false")
	}
	if q.undo.prevCur != 2 {
		t.Fatalf("undo prevCur = %d, want 2", q.undo.prevCur)
	}
}
