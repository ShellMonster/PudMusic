package mobileui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

func mixedCollectionFixture(t *testing.T) (*shell, *collectionState) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	client := bili.NewClient(bili.Options{Transport: server.Client().Transport})
	client.TestOnlyPointHostsAtFixture(server.URL)
	a := app.New()
	t.Cleanup(a.Close)
	a.TestOnlyNoAutoLyrics()
	a.MountStreaming(app.Streaming{Client: client, Media: server.Client(), TempDir: t.TempDir()})
	s := newShell(t.Context(), a)
	state := &collectionState{}
	state.pages.account = a.AccountSnapshot()
	state.pages.items = []bili.Media{
		{ID: 10, Type: 2, Bvid: "BVfixture10", Title: "第一首"},
		{ID: 20, Type: 2, Bvid: "BVfixture20", Title: "失效内容", Attr: 1},
		{ID: 30, Type: 2, Bvid: "BVfixture30", Title: "第三首"},
	}
	s.push(route{kind: pageCollection, ref: app.CollectionRef{ID: 42, Type: bili.CollectionFavorite}, data: state})
	return s, state
}

// TestMobileMixedCollectionKeepsPlayableQueue checks that an unavailable row does not split the playlist.
func TestMobileMixedCollectionKeepsPlayableQueue(t *testing.T) {
	s, _ := mixedCollectionFixture(t)
	tester := ui.NewTester(s.view, 390, 844)
	if err := tester.Click("第三首"); err != nil {
		t.Fatal(err)
	}
	queue := s.app.Queue().Snapshot()
	first, _ := queue.At(0)
	if queue.Len() != 2 || first.Title != "第一首" {
		t.Fatalf("queue length = %d, first = %q", queue.Len(), first.Title)
	}
}

// TestMobileCollectionCompletionRespectsNewerPlayback verifies that a delayed Play All cannot replace a later local selection.
func TestMobileCollectionCompletionRespectsNewerPlayback(t *testing.T) {
	s, state := mixedCollectionFixture(t)
	updates := make(chan func(), 1)
	s.dispatch = func(fn func()) { updates <- fn }
	s.playCollection(state, s.current().ref, false)
	update := <-updates
	local := app.LocalItem(filepath.Join("..", "service", "audio", "testdata", "tone.mp3"))
	if err := s.app.PlayFromIndex([]app.Item{local}, 0); err != nil {
		t.Fatal(err)
	}
	update()
	if current, ok := s.app.Queue().Current(); !ok || current.Path != local.Path {
		t.Fatalf("collection completion replaced %q with %q", local.Path, current.Path)
	}
}

// TestTabletCollectionSwitchesDetailsFromTheIndex verifies that related playlists stay reachable beside an open playlist.
func TestTabletCollectionSwitchesDetailsFromTheIndex(t *testing.T) {
	s, _ := mixedCollectionFixture(t)
	first := s.current().ref
	second := app.CollectionRef{ID: 43, Type: bili.CollectionFavorite}
	s.stacks[0][0].data = &discoverState{mixes: browseMixes{cards: []browseCollection{
		{ref: first, title: "歌单甲"}, {ref: second, title: "歌单乙"},
	}}}
	tester := ui.NewTester(s.view, 780, 844)
	index, indexFound := tester.Find("歌单乙")
	track, trackFound := tester.Find("第一首")
	if !indexFound || !trackFound || index.X+index.W > track.X {
		t.Fatalf("playlist list and track detail did not appear side by side: %v, %v", index, track)
	}
	mobileLibraryShot(t, tester, "tablet-collection.png")
	if err := tester.Click("歌单乙"); err != nil {
		t.Fatal(err)
	}
	if s.current().ref != second || len(s.stacks[0]) != 2 {
		t.Fatalf("playlist selection = %+v, stack depth = %d", s.current().ref, len(s.stacks[0]))
	}
}
