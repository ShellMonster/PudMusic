package app

import "testing"

func TestQueueSnapshotReusesStableRevisionAndPreservesDuplicates(t *testing.T) {
	q := NewQueueWithRand(testRand{})
	first := Item{Source: SourceOnline, Type: "mv", Bvid: "BV1", Title: "第一份"}
	duplicate := first
	duplicate.Title = "第二份"
	q.Reset(first, duplicate)

	before := q.Snapshot()
	after := q.Snapshot()
	if before.Revision() == 0 || before.Revision() != after.Revision() {
		t.Fatalf("unchanged queue revision = %d then %d", before.Revision(), after.Revision())
	}
	if before.Len() != 2 {
		t.Fatalf("snapshot length = %d, want 2 including duplicate", before.Len())
	}
	got, ok := before.At(1)
	if !ok || got.Title != "第二份" {
		t.Fatalf("snapshot second item = %+v, %v", got, ok)
	}

	q.SetPlayMode(PlayModeRandom)
	changed := q.Snapshot()
	if changed.Revision() == before.Revision() {
		t.Fatal("play mode change did not invalidate queue snapshot")
	}
	if changed.Len() != 2 || changed.PlayMode() != PlayModeRandom {
		t.Fatalf("changed snapshot = len %d mode %q", changed.Len(), changed.PlayMode())
	}
}

type testRand struct{}

func (testRand) Intn(int) int { return 0 }
