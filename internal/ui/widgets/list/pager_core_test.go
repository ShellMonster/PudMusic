package list

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestPagerDropsStaleResultsAndDeduplicatesPages(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	current := map[int]PagerPage[string]{
		1: {Items: []string{"a", "b"}, HasMore: true},
	}
	fetch := func(ctx context.Context, page, loaded int, _ struct{}) (PagerPage[string], error) {
		if page == 2 {
			<-release
			return PagerPage[string]{Items: []string{"b", "c"}, HasMore: false}, nil
		}
		mu.Lock()
		defer mu.Unlock()
		return current[page], nil
	}
	var got []string
	var gotMu sync.Mutex
	p := NewPager(fetch, func(v string) string { return v }, func() struct{} { return struct{}{} })
	p.SetApply(func(page PagerPage[string]) int {
		gotMu.Lock()
		got = append(got, page.Items...)
		gotMu.Unlock()
		return len(page.Items)
	})
	p.Sync(ui.Services{})
	waitPager(t, func() bool {
		p.Sync(ui.Services{})
		return p.Status().Loaded == 2 && !p.Status().Loading
	})
	p.Load(PagerScrollEnd)
	waitPager(t, func() bool {
		p.Sync(ui.Services{})
		return p.Status().Loading
	})

	mu.Lock()
	current[1] = PagerPage[string]{Items: []string{"new"}, HasMore: false}
	mu.Unlock()
	gotMu.Lock()
	got = nil
	gotMu.Unlock()
	p.Reload()
	waitPager(t, func() bool {
		p.Sync(ui.Services{})
		return p.Status().Loading
	})
	close(release)
	waitPager(t, func() bool {
		p.Sync(ui.Services{})
		return p.Status().Loaded == 1 && !p.Status().Loading
	})

	gotMu.Lock()
	defer gotMu.Unlock()
	if len(got) != 1 || got[0] != "new" {
		t.Fatalf("stale page landed: %#v", got)
	}
}

func TestPagerStopsAfterFailureAndRetries(t *testing.T) {
	var mu sync.Mutex
	failed := true
	fetch := func(context.Context, int, int, struct{}) (PagerPage[string], error) {
		mu.Lock()
		defer mu.Unlock()
		if failed {
			return PagerPage[string]{}, errors.New("temporary")
		}
		return PagerPage[string]{Items: []string{"ok"}, HasMore: false}, nil
	}
	p := NewPager(fetch, func(v string) string { return v }, func() struct{} { return struct{}{} })
	p.SetApply(func(page PagerPage[string]) int { return len(page.Items) })
	p.Sync(ui.Services{})
	waitPager(t, func() bool {
		p.Sync(ui.Services{})
		return p.Status().Err != nil
	})
	if p.Status().Loading || !p.Status().Blocked {
		t.Fatalf("failure did not unlock and block: %+v", p.Status())
	}

	mu.Lock()
	failed = false
	mu.Unlock()
	p.Retry()
	waitPager(t, func() bool {
		p.Sync(ui.Services{})
		return p.Status().Loaded == 1 && !p.Status().Loading
	})
	if p.Status().Err != nil || p.Status().HasMore {
		t.Fatalf("retry did not recover: %+v", p.Status())
	}
}

func TestPagerStopsAfterEmptyPage(t *testing.T) {
	var calls int
	fetch := func(context.Context, int, int, struct{}) (PagerPage[string], error) {
		calls++
		if calls == 1 {
			return PagerPage[string]{Items: []string{"one"}, HasMore: true}, nil
		}
		return PagerPage[string]{Items: nil, HasMore: true}, nil
	}
	var got []string
	p := NewPager(fetch, func(v string) string { return v }, func() struct{} { return struct{}{} })
	p.SetApply(func(page PagerPage[string]) int {
		got = append(got, page.Items...)
		return len(page.Items)
	})
	p.Sync(ui.Services{})
	waitPager(t, func() bool {
		p.Sync(ui.Services{})
		return p.Status().Page == 1 && !p.Status().Loading
	})
	p.Load(PagerScrollEnd)
	waitPager(t, func() bool {
		p.Sync(ui.Services{})
		return !p.Status().Loading
	})
	st := p.Status()
	if st.HasMore {
		t.Fatalf("empty page left pager open: %+v", st)
	}
	if calls != 2 || len(got) != 1 || got[0] != "one" {
		t.Fatalf("calls=%d got=%v, want two calls and one item", calls, got)
	}
	p.Load(PagerScrollEnd)
	if calls != 2 {
		t.Fatalf("pager loaded after empty terminal page: %d calls", calls)
	}
}

func TestPagerWakesRegisteredServicesWhenResultArrives(t *testing.T) {
	var wakes atomic.Int32
	p := NewPager(func(context.Context, int, int, struct{}) (PagerPage[string], error) {
		return PagerPage[string]{Items: []string{"ok"}}, nil
	}, func(v string) string { return v }, func() struct{} { return struct{}{} })
	p.SetApply(func(page PagerPage[string]) int { return len(page.Items) })
	p.wakeMu.Lock()
	p.wakes = append(p.wakes, pagerWake{wake: func() { wakes.Add(1) }})
	p.wakeMu.Unlock()
	p.Sync(ui.Services{})
	waitPager(t, func() bool {
		p.Sync(ui.Services{})
		return p.Status().Loaded == 1 && !p.Status().Loading
	})
	if wakes.Load() == 0 {
		t.Fatal("pager result did not invalidate a registered window")
	}
}

func waitPager(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("pager condition did not become true")
		}
		runtime.Gosched()
	}
}
